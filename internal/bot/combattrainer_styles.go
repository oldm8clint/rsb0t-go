package bot

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
)

// The Ranged and Magic half of CombatTrainer: which level the ladder bands on, what the run has
// to be holding, and how it keeps itself in arrows and runes.
//
// Everything here exists because the trainer used to be melee-only, and melee is the one
// discipline where the weapon, the supply and the stat all look after themselves. A sword never
// runs out. Ranged and Magic both spend something every hit, and both can be "training" in a way
// that quietly pays the wrong stat: a bow that never got equipped leaves the account punching,
// and a staff that is not autocasting is just a blunt instrument. So each of the two has a
// supply step that runs before the fight and a weapon step that runs after the walk.
//
// The ordering matters and is the non-obvious part. Supply happens early in Loop, because runes
// have to be in the pack before the server will accept the autocast at all
// (~check_spell_requirements runs at SET time, not only at cast time). The weapon step happens
// after travel, because the route can demand a different weapon than the fight does.

const (
	// arrowLowWater is how few arrows may remain before the run goes shopping. Counted across
	// the quiver and the pack together, because arrows are worn: once a run is firing, a count
	// that asked only the pack would read zero forever and restock on every pass.
	arrowLowWater = 60
	// runeLowWaterCasts is how few casts' worth of a rune may remain before a restock, and
	// runeBuyCasts is how many casts' worth to buy. Aubury keeps 1000 death runes and 2000 of
	// each element, so a trip this size is one purchase per rune rather than a queue of them.
	runeLowWaterCasts = 50
	runeBuyCasts      = 400
	// supplyAttempts is how many failed trips one supply item gets before the run stops trying,
	// matching gearAttemptsPerSlot's reasoning: the failures worth retrying are transient, and
	// the ones that are not are permanent for the whole run.
	supplyAttempts = 3
)

// wearsMeleeWeapon reports whether the discipline being trained is the one that actually swings
// the gear table's weapon slot. An empty style is treated as melee, which is what a run reads as
// before its first discipline choice has been made.
func wearsMeleeWeapon(style combatplan.Style) bool {
	return style == combatplan.StyleMelee || style == ""
}

// bandLevel is the level the ladder bands on for the discipline being trained.
//
// For melee it is the lowest of the three melee stats, which is what it has always been. For
// Ranged and Magic it is that stat's own level, and that substitution is what makes the ladder
// usable by them at all: a run that has finished melee is 75/75/75, so banding a Ranged 1 run on
// the lowest melee stat would ask the ladder for a rung covering level 75, find none, and stop
// the run with its Ranged goal untouched.
func (b *combatTrainerBot) bandLevel(l combatplan.Levels, style combatplan.Style) int {
	level := 1
	switch style {
	case combatplan.StyleRanged:
		level = l.Ranged
	case combatplan.StyleMagic:
		level = l.Magic
	default:
		_, lowest := combatplan.LowestMelee(l)
		level = lowest
	}
	if level < 1 {
		return 1
	}
	return level
}

// describeTraining names what the run is advancing, for the rung log line.
func (b *combatTrainerBot) describeTraining(l combatplan.Levels, style combatplan.Style) string {
	if stat, ok := combatplan.TrainedStatOf(style); ok {
		return string(stat)
	}
	stat, _ := combatplan.BalancedStatToTrain(l, b.cfg.goals)
	return string(stat)
}

// announceCeilings says once, per discipline, that the shops cannot supply anything better.
//
// All three ceilings are real and none of them stops the run: adamant still trains melee to 75,
// an oak bow and steel arrows still train Ranged, and Fire Blast still trains Magic. The line
// exists so nobody reads a permanent shop limit as a stuck script, which is exactly how the
// melee one was read before CheckGearCeiling started saying it out loud.
func (b *combatTrainerBot) announceCeilings(ctx *Context, l combatplan.Levels, style combatplan.Style) {
	switch style {
	case combatplan.StyleRanged:
		if c := combatplan.CheckRangedCeiling(l, nil); c.Stalled {
			b.sayOnce(ctx, "ceiling-ranged", "RANGED CEILING: %s", c.Reason)
		}
	case combatplan.StyleMagic:
		_, _, staffRune, _ := combatplan.ElementOf(b.cfg.magicElement + " Strike")
		if c := combatplan.CheckMagicCeilingExcept(l.Magic, b.cfg.members, combatrunner.SpellOptions(), staffRune, b.unavailable); c.Stalled {
			b.sayOnce(ctx, "ceiling-magic", "MAGIC CEILING: %s", c.Reason)
		}
	default:
		if c := combatplan.CheckGearCeiling(l, nil); c.Stalled {
			b.sayOnce(ctx, "ceiling", "GEAR CEILING: %s", c.Reason)
		}
	}
}

