// Package termreset restores a terminal to a sane state after a full-screen
// program (pi, vim, less) left DEC private modes switched on.
//
// WHY THIS IS A SHARED PACKAGE: the reset has to be emitted by whichever
// process on THIS side of the wire owns the terminal. For `pism attach` that is
// the local pism process; for `pism <host> attach` it is the ssh child's parent
// — also pism. Keeping one byte sequence here means every teardown path (clean
// detach, pi exit, panic, signal, network drop, `pism reset-term`) resets the
// same set of modes, so no path can silently fall behind and leak.
//
// THE BUG CLASS THIS EXISTS FOR: pi enables xterm modifyOtherKeys (and/or the
// Kitty keyboard stack), mouse reporting, bracketed paste, synchronised updates
// and the alternate screen. pism is a transparent passthrough, so those ride
// through to the real terminal emulator. A pism detach leaves pi running (it
// never emits the matching disables), and a dropped ssh connection kills the
// remote pty before it can reset anything — so the LOCAL emulator keeps e.g.
// mouse reporting enabled and the shell prompt prints `ESC[<35;20;5M` on every
// mouse move.
package termreset

import (
	"io"
	"os"

	"golang.org/x/term"
)

// InputProtoReset returns a terminal's keyboard-input encoding to the legacy
// state. Two independent "enhanced key" protocols must be undone:
//
//	\x1b[>4;0m   xterm modifyOtherKeys OFF. The one that actually bit us:
//	             byte-level capture proved pi took the modifyOtherKeys FALLBACK
//	             (it cannot negotiate the Kitty protocol against the holder's
//	             headless pty), so \x1b[>4;2m was left dangling. That state is
//	             INVISIBLE to the Kitty query \x1b[?u (flags read 0), so popping
//	             the Kitty stack alone never fixed it.
//	\x1b[<u ×4   pop the Kitty keyboard stack, in case pi did take the Kitty
//	             path. Over-popping an empty stack is a harmless no-op.
//
// Both are safe to send unconditionally.
var InputProtoReset = []byte("\x1b[>4;0m\x1b[<u\x1b[<u\x1b[<u\x1b[<u")

// Sequence returns the full terminal-restore sequence.
//
// Every entry is an explicit DECRST for a mode a TUI is known to enable and to
// leave behind, because the whole failure mode is "nobody sent the off".
// Everything here is idempotent and harmless when the mode was never set.
//
// When clear is true the screen is also cleared (used on detach so no
// status-bar remnants are left on the primary screen).
func Sequence(clear bool) []byte {
	b := make([]byte, 0, len(InputProtoReset)+224)
	b = append(b, InputProtoReset...)

	// Mouse reporting: every encoding variant, so whichever one pi negotiated
	// (X10, VT200, button-event, any-event, utf8, SGR, urxvt) is turned off.
	b = append(b, "\x1b[?9l"...)    // X10 mouse
	b = append(b, "\x1b[?1000l"...) // VT200 mouse
	b = append(b, "\x1b[?1001l"...) // highlight mouse tracking
	b = append(b, "\x1b[?1002l"...) // button-event tracking
	b = append(b, "\x1b[?1003l"...) // any-event tracking
	b = append(b, "\x1b[?1005l"...) // utf8 coordinate encoding
	b = append(b, "\x1b[?1006l"...) // SGR coordinate encoding
	b = append(b, "\x1b[?1015l"...) // urxvt coordinate encoding

	// Other "the shell keeps receiving escapes" leaks: focus in/out events
	// (ESC[I / ESC[O after every click), bracketed paste (ESC[200~),
	// synchronised updates, kitty grapheme clustering and light/dark reports.
	b = append(b, "\x1b[?1004l"...)
	b = append(b, "\x1b[?2004l"...)
	b = append(b, "\x1b[?2026l"...)
	b = append(b, "\x1b[?2027l"...)
	b = append(b, "\x1b[?2031l"...)

	// Cursor and keyboard/keypad mode leakage.
	b = append(b, "\x1b[?12l"...) // stop cursor blinking
	b = append(b, "\x1b[?1l"...)  // normal (not application) cursor keys
	b = append(b, "\x1b[?66l"...) // normal (not application) keypad
	b = append(b, "\x1b[?7h"...)  // autowrap back on

	// DECSTR soft reset: catch-all for insert mode, origin mode, charset,
	// scroll region, SGR and anything else a TUI reconfigured.
	b = append(b, "\x1b[!p"...)
	b = append(b, "\x1b[r"...)    // full scroll region (DECSTBM)
	b = append(b, "\x1b[0m"...)   // reset SGR attributes
	b = append(b, "\x1b[?25h"...) // show the cursor

	// Leave the alternate screen. 1049 is the common one; 1047 and 47 are the
	// older subsets. All three are no-ops when that screen is not active.
	b = append(b, "\x1b[?1047l"...)
	b = append(b, "\x1b[?47l"...)
	b = append(b, "\x1b[?1049l"...)

	if clear {
		b = append(b, "\x1b[H\x1b[2J"...) // home + clear
	}
	return b
}

// Write sends the restore sequence to w. A nil writer is ignored.
func Write(w io.Writer, clear bool) error {
	if w == nil {
		return nil
	}
	return errOnly(w.Write(Sequence(clear)))
}

// errOnly drops a byte count, keeping only the error (a restore sequence is
// best-effort: a short write to a dying terminal is not actionable).
func errOnly(_ int, err error) error { return err }

// Reset writes the restore sequence to f when f is a terminal, and reports
// whether anything was written. This is the form used by every teardown path:
// it never fails, never panics, and never writes to a redirect.
func Reset(f *os.File, clear bool) bool {
	if f == nil {
		return false
	}
	if !term.IsTerminal(int(f.Fd())) {
		return false
	}
	return Write(f, clear) == nil
}

// ResetStdout is Reset on os.Stdout, the sink every pism teardown path uses.
func ResetStdout(clear bool) bool { return Reset(os.Stdout, clear) }
