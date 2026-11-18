package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testDoc() map[string]any {
	return map[string]any{
		"id":           "cap.services.account",
		"summary":      "账号服务",
		"owner_module": "account",
		"schemas": map[string]any{
			"ZFilter": map[string]any{},
			"AFilter": map[string]any{},
		},
		"services": []any{
			map[string]any{
				"name":           "get",
				"summary":        "获取账号",
				"visibility":     "public",
				"transaction":    "read_only",
				"max_iterations": 3,
				"params": []any{
					map[string]any{"name": "ctx", "type": "context.Context"},
					map[string]any{"name": "id", "type": "integer"},
				},
				"returns": []any{
					map[string]any{"name": "account", "type": "data.entities.account"},
					map[string]any{"name": "err", "type": "error"},
				},
				"steps": []any{
					map[string]any{
						"id":      "load",
						"kind":    "logic",
						"summary": "读取账号",
						"when":    "input.input.auth_type == 0",
						"errors":  []any{"platform.errors.ACCOUNT_NOT_FOUND"},
						"input":   map[string]any{"account": "step.load.other"},
						"output": []any{
							map[string]any{"name": "account", "type": "data.entities.account", "nullable": true},
						},
						"queries": []any{
							map[string]any{
								"query": "data.queries.account.get_account",
								"input": map[string]any{
									"id": "input.id",
									"b":  "step.load.other",
									"a":  "input.number",
								},
							},
						},
						"rules": []any{"feature.rules.account.self_only"},
						"on_error": []any{
							map[string]any{"error": "platform.errors.COMMON_INTERNAL", "action": "goto", "target": "load"},
						},
					},
				},
			},
		},
	}
}

func TestParseAndRenderFile(t *testing.T) {
	f, err := parseFile("/tmp/account.yaml", testDoc())
	if err != nil {
		t.Fatal(err)
	}
	if f.GoName != "Account" {
		t.Fatalf("GoName = %q, want Account", f.GoName)
	}
	if got := strings.Join(f.Schemas, ","); got != "AFilter,ZFilter" {
		t.Fatalf("Schemas = %q, want sorted", got)
	}
	if f.Services[0].ID != "cap.services.account.get" {
		t.Fatalf("service ID = %q", f.Services[0].ID)
	}

	out, err := renderFile(f, "example.com/apps/api")
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	for _, want := range []string{
		`var AccountContract = contracttest.File{`,
		`"input.input.auth_type == 0"`,
		`Action: "goto"`,
		`Target: "load"`,
		`MaxIterations: 3`,
		`{Name: "account", Type: "data.entities.account", Nullable: true}`,
		`"example.com/apps/api/internal/contracttest"`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("renderFile missing %q:\n%s", want, src)
		}
	}
	if strings.Index(src, `Name: "a"`) > strings.Index(src, `Name: "b"`) {
		t.Fatalf("input keys are not sorted:\n%s", src)
	}
}

func TestRenderTestCoversEveryStep(t *testing.T) {
	f, err := parseFile("/tmp/account.yaml", testDoc())
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderTest(f, "example.com/apps/api")
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	for _, want := range []string{
		`t.Run("get"`,
		`t.Run("load"`,
		`// step: load (logic)`,
		`// when: input.input.auth_type == 0`,
		`// input:`,
		`//   account <- step.load.other`,
		`// output:`,
		`//   account: data.entities.account (nullable)`,
		`// queries:`,
		`//   data.queries.account.get_account`,
		`//     id <- input.id`,
		`// errors:`,
		`//   platform.errors.ACCOUNT_NOT_FOUND`,
		`// on_error:`,
		`//   platform.errors.COMMON_INTERNAL -> goto load`,
		`// rules:`,
		`//   feature.rules.account.self_only`,
		`contracttest.RequireFile(t, AccountContract)`,
		`contracttest.RequireStep(t, AccountContract.Services[0].Steps[0])`,
		`// TODO: 行为断言写在非生成测试中`,
		`"example.com/apps/api/internal/contracttest"`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("renderTest missing %q:\n%s", want, src)
		}
	}
	// 每个 step 的空字段也要显式写出，避免遗漏。
	for _, want := range []string{
		"// depends_on: -",
		"// events: -",
		"// transitions: -",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("renderTest missing empty marker %q:\n%s", want, src)
		}
	}
}

