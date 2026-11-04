package retaliateplan

import "fmt"

// Why is the reason the loop stops running and hands control back to the runner.
type Why string

// The reasons Supervise gives.
const (
	WhyNone      Why = ""
	WhyDead      Why = "dead"
	WhyHP        Why = "hp"
	WhyRatsHurt  Why = "rats-hurt"
	WhyEvent     Why = "event"
	WhyGear      Why = "gear"
	WhyFull      Why = "full"
	WhyRephase   Why = "rephase"
	WhyNoTicks   Why = "no-ticks"
	WhyStopped   Why = "stopped"
	WhyNoTree    Why = "no-tree"
	WhyDialogBad Why = "dialog"
)

// Check is what the runner read this tick.
type Check struct {
	Dead bool
	// HP, MaxHP and StopPct feed HPAbort.
	HP, MaxHP, StopPct int
	// RatsHurt is non-empty when a rat shows damage (RatsHurt's text).
	RatsHurt string
	// EventBusy is a random event on the account.
	EventBusy bool
	// WeaponOK is a shortbow worn; QuiverEmpty no ammo worn.
	WeaponOK, QuiverEmpty bool
	// PackFull is a full pack.
	PackFull bool
}

// Supervise says whether the loop must stop this tick, and the text for the log. The order is the order of danger: a death, a
// loss of hitpoints (the rats hit for 0, so it is another monster), a hurt rat (the account would be fighting one to the death:
// a rat has 2 hitpoints), a random event, a gear change (a missing bow would have the account punch the rat; a quiver would
// fire arrows), and a full pack last.
func Supervise(c Check) (Why, string) {
	switch {
	case c.Dead:
		return WhyDead, "the account died"
	case HPAbort(c.HP, c.MaxHP, c.StopPct):
		return WhyHP, fmt.Sprintf("hitpoints fell to %d of %d (below %d%%): the rats hit for 0, so something else is hitting the account", c.HP, c.MaxHP, c.StopPct)
	case c.RatsHurt != "":
		return WhyRatsHurt, c.RatsHurt + ": auto-retaliate is being turned off, a rat is never to be fought"
	case c.EventBusy:
		return WhyEvent, "a random event is on the account"
	case !c.WeaponOK:
		return WhyGear, "no shortbow is worn: with auto-retaliate on the account would fight the rat bare-handed"
	case !c.QuiverEmpty:
		return WhyGear, "ammo is worn: a slipped click would fire arrows at a rat"
	case c.PackFull:
		return WhyFull, "the pack is full"
	}
	return WhyNone, ""
}
