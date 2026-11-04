package essenceplan

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGatewayTable(t *testing.T) {
	guild, ok := GatewayByKey("Guild")
	if !ok || guild.NPC != "Wizard Distentor" || guild.Op != "Teleport" || guild.MinMagic != 66 || guild.BankName != "Yanille" {
		t.Fatalf("guild: %+v", guild)
	}
	if guild.Stand.X != 2594 || guild.Stand.Z != 3089 || guild.Bank.X != 2613 || guild.Bank.Z != 3094 {
		t.Errorf("guild tiles: stand %v bank %v", guild.Stand, guild.Bank)
	}
	aub, _ := GatewayByKey(GatewayAubury)
	if aub.NPC != Aubury || aub.Stand != AuburyTile || aub.Leash != AuburyLeash || aub.Bank != BankStand || aub.MinMagic != 0 {
		t.Errorf("aubury: %+v", aub)
	}
	if AuburyGateway() != aub {
		t.Error("AuburyGateway")
	}
	crom, _ := GatewayByKey(GatewayCromperty)
	if crom.NPC != "Wizard Cromperty" || crom.Stand.X != 2683 || crom.Stand.Z != 3326 {
		t.Errorf("cromperty: %+v", crom)
	}
	if _, ok := GatewayByKey("sedridor"); ok {
		t.Error("sedridor is not a surface gateway")
	}
	if got := strings.Join(GatewayKeys(), ","); got != "aubury,guild,cromperty" {
		t.Errorf("keys %q", got)
	}
	// The Guild needs Magic 66 and Aubury does not.
	if ok, why := guild.Allows(65); ok || !strings.Contains(why, "Magic 66") {
		t.Errorf("guild at 65: %v %q", ok, why)
	}
	if ok, _ := guild.Allows(66); !ok {
		t.Error("guild at 66")
	}
	if ok, _ := aub.Allows(1); !ok {
		t.Error("aubury at 1")
	}
	// Every gateway has a bank and a stand, and looks for the NPC as far as he can wander.
	for _, g := range Gateways() {
		if g.NPC == "" || g.Op == "" || g.Leash < 1 || g.BankName == "" || g.Bank.X == 0 || g.Wander < 1 {
			t.Errorf("%s incomplete: %+v", g.Key, g)
		}
		if g.Leash < g.Wander {
			t.Errorf("%s looks for %s within %d of his stand, but he walks %d from it", g.Key, g.NPC, g.Leash, g.Wander)
		}
	}
}

// The refusal lines the teleport can answer with: Rune Mysteries ends a miner's attempt at once; any other
// line is not a refusal.
func TestRefusalReason(t *testing.T) {
	const line = "You need to have completed the Rune Mysteries Quest to use this feature."
	if got := RefusalReason(line); got != "Rune Mysteries is not complete" {
		t.Errorf("got %q", got)
	}
	if !IsQuestRefusalMessage(line) {
		t.Error("the quest refusal")
	}
	for _, other := range []string{"", "Welcome to RuneScape.", "You mine some rune essence."} {
		if RefusalReason(other) != "" {
			t.Errorf("%q is not a refusal", other)
		}
	}
}

// Each gateway's NPC is where the table says in content's map files (a spawn "level x z: npc id" in
// maps/mX_Z.jm2) -- the stand tile is not a guess. Needs RSB0T_CONTENT_DIR.
func TestGatewayNPCsAreWhereContentSpawnsThem(t *testing.T) {
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "..", "content")
	}
	npcPack, err := os.Open(filepath.Join(dir, "pack", "npc.pack"))
	if err != nil {
		t.Skipf("no content checkout (%v)", err)
	}
	defer npcPack.Close()
	ids := map[string]int{}
	sc := bufio.NewScanner(npcPack)
	for sc.Scan() {
		if id, name, ok := strings.Cut(strings.TrimSpace(sc.Text()), "="); ok {
			if n, err := strconv.Atoi(id); err == nil {
				ids[name] = n
			}
		}
	}
	for _, c := range []struct {
		key, internal string
	}{{GatewayAubury, "aubury"}, {GatewayGuild, "guild_wizard"}, {GatewayCromperty, "ardounge_wizard"}} {
		gw, _ := GatewayByKey(c.key)
		id := ids[c.internal]
		if id == 0 {
			t.Errorf("%s: npc %s is not in npc.pack", c.key, c.internal)
			continue
		}
		sq := "m" + strconv.Itoa(gw.Stand.X>>6) + "_" + strconv.Itoa(gw.Stand.Z>>6) + ".jm2"
		f, err := os.Open(filepath.Join(dir, "maps", sq))
		if err != nil {
			t.Errorf("%s: %v", c.key, err)
			continue
		}
		found, inNPC := false, false
		ms := bufio.NewScanner(f)
		for ms.Scan() {
			line := strings.TrimSpace(ms.Text())
			if strings.HasPrefix(line, "====") {
				inNPC = line == "==== NPC ===="
				continue
			}
			if !inNPC {
				continue
			}
			head, tail, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			hf := strings.Fields(head)
			if n, err := strconv.Atoi(strings.TrimSpace(tail)); err == nil && n == id && len(hf) == 3 {
				lv, _ := strconv.Atoi(hf[0])
				x, _ := strconv.Atoi(hf[1])
				z, _ := strconv.Atoi(hf[2])
				if lv == 0 && gw.Stand.X>>6<<6+x == gw.Stand.X && gw.Stand.Z>>6<<6+z == gw.Stand.Z {
					found = true
				}
			}
		}
		f.Close()
		if !found {
			t.Errorf("%s: %s (npc %d) is not spawned at %v on level 0 in %s", c.key, gw.NPC, id, gw.Stand, sq)
		}
	}
}

// Each gateway's Wander is the NPC's wanderrange in content's npc config (the default, NpcType's 5, when the config sets none) --
// the stand tile is where he is spawned, not where he is.
func TestGatewayWanderIsTheNPCsWanderRange(t *testing.T) {
	dir := os.Getenv("RSB0T_CONTENT_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "..", "content")
	}
	scripts := filepath.Join(dir, "scripts")
	if _, err := os.Stat(scripts); err != nil {
		t.Skipf("no content checkout (%v)", err)
	}
	want := map[string]string{GatewayAubury: "aubury", GatewayGuild: "guild_wizard", GatewayCromperty: "ardounge_wizard"}
	got := map[string]int{}
	err := filepath.WalkDir(scripts, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".npc") {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		section := ""
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				section = strings.Trim(line, "[]")
				for key, name := range want {
					if section == name {
						if _, seen := got[key]; !seen {
							got[key] = 5 // NpcType.wanderrange's default
						}
					}
				}
				continue
			}
			for key, name := range want {
				if section == name {
					if v, ok := strings.CutPrefix(line, "wanderrange="); ok {
						if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
							got[key] = n
						}
					}
				}
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, name := range want {
		gw, _ := GatewayByKey(key)
		n, found := got[key]
		if !found {
			t.Errorf("%s: no [%s] in content's npc configs", key, name)
			continue
		}
		if gw.Wander != n {
			t.Errorf("%s: Wander %d, but content's %s has wanderrange %d", key, gw.Wander, name, n)
		}
	}
}
