package controlpanel

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Era skins (streamskin.go). Every test here asserts the same thing from a different
// angle: a skin the installed provider did not declare must produce a clear refusal with
// the real list in it, never a view that quietly draws a different era.

func TestStreamSkinManifestValidation(t *testing.T) {
	ok := func(id string) StreamSkin { return StreamSkin{ID: id, Label: id} }
	cases := []struct {
		name    string
		skins   []StreamSkin
		want    string // expected default skin id when valid
		wantErr string // substring the error must name, when invalid
	}{
		{name: "none declared", skins: nil, want: ""},
		{name: "one, implicitly the default", skins: []StreamSkin{ok("2004")}, want: "2004"},
		{name: "first is the default unless marked", skins: []StreamSkin{ok("2004"), ok("2011")}, want: "2004"},
		{name: "an explicit default wins", skins: []StreamSkin{ok("2004"), {ID: "2011", Label: "2011", Default: true}}, want: "2011"},
		{name: "a fallback to a declared skin", skins: []StreamSkin{ok("2004"), {ID: "classic", Label: "RSC", Fallback: "2004"}}, want: "2004"},

		{name: "unknown id", skins: []StreamSkin{ok("2013")}, wantErr: `"2013"`},
		{name: "duplicate id", skins: []StreamSkin{ok("2004"), ok("2004")}, wantErr: "more than once"},
		{name: "no label", skins: []StreamSkin{{ID: "2004"}}, wantErr: "no label"},
		{name: "blank label", skins: []StreamSkin{{ID: "2004", Label: "   "}}, wantErr: "no label"},
		{name: "two defaults", skins: []StreamSkin{{ID: "2004", Label: "a", Default: true}, {ID: "2009", Label: "b", Default: true}}, wantErr: "both claim to be the default"},
		{name: "fallback to an undeclared skin", skins: []StreamSkin{{ID: "classic", Label: "RSC", Fallback: "2004"}}, wantErr: "does not declare"},
		{name: "fallback to itself", skins: []StreamSkin{{ID: "2004", Label: "a", Fallback: "2004"}}, wantErr: "falls back to itself"},
		{name: "fallback loop", skins: []StreamSkin{
			{ID: "classic", Label: "a", Fallback: "2009"},
			{ID: "2009", Label: "b", Fallback: "2011"},
			{ID: "2011", Label: "c", Fallback: "classic"},
		}, wantErr: "loop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateStreamSkins(tc.skins)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("accepted %v, want an error naming %q", tc.skins, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not name %q — an operator has to be able to see what is wrong", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("rejected a valid skin list: %v", err)
			}
			if got != tc.want {
				t.Errorf("default skin = %q, want %q", got, tc.want)
			}
		})
	}
}

