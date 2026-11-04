package actions

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"rsb0t-go/internal/gamestate"
)

// useOnVector is one entry of test/fixtures/useon_vector.json. Decoded is what Engine-TS's
// decoder read from the bytes; its keys use the engine's own field names.
type useOnVector struct {
	Name    string                 `json:"name"`
	Opcode  int                    `json:"opcode"`
	Length  int                    `json:"length"`
	Fields  map[string]int         `json:"fields"`
	RawByte []int                  `json:"bytes"`
	Decoded map[string]interface{} `json:"decoded"`
	Bytes   []byte                 `json:"-"`
}

func loadUseOnVectors(t *testing.T) map[string]useOnVector {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve test file path")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "test", "fixtures", "useon_vector.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var list []useOnVector
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]useOnVector, len(list))
	for _, v := range list {
		for _, b := range v.RawByte {
			v.Bytes = append(v.Bytes, byte(b))
		}
		out[v.Name] = v
	}
	return out
}

// checkVector compares a sent packet with the TS-built payload: the opcode byte (no ISAAC in
// the fake session), the fixed length the engine expects, and every payload byte.
func checkVector(t *testing.T, sent []byte, v useOnVector) {
	t.Helper()
	if len(sent) == 0 || int(sent[0]) != v.Opcode {
		t.Fatalf("%s: opcode byte = %x, want %d", v.Name, sent, v.Opcode)
	}
	if len(sent)-1 != v.Length {
		t.Fatalf("%s: payload is %d bytes, engine expects %d", v.Name, len(sent)-1, v.Length)
	}
	if !bytes.Equal(sent[1:], v.Bytes) {
		t.Fatalf("%s: payload %x, want %x", v.Name, sent[1:], v.Bytes)
	}
}

// TestUseOnVectorsDecodeAsIntended checks that the engine's decoder read each field back as
// the value the script wrote, so the byte order in the fixture is the one the server expects.
func TestUseOnVectorsDecodeAsIntended(t *testing.T) {
	engineName := map[string]string{"npc": "npcSlot"}
	for name, v := range loadUseOnVectors(t) {
		for field, want := range v.Fields {
			key := field
			if alias, ok := engineName[field]; ok {
				key = alias
			}
			got, ok := v.Decoded[key].(float64)
			if !ok || int(got) != want {
				t.Fatalf("%s: engine decoded %s = %v, want %d", name, key, v.Decoded[key], want)
			}
		}
	}
}

func TestUseOnPacketsMatchTheReferenceVectors(t *testing.T) {
	vectors := loadUseOnVectors(t)
	if len(vectors) != 4 {
		t.Fatalf("fixture has %d vectors, want 4", len(vectors))
	}

	f := newFakeSession()
	v := vectors["OPNPCU"]
	fl := v.Fields
	if err := OpNpcU(f, fl["npc"], int32(fl["useObj"]), fl["useSlot"], fl["useCom"]); err != nil {
		t.Fatal(err)
	}
	checkVector(t, f.lastSent(), v)

	v = vectors["OPHELDU"]
	fl = v.Fields
	if err := OpHeldU(f, int32(fl["obj"]), fl["slot"], fl["com"], int32(fl["useObj"]), fl["useSlot"], fl["useCom"]); err != nil {
		t.Fatal(err)
	}
	checkVector(t, f.lastSent(), v)

	v = vectors["OPOBJU"]
	fl = v.Fields
	if err := OpObjU(f, fl["x"], fl["z"], fl["obj"], int32(fl["useObj"]), fl["useSlot"], fl["useCom"]); err != nil {
		t.Fatal(err)
	}
	checkVector(t, f.lastSent(), v)

	v = vectors["OPLOCU"]
	fl = v.Fields
	if err := OpLocU(f, fl["x"], fl["z"], fl["loc"], int32(fl["useObj"]), fl["useSlot"], fl["useCom"]); err != nil {
		t.Fatal(err)
	}
	checkVector(t, f.lastSent(), v)
}

func TestUseOnWrappersPickTheBackpackSlots(t *testing.T) {
	vectors := loadUseOnVectors(t)
	f, e := equipmentSession()
	// Backpack (component 3214): tinderbox 590 slot 0, dagger 1205 slot 1, 11 slot 2,
	// coins 995 slot 3, 996 slot 4.
	if err := e.UseOnNpc(590, 1234); err != nil {
		t.Fatal(err)
	}
	checkVector(t, f.lastSent(), useOnVector{Name: "OPNPCU", Opcode: vectors["OPNPCU"].Opcode, Length: 8,
		Bytes: []byte{0x04, 0xd2, 0x02, 0x4e, 0x00, 0x00, 0x0c, 0x8e}})

	if err := e.UseOnHeld(1205, 995); err != nil {
		t.Fatal(err)
	}
	checkVector(t, f.lastSent(), useOnVector{Name: "OPHELDU", Opcode: vectors["OPHELDU"].Opcode, Length: 12,
		Bytes: []byte{0x03, 0xe3, 0x00, 0x03, 0x0c, 0x8e, 0x04, 0xb5, 0x00, 0x01, 0x0c, 0x8e}})

	if err := e.UseOnGroundItem(590, gamestate.GroundItem{ID: 1511, X: 3222, Z: 3218}); err != nil {
		t.Fatal(err)
	}
	checkVector(t, f.lastSent(), useOnVector{Name: "OPOBJU", Opcode: vectors["OPOBJU"].Opcode, Length: 12,
		Bytes: []byte{0x0c, 0x96, 0x0c, 0x92, 0x05, 0xe7, 0x02, 0x4e, 0x00, 0x00, 0x0c, 0x8e}})

	if err := e.UseOnLoc(11, 2711, 3471, 2644); err != nil {
		t.Fatal(err)
	}
	checkVector(t, f.lastSent(), useOnVector{Name: "OPLOCU", Opcode: vectors["OPLOCU"].Opcode, Length: 12,
		Bytes: []byte{0x0a, 0x97, 0x0d, 0x8f, 0x0a, 0x54, 0x00, 0x0b, 0x00, 0x02, 0x0c, 0x8e}})

	sent := f.sendCount()
	if err := e.UseOnHeld(590, 590); err != ErrNoOtherItem {
		t.Fatalf("using the only tinderbox on itself: err = %v", err)
	}
	if err := e.UseOnNpc(4151, 1); err != ErrItemNotFound {
		t.Fatalf("an item not held: err = %v", err)
	}
	openMainSide(f.state, BankMainModal, 2005)
	if err := e.UseOnNpc(590, 1); err != ErrUseWhileBanking {
		t.Fatalf("use while banking: err = %v", err)
	}
	if f.sendCount() != sent {
		t.Fatal("a refused use must not send")
	}
}

func TestUseOnHeldSameIDTargetsAnotherSlot(t *testing.T) {
	f, e := equipmentSession()
	setFullInventory(f.state, 3214, [2]int32{1511, 1}, [2]int32{0, 0}, [2]int32{1511, 1})
	if err := e.UseOnHeld(1511, 1511); err != nil {
		t.Fatal(err)
	}
	got := f.lastSent()
	// target obj 1511 slot 2 com 3214, used obj 1511 slot 0 com 3214
	want := []byte{0x05, 0xe7, 0x00, 0x02, 0x0c, 0x8e, 0x05, 0xe7, 0x00, 0x00, 0x0c, 0x8e}
	if !bytes.Equal(got[1:], want) {
		t.Fatalf("payload %x, want %x", got[1:], want)
	}
}
