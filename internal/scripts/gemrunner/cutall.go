package gemrunner

import (
	"fmt"

	"rsb0t-go/internal/scripts/gemplan"
)

// CutAllResult is one CutAll's outcome.
type CutAllResult struct {
	Gem gemplan.Gem
	// Cuts and Crushed are real pack deltas, XPGained the live Crafting XP delta in displayed
	// points, Bursts how many bursts went out.
	Cuts, Crushed, XPGained, Bursts int
	// RefusedLevel is the Crafting level a refusal line demanded, set only for "refused".
	RefusedLevel int
	// Left is the uncut gems of this kind the pack still held at the end; Ticks how many server
	// ticks the cut spanned.
	Left, Ticks int
	// Reason is "done" (every uncut gem of this kind in the pack is cut), "no-chisel", "refused",
	// "members", "stalled" (the uses changed nothing), "partial" (the engine dropped some uses:
	// Left are still held) or "stopped".
	Reason string
}

// CutAll cuts every uncut gem of gem in the pack: UseAll's pipeline (pipeline.go), five different
// gem slots used on the chisel a game tick (actions.Equipment.UseOnHeldManyExcept), every burst
// going out on the next server tick without waiting for the last to show, and one confirm at the
// end. Cutting has no delay in content (skill_crafting/scripts/gem/uncut_gem.rs2: inv_del, inv_add,
// stat_advance, no p_delay), so a pack of 27 is six bursts on six consecutive ticks, 3.6 seconds,
// where waiting for each burst to show took about twice that.
//
// A level or members refusal ends it at once, as Cut's does: the caller denies the gem. A run whose
// uses changed nothing ends "stalled"; one where the engine dropped some ends "partial" with those
// gems still held (the next bank visit deposits them and draws them again).
func CutAll(d *Deps, gem gemplan.Gem) CutAllResult {
	return CutAllThen(d, gem, nil)
}

// CutAllThen is CutAll that runs afterLast the moment the last burst has gone out: the caller's
// next bank open, which then reaches the engine in the same tick as the last cuts (see
// UseOptions.AfterLast).
func CutAllThen(d *Deps, gem gemplan.Gem, afterLast func()) (res CutAllResult) {
	res.Gem = gem
	cut0, crushed0, xp0 := CountByID(d, gem.CutID), CountByID(d, gemplan.CrushedGemstoneID), CraftingXP(d)
	defer func() {
		res.Cuts = max(0, CountByID(d, gem.CutID)-cut0)
		res.Crushed = max(0, CountByID(d, gemplan.CrushedGemstoneID)-crushed0)
		res.XPGained = CraftingXP(d) - xp0
	}()
	if CountByID(d, gem.UncutID) == 0 {
		res.Reason = "done"
		return res
	}
	if !HasChisel(d) {
		res.Reason = "no-chisel"
		return res
	}
	u := UseAll(d, UseOptions{
		Tool: gemplan.ChiselID, Target: gem.UncutID, AfterLast: afterLast,
		Refused: GemRefusal,
	})
	res.Bursts, res.Ticks, res.Left, res.RefusedLevel, res.Reason = u.Bursts, u.Ticks, u.Left, u.RefusedLevel, u.Reason
	if res.Reason == "no-tool" {
		res.Reason = "no-chisel"
	}
	return res
}

// GemRefusal reads a cut's chat for content's two refusals: the Crafting level objbox
// ("refused", with the level) and the members-world line ("members").
func GemRefusal(said string) (string, int) {
	if level, refused := gemplan.LevelRefusal(said); refused {
		return "refused", level
	}
	if gemplan.IsMembersRefusal(said) {
		return "members", 0
	}
	return "", 0
}

// Describe is a one-line account of a CutAll for a log.
func (r CutAllResult) Describe() string {
	s := fmt.Sprintf("%s: %d cut", r.Gem.Name, r.Cuts)
	if r.Crushed > 0 {
		s += fmt.Sprintf(", %d smashed", r.Crushed)
	}
	return fmt.Sprintf("%s in %d burst(s) over %d tick(s), +%d Crafting XP (%s)", s, r.Bursts, r.Ticks, r.XPGained, r.Reason)
}
