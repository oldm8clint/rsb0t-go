package essenceplan

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/tile"
)

// Settings are EssenceMiner's settings. The first five JSON keys are the TS manifest's
// (EssenceMiner.ts's settingsSchema), so an account's fleet.config.json settings object decodes straight
// into DefaultSettings(); the rest are the essence SUPPLY (docs/runecraft_p2_2026-10-13.md): a miner that
// works from the Wizards' Guild and keeps the mule's Rune essence row stocked.
type Settings struct {
	// Pickaxe is BestAvailable or one of PickTiers' Tier names.
	Pickaxe string `json:"pickaxe"`
	// Mule is the MuleReceiver account; blank never hands anything over.
	Mule string `json:"mule"`
	// MuleAt is the banked essence count that starts a hand-over. Blank in the settings it is MuleEssenceAt
	// (10,000) at Aubury, the original script's number, and GuildMuleAt (5,000) at the other gateways.
	MuleAt int `json:"muleAt"`
	// MuleKeep is a comma-separated list of bank item names that stay with the miner. Pickaxes
	// always stay regardless of this list.
	MuleKeep  string `json:"muleKeep"`
	LogDetail string `json:"logDetail"`

	// Gateway is the way into the essence mine: "aubury" (the original script: the Varrock east bank),
	// "guild" (the Wizards' Guild's Wizard Distentor, Magic 66, the Yanille bank 20 steps from the exit) or
	// "cromperty". Only the original gateway runs the original loop; the others run the supply loop.
	Gateway string `json:"gateway"`
	// MuleStopAt stops mining while the mule's published Rune essence has reached it (0 never stops): the
	// mule's bank has 220 rows and 150,000 essence is enough for any campaign. The mule's own figure is the
	// measure; one the mule has not published is replaced by this miner's own deliveries.
	MuleStopAt int `json:"muleStopAt"`
	// MuleItems is what a hand-over carries: "all" (every bank row but the pickaxes and the keep list, the
	// original script) or "essence" (one note stack of Rune essence and nothing else). Blank is "all" at
	// Aubury and "essence" at the other gateways: a Guild miner is an account taken from ScarabSwarm, whose
	// bank holds its own food, coins and quest items. "all" at another gateway is refused.
	MuleItems string `json:"muleItems"`
	// MuleAtTile is where the mule is met, "x,z" or "x,z,level"; blank is the post its heartbeat publishes
	// (the supply loop) or the Varrock east bank (the original loop).
	MuleAtTile string `json:"muleAtTile"`
	// Batch is the essence one mining stretch brings before the miner looks at the mule and the bank again
	// (rounded up to whole loads of LoadSize).
	Batch int `json:"batch"`
	// BuyPickaxe lets the supply loop make a trip to Nurmof's shop in the Dwarven Mine under Falador for a better
	// pickaxe when its Mining level allows one and its coins pay for it (see upgrade.go). Off by default: the trip is
	// not proven live.
	BuyPickaxe bool `json:"buyPickaxe"`
	// MineTickManip makes the supply loop's mining stretches write a knife-on-log batch in the stale tick after every roll, so the
	// rock rolls every 3 ticks instead of every pickaxe-rate-plus-one (bronze 8, iron 7, steel 6, mithril 5, adamant 4; a rune
	// pickaxe's 3 is the floor and never runs it). It needs a Knife and a plain Log in the pack or the Yanille bank; a load is then
	// 25 essence, not 27. Off by default: it is read from content 289 and modelled tick by tick, not yet run on the live server
	// (internal/scripts/minemanipplan, essencerunner/minemanip.go). Only the supply loop (a gateway other than aubury) has it.
	MineTickManip bool `json:"mineTickManip"`
}

// The values of MuleItems.
const (
	MuleItemsAll     = "all"
	MuleItemsEssence = "essence"
)

// Bounds of the supply settings.
const (
	MinBatch = LoadSize
	MaxBatch = 100_000
	// MaxMuleStopAt is the largest muleStopAt: nothing needs more than a million essence on the mule.
	MaxMuleStopAt = 1_000_000
)

// DefaultSettings are the manifest defaults.
func DefaultSettings() Settings {
	return Settings{
		Pickaxe:    BestAvailable,
		Mule:       "",
		MuleAt:     MuleEssenceAt,
		MuleKeep:   "",
		LogDetail:  "Normal",
		Gateway:    GatewayAubury,
		MuleStopAt: MuleEssenceCap,
		MuleItems:  MuleItemsAll,
		MuleAtTile: "",
		Batch:      DefaultBatch,
	}
}

