package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Meta is the on-disk descriptor for one live (or recently-live) session.
type Meta struct {
	ID       string    `json:"id"`        // == pi --session-id (uuid v4)
	PID      int       `json:"pid"`       // holder process id
	Cmd      string    `json:"cmd"`       // e.g. "pi"
	Args     []string  `json:"args"`      // extra args passed to pi
	Cwd      string    `json:"cwd"`       // working dir pi runs in
	Endpoint string    `json:"endpoint"`  // unix socket path or \\.\pipe\ name
	Token    string    `json:"token"`     // attach auth token (hex)
	Created  time.Time `json:"created"`
}

func metaPath(id string) string { return filepath.Join(SessionsDir(), id+".json") }

// LogPath is where the detached holder's stdout/stderr are captured.
func LogPath(id string) string { return filepath.Join(SessionsDir(), id+".log") }

// Save writes metadata atomically with 0600 perms.
func (m *Meta) Save() error {
	if _, err := EnsureSessionsDir(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	p := metaPath(m.ID)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Load reads metadata for a full or partial id.
func Load(idOrPrefix string) (*Meta, error) {
	// exact first
	if b, err := os.ReadFile(metaPath(idOrPrefix)); err == nil {
		var m Meta
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		return &m, nil
	}
	// prefix match
	all, err := List()
	if err != nil {
		return nil, err
	}
	var hits []*Meta
	for _, m := range all {
		if len(idOrPrefix) > 0 && len(m.ID) >= len(idOrPrefix) && m.ID[:len(idOrPrefix)] == idOrPrefix {
			hits = append(hits, m)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("no session matching %q", idOrPrefix)
	case 1:
		return hits[0], nil
	default:
		return nil, fmt.Errorf("ambiguous id %q matches %d sessions", idOrPrefix, len(hits))
	}
}

// List returns all known session metadata, newest first.
func List() ([]*Meta, error) {
	dir := SessionsDir()
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Meta
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m Meta
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		out = append(out, &m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// Remove deletes metadata (and unix socket, if any) for a session.
func (m *Meta) Remove() {
	os.Remove(metaPath(m.ID))
	// socket cleanup is best-effort; done by transport on the holder side too
	os.Remove(m.Endpoint)
}

// NewID returns a random uuid-v4 string suitable for pi --session-id.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewToken returns a random hex auth token.
func NewToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
