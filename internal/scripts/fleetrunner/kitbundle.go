package fleetrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/orderplan"
	"rsb0t-go/internal/scripts/tile"
)

// KitBundleOptions shapes one bundled kit ask.
type KitBundleOptions struct {
	// Timeout is the kit ask's whole budget; 0 uses donorrunner.DefaultKitTimeout.
	Timeout time.Duration
	// Bank is the bank beside the mule's post, used to deposit first and to un-note after. The
	// zero tile skips both (the ask then reports whatever room the pack has).
	Bank tile.Tile
	// Keep is every item name that stays in the pack through the deposit (worn items never
	// leave: a deposit only touches the pack). Ignored when Bank is zero.
	Keep []string
	// KeepNotes leaves the kit's noted lines as notes. By default, whenever Bank is given, they
	// are drawn back as items there after the trade.
	KeepNotes bool
	// Noted asks for every line as bank notes (orderplan.KitLine.Noted): what has a note form
	// crosses in one trade slot a line. For a donor that banks what it gets.
	Noted bool
	// PerOrder and FallbackTimeout bound the per-item fallback, as RequestKitFromMule's own.
	PerOrder        int
	FallbackTimeout time.Duration
	// World is the world this account is on (bot.Context.CurrentWorld), stamped on the kit order
	// so a mule elsewhere refuses it at once. 0: not known, not stamped. MuleDeps alone never set
	// it (revK).
	World int
	// Hold and Republish are donorrunner.KitAsk's: a check run while the kit waits in the mule's
	// line (non-empty gives the wait up), and how often a kit still in line is published again.
	// Both zero: a kit order waits exactly as before.
	Hold      func() string
	Republish time.Duration
}

// KitBundleInfo is what the bundle did beyond KitResult.
type KitBundleInfo struct {
	// Bundled is true when the mule answered the kit order; false means the per-item fallback ran.
	Bundled bool
	// FreeSlots is the room the ask reported after the deposit (-1 unread).
	FreeSlots int
	// Noted is how many of each item arrived as notes; Unnoted how many were drawn back as items;
	// NotedHeld how many are still notes in the pack; Banked how many went into the bank while
	// un-noting and stayed there for want of pack room. None of these is in KitResult.Got.
	Noted     map[string]int
	Unnoted   map[string]int
	NotedHeld map[string]int
	Banked    map[string]int
	Reason    string
	Stopped   bool
}

