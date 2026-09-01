package holder

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/vandlol/pism/internal/session"
	"github.com/vandlol/pism/internal/transport"
)

// Launch spawns a detached holder for a new session and waits until it is
// ready to accept attaches. readyTimeout bounds the wait; a value <= 0 waits
// indefinitely (useful when pi is slow to spin up). Returns the session id.
func Launch(cwd, piCmd string, extraArgs []string, readyTimeout time.Duration) (string, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if _, err := session.EnsureSessionsDir(); err != nil {
		return "", err
	}
	id := session.NewID()

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}

	argv := []string{"__holder", "--id", id, "--cwd", cwd, "--pi", piCmd}
	if len(extraArgs) > 0 {
		argv = append(argv, "--")
		argv = append(argv, extraArgs...)
	}

	logf, err := os.Create(session.LogPath(id))
	if err != nil {
		return "", err
	}
	defer logf.Close()

	cmd := exec.Command(exe, argv...)
	cmd.Dir = cwd
	cmd.Stdin = nil
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return "", err
	}

	// Detect the holder dying before it becomes ready (so an infinite wait
	// can't hang forever on a crashed holder).
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// Wait for readiness: metadata written + endpoint dialable.
	endpoint := transport.Endpoint(id)
	start := time.Now()
	nextNotice := 5 * time.Second
	for {
		if _, err := session.Load(id); err == nil {
			if c, err := transport.Dial(endpoint); err == nil {
				c.Close()
				return id, nil
			}
		}
		select {
		case werr := <-exited:
			return id, fmt.Errorf("holder exited before becoming ready (%v); see %s", werr, session.LogPath(id))
		default:
		}
		if readyTimeout > 0 && time.Since(start) >= readyTimeout {
			return id, fmt.Errorf("holder did not become ready after %s; see %s\n  (raise it: pism new --wait 0  or  pism config ready-timeout 0  to wait indefinitely)",
				readyTimeout, session.LogPath(id))
		}
		if elapsed := time.Since(start); elapsed >= nextNotice {
			fmt.Fprintf(os.Stderr, "pism: waiting for session %s to come up... (%s)\n", id[:8], elapsed.Round(time.Second))
			nextNotice += 5 * time.Second
		}
		time.Sleep(150 * time.Millisecond)
	}
}
