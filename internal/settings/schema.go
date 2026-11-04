package settings

import (
	"strconv"
	"strings"

	"rsb0t-go/internal/scripts/tile"
)

// SettingsSchema, SettingDef and SettingsBag.tile from the TS scripting API
// (reference/server-api-docs.md, "SettingsBag / SettingsSchema"; upstream
// src/bot/runtime/Settings.ts). A script declares a Schema of typed settings with defaults;
// Resolve turns whatever the account's settings hold (JSON values from fleet.config.json or a
// settings file, or strings from a panel or URL) into a Bag of correctly typed values, the way
// upstream's parseValue does: numbers clamped to Min/Max, strings and lists matched against
// Options ignoring case, tiles parsed, and the default wherever a value is missing or unusable.
//
// Display-only fields upstream also carries (step, color, optionLabels, optionsFrom,
// csvToggle) are left out; they drive its browser panel, which this client does not have.

// SettingType is a SettingDef's value type.
type SettingType string

const (
	TypeBoolean SettingType = "boolean"
	TypeNumber  SettingType = "number"
	TypeString  SettingType = "string"
	TypeList    SettingType = "string[]"
	TypeTile    SettingType = "tile"
)

// ShowIf hides a setting in a UI unless Key's value is one of AnyOf.
type ShowIf struct {
	Key   string
	AnyOf []string
}

// SettingDef declares one setting.
type SettingDef struct {
	Type SettingType
	// Default is used when the value is missing or unusable: a bool, float64, string,
	// []string or tile.Tile matching Type.
	Default interface{}
	Label   string
	Help    string
	Group   string
	// Min and Max clamp a number when set.
	Min, Max *float64
	// Options restricts a string or list to these values, matched ignoring case and stored in
	// the Options spelling.
	Options []string
	ShowIf  *ShowIf
}

// Schema maps setting keys to their definitions.
type Schema map[string]SettingDef

// Float returns a pointer to v, for SettingDef.Min and Max.
func Float(v float64) *float64 { return &v }

// Resolve builds a Bag holding every schema key resolved against raw, plus any raw keys the
// schema does not declare, unchanged.
func (sc Schema) Resolve(raw map[string]interface{}) *Bag {
	values := make(map[string]interface{}, len(raw)+len(sc))
	for k, v := range raw {
		values[k] = v
	}
	for key, def := range sc {
		v, present := raw[key]
		if !present {
			values[key] = def.Default
			continue
		}
		values[key] = def.resolve(v)
	}
	return &Bag{values: values}
}

func (def SettingDef) resolve(v interface{}) interface{} {
	switch def.Type {
	case TypeBoolean:
		switch x := v.(type) {
		case bool:
			return x
		case string:
			s := strings.ToLower(strings.TrimSpace(x))
			return s == "true" || s == "1" || s == "yes"
		}
	case TypeNumber:
		switch x := v.(type) {
		case float64:
			return def.clamp(x)
		case int:
			return def.clamp(float64(x))
		case string:
			if n, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
				return def.clamp(n)
			}
		}
	case TypeString:
		if s, ok := v.(string); ok {
			if len(def.Options) == 0 {
				return strings.TrimSpace(s)
			}
			if opt, ok := def.option(s); ok {
				return opt
			}
		}
	case TypeList:
		var items []string
		switch x := v.(type) {
		case string:
			items = strings.Split(x, ",")
		case []string:
			items = x
		case []interface{}:
			for _, e := range x {
				s, ok := e.(string)
				if !ok {
					return def.Default
				}
				items = append(items, s)
			}
		default:
			return def.Default
		}
		out := []string{}
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if len(def.Options) == 0 {
				out = append(out, item)
			} else if opt, ok := def.option(item); ok {
				out = append(out, opt)
			}
		}
		return out
	case TypeTile:
		if t, ok := toTile(v); ok {
			return t
		}
	}
	return def.Default
}

func (def SettingDef) clamp(n float64) float64 {
	if def.Min != nil && n < *def.Min {
		n = *def.Min
	}
	if def.Max != nil && n > *def.Max {
		n = *def.Max
	}
	return n
}

func (def SettingDef) option(s string) (string, bool) {
	want := strings.TrimSpace(s)
	for _, opt := range def.Options {
		if strings.EqualFold(opt, want) {
			return opt, true
		}
	}
	return "", false
}

// NewBagFrom is a Bag over a copy of values, e.g. a bot Context's Settings map.
func NewBagFrom(values map[string]interface{}) *Bag {
	b := NewBag()
	for k, v := range values {
		b.values[k] = v
	}
	return b
}

// Tile ports SettingsBag.tile: the value at key as a tile, or fallback. It accepts a tile.Tile,
// a JSON object {"x", "z", "level"}, an array [x, z, level], or upstream's string form
// "x,z[,level]"; level defaults to 0.
func (b *Bag) Tile(key string, fallback tile.Tile) tile.Tile {
	b.mu.Lock()
	v := b.values[key]
	b.mu.Unlock()
	if t, ok := toTile(v); ok {
		return t
	}
	return fallback
}

// ParseTile ports parseTile: "x,z[,level]".
func ParseTile(s string) (tile.Tile, bool) {
	parts := strings.Split(s, ",")
	if len(parts) < 2 || len(parts) > 3 {
		return tile.Tile{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return tile.Tile{}, false
		}
		nums[i] = n
	}
	return tile.Tile{X: nums[0], Z: nums[1], Level: nums[2]}, true
}

func toTile(v interface{}) (tile.Tile, bool) {
	switch x := v.(type) {
	case tile.Tile:
		return x, true
	case string:
		return ParseTile(x)
	case map[string]interface{}:
		xs, okX := x["x"].(float64)
		zs, okZ := x["z"].(float64)
		if !okX || !okZ {
			return tile.Tile{}, false
		}
		level, _ := x["level"].(float64)
		return tile.Tile{X: int(xs), Z: int(zs), Level: int(level)}, true
	case []interface{}:
		if len(x) < 2 || len(x) > 3 {
			return tile.Tile{}, false
		}
		nums := make([]int, 3)
		for i, e := range x {
			f, ok := e.(float64)
			if !ok {
				return tile.Tile{}, false
			}
			nums[i] = int(f)
		}
		return tile.Tile{X: nums[0], Z: nums[1], Level: nums[2]}, true
	}
	return tile.Tile{}, false
}
