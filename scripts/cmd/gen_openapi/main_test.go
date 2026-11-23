package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

func TestGeneratedOpenAPISeparatesSourceVocabulary(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		var doc any
		if err := yaml.Unmarshal(body, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if key, found := forbiddenSourceKey(doc); found {
			t.Fatalf("%s contains source-only key/value %q", name, key)
		}
	}
}

func forbiddenSourceKey(v any) (string, bool) {
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			if strings.HasPrefix(key, "x-") || key == "source" || key == "contract" {
				return key, true
			}
			if found, ok := forbiddenSourceKey(child); ok {
				return found, true
			}
		}
	case []any:
		for _, child := range value {
			if found, ok := forbiddenSourceKey(child); ok {
				return found, true
			}
		}
	case string:
		if strings.Contains(value, "_types.") {
			return value, true
		}
	}
	return "", false
}

func writeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTypes(t *testing.T, root string) {
	t.Helper()
	writeFixture(t, root, "contracts/data/types.yaml", `id: data.types
summary: "test"
schema_version: 1
stability: draft
do_not: ["test"]
custom_types: {}
`)
	shared, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "_meta", "_types.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "contracts/_meta/_types.yaml", string(shared))

}

func testNodeKinds() map[string]contractspec.SchemaKind {
	return map[string]contractspec.SchemaKind{
		"object": {Keys: []contractspec.Field{{Name: "type"}, {Name: "properties"}}},
		"array":  {Keys: []contractspec.Field{{Name: "type"}, {Name: "items"}}},
		"map":    {Keys: []contractspec.Field{{Name: "type"}, {Name: "values"}}},
		"scalar": {Keys: []contractspec.Field{{Name: "type"}}},
		"ref":    {Keys: []contractspec.Field{{Name: "ref"}}},
	}
}

