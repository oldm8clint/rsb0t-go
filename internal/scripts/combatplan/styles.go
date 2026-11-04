package combatplan

import "strings"

// Which discipline to train next, and whether its supply can actually be bought.
//
// The melee ladder in ladder.go answers "where do I fight". This file answers the two
// questions that come before it once a run is no longer melee-only: which of Attack,
// Strength, Defence, Ranged and Magic the run should be advancing right now, and whether the
// shops a combat-only account can reach can keep that discipline in arrows or runes.
//
// The order is the account owner's, not an optimisation: melee to its goals first, then
// Ranged, then Magic. It is one stat block at a time on purpose, because the rungs are shared
// (the same giants are fought with a sword, a bow or a spell) and switching discipline means
// re-gearing, so interleaving them would pay for the same re-gear many times over.
//
// Both new disciplines have a supply ceiling, and both are stated here rather than discovered
// as a spin in a log. That is the same treatment CheckGearCeiling gives melee's adamant wall,
// and for the same reason: with no mule, a tier the shops do not stock is not a setback that
// waiting fixes.
//
//   - Ranged. Lowe is the only archery shop a combat-only account can reach
//     (content/scripts/areas/area_varrock/configs/varrock.inv, [archeryshop]) and he stocks
//     bows only to oak and arrows only to steel. Everything above that (willow bows, mithril,
//     adamant and rune arrows) is the Ranging Guild, whose door refuses entry below Ranged 40
//     and to which no route has ever been walked with this client. So the honest ceiling is an
//     oak bow and steel arrows, from Ranged 5 all the way to 75.
//   - Magic. Aubury and Betty between them stock air, water, earth, fire, mind, body, chaos
//     and death runes, which covers every spell up to Fire Blast at Magic 59. The four Waves
//     need blood runes, and the only shop in the build that stocks a blood rune is the Magic
//     Guild, behind a Magic 66 door, while Wind Wave unlocks at 62. So 62 to 66 has no rune
//     source at all, and above 66 the route is unwalked. Fire Blast to 75 is the answer, and
//     PlanMagicSpell reaches it by asking which runes are buyable rather than by hardcoding a
//     level.

// Style is which combat discipline a run is training.
type Style string

const (
	StyleMelee  Style = "melee"
	StyleRanged Style = "ranged"
	StyleMagic  Style = "magic"
)

// RangedGoalMet and MagicGoalMet treat a goal of zero or less as "not wanted", so a run
// configured for melee only behaves exactly as it did before these disciplines existed.
func RangedGoalMet(l Levels, g Goals) bool { return g.Ranged <= 0 || l.Ranged >= g.Ranged }

// MagicGoalMet reports whether the Magic goal is reached or was never asked for.
func MagicGoalMet(l Levels, g Goals) bool { return g.Magic <= 0 || l.Magic >= g.Magic }

// StyleToTrain picks the discipline to advance now: melee until all three melee goals are met,
// then Ranged, then Magic. ok is false once every goal is met.
//
// Deliberately not "whichever is furthest behind". StatToTrain does that WITHIN melee, where
// the three stats share one weapon and rotating between them costs nothing. Rotating between
// disciplines costs a full re-gear and a different rune or ammo supply, so the run finishes
// one before starting the next.
func StyleToTrain(l Levels, g Goals) (Style, bool) {
	if !MeleeGoalsMet(l, g) {
		return StyleMelee, true
	}
	if !RangedGoalMet(l, g) {
		return StyleRanged, true
	}
	if !MagicGoalMet(l, g) {
		return StyleMagic, true
	}
	return "", false
}

// AllGoalsMet reports whether every goal this trainer has, including Prayer, is reached.
func AllGoalsMet(l Levels, g Goals, prayerLevel int) bool {
	if !MeleeGoalsMet(l, g) || !RangedGoalMet(l, g) || !MagicGoalMet(l, g) {
		return false
	}
	return g.Prayer <= 0 || prayerLevel >= g.Prayer
}

// TrainedStatOf names the stat a discipline advances, for a log line and for the XP check a
// proof makes. Melee has no single answer, so callers asking about melee use StatToTrain.
func TrainedStatOf(s Style) (Skill, bool) {
	switch s {
	case StyleRanged:
		return SkillRanged, true
	case StyleMagic:
		return SkillMagic, true
	}
	return "", false
}

