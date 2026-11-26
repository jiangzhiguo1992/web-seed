package contractspec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
contract: data.tables
fields:
  - { name: id, type: _types.basics.string }
`)
	s, path, err := LoadSchemaByType(root, "data.tables")
	if err != nil {
		t.Fatal(err)
	}
	if s.Contract != "data.tables" || filepath.Base(path) != "data.tables.yaml" {
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
contract: data.tables
fields:
  - { name: id, type: _types.basics.string }
`)
	if _, _, err := LoadSchemaByType(root, "data.tables"); err == nil {
		t.Fatal("missing _types.yaml must fail for data.tables")
	}
}

func TestLoadSchemaByTypeDuplicate(t *testing.T) {
	root := t.TempDir()
	writeMeta(t, root, "a.yaml", "id: meta.a\ntype: data.tables\nfields:\n  - { name: id, type: _types.basics.string }\n")
	writeMeta(t, root, "b.yaml", "id: meta.b\ntype: data.tables\nfields:\n  - { name: id, type: _types.basics.string }\n")
	if _, _, err := LoadSchemaByType(root, "data.tables"); err == nil {
		t.Fatal("duplicate meta type must fail")
	}
}

func TestLoadSchemaRejectsUnknownField(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "data.tables.yaml")
	body := `
id: meta.data.tables
contract: data.tables
fields:
  - { name: id, type: _types.basics.string, requred: true }
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

	body = strings.Replace(string(shared), "\nschemas:\n", "\nschemas:\n  bogus:\n    description: \"x\"\n    keys:\n      - { name: type, type: _types.basics.string, required: true }\n", 1)
	writeMeta(t, root, "_types.yaml", body)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), "unknown schema bogus") {
		t.Fatalf("unknown schema kind: %v", err)
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
	tail := strings.Replace(string(shared)[idx:], "type: _types.basics.integer.32", "type: _types.basics.bogus.32", 1)
	writeMeta(t, root, "_types.yaml", string(shared)[:idx]+tail)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), "unknown basics family") {
		t.Fatalf("unknown structure param type: %v", err)
	}

	idx = strings.Index(string(shared), "\nstructures:")
	tail = strings.Replace(string(shared)[idx:], "max_items:", "len:", 1)
	writeMeta(t, root, "_types.yaml", string(shared)[:idx]+tail)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), "does not allow param len") {
		t.Fatalf("invalid list param: %v", err)
	}
}

func TestSplitTypeRef(t *testing.T) {
	cases := []struct {
		ref   string
		class string
		name  string
		ok    bool
	}{
		{"_types.basics.string", TypeClassBasics, "string", true},
		{"_types.structures.list<_types.basics.string>", TypeClassStructures, "list<_types.basics.string>", true},
		{"_types.schemas.*", TypeClassSchemas, "*", true},
		{"_types.string", "", "", false},
		{"_types.unknown.string", "", "", false},
		{"data.entities.account", "", "", false},
	}
	for _, tc := range cases {
		class, name, ok := SplitTypeRef(tc.ref)
		if ok != tc.ok || class != tc.class || name != tc.name {
			t.Fatalf("SplitTypeRef(%q) = (%q,%q,%v), want (%q,%q,%v)", tc.ref, class, name, ok, tc.class, tc.name, tc.ok)
		}
	}
}

func TestSchemaNodeKindRequiresNamespace(t *testing.T) {
	kinds := map[string]SchemaKind{
		"object": {Keys: []Field{{Name: "type"}}},
		"array":  {Keys: []Field{{Name: "type"}}},
		"map":    {Keys: []Field{{Name: "type"}}},
		"scalar": {Keys: []Field{{Name: "type"}}},
		"ref":    {Keys: []Field{{Name: "ref"}}},
	}
	if _, ok := SchemaNodeKind(map[string]any{"type": "_types.object"}, kinds); ok {
		t.Fatal("legacy _types.object must not be a schema node kind")
	}
	if kind, ok := SchemaNodeKind(map[string]any{"kind": "_types.schemas.object"}, kinds); !ok || kind != "object" {
		t.Fatalf("qualified object node = (%q,%v)", kind, ok)
	}
	if kind, ok := SchemaNodeKind(map[string]any{"type": "_types.basics.string"}, kinds); !ok || kind != "scalar" {
		t.Fatalf("qualified scalar node = (%q,%v)", kind, ok)
	}
}

func TestDatetimeTypeKeys(t *testing.T) {
	shared, err := LoadSharedTypes(filepath.Join("..", "..", "..", "..", "contracts", "_meta"))
	if err != nil {
		t.Fatal(err)
	}
	fam, ok := shared.TypeFamilies["datetime"]
	if !ok {
		t.Fatal("datetime family missing")
	}
	for _, want := range []string{"timezone", "time"} {
		if _, ok := fam.Types[want]; !ok {
			t.Fatalf("datetime missing type key %q", want)
		}
	}
	if len(fam.Types) != 2 {
		t.Fatalf("datetime type keys = %v", fam.Types)
	}
}

func TestTimestampTypeKeys(t *testing.T) {
	shared, err := LoadSharedTypes(filepath.Join("..", "..", "..", "..", "contracts", "_meta"))
	if err != nil {
		t.Fatal(err)
	}
	fam, ok := shared.TypeFamilies["timestamp"]
	if !ok {
		t.Fatal("timestamp family missing")
	}
	for _, want := range []string{"time", "long"} {
		if _, ok := fam.Types[want]; !ok {
			t.Fatalf("timestamp missing variant %q", want)
		}
	}
	if len(fam.Types) != 2 {
		t.Fatalf("timestamp type keys = %v", fam.Types)
	}
}

func TestSharedTypesConcreteTypes(t *testing.T) {
	root := t.TempDir()
	shared, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "_meta", "_types.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeMeta(t, root, "_types.yaml", string(shared))
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err != nil {
		t.Fatalf("valid _types.yaml rejected: %v", err)
	}

	body := strings.Replace(string(shared), "boolean: { go: bool, pg: [ boolean ], ts: boolean }", "bool: { go: bool, pg: [ boolean ], ts: boolean }", 1)
	writeMeta(t, root, "_types.yaml", body)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), `missing concrete type "boolean"`) {
		t.Fatalf("missing concrete type: %v", err)
	}

	body = strings.Replace(string(shared), "pg: [ smallint ]", "pg: [ bogus ]", 1)
	writeMeta(t, root, "_types.yaml", body)
	if _, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta")); err == nil || !strings.Contains(err.Error(), `invalid pg type "bogus"`) {
		t.Fatalf("invalid pg type: %v", err)
	}
}

func TestLoadCustomTypesRequiresConcreteType(t *testing.T) {
	root := t.TempDir()
	sharedRaw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "_meta", "_types.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeMeta(t, root, "_types.yaml", string(sharedRaw))
	shared, err := LoadSharedTypes(filepath.Join(root, "contracts/_meta"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := shared.Commons.BasicsParams["default"]; !ok {
		t.Fatal("commons.basics_params.default is not declared")
	}

	write := func(body string) error {
		dir := filepath.Join(root, "contracts/data")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "types.yaml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := LoadCustomTypes(root, shared.TypeFamilies, shared.Commons.BasicsParams)
		return err
	}

	valid := `id: data.types
