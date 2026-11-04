// Command gen writes internal/bankxp/table.go from a LostCityRS Content checkout.
//
// Run it the same way the protocol tables are regenerated (CLAUDE.md, "Regenerate opcode
// tables"), pointing at the content the server actually runs:
//
//	go run ./internal/bankxp/gen -content <Content checkout> -out internal/bankxp/table.go
//
// The checkout is NOT a build dependency: the generated table is committed, and this command
// only ever runs when somebody deliberately re-derives it. That is the same arrangement
// internal/protocol/gen already uses, and for the same reason — the numbers have to come from
// content, but the build must not need a copy of content to exist.
//
// What it reads, and nothing else:
//
//	pack/obj.pack                                                   obj id  -> content key
//	scripts/**/*.obj                                                content key -> display name, cost, params
//	scripts/**/*.dbrow, *.dbtable                                   the cooking/fletching/gem/runecraft tables
//	scripts/**/*.struct                                             smelting and spinning
//
// Anything a route needs that content does not state in data — a secondary ingredient that only
// the .rs2 script names, for instance — comes from the `extras` table at the bottom of this
// file, where every entry carries the script file and line that proves it. Nothing is added
// there from memory.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	content := flag.String("content", "", "path to the LostCityRS Content checkout (the directory holding pack/ and scripts/)")
	out := flag.String("out", "internal/bankxp/table.go", "file to write")
	flag.Parse()
	if *content == "" {
		fmt.Fprintln(os.Stderr, "gen: -content is required; it must point at the Content checkout this server runs")
		os.Exit(2)
	}
	g, err := load(*content)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	routes, err := g.routes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	src, err := render(*content, routes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
	fmt.Printf("gen: wrote %d routes to %s\n", len(routes), *out)
}

// ---- content parsing ----

type objDef struct {
	Key    string
	Name   string
	Cost   int
	Params map[string]string
	File   string
}

type dbRow struct {
	Key   string
	Table string
	Data  map[string][]string
	File  string
}

type structDef struct {
	Key    string
	Params map[string]string
	File   string
}

type content struct {
	idByKey map[string]int
	objs    map[string]*objDef
	rows    []*dbRow
	structs map[string]*structDef
}

