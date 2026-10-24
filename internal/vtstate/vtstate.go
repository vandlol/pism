// Package vtstate tracks the terminal modes a byte stream switches on, so the
// same state can be re-asserted on a fresh terminal later.
//
// WHY THIS EXISTS: pism is a transparent passthrough, so the app inside (pi)
// enables mouse reporting, the alternate screen, bracketed paste, autowrap-off
// and its keyboard-encoding choice directly on the user's real terminal — once,
// at startup. Two things then lose that state:
//
//   - pism resets those modes on every detach (internal/termreset), because
//     otherwise the bare shell prompt prints mouse escapes; and
//   - the holder can only replay the last ringMax (256 KiB) of pty output to a
//     newly attached client, so the app's startup bytes are long gone by the
//     time you re-attach.
//
// Net effect: pi believes the mouse is on, the terminal has it off, and pi's
// internal (fullscreen) scrollback — which is driven by mouse-wheel events —
// can never be reached again. Same for the alternate screen, autowrap and
// bracketed paste. The holder is the only process that sees every byte the app
// ever wrote, so it tracks the state here and replays it as a preamble.
package vtstate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

// trackedModes is the set of DEC private modes whose state we re-assert on
// attach. It is deliberately exactly the set internal/termreset knows how to
// turn OFF, so "assert" and "reset" cannot drift apart.
var trackedModes = map[int]bool{
	1:    true, // DECCKM, application cursor keys
	5:    true, // DECSCNM, reverse video
	6:    true, // DECOM, origin mode
	7:    true, // DECAWM, autowrap (pi turns it OFF: ESC[?7l)
	9:    true, // X10 mouse
	12:   true, // cursor blinking
	25:   true, // DECTCEM, cursor visible (pi hides it: ESC[?25l)
	47:   true, // alt screen (legacy)
	66:   true, // DECNKM, application keypad
	69:   true, // DECLRMM, left/right margins
	1000: true, // VT200 mouse
	1001: true, // highlight mouse tracking
	1002: true, // button-event mouse tracking
	1003: true, // any-event mouse tracking
	1004: true, // focus in/out reporting
	1005: true, // utf8 mouse coordinates
	1006: true, // SGR mouse coordinates
	1015: true, // urxvt mouse coordinates
	1047: true, // alt screen
	1049: true, // alt screen + save/restore cursor
	2004: true, // bracketed paste
	2026: true, // synchronised output
	2027: true, // grapheme clustering
	2031: true, // light/dark appearance reports
}

// defaultOn lists the tracked modes a fresh terminal already has ENABLED, so a
// only a RESET of one of these is worth re-asserting. A reset of an
// already-default-off mode (mouse, alt screen, …) is redundant; re-asserting it
// would only make every attach emit noise.
var defaultOn = map[int]bool{
	7:  true, // autowrap is on by default (pi turns it off)
	12: true, // cursor blinking is on by default
	25: true, // cursor is visible by default (pi hides it)
}

// Tracker accumulates terminal mode state from a pty output stream.
//
// Write is incremental and safe across arbitrary chunk boundaries (the ANSI
// parser carries its own state), and string payloads (OSC/DCS/APC/PM/SOS) are
// consumed by the parser instead of being read as mode changes, so a mode-looking
// substring inside an OSC title or a kitty-graphics payload cannot wedge the
// terminal.
//
// A Tracker is not safe for concurrent use; the holder feeds it from its single
// pty reader.
type Tracker struct {
	p *ansi.Parser

	// explicit is the last setting the app applied for a tracked mode: true =
	// set, false = reset. Modes the app never touched are absent, so "was never
	// mentioned" stays distinguishable from "explicitly turned off".
	explicit map[int]bool

	// Kitty keyboard stack: depth and the flags of the most recent push. Tracked
	// for diagnostics and for a future precise teardown, but NOT re-asserted —
	// see Preamble.
	kittyDepth int
	kittyFlags int

	modifyOtherKeys int // 0 = off, 1..5 = ESC[>4;<n>m level

	unknown map[int]bool // '?'-prefixed modes we saw but don't track
}

