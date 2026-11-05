package mastercraftplan

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// THE ROUND SCHEDULE (a user request). A fixed priority list let one big stock block every other round, so the owner gave each
// round its own TRIGGER and LENGTH, and the main account rotates. The order Decide tries them in (the first that is due and not
// paused runs; a round under way always carries on first), and what brings each one back:
//
//	 1  trawler       the mule is out of Raw sea turtle, Raw manta ray and Raw shark; a 3-hour block (trawler.go)
//	 2  hand-over     products an earlier round left in the bank
//	 3  jewellery     the mule holds none (jewellery.go)
//	 4  nature        the banks hold under craftRuneStartBelow Natures together; runs to craftRuneTarget more (campaign.go)
//	 5  gems          20,000 uncut sapphires on the mule: all uncut gems are cut until done
//	 6  bows, pearls  as before (stock thresholds)
//	 7  staves        Battlestaff and Fire orb both at the threshold; as fast as possible
//	 8  storage       the mule's bank is nearly full
//	 9  shop, arrows  arrows and bolts to tip: only at craftArrowThreshold / craftBoltThreshold (100,000 each)
//	10  superheat     STEEL first (phase "steel"), then Mithril (and Gold with the gauntlets), 1-hour batches
//	11  platebodies   an hour, the highest bar, never Steel; paused craftPausePlatebodyMinutes after
//	11b bones         the mule holds craftBonesMinStock bones (bones.go): an hour of burying at the bank, in the rotation
//	12  cannonballs   its own trigger (cannonballs.go; not moved to the fleet in this pass)
//	13  herbs         once every craftHerbEveryMinutes (24 hours)
//	14  chest         the mule holds craftChestStartKeys (1,000) Crystal keys: loots them all, ahead of every NEW round but the trawler
//	15  adamantite    LAST: only when nothing above is due; 1-hour batches, paused craftPauseAdamantMinutes after
//	--  ScarabSwarm   fills every other minute; craftIdle "oaks-daily" chops 2-tick oaks craftOakMinutes (2 h) once every 24 hours
//
// Clock-based rules (herbs once a day, the oak window, the per-phase superheat pauses) are kept in Ledger.Sched, which is saved in
// the side store and so survives a restart.

// Heat phase names (HeatPhase.Name, ScheduleLedger.HeatPause).
const (
	PhaseSteel   = "steel"
	PhaseMithril = "mithril"
	PhaseAdamant = "adamant"
)

// ScheduleSettings are the schedule's knobs; every duration is a setting.
type ScheduleSettings struct {
	// TrawlerBlock is how long one trawler block lasts (owner: 3 hours); TrawlerRest the time after it before the raw fish
	// trigger may start the next (the other rounds have their turn; 0 runs the blocks back to back).
	TrawlerBlock, TrawlerRest time.Duration
	// SteelStart: a Steel phase starts when the combined Steel bars are under it; SteelStop: it ends when they reach it;
	// SteelMinCasts: the casts' worth of ore, coal and Natures a new Steel phase needs (owner: 500 bars).
	SteelStart, SteelStop, SteelMinCasts int
	// AdamantRound is the Adamantite batch (the Mithril batch is craftSuperheatRoundMinutes).
	AdamantRound time.Duration
	// PauseMithril, PauseAdamant and PausePlatebody are how long each round is left alone after its batch.
	PauseMithril, PauseAdamant, PausePlatebody time.Duration
	// HerbEvery is the least time between herb rounds.
	HerbEvery time.Duration
	// OakWindow is the oak chopping allowed in one OakEvery.
	OakWindow, OakEvery time.Duration
	// ChestStartKeys is the Crystal keys on the mule that start a chest round.
	ChestStartKeys int
	// BonesMinStock is the bones the mule must hold to start a bone-burying round, BonesRound its length and BonesPause the time it
	// is left alone after (bones.go).
	BonesMinStock          int
	BonesRound, BonesPause time.Duration
}

