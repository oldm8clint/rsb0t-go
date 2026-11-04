package questplan

// ownerFillerTail is the owner ladder from Cook's Assistant to the finale: the quest-point fillers in the order they run. Every
// filler lane (W13 of docs/quest_plan_2026-10-15.md) registered its quests after Romeo & Juliet and ahead of Dragon Slayer, so the
// merged tail is those lanes' quests in the order they were merged; Black Knights' Fortress wants 12 quest points before Sir Amik
// will speak to the account, so it is last of the fillers, once the others have paid theirs. A new filler is added here and in
// OwnerLadder, and this is the one place the questplan tests spell the order (the bot tests have their own, unprovenFillerTail).
var ownerFillerTail = []string{"cook", "sheep", "gobdip", "romeo", "haunted", "hunt", "prince", "doric", "fluffs", "drunkmonk", "cog", "demon", "druid", "junglepotion", "tbwt", "elena", "biohazard", "totem", "fishingcompo", "itexam", "itgronigen", "seaslug", "sheepherder", "mcannon", "hazeelcult", "elemental_workshop", "blackknight", "dragon"}