// wearBladeForTravel puts a bladed melee weapon on for a route that needs one, out of whatever
// the pack is carrying. It reports whether a blade ended up in the right hand.
//
// A Ranged or Magic run reaches this holding a bow or a staff, because the sewer web is cut from
// the right-hand slot only and neither of those cuts anything. The swap back happens in
// pointWeapon once the account has arrived.
func (b *combatTrainerBot) wearBladeForTravel(ctx *Context, d *combatrunner.Deps) bool {
	for _, name := range combatrunner.PackNames(d) {
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "sword") && !strings.Contains(lower, "scimitar") && !strings.Contains(lower, "dagger") {
			continue
		}
		if combatrunner.EquipByName(d, name, 5*time.Second) {
			b.sayOnce(ctx, "blade-travel", "wearing the %s for the walk to rung %q, because the sewer web is cut from the right hand and a bow or staff will not cut it. Swapping back on arrival", name, b.rung.Key)
			return true
		}
	}
	return combatrunner.WieldingBlade(d)
}

// pointWeapon puts the right weapon in the right hand and points it at the right stat, and
// reports whether the run is fit to fight.
//
// Answering false is deliberate and is the control that stops a silent wrong-stat run: a Ranged
// run with no bow on the body would otherwise punch the monster and earn Attack, which looks
// like training right up until somebody reads the stat table. Loop returns on a false without
// fighting, so the next pass re-runs the supply step that can fix it.
func (b *combatTrainerBot) pointWeapon(ctx *Context, d *combatrunner.Deps, l combatplan.Levels, style combatplan.Style) bool {
	switch style {
	case combatplan.StyleRanged:
		return b.pointBow(ctx, d, l)
	case combatplan.StyleMagic:
		return b.pointStaff(ctx, d, l)
	}

	// Melee, unchanged: the server advances whichever style the weapon is set to, so a run that
	// decided to train Strength and never touched the style would train Attack to 75 instead.
	if stat, ok := combatplan.BalancedStatToTrain(l, b.cfg.goals); ok {
		if !combatrunner.EnsureTrainingStyle(d, stat) {
			b.warn(ctx, "style", time.Minute, "could not set the combat style to %s — the fight would train the wrong stat", stat)
		} else if b.trainingStat != stat {
			b.trainingStat = stat
			ctx.logf("combattrainer: now training %s (Strength 5 over Attack, Attack 5 over Defence)", stat)
		}
	}
	return true
}

// pointBow wears the best bow the account actually HOLDS, nocks the best arrow that bow can
// fire, and selects Rapid.
//
// Best held, not best buyable. Asking PlanRangedKit what to wear is what made the first live
// Ranged run stand in the cow field refusing to fight: it held a Shortbow, Lowe sells an Oak
// shortbow, so the weapon step demanded a bow the account had never owned and warned every pass
// instead of shooting. Buying the upgrade is rangedSupplyStep's job and happens separately.
func (b *combatTrainerBot) pointBow(ctx *Context, d *combatrunner.Deps, l combatplan.Levels) bool {
	held := append(combatrunner.WornNames(d), combatrunner.PackNames(d)...)
	bow, ok := combatplan.BestHeldInSlot(combatplan.SlotBow, l, nil, held)
	if !ok {
		b.warn(ctx, "bow-none", time.Minute, "no bow worn or carried, so there is nothing to fire — not fighting, because a missing bow means punching the monster and earning Attack instead of Ranged")
		return false
	}
	if !combatrunner.WieldingBow(d) && !combatrunner.EquipByName(d, bow.Name, 5*time.Second) {
		b.warn(ctx, "bow-wear", time.Minute, "carrying a %s but it would not go on the body — not fighting, because that would pay Attack rather than Ranged", bow.Name)
		return false
	}

	arrow, haveArrow := combatplan.BestHeldArrowForBow(bow, held)
	if !haveArrow {
		b.warn(ctx, "arrow-none", time.Minute, "a %s is worn but no arrow it can fire is nocked or carried — not fighting without ammunition", bow.Name)
		return false
	}
	b.arrowName = arrow.Name
	// Arrows live in the ammunition slot, so equipping is what loads them. Already-nocked arrows
	// read through WornCount, which is why this does not re-equip every pass.
	if combatrunner.WornCount(d, arrow.Name) == 0 {
		combatrunner.EquipByName(d, arrow.Name, 5*time.Second)
		if combatrunner.WornCount(d, arrow.Name) == 0 {
			b.warn(ctx, "arrow-wear", time.Minute, "carrying %s but they would not nock into the ammunition slot", strings.ToLower(arrow.Name))
			return false
		}
	}
	if !combatrunner.EnsureRangedStyle(d) {
		b.warn(ctx, "ranged-style", time.Minute, "could not select the bow's Rapid style; the shots still pay Ranged, but more slowly than they should")
	}
	if b.trainingStat != combatplan.SkillRanged {
		b.trainingStat = combatplan.SkillRanged
		ctx.logf("combattrainer: now training ranged with a %s and %s", bow.Name, strings.ToLower(arrow.Name))
	}
	return true
}

