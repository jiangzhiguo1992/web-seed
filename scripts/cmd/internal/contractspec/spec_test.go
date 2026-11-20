package contractspec

import (
	"os"
	"path/filepath"
	"strings"
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
	shared, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "_meta", "_types.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeMeta(t, root, "_types.yaml", string(shared))
	writeMeta(t, root, "data.tables.yaml", `
id: meta.data.tables
type: data.tables
fields:
  - { name: id, type: _types.string }
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
  - { name: id, type: _types.string }
`)
	if _, _, err := LoadSchemaByType(root, "data.tables"); err == nil {
		t.Fatal("missing _types.yaml must fail for data.tables")
	}
}

func TestLoadSchemaByTypeDuplicate(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "a.yaml", "id: meta.a\ntype: data.tables\nfields:\n  - { name: id, type: _types.string }\n")
	writeMeta(t, root, "b.yaml", "id: meta.b\ntype: data.tables\nfields:\n  - { name: id, type: _types.string }\n")
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
  - { name: id, type: _types.string, requred: true }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSchema(path); err == nil {
		t.Fatal("unknown meta field must fail instead of being ignored")
	}
}

func TestLoadSharedTypesSchemas(t *testing.T) {
	root := t.TempDir()
	shared, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "_meta", "_types.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeMeta(t, root, "_types.yaml", string(shared))
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err != nil {
		t.Fatalf("valid _types.yaml rejected: %v", err)
	}

	body := strings.Replace(string(shared), "\n  scalar:\n", "\n  scalar_x:\n", 1)
	writeMeta(t, root, "_types.yaml", body)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), "schemas must define scalar") {
		t.Fatalf("missing scalar schema: %v", err)
	}

	body = string(shared)
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	inItemFields := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "item_fields:" && !inItemFields {
			inItemFields = true
			continue
		}
		if inItemFields {
			if strings.HasPrefix(line, "          ") {
				continue
			}
			inItemFields = false
		}
		kept = append(kept, line)
	}
	body = strings.Join(kept, "\n")
	writeMeta(t, root, "_types.yaml", body)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), "properties must define item_fields") {
		t.Fatalf("empty struct item_fields: %v", err)
	}

	body = strings.Replace(string(shared), "\ntype_vocabulary:\n", "\n  bogus:\n    description: \"x\"\n    keys:\n      - { name: type, type: _types.string, required: true }\n\ntype_vocabulary:\n", 1)
	writeMeta(t, root, "_types.yaml", body)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), "unknown schema bogus") {
		t.Fatalf("unknown schema kind: %v", err)
	}
}

func TestSharedTypeVocabularyExceptions(t *testing.T) {
	shared, err := LoadSharedTypes(filepath.Join("..", "..", "..", "..", "contracts", "_meta"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"contracts/_meta":                       "dir",
		"contracts/generated":                   "dir",
		"contracts/capabilities/protocols/http": "dir",
		"contracts/index.yaml":                  "file",
		"contracts/project.yaml":                "file",
	}
	if len(shared.TypeVocabulary.Exceptions) != len(want) {
		t.Fatalf("exceptions = %#v", shared.TypeVocabulary.Exceptions)
	}
	for _, ex := range shared.TypeVocabulary.Exceptions {
		kind, ok := want[ex.Path]
		if !ok || kind != ex.Kind {
			t.Fatalf("unexpected exception %#v", ex)
		}
	}
}

func TestLoadSharedTypesStructures(t *testing.T) {
	root := t.TempDir()
	shared, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "_meta", "_types.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeMeta(t, root, "_types.yaml", string(shared))
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err != nil {
		t.Fatalf("valid _types.yaml rejected: %v", err)
	}

	body := strings.Replace(string(shared), "\n  list:\n", "\n  bag:\n", 1)
	writeMeta(t, root, "_types.yaml", body)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), "unknown structure bag") {
		t.Fatalf("unknown structure: %v", err)
	}

	idx := strings.Index(string(shared), "\nstructures:")
	tail := strings.Replace(string(shared)[idx:], "type: _types.integer", "type: _types.bogus", 1)
	writeMeta(t, root, "_types.yaml", string(shared)[:idx]+tail)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("unknown structure param type: %v", err)
	}

	idx = strings.Index(string(shared), "\nstructures:")
	tail = strings.Replace(string(shared)[idx:], "max_items:", "len:", 1)
	writeMeta(t, root, "_types.yaml", string(shared)[:idx]+tail)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), "does not allow param len") {
		t.Fatalf("invalid list param: %v", err)
	}
}
