package mastercraftplan

// The Nature campaign (runecraft P4, docs/runecraft_p4_2026-10-14.md). a user request: "trigger when the mule is out of nature
// runes, make 100k runes for the mule, keep some instead of trading all then asking back". P3's gate round runs whenever
// craftRuneManual says so; with craftRuneCampaign the account decides for itself:
//
//	START   the mule's PUBLISHED Nature rune is under craftRuneStartBelow, with Runecrafting 44, a members world, essence to work with
//	        (the mule's, or a whole-stack market buy-out's), a Nature talisman and carriers ready (runecraftplan.DecideCampaignStart)
//	RUN     gate rounds (nature.go, internal/bot/mastercrafter_nature.go) until one ends in a way that finishes it, pauses it or hands
//	        over and goes on (runecraftplan.DecideCampaignEnd)
//	END     craftRuneTarget crafted or the mule at craftRuneStopAbove: DONE, disarmed until the mule's Natures fall under StartBelow
//	        again; the round's time, no carriers, no essence: PAUSED craftRunePauseMinutes, then it carries on
//
// WHERE IT RANKS is the gate round's: after the jewellery round and before the levelling round and the batch rounds, behind chest loot
// owed to the mule (DecideWithChest). A campaign's own next round (after a hand-over trip, a supply run) chains straight on
// (Decision.Continue); a PAUSED one gives the other rounds, and ScarabSwarm, their turn.
//
// The ledger (RuneLedger.Campaign) is its memory across rounds and restarts: crafted so far, the pause, the hysteresis.

import (
	"fmt"
	"strconv"
	"time"

	"rsb0t-go/internal/scripts/runecraftplan"
)

// Setting keys of the campaign.
const (
	KeyRuneCampaign      = "craftRuneCampaign"
	KeyRuneStartBelow    = "craftRuneStartBelow"
	KeyRuneStopAbove     = "craftRuneStopAbove"
	KeyRuneTarget        = "craftRuneTarget"
	KeyRuneMinEssence    = "craftRuneMinEssence"
	KeyRuneMinRunners    = "craftRuneMinRunners"
	KeyRuneHandOverAt    = "craftRuneHandOverAt"
	KeyRuneHandOverMin   = "craftRuneHandOverMin"
	KeyRuneCourierWait   = "craftRuneCourierWaitMinutes"
	KeyRunePause         = "craftRunePauseMinutes"
	KeyRuneBuyAll        = "craftRuneBuyAll"
	KeyRuneMuleCoinFloor = "craftRuneMuleCoinFloor"
	KeyRuneBuyTrip       = "craftRuneBuyTrip"
)

func init() {
	for _, k := range []string{KeyRuneCampaign, KeyRuneStartBelow, KeyRuneStopAbove, KeyRuneTarget, KeyRuneMinEssence, KeyRuneMinRunners,
		KeyRuneHandOverAt, KeyRuneHandOverMin, KeyRuneCourierWait, KeyRunePause, KeyRuneBuyAll, KeyRuneMuleCoinFloor, KeyRuneBuyTrip} {
		knownKeys[k] = true
	}
}

// Bounds of the campaign's settings.
const (
	// MaxRuneCampaignNumber bounds every count setting (ten million is a hundred times the target and no bank holds more).
	MaxRuneCampaignNumber = 10_000_000
	// MaxRuneMinRunners is the most carriers a campaign may ask for: the gate saturates at runecraftplan.MaxCarriers.
	MaxRuneMinRunners = 10
	// MaxRuneMuleCoinFloor bounds the floor the mule keeps.
	MaxRuneMuleCoinFloor = 2_000_000_000
	// MinRuneBuyTrip is the smallest trip a nonzero craftRuneBuyTrip may name (below it the walk to the market costs more than the lot).
	MinRuneBuyTrip = 100_000
)

