// Package ruletests 统计测试文件对 rule ID 的断言引用。
package ruletests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Ref 是单个 rule ID 的测试引用。
type Ref struct {
	Asserted int // mustContainRule(..., "id") 或 "[id]" 字面断言
	Dynamic  int // 表驱动测试中通过变量传入 rule ID 的断言（每文件计 1）
}

// Refs 扫描 check 包的 *_test.go，汇总每个 rule ID 的引用。
func Refs(root string, ids []string) map[string]Ref {
	out := map[string]Ref{}
	files, _ := filepath.Glob(filepath.Join(root, "scripts/cmd/internal/check/*_test.go"))
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for id, ref := range RefsIn(string(b), ids) {
			cur := out[id]
			cur.Asserted += ref.Asserted
			cur.Dynamic += ref.Dynamic
			out[id] = cur
		}
	}
	return out
}

// RefsIn 统计单个测试文件内容的引用；注释不参与匹配。
func RefsIn(content string, ids []string) map[string]Ref {
	f, err := parser.ParseFile(token.NewFileSet(), "test.go", content, 0)
	if err != nil {
		return map[string]Ref{}
	}
	known := map[string]bool{}
	for _, id := range ids {
		known[id] = true
	}
	out := map[string]Ref{}
	var literals []string
	dynamic := false
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if callName(x.Fun) != "mustContainRule" || len(x.Args) < 3 {
				break
			}
			if lit, ok := x.Args[2].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil && known[s] {
					ref := out[s]
					ref.Asserted++
					out[s] = ref
				}
			} else {
				dynamic = true
			}
		case *ast.BinaryExpr:
			if x.Op == token.ADD && (isStringLit(x.X, "[") || isStringLit(x.Y, "[") || isStringLit(x.X, "]") || isStringLit(x.Y, "]")) {
				dynamic = true
			}
		case *ast.BasicLit:
			if x.Kind != token.STRING {
				break
			}
			s, err := strconv.Unquote(x.Value)
			if err != nil {
				break
			}
			literals = append(literals, s)
			if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
				id := s[1 : len(s)-1]
				if known[id] {
					ref := out[id]
					ref.Asserted++
					out[id] = ref
				}
			}
		}
		return true
	})
	if !dynamic {
		return out
	}
	for _, id := range ids {
		re := regexp.MustCompile(`(^|[^A-Za-z0-9_.-])` + regexp.QuoteMeta(id) + `([^A-Za-z0-9_.-]|$)`)
		for _, lit := range literals {
			if re.MatchString(lit) {
				ref := out[id]
				ref.Dynamic++
				out[id] = ref
				break
			}
		}
	}
	return out
}

func callName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func isStringLit(expr ast.Expr, want string) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == want
}
