package crafterrunner

// The mule flax trip (crafterplan.MuleFlax): one bundled kit ask for flax at the mule's post,
// through the same scarabrunner.RequestKitBundle every other kit asker uses (ScarabSwarm,
// BowStringer, MasterCrafter's rounds), and the notes banked at the bank beside the post. A bank is
// one bank everywhere, so the flax is drawn back as items at the Seers bank by the ordinary
// DrawFlax step; nothing is carried across as notes.

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
)

// FlaxKit is what one mule flax trip came to.
type FlaxKit struct {
	// Got is the flax that arrived, notes and items alike.
	Got int
	// Declined is the mule's reason for the flax line, "" when it was served.
	Declined string
	// Banked is whether the bank beside the post was opened after the trade and the pack deposited;
	// BankFlax is the flax the bank then held (-1 when it was not opened).
	Banked   bool
	BankFlax int
	// Bundled is false when the mule never answered the kit order and the per-item fallback ran.
	Bundled bool
	Stopped bool
	Reason  string
}

// FetchFlaxKit asks mule for qty flax in one kit at post (the account must already be there), then
// banks the notes at the bank beside it. Everything but SeersKeep goes into the bank around the ask,
// so the kit's room and the bank's flax count are both read from a clean pack. A zero post asks
// without banking (a harness with no bank). timeout bounds the kit ask, and the per-item fallback
// for a mule that never answers kit orders at the same or 90 s, whichever is shorter.
func FetchFlaxKit(d *fleetrunner.Deps, bus *mulebus.Bus, me, mule string, qty int, post tile.Tile, world int, timeout time.Duration, keepExtra ...string) FlaxKit {
	out := FlaxKit{BankFlax: -1}
	keep := append(append([]string(nil), crafterplan.SeersKeep()...), keepExtra...) // + the run staller's jewellery
	items := []fleetplan.ItemCount{{Item: crafterplan.Flax, Count: qty}}
	res, info := fleetrunner.RequestKitBundle(d, bus, me, mule, items, fleetrunner.KitBundleOptions{
		Timeout: timeout, Bank: post, Keep: keep, KeepNotes: true,
		PerOrder: qty, FallbackTimeout: min(timeout, 90*time.Second), World: world,
	})
	out.Bundled, out.Stopped, out.Reason = info.Bundled, info.Stopped, info.Reason
	if out.Stopped {
		return out
	}
	out.Got = lookup(res.Got, crafterplan.Flax) + lookup(info.NotedHeld, crafterplan.Flax)
	for item, why := range res.Declined {
		if strings.EqualFold(strings.TrimSpace(item), crafterplan.Flax) {
			out.Declined = why
		}
	}
	if post == (tile.Tile{}) {
		return out
	}
	bank, ok := fleetrunner.OpenBank(d, post)
	if !ok {
		return out
	}
	defer func() { _ = bank.Close() }()
	donorrunner.DepositForKit(fleetrunner.MuleDeps(d, bus, me), bank, keep)
	out.Banked = true
	out.BankFlax = bankFlax(d, bank)
	// The bank list can trail the deposit by a tick; a kit that arrived is worth a short wait.
	for i := 0; i < 10 && out.Got > 0 && out.BankFlax <= 0; i++ {
		if !d.Delay(300 * time.Millisecond) {
			break
		}
		out.BankFlax = bankFlax(d, bank)
	}
	return out
}

func lookup(m map[string]int, name string) int {
	n := 0
	for k, v := range m {
		if strings.EqualFold(strings.TrimSpace(k), name) {
			n += v
		}
	}
	return n
}

// bankFlax is the flax an open bank holds (a bank stores the item, never its note).
func bankFlax(d *fleetrunner.Deps, bank *actions.Bank) int {
	n := 0
	for _, it := range bank.Items() {
		if d.Config == nil {
			break
		}
		if o, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(o.Name), crafterplan.Flax) {
			n += int(it.Count)
		}
	}
	return n
}

// NotedFlaxCount is the flax the pack holds as bank notes: a kit the trip could not bank.
func NotedFlaxCount(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		o, ok := d.Config.Obj(int(it.ID))
		if !ok || !o.Noted() || !strings.EqualFold(strings.TrimSpace(o.Name), crafterplan.Flax) {
			continue
		}
		n += max(1, int(it.Count))
	}
	return n
}
