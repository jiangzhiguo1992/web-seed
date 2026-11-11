package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIsColumnType(t *testing.T) {
	ct := ColumnTypes{
		ValueTypeFamilies: map[string]ValueTypeFamily{
			"string": {}, "timestamp": {}, "integer": {}, "datetime": {}, "decimal": {}, "float": {},
		},
	}
	refPrefixes := []string{"data.enums.", "data.tables."}
	cases := []struct {
		in   string
		want bool
	}{
		{"string", true},
		{"uuid", false},
		{"varchar", false},
		{"timestamp", true},
		{"datetime", true},
		{"decimal", true},
		{"float", true},
		{"date", false},
		{"time", false},
		{"text", false},
		{"timestamptz", false},
		{"integer", true},
		{"int", false},
		{"int32", false},
		{"double", false},
		{"data.enums.account_status", true},
		{"data.enums.", false},
		{"data.tables.account.col.id", true},
		{"data.tables.account", false},
		{"data.tables.account.col.", false},
	}
	for _, tc := range cases {
		if got := isColumnType(tc.in, ct, refPrefixes); got != tc.want {
			t.Errorf("isColumnType(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCommonColumnFieldsExcludesFamilyParams(t *testing.T) {
	schema := &Schema{
		Fields: []Field{{
			Name: "columns", Type: "list<object>", ItemFields: []Field{
				{Name: "name", Type: "string"},
				{Name: "type", Type: "string"},
				{Name: "min_len", Type: "integer"},
				{Name: "comment", Type: "string"},
			},
		}},
		ColumnTypes: ColumnTypes{ValueTypeFamilies: map[string]ValueTypeFamily{
			"string": {Params: map[string]Field{"min_len": {Type: "integer"}}},
		}},
	}
	common := commonColumnFields(schema)
	if !common["name"] || !common["type"] || !common["comment"] {
		t.Fatalf("common fields missing: %v", common)
	}
	if common["min_len"] {
		t.Fatalf("family param leaked into common fields: %v", common)
	}
}

func TestFamilyParams(t *testing.T) {
	schema := &Schema{
		Fields: []Field{{
			Name: "columns", Type: "list<object>", ItemFields: []Field{
				{Name: "name", Type: "string"},
				{Name: "type", Type: "string"},
				{Name: "nullable", Type: "boolean"},
			},
		}},
		ColumnTypes: ColumnTypes{ValueTypeFamilies: map[string]ValueTypeFamily{
			"decimal": {Params: map[string]Field{
				"precision": {Type: "integer", Required: true, Min: 1},
				"scale":     {Type: "integer", Min: 0},
			}},
			"string": {Params: map[string]Field{
				"min_len": {Type: "integer", Min: 0},
				"max_len": {Type: "integer", Min: 0},
			}},
		}}}

	c := &Checker{}
	c.checkFamilyParams(map[string]any{"name": "amount", "type": "decimal", "scale": 2}, "decimal", "x", 0, "amount", schema)
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `missing required param "precision"`) {
		t.Fatalf("missing required family param not reported: %s", errs)
	}

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"name": "amount", "type": "decimal", "precision": 19, "scale": 4}, "decimal", "x", 0, "amount", schema)
	if len(c.errs) != 0 {
		t.Fatalf("valid decimal params reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"name": "amount", "type": "decimal", "precision": 19, "scale": 4, "min_len": 1}, "decimal", "x", 0, "amount", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), `param "min_len" not allowed for type decimal`) {
		t.Fatalf("family-mismatched param not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"name": "nickname", "type": "string", "min_len": 10, "max_len": 2}, "string", "x", 0, "nickname", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "min_len > max_len") {
		t.Fatalf("min_len > max_len not reported: %v", c.errs)
	}
}

func TestValidateDocRequiredAndUnknown(t *testing.T) {
	c := &Checker{}
	fields := []Field{{Name: "id", Type: "string", Required: true}}
	c.validateDoc(map[string]any{"extra": 1}, fields, "x")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `missing required field "id"`) {
		t.Fatalf("missing required field not reported: %s", errs)
	}
	if !strings.Contains(errs, `unknown field "extra"`) {
		t.Fatalf("unknown field not reported: %s", errs)
	}
}

func TestTableRulesRejectUnknownColumn(t *testing.T) {
	schema := &Schema{
		ColumnTypes: ColumnTypes{
			ValueTypeFamilies: map[string]ValueTypeFamily{
				"string": {Params: map[string]Field{
					"min_len": {Type: "integer", Min: 0},
					"max_len": {Type: "integer", Min: 0},
				}},
			},
		},
		ColumnRefs: ColumnRefs{Prefixes: []string{"data.enums."}},
	}
	doc := map[string]any{
		"id":           "data.tables.x",
		"extends":      []any{"data.tables.base"},
		"owner_module": "x",
		"table":        map[string]any{"comment": "x", "size_class": "small"},
		"columns": []any{
			map[string]any{"name": "status", "type": "data.enums.x_status"},
			map[string]any{"name": "name", "type": "string"},
		},
		"constraints": []any{
			map[string]any{
				"name":       "fk_x_other",
				"type":       "foreign_key",
				"columns":    []any{"missing"},
				"references": "data.tables.other.col.id",
			},
		},
		"indexes": []any{
			map[string]any{
				"name":    "idx_x_missing",
				"columns": []any{"missing"},
				"reason":  "test",
			},
		},
	}
	c := &Checker{}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", schema)
	errs := strings.Join(c.errs, "\n")
	if strings.Count(errs, `unknown column "missing"`) != 2 {
		t.Fatalf("expected two unknown-column errors, got:\n%s", errs)
	}
}

func TestCheckColumnRef(t *testing.T) {
	c := &Checker{}
	tables := map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"}
	cols := map[string]map[string]bool{"data.tables.x": {"id": true}}
	checkColumnRef(c, "data.tables.x.col.id", tables, cols, "ok")
	if len(c.errs) != 0 {
		t.Fatalf("valid ref reported error: %v", c.errs)
	}
	checkColumnRef(c, "data.tables.x.col.missing", tables, cols, "bad")
	if len(c.errs) != 1 || !strings.Contains(c.errs[0], "unknown column") {
		t.Fatalf("invalid ref not reported: %v", c.errs)
	}
}

func TestBaseRequiresSummary(t *testing.T) {
	schema := &Schema{Fields: []Field{
		{Name: "schema_version", Type: "integer", Required: true, Const: 1},
		{Name: "id", Type: "string", Required: true},
		{Name: "kind", Type: "string", Enum: []any{"base"}},
		{Name: "summary", Type: "string", Required: true},
		{Name: "stability", Type: "string", Required: true, Enum: []any{"stable"}},
		{Name: "do_not", Type: "list<string>"},
		{Name: "columns", Type: "list<object>", Required: true, ItemFields: []Field{{Name: "name", Type: "string", Required: true}}},
	}}
	doc := map[string]any{
		"schema_version": 1,
		"id":             "data.tables.base",
		"kind":           "base",
		"stability":      "stable",
		"columns": []any{
			map[string]any{"name": "id"},
			map[string]any{"name": "status"},
			map[string]any{"name": "created_at"},
			map[string]any{"name": "updated_at"},
			map[string]any{"name": "deleted_at"},
		},
	}
	c := &Checker{}
	c.checkBaseTable(doc, schema, "contracts/data/tables/_base.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), `missing required field "summary"`) {
		t.Fatalf("base missing summary not reported: %v", c.errs)
	}
}

func TestRepoContracts(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.run()
	if len(c.errs) > 0 {
		t.Fatalf("repository contracts failed:\n%s", strings.Join(c.errs, "\n"))
	}
}
