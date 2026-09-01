package session

import (
	"os"
	"path/filepath"
)

// StateDir is where pism keeps per-session metadata, sockets and logs.
//   - Unix/macOS: $XDG_STATE_HOME/pism  (fallback ~/.local/state/pism)
//   - Windows:    %LOCALAPPDATA%\pism
// Override with $PISM_STATE_DIR.
func StateDir() string {
	if d := os.Getenv("PISM_STATE_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "pism")
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		return filepath.Join(la, "pism")
	}
	return filepath.Join(home, ".local", "state", "pism")
}

// SessionsDir holds one <id>.json (+ <id>.sock on unix, <id>.log) per session.
func SessionsDir() string {
	return filepath.Join(StateDir(), "sessions")
}

// EnsureSessionsDir creates the sessions dir with tight perms.
func EnsureSessionsDir() (string, error) {
	d := SessionsDir()
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// PiSessionsDir is where pi stores its JSONL transcripts, used to read topics.
// Default: <home>/.pi/agent/sessions. Override with $PISM_PI_SESSIONS_DIR.
func PiSessionsDir() string {
	if d := os.Getenv("PISM_PI_SESSIONS_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent", "sessions")
}
