package donorplan

import (
	"reflect"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

func TestSettings(t *testing.T) {
	if s := NewSettings(DefaultMule, DefaultMuleAt, DefaultKeep); !s.Enabled() || s.Mule != "mulecon" || s.MuleAt != 5000 || !reflect.DeepEqual(s.Keep, []string{"Coins", "Shears"}) {
		t.Errorf("defaults = %+v", s)
	}
	if s := NewSettings(" mulecon , other ", 5000, ""); s.Mule != "mulecon" || len(s.Keep) != 0 {
		t.Errorf("first of the list = %+v", s)
	}
	if NewSettings("", 5000, "").Enabled() || NewSettings("mulecon", -5, "").Enabled() {
		t.Error("blank mule or no threshold hands nothing over")
	}
	if s := NewSettings("mulecon", 5_000_000, ""); s.MuleAt != MaxMuleAt {
		t.Errorf("clamped = %d", s.MuleAt)
	}
	if got := ParseList(" a, ,b "); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("ParseList = %v", got)
	}
	if !NamesMatch("MuleCon", " mulecon ") || !NamesMatch("crafter_1", "Crafter 1") || NamesMatch("", "") || NamesMatch("mulecon", "mulecon2") {
		t.Error("NamesMatch")
	}
}

func TestLoaded(t *testing.T) {
	now := ms(5000)
	h := Begin(now)
	if h.Step != StepLoad {
		t.Fatalf("Begin = %s", h.Step)
	}
	if got := h.Loaded(0, 0, now); got != Finished {
		t.Errorf("empty bank = %s", got)
	}
	if got := h.Loaded(3, 0, now); got != Paused || h.Step != StepLoad {
		t.Errorf("failed withdrawal = %s at %s", got, h.Step)
	}
	if got := h.Loaded(3, 2, now); got != Continue || h.Step != StepMeet {
		t.Errorf("drawn = %s at %s", got, h.Step)
	}
}

func TestMeet(t *testing.T) {
	stand := tile.Tile{X: 2725, Z: 3493}
	start := ms(0)
	h := Begin(start)
	h.Loaded(1, 1, start)
	view := func(now time.Time, me, mule *tile.Tile) MeetView {
		return MeetView{Now: now, Me: me, Mule: mule, Stand: stand}
	}
	here := &tile.Tile{X: 2726, Z: 3492}
	far := &tile.Tile{X: 2760, Z: 3493}
	upstairs := &tile.Tile{X: 2725, Z: 3493, Level: 1}
	besideMule := &tile.Tile{X: 2725, Z: 3491}
	muleAtPost := &tile.Tile{X: 2725, Z: 3490}
	muleAcross := &tile.Tile{X: 2722, Z: 3488}

	steps := []struct {
		name string
		v    MeetView
		want MeetAction
	}{
		{"not there yet", view(start, far, muleAtPost), MeetWalk},
		{"on the wrong floor", view(start, upstairs, muleAtPost), MeetWalk},
		{"mule not in sight", view(start, here, nil), MeetWait},
		{"mule in sight but more than 2 tiles off", view(start, here, muleAcross), MeetApproach},
		{"first request", view(ms(1000), besideMule, muleAtPost), MeetRequest},
		{"no repeat at once", view(ms(2000), besideMule, muleAtPost), MeetWaitReply},
		{"repeat after the interval", view(ms(1000).Add(RequestEvery), besideMule, muleAtPost), MeetRequest},
		// Past the plain MeetTimeout but the mule is still standing right there: keep
		// waiting, don't abandon the hand-off. Measured 2026-09-18 — the mule's idle
		// cooking round takes it ~8 tiles away (inside SightRange) for ~99s, and donors
		// that gave up at exactly 60s were walking off seconds before it returned. See
		// Meet's own comment and MeetTimeoutMuleVisible.
		{"past the short timeout but the mule is visible", view(start.Add(MeetTimeout+time.Second), besideMule, muleAtPost), MeetRequest},
		// Genuinely gone (out of sight): the original timeout still applies unchanged.
		{"timeout with the mule out of sight", view(start.Add(MeetTimeout+time.Second), here, nil), MeetGiveUp},
		// Even a visible mule does not hold a donor forever.
		{"visible but past the long ceiling", view(start.Add(MeetTimeoutMuleVisible+time.Second), besideMule, muleAtPost), MeetGiveUp},
	}
	for _, s := range steps {
		if got := h.Meet(s.v); got != s.want {
			t.Errorf("%s: Meet = %s, want %s", s.name, got, s.want)
		}
	}
	open := view(ms(3000), besideMule, muleAtPost)
	open.TradeOpen = true
	if got := h.Meet(open); got != MeetTrading || h.Step != StepTrade {
		t.Errorf("open screen = %s at %s", got, h.Step)
	}
	if h.TradeExpired(ms(3000).Add(TradeTimeout)) || !h.TradeExpired(ms(3001).Add(TradeTimeout)) {
		t.Error("the trade screens get TradeTimeout")
	}
}

