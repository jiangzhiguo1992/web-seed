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
	if !strings.Contains(string(b), "  extends:\n    - data.tables.base\n") {
		t.Fatalf("table.extends must be block style:\n%s", b)
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
	b, err := RenderEntity(s, "account_preview", "account", "account", "账号预览")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.entities.account_preview" || doc["owner_module"] != "account" {
		t.Fatalf("unexpected top-level: %#v", doc)
	}
	if _, ok := doc["do_not"]; !ok {
		t.Fatalf("do_not is required and must be scaffolded: %#v", doc)
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
	if !strings.Contains(string(b), "  extends:\n    - data.entities.base\n") {
		t.Fatalf("model.extends must be block style:\n%s", b)
	}
	fields, _ := doc["fields"].([]any)
	if len(fields) != 0 {
		t.Fatalf("entity scaffold must not repeat base fields: %#v", fields)
	}
	usedBy, _ := doc["used_by"].(map[string]any)
	if _, ok := usedBy["queries"]; !ok {
		t.Fatalf("used_by.queries missing: %#v", usedBy)
	}
	if _, ok := usedBy["capabilities"]; !ok {
		t.Fatalf("used_by.capabilities missing: %#v", usedBy)
	}
}

func TestRenderQuery(t *testing.T) {
	s := loadSchemaByType(t, "data.queries")
	b, err := RenderQuery(s, "account", "account", "account", "account", "账号查询")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.queries.account" || doc["owner_module"] != "account" {
		t.Fatalf("unexpected top-level: %#v", doc)
	}
	if _, ok := doc["do_not"]; !ok {
		t.Fatalf("do_not is required and must be scaffolded: %#v", doc)
	}
	if strings.Index(string(b), "id: data.queries.account") > strings.Index(string(b), "schema_version") {
		t.Fatalf("query scaffold must follow template order (id before schema_version):\n%s", b)
	}
	queries, _ := doc["queries"].([]any)
	if len(queries) != 1 {
		t.Fatalf("want 1 query, got %d", len(queries))
	}
	q, _ := queries[0].(map[string]any)
	if q["name"] != "ExistsAccount" {
		t.Fatalf("query name not scaffolded: %#v", q)
	}
	if _, ok := q["id"]; ok {
		t.Fatalf("query item id must not be scaffolded: %#v", q)
	}
	returns, _ := q["returns"].([]any)
	if len(returns) != 1 {
		t.Fatalf("query returns not scaffolded: %#v", q)
	}
	params, _ := q["params"].([]any)
	if len(params) != 1 {
		t.Fatalf("query params not scaffolded: %#v", q)
	}
	param, _ := params[0].(map[string]any)
	if param["type"] != "integer" || param["column"] != "data.tables.account.col.id" {
		t.Fatalf("query id param must bind the integer primary key: %#v", param)
	}
	ret, _ := returns[0].(map[string]any)
	if ret["kind"] != "scalar" || ret["type"] != "boolean" {
		t.Fatalf("query return shape not scaffolded: %#v", ret)
	}
	targets, _ := q["tables"].([]any)
	if len(targets) != 1 || targets[0] != "data.tables.account" {
		t.Fatalf("tables not scaffolded: %#v", q["tables"])
	}
	if _, ok := q["access"]; ok {
		t.Fatalf("access must be omitted for read scaffold: %#v", q)
	}
	if _, ok := q["kind"]; ok {
		t.Fatalf("query kind must not be scaffolded: %#v", q)
	}
	refs, _ := q["index_ref"].([]any)
	if len(refs) != 1 || refs[0] != "data.tables.account.pk" {
		t.Fatalf("index_ref not scaffolded: %#v", q["index_ref"])
	}
	if _, ok := q["sql"]; ok {
		t.Fatalf("query must not scaffold sql: %#v", q)
	}
	if _, err := RenderQuery(s, "note", "notes", "note", "bad name", "x"); err == nil {
		t.Fatal("invalid physical table name should fail")
	}
}

func TestRenderEntityRejectsUnsupportedModelRequired(t *testing.T) {
	s := loadSchemaByType(t, "data.entities")
	for i := range s.Fields {
		if s.Fields[i].Name == "model" {
			s.Fields[i].Fields = append(s.Fields[i].Fields, Field{Name: "new_required", Type: "string", Required: true})
		}
	}
	if _, err := RenderEntity(s, "note", "note", "notes", "备注实体"); err == nil || !strings.Contains(err.Error(), "new_required") {
		t.Fatalf("expected unsupported required model field to fail with field name, got %v", err)
	}
}

func TestRenderQueryRejectsUnsupportedReturnRequired(t *testing.T) {
	s := loadSchemaByType(t, "data.queries")
	for i := range s.Fields {
		if s.Fields[i].Name != "queries" {
			continue
		}
		for j := range s.Fields[i].ItemFields {
			if s.Fields[i].ItemFields[j].Name == "returns" {
				s.Fields[i].ItemFields[j].ItemFields = append(s.Fields[i].ItemFields[j].ItemFields, Field{Name: "new_required", Type: "string", Required: true})
			}
		}
	}
	if _, err := RenderQuery(s, "note", "notes", "note", "note", "备注查询"); err == nil || !strings.Contains(err.Error(), "new_required") {
		t.Fatalf("expected unsupported required return field to fail with field name, got %v", err)
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

func TestRenderTableFallbacks(t *testing.T) {
	s := loadSchemaByType(t, "data.tables")
	b, err := RenderTable(s, "note", "notes", "备注表")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["stability"] != "draft" {
		t.Fatalf("stability fallback must be draft: %#v", doc["stability"])
	}
	cols, _ := doc["columns"].([]any)
	if len(cols) != 1 {
		t.Fatalf("want one column: %#v", doc["columns"])
	}
	first, _ := cols[0].(map[string]any)
	if first["type"] != "string" {
		t.Fatalf("column type fallback must be string: %#v", first)
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
	if doc["id"] != nil || doc["name"] != "note_status" || doc["go_type"] != "NoteStatus" {
		t.Fatalf("enum item must not carry id and must keep name: %#v", doc)
	}
}

func TestRenderEnumItemUsesNameAsIDSource(t *testing.T) {
	s := &Schema{Type: "data.enums", Targets: Targets{ID: "data.enums", File: "contracts/data/enums.yaml"}}
	b, err := RenderEnumItem(s, "note_status")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != nil || doc["name"] != "note_status" {
		t.Fatalf("item id is derived from name and must not be rendered: %#v", doc)
	}
	usedBy, ok := doc["used_by"].(map[string]any)
	if !ok || usedBy["capabilities"] == nil {
		t.Fatalf("used_by.capabilities must be scaffolded: %#v", doc)
	}
	if strings.Index(string(b), "values:") > strings.Index(string(b), "used_by:") {
		t.Fatalf("scaffold must follow template order (values before used_by):\n%s", b)
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
					Name: "tables", Type: "list<string>", Required: true,
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
