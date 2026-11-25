package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/check"
	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"github.com/yourorg/web-seed/scripts/cmd/internal/graph"
	"gopkg.in/yaml.v3"
)

func tableSchemaForTest() *contractspec.Schema {
	return &contractspec.Schema{Contract: "data.tables",
		Targets: contractspec.Targets{Dir: "contracts/data/tables", Glob: "*.yaml", ID: "data.tables.<file>"},
	}
}

func writeTable(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTableContract(t *testing.T) {
	root := t.TempDir()
	writeTable(t, root, "note", `id: data.tables.note
owner_module: notes
`)
	owner, tableID, err := loadTableContract(root, tableSchemaForTest(), "note")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "notes" || tableID != "data.tables.note" {
		t.Fatalf("owner/tableID = %q/%q, want notes/data.tables.note", owner, tableID)
	}

	if _, _, err := loadTableContract(root, tableSchemaForTest(), "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing table not reported: %v", err)
	}

	writeTable(t, root, "bad", "id: data.tables.other\n")
	if _, _, err := loadTableContract(root, tableSchemaForTest(), "bad"); err == nil || !strings.Contains(err.Error(), "id must be") {
		t.Fatalf("id mismatch not reported: %v", err)
	}
}

// TestScaffoldPassesStructureCheck 验证 contract-new 的产物能通过结构、字段顺序与文件名校验。
// generated 字段在 gen-graph 前允许为空；跨契约引用不在本测试范围。
func TestScaffoldPassesStructureCheck(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		typ      string
		fileName string
		render   func(*contractspec.Schema) ([]byte, error)
	}{
		{"data.tables", "scaffold_note.yaml", func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderTable(s, "scaffold_note", "notes", "备注表")
		}},
		{"data.entities", "scaffold_note.yaml", func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderEntity(s, "scaffold_note", "data.tables.account", "account", "备注实体")
		}},
		{"data.queries", "scaffold_note.yaml", func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderQuery(s, "scaffold_note", "account", "data.tables.account", "备注查询")
		}},
		{"cap.services", "scaffold_note.yaml", func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderService(s, "scaffold_note", "account", "备注服务")
		}},
		{"cap.lifecycles", "scaffold_note.yaml", func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderLifecycle(s, "scaffold_note", "account", "备注生命周期")
		}},
		{"feature.rules", "scaffold_note.yaml", func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderRule(s, "scaffold_note", "account", "备注规则")
		}},
		{"feature.pages", "scaffold_note.yaml", func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderPage(s, "scaffold_note", "备注页面")
		}},
		{"cap.events", "scaffold_note.yaml", func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderEvents(s, "scaffold_note", "account", "备注事件")
		}},
		{"cap.protocols.http", "scaffold_note.yaml", func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderHTTP(s, "scaffold_note", "account", "备注HTTP", "cap.services.account.get", "/scaffold_note", "get")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			schema, _, err := contractspec.LoadSchemaByType(root, tc.typ)
			if err != nil {
				t.Fatal(err)
			}
			b, err := tc.render(schema)
			if err != nil {
				t.Fatal(err)
			}
			if errs := check.ValidateScaffold(root, tc.typ, b, tc.fileName); len(errs) != 0 {
				t.Fatalf("%s scaffold failed:\n%s", tc.typ, strings.Join(errs, "\n"))
			}
		})
	}
}

