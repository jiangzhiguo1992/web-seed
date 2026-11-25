package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypeVocabulary(t *testing.T) {
	c := &Checker{valueTypes: testValueTypes()}
	c.checkTypeRef("integer", "x")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.type-vocabulary]") {
		t.Fatalf("bare type name not rejected: %v", c.errs)
	}
	c.errs = nil
	c.checkTypeRef("_types.basics.integer", "x")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.type-concrete]") {
		t.Fatalf("family without variant not rejected in instance: %v", c.errs)
	}
	c.errs = nil
	c.checkTypeRef("_types.basics.integer.32", "x")
	if len(c.errs) != 0 {
		t.Fatalf("concrete basics reference rejected: %v", c.errs)
	}
	c.errs = nil
	c.checkTypeRef("_types.structures.list<data.entities.account>", "x")
	if len(c.errs) != 0 {
		t.Fatalf("_types.structures.list<entity> rejected: %v", c.errs)
	}
	for _, typ := range []string{"object", "array", "map"} {
		c.errs = nil
		c.checkTypeRef(typ, "x")
		if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.type-vocabulary]") {
			t.Fatalf("bare schema kind %s not rejected: %v", typ, c.errs)
		}
	}
	for _, typ := range []string{"_types.schemas.object", "_types.schemas.array", "_types.schemas.map"} {
		c.errs = nil
		c.checkTypeRef(typ, "x")
		if len(c.errs) != 0 {
			t.Fatalf("schema kind %s rejected: %v", typ, c.errs)
		}
	}
}

func TestTypeVocabularyWalksUnvalidatedFiles(t *testing.T) {
	root := t.TempDir()
	copyMeta(t, root, "cap.services")
	dir := filepath.Join(root, "contracts", "platform")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loose.yaml"), []byte("type: string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, valueTypes: testValueTypes()}
	c.checkTypeVocabulary()
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.type-vocabulary]") {
		t.Fatalf("bare type in unvalidated file not rejected: %v", c.errs)
	}
}

func TestTypeVocabularyExternalRefs(t *testing.T) {
	c := &Checker{
		enumIDs:        map[string]bool{"data.enums.x": true},
		entities:       map[string]*entityInfo{"data.entities.x": {}},
		methodTypeDefs: map[string]map[string]any{"cap.services.x.schemas.T": {}},
		tableColumns:   map[string]map[string]columnMeta{"data.tables.x": {"id": {}}},
	}
	for _, ref := range []string{
		"data.enums.x",
		"data.entities.x",
		"cap.services.x.schemas.T",
		"data.tables.x.col.id",
	} {
		c.errs = nil
		c.checkTypeRef(ref, "p")
		if len(c.errs) != 0 {
			t.Fatalf("known ref %s rejected: %v", ref, c.errs)
		}
	}
	for _, ref := range []string{
		"data.enums.y",
		"data.entities.y",
		"cap.services.x.schemas.Missing",
		"data.tables.x.col.missing",
		"data.tables.x.col",
	} {
		c.errs = nil
		c.checkTypeRef(ref, "p")
		if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.type-vocabulary]") {
			t.Fatalf("unknown ref %s not rejected: %v", ref, c.errs)
		}
	}
}

func TestTypeVocabularySuppressesDomainError(t *testing.T) {
	domain := "[schema.ref] contracts/x.yaml: services[0].steps[1].output[0].type: unknown enum data.enums.y"
	c := &Checker{
		errs:    []string{domain},
		enumIDs: map[string]bool{},
	}
	c.checkTypeRef("data.enums.y", "contracts/x.yaml.services[0].steps[1].output[0].type")
	if len(c.errs) != 1 || c.errs[0] != domain {
		t.Fatalf("domain error should suppress vocabulary duplicate: %v", c.errs)
	}

	c = &Checker{enumIDs: map[string]bool{}}
	c.checkTypeRef("data.enums.y", "contracts/x.yaml.services[0].steps[1].output[0].type")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.type-vocabulary]") {
		t.Fatalf("vocabulary error without domain fallback missing: %v", c.errs)
	}

	domainType := "[service-ref] contracts/x.yaml:issue: unknown type cap.services.x.schemas.Missing"
	c = &Checker{
		errs:           []string{domainType},
		methodTypeDefs: map[string]map[string]any{},
	}
	c.checkTypeRef("cap.services.x.schemas.Missing", "contracts/x.yaml.services[0].returns[0].type")
	if len(c.errs) != 1 || c.errs[0] != domainType {
		t.Fatalf("same missing type should suppress vocabulary duplicate: %v", c.errs)
	}

	prefix := "[service-ref] contracts/x.yaml: unknown enum data.enums.account_status"
	c = &Checker{
		errs:    []string{prefix},
		enumIDs: map[string]bool{},
	}
	c.checkTypeRef("data.enums.account", "contracts/x.yaml.services[0].returns[0].type")
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown enum data.enums.account") {
		t.Fatalf("prefix token should not suppress a different missing enum: %v", c.errs)
	}
}

