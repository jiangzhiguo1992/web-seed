package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestBuildExpandsServiceTypes(t *testing.T) {
	root := t.TempDir()
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
      x-service-type: cap.services.x.types.X
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
types:
  X:
    type: object
    additionalProperties: false
    properties:
      id:
        type: integer
services: []
`)
	writeFixture(t, root, "contracts/data/enums.yaml", `id: data.enums
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
types: {}
services: []
`)
	writeFixture(t, root, "contracts/data/enums.yaml", `id: data.enums
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