// New returns an empty tracker.
func New() *Tracker {
	return &Tracker{
		p:        ansi.NewParser(),
		explicit: make(map[int]bool, len(trackedModes)),
		unknown:  make(map[int]bool),
	}
}

// Write feeds pty output to the tracker. It always consumes the whole slice.
func (t *Tracker) Write(p []byte) (int, error) {
	for _, b := range p {
		// The parser's state is only meaningful BEFORE the byte that dispatches
		// a sequence: a dispatch transitions it straight back to Ground, so
		// reading State() afterwards tells us nothing about what was dispatched.
		prev := t.p.State()
		if t.p.Advance(b) != parser.DispatchAction {
			continue
		}
		switch prev {
		case parser.CsiEntryState, parser.CsiParamState, parser.CsiIntermediateState:
			t.csi(ansi.Cmd(t.p.Command()), t.p.Params())
		}
	}
	return len(p), nil
}

// csi applies one dispatched CSI sequence.
func (t *Tracker) csi(cmd ansi.Cmd, params ansi.Params) {
	switch cmd.Prefix() {
	case '?': // DEC private modes: CSI ? Pm h|l
		switch cmd.Final() {
		case 'h', 'l':
			set := cmd.Final() == 'h'
			params.ForEach(0, func(_ int, mode int, _ bool) {
				if mode <= 0 {
					return
				}
				if !trackedModes[mode] {
					t.unknown[mode] = true
					return
				}
				if set {
					t.explicit[mode] = true
				} else {
					t.explicit[mode] = false
				}
			})
		}
	case '>': // "progressive" forms
		switch cmd.Final() {
		case 'u': // kitty keyboard push: CSI > flags u
			flags, _, _ := params.Param(0, 0)
			t.kittyDepth++
			t.kittyFlags = flags
		case 'm': // xterm modifyOtherKeys: CSI > 4 ; n m
			if res, _, _ := params.Param(0, 0); res == 4 {
				n, _, _ := params.Param(1, 0)
				t.modifyOtherKeys = n
			}
		}
	case '<': // kitty keyboard pop: CSI < n u
		if cmd.Final() == 'u' {
			n, _, ok := params.Param(0, 1)
			if !ok || n < 1 {
				n = 1 // bare CSI < u pops one level
			}
			if t.kittyDepth -= n; t.kittyDepth < 0 {
				t.kittyDepth = 0
			}
			if t.kittyDepth == 0 {
				t.kittyFlags = 0
			}
		}
	}
}

// Preamble returns the byte sequence that re-establishes the tracked state on a
// freshly attached terminal, or nil when nothing needs asserting.
//
// Two groups, resets first:
//
//	DECRST  modes a fresh terminal has ON but the app turned OFF (autowrap,
//	        cursor visibility, cursor blink);
//	DECSET  modes the app turned ON (alternate screen, mouse tracking, focus
//	        reporting, bracketed paste, synchronised output, …), ascending so the
//	        mouse coordinate ENCODING (1006) lands after the tracking mode that
//	        consumes it (1002/1003).
//
// The Kitty keyboard push is deliberately NOT re-asserted. Under pism the app
// runs against the holder's headless pty, so its ESC[?u query is never answered
// and it always takes the modifyOtherKeys fallback; re-pushing kitty flags on a
// terminal that supports the protocol would start sending CSI-u to an app that
// is not expecting it. modifyOtherKeys (which the app DID enable) is re-asserted
// instead, and internal/termreset pops the kitty stack on teardown.
func (t *Tracker) Preamble() []byte {
	var b strings.Builder

	if off := t.resets(); len(off) > 0 {
		b.WriteString(ansi.DECRST(decmode(off)...))
	}
	if t.modifyOtherKeys > 0 {
		fmt.Fprintf(&b, "\x1b[>4;%dm", t.modifyOtherKeys)
	}
	if on := t.asserted(); len(on) > 0 {
		b.WriteString(ansi.DECSET(decmode(on)...))
	}

	if b.Len() == 0 {
		return nil
	}
	return []byte(b.String())
}

