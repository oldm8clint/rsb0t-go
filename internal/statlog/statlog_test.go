package statlog

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

func TestObserveBaselineAndPositiveDelta(t *testing.T) {
	var out bytes.Buffer
	l := New("acct-1", &out, func(id int32) string { return "item-name" })
	base := map[int][]gamestate.SlotItem{93: {{Slot: 0, Item: gamestate.Item{ID: 4151, Count: 3}}}}
	if err := l.Observe(base, time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatal("baseline emitted an event")
	}

	updated := map[int][]gamestate.SlotItem{93: {{Slot: 0, Item: gamestate.Item{ID: 4151, Count: 5}}}}
	if err := l.Observe(updated, time.Unix(101, 0)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{`"account":"acct-1"`, `"item_id":4151`, `"item_name":"item-name"`, `"delta":2`} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %s: %s", want, got)
		}
	}
}

func TestObserveIgnoresRemovalsAndItemReplacement(t *testing.T) {
	var out bytes.Buffer
	l := New("acct", &out, nil)
	first := map[int][]gamestate.SlotItem{1: {
		{Slot: 0, Item: gamestate.Item{ID: 10, Count: 5}},
		{Slot: 1, Item: gamestate.Item{ID: 20, Count: 2}},
	}}
	_ = l.Observe(first, time.Unix(1, 0))
	second := map[int][]gamestate.SlotItem{1: {
		{Slot: 0, Item: gamestate.Item{ID: 10, Count: 3}},
		{Slot: 1, Item: gamestate.Item{ID: 30, Count: 8}},
	}}
	if err := l.Observe(second, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected gain event: %s", out.String())
	}
}

func TestFlushIsSafe(t *testing.T) {
	var out bytes.Buffer
	l := New("acct", &out, nil)
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
}
