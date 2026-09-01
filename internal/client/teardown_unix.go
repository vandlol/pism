//go:build !windows

package client

import (
	"os"
	"os/signal"
	"syscall"
)

// installSignalRestore runs restore() when the process is asked to terminate
// (SIGTERM/SIGHUP/SIGINT/SIGQUIT), then re-raises the signal with the default
// disposition so the exit status is correct. This guarantees the terminal's
// input protocols (modifyOtherKeys / Kitty) are reset even on paths where Go's
// deferred functions would not run. Returns a stop func to uninstall it.
//
// SIGKILL cannot be caught (nothing can); the "SIGKILL-adjacent" acceptance
// case is covered as best-effort by every other path resetting the terminal.
func installSignalRestore(restore func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-ch:
			restore()
			// Re-raise with default handling for the correct exit code.
			signal.Stop(ch)
			if s, ok := sig.(syscall.Signal); ok {
				_ = syscall.Kill(os.Getpid(), s)
			}
			os.Exit(1)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