// ---- Ranged supply ----

// ArrowDropNumerator and ArrowDropDenominator are the engine's own arrow recovery odds, read
// out of content rather than estimated.
//
// skill_combat/scripts/player/player_ranged.rs2's [proc,ranged_dropammo_npc] is
//
//	if (random(^dropammo_chance) ! 0) {
//	    if (map_blocked(npc_coord) = false) {
//	        inv_dropitem_delayed(worn, npc_coord, $ammo, 1, ^lootdrop_duration, $delay);
//	        return;
//	    }
//	}
//	inv_del(worn, $ammo, 1);
//
// with ^dropammo_chance = 5 (skill_combat/configs/combat.constant). random(5) returns 0 to 4,
// so "not zero" is four times in five: 4 arrows in 5 land on the target's tile and can be
// picked back up, and the fifth is destroyed outright. A blocked tile also destroys the arrow.
//
// So a run that recovers every drop spends about one arrow in five, and one that recovers none
// spends all of them. That is the difference between a few hundred arrows lasting a session
// and lasting minutes, which is why the fight loop takes arrow drops the way it takes bones.
const (
	ArrowDropNumerator   = 4
	ArrowDropDenominator = 5
)

// ArrowsPerTrip is how many arrows to carry out of the bank.
//
// Arrows stack, so this costs one pack slot whatever the number. Sized against the recovery
// rate above rather than picked: at one arrow genuinely spent in five, a few hundred is a long
// session, and Lowe keeps 1000 steel and 2000 bronze so a restock is one purchase.
const ArrowsPerTrip = 500

// RangedKit is what a Ranged run wears: a bow and the strongest arrow that bow can fire.
type RangedKit struct {
	Bow   Item
	Arrow Item
	// Arrows is how many to carry per trip.
	Arrows int
}

// PlanRangedKit picks the best bow the account may wear that a reachable shop stocks, then the
// best arrow that bow can actually fire from the shops that same run can reach.
//
// The arrow is chosen against the BOW, not against the account's Ranged level, because that is
// what the server checks: player_ranged_check_ammo compares the two items' own levelrequire
// params at the moment of the shot and answers "Your bow isn't powerful enough for those
// arrows." A level 1 account may wear rune arrows and a level 75 one may not fire them from a
// shortbow.
func PlanRangedKit(l Levels, questsDone map[string]bool, unavailable map[string]bool) (RangedKit, bool) {
	bow, ok := bestReachableBuyable(SlotBow, l, questsDone, unavailable)
	if !ok {
		return RangedKit{}, false
	}
	arrow, ok := bestReachableArrow(bow, unavailable)
	if !ok {
		return RangedKit{}, false
	}
	return RangedKit{Bow: bow, Arrow: arrow, Arrows: ArrowsPerTrip}, true
}

// BestHeldInSlot returns the best item of one slot the account both holds and may wear.
//
// This is the WEAR question, and it is deliberately not the BUY question that BestBuyable and
// PlanRangedKit answer. A run wears the best thing it actually has; it buys the best thing a
// reachable shop stocks. Conflating the two is not hypothetical: the Ranged weapon step asked
// the buy plan what to wear, was told "Oak shortbow" because that is what Lowe sells, and then
// refused to fight at all while a perfectly serviceable Shortbow sat in the pack. Melee never
// had this bug because EquipPlan and BuyPlan were always separate functions.
//
// held is worn and carried names together, since for this question it makes no difference
// whether the item is already on the body.
func BestHeldInSlot(slot Slot, l Levels, questsDone map[string]bool, held []string) (Item, bool) {
	best, found := Item{}, false
	for _, name := range held {
		it, ok := ItemByName(name)
		if !ok || it.Slot != slot || !Wearable(it, l, questsDone) {
			continue
		}
		if !found || it.Level > best.Level {
			best, found = it, true
		}
	}
	return best, found
}

// BestHeldArrowForBow is the strongest arrow the account holds that this bow can actually fire.
//
// The bow is the constraint, not the account's Ranged level: player_ranged_check_ammo compares
// the two items' own levelrequire params at the moment of the shot.
func BestHeldArrowForBow(bow Item, held []string) (Item, bool) {
	best, found := Item{}, false
	for _, name := range held {
		it, ok := ItemByName(name)
		if !ok || it.Slot != SlotAmmo || !ArrowFitsBow(it, bow) {
			continue
		}
		if !found || it.AmmoTier > best.AmmoTier {
			best, found = it, true
		}
	}
	return best, found
}

