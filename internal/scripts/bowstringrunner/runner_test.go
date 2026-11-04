package bowstringrunner

import (
	"os"
	"testing"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/scripts/bowstringplan"
)

// TestResolveIDsRealConfig pins the ids content 289 gives the bows the mule holds: the unstrung
// bow is the cheaper of the two same-name ids (pack/obj.pack: 58 unstrung_willow_longbow, 62
// unstrung_maple_longbow, 64 unstrung_maple_shortbow, 68 unstrung_yew_shortbow).
func TestResolveIDsRealConfig(t *testing.T) {
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	want := map[string]int32{"Willow longbow": 58, "Maple longbow": 62, "Maple shortbow": 64, "Yew shortbow": 68}
	for name, id := range want {
		ids, err := ResolveIDs(cfg, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ids.Unstrung != id || ids.Strung == id || ids.String == 0 {
			t.Errorf("%s: %+v, want unstrung %d", name, ids, id)
		}
	}
	for _, name := range bowstringplan.DefaultBowNames() {
		if _, err := ResolveIDs(cfg, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
