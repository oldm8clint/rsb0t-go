package freshstartrunner

import (
	"fmt"
	"strings"

	"rsb0t-go/internal/actions"
)

// The last rung of walkNearCore's STRANDED ladder casts Lumbridge Teleport. It used to cast it
// whether or not the account could: live 2026-09-25, four ScarabSwarm accounts shut in the sheep pen
// north of East Ardougne (Magic 22 to 26, no law rune between them) sent the cast, waited out the
// ten-second landing timeout and logged "did not move the account" 9 to 40 times each, every STRANDED
// cycle, for hours. The server had refused every one of them before the wait began.
//
// So the ladder now asks first. Content's own requirements, [magic_spell_teleport_lumbridge] in
// skill_magic/configs/magic_spells.dbrow: levelrequired 31, runesrequired earthrune 1, airrune 3,
// lawrune 1, checked against the CURRENT Magic level (stat(magic), so a drained level refuses too).
// A wielded staff pays for its element (magic_staff_table in skill_magic/configs/magic_staff.dbrow);
// the two this spell can use are listed below.
const (
	lumbridgeTeleportLevel = 31
	lumbridgeTeleportLaw   = 1
	lumbridgeTeleportEarth = 1
	lumbridgeTeleportAir   = 3
)

// airStaves and earthStaves are content's magic_staff_table rows for air and earth.
var (
	airStaves   = []string{"Staff of air", "Air battlestaff", "Mystic air staff"}
	earthStaves = []string{"Staff of earth", "Earth battlestaff", "Mystic earth staff", "Lava battlestaff", "Mystic lava staff"}
)

// magicStat is UPDATE_STAT's Magic index (gamestate.SkillNames[6]).
const magicStat = 6

func wearsAny(worn []string, staves []string) bool {
	for _, w := range worn {
		for _, s := range staves {
			if strings.EqualFold(strings.TrimSpace(w), s) {
				return true
			}
		}
	}
	return false
}

// lumbridgeTeleportRefusal is why an account cannot cast Lumbridge Teleport, or "" when it can.
// magic is the current Magic level and magicKnown whether the server has sent it at all; pack counts
// the backpack by item name (any case); worn names what is equipped. Pure.
func lumbridgeTeleportRefusal(magic int, magicKnown bool, pack map[string]int, worn []string) string {
	if !magicKnown {
		return "its Magic level has not been reported yet"
	}
	if magic < lumbridgeTeleportLevel {
		return fmt.Sprintf("Magic %d is below the %d it needs", magic, lumbridgeTeleportLevel)
	}
	count := func(name string) int {
		n := 0
		for k, v := range pack {
			if strings.EqualFold(strings.TrimSpace(k), name) {
				n += v
			}
		}
		return n
	}
	var short []string
	if n := count("Law rune"); n < lumbridgeTeleportLaw {
		short = append(short, fmt.Sprintf("%d of %d Law rune", n, lumbridgeTeleportLaw))
	}
	if n := count("Earth rune"); n < lumbridgeTeleportEarth && !wearsAny(worn, earthStaves) {
		short = append(short, fmt.Sprintf("%d of %d Earth rune and no earth staff wielded", n, lumbridgeTeleportEarth))
	}
	if n := count("Air rune"); n < lumbridgeTeleportAir && !wearsAny(worn, airStaves) {
		short = append(short, fmt.Sprintf("%d of %d Air runes and no air staff wielded", n, lumbridgeTeleportAir))
	}
	if len(short) > 0 {
		return "the pack holds " + strings.Join(short, ", ")
	}
	return ""
}

// spellRescueRefusal reads the account's Magic level, pack and worn items and asks
// lumbridgeTeleportRefusal. Without an item config the runes cannot be named, which refuses too
// once the level alone does not: a cast nobody can show is possible is the thing this replaces.
func spellRescueRefusal(d *Deps) string {
	st := d.Session.State()
	sk, ok := st.Skill(magicStat)
	magicKnown := ok && sk.Generation > 0
	if !magicKnown || sk.Level < lumbridgeTeleportLevel {
		return lumbridgeTeleportRefusal(sk.Level, magicKnown, nil, nil)
	}
	if d.Config == nil {
		return "there is no item config to read its runes with"
	}
	pack := map[string]int{}
	if inv := st.Inventory(playerPack); inv != nil {
		for _, it := range inv.NonEmpty() {
			if obj, ok := d.Config.Obj(int(it.ID)); ok {
				c := int(it.Count)
				if c < 1 {
					c = 1
				}
				pack[obj.Name] += c
			}
		}
	}
	var worn []string
	for _, it := range actions.NewEquipment(d.Session, d.Components).Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok {
			worn = append(worn, obj.Name)
		}
	}
	return lumbridgeTeleportRefusal(sk.Level, magicKnown, pack, worn)
}

// EmergencyLumbridgeTeleport casts Lumbridge Teleport now, for a fight that cannot be survived (a
// Kalphite Queen fighter out of food or prayer). The engine's teleport delay lets hits already
// queued land, so a caller casts it with health to spare. landed reports whether the account moved;
// why is the refusal (level, runes) when no cast was sent.
func EmergencyLumbridgeTeleport(d *Deps) (landed bool, why string) {
	if why = spellRescueRefusal(d); why != "" {
		return false, why
	}
	here := currentTile(d)
	if here == nil {
		return false, "position unknown"
	}
	landed, _ = attemptSpellRescue(d, *here)
	if !landed {
		why = "the cast did not move the account"
	}
	return landed, why
}
