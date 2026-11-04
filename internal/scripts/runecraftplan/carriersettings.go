package runecraftplan

// RuneCarrier's settings, loaded strictly (like EssenceMiner's, essenceplan.DecodeSettings): a key that is not a setting, a value
// of the wrong kind or one outside its range stops the start with the whole list, so a typo in carrierMinHP does not quietly send
// a 12-hitpoint account through the jungle. The bot's panel schema (internal/bot/runecarrier.go RuneCarrierSettings) has exactly
// these keys; a test keeps them identical.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The keys.
const (
	KeyCrafter     = "runecraftCrafter"
	KeyCarrierMule = "mule"
	KeySource      = "carrierSource"
	KeyKitEssence  = "carrierKitEssence"
	KeyKitCoins    = "carrierKitCoins"
	KeyMinHP       = "carrierMinHP"
	KeyShelfFloor  = "carrierShelfFloor"
	KeyKeepNatures = "carrierKeepNatures"
	KeyWalkRub     = "walkRub"
	KeyWalkAlch    = "walkAlch"
	KeySwarm       = "swarmSettings"

	keyHomeWorld = "homeWorld"
	keyPinWorld  = "pinWorld"
)

// The values of carrierSource.
const (
	// SourceShelf un-notes the mule's noted essence at Jiminua's Jungle Store: no bank anywhere on the island.
	SourceShelf = "shelf"
	// SourceBankCarrier carries unnoted essence from a bank. NOT BUILT in P3: the key names it so the settings do not change
	// when it is, and a carrier started with it refuses with the reason.
	SourceBankCarrier = "bank"
)

// Bounds.
const (
	DefaultCarrierMule = "mule"
	// MinCarrierHP and MaxCarrierHP bound carrierMinHP: the corridor's snakes bite for a few, a spider for about five; under 10
	// is a death waiting.
	MinCarrierHP     = 10
	MaxCarrierHP     = 99
	DefaultCarrierHP = 25
	// MaxKitEssence bounds a kit: the mule's bank holds 28 slots a trade and 3,000 notes are one.
	MaxKitEssence = 20000
	MaxKitCoins   = 1_000_000
	MinKitCoins   = 500
	MinFloor      = 0
	MaxFloor      = 99
	// DefaultKeepNatures is carrierKeepNatures: the Nature runes a carrier that carried a courier haul home keeps in its own bank
	// for its own alchemy instead of handing them all to the mule and asking for a kit back: one ScarabSwarm walk-alch kit
	// (scarabplan.WalkAlchSettings' 200 casts). MaxKeepNatures bounds it.
	DefaultKeepNatures = 200
	MaxKeepNatures     = 10000
)

// CarrierSettings are RuneCarrier's settings.
type CarrierSettings struct {
	// Crafter is the account standing at the Nature gate (MasterCrafter's login name): the carrier works for the gate it
	// publishes. Blank never starts a shift.
	Crafter string `json:"runecraftCrafter"`
	// Mule is the MuleReceiver the kit comes from and the leftovers go back to.
	Mule string `json:"mule"`
	// Source is "shelf" (the only one built).
	Source string `json:"carrierSource"`
	// KitEssence is the noted essence asked for a shift (whole loads of CarrierOffer, at most what the mule holds), KitCoins the
	// coins, MinHP the maximum-hitpoints floor of an eligible account, ShelfFloor the shelf the carrier keeps after each visit.
	KitEssence int `json:"carrierKitEssence"`
	KitCoins   int `json:"carrierKitCoins"`
	MinHP      int `json:"carrierMinHP"`
	ShelfFloor int `json:"carrierShelfFloor"`
	// KeepNatures is the Nature runes the carrier keeps in its own bank out of what a courier trip brought home (0 hands the mule
	// everything, as P3 did): its own stock counts, so a carrier that already holds the keep hands the mule all of the haul.
	KeepNatures int `json:"carrierKeepNatures"`
	// WalkRub rubs a Games necklace or Ring of dueling every third tick of a run; WalkAlch is refused when true (a cast burns a
	// Nature rune, about as many as a carrier delivers).
	WalkRub  bool `json:"walkRub"`
	WalkAlch bool `json:"walkAlch"`
	// SwarmSettings is what ScarabSwarm runs with between shifts (the settings object of the embedded script).
	SwarmSettings map[string]interface{} `json:"swarmSettings"`
}

// DefaultCarrierSettings are the defaults.
func DefaultCarrierSettings() CarrierSettings {
	return CarrierSettings{
		Mule: DefaultCarrierMule, Source: SourceShelf, KitEssence: KitEssenceDefault, KitCoins: KitCoinsDefault,
		MinHP: DefaultCarrierHP, ShelfFloor: DefaultShelfFloor, KeepNatures: DefaultKeepNatures, WalkRub: true, SwarmSettings: map[string]interface{}{},
	}
}

