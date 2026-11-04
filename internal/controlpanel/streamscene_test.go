package controlpanel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

func sceneSnapshot() gamestate.RenderSnapshot {
	snap := gamestate.RenderSnapshot{X: 2840, Z: 3432, CentreX: 355, CentreZ: 429, SelfSlot: 7, Appearance: []byte{0, 1, 2}}
	snap.NPCs = []gamestate.NPC{
		{Index: 12, Type: 316, X: 2842, Z: 3433, Anim: -1, FaceEntity: -1},
		{Index: 3, Type: 2000, X: 2838, Z: 3430, Anim: 422, AnimTick: 880, FaceEntity: 32768 + 7},
	}
	snap.Animation = -1
	snap.Players = []gamestate.RenderPlayer{
		{Index: 7, Name: "Me", X: 2840, Z: 3432}, // the account itself must not appear as another player
		{Index: 40, Name: "Neighbour", X: 2841, Z: 3431, Appearance: []byte{9, 8, 7}, Anim: -1, FaceEntity: -1},
	}
	snap.Objs = []gamestate.GroundItem{{ID: 526, Count: 1, X: 2843, Z: 3432}, {ID: 995, Count: 40, X: 2839, Z: 3432}, {ID: 1, Count: 1, X: 2839, Z: 3432, Level: 1}}
	snap.Locs = []gamestate.LocInstance{{Type: 1276, Shape: 10, Rotate: 1, X: 2845, Z: 3435}, {Type: -1, Shape: 0, X: 2840, Z: 3440, Deleted: true}}
	slots := make([]gamestate.Item, 28)
	slots[0] = gamestate.Item{ID: 303, Count: 1}
	slots[1] = gamestate.Item{ID: 317, Count: 1}
	run := 87
	snap.UI = gamestate.RenderUI{Version: 1, Tick: 900, Name: "fisher1", RunEnergy: &run,
		Inventories: map[int]gamestate.Inventory{streamBackpackComponent: {Generation: 3, Slots: slots}, 93: {Slots: []gamestate.Item{{ID: 1, Count: 5}}}}}
	for i := 0; i < 8; i++ {
		snap.UI.Chat = append(snap.UI.Chat, gamestate.RenderChatLine{Type: 0, Text: fmt.Sprintf("line %d", i)})
	}
	return snap
}

func testNames(id int) (string, int) {
	switch id {
	case 316:
		return "Fishing spot", 1
	case 2000:
		return "Big thing", 2
	}
	return "", 0
}

