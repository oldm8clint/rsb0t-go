package consolidateplan

// THE LOOT HAND-OFF (a user request): "we need to implement loot hand off on scarab swarm
// periodically so we can grab all the loot from the different mobs theyve killed, we loot uncut
// gems, bolts, and more. its also a good way to spread out food and runes on each of the
// scarabswarm accounts".
//
// The deposit trip already carried the surplus coins, runes and food (consolidate.go). This file
// adds everything else in the bank that is not the account's WORKING KIT, decided item by item by
// Classify, and the one trip carries both. the mule banks what arrives, and the existing asks push
// the food and runes back out (TopUp's fair share of a stock many accounts draw on), so the
// hand-off pulls surplus in and the asks spread it.
//
// WHAT IS KIT (never handed over), in Classify's order:
//
//   - names on the keep lists (consolidateKeepItems, the training and walk-alch alchables);
//   - any staff (the autocast ladder: Staff, Staff of air, Magic staff, a battlestaff);
//   - armour the Magic half took off (the checkpoint's MagicShed) and what the account wears;
//   - the working tools and Stage 6's kit (KitItems: rope, Shantay pass, waterskins, knife, the
//     Cows rung's Bronze sword ...), and anything raw (a self-cook round's fish);
//   - while the Stage 5 quest ladder is still running, every item a quest kit names (QuestItems):
//     quests acquire their own items, and a hand-off would make them acquire them again;
//   - per melee slot, the best gear-table piece in the bank that is an UPGRADE on what is worn and
//     wearable by the melee goal (one of it; a second copy is loot);
//   - Big bones (then Bones) up to what the Prayer goal still needs;
//   - clue scrolls unless consolidateLootClues (see LootClues).
//
// Untradeable items are never offered (the caller marks them from content's own tradeable=no list).
//
// Everything else is LOOT: uncut and cut gems, bolts and arrows, caskets, jewellery, gear that is
// neither worn nor an upgrade, herbs, seeds, ores, bars, logs, hides, surplus bones, and anything
// else non-kit. It crosses as BANK NOTES, one pack slot a row, so a 60-row bank is three loads.

import (
	"math"
	"sort"
	"strings"

	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/levels"
)

// Kind is what one bank row is to the hand-off.
type Kind string

const (
	KindCoins Kind = "coins"
	KindRune  Kind = "rune"
	KindFood  Kind = "food"
	KindKit   Kind = "kit"
	KindLoot  Kind = "loot"
)

// MuleBankSlots is a bank's size in content 289: interface_bank/configs/bank.constant
// ^bank_total_slots = 288, every row a different obj (bank.inv stackall=yes, and a deposited note
// is banked as its item: bank_deposit_request's inv_moveitem_uncert). A deposit that would need a
// 289th row is refused with "You don't have enough space in your bank account." and the item stays
// in the depositor's pack.
const MuleBankSlots = 288

// KitItems are the tools and kit pieces a Scarab account keeps whatever its quests (matched
// ignoring case; a name ending in "(" matches by prefix, for the waterskin doses).
var KitItems = []string{
	"Shantay pass", "Rope", "Waterskin(", "Knife", "Tinderbox", "Hammer", "Chisel", "Needle",
	"Thread", "Bucket", "Pot", "Jug", "Spade", "Small fishing net", "Lobster pot",
	"Bronze pickaxe", "Iron pickaxe", "Bronze axe", "Iron axe",
	// Stage 5's Cows rung wields it from the bank (scarabrunner.PrepareMelee).
	"Bronze sword",
}

// QuestItems are the items a quest kit in questplan, questmods or waterfallplan names that are not
// already kit, food or runes (read from their KitLine names, 4 Oct 2026). Kept while the Stage 5
// quest ladder is still running.
var QuestItems = []string{
	"Ghostspeak amulet", "Excalibur", "Steel bar", "Iron bar", "Bronze bar", "Silver bar",
	"Silver ore", "Rune essence", "Rat's tail", "Plank", "Nails", "Logs", "Leather gloves",
	"Eye of newt", "Beer", "Asgarnian ale", "Wizard's mind bomb", "Unfired bowl", "Swamp tar",
	"Skull", "Silver sickle", "Silk", "Sickle mould", "Seaweed", "Scorpion cage", "Orb of protection",
	"Onion", "Molten glass", "Mithril sword", "Magnet", "Insect repellent", "Garlic",
	"Criminals' thread", "Clay", "Cheese", "Burnt meat", "Bucket of sand", "Bronze dagger",
	"Bronze arrow", "Bat bones", "Antipoison(3)", "Anti-dragon shield", "Pickaxe", "Axe",
	"Black bead", "Red bead", "White bead", "Yellow bead", "Raw beef", "Raw shark",
	"Maze key", "Map part",
}

