package runecraftplan

// The Nature campaign (runecraft P4, docs/runecraft_p4_2026-10-14.md). a user request: "trigger when the mule is out of nature
// runes, make 100k runes for the mule, keep some instead of trading all then asking back". The gate round of P3 crafts what the
// carriers bring for as long as the main account is told to; a CAMPAIGN is the autopilot over it:
//
//	START   the mule's PUBLISHED Nature rune is under craftRuneStartBelow (1,000: its shortage floor is 2,000), Runecrafting 44, a
//	        members world, essence to work with (the mule's, or what a whole-stack market buy-out could fund), a Nature talisman, and at
//	        least craftRuneMinRunners carriers ready. A mule that is not heard, or has not published, never starts one.
//	RUN     a gate round, as P3, for as long as none of the ends below holds; a round that ends and leaves the campaign going is
//	        followed at once by the next (the main account's own hand-over trip, the supply run).
//	END     the Natures crafted this campaign reach craftRuneTarget (100,000), or the mule's published Nature reaches
//	        craftRuneStopAbove (60,000): the campaign is DONE and re-arms only when the mule falls under craftRuneStartBelow again
//	        (hysteresis: a stop at 60,000 must not start at once when the fleet uses 59,000). Or it is PAUSED: the round's eight hours
//	        are up, fewer than two carriers have been at work for half an hour, two fifteen-minute checks have seen no essence trade
//	        (a refill is tried first), the owner turned it off.
//	KEEP    the main account keeps craftRuneKeep (4,000: the chest round's 2,000-rune ask and the superheat) and hands the mule only what he holds
//	        beyond it, by a COURIER first (a carrier whose kit is spent takes up to craftRuneCourierMax of it home on its way), else
//	        himself when the surplus is craftRuneHandOverAt and no courier took any for craftRuneCourierWait, or at the end of any round.
//
// Pure: the bot (internal/bot/mastercrafter_campaign.go) reads the world and acts; this file decides, with the numbers the settings
// give it, and says why in the words the log uses.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The campaign's defaults (the settings' defaults, craftRune*).
const (
	// DefaultStartBelow is craftRuneStartBelow: a campaign starts when the mule's published Nature rune is under it.
	DefaultStartBelow = 1000
	// DefaultStopAbove is craftRuneStopAbove: a campaign ends when the mule's published Nature rune reaches it.
	DefaultStopAbove = 60000
	// DefaultTarget is craftRuneTarget: the Natures one campaign crafts (about 900,000 Runecrafting experience, Runecrafting 44 to 72).
	DefaultTarget = 100000
	// DefaultMinEssence is craftRuneMinEssence: the least essence a campaign starts with (the mule's, or a buy-out's).
	DefaultMinEssence = 5000
	// DefaultMinRunners is craftRuneMinRunners: the carriers that must be ready.
	DefaultMinRunners = 3
	// DefaultHandOverAt is craftRuneHandOverAt: the main account's own trip is made when he holds this much beyond his keep (and no courier is
	// coming); DefaultHandOverMin the least surplus an own trip is made for at all.
	DefaultHandOverAt  = 14000
	DefaultHandOverMin = 5000
	// DefaultCourierWait is craftRuneCourierWait: no courier took any for this long, so the main account delivers himself.
	DefaultCourierWait = 120 * time.Minute
	// DefaultPause is craftRunePauseMinutes: how long a campaign sits out after a round that ended on its time or on a gate that
	// came to nothing, so the other rounds (gems, bows, what is owed to the mule) get their turn.
	DefaultPause = 45 * time.Minute
	// DefaultMuleCoinFloor is craftRuneMuleCoinFloor: the coins the mule keeps when the main account takes its whole stack for essence.
	// The concrete consumers are the cohort's small asks (a kit's thousand-coin line, a fisher's 500-coin fare); the same floor the
	// RuneRunner's whole-stack top-up leaves (muleCoinFloor).
	DefaultMuleCoinFloor = 10000
)

