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
    includes: [cap.http.x]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.http.x
base_path: /v1
paths:
  /x:
    get:
      operationId: getX
      x-contract-id: cap.http.x.get
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
