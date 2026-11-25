package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件为 rule-tests 映射补齐可直接驱动 checker 函数的行为断言。
// 每条断言都必须命中对应规则码，而不是只列出规则 ID。

func mustContainRule(t *testing.T, errs []string, rule string) {
	t.Helper()
	if !strings.Contains(strings.Join(errs, "\n"), "["+rule+"]") {
		t.Fatalf("missing [%s] in errors: %v", rule, errs)
	}
}

func TestFieldOrderRuleIDs(t *testing.T) {
	cases := []struct{ name, top, nested, model, body string }{
		{"table-key-order", "table-key-order", "table-key-order", "", "summary: \"x\"\nid: x\n"},
		{"entity-key-order", "entity-key-order", "entity-key-order", "", "summary: \"x\"\nid: x\n"},
		{"query-key-order", "query-key-order", "query-key-order", "", "summary: \"x\"\nid: x\n"},
		{"entity-model-order", "entity-key-order", "entity-key-order", "entity-model-order", "id: x\nmodel:\n  extends: []\n  name: X\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "x.yaml"), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			fields := []Field{
				{Name: "id"}, {Name: "summary"},
				{Name: "model", Fields: []Field{{Name: "name"}, {Name: "extends"}}},
			}
			c := &Checker{root: root}
			c.checkFieldOrder("x.yaml", fields, orderRules{top: tc.top, nested: tc.nested, model: tc.model})
			rules := strings.Join(c.errs, "\n")
			if !strings.Contains(rules, "["+tc.top+"]") && !strings.Contains(rules, "["+tc.nested+"]") && !strings.Contains(rules, "["+tc.model+"]") {
				t.Fatalf("missing rule code in: %v", c.errs)
			}
		})
	}
}

func TestTableTextLengths(t *testing.T) {
	c := &Checker{}
	c.checkTableTextLengths(map[string]any{
		"summary": strings.Repeat("x", tableSummaryMaxRunes+1),
		"columns": []any{map[string]any{
			"name":    "x",
			"comment": strings.Repeat("y", columnCommentMaxRunes+1),
		}},
	}, "x.yaml")
	mustContainRule(t, c.errs, "table-summary-length")
	mustContainRule(t, c.errs, "column-comment-length")
}

func TestIndexUsedByRef(t *testing.T) {
	cases := []map[string]any{
		{"used_by": []any{"bad"}},
		{"used_by": []any{"data.queries.missing"}},
		{"used_by": []any{"data.queries.x"}},
	}
	for _, m := range cases {
		c := &Checker{
			queryIDs:    map[string]string{"data.queries.x": "x.yaml"},
			queryTables: map[string]map[string]bool{"data.queries.x": {"data.tables.x": true}},
		}
		table := "data.tables.x"
		if _, ok := m["used_by"].([]any)[0].(string); ok && m["used_by"].([]any)[0] == "data.queries.x" {
			table = "data.tables.y"
		}
		c.checkIndexUsedBy(m, table, "x.yaml", 0, "idx_x")
		mustContainRule(t, c.errs, "index-used-by-ref")
	}
}

func TestTableReferencedBy(t *testing.T) {
	c := &Checker{
		baseID: "data.tables.base",
		tableDocs: map[string]map[string]any{
			"data.tables.x": {"table": map[string]any{"referenced_by": []any{}}},
		},
	}
	c.checkTableReferencedBy(map[string]string{"data.tables.x": "x.yaml"}, map[string]map[string]bool{
		"data.tables.x": {"data.tables.y": true},
	})
	mustContainRule(t, c.errs, "table-referenced-by")
}

func TestEntityBaseFileMissing(t *testing.T) {
	c := &Checker{root: t.TempDir()}
	c.checkEntityBase(&Schema{})
	mustContainRule(t, c.errs, "entity-base-file")
}