// The defaults.
const (
	DefaultTrawlerBlock   = 3 * time.Hour
	DefaultTrawlerRest    = 60 * time.Minute
	DefaultSteelStart     = 10000
	DefaultSteelStop      = 10000
	DefaultSteelMinCasts  = 500
	DefaultAdamantRound   = 60 * time.Minute
	DefaultSchedPause     = 60 * time.Minute
	DefaultHerbEvery      = 24 * time.Hour
	DefaultOakWindow      = 2 * time.Hour
	DefaultOakEvery       = 24 * time.Hour
	DefaultChestStartKeys = 1000
)

// The settings' names.
const (
	KeyTrawlerBlockMinutes   = "craftTrawlerBlockMinutes"
	KeyTrawlerRestMinutes    = "craftTrawlerRestMinutes"
	KeySteelStart            = "craftSteelStart"
	KeySteelStop             = "craftSteelStop"
	KeySteelMinCasts         = "craftSteelMinCasts"
	KeyAdamantRoundMinutes   = "craftAdamantRoundMinutes"
	KeyPauseMithrilMinutes   = "craftPauseMithrilMinutes"
	KeyPauseAdamantMinutes   = "craftPauseAdamantMinutes"
	KeyPausePlatebodyMinutes = "craftPausePlatebodyMinutes"
	KeyHerbEveryMinutes      = "craftHerbEveryMinutes"
	KeyOakMinutes            = "craftOakMinutes"
	KeyOakEveryMinutes       = "craftOakEveryMinutes"
	KeyChestStartKeys        = "craftChestStartKeys"
)

func init() {
	for _, k := range []string{KeyTrawlerBlockMinutes, KeyTrawlerRestMinutes, KeySteelStart, KeySteelStop, KeySteelMinCasts, KeyAdamantRoundMinutes, KeyPauseMithrilMinutes,
		KeyPauseAdamantMinutes, KeyPausePlatebodyMinutes, KeyHerbEveryMinutes, KeyOakMinutes, KeyOakEveryMinutes, KeyChestStartKeys} {
		knownKeys[k] = true
	}
}

// DefaultScheduleSettings are the owner's numbers.
func DefaultScheduleSettings() ScheduleSettings {
	return ScheduleSettings{
		TrawlerBlock: DefaultTrawlerBlock, TrawlerRest: DefaultTrawlerRest, SteelStart: DefaultSteelStart, SteelStop: DefaultSteelStop, SteelMinCasts: DefaultSteelMinCasts,
		AdamantRound: DefaultAdamantRound, PauseMithril: DefaultSchedPause, PauseAdamant: DefaultSchedPause, PausePlatebody: DefaultSchedPause,
		HerbEvery: DefaultHerbEvery, OakWindow: DefaultOakWindow, OakEvery: DefaultOakEvery, ChestStartKeys: DefaultChestStartKeys,
		BonesMinStock: DefaultBonesMinStock, BonesRound: DefaultBonesRound, BonesPause: DefaultBonesPause,
	}
}

// decode reads one key; it reports whether the key was one of the schedule's.
func (c *ScheduleSettings) decode(k string, v interface{}, num func(string, interface{}, *int), mins func(string, interface{}, *time.Duration)) bool {
	switch k {
	case KeyTrawlerBlockMinutes:
		mins(k, v, &c.TrawlerBlock)
	case KeyTrawlerRestMinutes:
		mins(k, v, &c.TrawlerRest)
	case KeySteelStart:
		num(k, v, &c.SteelStart)
	case KeySteelStop:
		num(k, v, &c.SteelStop)
	case KeySteelMinCasts:
		num(k, v, &c.SteelMinCasts)
	case KeyAdamantRoundMinutes:
		mins(k, v, &c.AdamantRound)
	case KeyPauseMithrilMinutes:
		mins(k, v, &c.PauseMithril)
	case KeyPauseAdamantMinutes:
		mins(k, v, &c.PauseAdamant)
	case KeyPausePlatebodyMinutes:
		mins(k, v, &c.PausePlatebody)
	case KeyHerbEveryMinutes:
		mins(k, v, &c.HerbEvery)
	case KeyOakMinutes:
		mins(k, v, &c.OakWindow)
	case KeyOakEveryMinutes:
		mins(k, v, &c.OakEvery)
	case KeyChestStartKeys:
		num(k, v, &c.ChestStartKeys)
	case KeyBonesMinStock:
		num(k, v, &c.BonesMinStock)
	case KeyBonesRound:
		mins(k, v, &c.BonesRound)
	case KeyBonesPause:
		mins(k, v, &c.BonesPause)
	default:
		return false
	}
	return true
}

