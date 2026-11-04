package controlpanel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func panelJSON(id, typ string, col, row, w, h int, extra string) string {
	return fmt.Sprintf(`{"id":%q,"type":%q,"title":"","col":%d,"row":%d,"w":%d,"h":%d,"accounts":[],"settings":{}%s}`, id, typ, col, row, w, h, extra)
}

func layoutJSON(panels ...string) string {
	return `{"v":1,"panels":[` + strings.Join(panels, ",") + `]}`
}

const goodLayout = `{"v":1,"panels":[
	{"id":"fleet","type":"fleet","title":"","col":0,"row":0,"w":3,"h":2,"accounts":[],"settings":{"memory":true}},
	{"id":"needs","type":"feed","title":"Needs you","col":3,"row":0,"w":9,"h":3,"accounts":["fisher01"],"settings":{ "levels" : ["warn", "error"] }},
	{"id":"bare","type":"memory","col":0,"row":2,"w":3,"h":1}
]}`

func TestParseLayoutAcceptsAndNormalises(t *testing.T) {
	l, err := ParseLayout([]byte(goodLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Panels) != 3 {
		t.Fatalf("%d panels", len(l.Panels))
	}
	if got := string(l.Panels[1].Settings); got != `{"levels":["warn","error"]}` {
		t.Errorf("settings not compacted: %s", got)
	}
	if l.Panels[2].Accounts == nil || string(l.Panels[2].Settings) != "{}" {
		t.Errorf("missing fields not normalised: %+v", l.Panels[2])
	}
	if _, err := ParseLayout([]byte(`{"v":1,"panels":[]}`)); err != nil {
		t.Errorf("an empty page is a layout: %v", err)
	}
}

func TestParseLayoutRejects(t *testing.T) {
	many := make([]string, MaxPanels+1)
	for i := range many {
		many[i] = panelJSON(fmt.Sprintf("p%d", i), "fleet", i%GridColumns, i/GridColumns, 1, 1, "")
	}
	bell, _ := json.Marshal("bell" + string(rune(7)))
	cases := []struct {
		name, body, want string
	}{
		{"truncated", `{"v":1,`, "not valid"},
		{"trailing data", goodLayout + `{}`, "after its closing brace"},
		{"unknown top-level field", `{"v":1,"panels":[],"extra":1}`, "unknown field"},
		{"unknown panel field", layoutJSON(panelJSON("a", "fleet", 0, 0, 1, 1, `,"colour":"red"`)), "unknown field"},
		{"wrong version", `{"v":2,"panels":[]}`, "version"},
		{"null", `null`, "version"},
		{"not an object", `[1,2]`, "not valid"},
		{"bad id", layoutJSON(panelJSON("Fleet!", "fleet", 0, 0, 1, 1, "")), "id"},
		{"empty id", layoutJSON(panelJSON("", "fleet", 0, 0, 1, 1, "")), "id"},
		{"duplicate id", layoutJSON(panelJSON("a", "fleet", 0, 0, 1, 1, ""), panelJSON("a", "feed", 1, 0, 1, 1, "")), "twice"},
		{"bad type", layoutJSON(panelJSON("a", "Fleet", 0, 0, 1, 1, "")), "type"},
		{"past the right edge", layoutJSON(panelJSON("a", "fleet", 10, 0, 3, 1, "")), "off the"},
		{"past the last row", layoutJSON(panelJSON("a", "fleet", 0, GridMaxRows-1, 1, 2, "")), "off the"},
		{"negative row", layoutJSON(panelJSON("a", "fleet", 0, -1, 1, 1, "")), "off the"},
		{"zero width", layoutJSON(panelJSON("a", "fleet", 0, 0, 0, 1, "")), "off the"},
		{"fractional cell", `{"v":1,"panels":[{"id":"a","type":"fleet","col":0.5,"row":0,"w":1,"h":1}]}`, "not valid"},
		{"overlap", layoutJSON(panelJSON("a", "fleet", 0, 0, 3, 2, ""), panelJSON("b", "feed", 2, 1, 2, 2, "")), "overlaps"},
		{"too many panels", layoutJSON(many...), "panels, over"},
		{"settings array", layoutJSON(panelJSON("a", "fleet", 0, 0, 1, 1, `,"settings":[1]`)), "JSON object"},
		{"settings string", layoutJSON(panelJSON("a", "fleet", 0, 0, 1, 1, `,"settings":"x"`)), "JSON object"},
		{"settings too big", layoutJSON(panelJSON("a", "fleet", 0, 0, 1, 1, `,"settings":{"x":"`+strings.Repeat("a", MaxSettingsBytes)+`"}`)), "byte limit"},
		{"account too long", layoutJSON(panelJSON("a", "fleet", 0, 0, 1, 1, `,"accounts":["`+strings.Repeat("n", 33)+`"]`)), "account name"},
		{"empty account", layoutJSON(panelJSON("a", "fleet", 0, 0, 1, 1, `,"accounts":[""]`)), "account name"},
		{"control character in title", layoutJSON(panelJSON("a", "fleet", 0, 0, 1, 1, `,"title":`+string(bell))), "title"},
		{"title too long", layoutJSON(panelJSON("a", "fleet", 0, 0, 1, 1, `,"title":"`+strings.Repeat("é", maxTitleRunes+1)+`"`)), "title"},
		{"too large", `{"v":1,"panels":[` + strings.Repeat(" ", MaxLayoutBytes) + `]}`, "byte limit"},
	}
	for _, c := range cases {
		_, err := ParseLayout([]byte(c.body))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one mentioning %q", c.name, err, c.want)
		}
	}
}