// RequestKitBundle asks mule for the whole kit in one trade; see this file's doc comment. The
// account must already be at the mule's post.
func RequestKitBundle(d *Deps, bus *mulebus.Bus, me, mule string, items []fleetplan.ItemCount, opts KitBundleOptions) (KitResult, KitBundleInfo) {
	res := KitResult{Got: map[string]int{}, Declined: map[string]string{}}
	info := KitBundleInfo{FreeSlots: -1}
	if len(items) == 0 {
		return res, info
	}
	dd := MuleDeps(d, bus, me)
	dd.MyWorld = opts.World
	hasBank := opts.Bank != (tile.Tile{})
	var give []donorrunner.GiveLine
	giveBefore := map[int32]int{} // what the pack held of each give id before the hand-over was drawn
	if hasBank {
		if bank, ok := OpenBank(d, opts.Bank); ok {
			info.FreeSlots = donorrunner.DepositForKit(dd, bank, opts.Keep)
			d.Logf("scarab kit: deposited all but %s before the bundled ask — %d free slot(s)", strings.Join(opts.Keep, ", "), info.FreeSlots)
			// Food is kept by default, and a pack full of it left no room for the kit (live 3 Oct:
			// 11 travel kits refused "no room in my pack" with 0 free slots after this deposit).
			if need := kitNewItems(d, items); info.FreeSlots >= 0 && info.FreeSlots < need {
				tight := KitRoomKeep(opts.Keep, items, d.KitKeepExtra)
				info.FreeSlots = donorrunner.DepositForKit(dd, bank, tight)
				d.Logf("scarab kit: %d new item(s) need room — banked the food too, keeping %s — %d free slot(s)", need, strings.Join(tight, ", "), info.FreeSlots)
			}
			if d.KitGive != nil {
				pre := packCountsByID(d)
				give = d.KitGive(bank)
				for _, g := range give {
					giveBefore[g.ID] = pre[g.ID]
				}
			}
			_ = bank.Close()
		} else {
			d.Logf("scarab kit: could not open the bank at %d,%d to make room — asking with the pack as it is", opts.Bank.X, opts.Bank.Z)
		}
	}
	// The baseline for Got, after the deposit: the item itself, never its note (and, for a line kept as
	// notes, the notes already held, so only what arrives counts).
	before, notedBefore := map[string]int{}, map[string]int{}
	for _, it := range items {
		before[it.Item] = CountPackUnnoted(d, it.Item)
		if d.Travel.KitKeepsNoted(it.Item) {
			notedBefore[it.Item] = CountPackNamed(d, it.Item) - before[it.Item]
		}
	}
	lines := make([]orderplan.KitLine, 0, len(items))
	for _, it := range items {
		lines = append(lines, orderplan.KitLine{Item: it.Item, Qty: it.Count, Noted: opts.Noted || d.Travel.KitKeepsNoted(it.Item)})
	}
	ans := donorrunner.AwaitKit(dd, donorrunner.KitAsk{Mule: mule, Me: me, Lines: lines, Bus: bus, Timeout: opts.Timeout,
		Hold: opts.Hold, Republish: opts.Republish, Give: give})
	info.Stopped, info.Reason, info.Noted = ans.Stopped, ans.Reason, ans.Noted
	if len(give) > 0 {
		res.Gave = kitGaveAndBankRest(d, opts.Bank, give, giveBefore)
	}
	if ans.Stopped {
		return res, info
	}
	if !ans.Answered {
		d.Logf("scarab kit: %s did not answer the bundled kit order (%s) — asking item by item", mule, ans.Reason)
		fb := RequestKitFromMule(d, bus, me, mule, items, opts.PerOrder, opts.FallbackTimeout)
		return fb, info
	}
	info.Bundled = true
	for item, why := range ans.Declined {
		res.Declined[item] = why
	}
	d.Logf("scarab kit: %s handed %s in one trade%s", mule, describeGot(ans.Got), declinedSuffix(res.Declined))
	if !opts.KeepNotes && hasBank && len(ans.Noted) > 0 {
		if bank, ok := OpenBank(d, opts.Bank); ok {
			var noted []string
			for item := range ans.Noted {
				if d.Travel.KitKeepsNoted(item) {
					continue // the stock walk alch casts on as notes
				}
				noted = append(noted, item)
			}
			un := donorrunner.UnnoteKit(dd, bank, noted)
			info.Unnoted, info.Banked = un.Unnoted, un.Banked
			_ = bank.Close()
		} else {
			d.Logf("scarab kit: could not open the bank at %d,%d to un-note the kit", opts.Bank.X, opts.Bank.Z)
		}
	}
	// Got from the pack as it now stands. A line kept as notes counts the notes that arrived (and is no
	// longer reported as notes still held: holding them is what it asked for).
	res.Got, info.NotedHeld = kitGotFromPack(d, items, before)
	res.Banked = bankedOf(info.Banked)
	for item, held := range notedBefore {
		if n := info.NotedHeld[item] - held; n > 0 {
			res.Got[item] += n
		}
		delete(info.NotedHeld, item)
	}
	if len(info.Banked) > 0 || len(info.NotedHeld) > 0 {
		d.Logf("scarab kit: in the pack as items %s; still notes %v; left in the bank %v", describeGot(res.Got), info.NotedHeld, info.Banked)
	}
	return res, info
}

// bankedOf is KitBundleInfo.Banked as KitResult.Banked: the positive counts, nil when the kit left nothing in the
// bank.
func bankedOf(banked map[string]int) map[string]int {
	var out map[string]int
	for item, n := range banked {
		if n > 0 {
			if out == nil {
				out = map[string]int{}
			}
			out[item] = n
		}
	}
	return out
}

// kitGotFromPack is what a kit left in the pack, per line: got is the ITEM itself gained since
// before (never a note, never what went into the bank -- see this file's doc comment), notedHeld
// the notes still in the pack.
func kitGotFromPack(d *Deps, items []fleetplan.ItemCount, before map[string]int) (got, notedHeld map[string]int) {
	got, notedHeld = map[string]int{}, map[string]int{}
	for _, it := range items {
		if n := CountPackUnnoted(d, it.Item) - before[it.Item]; n > 0 {
			got[it.Item] = n
		}
		if n := CountPackNamed(d, it.Item) - CountPackUnnoted(d, it.Item); n > 0 {
			notedHeld[it.Item] = n
		}
	}
	return got, notedHeld
}