// validateCampaign reports every problem with the campaign's settings.
func (r RuneSettings) validateCampaign() []string {
	var p []string
	between := func(key string, v, lo, hi int) {
		if v < lo || v > hi {
			p = append(p, key+" "+strconv.Itoa(v)+" is outside "+strconv.Itoa(lo)+".."+strconv.Itoa(hi))
		}
	}
	between(KeyRuneStartBelow, r.StartBelow, 1, MaxRuneCampaignNumber)
	between(KeyRuneStopAbove, r.StopAbove, 2, MaxRuneCampaignNumber)
	if r.StopAbove <= r.StartBelow {
		p = append(p, KeyRuneStopAbove+" "+strconv.Itoa(r.StopAbove)+" must be above "+KeyRuneStartBelow+" "+strconv.Itoa(r.StartBelow)+" (a campaign that ends where it starts never gets going)")
	}
	between(KeyRuneTarget, r.Target, 1000, MaxRuneCampaignNumber)
	between(KeyRuneMinEssence, r.MinEssence, 0, MaxRuneCampaignNumber)
	between(KeyRuneMinRunners, r.MinRunners, 0, MaxRuneMinRunners)
	between(KeyRuneHandOverAt, r.HandOverAt, 1000, MaxRuneCampaignNumber)
	between(KeyRuneHandOverMin, r.HandOverMin, 1, MaxRuneCampaignNumber)
	if r.HandOverMin > r.HandOverAt {
		p = append(p, KeyRuneHandOverMin+" "+strconv.Itoa(r.HandOverMin)+" is above "+KeyRuneHandOverAt+" "+strconv.Itoa(r.HandOverAt)+" (the least surplus worth a trip cannot exceed the surplus that makes one)")
	}
	if r.CourierWait < time.Minute || r.CourierWait > 24*time.Hour {
		p = append(p, KeyRuneCourierWait+" "+r.CourierWait.String()+" is outside 1..1440 minutes")
	}
	if r.Pause < time.Minute || r.Pause > 24*time.Hour {
		p = append(p, KeyRunePause+" "+r.Pause.String()+" is outside 1..1440 minutes")
	}
	between(KeyRuneMuleCoinFloor, r.MuleCoinFloor, 0, MaxRuneMuleCoinFloor)
	if r.BuyTrip != 0 {
		between(KeyRuneBuyTrip, r.BuyTrip, MinRuneBuyTrip, MaxRuneMuleCoinFloor)
	}
	if r.Campaign && r.Manual {
		p = append(p, KeyRuneCampaign+" and "+KeyRuneManual+" are alternatives: the campaign starts and stops the gate by itself, the switch starts it whenever it is possible; set one")
	}
	if r.Campaign && !r.On {
		p = append(p, KeyRuneCampaign+" needs "+KeyRune+" on")
	}
	return p
}

// CampaignLedger is the campaign's memory (RuneLedger.Campaign). The counters are THIS campaign's: Begin zeroes them.
type CampaignLedger struct {
	// Active is set from the round that starts a campaign to the one that finishes it. Disarmed is set when a campaign finished
	// (target, mule full) and cleared when the mule's published Nature rune is seen under craftRuneStartBelow again.
	Active   bool `json:"active,omitempty"`
	Disarmed bool `json:"disarmed,omitempty"`
	// Number is the campaigns started and Done the campaigns finished, for good.
	Number int `json:"number,omitempty"`
	Done   int `json:"done,omitempty"`
	// Started is when this campaign began and MuleAtStart the mule's Nature runes then.
	Started     time.Time `json:"started,omitempty"`
	MuleAtStart int       `json:"muleAtStart,omitempty"`
	// This campaign's counters: the Natures crafted, the essence traded for them, the gate rounds, the main account's own hand-over trips, the
	// Natures he handed the mule himself and the ones a courier carried; the supply runs (market buy-outs), the essence they brought
	// the mule and the coins they spent.
	Crafted        int `json:"crafted,omitempty"`
	EssenceTaken   int `json:"essenceTaken,omitempty"`
	Rounds         int `json:"rounds,omitempty"`
	OwnTrips       int `json:"ownTrips,omitempty"`
	HandedOver     int `json:"handedOver,omitempty"`
	CourierNatures int `json:"courierNatures,omitempty"`
	BuyOuts        int `json:"buyOuts,omitempty"`
	Bought         int `json:"bought,omitempty"`
	BoughtSpent    int `json:"boughtSpent,omitempty"`
	// NoStockUntil holds the market buy-out off until then: the last one reached the stalls and bought under essenceplan.BuyOutMin
	// (essenceplan.BuyOutShort), so asking the mule for its coins again would only repeat it. It outlives Begin: a new campaign
	// starts from zero, the stalls do not.
	NoStockUntil time.Time `json:"noStockUntil,omitempty"`
	// DryRefills is how many gate rounds in a row ended dry and were followed by a refill without a pause; a round that traded, or
	// ended any other way, sets it to 0 (essenceplan.MaxDryRefills is the most).
	DryRefills int `json:"dryRefills,omitempty"`
	// HandOverFails is how many hand-overs of Natures to the mule failed in a row (the mule away, the bank would not open, the
	// trade not made); craftMaxHandoverFailures of them in a row give the campaign up until the script restarts.
	HandOverFails int `json:"handOverFails,omitempty"`
	// PausedUntil holds an Active campaign off until then; LastEnd is how the last round ended (a runecraftplan.EndKind), LastWhy
	// the log's words for it and LastEndAt when.
	PausedUntil time.Time `json:"pausedUntil,omitempty"`
	LastEnd     string    `json:"lastEnd,omitempty"`
	LastWhy     string    `json:"lastWhy,omitempty"`
	LastEndAt   time.Time `json:"lastEndAt,omitempty"`
}