func TestProjectStreamSceneCarriesWhatTheViewDraws(t *testing.T) {
	snap := sceneSnapshot()
	scene := projectStreamScene(&snap, streamStatus{Bot: "ProgressiveFisher", Activity: "fishing"}, testNames)
	if scene.Self.X != 2840 || scene.Self.Z != 3432 || scene.Self.CentreX != 355 || scene.Self.Slot != 7 || scene.Self.Name != "fisher1" || scene.Tick != 900 {
		t.Fatalf("self = %+v tick %d", scene.Self, scene.Tick)
	}
	// Sorted by key, npc names and footprints resolved, the account itself left out.
	want := []streamEntity{
		{Key: "n12", X: 2842, Z: 3433, Type: 316, Name: "Fishing spot"},
		{Key: "n3", X: 2838, Z: 3430, Type: 2000, Size: 2, Name: "Big thing", A: 423, AT: 880, F: 32776},
		{Key: "p40", X: 2841, Z: 3431, Name: "Neighbour"},
	}
	if len(scene.Entities) != len(want) {
		t.Fatalf("entities = %+v", scene.Entities)
	}
	for i := range want {
		if scene.Entities[i] != want[i] {
			t.Errorf("entity %d = %+v, want %+v", i, scene.Entities[i], want[i])
		}
	}
	// Only the backpack, as [id,count] per slot, empty slots zero.
	if len(scene.Inv) != 28 || scene.Inv[0] != [2]int32{303, 1} || scene.Inv[1] != [2]int32{317, 1} || scene.Inv[2] != [2]int32{} {
		t.Errorf("inv = %v", scene.Inv)
	}
	if len(scene.Chat) != streamSceneChatLines || scene.Chat[len(scene.Chat)-1].Text != "line 7" {
		t.Errorf("chat = %+v, want the last %d lines", scene.Chat, streamSceneChatLines)
	}
	if scene.Status.Activity != "fishing" || scene.Status.RunEnergy == nil || *scene.Status.RunEnergy != 87 {
		t.Errorf("status = %+v", scene.Status)
	}
	// Appearance blocks for the account and each kept player, never for the account as
	// "another player"; ground items on this level only, sorted by tile.
	if len(scene.Looks) != 2 || scene.Looks["self"] != "AAEC" || scene.Looks["p40"] != "CQgH" {
		t.Errorf("looks = %v", scene.Looks)
	}
	if len(scene.Objs) != 2 || scene.Objs[0] != [4]int{2839, 3432, 995, 40} || scene.Objs[1] != [4]int{2843, 3432, 526, 1} {
		t.Errorf("objs = %v", scene.Objs)
	}
	// Live loc changes, a removal as type -1.
	if len(scene.Locs) != 2 || scene.Locs[0] != [6]int{2840, 3440, 0, -1, 0, 0} || scene.Locs[1] != [6]int{2845, 3435, 0, 1276, 10, 1} {
		t.Errorf("locs = %v", scene.Locs)
	}
	// No names source (the -fake fleet): ids only, never invented names.
	bare := projectStreamScene(&snap, streamStatus{}, nil)
	if bare.Entities[0].Name != "" || bare.Entities[0].Type != 316 {
		t.Errorf("without a names source got %+v", bare.Entities[0])
	}
}

func TestProjectStreamSceneBoundsACrowdNearestFirst(t *testing.T) {
	snap := gamestate.RenderSnapshot{X: 3200, Z: 3200}
	for i := 0; i < 400; i++ {
		snap.Players = append(snap.Players, gamestate.RenderPlayer{Index: i + 1, X: 3200 + i%30, Z: 3200 + i/30})
	}
	scene := projectStreamScene(&snap, streamStatus{}, nil)
	if len(scene.Entities) != streamSceneMaxEntities {
		t.Fatalf("a 400-player crowd produced %d entities, want the %d cap", len(scene.Entities), streamSceneMaxEntities)
	}
	far := 0
	for _, e := range scene.Entities {
		if e.X-3200 > 20 || e.Z-3200 > 20 {
			far++
		}
	}
	if far != 0 {
		t.Errorf("%d kept entities are farther than 20 tiles while nearer ones were dropped", far)
	}
}

