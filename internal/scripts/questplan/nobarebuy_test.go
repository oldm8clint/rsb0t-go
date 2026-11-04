package questplan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// No module decides a bare shop buy: shop items go through the provisioner (ShopItemStep), so a
// quest cannot pass with its provisioner disabled. Checked structurally on the package's own
// (non-test) source: any use of the StepBuy identifier fails, however a step is built with it
// (a literal's Kind field, an assignment, a variable, a call argument), except its declaration
// and a switch case that only MATCHES the kind (the engine's own Describe and ValidateStep).
func TestNoModuleDecidesABareShopBuy(t *testing.T) {
	// This package's modules, and the runners and modules that build steps on it (not
	// questrunner, whose executor dispatches on the kind).
	var files []string
	for _, dir := range []string{".", "../questmods", "../waterfallplan", "../waterfallrunner"} {
		fs, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, fs...)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		// Allowed: the declaration's own name, and a bare `case StepBuy:` label.
		decl := map[*ast.Ident]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.ValueSpec:
				for _, name := range v.Names {
					decl[name] = true
				}
			case *ast.CaseClause:
				for _, e := range v.List {
					switch x := e.(type) {
					case *ast.Ident:
						decl[x] = true
					case *ast.SelectorExpr:
						decl[x.Sel] = true
					}
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if ok && id.Name == "StepBuy" && !decl[id] {
				t.Errorf("%s: StepBuy used; get a shop item through ShopItemStep", fset.Position(id.Pos()))
			}
			return true
		})
	}
	if checked < 40 {
		t.Fatalf("only %d source files checked: the glob is not the package directory", checked)
	}
}
