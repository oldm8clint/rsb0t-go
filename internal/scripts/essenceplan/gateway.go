package essenceplan

import (
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// The gateways into the Rune essence mine. Content 289 has five NPCs with a "Teleport" option; the
// three on the surface are here (Sedridor is in the Wizards' Tower basement, Brimstail in the gnome
// cave). All need Rune Mysteries complete (skill_runecraft/scripts/essence_mine.rs2:2-6), cast the same
// five-tick teleport into one of 22 spots of mapsquare 45_75, and the mine's portal brings the player
// out beside the NPC that sent it in (essence_mine.rs2:27-41).
//
//	Aubury              op4 at 3253,3402 (areas/area_varrock/scripts/aubury.rs2:32-33)
//	Wizard Distentor    op3 at 2594,3089 inside the Wizards' Guild (area_yanille/scripts/wizard_distentor.rs2:15-16);
//	                    the guild's door refuses Magic under 66 (magic_guild.rs2:11)
//	Wizard Cromperty    op3 at 2683,3326 in East Ardougne (area_ardougne_east/scripts/wizard_cromperty.rs2:9-10)
//
// The bank distances are the real thing: from the mine's exit the Yanille bank booths (2614,3091-3094) are
// about 20 steps through the guild's east door, Aubury's exit is about 49 steps from the Varrock east bank,
// Cromperty's 67-74 from the East Ardougne south bank, so with the mining time the same either way the
// Guild is the best place to mine from (docs/runecraft_p1_2026-10-13.md).

// The gateway keys (craftRuneGateway).
const (
	GatewayAubury    = "aubury"
	GatewayGuild     = "guild"
	GatewayCromperty = "cromperty"
)

// Gateway is one way into the essence mine.
type Gateway struct {
	// Key is GatewayAubury and so on.
	Key string
	// NPC is the cache name of the NPC, Op the option that teleports (matched by name, so the option
	// number is whatever the cache says: 4 for Aubury, 3 for the wizards).
	NPC string
	Op  string
	// Stand is where the NPC stands and Leash how far from it he is still looked for. Wander is how far from Stand
	// he walks: the NPC's wanderrange in content's npc config (guild_wizard and ardounge_wizard set 4, aubury sets none
	// and so has NpcType's default of 5), and the engine picks a destination in that square every few ticks
	// (Npc.wanderMode), so he is rarely on Stand: a click on him must be sent from beside where he IS.
	Stand  tile.Tile
	Leash  int
	Wander int
	// Bank is the booth bank nearest the mine's exit by this gateway, BankName its name.
	Bank     tile.Tile
	BankName string
	// MinMagic is the Magic level the way to the NPC needs (the Wizards' Guild door), 0 for none.
	MinMagic int
}

// Gateways is every gateway the miner knows.
func Gateways() []Gateway {
	return []Gateway{
		{Key: GatewayAubury, NPC: Aubury, Op: TeleportOp, Stand: AuburyTile, Leash: AuburyLeash, Wander: 5,
			Bank: BankStand, BankName: "Varrock east"},
		{Key: GatewayGuild, NPC: "Wizard Distentor", Op: TeleportOp, Stand: tile.Tile{X: 2594, Z: 3089}, Leash: 8, Wander: 4,
			Bank: tile.Tile{X: 2613, Z: 3094}, BankName: "Yanille", MinMagic: 66},
		{Key: GatewayCromperty, NPC: "Wizard Cromperty", Op: TeleportOp, Stand: tile.Tile{X: 2683, Z: 3326}, Leash: 8, Wander: 4,
			Bank: tile.Tile{X: 2655, Z: 3286}, BankName: "East Ardougne south"},
	}
}

// GatewayByKey is the gateway for a key (case-insensitive).
func GatewayByKey(key string) (Gateway, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, g := range Gateways() {
		if g.Key == key {
			return g, true
		}
	}
	return Gateway{}, false
}

// GatewayKeys lists the keys, for a settings error.
func GatewayKeys() []string {
	var out []string
	for _, g := range Gateways() {
		out = append(out, g.Key)
	}
	return out
}

// AuburyGateway is the one the original miner uses.
func AuburyGateway() Gateway {
	g, _ := GatewayByKey(GatewayAubury)
	return g
}

// Allows reports whether an account at magic can use the gateway at all, and why not.
func (g Gateway) Allows(magic int) (bool, string) {
	if g.MinMagic > 0 && magic < g.MinMagic {
		return false, g.NPC + " is behind the Wizards' Guild door, which needs Magic " + strconv.Itoa(g.MinMagic) + " (have " + strconv.Itoa(magic) + ")"
	}
	return true, ""
}

// RefusalReason names why a chat line is a gateway refusing the teleport, "" for any other line. The
// teleport answers a refusal in the same tick as the click, so a caller that sees one gives up AT ONCE
// instead of waiting out the 15 seconds it allows a teleport to land (live 13 Oct 2026: 39 timeouts and 25
// refusals in one log window, none a success: every wait was a refusal nobody read).
func RefusalReason(line string) string {
	if IsQuestRefusalMessage(line) {
		return "Rune Mysteries is not complete"
	}
	return ""
}
