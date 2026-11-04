package actions

import (
	"bytes"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// prayerTestSetStat feeds an UPDATE_STAT the way the server would: stat index, whole XP,
// current level. (setVarp is the package's existing helper, in bank_more_test.go.)
func prayerTestSetStat(f *fakeSession, stat, xp, level int) {
	f.state.Handle(serverprot.UPDATE_STAT, payload(func(p *packet.Packet) {
		p.P1(byte(stat))
		p.P4(int32(xp))
		p.P1(byte(level))
	}))
}

// xpForLevel43 sits between level 43's 50,339 XP threshold and level 44's 55,649, so
// PrayerLevel derives exactly 43 — the level Protect from Melee needs.
const xpForLevel43 = 51000

func TestPrayerActiveReadsTheServersOwnVarp(t *testing.T) {
	f := newFakeSession()
	if PrayerActive(f, ProtectFromMelee) {
		t.Fatal("a prayer the server never mentioned must read as off")
	}
	setVarp(f.state, ProtectFromMelee.Varp, 1)
	if !PrayerActive(f, ProtectFromMelee) {
		t.Fatal("varp 97 = 1 must read as Protect from Melee active")
	}
	setVarp(f.state, ProtectFromMelee.Varp, 0)
	if PrayerActive(f, ProtectFromMelee) {
		t.Fatal("varp 97 = 0 must read as inactive")
	}
}

func TestActivatePrayerClicksTheRightButton(t *testing.T) {
	f := newFakeSession()
	prayerTestSetStat(f, PrayerStat, xpForLevel43, 43)
	if err := ActivatePrayer(f, ProtectFromMelee); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(5623)) {
		t.Fatalf("sent %x, want IF_BUTTON 5623 (prayer:prayer_protectfrommelee)", f.lastSent())
	}
}

// The button is a toggle server-side, so clicking it while the prayer is already on would
// turn it OFF. This is the bug the read-before-click in SetPrayer exists to prevent.
func TestActivatingAnAlreadyActivePrayerSendsNothing(t *testing.T) {
	f := newFakeSession()
	prayerTestSetStat(f, PrayerStat, xpForLevel43, 43)
	setVarp(f.state, ProtectFromMelee.Varp, 1)
	if err := ActivatePrayer(f, ProtectFromMelee); err != nil {
		t.Fatal(err)
	}
	if f.sendCount() != 0 {
		t.Fatalf("sent %d packets, want none: re-clicking an active prayer would toggle it off", f.sendCount())
	}
}

func TestDeactivatingAnInactivePrayerSendsNothing(t *testing.T) {
	f := newFakeSession()
	prayerTestSetStat(f, PrayerStat, xpForLevel43, 43)
	if err := DeactivatePrayer(f, ProtectFromMelee); err != nil {
		t.Fatal(err)
	}
	if f.sendCount() != 0 {
		t.Fatalf("sent %d packets, want none", f.sendCount())
	}
}

func TestDeactivatePrayerClicksWhenItIsOn(t *testing.T) {
	f := newFakeSession()
	prayerTestSetStat(f, PrayerStat, xpForLevel43, 43)
	setVarp(f.state, ProtectFromMelee.Varp, 1)
	if err := DeactivatePrayer(f, ProtectFromMelee); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(5623)) {
		t.Fatalf("sent %x, want IF_BUTTON 5623", f.lastSent())
	}
}

func TestActivateRefusesBelowTheLevelRequirement(t *testing.T) {
	f := newFakeSession()
	// Level 1 Prayer: Thick Skin is allowed, Protect from Melee (43) is not.
	prayerTestSetStat(f, PrayerStat, 0, 1)
	if err := ActivatePrayer(f, ProtectFromMelee); err != ErrPrayerLevel {
		t.Fatalf("err = %v, want ErrPrayerLevel", err)
	}
	if f.sendCount() != 0 {
		t.Fatalf("sent %d packets, want none — the server would ignore this click silently", f.sendCount())
	}
	if err := ActivatePrayer(f, ThickSkin); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(5609)) {
		t.Fatalf("sent %x, want IF_BUTTON 5609 (thick skin)", f.lastSent())
	}
}

func TestActivateRefusesWithNoPrayerPoints(t *testing.T) {
	f := newFakeSession()
	// Base level 43 by XP, but drained to 0 points: prayer.rs2 refuses with
	// "You need to recharge your prayer at an altar."
	prayerTestSetStat(f, PrayerStat, xpForLevel43, 0)
	if err := ActivatePrayer(f, ProtectFromMelee); err != ErrNoPrayerPoints {
		t.Fatalf("err = %v, want ErrNoPrayerPoints", err)
	}
	if f.sendCount() != 0 {
		t.Fatalf("sent %d packets, want none", f.sendCount())
	}
}

