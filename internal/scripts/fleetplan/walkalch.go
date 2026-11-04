package fleetplan

import (
	"strings"
	"time"
)

// WalkAlchSettings are the walk-alch knobs (walkAlch* settings).
type WalkAlchSettings struct {
	// Enabled is walkAlch, default false: not lab- or live-proven yet.
	Enabled bool
	// MinMagic is walkAlchMinMagic, default 55 and never below High Level Alchemy's 55.
	MinMagic int
	// Items is the allow-list (walkAlchItems, comma separated; default DefaultAlchItems, the owner's
	// alch stock). Nothing else is ever alched, and never a neverAlch item whatever the list says.
	Items []string
	// MinValue is the least coins a cast must pay (walkAlchMinValue, default 1).
	MinValue int
	// Kit is the casts one mule ask sizes natures, fire runes and bows for (walkAlchKit, 200).
	Kit int
	// Carry is the most casts' worth of natures, fire runes and alchables the pack holds after a bank visit
	// (walkAlchCarry, default DefaultWalkAlchCarry); the rest of a kit stays banked (alchcarry.go says why). FightCarry
	// is the same for a visit that kits a safespot pen (walkAlchFightCarry, default DefaultWalkAlchFightCarry). The mule
	// is asked for Carry's worth, not Kit's.
	Carry, FightCarry int
	// FireReserve is fire runes a cast never spends (walkAlchFireReserve, default 0): an account
	// training a fire spell keeps that many.
	FireReserve int
	// AskEvery is the walk-alch mule ask's own interval (walkAlchAskMinutes, default 60).
	AskEvery time.Duration
}

// Normalised clamps s: MinMagic to High Level Alchemy's level, Kit to at least 1, the
// interval to at least ten minutes.
func (s WalkAlchSettings) Normalised() WalkAlchSettings {
	if s.MinMagic < HighLevelAlchemy.Level {
		s.MinMagic = HighLevelAlchemy.Level
	}
	if s.Kit < 1 {
		s.Kit = 200
	}
	if s.MinValue < 1 {
		s.MinValue = 1
	}
	if s.FireReserve < 0 {
		s.FireReserve = 0
	}
	if s.AskEvery < 10*time.Minute {
		s.AskEvery = 60 * time.Minute
	}
	if s.Carry <= 0 {
		s.Carry = DefaultWalkAlchCarry
	}
	if s.FightCarry <= 0 {
		s.FightCarry = DefaultWalkAlchFightCarry
	}
	return s
}

// CarryCasts is the casts' worth one bank visit leaves in the pack: Carry, never more than the kit asked for.
func (s WalkAlchSettings) CarryCasts() int {
	s = s.Normalised()
	return min(s.Carry, s.Kit)
}

// FightCarryCasts is CarryCasts for a safespot pen's kit: FightCarry, never more than the walk carry.
func (s WalkAlchSettings) FightCarryCasts() int {
	s = s.Normalised()
	return min(s.FightCarry, s.CarryCasts())
}

// trainSettings is s as the RankAlchables filter reads it.
func (s WalkAlchSettings) trainSettings() TrainModeSettings {
	return TrainModeSettings{AlchItems: s.Items, AlchMinValue: s.MinValue}
}

// RankWalkAlchables is RankAlchables under the walk-alch allow-list, never food: an account on
// the move eats what it carries, so neither an edible obj nor a mule food name is ever alched.
func (s WalkAlchSettings) RankWalkAlchables(held []AlchCandidate) []AlchCandidate {
	keep := make([]AlchCandidate, 0, len(held))
	for _, c := range held {
		if c.Edible || isMuleFood(c.Name) {
			continue
		}
		keep = append(keep, c)
	}
	return RankAlchables(keep, s.trainSettings())
}

func isMuleFood(name string) bool {
	for _, f := range MuleFoods {
		if strings.EqualFold(strings.TrimSpace(name), f) {
			return true
		}
	}
	return false
}

// ItemsRanked is the allow-list as a mule ask's choice ladder, best payout first.
func (s WalkAlchSettings) ItemsRanked() []string { return s.trainSettings().AlchItemsRanked() }

// WalkAlchFirePerCast is High Level Alchemy's fire runes (magic_spells.dbrow: firerune,5).
const WalkAlchFirePerCast = 5

// WalkAlchAfterClick is how long after a movement click a cast may be sent: two ticks, so the
// click and the cast can never be decoded in one server tick (which drops the route).
const WalkAlchAfterClick = 1200 * time.Millisecond

