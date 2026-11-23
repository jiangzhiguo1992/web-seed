package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCapabilityFileFieldOrder 校验 flows / HTTP 契约的顶层字段顺序漂移会被拒绝。
func TestCapabilityFileFieldOrder(t *testing.T) {
	flowFields := []Field{
		{Name: "id", Type: "string"},
		{Name: "summary", Type: "string"},
		{Name: "owner_module", Type: "string"},
		{Name: "schema_version", Type: "integer"},
		{Name: "stability", Type: "string"},
		{Name: "do_not", Type: "list<string>"},
		{Name: "flows", Type: "list<object>"},
	}
	httpFields := []Field{
		{Name: "id", Type: "string"},
		{Name: "summary", Type: "string"},
		{Name: "owner_module", Type: "string"},
		{Name: "schema_version", Type: "integer"},
		{Name: "stability", Type: "string"},
		{Name: "do_not", Type: "list<string>"},
		{Name: "default_error_responses", Type: "list<any>"},
		{Name: "paths", Type: "any"},
		{Name: "components", Type: "any"},
	}

	cases := []struct {
		name   string
		rel    string
		body   string
		fields []Field
		want   string
	}{
		{
			name:   "flow schema_version before id",
			rel:    "contracts/capabilities/flows/x.yaml",
			body:   "schema_version: 1\nid: cap.flows.x\nsummary: \"x\"\nowner_module: x\nstability: draft\ndo_not: []\nflows: []\n",
			fields: flowFields,
			want:   "field id must come before schema_version",
		},
		{
			name:   "http stability before summary",
			rel:    "contracts/capabilities/protocols/http/x.yaml",
			body:   "id: cap.protocols.http.x\nstability: draft\nsummary: \"x\"\nowner_module: x\nschema_version: 1\ndo_not: []\npaths: {}\n",
			fields: httpFields,
			want:   "field summary must come before stability",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, tc.rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			c := &Checker{root: root}
			c.checkFieldOrder(tc.rel, tc.fields, orderRules{})
			if !strings.Contains(strings.Join(c.errs, "\n"), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, c.errs)
			}
		})
	}
}
