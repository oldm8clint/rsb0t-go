package boneplan

import "testing"

func TestHaulFor(t *testing.T) {
	base := HaulInput{Prayer: 43, Skip: 43, On: true, Free: 20}
	cases := []struct {
		name string
		mod  func(*HaulInput)
		want HaulAction
	}{
		{"under the skip level buries", func(i *HaulInput) { i.Prayer = 42 }, HaulBury},
		{"under the skip level buries even with the haul off", func(i *HaulInput) { i.Prayer = 10; i.On = false }, HaulBury},
		{"at the skip level carries", func(i *HaulInput) {}, HaulCarry},
		{"haul off leaves them", func(i *HaulInput) { i.On = false }, HaulLeave},
		{"carry cap reached", func(i *HaulInput) { i.Carried = DefaultHaulCarry }, HaulLeave},
		{"one under the cap carries", func(i *HaulInput) { i.Carried = DefaultHaulCarry - 1 }, HaulCarry},
		{"reserve of free slots is kept", func(i *HaulInput) { i.Free = DefaultHaulReserve }, HaulLeave},
		{"one over the reserve carries", func(i *HaulInput) { i.Free = DefaultHaulReserve + 1 }, HaulCarry},
		{"custom cap", func(i *HaulInput) { i.Carry = 3; i.Carried = 3 }, HaulLeave},
	}
	for _, c := range cases {
		in := base
		c.mod(&in)
		if got := HaulFor(in); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBonePrayerXP(t *testing.T) {
	want := map[string]int{"Bones": 45, "Big bones": 150, "Dragon bones": 720, "Babydragon bones": 300, "Wolf bones": 45, "Bat bones": 45, "Logs": 0}
	for n, x := range want {
		if got := BonePrayerXP10(n); got != x {
			t.Errorf("%s: %d, want %d", n, got, x)
		}
	}
	for i := 1; i < len(BoneKinds); i++ {
		if BoneKinds[i].XP10 > BoneKinds[i-1].XP10 {
			t.Errorf("BoneKinds not dearest first at %d", i)
		}
	}
}
