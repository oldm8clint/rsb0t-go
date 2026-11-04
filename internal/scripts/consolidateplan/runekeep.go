package consolidateplan

// THE THIEVES' RUNES (a user request). An account that pickpockets for runes -- Chaos from the Paladins at
// Thieving 70, Death and Blood from the Heroes at 80 -- hands the mule NONE of such a rune until it holds
// 10,000 of it, and then all of it but a ration to train with.
//
// The owner's words, the earlier one superseded by the later: "account that thieve for runes supply should
// pickpocket 10k runes before handing them off to the mule", then "we should have the mule stack up 20k of
// runes before it starts handing out 1k of each to the accounts requesting it, this will save the mule from
// continuously being traded and the scarabswarm accounts can go to thieving for runes, 10k of any rune type,
// hand over all but 1k casts to train with". This file is the thieves' half (the mule's half is
// orderplan/runegate.go): what an account keeps of a rune it pocketed, and when its surplus is worth a trade.
//
// THE RULE, per pocketed rune R, with n the account holds (bank and pack):
//
//   - n under Settings.PocketHold (10,000): nothing of R crosses. The rune stays and grows, the trade the
//     owner calls "continuously being traded" never happens, and the mule's pool is fed in lots of thousands.
//   - n at or over it: everything but the ration crosses, the ration being the runes of Settings.PocketCasts
//     (1,000) casts of the account's own training spell. A rune that spell does not use has a ration of 0, so
//     all of it goes. The hand-over leaves exactly the ration, so a second plan sees no surplus and the account
//     does not ask the mule for what it has just given (the ping-pong commit 3c6c62b5 fixed once already).
//
// The ration REPLACES the casts-to-the-Magic-target keep for such a rune: with Magic 99 as the goal the old keep
// would be a million runes, and with 66 a trainee's would be tens of thousands, either of which is "keep
// everything". The owner's number is 1,000 casts; the training round (the bot merges it by the larger count) is
// the same size. Every other rune keeps what KeepFor always kept.
//
// WHO IS A THIEF is the caller's reading (KeepInput.ThievingLevel is non-zero only for an account that works the
// rune-paying rungs): the level decides which runes, by the content's own gates below.
//
// Pure.

import (
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/fleetplan"
)

// The thieves' rule's defaults.
const (
	// DefaultPocketHold is the 10,000 a thief holds of a pocketed rune before it hands any over.
	DefaultPocketHold = 10_000
	// DefaultPocketCasts is the 1,000 casts' worth it keeps to train with when it does.
	DefaultPocketCasts = 1_000
)

// pocketRuneNames are the runes a pickpocket pays, with the Thieving level of the rung that pays each: the
// Paladin's Chaos from 70, the Hero's Death and Blood from 80 (skill_thieving pickpocket.dbrow; scarabplan.SourcesFor).
var pocketRuneNames = []struct {
	name  string
	level int
}{
	{"Chaos rune", fleetplan.PaladinLevel},
	{"Death rune", fleetplan.HeroLevel},
	{"Blood rune", fleetplan.HeroLevel},
}

// DefaultPocketRunes are the runes the rule covers by default: every rune a pickpocket pays.
func DefaultPocketRunes() []string {
	out := make([]string, 0, len(pocketRuneNames))
	for _, p := range pocketRuneNames {
		out = append(out, p.name)
	}
	return out
}

// PocketRuneLevel is the Thieving level at which a pickpocket pays the rune name, case-insensitively; false for
// a rune no pickpocket pays (those are never pocketed runes, whatever the setting lists).
func PocketRuneLevel(name string) (int, bool) {
	_, level, ok := pocketRune(name)
	return level, ok
}

// pocketRune is the canonical spelling and rung level of a pocketed rune name.
func pocketRune(name string) (string, int, bool) {
	name = strings.TrimSpace(name)
	for _, p := range pocketRuneNames {
		if strings.EqualFold(p.name, name) {
			return p.name, p.level, true
		}
	}
	return "", 0, false
}

// applyPocket turns the covered runes this account pockets into pocket runes: their keep becomes the ration
// (the larger of the training spells' needs for Settings.PocketCasts casts and any kit extra), and they are marked
// so PlanDeposit holds them back until the hold is reached. A rune is covered when it is on Settings.PocketRunes,
// a pickpocket pays it, and the account's Thieving level reaches that rung.
func (k *Keep) applyPocket(s Settings, in KeepInput) {
	if s.PocketHold <= 0 || in.ThievingLevel <= 0 {
		return
	}
	for _, raw := range s.PocketRunes {
		name, level, ok := pocketRune(raw)
		if !ok || in.ThievingLevel < level {
			continue
		}
		ration := 0
		for _, sp := range in.TrainSpells {
			for _, r := range fleetplan.RunesForCasts(sp, s.PocketCasts, in.Staff) {
				if strings.EqualFold(r.Name, name) {
					ration = max(ration, r.Count)
				}
			}
		}
		ration = max(ration, keepOf(in.ExtraRunes, name))
		// One key per rune, whatever spelling an earlier line used: keepOf walks a map.
		for key := range k.Runes {
			if strings.EqualFold(key, name) {
				delete(k.Runes, key)
			}
		}
		if ration > 0 {
			k.Runes[name] = ration
		}
		if k.Pocket == nil {
			k.Pocket = map[string]bool{}
		}
		k.Pocket[name] = true
	}
	if len(k.Pocket) > 0 {
		k.PocketHold = s.PocketHold
	}
}

// IsPocket reports whether name is one of the account's pocket runes.
func (k Keep) IsPocket(name string) bool {
	for p := range k.Pocket {
		if strings.EqualFold(p, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// pocketHeld reports whether a pocket rune of which the account holds n stays whole: it is under the hold.
func (k Keep) pocketHeld(name string, n int) bool {
	return k.PocketHold > 0 && n < k.PocketHold && k.IsPocket(name)
}

// PocketRow is one pocket rune's reading for the log: what the account holds, and what the rule keeps and hands.
type PocketRow struct {
	Name string
	Held int
	// Hold is the hold the rune must reach; Keep what stays and Hand what crosses.
	Hold, Keep, Hand int
}

// PocketReport is every pocket rune the account holds some of, by name, with the rule's verdict on it.
func PocketReport(k Keep, h Holdings) []PocketRow {
	var out []PocketRow
	for name := range k.Pocket {
		n := 0
		for held, c := range h.Runes {
			if strings.EqualFold(held, name) {
				n += c
			}
		}
		if n <= 0 {
			continue
		}
		row := PocketRow{Name: name, Held: n, Hold: k.PocketHold}
		if n < k.PocketHold {
			row.Keep = n
		} else {
			row.Keep = min(n, keepOf(k.Runes, name))
			row.Hand = n - row.Keep
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
