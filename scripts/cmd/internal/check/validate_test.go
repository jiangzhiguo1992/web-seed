package check

import (
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
	c.validateValue("ab", Field{Type: "_types.string", MinLen: 3}, "x")
	c.validateValue("abcd", Field{Type: "_types.string", MaxLen: 3}, "x")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[schema.len]") {
		t.Fatalf("string bounds not enforced: %v", c.errs)
	}
}

func TestLengthKeysRequireLengthType(t *testing.T) {
	if err := validateFieldTypes([]Field{{Name: "n", Type: "_types.integer", MinLen: 1}}, ValueTypes{}, false); err == nil {
		t.Fatal("min_len on integer must be rejected")
	}
	if err := validateFieldTypes([]Field{{Name: "n", Type: "_types.string", MaxLen: "x"}}, ValueTypes{}, false); err == nil {
		t.Fatal("non-numeric max_len must be rejected")
	}
	if err := validateFieldTypes([]Field{{Name: "s", Type: "_types.string", Len: 3}}, ValueTypes{}, false); err == nil {
		t.Fatal("len on string must be rejected")
	}
	if err := validateFieldTypes([]Field{{Name: "a", Type: "_types.array<_types.string>", Len: 3, MinItems: 1}}, ValueTypes{}, false); err == nil {
		t.Fatal("min_items on array must be rejected")
	}
	if err := validateFieldTypes([]Field{{Name: "n", Type: "_types.integer", Pattern: "^x$"}}, ValueTypes{}, false); err == nil {
		t.Fatal("pattern on integer must be rejected")
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
	f := Field{Name: "x", Type: "_types.string", Enum: []any{"stable", "draft"}, Default: "draf"}
	if err := validateFieldTypes([]Field{f}, ValueTypes{}, false); err == nil || !strings.Contains(err.Error(), "not in enum") {
		t.Fatalf("default outside enum must be rejected: %v", err)
	}
	f.Default = "draft"
	if err := validateFieldTypes([]Field{f}, ValueTypes{}, false); err != nil {
		t.Fatalf("default inside enum rejected: %v", err)
	}
}
