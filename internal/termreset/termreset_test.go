package termreset

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSequenceDisablesInputProtocols(t *testing.T) {
	// The regression this guards: teardown must emit modifyOtherKeys-off, not
	// just the Kitty pop. Without \x1b[>4;0m the terminal stays wedged (arrows,
	// Shift+Tab, F-keys keep sending escapes that only pi parses).
	seq := Sequence(false)
	if !bytes.Contains(seq, []byte("\x1b[>4;0m")) {
		t.Fatalf("Sequence %q missing modifyOtherKeys-off \\x1b[>4;0m", seq)
	}
	if !bytes.Contains(seq, []byte("\x1b[<u")) {
		t.Fatalf("Sequence %q missing Kitty pop \\x1b[<u", seq)
	}
	if !bytes.HasPrefix(seq, InputProtoReset) {
		t.Fatalf("Sequence must start with InputProtoReset, got %q", seq)
	}
}

func TestSequenceDisablesMouseEveryEncoding(t *testing.T) {
	// A dropped ssh connection loses the reset the remote pty would have sent,
	// so the LOCAL emulator keeps mouse reporting on and the shell prompt prints
	// ESC[<35;20;5M on every mouse move. Every encoding pi can negotiate has to
	// be turned off, not just the SGR one.
	seq := string(Sequence(false))
	for _, mode := range []string{
		"\x1b[?9l", "\x1b[?1000l", "\x1b[?1001l", "\x1b[?1002l", "\x1b[?1003l",
		"\x1b[?1005l", "\x1b[?1006l", "\x1b[?1015l",
	} {
		if !strings.Contains(seq, mode) {
			t.Errorf("Sequence missing mouse-off %q", mode)
		}
	}
}

func TestSequenceRestoresShellVisibleState(t *testing.T) {
	seq := string(Sequence(false))
	for _, want := range []string{
		"\x1b[?1004l", // focus reporting: ESC[I / ESC[O after every click
		"\x1b[?2004l", // bracketed paste
		"\x1b[?2026l", // synchronised updates
		"\x1b[?1l",    // normal cursor keys
		"\x1b[?7h",    // autowrap back on
		"\x1b[!p",     // DECSTR catch-all
		"\x1b[r",      // full scroll region
		"\x1b[0m",     // SGR reset
		"\x1b[?25h",   // cursor visible
		"\x1b[?1049l", // leave alternate screen
	} {
		if !strings.Contains(seq, want) {
			t.Errorf("Sequence missing %q", want)
		}
	}
}

func TestSequenceClearIsOptInAndLast(t *testing.T) {
	plain := Sequence(false)
	cleared := Sequence(true)
	if strings.Contains(string(plain), "\x1b[2J") {
		t.Fatalf("Sequence(false) must not clear the screen: %q", plain)
	}
	if !bytes.HasSuffix(cleared, []byte("\x1b[H\x1b[2J")) {
		t.Fatalf("Sequence(true) must end with home+clear, got %q", cleared)
	}
	// Everything else identical, so a caller can't lose a reset by clearing.
	if !bytes.HasPrefix(cleared, plain) {
		t.Fatalf("Sequence(true) must start with Sequence(false)")
	}
}

func TestSequenceEndsInAltScreenOffBeforeClear(t *testing.T) {
	// Order matters: leaving the alternate screen after clearing would wipe the
	// primary screen we just cleared, and leaving it after showing the cursor
	// re-hides it in xterm's 1049 restore path.
	seq := string(Sequence(true))
	iAlt := strings.Index(seq, "\x1b[?1049l")
	iClear := strings.Index(seq, "\x1b[2J")
	iCursor := strings.Index(seq, "\x1b[?25h")
	if iAlt < 0 || iClear < 0 || iCursor < 0 {
		t.Fatalf("missing a key sequence: alt=%d clear=%d cursor=%d", iAlt, iClear, iCursor)
	}
	if !(iCursor < iAlt && iAlt < iClear) {
		t.Fatalf("want cursor<alt<clear, got cursor=%d alt=%d clear=%d", iCursor, iAlt, iClear)
	}
}

func TestResetIgnoresNonTerminal(t *testing.T) {
	// Teardown paths call Reset unconditionally; writing into a redirect would
	// corrupt piped output (e.g. `pism ls | grep`).
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if Reset(w, false) {
		w.Close()
		t.Fatal("Reset() reported a write to a non-terminal pipe")
	}
	_ = w.Close() // EOF, so the read below can't block
	b, _ := io.ReadAll(r)
	if len(b) != 0 {
		t.Fatalf("Reset wrote %q to a pipe", b)
	}
}

func TestWriteReachesWriter(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, true); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.Len() != len(Sequence(true)) {
		t.Fatalf("wrote %d bytes, want %d", buf.Len(), len(Sequence(true)))
	}
	if err := Write(nil, false); err != nil {
		t.Fatalf("Write(nil) should be a no-op, got %v", err)
	}
}
