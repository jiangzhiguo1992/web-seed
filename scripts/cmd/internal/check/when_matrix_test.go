// when_matrix_test.go 用矩阵覆盖 service-when-types 的分支字面量类型。
package check

import (
	"strings"
	"testing"
)

func TestWhenLiteralMatrix(t *testing.T) {
	c := &Checker{enumValues: map[string]map[string]bool{
		"data.enums.status": {"active": true},
	}}
	cases := []struct {
		name  string
		typ   string
		value any
		want  bool
	}{
		{"boolean-bool", "boolean", true, false},
		{"boolean-string", "boolean", "true", true},
		{"integer-integral-float", "integer", float64(1), false},
		{"integer-fraction", "integer", 1.5, true},
		{"integer-string", "integer", "1", true},
		{"string-string", "string", "x", false},
		{"string-int", "string", 1, true},
		{"enum-valid", "data.enums.status", "active", false},
		{"enum-unknown", "data.enums.status", "nope", true},
		{"enum-nonstring", "data.enums.status", 1, true},
		{"list", "List<integer>", 1, true},
		{"unknown-family", "Custom", "x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c.errs = nil
			c.checkWhenLiteral(tc.typ, tc.value, "rel", "step", "field")
			got := strings.Contains(strings.Join(c.errs, "\n"), "[service-when-types]")
			if got != tc.want {
				t.Fatalf("service-when-types = %v, want %v: %v", got, tc.want, c.errs)
			}
		})
	}
}