// Normalised applies the TS setting readers' fallbacks and bounds: a blank pickaxe selection falls
// back to BestAvailable, and MuleAt is clamped to 0 through donorplan.MaxMuleAt the same way every
// other script's mule threshold is. The supply settings are brought inside their bounds and a blank
// gateway or hand-over kind takes its default.
func (s Settings) Normalised() Settings {
	s.Pickaxe = strings.TrimSpace(s.Pickaxe)
	if s.Pickaxe == "" {
		s.Pickaxe = BestAvailable
	}
	if s.MuleAt < 0 {
		s.MuleAt = 0
	}
	if s.MuleAt > donorplan.MaxMuleAt {
		s.MuleAt = donorplan.MaxMuleAt
	}
	s.Gateway = strings.ToLower(strings.TrimSpace(s.Gateway))
	if s.Gateway == "" {
		s.Gateway = GatewayAubury
	}
	s.MuleItems = strings.ToLower(strings.TrimSpace(s.MuleItems))
	if s.MuleItems == "" {
		s.MuleItems = MuleItemsAll
		if s.Gateway != GatewayAubury {
			s.MuleItems = MuleItemsEssence
		}
	}
	if s.MuleStopAt < 0 {
		s.MuleStopAt = 0
	}
	if s.MuleStopAt > MaxMuleStopAt {
		s.MuleStopAt = MaxMuleStopAt
	}
	if s.Batch < MinBatch {
		s.Batch = MinBatch
	}
	if s.Batch > MaxBatch {
		s.Batch = MaxBatch
	}
	s.MuleAtTile = strings.TrimSpace(s.MuleAtTile)
	return s
}

// Verbose reports the Verbose log detail.
func (s Settings) Verbose() bool {
	return strings.EqualFold(strings.TrimSpace(s.LogDetail), "verbose")
}

// Donor is the hand-over settings this script's Settings imply.
func (s Settings) Donor() donorplan.Settings {
	return donorplan.NewSettings(s.Mule, s.MuleAt, s.MuleKeep)
}

// SupplyLoop reports whether the miner runs the supply loop (a stretch of mining and banking at the
// gateway's own bank, hand-overs of one note stack, the mule's stock watched) instead of the original script's
// loop: every gateway but Aubury.
func (s Settings) SupplyLoop() bool { return s.Gateway != GatewayAubury }

// GatewayInfo is the gateway the settings name (Aubury's when the key is not one: DecodeSettings refuses
// that).
func (s Settings) GatewayInfo() Gateway {
	if g, ok := GatewayByKey(s.Gateway); ok {
		return g
	}
	return AuburyGateway()
}

// Meet is the tile MuleAtTile names; ok is false when it is blank or unreadable.
func (s Settings) Meet() (tile.Tile, bool) { return ParseTile(s.MuleAtTile) }

// ParseTile reads "x,z" or "x,z,level".
func ParseTile(raw string) (tile.Tile, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return tile.Tile{}, false
	}
	parts := strings.Split(raw, ",")
	if len(parts) < 2 || len(parts) > 3 {
		return tile.Tile{}, false
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return tile.Tile{}, false
		}
		v[i] = n
	}
	if v[0] == 0 || v[1] == 0 || v[2] > 3 {
		return tile.Tile{}, false
	}
	return tile.Tile{X: v[0], Z: v[1], Level: v[2]}, true
}

// The settings keys EssenceMiner reads. homeWorld and pinWorld are the fleet's own, put into any script's
// settings (the world a login uses, the world a script is pinned to), and are not this script's to refuse.
const (
	KeyPickaxe    = "pickaxe"
	KeyMule       = "mule"
	KeyMuleAt     = "muleAt"
	KeyMuleKeep   = "muleKeep"
	KeyLogDetail  = "logDetail"
	KeyGateway    = "gateway"
	KeyMuleStopAt = "muleStopAt"
	KeyMuleItems  = "muleItems"
	KeyMuleAtTile = "muleAtTile"
	KeyBatch      = "batch"
	KeyBuyPickaxe = "buyPickaxe"

	KeyMineTickManip = "mineTickManip"

	keyHomeWorld = "homeWorld"
	keyPinWorld  = "pinWorld"
)

// KnownKeys lists every key the script reads, sorted.
func KnownKeys() []string {
	out := []string{KeyPickaxe, KeyMule, KeyMuleAt, KeyMuleKeep, KeyLogDetail, KeyGateway, KeyMuleStopAt, KeyMuleItems, KeyMuleAtTile, KeyBatch, KeyBuyPickaxe, KeyMineTickManip}
	sort.Strings(out)
	return out
}

func isKnownKey(k string) bool {
	for _, n := range KnownKeys() {
		if n == k {
			return true
		}
	}
	return k == keyHomeWorld || k == keyPinWorld
}