func putLayout(h *harness, body string) *http.Response {
	return h.request(http.MethodPut, "/api/layout", strings.NewReader(body), http.Header{"Content-Type": {"application/json"}})
}

func TestLayoutSavesAndLoadsOverHTTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "layout.json")
	h := newHarness(t, Config{LayoutFile: path}, testOptions(), nil)

	var got layoutBody
	decode(t, h.get("/api/layout"), &got)
	if got.Layout != nil || !got.Persisted || got.Problem != "" {
		t.Fatalf("before any save: %+v", got)
	}
	if res := putLayout(h, goodLayout); res.StatusCode != http.StatusOK {
		t.Fatalf("PUT: %d %s", res.StatusCode, bodyText(t, res))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("layout file: %v", err)
	}
	decode(t, h.get("/api/layout"), &got)
	if got.Layout == nil || len(got.Layout.Panels) != 3 || string(got.Layout.Panels[1].Settings) != `{"levels":["warn","error"]}` {
		t.Fatalf("after save: %+v", got.Layout)
	}

	second := layoutJSON(panelJSON("only", "accounts", 0, 0, 12, 4, ""))
	if res := putLayout(h, second); res.StatusCode != http.StatusOK {
		t.Fatalf("second PUT: %d", res.StatusCode)
	}
	decode(t, h.get("/api/layout"), &got)
	if len(got.Layout.Panels) != 1 || got.Layout.Panels[0].ID != "only" {
		t.Fatalf("second save not loaded: %+v", got.Layout)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".dashboard-layout-*")); len(leftovers) != 0 {
		t.Errorf("temporary files left behind: %v", leftovers)
	}
}

func TestLayoutRefusesBadWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layout.json")
	h := newHarness(t, Config{LayoutFile: path}, testOptions(), nil)
	if res := putLayout(h, goodLayout); res.StatusCode != http.StatusOK {
		t.Fatalf("setup PUT: %d", res.StatusCode)
	}
	saved, _ := os.ReadFile(path)

	overlap := layoutJSON(panelJSON("a", "fleet", 0, 0, 3, 2, ""), panelJSON("b", "feed", 2, 1, 2, 2, ""))
	res := putLayout(h, overlap)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("overlapping layout: %d, want 400", res.StatusCode)
	}
	var e map[string]string
	decode(t, res, &e)
	if !strings.Contains(e["error"], "overlaps") {
		t.Errorf("error %q does not say what is wrong", e["error"])
	}
	if res := putLayout(h, "{not json"); res.StatusCode != http.StatusBadRequest {
		t.Errorf("bad JSON: %d, want 400", res.StatusCode)
	}
	if res := h.request(http.MethodPut, "/api/layout", strings.NewReader(goodLayout), http.Header{"Content-Type": {"text/plain"}}); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d, want 415", res.StatusCode)
	}
	huge := `{"v":1,"panels":[` + strings.Repeat(" ", MaxLayoutBytes+10) + `]}`
	if res := putLayout(h, huge); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized: %d, want 413", res.StatusCode)
	}
	if after, _ := os.ReadFile(path); string(after) != string(saved) {
		t.Error("a refused layout changed the saved file")
	}

	bare := newHarness(t, Config{}, testOptions(), nil)
	if res := putLayout(bare, goodLayout); res.StatusCode != http.StatusConflict {
		t.Errorf("PUT with no layout path: %d, want 409", res.StatusCode)
	}
}

func TestUnreadableLayoutFileIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layout.json")
	if err := os.WriteFile(path, []byte(`{"v":1,"panels":[{"id":"a"`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, Config{LayoutFile: path}, testOptions(), nil)
	res := h.get("/api/layout")
	var got map[string]json.RawMessage
	decode(t, res, &got)
	if res.StatusCode != http.StatusOK || string(got["layout"]) != "null" || len(got["problem"]) < 3 || string(got["persisted"]) != "true" {
		t.Fatalf("%d %v", res.StatusCode, got)
	}
}
