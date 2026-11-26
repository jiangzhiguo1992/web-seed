package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
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

func TestLoadTableColumnsResolvesContractRefs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"_base.yaml": `id: data.tables.base
columns:
  - { name: id, type: _types.basics.integer.64, nullable: false, primary_key: true }
`,
		"source.yaml": `id: data.tables.source
columns:
  - { name: code, type: _types.basics.string, nullable: false, max_len: 16, pattern: "^[a-z]+$" }
`,
		"ref.yaml": `id: data.tables.ref
columns:
  - { name: copy_code, type: data.tables.source.col.code, nullable: true }
  - { name: base_id, type: data.tables.base.col.id, nullable: false }
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	columns, err := loadTableColumns(root)
	if err != nil {
		t.Fatal(err)
	}
	copyCode := columns["data.tables.ref.col.copy_code"]
	if copyCode.typ != "_types.basics.string" || copyCode.params["max_len"] != 16 || copyCode.params["pattern"] != "^[a-z]+$" {
		t.Fatalf("source ref = %+v", copyCode)
	}
	baseID := columns["data.tables.ref.col.base_id"]
	if baseID.typ != "_types.basics.integer.64" {
		t.Fatalf("base ref = %+v", baseID)
	}
}

func TestTypeSchemaContainers(t *testing.T) {
	set := typeSchema("_types.structures.set<_types.basics.string>", nil, nil)
	if set["type"] != "array" || set["uniqueItems"] != true {
		t.Fatalf("set schema = %#v", set)
	}
	mapping := typeSchema("_types.structures.map<_types.basics.string, _types.basics.integer.32>", nil, nil)
	if mapping["type"] != "object" {
		t.Fatalf("map schema = %#v", mapping)
	}
	values := asMap(mapping["additionalProperties"])
	if values["type"] != "integer" {
		t.Fatalf("map value schema = %#v", values)
	}
}

func TestScalarSchemaPolymorphicTypes(t *testing.T) {
	bit := scalarSchema("_types.basics.bit", nil)
	if bit["type"] != "integer" {
		t.Fatalf("bit schema = %#v, want integer", bit)
	}
	anyValue := scalarSchema("_types.basics.any", nil)
	if len(anyValue) != 0 {
		t.Fatalf("any schema = %#v, want unconstrained", anyValue)
	}
}

func TestEntitySchemaInheritsColumnParams(t *testing.T) {
	columns := map[string]columnMeta{
		"data.tables.x.col.name": {
			typ:    "_types.basics.string",
			params: map[string]any{"min_len": 1, "max_len": 255, "pattern": "^[a-z]+$"},
		},
	}
	fields := []any{map[string]any{"name": "Name", "source": map[string]any{"column": "data.tables.x.col.name"}}}
	schema := entitySchema(fields, nil, columns, nil)
	props, _ := schema["properties"].(map[string]any)
	name, _ := props["name"].(map[string]any)
	if name["minLength"] != 1 || name["maxLength"] != 255 || name["pattern"] != "^[a-z]+$" {
		t.Fatalf("column params not projected: %#v", name)
	}
}

func TestEntitySchemaExcludesInheritedSecretColumns(t *testing.T) {
	columns := map[string]columnMeta{
		"data.tables.x.col.secret_value": {typ: "_types.basics.string", sensitivity: "secret"},
		"data.tables.x.col.public_value": {typ: "_types.basics.string"},
	}
	fields := []any{
		map[string]any{"name": "SecretValue", "source": map[string]any{"column": "data.tables.x.col.secret_value"}},
		map[string]any{"name": "PublicValue", "source": map[string]any{"column": "data.tables.x.col.public_value"}},
	}
	schema := entitySchema(fields, nil, columns, nil)
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["secret_value"]; ok {
		t.Fatalf("inherited secret column leaked into schema: %#v", props)
	}
	if _, ok := props["public_value"]; !ok {
		t.Fatalf("public column missing: %#v", props)
	}
}

func TestResolveColumnMetaRefs(t *testing.T) {
	columns := map[string]columnMeta{
		"data.tables.sample.col.id":      {typ: "_types.basics.integer.64", params: map[string]any{"max_len": 10, "default": 5}},
		"data.tables.sample.col.copy_id": {typ: "data.tables.sample.col.id", nullable: true, params: map[string]any{"default": 7}},
	}
	if err := resolveColumnMetaRefs(columns); err != nil {
		t.Fatal(err)
	}
	got := columns["data.tables.sample.col.copy_id"]
	if got.typ != "_types.basics.integer.64" || !got.nullable {
		t.Fatalf("resolved column meta = %+v", got)
	}
	if got.params["max_len"] != 10 || got.params["default"] != 7 {
		t.Fatalf("resolved params = %#v", got.params)
	}

	cycle := map[string]columnMeta{
		"data.tables.cycle.col.a": {typ: "data.tables.cycle.col.b"},
		"data.tables.cycle.col.b": {typ: "data.tables.cycle.col.a"},
	}
	if err := resolveColumnMetaRefs(cycle); err == nil || !strings.Contains(err.Error(), "cyclic column ref") {
		t.Fatalf("cycle must be rejected, got %v", err)
	}
}

func forbiddenSourceKey(v any) (string, bool) {
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			if (strings.HasPrefix(key, "x-") && key != "x-error-codes" && key != "x-response-msg") || key == "source" || key == "contract" {
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

func writeHTTPPolicy(t *testing.T, root, always, secured string) {
	t.Helper()
	writeFixture(t, root, "contracts/platform/http_conventions.yaml", fmt.Sprintf(`audiences: [admin]
