package questplan

import (
	"regexp"
	"strings"
)

// colourTag matches the client's own inline colour markup, `@dbl@`, `@red@`, `@gre@` and the
// rest: an at sign, exactly three alphanumerics, an at sign.
//
// It is replaced with a SPACE rather than removed, and that is load-bearing. Stripping the tag
// outright joins the words either side of it, so a journal needle spanning where a tag stood
// stops matching. Upstream hit the mirror-image bug and recorded it in
// docs/decisions/quest-pitfalls-engine.md: replacing with a space leaves `"potion@dbl@."` as
// `"potion ."`, so a needle must not span a tag boundary next to a punctuation mark either.
// Collapsing runs of whitespace (below) makes the first problem harmless and leaves only the
// second, which is a rule for whoever writes the needle.
var colourTag = regexp.MustCompile(`(?i)@[a-z0-9]{3}@`)

// whitespaceRun matches a run of pipes and whitespace. The client uses `|` as its own line
// break inside a single settext string, so it is whitespace for matching purposes.
var whitespaceRun = regexp.MustCompile(`[|\s]+`)

// NormaliseLines flattens rendered interface or dialogue lines into one lower-cased string
// with colour tags and line breaks reduced to single spaces, which is the form every needle in
// this package and in barcrawlplan is written against. Ported from the `flattenLines` /
// `normalise` helpers in exec/primitives.ts and barcrawl/BarcrawlLogic.ts, which are
// character-identical to each other.
func NormaliseLines(lines []string) string {
	joined := strings.Join(lines, " ")
	joined = colourTag.ReplaceAllString(joined, " ")
	joined = whitespaceRun.ReplaceAllString(joined, " ")
	return strings.ToLower(strings.TrimSpace(joined))
}

// PickPreferred chooses the first option that contains any preference, preferences taking
// priority in the order given. Ported from exec/primitives.ts pickPreferred.
//
// Matching by option TEXT rather than by index is the whole point: the server reorders a menu
// between quest stages (Aubury's own menu is multi2 or multi3 depending on `%runemysteries`),
// and an index-based pick silently chooses a different line when it does.
//
// ok is false when nothing matched, and every caller must treat that as a stop rather than a
// guess. Falling through to an unmatched option is worse than stopping: loc prompts routinely
// put the refusal first, and several NPCs offer "I have come to kill you" as the alternative.
func PickPreferred(options []string, prefer []string) (string, bool) {
	for _, p := range prefer {
		needle := strings.ToLower(p)
		for _, o := range options {
			if strings.Contains(strings.ToLower(o), needle) {
				return o, true
			}
		}
	}
	return "", false
}

// LineRule chooses an option based on what the NPC just said, for a dialogue whose correct
// answer is not fixed. Ported from exec/primitives.ts LineRule.
type LineRule struct {
	// WhenLine is a lower-cased fragment of the NPC's spoken line.
	WhenLine string
	// Choose is the option to take when that fragment is present.
	Choose string
}

// PickByLine chooses an option from what was spoken, longest rule first. Ported from
// exec/primitives.ts pickByLine.
//
// Longest-first is deliberate: an overlapping shorter phrase must not win on a line that also
// contains the longer one.
func PickByLine(spoken []string, options []string, rules []LineRule) (string, bool) {
	said := NormaliseLines(spoken)
	if said == "" {
		return "", false
	}
	best, found := LineRule{}, false
	for _, r := range rules {
		if !strings.Contains(said, strings.ToLower(r.WhenLine)) {
			continue
		}
		if !found || len(r.WhenLine) > len(best.WhenLine) {
			best, found = r, true
		}
	}
	if !found {
		return "", false
	}
	needle := strings.ToLower(best.Choose)
	for _, o := range options {
		if strings.Contains(strings.ToLower(o), needle) {
			return o, true
		}
	}
	return "", false
}
