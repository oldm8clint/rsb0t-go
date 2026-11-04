package controlpanel

import (
	"errors"
	"fmt"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
)

// RenderSnapshot implements RenderSource for the simulated fleet, so the stream client's
// existing-session bridge (streamclient.go), the provider contract and the panel can all
// be exercised end to end under `-fake` with no game connection at all.
//
// Every value in here is SYNTHETIC. It is shaped exactly like a real
// gamestate.RenderSnapshot — same fields, same absolute-coordinate convention, same
// per-frame movement — because the point is to exercise the transport, the lease, the
// epoch check and a provider's own geometry path, not to look like a real world. Nothing
// a browser draws from this proves anything about a real account, and a fixture built on
// it must say so. The real Source (FleetSource) reads its snapshot off an actual
// already-connected session instead; see capture.go.
//
// Deliberately NOT marked in the JSON as fake: the snapshot type belongs to
// internal/gamestate and adding a "this one is pretend" field to it would put a test
// concern in the game-state package. cmd/controlpanel already prints "simulating N
// accounts; nothing connects to a game server" at startup, and the panel's own -fake mode
// is what an operator chose.
func (f *FakeSource) RenderSnapshot(name string) (gamestate.RenderSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.accounts {
		a := &f.accounts[i]
		if a.Name != name {
			continue
		}
		if a.State != StateIngame {
			return gamestate.RenderSnapshot{}, fmt.Errorf("account is %s, not in game", a.State)
		}
		if a.Pos == nil {
			return gamestate.RenderSnapshot{}, errors.New("account scene is unavailable")
		}
		return f.fakeSnapshotLocked(a, i), nil
	}
	return gamestate.RenderSnapshot{}, errors.New("unknown account")
}

func (f *FakeSource) fakeSnapshotLocked(a *AccountStatus, index int) gamestate.RenderSnapshot {
	x, z, level := a.Pos.X, a.Pos.Z, a.Pos.Level
	// Scene centre is in ZONE units (8-tile), exactly as a live snapshot reports it:
	// gamestate copies REBUILD_NORMAL's zone coordinates, and the capture adapter replays
	// them as that packet's payload. An earlier version sent the absolute zone-base TILE
	// here, which pointed the adapter's map rebuild at a region ~8x off the world.
	out := gamestate.RenderSnapshot{
		X: x, Z: z, Level: level,
		CentreX: x / 8, CentreZ: z / 8,
		SelfSlot: 1, Animation: -1,
	}
	// A small ring of synthetic neighbours, stepping with the clock so successive frames
	// genuinely differ and a provider's frame-to-frame handling is exercised.
	step := f.now().UnixNano() / int64(1e9)
	for i := 0; i < 4; i++ {
		out.NPCs = append(out.NPCs, gamestate.NPC{
			Index: i + 1, Type: 1 + i, Level: level,
			X:    x + 2 + i - int(step+int64(i))%3,
			Z:    z + 1 + i + int(step+int64(i*2))%3,
			Anim: -1, FaceEntity: -1,
		})
	}
	for i := 0; i < 2; i++ {
		out.Players = append(out.Players, gamestate.RenderPlayer{Index: 100 + i, Name: fmt.Sprintf("Sim player %d", i+1), X: x - 2 - i, Z: z - 1 - i, Anim: -1, FaceEntity: -1})
	}
	f.fakeFightLocked(&out)
	for i := 0; i < 6; i++ {
		out.Locs = append(out.Locs, gamestate.LocInstance{Type: 1276 + i, Shape: 10, Rotate: i % 4, X: x - 3 + i, Z: z + 3, Level: level})
	}
	// EVERY "no interface open" slot must be the client's own -1 sentinel, including
	// Overlay. This used to leave Overlay at Go's zero value, and 0 is a real interface:
	// a renderer replaying this snapshot drew interface 0 (Thessalias' Makeovers in this
	// revision) across the whole world viewport on every fake frame, so every -fake proof
	// of the stream view was taken through a modal that no session had open. A live
	// session never hit it — gamestate.State starts OverlayID at NoModal — which is
	// exactly why it survived: the fixture, not the product, was wrong.
	// The game's tick is 600 ms; a viewer plays movement and combat on that clock, so the simulated
	// session must tick at the same rate for those to look right.
	out.UI = gamestate.RenderUI{Version: 1, Tick: int(f.now().UnixMilli() / 600), Name: a.Name,
		Main: gamestate.NoModal, Side: gamestate.NoModal, ChatModal: gamestate.NoModal, Overlay: gamestate.NoModal}
	// The sidebar tabs. A real session learns these from IF_SETICON on login (see
	// gamestate.applyIfSetIcon) and the reference client draws a tab's icon, and the
	// panel under it, only for a tab whose interface it knows. With this map empty the
	// whole right-hand side of the game window renders as an EMPTY stone frame — no tab
	// icons, no inventory — which is indistinguishable from the interface layer being
	// broken, and is what an operator looking at a -fake panel would report as such.
	// Only tab 3 is claimed here, because 3213 is the one id this repository has pinned
	// from a real IF_SETICON (gamestate/render_ui_test.go's fixture, and the backpack
	// layer actions/equipment.go resolves); the rest stay unknown rather than invented.
	out.UI.SideIcons = map[int]int{3: streamBackpackLayer}
	// A synthetic backpack that fills one slot every few seconds and a chat line that
	// changes with it, so the stream client's inventory and chat chrome, and the delta
	// transport's slot/chat diffs, have something to follow under -fake. Item 317 is raw
	// shrimps and 303 a small fishing net in this revision's cache; the counts are invented.
	fish := int(step/3) % 27
	slots := make([]gamestate.Item, 28)
	slots[0] = gamestate.Item{ID: 303, Count: 1}
	for i := 1; i <= fish; i++ {
		slots[i] = gamestate.Item{ID: 317, Count: 1}
	}
	out.UI.Inventories = map[int]gamestate.Inventory{streamBackpackComponent: {Generation: 1, Slots: slots}}
	out.UI.Chat = []gamestate.RenderChatLine{{Type: 0, Text: fmt.Sprintf("You catch some shrimps. (synthetic, %d in the pack)", fish)}}
	if fakeAccountBanks(index) {
		f.fakeBankModalLocked(&out.UI, step)
	}
	return out
}

