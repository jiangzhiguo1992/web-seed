package check

import (
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

// TestReferenceListMinItems 锁定引用列表的非空声明，防止 types 重构再次误删。
func TestReferenceListMinItems(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		name  string
		typ   string
		path  []string
		items []bool // path 每一步进入 Fields(false) 还是 ItemFields(true)
	}{
		{"entity.tables", "data.entities", []string{"model", "tables"}, []bool{false, false}},
		{"entity.queries", "data.entities", []string{"model", "queries"}, []bool{false, false}},
		{"query.used_by", "data.queries", []string{"queries", "used_by"}, []bool{true, false}},
		{"table.queries", "data.tables", []string{"table", "queries"}, []bool{false, false}},
		{"service.used_by", "cap.services", []string{"services", "used_by"}, []bool{true, false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema, _, err := contractspec.LoadSchemaByType(root, tc.typ)
			if err != nil {
				t.Fatal(err)
			}
			fields := schema.Fields
			var field Field
			for i, name := range tc.path {
				var ok bool
				field, ok = contractspec.FindField(fields, name)
				if !ok {
					t.Fatalf("%s: field %s not found", tc.typ, name)
				}
				if i < len(tc.path)-1 {
					if tc.items[i] {
						fields = field.ItemFields
					} else {
						fields = field.Fields
					}
				}
			}
			n, ok := toInt(field.MinItems)
			if !ok || n != 1 {
				t.Fatalf("%s: min_items must be 1, got %v", tc.name, field.MinItems)
			}
		})
	}
}

// TestHTTPUsedByOptional 记录协议层例外：对外 operation 不要求有页面消费者。
func TestHTTPUsedByOptional(t *testing.T) {
	root := repoRoot(t)
	schema, _, err := contractspec.LoadSchemaByType(root, "cap.protocols.http")
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := contractspec.FindField(schema.Fields, "paths")
	if !ok {
		t.Fatal("paths field missing")
	}
	get, ok := contractspec.FindField(paths.ItemFields, "get")
	if !ok {
		t.Fatal("paths.get missing")
	}
	usedBy, ok := contractspec.FindField(get.ItemFields, "used_by")
	if !ok {
		t.Fatal("used_by field missing")
	}
	if usedBy.Required {
		t.Fatal("http used_by must stay optional")
	}
	if usedBy.MinItems != nil {
		t.Fatalf("http used_by must not require min_items, got %v", usedBy.MinItems)
	}
}
