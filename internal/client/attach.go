// Package client implements attaching a local terminal to a session holder.
package client

import (
	"bytes"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/vandlol/pism/internal/proto"
	"github.com/vandlol/pism/internal/session"
	"github.com/vandlol/pism/internal/transport"
)

// DefaultDetach is Ctrl-\ (0x1c), matching dtach's default.
const DefaultDetach = 0x1c

// detachVariants returns every byte sequence that should trigger a detach for
// the given key. Kitty-keyboard CSI-u sequences (ESC [ <code> [;<mods>] u,
// used for F13-F20) are the tricky case: the Kitty spec makes the modifier
// field optional when there are no modifiers, so kitty itself emits
// "ESC[57379u" while wezterm/ghostty (and this user's terminal) emit
// "ESC[57379;1u". A literal match against one form silently fails on the
// other, so for a CSI-u key we match BOTH the bare and the ";1" form.
func detachVariants(k []byte) [][]byte {
	if len(k) == 0 {
		return nil
	}
	variants := [][]byte{k}
	// Recognize ESC '[' <digits> ('u' | ';1u').
	if len(k) >= 4 && k[0] == 0x1b && k[1] == '[' && k[len(k)-1] == 'u' {
		mid := k[2 : len(k)-1] // params between '[' and 'u'
		switch {
		case bytes.HasSuffix(mid, []byte(";1")):
			// have "<n>;1" -> also add bare "<n>"
			bare := append([]byte{0x1b, '['}, mid[:len(mid)-2]...)
			variants = append(variants, append(bare, 'u'))
		case isAllDigits(mid):
			// have bare "<n>" -> also add "<n>;1"
			withMod := append([]byte{0x1b, '['}, mid...)
			withMod = append(withMod, ';', '1', 'u')
			variants = append(variants, withMod)
		}
	}
	return variants
}

