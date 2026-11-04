package fleetplan

// CookFish is one fish content lets a player cook on a range: the raw and cooked names, the
// Cooking level it needs, and its success-chance pair (low at level 1, high at level 99) for a
// fire and, where content gives one, for a range.
//
// Every number is content 289's own, from scripts/skill_cooking/configs/cooking_source/
// cooking_generic.dbrow (levelrequired, successchance, successchance_range), and every name from
// skill_cooking/configs/cooking_generic/food_uncooked.obj and food_cooked.obj (checked 3 Oct 2026
// against C:/rsb0t/content). Heals are fighterfood's, which are
// content's consume_normal.dbrow heal_N rows (shrimps 3, anchovies 3, sardine 4, herring 5,
// mackerel 6, trout 7, cod 7, pike 8, salmon 9, tuna 10, lobster 12, bass 13, swordfish 14,
// shark 20). Mackerel, cod, bass and shark are members' items; the fleet's worlds are members.
//
// XP10 is cooking_generic.dbrow's experience column, in tenths of a Cooking xp (shrimps 300 = 30 xp),
// re-read 13 Oct 2026. The engine awards it by stat_advance only when the success roll passes
// (cooking.rs2), so the expected xp of one raw fish on a range is CookSuccess256 x XP10 / 2560.
type CookFish struct {
	Raw, Cooked string
	Level       int
	XP10        int
	// Low and High are successchance; RangeLow and RangeHigh successchance_range (0: none, the
	// fire pair applies on a range too -- cooking.rs2 swaps only when the range pair is set).
	Low, High           int
	RangeLow, RangeHigh int
}

// CookFishes is every fish row of cooking_generic.dbrow, in level order.
var CookFishes = []CookFish{
	{Raw: "Raw shrimps", Cooked: "Shrimps", Level: 1, XP10: 300, Low: 128, High: 512},
	{Raw: "Raw anchovies", Cooked: "Anchovies", Level: 1, XP10: 300, Low: 128, High: 512},
	{Raw: "Raw sardine", Cooked: "Sardine", Level: 1, XP10: 400, Low: 118, High: 492},
	{Raw: "Raw herring", Cooked: "Herring", Level: 5, XP10: 500, Low: 108, High: 472},
	{Raw: "Raw mackerel", Cooked: "Mackerel", Level: 10, XP10: 600, Low: 98, High: 452},
	{Raw: "Raw trout", Cooked: "Trout", Level: 15, XP10: 700, Low: 88, High: 432},
	{Raw: "Raw cod", Cooked: "Cod", Level: 18, XP10: 750, Low: 83, High: 422, RangeLow: 88, RangeHigh: 432},
	{Raw: "Raw pike", Cooked: "Pike", Level: 20, XP10: 800, Low: 78, High: 412},
	{Raw: "Raw salmon", Cooked: "Salmon", Level: 25, XP10: 900, Low: 68, High: 392},
	{Raw: "Raw tuna", Cooked: "Tuna", Level: 30, XP10: 1000, Low: 58, High: 372},
	{Raw: "Raw lobster", Cooked: "Lobster", Level: 40, XP10: 1200, Low: 38, High: 332},
	{Raw: "Raw bass", Cooked: "Bass", Level: 43, XP10: 1300, Low: 33, High: 312},
	{Raw: "Raw swordfish", Cooked: "Swordfish", Level: 45, XP10: 1400, Low: 18, High: 292, RangeLow: 30, RangeHigh: 310},
	{Raw: "Raw shark", Cooked: "Shark", Level: 80, XP10: 2100, Low: 1, High: 202, RangeLow: 1, RangeHigh: 232},
	{Raw: "Raw sea turtle", Cooked: "Sea turtle", Level: 82, XP10: 2113, Low: 1, High: 202, RangeLow: 1, RangeHigh: 222},
	{Raw: "Raw manta ray", Cooked: "Manta ray", Level: 91, XP10: 2163, Low: 1, High: 202, RangeLow: 1, RangeHigh: 222},
}