// The end rule's patience.
const (
	// CampaignMinCarriers is the carriers a campaign wants at work; fewer than this for CampaignLowFor ends the round.
	CampaignMinCarriers = 2
	CampaignLowFor      = 30 * time.Minute
	// CampaignDryCheck is the length of one dry check: no essence trade for this long is one, CampaignDryChecks of them in a row
	// (30 minutes, GateDry) end the round.
	CampaignDryCheck  = 15 * time.Minute
	CampaignDryChecks = 2
	// CampaignRateWindow is the window the log's rate is measured over.
	CampaignRateWindow = 10 * time.Minute
)

// EndKind is why a gate round of a campaign ends.
type EndKind string

// The ends.
const (
	// EndTarget: the Natures crafted this campaign reached craftRuneTarget. The campaign is done.
	EndTarget EndKind = "target"
	// EndMuleFull: the mule's published Nature rune reached craftRuneStopAbove. The campaign is done.
	EndMuleFull EndKind = "mule-full"
	// EndHandOver: the main account holds more beyond his keep than a courier will take in time. The round ends, the main account walks home, hands the
	// mule the surplus and the campaign goes straight on with a new round.
	EndHandOver EndKind = "hand-over"
	// EndTime: craftRuneMaxHours are used up. The campaign pauses (craftRunePauseMinutes) and resumes.
	EndTime EndKind = "max-hours"
	// EndCarriers: fewer than CampaignMinCarriers carriers at work for CampaignLowFor. The campaign pauses.
	EndCarriers EndKind = "carriers"
	// EndDry: no essence arrived for CampaignDryChecks checks. A refill is tried (the supply run); with none possible the campaign
	// pauses.
	EndDry EndKind = "essence-dry"
	// EndNone is no end.
	EndNone EndKind = ""
)

// Finishes reports whether the end completes the campaign: it re-arms only under craftRuneStartBelow.
func (k EndKind) Finishes() bool { return k == EndTarget || k == EndMuleFull }

// Continues reports whether the campaign goes straight on after the round: the next decision is a new round, no pause.
func (k EndKind) Continues() bool { return k == EndHandOver }

// ---- the start ----

// CampaignStartView is what DecideCampaignStart reads.
type CampaignStartView struct {
	// Known is set once the Runecrafting level has been read; Level it; Members whether the world crafts members runes.
	Known   bool
	Level   int
	Members bool
	// MuleHeard and MuleSameWorld: the mule's heartbeat is on the bus, on this world. MuleKnown: its bank is published (a fresh row).
	MuleHeard, MuleSameWorld, MuleKnown bool
	// MuleNature, MuleEssence, MuleCoins and MuleTalisman are its published Nature runes, Rune essence, coins and Nature talismans.
	MuleNature, MuleEssence, MuleCoins, MuleTalisman int
	// TalismanHeld is whether the main account has a Nature talisman (the pack or his bank).
	TalismanHeld bool
	// BuyOut is the essence a whole-stack market buy-out could fund now (0: off, or not possible, BuyWhy says which).
	BuyOut int
	BuyWhy string
	// Listed is how many carriers craftRuneRunners names, AtWork how many are at work for him on the bus, Standby how many say
	// they are ready; the carriers ready are the larger of Listed and AtWork plus Standby.
	Listed, AtWork, Standby int
	// Active: a campaign is under way (this is its next round, not a start). Disarmed: the last campaign finished and the mule has
	// not been under StartBelow since.
	Active, Disarmed bool
	// The terms: craftRuneStartBelow, craftRuneMinEssence and craftRuneMinRunners.
	StartBelow, MinEssence, MinRunners int
}

// Carriers is the carriers ready: the larger of the names listed and those on the bus (at work or standing by).
func (v CampaignStartView) Carriers() int { return max(v.Listed, v.AtWork+v.Standby) }