// pointStaff wears a staff and makes sure the chosen spell is actually autocasting.
//
// Any staff will do, and preferring the one that matches the spell is an economy rather than a
// requirement: a staff supplies its own element's rune free, so a Fire Blast caster holding a
// staff of fire pays a death rune and four air runes instead of also paying five fire runes.
// Wearing whatever staff is actually held, rather than insisting on the matching one, is the
// same correction pointBow needed — the run should use what it has and buy the upgrade
// separately.
func (b *combatTrainerBot) pointStaff(ctx *Context, d *combatrunner.Deps, l combatplan.Levels) bool {
	spell, staffName, _, ok := b.chooseSpell(l)
	if !ok {
		b.warn(ctx, "spell-none", time.Minute, "no spell this run can both cast and keep in runes, so Magic cannot be trained")
		return false
	}
	if !combatrunner.WieldingStaff(d) {
		held := append(combatrunner.WornNames(d), combatrunner.PackNames(d)...)
		wear := staffName
		if combatrunner.HeldCount(d, staffName) == 0 {
			if any, found := combatplan.BestHeldInSlot(combatplan.SlotStaff, l, nil, held); found {
				wear = any.Name
			}
		}
		if !combatrunner.EquipByName(d, wear, 5*time.Second) {
			b.warn(ctx, "staff-wear", time.Minute, "no staff on the body and none in the pack to put on — not fighting, because without a staff the autocast buttons are not even on screen")
			return false
		}
	}
	// EnsureAutocast is handed this run's own memory of what it set, because there is nothing to
	// read: content declares autocast_spell (varp 59) without transmit=yes, so no client can see
	// WHICH spell is active, only that one is. If the server says autocasting stopped, which is
	// what an empty rune pack looks like, the sequence runs again.
	if err := combatrunner.EnsureAutocast(d, spell, b.autocastSpell, 12*time.Second); err != nil {
		b.autocastSpell = ""
		b.warn(ctx, "autocast", time.Minute, "could not set %s to autocast (%v) — not fighting, because a staff that is not casting is swung as a club and pays Attack", spell.Name, err)
		return false
	}
	if b.autocastSpell != spell.Name {
		b.autocastSpell = spell.Name
		ctx.logf("combattrainer: now training magic, autocasting %s from a %s", spell.Name, strings.ToLower(staffName))
	}
	if b.trainingStat != combatplan.SkillMagic {
		b.trainingStat = combatplan.SkillMagic
	}
	return true
}

// chooseSpell picks the spell to autocast, and the staff and free rune that go with it.
//
// The choice prefers the configured element and falls back to any element, which matters at the
// bottom of the ladder: a run configured for Fire has nothing to cast until Magic 13, because
// Wind Strike is the only spell a level 1 account knows. Preferring an element it cannot use yet
// would leave it casting nothing at all.
func (b *combatTrainerBot) chooseSpell(l combatplan.Levels) (actions.Spell, string, string, bool) {
	options := combatrunner.SpellOptions()
	// The shelves this run has WATCHED come up empty are struck here, and that is what stops the
	// sixth zero-progress loop. Asking the static map instead kept answering Fire Blast after
	// Aubury had run out of death runes; the server then refused the autocast at SET time,
	// pointStaff returned false, and Loop returned without fighting on every pass for the rest
	// of the session. Striking the line degrades the run to Fire Bolt, which still trains Magic.
	buyable := combatplan.ShopRunesExcept(b.unavailable)

	// staffRune is deliberately empty for the choice itself. Passing a staff's rune would let a
	// spell qualify on the strength of a staff we may not own yet, and every spell up to Fire
	// Blast is fully buyable without one anyway; the staff is an economy, not an enabler.
	pick, ok := combatplan.PlanMagicSpell(l.Magic, b.cfg.members, spellsOfElement(options, b.cfg.magicElement), "", buyable)
	if !ok {
		pick, ok = combatplan.PlanMagicSpell(l.Magic, b.cfg.members, options, "", buyable)
	}
	if !ok {
		return actions.Spell{}, "", "", false
	}
	_, staffName, staffRune, _ := combatplan.ElementOf(pick.Name)
	spell, found := actions.SpellByName(pick.Name)
	if !found {
		return actions.Spell{}, "", "", false
	}
	return spell, staffName, staffRune, true
}