func TestEntityNoBaseFields(t *testing.T) {
	c := &Checker{}
	e := &entityInfo{Rel: "x.yaml", Fields: []entityFieldInfo{{Name: "ID", BaseRedeclared: true}}}
	c.checkEntityFields(e)
	mustContainRule(t, c.errs, "entity-no-base-fields")
}

func TestEntityModelTableShape(t *testing.T) {
	c := &Checker{}
	c.checkEntityModel(&entityInfo{Rel: "x.yaml"})
	mustContainRule(t, c.errs, "entity-table-shape")
}

func TestEntityIDRule(t *testing.T) {
	c := &Checker{}
	c.checkEntityID(&entityInfo{Rel: "x.yaml", ID: "data.entities.x"})
	mustContainRule(t, c.errs, "entity-id")
}

func TestEntityOwnerModuleRule(t *testing.T) {
	c := &Checker{tableOwners: map[string]string{"data.tables.x": "account"}}
	c.checkEntityOwnerModule(&entityInfo{Rel: "x.yaml", Primary: "data.tables.x", OwnerModule: "billing"})
	mustContainRule(t, c.errs, "entity-owner-module")
}

func TestEntityFieldColumnRule(t *testing.T) {
	c := &Checker{}
	e := &entityInfo{
		Rel:    "x.yaml",
		Tables: []string{"data.tables.a", "data.tables.b"},
		Fields: []entityFieldInfo{{Name: "ID", ColumnTable: "data.tables.a", Column: "id"}},
	}
	c.checkEntityFields(e)
	mustContainRule(t, c.errs, "entity-field-column")
}

func TestEntityRelationTypeRule(t *testing.T) {
	c := &Checker{}
	c.checkRelationFieldShape(entityFieldInfo{Name: "B", IsRelation: true, Type: "string", HasType: true, JoinFromRef: "a", JoinToRef: "b"}, "where")
	mustContainRule(t, c.errs, "entity-relation-type")
}

func TestEntityRelationTargetRule(t *testing.T) {
	c := &Checker{entities: map[string]*entityInfo{}}
	e := &entityInfo{
		ID: "data.entities.a", Rel: "a.yaml", Tables: []string{"data.tables.a"},
		Fields: []entityFieldInfo{{Name: "B", IsRelation: true, Type: "data.entities.missing", TargetID: "data.entities.missing"}},
	}
	c.checkEntityRelations(e, map[string]map[string]entityRelationEntry{})
	mustContainRule(t, c.errs, "entity-relation-target")
}

func TestEntityRelationCardinalityRule(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.a": {"id": {typ: "integer"}, "b_id": {typ: "integer"}},
			"data.tables.b": {"id": {typ: "integer"}},
		},
		entities: map[string]*entityInfo{
			"data.entities.b": {ID: "data.entities.b", Tables: []string{"data.tables.b"}},
		},
	}
	e := &entityInfo{
		ID: "data.entities.a", Rel: "a.yaml", Tables: []string{"data.tables.a"},
		Fields: []entityFieldInfo{{
			Name: "B", IsRelation: true, Type: "data.entities.b", TargetID: "data.entities.b",
			JoinFromTable: "data.tables.a", JoinFromColumn: "b_id",
			JoinToTable: "data.tables.b", JoinToColumn: "id",
		}},
	}
	c.checkEntityRelations(e, map[string]map[string]entityRelationEntry{})
	mustContainRule(t, c.errs, "entity-relation-cardinality")
}

func TestEntityDoNotSpecific(t *testing.T) {
	c := &Checker{}
	c.checkEntityDoNot(entityDoc{rel: "x.yaml", doc: map[string]any{
		"do_not": []any{"keep it clean"},
	}}, "account")
	mustContainRule(t, c.errs, "entity-do-not-specific")
}