// WalkAlchCastTicks is the ticks from one accepted High Alchemy to the next: p_delay(3) plus
// the tick the delay ends on (alchplan.HighAlch.LockTicks).
const WalkAlchCastTicks = 5

// walkAlchArrive is webwalk's arriveRadius: the walker re-clicks once this close to its target.
const walkAlchArrive = 4

// WalkAlchAhead is the least distance (tiles) the walk's click target must still be ahead for a
// cast to fit before the walker's next click: its re-click radius plus the tiles covered during
// the cast's delay (two a tick running, one walking).
func WalkAlchAhead(running bool) int { return WalkCastAhead(running, WalkAlchCastTicks) }

// WalkCastAhead is WalkAlchAhead for a spell whose accepted casts are castTicks apart (walkenchant.go).
func WalkCastAhead(running bool, castTicks int) int {
	per := 1
	if running {
		per = 2
	}
	return walkAlchArrive + per*castTicks
}

// WalkAlchTriggerClear is how many tiles of the walk's path ahead must fire no zone trigger
// (zonetrigger.Fires) for a cast to go out: the ticks the cast holds the account (WalkAlchCastTicks
// less the tick its delay ends on) at two tiles a tick running, one walking, and two tiles for a
// view a tick stale. A trigger fired inside the delay queues an ENGINE script that cannot run until
// the delay ends, and the walk halts until then: on the lab engine on 6 Oct 2026 a run with a cast
// every 5 ticks covered 90 tiles in 62 ticks against 122 without (claude/tickmanip-1005). The fleet no
// longer needs it: scarabrunner.Travel sends a clear-move in front of every cast (actions.ClearMoveRequest),
// which lets the walk go on through the trigger; the gate runs only for a Travel tuned without it.
func WalkAlchTriggerClear(running bool) int {
	per := 1
	if running {
		per = 2
	}
	return per*(WalkAlchCastTicks-1) + 2
}

// WalkAlchPoll is one walk poll as the cast decision sees it.
type WalkAlchPoll struct {
	Magic int
	// SinceClick is how long ago the walk's last movement click went; SinceCast the last cast
	// (a large value when there was none).
	SinceClick, SinceCast time.Duration
	// Tick is the engine tick (600 ms when zero).
	Tick time.Duration
	// Ahead is tiles from the account to the click's target; LegEnd that the target ends the
	// leg (a hop or the destination follows, and the walk acts on reaching it).
	Ahead  int
	LegEnd bool
	// ZoneTriggerIn is the tiles along the walk's path to the first step that fires a zone trigger,
	// 1 when the last tick's own step fired one, 0 when none lies within WalkAlchTriggerClear.
	ZoneTriggerIn int
	// Running is the account's run toggle.
	Running bool
	// InCombat, Wilderness (level > 0) and Modal (an interface is open) each stop a cast.
	InCombat   bool
	Wilderness int
	Modal      bool
	// HP and MaxHP are the account's hitpoints (0 MaxHP when unread): below StallMinHPPercent no
	// cast goes out, so a bite can land (a cast's p_delay refuses it). NoFood (nothing to eat) lifts that
	// (StallStandAside).
	HP, MaxHP int
	NoFood    bool
	// Natures and Fire are the pack's runes; FireStaff a fire staff wielded.
	Natures, Fire int
	FireStaff     bool
	// Alchables is the approved alchables in the pack (RankWalkAlchables).
	Alchables int
}

// WalkAlchDecision is whether to cast now, and why not when not.
type WalkAlchDecision struct {
	Cast bool
	Why  string
}

// ShouldWalkAlch decides one poll. Cheap: every gate is a comparison on what the poll read.
func ShouldWalkAlch(s WalkAlchSettings, p WalkAlchPoll) WalkAlchDecision {
	no := func(why string) WalkAlchDecision { return WalkAlchDecision{Why: why} }
	if !s.Enabled {
		return no("walk alch off")
	}
	s = s.Normalised()
	tick := p.Tick
	if tick <= 0 {
		tick = 600 * time.Millisecond
	}
	switch {
	case p.Magic < s.MinMagic:
		return no("Magic below " + Itoa(s.MinMagic))
	case p.InCombat:
		return no("in combat")
	case StallStandAside(p.HP, p.MaxHP, p.NoFood):
		return no("hitpoints low: the account must be free to eat")
	case p.Wilderness > 0:
		return no("in the Wilderness")
	case p.Modal:
		return no("an interface is open")
	case p.SinceClick < WalkAlchAfterClick:
		return no("a movement click went out this tick")
	case p.SinceCast < time.Duration(WalkAlchCastTicks)*tick:
		return no("the last cast's delay is not over")
	case p.Ahead < WalkAlchAhead(p.Running):
		return no("the walk will click again inside the cast's delay")
	case p.ZoneTriggerIn > 0 && p.ZoneTriggerIn <= WalkAlchTriggerClear(p.Running):
		return no("the walk fires a zone trigger inside the cast's delay")
	case p.Natures < 1:
		return no("no nature runes")
	case !p.FireStaff && p.Fire-s.FireReserve < WalkAlchFirePerCast:
		return no("no fire staff and no fire runes above the reserve")
	case p.Alchables < 1:
		return no("nothing on the allow-list to alch")
	}
	return WalkAlchDecision{Cast: true}
}

