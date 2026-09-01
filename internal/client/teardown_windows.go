//go:build windows

package client

import (
	"os"
	"os/signal"
)

// installSignalRestore runs restore() on an interrupt (Ctrl-C / Ctrl-Break)
// before exiting, so the terminal's input protocols are reset on that path too.
// Windows has no SIGTERM/SIGHUP; os.Interrupt is what the console delivers.
func installSignalRestore(restore func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			restore()
			os.Exit(1)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
