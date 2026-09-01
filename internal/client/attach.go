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

// Attach connects the current terminal to the given session until the user
// presses the detach key (session keeps running) or pi exits.
// detachKey is the byte sequence that triggers detach; empty disables it.
func Attach(m *session.Meta, detachKey []byte) error {
	nc, err := transport.Dial(m.Endpoint)
	if err != nil {
		return fmt.Errorf("dial session: %w", err)
	}
	defer nc.Close()

	if err := proto.WriteFrame(nc, proto.THello, []byte(m.Token)); err != nil {
		return err
	}
	t, payload, err := proto.ReadFrame(nc)
	if err != nil {
		return err
	}
	if t == proto.TError {
		return fmt.Errorf("attach rejected: %s", string(payload))
	}
	if t != proto.THelloOK {
		return fmt.Errorf("unexpected handshake reply %d", t)
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

	// stdin -> holder, intercepting the detach key.
	detached := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				if len(detachKey) != 0 {
					if i := bytes.Index(chunk, detachKey); i >= 0 {
						if i > 0 {
							_ = cw.Write(proto.TInput, chunk[:i])
						}
						close(detached)
						return
					}
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
			return &ExitError{Code: code}
		}
		if code == -1 {
			fmt.Fprintln(os.Stderr, "\r\npism: session ended.")
		}
		return nil
	case <-detached:
		// The child (pi) is a full-screen TUI: it set a scroll region and a
		// persistent status bar that it won't tear down (it doesn't know we
		// left). Reset the terminal and clear so the shell prompt comes back
		// clean instead of overprinting pi's leftover status bar.
		resetTerm(true)
		if raw {
			restore()
		}
		fmt.Fprintf(os.Stderr, "[detached from %s]\r\n", short(m.ID))
		return nil
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