func TestDiffStreamSceneSendsOnlyWhatChanged(t *testing.T) {
	snap := sceneSnapshot()
	prev := projectStreamScene(&snap, streamStatus{Activity: "fishing"}, testNames)
	if d := diffStreamScene(4, &prev, &prev); !d.empty() || d.Base != 4 {
		t.Fatalf("an unchanged scene produced %+v", d)
	}

	// One npc steps, the neighbour walks out of view, a new npc appears, one fish lands in
	// the pack, one chat line arrives. Self, status and the other npc are unchanged.
	snap.NPCs[0].X++
	snap.Players = snap.Players[:1]
	snap.NPCs = append(snap.NPCs, gamestate.NPC{Index: 99, Type: 316, X: 2850, Z: 3440, Anim: -1, FaceEntity: -1})
	// A new snapshot is a fresh copy, as gamestate hands out: never mutate the previous one.
	pack := snap.UI.Inventories[streamBackpackComponent]
	pack.Slots = append([]gamestate.Item(nil), pack.Slots...)
	pack.Slots[2] = gamestate.Item{ID: 317, Count: 1}
	pack.Generation++
	snap.UI.Inventories = map[int]gamestate.Inventory{streamBackpackComponent: pack, 93: snap.UI.Inventories[93]}
	snap.UI.Chat = append(snap.UI.Chat, gamestate.RenderChatLine{Text: "You catch some shrimps."})
	snap.Objs = snap.Objs[1:]
	next := projectStreamScene(&snap, streamStatus{Activity: "fishing"}, testNames)
	d := diffStreamScene(4, &prev, &next)
	if d.Self != nil || d.Status != nil || d.InvLen != nil {
		t.Errorf("unchanged sections were sent: self=%v status=%v invLen=%v", d.Self, d.Status, d.InvLen)
	}
	if len(d.Upsert) != 2 || d.Upsert[0].Key != "n12" || d.Upsert[0].X != 2843 || d.Upsert[1].Key != "n99" || d.Upsert[1].A != 0 {
		t.Errorf("upsert = %+v, want the moved n12 and the new n99", d.Upsert)
	}
	if len(d.Remove) != 1 || d.Remove[0] != "p40" {
		t.Errorf("remove = %v, want [p40]", d.Remove)
	}
	if len(d.Inv) != 1 || d.Inv[0] != [3]int32{2, 317, 1} {
		t.Errorf("inv = %v, want only slot 2", d.Inv)
	}
	if d.Chat == nil || (*d.Chat)[len(*d.Chat)-1].Text != "You catch some shrimps." {
		t.Errorf("chat = %v", d.Chat)
	}
	if len(d.Looks) != 0 {
		t.Errorf("unchanged looks were resent: %v", d.Looks)
	}
	if d.Objs == nil || len(*d.Objs) != 1 {
		t.Errorf("objs = %v, want the one remaining stack", d.Objs)
	}
	if d.Locs != nil {
		t.Errorf("unchanged locs were resent: %v", d.Locs)
	}
	// One fish in the backpack is one changed inventory in the interface section, and the
	// new chat line; nothing else of the interface state is resent.
	if d.UI == nil || len(d.UI.Inventories) != 1 || d.UI.Inventories[streamBackpackComponent] == nil || d.UI.Chat == nil || d.UI.Texts != nil || d.UI.Skills != nil {
		t.Errorf("interface delta = %+v", d.UI)
	}

	// Applying the delta to prev must reproduce next exactly — the property the browser
	// relies on. This is the same apply rule tools/stream/scene-state.js implements.
	got := applyStreamDeltaForTest(prev, d)
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(next)
	if string(a) != string(b) {
		t.Errorf("prev+delta != next:\n got %s\nwant %s", a, b)
	}

	// The account walks and its run energy changes: self and status sections appear.
	snap.X++
	run := 80
	snap.UI.RunEnergy = &run
	walked := projectStreamScene(&snap, streamStatus{Activity: "fishing"}, testNames)
	d = diffStreamScene(5, &next, &walked)
	if d.Self == nil || d.Self.X != 2841 || d.Status == nil || *d.Status.RunEnergy != 80 {
		t.Errorf("walk delta = %+v", d)
	}
	// The same sequence restarted (a new tick) is an upsert: the renderer replays it.
	snap.NPCs[1].AnimTick = 890
	replayed := projectStreamScene(&snap, streamStatus{Activity: "fishing"}, testNames)
	if d := diffStreamScene(6, &walked, &replayed); len(d.Upsert) != 1 || d.Upsert[0].Key != "n3" || d.Upsert[0].AT != 890 {
		t.Errorf("restarted animation delta = %+v", d.Upsert)
	}
	walked = replayed
	// The account changes armour: only its look is resent.
	snap.Appearance = []byte{0, 1, 3}
	dressed := projectStreamScene(&snap, streamStatus{Activity: "fishing"}, testNames)
	d = diffStreamScene(6, &walked, &dressed)
	if len(d.Looks) != 1 || d.Looks["self"] != "AAED" || len(d.Upsert) != 0 || d.Self != nil {
		t.Errorf("appearance delta = %+v", d)
	}
}