func (c ScheduleSettings) validate() []string {
	var p []string
	dur := func(key string, d time.Duration, lo, hi time.Duration) {
		if d < lo || d > hi {
			p = append(p, fmt.Sprintf("%s %s is outside %d..%d minutes", key, d, int(lo.Minutes()), int(hi.Minutes())))
		}
	}
	dur(KeyTrawlerBlockMinutes, c.TrawlerBlock, 10*time.Minute, 24*time.Hour)
	dur(KeyTrawlerRestMinutes, c.TrawlerRest, 0, 24*time.Hour)
	dur(KeyAdamantRoundMinutes, c.AdamantRound, 0, 24*time.Hour)
	dur(KeyPauseMithrilMinutes, c.PauseMithril, 0, 24*time.Hour)
	dur(KeyPauseAdamantMinutes, c.PauseAdamant, 0, 24*time.Hour)
	dur(KeyPausePlatebodyMinutes, c.PausePlatebody, 0, 24*time.Hour)
	dur(KeyHerbEveryMinutes, c.HerbEvery, 0, 30*24*time.Hour)
	dur(KeyOakMinutes, c.OakWindow, 0, 24*time.Hour)
	dur(KeyOakEveryMinutes, c.OakEvery, time.Minute, 30*24*time.Hour)
	if c.OakWindow > c.OakEvery {
		p = append(p, fmt.Sprintf("%s %s is longer than %s %s", KeyOakMinutes, c.OakWindow, KeyOakEveryMinutes, c.OakEvery))
	}
	if c.SteelStart < 0 || c.SteelStop < 1 || c.SteelStop < c.SteelStart {
		p = append(p, fmt.Sprintf("%s %d and %s %d: the start is 0 (off) or up to the stop", KeySteelStart, c.SteelStart, KeySteelStop, c.SteelStop))
	}
	if c.SteelMinCasts < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeySteelMinCasts, c.SteelMinCasts))
	}
	if c.ChestStartKeys < 1 {
		p = append(p, fmt.Sprintf("%s %d is below 1", KeyChestStartKeys, c.ChestStartKeys))
	}
	p = append(p, c.validateBones()...)
	return p
}

// ScheduleLedger is the schedule's clock-based memory (Ledger.Sched).
type ScheduleLedger struct {
	// SteelActive: a Steel phase started and has not reached craftSteelStop yet (hysteresis when the start is under the stop).
	SteelActive bool `json:"steelActive,omitempty"`
	// HeatPause holds a superheat phase off until then (PhaseMithril, PhaseAdamant).
	HeatPause map[string]time.Time `json:"heatPause,omitempty"`
	// LastEnd is when a clock-based round last ended, by key (KeyHerbRound).
	LastEnd map[string]time.Time `json:"lastEnd,omitempty"`
	// LastRun is when a batch round (RotationKey) last ended: the rotation serves the one that waited longest.
	LastRun map[string]time.Time `json:"lastRun,omitempty"`
	// OakDay is when the current 24-hour oak window began and OakUsed the chopping done in it.
	OakDay  time.Time     `json:"oakDay,omitempty"`
	OakUsed time.Duration `json:"oakUsed,omitempty"`
}

// KeyHerbRound is LastEnd's key for the herb round.
const KeyHerbRound = "herbs"

// Due reports whether a round that last ended at LastEnd[key] may run again: every has passed (never run: due).
func (l ScheduleLedger) Due(key string, now time.Time, every time.Duration) bool {
	last := l.LastEnd[key]
	return every <= 0 || last.IsZero() || now.Sub(last) >= every
}

// NoteEnd records that the round of key ended at now.
func (l *ScheduleLedger) NoteEnd(key string, now time.Time) {
	if l.LastEnd == nil {
		l.LastEnd = map[string]time.Time{}
	}
	l.LastEnd[key] = now
}

// NoteRun records that the batch round of key ended at now.
func (l *ScheduleLedger) NoteRun(key string, now time.Time) {
	if key == "" {
		return
	}
	if l.LastRun == nil {
		l.LastRun = map[string]time.Time{}
	}
	l.LastRun[key] = now
}

