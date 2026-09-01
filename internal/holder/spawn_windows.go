//go:build windows

package holder

import "syscall"

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detachAttr starts the holder detached from the current console so it keeps
// running after the launching shell / ssh session exits. The holder creates
// its own ConPTY for pi, so it needs no inherited console.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
