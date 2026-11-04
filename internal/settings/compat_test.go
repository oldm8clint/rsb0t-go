package settings

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

type compatInner struct {
	Lives int            `json:"lives"`
	Seen  map[string]int `json:"seen,omitempty"`
}

type compatEmbedded struct {
	Promoted int `json:"promoted"`
}

type compatOuter struct {
	compatEmbedded
	Stage   int                    `json:"stage"`
	Inner   compatInner            `json:"inner"`
	Ptr     *compatInner           `json:"ptr,omitempty"`
	ByName  map[string]compatInner `json:"byName,omitempty"`
	List    []compatInner          `json:"list,omitempty"`
	When    time.Time              `json:"when"`
	Skipped int                    `json:"-"`
	NoTag   string
}

func mustBag(t *testing.T, js string) *Bag {
	t.Helper()
	bag, err := ParseBag([]byte(js))
	if err != nil {
		t.Fatalf("ParseBag: %v", err)
	}
	return bag
}

// The strict decoder is the reason this file exists: a single unknown field fails the whole
// decode. Pinned so nobody "fixes" Decode itself and silently loosens operator settings.
func TestDecodeStaysStrictForOperatorSettings(t *testing.T) {
	bag := mustBag(t, `{"stage": 6, "prod": {}}`)
	var v compatOuter
	if err := bag.Decode(&v); err == nil {
		t.Fatal("Decode accepted an unknown field; operator settings must stay strict")
	}
}

func TestDecodeCompatReadsKnownFieldsAndReportsUnknownOnes(t *testing.T) {
	bag := mustBag(t, `{
		"stage": 6, "promoted": 3, "NoTag": "x",
		"prod": {"muleAsks": {"staff": "2026-09-23T01:00:00Z"}},
		"inner": {"lives": 4, "newCounter": 9, "seen": {"a": 1}},
		"byName": {"q": {"lives": 1, "extra": true}},
		"list": [{"lives": 2, "gone": 1}],
		"when": "2026-09-23T00:00:00Z"
	}`)
	var v compatOuter
	u, err := bag.DecodeCompat(&v)
	if err != nil {
		t.Fatalf("DecodeCompat: %v", err)
	}
	if v.Stage != 6 || v.Promoted != 3 || v.NoTag != "x" || v.Inner.Lives != 4 || v.ByName["q"].Lives != 1 || len(v.List) != 1 {
		t.Fatalf("known fields not decoded: %+v", v)
	}
	want := []string{"byName.q.extra", "inner.newCounter", "list[0].gone", "prod"}
	if got := u.Paths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Paths = %v, want %v", got, want)
	}
	if got := u.Dropped(); !reflect.DeepEqual(got, []string{"list[0].gone"}) {
		t.Fatalf("Dropped = %v, want only the list element's field", got)
	}
	if u.Empty() {
		t.Fatal("Empty() with four unknown fields")
	}
}

func TestDecodeCompatMatchesKeysCaseInsensitivelyLikeEncodingJSON(t *testing.T) {
	bag := mustBag(t, `{"Stage": 2, "notag": "y"}`)
	var v compatOuter
	u, err := bag.DecodeCompat(&v)
	if err != nil || !u.Empty() || v.Stage != 2 || v.NoTag != "y" {
		t.Fatalf("got %+v, unknown %v, err %v", v, u.Paths(), err)
	}
}

// A field this build knows, holding a value of the wrong type, is NOT something to zero and write
// back: the caller must see an error and treat the file as unreadable.
func TestDecodeCompatFailsOnAWrongTypedKnownField(t *testing.T) {
	bag := mustBag(t, `{"stage": "six"}`)
	var v compatOuter
	if _, err := bag.DecodeCompat(&v); err == nil {
		t.Fatal("DecodeCompat accepted a string for an int field")
	}
}

