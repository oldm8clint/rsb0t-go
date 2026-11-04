package trawlerplan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Settings are the FishingTrawler's settings. Loading is strict (DecodeSettings): an unknown key is an error.
type Settings struct {
	// Anchor is the account that stays aboard to the end and claims the catch; the crew logs out before the end
	// only while this account is seen aboard.
	Anchor string `json:"anchor"`
	// World pins the world the crew plays on (the anchor's world); 0 plays on whichever world the account is on.
	World int `json:"world"`
	// KitPaste, KitRope are the Swamp paste and Rope bought at the Port Khazard store before each trip. KitRope is the Rope of an
	// account that cannot mend (Crafting under NetCrafting; default 0: it would only bail, Aboard); KitMendRope the Rope of one that
	// can (KitRopeFor), bought at the store and drawn from its own bank or asked of the mule at a bank visit.
	KitPaste    int `json:"kitPaste"`
	KitRope     int `json:"kitRope"`
	KitMendRope int `json:"kitMendRope"`
	// Bucket buys (and uses) Bailing buckets, KitBuckets of them (one is all a bailer can use a tick; the anchor carries
	// spares for the crew's sake of a full kit).
	Bucket     bool `json:"bucket"`
	KitBuckets int  `json:"kitBuckets"`
	// LeaveAtSecondsLeft is the estimated time left at which the crew logs out.
	LeaveAtSecondsLeft int `json:"leaveAtSecondsLeft"`
	// MaxFloodToLeave is the highest known flood level (0-100) the crew still leaves at; a flooded boat whose
	// level is unknown is treated as above it.
	MaxFloodToLeave int `json:"maxFloodToLeave"`
	// Alch casts High Level Alchemy whenever the account waits (the lobby, the walks, idle aboard) with a fire staff wielded
	// and the natures and alchables drawn from its own bank (fleetrunner.Travel).
	Alch bool `json:"alch"`
	// Mule is the mule asked for a fire staff and Nature runes when the account's own bank has none; "" asks nobody.
	Mule string `json:"mule"`
	// LogDetail is "Normal" or "Verbose".
	LogDetail string `json:"logDetail"`
	// PinWorld is read by the fleet, not by this script; accepted so a pinned roster entry still loads.
	PinWorld json.Number `json:"pinWorld,omitempty"`
}

var knownKeys = map[string]bool{
	"anchor": true, "world": true, "kitPaste": true, "kitRope": true, "bucket": true,
	"leaveAtSecondsLeft": true, "maxFloodToLeave": true, "logDetail": true, "pinWorld": true,
	"alch": true, "mule": true, "kitBuckets": true, "kitMendRope": true,
}

// DefaultSettings are the defaults.
func DefaultSettings() Settings {
	return Settings{
		Anchor: DefaultAnchor, World: 0, KitPaste: DefaultKitPaste, KitRope: DefaultKitRope, KitMendRope: DefaultKitMendRope, Bucket: true, KitBuckets: 1,
		LeaveAtSecondsLeft: DefaultLeaveAtSecondsLeft, MaxFloodToLeave: DefaultMaxFloodToLeave, LogDetail: "Normal",
		Alch: true,
	}
}

// Verbose reports whether every step is logged.
func (s Settings) Verbose() bool { return strings.EqualFold(s.LogDetail, "Verbose") }

// DecodeSettings strictly decodes raw over the defaults. Numbers may be JSON numbers or numeric strings and
// "bucket" a JSON bool or "true"/"false" (the control panel's form sends strings).
func DecodeSettings(raw map[string]interface{}) (Settings, error) {
	s := DefaultSettings()
	fixed := make(map[string]interface{}, len(raw))
	for k, v := range raw {
		if !knownKeys[k] {
			return Settings{}, fmt.Errorf("FishingTrawler settings: unknown key %q", k)
		}
		switch k {
		case "world", "kitPaste", "kitRope", "kitMendRope", "leaveAtSecondsLeft", "maxFloodToLeave":
			if str, ok := v.(string); ok {
				n, err := strconv.Atoi(strings.TrimSpace(str))
				if err != nil {
					return Settings{}, fmt.Errorf("FishingTrawler settings: %s: %q is not a whole number", k, str)
				}
				v = n
			}
		case "bucket", "alch":
			if str, ok := v.(string); ok {
				b, err := strconv.ParseBool(strings.TrimSpace(str))
				if err != nil {
					return Settings{}, fmt.Errorf("FishingTrawler settings: %s: %q is not true or false", k, str)
				}
				v = b
			}
		case "pinWorld":
			v = fmt.Sprint(v)
		}
		fixed[k] = v
	}
	data, err := json.Marshal(fixed)
	if err != nil {
		return Settings{}, fmt.Errorf("FishingTrawler settings: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Settings{}, fmt.Errorf("FishingTrawler settings: %w", err)
	}
	return s, s.Validate()
}

// Validate reports every problem with s.
func (s Settings) Validate() error {
	var p []string
	if strings.TrimSpace(s.Anchor) == "" {
		p = append(p, "anchor is blank")
	}
	if s.World < 0 || s.World > 100 {
		p = append(p, fmt.Sprintf("world %d is outside 0..100", s.World))
	}
	if s.KitPaste < 0 || s.KitPaste > 500 {
		// Swamp paste stacks (one slot); 500 is the store's whole stock.
		p = append(p, fmt.Sprintf("kitPaste %d is outside 0..500", s.KitPaste))
	}
	if s.KitRope < 0 || s.KitRope > 26 || s.KitBuckets < 0 || s.KitBuckets > 26 {
		p = append(p, fmt.Sprintf("kitRope %d or kitBuckets %d is outside 0..26", s.KitRope, s.KitBuckets))
	}
	if s.KitRope+s.KitBuckets > 26 {
		p = append(p, "kitRope + kitBuckets leaves no room for the paste and the alchemy kit")
	}
	if s.KitMendRope < 0 || s.KitMendRope > 20 {
		// Single Ropes take a slot each: past 20 there is no room for the alchemy kit and the catch.
		p = append(p, fmt.Sprintf("kitMendRope %d is outside 0..20", s.KitMendRope))
	}
	if s.KitMendRope+s.KitBuckets > 26 {
		p = append(p, "kitMendRope + kitBuckets leaves no room for the paste and the alchemy kit")
	}
	if s.LeaveAtSecondsLeft < 5 || s.LeaveAtSecondsLeft > 59 {
		// The overlay shows whole minutes and "Under 1 min" for the last 100 ticks, so only the last minute can be timed.
		p = append(p, fmt.Sprintf("leaveAtSecondsLeft %d is outside 5..59", s.LeaveAtSecondsLeft))
	}
	if s.MaxFloodToLeave < 0 || s.MaxFloodToLeave > 100 {
		p = append(p, fmt.Sprintf("maxFloodToLeave %d is outside 0..100", s.MaxFloodToLeave))
	}
	if !strings.EqualFold(s.LogDetail, "Normal") && !strings.EqualFold(s.LogDetail, "Verbose") {
		p = append(p, fmt.Sprintf("logDetail %q is not Normal or Verbose", s.LogDetail))
	}
	if len(p) > 0 {
		return fmt.Errorf("FishingTrawler settings: %s", strings.Join(p, "; "))
	}
	return nil
}
