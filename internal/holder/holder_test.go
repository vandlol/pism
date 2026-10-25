package holder

import (
	"strings"
	"testing"

	"github.com/vandlol/pism/internal/vtstate"
)

func newTestHolder() *Holder {
	return &Holder{clients: map[*client]struct{}{}, tracker: vtstate.New()}
}

func TestAppendRingFeedsTracker(t *testing.T) {
	// The holder is the only process that sees every byte the app ever wrote, so
	// the attach-time mode re-assert depends on this wiring: ring write in,
	// mode state out.
	h := newTestHolder()
	h.appendRing([]byte("\x1b[?1049h\x1b[?7l\x1b[?1000h\x1b[?1002h\x1b[?1006h\x1b[?25l"))

	pre := string(h.tracker.Preamble())
	for _, want := range []string{"\x1b[?7;25l", "1000", "1002", "1006", "1049"} {
		if !strings.Contains(pre, want) {
			t.Fatalf("preamble %q missing %q", pre, want)
		}
	}
}

func TestAppendRingTracksAcrossChunks(t *testing.T) {
	// pty reads are 32 KiB, so sequences arrive split; the tracker must not care.
	h := newTestHolder()
	for _, chunk := range []string{"\x1b[?10", "02h\x1b[", "?1006", "h"} {
		h.appendRing([]byte(chunk))
	}
	pre := string(h.tracker.Preamble())
	if !strings.Contains(pre, "1002") || !strings.Contains(pre, "1006") {
		t.Fatalf("preamble %q lost a mode split across reads", pre)
	}
}

func TestAppendRingKeepsRingWindow(t *testing.T) {
	// The tracker must not change the replay buffer's truncation behaviour.
	h := newTestHolder()
	big := strings.Repeat("x", ringMax+1024)
	h.appendRing([]byte(big))
	if len(h.ring) != ringMax {
		t.Fatalf("ring len = %d; want %d", len(h.ring), ringMax)
	}
}