// spellsOfElement keeps only the spells of one element, by the first word of the name.
func spellsOfElement(options []combatplan.SpellOption, element string) []combatplan.SpellOption {
	if strings.TrimSpace(element) == "" {
		return options
	}
	var out []combatplan.SpellOption
	for _, sp := range options {
		if name, _, _, ok := combatplan.ElementOf(sp.Name); ok && strings.EqualFold(name, element) {
			out = append(out, sp)
		}
	}
	return out
}

// ---- supply ----

// supplyStep buys the consumable the chosen discipline runs on. It reports done=true when it
// acted, so Loop returns and re-derives rather than fighting on a stale reading.
func (b *combatTrainerBot) supplyStep(ctx *Context, d *combatrunner.Deps, l combatplan.Levels, style combatplan.Style) (bool, error) {
	switch style {
	case combatplan.StyleRanged:
		return b.rangedSupplyStep(ctx, d, l)
	case combatplan.StyleMagic:
		return b.magicSupplyStep(ctx, d, l)
	}
	return false, nil
}

// rangedSupplyStep keeps a bow on the body and arrows in the quiver.
func (b *combatTrainerBot) rangedSupplyStep(ctx *Context, d *combatrunner.Deps, l combatplan.Levels) (bool, error) {
	kit, ok := combatplan.PlanRangedKit(l, nil, b.unavailable)
	if !ok {
		b.sayOnce(ctx, "ranged-supply", "no bow and arrow pair this run can buy: Lowe is the only archery shop it can reach and nothing on his shelf is usable")
		return false, nil
	}
	held := append(combatrunner.WornNames(d), combatrunner.PackNames(d)...)
	bow, haveBow := combatplan.BestHeldInSlot(combatplan.SlotBow, l, nil, held)

	// Buy a bow when there is none to fire, or when the shop genuinely sells a better one than
	// the account is carrying. "Better" is the wear requirement, which for bows is also the
	// levelrequire param the ammo check compares, so an upgrade widens what it can shoot too.
	if !haveBow || kit.Bow.Level > bow.Level {
		if !b.mayShop() {
			return false, nil
		}
		return b.buySupplyItem(ctx, d, kit.Bow, 1)
	}

	// The kit's arrow is chosen for the kit's bow, which may be a tier above the one actually
	// being carried. Buying it then would put arrows in the pack that the server refuses to
	// fire: "Your bow isn't powerful enough for those arrows."
	want := kit.Arrow
	if !combatplan.ArrowFitsBow(want, bow) {
		alt, ok := combatplan.BestReachableArrowForBow(bow, b.unavailable)
		if !ok {
			return false, nil
		}
		want = alt
	}
	b.arrowName = want.Name
	if arrow, have := combatplan.BestHeldArrowForBow(bow, held); have {
		b.arrowName = arrow.Name
		if combatrunner.ArrowsAvailable(d, arrow.Name) >= arrowLowWater {
			return false, nil
		}
	}
	if !b.mayShop() {
		return false, nil
	}
	return b.buySupplyItem(ctx, d, want, kit.Arrows)
}