// Begin starts a campaign: the counters are zeroed, the hysteresis cleared. What the stalls had to sell is not forgotten.
func (c *CampaignLedger) Begin(now time.Time, muleNature int) {
	n, done, noStock := c.Number, c.Done, c.NoStockUntil
	*c = CampaignLedger{Active: true, Number: n + 1, Done: done, Started: now, MuleAtStart: muleNature, NoStockUntil: noStock}
}

// CraftedNow is the Natures the campaign under way has crafted: 0 when none is (Crafted is the LAST campaign's total once it ended, and
// a new one starts from zero), so a trigger that sizes the essence a campaign needs reads it and not the field.
func (c *CampaignLedger) CraftedNow() int {
	if c == nil || !c.Active {
		return 0
	}
	return c.Crafted
}

// NoStock reports whether the market buy-out is held off at now, and until when.
func (c *CampaignLedger) NoStock(now time.Time) (until time.Time, held bool) {
	if c == nil {
		return time.Time{}, false
	}
	return c.NoStockUntil, now.Before(c.NoStockUntil)
}

// NoteNoStock holds the market buy-out off for wait from now (a buy-out that reached the stalls and bought too little); NoteStock
// clears it (one that bought what it should).
func (c *CampaignLedger) NoteNoStock(now time.Time, wait time.Duration) {
	c.NoStockUntil = now.Add(wait)
}
func (c *CampaignLedger) NoteStock() { c.NoStockUntil = time.Time{} }

// AllowDryRefill says whether a gate round that ended dry may be followed at once by a refill, and counts it when it may: at most
// max in a row, then the campaign pauses (a round that traded or ended another way starts the count again, ResetDryRefills).
func (c *CampaignLedger) AllowDryRefill(max int) bool {
	if c.DryRefills >= max {
		return false
	}
	c.DryRefills++
	return true
}

// ResetDryRefills starts the count of pause-free dry-end refills again.
func (c *CampaignLedger) ResetDryRefills() { c.DryRefills = 0 }

// NoteCraft records Natures crafted this campaign; NoteTrade essence traded for them; NoteCourier Natures a courier carried;
// NoteHandOver Natures the main account handed the mule himself (own: an own trip, not the end of a round).
func (c *CampaignLedger) NoteCraft(natures int) { c.Crafted += max(0, natures) }
func (c *CampaignLedger) NoteTrade(essence int) { c.EssenceTaken += max(0, essence) }
func (c *CampaignLedger) NoteCourier(n int)     { c.CourierNatures += max(0, n) }
func (c *CampaignLedger) NoteRound()            { c.Rounds++ }
func (c *CampaignLedger) NoteHandOver(n int, own bool) {
	c.HandedOver += max(0, n)
	if own {
		c.OwnTrips++
	}
}

// NoteHandOverResult records whether the last hand-over of Natures to the mule failed: a failure counts in a row, a hand-over that
// worked clears the count.
func (c *CampaignLedger) NoteHandOverResult(failed bool) {
	if failed {
		c.HandOverFails++
		return
	}
	c.HandOverFails = 0
}

// GivenUp reports whether craftMaxHandoverFailures hand-overs in a row have failed: the campaign starts no round until the script
// restarts (the Natures stay in the bank).
func (c *CampaignLedger) GivenUp(set Settings) bool {
	return c != nil && set.MaxHandoverFailures > 0 && c.HandOverFails >= set.MaxHandoverFailures
}

// NoteBuyOut records a supply run: the essence it brought the mule and the coins it spent.
func (c *CampaignLedger) NoteBuyOut(essence, spent int) {
	c.BuyOuts++
	c.Bought += max(0, essence)
	c.BoughtSpent += max(0, spent)
}

// End records how a round of the campaign ended. A target or a full mule FINISHES the campaign (it re-arms only under
// craftRuneStartBelow); a round that ended on its time, its carriers or its essence PAUSES it for `pause`; a hand-over trip, a supply
// run and a passing failure change nothing (the campaign goes straight on, or backs off through the mode's own backoff).
func (c *CampaignLedger) End(kind runecraftplan.EndKind, why string, now time.Time, pause time.Duration) {
	c.LastEnd, c.LastWhy, c.LastEndAt = string(kind), why, now
	switch {
	case kind.Finishes():
		c.Active, c.Disarmed, c.PausedUntil = false, true, time.Time{}
		c.Done++
	case kind == runecraftplan.EndTime, kind == runecraftplan.EndCarriers, kind == runecraftplan.EndDry:
		c.PausedUntil = now.Add(pause)
	default:
		c.PausedUntil = time.Time{}
	}
}