error_responses:
  always: %s
  secured: %s
body_types:
  json: application/json
  form: application/x-www-form-urlencoded
  multipart: multipart/form-data
  text: text/plain
  binary: application/octet-stream
unbound_operations: []
`, always, secured))
}

func testNodeKinds() map[string]contractspec.SchemaKind {
	return map[string]contractspec.SchemaKind{
		"object": {Keys: []contractspec.Field{{Name: "type"}, {Name: "properties"}}},
		"array":  {Keys: []contractspec.Field{{Name: "type"}, {Name: "items"}}},
		"map":    {Keys: []contractspec.Field{{Name: "type"}, {Name: "values"}}},
		"scalar": {Keys: []contractspec.Field{{Name: "type"}}},
		"ref":    {Keys: []contractspec.Field{{Name: "type"}}},
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
	writeHTTPPolicy(t, root, "[]", "[]")
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    api_version: v1
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
mount:
  api_version: v1
  path: /
  audience: [admin]
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
          msg: "OK"
          headers:
            - { name: Content-Type, required: true, value: application/json, description: "响应体媒体类型" }
          body:
            kind: json
            type: cap.services.x.schemas.X`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
schemas:
  X:
    kind: _types.schemas.object
    properties:
      - { name: id, schema: { type: _types.basics.integer }, required: true, description: "id" }
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

func TestBuildAppsShareFiles(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeFixture(t, root, "contracts/platform/http_conventions.yaml", `audiences: [admin]
body_types:
  json: application/json
  form: application/x-www-form-urlencoded
  multipart: multipart/form-data
  text: text/plain
  binary: application/octet-stream
error_responses: { always: [], secured: [] }
components:
  responses:
    Conflict: { description: "409" }
`)
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  v1:
    kind: api
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/v1.yaml
  v2:
    kind: api
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/v2.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
mount:
  api_version: v1
  path: /
  audience: [admin]
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
          msg: "OK"
        "409":
          msg: "conflict"
          errors:
            - platform.errors.X
`)
	writeFixture(t, root, "contracts/platform/errors/x.yaml", `id: platform.errors.x
errors:
  - { code: X, biz_code: 9001, status: 409, message: "conflict" }
`)
	for _, dir := range []string{"contracts/capabilities/services", "contracts/data/entities"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"generated/openapi/v1.yaml", "generated/openapi/v2.yaml"} {
		out := string(files[key])
		if !strings.Contains(out, "/x:") {
			t.Fatalf("%s missing mounted path /x:\n%s", key, out)
		}
		if !strings.Contains(out, "url: /v1") {
			t.Fatalf("%s missing mount-derived server /v1:\n%s", key, out)
		}
		if !strings.Contains(out, "getX") {
			t.Fatalf("%s missing shared operation:\n%s", key, out)
		}
		if !strings.Contains(out, "biz_code: 9001") {
			t.Fatalf("%s missing shared error codes:\n%s", key, out)
		}
	}
}

func TestBuildEventPayloads(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeHTTPPolicy(t, root, "[]", "[]")
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    api_version: v1
    audience: admin
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
        kind: _types.schemas.object
        properties:
          - { name: id, schema: { type: _types.basics.integer }, required: true, description: "id" }
          - { name: account, schema: { type: data.entities.account }, required: true, description: "account" }
          - name: attributes
            schema:
              kind: _types.schemas.map
              values: { type: _types.basics.string }
              min_items: 1
              max_items: 5
              key_patterns: [ '^[a-z][a-z0-9_]*$' ]
            description: "attrs"
      emitted_by: []
`)
	writeFixture(t, root, "contracts/data/entities/account.yaml", `id: data.entities.account
fields:
  - { name: id, json_name: id, type: _types.basics.integer, description: "id" }
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

func TestBuildRejectsMixedMountVersions(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeHTTPPolicy(t, root, "[]", "[]")
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.a, cap.protocols.http.b]
    generated:
      spec: generated/openapi/x.yaml
`)
	for _, item := range []struct{ name, version string }{{"a", "v1"}, {"b", "v2"}} {
		writeFixture(t, root, "contracts/capabilities/protocols/http/"+item.name+`.yaml`, `id: "cap.protocols.http.`+item.name+`"
mount:
  api_version: `+item.version+`
  path: /
  audience: [admin]
paths:
  /`+item.name+`:
    get:
      name: "get`+strings.ToUpper(item.name)+`"
      id: "cap.protocols.http.`+item.name+`.get"
      service: "cap.services.x.get"
      request: {}
      response:
        "204": { msg: "OK" }
`)
	}
	for _, dir := range []string{"contracts/capabilities/services", "contracts/data/enums", "contracts/data/entities"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, err := build(root)
	if err == nil || !strings.Contains(err.Error(), "includes mix mount.api_version") {
		t.Fatalf("mixed mount versions not rejected: %v", err)
	}
}