// applyStreamDeltaForTest mirrors the browser's apply rule so the round-trip property is
// checked in Go as well as in the browser fixture.
func applyStreamDeltaForTest(s streamScene, d streamDelta) streamScene {
	if d.Self != nil {
		s.Self = *d.Self
	}
	byKey := map[string]streamEntity{}
	for _, e := range s.Entities {
		byKey[e.Key] = e
	}
	for _, e := range d.Upsert {
		byKey[e.Key] = e
	}
	for _, k := range d.Remove {
		delete(byKey, k)
	}
	s.Entities = s.Entities[:0:0]
	for _, e := range byKey {
		s.Entities = append(s.Entities, e)
	}
	sortEntitiesForTest(s.Entities)
	inv := append([][2]int32(nil), s.Inv...)
	if d.InvLen != nil {
		for len(inv) < *d.InvLen {
			inv = append(inv, [2]int32{})
		}
		inv = inv[:*d.InvLen]
	}
	for _, c := range d.Inv {
		inv[c[0]] = [2]int32{c[1], c[2]}
	}
	s.Inv = inv
	if d.Chat != nil {
		s.Chat = *d.Chat
	}
	if d.Status != nil {
		s.Status = *d.Status
	}
	looks := map[string]string{}
	for k, v := range s.Looks {
		if _, gone := byKey[k]; gone || k == "self" {
			looks[k] = v
		}
	}
	for k, v := range d.Looks {
		looks[k] = v
	}
	s.Looks = looks
	if d.Objs != nil {
		s.Objs = *d.Objs
	}
	if d.Locs != nil {
		s.Locs = *d.Locs
	}
	if d.UI != nil {
		s.UI = applyUIDeltaForTest(s.UI, *d.UI)
	}
	return s
}

func applyUIDeltaForTest(u gamestate.RenderUI, d streamUIDelta) gamestate.RenderUI {
	set := func(p *int, v *int) {
		if v != nil {
			*p = *v
		}
	}
	set(&u.Version, d.Version)
	set(&u.Main, d.Main)
	set(&u.Side, d.Side)
	set(&u.ChatModal, d.ChatModal)
	if d.Name != nil {
		u.Name = *d.Name
	}
	inv := map[int]gamestate.Inventory{}
	for k, v := range u.Inventories {
		inv[k] = v
	}
	for k, v := range d.Inventories {
		if v == nil {
			delete(inv, k)
		} else {
			inv[k] = *v
		}
	}
	u.Inventories = inv
	u.Texts = applyMapForTest(u.Texts, d.Texts)
	u.Colours = applyMapForTest(u.Colours, d.Colours)
	u.Varps = applyMapForTest(u.Varps, d.Varps)
	u.SideIcons = applyMapForTest(u.SideIcons, d.SideIcons)
	u.Objects = applyMapForTest(u.Objects, d.Objects)
	if d.Skills != nil {
		u.Skills = *d.Skills
	}
	if d.Chat != nil {
		u.Chat = *d.Chat
	}
	if d.RunEnergy != nil {
		u.RunEnergy = d.RunEnergy
	}
	if d.Weight != nil {
		u.Weight = d.Weight
	}
	return u
}

func applyMapForTest[V any](m map[int]V, d map[int]*V) map[int]V {
	out := map[int]V{}
	for k, v := range m {
		out[k] = v
	}
	for k, v := range d {
		if v == nil {
			delete(out, k)
		} else {
			out[k] = *v
		}
	}
	if len(out) == 0 && m == nil {
		return nil
	}
	return out
}

func sortEntitiesForTest(es []streamEntity) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && es[j].Key < es[j-1].Key; j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}

