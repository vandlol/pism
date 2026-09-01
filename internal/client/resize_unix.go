//go:build !windows

package client

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/vandlol/pism/internal/proto"
)

// watchResize forwards SIGWINCH-driven terminal size changes to the holder.
func watchResize(cw *proto.ConnWriter) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				sendSize(cw)
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