func TestFamilyParamRules(t *testing.T) {
	schema := &Schema{
		Fields: []Field{{Name: "columns", ItemFields: []Field{{Name: "name"}, {Name: "type"}, {Name: "comment"}}}},
		ValueTypes: ValueTypes{TypeFamilies: map[string]ValueTypeFamily{
			"integer": {Params: map[string]Field{"min": {Type: "integer"}, "max": {Type: "integer"}}},
			"decimal": {Params: map[string]Field{"precision": {Type: "integer"}, "scale": {Type: "integer"}}},
		}},
	}

	c := &Checker{}
	c.checkFamilyParams(map[string]any{"bogus": 1}, "integer", "x.yaml", 0, "x", schema)
	mustContainRule(t, c.errs, "family-params")

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"min": 10, "max": 5}, "integer", "x.yaml", 0, "x", schema)
	mustContainRule(t, c.errs, "family-range")

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"precision": 5, "scale": 10}, "decimal", "x.yaml", 0, "x", schema)
	mustContainRule(t, c.errs, "family-decimal")
}

func TestColumnTypeSupportedRules(t *testing.T) {
	schema := &Schema{
		Fields: []Field{{Name: "columns", ItemFields: []Field{
			{Name: "name"}, {Name: "type"}, {Name: "comment"}, {Name: "bits"}, {Name: "unsigned"}, {Name: "min"}, {Name: "max"},
		}}},
		ValueTypes: ValueTypes{TypeFamilies: map[string]ValueTypeFamily{
			"any": {},
			"integer": {Params: map[string]Field{
				"bits":     {Type: "integer"},
				"unsigned": {Type: "boolean"},
				"min":      {Type: "integer"},
				"max":      {Type: "integer"},
			}},
		}},
	}

	c := &Checker{}
	c.checkFamilyParams(map[string]any{"name": "payload", "type": "_types.basics.any"}, "any", "x.yaml", 0, "payload", schema)
	mustContainRule(t, c.errs, "column-type-supported")

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"name": "count", "type": "_types.basics.integer", "unsigned": true}, "integer", "x.yaml", 0, "count", schema)
	mustContainRule(t, c.errs, "column-type-supported")
}

func TestEnumCodeLiteralRule(t *testing.T) {
	c := &Checker{
		enumCodes: map[string]map[int]bool{"data.enums.x": {1: true}},
		entities: map[string]*entityInfo{"data.entities.x": {Fields: []entityFieldInfo{{
			Type: "data.enums.x", SourceColumnRef: "data.tables.x.col.status",
			ColumnTable: "data.tables.x", Column: "status",
		}}}},
		tableDocs: map[string]map[string]any{
			"data.tables.x": {"constraints": []any{map[string]any{
				"name": "ck_x", "expr": "status IN (1, 2)",
			}}},
		},
	}
	c.checkEnumCodeLiterals()
	mustContainRule(t, c.errs, "enum-code-literal")
}