// PauseHeat holds a superheat phase off for the phase's pause setting.
func (l *ScheduleLedger) PauseHeat(phase string, now time.Time, set Settings) {
	var d time.Duration
	switch phase {
	case PhaseMithril:
		d = set.Sched.PauseMithril
	case PhaseAdamant:
		d = set.Sched.PauseAdamant
	default:
		return
	}
	if l.HeatPause == nil {
		l.HeatPause = map[string]time.Time{}
	}
	if d <= 0 {
		delete(l.HeatPause, phase)
		return
	}
	l.HeatPause[phase] = now.Add(d)
}

// HeatPaused reports whether a phase is held off at now, and until when.
func (l ScheduleLedger) HeatPaused(phase string, now time.Time) (time.Time, bool) {
	until := l.HeatPause[phase]
	return until, now.Before(until)
}

// SettleSteel clears SteelActive once the combined Steel bars have reached the stop.
func (l *ScheduleLedger) SettleSteel(set Settings, steel int, known bool) bool {
	if l.SteelActive && known && steel >= set.Sched.SteelStop {
		l.SteelActive = false
		return true
	}
	return false
}

// ---- the oak window ----

// RollOakDay starts a new window when the last one began OakEvery ago (or never); it reports whether it did.
func (l *ScheduleLedger) RollOakDay(set Settings, now time.Time) bool {
	if l.OakDay.IsZero() || now.Sub(l.OakDay) >= set.Sched.OakEvery {
		l.OakDay, l.OakUsed = now, 0
		return true
	}
	return false
}

// OaksDue reports whether the 2-tick oaks may be chopped now: the window's allowance is not used up. extra is chopping under way
// that the ledger has not been told of yet. A window that has run its OakEvery is a new one.
func (l ScheduleLedger) OaksDue(set Settings, now time.Time, extra time.Duration) bool {
	if set.Sched.OakWindow <= 0 {
		return false
	}
	if l.OakDay.IsZero() || now.Sub(l.OakDay) >= set.Sched.OakEvery {
		return true
	}
	return l.OakUsed+extra < set.Sched.OakWindow
}

// NoteOaks adds chopping time to the window.
func (l *ScheduleLedger) NoteOaks(set Settings, now time.Time, chopped time.Duration) {
	l.RollOakDay(set, now)
	l.OakUsed += max(chopped, 0)
}

// ---- the superheat phases ----

// HeatPhase is one superheat batch's rules.
type HeatPhase struct {
	// Name is PhaseSteel, PhaseMithril or PhaseAdamant; Bars the bars it may make; Budget its time limit (0: none); SteelStop,
	// for the Steel phase, the combined stock it ends at (0 for the others).
	Name      string
	Bars      []string
	Budget    time.Duration
	SteelStop int
	// SteelAtStart is the combined Steel bars when the round was decided (the round adds what it makes to it).
	SteelAtStart int
}

// SteelReached reports whether a Steel round that began with SteelAtStart bars and has made made has reached the stop.
func (p HeatPhase) SteelReached(made int) bool {
	return p.Name == PhaseSteel && p.SteelStop > 0 && p.SteelAtStart+made >= p.SteelStop
}

// restrictBars keeps only the bars the (retired) craftSuperheatBars setting still allows; empty means all.
func restrictBars(set Settings, bars []string) []string {
	if len(set.SuperheatBars) == 0 {
		return bars
	}
	var out []string
	for _, n := range bars {
		for _, a := range set.SuperheatBars {
			if b, ok := BarByName(a); ok && b.Name == n {
				out = append(out, n)
			}
		}
	}
	return out
}

// ScheduleInput is what the schedule reads beyond Snapshot's other fields.
type ScheduleInput struct {
	// Steel is the Steel bars every bank holds together (the fleet's banks on the peer board plus this account's own);
	// SteelKnown that it was read.
	Steel      int
	SteelKnown bool
	// Nature is the Nature runes every bank holds together; NatureKnown that it was read.
	Nature      int
	NatureKnown bool
}