// fakeAppearance is a plain male player's appearance block (the bytes of an APPEARANCE update
// including its tail): body kits for torso, arms, legs, hair, hands, feet and beard, a bronze
// sword and a wooden shield, and the standard movement sequences. Synthetic, like everything here.
func fakeAppearance(weapon bool) []byte {
	b := []byte{0, 0} // gender, headicon
	part := func(v int) {
		if v == 0 {
			b = append(b, 0)
			return
		}
		b = append(b, byte(v>>8), byte(v))
	}
	held := func(item int) int {
		if weapon {
			return 512 + item
		}
		return 0
	}
	for _, v := range []int{0, 0, 0, held(1277), 256 + 18, held(1171), 256 + 26, 256 + 36, 256, 256 + 33, 256 + 42, 256 + 10} {
		part(v)
	}
	b = append(b, 3, 7, 5, 0, 2)
	for _, anim := range []int{808, 823, 819, 820, 821, 822, 824} {
		b = append(b, byte(anim>>8), byte(anim))
	}
	// The block's tail: the name (a base-37 long), combat level and skill level.
	b = append(b, 0, 0, 0, 0, 0, 0, 0x2f, 0x51, 3, 0, 0)
	return b
}

// fakeFightLocked gives the simulated world something to draw that a real one has all day: a
// player fighting the first npc, hitmarks and a health bar on both, a spot animation and a
// projectile in flight, and looks for the players, so the stream view's animations and combat
// effects can be seen and measured under -fake. A round is three game ticks.
func (f *FakeSource) fakeFightLocked(out *gamestate.RenderSnapshot) {
	tick := int(f.now().UnixMilli() / 600)
	round := tick - tick%3
	out.Appearance = fakeAppearance(true)
	for i := range out.Players {
		out.Players[i].Appearance = fakeAppearance(i == 0)
	}
	if len(out.NPCs) == 0 || len(out.Players) == 0 {
		return
	}
	npc, player := &out.NPCs[0], &out.Players[0]
	// The fighters stand still and face each other; the attack sequences restart each round.
	npc.X, npc.Z = out.X+2, out.Z+1
	player.X, player.Z = out.X+1, out.Z+1
	player.Anim, player.AnimTick, player.FaceEntity = 422, round, npc.Index
	npc.Anim, npc.AnimTick, npc.FaceEntity = 422, round, 32768+player.Index
	hit := func(damage, kind int) gamestate.RenderCombat {
		c := gamestate.RenderCombat{Health: 12 - tick%5, Total: 20, Tick: round, Seen: true}
		c.Hits[0] = gamestate.RenderHit{Damage: damage, Type: kind, Tick: round, Seen: true}
		if tick%6 < 3 {
			c.Hits[1] = gamestate.RenderHit{Damage: 3, Type: 1, Tick: round, Seen: true}
		}
		return c
	}
	npcCombat := hit(7, 1)
	npc.Combat = &npcCombat
	player.Combat = hit(0, 0)
	out.Combat = hit(2, 1)
	out.Animation, out.AnimationTick = 422, round
	// A spot animation on the npc and a projectile from the account to it, once per round.
	out.Effects = []gamestate.RenderEffect{
		{ID: 90, Tick: round, Level: out.Level, X: npc.X, Z: npc.Z, Height: 100, End: 250},
		{ID: 15, Tick: round, Level: out.Level, X: out.X, Z: out.Z, ToX: npc.X, ToZ: npc.Z, Target: npc.Index + 1, Height: 100, EndHeight: 60, Start: 0, End: 40, Slope: 16, Arc: 64, Projectile: true},
	}
}

