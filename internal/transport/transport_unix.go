//go:build !windows

package transport

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// runtimeDir returns a SHORT directory for unix sockets. Socket paths are
// capped at ~104 bytes (macOS) / 108 (Linux), so we must avoid long temp
// paths like macOS's /var/folders/... $TMPDIR. Prefer $XDG_RUNTIME_DIR, else
// a per-uid dir under /tmp.
func runtimeDir() string {
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
		return filepath.Join(x, "pism")
	}
	return filepath.Join("/tmp", "pism-"+strconv.Itoa(os.Getuid()))
}

// Endpoint returns the unix socket path for a session id.
func Endpoint(id string) string {
	return filepath.Join(runtimeDir(), id+".sock")
}

// Listen creates the listening endpoint (removing any stale socket first).
func Listen(endpoint string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(endpoint), 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(endpoint)
	l, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(endpoint, 0o600)
	return l, nil
}

// Dial connects to an existing endpoint.
func Dial(endpoint string) (net.Conn, error) {
	return net.DialTimeout("unix", endpoint, 5*time.Second)
}

// Cleanup removes the endpoint file.
func Cleanup(endpoint string) { _ = os.Remove(endpoint) }
