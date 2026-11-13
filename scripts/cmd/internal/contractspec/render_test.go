package contractspec

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func loadTestSchema(t *testing.T) *Schema {
	t.Helper()
	path, err := filepath.Abs("../../../../contracts/_meta/data.tables.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRenderTableUsesSchemaDefaults(t *testing.T) {
	s := loadTestSchema(t)
	if len(s.ColumnRefs.Prefixes) == 0 {
		t.Fatal("column_refs.prefixes not loaded")
	}
	for i := range s.Fields {
		switch s.Fields[i].Name {
		case "stability":
			s.Fields[i].Default = "stable"
		case "columns":
			for j := range s.Fields[i].ItemFields {
				if s.Fields[i].ItemFields[j].Name == "name" {
					s.Fields[i].ItemFields[j].Examples = []any{"custom_name"}
				}
			}
		}
	}

	b, err := RenderTable(s, "note", "notes", "备注表")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.tables.note" || doc["stability"] != "stable" {
		t.Fatalf("unexpected top-level: %#v", doc)
	}
	if _, ok := doc["extends"]; ok {
		t.Fatalf("extends must live under table: %#v", doc)
	}
	table, _ := doc["table"].(map[string]any)
	extends, _ := table["extends"].([]any)
	if len(extends) != 1 || extends[0] != "data.tables.base" {
		t.Fatalf("table.extends must be hardcoded to [data.tables.base]: %#v", table["extends"])
	}
	cols, _ := doc["columns"].([]any)
	if len(cols) != 1 {
		t.Fatalf("want 1 column, got %d", len(cols))
	}
	first, _ := cols[0].(map[string]any)
	if first["name"] != "custom_name" {
		t.Fatalf("column name not from schema examples: %#v", first)
	}
	if strings.Contains(string(b), "data.enums.") {
		t.Fatalf("table scaffold must not reference enums:\n%s", b)
	}
	if !strings.Contains(string(b), `comment: "TODO: 表说明"`) {
		t.Fatalf("scaffold prose must be double-quoted:\n%s", b)
	}
}

func loadSchemaByType(t *testing.T, typ string) *Schema {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := LoadSchemaByType(root, typ)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRenderEntity(t *testing.T) {
	s := loadSchemaByType(t, "data.entities")
	b, err := RenderEntity(s, "account_preview", "account", "accounts", "账号预览")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.entities.account_preview" || doc["owner_module"] != "accounts" {
		t.Fatalf("unexpected top-level: %#v", doc)
	}
	model, _ := doc["model"].(map[string]any)
	if model["name"] != "accountPreview" {
		t.Fatalf("model.name not scaffolded: %#v", model)
	}
	tables, _ := model["tables"].([]any)
	if len(tables) != 1 || tables[0] != "data.tables.account" {
		t.Fatalf("model.tables not scaffolded: %#v", model["tables"])
	}
	if _, ok := doc["table"]; ok {
		t.Fatalf("single table field must not be generated: %#v", doc)
	}
	if _, ok := doc["identity"]; ok {
		t.Fatalf("identity must not be generated: %#v", doc)
	}
	extends, _ := model["extends"].([]any)
	if len(extends) != 1 || extends[0] != "data.entities.base" {
		t.Fatalf("model.extends must be [data.entities.base]: %#v", model["extends"])
	}
	fields, _ := doc["fields"].([]any)
	if len(fields) != 0 {
		t.Fatalf("entity scaffold must not repeat base fields: %#v", fields)
	}
	usedBy, _ := doc["used_by"].(map[string]any)
	if _, ok := usedBy["queries"]; !ok {
		t.Fatalf("used_by.queries missing: %#v", usedBy)
	}
}

func TestRenderQuery(t *testing.T) {
	s := loadSchemaByType(t, "data.queries")
	b, err := RenderQuery(s, "account", "accounts", "account", "accounts", "账号查询")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.queries.account" || doc["owner_module"] != "accounts" {
		t.Fatalf("unexpected top-level: %#v", doc)
	}
	queries, _ := doc["queries"].([]any)
	if len(queries) != 1 {
		t.Fatalf("want 1 query, got %d", len(queries))
	}
	q, _ := queries[0].(map[string]any)
	if q["id"] != "data.queries.account.exists_account" || q["name"] != "ExistsAccount" {
		t.Fatalf("query id/name not scaffolded: %#v", q)
	}
	if q["returns"] != "boolean" {
		t.Fatalf("query returns not scaffolded: %#v", q)
	}
	targets, _ := q["targets"].([]any)
	if len(targets) != 1 || targets[0] != "data.tables.account" {
		t.Fatalf("targets not scaffolded: %#v", q["targets"])
	}
	if q["statement"] != "select" || q["expr"] != "COUNT(*) > 0" {
		t.Fatalf("statement/expr not scaffolded: %#v", q)
	}
	refs, _ := q["index_ref"].([]any)
	if len(refs) != 1 || refs[0] != "data.tables.account.pk" {
		t.Fatalf("index_ref not scaffolded: %#v", q["index_ref"])
	}
	if _, ok := q["sql"]; ok {
		t.Fatalf("query must not scaffold sql: %#v", q)
	}
	if !strings.Contains(string(b), `semantics: "TODO: 查询语义"`) {
		t.Fatalf("semantics must be double-quoted:\n%s", b)
	}
	if _, err := RenderQuery(s, "note", "notes", "note", "bad name", "x"); err == nil {
		t.Fatal("invalid physical table name should fail")
	}
}