// fakeAccountBanks names the fake accounts that hold a bank open. A MAIN modal is the one
// part of the interface layer nothing else under -fake exercised — the side panel and the
// chatbox are drawn from the tab and chat state above, but a bank, shop or trade is drawn
// from a main modal, and that is the path the era skins' declared 2004 fallback turns on
// (tools/stream/scene-chrome-classic.js's mainOpen). It is every third account rather than
// a clock cycle so a fixture can name the account and get the same answer every run.
func fakeAccountBanks(index int) bool { return index%3 == 2 }

// fakeBankModalLocked opens the synthetic bank. The ids are this content build's, pinned
// in internal/actions/bank.go (5292 main, 5382 the withdraw-side inventory) and in the
// render_ui fixture (5383 the title). Side is deliberately left at NoModal: the bank's
// DEPOSIT-side component is not a fixed literal in this build — actions.Bank resolves it
// from IF_OPENMAIN_SIDE against the interface archive each time — so the sidebar keeps the
// backpack tab rather than this fixture inventing an id the game never sent.
func (f *FakeSource) fakeBankModalLocked(ui *gamestate.RenderUI, step int64) {
	ui.Main = actions.BankMainModal
	bank := make([]gamestate.Item, 48)
	for i := range bank {
		bank[i] = gamestate.Item{ID: fakeBankItems[i%len(fakeBankItems)], Count: int32(1 + (step+int64(i))%97)}
	}
	ui.Inventories[actions.BankWithdraw] = gamestate.Inventory{Generation: 1, Slots: bank}
	ui.Texts = map[int]string{fakeBankTitleComponent: "Bank of RuneScape (synthetic)"}
}

// fakeBankTitleComponent is the bank window's own title text component, 5383 — the id the
// render_ui fixture captured carrying "Bank of RuneScape" from a real session.
const fakeBankTitleComponent = 5383

// streamBackpackLayer is the backpack tab's ROOT LAYER, which is what IF_SETICON names for
// tab 3; streamBackpackComponent (3214) is the inv component inside it. See
// internal/actions/equipment.go, which resolves 3214 as layer 3213's only inv child.
const streamBackpackLayer = 3213

// fakeBankItems are ordinary stackable and non-stackable objs of this revision, so the
// synthetic bank draws a mix of item models rather than 48 of one icon.
var fakeBankItems = []int32{317, 315, 995, 1511, 1521, 440, 453, 1739, 314, 1265}
