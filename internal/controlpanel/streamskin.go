package controlpanel

import (
	"fmt"
	"sort"
	"strings"
)

// Era SKINS for the stream client: one installed provider, one observation bridge, one
// game session — and a choice of which era's assets it draws that session with.
//
// Users asked for the existing 2004 view plus a RuneScape Classic, a 2009 and a 2011
// view of the same live account. This file is ONLY the selection mechanism: it does not
// contain, fetch or decode a single asset from any era. What it buys is that "which skin"
// is a validated, declared property of the installed provider that travels with it and is
// advertised to the panel and the OBS view, exactly as the renderer identity already is —
// instead of a string a page invents or a flag typed next to an unrelated file.
//
// Read this alongside streamclient.go's loadStreamManifest comment, and keep its honesty:
// a manifest is the module's own claim about itself. Declaring a skin here proves the
// provider says it can draw that era; it proves nothing about whether the assets exist on
// this machine, and NOTHING about whether they may lawfully be redistributed. See
// docs/stream_client_panel.md's era-skin section for the licence position per source
// repository, which is the part that actually decides whether a skin can ship.
//
// Why skin ids are a closed set. The same reason renderer identities are: a typo in a
// hand-edited manifest must be a startup error naming what was expected, not a panel that
// silently offers a skin nothing can draw. Adding an era means adding it here, which means
// somebody had to think about it.
const (
	// StreamSkin2004 is what the installed provider already draws: the Lost City 289
	// cache this fleet's own sessions speak, decoded in the browser by the capture
	// adapter. It is the baseline every other skin falls back to.
	StreamSkin2004 = "2004"
	// StreamSkinClassic is RuneScape Classic. It is NOT a different asset pack for the
	// same renderer — Classic is a 2D/2.5D sprite world with its own map model, so a
	// Classic skin is a separate renderer sharing the bridge, not a swap of models
	// underneath the existing one. The manifest field exists so that distinction can be
	// declared rather than discovered.
	StreamSkinClassic = "classic"
	// StreamSkin2009 is the 2009-era (RS2 ~rev 530) look.
	StreamSkin2009 = "2009"
	// StreamSkin2011 is the 2011-era (RuneTek-5, ~rev 667) look.
	StreamSkin2011 = "2011"
)

// streamSkinIdentities is the closed set above. Order matters for error text only.
var knownStreamSkins = []string{StreamSkin2004, StreamSkinClassic, StreamSkin2009, StreamSkin2011}

var streamSkinIdentities = func() map[string]bool {
	m := make(map[string]bool, len(knownStreamSkins))
	for _, id := range knownStreamSkins {
		m[id] = true
	}
	return m
}()

// StreamSkin is one era a provider declares it can draw the same session in. It is
// serialised into /api/meta.streamClient.skins verbatim, so every field here is something
// the operator is allowed to read off the panel and believe.
type StreamSkin struct {
	// ID is one of knownStreamSkins. Anything else refuses server construction.
	ID string `json:"id"`
	// Label is what the panel's skin selector shows. Required: an unlabelled option in a
	// dropdown is worse than no option.
	Label string `json:"label"`
	// Description is the provider's own one line about what this skin actually draws —
	// including what it does NOT draw. Shown verbatim beside the selector for the same
	// reason the renderer description is: a deliberately partial skin must be able to say
	// so in the operator's own view.
	Description string `json:"description,omitempty"`
	// Assets names where this skin's art comes from, in words the operator can check
	// against docs/stream_client_panel.md — e.g. "Lost City 289 cache (this fleet's own)".
	// Empty means the provider did not say, which the panel reports as "unstated".
	Assets string `json:"assets,omitempty"`
	// Fallback is the skin id this one borrows from when an entity has no asset of its
	// own in this era. The owner's rule for Classic ("where Classic has no equivalent
	// item/asset/model use the 2004 one") generalised: a 2004 id does not map 1:1 onto a
	// later era's ids either, so every non-2004 skin is expected to name a fallback.
	// Validated to be another declared skin, and to terminate.
	Fallback string `json:"fallback,omitempty"`
	// Default marks the skin used when nobody chooses one (the panel's initial value and
	// a /stream/view URL with no ?skin=). At most one skin may set it; if none does, the
	// first declared skin is the default.
	Default bool `json:"default,omitempty"`

	// The two fields below are NOT read from the manifest. They are filled in per request
	// from what is actually on this machine's disk, which is the difference between "a
	// provider says it can draw this era" and "this era can be drawn here". A provider
	// declaring the Classic skin on a machine with no Classic assets must not be offered
	// one, and must be told why instead of failing at mount with nothing to look at.

	// Available is whether this era's assets were found (internal/eraassets). Computed,
	// never declared, in exactly the way existingSession and sessionInput are.
	Available bool `json:"available"`
	// Found is one line about what was found on disk, or why nothing was — the era
	// store's own words, shown in the panel beside the skin.
	Found string `json:"found,omitempty"`
}

