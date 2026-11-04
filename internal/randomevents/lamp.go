package randomevents

// Which skill a Lamp advances (scarabqueue-1007). The Genie's Lamp (content's macro_genilamp, name
// "Lamp", Rub = OPHELD1) opens the xplamp skill-choice modal; the owner's rule for the ScarabSwarm,
// MasterCrafter and RuneRunner accounts is Magic, where this package used to hardcode Fishing for the
// fishers it was written for. The choice is per session (SetLampSkill) and defaults to Fishing, so
// every other script behaves as before.
//
// THE CONTENT FACTS (content/scripts/general/scripts/xplamp/xplamp.rs2, interfaces/xplamp.if):
//   - [opheld1,macro_genilamp] opens the modal: if_openmain(xplamp), interface 2808, and resets %xplamp.
//   - One click on a skill button selects it ([if_button,xplamp:<skill>] -> xplamp_select); the button
//     ids run in xplamp.if's declaration order from 2812 (attack, strength, ranged, MAGIC = 2815,
//     defence, hitpoints, prayer, agility, herblore, thieving 2821, crafting, runecraft, mining,
//     smithing, fishing 2826, cooking, firemaking, woodcutting, fletching). A choice the account is not
//     eligible for (Herblore without Druidic Ritual, Runecraft without Rune Mysteries) is refused with a
//     message and nothing is selected; Magic has no requirement.
//   - xplamp:confirm (2831) then deletes the Lamp and advances the chosen skill by its base level x 10
//     experience (stat_advance(stat, level x 100) in tenths), so a Magic-66 account gets 660 xp.
//   - The Lamp is "unbankable" (param=unbankable): bank.rs2's bank_check_allowed refuses it ("A magical
//     force prevents you from banking this item!"). It can therefore only ever be in the backpack, never
//     in an account's own bank, which is why nothing here withdraws one.

import (
	"fmt"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
)

// LampSkill is one skill button on the xplamp modal and the stat it advances.
type LampSkill struct {
	Name     string
	ButtonID int // the xplamp:<skill> component
	Stat     int // gamestate's stat index (UPDATE_STAT)
}

var (
	// LampFishing is the default: every script that could earn a Lamp used to be a fisher.
	LampFishing = LampSkill{Name: "Fishing", ButtonID: xplampFishingID, Stat: 10}
	// LampMagic is the ScarabSwarm / MasterCrafter / RuneRunner choice.
	LampMagic = LampSkill{Name: "Magic", ButtonID: 2815, Stat: 6}
)

var (
	lampSkillMu sync.Mutex
	lampSkills  = map[actions.Session]LampSkill{}
)

// SetLampSkill chooses the skill this session's Lamps advance. A script sets it in OnStart and
// restores LampFishing in OnStop.
func SetLampSkill(s actions.Session, skill LampSkill) {
	lampSkillMu.Lock()
	defer lampSkillMu.Unlock()
	if skill == LampFishing {
		delete(lampSkills, s)
		return
	}
	lampSkills[s] = skill
}

// lampSkillFor is the skill this session's Lamps advance.
func lampSkillFor(s actions.Session) LampSkill {
	lampSkillMu.Lock()
	defer lampSkillMu.Unlock()
	if skill, ok := lampSkills[s]; ok {
		return skill
	}
	return LampFishing
}

// LampRubbedLine is the log line for a Lamp that was spent: "lamp: rubbed for Magic (+660 xp)".
func LampRubbedLine(skill LampSkill, xp int) string {
	return fmt.Sprintf("lamp: rubbed for %s (+%d xp)", skill.Name, xp)
}

// lampXPSettle is how long a spent Lamp waits for its UPDATE_STAT, which can land a tick after the
// Lamp leaves the pack.
const lampXPSettle = 1500 * time.Millisecond

// lampXPGained is the experience the skill gained since xp0, waiting up to lampXPSettle for it.
func lampXPGained(s actions.Session, skill LampSkill, xp0 int) int {
	deadline := time.Now().Add(lampXPSettle)
	for {
		if sk, ok := s.State().Skill(skill.Stat); ok && sk.XP > xp0 {
			return sk.XP - xp0
		}
		if !time.Now().Before(deadline) {
			return 0
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// lampXP is the skill's experience now (0 when unknown).
func lampXP(s actions.Session, skill LampSkill) int {
	if sk, ok := s.State().Skill(skill.Stat); ok {
		return sk.XP
	}
	return 0
}

// HeldLamp finds a Lamp in the backpack, nil when there is none (or no name resolver yet). It looks
// at the backpack only: heldEventInventories.
func HeldLamp(s actions.Session) *Event {
	res := currentResolver()
	if res == nil {
		return nil
	}
	return detectBoxLampEligible(res, heldEventInventories(s.State()), func(ev *Event) bool { return ev.Name == "Lamp" })
}

// RubHeldLamps rubs every Lamp in the backpack for the session's chosen skill (SetLampSkill), under
// the account-wide action lock so the Guardian is not clicking at the same moment, and returns how
// many were spent. log gets the "lamp: rubbed for Magic (+N xp)" line for each. A script calls it
// from its own loop at a quiet moment; the Guardian claims a fresh Genie Lamp on its own.
func RubHeldLamps(s actions.Session, pack *pathfind.Pack, log func(string)) int {
	if HeldLamp(s) == nil {
		return 0
	}
	unlock := Lock(s)
	defer unlock()
	spent := 0
	for tries := 0; tries < 4; tries++ {
		ev := HeldLamp(s)
		if ev == nil {
			break
		}
		if err := handleLamp(s, pack, ev, log); err != nil {
			if log != nil {
				log("lamp: rub failed: " + err.Error())
			}
			break
		}
		if lampStillPresent(s, ev) {
			break // the modal never opened, or the choice was refused: not a loop to spin on
		}
		spent++
	}
	return spent
}
