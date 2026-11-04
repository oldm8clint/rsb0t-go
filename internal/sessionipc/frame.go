// Package sessionipc is scaffolding for the split described in HOTRELOAD.md: a small,
// dependency-free, length-prefixed RPC/streaming protocol that a future session-holder
// process (owns the live client.Session, the TCP/WebSocket socket, ISAAC ciphers — see
// internal/client.Session) and a future bot-logic ("brain") process (owns internal/bot's
// Runner/Bot loop) can speak across a local connection, so restarting the brain to pick
// up a code fix never has to touch a live account's own connection.
//
// Nothing in this package is imported by cmd/controlpanel or internal/fleet yet — see
// HOTRELOAD.md's phased rollout. It stands on its own, proven by this package's own
// tests (and cmd/hotreloaddemo, a runnable but throwaway demonstration), as the first
// concrete slice: the wire format (this file), a packet-stream state mirror
// (packetstream.go), and a named-action call/dispatch round trip (action.go, conn.go).
// Together these two primitives are enough to reconstruct actions.Session's own
// Send/State/Cheats contract across a process boundary without changing a single line
// of internal/gamestate or internal/actions — see HOTRELOAD.md's "the narrow seam"
// section for why.
package sessionipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// maxFrameSize bounds a single frame's decoded length, so a corrupt (or, if this ever
// crosses a real trust boundary, malicious) length prefix can't make ReadFrame allocate
// an unbounded buffer before it even sees the JSON.
const maxFrameSize = 16 << 20 // 16 MiB - generous for a packet payload or an action call

// WriteFrame JSON-encodes v and writes it as one length-prefixed frame: a 4-byte
// big-endian length, then that many bytes of JSON. Frames are the unit both PacketFrame
// streaming and ActionCall/ActionResult RPC use, over any io.Writer — a net.Conn in
// practice (see conn.go), a bytes.Buffer in tests.
func WriteFrame(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sessionipc: encode frame: %w", err)
	}
	if len(data) > maxFrameSize {
		return fmt.Errorf("sessionipc: frame too large: %d bytes", len(data))
	}
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("sessionipc: write frame length: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("sessionipc: write frame body: %w", err)
	}
	return nil
}

// ReadFrame reads one WriteFrame-encoded frame from r and JSON-decodes it into v. A
// clean close between frames surfaces as io.EOF (from the length prefix read) exactly
// like a bare io.Reader would; a close mid-frame surfaces as io.ErrUnexpectedEOF via the
// wrapped error from the body read.
func ReadFrame(r io.Reader, v any) error {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if n > maxFrameSize {
		return fmt.Errorf("sessionipc: frame too large: %d bytes", n)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return fmt.Errorf("sessionipc: read frame body: %w", err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("sessionipc: decode frame: %w", err)
	}
	return nil
}
