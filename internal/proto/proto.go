// Package proto defines the framed wire protocol spoken between a pism
// client (attach) and a session holder over a unix socket / named pipe.
//
// Frame layout: [type:1][length:4 big-endian][payload:length]
package proto

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
)

// Message types. Client -> Holder are < 10, Holder -> Client are >= 10.
const (
	// Client -> Holder
	THello  byte = 1 // payload: auth token bytes
	TInput  byte = 2 // payload: raw stdin bytes
	TResize byte = 3 // payload: [cols:2][rows:2] big-endian
	TKill   byte = 4 // payload: none; ask holder to terminate the child + exit

	// Holder -> Client
	TOutput  byte = 10 // payload: raw pty output bytes
	TExit    byte = 11 // payload: [code:4] big-endian; child exited
	THelloOK byte = 12 // payload: none; handshake accepted
	TError   byte = 13 // payload: utf-8 error message
)

const maxFrame = 8 << 20 // 8 MiB hard cap to avoid runaway allocs

// WriteFrame writes a single framed message.
func WriteFrame(w io.Writer, t byte, payload []byte) error {
	if len(payload) > maxFrame {
		return fmt.Errorf("proto: payload too large (%d)", len(payload))
	}
	var hdr [5]byte
	hdr[0] = t
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

// ReadFrame reads a single framed message.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > maxFrame {
		return 0, nil, fmt.Errorf("proto: frame too large (%d)", n)
	}
	if n == 0 {
		return hdr[0], nil, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return hdr[0], buf, nil
}

// EncodeResize packs a terminal size into a payload.
func EncodeResize(cols, rows int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b[0:], uint16(cols))
	binary.BigEndian.PutUint16(b[2:], uint16(rows))
	return b
}

// DecodeResize unpacks a resize payload.
func DecodeResize(b []byte) (cols, rows int, ok bool) {
	if len(b) < 4 {
		return 0, 0, false
	}
	return int(binary.BigEndian.Uint16(b[0:])), int(binary.BigEndian.Uint16(b[2:])), true
}

// EncodeExit / DecodeExit for exit codes.
func EncodeExit(code int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(code))
	return b
}

func DecodeExit(b []byte) int {
	if len(b) < 4 {
		return 0
	}
	return int(int32(binary.BigEndian.Uint32(b)))
}

// ConnWriter serializes frame writes to a single connection so multiple
// goroutines (broadcast + control replies) can share it safely.
type ConnWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func NewConnWriter(w io.Writer) *ConnWriter { return &ConnWriter{w: w} }

func (c *ConnWriter) Write(t byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return WriteFrame(c.w, t, payload)
}
