package quests

// questRow is one row of the quest tab.
type questRow struct {
	Label   string // the row's text, the quest's display name
	ComID   int    // its text component in the questlist interface
	Members bool   // listed under MEMBERS QUESTS
}

// questRows is every quest row of content 289's player/interfaces/questlist.if, in tab order,
// with the component ids content/pack/interface.pack assigns to `questlist:<component>`. It was
// generated from those two files, not typed by hand. Each row is also the IF_BUTTON that opens
// the quest's journal (`[if_button,questlist:cook]` in quest_cook/scripts/cook_journal.rs2).
var questRows = []questRow{
	{"Black Knight's Fortress", 7332, false},  // questlist:fortress
	{"Cook's Assistant", 7333, false},         // questlist:cook
	{"Demon Slayer", 7334, false},             // questlist:demon
	{"Rune Mysteries Quest", 7335, false},     // questlist:runemysteries
	{"Doric's Quest", 7336, false},            // questlist:doric
	{"The Restless Ghost", 7337, false},       // questlist:priest
	{"Goblin Diplomacy", 7338, false},         // questlist:gobdip
	{"Ernest the Chicken", 7339, false},       // questlist:haunted
	{"Imp Catcher", 7340, false},              // questlist:imp
	{"Pirate's Treasure", 7341, false},        // questlist:hunt
	{"Prince Ali Rescue", 7342, false},        // questlist:prince
	{"Romeo & Juliet", 7343, false},           // questlist:romeojuliet
	{"Sheep Shearer", 7344, false},            // questlist:sheep
	{"Shield of Arrav", 7345, false},          // questlist:blackarmgang
	{"The Knight's Sword", 7346, false},       // questlist:squire
	{"Vampire Slayer", 7347, false},           // questlist:vampire
	{"Witch's Potion", 7348, false},           // questlist:hetty
	{"Watch Tower", 7349, false},              // questlist:itwatchtower
	{"Waterfall Quest", 7350, true},           // questlist:waterfall
	{"Witch's House", 7351, true},             // questlist:ball
	{"Biohazard", 7352, true},                 // questlist:biohazard
	{"Clock Tower", 7353, true},               // questlist:cog
	{"Digsite Quest", 7354, true},             // questlist:itexam
	{"Big Chompy Bird Hunting", 673, true},    // questlist:chompybird
	{"Dwarf Cannon", 7356, true},              // questlist:mcannon
	{"Family Crest", 7357, true},              // questlist:crest
	{"Fight Arena", 7358, true},               // questlist:arena
	{"Fishing Contest", 7359, true},           // questlist:fishingcompo
	{"Gertrude's Cat", 7360, true},            // questlist:fluffs
	{"The Grand Tree", 7361, true},            // questlist:grandtree
	{"Hazeel Cult", 7362, true},               // questlist:hazeelcult
	{"Hero's Quest", 7363, true},              // questlist:hero
	{"Jungle Potion", 7365, true},             // questlist:junglepotion
	{"Legends Quest", 7366, true},             // questlist:legends
	{"Lost City", 7367, true},                 // questlist:zanaris
	{"Merlin's Crystal", 7368, true},          // questlist:arthur
	{"Monk's Friend", 7369, true},             // questlist:drunkmonk
	{"Murder Mystery", 7370, true},            // questlist:murder
	{"Observatory Quest", 7371, true},         // questlist:itgronigen
	{"Plague City", 7372, true},               // questlist:elena
	{"Scorpion Catcher", 7373, true},          // questlist:scorpcatcher
	{"Sea Slug Quest", 7374, true},            // questlist:seaslug
	{"Sheep Herder", 7375, true},              // questlist:sheepherder
	{"Shilo Village", 7376, true},             // questlist:zombiequeen
	{"Temple of Ikov", 7378, true},            // questlist:ikov
	{"The Tourist Trap", 7379, true},          // questlist:desertrescue
	{"Tree Gnome Village", 7380, true},        // questlist:tree
	{"Tribal Totem", 7381, true},              // questlist:totem
	{"Underground Pass", 9927, true},          // questlist:upass
	{"Dragon Slayer", 7383, true},             // questlist:dragon
	{"Elemental Workshop", 7459, true},        // questlist:elemental_workshop
	{"Druidic Ritual", 7355, true},            // questlist:druid
	{"Priest in Peril", 8115, true},           // questlist:priestperil
	{"Nature Spirit", 8137, true},             // questlist:druidspirit
	{"Death Plateau", 8438, true},             // questlist:death
	{"Troll Stronghold", 191, true},           // questlist:troll
	{"Eadgar's Ruse", 8679, true},             // questlist:eadgar
	{"Tai Bwo Wannai Trio", 1740, true},       // questlist:tbwt
	{"Regicide", 8576, true},                  // questlist:regicide
	{"Shades of Mortton", 8969, true},         // questlist:mortton
	{"Throne of Miscellania", 6518, true},     // questlist:misc
	{"The Fremennik Trials", 10115, true},     // questlist:viking
	{"Holy Grail", 7364, true},                // questlist:grail
	{"Horror from the Deep", 10135, true},     // questlist:horror
	{"Haunted Mine", 11857, true},             // questlist:com_90
	{"Monkey Madness", 11132, true},           // questlist:mm
	{"Troll Romance", 11858, true},            // questlist:troll_love
	{"In Search of the Myreque", 11907, true}, // questlist:routequest
}

// buildComponentIDs keys questRows by lower-cased label.
func buildComponentIDs() map[string]int {
	out := make(map[string]int, len(questRows))
	for _, row := range questRows {
		out[normalise(row.Label)] = row.ComID
	}
	return out
}

// rowFor returns the questRows entry for a lower-cased quest name.
func rowFor(name string) (questRow, bool) {
	for _, row := range questRows {
		if normalise(row.Label) == name {
			return row, true
		}
	}
	return questRow{}, false
}