func TestEncodeCompatWritesUnknownFieldsBackWhereTheyWere(t *testing.T) {
	bag := mustBag(t, `{
		"stage": 6,
		"prod": {"muleAsks": {"staff": "2026-09-23T01:00:00Z"}},
		"inner": {"lives": 4, "newCounter": 9},
		"byName": {"q": {"lives": 1, "extra": true}},
		"list": [{"lives": 2, "gone": 1}]
	}`)
	var v compatOuter
	u, err := bag.DecodeCompat(&v)
	if err != nil {
		t.Fatal(err)
	}
	v.Stage = 7
	v.Inner.Lives = 5
	out := NewBag()
	if err := out.EncodeCompat(v, u); err != nil {
		t.Fatal(err)
	}
	raw := out.Raw()
	if raw["stage"].(float64) != 7 {
		t.Fatalf("this build's own change lost: stage %v", raw["stage"])
	}
	inner := raw["inner"].(map[string]interface{})
	if inner["lives"].(float64) != 5 || inner["newCounter"].(float64) != 9 {
		t.Fatalf("inner = %v, want lives 5 (ours) and newCounter 9 (kept)", inner)
	}
	prod, ok := raw["prod"].(map[string]interface{})
	if !ok || prod["muleAsks"].(map[string]interface{})["staff"] != "2026-09-23T01:00:00Z" {
		t.Fatalf("unknown top-level object not written back: %v", raw["prod"])
	}
	if raw["byName"].(map[string]interface{})["q"].(map[string]interface{})["extra"] != true {
		t.Fatalf("unknown field inside a map value not written back: %v", raw["byName"])
	}
	if _, kept := raw["list"].([]interface{})[0].(map[string]interface{})["gone"]; kept {
		t.Fatal("a list element's unknown field was claimed kept; it is reported as Dropped")
	}

	// And it round-trips: decoding the re-encoded file finds the same kept fields again.
	var again compatOuter
	u2, err := out.DecodeCompat(&again)
	if err != nil {
		t.Fatal(err)
	}
	if got := u2.Paths(); !reflect.DeepEqual(got, []string{"byName.q.extra", "inner.newCounter", "prod"}) {
		t.Fatalf("second round Paths = %v", got)
	}
}

// An object this build's encoding leaves out (an omitempty map it cleared) must not be re-created
// just to hold an unknown child: that would resurrect state this build deliberately cleared.
func TestEncodeCompatDoesNotResurrectAnObjectThisBuildCleared(t *testing.T) {
	bag := mustBag(t, `{"stage": 1, "ptr": {"lives": 1, "extra": 2}}`)
	var v compatOuter
	u, err := bag.DecodeCompat(&v)
	if err != nil {
		t.Fatal(err)
	}
	v.Ptr = nil
	out := NewBag()
	if err := out.EncodeCompat(v, u); err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Raw()["ptr"]; ok {
		t.Fatalf("ptr re-created: %v", out.Raw()["ptr"])
	}
}

func TestEncodeCompatWithNothingUnknownIsEncode(t *testing.T) {
	v := compatOuter{Stage: 3}
	a, b := NewBag(), NewBag()
	if err := a.Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := b.EncodeCompat(v, Unknown{}); err != nil {
		t.Fatal(err)
	}
	ja, _ := a.MarshalIndent()
	jb, _ := b.MarshalIndent()
	if string(ja) != string(jb) {
		t.Fatalf("EncodeCompat(v, Unknown{}) differs from Encode:\n%s\n%s", ja, jb)
	}
}

// The kept values are copies: changing the Bag after the decode must not change what is kept.
func TestDecodeCompatKeepsCopiesNotAliases(t *testing.T) {
	bag := mustBag(t, `{"stage": 1, "prod": {"n": 1}}`)
	var v compatOuter
	u, err := bag.DecodeCompat(&v)
	if err != nil {
		t.Fatal(err)
	}
	bag.Raw()["prod"].(map[string]interface{})["n"] = 99.0 // Raw is a shallow copy; the inner map is shared
	out := NewBag()
	if err := out.EncodeCompat(v, u); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(out.Raw()["prod"])
	if !strings.Contains(string(data), `"n":1`) {
		t.Fatalf("kept value aliased the live bag: %s", data)
	}
}

func TestDecodeCompatNeedsAPointer(t *testing.T) {
	var v compatOuter
	if _, err := mustBag(t, `{}`).DecodeCompat(v); err == nil {
		t.Fatal("DecodeCompat accepted a non-pointer")
	}
}
