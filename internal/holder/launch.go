package holder

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/vandlol/pism/internal/dbg"
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
	// Pre-flight: make sure pi is actually installed here, so a missing pi gives
	// a clear message instead of an opaque "holder exited (exit status 1)".
	if path, err := exec.LookPath(piCmd); err != nil {
		return "", fmt.Errorf("pi command %q not found on PATH \u2014 install pi (https://pi.dev), "+
			"or point pism at it with --pi <path> or `pism config pi <path>`", piCmd)
	} else {
		dbg.Logf(2, "resolved pi: %s", path)
	}
	if _, err := session.EnsureSessionsDir(); err != nil {
		return "", err
	}
	id := session.NewID()
	name := session.GenerateName(session.TakenNames())

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}

	argv := []string{"__holder", "--id", id, "--name", name, "--cwd", cwd, "--pi", piCmd}
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
	// Propagate verbosity to the detached holder; its stderr is this log file.
	cmd.Env = append(os.Environ(), "PISM_VERBOSITY="+strconv.Itoa(dbg.Level()))
	cmd.SysProcAttr = detachAttr()
	dbg.Logf(1, "launching holder %s (cwd=%s pi=%s)", id[:8], cwd, piCmd)
	dbg.Logf(2, "holder argv: %v", argv)
	dbg.Logf(2, "holder log: %s", session.LogPath(id))
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
				dbg.Logf(1, "holder %s ready after %s", id[:8], time.Since(start).Round(time.Millisecond))
				return id, nil
			}
		}
		select {
		case werr := <-exited:
			dbg.Logf(1, "holder %s exited before ready: %v", id[:8], werr)
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