// BestReachableArrowForBow is the strongest arrow a shop this trainer can enter will sell for
// this bow. Used when the kit's own arrow is too strong for the bow actually being carried.
func BestReachableArrowForBow(bow Item, unavailable map[string]bool) (Item, bool) {
	return bestReachableArrow(bow, unavailable)
}

// bestReachableArrow is BestArrowForBow restricted to shops this trainer can actually enter and
// to arrows a run has not already found missing from the shelf.
func bestReachableArrow(bow Item, unavailable map[string]bool) (Item, bool) {
	best, found := Item{}, false
	for _, it := range Items() {
		if it.Slot != SlotAmmo || it.Shop == "" || !ArrowFitsBow(it, bow) {
			continue
		}
		if unavailable[strings.ToLower(it.Name)] {
			continue
		}
		site, ok := ShopSiteFor(it)
		if !ok || !site.Reachable() {
			continue
		}
		if !found || it.AmmoTier > best.AmmoTier {
			best, found = it, true
		}
	}
	return best, found
}

// CheckRangedCeiling reports whether the account has outgrown what the archery shops can sell
// it, the Ranged counterpart of CheckGearCeiling.
//
// Stalled is true once the account could wear a better bow than any reachable shop stocks. The
// run does not stop: an oak bow and steel arrows still train Ranged to 75, just slowly. It says
// so once so nobody reads a ceiling as a stuck run.
func CheckRangedCeiling(l Levels, questsDone map[string]bool) GearCeiling {
	c := GearCeiling{}
	bow, haveBow := bestReachableBuyable(SlotBow, l, questsDone, nil)
	if haveBow {
		c.BestWeaponTier = bow.Tier
	}
	if arrow, ok := bestReachableArrow(bow, nil); haveBow && ok {
		c.BestArmourTier = arrow.Tier
	}
	best, anyWearable := BestWearable(SlotBow, l, questsDone)
	if anyWearable && haveBow && best.Level > bow.Level {
		c.Stalled = true
		c.Reason = "Ranged has reached " + itoa(l.Ranged) + ", which allows a " + best.Name +
			", but the only archery shop this run can reach is Lowe's, and he stocks bows no better than oak and arrows no better than steel. " +
			"Willow bows and mithril, adamant and rune arrows are the Ranging Guild's, behind a Ranged 40 door on a route nobody has walked with this client. " +
			"Without the mule item path this run stays on " + bow.Name + " and " + c.BestArmourTier + " arrows to its goal"
	}
	return c
}

// ---- Magic supply ----

// RuneCost is one rune a spell consumes per cast.
type RuneCost struct {
	Name  string
	Count int
}

// SpellOption is one autocastable spell, in the terms this pure package can reason about.
//
// It deliberately mirrors internal/actions.Spell rather than importing it: this package takes
// no dependency on actions, so the caller converts. That keeps the supply decision below
// unit-testable without a session and without duplicating content's spell table, which stays
// where the clicks are.
type SpellOption struct {
	Name    string
	Level   int
	Members bool
	Runes   []RuneCost
}

// shopRunes are the runes Aubury (varrock.inv [runeshop]) and Betty (port_sarim.inv
// [magicshop]) actually stock. Both shelves are the same eight lines, all with a non-zero
// stock count, so either keeper can supply any of them.
//
// What is NOT here is the point: blood, law, nature and soul runes are stocked only by the
// Magic Guild, behind a door that refuses entry below Magic 66.
var shopRunes = map[string]bool{
	"air rune":   true,
	"water rune": true,
	"earth rune": true,
	"fire rune":  true,
	"mind rune":  true,
	"body rune":  true,
	"chaos rune": true,
	"death rune": true,
}

// ShopRunes returns the runes a reachable shop stocks, keyed lower-case.
func ShopRunes() map[string]bool {
	out := make(map[string]bool, len(shopRunes))
	for k, v := range shopRunes {
		out[k] = v
	}
	return out
}

