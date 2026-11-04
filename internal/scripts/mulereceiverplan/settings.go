package mulereceiverplan

// The mule's settings shape: what MuleReceiver's overlay settings page reads and writes, kept
// between runs under the script's id. Ported from rsb0t-scripts' muleSettings.ts.
//
// Out of scope, and why: the idle-alching fields (Alch, AlchFireStaff, AlchSpell, AlchItems,
// AlchCasts) are carried here as plain settings data only, because MuleConfig's shape includes
// them — no alching decision logic comes with them; that logic lives in alchPlan.ts/alchCaster.ts,
// a separate, lower-priority chunk left unported (see plan.go's package doc). AlchSpell's default
// is alchPlan.ts's AUTO_SPELL constant, carried over as the literal string it is — a label, not a
// spell lookup.
//
// muleSettings.ts's MuleFlagKey/MuleChoiceKey type unions and the BANK_LOCATIONS-derived
// BANK_OPTIONS list exist only to wire the overlay's generic settings-page widgets (a checkbox
// toggler, a bank-name dropdown sourced from live content) — UI plumbing with no decision inside
// it, not ported. NumberKey and ListKey are kept because Nudged and WithNames are real decisions.

import (
	"fmt"
	"regexp"
	"strings"
)

// MuleStoreID is the settings' storage key (muleSettings.ts's MULE_STORE_ID).
const MuleStoreID = "muleReceiver"

// AutoSpellLabel is alchPlan.ts's AUTO_SPELL literal, carried as AlchSpell's default label. No
// spell resolution comes with it — see the package doc.
const AutoSpellLabel = "Best for level"

// MuleConfig is every setting the overlay's settings page keeps. Field order matches
// muleSettings.ts's MuleConfig/MULE_DEFAULTS, which ConfigFromForm's carried-over order also
// follows.
type MuleConfig struct {
	// Partners: whose trades are taken, whose PMs are obeyed, and who is collected from.
	Partners      []string
	PmOrders      bool
	DeliverOrders bool
	TrustBus      bool
	// AdoptWall: write any account heard on the wall bus into the partner list, the first time it
	// is heard.
	AdoptWall bool
	// Post is a bank by name; PostTile is a typed tile that overrides it when set.
	Post        string
	PostTile    string
	Pickups     bool
	WalkRadius  int
	MaxLegTiles int
	KitLaw      int
	KitAir      int
	KitFire     int
	KitWater    int
	KitEarth    int
	KitAirStaff bool
	Alch        bool
	// AlchFireStaff: wield a fire staff to alch, and swap the air staff back on for every teleport.
	AlchFireStaff bool
	AlchSpell     string
	AlchItems     []string
	AlchCasts     int
}

// MuleDefaults are the settings' defaults.
func MuleDefaults() MuleConfig {
	return MuleConfig{
		Partners:      nil,
		PmOrders:      true,
		DeliverOrders: true,
		TrustBus:      false,
		AdoptWall:     false,
		Post:          "Seers",
		PostTile:      "",
		Pickups:       true,
		WalkRadius:    50,
		MaxLegTiles:   150,
		KitLaw:        10,
		KitAir:        40,
		KitFire:       5,
		KitWater:      5,
		KitEarth:      5,
		KitAirStaff:   false,
		Alch:          false,
		AlchFireStaff: true,
		AlchSpell:     AutoSpellLabel,
		AlchItems:     nil,
		AlchCasts:     50,
	}
}

// RuneCounts is cfg's kit as KitFromSettings wants it.
func (c MuleConfig) RuneCounts() RuneCounts {
	return RuneCounts{Law: c.KitLaw, Air: c.KitAir, Fire: c.KitFire, Water: c.KitWater, Earth: c.KitEarth}
}

// NumberKey names one of the number settings Nudged bounds and steps.
type NumberKey string

// The number settings.
const (
	NumWalkRadius  NumberKey = "walkRadius"
	NumMaxLegTiles NumberKey = "maxLegTiles"
	NumKitLaw      NumberKey = "kitLaw"
	NumKitAir      NumberKey = "kitAir"
	NumKitFire     NumberKey = "kitFire"
	NumKitWater    NumberKey = "kitWater"
	NumKitEarth    NumberKey = "kitEarth"
	NumAlchCasts   NumberKey = "alchCasts"
)

// ListKey names one of the settings that hold a list of names WithNames edits.
type ListKey string

// The list settings.
const (
	ListPartners  ListKey = "partners"
	ListAlchItems ListKey = "alchItems"
)

