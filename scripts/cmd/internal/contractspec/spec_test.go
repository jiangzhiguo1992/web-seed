package contractspec

import (
	"os"
	"path/filepath"
	"testing"
)

func writeMeta(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "contracts/_meta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSchemaByType(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "_helpers.yaml", "id: meta._helpers\n")
	writeMeta(t, root, "_types.yaml", `
id: meta.types
type_families:
  string:
    description: "string"
`)
	writeMeta(t, root, "data.tables.yaml", `
id: meta.data.tables
type: data.tables
fields:
  - { name: id, type: string }
`)
	s, path, err := LoadSchemaByType(root, "data.tables")
	if err != nil {
		t.Fatal(err)
	}
	if s.Type != "data.tables" || filepath.Base(path) != "data.tables.yaml" {
		t.Fatalf("unexpected schema: %#v %s", s, path)
	}
	if _, _, err := LoadSchemaByType(root, "missing"); err == nil {
		t.Fatal("missing type must fail")
	}
}

func TestLoadSchemaByTypeRequiresSharedTypes(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "data.tables.yaml", `
id: meta.data.tables
type: data.tables
fields:
  - { name: id, type: string }
`)
	if _, _, err := LoadSchemaByType(root, "data.tables"); err == nil {
		t.Fatal("missing _types.yaml must fail for data.tables")
	}
}

func TestLoadSchemaByTypeDuplicate(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "a.yaml", "id: meta.a\ntype: data.tables\nfields:\n  - { name: id, type: string }\n")
	writeMeta(t, root, "b.yaml", "id: meta.b\ntype: data.tables\nfields:\n  - { name: id, type: string }\n")
	if _, _, err := LoadSchemaByType(root, "data.tables"); err == nil {
		t.Fatal("duplicate meta type must fail")
	}
}

func TestLoadSchemaRejectsUnknownField(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.tables.yaml")
	body := `
id: meta.data.tables
type: data.tables
fields:
  - { name: id, type: string, requred: true }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSchema(path); err == nil {
		t.Fatal("unknown meta field must fail instead of being ignored")
	}
}

func TestLoadSharedSchemasRequiresDescription(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "_schemas.yaml", `id: meta.schemas
title: "schema"
kinds:
  object:
    keys:
      - { name: type, type: string, description: "类型" }
      - { name: properties, type: list<property>, description: "属性" }
  array:
    description: "数组"
    keys:
      - { name: type, type: string, description: "类型" }
      - { name: items, type: schema, description: "元素" }
  scalar:
    description: "标量"
    keys:
      - { name: type, type: string, description: "类型" }
  ref:
    description: "引用"
    keys:
      - { name: ref, type: string, description: "引用" }
property:
  keys:
    - { name: name, type: string, description: "名" }
    - { name: schema, type: schema, description: "值域" }
`)
	if _, err := LoadSharedSchemas(filepath.Join(root, "contracts/_meta")); err == nil {
		t.Fatal("missing kind description must fail")
	}
}

func TestLoadSharedSchemasFields(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "_schemas.yaml", `id: meta.schemas
title: "schema"
kinds:
  object:
    description: "对象"
    keys:
      - { name: type, type: string, description: "类型" }
      - { name: properties, type: list<property>, description: "属性" }
  array:
    description: "数组"
    keys:
      - { name: type, type: string, description: "类型" }
      - { name: items, type: schema, description: "元素" }
  scalar:
    description: "标量"
    keys:
      - { name: type, type: string, description: "类型" }
  ref:
    description: "引用"
    keys:
      - { name: ref, type: string, description: "引用" }
property:
  description: "属性"
  keys:
    - { name: name, type: string, description: "名" }
    - { name: schema, type: schema, description: "值域" }
    - { name: required, type: boolean, description: "必填" }
`)
	sh, err := LoadSharedSchemas(filepath.Join(root, "contracts/_meta"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, key := range sh.Kinds["object"].Keys {
		names[key.Name] = true
	}
	for _, want := range []string{"type", "properties"} {
		if !names[want] {
			t.Fatalf("missing key spec %s: %#v", want, sh.Kinds["object"].Keys)
		}
	}
	if len(sh.Property.Keys) != 3 {
		t.Fatalf("property keys not loaded: %#v", sh.Property.Keys)
	}
}