func TestQueryRuleIDs(t *testing.T) {
	t.Run("query-pagination", func(t *testing.T) {
		c := &Checker{}
		c.checkQueryPagination(&queryInfo{
			File: "x.yaml", ID: "data.queries.x",
			Returns: []queryReturn{{Name: "items", Kind: "scalar", Type: "string", List: true}},
		})
		mustContainRule(t, c.errs, "query-pagination")
	})

	t.Run("query-params", func(t *testing.T) {
		c := &Checker{}
		c.checkQueryParams(&queryInfo{
			File: "x.yaml", ID: "data.queries.x",
			Params: []queryParam{{Name: "id", Type: "string"}},
		})
		mustContainRule(t, c.errs, "query-params")
	})

	t.Run("query-return-columns", func(t *testing.T) {
		c := &Checker{tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {"id": {typ: "integer"}},
		}}
		c.checkColumnProjection("where", &queryInfo{
			File: "x.yaml", ID: "data.queries.x", Targets: []string{"data.tables.x"},
		}, []string{"bad"})
		mustContainRule(t, c.errs, "query-return-columns")
	})

	t.Run("query-errors", func(t *testing.T) {
		c := &Checker{}
		c.checkQueryErrors(&queryInfo{
			File: "x.yaml", ID: "data.queries.x",
			Errors: []string{"platform.errors.UNKNOWN"},
		})
		mustContainRule(t, c.errs, "query-errors")
	})

	t.Run("query-do-not-specific", func(t *testing.T) {
		c := &Checker{}
		c.checkQueryDoNot(queryDoc{rel: "x.yaml", fileID: "data.queries.x", doc: map[string]any{
			"do_not": []any{"keep it clean"},
		}})
		mustContainRule(t, c.errs, "query-do-not-specific")
	})

	t.Run("query-used-by", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "contracts/capabilities/services")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `id: cap.services.x
services:
  - name: get
    steps:
      - { id: load, kind: query, query: data.queries.x }
`
		if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		c := &Checker{
			root:       root,
			queryOrder: []string{"data.queries.x"},
			queries:    map[string]*queryInfo{"data.queries.x": {ID: "data.queries.x"}},
		}
		c.checkQueryUsedBy()
		mustContainRule(t, c.errs, "query-used-by")
	})

	t.Run("query-unused", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "contracts/capabilities/services")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `id: cap.services.x
services:
  - name: get
    steps: []
`
		if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		c := &Checker{
			root:       root,
			queryOrder: []string{"data.queries.x"},
			queries:    map[string]*queryInfo{"data.queries.x": {ID: "data.queries.x"}},
		}
		c.checkQueryUsedBy()
		mustContainRule(t, c.errs, "query-unused")
	})

	t.Run("query-name-unique", func(t *testing.T) {
		c := &Checker{root: t.TempDir(), queryDupIDs: map[string]bool{"data.queries.x": true}}
		c.checkQueries(&Schema{})
		mustContainRule(t, c.errs, "query-name-unique")
	})
}

func TestRuleDocRuleIDUnique(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/feature.rules.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkRuleDoc(map[string]any{
		"id": "feature.rules.wrong",
		"rules": []any{
			map[string]any{"id": "feature.rules.wrong.a"},
			map[string]any{"id": "feature.rules.wrong.a"},
		},
	}, "contracts/features/rules/x.yaml", schema, map[string]bool{})
	mustContainRule(t, c.errs, "rule-id-unique")
}

