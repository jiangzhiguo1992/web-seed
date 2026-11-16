package httpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestProblemWriterOnlyViaContext 静态检查：httpserver 包外禁止引用 ProblemWriter
// 或调用未导出的 writeProblem，必须通过 ProblemWriterFrom(ctx) 获取。
func TestProblemWriterOnlyViaContext(t *testing.T) {
	root := moduleRoot(t)
	pkgDir := mustGetwd(t)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || filepath.Dir(path) == pkgDir {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if x.Name == "ProblemWriter" {
					t.Errorf("%s: reference ProblemWriter only via ProblemWriterFrom(ctx)", fset.Position(x.Pos()))
				}
			case *ast.SelectorExpr:
				if x.Sel.Name == "ProblemWriter" {
					t.Errorf("%s: reference ProblemWriter only via ProblemWriterFrom(ctx)", fset.Position(x.Pos()))
				}
			case *ast.CompositeLit:
				if isProblemWriterType(x.Type) {
					t.Errorf("%s: construct ProblemWriter only via ProblemWriterFrom(ctx)", fset.Position(x.Pos()))
				}
			case *ast.CallExpr:
				if isNamedFunc(x.Fun, "writeProblem") {
					t.Errorf("%s: call writeProblem only inside httpserver; use ProblemWriterFrom(ctx).Write", fset.Position(x.Pos()))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir := mustGetwd(t)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", mustGetwd(t))
		}
		dir = parent
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func isProblemWriterType(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == "ProblemWriter"
	case *ast.SelectorExpr:
		return e.Sel.Name == "ProblemWriter"
	}
	return false
}

func isNamedFunc(expr ast.Expr, name string) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == name
	case *ast.SelectorExpr:
		return e.Sel.Name == name
	}
	return false
}