func TestGeneratedSpecsOmitTransportHeaders(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "contracts", "generated", "openapi")
	for _, name := range []string{"admin.yaml", "user.yaml"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		out := string(b)
		for _, forbidden := range []string{"name: Connection", "name: Content-Length", "name: Set-Cookie", "Set-Cookie:"} {
			if strings.Contains(out, forbidden) {
				t.Fatalf("%s contains transport header %q", name, forbidden)
			}
		}
	}
}

func TestBuildTextResponse(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeHTTPPolicy(t, root, "[]", "[]")
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
mount:
  api_version: v1
  path: /
  audience: [admin]
paths:
  /x:
    get:
      name: getX
      id: cap.protocols.http.x.get
      service: cap.services.x.get
      request: {}
      response:
        "200":
          msg: "OK"
          headers:
            - { name: Content-Type, required: true, value: text/plain, description: "body" }
          body:
            kind: text
            type: _types.basics.string
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
	if !strings.Contains(out, "text/plain") {
		t.Fatalf("text response media type missing:\n%s", out)
	}
	if strings.Contains(out, "ApiStringResponse") {
		t.Fatalf("text response must not be wrapped in JSON envelope:\n%s", out)
	}
}

func TestBuildFormBody(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeHTTPPolicy(t, root, "[]", "[]")
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    api_version: v1
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
mount:
  api_version: v1
  path: /
  audience: [admin]
paths:
  /x:
    post:
      name: createX
      id: cap.protocols.http.x.create
      service: cap.services.x.create
      request:
        headers:
          - { name: Content-Type, required: true, value: application/x-www-form-urlencoded, description: "body" }
        body:
          kind: form
          fields:
            - { name: username, type: _types.basics.string, required: true, description: "name", to: params.input.username }
      response:
        "204":
          msg: "OK"
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
schemas:
  Input:
    kind: _types.schemas.object
    properties:
      - { name: username, schema: { type: _types.basics.string }, required: true, description: "name" }
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
	for _, want := range []string{"application/x-www-form-urlencoded", "username", "required:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated form spec missing %q:\n%s", want, out)
		}
	}
}

func TestBuildMultipartBody(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeHTTPPolicy(t, root, "[]", "[]")
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    api_version: v1
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.x]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
mount:
  api_version: v1
  path: /
  audience: [admin]
paths:
  /x:
    post:
      name: createX
      id: cap.protocols.http.x.create
      service: cap.services.x.create
      request:
        headers:
          - { name: Content-Type, required: true, value: multipart/form-data, description: "body" }
        body:
          kind: multipart
          fields:
            - { name: avatar, kind: file, required: false, description: "file", to: params.input.avatar }
      response:
        "204":
          msg: "OK"
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
	for _, want := range []string{"multipart/form-data", "avatar", "format: binary"} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated multipart spec missing %q:\n%s", want, out)
		}
	}
}