// ShopRunesExcept is ShopRunes with the lines a run has WATCHED come up empty struck off,
// keyed lower-case the way combattrainer's own unavailable map is.
//
// The map above is a claim about the world, and the world is allowed to disagree. It asserts
// "death rune": true forever, which is true of the .inv line and not true of the shelf: a
// drained Aubury sells nothing. Before this the Magic path had no way to hear that. The run
// recorded the empty shelf in unavailable, then asked a function that could not read it, so it
// kept choosing Fire Blast, kept being refused the autocast at SET time (~check_spell_
// requirements runs before storing, not at cast time), kept returning from Loop without
// fighting, and retried on the 10-minute shop cadence forever. That is the sixth zero-progress
// loop this script has grown out of a pair of individually reasonable rules.
//
// Striking the line for the rest of the run rather than retrying it is content's own
// arithmetic and not impatience. varrock.inv's [runeshop] is stock8=deathrune,1000,150, and
// World.ts tops a drained line up by ONE unit every stockrate ticks, so a death rune returns
// at one per 90 seconds: 75 minutes to reach the 50-cast low-water mark a restock triggers on,
// and ten hours to refill the 400 casts a trip buys. A run cannot outwait that, and Fire Bolt
// on chaos runes trains Magic the whole time it would have been standing still.
func ShopRunesExcept(except map[string]bool) map[string]bool {
	out := ShopRunes()
	for name, struck := range except {
		if struck {
			delete(out, strings.ToLower(name))
		}
	}
	return out
}

// struckRunesOf names the runes a spell needs that the shelf DOES list but this run has struck,
// joined for a log line. It is empty when the spell's gap is the shelf's own rather than the
// run's own observation, which is what tells the two ceilings apart.
func struckRunesOf(sp SpellOption, staffRune string, except map[string]bool) string {
	var out []string
	for _, r := range sp.Runes {
		if strings.EqualFold(r.Name, staffRune) {
			continue
		}
		lower := strings.ToLower(r.Name)
		if shopRunes[lower] && except[lower] {
			out = append(out, lower)
		}
	}
	return strings.Join(out, " and ")
}

// ElementOf returns a combat spell's element, which is the first word of its name, and the
// elemental staff and rune that go with it. ok is false for a name that is not one of the four.
func ElementOf(spellName string) (element, staff, runeName string, ok bool) {
	fields := strings.Fields(spellName)
	if len(fields) == 0 {
		return "", "", "", false
	}
	switch strings.ToLower(fields[0]) {
	case "wind":
		return "Wind", "Staff of air", "Air rune", true
	case "water":
		return "Water", "Staff of water", "Water rune", true
	case "earth":
		return "Earth", "Staff of earth", "Earth rune", true
	case "fire":
		return "Fire", "Staff of fire", "Fire rune", true
	}
	return "", "", "", false
}

// SpellSupplied reports whether every rune a spell needs can actually be bought, given the rune
// the wielded staff supplies for free.
//
// staffRune is the rune a matching elemental staff replaces (magic_staff.dbrow maps each staff
// to one rune, and [proc,staff_runes] substitutes it), passed as a display name or empty for no
// staff. Its count drops out of the shopping list entirely, which is what makes Fire Blast cost
// a death rune and four air runes rather than a death rune, five fire runes and four air runes.
func SpellSupplied(sp SpellOption, staffRune string, buyable map[string]bool) bool {
	for _, r := range sp.Runes {
		if strings.EqualFold(r.Name, staffRune) {
			continue
		}
		if !buyable[strings.ToLower(r.Name)] {
			return false
		}
	}
	return true
}

// PlanMagicSpell picks the highest-level spell the account can cast whose runes a reachable
// shop actually stocks.
//
// The supply test is what keeps this honest. Asking only "the highest spell at this level"
// would choose Wind Wave the moment the account hits Magic 62 on a members world, and Wind Wave
// needs a blood rune, which has exactly one source in the whole build and that source is behind
// a Magic 66 door. The run would then autocast a spell it cannot restock, discover it at the
// first empty pack, and have nowhere to go. Asking "the highest spell I can keep casting"
// answers Fire Blast, which is buyable forever and trains to 75.
//
// members gates the four Waves the same way content does. staffRune is the rune the wielded
// staff supplies free, or empty.
func PlanMagicSpell(magicLevel int, members bool, options []SpellOption, staffRune string, buyable map[string]bool) (SpellOption, bool) {
	best, found := SpellOption{}, false
	for _, sp := range options {
		if sp.Level > magicLevel {
			continue
		}
		if sp.Members && !members {
			continue
		}
		if !SpellSupplied(sp, staffRune, buyable) {
			continue
		}
		if !found || sp.Level > best.Level {
			best, found = sp, true
		}
	}
	return best, found
}