type bound struct {
	min, max int
	// step is the size of one click from value in direction dir, so going back down retraces
	// going up.
	step func(value, dir int) int
}

// runeStep is ones to twenty, fives to a hundred, twenty-fives past it: a kit of 10 laws or 300
// airs is a few clicks either way.
func runeStep(value, dir int) int {
	at := value
	if dir <= 0 {
		at = value - 1
	}
	switch {
	case at < 20:
		return 1
	case at < 100:
		return 5
	default:
		return 25
	}
}

func flatStep(n int) func(int, int) int {
	return func(int, int) int { return n }
}

func alchCastsStep(value, dir int) int {
	at := value
	if dir <= 0 {
		at = value - 1
	}
	if at < 10 {
		return 1
	}
	return 10
}

var muleBounds = map[NumberKey]bound{
	NumWalkRadius:  {min: 5, max: 300, step: flatStep(5)},
	NumMaxLegTiles: {min: 10, max: 600, step: flatStep(10)},
	NumKitLaw:      {min: 0, max: 1000, step: runeStep},
	NumKitAir:      {min: 0, max: 1000, step: runeStep},
	NumKitFire:     {min: 0, max: 1000, step: runeStep},
	NumKitWater:    {min: 0, max: 1000, step: runeStep},
	NumKitEarth:    {min: 0, max: 1000, step: runeStep},
	NumAlchCasts:   {min: 1, max: 1000, step: alchCastsStep},
}

// Nudged is one click of a number setting, kept inside its bounds.
func Nudged(key NumberKey, value, dir int) int {
	b := muleBounds[key]
	out := value + dir*b.step(value, dir)
	if out < b.min {
		return b.min
	}
	if out > b.max {
		return b.max
	}
	return out
}

func clampToBound(key NumberKey, v int) int {
	b := muleBounds[key]
	if v < b.min {
		return b.min
	}
	if v > b.max {
		return b.max
	}
	return v
}

// nameMax is the longest name each list takes: a character name is twelve at most, an item name
// is longer.
var nameMax = map[ListKey]int{ListPartners: 12, ListAlchItems: 40}

var (
	nameOK        = regexp.MustCompile(`^[a-zA-Z0-9 '_().-]+$`)
	collapseSpace = regexp.MustCompile(`\s+`)
)

// WithNames is list with every comma-separated name in raw added, trimmed, and once each whatever
// the case. A name too long or full of punctuation is left out rather than kept to fail later.
func WithNames(list []string, raw string, key ListKey) []string {
	out := append([]string(nil), list...)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(collapseSpace.ReplaceAllString(part, " "))
		if name == "" || len(name) > nameMax[key] || !nameOK.MatchString(name) {
			continue
		}
		dup := false
		for _, n := range out {
			if strings.EqualFold(n, name) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, name)
		}
	}
	return out
}

// WithoutName is list with name removed, matched case-insensitively.
func WithoutName(list []string, name string) []string {
	var out []string
	for _, n := range list {
		if !strings.EqualFold(n, strings.TrimSpace(name)) {
			out = append(out, n)
		}
	}
	return out
}

// Adopted is the partner list with an account heard on the wall added, ok=false when nothing
// changed: a name already on it, the mule's own, or one WithNames would not take.
func Adopted(list []string, name, me string) ([]string, bool) {
	heard := strings.TrimSpace(name)
	if heard == "" || strings.EqualFold(heard, strings.TrimSpace(me)) {
		return nil, false
	}
	next := WithNames(list, heard, ListPartners)
	if len(next) > len(list) {
		return next, true
	}
	return nil, false
}

// Normalised is cfg with numbers inside their bounds, lists deduplicated: whatever was stored, a
// sane config.
func Normalised(cfg MuleConfig) MuleConfig {
	out := cfg
	out.WalkRadius = clampToBound(NumWalkRadius, out.WalkRadius)
	out.MaxLegTiles = clampToBound(NumMaxLegTiles, out.MaxLegTiles)
	out.KitLaw = clampToBound(NumKitLaw, out.KitLaw)
	out.KitAir = clampToBound(NumKitAir, out.KitAir)
	out.KitFire = clampToBound(NumKitFire, out.KitFire)
	out.KitWater = clampToBound(NumKitWater, out.KitWater)
	out.KitEarth = clampToBound(NumKitEarth, out.KitEarth)
	out.AlchCasts = clampToBound(NumAlchCasts, out.AlchCasts)
	out.Partners = WithNames(nil, strings.Join(out.Partners, ","), ListPartners)
	out.AlchItems = WithNames(nil, strings.Join(out.AlchItems, ","), ListAlchItems)
	out.Post = strings.TrimSpace(out.Post)
	if out.Post == "" {
		out.Post = MuleDefaults().Post
	}
	out.PostTile = strings.TrimSpace(out.PostTile)
	return out
}