// A manifest declaring a skin this build cannot name must refuse to start the whole
// control panel, exactly as an unknown renderer identity does — a dropdown quietly
// missing an entry the operator installed is the least useful possible answer.
func TestUnknownSkinInAManifestRefusesServerConstruction(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "client.js")
	if err := os.WriteFile(bundle, []byte("export async function mount(){}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{"sceneRenderer":"webgl-tile-geometry","skins":[{"id":"2004","label":"2004"},{"id":"2013","label":"2013"}]}`
	if err := os.WriteFile(bundle+".manifest.json", []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := newServer(Config{Enabled: true, Listen: "127.0.0.1:0", StreamClientBundle: bundle}, &staticSource{}, testOptions())
	if err == nil || !strings.Contains(err.Error(), "2013") {
		t.Fatalf("a manifest declaring an unknown era skin was accepted or reported unclearly: %v", err)
	}
}

func skinHarness(t *testing.T, manifest string) *harness {
	t.Helper()
	return skinHarnessWith(t, manifest, Config{})
}

// skinHarnessWith is skinHarness with extra config. A skin's availability is COMPUTED from
// what is on disk, so a test that wants an AVAILABLE skin has to give the server something
// real to find: CaptureCacheDir for 2004 (whose assets are the fleet's own cache) and
// EraAssets for any other era. This helper sets CaptureCacheDir by default, so 2004 is
// available unless a test deliberately wants it not to be.
func skinHarnessWith(t *testing.T, manifest string, cfg Config) *harness {
	t.Helper()
	dir := t.TempDir()
	bundle := filepath.Join(dir, "client.js")
	if err := os.WriteFile(bundle, []byte("export async function mount(){}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle+".manifest.json", []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.StreamClientBundle = bundle
	if cfg.CaptureCacheDir == "" {
		cfg.CaptureCacheDir = dir
	}
	src := &renderSource{staticSource: &staticSource{accounts: []AccountStatus{ingameAccount("a")}}}
	return newHarness(t, cfg, testOptions(), src)
}

func TestMetaAdvertisesTheInstalledProvidersSkins(t *testing.T) {
	h := skinHarness(t, `{"sceneRenderer":"webgl-tile-geometry","skins":[
		{"id":"2004","label":"2004","assets":"this fleet's own cache"},
		{"id":"2011","label":"2011","fallback":"2004","default":true}]}`)
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if len(m.StreamClient.Skins) != 2 {
		t.Fatalf("skins = %+v, want the two the manifest declares", m.StreamClient.Skins)
	}
	if m.StreamClient.Skin != "2011" {
		t.Errorf("default skin = %q, want the one the manifest marks default", m.StreamClient.Skin)
	}
	if m.StreamClient.Skins[0].Assets != "this fleet's own cache" || m.StreamClient.Skins[1].Fallback != "2004" {
		t.Errorf("the provider's own words did not reach the panel verbatim: %+v", m.StreamClient.Skins)
	}

	// A provider that declares no skins must leave the field out entirely, so the panel
	// shows no selector rather than a choice of one.
	plain := skinHarness(t, `{"sceneRenderer":"webgl-tile-geometry"}`)
	var pm metaBody
	decode(t, plain.get("/api/meta"), &pm)
	if len(pm.StreamClient.Skins) != 0 || pm.StreamClient.Skin != "" {
		t.Errorf("a provider declaring no skins advertised %+v / %q", pm.StreamClient.Skins, pm.StreamClient.Skin)
	}
}

// The OBS view URL must refuse an undeclared skin with the real list in the message. An
// OBS Browser Source retries a failed page forever, so a permanent mistake in the URL has
// to be readable once rather than watched as a black source.
func TestStreamViewRefusesASkinTheProviderDoesNotDeclare(t *testing.T) {
	h := skinHarness(t, `{"sceneRenderer":"webgl-tile-geometry","skins":[{"id":"2004","label":"2004"}]}`)
	for _, url := range []string{"/stream/view?account=a", "/stream/view?account=a&skin=2004"} {
		if res := h.get(url); res.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", url, res.StatusCode)
		}
	}
	res := h.get("/stream/view?account=a&skin=2011")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("an undeclared skin = %d, want 400", res.StatusCode)
	}
	if body := bodyText(t, res); !strings.Contains(body, "2011") || !strings.Contains(body, "2004") {
		t.Errorf("the refusal %q names neither the bad skin nor what is installed", body)
	}
	// Unknown to this BUILD, not merely to the provider: same answer, same clarity.
	if res := h.get("/stream/view?account=a&skin=2013"); res.StatusCode != http.StatusBadRequest {
		t.Errorf("a skin id this build does not know = %d, want 400", res.StatusCode)
	}

	// A provider that declares no skins at all: ?skin= is a mistake whatever its value,
	// and saying "it declares []" would be useless, so it says so in words.
	plain := skinHarness(t, `{"sceneRenderer":"webgl-tile-geometry"}`)
	if res := plain.get("/stream/view?account=a"); res.StatusCode != http.StatusOK {
		t.Errorf("a skinless provider refused a URL with no skin: %d", res.StatusCode)
	}
	res = plain.get("/stream/view?account=a&skin=2004")
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(bodyText(t, res), "no era skins") {
		t.Errorf("?skin= against a skinless provider = %d %q", res.StatusCode, bodyText(t, res))
	}
}

// The provider module and its manifest must agree about which eras it can draw. The
// manifest is what the Go server validates and the panel offers; the module's own SKINS
// list is what actually refuses to mount. If they drift, the panel offers a skin the
// module throws on — which looks like a broken build rather than a missing asset set.
func TestShippedProviderSkinsMatchTheModuleThatDrawsThem(t *testing.T) {
	const bundle = "../../tools/stream/cache-terrain-client.js"
	m, err := loadStreamManifest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateStreamSkins(m.Skins); err != nil {
		t.Fatalf("the shipped manifest's skins do not validate: %v", err)
	}
	if len(m.Skins) == 0 {
		t.Fatal("the shipped cache-terrain provider declares no skins")
	}
	declared := make([]string, 0, len(m.Skins))
	for _, sk := range m.Skins {
		declared = append(declared, sk.ID)
	}
	source, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^export const SKINS = \[([^\]]*)\];`).FindSubmatch(source)
	if match == nil {
		t.Fatal("the shipped provider no longer exports a SKINS list; this drift guard cannot check it")
	}
	var inModule []string
	for _, raw := range strings.Split(string(match[1]), ",") {
		if id := strings.Trim(strings.TrimSpace(raw), `'"`); id != "" {
			inModule = append(inModule, id)
		}
	}
	sort.Strings(declared)
	sort.Strings(inModule)
	if strings.Join(declared, ",") != strings.Join(inModule, ",") {
		t.Errorf("manifest declares skins %v but the module draws %v", declared, inModule)
	}
}