// steelNow is the Steel bars in every bank and whether that was read: the bot's reading of the fleet's banks (ScheduleInput), else
// the mule's published count plus this account's own bank.
func steelNow(s Snapshot) (int, bool) {
	if s.Sched.SteelKnown {
		return s.Sched.Steel, true
	}
	if s.Stock == nil {
		return 0, false
	}
	n := stockOf(s.Stock, "Steel bar")
	if s.OwnSmith != nil {
		n += s.OwnSmith("Steel bar")
	}
	return n, true
}

// heatPhases lists the phases open to Decide at this point, in the owner's order. final selects the last-of-all phase.
func heatPhases(set Settings, s Snapshot, final bool) []HeatPhase {
	if final {
		return []HeatPhase{{Name: PhaseAdamant, Bars: restrictBars(set, []string{"Adamantite bar", "Runite bar"}), Budget: set.Sched.AdamantRound}}
	}
	steelAt, _ := steelNow(s)
	steel := HeatPhase{Name: PhaseSteel, Bars: restrictBars(set, []string{"Steel bar"}), SteelStop: set.Sched.SteelStop, SteelAtStart: steelAt}
	mith := []string{"Mithril bar"}
	if set.SuperheatGold {
		mith = append(mith, "Gold bar")
	}
	return []HeatPhase{steel, {Name: PhaseMithril, Bars: restrictBars(set, mith), Budget: set.SuperheatRound}}
}

// decideHeat chooses a superheat batch. final false tries Steel then Mithril; final true tries the Adamantite batch (the caller
// has found nothing else due). why says why not.
func decideHeat(set Settings, s Snapshot, led *Ledger, final bool) (Decision, string) {
	if until := led.SuperheatPauseUntil; s.Now.Before(until) {
		return Decision{}, "superheat rounds are paused until " + until.Format("15:04")
	}
	if until, ok := led.BackedOff(ModeSuperheat, s.Now); ok {
		return Decision{}, "superheat rounds are backing off until " + until.Format("15:04") + " after " + strconv.Itoa(led.Fails(ModeSuperheat)) + " failed in a row"
	}
	why := ""
	add := func(w string) {
		if why != "" {
			why += "; "
		}
		why += w
	}
	for _, ph := range heatPhases(set, s, final) {
		if len(ph.Bars) == 0 {
			add(ph.Name + ": no bar allowed by craftSuperheatBars")
			continue
		}
		if until, held := led.Sched.HeatPaused(ph.Name, s.Now); held {
			add(ph.Name + " batches are paused until " + until.Format("15:04"))
			continue
		}
		hs := set
		hs.GauntletsHeld = s.GoldGauntlets
		hs.SuperheatBars = ph.Bars
		p, ok, resume, w := SuperheatReady(hs, s.MagicLevel, s.SmithingLevel, s.Stock, s.OwnSuperheat, s.FireStaff)
		if ph.Name == PhaseSteel {
			steel, known := steelNow(s)
			switch {
			case !known && !resume:
				add("steel: the combined Steel bars are not known yet")
				continue
			case known && steel >= set.Sched.SteelStop:
				add("steel: " + strconv.Itoa(steel) + " Steel bars, at the stop " + strconv.Itoa(set.Sched.SteelStop))
				continue
			case known && !led.Sched.SteelActive && steel >= set.Sched.SteelStart:
				add("steel: " + strconv.Itoa(steel) + " Steel bars, not under the start " + strconv.Itoa(set.Sched.SteelStart))
				continue
			}
			if ok && !resume && p.Total < set.Sched.SteelMinCasts {
				add("steel: the ore, coal and Natures make " + strconv.Itoa(p.Total) + " bars, under the " + strconv.Itoa(set.Sched.SteelMinCasts) + " a Steel batch needs")
				continue
			}
		}
		if !ok {
			add(ph.Name + ": " + w)
			continue
		}
		return Decision{Mode: ModeSuperheat, Superheat: p, Heat: ph, Resume: resume, Why: ph.Name + " batch: " + w}, ""
	}
	return Decision{}, "superheat: " + why
}

// heatBudget is the time limit of a superheat round on bar (0: none): the Mithril and Gold batch, the Adamantite and Runite one.
func heatBudget(set Settings, bar string) time.Duration {
	switch bar {
	case "Mithril bar", "Gold bar":
		return set.SuperheatRound
	case "Adamantite bar", "Runite bar":
		return set.Sched.AdamantRound
	}
	return 0
}