func TestBuildInjectsPlatformBaseline(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeFixture(t, root, "contracts/platform/http_conventions.yaml", `audiences: [admin]
body_types:
  json: application/json
  form: application/x-www-form-urlencoded
  multipart: multipart/form-data
  text: text/plain
  binary: application/octet-stream
error_responses:
  always: [400, 429, 500, 503]
  secured: [401, 403]
components:
  securitySchemes:
    Bearer: { type: http, scheme: bearer }
  responses:
    BadRequest: { description: "400" }
    Unauthorized: { description: "401" }
    Forbidden: { description: "403" }
    TooManyRequests: { description: "429" }
    Internal: { description: "500" }
    ServiceUnavailable: { description: "503" }
`)
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.a, cap.protocols.http.b]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/a.yaml", `id: "cap.protocols.http.a"
mount:
  api_version: v1
  path: /
  audience: [admin]
paths:
  /a:
    get:
      name: "getA"
      id: "cap.protocols.http.a.get"
      service: "cap.services.x.get"
      request:
        headers:
          - { name: Authorization, required: true, auth: Bearer, description: "JWT access token" }
      response:
        "200":
          msg: "OK"
`)
	writeFixture(t, root, "contracts/platform/errors/x.yaml", `id: platform.errors.x
errors:
  - { code: X, biz_code: 9001, status: 409, message: "conflict" }
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/b.yaml", `id: "cap.protocols.http.b"
mount:
  api_version: v1
  path: /
  audience: [admin]
paths:
  /b:
    get:
      name: "getB"
      id: "cap.protocols.http.b.get"
      service: "cap.services.x.get"
      request: {}
      response:
        "200":
          msg: "OK"
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
	for _, want := range []string{"401:", "403:", "503:"} {
		if !strings.Contains(a, want) {
			t.Fatalf("secured operation missing %s:\n%s", want, a)
		}
	}
	for _, want := range []string{"400:", "429:", "500:", "503:"} {
		if !strings.Contains(b, want) {
			t.Fatalf("public operation missing %s:\n%s", want, b)
		}
	}
	if strings.Contains(b, "401:") || strings.Contains(b, "403:") {
		t.Fatalf("public operation must not get secured baseline:\n%s", b)
	}
}

func TestBuildInjectsPlatformComponents(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeFixture(t, root, "contracts/platform/http_conventions.yaml", `audiences: [admin]
body_types:
  json: application/json
  form: application/x-www-form-urlencoded
  multipart: multipart/form-data
  text: text/plain
  binary: application/octet-stream
error_responses:
  always: [409]
components:
  securitySchemes:
    Bearer: { type: http, scheme: bearer }
  schemas:
    ApiErrorResponse:
      type: object
      additionalProperties: false
      properties:
        code: { type: integer }
  responses:
    Conflict:
      description: "409"
      content:
        application/json:
          schema: { $ref: "#/components/schemas/ApiErrorResponse" }
`)
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.a]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/a.yaml", `id: "cap.protocols.http.a"
mount:
  api_version: v1
  path: /
  audience: [admin]
paths:
  /a:
    get:
      name: getA
      id: cap.protocols.http.a.get
      service: cap.services.x.get
      summary: "a"
      used_by: [feature.pages.x]
      request:
        headers:
          - { name: Authorization, required: true, auth: Bearer, description: "JWT access token" }
      response:
        "200":
          msg: "OK"
          headers:
            - { name: Content-Type, required: true, value: application/json, description: "响应体媒体类型" }
          body:
            kind: json
            type: cap.services.x.schemas.X
        "409":
          msg: "conflict"
          errors: [platform.errors.X]
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
schemas:
  X:
    kind: _types.schemas.object
    properties:
      - { name: id, schema: { type: _types.basics.integer }, required: true, description: "id" }
services: []
`)
	writeFixture(t, root, "contracts/platform/errors/x.yaml", `id: platform.errors.x
errors:
  - { code: X, biz_code: 9001, status: 409, message: "conflict" }
`)
	for _, dir := range []string{"contracts/data/enums", "contracts/data/entities"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	out := string(files["generated/openapi/x.yaml"])
	for _, want := range []string{
		"/a:",
		"Bearer:",
		"ApiErrorResponse:",
		"Conflict:",
		"url: /v1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated spec missing %q:\n%s", want, out)
		}
	}
}

func TestBuildEmitsErrorCodes(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeFixture(t, root, "contracts/platform/http_conventions.yaml", `audiences: [admin]
body_types:
  json: application/json
  form: application/x-www-form-urlencoded
  multipart: multipart/form-data
  text: text/plain
  binary: application/octet-stream
error_responses: { always: [], secured: [] }
components:
  responses:
    Conflict: { description: "409" }
`)
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  api:
    kind: api
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.a]
    generated:
      spec: generated/openapi/x.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/a.yaml", `id: "cap.protocols.http.a"