// DecideCampaignStart says whether a campaign starts (or, Active, a round of the one under way does) and why not when it does not.
// The reasons are the log's, and the first that holds is the one said.
func DecideCampaignStart(v CampaignStartView) (start bool, why string) {
	nature := Nature
	switch {
	case !v.Known:
		return false, "the Runecrafting level has not been read yet"
	case v.Level < nature.Level:
		return false, "Runecrafting " + strconv.Itoa(v.Level) + " is under the " + strconv.Itoa(nature.Level) + " the Nature altar needs"
	case !v.Members:
		return false, "this is not a members world, and a Nature craft on a free world does nothing"
	case !v.MuleHeard:
		return false, "the mule is not heard on the bus"
	case !v.MuleSameWorld:
		return false, "the mule is on another world"
	case !v.MuleKnown:
		return false, "the mule has not published its bank, so its Nature runes are not known"
	}
	if !v.Active {
		switch {
		case v.MuleNature >= v.StartBelow && v.Disarmed:
			return false, "the last campaign finished and re-arms when the mule's Nature runes fall under " + Thousands(v.StartBelow) + " (it holds " + Thousands(v.MuleNature) + ")"
		case v.MuleNature >= v.StartBelow:
			return false, "the mule holds " + Thousands(v.MuleNature) + " Nature runes, not under the " + Thousands(v.StartBelow) + " that starts a campaign"
		}
	}
	if avail := v.MuleEssence + v.BuyOut; avail < v.MinEssence {
		w := "the mule holds " + Thousands(v.MuleEssence) + " Rune essence"
		if v.BuyOut > 0 {
			w += " and a market buy-out would fund " + Thousands(v.BuyOut)
		} else if v.BuyWhy != "" {
			w += ", no buy-out: " + v.BuyWhy
		}
		return false, w + " - " + Thousands(v.MinEssence) + " are needed to start"
	}
	if !v.TalismanHeld && v.MuleTalisman <= 0 {
		return false, "no " + Nature.Talisman + " in the pack or the bank, and the mule publishes none"
	}
	if n := v.Carriers(); n < v.MinRunners {
		return false, strconv.Itoa(n) + " carrier(s) ready (" + strconv.Itoa(v.Listed) + " listed, " + strconv.Itoa(v.AtWork) + " at work, " + strconv.Itoa(v.Standby) +
			" standing by), " + strconv.Itoa(v.MinRunners) + " needed"
	}
	word := "start - mule Nature " + Thousands(v.MuleNature) + " < " + Thousands(v.StartBelow)
	if v.Active {
		word = "go on - the campaign is under way, mule Nature " + Thousands(v.MuleNature)
	}
	via := Thousands(v.MuleEssence) + " essence on the mule"
	if v.BuyOut > 0 && v.MuleEssence < v.MinEssence {
		via += ", " + Thousands(v.BuyOut) + " more by a buy-out"
	}
	return true, word + "; " + via + "; " + strconv.Itoa(v.Carriers()) + " carriers ready"
}

// ---- the end of a round ----

// CampaignEndView is what DecideCampaignEnd reads.
type CampaignEndView struct {
	Now, Started time.Time
	// Opened is when the gate opened: the refill and the walk to the ruins come before it and can take most of an hour, and the
	// warm-up, the dry checks, the carriers' count and the courier's wait are all measured from it. The zero time is Started.
	Opened time.Time
	// MaxRound is craftRuneMaxHours (from Started: the whole round counts).
	MaxRound time.Duration
	// Target is craftRuneTarget and Crafted the Natures crafted this campaign (this round included); StopAbove is
	// craftRuneStopAbove, MuleNature the mule's published Nature runes and MuleKnown whether that reading is fresh.
	Target, Crafted, StopAbove int
	MuleNature                 int
	MuleKnown                  bool
	// Carriers is how many are at work now and LowSince since when fewer than CampaignMinCarriers have been (zero: not low).
	Carriers int
	LowSince time.Time
	// LastTrade is when essence last arrived this round (zero: none).
	LastTrade time.Time
	// Held is the Natures in the pack, Keep craftRuneKeep, HandOverAt craftRuneHandOverAt, HandOverMin craftRuneHandOverMin;
	// CourierOn whether couriers are allowed, LastCourier when one last took Natures this round (zero: none) and CourierWait
	// craftRuneCourierWait.
	Held, Keep, HandOverAt, HandOverMin int
	CourierOn                           bool
	LastCourier                         time.Time
	CourierWait                         time.Duration
}

