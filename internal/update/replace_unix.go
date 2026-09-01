//go:build !windows

package update

import "os"

// replaceExecutable atomically swaps the running binary. On Unix a running
// executable can be renamed over (the open text file keeps the old inode),
// so a single rename on the same filesystem is safe.
func replaceExecutable(self, tmp string) error {
	return os.Rename(tmp, self)
}