// TestScaffoldGenGraphSmoke 验证 gen-graph 能解析 contract-new 生成的 query / feature.rules 骨架。
// 生成字段在无 service 引用时保持空列表，不应导致 graph 失败。
func TestScaffoldGenGraphSmoke(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	copyContracts(t, filepath.Join(root, "contracts"), filepath.Join(tmp, "contracts"))

	qs, _, err := contractspec.LoadSchemaByType(root, "data.queries")
	if err != nil {
		t.Fatal(err)
	}
	qb, err := contractspec.RenderQuery(qs, "scaffold", "account", "data.tables.account", "备注查询")
	if err != nil {
		t.Fatal(err)
	}
	qb = []byte(strings.Replace(string(qb), "    params:", "    caches:\n      - data.tables.account.cache.id\n    params:", 1))
	if err := os.WriteFile(filepath.Join(tmp, "contracts/data/queries/scaffold.yaml"), qb, 0o644); err != nil {
		t.Fatal(err)
	}

	rs, _, err := contractspec.LoadSchemaByType(root, "feature.rules")
	if err != nil {
		t.Fatal(err)
	}
	rb, err := contractspec.RenderRule(rs, "scaffold", "account", "备注规则")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "contracts/features/rules/scaffold.yaml"), rb, 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := graph.Compute(tmp)
	if err != nil {
		t.Fatalf("gen-graph compute: %v", err)
	}
	if _, err := snap.Write(tmp); err != nil {
		t.Fatalf("gen-graph write: %v", err)
	}

	var qdoc map[string]any
	if b, err := os.ReadFile(filepath.Join(tmp, "contracts/data/queries/scaffold.yaml")); err != nil {
		t.Fatal(err)
	} else if err := yaml.Unmarshal(b, &qdoc); err != nil {
		t.Fatal(err)
	}
	qitems, _ := qdoc["queries"].([]any)
	if len(qitems) != 1 {
		t.Fatalf("query scaffold lost: %v", qdoc)
	}
	qitem, _ := qitems[0].(map[string]any)
	if _, ok := qitem["used_by"]; !ok {
		t.Fatalf("query.used_by missing after graph write: %v", qitem)
	}

	var rdoc map[string]any
	if b, err := os.ReadFile(filepath.Join(tmp, "contracts/features/rules/scaffold.yaml")); err != nil {
		t.Fatal(err)
	} else if err := yaml.Unmarshal(b, &rdoc); err != nil {
		t.Fatal(err)
	}
	ritems, _ := rdoc["rules"].([]any)
	if len(ritems) != 1 {
		t.Fatalf("rule scaffold lost: %v", rdoc)
	}
	ritem, _ := ritems[0].(map[string]any)
	if _, ok := ritem["implemented_by"]; !ok {
		t.Fatalf("rule.implemented_by missing after graph write: %v", ritem)
	}
}

func copyContracts(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestScaffoldCheckRun 验证 contract-new 产物在 service 引用后能通过完整 check.Run。
func TestScaffoldCheckRun(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	copyContracts(t, filepath.Join(root, "contracts"), filepath.Join(tmp, "contracts"))

	qs, _, err := contractspec.LoadSchemaByType(root, "data.queries")
	if err != nil {
		t.Fatal(err)
	}
	qb, err := contractspec.RenderQuery(qs, "scaffold", "account", "data.tables.account", "备注查询")
	if err != nil {
		t.Fatal(err)
	}
	qb = []byte(strings.Replace(string(qb), "    params:", "    caches:\n      - data.tables.account.cache.id\n    params:", 1))
	if err := os.WriteFile(filepath.Join(tmp, "contracts/data/queries/scaffold.yaml"), qb, 0o644); err != nil {
		t.Fatal(err)
	}

	rs, _, err := contractspec.LoadSchemaByType(root, "feature.rules")
	if err != nil {
		t.Fatal(err)
	}
	rb, err := contractspec.RenderRule(rs, "scaffold", "account", "备注规则")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "contracts/features/rules/scaffold.yaml"), rb, 0o644); err != nil {
		t.Fatal(err)
	}

	service := `id: cap.services.scaffold
summary: "脚手架服务"
owner_module: account
schema_version: 1
stability: draft
do_not: []
services:
  - name: get
    summary: "读取脚手架查询"
    visibility: public
    tx_mode: read_only
    params:
      - { name: ctx, type: context.Context }
      - { name: id, type: _types.basics.id }
    returns:
      - { name: exists, type: _types.basics.boolean, value: step.load.exists }
      - { name: err, type: error, value: "*" }
    steps:
      - id: load
        kind: query
        summary: "调用脚手架查询"
        input:
          - { name: id, value: params.id, description: "主键" }
        output:
          - { name: exists, type: _types.basics.boolean, description: "是否存在" }
        errors: []
        query: data.queries.scaffold.exists_scaffold
        goto: note
      - id: note
        kind: logic
        summary: "引用脚手架规则"
        input: []
        output: []
        errors:
          - platform.errors.COMMON_INTERNAL
        rules:
          - { rule: feature.rules.scaffold.example }
        goto: "$end"
`
	if err := os.WriteFile(filepath.Join(tmp, "contracts/capabilities/services/scaffold.yaml"), []byte(service), 0o644); err != nil {
		t.Fatal(err)
	}

	indexPath := filepath.Join(tmp, "contracts/index.yaml")
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	indexText := string(indexBytes)
	indexText = strings.Replace(indexText, "\nglobal_notes:", "\n  - { id: data.queries.scaffold, path: data/queries/scaffold.yaml, summary: \"x\", consumers: [tools], enforcement: tested, stability: draft }\n  - { id: feature.rules.scaffold, path: features/rules/scaffold.yaml, summary: \"x\", consumers: [tools], enforcement: tested, stability: draft }\n  - { id: cap.services.scaffold, path: capabilities/services/scaffold.yaml, summary: \"x\", consumers: [api], enforcement: tested, stability: draft }\nglobal_notes:", 1)
	if err := os.WriteFile(indexPath, []byte(indexText), 0o644); err != nil {
		t.Fatal(err)
	}

	snap, err := graph.Compute(tmp)
	if err != nil {
		t.Fatalf("gen-graph compute: %v", err)
	}
	if _, err := snap.Write(tmp); err != nil {
		t.Fatalf("gen-graph write: %v", err)
	}

	if errs := check.Run(tmp); len(errs) != 0 {
		t.Fatalf("check.Run failed:\n%s", strings.Join(errs, "\n"))
	}
}

