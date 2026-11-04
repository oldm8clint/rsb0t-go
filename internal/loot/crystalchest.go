package loot

// The Taverley crystal chest, as a loot source (owner, 2026-09-20: "make sure it shows loot
// for the crystal chest in taverly that is opened by using a crystal key").
//
// It is an openable like a Casket or an Oyster — the panel already draws that kind, merges
// sources sharing a name and action, and values the drops at real market prices — so nothing
// here is new machinery. What it needs is a Source with a stable identity and a model the
// panel can actually draw, and that is the one thing worth writing down:
//
//   - The chest itself is a LOC (loc 172 "Closed chest" at 2914,3452,0 in content 289, which
//     loc_changes to 173 "Open chest" for two ticks). The Loot panel can only draw an npc
//     model or an obj model (web/panels/loot.js's own modelKey: `npc:<id>` or `obj:<id>`),
//     and there is no obj for the chest. So the card's model is the CRYSTAL KEY, the item the
//     open consumes, and the card's name says what was opened. Better an honest key than a
//     blank monogram.
//   - The Source.ID is therefore the key's obj id, not a loc id. Nothing reads Source.ID as a
//     loc id, and Key() already namespaces it by action and name, so a Casket (obj 405) and
//     this can never collide.
//
// The drops themselves are never predicted here. Like every other source, they are measured
// as the pack's own change across the open (loot.Gained), so what the panel shows is what
// really arrived — see this package's own doc comment.

// CrystalKeyID is content 289's unnoted crystal_key (content/pack/obj.pack: `989=crystal_key`;
// 990 is its note). It is the model the Crystal chest card draws and the item an open consumes.
const CrystalKeyID = 989

// CrystalKeyName is the cache's display name for both 989 and its note.
const CrystalKeyName = "Crystal key"

// CrystalChestName is the Loot panel's card name for the source.
const CrystalChestName = "Crystal chest"

// CrystalChest is the loot Source for one opening of the Taverley crystal chest. Its Events
// count is chests opened, which is also keys consumed: content's [label,open_crystal_chest]
// does inv_del(inv, crystal_key, 1) on every single open, before it adds anything, so a key
// is spent whatever the chest gives back.
func CrystalChest() Source { return Openable(CrystalChestName, CrystalKeyID) }
