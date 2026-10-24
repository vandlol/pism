package vtstate

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
)

// piStartup is the shape of what pi's TUI writes once at startup
// (ProcessTerminal.start + TuiAltScreen.beforeTerminalStart): kitty keyboard
// push+query, then alt screen, autowrap off, mouse tracking, cursor hide.
const piStartup = "\x1b[>7u\x1b[?u" +
	"\x1b[?2004h" +
	"\x1b[?1049h\x1b[?7l\x1b[?1000h\x1b[?1002h\x1b[?1003h\x1b[?1004h\x1b[?1006h" +
	"\x1b[2J\x1b[H\x1b[?25l"

func feed(t *Tracker, s string) *Tracker {
	t.Write([]byte(s))
	return t
}

func TestPreambleReassertsPiStartup(t *testing.T) {
	// This is the reported bug: after a detach+reattach the terminal no longer
	// has mouse reporting, so pi's wheel-driven internal scrollback is dead.
	// Resets come first (autowrap off, cursor hidden), then the enables.
	const want = "\x1b[?7;25l" + // autowrap off + cursor hidden (non-default)
		"\x1b[?1000;1002;1003;1004;1006;1049;2004h"
	if got := string(feed(New(), piStartup).Preamble()); got != want {
		t.Fatalf("Preamble = %q;\nwant %q", got, want)
	}
}

func TestPreambleDoesNotRepushKitty(t *testing.T) {
	// Under pism the app never gets a kitty-protocol reply (headless holder
	// pty), so it always runs in modifyOtherKeys fallback. Re-pushing kitty
	// flags would make a capable terminal send CSI-u to an app expecting xterm
	// encodings.
	tr := feed(New(), "\x1b[>7u\x1b[?1002h\x1b[?1006h")
	if got := string(tr.Preamble()); strings.Contains(got, "\x1b[>") {
		t.Fatalf("Preamble %q must not re-push kitty keyboard flags", got)
	}
	if tr.KittyPushDepth() != 1 || tr.KittyFlags() != 7 {
		t.Fatalf("kitty depth/flags = %d/%d; want 1/7", tr.KittyPushDepth(), tr.KittyFlags())
	}
}

func TestModifyOtherKeysReasserted(t *testing.T) {
	tr := feed(New(), "\x1b[>4;2m")
	if got := string(tr.Preamble()); !strings.Contains(got, "\x1b[>4;2m") {
		t.Fatalf("Preamble %q missing modifyOtherKeys-on", got)
	}
	tr = feed(tr, "\x1b[>4;0m")
	if got := string(tr.Preamble()); strings.Contains(got, "\x1b[>4;") {
		t.Fatalf("Preamble %q still has modifyOtherKeys after disable", got)
	}
	if tr.ModifyOtherKeys() != 0 {
		t.Fatalf("ModifyOtherKeys = %d; want 0", tr.ModifyOtherKeys())
	}
}

func TestMouseEncodingAfterTracking(t *testing.T) {
	// 1006 is the coordinate ENCODING; enabling it before the tracking mode that
	// consumes it would leave the terminal encoding reports nothing uses.
	pre := string(feed(New(), "\x1b[?1006h\x1b[?1002h").Preamble())
	iTrack := strings.Index(pre, "1002")
	iEnc := strings.Index(pre, "1006")
	if iTrack < 0 || iEnc < 0 || iTrack > iEnc {
		t.Fatalf("Preamble %q must enable 1002 before 1006", pre)
	}
}

func TestSetResetCycle(t *testing.T) {
	tr := feed(New(), "\x1b[?1000h\x1b[?1002h")
	if got := tr.Modes(); len(got) != 2 || !tr.Has(1002) {
		t.Fatalf("modes = %v; want 1000,1002", got)
	}
	feed(tr, "\x1b[?1002l")
	if tr.Has(1002) || !tr.Has(1000) {
		t.Fatalf("modes after reset = %v; want just 1000", tr.Modes())
	}
	feed(tr, "\x1b[?1000l")
	// Both are default-off modes the app merely switched back off, so there is
	// nothing worth re-asserting on a fresh terminal.
	if pre := tr.Preamble(); pre != nil {
		t.Fatalf("Preamble = %q; want nil when the app left everything at its default", pre)
	}
}

func TestResetOfDefaultOnModeIsReasserted(t *testing.T) {
	// The counterpart: a reset of something the terminal enables by default MUST
	// be re-asserted, otherwise autowrap silently comes back after a re-attach
	// and the app's fixed-width frames wrap.
	tr := feed(New(), "\x1b[?7l\x1b[?25l")
	if got, want := string(tr.Preamble()), "\x1b[?7;25l"; got != want {
		t.Fatalf("Preamble = %q; want %q", got, want)
	}
	// Re-enabling them again returns to the default state: nothing to assert.
	feed(tr, "\x1b[?7h\x1b[?25h")
	if pre := tr.Preamble(); pre != nil {
		t.Fatalf("Preamble = %q; want nil", pre)
	}
}

func TestMultipleModesOneSequence(t *testing.T) {
	tr := feed(New(), "\x1b[?1000;1002;1006h")
	if got := tr.Modes(); len(got) != 3 {
		t.Fatalf("modes = %v; want 3", got)
	}
	feed(tr, "\x1b[?1000;1002;1006l")
	if got := tr.Modes(); len(got) != 0 {
		t.Fatalf("modes = %v; want none", got)
	}
}

