package check

import "testing"

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
