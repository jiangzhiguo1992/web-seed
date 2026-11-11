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
		case "table":
			for j := range s.Fields[i].Fields {
				if s.Fields[i].Fields[j].Name == "size_class" {
					s.Fields[i].Fields[j].Default = "large"
				}
			}
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
	table, _ := doc["table"].(map[string]any)
	if table["size_class"] != "large" {
		t.Fatalf("size_class not from schema: %#v", table)
	}
	cols, _ := doc["columns"].([]any)
	if len(cols) != 2 {
		t.Fatalf("want 2 columns, got %d", len(cols))
	}
	first, _ := cols[0].(map[string]any)
	if first["name"] != "custom_name" {
		t.Fatalf("column name not from schema examples: %#v", first)
	}
	if !strings.Contains(string(b), "data.enums.note_status") {
		t.Fatalf("status enum not generated:\n%s", b)
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

func TestRenderTableRejectsUnsupportedRequired(t *testing.T) {
	s := loadTestSchema(t)
	s.Fields = append(s.Fields, Field{Name: "new_required", Type: "string", Required: true})
	if _, err := RenderTable(s, "note", "notes", "备注表"); err == nil {
		t.Fatal("expected unsupported required field to fail")
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
