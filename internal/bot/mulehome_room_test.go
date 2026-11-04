package bot

import (
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"testing"
)

type muleRoomTypes map[int]cachecfg.Obj

func (m muleRoomTypes) Obj(id int) (cachecfg.Obj, bool) { o, ok := m[id]; return o, ok }

func muleRoomPack(s *apiSession, ids []int) {
	w := packet.NewWriter(256)
	w.P2(3214)
	w.P2(uint16(len(ids)))
	for _, id := range ids {
		w.P2(uint16(id + 1))
		w.P1(1)
	}
	s.st.Handle(serverprot.UPDATE_INV_FULL, w.Data[:w.Pos])
}

func TestMuleFareRoomRecovery(t *testing.T) {
	types := muleRoomTypes{
		385: {Name: "Shark", CertTemplate: -1, IOps: [5]string{"Eat"}},
		387: {Name: "Burnt shark", CertTemplate: -1},
		995: {Name: "Coins", CertTemplate: -1},
		99:  {Name: "valuable stock", CertTemplate: -1},
	}
	for _, tc := range []struct {
		name                    string
		item                    int
		confirmed, cancel, want bool
		packets                 int
	}{
		{"burnt slot clears", 387, true, false, true, 1},
		{"food consumed", 385, true, false, true, 1},
		{"existing coin stack", 995, false, false, true, 0},
		{"preserve other stock", 99, false, false, false, 0},
		{"cancelled click does not claim room", 387, false, true, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAPISession()
			ctx := NewContext(s, "test", nil, nil, nil, nil, nil, nil, nil, nil, func(string) {})
			ids := make([]int, 28)
			for i := range ids {
				ids[i] = tc.item
			}
			muleRoomPack(s, ids)
			s.afterSend = func() {
				if tc.confirmed {
					muleRoomPack(s, ids[:27])
				}
				if tc.cancel {
					ctx.requestStop()
				}
			}
			if got := muleMakeFareRoom(ctx, types); got != tc.want {
				t.Fatalf("room=%v want %v", got, tc.want)
			}
			if got := len(s.sentPackets()); got != tc.packets {
				t.Fatalf("packets=%d want %d", got, tc.packets)
			}
		})
	}
}

func TestMuleFareClearPrefersWasteAndRejectsNotes(t *testing.T) {
	types := muleRoomTypes{1: {Name: "Shark", CertTemplate: -1, IOps: [5]string{"Eat"}}, 2: {Name: "Burnt shark", CertTemplate: -1}, 3: {Name: "Shark", CertTemplate: 799, IOps: [5]string{"Eat"}}}
	items := []gamestate.SlotItem{{Slot: 0, Item: gamestate.Item{ID: 1, Count: 1}}, {Slot: 1, Item: gamestate.Item{ID: 2, Count: 1}}}
	it, op := muleFareClearItem(items, types)
	if it.ID != 2 || op != 5 {
		t.Fatal("must clear burnt fish before cooked food")
	}
	items[0].ID = 3
	if _, op := muleFareClearItem(items[:1], types); op != 0 {
		t.Fatal("must not eat or drop notes")
	}
}

func TestMuleHomeHonoursStopBeforePlanning(t *testing.T) {
	ctx := NewContext(nil, "test", nil, nil, nil, nil, nil, nil, nil, nil, nil)
	ctx.requestStop()
	if (&muleBot{}).returnHome(ctx) {
		t.Fatal("cancelled recovery succeeded")
	}
}

func TestMuleFareRoomUnknownPackAndConfig(t *testing.T) {
	s := newAPISession()
	ctx := NewContext(s, "test", nil, nil, nil, nil, nil, nil, nil, nil, func(string) {})
	if muleMakeFareRoom(ctx, nil) {
		t.Fatal("an unobserved pack must not be treated as empty")
	}
	ids := make([]int, 28)
	for i := range ids {
		ids[i] = 387
	}
	muleRoomPack(s, ids)
	var cfg *cachecfg.Config
	if muleMakeFareRoom(ctx, cfg) || len(s.sentPackets()) != 0 {
		t.Fatal("unknown item types must not produce item actions")
	}
}