func TestTypeVocabularyCrossFileDuplicates(t *testing.T) {
	c := &Checker{enumIDs: map[string]bool{}}
	c.checkTypeRef("data.enums.missing", "contracts/a.yaml.services[0].returns[0].type")
	c.checkTypeRef("data.enums.missing", "contracts/b.yaml.services[0].returns[0].type")
	if got := strings.Count(strings.Join(c.errs, "\n"), "unknown enum data.enums.missing"); got != 2 {
		t.Fatalf("cross-file duplicates must both be reported, got %d: %v", got, c.errs)
	}
}

func TestTypeVocabularyOpenAPIException(t *testing.T) {
	root := t.TempDir()
	copyMeta(t, root, "cap.services")
	for _, item := range []struct{ path, body string }{
		{"contracts/capabilities/protocols/http/x.yaml", "type: object\n"},
		{"contracts/generated/x.yaml", "type: object\n"},
		{"contracts/platform/analytics/y.yaml", "type: object\n"},
	} {
		dir := filepath.Dir(filepath.Join(root, item.path))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, item.path), []byte(item.body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &Checker{root: root}
	c.checkTypeVocabulary()
	joined := strings.Join(c.errs, "\n")
	if strings.Contains(joined, "protocols/http") {
		t.Fatalf("protocols/http exception reported: %v", c.errs)
	}
	if strings.Contains(joined, "generated") {
		t.Fatalf("generated exception reported: %v", c.errs)
	}
	if !strings.Contains(joined, "contracts/platform/analytics/y.yaml") {
		t.Fatalf("non-exempt bare object not reported: %v", c.errs)
	}
}

func TestSchemaMapNode(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.services")
	c := &Checker{root: root, nodeKinds: schema.NodeKinds, propertyKeys: schema.PropertyKeys, valueTypes: schema.ValueTypes}
	c.validateSchemaNode(map[string]any{
		"type":   "_types.schemas.map",
		"values": map[string]any{"type": "_types.basics.string"},
	}, "payload")
	if len(c.errs) != 0 {
		t.Fatalf("valid map node rejected: %v", c.errs)
	}
	c.errs = nil
	c.validateSchemaNode(map[string]any{"type": "_types.schemas.map"}, "payload")
	if !strings.Contains(strings.Join(c.errs, "\n"), "map node requires values") {
		t.Fatalf("map node without values not rejected: %v", c.errs)
	}

	c.errs = nil
	c.validateSchemaNode(map[string]any{
		"type":      "_types.schemas.map",
		"values":    map[string]any{"type": "_types.basics.string"},
		"min_items": 2,
		"max_items": 1,
	}, "payload")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.range]") {
		t.Fatalf("map min/max bounds not rejected: %v", c.errs)
	}

	c.errs = nil
	c.validateSchemaNode(map[string]any{
		"type":         "_types.schemas.map",
		"values":       map[string]any{"type": "_types.basics.string"},
		"key_patterns": []any{"^[a-z]+$", "^[a-z0-9_]+$"},
	}, "payload")
	if len(c.errs) != 0 {
		t.Fatalf("valid map key_patterns rejected: %v", c.errs)
	}

	c.errs = nil
	c.validateSchemaNode(map[string]any{
		"type":         "_types.schemas.map",
		"values":       map[string]any{"type": "_types.basics.string"},
		"key_patterns": []any{"[invalid"},
	}, "payload")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.pattern]") {
		t.Fatalf("invalid map key pattern not rejected: %v", c.errs)
	}

	c.errs = nil
	c.validateSchemaNode(map[string]any{
		"type":   "_types.schemas.map",
		"values": map[string]any{"type": "_types.basics.string"},
		"validation": []any{
			map[string]any{"rule": "value != null", "message": "必填"},
		},
	}, "payload")
	if len(c.errs) != 0 {
		t.Fatalf("valid map validation rejected: %v", c.errs)
	}

	c.errs = nil
	c.validateSchemaNode(map[string]any{
		"type":   "_types.schemas.map",
		"values": map[string]any{"type": "_types.basics.string"},
		"validation": []any{
			map[string]any{"rule": "value.foo != null", "message": "x"},
		},
	}, "payload")
	if !strings.Contains(strings.Join(c.errs, "\n"), "map nodes may only reference value") {
		t.Fatalf("map validation path not rejected: %v", c.errs)
	}
}