// RunesPerTrip is how many casts' worth of runes to carry, and so how many of each rune to buy.
//
// Runes stack, so like arrows this is one pack slot per kind whatever the number. Aubury keeps
// 1000 death runes and 2000 of each element, so a trip this size is a single purchase per rune.
const RunesPerTrip = 500

// RuneShoppingList is how many of each rune a trip needs for a spell, with the staff's own rune
// already removed. The counts are per cast multiplied by casts.
func RuneShoppingList(sp SpellOption, staffRune string, casts int) []RuneCost {
	if casts < 1 {
		casts = 1
	}
	var out []RuneCost
	for _, r := range sp.Runes {
		if strings.EqualFold(r.Name, staffRune) {
			continue
		}
		out = append(out, RuneCost{Name: r.Name, Count: r.Count * casts})
	}
	return out
}

// CheckMagicCeiling reports whether the account has outgrown the spells its runes can supply.
//
// Stalled is true once a higher-level spell exists that the account could cast but cannot keep
// in runes. As with the other two ceilings the run carries on, because Fire Blast trains Magic
// to 75 perfectly well; the line exists so a reader knows why the spell stopped improving.
func CheckMagicCeiling(magicLevel int, members bool, options []SpellOption, staffRune string) GearCeiling {
	return CheckMagicCeilingExcept(magicLevel, members, options, staffRune, nil)
}

// CheckMagicCeilingExcept is CheckMagicCeiling with the shelves this run has watched come up
// empty struck off, so the announced reason names the cause that actually applies.
//
// The distinction is the whole reason this exists. A ceiling caused by the static shelf is
// permanent and geographic — the blood rune is behind the Magic Guild's Magic 66 door. A
// ceiling caused by an empty shelf is this run's own observation, and saying "the Magic Guild
// refuses you" when the truth is "Aubury sold out of death runes" would send whoever reads the
// log to the wrong place entirely.
func CheckMagicCeilingExcept(magicLevel int, members bool, options []SpellOption, staffRune string, except map[string]bool) GearCeiling {
	c := GearCeiling{}
	buyable := ShopRunesExcept(except)
	supplied, haveSupplied := PlanMagicSpell(magicLevel, members, options, staffRune, buyable)
	if haveSupplied {
		c.BestWeaponTier = supplied.Name
	}
	// The best spell ignoring supply entirely, which is what the account's level alone allows.
	best, haveBest := SpellOption{}, false
	for _, sp := range options {
		if sp.Level > magicLevel || (sp.Members && !members) {
			continue
		}
		if !haveBest || sp.Level > best.Level {
			best, haveBest = sp, true
		}
	}
	if haveBest {
		c.BestArmourTier = best.Name
	}
	if haveBest && haveSupplied && best.Level > supplied.Level {
		c.Stalled = true
		if struck := struckRunesOf(best, staffRune, except); struck != "" {
			c.Reason = "Magic has reached " + itoa(magicLevel) + ", which allows " + best.Name +
				", but this run found the shop out of " + struck + " rather than unable to sell it. " +
				"Waiting is not the answer: content tops a drained line up by one unit every stockrate ticks, which for a death rune is one every 90 seconds. " +
				"Dropping to " + supplied.Name + ", which is slower and still trains Magic"
		} else {
			c.Reason = "Magic has reached " + itoa(magicLevel) + ", which allows " + best.Name +
				", but its runes cannot be bought anywhere this run can reach: the only shop in content 289 that stocks a blood rune is the Magic Guild's, and its door refuses entry below Magic 66 while " + best.Name + " unlocks at " + itoa(best.Level) + ". " +
				"Without the mule item path this run keeps autocasting " + supplied.Name + ", which is buyable from Aubury and Betty forever and trains Magic to 75"
		}
	}
	return c
}