func TestRenderTableDecimalFamilyParams(t *testing.T) {
	s := loadTestSchema(t)
	b, err := RenderTableWithColumnType(s, "note", "notes", "备注表", "decimal")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	cols, _ := doc["columns"].([]any)
	first, _ := cols[0].(map[string]any)
	if first["type"] != "decimal" {
		t.Fatalf("column type not overridden: %#v", first)
	}
	if first["precision"] != 19 {
		t.Fatalf("precision not scaffolded: %#v", first)
	}
	if first["scale"] != 0 {
		t.Fatalf("scale not scaffolded: %#v", first)
	}
}

func TestRenderTableScaffoldRequiresParamValue(t *testing.T) {
	s := loadTestSchema(t)
	s.ColumnTypes.ValueTypeFamilies["weird"] = ValueTypeFamily{Params: map[string]Field{
		"missing": {Type: "integer", Required: true},
	}}
	if _, err := RenderTableWithColumnType(s, "note", "notes", "备注表", "weird"); err == nil {
		t.Fatal("expected required param without default/examples to fail")
	}
}

func TestRenderTableValidatesInput(t *testing.T) {
	s := loadTestSchema(t)
	if _, err := RenderTable(s, "Bad-Name", "notes", "备注表"); err == nil {
		t.Fatal("expected invalid name to fail")
	}
	if _, err := RenderTable(s, "note", "Bad-Module", "备注表"); err == nil {
		t.Fatal("expected invalid module to fail")
	}
	if _, err := RenderTable(s, "note", "notes", "  "); err == nil {
		t.Fatal("expected empty summary to fail")
	}
}

func TestRenderTableRejectsUnsupportedRequired(t *testing.T) {
	s := loadTestSchema(t)
	s.Fields = append(s.Fields, Field{Name: "new_required", Type: "string", Required: true})
	if _, err := RenderTable(s, "note", "notes", "备注表"); err == nil {
		t.Fatal("expected unsupported required field to fail")
	}
}

func TestRenderTableOmitsInheritedStatus(t *testing.T) {
	s := loadTestSchema(t)
	b, err := RenderTable(s, "note", "notes", "备注表")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	cols, _ := doc["columns"].([]any)
	for _, raw := range cols {
		col, _ := raw.(map[string]any)
		if col["name"] == "status" {
			t.Fatalf("status is inherited from base and must not be scaffolded: %#v", col)
		}
	}
}

func TestRenderEnumItemRejectsInvalidName(t *testing.T) {
	enumSchema := &Schema{Type: "data.enums"}
	if _, err := RenderEnumItem(enumSchema, "Bad-Name"); err == nil {
		t.Fatal("expected invalid enum name to fail")
	}
}

func TestRenderEnumItem(t *testing.T) {
	path, err := filepath.Abs("../../../../contracts/_meta/data.enums.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderEnumItem(s, "note_status")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.enums.note_status" || doc["go_type"] != "NoteStatus" {
		t.Fatalf("unexpected enum item: %#v", doc)
	}
}

func TestRenderEnumItemUsesTargetsID(t *testing.T) {
	s := &Schema{Type: "data.enums", Targets: Targets{ID: "data.enums", File: "contracts/data/enums.yaml"}}
	b, err := RenderEnumItem(s, "note_status")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.enums.note_status" {
		t.Fatalf("id must derive from targets.id: %#v", doc)
	}
}

func TestRenderEnumItemRejectsUnsupportedUsedByField(t *testing.T) {
	path, err := filepath.Abs("../../../../contracts/_meta/data.enums.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range s.Fields {
		if s.Fields[i].Name != "enums" {
			continue
		}
		for j := range s.Fields[i].ItemFields {
			if s.Fields[i].ItemFields[j].Name == "used_by" {
				s.Fields[i].ItemFields[j].Fields = append(s.Fields[i].ItemFields[j].Fields, Field{
					Name: "capabilities", Type: "list<string>", Required: true,
				})
			}
		}
	}
	if _, err := RenderEnumItem(s, "note_status"); err == nil {
		t.Fatal("expected unsupported required used_by child to fail")
	}
}

func TestRenderEnumItemRequiresTargets(t *testing.T) {
	if _, err := RenderEnumItem(&Schema{Type: "data.enums"}, "note_status"); err == nil {
		t.Fatal("expected missing targets to fail")
	}
}
