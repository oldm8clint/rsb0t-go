package fisherplan

import (
	"strings"

	"rsb0t-go/internal/scripts/freshstart"
)

// WhirlpoolIDs are the NPC types a fishing spot turns into while it is a whirlpool, from the TS
// api's WHIRLPOOL_IDS (src/bot/data/fishingMethods.ts). ProgressiveFisher.ts never clicks one and
// ends a session when its spot becomes one.
var WhirlpoolIDs = map[int]bool{403: true, 404: true, 405: true, 406: true}

// IsWhirlpool reports whether an NPC type is a whirlpool.
func IsWhirlpool(npcType int) bool { return WhirlpoolIDs[npcType] }

// SellCountFor is how many of one fish to offer a shop for need coins: the count FishToSell plans
// for that fish alone, as ProgressiveFisher.ts's sellStep asks it (a room of 28), and at least 1.
func SellCountFor(need int, name string, held int) int {
	n := 0
	for _, line := range FishToSell(need, []freshstart.PackItem{{Name: name, Count: held}}, 28) {
		n += line.Count
	}
	if n < 1 {
		n = 1
	}
	return n
}

// SellOrder is the raw fish names to offer a shop, the most valuable first and each once, leaving
// out the names that shop has refused this run (keys lower-case). sellStep's own sort is by value
// alone; ties keep their pack order.
func SellOrder(pack []freshstart.PackItem, refused map[string]bool) []string {
	seen := map[string]bool{}
	var names []string
	for _, item := range pack {
		name := strings.TrimSpace(item.Name)
		key := strings.ToLower(name)
		if !IsRawFish(name) || seen[key] || refused[key] {
			continue
		}
		seen[key] = true
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && FishValue(names[j]) > FishValue(names[j-1]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}