func TestMeetWaitsOutTheMulesCooldownAfterATrade(t *testing.T) {
	now := ms(10_000)
	h := Begin(now)
	h.Loaded(2, 2, now)
	h.Traded(1, 1, now)
	v := MeetView{Me: &tile.Tile{X: 2725, Z: 3491}, Mule: &tile.Tile{X: 2725, Z: 3490}, Stand: tile.Tile{X: 2725, Z: 3493}}
	v.Now = now.Add(PostTradeCooldown - time.Millisecond)
	if got := h.Meet(v); got != MeetWaitReply {
		t.Errorf("inside the cooldown = %s", got)
	}
	v.Now = now.Add(PostTradeCooldown)
	if got := h.Meet(v); got != MeetRequest {
		t.Errorf("after the cooldown = %s", got)
	}
}

func TestDecideTrade(t *testing.T) {
	load := []LoadGroup{{"Bow string", 1778, 1}, {"Uncut sapphire", 1624, 1}}
	strings := ItemRow{"Bow string", 1778, 5200}
	sapphire := ItemRow{"Uncut sapphire", 1624, 2}
	base := TradeView{Screen: ScreenOffer, Partner: "MuleCon", Mule: "mulecon", Load: load}
	cases := []struct {
		name  string
		edit  func(v *TradeView)
		want  TradeAction
		group string
	}{
		{"closed", func(v *TradeView) { v.Screen = ScreenNone }, TradeClosed, ""},
		{"a stranger", func(v *TradeView) { v.Partner = "Stranger" }, TradeDecline, ""},
		{"the mule offers something", func(v *TradeView) { v.TheirOffer = []ItemRow{{"Coins", 995, 10}} }, TradeDecline, ""},
		{"offer the bow strings first", func(v *TradeView) {}, TradeOffer, "Bow string"},
		{"then the sapphire", func(v *TradeView) { v.Offered = []ItemRow{strings} }, TradeOffer, "Uncut sapphire"},
		{"all offered", func(v *TradeView) { v.Offered = []ItemRow{strings, sapphire} }, TradeAccept, ""},
		{"accepted, waiting", func(v *TradeView) { v.Offered, v.Accepted = []ItemRow{strings, sapphire}, true }, TradeWait, ""},
		{"nothing to give", func(v *TradeView) { v.Load = nil }, TradeDecline, ""},
		{"confirm", func(v *TradeView) { v.Screen, v.Offered = ScreenConfirm, []ItemRow{strings} }, TradeConfirm, ""},
		{"confirmed, waiting", func(v *TradeView) { v.Screen, v.Offered, v.Accepted = ScreenConfirm, []ItemRow{strings}, true }, TradeWait, ""},
		{"confirm with nothing offered", func(v *TradeView) { v.Screen = ScreenConfirm }, TradeDecline, ""},
	}
	for _, c := range cases {
		v := base
		c.edit(&v)
		got, group := DecideTrade(v)
		if got != c.want || group.Name != c.group {
			t.Errorf("%s: DecideTrade = %s %q, want %s %q", c.name, got, group.Name, c.want, c.group)
		}
	}
}

