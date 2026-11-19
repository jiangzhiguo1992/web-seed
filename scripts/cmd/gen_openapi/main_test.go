package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

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
	writeFixture(t, root, "contracts/_meta/_types.yaml", `id: meta.types
type_families:
  string: { description: "string", params: {} }
custom_types: {}
`)
	shared, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "_meta", "_schemas.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "contracts/_meta/_schemas.yaml", string(shared))

}

func testNodeKinds() map[string]contractspec.SchemaKind {
	return map[string]contractspec.SchemaKind{
		"object": {Keys: []contractspec.Field{{Name: "type"}, {Name: "properties"}}},
		"array":  {Keys: []contractspec.Field{{Name: "type"}, {Name: "items"}}},
		"scalar": {Keys: []contractspec.Field{{Name: "type"}}},
		"ref":    {Keys: []contractspec.Field{{Name: "ref"}}},
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
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
base_path: /v1
paths:
  /x:
    get:
      operationId: getX
      x-contract-id: cap.protocols.http.x.get
      x-service: cap.services.x.get
      responses:
        "200":
          description: OK
components:
  schemas:
    X:
      x-service-type: cap.services.x.schemas.X
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
schemas:
  X:
    type: object
    properties:
      id:
        type: integer
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
	for _, want := range []string{"openapi: 3.1.0", "getX", "x-service", "properties", "id:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated spec missing %q:\n%s", want, out)
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
    includes: [cap.protocols.http.a, cap.protocols.http.b, cap.protocols.http.shared]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/a.yaml", `id: cap.protocols.http.a
paths:
  /a:
    get:
      operationId: getA
      x-contract-id: cap.protocols.http.a.get
      x-service: cap.services.x.get
      responses:
        "200": { description: OK }
default_error_responses: [422]
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/b.yaml", `id: cap.protocols.http.b
paths:
  /b:
    get:
      operationId: getB
      x-contract-id: cap.protocols.http.b.get
      x-service: cap.services.x.get
      responses:
        "200": { description: OK }
default_error_responses: [423]
`)
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
	if !strings.Contains(a, `"422"`) || strings.Contains(a, `"423"`) {
		t.Fatalf("file a defaults wrong:\n%s", a)
	}
	if !strings.Contains(b, `"423"`) || strings.Contains(b, `"422"`) {
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
    includes: [cap.protocols.http.a, cap.protocols.http.shared]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/a.yaml", `id: cap.protocols.http.a
paths:
  /a:
    get:
      operationId: getA
      x-contract-id: cap.protocols.http.a.get
      x-service: cap.services.x.get
      x-default-errors: [423]
      responses:
        "200": { description: OK }
default_error_responses: [422]
`)
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
	if !strings.Contains(paths, `"423"`) || strings.Contains(paths, `"422"`) {
		t.Fatalf("operation override not applied:\n%s", out)
	}
}

func TestRenderSchemaAnnotations(t *testing.T) {
	obj := map[string]any{
		"type": "object",
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

	list := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	out, err = renderSchemaNode(list, nil, nil, map[string]bool{}, testNodeKinds())
	if err != nil {
		t.Fatal(err)
	}
	item := asMap(out["items"])
	if item["type"] != "string" {
		t.Fatalf("array items lost: %#v", item)
	}
}