// TestTypeVocabularyCrossFileCheckRun 验证完整 check.Run 下两个文件引用同一缺失类型都会报错，不跨文件去重。
func TestTypeVocabularyCrossFileCheckRun(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	copyContracts(t, filepath.Join(root, "contracts"), filepath.Join(tmp, "contracts"))
	dir := filepath.Join(tmp, "contracts/platform/analytics")
	for _, name := range []string{"zz_probe_a.yaml", "zz_probe_b.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("probe:\n  type: string\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	errs := check.Run(tmp)
	var hits []string
	for _, e := range errs {
		if strings.Contains(e, "[schema.type-vocabulary]") && strings.Contains(e, "zz_probe_") {
			hits = append(hits, e)
		}
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 cross-file vocabulary errors, got %d:\n%s", len(hits), strings.Join(errs, "\n"))
	}
}

// TestFeaturePageScaffoldCheck 端到端验证 feature.pages 骨架 + index 登记能通过完整 checker。
func TestFeaturePageScaffoldCheck(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	copyContracts(t, filepath.Join(root, "contracts"), filepath.Join(tmp, "contracts"))

	schema, _, err := contractspec.LoadSchemaByType(root, "feature.pages")
	if err != nil {
		t.Fatal(err)
	}
	page, err := contractspec.RenderPage(schema, "smoke_page", "临时页面")
	if err != nil {
		t.Fatal(err)
	}
	pagePath := filepath.Join(tmp, "contracts/features/pages/smoke_page.yaml")
	if err := os.WriteFile(pagePath, page, 0o644); err != nil {
		t.Fatal(err)
	}

	indexPath := filepath.Join(tmp, "contracts/index.yaml")
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := "  - { id: feature.pages.smoke_page, path: features/pages/smoke_page.yaml, summary: \"临时页面\", consumers: [web/admin], enforcement: review-only, stability: draft }\n"
	indexText := strings.Replace(string(indexBytes), "\n  # ---------- platform ----------", "\n"+entry+"\n  # ---------- platform ----------", 1)
	if err := os.WriteFile(indexPath, []byte(indexText), 0o644); err != nil {
		t.Fatal(err)
	}

	if errs := check.Run(tmp); len(errs) != 0 {
		t.Fatalf("feature.pages scaffold check failed:\n%s", strings.Join(errs, "\n"))
	}
}
