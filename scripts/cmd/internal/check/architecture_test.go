package check

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// domainFiles 是 checker 内的领域文件。领域文件之间不得互相调用对方的函数；
// 共享类型/主流程放在 sharedFiles，由 capabilities.go 统一编排。
var domainFiles = map[string]string{
	"services.go":      "services",
	"service_chain.go": "services",
	"service_types.go": "services",
	"http.go":          "http",
	"lifecycles.go":    "lifecycles",
	"pages.go":         "pages",
	"events.go":        "events",
	"rules.go":         "rules",
}

var sharedFiles = map[string]bool{
	"access.go": true, "audit.go": true, "capabilities.go": true, "checker.go": true,
	"entities.go": true, "enum_codes.go": true, "enums.go": true,
	"errors.go": true, "modules.go": true, "order.go": true,
	"placement.go": true, "queries.go": true, "query_indexes.go": true, "query_storage.go": true,
	"redundancy.go": true, "references.go": true, "refs.go": true,
	"rule_ids.go": true, "rule_ids_gen.go": true, "scaffold.go": true, "schema.go": true,
	"platform.go":    true,
	"sensitivity.go": true, "storage.go": true, "tables.go": true,
	"targets.go": true, "typevocab.go": true, "util.go": true, "validate.go": true, "when.go": true,
}

// TestCheckerFilesAreClassified 防止新增 checker 文件绕过领域归属。
func TestCheckerFilesAreClassified(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if _, ok := domainFiles[name]; ok {
			continue
		}
		if sharedFiles[name] {
			continue
		}
		t.Errorf("unclassified checker file %s; add it to domainFiles or sharedFiles", name)
	}
}

// TestDomainFilesDoNotCallOtherDomains 用 AST 检查领域文件之间的函数调用。
// 共享文件（capabilities.go 等）可以编排所有领域；领域文件只能调用本领域或共享函数。
func TestDomainFilesDoNotCallOtherDomains(t *testing.T) {
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	owners := map[string]map[string]bool{}
	for name, domain := range domainFiles {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		files[name] = file
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if owners[fn.Name.Name] == nil {
				owners[fn.Name.Name] = map[string]bool{}
			}
			owners[fn.Name.Name][domain] = true
		}
	}
	for name, domain := range domainFiles {
		var calls []string
		ast.Inspect(files[name], func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				calls = append(calls, fun.Name)
			case *ast.SelectorExpr:
				calls = append(calls, fun.Sel.Name)
			}
			return true
		})
		for _, call := range calls {
			for owner := range owners[call] {
				if owner != domain {
					t.Errorf("%s (%s) calls %s from %s domain", name, domain, call, owner)
				}
			}
		}
	}
}
