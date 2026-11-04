package questmods

// The stage oracles of Dwarf Cannon's customs. Engine.StageIs re-reads the journal only when the engine's reading is stale, and the reading
// Engine.Snapshot took at the start of the tick is fresh, so a custom that asks StageIs after doing something gets the stage from BEFORE it
// acted. Four customs did exactly that (the remains, the child, the cannon, the report): the quest still moved on at the next tick, but each
// run lost the best part of ten minutes of its slot waiting or re-Inspecting for a result it already had, and logged a success as a failure.
// The customs run on a real session, so the regression is pinned in two halves: the helper's order of calls against a fake with the engine's
// own staleness rule, and the source (parsed, not grepped) so no custom can go back to asking StageIs bare.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/questplan"
)

// staleEngine is the engine's stage reading: StageIs answers from the last reading and reads the journal again only when it was
// invalidated, as Engine.StageIs does.
type staleEngine struct {
	fresh  bool
	read   int // the reading the engine holds
	actual int // what the journal says now
	reads  int
}

func (f *staleEngine) InvalidateStage() { f.fresh = false }

func (f *staleEngine) StageIs(n int) bool {
	if !f.fresh {
		f.read, f.fresh = f.actual, true
		f.reads++
	}
	return f.read == n
}

func TestTheStageOracleOpensTheJournalAgainAfterAStep(t *testing.T) {
	// The snapshot read stage 21 (the remains not taken); the take then moved the journal to 22.
	f := &staleEngine{fresh: true, read: questplan.McTowerRemains - 1, actual: questplan.McTowerRemains}
	if f.StageIs(questplan.McTowerRemains) {
		t.Fatal("the fake does not model the engine: a fresh reading should answer with the stage from before the step")
	}
	if f.reads != 0 {
		t.Fatalf("a fresh reading was read again (%d reads)", f.reads)
	}
	if !stageNow(f, questplan.McTowerRemains) {
		t.Fatal("stageNow answered with the stage from before the step")
	}
	if f.reads != 1 {
		t.Fatalf("stageNow read the journal %d times, want one", f.reads)
	}
	// And the stage from before the step is not what it reports.
	g := &staleEngine{fresh: true, read: questplan.McChildRescued - 1, actual: questplan.McChildRescued}
	if stageNow(g, questplan.McChildRescued-1) {
		t.Fatal("stageNow still believes the stage before the step")
	}
}

func TestTheStageOracleDoesNotInventAStage(t *testing.T) {
	f := &staleEngine{fresh: true, read: questplan.McCannonFixed - 1, actual: questplan.McCannonFixed - 1}
	if stageNow(f, questplan.McCannonFixed) {
		t.Fatal("stageNow reported a stage the journal does not show")
	}
}

// dwarfCannonFuncs parses dwarfcannon.go and returns its top-level functions by name.
func dwarfCannonFuncs(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "dwarfcannon.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
			out[fn.Name.Name] = fn
		}
	}
	return out
}

// callsIn returns, for the function fn, how many times each named function or method is called (a method by its selector name).
func callsIn(fn *ast.FuncDecl) map[string]int {
	out := map[string]int{}
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch f := call.Fun.(type) {
		case *ast.Ident:
			out[f.Name]++
		case *ast.SelectorExpr:
			out[f.Sel.Name]++
		}
		return true
	})
	return out
}

func TestDwarfCannonAsksTheStageOnlyThroughItsTwoOracles(t *testing.T) {
	funcs := dwarfCannonFuncs(t)
	// The only places allowed to name StageIs: the helper that invalidates first, and the cached wrapper.
	allowed := map[string]bool{"stageNow": true, "cannonStageCached": true}
	var bare []string
	// The customs are function literals in DwarfCannon() as well as named functions, so walk the whole file, by enclosing function.
	for name, fn := range funcs {
		if allowed[name] {
			continue
		}
		if callsIn(fn)["StageIs"] > 0 {
			bare = append(bare, name)
		}
	}
	sort.Strings(bare)
	if len(bare) > 0 {
		t.Fatalf("%s call Engine.StageIs bare: after a step it answers with the stage from before it; use cannonStageNow (or cannonStageCached for the opening question)", strings.Join(bare, ", "))
	}
	now := funcs["stageNow"]
	if now == nil {
		t.Fatal("stageNow is missing")
	}
	// The invalidation comes first: the first statement of stageNow is the call.
	first, ok := now.Body.List[0].(*ast.ExprStmt)
	if !ok {
		t.Fatalf("stageNow's first statement is %T, want the InvalidateStage call", now.Body.List[0])
	}
	call, ok := first.X.(*ast.CallExpr)
	if !ok {
		t.Fatalf("stageNow's first statement is %T, want the InvalidateStage call", first.X)
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "InvalidateStage" {
		t.Fatal("stageNow does not invalidate the reading before asking")
	}
}

func TestDwarfCannonCustomsThatActUseTheFreshOracleForTheirResult(t *testing.T) {
	funcs := dwarfCannonFuncs(t)
	// Each of these ends in an action whose result is a stage change, so each must read the journal again for it.
	for _, name := range []string{"cannonTakeRemains", "cannonChild", "cannonRepair", "cannonReport"} {
		fn := funcs[name]
		if fn == nil {
			t.Fatalf("%s is missing", name)
		}
		if callsIn(fn)["cannonStageNow"] == 0 {
			t.Errorf("%s never asks cannonStageNow: its result would be judged on the stage from before it acted", name)
		}
	}
	// The cached oracle is for the opening question and the wait that no action of the custom's own can have moved: nowhere else.
	allowed := map[string]bool{"cannonTower": true, "cannonTakeRemains": true}
	for name, fn := range funcs {
		if callsIn(fn)["cannonStageCached"] > 0 && !allowed[name] {
			t.Errorf("%s asks the cached stage; after an action it must read the journal again", name)
		}
	}
	// cannonRepair and cannonReport judge only what they have just done, so neither may use the cached stage at all.
	for _, name := range []string{"cannonChild", "cannonRepair", "cannonReport"} {
		if callsIn(funcs[name])["cannonStageCached"] > 0 {
			t.Errorf("%s asks the cached stage for a result it has just produced", name)
		}
	}
}
