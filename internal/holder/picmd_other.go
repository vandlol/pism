//go:build !windows

package holder

// resolvePiCommand is a no-op on Unix: pi is a real executable and runs
// directly under the pty.
func resolvePiCommand(name string, args []string) (string, []string) {
	return name, args
}