func TestSceneDeltaTransportResyncsAndConfirmsItsBase(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	src := &renderSource{
		staticSource: &staticSource{accounts: []AccountStatus{{Name: "fisher1", State: StateIngame, SinceUnixMs: 1000, Pos: &Position{X: 2840, Z: 3432}, Bot: "ProgressiveFisher", Activity: "fishing"}}},
		snapshot:     sceneSnapshot(),
	}
	h := streamHarness(t, src, &at)

	if res := h.postJSON("/api/stream/open", map[string]string{"account": "fisher1", "transport": "carrier-pigeon"}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown transport = %d, want 400", res.StatusCode)
	}
	var open streamSessionBody
	decode(t, h.postJSON("/api/stream/open", map[string]interface{}{"account": "fisher1", "transport": "scene-delta"}), &open)
	if open.Transport != streamTransportScene {
		t.Fatalf("open transport = %q", open.Transport)
	}
	frame := func(have int64) streamFrame {
		t.Helper()
		at = at.Add(time.Second / DefaultStreamMaxFPS)
		var f streamFrame
		decode(t, h.get(fmt.Sprintf("/api/stream/frame?session=%s&have=%d", open.Session, have)), &f)
		if f.Snapshot != nil {
			t.Fatalf("a scene-delta frame carried the whole RenderSnapshot too")
		}
		return f
	}

	first := frame(0)
	if first.Scene == nil || first.Delta != nil || first.Transport != streamTransportScene || first.Tick != 900 {
		t.Fatalf("first frame = %+v, want a whole scene", first)
	}
	if first.Scene.Status.Bot != "ProgressiveFisher" || first.Scene.Status.Activity != "fishing" {
		t.Errorf("status line = %+v, want the account list's bot and activity", first.Scene.Status)
	}
	// Confirming the base gets a delta; nothing changed, so it is empty.
	second := frame(first.Seq)
	if second.Delta == nil || second.Scene != nil || second.Delta.Base != first.Seq || !second.Delta.empty() {
		t.Fatalf("second frame = %+v, want an empty delta on base %d", second, first.Seq)
	}
	// Something moves: the delta carries just that.
	src.mu.Lock()
	src.snapshot.NPCs = append([]gamestate.NPC(nil), src.snapshot.NPCs...)
	src.snapshot.NPCs[0].Z++
	src.mu.Unlock()
	third := frame(second.Seq)
	if third.Delta == nil || len(third.Delta.Upsert) != 1 || third.Delta.Upsert[0].Z != 3434 {
		t.Fatalf("third frame = %+v", third)
	}
	// A browser that did not apply the last frame (a lost response) names an older base:
	// it gets a whole scene, never a delta against something it does not hold.
	lost := frame(second.Seq)
	if lost.Scene == nil || lost.Delta != nil {
		t.Fatalf("a stale base got %+v, want a whole scene", lost)
	}
	// Even a browser that confirms every frame gets a whole scene at least every resync.
	have, wholes := lost.Seq, 0
	for i := 0; i < int(streamSceneResync/(time.Second/DefaultStreamMaxFPS))+2; i++ {
		f := frame(have)
		if f.Scene != nil {
			wholes++
		}
		have = f.Seq
	}
	if wholes != 1 {
		t.Errorf("%d whole scenes across one resync window plus two frames, want exactly 1", wholes)
	}

	// The default transport is unchanged: whole snapshots, no scene.
	h.postJSON("/api/stream/close", map[string]string{"session": open.Session})
	decode(t, h.postJSON("/api/stream/open", map[string]string{"account": "fisher1"}), &open)
	if open.Transport != streamTransportFull {
		t.Fatalf("default transport = %q", open.Transport)
	}
	at = at.Add(time.Second)
	var full streamFrame
	decode(t, h.get("/api/stream/frame?session="+open.Session), &full)
	if full.Snapshot == nil || full.Scene != nil || full.Delta != nil {
		t.Fatalf("full-snapshot frame = %+v", full)
	}
}

func TestMetaAdvertisesBothFrameTransports(t *testing.T) {
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("fisher1")}}}
	h := streamHarness(t, src, nil)
	var meta struct {
		StreamClient StreamClientCapabilities `json:"streamClient"`
	}
	decode(t, h.get("/api/meta"), &meta)
	got := meta.StreamClient
	if got.FrameTransport != streamTransportFull || len(got.FrameTransports) != 2 || got.FrameTransports[1] != streamTransportScene {
		t.Errorf("frame transports = %q / %q", got.FrameTransport, got.FrameTransports)
	}
	if got.SessionInput {
		t.Error("sessionInput must stay false: nothing implements StreamInputSink")
	}
}

