package donorrunner

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/orderplan"
)

// content 289 gives a strung bow and its unstrung form ONE display name: Yew shortbow is obj 857
// strung and obj 68 unstrung, each with its own bank note.
const (
	kitYewShortUnstrung, kitYewShortUnstrungNote = 68, 69
	kitYewShortStrung, kitYewShortStrungNote     = 857, 858
	qualUnstrung                                 = "Yew shortbow id68"
)

// A kit asked for the UNSTRUNG Yew shortbow by its qualified name is received when the mule hands
// exactly that obj, although the trade screen names it "Yew shortbow" like the strung one, and the
// receipt counts the note by id.
func TestAwaitKitReceivesTheUnstrungBowsItAskedForByQualifiedName(t *testing.T) {
	cfg := kitConfig(t)
	if o, _ := cfg.Obj(kitYewShortUnstrungNote); o.CertLink != kitYewShortUnstrung || o.Name != "Yew shortbow" {
		t.Fatalf("fixture: obj %d is %+v, want the note of the unstrung Yew shortbow", kitYewShortUnstrungNote, o)
	}
	sim := actionsim.New(simObjs(cfg), nil)
	sim.ShowPlayers("mule")
	chat := chatlog.New()
	bus := mulebus.NewBus()
	m := &fakeKitMule{sim: sim, bus: bus, chat: chat,
		plan:   []mulebus.KitLine{{Item: qualUnstrung, Qty: 200, Noted: true}, {Item: "Bow string", Qty: 300, Noted: true}},
		theirs: []actionsim.Stack{{ID: kitYewShortUnstrungNote, Count: 200}, {ID: 1778, Count: 300}}}
	stop := make(chan struct{})
	defer close(stop)
	m.start(stop)
	d := &Deps{Session: sim, Config: cfg, Chat: chat, Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "main"}
	res := AwaitKit(d, KitAsk{Mule: "mule", Me: "main", Bus: bus, Timeout: 20 * time.Second,
		Lines: []orderplan.KitLine{{Item: qualUnstrung, Qty: 200, Noted: true}, {Item: "Bow string", Qty: 300, Noted: true}}})
	if !res.Answered || res.Got[qualUnstrung] != 200 || res.Noted[qualUnstrung] != 200 || res.Got["Bow string"] != 300 {
		t.Fatalf("result %+v", res)
	}
	if len(res.Declined) != 0 {
		t.Fatalf("declined %v", res.Declined)
	}
	if count(sim.Pack(), kitYewShortUnstrungNote) != 200 || count(sim.Pack(), kitYewShortStrungNote) != 0 {
		t.Fatalf("pack %+v", sim.Pack())
	}
}

// THE LIVE BUG, from the donor's side (6 Oct 2026, kit 3: "asking the mule for 55634 Yew shortbow",
// then "70079 item(s) arrived, bank can now fund 0 Yew shortbow"): asked for the unstrung bows, the
// donor was handed 55,634 noted STRUNG ones and took them, because the trade screen names both
// "Yew shortbow". Asked by qualified name it walks away from a screen holding the other obj, for a
// kit and for a single order alike; asked by the plain name it takes either, as it always did.
func TestTheScreenOfTheStrungBowIsNotTheUnstrungKitThatWasAskedFor(t *testing.T) {
	cfg := kitConfig(t)
	d := &Deps{Config: cfg}
	screen := func(names askedNames, id int32) []orderplan.ItemCount {
		return handoutRows(d, []gamestate.SlotItem{{Item: gamestate.Item{ID: id, Count: 200}}}, names)
	}
	kit := func(item string, names askedNames, id int32) (orderplan.ReceiveAction, string) {
		return orderplan.DecideReceiveKit(orderplan.KitReceiveView{
			Screen: orderplan.ScreenOffer, Partner: "mule", Mule: "mule", Planned: true,
			Want: []orderplan.KitLine{{Item: item, Qty: 200, Noted: true}}, Theirs: screen(names, id),
		})
	}
	single := func(item string, names askedNames, id int32) (orderplan.ReceiveAction, string) {
		return orderplan.DecideReceive(orderplan.ReceiveView{
			Screen: orderplan.ScreenOffer, Partner: "mule", Mule: "mule",
			Want: orderplan.Order{Item: item, Qty: 200}, Theirs: screen(names, id),
		})
	}
	qual := newAskedNames(cfg, qualUnstrung)
	for _, ask := range []func(string, askedNames, int32) (orderplan.ReceiveAction, string){kit, single} {
		if act, why := ask(qualUnstrung, qual, kitYewShortUnstrungNote); act != orderplan.ReceiveAccept {
			t.Errorf("the note of the unstrung bow that was asked for: %q (%s)", act, why)
		}
		if act, why := ask(qualUnstrung, qual, kitYewShortUnstrung); act != orderplan.ReceiveAccept {
			t.Errorf("the unstrung bow that was asked for: %q (%s)", act, why)
		}
		for _, other := range []int32{kitYewShortStrungNote, kitYewShortStrung} {
			if act, why := ask(qualUnstrung, qual, other); act != orderplan.ReceiveDecline || why == "" {
				t.Errorf("obj %d, the strung bow, for an ask of the unstrung one: %q (%s), want a decline", other, act, why)
			}
		}
		// The plain name takes whichever obj of the name is put on the screen.
		if act, why := ask("Yew shortbow", nil, kitYewShortStrungNote); act != orderplan.ReceiveAccept {
			t.Errorf("a plain ask for the strung bow's note: %q (%s)", act, why)
		}
	}
}