// FormCarryOver is what ConfigFromForm found: the settings the old start-up form left behind, and
// which of them differed from the defaults.
type FormCarryOver struct {
	Config  MuleConfig
	Carried []string
}

func readBool(raw map[string]interface{}, key string, fallback bool, carried *[]string) bool {
	if v, ok := raw[key]; ok {
		if b, ok := v.(bool); ok {
			if b != fallback {
				*carried = append(*carried, key)
			}
			return b
		}
	}
	return fallback
}

func readString(raw map[string]interface{}, key string, fallback string, carried *[]string) string {
	if v, ok := raw[key]; ok {
		if s, ok := v.(string); ok {
			if s != fallback {
				*carried = append(*carried, key)
			}
			return s
		}
	}
	return fallback
}

// asNumber accepts the numeric shapes a decoded JSON blob or a plain Go literal could carry.
func asNumber(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

func readNumber(raw map[string]interface{}, key string, fallback int, carried *[]string) int {
	if v, ok := raw[key]; ok {
		if n, ok := asNumber(v); ok {
			if n != fallback {
				*carried = append(*carried, key)
			}
			return n
		}
	}
	return fallback
}

func readList(raw map[string]interface{}, key string, listKey ListKey, carried *[]string) []string {
	if v, ok := raw[key]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			*carried = append(*carried, key)
			return WithNames(nil, s, listKey)
		}
	}
	return nil
}

// ConfigFromForm reads the settings the old start-up form left behind — this script's own
// fleet.config.json settings object, arrived as raw untyped values the way the old form's schema
// always was — into a MuleConfig, falling back to defaults for anything missing or of the wrong
// type. The two list settings were comma-separated text there.
func ConfigFromForm(raw map[string]interface{}) FormCarryOver {
	def := MuleDefaults()
	var carried []string
	cfg := MuleConfig{
		Partners:      readList(raw, "partners", ListPartners, &carried),
		PmOrders:      readBool(raw, "pmOrders", def.PmOrders, &carried),
		DeliverOrders: readBool(raw, "deliverOrders", def.DeliverOrders, &carried),
		TrustBus:      readBool(raw, "trustBus", def.TrustBus, &carried),
		AdoptWall:     readBool(raw, "adoptWall", def.AdoptWall, &carried),
		Post:          readString(raw, "post", def.Post, &carried),
		PostTile:      readString(raw, "postTile", def.PostTile, &carried),
		Pickups:       readBool(raw, "pickups", def.Pickups, &carried),
		WalkRadius:    readNumber(raw, "walkRadius", def.WalkRadius, &carried),
		MaxLegTiles:   readNumber(raw, "maxLegTiles", def.MaxLegTiles, &carried),
		KitLaw:        readNumber(raw, "kitLaw", def.KitLaw, &carried),
		KitAir:        readNumber(raw, "kitAir", def.KitAir, &carried),
		KitFire:       readNumber(raw, "kitFire", def.KitFire, &carried),
		KitWater:      readNumber(raw, "kitWater", def.KitWater, &carried),
		KitEarth:      readNumber(raw, "kitEarth", def.KitEarth, &carried),
		KitAirStaff:   readBool(raw, "kitAirStaff", def.KitAirStaff, &carried),
		Alch:          readBool(raw, "alch", def.Alch, &carried),
		AlchFireStaff: readBool(raw, "alchFireStaff", def.AlchFireStaff, &carried),
		AlchSpell:     readString(raw, "alchSpell", def.AlchSpell, &carried),
		AlchItems:     readList(raw, "alchItems", ListAlchItems, &carried),
		AlchCasts:     readNumber(raw, "alchCasts", def.AlchCasts, &carried),
	}
	return FormCarryOver{Config: Normalised(cfg), Carried: carried}
}

// DescribeSetting is a setting's value as the log says it.
func DescribeSetting(value interface{}) string {
	switch v := value.(type) {
	case []string:
		if len(v) == 0 {
			return "none"
		}
		return strings.Join(v, ", ")
	case bool:
		if v {
			return "on"
		}
		return "off"
	case string:
		if v == "" {
			return "blank"
		}
		return v
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