// RubKeep is how many rub jewellery pieces (Games necklace, Ring of dueling; scarabplan.RubItems) an
// account keeps, counting every charge. A MasterCrafter's jewellery rounds make far more than that, and
// the rest is loot as ever.
const RubKeep = 2

// Prayer experience a bone pays when buried (content's bury scripts: Big bones 15, Bones 4.5).
const (
	BigBones     = "Big bones"
	Bones        = "Bones"
	bigBonesXP10 = 150 // tenths of an XP point
	bonesXP10    = 45
)

// ClueScroll is the trail clue's name. Every clue step is its own obj with this one name, so a
// mule receiving clues from the cohort would spend a bank row on each step id it ever saw.
const ClueScroll = "Clue scroll"

// KitInput is what Classify needs to know about the account.
type KitInput struct {
	// Keep are names never handed over (consolidateKeepItems plus the alch lists).
	Keep []string
	// Worn is what the account wears now; Shed the armour its Magic half took off (MagicShed).
	Worn, Shed []string
	// QuestsPending is the Stage 5 quest ladder configured and not finished.
	QuestsPending bool
	// Levels are the base levels the gear rule reads; MeleeGoal the level the melee trains to
	// (an upgrade the account cannot wear before its goal is not one); QuestsDone the gear gates.
	Levels     combatplan.Levels
	MeleeGoal  int
	QuestsDone map[string]bool
	// PrayerXP is the account's Prayer experience and PrayerGoal the level it still buries for
	// (0 means no Prayer goal: every bone is loot).
	PrayerXP, PrayerGoal int
	// LootClues hands clue scrolls over too (consolidateLootClues).
	LootClues bool
}

// Classify says what one item NAME is. Bones and gear upgrades are counted, not just named: a
// name Classify calls loot may still be partly kept by PlanLoot (the bones the Prayer goal needs,
// one upgrade per slot).
func Classify(name string, in KitInput) (Kind, string) {
	n := strings.TrimSpace(name)
	l := strings.ToLower(n)
	switch {
	case l == "":
		return KindKit, "unnamed"
	case strings.EqualFold(n, CoinsName):
		return KindCoins, "coins"
	case strings.HasSuffix(l, " rune"):
		return KindRune, "rune"
	case kept(in.Keep, n):
		return KindKit, "on the keep list"
	case strings.Contains(l, "staff"):
		return KindKit, "a staff"
	case kept(in.Worn, n):
		return KindKit, "worn"
	case kept(in.Shed, n):
		return KindKit, "armour the Magic half took off"
	case nameIn(KitItems, n):
		return KindKit, "a tool or kit piece"
	case strings.HasPrefix(l, "raw "):
		return KindKit, "raw food for a cook round"
	case in.QuestsPending && nameIn(QuestItems, n):
		return KindKit, "a quest kit item (the quest ladder is still running)"
	case strings.EqualFold(n, ClueScroll) && !in.LootClues:
		return KindKit, "a clue scroll (consolidateLootClues is off)"
	}
	if _, ok := fighterfood.BaseFood(n); ok {
		return KindFood, "food"
	}
	return KindLoot, "loot"
}

// BankRow is one row of the account's bank (or pack), by obj.
type BankRow struct {
	Name  string
	ID    int
	Count int
	// Value is one item's obj cost (the shop value), for ranking and the minimum.
	Value int
	// Untradeable is content's tradeable=no (donorplan.Untradeable): never offered.
	Untradeable bool
	// Noted rows are skipped (a bank never holds a note; a pack's note is already loot in hand).
	Noted bool
}

// LootLine is one row of loot to hand over.
type LootLine struct {
	Name  string
	ID    int
	Count int
	// Value is the line's total (Count x the row's Value).
	Value int
}

