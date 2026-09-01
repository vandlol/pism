//go:build windows

package update

import "os"

// replaceExecutable swaps the running binary on Windows, where an executing
// image cannot be overwritten directly. We move the current binary aside to
// <self>.old (allowed while running), then rename the new file into place.
// The stale .old is cleaned up best-effort on the next update.
func replaceExecutable(self, tmp string) error {
	old := self + ".old"
	_ = os.Remove(old) // remove leftover from a previous update
	if err := os.Rename(self, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, self); err != nil {
		// try to roll back
		_ = os.Rename(old, self)
		return err
	}
	_ = os.Remove(old) // may fail if still locked; harmless
	return nil
}
