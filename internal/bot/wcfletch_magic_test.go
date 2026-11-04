package bot

import (
	"fmt"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutplan"
)

// THE MAGIC BOARD, as the bot uses it: a world's four magic accounts seated one after another from the Seers
// bank go two to the ladder's three trees and two to the Sorcerers' Tower garden, and each new seat says the
// camp, its trees, its bank and walk, how many are seated of its capacity, and the world.
func TestSeatMagicSpreadsAWorldsFourAndSaysTheCamp(t *testing.T) {
	const world = 97
	tag := yewTestTag()
	board := wcfletchplan.SharedMagicBoardFor(world)
	var rigs []*yewRig
	for i := 0; i < 4; i++ {
		r := newYewRig(world, fmt.Sprintf("Mag%d_%s", i, tag), woodcutplan.Seers)
		c := r.b.seatMagic(r.ctx, r.d)
		if !r.b.magic.have || r.b.yew.have || c.Rung != wcfletchplan.MagicRung {
			t.Fatalf("account %d: camp %+v, magic seat %+v", i, c, r.b.magic)
		}
		rigs = append(rigs, r)
	}
	defer func() {
		for _, r := range rigs {
			r.b.leaveMagic(r.ctx)
		}
	}()
	if occ := board.Occupancy(time.Now()); len(occ) != 2 || occ[0] != 2 || occ[1] != 2 {
		t.Fatalf("a world's four seated %v, want two at each camp", occ)
	}
	want := []string{
		fmt.Sprintf("wcfletch: magic camp Seers magic trees (3 trees, bank Seers 74 steps): seated 1 of capacity 3 on world %d", world),
		fmt.Sprintf("wcfletch: magic camp Sorcerers' Tower garden (4 trees, bank Seers 114 steps): seated 1 of capacity 5 on world %d", world),
		fmt.Sprintf("wcfletch: magic camp Seers magic trees (3 trees, bank Seers 74 steps): seated 2 of capacity 3 on world %d", world),
		fmt.Sprintf("wcfletch: magic camp Sorcerers' Tower garden (4 trees, bank Seers 114 steps): seated 2 of capacity 5 on world %d", world),
	}
	for i, w := range want {
		if !rigs[i].said(w) {
			t.Errorf("account %d should say %q: %v", i, w, *rigs[i].lines)
		}
	}
	// A seat is said once: the next pass keeps it quietly.
	n := len(*rigs[0].lines)
	rigs[0].b.seatMagic(rigs[0].ctx, rigs[0].d)
	if len(*rigs[0].lines) != n {
		t.Errorf("a kept seat was said again: %v", (*rigs[0].lines)[n:])
	}
	// The stop line names the camp, and the idle report's label is the camp's.
	if note := rigs[1].b.stopNote(rigs[1].ctx, magicSide); note != ", magic rung at Sorcerers' Tower garden" {
		t.Errorf("stop note %q", note)
	}
	if got := rigs[1].b.campLabel(rigs[1].ctx, wcfletchplan.Ladder[wcfletchplan.MagicRung].Tier); got != "Sorcerers' Tower garden" {
		t.Errorf("campLabel %q", got)
	}
	// Leaving gives the seat back.
	rigs[3].b.leaveMagic(rigs[3].ctx)
	if occ := board.Occupancy(time.Now()); occ[1] != 1 || rigs[3].b.magic.have {
		t.Errorf("after leaving: %v, %+v", occ, rigs[3].b.magic)
	}
}

// The bank a pass carries to is the seated camp's: both magic camps bank at Seers, so the bank steps open the
// Seers bank exactly as before, and a rung with no board does too.
func TestTheBankIsTheSeatedCamps(t *testing.T) {
	b := &wcfletchBot{}
	if got := b.campBank(); got != wcfletchplan.SeersBank {
		t.Errorf("no seat: %+v, want the Seers bank", got)
	}
	for _, c := range wcfletchplan.MagicCamps {
		b.bank = c.BankOf()
		if got := b.campBank(); got.Name != "Seers" || got.Stand != woodcutplan.Seers {
			t.Errorf("%s: bank %+v", c.Name, got)
		}
	}
}

// The magic setting is read, default on, and switches off.
func TestMagicSettings(t *testing.T) {
	if def := wcfletchSettings(map[string]interface{}{}).Normalised(); !def.SpreadMagic {
		t.Errorf("defaults: spreadMagic %v", def.SpreadMagic)
	}
	if got := wcfletchSettings(map[string]interface{}{"spreadMagic": false}).Normalised(); got.SpreadMagic {
		t.Errorf("settings not read: %+v", got)
	}
}
