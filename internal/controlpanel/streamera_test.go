package controlpanel

import (
	"image/png"
	"net/http"
	"os"
	"strings"
	"testing"
)

const classicSkinManifest = `{"sceneRenderer":"webgl-tile-geometry","skins":[
	{"id":"2004","label":"2004","default":true},
	{"id":"classic","label":"RuneScape Classic","fallback":"2004"}]}`

// A build with no -era-assets must not serve era chrome, and must say WHY in the same
// words the panel shows — an empty manifest would look like a working skin with no art.
func TestEraChromeIsAbsentWithoutEraAssets(t *testing.T) {
	h := skinHarness(t, classicSkinManifest)
	res := h.get("/stream/era/classic/manifest.json")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("manifest = %d, want 404 on a build with no era assets", res.StatusCode)
	}
	if body := bodyText(t, res); !strings.Contains(body, "not on this machine") {
		t.Errorf("the refusal does not say the assets are missing: %q", body)
	}
	if res := h.get("/stream/era/2009/manifest.json"); res.StatusCode != http.StatusNotImplemented {
		t.Errorf("2009 chrome = %d, want 501: this repo has no JS5 sprite decoder", res.StatusCode)
	}
}

// With the operator's real Classic cache on disk, the endpoint serves sprites this process
// decoded itself. Skips without it: era assets are never in this repository.
func TestEraChromeServesDecodedClassicSprites(t *testing.T) {
	root := os.Getenv("RSB0T_ERA_ASSETS")
	if root == "" {
		t.Skip("RSB0T_ERA_ASSETS is not set: point it at an era asset directory")
	}
	h := skinHarnessWith(t, classicSkinManifest, Config{EraAssets: root})
	var man eraSpriteManifest
	res := h.get("/stream/era/classic/manifest.json")
	if res.StatusCode != http.StatusOK {
		t.Skipf("no Classic assets under %s: %s", root, bodyText(t, res))
	}
	decode(t, res, &man)
	if len(man.Sprites) == 0 {
		t.Fatal("the manifest lists no sprites")
	}
	// The chrome the Classic skin actually draws with. A missing one is a skin that
	// would silently draw less, so each is named rather than counted.
	want := map[string]bool{"inv1": false, "inv2": false, "compass": false, "hbar": false,
		"hbar2": false, "buttons": false, "corners": false, "arrows": false, "runescape": false}
	drawn := 0
	for _, sp := range man.Sprites {
		if _, ok := want[sp.Name]; ok {
			want[sp.Name] = true
		}
		if sp.Width <= 0 || sp.Height <= 0 || sp.PlaneWidth < sp.Width || sp.PlaneHeight < sp.Height {
			t.Fatalf("%s frame %d has impossible geometry: %+v", sp.Name, sp.Frame, sp)
		}
		img := h.get(sp.URL)
		if img.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d", sp.URL, img.StatusCode)
		}
		if ct := img.Header.Get("Content-Type"); ct != "image/png" {
			t.Fatalf("%s content type %q", sp.URL, ct)
		}
		decoded, err := png.Decode(img.Body)
		if err != nil {
			t.Fatalf("%s is not a PNG: %v", sp.URL, err)
		}
		b := decoded.Bounds()
		if b.Dx() != sp.Width || b.Dy() != sp.Height {
			t.Fatalf("%s is %dx%d, but the manifest says %dx%d", sp.URL, b.Dx(), b.Dy(), sp.Width, sp.Height)
		}
		drawn++
	}
	for name, found := range want {
		if !found {
			t.Errorf("the Classic chrome sprite %q is not in the manifest", name)
		}
	}
	t.Logf("served %d decoded Classic sprites from %s (%s)", drawn, man.Source, man.Detail)
	if res := h.get("/stream/era/classic/not-a-sprite.png"); res.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown sprite name = %d, want 404", res.StatusCode)
	}
	// Nothing may reach outside the decoded set: the names are a closed map, not a path.
	for _, bad := range []string{"..%2f..%2fsecret.png", "manifest.json.png"} {
		if res := h.get("/stream/era/classic/" + bad); res.StatusCode != http.StatusNotFound {
			t.Errorf("%q = %d, want 404", bad, res.StatusCode)
		}
	}
}