summary: "x"
schema_version: 1
stability: draft
do_not: [ "x" ]
custom_types:
  id:
    type: _types.basics.integer.64
    description: "id"
  page_offset:
    type: _types.basics.integer.32
    description: "offset"
    params: { min: 0, default: 0 }
`
	if err := write(valid); err != nil {
		t.Fatalf("valid custom types rejected: %v", err)
	}
	if err := write(strings.Replace(valid, "params: { min: 0, default: 0 }", "params: { auto_increment: true }", 1)); err == nil || !strings.Contains(err.Error(), "table-only") {
		t.Fatalf("table-only param accepted in custom type: %v", err)
	}
	if err := write(strings.Replace(valid, "type: _types.basics.integer.64", "type: _types.basics.integer", 1)); err == nil || !strings.Contains(err.Error(), "requires a concrete variant") {
		t.Fatalf("bare integer accepted: %v", err)
	}
	if err := write(strings.Replace(valid, "type: _types.basics.integer.64", "type: _types.basics.string.string", 1)); err == nil || !strings.Contains(err.Error(), "single-variant") {
		t.Fatalf("explicit single-variant suffix accepted: %v", err)
	}
	if err := write(strings.Replace(valid, "type: _types.basics.integer.64", "family: integer", 1)); err == nil || !strings.Contains(err.Error(), "field family not found") {
		t.Fatalf("legacy family key accepted: %v", err)
	}
}

// loadMetaSchemas 加载 contracts/_meta 下除 _types 外的全部类型规范。
func loadMetaSchemas(t *testing.T) []*Schema {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "contracts", "_meta")
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var out []*Schema
	for _, file := range files {
		base := filepath.Base(file)
		if base == "_types.yaml" {
			continue
		}
		s, err := LoadSchema(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		t.Fatal("no meta schemas loaded")
	}
	return out
}

func walkMetaFields(fields []Field, fn func(Field)) {
	for _, f := range fields {
		fn(f)
		walkMetaFields(f.Fields, fn)
		walkMetaFields(f.ItemFields, fn)
	}
}

// TestMetaPatternsAreAnchored 防止正则退化成前缀匹配：字段与 key 的正则必须整体锚定。
func TestMetaPatternsAreAnchored(t *testing.T) {
	if violations := metaPatternViolations(loadMetaSchemas(t)); len(violations) > 0 {
		t.Fatalf("unanchored patterns: %v", violations)
	}
}

func TestMetaPatternsAreAnchoredRejectsPrefix(t *testing.T) {
	bad := &Schema{Contract: "x", Fields: []Field{
		{Name: "prefix", Pattern: `^prefix`},
		{Name: "key", KeyPatterns: []string{`^[a-z]+`}},
	}}
	violations := metaPatternViolations([]*Schema{bad})
	if len(violations) != 2 {
		t.Fatalf("want 2 violations, got %v", violations)
	}
}

func metaPatternViolations(schemas []*Schema) []string {
	var out []string
	for _, s := range schemas {
		walkMetaFields(s.Fields, func(f Field) {
			if f.Pattern != "" && (!strings.HasPrefix(f.Pattern, "^") || !strings.HasSuffix(f.Pattern, "$")) {
				out = append(out, fmt.Sprintf("%s.%s: pattern must be anchored: %q", s.Contract, f.Name, f.Pattern))
			}
			for _, p := range f.KeyPatterns {
				if !strings.HasPrefix(p, "^") || !strings.HasSuffix(p, "$") {
					out = append(out, fmt.Sprintf("%s.%s: key_pattern must be anchored: %q", s.Contract, f.Name, p))
				}
			}
		})
	}
	return out
}

// TestMetaGeneratedFieldsAreMarked 保证描述里声明由 gen-graph 写入的字段同时标记 generated/write。
func TestMetaGeneratedFieldsAreMarked(t *testing.T) {
	for _, s := range loadMetaSchemas(t) {
		walkMetaFields(s.Fields, func(f Field) {
			if strings.Contains(f.Description, "gen-graph") && (!f.Generated || f.Write != "forbidden") {
				t.Errorf("%s.%s: gen-graph field must set generated: true and write: forbidden", s.Contract, f.Name)
			}
		})
	}
}

// TestMetaErrorPatternsCanonical 统一 platform.errors 引用的正则，避免前缀匹配吞掉空错误码。
func TestMetaErrorPatternsCanonical(t *testing.T) {
	const canonical = `^platform\.errors\.[A-Z][A-Z0-9_]*$`
	for _, s := range loadMetaSchemas(t) {
		walkMetaFields(s.Fields, func(f Field) {
			if (f.Name == "errors" || f.Name == "on_violation") && f.Pattern != "" && f.Pattern != canonical {
				t.Errorf("%s.%s: error reference pattern must be %q, got %q", s.Contract, f.Name, canonical, f.Pattern)
			}
		})
	}
}

func TestFeatureRoutesPathPattern(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	s, _, err := LoadSchemaByType(root, "feature.routes")
	if err != nil {
		t.Fatal(err)
	}
	routes, ok := FindField(s.Fields, "routes")
	if !ok {
		t.Fatal("routes field missing")
	}
	path, ok := FindField(routes.ItemFields, "path")
	if !ok || path.Pattern == "" {
		t.Fatal("path pattern missing")
	}
	re := regexp.MustCompile(path.Pattern)
	for _, valid := range []string{"/", "/account", "/account/:id", "/account/:id/security"} {
		if !re.MatchString(valid) {
			t.Errorf("valid path rejected: %q", valid)
		}
	}
	for _, invalid := range []string{"", "//", "/account/", "/account//x", "/:"} {
		if re.MatchString(invalid) {
			t.Errorf("invalid path accepted: %q", invalid)
		}
	}
}