mount:
  api_version: v1
  path: /
  audience: [admin]
paths:
  /a:
    post:
      name: "createA"
      id: "cap.protocols.http.a.create"
      service: "cap.services.x.create"
      request: {}
      response:
        "204":
          msg: "OK"
        "409":
          msg: "conflict"
          errors:
            - platform.errors.ACCOUNT_NUMBER_TAKEN
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
schemas: {}
services: []
`)
	writeFixture(t, root, "contracts/data/enums/account.yaml", `id: data.enums
enums: []
`)
	writeFixture(t, root, "contracts/platform/errors/account.yaml", `id: platform.errors.account
errors:
  - { code: ACCOUNT_NUMBER_TAKEN, biz_code: 2002, status: 409, message: "taken" }
`)
	if err := os.MkdirAll(filepath.Join(root, "contracts/data/entities"), 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	out := string(files["generated/openapi/x.yaml"])
	for _, want := range []string{"x-response-msg: conflict", "x-error-codes:", "code: ACCOUNT_NUMBER_TAKEN", "biz_code: 2002", "msg: taken"} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated spec missing %q:\n%s", want, out)
		}
	}
}

func TestBuildFiltersByAudience(t *testing.T) {
	root := t.TempDir()
	writeTypes(t, root)
	writeHTTPPolicy(t, root, "[]", "[]")
	writeFixture(t, root, "contracts/project.yaml", `project:
  name: test
  version: "1"
apps:
  admin:
    kind: api
    api_version: v1
    audience: admin
    timeout_ms: 5000
    includes: [cap.protocols.http.a, cap.protocols.http.b]
    generated:
      spec: generated/openapi/admin.yaml
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/a.yaml", `id: "cap.protocols.http.a"
mount:
  api_version: v1
  path: /
  audience: [admin]
paths:
  /admin:
    get:
      name: "adminOnly"
      id: "cap.protocols.http.a.admin"
      service: "cap.services.x.get"
      request: {}
      response:
        "204":
          msg: "OK"
`)
	writeFixture(t, root, "contracts/capabilities/protocols/http/b.yaml", `id: "cap.protocols.http.b"
mount:
  api_version: v1
  path: /
  audience: [user]
paths:
  /user:
    get:
      name: "userOnly"
      id: "cap.protocols.http.b.user"
      service: "cap.services.x.get"
      request: {}
      response:
        "204":
          msg: "OK"
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
	out := string(files["generated/openapi/admin.yaml"])
	if !strings.Contains(out, "adminOnly") {
		t.Fatalf("admin operation filtered out:\n%s", out)
	}
	if strings.Contains(out, "userOnly") || strings.Contains(out, "/user:") {
		t.Fatalf("user operation leaked into admin spec:\n%s", out)
	}
}

func TestRenderSchemaAnnotations(t *testing.T) {
	obj := map[string]any{
		"kind": "_types.schemas.object",
		"properties": []any{
			map[string]any{
				"name":        "id",
				"schema":      map[string]any{"type": "_types.basics.string"},
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

	list := map[string]any{"kind": "_types.schemas.array", "items": map[string]any{"type": "_types.basics.string"}}
	out, err = renderSchemaNode(list, nil, nil, map[string]bool{}, testNodeKinds())
	if err != nil {
		t.Fatal(err)
	}
	item := asMap(out["items"])
	if item["type"] != "string" {
		t.Fatalf("array items lost: %#v", item)
	}

	m := map[string]any{"kind": "_types.schemas.map", "values": map[string]any{"type": "_types.basics.string"}}
	out, err = renderSchemaNode(m, nil, nil, map[string]bool{}, testNodeKinds())
	if err != nil {
		t.Fatal(err)
	}
	ap := asMap(out["additionalProperties"])
	if out["type"] != "object" || ap["type"] != "string" {
		t.Fatalf("map values lost: %#v", out)
	}
}

func TestGeneratedSpecsValidateWithKinOpenAPI(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	loader := openapi3.NewLoader()
	for name, body := range files {
		doc, err := loader.LoadFromData(body)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := doc.Validate(context.Background()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestScalarParamsIncludeNormalize(t *testing.T) {
	out := map[string]any{}
	applyScalarParams(out, map[string]any{"normalize": "upper", "min_len": 1})
	if out["x-normalize"] != "upper" {
		t.Fatalf("normalize not emitted: %#v", out)
	}
	if out["minLength"] != 1 {
		t.Fatalf("minLength not emitted: %#v", out)
	}
}