// decmode converts mode numbers to the ansi builder's Mode values.
func decmode(modes []int) []ansi.Mode {
	out := make([]ansi.Mode, len(modes))
	for i, m := range modes {
		out[i] = ansi.DECMode(m)
	}
	return out
}

// Modes returns the tracked DEC private modes the app has switched ON, ascending.
// This is the raw state (including modes a fresh terminal has on by default);
// Preamble filters those out.
func (t *Tracker) Modes() []int { return t.withValue(true) }

// asserted returns the modes worth switching ON on a fresh terminal: those the
// app turned on that are not already on by default.
func (t *Tracker) asserted() []int {
	out := t.withValue(true)
	kept := out[:0]
	for _, m := range out {
		if !defaultOn[m] {
			kept = append(kept, m)
		}
	}
	return kept
}

// resets returns the tracked modes the app has switched OFF that a fresh
// terminal would otherwise have ON — the only resets worth re-asserting.
func (t *Tracker) resets() []int {
	out := t.withValue(false)
	kept := out[:0]
	for _, m := range out {
		if defaultOn[m] {
			kept = append(kept, m)
		}
	}
	return kept
}

// withValue returns the explicit modes matching value (set or reset), ascending.
func (t *Tracker) withValue(value bool) []int {
	out := make([]int, 0, len(t.explicit))
	for m, v := range t.explicit {
		if v == value {
			out = append(out, m)
		}
	}
	sort.Ints(out)
	return out
}

// Has reports whether a DEC private mode is currently set.
func (t *Tracker) Has(mode int) bool {
	v, ok := t.explicit[mode]
	return ok && v
}

// Unknown returns the '?'-prefixed modes we saw but do not track, ascending.
// A non-empty result is a signal to extend trackedModes (and termreset).
func (t *Tracker) Unknown() []int {
	out := make([]int, 0, len(t.unknown))
	for m := range t.unknown {
		if !trackedModes[m] {
			out = append(out, m)
		}
	}
	sort.Ints(out)
	return out
}

// KittyPushDepth returns the number of unpopped kitty keyboard pushes seen.
func (t *Tracker) KittyPushDepth() int { return t.kittyDepth }

// KittyFlags returns the flags of the most recent kitty keyboard push.
func (t *Tracker) KittyFlags() int { return t.kittyFlags }

// ModifyOtherKeys returns the active xterm modifyOtherKeys level (0 = off).
func (t *Tracker) ModifyOtherKeys() int { return t.modifyOtherKeys }

// String renders the state for logs, e.g.
// "on=1000,1002,1003,1006 off=7,25 mok=2 kitty=1(flags=7) unknown=[2048]".
func (t *Tracker) String() string {
	join := func(modes []int) string {
		names := make([]string, len(modes))
		for i, m := range modes {
			names[i] = strconv.Itoa(m)
		}
		return strings.Join(names, ",")
	}
	s := "on=" + join(t.asserted())
	if off := t.resets(); len(off) > 0 {
		s += " off=" + join(off)
	}
	if t.modifyOtherKeys > 0 {
		s += fmt.Sprintf(" mok=%d", t.modifyOtherKeys)
	}
	if t.kittyDepth > 0 {
		s += fmt.Sprintf(" kitty=%d(flags=%d)", t.kittyDepth, t.kittyFlags)
	}
	if u := t.Unknown(); len(u) > 0 {
		s += fmt.Sprintf(" unknown=%v", u)
	}
	return s
}

// Reset clears the tracker.
func (t *Tracker) Reset() {
	t.p.Reset()
	t.explicit = make(map[int]bool, len(trackedModes))
	t.unknown = make(map[int]bool)
	t.kittyDepth, t.kittyFlags, t.modifyOtherKeys = 0, 0, 0
}