// magicSupplyStep keeps a staff on the body and the spell's runes in the pack.
func (b *combatTrainerBot) magicSupplyStep(ctx *Context, d *combatrunner.Deps, l combatplan.Levels) (bool, error) {
	spell, staffName, _, ok := b.chooseSpell(l)
	if !ok {
		b.sayOnce(ctx, "magic-supply", "no spell this run can both cast at Magic %d and keep in runes from Aubury's shelf", l.Magic)
		return false, nil
	}

	// The staff before the runes, because the staff is what removes one of them from the list.
	// Only buy when there is no staff at all: a mismatched staff still casts, it just does not
	// pay for the element rune, and a cross-map trip to Zaff costs the run a kill under the
	// shopping rule.
	if !combatrunner.WieldingStaff(d) && !b.holdsAnyStaff(d, l) {
		if !b.mayShop() {
			return false, nil
		}
		if staff, known := combatplan.ItemByName(staffName); known {
			return b.buySupplyItem(ctx, d, staff, 1)
		}
	}

	// Which rune is free depends on the staff actually WORN, not on the one the spell would
	// prefer. Reading it off the body is what keeps the shopping list honest when the account is
	// casting Fire Blast with a staff of air in its hand.
	held := combatplan.StaffRune(b.wornStaffName(d))
	for _, want := range combatplan.RuneShoppingList(combatplan.SpellOption{Name: spell.Name, Runes: runeCostsOf(spell)}, held, runeLowWaterCasts) {
		if combatrunner.HeldCount(d, want.Name) >= want.Count {
			continue
		}
		if !b.mayShop() {
			return false, nil
		}
		site, known := combatplan.ShopSiteByShop(combatplan.ShopVarrockRune)
		if !known {
			return false, nil
		}
		perCast := want.Count / runeLowWaterCasts
		if perCast < 1 {
			perCast = 1
		}
		return b.buySupply(ctx, d, site, want.Name, perCast*runeBuyCasts)
	}
	return false, nil
}

// holdsAnyStaff reports whether a staff is worn or carried, whatever its element.
//
// Whatever its element is the point: a mismatched staff still casts every spell, it simply does
// not pay for that spell's own element rune. So a run holding a staff of air while casting Fire
// Blast is fine and should buy fire runes, not another staff.
func (b *combatTrainerBot) holdsAnyStaff(d *combatrunner.Deps, l combatplan.Levels) bool {
	held := append(combatrunner.WornNames(d), combatrunner.PackNames(d)...)
	_, ok := combatplan.BestHeldInSlot(combatplan.SlotStaff, l, nil, held)
	return ok
}

// wornStaffName is the elemental staff currently on the body, empty when none is.
//
// StaffRune is the test rather than a name match, because the question being asked is exactly
// "which rune does the thing in our hand supply free", and only the four elemental staffs supply
// any. A plainstaff or a magic staff answers empty here, which is correct: they substitute
// nothing and every rune has to be bought.
func (b *combatTrainerBot) wornStaffName(d *combatrunner.Deps) string {
	for _, name := range combatrunner.WornNames(d) {
		if combatplan.StaffRune(name) != "" {
			return name
		}
	}
	return ""
}

// runeCostsOf converts an actions.Spell's rune list into the pure package's shape.
func runeCostsOf(s actions.Spell) []combatplan.RuneCost {
	out := make([]combatplan.RuneCost, 0, len(s.Runes))
	for _, r := range s.Runes {
		out = append(out, combatplan.RuneCost{Name: r.Name, Count: r.Count})
	}
	return out
}

// buySupplyItem buys a gear-table item from whichever shop stocks it.
func (b *combatTrainerBot) buySupplyItem(ctx *Context, d *combatrunner.Deps, it combatplan.Item, qty int) (bool, error) {
	site, ok := combatplan.ShopSiteFor(it)
	if !ok {
		return false, nil
	}
	return b.buySupply(ctx, d, site, it.Name, qty)
}

