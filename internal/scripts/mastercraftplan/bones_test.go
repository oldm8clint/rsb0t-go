package mastercraftplan

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/boneplan"
)

func TestBonesDefaultsAndValidation(t *testing.T) {
	set := DefaultSettings()
	if !set.Bones || set.Sched.BonesMinStock != 5000 || set.Sched.BonesRound != time.Hour || set.Sched.BonesPause != 0 {
		t.Fatalf("%+v", set.Sched)
	}
	if err := set.Validate(); err != nil {
		t.Fatal(err)
	}
	set.Sched.BonesMinStock = 10
	if set.Validate() == nil {
		t.Error("a stock under one load is refused")
	}
}

func TestBoneStreamIsSixTicksOf28(t *testing.T) {
	if !BoneStream.Valid() || BoneStream.Ticks() != 6 || BoneStream.LoadEvents() != 30 {
		t.Fatalf("ticks %d events %d valid %v", BoneStream.Ticks(), BoneStream.LoadEvents(), BoneStream.Valid())
	}
	// The booth op is the last event of its tick and the load's uses are every pack slot.
	if ev := BoneStream.EventAt(29); ev.Kind != FastBooth {
		t.Errorf("event 29 is %v", ev.Kind)
	}
	if got := BoneStream.UsesIn(6, 1); got != BoneLoad {
		t.Errorf("uses in a load %d", got)
	}
}

func TestBonesReady(t *testing.T) {
	set := DefaultSettings()
	big := stockMap(map[string]int{"Big bones": 4000, "Bones": 1000})
	p, ok := BonesReady(set, 0, big, nil)
	if !ok || p.Resume || p.Mule != 5000 {
		t.Fatalf("5,000 bones start a round: %+v %v", p, ok)
	}
	if _, ok := BonesReady(set, 0, stockMap(map[string]int{"Bones": 4999}), nil); ok {
		t.Error("4,999 is under the stock")
	}
	if _, ok := BonesReady(set, 0, nil, nil); ok {
		t.Error("an unpublished mule starts nothing")
	}
	if _, ok := BonesReady(set, MaxSkillXP, big, nil); ok {
		t.Error("Prayer at the cap earns nothing more")
	}
	// A load in this account's own bank resumes with no stock.
	p, ok = BonesReady(set, 0, stockMap(nil), map[string]int{"Big bones": 60})
	if !ok || !p.Resume {
		t.Errorf("own bank resume: %+v %v", p, ok)
	}
	if _, ok := BonesReady(set, 0, stockMap(nil), map[string]int{"Big bones": 27}); ok {
		t.Error("under a load is not a resume")
	}
	set.Sched.BonesMinStock = 100
	if _, ok := BonesReady(set, 0, stockMap(map[string]int{"Bones": 100}), nil); !ok {
		t.Error("the stock is a setting")
	}
}

func TestBestBoneAndAsk(t *testing.T) {
	mule := stockMap(map[string]int{"Bones": 3000, "Big bones": 1500, "Dragon bones": 10})
	k, ok := BestBone(BoneCounts(mule))
	if !ok || k.Name != "Big bones" {
		t.Fatalf("dragon bones are under a load, so Big bones: %+v", k)
	}
	set := DefaultSettings()
	lines := PlanBonesAsk(set, mule)
	if len(lines) != 1 || lines[0].Item != "Big bones" || lines[0].Qty != 980 {
		t.Fatalf("one line of whole loads under the kit cap: %+v", lines)
	}
	if PlanBonesAsk(set, stockMap(map[string]int{"Bones": 20})) != nil {
		t.Error("under a load asks for nothing")
	}
}

func TestBonesXPAnHour(t *testing.T) {
	// Big bones: 28 x 15 xp = 420 a load, a load every 3.6 s: 420,000 an hour. Bones 126 a load, 126,000 an hour.
	byName := map[string]int{}
	for _, k := range boneplan.BoneKinds {
		byName[k.Name] = BonesPerHour(k)
	}
	if byName["Big bones"] != 420000 || byName["Bones"] != 126000 || byName["Dragon bones"] != 2016000 {
		t.Fatalf("%v", byName)
	}
}

// The round takes its turn in the rotation, an hour at a time.
func TestBonesRoundRotationAndLimit(t *testing.T) {
	set := orderSettings()
	set.Bones = true
	snap := readySnapshot(map[string]int{"Big bones": 9000})
	snap.Now = schedNow
	d := Decide(set, snap)
	if d.Mode != ModeBones || !strings.Contains(d.Why, "9000 bones") {
		t.Fatalf("%s %s", d.Mode, d.Why)
	}
	if RotationKey(d) != KeyBonesRotation {
		t.Errorf("rotation key %q", RotationKey(d))
	}
	if got := RoundBudget(set, ModeBones, ""); got != time.Hour {
		t.Errorf("budget %s", got)
	}
	var l Ledger
	l.PauseForLimit(ModeBones, schedNow, set)
	if !l.Bones.PauseUntil.IsZero() {
		t.Errorf("the default pause is none: %s", l.Bones.PauseUntil)
	}
	set.Sched.BonesPause = 30 * time.Minute
	l.PauseForLimit(ModeBones, schedNow, set)
	if !l.Bones.PauseUntil.Equal(schedNow.Add(30 * time.Minute)) {
		t.Errorf("pause %s", l.Bones.PauseUntil)
	}
	snap.Ledger = &l
	if d := Decide(set, snap); d.Mode == ModeBones {
		t.Error("a paused round is not chosen")
	}
}
