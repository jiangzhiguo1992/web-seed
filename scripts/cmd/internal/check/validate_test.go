package check

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func TestGeneratedFieldMarkers(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		ok    bool
	}{
		{"plain", Field{Name: "plain"}, true},
		{"generated with forbidden", Field{Name: "gen", Generated: true, Write: "forbidden"}, true},
		{"generated without write", Field{Name: "gen", Generated: true}, false},
		{"write without generated", Field{Name: "gen", Write: "forbidden"}, false},
		{"write with unknown value", Field{Name: "gen", Generated: true, Write: "manual"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFieldMarkers([]Field{tc.field})
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestStringLengthBounds(t *testing.T) {
	c := &Checker{}
	c.validateValue("ab", Field{Type: "_types.basics.string", MinLen: 3}, "x")
	c.validateValue("abcd", Field{Type: "_types.basics.string", MaxLen: 3}, "x")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[schema.len]") {
		t.Fatalf("string bounds not enforced: %v", c.errs)
	}
}

func TestLengthKeysRequireLengthType(t *testing.T) {
	if err := validateFieldTypes([]Field{{Name: "n", Type: "_types.basics.integer", MinLen: 1}}, ValueTypes{}, false); err == nil {
		t.Fatal("min_len on integer must be rejected")
	}
	if err := validateFieldTypes([]Field{{Name: "n", Type: "_types.basics.string", MaxLen: "x"}}, ValueTypes{}, false); err == nil {
		t.Fatal("non-numeric max_len must be rejected")
	}
	if err := validateFieldTypes([]Field{{Name: "s", Type: "_types.basics.string", Len: 3}}, ValueTypes{}, false); err == nil {
		t.Fatal("len on string must be rejected")
	}
	if err := validateFieldTypes([]Field{{Name: "a", Type: "_types.structures.array<_types.basics.string>", Len: 3, MinItems: 1}}, ValueTypes{}, false); err == nil {
		t.Fatal("min_items on array must be rejected")
	}
	if err := validateFieldTypes([]Field{{Name: "n", Type: "_types.basics.integer", Pattern: "^x$"}}, ValueTypes{}, false); err == nil {
		t.Fatal("pattern on integer must be rejected")
	}
}

func TestTableColumnNullableRequired(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	columns, ok := contractspec.FindField(schema.Fields, "columns")
	if !ok {
		t.Fatal("columns field missing in data.tables meta")
	}
	c := &Checker{}
	c.validateValue([]any{map[string]any{
		"name": "x", "type": "_types.basics.integer",
	}}, columns, "columns")
	if !strings.Contains(entityErrs(c), `missing required field "nullable"`) {
		t.Fatalf("missing column nullable not reported: %v", c.errs)
	}
}

func TestSchemaAliasType(t *testing.T) {
	vt := testValueTypes()
	vt.Schemas = map[string]contractspec.SchemaKind{"object": {}, "array": {}, "scalar": {}, "ref": {}}
	if err := validateFieldTypes([]Field{{Name: "x", Types: []string{contractspec.SchemaAlias}}}, vt, false); err != nil {
		t.Fatalf("schema alias rejected: %v", err)
	}
	if err := validateFieldTypes([]Field{{Name: "x", Types: []string{"bogus"}}}, vt, false); err == nil {
		t.Fatal("unknown schema kind must be rejected")
	}
}

func TestTypesRejectsDeadBounds(t *testing.T) {
	for _, f := range []Field{
		{Name: "x", Types: []string{"object"}, MinLen: 1},
		{Name: "x", Types: []string{"object"}, MaxItems: 1},
		{Name: "x", Types: []string{"object"}, Len: 1},
	} {
		if err := validateFieldTypes([]Field{f}, testValueTypes(), false); err == nil || !strings.Contains(err.Error(), "types only accepts") {
			t.Fatalf("dead bound on types field must be rejected: %+v", f)
		}
	}
}

func TestDefaultMustBeEnumValue(t *testing.T) {
	f := Field{Name: "x", Type: "_types.basics.string", Enum: []any{"stable", "draft"}, Default: "draf"}
	if err := validateFieldTypes([]Field{f}, ValueTypes{}, false); err == nil || !strings.Contains(err.Error(), "not in enum") {
		t.Fatalf("default outside enum must be rejected: %v", err)
	}
	f.Default = "draft"
	if err := validateFieldTypes([]Field{f}, ValueTypes{}, false); err != nil {
		t.Fatalf("default inside enum rejected: %v", err)
	}
}

func TestTypeRefNamespaceRequired(t *testing.T) {
	vt := testValueTypes()
	cases := []struct {
		typ string
		ok  bool
	}{
		{"_types.basics.string", true},
		{"_types.basics.integer", true},
		{"_types.basics.integer.32", true},
		{"_types.basics.integer.99", false},
		{"_types.basics.string.default", false},
		{"_types.structures.object", true},
		{"_types.structures.list<_types.basics.string>", true},
		{"_types.structures.map<_types.basics.integer, _types.structures.object>", true},
		{"_types.string", false},
		{"_types.object", false},
		{"object", false},
		{"_types.basics.list", false},
		{"_types.structures.string", false},
		{"_types.schemas.string", false},
	}
	for _, tc := range cases {
		_, err := parseTypeExpr(tc.typ, vt)
		if tc.ok && err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.typ, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("%s: expected namespace error", tc.typ)
		}
	}
}

func TestConcreteVariantRequiredEverywhere(t *testing.T) {
	c := &Checker{valueTypes: testValueTypes()}
	for _, path := range []string{
		"contracts/data/tables/x.yaml.columns[0].type",
		"contracts/_meta/data.tables.yaml.fields[0].type",
	} {
		c.errs = nil
		c.checkTypeRef("_types.basics.integer", path)
		if !strings.Contains(strings.Join(c.errs, "\n"), "schema.type-concrete") {
			t.Fatalf("%s: family without variant accepted: %v", path, c.errs)
		}
	}
	c.errs = nil
	c.checkTypeRef("_types.basics.string", "contracts/_meta/data.tables.yaml.fields[0].type")
	if len(c.errs) != 0 {
		t.Fatalf("single-variant family rejected: %v", c.errs)
	}
	c.errs = nil
	c.checkTypeRef("_types.basics.string.string", "contracts/data/tables/x.yaml.columns[0].type")
	if !strings.Contains(strings.Join(c.errs, "\n"), "single-variant family") {
		t.Fatalf("explicit single-variant suffix accepted: %v", c.errs)
	}
	c.errs = nil
	c.checkTypeRef("_types.basics.integer.32", "contracts/data/tables/x.yaml.columns[0].type")
	if len(c.errs) != 0 {
		t.Fatalf("concrete variant rejected: %v", c.errs)
	}
}

func TestMetaFieldValueConstraints(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		ok    bool
	}{
		{"integer default string", Field{Name: "n", Type: "_types.basics.integer.32", Default: "x"}, false},
		{"integer default int", Field{Name: "n", Type: "_types.basics.integer.32", Default: 1}, true},
		{"string enum int", Field{Name: "s", Type: "_types.basics.string", Enum: []any{1}}, false},
		{"list string enum", Field{Name: "s", Type: "_types.structures.list<_types.basics.string>", Enum: []any{"a"}}, true},
		{"boolean default string", Field{Name: "b", Type: "_types.basics.boolean", Default: "true"}, false},
		{"const default conflict", Field{Name: "n", Type: "_types.basics.integer.32", Const: 1, Default: 2}, false},
		{"const outside enum", Field{Name: "s", Type: "_types.basics.string", Const: "x", Enum: []any{"a"}}, false},
		{"example wrong type", Field{Name: "s", Type: "_types.basics.string", Examples: []any{1}}, false},
		{"unique on string", Field{Name: "s", Type: "_types.basics.string", Unique: true}, false},
		{"unique object list without key", Field{
			Name: "xs", Type: "_types.structures.list<_types.structures.object>", Unique: true,
			ItemFields: []Field{{Name: "name", Type: "_types.basics.string"}},
		}, false},
		{"fields on string", Field{Name: "s", Type: "_types.basics.string", Fields: []Field{{Name: "x", Type: "_types.basics.string"}}}, false},
		{"integer min float", Field{Name: "n", Type: "_types.basics.integer.32", Min: 1.5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFieldTypes([]Field{tc.field}, ValueTypes{}, false)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSchemaMapKeyPatternsAnchored(t *testing.T) {
	c := &Checker{}
	c.validateMapNode(map[string]any{"key_patterns": []any{"^[a-z]+"}}, "m")
	if !strings.Contains(strings.Join(c.errs, "\n"), "must be anchored") {
		t.Fatalf("unanchored key_patterns accepted: %v", c.errs)
	}
}

func TestServiceStepFieldSubsetDrift(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	schema, _, err := contractspec.LoadSchemaByType(root, "cap.services")
	if err != nil {
		t.Fatal(err)
	}
	si := fieldIndex(schema.Fields, "services")
	if si < 0 {
		t.Fatal("services field missing")
	}
	ji := fieldIndex(schema.Fields[si].ItemFields, "steps")
	if ji < 0 {
		t.Fatal("steps field missing")
	}
	schema.Fields[si].ItemFields[ji].ItemFields = append(schema.Fields[si].ItemFields[ji].ItemFields, Field{Name: "bogus"})
	c := &Checker{}
	c.checkServiceStepFields(schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), `unknown field "bogus"`) {
		t.Fatalf("step field drift not reported: %v", c.errs)
	}
}

func TestStorageFieldSubsetDrift(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	schema, _, err := contractspec.LoadSchemaByType(root, "data.storage")
	if err != nil {
		t.Fatal(err)
	}
	i := fieldIndex(schema.Fields, "tables")
	if i < 0 {
		t.Fatal("tables field missing")
	}
	schema.Fields[i].ItemFields = append(schema.Fields[i].ItemFields, Field{Name: "bogus"})
	c := &Checker{}
	c.checkStorageFieldSubset(schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), `unknown table entry field "bogus"`) {
		t.Fatalf("storage field drift not reported: %v", c.errs)
	}
}

func fieldIndex(fields []Field, name string) int {
	for i := range fields {
		if fields[i].Name == name {
			return i
		}
	}
	return -1
}

func TestMetaFieldValueBounds(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		ok    bool
	}{
		{"enum violates pattern", Field{Name: "s", Type: "_types.basics.string", Enum: []any{"a"}, Pattern: "^b$"}, false},
		{"default too short", Field{Name: "s", Type: "_types.basics.string", Default: "a", MinLen: 2}, false},
		{"default too long", Field{Name: "s", Type: "_types.basics.string", Default: "abc", MaxLen: 2}, false},
		{"default below min", Field{Name: "n", Type: "_types.basics.integer.32", Default: 1, Min: 2}, false},
		{"example above max", Field{Name: "n", Type: "_types.basics.integer.32", Examples: []any{5}, Max: 4}, false},
		{"valid string", Field{Name: "s", Type: "_types.basics.string", Default: "ab", MinLen: 2, MaxLen: 3}, true},
		{"valid integer", Field{Name: "n", Type: "_types.basics.integer.32", Default: 2, Min: 1, Max: 3}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFieldTypes([]Field{tc.field}, ValueTypes{}, false)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