func load(root string) (*content, error) {
	c := &content{idByKey: map[string]int{}, objs: map[string]*objDef{}, structs: map[string]*structDef{}}
	if err := c.loadObjPack(filepath.Join(root, "pack", "obj.pack")); err != nil {
		return nil, err
	}
	scripts := filepath.Join(root, "scripts")
	err := filepath.Walk(scripts, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".obj":
			return c.loadObjs(path)
		case ".dbrow":
			return c.loadRows(path)
		case ".struct":
			return c.loadStructs(path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(c.idByKey) == 0 || len(c.objs) == 0 {
		return nil, fmt.Errorf("read nothing useful from %s; is that a Content checkout?", root)
	}
	return c, nil
}

func (c *content) loadObjPack(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		id, key, ok := strings.Cut(strings.TrimSpace(s.Text()), "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(id)
		if err != nil {
			continue
		}
		c.idByKey[key] = n
	}
	return s.Err()
}

// sections splits one config file into [key] blocks of key=value lines, dropping // comments.
func sections(path string) ([]struct {
	Key   string
	Lines []string
}, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []struct {
		Key   string
		Lines []string
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			out = append(out, struct {
				Key   string
				Lines []string
			}{Key: strings.Trim(line, "[]")})
			continue
		}
		if len(out) == 0 {
			continue
		}
		out[len(out)-1].Lines = append(out[len(out)-1].Lines, line)
	}
	return out, nil
}

func (c *content) loadObjs(path string) error {
	secs, err := sections(path)
	if err != nil {
		return err
	}
	for _, sec := range secs {
		o := &objDef{Key: sec.Key, Params: map[string]string{}, File: path}
		for _, line := range sec.Lines {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch k {
			case "name":
				o.Name = v
			case "cost":
				o.Cost, _ = strconv.Atoi(v)
			case "param":
				pk, pv, ok := strings.Cut(v, ",")
				if ok {
					o.Params[pk] = pv
				}
			}
		}
		c.objs[sec.Key] = o
	}
	return nil
}

func (c *content) loadRows(path string) error {
	secs, err := sections(path)
	if err != nil {
		return err
	}
	for _, sec := range secs {
		r := &dbRow{Key: sec.Key, Data: map[string][]string{}, File: path}
		for _, line := range sec.Lines {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			switch k {
			case "table":
				r.Table = v
			case "data":
				col, rest, ok := strings.Cut(v, ",")
				if !ok {
					// A column with no value, e.g. a bare flag; keep it visible rather
					// than silently dropping it.
					col, rest = v, ""
				}
				if _, seen := r.Data[col]; seen {
					// LIST columns repeat. The first is the one every consumer here
					// wants; keeping it makes the choice explicit.
					continue
				}
				r.Data[col] = strings.Split(rest, ",")
			}
		}
		c.rows = append(c.rows, r)
	}
	return nil
}

func (c *content) loadStructs(path string) error {
	secs, err := sections(path)
	if err != nil {
		return err
	}
	for _, sec := range secs {
		s := &structDef{Key: sec.Key, Params: map[string]string{}, File: path}
		for _, line := range sec.Lines {
			k, v, ok := strings.Cut(line, "=")
			if !ok || k != "param" {
				continue
			}
			pk, pv, ok := strings.Cut(v, ",")
			if ok {
				s.Params[pk] = pv
			}
		}
		c.structs[sec.Key] = s
	}
	return nil
}

// ---- route building ----

type ingredient struct {
	ID    int
	Name  string
	Count int
}

type route struct {
	Key         string
	Skill       string
	Via         string
	Inputs      []ingredient
	Product     ingredient
	XP10        int
	Level       int
	Roll        bool
	SuccessLow  int
	SuccessHigh int
	Fleet       string
	Source      string
}

// item resolves a content key to an id and its display name. A key content never packs is a
// hard error rather than a silently dropped route: a missing item means this generator and the
// checkout disagree, and a table built from a disagreement is exactly the kind of confident
// wrong number docs/trustworthy_harnesses.md is about.
func (c *content) item(key string, count int) (ingredient, error) {
	key = strings.TrimSpace(key)
	id, ok := c.idByKey[key]
	if !ok {
		return ingredient{}, fmt.Errorf("content key %q is not in pack/obj.pack", key)
	}
	name := ""
	if o := c.objs[key]; o != nil {
		name = o.Name
	}
	if name == "" {
		return ingredient{}, fmt.Errorf("content key %q (id %d) has no display name in any .obj", key, id)
	}
	return ingredient{ID: id, Name: name, Count: count}, nil
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(path)
}

func (c *content) routes() ([]route, error) {
	var out []route
	add := func(r route, err error) error {
		if err != nil {
			return err
		}
		out = append(out, r)
		return nil
	}
	for _, r := range c.rows {
		var err error
		switch r.Table {
		case "cooking_generic":
			err = c.cooking(r, add)
		case "fletch_bow_table":
			err = c.cutLogs(r, add)
		case "fletching_table":
			err = c.stringBow(r, add)
		case "gem_cutting_table":
			err = c.cutGem(r, add)
		case "runecraft_table":
			err = c.runecraft(r, add)
		}
		if err != nil {
			return nil, err
		}
	}
	if err := c.fromStructs(add); err != nil {
		return nil, err
	}
	if err := c.bury(add); err != nil {
		return nil, err
	}
	if err := c.burn(add); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Inputs[0].ID != out[j].Inputs[0].ID {
			return out[i].Inputs[0].ID < out[j].Inputs[0].ID
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func n(r *dbRow, col string) int {
	v := r.Data[col]
	if len(v) == 0 {
		return 0
	}
	i, _ := strconv.Atoi(strings.TrimSpace(v[0]))
	return i
}

func s0(r *dbRow, col string) string {
	v := r.Data[col]
	if len(v) == 0 {
		return ""
	}
	return strings.TrimSpace(v[0])
}

type adder func(route, error) error

// cooking: content/scripts/skill_cooking/configs/cooking_source/cooking_generic.dbrow.
//
// Rows with experience 0 are the deliberate always-burn rows (cooked meat -> burnt meat), not
// processing routes, and are skipped. The roll taken is successchance_range where the row has
// one, because the fleet's own cooking happens at the Catherby RANGE (mulerunner/cook.go), not
// on a fire.
func (c *content) cooking(r *dbRow, add adder) error {
	xp := n(r, "experience")
	if xp <= 0 {
		return nil
	}
	in, err := c.item(s0(r, "uncooked"), 1)
	if err != nil {
		return err
	}
	prod, err := c.item(s0(r, "cooked"), 1)
	if err != nil {
		return err
	}
	low, high, roll := 0, 0, false
	if v := r.Data["successchance_range"]; len(v) >= 2 {
		low, _ = strconv.Atoi(strings.TrimSpace(v[0]))
		high, _ = strconv.Atoi(strings.TrimSpace(v[1]))
		roll = true
	} else if v := r.Data["successchance"]; len(v) >= 2 {
		low, _ = strconv.Atoi(strings.TrimSpace(v[0]))
		high, _ = strconv.Atoi(strings.TrimSpace(v[1]))
		roll = true
	}
	return add(route{
		Key: r.Key, Skill: "Cooking", Via: "Cook on a range",
		Inputs: []ingredient{in}, Product: prod,
		XP10: xp, Level: n(r, "levelrequired"),
		Roll: roll, SuccessLow: low, SuccessHigh: high,
		Source: r.File,
	}, nil)
}

// cutLogs: one log has three routes, which is the clearest case of the "more than one route"
// problem the panel has to be honest about. Shaft experience is not in the table at all — it is
// multiply($shaft_count, 5) in cut_logs.rs2 — so it is computed the same way here.
func (c *content) cutLogs(r *dbRow, add adder) error {
	log, err := c.item(s0(r, "log"), 1)
	if err != nil {
		return err
	}
	if shafts := n(r, "shafts"); shafts > 0 {
		prod, err := c.item("arrow_shaft", shafts)
		if err != nil {
			return err
		}
		if err := add(route{
			Key: r.Key + "_shafts", Skill: "Fletching", Via: "Cut into arrow shafts with a knife",
			Inputs: []ingredient{log}, Product: prod,
			XP10: shafts * 5, Level: 1,
			Source: r.File,
		}, nil); err != nil {
			return err
		}
	}
	for _, col := range []string{"shortbow", "longbow"} {
		v := r.Data[col]
		if len(v) < 3 {
			continue
		}
		prod, err := c.item(v[0], 1)
		if err != nil {
			return err
		}
		lvl, _ := strconv.Atoi(strings.TrimSpace(v[1]))
		xp, _ := strconv.Atoi(strings.TrimSpace(v[2]))
		if err := add(route{
			Key: r.Key + "_" + col, Skill: "Fletching", Via: "Cut into an unstrung " + col + " with a knife",
			Inputs: []ingredient{log}, Product: prod,
			XP10: xp, Level: lvl,
			Source: r.File,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// stringBow covers every fletching_table row: stringing an unstrung bow, and the arrow/dart/bolt
// rows that share the table. Secondary ingredients are not in the table; they come from extras,
// each citing the .rs2 line that consumes it.
func (c *content) stringBow(r *dbRow, add adder) error {
	xp := n(r, "experience")
	if xp <= 0 {
		return nil
	}
	in, err := c.item(s0(r, "item"), 1)
	if err != nil {
		return err
	}
	pv := r.Data["product"]
	if len(pv) == 0 {
		return nil
	}
	// The SECOND value of the product column is $max_count, the batch cap the scripts clamp
	// to (arrows.rs2:69 `min(inv_total(inv, $arrow_tips), $max_count)`), NOT a per-craft
	// product multiplier — every one of these scripts adds exactly one product per input
	// consumed. Reading it as a multiplier would have inflated arrow fletching fifteenfold.
	prod, err := c.item(pv[0], 1)
	if err != nil {
		return err
	}
	inputs := []ingredient{in}
	via := "Fletch"
	if ex, ok := extras[filepath.Base(r.File)]; ok {
		via = ex.via
		for _, e := range ex.also {
			it, err := c.item(e.key, e.count)
			if err != nil {
				return err
			}
			inputs = append(inputs, it)
		}
	}
	return add(route{
		Key: r.Key, Skill: "Fletching", Via: via,
		Inputs: inputs, Product: prod,
		XP10: xp, Level: n(r, "level"),
		Source: r.File,
	}, nil)
}

func (c *content) cutGem(r *dbRow, add adder) error {
	in, err := c.item(s0(r, "uncut_gem"), 1)
	if err != nil {
		return err
	}
	prod, err := c.item(s0(r, "cut_gem"), 1)
	if err != nil {
		return err
	}
	low, high, roll := 0, 0, false
	if v := r.Data["success_rate"]; len(v) >= 2 {
		low, _ = strconv.Atoi(strings.TrimSpace(v[0]))
		high, _ = strconv.Atoi(strings.TrimSpace(v[1]))
		roll = true
	}
	return add(route{
		Key: r.Key, Skill: "Crafting", Via: "Cut with a chisel",
		Inputs: []ingredient{in}, Product: prod,
		XP10: n(r, "experience"), Level: n(r, "level"),
		Roll: roll, SuccessLow: low, SuccessHigh: high,
		Source: r.File,
	}, nil)
}

// runecraft: every altar turns the SAME banked item (rune essence) into a different rune for a
// different experience, so rune essence ends up with one route per altar. That is not a flaw in
// the table; it is the truth, and the panel names whichever one it counted.
func (c *content) runecraft(r *dbRow, add adder) error {
	xp := n(r, "experience")
	if xp <= 0 {
		return nil
	}
	in, err := c.item("blankrune", 1)
	if err != nil {
		return err
	}
	prod, err := c.item(s0(r, "rune"), 1)
	if err != nil {
		return err
	}
	return add(route{
		Key: r.Key, Skill: "Runecraft", Via: "Craft at the " + s0(r, "name") + " altar",
		Inputs: []ingredient{in}, Product: prod,
		XP10: xp, Level: n(r, "level"),
		Source: r.File,
	}, nil)
}

// fromStructs covers smelting and spinning, which content models identically: a struct with
// ingredient/product/productexp params. Smelting spells its level requirement "levelrequired"
// and spinning spells it "levelrequire"; both are read.
func (c *content) fromStructs(add adder) error {
	keys := make([]string, 0, len(c.structs))
	for k := range c.structs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		st := c.structs[k]
		ing := st.Params["ingredient"]
		prodKey := st.Params["product"]
		xp, _ := strconv.Atoi(st.Params["productexp"])
		if ing == "" || prodKey == "" || xp <= 0 {
			continue
		}
		skill, via := "", ""
		switch {
		case strings.Contains(filepath.ToSlash(st.File), "/smelting/"):
			skill, via = "Smithing", "Smelt in a furnace"
		case strings.Contains(filepath.ToSlash(st.File), "/spinning/"):
			skill, via = "Crafting", "Spin on a spinning wheel"
		default:
			continue
		}
		in, err := c.item(ing, 1)
		if err != nil {
			return err
		}
		count := 1
		if n, err := strconv.Atoi(st.Params["bar_count"]); err == nil && n > 0 {
			count = n
		}
		prod, err := c.item(prodKey, count)
		if err != nil {
			return err
		}
		inputs := []ingredient{in}
		if sec := st.Params["ingredient_secondary"]; sec != "" && sec != "null" {
			n, _ := strconv.Atoi(st.Params["ingredient_secondary_count"])
			if n <= 0 {
				n = 1
			}
			it, err := c.item(sec, n)
			if err != nil {
				return err
			}
			inputs = append(inputs, it)
		}
		lvl, _ := strconv.Atoi(st.Params["levelrequired"])
		if lvl == 0 {
			lvl, _ = strconv.Atoi(st.Params["levelrequire"])
		}
		if err := add(route{
			Key: k, Skill: skill, Via: via,
			Inputs: inputs, Product: prod,
			XP10: xp, Level: lvl,
			Source: st.File,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// bury: every obj carrying a bone_exp param, wherever content defines it. bury_bone.rs2 reads
// exactly this param and advances Prayer by it, with no level requirement and no roll.
func (c *content) bury(add adder) error {
	keys := make([]string, 0, len(c.objs))
	for k := range c.objs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o := c.objs[k]
		xp, err := strconv.Atoi(o.Params["bone_exp"])
		if err != nil || xp <= 0 {
			continue
		}
		if _, ok := c.idByKey[k]; !ok {
			continue
		}
		in, err := c.item(k, 1)
		if err != nil {
			return err
		}
		if err := add(route{
			Key: "bury_" + k, Skill: "Prayer", Via: "Bury",
			Inputs: []ingredient{in}, Product: ingredient{ID: -1, Name: "nothing", Count: 0},
			XP10: xp, Level: 1,
			Source: o.File,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// burn: the log types in skill_firemaking's own obj file, which are the only objs whose
// productexp param firemaking.rs2 reads (stat_advance(firemaking, oc_param($log, productexp))).
// Restricting to that one file is what keeps smelting's identically-named param out.
func (c *content) burn(add adder) error {
	keys := make([]string, 0, len(c.objs))
	for k := range c.objs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o := c.objs[k]
		if !strings.HasSuffix(filepath.ToSlash(o.File), "skill_firemaking/configs/firemaking.obj") {
			continue
		}
		xp, err := strconv.Atoi(o.Params["productexp"])
		if err != nil || xp <= 0 {
			continue
		}
		in, err := c.item(k, 1)
		if err != nil {
			return err
		}
		lvl, _ := strconv.Atoi(o.Params["levelrequire"])
		if lvl <= 0 {
			lvl = 1
		}
		if err := add(route{
			Key: "burn_" + k, Skill: "Firemaking", Via: "Burn with a tinderbox",
			Inputs: []ingredient{in}, Product: ingredient{ID: -1, Name: "ashes", Count: 1},
			XP10: xp, Level: lvl,
			Source: o.File,
		}, nil); err != nil {
			return err
		}
	}
	return nil
}

// ---- the two hand-written overlays, each with its citation ----

type extra struct {
	via  string
	also []struct {
		key   string
		count int
	}
}

func need(key string, count int) struct {
	key   string
	count int
} {
	return struct {
		key   string
		count int
	}{key, count}
}

// extras supplies ingredients that only a .rs2 script names, keyed by the dbrow FILE the row
// came from. The file is the key rather than the table name because four different scripts
// share content's single `fletching_table` and each consumes a DIFFERENT second ingredient —
// keying by table once credited every arrow, dart and bolt row with a bow string. Every entry
// cites the line that consumes the item. Nothing goes in here from memory.
var extras = map[string]extra{
	// bows.rs2:37 — `inv_del(inv, bow_string, 1);` right beside the unstrung bow's own delete.
	"bows.dbrow": {via: "Attach a bow string", also: []struct {
		key   string
		count int
	}{need("bow_string", 1)}},
	// arrows.rs2:75-76 — `inv_del(inv, $arrow_tips, $arrows_count); inv_del(inv,
	// headless_arrow, $arrows_count);`
	"arrows.dbrow": {via: "Attach heads to headless arrows", also: []struct {
		key   string
		count int
	}{need("headless_arrow", 1)}},
	// darts.rs2:52-53 — `inv_del(inv, $dart_tips, $darts_count); inv_del(inv, feather,
	// $darts_count);`
	"darts.dbrow": {via: "Attach feathers to dart tips", also: []struct {
		key   string
		count int
	}{need("feather", 1)}},
	// bolts.rs2:78-79 — `inv_del(inv, $bolt_tips, $bolts_count); inv_del(inv, bolt,
	// $bolts_count);`
	"bolts.dbrow": {via: "Attach tips to bolts", also: []struct {
		key   string
		count int
	}{need("bolt", 1)}},
}

// fleetScript credits the registered script in THIS repository that actually runs a route today.
// Keyed by route key prefix; the longest matching prefix wins. Each entry names the Go file that
// proves it, so the credit can be checked the same way a content number can.
var fleetScript = []struct{ prefix, script, why string }{
	// internal/scripts/mulerunner/cook.go — MuleReceiver's idle pass withdraws banked raw fish
	// and cooks it at the Catherby range. It cooks ONLY the nine tiers in
	// mulereceiverplan.FishTiers, so only those nine rows are credited; crediting every
	// cooking row (pies, cake, stew) would claim the fleet processes goods it never touches.
	{"cooking_generic_shrimp", "MuleReceiver", "internal/scripts/mulerunner/cook.go"},
	{"cooking_generic_anchovies", "MuleReceiver", "internal/scripts/mulerunner/cook.go"},
	{"cooking_generic_mackerel", "MuleReceiver", "internal/scripts/mulerunner/cook.go"},
	{"cooking_generic_cod", "MuleReceiver", "internal/scripts/mulerunner/cook.go"},
	{"cooking_generic_tuna", "MuleReceiver", "internal/scripts/mulerunner/cook.go"},
	{"cooking_generic_lobster", "MuleReceiver", "internal/scripts/mulerunner/cook.go"},
	{"cooking_generic_bass", "MuleReceiver", "internal/scripts/mulerunner/cook.go"},
	{"cooking_generic_swordfish", "MuleReceiver", "internal/scripts/mulerunner/cook.go"},
	{"cooking_generic_shark", "MuleReceiver", "internal/scripts/mulerunner/cook.go"},
	// internal/scripts/bankfletchplan/plan.go — BankFletcher's only ported mode is Logs plus a
	// knife into Arrow shafts. The bow modes are explicitly NOT ported, so they get no credit.
	{"fletching_normal_shafts", "BankFletcher", "internal/scripts/bankfletchplan/plan.go"},
	// internal/scripts/boneplan/plan.go — BoneBurier's DisposalBury clicks Bury/Scatter on
	// every bone kind it finds, so the whole bury family is credited.
	{"bury_", "BoneBurier", "internal/scripts/boneplan/plan.go"},
	// internal/scripts/crafterplan/crafter.go — ProgressiveCrafter spins flax into bow strings
	// (crafterrunner.SpinFlaxUntil). The spinning struct's own section key is "flax".
	{"flax", "ProgressiveCrafter", "internal/scripts/crafterplan/crafter.go"},
}

func creditFor(key string) (string, string) {
	best, why := "", ""
	for _, f := range fleetScript {
		if strings.HasPrefix(key, f.prefix) && len(f.prefix) > len(best) {
			best, why = f.script, f.why
		}
	}
	return best, why
}

// ---- rendering ----

func render(contentRoot string, routes []route) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, `// Code generated by internal/bankxp/gen. DO NOT EDIT.
//
// Source: the LostCityRS Content checkout this server runs (client revision 289).
// Regenerate with:
//
//	go run ./internal/bankxp/gen -content <Content checkout> -out internal/bankxp/table.go
//
// XP10 is TENTHS of an experience point, exactly as content stores it (see the package doc).
// Every route names the content file its numbers came from.

package bankxp

// Routes is every processing route this table knows, sorted by the id of the item it consumes.
var Routes = []Route{
`)
	for _, r := range routes {
		script, _ := creditFor(r.Key)
		fmt.Fprintf(&b, "\t{Key: %q, Skill: %q, Via: %q, Inputs: []Ingredient{", r.Key, r.Skill, r.Via)
		for i, in := range r.Inputs {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "{ID: %d, Name: %q, Count: %d}", in.ID, in.Name, in.Count)
		}
		fmt.Fprintf(&b, "}, Product: Ingredient{ID: %d, Name: %q, Count: %d}, XP10: %d, Level: %d",
			r.Product.ID, r.Product.Name, r.Product.Count, r.XP10, r.Level)
		if r.Roll {
			fmt.Fprintf(&b, ", Roll: true, SuccessLow: %d, SuccessHigh: %d", r.SuccessLow, r.SuccessHigh)
		}
		if script != "" {
			fmt.Fprintf(&b, ", Fleet: %q", script)
		}
		fmt.Fprintf(&b, ", Source: %q},\n", rel(contentRoot, r.Source))
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}
