package bot

import (
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/peerrunner"
	"rsb0t-go/internal/scripts/tile"
)

// scarabSwarmCheckpointDir is where Checkpoint files are persisted, one JSON file per
// account under <dir>/<account>/<ScarabSwarmStoreID>.json (see internal/settings' own doc
// comment on this exact Store/Bag shape). Relative to the process's working directory —
// every cmd/*/main.go and cmd/fleet/cmd/controlpanel in this repo is already run from the
// repo root, so this lands as a real, inspectable directory next to fleet.config.json rather
// than somewhere hidden. Overridable per-account via the "checkpointDir" setting for a test
// tool that wants an isolated directory (see cmd/scarabswarmtest).
const scarabSwarmCheckpointDir = "data/scarabswarm-checkpoints"

func currentTileFor(ctx *Context) *tile.Tile {
	x, z, level, ok := ctx.Session.State().Tile()
	if !ok {
		return nil
	}
	return &tile.Tile{X: x, Z: z, Level: level}
}

// firstNonEmpty is a, or b when a is empty.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// alchAskName is how an alch stock item is asked of the mule (a user request): for a name that
// more than one un-noted obj carries and the mule publishes per obj (peerrunner.IDKeyedNames: the
// bows, the Ruby ring, the Black dagger) it is the QUALIFIED name of the obj a High Alchemy cast pays
// most for ("Yew shortbow id857", the strung bow: the plain name is handed whichever obj the mule's
// bank holds first, and for the Yew longbow that is the unstrung one, 384 coins a cast against 768).
// Every other name is asked as it is. nil with no config: the plain names.
func alchAskName(cfg *cachecfg.Config) func(item string) string {
	if cfg == nil {
		return nil
	}
	return func(item string) string {
		if !peerrunner.IsIDKeyed(item) {
			return item
		}
		id, shared, ok := cfg.Dearest(item)
		if !ok || !shared {
			return item
		}
		return cachecfg.QualifiedName(item, id)
	}
}

// runeGateFrom reads a rune-pool rule from settings under the given key names, the owner's numbers where unset: the
// mule's own roster entry (handoutRune*) and a ScarabSwarm account's view of it (muleRune*) are the same rule.
func runeGateFrom(m map[string]interface{}, floorKey, maxKey, runesKey string) orderplan.RuneGate {
	g := orderplan.DefaultRuneGate()
	g.Floor = IntSetting(m, floorKey, g.Floor)
	g.Max = IntSetting(m, maxKey, g.Max)
	if hasSetting(m, runesKey) {
		g.Runes = stringListSetting(m, runesKey) // an empty list gates no rune
	}
	return g
}

// kqPrayerRungLevelHook is the reading of the Queen's Prayer rung level that ScarabSwarm registers (its kq*
// and kqGate* settings live in its own plan); nil, in a build without it, so there is no rung (0).
var kqPrayerRungLevelHook func(m map[string]interface{}) int

// kqPrayerRungLevel is the Prayer level the Queen's rung trains to: the level of the protection prayer
// kqPrayer holds at her, or the gate's explicit Prayer floor when one is set. 0 is "none": the rung is
// left out (also what a build without ScarabSwarm reads).
func kqPrayerRungLevel(m map[string]interface{}) int {
	if kqPrayerRungLevelHook == nil {
		return 0
	}
	return kqPrayerRungLevelHook(m)
}
