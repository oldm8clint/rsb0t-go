package waterfallplan

import (
	"fmt"

	"rsb0t-go/internal/scripts/questplan"
)

// Settings are the quest's knobs. The JSON-facing names live in the callers (the standalone bot
// and ScarabSwarm's Stage 5 hook); DefaultSettings is the owner's defaults.
type Settings struct {
	// Enabled switches the quest on for ScarabSwarm's Stage 5 ("stage5Waterfall").
	Enabled bool
	// MinHitpoints is the Hitpoints LEVEL at which the quest is attempted (the owner: "once we
	// hit 20 hitpoints"; "stage5WaterfallMinHp").
	MinHitpoints int
	// MaxDeaths is how many deaths the quest may cost before it parks ("stage5WaterfallMaxDeaths";
	// 0 is no cap). The owner's ruling: deaths are accepted, but a quest that kills a 20-hitpoint
	// account repeatedly is a design failure, so after this many it waits for the giants training.
	MaxDeaths int
	// ParkStep is how many Hitpoints levels a parked quest waits for per death over the limit
	// ("stage5WaterfallParkStep"): with 20 and 10, one death parks it until Hitpoints 30.
	ParkStep int
	// TombFood and TombFoodMin are the food drawn for, and the least food to start, the Tree
	// Gnome Village and tomb trip; DungeonFood and DungeonFoodMin the same for the dungeon.
	TombFood, TombFoodMin       int
	DungeonFood, DungeonFoodMin int
	// EatAtDungeon and EatAtSurface are the hitpoints at or below which the runner eats. 11 is a
	// fire giant's max hit (content: floor(((strength+9)*(bonus+64)+320)/640) for level 86), so
	// at 12 or more one max hit cannot kill; 7 is a moss giant's max (6) plus one.
	EatAtDungeon, EatAtSurface int
	// LaneWaitSeconds bounds how long the runner waits at the Large door for the fire giants to
	// wander off the lane before running it anyway at full health.
	LaneWaitSeconds int
}

// DefaultSettings are the owner's numbers (2026-09-23) and the measured food sizing: at the
// chalice the pack is rope, urn, key and food and needs 5 free slots, so 18 sharks plus the
// seven quest items fit the 28 slots on the way in (the pillars and the statue free four).
//
// The tomb trip's food was 10 units (50 hitpoints) with a floor of 4, and acct died in the tomb on
// 5 Oct 2026 after taking about 40 hitpoints in 79 seconds (see TombEntryHealthPct); it is 14 units
// (70) with a floor of 11 (55): the account's own maximum and that food cover such a visit about
// twice over, and it is still under a pack's worth.
func DefaultSettings() Settings {
	return Settings{
		Enabled:         true,
		MinHitpoints:    20,
		MaxDeaths:       1,
		ParkStep:        10,
		TombFood:        14,
		TombFoodMin:     11,
		DungeonFood:     18,
		DungeonFoodMin:  10,
		EatAtDungeon:    11,
		EatAtSurface:    7,
		LaneWaitSeconds: 20,
	}
}

// Normalised clamps nonsense to the defaults' floor.
func (s Settings) Normalised() Settings {
	d := DefaultSettings()
	if s.MinHitpoints < 10 {
		s.MinHitpoints = d.MinHitpoints
	}
	if s.MaxDeaths < 0 {
		s.MaxDeaths = 0
	}
	if s.ParkStep < 0 {
		s.ParkStep = 0
	}
	if s.TombFood < 0 {
		s.TombFood = 0
	}
	if s.TombFoodMin > s.TombFood {
		s.TombFoodMin = s.TombFood
	}
	if s.DungeonFood > 21 {
		// 21 food plus the seven quest items is the whole pack.
		s.DungeonFood = 21
	}
	if s.DungeonFoodMin > s.DungeonFood {
		s.DungeonFoodMin = s.DungeonFood
	}
	if s.EatAtDungeon < 1 {
		s.EatAtDungeon = d.EatAtDungeon
	}
	if s.EatAtSurface < 1 {
		s.EatAtSurface = d.EatAtSurface
	}
	if s.LaneWaitSeconds < 0 {
		s.LaneWaitSeconds = 0
	}
	return s
}

// EatAt is the hitpoints at or below which to eat in an area, capped below the account's own
// maximum so a small account still eats rather than never.
func (s Settings) EatAt(a Area, maxHP int) int {
	at := s.EatAtSurface
	if a.InWaterfallDungeon() || a == AreaLedge {
		at = s.EatAtDungeon
	}
	if maxHP > 1 && at >= maxHP {
		at = maxHP - 1
	}
	return at
}

// RequiredHitpoints is the Hitpoints level the quest waits for after deaths deaths. MaxDeaths 0
// means no cap.
func (s Settings) RequiredHitpoints(deaths int) int {
	if s.MaxDeaths <= 0 || deaths < s.MaxDeaths {
		return s.MinHitpoints
	}
	return s.MinHitpoints + s.ParkStep*(deaths-s.MaxDeaths+1)
}

// Wanted is ScarabSwarm Stage 5's gate: "HP >= 20 and Waterfall not complete: do the quest, then
// the giants". It answers whether the quest should run now, and why not.
//
// An account standing somewhere only the quest goes (the mound, the ledge, a quest dungeon) is
// always wanted, whatever the level or the death count: the Stage 5 training that would run
// instead cannot leave those places, so the quest's own exits must.
func Wanted(s Settings, hitpoints int, status questplan.Status, deaths int, area Area) (bool, string) {
	if status == questplan.StatusComplete {
		if area != AreaMainland && area != AreaUnknown {
			return true, "complete, still leaving " + area.String()
		}
		return false, "the Waterfall Quest is complete"
	}
	if area != AreaMainland && area != AreaUnknown && status == questplan.StatusInProgress {
		return true, "standing in " + area.String()
	}
	if !s.Enabled {
		return false, "the Waterfall Quest is switched off (stage5Waterfall false)"
	}
	if status == questplan.StatusUnknown {
		return false, "the quest tab has not been coloured yet"
	}
	if hitpoints < s.MinHitpoints {
		return false, fmt.Sprintf("Hitpoints %d is below %d", hitpoints, s.MinHitpoints)
	}
	if need := s.RequiredHitpoints(deaths); hitpoints < need {
		return false, fmt.Sprintf("parked after %d death(s) until Hitpoints %d (now %d)", deaths, need, hitpoints)
	}
	return true, ""
}