// WalkAlchAskInput is what the walk-alch kit ask reads.
type WalkAlchAskInput struct {
	Magic int
	// Natures, Fire and Alchables are what the account carries now.
	Natures, Fire, Alchables int
	// BankNatures is the Nature runes the account's own bank holds as the session last saw it (0 when unread): it draws
	// them at its next bank visit, so stock may be asked for against them (the nature rule: alchnatures.go).
	BankNatures int
	// FireStaff is a fire staff wielded or carried.
	FireStaff bool
	// Mule is the mule's published bank, nil when unpublished.
	Mule func(item string) (int, bool)
	// AskAs names an alch stock item the way the mule is asked for it and publishes it (see
	// TrainModeAskInput.AskAs); nil asks by the plain name.
	AskAs func(item string) string
}

// WalkAlchAskLines is the walk-alch kit for one mule ask: natures, fire runes (none with a fire
// staff) and allow-listed alchables for s.Kit casts, each line only when the account holds less
// than a quarter of it. Empty below the level, with walk alch off, or when the account carries
// the kit. The natures are a FairShare line (39,000 on the mule, 3 Oct, shared by the cohort);
// the alchables are asked fewest-in-the-mule's-bank first (AskLine.FewestFirst).
func WalkAlchAskLines(s WalkAlchSettings, in WalkAlchAskInput) []AskLine {
	if !s.Enabled {
		return nil
	}
	s = s.Normalised()
	if in.Magic < s.MinMagic {
		return nil
	}
	kit := s.CarryCasts() // a trip's worth, not the whole kit: a death drops the lot (alchcarry.go)
	var lines []AskLine
	naturesAsked := false
	if need := kit - in.Natures; need > kit*3/4 {
		lines = append(lines, AskLine{Choices: []string{RuneNature}, Count: need, FairShare: true})
		naturesAsked = true
	}
	if !in.FireStaff {
		want := kit*WalkAlchFirePerCast + s.FireReserve
		if need := want - in.Fire; need > want*3/4 {
			lines = append(lines, AskLine{Choices: []string{RuneFire}, Count: need, FairShare: true})
		}
	}
	// Stock only against Nature runes: held (pack or the account's own bank) or asked in this same ask (alchnatures.go).
	if need := kit - in.Alchables; need > kit*3/4 && CanCastStock(in.Natures, in.BankNatures, naturesAsked) {

		// Fill: the best-paying items the mule holds, as many kinds as the kit's casts take (owner,
		// 6 Oct 2026), the strung bows and the like asked by their qualified names.
		// Best-paying first (a user request: "hand out the best profit alch like the magic longbow strung ... instead of
		// the maple strung long bows first"): fewest-first had every kit asking for the mule's last Maple longbows.
		// Fill, so each rung is asked up to what the mule holds and the next rung covers the rest (without it, 1-2 Nov the
		// line asked its first held rung for the whole count and the mule handed over only that rung's stock: 36 casts of 120).
		lines = append(lines, AskLine{Choices: AskNames(s.ItemsRanked(), in.AskAs), Count: need, Fill: true})
	}
	return lines
}

// WalkAlchKitWorthAsking is whether a filtered plan still funds casts: the mule must hold
// natures (or the account already carries some, or its own bank holds some) and something on the
// allow-list (or the account carries some). A plan that would bring only fire runes is not a reason to walk.
func WalkAlchKitWorthAsking(in WalkAlchAskInput, plan []AskPlan) bool {
	natures, alch := in.Natures > 0 || in.BankNatures > 0, in.Alchables > 0
	for _, p := range plan {
		for _, r := range p.Rungs {
			if r.Item == RuneNature {
				natures = true
			} else if r.Item != RuneFire {
				alch = true
			}
		}
	}
	return natures && alch
}
