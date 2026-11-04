package runerunnerplan

// RuneRunner's travel: the run staller, walk alchemy and teleports on its long walks.
//
// a user request: RuneRunner's walks (the boat chain to Port Sarim, Varrock, Yanille, East Ardougne, back to
// Catherby) used none of what ScarabSwarm's walks use -- no jewellery rub, no High Level Alchemy aside, no teleport --
// so it ran dry and crawled. The machinery is scarabrunner.Travel; what is RuneRunner's own is what it may spend:
//
//   - Walk alchemy burns Nature runes, which are the fleet's bottleneck (the mule held 107 on 7 October while the
//     runners bought them at the Wizards' Guild). So it is on only while the mule's published bank holds at least
//     AlchNatureFloor of them (AlchSpare), and its kit (natures, an alchable) comes from the mule after a delivery.
//   - A teleport costs a Law rune and its elementals. The mule's own law floor gates the ask (TeleTravelAskLines), a
//     runner with no Staff of air pays the air from the pack, and TravelAirLine asks for those when the mule has plenty.
//   - The rub item is one Games necklace or Ring of dueling, drawn from the account's own bank when the pack holds
//     none, kept through its deposits and asked of the mule when neither holds one.
//
// This file is only the decision of what to ask the mule for; the I/O is internal/bot/runerunner_travel.go.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

// Travel defaults.
const (
	// DefaultWalkAlch, DefaultWalkRub and DefaultTeleTravel are the three travel settings' defaults: all on.
	DefaultWalkAlch   = true
	DefaultWalkRub    = true
	DefaultTeleTravel = true
	// TravelAlchKit is how many casts one kit ask sizes natures and alchables for.
	TravelAlchKit = 100
	// TravelTeleKit is how many teleports one kit ask sizes laws for.
	TravelTeleKit = 10
	// TravelAirPerCast is the air runes one teleport takes (Varrock, Falador and Lumbridge cost 3, Camelot 5).
	TravelAirPerCast = 5
	// TravelAirFloor is the air runes the mule must hold before it is asked for any: the cohort's own casting
	// comes first.
	TravelAirFloor = 5000
	// KitAskEvery bounds how often a runner asks the mule for its travel kit.
	KitAskEvery = 10 * time.Minute
)

// AlchNatureFloor is the Nature runes the mule must hold before a runner walk-alchs: the same level
// ShortageLevels calls "not short", so a mule the shortage rule is still feeding is never drawn on.
func AlchNatureFloor() int { return ShortageLevels["Nature rune"] }

// AlchSpare reports whether the mule can spare Nature runes for walk alchemy, and says why not. An unpublished
// bank is not spare: nothing is spent on a guess.
func AlchSpare(muleNatures int, known bool) (bool, string) {
	switch {
	case !known:
		return false, "the mule has published no bank, so its Nature runes are not known to be spare"
	case muleNatures < AlchNatureFloor():
		return false, fmt.Sprintf("the mule holds %d Nature rune(s), under %d: they go to the cohort", muleNatures, AlchNatureFloor())
	}
	return true, ""
}

// TravelAirLine is the Air rune line of a travel kit: five casts' worth of air less what the pack holds,
// only when the account wields no Staff of air (which pays the air itself), the kit has laws in it
// (lawsAsked), and the mule holds TravelAirFloor or more.
func TravelAirLine(staff string, airHeld, muleAir int, muleKnown, lawsAsked bool) (fleetplan.AskLine, bool) {
	if !lawsAsked || strings.EqualFold(strings.TrimSpace(staff), "Staff of air") {
		return fleetplan.AskLine{}, false
	}
	if muleKnown && muleAir < TravelAirFloor {
		return fleetplan.AskLine{}, false
	}
	need := TravelAirPerCast*TravelTeleKit - airHeld
	if need <= 0 {
		return fleetplan.AskLine{}, false
	}
	return fleetplan.AskLine{Choices: []string{"Air rune"}, Count: need}, true
}

// TravelKitInput is what a runner's travel kit is made from.
type TravelKitInput struct {
	Alch fleetplan.WalkAlchSettings
	Tele fleetplan.TeleTravelSettings
	// Rub is walkRub; RubHeld whether the pack holds a Games necklace or Ring of dueling.
	Rub, RubHeld bool
	Members      bool
	Magic        int
	// Natures, Fire and Alchables are what the pack carries; FireStaff a fire staff worn or carried.
	Natures, Fire, Alchables int
	FireStaff                bool
	// Pack is the runes carried; Staff the staff worn ("" for none).
	Pack  fleetplan.RuneStock
	Staff string
	// QuestDone answers a quest gate (nil: none done).
	QuestDone func(string) bool
	// Mule is the mule's published bank, nil when unpublished.
	Mule  func(item string) (int, bool)
	AskAs func(item string) string
}

// TravelKit is what to ask the mule for, filtered against its published bank, and the log words for what
// was left out. An empty plan means: do not ask. Nothing is asked of an unpublished bank.
func TravelKit(in TravelKitInput) (plan []fleetplan.AskPlan, none []string, notes []string) {
	if in.Mule == nil {
		return nil, nil, []string{"the mule's bank is not published"}
	}
	var lines []fleetplan.AskLine
	if in.Alch.Enabled {
		natures, known := in.Mule("Nature rune")
		if ok, why := AlchSpare(natures, known); !ok {
			notes = append(notes, "no walk alch kit: "+why)
		} else {
			ai := fleetplan.WalkAlchAskInput{Magic: in.Magic, Natures: in.Natures, Fire: in.Fire, Alchables: in.Alchables,
				FireStaff: in.FireStaff, Mule: in.Mule, AskAs: in.AskAs}
			alch := fleetplan.WalkAlchAskLines(in.Alch, ai)
			if len(alch) > 0 {
				if p, _ := fleetplan.FilterAskByBank(alch, in.Mule); fleetplan.WalkAlchKitWorthAsking(ai, p) {
					lines = append(lines, alch...)
				} else {
					notes = append(notes, "no walk alch kit: the mule holds no alchable or no Nature rune to pair")
				}
			}
		}
	}
	if in.Tele.Enabled {
		tele := fleetplan.TeleTravelAskLines(in.Tele, fleetplan.TeleTravelAskInput{
			Magic: in.Magic, Members: in.Members, Pack: in.Pack, Staff: in.Staff, QuestDone: in.QuestDone, Mule: in.Mule,
		})
		lawsAsked := false
		for _, l := range tele {
			for _, c := range l.Choices {
				if strings.EqualFold(c, "Law rune") {
					lawsAsked = true
				}
			}
		}
		lines = append(lines, tele...)
		if in.Tele.AirRunes {
			air, known := in.Mule("Air rune")
			if l, ok := TravelAirLine(in.Staff, in.Pack["Air rune"], air, known, lawsAsked); ok {
				lines = append(lines, l)
			}
		}
	}
	if in.Rub {
		if l, ok := fleetplan.RubAskLine(in.RubHeld); ok {
			lines = append(lines, l)
		}
	}
	plan, none = fleetplan.FilterAskByBank(lines, in.Mule)
	return plan, none, notes
}
