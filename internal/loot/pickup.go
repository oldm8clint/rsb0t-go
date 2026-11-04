package loot

import "rsb0t-go/internal/cachecfg"

// PICKUPS THAT NO KILL MADE: an obj spawned on the map (the Kalphite Queen's Potato cacti, the
// knife ScarabSwarm's Stage 3 picks up, the stray bones BoneBurier collects). They are loot the
// account really took, and until now nothing reported them — so they get a source of their own,
// one card per item name, under their own filter entry rather than on some npc's card.

// KindPickup is an obj taken off the ground that no kill of this account dropped.
const KindPickup = "pickup"

// ActionTake is one pickup, for the panel's verb.
const ActionTake = "take"

// Pickup is a pickup source for one obj, keyed by the obj's id and drawn with its own model.
func Pickup(cfg *cachecfg.Config, objID int, fallback string) Source {
	return Source{Kind: KindPickup, Action: ActionTake, Name: objName(cfg, objID, fallback), ID: objID, Model: ModelObj}
}

// FoldsIntoKill reports whether a pickup no kill could claim still belongs on an npc's card rather
// than a pickup card of its own: a rune or a bone (a user request: "death runes show as their own
// panel, they should not; runes go under the activity that used/produced them. Big bones show as
// their own panel, put them under the activity (the npc kills) that produced them").
//
// Both are things the fleet's own fights leave on the ground: the bones an npc dropped that its kill
// aged out of the kill log before anyone took, the runes an account died holding or a caster's pile.
// So they are filed under the account's newest kill (KillLog.Latest) with no event of their own, and
// a rune with no kill to go under is not recorded at all.
func FoldsIntoKill(objID int) bool { return IsRuneID(objID) || IsBuryable(objID) }

// PickupSource is where a ground pickup that no recent kill claimed is recorded: the account's newest
// kill when the item is one that folds into kills (FoldsIntoKill) and the account has made one; a
// pickup card of the item itself for anything else that is not a rune. ok is false for a rune with
// no kill to go under, which is recorded nowhere. events is 1 for a pickup card (a take is its own
// event there) and 0 on a kill's card, where the kill was counted when it happened.
func PickupSource(cfg *cachecfg.Config, kills *KillLog, objID int, fallback string) (src Source, events int, ok bool) {
	if FoldsIntoKill(objID) {
		if last, found := kills.Latest(); found {
			return last, 0, true
		}
		if IsRuneID(objID) {
			return Source{}, 0, false
		}
	}
	return Pickup(cfg, objID, fallback), 1, true
}
