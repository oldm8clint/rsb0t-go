package freshstartrunner

import (
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
)

// pickpocketLootWait bounds how long RecordPickpocketLoot waits for the stolen item to show in
// the pack after the XP has already confirmed the success: one game tick. It is only ever spent
// when the pack shows nothing yet (a full pack, or the inventory update landing just after the
// XP one); a normal success returns at once.
const pickpocketLootWait = 600 * time.Millisecond

// RecordPickpocketLoot reports one successful pickpocket of npcType to d.Loot, measured as what
// arrived in the pack since packBefore (taken just before the click). A success whose take never
// shows (a full pack) is still counted as one pickpocket with no items. Exported for
// thieverrunner, whose own pickpocket shares the measurement.
func RecordPickpocketLoot(d *Deps, npcType int, name string, packBefore loot.Counts) {
	if d.Loot == nil {
		return
	}
	read := func() *gamestate.Inventory { return d.Session.State().Inventory(playerPack) }
	// A bite of food inside the window turns a Cake into a "2/3 cake", which ARRIVES and so reads
	// as a gain; no pickpocket table gives one (loot.IsEatenLeftover), so it is skipped.
	gained := loot.WaitGained(read, packBefore, d.Delay, pickpocketLootWait, loot.IsEatenLeftover)
	d.Loot(loot.Drop{Source: loot.NPC(d.Config, loot.ActionPickpocket, npcType, name), Events: 1, Items: loot.Items(d.Config, gained)})
}