// buySupply makes one supply trip and reports what the server actually did.
//
// It books the trip against mayShop the same way gearStep does, because a supply trip costs the
// run exactly what a gear trip costs it: a cross-map walk that is not fighting. Without that the
// shopping rule that stopped the gear loop starving the run would simply be re-opened here.
func (b *combatTrainerBot) buySupply(ctx *Context, d *combatrunner.Deps, site combatplan.ShopSite, name string, qty int) (bool, error) {
	if b.supplyFails[strings.ToLower(name)] >= supplyAttempts {
		return false, nil
	}
	// A shelf already found empty is not walked to again. The spell and kit planners both strike
	// these before choosing, so reaching here means something asked anyway — and a cross-map trip
	// that cannot succeed costs the run exactly what a real one costs it.
	if b.unavailable[strings.ToLower(name)] {
		return false, nil
	}
	// The host's hold on a shop trip (combatTrainerEmbed.walkHold), asked before the trip is booked. Every supply trip -- the bow, the arrows,
	// the staff, the runes -- starts here, and only once a purchase is really wanted, so the hold speaks only about a walk that was about to be made.
	if b.shopHeld(ctx) {
		return false, nil
	}
	b.vlog(ctx, "buying %d x %s from %s at %d,%d", qty, name, site.Keeper, site.Stand.X, site.Stand.Z)
	b.gearTrips++
	b.lastGearTrip = time.Now()
	b.killsAtLastGearTrip = b.kills

	res, err := combatrunner.BuyFrom(d, site, name, qty)
	key := strings.ToLower(name)
	if err != nil {
		if errors.Is(err, combatrunner.ErrNotOnShelf) {
			// A shelf that does not carry it never will: the .inv line is either absent or zero,
			// and World.ts tops a line up only toward its own stockcount, so a 0 never rises.
			b.unavailable[key] = true
			ctx.logf("combattrainer: %s does not stock %s — striking it, since a stock line this run cannot see does not restock", site.Keeper, name)
			return true, nil
		}
		b.supplyFails[key]++
		if b.supplyFails[key] >= supplyAttempts {
			ctx.logf("combattrainer: giving up on buying %s from %s after %d failed trips (%v)", name, site.Keeper, b.supplyFails[key], err)
		} else {
			b.warn(ctx, "supply-"+key, time.Minute, "could not buy %s from %s (attempt %d of %d): %v", name, site.Keeper, b.supplyFails[key], supplyAttempts, err)
		}
		ctx.Delay(2 * time.Second)
		return true, nil
	}
	if res.Bought <= 0 {
		if outOfStockSaid(res.Said) {
			b.unavailable[key] = true
			ctx.logf("combattrainer: %s keeps no %s at all — the server said %q, which is an empty stock line rather than a price", site.Keeper, name, strings.TrimSpace(res.Said))
			return true, nil
		}
		b.supplyFails[key]++
		b.warn(ctx, "supply-afford-"+key, time.Minute, "%s would not sell us %s — the server said %q. Training on until the drops have paid for it", site.Keeper, name, res.Said)
		return true, nil
	}
	b.supplyFails[key] = 0
	b.bought += res.Bought
	ctx.logf("combattrainer: bought %d x %s from %s", res.Bought, name, site.Keeper)
	combatrunner.EquipByName(d, name, 5*time.Second)
	// The shop is nowhere near the rung, so the trip back has to be re-derived.
	b.haveRung = false
	return true, nil
}

// ---- arrow recovery ----

// takeArrows picks one stack of this run's own arrows back off the ground.
//
// It is worth its own step rather than being folded into the bone loot because the economics are
// different and much sharper. Content destroys one fired arrow in five and drops the other four
// on the target's own tile (ranged_dropammo_npc, against ^dropammo_chance = 5), so recovery is
// the difference between spending one arrow per shot and spending one per five. At Lowe's prices
// that is the difference between an arrow budget that outlasts a session and one that does not
// outlast a rung.
//
// Attribution is the same rule the bones use: the stack has to lie where something we killed
// recently died, so another account's arrows are left alone.
func (b *combatTrainerBot) takeArrows(ctx *Context, d *combatrunner.Deps) bool {
	if b.arrowName == "" || b.trainingStyle != combatplan.StyleRanged {
		return false
	}
	inv := d.Session.State().Inventory(combatrunner.PlayerPack)
	if inv != nil && len(inv.NonEmpty()) >= 28 && combatrunner.HeldCount(d, b.arrowName) == 0 {
		// A full pack with no existing stack to merge into has nowhere to put them.
		return false
	}
	now := time.Now().UnixMilli()
	for _, g := range combatrunner.GroundItemsNamed(d, b.arrowName) {
		if !combatplan.DropFromKill(g.X, g.Z, g.Level, b.killSites, now, 180_000) {
			continue
		}
		if err := combatrunner.ApproachAndTake(d, g, 1, b.rung.Leash, 20*time.Second); err != nil {
			b.vlog(ctx, "could not recover the %s: %v", strings.ToLower(b.arrowName), err)
			return false
		}
		b.arrowsTaken++
		b.vlog(ctx, "recovered a %s (%d so far, %d now to hand)", strings.ToLower(b.arrowName), b.arrowsTaken, combatrunner.ArrowsAvailable(d, b.arrowName))
		return true
	}
	return false
}

// unusedFmtRef keeps the fmt import honest if a future edit drops the last use.
var _ = fmt.Sprintf
