package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func TestVarpUnknownReadsZero(t *testing.T) {
	s := New()
	if got := s.Varp(29); got != 0 {
		t.Fatalf("Varp on a never-received id should read 0, got %d", got)
	}
	if v, ok := s.VarpKnown(29); ok || v != 0 {
		t.Fatalf("VarpKnown on a never-received id should be (0, false), got (%d, %v)", v, ok)
	}
}

func TestVarpSmall(t *testing.T) {
	s := New()
	w := packet.NewWriter(3)
	w.P2(101) // qp
	var negFive int8 = -5
	w.P1(byte(negFive))
	s.Handle(serverprot.VARP_SMALL, w.Data[:w.Pos])

	v, ok := s.VarpKnown(101)
	if !ok || v != -5 {
		t.Fatalf("VarpKnown(101) = (%d, %v), want (-5, true)", v, ok)
	}
	if got := s.Varp(101); got != -5 {
		t.Fatalf("Varp(101) = %d, want -5", got)
	}
}

func TestVarpLarge(t *testing.T) {
	s := New()
	w := packet.NewWriter(6)
	w.P2(101)
	w.P4(70000) // outside VARP_SMALL's -128..127 range
	s.Handle(serverprot.VARP_LARGE, w.Data[:w.Pos])

	if got := s.Varp(101); got != 70000 {
		t.Fatalf("Varp(101) = %d, want 70000", got)
	}
}

func TestVarpLargeNegative(t *testing.T) {
	s := New()
	w := packet.NewWriter(6)
	w.P2(5)
	w.P4(-1000)
	s.Handle(serverprot.VARP_LARGE, w.Data[:w.Pos])

	if got := s.Varp(5); got != -1000 {
		t.Fatalf("Varp(5) = %d, want -1000", got)
	}
}

func TestVarpSmallOverwritesPreviousValue(t *testing.T) {
	s := New()
	send := func(id int, value int8) {
		w := packet.NewWriter(3)
		w.P2(uint16(id))
		w.P1(byte(value))
		s.Handle(serverprot.VARP_SMALL, w.Data[:w.Pos])
	}
	send(29, 1)
	if got := s.Varp(29); got != 1 {
		t.Fatalf("Varp(29) = %d, want 1", got)
	}
	send(29, 2)
	if got := s.Varp(29); got != 2 {
		t.Fatalf("Varp(29) = %d, want 2 after a second VARP_SMALL", got)
	}
}

func TestVarpSyncIsANoOp(t *testing.T) {
	// VARP_SYNC carries no payload and, per this file's own doc comment, this client
	// never runs interface scripts that could locally diverge var[] from varServ[] —
	// so it must not panic on an empty payload and must not touch any tracked varp.
	s := New()
	w := packet.NewWriter(3)
	w.P2(29)
	w.P1(1)
	s.Handle(serverprot.VARP_SMALL, w.Data[:w.Pos])

	s.Handle(serverprot.VARP_SYNC, nil)

	if got := s.Varp(29); got != 1 {
		t.Fatalf("VARP_SYNC changed a tracked varp: Varp(29) = %d, want 1", got)
	}
}

func TestIfSetColourDecode(t *testing.T) {
	s := New()
	w := packet.NewWriter(4)
	w.P2(7333) // cook's assistant's questlist component id
	// 5 bits per channel: r=31 (0x1f), g=0, b=0 -> content's red_rgb (0xFF0000) quantized.
	packed := uint16(0x1f<<10 | 0<<5 | 0)
	w.P2(packed)
	s.Handle(serverprot.IF_SETCOLOUR, w.Data[:w.Pos])

	c, ok := s.Colour(7333)
	if !ok {
		t.Fatalf("expected a colour to be recorded for component 7333")
	}
	if c.R != 0x1f || c.G != 0 || c.B != 0 {
		t.Fatalf("unexpected decoded colour: %+v", c)
	}

	if _, ok := s.Colour(1); ok {
		t.Fatalf("expected no colour recorded for an untouched component")
	}
}

func TestIfSetColourGreen(t *testing.T) {
	s := New()
	w := packet.NewWriter(4)
	w.P2(7333)
	packed := uint16(0<<10 | 0x1f<<5 | 0) // content's green_rgb (0x00FF00) quantized
	w.P2(packed)
	s.Handle(serverprot.IF_SETCOLOUR, w.Data[:w.Pos])

	c, _ := s.Colour(7333)
	if c.R != 0 || c.G != 0x1f || c.B != 0 {
		t.Fatalf("unexpected decoded colour: %+v", c)
	}
}
