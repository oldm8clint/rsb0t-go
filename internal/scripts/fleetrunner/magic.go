package fleetrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// Combat adapts d to combatrunner's Deps — the same nine fields, the Delayer converted, plus
// the kill hook so every monster this stage fights reaches the loot panel.
//
// The hook matters here more than anywhere else in the script: Stage 3 trains Magic by
// killing Cows north of the Ardougne market, and until 2026-09-22 the panel had never
// recorded a single one. Autocast training goes through combatrunner.FightOne, so wiring it
// once here covers the whole primary training path.
func Combat(d *Deps) *combatrunner.Deps {
	var lastBite time.Time
	return &combatrunner.Deps{
		Session: d.Session, Config: d.Config, Components: d.Components, Pack: d.Pack,
		Delay: combatrunner.Delayer(d.Delay), Log: d.Log, Locs: d.Locs, Walker: d.Walker, Chat: d.Chat,
		OnKill: killRecorder(d), OnKillSpent: killSpentRecorder(d), OnFightSpent: fightSpentRecorder(d),
		ClickRange: d.ClickRange,
		// A WalkNear nobody is eating for is a walk into whatever hits the account (ogresafe-1017): it eats at every wait, at
		// the line against what faces it, and stops the account where it stands when it gives up.
		Watch:         func() { EatIfLow(d, &lastBite, "walk eat") },
		StopOnAbandon: true,
	}
}

// killSpentRecorder is noteKill with the runes the fight used up (combatrunner.Deps.OnKillSpent),
// reported as the kill's Spent so the panel nets them off that npc's drops; nil without a sink.
func killSpentRecorder(d *Deps) func(npcType, x, z, level int, spent map[int32]int) {
	if d == nil || d.Loot == nil {
		return nil
	}
	return func(npcType, x, z, level int, spent map[int32]int) {
		NoteKillSpent(d, npcType, x, z, level, spent)
	}
}

// NoteKillSpent records one kill with the runes it cost: one event and the Spent runes on the npc,
// remembered at its tile so the drops are filed under it. npcType < 0 or no sink records nothing.
func NoteKillSpent(d *Deps, npcType, x, z, level int, spent map[int32]int) {
	if d == nil || d.Loot == nil || npcType < 0 {
		return
	}
	src := loot.NPC(d.Config, loot.ActionKill, npcType, "")
	d.Loot(loot.Drop{Source: src, Events: 1, Spent: runeItems(d, spent)})
	d.kills().Note(src, x, z, level, time.Now())
}

// fightSpentRecorder is what combatrunner.Deps.OnFightSpent calls for a fight that used runes and
// ended with no kill: the runes are that npc's cost with no kill added. nil without a sink.
func fightSpentRecorder(d *Deps) func(npcType int, spent map[int32]int) {
	if d == nil || d.Loot == nil {
		return nil
	}
	return func(npcType int, spent map[int32]int) { NoteSpentOnly(d, npcType, spent) }
}

// NoteSpentOnly records runes spent at an npc that did not die (yet): the cost, and no kill.
func NoteSpentOnly(d *Deps, npcType int, spent map[int32]int) {
	if d == nil || d.Loot == nil || npcType < 0 || len(spent) == 0 {
		return
	}
	d.Loot(loot.Drop{Source: loot.NPC(d.Config, loot.ActionKill, npcType, ""), Spent: runeItems(d, spent)})
}

// runeItems is a measured rune spend as loot items, named through the cache.
func runeItems(d *Deps, spent map[int32]int) []loot.Item {
	counts := loot.Counts{}
	for id, n := range spent {
		counts[int(id)] = int64(n)
	}
	return loot.Items(d.Config, counts)
}

// NoteKill records one observed kill through d.Loot, which internal/bot sets from
// Context.Loot, and remembers it at its tile so the bones it leaves are recorded on it
// (bones.go, lootreport.go). npcType < 0 or no sink records nothing.
func NoteKill(d *Deps, npcType, x, z, level int) {
	if d == nil || d.Loot == nil || npcType < 0 {
		return
	}
	src := loot.NPC(d.Config, loot.ActionKill, npcType, "")
	d.Loot(loot.Drop{Source: src, Events: 1})
	d.kills().Note(src, x, z, level, time.Now())
}

// killRecorder is noteKill shaped for combatrunner.Deps.OnKill, or nil when there is no loot
// sink — which is what combatrunner's own nil check wants, so a harness behaves as before.
func killRecorder(d *Deps) func(npcType, x, z, level int) {
	if d == nil || d.Loot == nil {
		return nil
	}
	return func(npcType, x, z, level int) { NoteKill(d, npcType, x, z, level) }
}

