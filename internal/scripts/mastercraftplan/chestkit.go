package mastercraftplan

// The crystal chest round's kit and its pack (a user request). Pure, like the rest of the plan.
//
//   - Which runes a worn staff already pays for: "masters taking air runes when it has an air staff
//     equipped". Content's [proc,staff_runes] (skill_magic/scripts/magic.rs2) takes a wielded staff's
//     element off every spell that needs it (skill_magic/configs/magic_staff.dbrow), so the Falador
//     Teleport's three Air runes, and the Camelot Teleport's five, are not in the kit at all.
//   - The Strange fruit that keeps the walk to the chest running: "crystal chest runs could be sped
//     up by using strange fruit to restore run energy while running from the bank to the chest".
//     Content's consume_effect_strange_fruit is healenergy(3000), 30% of the run bar a bite
//     (strange_fruit.rs2); the engine drains 67 to 134 of the 10000 a tick while running
//     (Player.updateEnergy), so the 160 tiles from the Falador west bank to the chest cost about 55%
//     at no weight and a trip does not pay for itself from the regeneration standing still.
//   - What a bank visit does with the pack: everything is banked, and the kit drawn again from what
//     the bank then holds. Banking the lot is what gets the chest's coins and runes into the bank
//     (they used to ride in the pack from trip to trip, kept "for the fare" and "for the spells"),
//     gives the kit its exact count however the last trip left it, and turns the mule's notes
//     (keys, fruit) into items without a step of their own.

import (
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/lootchestfisherplan"
)

// The runes the round's spells use, as scarabplan.Teleports names them: Falador Teleport's water, air
// and law, Camelot's air and law, Varrock's fire, air and law.
const (
	RuneAir   = "Air rune"
	RuneWater = "Water rune"
	RuneLaw   = "Law rune"
	RuneFire  = "Fire rune"
)

const (
	// ChestFruit is the Strange fruit's name (scarabplan.StrangeFruit), the Strange plant random
	// event's fruit, which the mule holds a supply of.
	ChestFruit = fleetplan.StrangeFruit
	// ChestFruitCarry is the fruit a bank visit draws for a trip: about two bites carry the run to
	// the chest (the trip costs about 55% of the bar, a bite gives 30%). A fruit still in the pack at
	// the chest is a slot the loot could use, so the chest eats what is left when a slot is all that
	// stands between it and the next open (ChestStepEat).
	ChestFruitCarry = 3
	// ChestFruitAsk is the fruit the round's key ask also asks the mule for, as notes: about a
	// thousand, and only what the mule holds (owner: "should request about ~1000 not definitive").
	ChestFruitAsk = 1000
	// ChestFruitRunAt is the run energy, in percent, at or below which a fruit is eaten on a walk.
	// 20, because a bite takes effect within two ticks of the poll that finds it, a heavy pack drains
	// 0.9% a tick, and a poll can be five seconds behind (a bank or dialogue open skips it): 20% is
	// about 22 ticks of run in hand, more than any of that, so the bar never reaches the 0 that drops
	// the run toggle (Player.updateEnergy), and a bite lifts it no higher than 50%, so none of the
	// 30% is ever wasted against the 100% ceiling.
	ChestFruitRunAt = 20
)

// ChestEatFruit reports whether a walk eats a Strange fruit now: run energy known and at or below
// ChestFruitRunAt, and a fruit in the pack.
func ChestEatFruit(energy int, known bool, held int) bool {
	return known && held > 0 && energy <= ChestFruitRunAt
}

// ChestBiteGap is the least spacing of two bites. Content refuses a second one until the third tick
// after the first (consume.rs2 player_consume_item sets %eat_delay to map_clock + 2 and refuses a bite
// while %eat_delay >= map_clock): 1.8 seconds, and a refused bite looks to the client like a fruit that
// was never eaten, which costs the whole wait for it.
const ChestBiteGap = 2 * time.Second

// ChestBiteWait is how long to hold the next bite so it falls ChestBiteGap after the last one, given
// how long ago that was (a bite never made reads as a long time ago).
func ChestBiteWait(since time.Duration) time.Duration {
	if since >= ChestBiteGap {
		return 0
	}
	return ChestBiteGap - max(since, 0)
}

