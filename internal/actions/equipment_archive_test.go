package actions

import (
	"os"
	"testing"

	"rsb0t-go/internal/archive"
)

// PlayerWorn and PlayerPack are only safe to fall back on while they still name the components a
// healthy sidebar tab would have resolved to. Nothing else in this package can check that: every
// other test here runs against equipmentSession(), whose archive.Components are hand-built to
// match these two constants, so they would go on passing after a content rebuild moved the real
// ids and left the fallbacks pointing at nothing.
//
// This is the test that would notice. It parses the engine's own client interface archive — the
// same JagFile it serves at /interface<crc>, read off disk, no server contact — and re-derives
// both ids from the properties that identify the panels rather than looking them up and agreeing
// with itself:
//
//   - the worn panel is the only TYPE_INV component whose first item option is "Remove", which is
//     wornitems.if's [wear];
//   - the backpack is the only TYPE_INV component with item options and "Use" both enabled and no
//     item-option labels at all, which is inventory.if's [inv].
//
// Each property has to pick out exactly ONE component, which is what makes it identifying rather
// than merely true of the right one. Then it checks what the constants are actually for: that
// FindInvComponentIn reaches each from its root layer under the very predicate tabInvComponent
// uses, so the fixed id is the same answer the tab gives when the tab works.
//
// Set RSB0T_INTERFACE_ARCHIVE to that archive — an engine checkout's
// data/pack/client/interface, or the bytes of a GET /interface<crc> from a running engine — to
// run it. Without it this skips, the way the collision-pack and config-archive tests do; the path
// is not hardcoded because it lives outside this repo and differs between machines.
func TestConstantsMatchTheRealArchive(t *testing.T) {
	path := os.Getenv("RSB0T_INTERFACE_ARCHIVE")
	if path == "" {
		t.Skip("set RSB0T_INTERFACE_ARCHIVE to the client interface archive to check PlayerWorn/PlayerPack against it")
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		t.Skipf("RSB0T_INTERFACE_ARCHIVE names %s, which is not there", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	jag, err := archive.ParseJagFile(raw)
	if err != nil {
		t.Fatalf("parsing %s as a JagFile: %v", path, err)
	}
	data, err := jag.Read("data")
	if err != nil || data == nil {
		t.Fatalf("reading the 'data' entry of %s: %v", path, err)
	}
	components := archive.ParseInterfaces(data)
	if len(components) == 0 {
		t.Fatalf("%s parsed to no components at all", path)
	}

	var removeFirst, packShaped []int
	for id, com := range components {
		if com.Type != archive.TypeInv {
			continue
		}
		if com.Iop[0] == "Remove" {
			removeFirst = append(removeFirst, id)
		}
		labelled := false
		for _, label := range com.Iop {
			if label != "" {
				labelled = true
			}
		}
		if com.ObjOps && com.ObjUse && !labelled {
			packShaped = append(packShaped, id)
		}
	}

	if len(removeFirst) != 1 || removeFirst[0] != PlayerWorn {
		t.Errorf(`TYPE_INV components whose first item option is "Remove" = %v, want exactly [%d] (PlayerWorn)`, removeFirst, PlayerWorn)
	}
	if len(packShaped) != 1 || packShaped[0] != PlayerPack {
		t.Errorf("TYPE_INV components with objOps and objUse set and no item-option labels = %v, want exactly [%d] (PlayerPack)", packShaped, PlayerPack)
	}

	for _, c := range []struct {
		name  string
		comID int
		layer int
	}{
		{"worn", PlayerWorn, 1644},
		{"pack", PlayerPack, 3213},
	} {
		com := components[c.comID]
		if com == nil {
			t.Errorf("the %s component %d is not in this archive at all", c.name, c.comID)
			continue
		}
		if com.Type != archive.TypeInv {
			t.Errorf("the %s component %d has type %d, want TYPE_INV (%d)", c.name, c.comID, com.Type, archive.TypeInv)
		}
		if com.LayerID != c.layer {
			t.Errorf("the %s component %d sits under layer %d, want %d", c.name, c.comID, com.LayerID, c.layer)
		}
	}

	// The accept funcs are tabInvComponent's own: any TYPE_INV for tab 4 (the worn panel does not
	// set objOps, which is why that special case exists), objOps for tab 3.
	if got := archive.FindInvComponentIn(components, 1644, func(*archive.Component) bool { return true }); got != PlayerWorn {
		t.Errorf("FindInvComponentIn(1644) = %d, want PlayerWorn %d", got, PlayerWorn)
	}
	if got := archive.FindInvComponentIn(components, 3213, func(c *archive.Component) bool { return c.ObjOps }); got != PlayerPack {
		t.Errorf("FindInvComponentIn(3213, objOps) = %d, want PlayerPack %d", got, PlayerPack)
	}

	// inventory.if's root holds nothing but the panel itself, which is why tab 3's breadth-first
	// search cannot land anywhere else even if another objOps panel appeared in the archive.
	if root := components[3213]; root == nil {
		t.Errorf("layer 3213 is not in this archive")
	} else if len(root.Children) != 1 || root.Children[0] != PlayerPack {
		t.Errorf("layer 3213's children = %v, want exactly [%d]", root.Children, PlayerPack)
	}
}
