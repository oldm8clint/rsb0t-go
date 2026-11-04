package controlpanel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"sort"
	"strings"
	"sync"

	"rsb0t-go/internal/eraassets"
)

// Serving an era's own 2D art to the stream client provider.
//
// The Classic skin draws RuneScape Classic's own interface furniture — its panel header
// bars, tab icons, compass, status bars, hit splats and speech bubble — around the
// existing 2004 world viewport. Those sprites live in the operator's -era-assets root
// (internal/eraassets), never in this repository, for the three reasons that package's
// comment gives. They therefore have to reach the browser through this process.
//
// What this file is careful about:
//
//   - It decodes rather than proxies. The browser is handed PNGs this process produced
//     from the operator's .jag with internal/eraassets' ported decoder, so a malformed
//     or wrong-revision archive fails HERE, with a readable error, instead of becoming a
//     browser-side mystery. It also means no raw cache file is ever served.
//   - Names are a closed set. A request can only ever name a sprite the decoder produced;
//     there is no path joining and nothing reads the disk per request.
//   - It is off unless the assets are present. No -era-assets, or a root with no Classic
//     set, answers 404 with the same sentence the panel shows — not an empty manifest
//     that would look like a working skin with no art.
//
// This serves 2D chrome only. It does not serve models, map data or item sprites: see
// docs/stream_client_panel.md for what else is on disk and what each would still cost.

// eraSpriteInfo is one decoded frame as the provider's manifest describes it. Offset and
// plane travel with the frame because Classic's own interface code positions frames by
// them, so anything drawing the chrome needs the same numbers.
type eraSpriteInfo struct {
	Name        string `json:"name"`
	Frame       int    `json:"frame"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	PlaneWidth  int    `json:"planeWidth"`
	PlaneHeight int    `json:"planeHeight"`
	OffsetX     int    `json:"offsetX"`
	OffsetY     int    `json:"offsetY"`
	URL         string `json:"url"`
}

type eraSpriteManifest struct {
	Era     string          `json:"era"`
	Source  string          `json:"source"`
	Detail  string          `json:"detail"`
	Sprites []eraSpriteInfo `json:"sprites"`
}

// eraSprites is the decoded-once cache for one era's chrome sprites.
type eraSprites struct {
	once     sync.Once
	err      error
	manifest eraSpriteManifest
	png      map[string][]byte // "<name>-<frame>" -> encoded PNG
}

const eraSpritePrefix = "/stream/era/"

// classicChrome decodes media58.jag's interface sheets once and keeps the PNGs. The whole
// set is 33 frames and ~77k opaque pixels, so this is tens of kilobytes held for the life
// of the process, and only on a build that was given -era-assets.
func (s *Server) classicChrome() (*eraSprites, error) {
	s.eraOnce.Do(func() { s.eraClassic = &eraSprites{} })
	c := s.eraClassic
	c.once.Do(func() {
		if !s.era.Available(eraassets.EraClassic) {
			c.err = fmt.Errorf("the RuneScape Classic assets are not on this machine: %s", s.era.Era(eraassets.EraClassic).Problem)
			return
		}
		raw, err := s.era.ClassicArchive("media58.jag")
		if err != nil {
			c.err = err
			return
		}
		sprites, err := eraassets.DecodeClassicMedia(raw)
		if err != nil {
			c.err = fmt.Errorf("media58.jag did not decode: %w", err)
			return
		}
		info := s.era.Era(eraassets.EraClassic)
		c.png = make(map[string][]byte, len(sprites))
		c.manifest = eraSpriteManifest{Era: eraassets.EraClassic, Source: s.era.ClassicCacheDir(), Detail: info.Detail}
		for _, sp := range sprites {
			key := fmt.Sprintf("%s-%d", sp.Name, sp.Frame)
			var buf bytes.Buffer
			if err := png.Encode(&buf, sp.Image); err != nil {
				c.err = fmt.Errorf("%s could not be encoded: %w", key, err)
				return
			}
			c.png[key] = buf.Bytes()
			c.manifest.Sprites = append(c.manifest.Sprites, eraSpriteInfo{
				Name: sp.Name, Frame: sp.Frame, Width: sp.Width, Height: sp.Height,
				PlaneWidth: sp.PlaneWidth, PlaneHeight: sp.PlaneHeight,
				OffsetX: sp.OffsetX, OffsetY: sp.OffsetY,
				URL: eraSpritePrefix + eraassets.EraClassic + "/" + key + ".png",
			})
		}
		sort.Slice(c.manifest.Sprites, func(i, j int) bool {
			a, b := c.manifest.Sprites[i], c.manifest.Sprites[j]
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.Frame < b.Frame
		})
	})
	if c.err != nil {
		return nil, c.err
	}
	return c, nil
}

// handleStreamEraAsset serves /stream/era/<era>/manifest.json and
// /stream/era/<era>/<name>-<frame>.png. Only Classic is implemented; 2009's art is in a
// JS5 filestore whose sprite format this repo has no decoder for, and answering anything
// other than "not implemented" for it would be a claim nothing supports.
func (s *Server) handleStreamEraAsset(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, eraSpritePrefix)
	era, file, ok := strings.Cut(rest, "/")
	if !ok || file == "" || strings.Contains(file, "/") {
		http.NotFound(w, r)
		return
	}
	if era != eraassets.EraClassic {
		writeError(w, http.StatusNotImplemented,
			fmt.Sprintf("this build serves era chrome for %q only; %q has no decoder here", eraassets.EraClassic, era))
		return
	}
	chrome, err := s.classicChrome()
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	if file == "manifest.json" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(chrome.manifest)
		return
	}
	body, ok := chrome.png[strings.TrimSuffix(file, ".png")]
	if !ok || !strings.HasSuffix(file, ".png") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(body)
}