// The same ask for the PLAIN name still matches by display name, as it always did: a kit that names
// "Yew shortbow" takes whichever obj of that name the mule puts on the screen.
func TestAwaitKitStillMatchesAPlainNameByDisplayName(t *testing.T) {
	cfg := kitConfig(t)
	sim := actionsim.New(simObjs(cfg), nil)
	sim.ShowPlayers("mule")
	chat := chatlog.New()
	bus := mulebus.NewBus()
	m := &fakeKitMule{sim: sim, bus: bus, chat: chat,
		plan:   []mulebus.KitLine{{Item: "Yew shortbow", Qty: 200, Noted: true}},
		theirs: []actionsim.Stack{{ID: kitYewShortStrungNote, Count: 200}}}
	stop := make(chan struct{})
	defer close(stop)
	m.start(stop)
	d := &Deps{Session: sim, Config: cfg, Chat: chat, Log: func(string) {}, Delay: fastDelay, Bus: bus, Me: "main"}
	res := AwaitKit(d, KitAsk{Mule: "mule", Me: "main", Bus: bus, Timeout: 20 * time.Second,
		Lines: []orderplan.KitLine{{Item: "Yew shortbow", Qty: 200, Noted: true}}})
	if res.Got["Yew shortbow"] != 200 {
		t.Fatalf("result %+v", res)
	}
}

// A qualified name in the keep list keeps that obj and its note through DepositForKit, and nothing
// else that shares its display name; a plain name keeps every obj of it, as ever.
func TestDepositForKitKeepsAQualifiedObjAndItsNoteOnly(t *testing.T) {
	cfg := kitConfig(t)
	pack := []actionsim.Stack{
		{ID: kitYewShortUnstrungNote, Count: 50}, {ID: kitYewShortUnstrung, Count: 1},
		{ID: kitYewShortStrungNote, Count: 70}, {ID: kitBones, Count: 1},
	}
	for _, tc := range []struct {
		keep             []string
		unstrung, strung bool // still in the pack (note and item alike)
		wantFree         int
	}{
		{[]string{qualUnstrung}, true, false, 26},
		{[]string{"Yew shortbow id857"}, false, true, 27},
		{[]string{"Yew shortbow"}, true, true, 25},
		{[]string{"Fire rune"}, false, false, 28},
	} {
		sim := actionsim.New(simObjs(cfg), pack)
		sim.OpenBank(nil)
		d := &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Log: func(string) {}, Delay: fastDelay}
		free := DepositForKit(d, actions.NewBank(sim, d.Components), tc.keep)
		un := count(sim.Pack(), kitYewShortUnstrungNote)+count(sim.Pack(), kitYewShortUnstrung) > 0
		st := count(sim.Pack(), kitYewShortStrungNote) > 0
		if un != tc.unstrung || st != tc.strung || free != tc.wantFree {
			t.Errorf("keep %v: unstrung kept %v strung kept %v free %d; want %v %v %d (pack %+v)",
				tc.keep, un, st, free, tc.unstrung, tc.strung, tc.wantFree, sim.Pack())
		}
	}
}

func TestNewAskedNamesMapsOnlyQualifiedNames(t *testing.T) {
	cfg := kitConfig(t)
	got := newAskedNames(cfg, qualUnstrung, "Yew shortbow", "Cake")
	var ids []int
	for id, name := range got {
		if name != qualUnstrung {
			t.Errorf("obj %d is read as %q", id, name)
		}
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	if !reflect.DeepEqual(ids, []int{kitYewShortUnstrung, kitYewShortUnstrungNote}) {
		t.Errorf("ids %v, want the unstrung bow and its note only", ids)
	}
	if newAskedNames(cfg, "Yew shortbow", "Cake") != nil || newAskedNames(nil, qualUnstrung) != nil {
		t.Error("a plain name, or no config, asked for nothing by id")
	}
}
