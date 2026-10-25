//go:build windows

package termreset

import (
	"os"
	"os/signal"
)

// Supervise runs restore() on an interrupt (Ctrl-C / Ctrl-Break) before
// exiting, so the terminal is reset on that path too. Windows has no
// SIGTERM/SIGHUP; os.Interrupt is what the console delivers.
func Supervise(restore func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			if restore != nil {
				restore()
			}
			signal.Stop(ch)
			os.Exit(1)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
