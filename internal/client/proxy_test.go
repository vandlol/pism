package client

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/vandlol/pism/internal/proto"
	"github.com/vandlol/pism/internal/session"
)

// TestProxyBridgesHandshakeAndFrames verifies that Proxy performs the token
// handshake against a holder and then copies raw frames in both directions
// between the client stream and the holder socket.
func TestProxyBridgesHandshakeAndFrames(t *testing.T) {
	// macOS caps unix socket paths at ~104 bytes, so keep it short (t.TempDir
	// under /var/folders is too long). Use a brief /tmp name we clean up.
	sock := fmt.Sprintf("/tmp/pism-proxy-test-%d.sock", os.Getpid())
	_ = os.Remove(sock)
	defer os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	const token = "deadbeef"
	gotToken := make(chan string, 1)
	holderDone := make(chan struct{})

	// Fake holder: expect THello(token), reply THelloOK, expect one TInput,
	// echo back one TOutput with the same payload, then close.
	go func() {
		defer close(holderDone)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		ht, hp, err := proto.ReadFrame(c)
		if err != nil || ht != proto.THello {
			gotToken <- "<bad-hello>"
			return
		}
		gotToken <- string(hp)
		_ = proto.WriteFrame(c, proto.THelloOK, nil)
		mt, mp, err := proto.ReadFrame(c)
		if err != nil || mt != proto.TInput {
			return
		}
		_ = proto.WriteFrame(c, proto.TOutput, mp) // echo
	}()

	// Client side of the proxy: feed a single TInput frame in, collect out.
	var inBuf bytes.Buffer
	_ = proto.WriteFrame(&inBuf, proto.TInput, []byte("ping"))
	// Keep the input stream open a touch so Proxy's copy doesn't race the
	// holder's reply; a pipe lets us hold it open then close.
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write(inBuf.Bytes())
		time.Sleep(100 * time.Millisecond)
		_ = pw.Close()
	}()

	var out bytes.Buffer
	m := &session.Meta{Endpoint: sock, Token: token}
	done := make(chan error, 1)
	go func() { done <- Proxy(m, pr, &out) }()

	select {
	case tok := <-gotToken:
		if tok != token {
			t.Fatalf("holder saw token %q, want %q", tok, token)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handshake")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Proxy to finish")
	}

	// out should contain the echoed TOutput frame carrying "ping".
	ot, op, err := proto.ReadFrame(&out)
	if err != nil {
		t.Fatalf("read echoed frame: %v", err)
	}
	if ot != proto.TOutput || string(op) != "ping" {
		t.Fatalf("echoed frame = type %d %q; want TOutput \"ping\"", ot, op)
	}
	<-holderDone
}
