//go:build windows

package client

import (
	"os"
	"time"

	"golang.org/x/term"

	"github.com/vandlol/pism/internal/proto"
)

// watchResize polls the console size on Windows (no SIGWINCH) and forwards
// changes to the holder.
func watchResize(cw *proto.ConnWriter) (stop func()) {
	done := make(chan struct{})
	go func() {
		lastC, lastR := -1, -1
		t := time.NewTicker(400 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if c, r, err := term.GetSize(int(os.Stdout.Fd())); err == nil && c > 0 {
					if c != lastC || r != lastR {
						lastC, lastR = c, r
						sendSize(cw)
					}
				}
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}
