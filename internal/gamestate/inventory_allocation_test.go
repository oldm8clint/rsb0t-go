package gamestate

import (
	"fmt"
	"reflect"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func fullInventoryPayload(items []Item) []byte {
	p := packet.NewWriter(2 + len(items)*7)
	p.P2(uint16(len(items)))
	for _, it := range items {
		if it.ID == 0 {
			p.P2(0)
		} else {
			p.P2(uint16(it.ID + 1))
		}
		if it.Count < 255 {
			p.P1(byte(it.Count))
		} else {
			p.P1(255)
			p.P4(it.Count)
		}
	}
	return p.Data[:p.Pos]
}

func TestFullInventoryResyncTransitions(t *testing.T) {
	var inv Inventory
	change := func(slot int, before, after Item) Event {
		return Event{Kind: EventInventoryChanged, Comp: 3214, Slot: slot,
			ItemID: after.ID, Count: after.Count, PreviousID: before.ID, PreviousCount: before.Count}
	}
	a, b := Item{4, 255}, Item{9, 1000}
	smaller, c, d := Item{4, 7}, Item{10, 1}, Item{11, 32}
	steps := []struct {
		name       string
		wire, want []Item
		events     []Event
	}{
		{"empty", nil, nil, nil},
		{"first", []Item{a, {}, b}, []Item{a, {}, b}, []Event{change(0, Item{}, a), change(2, Item{}, b)}},
		{"unchanged", []Item{a, {}, b}, []Item{a, {}, b}, nil},
		{"shrink", []Item{smaller}, []Item{smaller, {}, {}}, []Event{change(0, a, smaller), change(2, b, Item{})}},
		{"grow", []Item{smaller, c, {}, {}, d}, []Item{smaller, c, {}, {}, d}, []Event{change(1, Item{}, c), change(4, Item{}, d)}},
		{"clear", nil, make([]Item, 5), []Event{change(0, smaller, Item{}), change(1, c, Item{}), change(4, d, Item{})}},
		{"clear-again", nil, make([]Item, 5), nil},
	}
	for i, step := range steps {
		got := inv.applyFull(3214, packet.NewReader(fullInventoryPayload(step.wire)))
		if !reflect.DeepEqual(got, step.events) || !reflect.DeepEqual(inv.Slots, step.want) {
			t.Fatalf("%s: slots=%v events=%v, want %v / %v", step.name, inv.Slots, got, step.want, step.events)
		}
		if inv.Generation != i+1 || inv.FullGeneration != i+1 {
			t.Fatalf("%s: generations %d/%d", step.name, inv.Generation, inv.FullGeneration)
		}
	}
}

func TestNonEmptyPreservesIndicesAndOwnership(t *testing.T) {
	inv := Inventory{Slots: []Item{{}, {4, 0}, {0, 99}, {9, 255}}}
	got := inv.NonEmpty()
	want := []SlotItem{{Slot: 1, Item: Item{4, 0}}, {Slot: 3, Item: Item{9, 255}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	got[0].Count = 42
	if inv.Slots[1].Count != 0 {
		t.Fatal("returned slots alias inventory")
	}
	inv.Slots[3].Count = 7
	if got[1].Count != 255 {
		t.Fatal("inventory mutation changed result")
	}
	for _, slots := range [][]Item{nil, {}, {{0, 99}, {}}} {
		empty := Inventory{Slots: slots}
		if empty.NonEmpty() != nil {
			t.Fatal("empty result must remain nil")
		}
	}
}

var inventoryBenchEvents []Event
var inventoryBenchSlots []SlotItem

func BenchmarkFullInventoryUnchanged(b *testing.B) {
	for _, size := range []int{28, 400} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			slots := make([]Item, size)
			for i := range slots {
				slots[i] = Item{int32(i + 1), 1000}
			}
			payload := fullInventoryPayload(slots)
			inv := Inventory{Slots: slots}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				inventoryBenchEvents = inv.applyFull(3214, packet.NewReader(payload))
			}
		})
	}
}

func BenchmarkInventoryNonEmpty(b *testing.B) {
	for _, spec := range []struct {
		name         string
		size, stride int
	}{
		{"empty-bank", 400, 0}, {"backpack", 28, 1}, {"full-bank", 400, 1}, {"sparse-bank", 400, 100},
	} {
		b.Run(spec.name, func(b *testing.B) {
			inv := Inventory{Slots: make([]Item, spec.size)}
			if spec.stride > 0 {
				for i := 0; i < spec.size; i += spec.stride {
					inv.Slots[i] = Item{int32(i + 1), 1}
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				inventoryBenchSlots = inv.NonEmpty()
			}
		})
	}
}

// InventoryGenerations reports an inventory's counters without copying it: nothing for a component never
// heard of, a full snapshot once one has arrived (FullGeneration 0 before it), and every update bumps
// the generation.
func TestInventoryGenerationsReadWithoutACopy(t *testing.T) {
	s := New()
	if _, _, ok := s.InventoryGenerations(5382); ok {
		t.Fatal("a component nothing was received for reported generations")
	}
	head := packet.NewWriter(2)
	head.P2(5382)
	s.Handle(serverprot.UPDATE_INV_FULL, append(append([]byte(nil), head.Data[:head.Pos]...), fullInventoryPayload([]Item{{ID: 4, Count: 2}})...))
	gen, full, ok := s.InventoryGenerations(5382)
	if !ok || full == 0 || gen != full {
		t.Fatalf("after a full snapshot: generation %d full %d ok %v", gen, full, ok)
	}
	inv := s.Inventory(5382)
	if inv == nil || inv.Generation != gen || inv.FullGeneration != full {
		t.Fatalf("the copy's counters %+v disagree with %d/%d", inv, gen, full)
	}
	// A partial update bumps the generation and leaves the full snapshot's.
	p := packet.NewWriter(8)
	p.P2(5382)
	p.P1(0) // slot 0
	p.P2(5) // object 4 + 1
	p.P1(9)
	s.Handle(serverprot.UPDATE_INV_PARTIAL, p.Data[:p.Pos])
	gen2, full2, _ := s.InventoryGenerations(5382)
	if gen2 != gen+1 || full2 != full {
		t.Fatalf("after a partial update: generation %d (was %d) full %d (was %d)", gen2, gen, full2, full)
	}
}