func TestPrayerPointsAndLevelAreDifferentValues(t *testing.T) {
	f := newFakeSession()
	if _, ok := PrayerPoints(f); ok {
		t.Fatal("points must not be reported before any UPDATE_STAT")
	}
	if _, ok := PrayerLevel(f); ok {
		t.Fatal("level must not be reported before any UPDATE_STAT")
	}
	// Base 43 by XP, currently drained to 12 points.
	prayerTestSetStat(f, PrayerStat, xpForLevel43, 12)
	points, ok := PrayerPoints(f)
	if !ok || points != 12 {
		t.Fatalf("points = %d, %v; want 12, true", points, ok)
	}
	level, ok := PrayerLevel(f)
	if !ok || level != 43 {
		t.Fatalf("level = %d, %v; want 43, true (the base level a drained stat keeps)", level, ok)
	}
}

func TestCanUsePrayer(t *testing.T) {
	f := newFakeSession()
	if _, ok := CanUsePrayer(f, ProtectFromMelee); ok {
		t.Fatal("must report unknown before the Prayer stat arrives")
	}
	prayerTestSetStat(f, PrayerStat, xpForLevel43, 43)
	allowed, ok := CanUsePrayer(f, ProtectFromMelee)
	if !ok || !allowed {
		t.Fatalf("allowed = %v, ok = %v; want true, true at 43 Prayer with points", allowed, ok)
	}
	prayerTestSetStat(f, PrayerStat, xpForLevel43, 0)
	allowed, ok = CanUsePrayer(f, ProtectFromMelee)
	if !ok || allowed {
		t.Fatalf("allowed = %v, ok = %v; want false, true with no points left", allowed, ok)
	}
}

func TestActivePrayersListsEveryOneThatIsOn(t *testing.T) {
	f := newFakeSession()
	setVarp(f.state, ThickSkin.Varp, 1)
	setVarp(f.state, ProtectFromMelee.Varp, 1)
	on := ActivePrayers(f)
	if len(on) != 2 || on[0].Name != "Thick Skin" || on[1].Name != "Protect from Melee" {
		t.Fatalf("active = %v, want Thick Skin and Protect from Melee in tab order", on)
	}
}

func TestDeactivateAllPrayersClicksOnlyTheActiveOnes(t *testing.T) {
	f := newFakeSession()
	setVarp(f.state, ThickSkin.Varp, 1)
	setVarp(f.state, ProtectFromMelee.Varp, 1)
	if err := DeactivateAllPrayers(f); err != nil {
		t.Fatal(err)
	}
	if f.sendCount() != 2 {
		t.Fatalf("sent %d packets, want exactly 2 (one per active prayer)", f.sendCount())
	}
}

func TestPrayerByName(t *testing.T) {
	p, ok := PrayerByName("protect from melee")
	if !ok || p.ComID != 5623 || p.Varp != 97 || p.Level != 43 {
		t.Fatalf("got %+v, %v; want Protect from Melee com 5623 varp 97 level 43", p, ok)
	}
	if _, ok := PrayerByName("Piety"); ok {
		t.Fatal("Piety does not exist in this content build and must not resolve")
	}
}

// The table is the whole primitive, so pin the two facts every id depends on: fifteen
// prayers, and varps running 83..97 in the same order as components 5609..5623.
func TestPrayerTableMatchesContentOrdering(t *testing.T) {
	all := Prayers()
	if len(all) != 15 {
		t.Fatalf("got %d prayers, want 15", len(all))
	}
	for i, p := range all {
		if want := 5609 + i; p.ComID != want {
			t.Errorf("%s: com %d, want %d", p.Name, p.ComID, want)
		}
		if want := 83 + i; p.Varp != want {
			t.Errorf("%s: varp %d, want %d", p.Name, p.Varp, want)
		}
	}
	// The three protection prayers every boss plan depends on, from prayers.dbrow.
	for _, c := range []struct {
		p     Prayer
		level int
	}{{ProtectFromMagic, 37}, {ProtectFromMissiles, 40}, {ProtectFromMelee, 43}} {
		if c.p.Level != c.level {
			t.Errorf("%s: level %d, want %d", c.p.Name, c.p.Level, c.level)
		}
	}
}