// CountPackUnnoted is how many of name the pack holds as the item itself, not as a note.
func CountPackUnnoted(d *Deps, name string) int {
	n := 0
	for _, it := range PackSlotItems(d) {
		if d.Config == nil {
			break
		}
		o, ok := d.Config.Obj(int(it.ID))
		if !ok || o.Noted() || !d.Config.IsNamed(name, int(it.ID)) {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

func describeGot(got map[string]int) string {
	if len(got) == 0 {
		return "nothing"
	}
	parts := make([]string, 0, len(got))
	for item, n := range got {
		parts = append(parts, itoaKit(n)+" "+item)
	}
	return strings.Join(parts, ", ")
}

func declinedSuffix(declined map[string]string) string {
	if len(declined) == 0 {
		return ""
	}
	parts := make([]string, 0, len(declined))
	for item, why := range declined {
		parts = append(parts, item+": "+why)
	}
	return " (not coming: " + strings.Join(parts, "; ") + ")"
}

func itoaKit(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// kitNewItems is how many of the kit's items the pack does not hold yet: each needs a slot.
func kitNewItems(d *Deps, items []fleetplan.ItemCount) int {
	n := 0
	for _, it := range items {
		held := CountPackUnnoted(d, it.Item)
		if d.Travel.KitKeepsNoted(it.Item) {
			held = CountPackNamed(d, it.Item) // a note stack already held takes no new slot
		}
		if held == 0 {
			n++
		}
	}
	return n
}

// KitRoomKeep narrows a kit deposit's keep list to what a kit cannot do without -- coins, runes,
// staffs, the kit's own items and the caller's extras -- for when the full list left no room.
func KitRoomKeep(keep []string, items []fleetplan.ItemCount, extra []string) []string {
	must := map[string]bool{"coins": true}
	for _, it := range items {
		must[strings.ToLower(strings.TrimSpace(it.Item))] = true
	}
	for _, n := range extra {
		must[strings.ToLower(strings.TrimSpace(n))] = true
	}
	var out []string
	for _, n := range keep {
		l := strings.ToLower(strings.TrimSpace(n))
		if must[l] || fleetplan.IsRuneName(n) || strings.Contains(l, "staff") {
			out = append(out, n)
		}
	}
	return out
}

// packCountsByID is the pack's total of each obj id.
func packCountsByID(d *Deps) map[int32]int {
	out := map[int32]int{}
	for _, it := range PackSlotItems(d) {
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out[it.ID] += c
	}
	return out
}

// kitGaveAndBankRest is what of the kit's hand-over crossed to the mule (by obj name), and banks again, at bank,
// every hand-over stack that came out of the bank whole and is still in the pack (the mule refused the kit, the
// trade failed). A stack drawn onto one the pack already held (coins onto the purse) stays in the pack: there is
// no deposit of part of a stack, and the next deposit before a kit banks it.
func kitGaveAndBankRest(d *Deps, at tile.Tile, give []donorrunner.GiveLine, before map[int32]int) map[string]int {
	now := packCountsByID(d)
	gave := map[string]int{}
	rest := map[int32]bool{}
	for _, g := range give {
		drawn := g.Qty
		left := max(0, now[g.ID]-before[g.ID])
		if n := drawn - left; n > 0 {
			name := fmt.Sprint(g.ID)
			if d.Config != nil {
				if o, ok := d.Config.Obj(int(g.ID)); ok {
					name = o.Name
				}
			}
			gave[name] += n
		}
		if left > 0 && before[g.ID] == 0 {
			rest[g.ID] = true
		}
	}
	if len(rest) > 0 && at != (tile.Tile{}) {
		if bank, ok := OpenBank(d, at); ok {
			_ = bank.DepositAllMatching(func(it gamestate.SlotItem) bool { return rest[it.ID] }, nil)
			_ = bank.Close()
			d.Logf("scarab kit: banked %d hand-over stack(s) the mule did not take", len(rest))
		}
	}
	if len(gave) == 0 {
		return nil
	}
	d.Logf("scarab kit: handed the mule %v back in the kit's own trade", gave)
	return gave
}