// chestStaves is content's magic_staff_table (skill_magic/configs/magic_staff.dbrow) for the two
// elements the round's spells use. The battlestaves are members items, and on a free world they pay
// nothing ([proc,staff_runes]: map_members false and a members staff returns the full cost).
var chestStaves = []struct {
	Rune    string
	Staves  []string
	Members []string
}{
	{RuneAir, []string{"Staff of air"}, []string{"Air battlestaff", "Mystic air staff"}},
	{RuneWater, []string{"Staff of water"}, []string{"Water battlestaff", "Mystic water staff"}},
	// The Staff of fire pays the Fire runes of Varrock Teleport and of High Level Alchemy (five a cast), which
	// the chest round's walks and chest-side alchemy cast (a user request: "wield a Staff of fire").
	{RuneFire, []string{"Staff of fire"}, []string{"Fire battlestaff", "Mystic fire staff", "Lava battlestaff", "Mystic lava staff"}},
}

// RunesPaidByStaff is the runes of the round's spells the worn items pay for, by rune name. members
// is whether the world is a members one (Settings.MembersWorld). Names compare without case.
func RunesPaidByStaff(worn []string, members bool) map[string]bool {
	out := map[string]bool{}
	wears := func(names []string) bool {
		for _, w := range worn {
			for _, n := range names {
				if strings.EqualFold(strings.TrimSpace(w), n) {
					return true
				}
			}
		}
		return false
	}
	for _, c := range chestStaves {
		if wears(c.Staves) || (members && wears(c.Members)) {
			out[c.Rune] = true
		}
	}
	return out
}

// ---- the kit ----

// KitIDs are the object ids behind the kit's items, as the cache gives them: the unnoted ids, the
// ones a bank draws from.
type KitIDs struct {
	// Keys are the unnoted Crystal key ids, the lowest first.
	Keys []int
	// Runes are the unnoted rune ids by rune name.
	Runes map[string][]int
	// Coins are the unnoted coin ids (content has two).
	Coins []int
	// Fruit are the unnoted Strange fruit ids.
	Fruit []int
	// Extras are the kit's other items (chestalch.go): the nature runes and noted rubies of the alchemy, the
	// Games necklace of the Games Room route, the weapon a Staff of fire replaced.
	Extras []ExtraKit
}

// ExtraKit is one more item of the kit: its name, the object ids a bank draws it from (unnoted), the ids it
// has in the pack when it arrives (the same ids, or its notes' when it is drawn as notes) and how many a
// visit carries.
type ExtraKit struct {
	Name string
	// Bank are the unnoted ids the bank holds it as, tried in order; Pack the ids that count as the kit in
	// the pack (Bank itself unless Noted).
	Bank, Pack []int
	// Noted says the item is withdrawn as notes (one pack slot for any number).
	Noted bool
	// Carry is how many a visit draws and how many the pack may hold before the rest counts as loot.
	Carry int
	// Keep says a bank visit leaves the item in the pack instead of banking it with the rest.
	Keep bool
	// Group names a set of alternatives: of the extras sharing a Group a visit draws the first the bank
	// holds (the noted rubies, else the noted diamonds), not all of them.
	Group string
}

// KitWant is what one bank visit draws, once the pack is banked, to be carrying the kit.
type KitWant struct {
	// Keys is the keys to draw, Runes the exact count of each rune, Coins the fare and Fruit the
	// Strange fruit.
	Keys  int
	Runes map[string]int
	Coins int
	Fruit int
	// Extras is the count of each of KitIDs.Extras to draw, by name (a name absent is none).
	Extras map[string]int
}

// ChestKitWant is the kit a visit draws: the trip's keys (ChestKeysToDraw, none on the way home),
// the runes the spells need that no worn staff pays (ChestRunes), the ships' fare and the Strange
// fruit. banked is the keys the bank holds with the pack banked.
func ChestKitWant(atPost bool, magic int, paid map[string]bool, expired bool, banked int) KitWant {
	w := KitWant{Runes: ChestRunes(magic, paid), Coins: ChestFares, Fruit: ChestFruitCarry}
	if !expired {
		w.Keys = ChestKeysToDraw(atPost, magic, banked, 0)
	}
	return w
}