func TestServiceStructuralRules(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"name": "run", "summary": "x", "visibility": "public", "tx_mode": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{map[string]any{"name": "err", "type": "error", "value": "*"}},
			"steps":   []any{},
		}
	}

	t.Run("service-signature", func(t *testing.T) {
		root, err := filepath.Abs("../../../..")
		if err != nil {
			t.Fatal(err)
		}
		schema, err := loadSchema(filepath.Join(root, "contracts/_meta/cap.services.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		d := base()
		d["params"] = []any{map[string]any{"name": "id", "type": "integer"}}
		c := &Checker{root: root}
		c.checkServiceDoc(map[string]any{"id": "cap.services.x", "services": []any{d}}, "contracts/capabilities/services/x.yaml", schema, map[string]bool{})
		if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "params[0] must be ctx") || !strings.Contains(got, "[service-signature]") {
			t.Fatalf("missing service-signature: %s", got)
		}
	})

	t.Run("service-err-value", func(t *testing.T) {
		root, err := filepath.Abs("../../../..")
		if err != nil {
			t.Fatal(err)
		}
		schema, err := loadSchema(filepath.Join(root, "contracts/_meta/cap.services.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		d := base()
		d["returns"] = []any{map[string]any{"name": "err", "type": "error", "value": "step.x.err"}}
		c := &Checker{root: root}
		c.checkServiceDoc(map[string]any{"id": "cap.services.x", "services": []any{d}}, "contracts/capabilities/services/x.yaml", schema, map[string]bool{})
		if got := strings.Join(c.errs, "\n"); !strings.Contains(got, `return err must declare value "*"`) || !strings.Contains(got, "[service-signature]") {
			t.Fatalf("missing service err value check: %s", got)
		}
	})

	t.Run("service-id", func(t *testing.T) {
		root, err := filepath.Abs("../../../..")
		if err != nil {
			t.Fatal(err)
		}
		schema, err := loadSchema(filepath.Join(root, "contracts/_meta/cap.services.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		c := &Checker{root: root}
		c.checkServiceDoc(map[string]any{"id": "cap.services.wrong", "services": []any{base()}}, "contracts/capabilities/services/x.yaml", schema, map[string]bool{})
		if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "[service-id]") {
			t.Fatalf("missing service-id: %s", got)
		}
	})

	t.Run("service-unique", func(t *testing.T) {
		d := base()
		d["steps"] = []any{logicStep("a"), logicStep("a")}
		c := &Checker{}
		got := runServiceItem(t, d, c)
		if !strings.Contains(got, "duplicate step id") || !strings.Contains(got, "[service-unique]") {
			t.Fatalf("missing service-unique: %s", got)
		}
	})

	t.Run("service-visibility", func(t *testing.T) {
		d := base()
		d["steps"] = []any{map[string]any{
			"id": "call", "kind": "service", "summary": "x", "goto": "$end",
			"input":   []any{},
			"output":  []any{},
			"errors":  []any{},
			"service": "cap.services.other.run",
		}}
		c := &Checker{errCodeSet: map[string]bool{"COMMON_INTERNAL": true}}
		c.checkServiceItem(d, "f", "cap.services.x.run", "billing",
			map[string]serviceMeta{"cap.services.other.run": {visibility: "internal", module: "other"}},
			map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		if !strings.Contains(strings.Join(c.errs, "\n"), "cannot call internal") || !strings.Contains(strings.Join(c.errs, "\n"), "[service-visibility]") {
			t.Fatalf("missing service-visibility: %v", c.errs)
		}
	})
}

func TestServiceOwnerWrite(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/capabilities/services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `id: cap.services.x
services:
  - name: run
    owner_module: billing
    steps:
      - { id: write, kind: query, query: data.queries.x.write }
`
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{
		root:        root,
		tableOwners: map[string]string{"data.tables.account": "account"},
		queries: map[string]*queryInfo{"data.queries.x.write": {
			ID: "data.queries.x.write", Writes: []string{"data.tables.account"},
		}},
	}
	c.checkCapabilityWrites(map[string]string{"data.tables.account": "account.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), "cross-module writes must go through kind=service") || !strings.Contains(strings.Join(c.errs, "\n"), "[service-owner-write]") {
		t.Fatalf("missing service-owner-write: %v", c.errs)
	}
}

func TestServiceSchemaRule(t *testing.T) {
	root := t.TempDir()
	metaSrc, err := filepath.Abs("../../../../contracts/_meta")
	if err != nil {
		t.Fatal(err)
	}
	copyTree(t, metaSrc, filepath.Join(root, "contracts/_meta"))
	dir := filepath.Join(root, "contracts/capabilities/services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		body := `id: cap.services.` + name + `
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
schemas:
  Foo: { type: _types.schemas.object, properties: {} }
services: []
`
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &Checker{root: root}
	c.checkCapabilities()
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "schema name Foo already defined") || !strings.Contains(got, "[service-schema]") {
		t.Fatalf("missing service-schema: %s", got)
	}
}

func copyTree(t *testing.T, src, dst string) {
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

func TestEntitySetRules(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.entities.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "contracts/data/entities")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(filepath.Join(root, "contracts/data/entities/_base.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_base.yaml"), base, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		body := `id: data.entities.` + name + `
summary: "实体"
owner_module: account
schema_version: 1
stability: draft
do_not: []
model:
  name: A
  extends: [data.entities.wrong]
  tables: [data.tables.` + name + `]
  includes: []
  included_by: [ { entity: data.entities.x, field: Missing, cardinality: one_to_one } ]
  queries: []
fields: []
`
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &Checker{
		root:   tmp,
		baseID: "data.tables.base",
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.a": {"id": {typ: "integer", primaryKey: true}},
			"data.tables.b": {"id": {typ: "integer", primaryKey: true}},
			"data.tables.c": {"id": {typ: "integer", primaryKey: true}},
		},
		tableOwners: map[string]string{
			"data.tables.a": "account", "data.tables.b": "account", "data.tables.c": "account",
		},
	}
	c.loadEntities(schema)
	c.checkEntities(schema)
	errs := strings.Join(c.errs, "\n")
	for _, rule := range []string{"entity-base-only", "entity-name-unique", "entity-included-by", "entity-table-covered"} {
		if !strings.Contains(errs, "["+rule+"]") {
			t.Fatalf("missing [%s] in:\n%s", rule, errs)
		}
	}
}

func writeStorage(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, "contracts/data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "storage.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStorageEntryFieldsRule(t *testing.T) {
	root := t.TempDir()
	writeStorage(t, root, `id: data.storage
summary: "x"
schema_version: 1
stability: draft
do_not: []
supported_engines: [postgres]
tables:
  postgres:
    - { table: data.tables.x, description: "x", bogus: 1 }
`)
	c := &Checker{root: root, baseID: "data.tables.base", tableDocs: map[string]map[string]any{
		"data.tables.x": {"summary": "x", "columns": []any{}},
	}}
	c.checkStorage(map[string]string{"data.tables.x": "x.yaml"}, nil)
	mustContainRule(t, c.errs, "storage-entry-fields")
}

func TestStorageArchiveFieldsRule(t *testing.T) {
	root := t.TempDir()
	writeStorage(t, root, `id: data.storage
summary: "x"
schema_version: 1
stability: draft
do_not: []
supported_engines: [postgres]
tables:
  postgres:
    - { table: data.tables.x, description: "x" }
    - { archive_of: data.tables.x, promotable: false, bogus: 1 }
`)
	c := &Checker{root: root, baseID: "data.tables.base", tableDocs: map[string]map[string]any{
		"data.tables.x": {"summary": "x", "columns": []any{}},
	}}
	c.checkStorage(map[string]string{"data.tables.x": "x.yaml"}, nil)
	mustContainRule(t, c.errs, "storage-archive-fields")
}

func TestStorageCacheUsedByRule(t *testing.T) {
	c := &Checker{
		queries:        map[string]*queryInfo{"data.queries.x": {ID: "data.queries.x"}},
		cacheEntries:   map[string]storageCacheEntry{"data.tables.x.cache.id": {table: "data.tables.x", key: []string{"id"}}},
		archiveEntries: map[string]storageArchiveEntry{},
	}
	c.checkQueryStorageRefs()
	mustContainRule(t, c.errs, "storage-cache-used-by")
}

func TestStorageTTLBoundRule(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"id": {typ: "integer"}}},
	}
	entries := map[string]*storageEntry{
		"data.tables.x": {table: "data.tables.x", retention: &storageRetention{unit: "minutes", ttl: 30}},
	}
	c.checkStorageCaches(map[string]any{"caches": map[string]any{"redis": []any{
		map[string]any{
			"prefix":   "x",
			"cache_of": "data.tables.x", "key": []any{"col.id"},
			"retention": true, "unit": "minutes", "cache_ttl": 60,
		},
	}}}, entries, "contracts/data/storage.yaml")
	mustContainRule(t, c.errs, "storage-ttl-bound")
}

func TestStorageCachePrefixRule(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"id": {typ: "integer"}}}}
	entries := map[string]*storageEntry{"data.tables.x": {table: "data.tables.x", engine: "mysql"}}
	c.checkStorageCaches(map[string]any{"caches": map[string]any{"redis": []any{
		map[string]any{"cache_of": "data.tables.x", "key": []any{"id"}, "retention": true, "unit": "days", "cache_ttl": 30},
	}}}, entries, "w")
	mustContainRule(t, c.errs, "storage-cache-prefix")
}

func TestEnumRules(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.enums.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "contracts/data/enums")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `id: data.enums.wrong
summary: "枚举"
schema_version: 1
stability: draft
do_not: []
enums:
  - name: x
    go_type: X
    ts_type: X
    values:
      - { code: 0, value: a }
      - { code: 0, value: a }
    used_by: { entities: [], capabilities: [] }
`
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: tmp}
	c.checkEnums(schema)
	errs := strings.Join(c.errs, "\n")
	for _, rule := range []string{"enum-id", "enum-values-unique"} {
		if !strings.Contains(errs, "["+rule+"]") {
			t.Fatalf("missing [%s] in:\n%s", rule, errs)
		}
	}
}

func TestAsyncStructureRule(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/cap.events.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "contracts/capabilities/events")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `id: cap.events.a
summary: "事件"
owner_module: a
schema_version: 1
stability: draft
do_not: []
crons:
  - id: cap.events.a.job
    name: job
    summary: "x"
    service: cap.services.a.get
    schedule: "* * * * *"
  - id: cap.events.a.job2
    name: job
    summary: "x"
    service: cap.services.a.other
    schedule: "* * * * *"
`
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: tmp}
	c.checkEventsDoc(schema, map[string]serviceMeta{
		"cap.services.a.get":   {module: "a"},
		"cap.services.a.other": {module: "a"},
	}, map[string]bool{})
	mustContainRule(t, c.errs, "async-structure")
}

func TestPhysicalNameUniqueRule(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(filepath.Join(root, "contracts/data/tables/_base.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_base.yaml"), base, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		body := `id: data.tables.` + name + `
summary: "表"
owner_module: account
schema_version: 1
stability: draft
do_not: []
extends: [data.tables.base]
table:
  name: dup
  queries: []
  entities: []
  referenced_by: []
columns:
  - { name: id, type: data.tables.base.col.id }
indexes: []
constraints: []
`
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &Checker{root: tmp, baseID: "data.tables.base"}
	c.checkTables(schema)
	mustContainRule(t, c.errs, "physical-name-unique")
}

func TestTableQueriesGeneratedRule(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `id: data.tables.x
summary: "表"
owner_module: account
schema_version: 1
stability: draft
do_not: []
table:
  name: x
  queries: []
  entities: []
  referenced_by: []
columns: []
indexes: []
constraints: []
`
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{
		root:       tmp,
		baseID:     "data.tables.base",
		queries:    map[string]*queryInfo{"data.queries.x.q": {ID: "data.queries.x.q", Targets: []string{"data.tables.x"}}},
		queryOrder: []string{"data.queries.x.q"},
	}
	c.checkUsedBy(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	mustContainRule(t, c.errs, "table-queries-generated")
}

func TestQueryIndexUsedByRule(t *testing.T) {
	c := &Checker{
		tableIndexes: map[string][]tableIndex{
			"data.tables.x": {{name: "idx_x", usedBy: map[string]bool{"data.queries.x.q": true}}},
		},
		queries:     map[string]*queryInfo{"data.queries.x.q": {ID: "data.queries.x.q", Targets: []string{"data.tables.x"}}},
		queryTables: map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true}},
	}
	c.checkQueryIndexRefs()
	mustContainRule(t, c.errs, "query-index-used-by")
}

func TestQueryArchiveRules(t *testing.T) {
	c := &Checker{
		queryOrder: []string{"data.queries.x"},
		queries: map[string]*queryInfo{"data.queries.x": {
			ID: "data.queries.x", File: "x.yaml", Targets: []string{"data.tables.x"},
			Archive: []queryArchiveRef{{Archive: "data.tables.x.archive", AgeBy: "wrong"}},
		}},
		archiveEntries: map[string]storageArchiveEntry{
			"data.tables.x": {source: "data.tables.x", promotable: true, ageBy: "created_at"},
			"data.tables.y": {source: "data.tables.y", promotable: true, ageBy: "created_at"},
		},
	}
	c.checkQueryStorageRefs()
	mustContainRule(t, c.errs, "query-archive-ref")
	mustContainRule(t, c.errs, "query-archive-used-by")
}