// PlanLoot is the loot in rows: every loot row, less the bones the Prayer goal still needs and
// one gear upgrade per melee slot. kept says why each kit name stays (for the log). The lines are
// ordered by value, dearest first, so the first load carries the most.
func PlanLoot(rows []BankRow, in KitInput) (lines []LootLine, kept map[string]string) {
	kept = map[string]string{}
	upgrade := upgradesIn(rows, in)
	bonesLeft := prayerXPNeeded10(in)
	rubLeft := RubKeep
	// Big bones first: they pay more a slot, and Stage 5 buries those.
	order := append([]BankRow(nil), rows...)
	sort.SliceStable(order, func(i, j int) bool {
		return strings.EqualFold(order[i].Name, BigBones) && !strings.EqualFold(order[j].Name, BigBones)
	})
	for _, r := range order {
		if r.Count <= 0 || r.Noted || strings.TrimSpace(r.Name) == "" {
			continue
		}
		if r.Untradeable {
			kept[r.Name] = "untradeable"
			continue
		}
		k, why := Classify(r.Name, in)
		if k != KindLoot {
			if k == KindKit {
				kept[r.Name] = why
			}
			continue
		}
		n := r.Count
		switch {
		case strings.EqualFold(r.Name, BigBones):
			n, bonesLeft = takeBones(n, bigBonesXP10, bonesLeft)
			if n < r.Count {
				kept[r.Name] = "buried for the Prayer goal"
			}
		case strings.EqualFold(r.Name, Bones):
			n, bonesLeft = takeBones(n, bonesXP10, bonesLeft)
			if n < r.Count {
				kept[r.Name] = "buried for the Prayer goal"
			}
		case fleetplan.IsRubItem(r.Name) && rubLeft > 0:
			// The run staller's jewellery stays in the account's hands (a few of it, across every
			// charge): a Games necklace or Ring of dueling is what it rubs on every walk.
			take := min(n, rubLeft)
			n -= take
			rubLeft -= take
			kept[r.Name] = "the run staller's jewellery"
		case upgrade[strings.ToLower(r.Name)]:
			n--
			kept[r.Name] = "a gear upgrade"
			upgrade[strings.ToLower(r.Name)] = false // one is kept; another copy is loot
		}
		if n > 0 {
			lines = append(lines, LootLine{Name: r.Name, ID: r.ID, Count: n, Value: n * max(0, r.Value)})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Value > lines[j].Value })
	return lines, kept
}

// takeBones keeps as many of n bones (xp10 tenths of XP each) as still cover need10, and reports
// how many are loot and the need left.
func takeBones(n, xp10, need10 int) (loot, left int) {
	if need10 <= 0 {
		return n, 0
	}
	keep := min(n, (need10+xp10-1)/xp10)
	return n - keep, max(0, need10-keep*xp10)
}

// prayerXPNeeded10 is the Prayer XP (in tenths) still between the account and its goal.
func prayerXPNeeded10(in KitInput) int {
	if in.PrayerGoal <= 1 {
		return 0
	}
	return max(0, levels.XPAtLevel(in.PrayerGoal)-max(0, in.PrayerXP)) * 10
}

// upgradesIn is, per melee slot, the best gear-table piece among rows the account will be able
// to wear by its melee goal and that beats what it wears in that slot. Keyed by lower-case name.
func upgradesIn(rows []BankRow, in KitInput) map[string]bool {
	goal := in.MeleeGoal
	if goal <= 0 {
		goal = 40
	}
	reach := combatplan.Levels{
		Attack: max(in.Levels.Attack, goal), Strength: max(in.Levels.Strength, goal),
		Defence: max(in.Levels.Defence, goal), Ranged: in.Levels.Ranged, Magic: in.Levels.Magic,
	}
	// Armour the Magic half took off is still the account's own: an upgrade must beat it too.
	wornOrShed := append(append([]string(nil), in.Worn...), in.Shed...)
	best := map[combatplan.Slot]combatplan.Item{}
	for _, r := range rows {
		if r.Count <= 0 || r.Noted || r.Untradeable {
			continue
		}
		it, ok := combatplan.ItemByName(r.Name)
		if !ok || !meleeSlot(it.Slot) || !combatplan.Wearable(it, reach, in.QuestsDone) {
			continue
		}
		if cur, wearing := combatplan.WornTier(wornOrShed, it.Slot); wearing && cur.Level >= it.Level {
			continue
		}
		if b, seen := best[it.Slot]; seen && b.Level >= it.Level {
			continue
		}
		best[it.Slot] = it
	}
	out := map[string]bool{}
	for _, it := range best {
		out[strings.ToLower(it.Name)] = true
	}
	return out
}

func meleeSlot(s combatplan.Slot) bool {
	for _, m := range combatplan.MeleeSlots {
		if m == s {
			return true
		}
	}
	return false
}

// nameIn matches name against list ignoring case; an entry ending in "(" matches by prefix.
func nameIn(list []string, name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, k := range list {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == n || (strings.HasSuffix(k, "(") && strings.HasPrefix(n, k)) {
			return true
		}
	}
	return false
}

// ---- the whole hand-off ----

// HandOffInput is one hand-off's reading: the surplus half's (Holdings and Keep, as PlanDeposit)
// and the loot half's (the bank rows and the account's kit), plus the mule's published bank.
type HandOffInput struct {
	Holdings Holdings
	Keep     Keep
	Bank     []BankRow
	Kit      KitInput
	// MuleKinds is the mule's published bank, lower-case names; MuleKnown false when it has not
	// published one (then no loot crosses: nothing says there is room for it).
	MuleKinds map[string]bool
	MuleKnown bool
}

