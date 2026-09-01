//go:build !windows

package holder

import "syscall"

// detachAttr starts the holder in its own session so it survives the parent
// terminal / ssh connection going away.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