// MagicLevel is the account's current Magic level.
func MagicLevel(d *Deps) int { return combatrunner.SkillLevel(Combat(d), fleetplan.MagicStat) }

// MagicXP is the account's Magic experience.
func MagicXP(d *Deps) int {
	sk, ok := d.Session.State().Skill(fleetplan.MagicStat)
	if !ok {
		return 0
	}
	return sk.XP
}

// StaffOnHand is the autocast staff worn or carried ("" for none) -- one the account can WIELD, so
// never a bank note (see heldStaff). StaffOwned also answers a noted one.
func StaffOnHand(d *Deps) (name string, worn bool) {
	if n := HeldStaff(HeldRows(d, WornSlots(d))); n != "" {
		return n, true
	}
	return HeldStaff(HeldRows(d, PackSlotItems(d))), false
}

// HeldRow is one held item by name, with whether it is a bank note.
type HeldRow struct {
	Name  string
	Noted bool
}

// HeldRows names slots through the config, noting the bank notes.
func HeldRows(d *Deps, slots []gamestate.SlotItem) []HeldRow {
	out := make([]HeldRow, 0, len(slots))
	for _, it := range slots {
		if it.ID <= 0 || it.Count <= 0 {
			continue
		}
		row := HeldRow{Name: ObjName(d, it.ID)}
		if d != nil && d.Config != nil {
			if o, ok := d.Config.Obj(int(it.ID)); ok {
				row.Noted = o.Noted()
			}
		}
		out = append(out, row)
	}
	return out
}

// WornSlots is the worn panel's rows (nil without an interface archive).
func WornSlots(d *Deps) []gamestate.SlotItem {
	if d == nil || d.Components == nil {
		return nil
	}
	return actions.NewEquipment(d.Session, d.Components).Worn()
}

// HeldStaff is the first autocast staff among rows that can be wielded: NEVER a bank note.
//
// LIVE-FOUND 2026-09-25: every Stage 3 account that bought its Staff of air at the Seers Market
// since 2026-09-24 -- fourteen of the 54-account cohort -- received it as a BANK NOTE. A note
// carries the item's name, so this used to answer "Staff of air, carried"; PrepareTraining then
// kept it out of the deposit by that name, never un-noted it, and EquipByName found no Wield
// option on it ("wear Staff of air: actions: no option with that label", 8,505 times in 37
// hours). With no staff wielded the combat tab stayed combat_unarmed, autocast was impossible,
// the hand-cast fallback had no air runes (the kit left them out, believing the staff paid
// them), and the account walked bank -> pen -> bank for 38 hours at Magic 1.
func HeldStaff(rows []HeldRow) string {
	for _, r := range rows {
		if !r.Noted && fleetplan.IsAutocastStaff(r.Name) {
			return r.Name
		}
	}
	return ""
}

// TotalFoodHeld counts every food item in the pack (fish, bakery, anything fighterfood knows) that
// can be EATEN: bank notes carry the item's name and are not food (the kit2 review: a bundled kit's
// noted cakes read as food and ended the food ask while the account could eat none of them).
func TotalFoodHeld(d *Deps) int {
	n := 0
	for _, f := range EatableFoods(d) {
		n += f.Count
	}
	return n
}

// PackUnnoted is the pack without its bank notes.
func PackUnnoted(d *Deps) []gamestate.SlotItem {
	return WithoutNotes(PackSlotItems(d), NotedIn(d))
}

// NotedIn is the object config's "is this object id a bank note".
func NotedIn(d *Deps) func(id int32) bool {
	return func(id int32) bool {
		if d == nil || d.Config == nil {
			return false
		}
		o, ok := d.Config.Obj(int(id))
		return ok && o.Noted()
	}
}

// EatableFoods is the pack's food by base name, bank notes left out: CarriedFoods(EatableFoodNames(d))
// read a stack at a time.
func EatableFoods(d *Deps) []fleetplan.CarriedFood {
	return fleetplan.CarriedFoodsOf(KitItems(d, PackUnnoted(d)))
}

// WithoutNotes drops the slots whose object is a bank note.
func WithoutNotes(slots []gamestate.SlotItem, noted func(id int32) bool) []gamestate.SlotItem {
	out := make([]gamestate.SlotItem, 0, len(slots))
	for _, it := range slots {
		if !noted(it.ID) {
			out = append(out, it)
		}
	}
	return out
}

// ZaffSite is Zaff's staff shop as a combatrunner.ShopSite.
func ZaffSite() combatplan.ShopSite {
	return combatplan.ShopSite{Shop: "Zaff's Superior Staffs", Keeper: "Zaff", Stand: fleetplan.ZaffStand,
		Bank: tile.Tile{X: 3185, Z: 3436}, Note: "Varrock; staff of air 2 in stock world-wide, +1 per 1000 ticks"}
}
