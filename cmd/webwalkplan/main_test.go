package main

import "testing"

// rotateFlagsReference is the engine's own Rotation.ts rotateFlags, transcribed. The audit
// builds its rotation with + where the engine uses |, which is only safe because the two
// halves never share a bit; this pins that rather than trusting it.
func rotateFlagsReference(angle, flags int) int {
	if angle == 0 {
		return flags
	}
	return ((flags << uint(angle)) & 0xf) | (flags >> uint(4-angle))
}

func TestRotateApproachMatchesTheEngine(t *testing.T) {
	for flags := 0; flags < 16; flags++ {
		for angle := 0; angle < 4; angle++ {
			got := rotateApproach(flags, angle)
			want := rotateFlagsReference(angle, flags)
			if got != want {
				t.Errorf("rotateApproach(%04b, angle %d) = %04b, engine rotateFlags gives %04b", flags, angle, got, want)
			}
		}
	}
}

// TestConsultsForceApproachFollowsTheExitStrategy pins the shape gate to
// ReachStrategy.exitStrategy: only the rectangle strategies are handed blockAccessFlags, so
// only those shapes can have a forceapproach the server actually enforces.
func TestConsultsForceApproachFollowsTheExitStrategy(t *testing.T) {
	for shape := 0; shape <= 22; shape++ {
		want := shape == 10 || shape == 11 || shape == 22
		if got := consultsForceApproach(shape); got != want {
			t.Errorf("consultsForceApproach(%d) = %v, want %v", shape, got, want)
		}
	}
	if !consultsForceApproach(-2) {
		t.Error("shape -2 is the exclusive rectangle strategy and does consult forceapproach")
	}
	if consultsForceApproach(-1) {
		t.Error("shape -1 is the no-op strategy and reaches nothing, so it consults nothing")
	}
}

// TestApproachBitNamesTheSideTheStandIsOn keeps the bit mapping honest against
// RectangleBoundary.reachRectangle1: standing west of a loc is gated by BLOCK_WEST (0x8),
// east by BLOCK_EAST (0x2), south by BLOCK_SOUTH (0x4) and north by BLOCK_NORTH (0x1).
func TestApproachBitNamesTheSideTheStandIsOn(t *testing.T) {
	const locX, locZ = 100, 200
	cases := []struct {
		standX, standZ int
		want           int
		name           string
	}{
		{locX - 1, locZ, dirWest, "west"},
		{locX + 1, locZ, dirEast, "east"},
		{locX, locZ - 1, dirSouth, "south"},
		{locX, locZ + 1, dirNorth, "north"},
		{locX - 1, locZ - 1, 0, "diagonal, not an orthogonal side"},
		{locX, locZ, 0, "on the loc itself"},
	}
	for _, c := range cases {
		if got := approachBit(c.standX, c.standZ, locX, locZ); got != c.want {
			t.Errorf("approachBit for the %s stand = %d, want %d", c.name, got, c.want)
		}
	}
	if sideName(dirWest) != "west" || sideName(dirNorth) != "north" {
		t.Error("sideName disagrees with the DirectionFlag bits")
	}
}

// TestLegalSidesListsTheSidesLeftOpen checks the reading that makes forceapproach
// counter-intuitive: a SET bit blocks, so forceapproach=13 (0b1101) leaves only east.
func TestLegalSidesListsTheSidesLeftOpen(t *testing.T) {
	got := legalSides(13)
	if len(got) != 1 || got[0] != "east" {
		t.Errorf("legalSides(13) = %v, want [east]: north, south and west are blocked", got)
	}
	if len(legalSides(0)) != 4 {
		t.Error("forceapproach 0 blocks nothing, so all four sides stay legal")
	}
	if len(legalSides(0xf)) != 0 {
		t.Error("forceapproach 0b1111 blocks every side")
	}
}