// ChestKitSlots is the pack slots the kit takes at its fullest: the trip's keys, one stack per kind
// of rune, the fare and the Strange fruit.
func ChestKitSlots(magic int, paid map[string]bool) int {
	return ChestKeysPerTrip + len(ChestRunes(magic, paid)) + 1 + ChestFruitCarry
}

// ChestKitSlotsWith is ChestKitSlots with the extras the visit carries: a slot each.
func ChestKitSlotsWith(magic int, paid map[string]bool, extras KitWant) int {
	n := ChestKitSlots(magic, paid)
	for _, c := range extras.Extras {
		if c > 0 {
			n++
		}
	}
	return n
}

// BankRow is one row of a bank or one stack of the pack: an object id and how many.
type BankRow struct{ ID, Count int }

// KitDraw is one withdrawal of a visit: the item (a kit name: the key, a rune, Coins, the fruit),
// the bank row's object id and how many.
type KitDraw struct {
	Item  string
	ID    int
	Count int
}

// PlanChestDraws is the withdrawals of a visit, from the bank as it stands with the pack banked:
// each wanted item from the first of its ids the bank holds, at most what the row holds and never
// zero (a count of 0 would be Withdraw-All). Keys, then the runes by name, then the fruit, then the
// coins.
func PlanChestDraws(bank []BankRow, ids KitIDs, want KitWant) []KitDraw {
	have := map[int]int{}
	for _, r := range bank {
		have[r.ID] += r.Count
	}
	var out []KitDraw
	draw := func(item string, candidates []int, n int) {
		if n <= 0 {
			return
		}
		for _, id := range candidates {
			if have[id] > 0 {
				out = append(out, KitDraw{Item: item, ID: id, Count: min(n, have[id])})
				return
			}
		}
	}
	draw(ChestKeyItem, ids.Keys, want.Keys)
	names := make([]string, 0, len(want.Runes))
	for name := range want.Runes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		draw(name, ids.Runes[name], want.Runes[name])
	}
	draw(ChestFruit, ids.Fruit, want.Fruit)
	draw("Coins", ids.Coins, want.Coins)
	for _, x := range ids.Extras {
		if !x.Noted {
			draw(x.Name, x.Bank, want.Extras[x.Name])
		}
	}
	return out
}

// PlanChestNotedDraws is the withdrawals of a visit that are made as notes (one pack slot each, any
// count): the extras marked Noted, from the bank as it stands. ID is the unnoted bank row; the caller
// withdraws it in note mode. The gems of the alchemy (the alchables, ChestAlchable) are drawn only against Nature
// runes: this visit draws some (PlanChestDraws takes them out of the same bank, and the pack was banked first), else
// they stay in the bank (a user request: "if accounts dont have nature runes they shouldnt take things to alch").
func PlanChestNotedDraws(bank []BankRow, ids KitIDs, want KitWant) []KitDraw {
	have := map[int]int{}
	for _, r := range bank {
		have[r.ID] += r.Count
	}
	natures := naturesDrawn(have, ids, want)
	var out []KitDraw
	done := map[string]bool{}
	for _, x := range ids.Extras {
		n := want.Extras[x.Name]
		if !x.Noted || n <= 0 || (x.Group != "" && done[x.Group]) {
			continue
		}
		if ChestAlchable(x.Name) && !natures {
			continue
		}
		for _, id := range x.Bank {
			if have[id] > 0 {
				out = append(out, KitDraw{Item: x.Name, ID: id, Count: min(n, have[id])})
				if x.Group != "" {
					done[x.Group] = true
				}
				break
			}
		}
	}
	return out
}

