package runecraftplan

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestHostileReach(t *testing.T) {
	for _, tc := range []struct {
		h    Hostile
		want int
	}{{JungleSpider, 4}, {Tribesman, 6}, {Scorpion, 15}} {
		if got := tc.h.Reach(); got != tc.want {
			t.Errorf("%s reaches %d, want %d", tc.h.Name, got, tc.want)
		}
	}
}

func TestSpawnBoxes(t *testing.T) {
	s := Spawn{Kind: JungleSpider, At: tile.Tile{X: 2796, Z: 3104}}
	b := s.Box()
	if b != (Box{MinX: 2792, MaxX: 2800, MinZ: 3100, MaxZ: 3108}) {
		t.Fatalf("spider box %+v", b)
	}
	if !b.Contains(tile.Tile{X: 2796, Z: 3108}) || b.Contains(tile.Tile{X: 2796, Z: 3109}) || b.Contains(tile.Tile{X: 2796, Z: 3104, Level: 1}) {
		t.Fatal("Contains is inclusive and level 0 only")
	}
	if n := len(KaramjaSpawns()); n != 57+21+12 {
		t.Fatalf("%d spawns, want 57 spiders + 21 tribesmen + 12 scorpions", n)
	}
	if n := len(AvoidBoxes()); n != len(KaramjaSpawns()) {
		t.Fatalf("%d boxes for %d spawns", n, len(KaramjaSpawns()))
	}
}

// The places the carriers must walk to are clear of every box: the pier's landing, the store stand, the gate's exit tile and the
// tiles beside the ruins, and the monsters' ground stays off the doorstep.
func TestTheWalkedPlacesAreClearOfTheBoxes(t *testing.T) {
	for name, at := range map[string]tile.Tile{
		"the landing": Landing, "the store stand": StoreStand, "Jiminua": JiminuaAt, "the exit tile": Nature.Exit,
		"the ruins' stand": {X: 2867, Z: 3021},
	} {
		if h, in := DangerAt(at); in {
			t.Errorf("%s %v is inside a %s box", name, at, h.Name)
		}
	}
	// The jungle spider's own spawn is dangerous, and DangerAt says which kind.
	if h, in := DangerAt(tile.Tile{X: 2796, Z: 3104}); !in || h.Name != "Jungle spider" {
		t.Fatalf("%+v %v", h, in)
	}
}

func TestOnKaramja(t *testing.T) {
	if !OnKaramja(Landing) || !OnKaramja(Nature.Exit) || OnKaramja(ArdougnePier) || OnKaramja(tile.Tile{X: 2809, Z: 3441}) {
		t.Fatal("the island is the box between Brimhaven and the ruins")
	}
}

// The table is the maps': every spider, tribesman and scorpion on level 0 inside the island's box in content's NPC sections,
// and the ranges are the NPC configs'. Needs the content checkout (RSB0T_CONTENT_DIR or the sibling "content"); skips without.
func TestSpawnTableMatchesTheMaps(t *testing.T) {
	dir := contentDir(t)
	ids := packIDs(t, filepath.Join(dir, "pack", "npc.pack"))
	want := map[int]string{ids["jungle_spider"]: "Jungle spider", ids["tribesman"]: "Tribesman", ids["scorpion"]: "Scorpion"}
	if len(want) != 3 {
		t.Fatalf("npc ids: %+v", ids["jungle_spider"])
	}
	got := map[string][]tile.Tile{}
	line := regexp.MustCompile(`^(\d+) (\d+) (\d+): (\d+)`)
	mapsDir := filepath.Join(dir, "maps")
	for mx := 41; mx <= 47; mx++ {
		for mz := 44; mz <= 51; mz++ {
			f, err := os.Open(filepath.Join(mapsDir, "m"+strconv.Itoa(mx)+"_"+strconv.Itoa(mz)+".jm2"))
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			inNPC := false
			for sc.Scan() {
				text := strings.TrimSpace(sc.Text())
				if strings.HasPrefix(text, "====") {
					inNPC = strings.Contains(text, "NPC")
					continue
				}
				if !inNPC {
					continue
				}
				m := line.FindStringSubmatch(text)
				if m == nil {
					continue
				}
				level, lx, lz, id := atoi(m[1]), atoi(m[2]), atoi(m[3]), atoi(m[4])
				name, ok := want[id]
				if !ok || level != 0 {
					continue
				}
				x, z := mx*64+lx, mz*64+lz
				if Karamja.Contains(tile.Tile{X: x, Z: z}) {
					got[name] = append(got[name], tile.Tile{X: x, Z: z})
				}
			}
			f.Close()
		}
	}
	have := map[string][]tile.Tile{}
	for _, s := range KaramjaSpawns() {
		have[s.Kind.Name] = append(have[s.Kind.Name], s.At)
	}
	for name, tiles := range got {
		g, h := tileStrings(tiles), tileStrings(have[name])
		if strings.Join(g, " ") != strings.Join(h, " ") {
			t.Errorf("%s: the maps hold %d spawns, the table %d\n maps:  %v\n table: %v", name, len(g), len(h), g, h)
		}
	}
	// The ranges: wanderrange and huntrange from the NPC config.
	cfg, err := os.ReadFile(filepath.Join(dir, "scripts", "_unpack", "225", "all.npc"))
	if err != nil {
		t.Skipf("no NPC config: %v", err)
	}
	for _, h := range []struct {
		section string
		kind    Hostile
		wander  int // the config's wanderrange, 5 when it has none
	}{{"jungle_spider", JungleSpider, 3}, {"tribesman", Tribesman, 5}, {"scorpion", Scorpion, 12}} {
		body := npcSection(string(cfg), h.section)
		if body == "" {
			t.Errorf("no [%s] in all.npc", h.section)
			continue
		}
		wander, hunt, level, size := 5, 0, 0, 1
		for _, l := range strings.Split(body, "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
				switch k {
				case "wanderrange":
					wander = atoi(v)
				case "huntrange":
					hunt = atoi(v)
				case "vislevel":
					level = atoi(v)
				case "size":
					size = atoi(v)
				}
			}
		}
		k := h.kind
		if k.Wander != wander || k.Hunt != hunt || k.Level != level || k.Size != size {
			t.Errorf("%s: the table has wander %d hunt %d level %d size %d, the config %d %d %d %d", k.Name, k.Wander, k.Hunt, k.Level, k.Size, wander, hunt, level, size)
		}
	}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func tileStrings(ts []tile.Tile) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, strconv.Itoa(t.X)+","+strconv.Itoa(t.Z))
	}
	sort.Strings(out)
	return out
}

// npcSection is the text of [name] up to the next section in an unpacked config.
func npcSection(cfg, name string) string {
	i := strings.Index(cfg, "\n["+name+"]")
	if i < 0 {
		return ""
	}
	rest := cfg[i+1:]
	if j := strings.Index(rest[1:], "\n["); j >= 0 {
		rest = rest[:j+1]
	}
	return rest
}
