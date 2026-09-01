//go:build windows

package transport

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// Endpoint returns the named-pipe path for a session id.
func Endpoint(id string) string {
	return `\\.\pipe\pism-` + id
}

// Listen creates the named-pipe listener. Access is gated by the per-session
// token in the protocol handshake; the pipe SDDL below limits to the local
// system, administrators and the creating user's implicit access.
func Listen(endpoint string) (net.Listener, error) {
	cfg := &winio.PipeConfig{
		// Deny remote/anonymous; allow creator + admins + system.
		SecurityDescriptor: "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;CO)",
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