// DecodeSettings is the strict load: a key that is not a setting, a value of the wrong kind or one outside its
// range is an error (a typo in muleStopAt must not silently leave the miner running without its stop). Numbers
// may be JSON numbers or numeric strings (the control panel's form sends strings). A missing muleAt is
// MuleEssenceAt at Aubury and GuildMuleAt elsewhere.
func DecodeSettings(raw map[string]interface{}) (Settings, error) {
	s := DefaultSettings()
	var problems []string
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	muleAtSet, itemsSet := false, false
	str := func(k string, v interface{}) (string, bool) {
		x, ok := v.(string)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %v is not text", k, v))
		}
		return x, ok
	}
	num := func(k string, v interface{}, lo, hi int, dst *int) {
		n, ok := toInt(v)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %v is not a whole number", k, v))
			return
		}
		if n < lo || n > hi {
			problems = append(problems, fmt.Sprintf("%s: %d is outside %d..%d", k, n, lo, hi))
			return
		}
		*dst = n
	}
	for _, k := range keys {
		v := raw[k]
		if !isKnownKey(k) {
			problems = append(problems, fmt.Sprintf("%s is not a setting (known: %s)", k, strings.Join(KnownKeys(), ", ")))
			continue
		}
		switch k {
		case KeyPickaxe:
			if x, ok := str(k, v); ok {
				s.Pickaxe = x
			}
		case KeyMule:
			if x, ok := str(k, v); ok {
				s.Mule = x
			}
		case KeyMuleKeep:
			if x, ok := str(k, v); ok {
				s.MuleKeep = x
			}
		case KeyLogDetail:
			if x, ok := str(k, v); ok {
				s.LogDetail = x
			}
		case KeyMuleAt:
			// Out of range is clamped by Normalised (0 never hands over), as it always was.
			muleAtSet = true
			num(k, v, math.MinInt32, math.MaxInt32, &s.MuleAt)
		case KeyMuleStopAt:
			num(k, v, 0, MaxMuleStopAt, &s.MuleStopAt)
		case KeyBatch:
			num(k, v, MinBatch, MaxBatch, &s.Batch)
		case KeyBuyPickaxe:
			if b, ok := toBool(v); ok {
				s.BuyPickaxe = b
			} else {
				problems = append(problems, fmt.Sprintf("%s: %v is not true or false", k, v))
			}
		case KeyMineTickManip:
			if b, ok := toBool(v); ok {
				s.MineTickManip = b
			} else {
				problems = append(problems, fmt.Sprintf("%s: %v is not true or false", k, v))
			}
		case KeyGateway:
			if x, ok := str(k, v); ok {
				g := strings.ToLower(strings.TrimSpace(x))
				if _, found := GatewayByKey(g); g != "" && !found {
					problems = append(problems, fmt.Sprintf("%s: %q is not one of %s", k, g, strings.Join(GatewayKeys(), ", ")))
				} else if g != "" {
					s.Gateway = g
				}
			}
		case KeyMuleItems:
			if x, ok := str(k, v); ok {
				m := strings.ToLower(strings.TrimSpace(x))
				switch m {
				case "", MuleItemsAll, MuleItemsEssence:
					s.MuleItems, itemsSet = m, m != ""
				default:
					problems = append(problems, fmt.Sprintf("%s: %q is not %q or %q", k, m, MuleItemsAll, MuleItemsEssence))
				}
			}
		case KeyMuleAtTile:
			if x, ok := str(k, v); ok {
				if strings.TrimSpace(x) != "" {
					if _, parsed := ParseTile(x); !parsed {
						problems = append(problems, fmt.Sprintf("%s: %q is not x,z or x,z,level", k, x))
					}
				}
				s.MuleAtTile = x
			}
		}
	}
	if itemsSet && s.MuleItems == MuleItemsAll && s.Gateway != GatewayAubury {
		problems = append(problems, fmt.Sprintf("%s: %q is the %s script's hand-over of the whole bank; the %s gateway hands over one note stack of Rune essence (%q)",
			KeyMuleItems, MuleItemsAll, GatewayAubury, s.Gateway, MuleItemsEssence))
	}
	if len(problems) > 0 {
		return Settings{}, fmt.Errorf("EssenceMiner settings: %s", strings.Join(problems, "; "))
	}
	if !muleAtSet && s.Gateway != GatewayAubury {
		s.MuleAt = GuildMuleAt
	}
	if !itemsSet {
		s.MuleItems = "" // the gateway's own default, which Normalised resolves
	}
	return s.Normalised(), nil
}

// toInt reads a whole number from the shapes a JSON-decoded settings map and the control panel's form produce.
func toInt(v interface{}) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || x != math.Trunc(x) || math.Abs(x) > 1<<31 {
			return 0, false
		}
		return int(x), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		return n, err == nil
	}
	return 0, false
}

// toBool reads a boolean from true/false or the strings "true"/"false" the control panel's form sends.
func toBool(v interface{}) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}
