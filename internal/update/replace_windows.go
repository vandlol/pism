//go:build windows

package update

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// replaceExecutable swaps the running binary on Windows, where an executing
// image can't be overwritten directly. We move the current binary aside to a
// UNIQUE sidelined name, then rename the new file into place.
//
// The unique name matters: the sidelined file stays locked until the old
// process exits, so a fixed ".old" name from a previous update can remain
// locked and block the next rename ("Access is denied"). A per-run unique name
// (`.old-<nanos>`) never collides with a locked leftover. Stale sidelined files
// are swept best-effort on each update.
func replaceExecutable(self, tmp string) error {
	sweepSidelined(self)

	old := fmt.Sprintf("%s.old-%d", self, time.Now().UnixNano())
	if err := os.Rename(self, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, self); err != nil {
		_ = os.Rename(old, self) // roll back
		return err
	}
	_ = os.Remove(old) // locked while this process runs; swept next time
	return nil
}

// sweepSidelined removes leftover ".old" / ".old-*" binaries from prior
// updates. Files still locked by a running process are skipped silently.
func sweepSidelined(self string) {
	_ = os.Remove(self + ".old")
	if matches, err := filepath.Glob(self + ".old-*"); err == nil {
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
}