func TestByteSplitEquivalence(t *testing.T) {
	// The holder feeds 32 KiB pty reads, so a sequence can be split anywhere.
	want := feed(New(), piStartup)
	wantPre, wantModes := string(want.Preamble()), want.Modes()

	// one byte at a time
	perByte := New()
	for i := 0; i < len(piStartup); i++ {
		perByte.Write([]byte{piStartup[i]})
	}
	if got := string(perByte.Preamble()); got != wantPre {
		t.Fatalf("per-byte preamble %q != whole-buffer %q", got, wantPre)
	}

	// random chunk sizes (deterministic seed)
	rnd := rand.New(rand.NewSource(42))
	chunked := New()
	src := []byte(piStartup)
	for len(src) > 0 {
		n := 1 + rnd.Intn(7)
		if n > len(src) {
			n = len(src)
		}
		chunked.Write(src[:n])
		src = src[n:]
	}
	if got := string(chunked.Preamble()); got != wantPre {
		t.Fatalf("chunked preamble %q != whole-buffer %q", got, wantPre)
	}
	if got := chunked.Modes(); len(got) != len(wantModes) {
		t.Fatalf("chunked modes %v != %v", got, wantModes)
	}
}

func TestPayloadsAreNotModeChanges(t *testing.T) {
	// Mode-looking text inside a string sequence is data. A raw ESC inside a
	// string aborts it (exactly like a real terminal / ECMA-48), so the test
	// cases are: terminated payloads containing mode-looking text, and a
	// payload with no ESC at all whose contents are only base64/text.
	stream := "" +
		"\x1b]0;title ?1000h ?1049h\x07" + // OSC title with mode-looking text
		"\x1b]8;;https://example.com/?1002h\x1b\\x\x1b]8;;\x1b\\" + // OSC-8 hyperlink
		"\x1b_Gf=100,a=T;MTAwMGg=\x1b\\" + // APC kitty image, base64 only
		"\x1bP1$r0m\x1b\\" + // DCS (DECRPM reply), no ESC inside
		"\x1b]4;1;rgb:ffff/0000/0000\x1b\\" // OSC palette
	tr := feed(New(), stream)
	if modes := tr.Modes(); len(modes) != 0 {
		t.Fatalf("modes = %v; want none (payloads must be ignored)", modes)
	}
	if pre := tr.Preamble(); pre != nil {
		t.Fatalf("Preamble = %q; want nil", pre)
	}
	// And a real mode change after all that still lands.
	feed(tr, "\x1b[?1000h")
	if !tr.Has(1000) {
		t.Fatalf("real mode change after payloads was lost: %s", tr)
	}
}

func TestUnknownModesRecordedNotReasserted(t *testing.T) {
	tr := feed(New(), "\x1b[?2048h\x1b[?1000h")
	if got := tr.Unknown(); len(got) != 1 || got[0] != 2048 {
		t.Fatalf("Unknown = %v; want [2048]", got)
	}
	if pre := string(tr.Preamble()); strings.Contains(pre, "2048") {
		t.Fatalf("Preamble %q must not re-assert untracked modes", pre)
	}
}

func TestAnsiNonPrivateModesIgnored(t *testing.T) {
	// CSI 4 h is ANSI IRM (insert mode), not a DEC private mode.
	tr := feed(New(), "\x1b[4h\x1b[20h")
	if got := tr.Modes(); len(got) != 0 || len(tr.Unknown()) != 0 {
		t.Fatalf("modes=%v unknown=%v; want none", got, tr.Unknown())
	}
}

func TestKittyPop(t *testing.T) {
	tr := feed(New(), "\x1b[>7u\x1b[>7u")
	if tr.KittyPushDepth() != 2 {
		t.Fatalf("depth = %d; want 2", tr.KittyPushDepth())
	}
	feed(tr, "\x1b[<u") // bare pop = 1
	if tr.KittyPushDepth() != 1 {
		t.Fatalf("depth after bare pop = %d; want 1", tr.KittyPushDepth())
	}
	feed(tr, "\x1b[<2u") // popping past the bottom clamps at 0
	if tr.KittyPushDepth() != 0 || tr.KittyFlags() != 0 {
		t.Fatalf("depth/flags = %d/%d; want 0/0", tr.KittyPushDepth(), tr.KittyFlags())
	}
}

func TestReset(t *testing.T) {
	tr := feed(New(), piStartup+"\x1b[>4;2m")
	tr.Reset()
	if pre := tr.Preamble(); pre != nil {
		t.Fatalf("Preamble after Reset = %q; want nil", pre)
	}
	if tr.KittyPushDepth() != 0 || tr.ModifyOtherKeys() != 0 {
		t.Fatalf("state survived Reset: %s", tr)
	}
}

func TestStringMentionsEverything(t *testing.T) {
	tr := feed(New(), "\x1b[?1002h\x1b[?7l\x1b[>4;2m\x1b[>7u\x1b[?2048h")
	got := tr.String()
	for _, want := range []string{"on=1002", "off=7", "mok=2", "kitty=1(flags=7)", "unknown=[2048]"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q missing %q", got, want)
		}
	}
}

func TestWriteReportsFullLength(t *testing.T) {
	// io.Writer contract: the holder ignores the count, but a short write would
	// silently drop mode changes.
	tr := New()
	n, err := tr.Write([]byte(piStartup))
	if err != nil || n != len(piStartup) {
		t.Fatalf("Write = (%d, %v); want (%d, nil)", n, err, len(piStartup))
	}
	if !bytes.Contains(tr.Preamble(), []byte("1006")) {
		t.Fatalf("Preamble %q lost the mouse encoding", tr.Preamble())
	}
}
