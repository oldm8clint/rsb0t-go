package questplan

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every shop in the tool table (tools.go) stocks its tool, read from a Content 289 checkout (RSB0T_CONTENT, skipped without one):
//
//	RSB0T_CONTENT=<Content checkout> go test -run ToolShop ./internal/scripts/questplan/
//
// A tool line that sends an account to a shop that does not sell the tool is a walk for nothing, and the first purchase is the only
// thing that would say so. The test follows the keeper's npc to the inventory it owns (param=owned_shop) and checks the tool is on
// its stock list, and holds each tool's object id and each keeper's npc id to the pack.

// toolContentName is each tool's name in the content.
var toolContentName = map[ToolKind]string{
	ToolHammer: "hammer", ToolPickaxe: "bronze_pickaxe", ToolTinderbox: "tinderbox", ToolChisel: "chisel", ToolKnife: "knife",
	ToolNet: "net", ToolFishingRod: "fishing_rod", ToolFlyRod: "fly_fishing_rod",
}

// contentBlocks reads every [name] block of the files with the extension under root/scripts (not the _unpack copies) into the
// block's lines, by name.
func contentBlocks(t *testing.T, root, ext string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(filepath.Join(root, "scripts"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "_unpack" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ext {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name := ""
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				name = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
				continue
			}
			if name != "" && line != "" && !strings.HasPrefix(line, "//") {
				out[name] = append(out[name], line)
			}
		}
		return nil
	})
	if err != nil {
		t.Skipf("reading the content's %s files: %v", ext, err)
	}
	return out
}

func TestEveryToolShopStocksItsToolInTheContent(t *testing.T) {
	obj, npc := csPackIDs(t, "obj"), csPackIDs(t, "npc")
	root := os.Getenv("RSB0T_CONTENT")
	npcName := map[int]string{}
	for name, id := range npc {
		npcName[id] = name
	}
	npcBlocks, invBlocks := contentBlocks(t, root, ".npc"), contentBlocks(t, root, ".inv")

	for _, tool := range Tools {
		name := toolContentName[tool.Kind]
		if id, ok := obj[name]; !ok || id != tool.ID {
			t.Errorf("%s: object %q is %d in the pack (found %v), the table says %d", tool.Kind, name, id, ok, tool.ID)
		}
		for _, sh := range tool.Shops {
			keeper, ok := npcName[sh.Shop.NpcID]
			if !ok {
				t.Errorf("%s: npc %d (%s) is not in the pack", tool.Kind, sh.Shop.NpcID, sh.Shop.Npc)
				continue
			}
			owned := ""
			for _, line := range npcBlocks[keeper] {
				if v, found := strings.CutPrefix(line, "param=owned_shop,"); found {
					owned = v
				}
			}
			if owned != sh.Inv {
				t.Errorf("%s: %s (npc %d, %q) owns the shop %q in the content, the table says %q", tool.Kind, sh.Shop.Npc, sh.Shop.NpcID, keeper, owned, sh.Inv)
				continue
			}
			stocked := false
			for _, line := range invBlocks[sh.Inv] {
				if k, v, found := strings.Cut(line, "="); found && strings.HasPrefix(k, "stock") {
					if item, _, _ := strings.Cut(v, ","); item == name {
						stocked = true
					}
				}
			}
			if !stocked {
				t.Errorf("%s: the shop %q (%s) does not stock %s in the content", tool.Kind, sh.Inv, sh.Shop.Npc, name)
			}
		}
	}
}
