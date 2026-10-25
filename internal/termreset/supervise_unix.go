//go:build !windows

package termreset

import (
	"os"
	"os/signal"
	"syscall"
)

// Supervise runs restore() when the process is asked to terminate
// (SIGTERM/SIGHUP/SIGINT/SIGQUIT), then re-raises the signal with the default
// disposition so the exit status is correct. This guarantees the terminal is
// restored on the paths where Go's deferred functions never run — a dropped ssh
// connection (SIGHUP) is exactly that case.
func Supervise(restore func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT)
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-ch:
			if restore != nil {
				restore()
			}
			signal.Stop(ch)
			// Re-raise with default handling for the correct exit code.
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