func TestMarshalUnquotesStatusKeys(t *testing.T) {
	b, err := marshal(map[string]any{
		"200": map[string]any{"description": "ok"},
		"503": map[string]any{"description": "retry"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"200":`) || strings.Contains(string(b), `"503":`) {
		t.Fatalf("status keys still quoted:\n%s", b)
	}
	if !strings.Contains(string(b), "200:") || !strings.Contains(string(b), "503:") {
		t.Fatalf("status keys missing:\n%s", b)
	}
}

func TestBuildExpandsServiceTypes(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    api_version: v1
    timeout_ms: 5000
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
paths:
  /x:
    get:
      name: "getX"
      contract:
        id: "cap.protocols.http.x.get"
        service: "cap.services.x.get"
      request: {}
      response:
        "200":
          description: "OK"
      errors: []
components:
  schemas:
    X:
      source:
        service_type: "cap.services.x.schemas.X"`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
schemas:
  X:
    type: _types.object
    properties:
      - { name: id, schema: { type: _types.integer }, required: true, description: "id" }
services: []
`)
	writeFixture(t, root, "contracts/data/enums/account.yaml", `id: data.enums
enums: []
`)
	if err := os.MkdirAll(filepath.Join(root, "contracts/capabilities/services"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "contracts/data/entities"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	out := string(files["generated/openapi/x.yaml"])
	for _, want := range []string{"openapi: 3.1.0", "getX", "properties", "id:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated spec missing %q:\n%s", want, out)
		}
	}
}

func TestBuildVersionedAppsShareFiles(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  v1:
    kind: api
    api_version: v1
    timeout_ms: 5000
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/v1.yaml
  v2:
    kind: api
    api_version: v2
    timeout_ms: 5000
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/v2.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
paths:
  /x:
    get:
      name: "getX"
      contract:
        id: "cap.protocols.http.x.get"
        service: "cap.services.x.get"
      request: {}
      response:
        "200":
          description: "OK"
      errors: []`)
	for _, dir := range []string{"contracts/capabilities/services", "contracts/data/entities"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	for version, key := range map[string]string{"v1": "generated/openapi/v1.yaml", "v2": "generated/openapi/v2.yaml"} {
		out := string(files[key])
		if !strings.Contains(out, "url: /"+version) {
			t.Fatalf("%s missing server /%s:\n%s", key, version, out)
		}
		if !strings.Contains(out, "getX") {
			t.Fatalf("%s missing shared operation:\n%s", key, out)
		}
	}
}

func TestBuildEventPayloads(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    api_version: v1
    timeout_ms: 5000
    includes: []
    generated:
      spec: generated/openapi/x.yaml
`)
	for _, dir := range []string{
		"contracts/capabilities/protocols/http",
		"contracts/capabilities/services",
		"contracts/data/enums",
		"contracts/data/entities",
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, root, "contracts/capabilities/events/x.yaml", `id: cap.events.x
topics:
  - id: cap.events.x.created
    name: x.created
    version: 1
    summary: "created"
    publish:
      payload:
        type: _types.object
        properties:
          - { name: id, schema: { type: integer }, required: true, description: "id" }
          - { name: account, schema: { ref: data.entities.account }, required: true, description: "account" }
          - name: attributes
            schema:
              type: _types.map
              values: { type: string }
              min_items: 1
              max_items: 5
              key_patterns: [ '^[a-z][a-z0-9_]*$' ]
            description: "attrs"
      emitted_by: []
`)
	writeFixture(t, root, "contracts/data/entities/account.yaml", `id: data.entities.account
fields:
  - { name: id, json_name: id, type: _types.integer, description: "id" }
`)
	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	out := string(files["generated/openapi/x.yaml"])
	for _, want := range []string{
		"EventXCreated",
		"additionalProperties:",
		"minProperties: 1",
		"maxProperties: 5",
		"propertyNames:",
		"pattern: ^[a-z][a-z0-9_]*$",
		"Account:",
		"$ref: '#/components/schemas/Account'",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("event payload missing %q:\n%s", want, out)
		}
	}
}

func TestBuildAppliesDefaultsPerFile(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    api_version: v1
    timeout_ms: 5000
    includes: [cap.protocols.http.a, cap.protocols.http.b, cap.protocols.http.shared]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/a.yaml", `id: "cap.protocols.http.a"
paths:
  /a:
    get:
      name: "getA"
      contract:
        id: "cap.protocols.http.a.get"
        service: "cap.services.x.get"
      request: {}
      response:
        "200":
          description: "OK"
      errors: []
default_error_responses:
  - 422`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/b.yaml", `id: "cap.protocols.http.b"
paths:
  /b:
    get:
      name: "getB"
      contract:
        id: "cap.protocols.http.b.get"
        service: "cap.services.x.get"
      request: {}
      response:
        "200":
          description: "OK"
      errors: []
default_error_responses:
  - 423`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/shared.yaml", `id: cap.protocols.http.shared
components:
  responses:
    UnprocessableEntity:
      description: "422"
      content:
        application/problem+json:
          schema: { $ref: "#/components/schemas/Problem" }
    Locked:
      description: "423"
      content:
        application/problem+json:
          schema: { $ref: "#/components/schemas/Problem" }
  schemas:
    Problem:
      type: object
      additionalProperties: false
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
schemas: {}
services: []
`)
	writeFixture(t, root, "contracts/data/enums/account.yaml", `id: data.enums
enums: []
`)
	if err := os.MkdirAll(filepath.Join(root, "contracts/data/entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	out := string(files["generated/openapi/x.yaml"])
	a := out[strings.Index(out, "/a:"):strings.Index(out, "/b:")]
	b := out[strings.Index(out, "/b:"):]
	if !strings.Contains(a, "422:") || strings.Contains(a, "423:") {
		t.Fatalf("file a defaults wrong:\n%s", a)
	}
	if !strings.Contains(b, "423:") || strings.Contains(b, "422:") {
		t.Fatalf("file b defaults wrong:\n%s", b)
	}
}

func TestBuildOperationDefaultOverride(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    api_version: v1
    timeout_ms: 5000
    includes: [cap.protocols.http.a, cap.protocols.http.shared]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/a.yaml", `id: "cap.protocols.http.a"
paths:
  /a:
    get:
      name: "getA"
      contract:
        id: "cap.protocols.http.a.get"
        service: "cap.services.x.get"
        default_errors:
          - 423
      request: {}
      response:
        "200":
          description: "OK"
      errors: []
default_error_responses:
  - 422`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/shared.yaml", `id: cap.protocols.http.shared
components:
  responses:
    UnprocessableEntity: { description: "422" }
    Locked: { description: "423" }
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
schemas: {}
services: []
`)
	writeFixture(t, root, "contracts/data/enums/account.yaml", `id: data.enums
enums: []
`)
	if err := os.MkdirAll(filepath.Join(root, "contracts/data/entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	out := string(files["generated/openapi/x.yaml"])
	paths := out[strings.Index(out, "paths:"):]
	if !strings.Contains(paths, "423:") || strings.Contains(paths, "422:") {
		t.Fatalf("operation override not applied:\n%s", out)
	}
}

func TestRenderSchemaAnnotations(t *testing.T) {
	obj := map[string]any{
		"type": "_types.object",
		"properties": []any{
			map[string]any{
				"name":        "id",
				"schema":      map[string]any{"type": "string"},
				"description": "id",
				"nullable":    true,
				"read_only":   true,
				"required":    true,
			},
		},
	}
	out, err := renderSchemaNode(obj, nil, nil, map[string]bool{}, testNodeKinds())
	if err != nil {
		t.Fatal(err)
	}
	child := asMap(asMap(out["properties"])["id"])
	if child["description"] != "id" || child["nullable"] != true || child["readOnly"] != true {
		t.Fatalf("property annotations lost: %#v", child)
	}
	req := asList(out["required"])
	if len(req) != 1 || req[0] != "id" {
		t.Fatalf("required aggregation lost: %#v", out)
	}

	list := map[string]any{"type": "_types.array", "items": map[string]any{"type": "string"}}
	out, err = renderSchemaNode(list, nil, nil, map[string]bool{}, testNodeKinds())
	if err != nil {
		t.Fatal(err)
	}
	item := asMap(out["items"])
	if item["type"] != "string" {
		t.Fatalf("array items lost: %#v", item)
	}

	m := map[string]any{"type": "_types.map", "values": map[string]any{"type": "string"}}
	out, err = renderSchemaNode(m, nil, nil, map[string]bool{}, testNodeKinds())
	if err != nil {
		t.Fatal(err)
	}
	ap := asMap(out["additionalProperties"])
	if out["type"] != "object" || ap["type"] != "string" {
		t.Fatalf("map values lost: %#v", out)
	}
}
