package questplan

import "rsb0t-go/internal/scripts/tile"

// What the quest-point fillers (Cook's Assistant, Sheep Shearer) share: where a shop is worth the walk, and which bank
// they use.

// ShopOnTheWay reports whether a shop is worth listing as a source for an account standing at s: the account is nearer to
// the shop than to home, the ground the quest itself works on. The owner's item rule puts the shop after the account's own
// work and the fleet's quest ladder is a chain of long walks, so a shop is worth a trip only when it costs less than the
// work it replaces. East Ardougne's wool, 850 steps from Fred's farm, is a source for an account that already stands in
// Ardougne and a detour of twenty minutes for one that stands in Lumbridge; Wydin's flour in Port Sarim, 314 steps from the
// Lumbridge Cook, is further than the windmill 150 steps from him. Before a position packet has arrived the shop is not
// listed: the account's own work is the safe choice.
func ShopOnTheWay(s Snapshot, shop ShopRef, home tile.Tile) bool {
	return s.TileKnown && tile.Chebyshev(s.Tile, shop.Anchor) < tile.Chebyshev(s.Tile, home)
}

// cookSheepBank is the bank a filler's provisioning and deposit steps use: the bank nearest the account, except that Lumbridge
// has none and Al Kharid's is behind a toll gate, so an account in Lumbridge banks at Draynor (as Imp Catcher and Witch's
// Potion do). Draynor before a position packet has arrived.
func cookSheepBank(s Snapshot) tile.Tile {
	if !s.TileKnown {
		return DraynorBank
	}
	if b := NearestBank(s.Tile); b.Name != "Al Kharid" {
		return b.Tile
	}
	return DraynorBank
}
