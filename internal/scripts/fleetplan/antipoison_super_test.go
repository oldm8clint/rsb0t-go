package fleetplan

import "testing"

func TestAntipoisonDosesSuper(t *testing.T) {
	for name, want := range map[string]int{"Antipoison(3)": 3, "Superantipoison(4)": 4, "superantipoison(1)": 1, "Shark": 0} {
		if got := AntipoisonDoses(name); got != want {
			t.Errorf("%s: %d doses, want %d", name, got, want)
		}
	}
	if last := AntipoisonForms[len(AntipoisonForms)-1]; last != "Superantipoison(4)" {
		t.Errorf("the mule ask must offer Superantipoison(4) first, list ends %s", last)
	}
}