// HandOff is one trip: the surplus (Deposit, whose own Walk is not used) and the loot, and
// whether the trip is worth making.
type HandOff struct {
	Deposit Plan
	Loot    []LootLine
	// Kept says why each kit name in the bank stays; Held the loot the mule's bank has no room
	// for this trip (new item types beyond its free rows).
	Kept map[string]string
	Held []string
	// LootValue is the loot's total value.
	LootValue int
	Walk      bool
	Why       string
}

// Line is one withdrawal and one name on the trade screen.
type Line struct {
	Name  string
	ID    int // 0 when drawn by name (coins, runes, food)
	Count int
	Kind  Kind
}

// Lines is everything the trip draws, in the order a load is filled: coins, runes, food, then
// loot dearest first.
func (h HandOff) Lines() []Line {
	var out []Line
	if h.Deposit.Coins > 0 {
		out = append(out, Line{Name: CoinsName, Count: h.Deposit.Coins, Kind: KindCoins})
	}
	for _, n := range sortedKeys(h.Deposit.Runes) {
		out = append(out, Line{Name: n, Count: h.Deposit.Runes[n], Kind: KindRune})
	}
	for _, n := range sortedKeys(h.Deposit.Food) {
		out = append(out, Line{Name: n, Count: h.Deposit.Food[n], Kind: KindFood})
	}
	for _, l := range h.Loot {
		out = append(out, Line{Name: l.Name, ID: l.ID, Count: l.Count, Kind: KindLoot})
	}
	return out
}

// PlanHandOff is the whole trip. The surplus is PlanDeposit's with food uncapped (it crosses as
// notes now, one slot a food); the loot is PlanLoot's, cut to the new item types the mule's bank
// has room for (MuleRoom). The trip is worth making when any one kind clears its own minimum:
// coins, runes or food over theirs, loot worth MinLootValue, or MinLootRows rows of it (bank space
// is worth freeing too). Once it is made, everything goes. With LootOnly (the solo dump) the
// surplus half is empty: only loot crosses, and only loot can justify the trip.
func PlanHandOff(s Settings, in HandOffInput) HandOff {
	s = s.Normalised()
	h := HandOff{Deposit: PlanDeposit(s, in.Holdings, in.Keep, math.MaxInt32), Kept: map[string]string{}}
	if s.LootOnly {
		// The solo dump: no surplus crosses, only loot (Classify already keeps every staff).
		h.Deposit = Plan{Runes: map[string]int{}, Food: map[string]int{}}
	}
	if s.Loot {
		lines, kept := PlanLoot(in.Bank, in.Kit)
		h.Kept = kept
		room := MuleRoom(s, len(in.MuleKinds), in.MuleKnown)
		for _, l := range lines {
			if !in.MuleKinds[strings.ToLower(strings.TrimSpace(l.Name))] {
				if room <= 0 {
					h.Held = append(h.Held, l.Name)
					continue
				}
				room--
			}
			h.Loot = append(h.Loot, l)
			h.LootValue += l.Value
		}
	}
	switch {
	case h.Deposit.Natures > 0:
		h.Walk, h.Why = true, "Nature runes the account cannot cast"
	case h.Deposit.Coins >= s.MinCoins:
		h.Walk, h.Why = true, "coins over the keep"
	case h.Deposit.RuneUnits() >= s.MinRunes:
		h.Walk, h.Why = true, "runes over the keep"
	case h.Deposit.FoodUnits() >= s.MinFood:
		h.Walk, h.Why = true, "food over the keep"
	case s.Loot && len(h.Loot) > 0 && h.LootValue >= s.MinLootValue:
		h.Walk, h.Why = true, "loot worth the trip"
	case s.Loot && len(h.Loot) >= s.MinLootRows:
		h.Walk, h.Why = true, "enough loot rows to free bank space"
	default:
		h.Why = "nothing over the keep is worth the trip"
	}
	return h
}

// MuleRoom is how many NEW item types the mule's bank may take on this trip: its free rows less
// the reserve kept for its own working stock (the asks' food, runes and kits arrive there too).
// An unpublished bank takes none.
func MuleRoom(s Settings, muleRows int, known bool) int {
	if !known {
		return 0
	}
	s = s.Normalised()
	return max(0, s.MuleBankSlots-s.MuleBankReserve-max(0, muleRows))
}

// LoadsFor is how many pack loads the lines take at free slots a load, one slot a line (every
// line is a stack or a note). The trip draws at most MaxLoads of them; the rest wait for the next.
func LoadsFor(lines []Line, free int) int {
	if free <= 0 || len(lines) == 0 {
		return 0
	}
	return (len(lines) + free - 1) / free
}
