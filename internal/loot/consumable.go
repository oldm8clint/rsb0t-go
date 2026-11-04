package loot

import (
	"regexp"
	"strconv"
	"strings"

	"rsb0t-go/internal/cachecfg"
)

// CONSUMABLES, on a tab of their own (a user request: "every food, potion, wine etc. consumed
// (wines, kebabs, sharks, cakes, potions...) with counts and coin value. Consumed inputs are costs,
// never loot. NO runes in this tab").
//
// What is eaten or drunk is not loot and not an input to any one activity: it is a running cost of
// all of them, so it has a card of its own, KindConsumable with Skill "Consumables", one row per food
// or drink. Every consumption is one event whose cost is the item itself, carried as Drop.Spent (the
// same measured-cost field the runes cast at an npc and at a teleport use), so the control panel
// prices it at what the market says and nets it off the fleet's value.
//
// WHAT COUNTS AS A CONSUMPTION, read from content 289 (scripts/player/scripts/consumption). Eating
// and drinking are the held-item option 1 of an obj whose cache says "Eat" or "Drink", and the
// script deletes the item from its slot in the same step as it prints "You eat the ..." or "You
// drink ..." (consume_effects.rs2: inv_delslot, then the message). A food that leaves its slot with no
// such line is NOT eaten: banked, traded, dropped, sold, or lost on a death (the whole pack leaves at
// once). internal/bot's consume watch (lootconsume.go) pairs the two, so no script that eats has to
// report anything.
//
// WHICH PANEL COUNTS WHAT, so nothing is counted twice:
//
//   - A food or drink eaten or drunk: here and nowhere else.
//   - Runes: never here. They are not edible, so they cannot reach this card, and each is counted
//     where it was spent: teleports (teleport.go), the Magic card (magic.go: alchemy and superheat) and
//     the npc a spell was cast at (Drop.Spent on the kill).
//   - Raw food cooked: the Cooking card's input (craft.go). A bone buried: the Prayer card's.
//     An oyster opened: the Fishing card's. All inputs of a production, not consumables.
//
// A POTION is drunk a dose at a time, and its four forms are four objs named "Strength potion(4)"
// down to "(1)". A drink is therefore one event of the potion by its base name, priced at a dose: a
// quarter of the four-dose form's price, as the form's own count says (Doses).

// KindConsumable groups every food and drink eaten on one card. Titled by Skill.
const KindConsumable = "consumable"

// SkillConsumables titles the Consumables card; it is not a game skill, only the card's name.
const SkillConsumables = "Consumables"

// The two actions, which are the verbs of the obj's own first option.
const (
	ActionEat   = "eat"
	ActionDrink = "drink"
)

// doseSuffix is the "(n)" content 289 puts on a potion's name for the doses it holds.
var doseSuffix = regexp.MustCompile(`\s*\((\d)\)\s*$`)

// BaseName is a consumable's name without its dose count: "Strength potion(3)" is "Strength potion".
func BaseName(name string) string {
	return strings.TrimSpace(doseSuffix.ReplaceAllString(name, ""))
}