// naturesDrawn is whether PlanChestDraws takes at least one Nature rune out of the bank (have: its rows by object id)
// for this visit: the kit wants some and a row of the Nature rune extra holds them.
func naturesDrawn(have map[int]int, ids KitIDs, want KitWant) bool {
	if want.Extras[NatureKit] <= 0 {
		return false
	}
	for _, x := range ids.Extras {
		if x.Name != NatureKit || x.Noted {
			continue
		}
		for _, id := range x.Bank {
			if have[id] > 0 {
				return true
			}
		}
	}
	return false
}

// ChestPackHasLoot reports whether the pack holds anything a bank visit would bank: any stack that is
// not a kit item, and any kit item beyond what the kit carries (the chest's coins on top of the fare,
// its runes on top of the spells' count, fruit beyond the trip's). Unnoted keys are the kit; a note
// is not (it is banked to become keys).
func ChestPackHasLoot(pack []BankRow, ids KitIDs, runes map[string]int) bool {
	kind := map[int]string{}
	for _, id := range ids.Keys {
		kind[id] = ChestKeyItem
	}
	for name, list := range ids.Runes {
		for _, id := range list {
			kind[id] = name
		}
	}
	for _, id := range ids.Coins {
		kind[id] = "Coins"
	}
	for _, id := range ids.Fruit {
		kind[id] = ChestFruit
	}
	carry := map[string]int{}
	for _, x := range ids.Extras {
		carry[x.Name] = x.Carry
		for _, id := range x.Pack {
			kind[id] = x.Name
		}
	}
	held := map[string]int{}
	for _, r := range pack {
		item, ok := kind[r.ID]
		if !ok {
			return true
		}
		held[item] += max(1, r.Count)
	}
	for item, n := range held {
		switch item {
		case ChestKeyItem:
		case "Coins":
			if n > ChestFares {
				return true
			}
		case ChestFruit:
			if n > ChestFruitCarry {
				return true
			}
		default:
			if c, extra := carry[item]; extra {
				if n > c {
					return true
				}
			} else if n > runes[item] {
				return true
			}
		}
	}
	return false
}

// ChestRoomWithFruit is ChestRoom if every Strange fruit in the pack were eaten: each bite empties
// the slot it was in (a fruit does not stack).
func ChestRoomWithFruit(free, fruit int, held func(id int) int) bool {
	return ChestRoom(free+max(0, fruit), held)
}

// ChestFruitWanted is the Strange fruit line of the key ask: the thousand (ChestFruitAsk) less what
// this account's bank holds, and no more than the mule publishes; 0 when the mule has none, which never
// stops the round.
func ChestFruitWanted(published, own int) int {
	return max(0, min(published, ChestFruitAsk-max(0, own)))
}

// ---- resuming ----

// ChestOwnKeys is the Crystal keys this account holds: those in its bank (the session's reading when
// it has opened the bank since it logged in, else what the ledger last saw there: a restart knows
// nothing of the bank until a booth opens) and those in its pack, as items or as notes.
func ChestOwnKeys(bankKnown bool, bank, ledger, pack int) int {
	if !bankKnown {
		bank = ledger
	}
	return max(0, bank) + max(0, pack)
}

// ChestResumes reports whether a chest round decided as dec starts its trips straight from where the
// account stands (a user request: "it needs to remember what it was doing last time ... and
// continue that if there's any supplies left for it before just heading to the mule upon startup"):
// the decision was a resume, the account holds keys of its own, and it is on the Falador side, where
// the bank and the chest are. Anywhere else the round goes to the mule's post first, as it always
// did: at the post the ask costs nothing and draws the kit (the ships' fare, the teleports' runes)
// that every long walk needs, and from far away the walk to Falador has none of it.
func ChestResumes(dec Decision, ownKeys int, onFaladorSide bool) bool {
	return dec.Mode == ModeChest && dec.Resume && ownKeys > 0 && onFaladorSide
}

// ChestRoom is the free-slot rule: another open only while the pack has room for the worst reward the
// drop table could give it now, the key's own slot counted free (lootchestfisherplan.
// CanOpenWithKeyInHand).
func ChestRoom(free int, held func(id int) int) bool {
	return lootchestfisherplan.CanOpenWithKeyInHand(free, held)
}