// A skin the provider DECLARES but whose ASSETS are not on this machine must be advertised
// as unavailable, refused by /stream/view, and refused with a message saying it is the
// assets that are missing — not that the provider cannot draw the era. Those are different
// problems with different fixes (point -era-assets somewhere real, versus install another
// provider), and collapsing them would send the operator after the wrong one.
func TestADeclaredSkinWithNoAssetsIsAdvertisedButNotAvailable(t *testing.T) {
	h := skinHarness(t, `{"sceneRenderer":"webgl-tile-geometry","skins":[
		{"id":"2004","label":"2004","default":true},
		{"id":"classic","label":"RuneScape Classic","fallback":"2004"}]}`)
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	if len(m.StreamClient.Skins) != 2 {
		t.Fatalf("skins = %+v, want both declared skins still advertised", m.StreamClient.Skins)
	}
	by := map[string]StreamSkin{}
	for _, sk := range m.StreamClient.Skins {
		by[sk.ID] = sk
	}
	if !by["2004"].Available {
		t.Errorf("2004 is unavailable although a capture cache is configured: %q", by["2004"].Found)
	}
	if by["classic"].Available {
		t.Error("the Classic skin is available on a build started without -era-assets")
	}
	if by["classic"].Found == "" {
		t.Error("an unavailable skin says nothing about why")
	}
	res := h.get("/stream/view?account=a&skin=classic")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a declared-but-assetless skin = %d, want 400", res.StatusCode)
	}
	if body := bodyText(t, res); !strings.Contains(body, "assets are not on this machine") {
		t.Errorf("the refusal blames the provider rather than the missing assets: %q", body)
	}
	if res := h.get("/stream/view?account=a&skin=2004"); res.StatusCode != http.StatusOK {
		t.Errorf("the available skin was refused: %d", res.StatusCode)
	}
}

// The other half of the join: with REAL assets on disk the same manifest makes the skin
// available. Skips without them, since era assets are never in this repository.
func TestARealEraAssetRootMakesItsSkinAvailable(t *testing.T) {
	root := os.Getenv("RSB0T_ERA_ASSETS")
	if root == "" {
		t.Skip("RSB0T_ERA_ASSETS is not set: point it at an era asset directory to check real assets")
	}
	h := skinHarnessWith(t, `{"sceneRenderer":"webgl-tile-geometry","skins":[
		{"id":"2004","label":"2004","default":true},
		{"id":"classic","label":"RuneScape Classic","fallback":"2004"},
		{"id":"2009","label":"2009","fallback":"2004"}]}`, Config{EraAssets: root})
	var m metaBody
	decode(t, h.get("/api/meta"), &m)
	available := 0
	for _, sk := range m.StreamClient.Skins {
		t.Logf("%s available=%v found=%q", sk.ID, sk.Available, sk.Found)
		if sk.ID == "2004" || !sk.Available {
			continue
		}
		available++
		if sk.Found == "" {
			t.Errorf("%s is available but says nothing about what was found on disk", sk.ID)
		}
		if res := h.get("/stream/view?account=a&skin=" + sk.ID); res.StatusCode != http.StatusOK {
			t.Errorf("the available skin %s was refused by /stream/view: %d %s", sk.ID, res.StatusCode, bodyText(t, res))
		}
	}
	if available == 0 {
		t.Skipf("neither era's assets are under %s", root)
	}
}
