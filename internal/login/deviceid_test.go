package login

import "testing"

func TestAccountDeviceIDStableAndDistinct(t *testing.T) {
	if AccountDeviceID("Acct12") != AccountDeviceID(" acct12 ") {
		t.Fatal("device id must not depend on case or surrounding space")
	}
	seen := map[int32]string{}
	for _, n := range []string{"mule", "main", "acct1", "acct2", "Acct199", "drugs r bad"} {
		id := AccountDeviceID(n)
		if id == 0 || id == DefaultDeviceID {
			t.Fatalf("%s: got reserved id %d", n, id)
		}
		if prev, ok := seen[id]; ok {
			t.Fatalf("%s and %s share id %d", n, prev, id)
		}
		seen[id] = n
	}
}

func TestDeviceIDForModes(t *testing.T) {
	if DeviceIDFor("", "a") != 0 || DeviceIDFor(DeviceIDFixed, "a") != 0 {
		t.Fatal("fixed mode must leave the id to the default")
	}
	if DeviceIDFor(DeviceIDPerAccount, "a") != AccountDeviceID("a") {
		t.Fatal("per-account mode must use AccountDeviceID")
	}
	if ValidDeviceIDMode("random") || !ValidDeviceIDMode(DeviceIDPerAccount) {
		t.Fatal("mode validation wrong")
	}
}

func TestBuildLoginBlockCarriesDeviceID(t *testing.T) {
	w := buildLoginBlock([4]int32{}, 0x01020304, "a", "b", false)
	got := w.Data[17:21]
	if got[0] != 1 || got[1] != 2 || got[2] != 3 || got[3] != 4 {
		t.Fatalf("device id bytes = %v, want [1 2 3 4]", got)
	}
}