// Doses is how many doses the form named holds ("Strength potion(3)" is 3), 1 for anything else. The
// panel prices a drink at a dose: the price of the form, over its doses.
func Doses(name string) int {
	m := doseSuffix.FindStringSubmatch(name)
	if m == nil {
		return 1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// ConsumableOp reports what an obj does when it is eaten or drunk, from the cache's own first
// inventory option: "eat" for Eat, "drink" for Drink, false for anything else (a noted item, whose
// options are a note's, is never one).
func ConsumableOp(cfg *cachecfg.Config, objID int) (action string, ok bool) {
	if cfg == nil {
		return "", false
	}
	o, found := cfg.Obj(objID)
	if !found || o.Noted() {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(o.IOps[0])) {
	case "eat":
		return ActionEat, true
	case "drink":
		return ActionDrink, true
	}
	return "", false
}

// Consumable is the source of one food or drink, by its base name (every dose form of a potion is
// one row), drawn with the obj consumed. A name the cache cannot give falls back to fallback.
func Consumable(cfg *cachecfg.Config, objID int, action, fallback string) Source {
	name := objName(cfg, objID, fallback)
	return Source{Kind: KindConsumable, Skill: SkillConsumables, Action: action, Name: BaseName(name), ID: objID, Model: ModelObj}
}

// Consumed is the one-event Drop of an obj eaten or drunk: the source, one event, and the item
// itself as the cost. ok is false for an obj that is not edible (loot.ConsumableOp).
func Consumed(cfg *cachecfg.Config, objID int) (Drop, bool) {
	action, ok := ConsumableOp(cfg, objID)
	if !ok {
		return Drop{}, false
	}
	// The later bites of a cake, a pizza or a pie are not recorded: the whole food was costed at its
	// first bite (LaterBite).
	if cfg != nil {
		if o, found := cfg.Obj(objID); found && LaterBite(o.Name) {
			return Drop{}, false
		}
	}
	return Drop{
		Source: Consumable(cfg, objID, action, ""),
		Events: 1,
		Spent:  Items(cfg, Counts{objID: 1}),
	}, true
}

// A FOOD EATEN A BITE AT A TIME COSTS THE WHOLE FOOD ONCE, AT ITS FIRST BITE (a user request: "consumables
// panel is counting 2/3 cake and cake slice cost but we only need to calc the cake item price so it stays
// accurate"). The 7 Oct audit priced each bite as its form less the form it leaves; with three market prices
// that do not add up, the three rows did not add up to a cake either. Now eating a Cake costs a Cake, and
// eating the 2/3 cake and the Slice it leaves records nothing (LaterBite).
//
// The 7 Oct audit, for the record:
//
// A Cake is three bites: eating it leaves a 2/3 cake, eating that a Slice of cake, eating that nothing.
// Each bite takes the form out of its slot with a "You eat ..." line, so the consume watch records one
// consumption of each form, and priced at the form each time the three bites of one cake cost a cake, a
// 2/3 cake and a slice: live on 7 Oct the Consumables card held 1,045 Cakes, 1,043 2/3 cakes and 1,036
// Slices, 80,400 gp for what were 1,045 cakes (55,400 gp). A bite is what the form is worth over the
// form it leaves behind, so the three add up to the cake. The last form of a chain (a slice, a half
// pizza, a half pie) is eaten whole and costs its own price.

// biteTable maps a food that leaves another food behind when it is eaten (lowercased names, as content
// 289 spells them) to the one it leaves.
var biteTable = map[string]string{
	"cake":               "2/3 cake",
	"2/3 cake":           "Slice of cake",
	"chocolate cake":     "2/3 chocolate cake",
	"2/3 chocolate cake": "Chocolate slice",
	"plain pizza":        "1/2 plain pizza",
	"meat pizza":         "1/2 meat pizza",
	"anchovy pizza":      "1/2 anchovy pizza",
	"pineapple pizza":    "1/2 pineapple pizza",
	"apple pie":          "Half an apple pie",
	"redberry pie":       "Half a redberry pie",
	"meat pie":           "Half a meat pie",
}

// BiteOf reports the food a bite of the named one leaves behind, and false for a food eaten whole or
// whose last bite this is.
func BiteOf(name string) (leaves string, ok bool) {
	leaves, ok = biteTable[strings.ToLower(strings.TrimSpace(name))]
	return leaves, ok
}

// LaterBite reports whether name is what a bite of another food leaves behind (a 2/3 cake, a Slice of cake,
// half a pizza or a pie): its eating is part of the food already costed at the first bite.
func LaterBite(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	for _, leaves := range biteTable {
		if strings.ToLower(leaves) == key {
			return true
		}
	}
	return false
}