// DecideCampaignEnd says whether the round of a campaign ends now, how, and why in the log's words. The order is the severity: the
// campaign's own ends (the target, the mule full), then the round's time, a gate without carriers, a gate without essence, and last
// the main account's hand-over, which the others do anyway on their way out.
func DecideCampaignEnd(v CampaignEndView) (end bool, kind EndKind, why string) {
	if v.Target > 0 && v.Crafted >= v.Target {
		return true, EndTarget, "the campaign has crafted " + Thousands(v.Crafted) + " Nature runes, craftRuneTarget " + Thousands(v.Target)
	}
	if v.MuleKnown && v.StopAbove > 0 && v.MuleNature >= v.StopAbove {
		return true, EndMuleFull, "the mule holds " + Thousands(v.MuleNature) + " Nature runes, craftRuneStopAbove " + Thousands(v.StopAbove)
	}
	if v.MaxRound > 0 && v.Now.Sub(v.Started) >= v.MaxRound {
		return true, EndTime, "the round's time (craftRuneMaxHours " + v.MaxRound.String() + ") is used up"
	}
	opened := v.Opened
	if opened.IsZero() || opened.Before(v.Started) {
		opened = v.Started
	}
	warm := v.Now.Sub(opened) >= GateWarmUp
	if warm && !v.LowSince.IsZero() && v.Now.Sub(v.LowSince) >= CampaignLowFor {
		return true, EndCarriers, "fewer than " + strconv.Itoa(CampaignMinCarriers) + " carriers have been at work for " + v.Now.Sub(v.LowSince).Round(time.Minute).String() +
			" (" + strconv.Itoa(v.Carriers) + " now)"
	}
	if warm {
		if checks := DryChecks(v.Now, opened, v.LastTrade); checks >= CampaignDryChecks {
			return true, EndDry, "no essence has arrived for " + (CampaignDryCheck * time.Duration(checks)).String() + " (" + strconv.Itoa(checks) + " of " + strconv.Itoa(CampaignDryChecks) + " dry checks)"
		}
	}
	if due, w := OwnTripDue(OwnTripView{Now: v.Now, Started: opened, Held: v.Held, Keep: v.Keep, At: v.HandOverAt, Min: v.HandOverMin,
		CourierOn: v.CourierOn, LastCourier: v.LastCourier, Wait: v.CourierWait}); due {
		return true, EndHandOver, w
	}
	return false, EndNone, ""
}

// DryChecks is how many whole CampaignDryCheck periods have passed with no essence trade: since the last trade, or since the round
// started when there has been none.
func DryChecks(now, started, lastTrade time.Time) int {
	since := lastTrade
	if since.IsZero() || since.Before(started) {
		since = started
	}
	if now.Before(since) {
		return 0
	}
	return int(now.Sub(since) / CampaignDryCheck)
}

// CarrierWatch notes how long the carriers at work have been fewer than a campaign wants.
type CarrierWatch struct {
	// Since is when the count last fell under the minimum (zero: it is not under it).
	Since time.Time
}

// Note records the carriers at work now against min and returns since when they have been under it (zero: they are not).
func (w *CarrierWatch) Note(now time.Time, carriers, min int) time.Time {
	if carriers >= min {
		w.Since = time.Time{}
		return time.Time{}
	}
	if w.Since.IsZero() {
		w.Since = now
	}
	return w.Since
}

// ---- keeping and handing over ----

// OwnTripView is what OwnTripDue reads.
type OwnTripView struct {
	Now, Started time.Time
	// Held is the Natures in the pack, Keep what he never hands over, At the surplus an own trip is made at, Min the least surplus
	// worth one.
	Held, Keep, At, Min int
	// CourierOn is whether couriers are allowed; LastCourier when one last took Natures (zero: none this round); Wait craftRuneCourierWait.
	CourierOn   bool
	LastCourier time.Time
	Wait        time.Duration
}

// HandOverSurplus is what the main account may hand the mule: the Natures held beyond his keep.
func HandOverSurplus(held, keep int) int { return max(0, held-keep) }

