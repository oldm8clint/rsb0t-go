package bot

import "rsb0t-go/internal/loot"

// lootHandover reports essence that crossed to the mule, for the Loot panel's Essence supply card (a user request:
// "runecrafting loot panel is needed"; internal/loot/runecraft.go says why a hand-over is a count and no item). It is
// called by the supply loop at the one place a hand-over is known to have happened: the trade was seen to take the stack
// and essence really left the pack. Nil-safe: a bare test Context has no Loot sink and records nothing.
func lootHandover(ctx *Context, essence int) {
	if ctx == nil {
		return
	}
	loot.EssenceHandedOver(ctx.Loot, essence)
}
