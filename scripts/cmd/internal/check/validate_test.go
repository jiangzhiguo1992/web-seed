package check

import (
	"strings"
	"testing"
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
