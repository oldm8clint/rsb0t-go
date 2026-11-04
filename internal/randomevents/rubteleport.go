package randomevents

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
)

// A JEWELLERY RUB AS A TELEPORT (plantfisher-1018, 18 Oct 2026).
//
// The last-resort exit (lastresort.go) casts the teleport spell the account can pay for: a Law rune, runes of the element
// and a Magic level. A ProgressiveFisher has none of those, but the one that died at the Fishing Guild bank carried a
// Games necklace or a Ring of dueling for the run staller (fisherrunner/stall.go: it rubs one on every third tick of a walk
// for the p_delay(1) that drains no run energy), and a rub is a teleport when the menu it opens is answered. Content 289
// (scripts/general/scripts/enchanted_jewellry/ring_of_dueling.rs2, necklace_of_minigames.rs2): [opheld4] says "You rub ...",
// p_delay(1), then a p_choice menu, "Al Kharid Duel Arena." first on the ring and "Burthorpe Games Rooms." first on the
// necklace; answering teleports (player_teleport_normal) and spends a charge only when it lands.
//
// The ring is the better exit: it lands in the open at the Duel Arena. The necklace lands in the Games Room, a catalogued
// strand site (strandsites.go) with stairs the strand ladder climbs, which costs minutes and is better than a death.
// Everywhere else the rub is a menu the Guardian answers "Nowhere." (eventDialogChoice), so nothing here changes that.

// rubTeleports are the jewellery an exit can use, best first: the item's name prefix (any charge), the text of the menu
// option to press, and where it goes for the log.
var rubTeleports = []struct{ prefix, option, where string }{
	{"ring of dueling", "Duel Arena", "the Al Kharid Duel Arena"},
	{"games necklace", "Burthorpe", "the Burthorpe Games Rooms"},
}

// rubMenuWait is how long the rub's menu is waited for: the rub's own p_delay(1) is two ticks, and the menu comes after it. A
// variable only so a test can shorten it; nothing else assigns it.
var rubMenuWait = 3 * time.Second

// rubPick is the jewellery in the pack a rub exit would use: the item, its slot and the menu option to press.
type rubPick struct {
	obj          int32
	slot         int
	name, option string
	where        string
}

// packItem is one pack slot as the rub choice reads it.
type packItem struct {
	Obj   int32
	Slot  int
	Name  string
	Noted bool
}

// rubItemIn is the best usable jewellery among the pack's items, unnoted. Pure.
func rubItemIn(items []packItem) (rubPick, bool) {
	for _, kind := range rubTeleports {
		for _, it := range items {
			if !it.Noted && strings.HasPrefix(strings.ToLower(strings.TrimSpace(it.Name)), kind.prefix) {
				return rubPick{obj: it.Obj, slot: it.Slot, name: it.Name, option: kind.option, where: kind.where}, true
			}
		}
	}
	return rubPick{}, false
}

// heldRubItem reads the account's pack for a jewellery it can rub to leave.
func heldRubItem(s actions.Session) (rubPick, bool) {
	res := currentResolver()
	if res == nil || s == nil || s.State() == nil {
		return rubPick{}, false
	}
	inv := s.State().Inventory(actions.PlayerPack)
	if inv == nil {
		return rubPick{}, false
	}
	var items []packItem
	for _, it := range inv.NonEmpty() {
		if obj, ok := res.Obj(int(it.ID)); ok {
			items = append(items, packItem{it.ID, it.Slot, obj.Name, obj.Noted()})
		}
	}
	return rubItemIn(items)
}

// rubTeleport rubs the account's Ring of dueling (else Games necklace) and answers the menu that opens with the option that
// leaves, and reports whether it did. A rub shares lastResortCasts with the spell exit, so the two never go out within gap of
// one another: the teleport takes a few ticks to land, and a second would only spend a charge or a rune. False for no
// jewellery, no interface archive to read the menu with, a refused send, a menu that never opened or did not offer the option.
func rubTeleport(s actions.Session, why string, gap time.Duration, log func(string)) bool {
	if v, ok := lastResortCasts.Load(s); ok && time.Since(v.(time.Time)) < gap {
		return false
	}
	pick, ok := heldRubItem(s)
	if !ok {
		return false
	}
	components := currentInterfaceArchive()
	if components == nil {
		return false
	}
	if err := actions.OpHeld(s, 4, pick.obj, pick.slot, actions.PlayerPack); err != nil {
		if log != nil {
			log(fmt.Sprintf("%s: rubbing the %s failed: %v", why, pick.name, err))
		}
		return false
	}
	lastResortCasts.Store(s, time.Now())
	actions.NoteDelayTicks(s, 3) // the rub's p_delay(1): an eat sent inside it is dropped
	dlg := actions.NewChatDialog(s, components)
	deadline := time.Now().Add(rubMenuWait)
	for time.Now().Before(deadline) {
		if dlg.IsOpen() {
			err := dlg.ChooseOption(pick.option)
			if err == nil {
				if log != nil {
					st := s.State()
					hp, _ := st.HP()
					max, _ := st.MaxHP()
					x, z, level, _ := st.Tile()
					log(fmt.Sprintf("%s: %d/%d hitpoints at %d,%d,%d (%s) — rubbed the %s and chose %s", why, hp, max, x, z, level, attackerDiag(st), pick.name, pick.where))
				}
				return true
			}
			if errors.Is(err, actions.ErrItemNotFound) {
				_ = actions.CloseModal(s)
				if log != nil {
					log(fmt.Sprintf("%s: the %s's menu did not offer %q", why, pick.name, pick.option))
				}
				return false
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if log != nil {
		log(fmt.Sprintf("%s: the %s's menu never opened", why, pick.name))
	}
	return false
}