// validateStreamSkins checks a manifest's declared skin list. Every failure here is a
// startup error naming the bundle, for the same reason an unknown renderer identity is:
// the operator installed something specific and deserves to be told exactly what about it
// this build will not accept.
//
// It returns the default skin id alongside the error so the caller does not have to repeat
// the "first one unless somebody said otherwise" rule.
func validateStreamSkins(skins []StreamSkin) (defaultSkin string, err error) {
	if len(skins) == 0 {
		return "", nil
	}
	seen := make(map[string]bool, len(skins))
	for i, sk := range skins {
		if !streamSkinIdentities[sk.ID] {
			return "", fmt.Errorf("skin %d declares id %q, which this build does not know; expected one of %q", i, sk.ID, knownStreamSkins)
		}
		if seen[sk.ID] {
			return "", fmt.Errorf("skin %q is declared more than once", sk.ID)
		}
		seen[sk.ID] = true
		if strings.TrimSpace(sk.Label) == "" {
			return "", fmt.Errorf("skin %q has no label", sk.ID)
		}
		if sk.Default {
			if defaultSkin != "" {
				return "", fmt.Errorf("skins %q and %q both claim to be the default", defaultSkin, sk.ID)
			}
			defaultSkin = sk.ID
		}
	}
	// Fallbacks are checked in a second pass so a skin may fall back to one declared
	// after it, and are walked to a bounded depth so a cycle is a named error rather than
	// a hang the first time the panel asks what a missing model should look like.
	for _, sk := range skins {
		if sk.Fallback == "" {
			continue
		}
		if sk.Fallback == sk.ID {
			return "", fmt.Errorf("skin %q falls back to itself", sk.ID)
		}
		if !seen[sk.Fallback] {
			return "", fmt.Errorf("skin %q falls back to %q, which this provider does not declare", sk.ID, sk.Fallback)
		}
	}
	if err := checkStreamSkinFallbackCycles(skins); err != nil {
		return "", err
	}
	if defaultSkin == "" {
		defaultSkin = skins[0].ID
	}
	return defaultSkin, nil
}

// checkStreamSkinFallbackCycles walks each skin's fallback chain to its end, refusing a
// chain longer than the number of declared skins (which can only mean a loop).
func checkStreamSkinFallbackCycles(skins []StreamSkin) error {
	by := make(map[string]StreamSkin, len(skins))
	for _, sk := range skins {
		by[sk.ID] = sk
	}
	for _, sk := range skins {
		id, chain := sk.Fallback, []string{sk.ID}
		for steps := 0; id != ""; steps++ {
			if steps > len(skins) {
				return fmt.Errorf("skin fallbacks form a loop: %s", strings.Join(append(chain, id), " -> "))
			}
			chain = append(chain, id)
			id = by[id].Fallback
		}
	}
	return nil
}

// resolvedStreamSkins is the declared list with each skin's real availability filled in
// from this machine's disk. It is computed per /api/meta request; the work is a map
// lookup per skin, because internal/eraassets measured the disk once at construction.
//
// The join is deliberately AND, not OR: a skin is offerable only when the installed
// PROVIDER declares it can draw that era and this MACHINE has that era's assets. Either
// half alone produces a view that cannot work, and the two failures need different
// answers from the operator — install a different provider, or point -era-assets
// somewhere real — so they are reported separately rather than as one dead dropdown.
func (s *Server) resolvedStreamSkins() []StreamSkin {
	if len(s.streamSkins) == 0 {
		return nil
	}
	out := make([]StreamSkin, 0, len(s.streamSkins))
	for _, sk := range s.streamSkins {
		sk.Available, sk.Found = s.streamSkinAssets(sk.ID)
		out = append(out, sk)
	}
	return out
}

// streamSkinAssets answers "are this era's assets here, and what was found".
//
// 2004 is the special case and it is not a fudge: its assets are the fleet's OWN cache,
// the same pack the sessions speak, and a provider that needs it already cannot start
// without -capture-cache (see newServer). So 2004 is available exactly when the capture
// cache is configured, which for the shipped provider is always.
func (s *Server) streamSkinAssets(id string) (bool, string) {
	if id == StreamSkin2004 {
		if s.cfg.CaptureCacheDir == "" {
			return false, "this build has no -capture-cache, so the fleet's own 2004 cache cannot be decoded"
		}
		return true, "the fleet's own cache at -capture-cache"
	}
	info := s.era.Era(id)
	if info.Present {
		return true, info.Detail
	}
	return false, info.Problem
}

// streamSkinAllowed reports whether this build can actually mount that skin: declared by
// the installed provider AND backed by assets on this machine. An empty id always passes —
// it means "whatever the default is", which is what a panel or an OBS URL that never heard
// of skins sends.
func (s *Server) streamSkinAllowed(id string) bool {
	if id == "" {
		return true
	}
	for _, sk := range s.streamSkins {
		if sk.ID == id {
			available, _ := s.streamSkinAssets(id)
			return available
		}
	}
	return false
}

// streamSkinRefusal says why a skin cannot be mounted, in a whole sentence an operator can
// act on. The two reasons are genuinely different problems and must not be flattened.
func (s *Server) streamSkinRefusal(id string) string {
	for _, sk := range s.streamSkins {
		if sk.ID == id {
			_, found := s.streamSkinAssets(id)
			return fmt.Sprintf("the installed provider declares the skin %q, but its assets are not on this machine: %s", id, found)
		}
	}
	if len(s.streamSkins) == 0 {
		return "this build's stream client provider declares no era skins, so ?skin= cannot be used; drop it from the URL"
	}
	return fmt.Sprintf("the installed stream client provider does not declare the skin %q; it declares %v", id, s.streamSkinIDs())
}

// streamSkinIDs lists the installed provider's declared skin ids, for error text.
func (s *Server) streamSkinIDs() []string {
	out := make([]string, 0, len(s.streamSkins))
	for _, sk := range s.streamSkins {
		out = append(out, sk.ID)
	}
	sort.Strings(out)
	return out
}