// Paused reports whether an active campaign is sitting out at now, and until when.
func (c *CampaignLedger) Paused(now time.Time) (until time.Time, paused bool) {
	if c == nil || !c.Active {
		return time.Time{}, false
	}
	return c.PausedUntil, now.Before(c.PausedUntil)
}

// Reconcile settles a campaign that was Active when no round was running (a restart, a pause): one that has already reached the target
// or whose mule holds the stop level is finished, and a disarmed one whose mule has fallen under the start level is re-armed. It
// reports what it did ("" for nothing); the caller saves the ledger when it is not empty.
func (c *CampaignLedger) Reconcile(set RuneSettings, muleNature int, muleKnown bool, now time.Time) string {
	if c == nil {
		return ""
	}
	switch {
	case c.Active && set.Target > 0 && c.Crafted >= set.Target:
		c.End(runecraftplan.EndTarget, fmt.Sprintf("the campaign crafted %s, craftRuneTarget %s", runecraftplan.Thousands(c.Crafted), runecraftplan.Thousands(set.Target)), now, 0)
		return "finished at the target"
	case c.Active && muleKnown && set.StopAbove > 0 && muleNature >= set.StopAbove:
		c.End(runecraftplan.EndMuleFull, fmt.Sprintf("the mule holds %s Nature runes, craftRuneStopAbove %s", runecraftplan.Thousands(muleNature), runecraftplan.Thousands(set.StopAbove)), now, 0)
		return "finished: the mule holds enough"
	case !c.Active && c.Disarmed && muleKnown && muleNature < set.StartBelow:
		c.Disarmed = false
		return "re-armed: the mule's Natures are under " + runecraftplan.Thousands(set.StartBelow)
	}
	return ""
}

// combinedNature is the Nature runes the banks hold together (a user request: "starts when the combined bank total is under 100k"):
// every bank on the peer board when the bot read them, else the mule's own published count.
func combinedNature(s Snapshot) int {
	if s.Sched.NatureKnown {
		return s.Sched.Nature
	}
	return stockOf(s.Stock, "Nature rune")
}

// decideCampaign is the campaign's trigger, for decideNature once the gate's own conditions (level, world, backoff, talisman) hold:
// it starts a campaign (or carries one on) and says why not when it does not. ownTalisman says the main account holds a Nature talisman.
func decideCampaign(set Settings, s Snapshot, led *Ledger, ownTalisman bool) (Decision, string) {
	r, in := set.Rune, s.Rune
	c := &led.Rune.Campaign
	if c.GivenUp(set) {
		return Decision{}, "nature campaign: GIVEN UP: " + strconv.Itoa(c.HandOverFails) + " hand-overs of Nature runes to the mule in a row failed, so no round starts (the Natures stay in the bank); restart the script to try again"
	}
	if until, paused := c.Paused(s.Now); paused {
		return Decision{}, "nature campaign: paused until " + until.Format("15:04") + " (" + c.LastWhy + ")"
	}
	nature := runecraftplan.Nature
	v := runecraftplan.CampaignStartView{
		Known: in.Known, Level: in.Level, Members: in.Members,
		MuleHeard: s.MuleHeard, MuleSameWorld: s.MuleSameWorld, MuleKnown: s.Stock != nil,
		MuleNature: combinedNature(s), MuleEssence: stockOf(s.Stock, runecraftplan.EssenceName),
		MuleCoins: stockOf(s.Stock, "Coins"), MuleTalisman: stockOf(s.Stock, nature.Talisman), TalismanHeld: ownTalisman,
		BuyOut: in.BuyOut, BuyWhy: in.BuyOutWhy,
		Listed: len(r.Runners), AtWork: in.Carriers, Standby: in.Standby,
		Active: c.Active, Disarmed: c.Disarmed,
		StartBelow: r.StartBelow, MinEssence: r.MinEssence, MinRunners: r.MinRunners,
	}
	start, why := runecraftplan.DecideCampaignStart(v)
	if !start {
		return Decision{}, "nature campaign: " + why
	}
	return Decision{Mode: ModeNature, Campaign: true, Continue: c.Active, Why: "nature campaign: " + why + "; target " + runecraftplan.Thousands(r.Target) +
		", stops at " + runecraftplan.Thousands(r.StopAbove) + " on the mule"}, ""
}