func TestRenderIsStable(t *testing.T) {
	f, err := parseFile("/tmp/account.yaml", testDoc())
	if err != nil {
		t.Fatal(err)
	}
	first, err := renderFile(f, "example.com/apps/api")
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderFile(f, "example.com/apps/api")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("renderFile is not deterministic")
	}
}

func TestGoName(t *testing.T) {
	for _, tc := range []struct{ owner, file, want string }{
		{"account", "account", "Account"},
		{"account", "auth", "AccountAuth"},
		{"account", "verification", "AccountVerification"},
	} {
		if got := goName(tc.owner, tc.file); got != tc.want {
			t.Fatalf("goName(%q,%q) = %q, want %q", tc.owner, tc.file, got, tc.want)
		}
	}
}

func TestMultilineSummaryIsFolded(t *testing.T) {
	f := fileDoc{
		Stem: "x", GoName: "X", ID: "cap.services.x", OwnerModule: "account",
		Services: []serviceDoc{{
			ID: "cap.services.x.a", Name: "a",
			Summary:    "第一行\n  第二行",
			Visibility: "public", Transaction: "none",
			Steps: []stepDoc{{ID: "s", Kind: "logic", Summary: "步骤\n说明"}},
		}},
	}
	out, err := renderTest(f, "example.com/apps/api")
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	for _, want := range []string{"// summary: 第一行 第二行", "// summary: 步骤 说明"} {
		if !strings.Contains(src, want) {
			t.Fatalf("multiline summary not folded, missing %q:\n%s", want, src)
		}
	}
}

func TestBuildRejectsDuplicateGoName(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "apps/api/go.mod"), "module example.com/apps/api\n\ngo 1.27.0\n")
	dir := filepath.Join(root, "contracts/capabilities/services")
	writeService := func(name, owner string) {
		doc := fmt.Sprintf(`
id: cap.services.%s
summary: "x"
owner_module: %s
schema_version: 1
stability: draft
do_not: ["x"]
services:
  - name: a
    summary: "a"
    visibility: public
    transaction: none
    params:
      - {name: ctx, type: context.Context}
    returns:
      - {name: err, type: error}
    steps:
      - id: s
        kind: logic
        summary: "s"
        errors: []
        output: []
`, name, owner)
		mustWrite(t, filepath.Join(dir, name+".yaml"), doc)
	}
	// owner==file 折叠为 AccountA；owner+file 也拼成 AccountA。
	writeService("account_a", "account_a")
	writeService("a", "account")
	_, err := build(root)
	if err == nil || !strings.Contains(err.Error(), "same Go name") {
		t.Fatalf("build error = %v, want duplicate Go name", err)
	}
}

func TestEmptyListsAreOmitted(t *testing.T) {
	f := fileDoc{
		Stem: "x", GoName: "X", ID: "cap.services.x", OwnerModule: "account",
		Services: []serviceDoc{{
			ID: "cap.services.x.a", Name: "a", Summary: "a",
			Visibility: "public", Transaction: "none",
			Steps: []stepDoc{{ID: "s", Kind: "logic", Summary: "s", Errors: []string{}, Output: []fieldDoc{}}},
		}},
	}
	src, err := renderFile(f, "example.com/apps/api")
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"Errors:", "Output:"} {
		if strings.Contains(string(src), unwanted) {
			t.Fatalf("empty %s should be omitted:\n%s", unwanted, src)
		}
	}
	testSrc, err := renderTest(f, "example.com/apps/api")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"// errors: -", "// output: -"} {
		if !strings.Contains(string(testSrc), want) {
			t.Fatalf("empty list marker missing %q:\n%s", want, testSrc)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