// ---- the chest ----

// ChestReadyFor reports whether the mule's key count starts a chest round under set.
func ChestReadyFor(set Settings, muleKeys int) bool { return muleKeys >= set.Sched.ChestStartKeys }

// ---- tipping ----

// TipsDue reports whether the tipping is due (a user request: only at 100,000 bolts or arrows to tip): arrows to tip are the
// arrowtips the mule holds, up to the headless arrows to match; bolts to tip the Pearl bolt tips, up to the Bolts.
func TipsDue(set Settings, arrowtips, headless, pearlTips, bolts int) (arrows, boltsDue bool) {
	return min(arrowtips, headless) >= set.ArrowThreshold, min(pearlTips, bolts) >= set.BoltThreshold
}

// ---- the rotation ----

// THE ROTATION. Decide returns the FIRST round that is due, so a round high in the list that is due again as soon as it ends (Steel,
// topping up a stock the cannonball fleet keeps eating) would hold every round below it off for ever. The batch rounds - Steel and
// Mithril superheating, platebodies, cannonballs and herbs - therefore take turns: of the batch rounds that are due now, the one that
// ended longest ago goes (never-run first; ties by Decide's order). Rounds of another kind (the trawler, gems, the chest, the Nature
// campaign, a hand-over) keep their place ahead of them, a round under way carries on, and the Adamantite batch stays last of all.

// RotationKey names the batch round d is, "" for any other decision (and for a resume, which is never rotated away from).
func RotationKey(d Decision) string {
	if d.Resume || d.Continue {
		return ""
	}
	switch d.Mode {
	case ModeSuperheat:
		if d.Heat.Name == PhaseSteel || d.Heat.Name == PhaseMithril {
			return d.Heat.Name
		}
	case ModePlatebody:
		return "platebody"
	case ModeBones:
		return KeyBonesRotation
	case ModeCannonballs:
		return "cannonballs"
	case ModeCannonRun:
		return "cannonrun"
	case ModeHerbs:
		return KeyHerbRound
	}
	return ""
}

// skipRound holds the batch round of key off in a copy of the ledger.
func skipRound(l *Ledger, key string, now time.Time) {
	far := now.Add(100 * 24 * time.Hour)
	switch key {
	case PhaseSteel, PhaseMithril:
		l.Sched.HeatPause[key] = far
	case "platebody":
		l.Plate.PauseUntil = far
	case KeyBonesRotation:
		l.Bones.PauseUntil = far
	case "cannonballs":
		l.Cannon.PauseUntil = far
	case "cannonrun":
		l.CannonRun.PauseUntil = far
	case KeyHerbRound:
		l.HerbPauseUntil = far
	}
}

// DecideRotating is Decide with the batch rounds taking turns (see THE ROTATION). It changes nothing in the ledger it was given.
func DecideRotating(set Settings, s Snapshot) Decision {
	d := Decide(set, s)
	key := RotationKey(d)
	if key == "" || s.Ledger == nil {
		return d
	}
	lc := *s.Ledger
	lc.Sched.HeatPause = map[string]time.Time{}
	for k, v := range s.Ledger.Sched.HeatPause {
		lc.Sched.HeatPause[k] = v
	}
	s2 := s
	s2.Ledger = &lc
	cands := []Decision{d}
	for i := 0; i < 6; i++ {
		skipRound(&lc, key, s.Now)
		nd := Decide(set, s2)
		if key = RotationKey(nd); key == "" {
			break
		}
		cands = append(cands, nd)
	}
	best, bestAt := cands[0], s.Ledger.Sched.LastRun[RotationKey(cands[0])]
	for _, c := range cands[1:] {
		if at := s.Ledger.Sched.LastRun[RotationKey(c)]; at.Before(bestAt) {
			best, bestAt = c, at
		}
	}
	if len(cands) > 1 {
		var due []string
		for _, c := range cands {
			due = append(due, RotationKey(c))
		}
		best.Why += " (in turn: due " + strings.Join(due, ", ") + "; " + RotationKey(best) + " ended longest ago)"
	}
	return best
}
