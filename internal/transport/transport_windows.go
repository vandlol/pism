//go:build windows

package transport

import (
	"net"
	"os/user"
	"time"

	"github.com/Microsoft/go-winio"
)

// Endpoint returns the named-pipe path for a session id.
func Endpoint(id string) string {
	return `\\.\pipe\pism-` + id
}

// pipeSecurityDescriptor builds an SDDL granting full access to LocalSystem,
// Administrators, and — crucially — the CURRENT USER's SID. The previous
// descriptor used CO (Creator Owner), which does NOT resolve to a usable SID
// for a directly-created pipe, so a sibling process of the same non-admin user
// (the `pism new` launcher dialing the holder) was denied access and the
// session never "became ready". Granting the owner's real SID fixes that.
// The per-session token in the protocol handshake still gates who may attach.
func pipeSecurityDescriptor() string {
	base := "D:P(A;;GA;;;SY)(A;;GA;;;BA)"
	if u, err := user.Current(); err == nil && u.Uid != "" {
		return base + "(A;;GA;;;" + u.Uid + ")"
	}
	return base + "(A;;GA;;;AU)" // fallback: Authenticated Users
}

// Listen creates the named-pipe listener.
func Listen(endpoint string) (net.Listener, error) {
	cfg := &winio.PipeConfig{
		SecurityDescriptor: pipeSecurityDescriptor(),
		MessageMode:        false,
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	}
	return winio.ListenPipe(endpoint, cfg)
}

// Dial connects to an existing named pipe.
func Dial(endpoint string) (net.Conn, error) {
	d := 5 * time.Second
	return winio.DialPipe(endpoint, &d)
}

// Cleanup is a no-op on Windows (named pipes vanish with the last handle).
func Cleanup(endpoint string) {}
