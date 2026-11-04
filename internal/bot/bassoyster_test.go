package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/fisherplan"
)

func TestBassOysterCasketsIsTheRegisteredFisher(t *testing.T) {
	b, ok := New("BassOysterCaskets")
	if !ok {
		t.Fatal("BassOysterCaskets is not registered: a roster entry cannot select it by name")
	}
	fb, isFisher := b.(*fisherBot)
	if !isFisher || !fb.bassOyster {
		t.Fatalf("the registry builds %T (bassOyster %v), want the fisher with bassOyster set", b, isFisher && fb.bassOyster)
	}
	pf, _ := New("ProgressiveFisher")
	if pf.(*fisherBot).bassOyster {
		t.Fatal("ProgressiveFisher has the BassOysterCaskets rules switched on")
	}
	if skill, ok := PrimarySkill("BassOysterCaskets"); !ok || skill != "Fishing" {
		t.Fatalf("PrimarySkill = %q, %v, want Fishing", skill, ok)
	}
}

// An account switched from ProgressiveFisher keeps its settings map; the two settings this script
// fixes are overridden whatever that map says.
func TestBassOysterCasketsFixesTheTargetAndTheGuild(t *testing.T) {
	settings := map[string]interface{}{"target": "Harpoon sharks", "autoSharks": true, "fishingGuild": false, "mule": "mule"}
	b := &fisherBot{bassOyster: true}
	b.target = fisherTargetSetting(settings)
	b.guild = readFisherGuild(settings, "someone")
	b.applyBassOysterSettings(&Context{})
	if b.target != fisherplan.BassOysterTarget() {
		t.Errorf("target = %q, want %q", b.target, fisherplan.BassOysterTarget())
	}
	if !b.guild.enabled {
		t.Error("fishingGuild: false turned the Guild off")
	}
	if r := fisherplan.CurrentRung(95, b.target); r.Label != "Big net" {
		t.Errorf("Fishing 95 fishes %q, want the Big net", r.Label)
	}
}

// Only BassOysterCaskets drops anything or runs the drop cycle; ProgressiveFisher's deps are exactly
// what they were.
func TestOnlyBassOysterCasketsDropsJunkAndCycles(t *testing.T) {
	ctx := &Context{}
	if d := (&fisherBot{}).deps(ctx); d.Junk != nil || d.Cycle != nil {
		t.Fatal("ProgressiveFisher's deps drop junk or run a drop cycle")
	}
	b := &fisherBot{bassOyster: true}
	b.applyBassOysterSettings(ctx)
	d := b.deps(ctx)
	if d.Junk == nil || !d.Junk("Raw mackerel") || !d.Junk("Empty oyster") || d.Junk("Raw bass") || d.Junk("Oyster pearl") {
		t.Fatal("BassOysterCaskets does not drop exactly the Big net junk and the Empty oyster")
	}
	if d.Cycle == nil || d.Cycle != b.dropCycle {
		t.Fatal("BassOysterCaskets' deps do not share the bot's drop cycle (its totals would be lost on every Loop call)")
	}
	if b.deps(ctx).Cycle != d.Cycle {
		t.Fatal("two Deps of one bot carry two drop cycles")
	}
}

// Every part of the drop cycle defaults on, and each setting switches its own part off.
func TestBassOysterCasketsCycleSettingsDefaultOnAndSwitchOff(t *testing.T) {
	b := &fisherBot{bassOyster: true}
	b.applyBassOysterSettings(&Context{})
	if o := b.dropCycle.Opts; !o.OpenCaskets || !o.OpenOysters || !o.CutPearls || !b.dropCycle.OnWalks {
		t.Fatalf("defaults %+v on walks %v: every part is on", o, b.dropCycle.OnWalks)
	}
	b = &fisherBot{bassOyster: true}
	b.applyBassOysterSettings(&Context{Settings: map[string]interface{}{
		"openCaskets": false, "openOysters": false, "cutPearls": false, "cycleOnWalks": false,
	}})
	if o := b.dropCycle.Opts; o.OpenCaskets || o.OpenOysters || o.CutPearls || b.dropCycle.OnWalks {
		t.Fatalf("switched off %+v on walks %v: every part is off", o, b.dropCycle.OnWalks)
	}
	b = &fisherBot{bassOyster: true}
	b.applyBassOysterSettings(&Context{Settings: map[string]interface{}{"openOysters": false}})
	if o := b.dropCycle.Opts; !o.OpenCaskets || o.OpenOysters || !o.CutPearls || !b.dropCycle.OnWalks {
		t.Fatalf("only oysters off: %+v on walks %v", o, b.dropCycle.OnWalks)
	}
}
