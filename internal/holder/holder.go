// Package holder implements the detached per-session process that owns a PTY
// running pi and keeps it alive across client (dis)connections.
package holder

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"

	"github.com/vandlol/pism/internal/dbg"
	"github.com/vandlol/pism/internal/proto"
	"github.com/vandlol/pism/internal/session"
	"github.com/vandlol/pism/internal/transport"
)

const ringMax = 256 * 1024 // replay buffer sent to newly-attached clients

// Config for a holder run (parsed from the hidden __holder subcommand).
type Config struct {
	ID        string
	Name      string
	Cwd       string
	PiCmd     string
	ExtraArgs []string
}

type client struct{ w *proto.ConnWriter }

type Holder struct {
	cfg  Config
	meta *session.Meta
	pty  pty.Pty
	cmd  *pty.Cmd

	mu      sync.Mutex
	clients map[*client]struct{}
	ring    []byte
	lastW   int // last known cols
	lastH   int // last known rows
}

// Run is the holder main loop. It blocks until pi exits, then cleans up.
func Run(cfg Config) error {
	if _, err := session.EnsureSessionsDir(); err != nil {
		return err
	}
	h := &Holder{cfg: cfg, clients: map[*client]struct{}{}, lastW: 80, lastH: 24}

	p, err := pty.New()
	if err != nil {
		return err
	}
	h.pty = p
	defer p.Close()

	args := append([]string{"--session-id", cfg.ID}, cfg.ExtraArgs...)
	// On Windows, pi is often a .cmd/.ps1 shim that ConPTY can't exec directly;
	// resolvePiCommand wraps such shims in their interpreter (no-op on Unix).
	piName, piArgs := resolvePiCommand(cfg.PiCmd, args)
	// NOTE (keyboard-protocol negotiation): pi is spawned here against a
	// *headless* pty — no real terminal is behind it yet, and typically no
	// client is even attached. At startup pi probes for the Kitty keyboard
	// protocol (ESC[?u) and Primary DA (ESC[c). Nothing on this pty answers the
	// Kitty query (the holder is not a terminal emulator and must stay a
	// transparent passthrough, so it deliberately does NOT synthesize a reply),
	// so pi times out and takes the xterm modifyOtherKeys FALLBACK
	// (ESC[>4;2m). That enable then rides through to whichever real terminal
	// later attaches, and a pism detach leaves pi running so pi never emits the
	// matching disable. The client is therefore responsible for resetting BOTH
	// modifyOtherKeys and the Kitty stack on every teardown path — see
	// internal/client.inputProtoReset. Making pi negotiate Kitty here would
	// require lying about terminal capabilities before we know the client's
	// terminal, which would corrupt non-Kitty terminals; the robust fix lives
	// at teardown instead.
	c := p.Command(piName, piArgs...)
	c.Dir = cfg.Cwd
	c.Env = append(os.Environ(), "PISM_SESSION="+cfg.ID)
	dbg.Logf(1, "holder %s: exec %q args=%v (cwd=%s)", cfg.ID[:8], piName, piArgs, cfg.Cwd)
	if err := c.Start(); err != nil {
		dbg.Logf(1, "holder %s: pi start FAILED: %v", cfg.ID[:8], err)
		return err
	}
	h.cmd = c
	if c.Process != nil {
		dbg.Logf(1, "holder %s: pi started (pid=%d)", cfg.ID[:8], c.Process.Pid)
	}
	_ = p.Resize(h.lastW, h.lastH)

	endpoint := transport.Endpoint(cfg.ID)
	l, err := transport.Listen(endpoint)
	if err != nil {
		_ = c.Process.Kill()
		return err
	}

	h.meta = &session.Meta{
		ID:       cfg.ID,
		Name:     cfg.Name,
		PID:      os.Getpid(),
		Cmd:      cfg.PiCmd,
		Args:     cfg.ExtraArgs,
		Cwd:      cfg.Cwd,
		Endpoint: endpoint,
		Token:    session.NewToken(),
		Created:  time.Now(),
	}
	if err := h.meta.Save(); err != nil {
		_ = c.Process.Kill()
		l.Close()
		return err
	}

	go h.acceptLoop(l)
	go h.readLoop()

	dbg.Logf(2, "holder %s: listening on %s; waiting for pi to exit", cfg.ID[:8], endpoint)
	code := 0
	if werr := c.Wait(); werr != nil {
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	dbg.Logf(1, "holder %s: pi exited (code=%d)", cfg.ID[:8], code)

	h.broadcast(proto.TExit, proto.EncodeExit(code))
	time.Sleep(120 * time.Millisecond) // let clients flush the exit frame
	l.Close()
	transport.Cleanup(endpoint)
	h.meta.Remove()
	return nil
}

// readLoop pumps pty output to the ring buffer and all attached clients.
func (h *Holder) readLoop() {
	buf := make([]byte, 32*1024)
	total := 0
	first := true
	for {
		n, err := h.pty.Read(buf)
		if n > 0 {
			if first {
				dbg.Logf(2, "holder: first pty output (%d bytes)", n)
				first = false
			}
			total += n
			dbg.Logf(3, "pty read %d bytes (total %d)", n, total)
			h.appendRing(buf[:n])
			h.broadcast(proto.TOutput, buf[:n])
		}
		if err != nil {
			dbg.Logf(2, "holder: pty read ended after %d bytes: %v", total, err)
			return
		}
	}
}

func (h *Holder) appendRing(b []byte) {
	h.mu.Lock()
	h.ring = append(h.ring, b...)
	if len(h.ring) > ringMax {
		h.ring = h.ring[len(h.ring)-ringMax:]
	}
	h.mu.Unlock()
}

func (h *Holder) broadcast(t byte, payload []byte) {
	h.mu.Lock()
	for c := range h.clients {
		_ = c.w.Write(t, payload)
	}
	h.mu.Unlock()
}

func (h *Holder) acceptLoop(l net.Listener) {
	for {
		nc, err := l.Accept()
		if err != nil {
			return
		}
		go h.handle(nc)
	}
}

func (h *Holder) handle(nc net.Conn) {
	defer nc.Close()

	t, payload, err := proto.ReadFrame(nc)
	if err != nil {
		return
	}
	if t != proto.THello || string(payload) != h.meta.Token {
		_ = proto.WriteFrame(nc, proto.TError, []byte("unauthorized"))
		return
	}
	cw := proto.NewConnWriter(nc)
	if err := cw.Write(proto.THelloOK, nil); err != nil {
		return
	}

	cl := &client{w: cw}
	h.mu.Lock()
	snap := make([]byte, len(h.ring))
	copy(snap, h.ring)
	h.clients[cl] = struct{}{}
	h.mu.Unlock()

	if len(snap) > 0 {
		_ = cw.Write(proto.TOutput, snap)
	}

	for {
		mt, mp, err := proto.ReadFrame(nc)
		if err != nil {
			break
		}
		switch mt {
		case proto.TInput:
			_, _ = h.pty.Write(mp)
		case proto.TResize:
			if cols, rows, ok := proto.DecodeResize(mp); ok && cols > 0 && rows > 0 {
				h.mu.Lock()
				h.lastW, h.lastH = cols, rows
				h.mu.Unlock()
				_ = h.pty.Resize(cols, rows)
			}
		case proto.TKill:
			if h.cmd != nil && h.cmd.Process != nil {
				_ = h.cmd.Process.Kill()
			}
		}
	}

	h.mu.Lock()
	delete(h.clients, cl)
	h.mu.Unlock()
}
