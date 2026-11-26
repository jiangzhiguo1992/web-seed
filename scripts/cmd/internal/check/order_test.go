package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

// TestSchemaNodeOrderUsesKind 证明 schemas.* 的节点顺序按 _types.yaml 的 keys 校验。
// schema 节点用 kind 判别，不再使用旧的 type: object/array/map 写法。
func TestSchemaNodeOrderUsesKind(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, _, err := contractspec.LoadSchemaByType(root, "cap.services")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "object keys",
			body: "schemas:\n  X:\n    properties: [ ]\n    kind: _types.schemas.object\n",
			want: "field kind must come before properties",
		},
		{
			name: "map value keys",
			body: "schemas:\n  X:\n    kind: _types.schemas.map\n    values:\n      validation: [ ]\n      type: _types.basics.string\n",
			want: "field type must come before validation",
		},
		{
			name: "property keys",
			body: "schemas:\n  X:\n    kind: _types.schemas.object\n    properties:\n      - description: \"a\"\n        name: a\n        schema:\n          type: _types.basics.string\n",
			want: "field name must come before description",
		},
		{
			name: "nested property schema keys",
			body: "schemas:\n  X:\n    kind: _types.schemas.object\n    properties:\n      - name: a\n        schema:\n          validation: [ ]\n          type: _types.basics.string\n        description: \"a\"\n",
			want: "field type must come before validation",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			rel := "contracts/capabilities/services/x.yaml"
			path := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			c := &Checker{root: dir, nodeKinds: schema.NodeKinds, propertyKeys: schema.PropertyKeys}
			c.checkFieldOrder(rel, schema.Fields, orderRules{})
			got := strings.Join(c.errs, "\n")
			if !strings.Contains(got, "[schema.field-order]") || !strings.Contains(got, tc.want) {
				t.Fatalf("schema node order not checked: %v", c.errs)
			}
		})
	}
}
