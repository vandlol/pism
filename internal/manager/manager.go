// Package manager provides higher-level session operations used by the CLI:
// liveness probing, topic-annotated listing, kill and garbage collection.
package manager

import (
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

// GC removes metadata (and sockets) for holders that are no longer alive.
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
	return n, nil
}