func isAllDigits(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// indexAny returns the earliest index at which any of keys occurs in chunk,
// plus the matched key's length, or (-1, 0) if none match.
func indexAny(chunk []byte, keys [][]byte) (int, int) {
	best, bestLen := -1, 0
	for _, k := range keys {
		if len(k) == 0 {
			continue
		}
		if i := bytes.Index(chunk, k); i >= 0 && (best == -1 || i < best) {
			best, bestLen = i, len(k)
		}
	}
	return best, bestLen
}

// Outcome reports why Attach returned, so the caller can decide whether to
// re-attach to an adjacent session (switch), stop cleanly (detach/exit), or
// propagate an exit code.
type Outcome int

const (
	// OutcomeExit means pi exited or the session ended.
	OutcomeExit Outcome = iota
	// OutcomeDetach means the user pressed the detach key.
	OutcomeDetach
	// OutcomeSwitchPrev means the user asked to attach to the previous session.
	OutcomeSwitchPrev
	// OutcomeSwitchNext means the user asked to attach to the next session.
	OutcomeSwitchNext
)

// Keys bundles the byte sequences Attach intercepts. Any field may be empty to
// disable that binding.
type Keys struct {
	Detach     []byte
	SwitchPrev []byte
	SwitchNext []byte
}

// Attach connects the current terminal to the given session until the user
// presses the detach key (session keeps running), presses a switch key
// (detach and signal the caller to move to an adjacent session), or pi exits.
func Attach(m *session.Meta, keys Keys) (Outcome, error) {
	nc, err := transport.Dial(m.Endpoint)
	if err != nil {
		return OutcomeExit, fmt.Errorf("dial session: %w", err)
	}
	defer nc.Close()

	if err := proto.WriteFrame(nc, proto.THello, []byte(m.Token)); err != nil {
		return OutcomeExit, err
	}
	t, payload, err := proto.ReadFrame(nc)
	if err != nil {
		return OutcomeExit, err
	}
	if t == proto.TError {
		return OutcomeExit, fmt.Errorf("attach rejected: %s", string(payload))
	}
	if t != proto.THelloOK {
		return OutcomeExit, fmt.Errorf("unexpected handshake reply %d", t)
	}

	in := int(os.Stdin.Fd())
	restore, raw := enterRaw(in)
	if raw {
		defer restore()
	}
	// Safety net: whatever happens (clean detach, pi exit, or a panic), make
	// sure we pop the Kitty keyboard protocol that the inner app pushed, so the
	// user's terminal never gets wedged (Shift emitting escape codes). Cheap
	// and idempotent — popping an empty Kitty stack is a no-op.
	defer popKitty()

	// NOTE: we deliberately do NOT push the Kitty keyboard protocol ourselves.
	// pism is a transparent passthrough, not a translating multiplexer: if we
	// enabled Kitty on the local terminal, every non-Kitty inner program (a
	// bare shell, vim, less) would get CSI-u for Escape/Ctrl/Alt and break.
	// Kitty-encoded detach keys (f13-f20) are matched only while the inner app
	// (pi) has the protocol active — which is exactly the foreground use case.
	// We still POP on teardown (resetTerm) so pi's push never leaks to the
	// shell.

	// All writes to the connection go through cw so goroutines don't interleave.
	cw := proto.NewConnWriter(nc)

	// Initial size + live resize notifications.
	sendSize(cw)
	stopResize := watchResize(cw)
	defer stopResize()

	exitCode := make(chan int, 1)

	// Holder -> stdout
	go func() {
		for {
			mt, mp, err := proto.ReadFrame(nc)
			if err != nil {
				exitCode <- -1
				return
			}
			switch mt {
			case proto.TOutput:
				_, _ = os.Stdout.Write(mp)
			case proto.TExit:
				exitCode <- proto.DecodeExit(mp)
				return
			case proto.TError:
				fmt.Fprintf(os.Stderr, "\r\npism: %s\r\n", string(mp))
			}
		}
	}()

	// stdin -> holder, intercepting the detach and switch keys. We match every
	// encoding variant of each key (e.g. Kitty CSI-u with or without the ";1"
	// no-mod field) so F13-F20 work across kitty/wezterm/ghostty.
	//
	// Each interceptor is tried in order; the earliest match in the chunk wins
	// so a switch key is never swallowed by a later detach match (or vice
	// versa). intent carries which action fired to the select below.
	type interceptor struct {
		keys   [][]byte
		intent Outcome
	}
	interceptors := []interceptor{
		{detachVariants(keys.Detach), OutcomeDetach},
		{detachVariants(keys.SwitchPrev), OutcomeSwitchPrev},
		{detachVariants(keys.SwitchNext), OutcomeSwitchNext},
	}
	stopped := make(chan Outcome, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				// Find the earliest interceptor match across all bindings.
				hitIdx, hitIntent := -1, OutcomeExit
				for _, ic := range interceptors {
					if len(ic.keys) == 0 {
						continue
					}
					if i, _ := indexAny(chunk, ic.keys); i >= 0 && (hitIdx == -1 || i < hitIdx) {
						hitIdx, hitIntent = i, ic.intent
					}
				}
				if hitIdx >= 0 {
					if hitIdx > 0 {
						_ = cw.Write(proto.TInput, chunk[:hitIdx])
					}
					stopped <- hitIntent
					return
				}
				if err2 := cw.Write(proto.TInput, chunk); err2 != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case code := <-exitCode:
		resetTerm(false)
		if raw {
			restore()
		}
		if code > 0 {
			return OutcomeExit, &ExitError{Code: code}
		}
		if code == -1 {
			fmt.Fprintln(os.Stderr, "\r\npism: session ended.")
		}
		return OutcomeExit, nil
	case intent := <-stopped:
		// The child (pi) is a full-screen TUI: it set a scroll region and a
		// persistent status bar that it won't tear down (it doesn't know we
		// left). Reset the terminal and clear so the shell prompt comes back
		// clean instead of overprinting pi's leftover status bar.
		resetTerm(true)
		if raw {
			restore()
		}
		if intent == OutcomeDetach {
			fmt.Fprintf(os.Stderr, "[detached from %s]\r\n", short(m.ID))
		}
		return intent, nil
	}
}

// kittyDrain pops the Kitty keyboard protocol off the terminal's stack several
// times. pi (and other TUIs) push it (flags 7, incl. report-event-types) and
// do NOT pop on a pism detach because they keep running, oblivious that we
// left. If we don't pop, wezterm/kitty/foot are left in Kitty mode at the bare
// shell and modifier keys (Shift!) start emitting escape codes instead of
// typing. Popping an empty stack is a no-op, so over-draining is safe while
// under-draining wedges the terminal.
var kittyDrain = []byte("\x1b[<u\x1b[<u\x1b[<u\x1b[<u")

// popKitty writes the Kitty drain directly (used as a defer safety net so an
// abnormal exit path still restores the terminal).
func popKitty() {
	if term.IsTerminal(int(os.Stdout.Fd())) {
		_, _ = os.Stdout.Write(kittyDrain)
	}
}

// resetTerm returns the terminal to a sane state after a full-screen child.
// It resets the scroll region and the modes a TUI commonly enables, shows the
// cursor and resets attributes. When clear is true it also clears the screen
// (used on detach so no status-bar remnants remain).
func resetTerm(clear bool) {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return
	}
	var b []byte
	b = append(b, kittyDrain...)      // return input encoding to legacy (see popKitty)
	b = append(b, "\x1b[r"...)        // reset scroll region (DECSTBM -> full)
	b = append(b, "\x1b[?25h"...)     // show cursor
	b = append(b, "\x1b[?2004l"...)   // bracketed paste off
	b = append(b, "\x1b[?1000l"...)   // mouse tracking off
	b = append(b, "\x1b[?1002l"...)
	b = append(b, "\x1b[?1003l"...)
	b = append(b, "\x1b[?1006l"...)
	b = append(b, "\x1b[?2026l"...)   // synchronized-update off
	b = append(b, "\x1b[?1049l"...)   // leave alternate screen (no-op if not set)
	b = append(b, "\x1b[0m"...)       // reset attributes
	if clear {
		b = append(b, "\x1b[H\x1b[2J"...) // home + clear screen
	}
	_, _ = os.Stdout.Write(b)
}

// ExitError carries pi's exit code so the CLI can propagate it.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("session exited with code %d", e.Code) }

func enterRaw(fd int) (restore func(), ok bool) {
	if !term.IsTerminal(fd) {
		return func() {}, false
	}
	st, err := term.MakeRaw(fd)
	if err != nil {
		return func() {}, false
	}
	return func() { _ = term.Restore(fd, st) }, true
}

func sendSize(cw *proto.ConnWriter) {
	cols, rows := 80, 24
	if c, r, err := term.GetSize(int(os.Stdout.Fd())); err == nil && c > 0 {
		cols, rows = c, r
	}
	_ = cw.Write(proto.TResize, proto.EncodeResize(cols, rows))
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