func TestAfterTheTrade(t *testing.T) {
	now := ms(9000)
	if traded, slots := TradeEnded(5, 3); !traded || slots != 2 {
		t.Error("slots left the pack")
	}
	if traded, _ := TradeEnded(5, 5); traded {
		t.Error("nothing left the pack")
	}
	h := Begin(now)
	h.Traded(2, 1, now)
	if h.Step != StepMeet || h.Loads != 1 || h.Slots != 2 || !h.NotBefore.Equal(now.Add(PostTradeCooldown)) {
		t.Errorf("haul left = %+v", h)
	}
	h.Traded(1, 0, now)
	if h.Step != StepLoad || h.Loads != 2 || h.Slots != 3 {
		t.Errorf("pack empty = %+v", h)
	}
	h.Failed(now)
	if h.Step != StepReturn {
		t.Errorf("failed = %s", h.Step)
	}
	if got := NextAllowed(now, true, ms(0)); !got.Equal(now.Add(HandoffRetry)) {
		t.Errorf("retry = %v", got)
	}
	if got := NextAllowed(now, false, ms(7)); !got.Equal(ms(7)) {
		t.Errorf("finished = %v", got)
	}
	for _, s := range []Step{StepLoad, StepMeet, StepTrade, StepReturn} {
		if Describe(s, "mulecon") == "" {
			t.Errorf("no description for %s", s)
		}
	}
}

func TestLoadFitsTheMulesDefaultFreeSlots(t *testing.T) {
	var pack []ItemRow
	for i := 0; i < 26; i++ {
		pack = append(pack, ItemRow{Name: "Item", ID: 5000 + i, Count: 1})
	}
	if got := PlanLoad(pack, DefaultMuleFreeSlots, Rules{}); len(got) != DefaultMuleFreeSlots {
		t.Errorf("load = %d groups, want %d", len(got), DefaultMuleFreeSlots)
	}
}

// TestLoadedTreatsRowsInBankAsTheSourceCount pins the trap that broke LootChestFisher's
// hand-off for a whole day.
//
// rowsInBank is read FIRST and zero means Finished -- "nothing to hand over" -- before drawn is
// ever considered. That is right for a donor whose haul comes out of a bank, and it is a trap
// for one whose haul is already in the pack: LootChestFisher passed a hardcoded 0 because its
// loot comes from chests and is never withdrawn, so every hand-off finished instantly without
// trading, and the account sat at the mule post retrying every five seconds with a full pack
// (live 2026-09-21).
//
// The fix is at the caller, so what this pins is the CONTRACT: a caller with something to give
// must report it in rowsInBank, whatever the pack got it from.
func TestLoadedTreatsRowsInBankAsTheSourceCount(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		name              string
		rowsInBank, drawn int
		want              Outcome
	}{
		{"nothing anywhere is genuinely finished", 0, 0, Finished},
		// The trap: a pack full of haul still reads Finished when rowsInBank says zero.
		{"a full pack still finishes when rowsInBank is zero", 0, 14, Finished},
		{"rows but nothing drawn pauses for a retry", 5, 0, Paused},
		{"rows and a drawn load continues", 5, 5, Continue},
		// A pack-sourced donor reporting its haul in both fields is what unblocks it.
		{"pack-sourced donor reporting its haul in both", 14, 14, Continue},
	} {
		h := Begin(now)
		if got := h.Loaded(c.rowsInBank, c.drawn, now); got != c.want {
			t.Errorf("%s: Loaded(rowsInBank=%d, drawn=%d) = %v, want %v",
				c.name, c.rowsInBank, c.drawn, got, c.want)
		}
	}
}