// CarrierKnownKeys lists every key the script reads, sorted.
func CarrierKnownKeys() []string {
	out := []string{KeyCrafter, KeyCarrierMule, KeySource, KeyKitEssence, KeyKitCoins, KeyMinHP, KeyShelfFloor, KeyKeepNatures, KeyWalkRub, KeyWalkAlch, KeySwarm}
	sort.Strings(out)
	return out
}

func isCarrierKey(k string) bool {
	for _, n := range CarrierKnownKeys() {
		if n == k {
			return true
		}
	}
	return k == keyHomeWorld || k == keyPinWorld
}

// DecodeCarrierSettings is the strict load. Numbers may be JSON numbers or numeric strings (the control panel's form sends
// strings), flags true/false or the strings "true"/"false", swarmSettings an object or a string holding one.
func DecodeCarrierSettings(raw map[string]interface{}) (CarrierSettings, error) {
	s := DefaultCarrierSettings()
	var problems []string
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	str := func(k string, v interface{}) (string, bool) {
		x, ok := v.(string)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %v is not text", k, v))
		}
		return strings.TrimSpace(x), ok
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
	flag := func(k string, v interface{}, dst *bool) {
		b, ok := toBool(v)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %v is not true or false", k, v))
			return
		}
		*dst = b
	}
	for _, k := range keys {
		v := raw[k]
		if !isCarrierKey(k) {
			problems = append(problems, fmt.Sprintf("%s is not a setting (known: %s)", k, strings.Join(CarrierKnownKeys(), ", ")))
			continue
		}
		switch k {
		case KeyCrafter:
			if x, ok := str(k, v); ok {
				s.Crafter = x
			}
		case KeyCarrierMule:
			if x, ok := str(k, v); ok && x != "" {
				s.Mule = x
			}
		case KeySource:
			if x, ok := str(k, v); ok {
				switch src := strings.ToLower(x); src {
				case "", SourceShelf:
					s.Source = SourceShelf
				case SourceBankCarrier:
					problems = append(problems, fmt.Sprintf("%s: %q is not built yet (P3 un-notes at the Jungle Store: %q)", k, src, SourceShelf))
				default:
					problems = append(problems, fmt.Sprintf("%s: %q is not %q or %q", k, x, SourceShelf, SourceBankCarrier))
				}
			}
		case KeyKitEssence:
			num(k, v, MinKitEssence, MaxKitEssence, &s.KitEssence)
		case KeyKitCoins:
			num(k, v, MinKitCoins, MaxKitCoins, &s.KitCoins)
		case KeyMinHP:
			num(k, v, MinCarrierHP, MaxCarrierHP, &s.MinHP)
		case KeyShelfFloor:
			num(k, v, MinFloor, MaxFloor, &s.ShelfFloor)
		case KeyKeepNatures:
			num(k, v, 0, MaxKeepNatures, &s.KeepNatures)
		case KeyWalkRub:
			flag(k, v, &s.WalkRub)
		case KeyWalkAlch:
			flag(k, v, &s.WalkAlch)
			if s.WalkAlch {
				problems = append(problems, fmt.Sprintf("%s: RuneCarrier walk-alch is on; refusing to start (a cast burns a Nature rune, about as many as a carrier delivers)", k))
			}
		case KeySwarm:
			m, err := parseSwarmSettings(v)
			if err != nil {
				problems = append(problems, err.Error())
				continue
			}
			s.SwarmSettings = m
		}
	}
	if len(problems) > 0 {
		return CarrierSettings{}, fmt.Errorf("RuneCarrier settings: %s", strings.Join(problems, "; "))
	}
	return s, nil
}

// parseSwarmSettings reads the swarmSettings value: absent, null or a blank string is an empty object; an object is copied; a
// string must hold a JSON object (the panel's form).
func parseSwarmSettings(v interface{}) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	switch x := v.(type) {
	case nil:
	case map[string]interface{}:
		for k, val := range x {
			out[k] = val
		}
	case string:
		if strings.TrimSpace(x) == "" {
			break
		}
		if err := json.Unmarshal([]byte(x), &out); err != nil {
			return nil, fmt.Errorf("swarmSettings: %q is not a JSON object (%v)", x, err)
		}
	default:
		return nil, fmt.Errorf("swarmSettings: %v is not an object of ScarabSwarm settings", v)
	}
	return out, nil
}

// SwarmSettingsMap is the settings ScarabSwarm runs with between shifts: a copy of the swarmSettings object, with this carrier's
// mule when the object names none.
func (s CarrierSettings) SwarmSettingsMap() map[string]interface{} {
	out := make(map[string]interface{}, len(s.SwarmSettings)+1)
	for k, v := range s.SwarmSettings {
		out[k] = v
	}
	if m, ok := out["mule"].(string); !ok || m == "" {
		out["mule"] = s.Mule
	}
	return out
}

// toInt reads a whole number from the shapes a JSON-decoded settings map and the control panel's form produce.
func toInt(v interface{}) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if x != float64(int(x)) {
			return 0, false
		}
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		return int(n), err == nil
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(x), "%d", &n); err != nil {
			return 0, false
		}
		if fmt.Sprint(n) != strings.TrimSpace(x) {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// toBool reads true/false or the strings "true"/"false".
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
