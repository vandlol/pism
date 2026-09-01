// Package client implements attaching a local terminal to a session holder.
package client

import (
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
// detachKey is the byte that triggers detach; 0 disables it.
func Attach(m *session.Meta, detachKey byte) error {
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
				if detachKey != 0 {
					if i := indexByte(chunk, detachKey); i >= 0 {
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
		if raw {
			restore()
		}
		fmt.Fprintf(os.Stderr, "\r\n[detached from %s]\r\n", short(m.ID))
		return nil
	}
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

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
