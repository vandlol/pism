// Package manager provides higher-level session operations used by the CLI:
// liveness probing, topic-annotated listing, kill and garbage collection.
package manager

import (
	"fmt"
	"os"
	"time"

	"github.com/vandlol/pism/internal/proto"
	"github.com/vandlol/pism/internal/session"
	"github.com/vandlol/pism/internal/transport"
)

// Row is a session annotated for display.
type Row struct {
	Meta  *session.Meta
	Topic string
	Alive bool
	Age   time.Duration
}

// Alive reports whether a holder is currently accepting connections.
func Alive(m *session.Meta) bool {
	c, err := transport.Dial(m.Endpoint)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// Rows lists all sessions with topic + liveness, newest first.
func Rows(topicLen int) ([]Row, error) {
	metas, err := session.List()
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(metas))
	for _, m := range metas {
		rows = append(rows, Row{
			Meta:  m,
			Topic: session.Topic(m.ID, m.Cwd, topicLen),
			Alive: Alive(m),
			Age:   time.Since(m.Created),
		})
	}
	return rows, nil
}

// NewestLive returns the most recently created session that is still live.
func NewestLive() (*session.Meta, error) {
	metas, err := session.List() // already sorted newest-first
	if err != nil {
		return nil, err
	}
	for _, m := range metas {
		if Alive(m) {
			return m, nil
		}
	}
	return nil, fmt.Errorf("no live sessions to attach to (start one with: pism new)")
}

// AdjacentLive returns the next (dir=+1) or previous (dir=-1) live session
// relative to currentID, ordered newest-first with wraparound. Dead sessions
// are skipped. If currentID is the only live session (or not found), it is
// returned unchanged. Returns an error only when no live session exists.
func AdjacentLive(currentID string, dir int) (*session.Meta, error) {
	metas, err := session.List() // newest-first
	if err != nil {
		return nil, err
	}
	live := make([]*session.Meta, 0, len(metas))
	for _, m := range metas {
		if Alive(m) {
			live = append(live, m)
		}
	}
	if len(live) == 0 {
		return nil, fmt.Errorf("no live sessions")
	}
	cur := -1
	for i, m := range live {
		if m.ID == currentID {
			cur = i
			break
		}
	}
	if cur == -1 {
		// Current isn't live/known; land on the newest.
		return live[0], nil
	}
	step := 1
	if dir < 0 {
		step = -1
	}
	n := len(live)
	next := ((cur+step)%n + n) % n
	return live[next], nil
}

// Kill asks a holder to terminate pi (graceful), falling back to signalling
// the holder PID, and cleans up metadata.
func Kill(idOrPrefix string) error {
	m, err := session.Load(idOrPrefix)
	if err != nil {
		return err
	}
	if c, derr := transport.Dial(m.Endpoint); derr == nil {
		_ = proto.WriteFrame(c, proto.THello, []byte(m.Token))
		// swallow hello reply
		_, _, _ = proto.ReadFrame(c)
		_ = proto.WriteFrame(c, proto.TKill, nil)
		_ = c.Close()
		// give the holder a moment to tear down
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if !Alive(m) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	// Best-effort process kill + metadata cleanup for stragglers.
	if m.PID > 0 {
		if p, perr := os.FindProcess(m.PID); perr == nil {
			_ = p.Kill()
		}
	}
	m.Remove()
	return nil
}

// GC removes metadata (and sockets) for holders that are no longer alive,
// and sweeps orphaned socket files left behind by holders that died without
// a clean teardown. The socket dir is shared per-uid across every state dir,
// so a dead socket whose endpoint no longer accepts connections is removed
// regardless of which PISM_STATE_DIR spawned it.
func GC() (int, error) {
	metas, err := session.List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range metas {
		if !Alive(m) {
			m.Remove()
			_ = os.Remove(session.LogPath(m.ID))
			n++
		}
	}
	// Sweep dead sockets: any endpoint we can't dial has no live holder.
	for _, ep := range transport.ListEndpoints() {
		c, derr := transport.Dial(ep)
		if derr == nil {
			_ = c.Close()
			continue
		}
		if os.Remove(ep) == nil {
			n++
		}
	}
	return n, nil
}