func TestSceneDeltaFrameSizeIsMeasuredNotAssumed(t *testing.T) {
	// A recorded measurement, not a threshold: the same crowd TestStreamFrameSizeIsMeasured
	// uses (32 npcs, 8 players, 200 locs), sent whole as a full snapshot, as a whole compact
	// scene, and as the delta for one ordinary tick (three npcs step, one fish lands).
	snap := gamestate.RenderSnapshot{X: 3222, Z: 3218, CentreX: 402, CentreZ: 402, UI: gamestate.RenderUI{Version: 1, Tick: 10}}
	for i := 0; i < 32; i++ {
		snap.NPCs = append(snap.NPCs, gamestate.NPC{Index: i, Type: 1000 + i, X: 3220 + i, Z: 3210 + i, Anim: -1, FaceEntity: -1})
	}
	for i := 0; i < 8; i++ {
		snap.Players = append(snap.Players, gamestate.RenderPlayer{Index: 100 + i, Name: fmt.Sprintf("player%d", i), X: 3220 + i, Z: 3210 + i, Appearance: make([]byte, 64)})
	}
	for i := 0; i < 200; i++ {
		snap.Locs = append(snap.Locs, gamestate.LocInstance{Type: 1276, Shape: 10, X: 3200 + i%64, Z: 3200 + i/64})
	}
	slots := make([]gamestate.Item, 28)
	for i := 0; i < 14; i++ {
		slots[i] = gamestate.Item{ID: 317, Count: 1}
	}
	snap.UI.Inventories = map[int]gamestate.Inventory{streamBackpackComponent: {Slots: slots}}
	names := func(id int) (string, int) { return fmt.Sprintf("Npc %d", id), 1 }
	full, _ := json.Marshal(streamFrame{Seq: 1, Epoch: 1, Account: "fisher1", Snapshot: &snap, Transport: streamTransportFull})
	prev := projectStreamScene(&snap, streamStatus{Bot: "ProgressiveFisher", Activity: "fishing"}, names)
	whole, _ := json.Marshal(streamFrame{Seq: 1, Epoch: 1, Account: "fisher1", Scene: &prev, Tick: 10, Transport: streamTransportScene})
	for i := 0; i < 3; i++ {
		snap.NPCs[i].X++
	}
	snap.UI.Inventories[streamBackpackComponent].Slots[14] = gamestate.Item{ID: 317, Count: 1}
	snap.UI.Tick++
	next := projectStreamScene(&snap, streamStatus{Bot: "ProgressiveFisher", Activity: "fishing"}, names)
	d := diffStreamScene(1, &prev, &next)
	delta, _ := json.Marshal(streamFrame{Seq: 2, Epoch: 1, Account: "fisher1", Delta: &d, Tick: 11, Transport: streamTransportScene})
	empty := streamDelta{Base: 2}
	idle, _ := json.Marshal(streamFrame{Seq: 3, Epoch: 1, Account: "fisher1", Delta: &empty, Tick: 11, Transport: streamTransportScene})
	t.Logf("full-snapshot frame %d bytes; scene-delta whole scene %d bytes, one-tick delta %d bytes, unchanged %d bytes; at %d observations/s: %.1f KB/s full vs %.1f KB/s delta",
		len(full), len(whole), len(delta), len(idle), DefaultStreamMaxFPS, float64(len(full)*DefaultStreamMaxFPS)/1024, float64(len(delta)*DefaultStreamMaxFPS)/1024)
	if len(delta) >= len(whole) || len(whole) >= len(full) {
		t.Errorf("sizes out of order: full %d, whole scene %d, delta %d", len(full), len(whole), len(delta))
	}
}
