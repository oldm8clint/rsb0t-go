// Native hiscores rendering: fetch and parse example.invalid's own real hiscores page
// server-side instead of only linking out to it (see web/panels/hiscores.js's original
// doc comment for the confirmed URL pattern).
//
// Investigated before writing this, the same way every other "is there a shortcut"
// question in this project gets answered — checked live, not assumed:
//   - No separate JSON API: loaded a real hiscores page in a browser and read its own
//     network requests — the skills table arrives as part of the server-rendered HTML
//     itself, nothing fetched separately client-side.
//   - No framing block either (checked via a plain HEAD/GET for X-Frame-Options/CSP
//     frame-ancestors — neither header is present), so an iframe embed was a real,
//     working alternative; parsing was chosen instead because it renders in this app's
//     own style rather than pulling in example.invalid's full page chrome (nav, footer,
//     donation blurb) inside a panel.
//   - The page itself is plain, old-school HTML — a single, very regular table (one
//     <tr> per skill, Overall first, in this content build's own skill order) with no
//     JS-rendered content to fight — so a direct regex over the raw response body is
//     honest here, not a fragile shortcut: internal/fleet.FetchProductionModulus
//     already does the exact same "regex over a raw fetched page" thing for the RSA
//     login modulus, so this follows established precedent in this codebase rather
//     than inventing a new technique.
//   - An unknown player 404s (confirmed live with a garbage name) — surfaced as
//     errHiscoresNotFound so the HTTP handler can answer its own clean 404 rather than
//     a generic fetch error.
package controlpanel

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// hiscoresBase is example.invalid's own per-player hiscores URL — confirmed live against
// the real site (see web/panels/hiscores.js): lowercase the name, spaces to underscores.
const hiscoresBase = "https://example.invalid/hiscores/player/"

// hiscoreRowPattern matches one <tr> of the page's own skill table: a plain skill name,
// then rank/level/xp cells each holding either a thousands-comma'd integer or the
// site's own literal "&mdash;" placeholder (raw HTML, not entity-decoded, since this is
// a regex over the response body — see parseHiscoreCell).
var hiscoreRowPattern = regexp.MustCompile(`<tr><td align=left>([^<]+)</td><td align=right>([^<]+)</td><td align=right>([^<]+)</td><td align=right>([^<]+)</td></tr>`)

// HiscoreSkill is one row of a player's hiscores, in the site's own display order
// (Overall first, then this content build's real skills).
type HiscoreSkill struct {
	Name   string `json:"name"`
	Ranked bool   `json:"ranked"`
	Rank   int    `json:"rank,omitempty"`
	Level  int    `json:"level,omitempty"`
	XP     int64  `json:"xp,omitempty"`
}

// HiscoresResult is one player's parsed hiscores page.
type HiscoresResult struct {
	Player string         `json:"player"`
	URL    string         `json:"url"`
	Skills []HiscoreSkill `json:"skills"`
}

var errHiscoresNotFound = errors.New("controlpanel: no such player on example.invalid's hiscores")

func normaliseHiscoresName(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
}

func hiscoresURL(name string) string {
	return hiscoresBase + normaliseHiscoresName(name)
}

// parseHiscoreCell parses one rank/level/xp cell: digits with optional thousands
// commas, or the literal "&mdash;" placeholder meaning "not ranked in this skill".
func parseHiscoreCell(s string) (int, bool) {
	s = strings.ReplaceAll(s, ",", "")
	if s == "" || strings.Contains(s, "mdash") {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// fetchHiscores fetches and parses one player's real hiscores page.
func fetchHiscores(client *http.Client, player string) (HiscoresResult, error) {
	url := hiscoresURL(player)
	resp, err := client.Get(url)
	if err != nil {
		return HiscoresResult{}, fmt.Errorf("controlpanel: fetching hiscores: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return HiscoresResult{}, errHiscoresNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return HiscoresResult{}, fmt.Errorf("controlpanel: hiscores site returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return HiscoresResult{}, fmt.Errorf("controlpanel: reading hiscores page: %w", err)
	}
	rows := hiscoreRowPattern.FindAllSubmatch(body, -1)
	if len(rows) == 0 {
		return HiscoresResult{}, fmt.Errorf("controlpanel: hiscores page had no recognisable skill rows")
	}
	skills := make([]HiscoreSkill, 0, len(rows))
	for _, row := range rows {
		sk := HiscoreSkill{Name: string(row[1])}
		if rank, ok := parseHiscoreCell(string(row[2])); ok {
			sk.Rank = rank
			sk.Ranked = true
		}
		if level, ok := parseHiscoreCell(string(row[3])); ok {
			sk.Level = level
		}
		if xp, ok := parseHiscoreCell(string(row[4])); ok {
			sk.XP = int64(xp)
		}
		skills = append(skills, sk)
	}
	return HiscoresResult{Player: player, URL: url, Skills: skills}, nil
}

// hiscoresCacheTTL bounds how often the same player's page is actually re-fetched from
// example.invalid — a lookup panel an operator might poke a few times in a row (checking a
// friend, re-clicking the same account) shouldn't hammer someone else's site for
// identical data.
const hiscoresCacheTTL = 30 * time.Second

type hiscoresCacheEntry struct {
	result HiscoresResult
	err    error
	at     time.Time
}

// hiscoresCache is a small TTL cache in front of fetchHiscores, keyed by the
// normalised player name so "Zezima"/"zezima"/"ZEZIMA" share one entry.
type hiscoresCache struct {
	client *http.Client
	mu     sync.Mutex
	data   map[string]hiscoresCacheEntry
}

func newHiscoresCache() *hiscoresCache {
	return &hiscoresCache{client: &http.Client{Timeout: 8 * time.Second}, data: make(map[string]hiscoresCacheEntry)}
}

func (c *hiscoresCache) get(player string) (HiscoresResult, error) {
	key := normaliseHiscoresName(player)
	if key == "" {
		return HiscoresResult{}, fmt.Errorf("controlpanel: player name is required")
	}
	c.mu.Lock()
	if e, ok := c.data[key]; ok && time.Since(e.at) < hiscoresCacheTTL {
		c.mu.Unlock()
		return e.result, e.err
	}
	c.mu.Unlock()

	result, err := fetchHiscores(c.client, player)

	c.mu.Lock()
	c.data[key] = hiscoresCacheEntry{result: result, err: err, at: time.Now()}
	c.mu.Unlock()
	return result, err
}
