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