// OwnTripDue says whether the main account should leave the gate now and deliver the surplus himself. A courier is the way: it carries on its
// kit trip and costs the main account no time. He goes himself when the surplus reaches `At` and no courier has taken any for `Wait` (none is
// coming), or whatever couriers do when it reaches twice that (the stack at risk on an island with no bank: a death drops it); and
// never for less than `Min`.
func OwnTripDue(v OwnTripView) (due bool, why string) {
	surplus := HandOverSurplus(v.Held, v.Keep)
	if surplus <= 0 || surplus < v.Min || v.At <= 0 {
		return false, ""
	}
	if surplus >= 2*v.At {
		return true, Thousands(surplus) + " Nature runes are held beyond the keep of " + Thousands(v.Keep) + ", twice the " + Thousands(v.At) + " an own trip waits for: delivering them"
	}
	if surplus < v.At {
		return false, ""
	}
	if !v.CourierOn {
		return true, Thousands(surplus) + " Nature runes are held beyond the keep of " + Thousands(v.Keep) + " and couriers are off: delivering them"
	}
	ref := v.LastCourier
	if ref.IsZero() || ref.Before(v.Started) {
		ref = v.Started
	}
	if idle := v.Now.Sub(ref); idle >= v.Wait {
		return true, Thousands(surplus) + " Nature runes are held beyond the keep of " + Thousands(v.Keep) + " and no courier has come for " + idle.Round(time.Minute).String() + ": delivering them"
	}
	return false, ""
}

// ---- the supply the campaign draws on ----

// CampaignNeed is the essence the campaign still lacks: what is left of the target (the Natures to make, one essence each), less
// the essence the mule, the main account's bank and his pack already hold. At least 0.
func CampaignNeed(target, crafted, mule, bank, pack int) int {
	return max(0, target-crafted-mule-bank-pack)
}

// ---- the measure ----

// RateWindow measures a running total over a window: Note the total as it grows, ask PerHour.
type RateWindow struct {
	// Window is how far back the rate looks (CampaignRateWindow when zero).
	Window  time.Duration
	samples []rateSample
}

type rateSample struct {
	at time.Time
	n  int
}

// Note records the total at now. Samples older than the window are dropped, keeping one at or before its edge to measure from.
func (r *RateWindow) Note(now time.Time, total int) {
	w := r.Window
	if w <= 0 {
		w = CampaignRateWindow
	}
	r.samples = append(r.samples, rateSample{now, total})
	cut := 0
	for i, s := range r.samples {
		if now.Sub(s.at) > w {
			cut = i
		}
	}
	if cut > 0 {
		r.samples = append([]rateSample(nil), r.samples[cut:]...)
	}
}

// PerHour is the growth of the total from the oldest sample in the window to now, an hour's worth (0 with under two samples or no
// time between them). Measured to now, not to the newest sample, so a stalled gate's rate falls as the minutes pass.
func (r *RateWindow) PerHour(now time.Time) int {
	if len(r.samples) < 2 {
		return 0
	}
	first, last := r.samples[0], r.samples[len(r.samples)-1]
	span := now.Sub(first.at)
	if span <= 0 {
		return 0
	}
	return int(float64(last.n-first.n) / span.Hours())
}

// ETA is how long `left` more takes at perHour; 0 when the rate is none.
func ETA(left, perHour int) time.Duration {
	if left <= 0 || perHour <= 0 {
		return 0
	}
	return time.Duration(float64(left) / float64(perHour) * float64(time.Hour))
}

// Describe is the end in one phrase for a log.
func (k EndKind) Describe() string {
	switch k {
	case EndTarget:
		return "the target was reached"
	case EndMuleFull:
		return "the mule holds enough"
	case EndHandOver:
		return "an own hand-over trip"
	case EndTime:
		return "the round's time was used up"
	case EndCarriers:
		return "too few carriers"
	case EndDry:
		return "no essence came"
	}
	return strings.TrimSpace(string(k))
}

// CampaignLine is the log line of a campaign's progress: what is crafted, how fast, who is at work, what is left to work with and the
// mule's Natures, and how long the target takes at the measured rate.
func CampaignLine(crafted, target, perHour, carriers, muleEssence, muleNature, held, keep int) string {
	eta := "unknown"
	if d := ETA(target-crafted, perHour); d > 0 {
		eta = d.Round(time.Minute).String()
	}
	return fmt.Sprintf("campaign: crafted %s of %s Nature runes, %s/h over the last %s, %d carrier(s) at work, the mule holds %s essence and %s Nature runes, the main account holds %s (keep %s); the target in about %s",
		Thousands(crafted), Thousands(target), Thousands(perHour), CampaignRateWindow, carriers, Thousands(muleEssence), Thousands(muleNature), Thousands(held), Thousands(keep), eta)
}
