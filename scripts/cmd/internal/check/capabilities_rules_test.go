package check

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func TestServiceCycle(t *testing.T) {
	c := &Checker{}
	c.checkServiceCycles(map[string][]string{
		"cap.services.a.get": {"cap.services.b.get"},
		"cap.services.b.get": {"cap.services.c.get"},
		"cap.services.c.get": {},
	})
	if len(c.errs) != 0 {
		t.Fatalf("acyclic services reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceCycles(map[string][]string{
		"cap.services.a.get": {"cap.services.b.get"},
		"cap.services.b.get": {"cap.services.a.get"},
	})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-cycle]") {
		t.Fatalf("cycle not reported: %v", c.errs)
	}
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "cap.services.a.get -> cap.services.b.get -> cap.services.a.get") &&
		!strings.Contains(errs, "cap.services.b.get -> cap.services.a.get -> cap.services.b.get") {
		t.Fatalf("cycle path not reported: %v", c.errs)
	}
}

func TestServiceParamExpr(t *testing.T) {
	doc := map[string]any{
		"params": []any{
			map[string]any{"name": "id", "type": "integer"},
		},
	}
	stepIndex := map[string]int{"load": 0}
	steps := []any{
		map[string]any{
			"id":   "load",
			"kind": "logic",
			"output": []any{
				map[string]any{"name": "account", "type": "data.entities.account"},
			},
		},
	}
	stepOutputs := []map[string]returnType{
		{"account": {typ: "data.entities.account"}},
	}
	c := &Checker{
		entities: map[string]*entityInfo{
			"data.entities.account": {Fields: []entityFieldInfo{
				{Name: "ID", JSONName: "id", Type: "integer"},
			}},
		},
	}
	allIndex := stepIndex
	reach := buildStepReach(steps, allIndex)
	c.checkServiceParamExpr("params.id", doc, stepIndex, allIndex, reach, stepOutputs, steps, "f", "s")
	c.checkServiceParamExpr("step.load.account.id", doc, stepIndex, allIndex, reach, stepOutputs, steps, "f", "s")
	if len(c.errs) != 0 {
		t.Fatalf("valid params reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceParamExpr("params.missing", doc, stepIndex, allIndex, reach, stepOutputs, steps, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-input-ref]") {
		t.Fatalf("unknown input not reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceParamExpr("step.load.missing", doc, stepIndex, allIndex, reach, stepOutputs, steps, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-input-ref]") {
		t.Fatalf("unknown step field not reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceParamExpr("step.after.x", doc, stepIndex, allIndex, reach, stepOutputs, steps, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-input-order]") {
		t.Fatalf("later step reference not reported: %v", c.errs)
	}
}

func TestServiceOutputCoversReturns(t *testing.T) {
	c := &Checker{errCodeSet: map[string]bool{"COMMON_INTERNAL": true}}
	doc := func(output []any) map[string]any {
		return map[string]any{
			"summary": "x",
			"tx_mode": "none",
			"params":  []any{},
			"returns": []any{
				map[string]any{"name": "account", "type": "any", "value": "step.create.account"},
			},
			"steps": []any{
				map[string]any{
					"id":      "create",
					"kind":    "logic",
					"summary": "x",
					"rules":   []any{},
					"errors":  []any{"platform.errors.COMMON_INTERNAL"},
					"output":  output,
					"goto":    "$end",
				},
			},
		}
	}
	c.checkServiceItem(doc([]any{map[string]any{"name": "account", "type": "any"}}), "f", "cap.services.x.create", "x", map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("covered output reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceItem(doc([]any{}), "f", "cap.services.x.create", "x", map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown step output account") {
		t.Fatalf("missing return not reported: %v", c.errs)
	}
}

func writeCapFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckCapabilitiesServiceCycle(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/_meta/_types.yaml", `id: meta.types
type_families:
  string: { description: "string", params: {} }
`)
	writeCapFile(t, root, "contracts/_meta/_types.yaml", string(mustRead(t, filepath.Join("..", "..", "..", "..", "contracts", "_meta", "_types.yaml"))))
	writeCapFile(t, root, "contracts/_meta/cap.services.yaml", `id: meta.cap.services
type: cap.services
targets:
  dir: contracts/capabilities/services
  glob: "*.yaml"
  id: "cap.services.<file>"
fields:
  - {name: id, type: _types.string, description: "id"}
  - {name: summary, type: _types.string, description: "summary"}
  - {name: owner_module, type: _types.string, description: "owner"}
  - {name: schema_version, type: _types.integer, description: "version"}
  - {name: stability, type: _types.string, description: "stability"}
  - {name: do_not, type: _types.list<_types.string>, description: "do_not"}
  - {name: interface, type: _types.any, description: "interface"}
  - {name: persistence, type: _types.any, description: "persistence"}
  - {name: services, type: _types.any, description: "services"}
`)
	for _, typ := range []string{"feature.rules", "cap.lifecycles", "cap.events", "cap.protocols.http"} {
		writeCapFile(t, root, "contracts/_meta/"+typ+".yaml", "id: meta."+typ+"\ntype: "+typ+`
targets:
  dir: contracts/features/unused
  glob: "*.yaml"
  id: "`+typ+`.<file>"
fields:
  - {name: id, type: _types.string, description: "id"}
`)
	}
	writeCapFile(t, root, "contracts/capabilities/services/a.yaml", `id: cap.services.a
summary: "a"
owner_module: x
schema_version: 1
stability: draft
do_not: []
services:
  - name: get
    summary: "a"
    visibility: public
    tx_mode: read_only
    steps:
      - {id: call, kind: service, summary: "x", goto: "$end", service: {ref: cap.services.b.get}}
`)
	writeCapFile(t, root, "contracts/capabilities/services/b.yaml", `id: cap.services.b
summary: "b"
owner_module: x
schema_version: 1
stability: draft
do_not: []
services:
  - name: get
    summary: "b"
    visibility: public
    tx_mode: read_only
    steps:
      - {id: call, kind: service, summary: "x", goto: "$end", service: {ref: cap.services.a.get}}
`)

	c := &Checker{root: root}
	c.checkCapabilities()
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-cycle]") {
		t.Fatalf("service cycle not reported: %v", c.errs)
	}
}

func TestHTTPRequestBodyType(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    post:
      name: "createX"
      contract:
        id: "cap.protocols.http.x.create"
        service: "cap.services.x.create"
      request:
        body:
          content_type: "application/json"
          schema:
            $ref: "#/components/schemas/WrongInput"
          required: true
        mapping:
          body: "input"
      response:
        "204":
          description: "OK"
components:
  schemas:
    WrongInput:
      source:
        service_type: "cap.services.x.schemas.OtherInput"`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {
			visibility:     "public",
			params:         map[string]bool{"input": true},
			requiredParams: map[string]bool{"input": true},
			paramTypes:     map[string]bool{"cap.services.x.schemas.XInput": true},
			paramDecls:     map[string]string{"input": "cap.services.x.schemas.XInput"},
		},
	}, map[string]bool{"cap.services.x.schemas.OtherInput": true}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-service-type]") {
		t.Fatalf("requestBody type mismatch not reported: %v", c.errs)
	}
}

func TestHTTPRequestBodyTypeMultipleMedia(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    post:
      name: "createX"
      contract:
        id: "cap.protocols.http.x.create"
        service: "cap.services.x.create"
      request:
        body:
          content_type: "application/json"
          schema:
            $ref: "#/components/schemas/WrongInput"
          required: true
        mapping:
          body: "input"
      response:
        "204":
          description: "OK"
components:
  schemas:
    WrongInput:
      source:
        service_type: "cap.services.x.schemas.OtherInput"`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {
			visibility:     "public",
			params:         map[string]bool{"input": true},
			requiredParams: map[string]bool{"input": true},
			paramTypes:     map[string]bool{"cap.services.x.schemas.XInput": true},
			paramDecls:     map[string]string{"input": "cap.services.x.schemas.XInput"},
		},
	}, map[string]bool{"cap.services.x.schemas.OtherInput": true}, map[string]bool{})
	if got := strings.Count(strings.Join(c.errs, "\n"), "[http-service-type]"); got != 1 {
		t.Fatalf("want 1 http-service-type error for application/json, got %d: %v", got, c.errs)
	}
}

func TestHTTPRequestBodyTypeInlineSkipped(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    post:
      name: "createX"
      contract:
        id: "cap.protocols.http.x.create"
        service: "cap.services.x.create"
      request:
        headers:
          RequestId:
            $ref: "#/components/parameters/RequestId"
        body:
          content_type: "application/json"
          schema:
            type: "object"
            additionalProperties: false
            required:
              - "id"
            properties:
              id:
                type: "integer"
          required: true
        mapping:
          body:
            id: "id"
        idempotency: "none"
      response:
        "204":
          description: "OK"
          headers:
            X-Request-Id:
              $ref: "#/components/headers/XRequestId"
components:
  parameters:
    RequestId:
      name: "X-Request-Id"
      in: "header"
      required: false
      schema:
        type: "string"
  headers:
    XRequestId:
      schema:
        type: "string"
  schemas: {}`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {
			visibility:     "public",
			params:         map[string]bool{"id": true},
			requiredParams: map[string]bool{"id": true},
			paramDecls:     map[string]string{"id": "_types.integer"},
		},
	}, map[string]bool{}, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("anonymous requestBody reported: %v", c.errs)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func copyMeta(t *testing.T, root, typ string) *Schema {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	src := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "contracts", "_meta", typ+".yaml")
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	writeCapFile(t, root, "contracts/_meta/"+typ+".yaml", string(b))
	for _, name := range []string{"_types.yaml"} {
		if shared, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "contracts", "_meta", name)); err == nil {
			writeCapFile(t, root, "contracts/_meta/"+name, string(shared))
		}
	}
	s, _, err := contractspec.LoadSchemaByType(root, typ)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCheckRuleDoc(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "feature.rules")
	serviceIDs := map[string]bool{"cap.services.x.create": true}
	rel := "contracts/features/rules/x.yaml"
	valid := `id: feature.rules.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
rules:
  - id: feature.rules.x.r1
    kind: policy
    summary: "x"
    on_violation: platform.errors.COMMON_INTERNAL
    implemented_by: [cap.services.x.create]
`
	writeCapFile(t, root, rel, valid)
	doc, err := loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, errCodeSet: map[string]bool{"COMMON_INTERNAL": true}}
	c.checkRuleDoc(doc, rel, schema, serviceIDs)
	if len(c.errs) != 0 {
		t.Fatalf("valid rule reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, `id: feature.rules.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
rules:
  - id: feature.rules.x.r2
    kind: constraint
    summary: "x"
    on_violation: platform.errors.COMMON_INTERNAL
    implemented_by: [cap.services.x.create]
`)
	doc, err = loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c.errs = nil
	c.checkRuleDoc(doc, rel, schema, serviceIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[rule-structure]") {
		t.Fatalf("constraint without field/op/value not reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, `id: feature.rules.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
rules:
  - id: feature.rules.x.r3
    kind: policy
    summary: "x"
    on_violation: platform.errors.COMMON_INTERNAL
    implemented_by: [cap.services.missing.create]
`)
	doc, err = loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c.errs = nil
	c.checkRuleDoc(doc, rel, schema, serviceIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown service cap.services.missing.create") {
		t.Fatalf("unknown implemented_by service not reported: %v", c.errs)
	}
}

func TestCheckLifecycleDoc(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.lifecycles")
	rel := "contracts/capabilities/lifecycles/x.yaml"
	stepIDs := map[string]bool{
		"cap.services.x.create.create":       true,
		"cap.services.x.delete.soft_delete":  true,
		"cap.services.x.activate.transition": true,
	}
	actionSteps := map[string][]string{
		"cap.lifecycles.x.create":               {"cap.services.x.create.create"},
		"cap.lifecycles.x.destroy":              {"cap.services.x.delete.soft_delete"},
		"cap.lifecycles.x.transitions.activate": {"cap.services.x.activate.transition"},
	}
	valid := `id: cap.lifecycles.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
subjects:
  - { entity: data.entities.x, field: status }
states:
  values: [pending, active, deleted]
  initial: pending
  final: [deleted]
create:
  implemented_by: [cap.services.x.create.create]
destroy:
  implemented_by: [cap.services.x.delete.soft_delete]
transitions:
  - name: activate
    description: "a"
    from: [pending]
    to: active
    implemented_by: [cap.services.x.activate.transition]
`
	writeCapFile(t, root, rel, valid)
	doc, err := loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{
		root:       root,
		errCodeSet: map[string]bool{"COMMON_INTERNAL": true},
		enumValues: map[string]map[string]bool{"data.enums.x_status": {"pending": true, "active": true, "deleted": true}},
		entities:   map[string]*entityInfo{"data.entities.x": {OwnerModule: "x", Primary: "data.tables.x", Fields: []entityFieldInfo{{Name: "Status", JSONName: "status", Type: "data.enums.x_status", SourceColumnRef: "data.tables.x.col.status"}}}},
	}
	c.checkLifecycleDoc(doc, rel, schema, stepIDs, actionSteps, map[string]map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid lifecycle reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "initial: pending", "initial: missing", 1))
	doc, err = loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c.errs = nil
	c.checkLifecycleDoc(doc, rel, schema, stepIDs, actionSteps, map[string]map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[lifecycle-structure]") {
		t.Fatalf("unknown initial state not reported: %v", c.errs)
	}
}

func TestServiceCycleMultiple(t *testing.T) {
	c := &Checker{}
	c.checkServiceCycles(map[string][]string{
		"a": {"b"}, "b": {"a"},
		"c": {"d"}, "d": {"c"},
	})
	if got := strings.Count(strings.Join(c.errs, "\n"), "[service-cycle]"); got != 2 {
		t.Fatalf("want 2 cycles, got %d: %v", got, c.errs)
	}
}

func TestHTTPComponentFields(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths: {}
components:
  schemas:
    X:
      source:
        service_type: "cap.services.x.schemas.X"
      type: "object"
      properties:
        id:
          type: "_types.string"`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {
			visibility: "public",
			paramTypes: map[string]bool{},
			typeSchemas: map[string]map[string]any{
				"cap.services.x.schemas.X": {"properties": map[string]any{"id": map[string]any{"type": "_types.integer"}}},
			},
		},
	}, map[string]bool{"cap.services.x.schemas.X": true}, map[string]bool{})
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, "[http-components]") {
		t.Fatalf("component properties not reported: %v", c.errs)
	}
	if strings.Contains(got, "[http-service-type]") {
		t.Fatalf("component properties must not be compared as a service type: %v", c.errs)
	}
}

func TestHTTPSharedValidation(t *testing.T) {
	c := &Checker{}
	c.checkHTTPShared(map[string]any{
		"shared": map[string]any{
			"securitySchemes": map[string]any{
				"BadHTTP": map[string]any{"type": "http"},
				"BadKey":  map[string]any{"type": "apiKey", "in": "path"},
				"BadOAuth": map[string]any{
					"type":  "oauth2",
					"flows": map[string]any{"authorizationCode": map[string]any{}},
				},
			},
			"responses": map[string]any{
				"NoDescription": map[string]any{},
				"NoSchema": map[string]any{
					"description": "x",
					"content":     map[string]any{"application/json": map[string]any{}},
				},
			},
			"parameters": map[string]any{
				"BadParam": map[string]any{"name": "id", "in": "path"},
			},
			"headers": map[string]any{
				"BadHeader": map[string]any{},
			},
			"schemas": map[string]any{
				"NoSource": map[string]any{"type": "object"},
				"Both": map[string]any{
					"source": map[string]any{"enum": "data.enums.x", "entity": "data.entities.x"},
				},
			},
		},
	}, "x.yaml")
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, "[http-shared]") {
		t.Fatalf("shared errors not tagged: %v", c.errs)
	}
	for _, want := range []string{
		"BadHTTP.scheme required",
		"BadKey.name required",
		"BadKey.in must be",
		"BadOAuth.flows.authorizationCode.authorizationUrl required",
		"BadOAuth.flows.authorizationCode.tokenUrl required",
		"BadOAuth.flows.authorizationCode.scopes required",
		"NoDescription.description required",
		"NoSchema.content.application/json.schema required",
		"BadParam.schema required",
		"BadParam.required must be true",
		"BadHeader.schema required",
		"NoSource.source required",
		"Both.source must declare exactly one source key",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %v", want, c.errs)
		}
	}
}

func TestHTTPVersion(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /v1/x:
    get:
      name: "getX"
      contract:
        id: "cap.protocols.http.x.get"
        service: "cap.services.x.get"
      request:
        mapping: {}
      response:
        "204":
          description: "OK"
components:
  schemas: {}`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {visibility: "public"},
	}, map[string]bool{}, map[string]bool{})
	if got := strings.Count(strings.Join(c.errs, "\n"), "[http-version]"); got != 1 {
		t.Fatalf("want 1 http-version error, got %d: %v", got, c.errs)
	}
}

func TestHTTPServiceParams(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    get:
      name: "getX"
      contract:
        id: "cap.protocols.http.x.get"
        service: "cap.services.x.get"
      request:
        query:
          id:
            schema:
              type: "integer"
            required: false
        mapping:
          query:
            id: "missing"
      response:
        "204":
          description: "OK"
components:
  schemas: {}`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {visibility: "public"},
	}, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-service-params]") {
		t.Fatalf("unknown service field not reported: %v", c.errs)
	}
}

func TestHTTPErrors(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    get:
      name: "getX"
      contract:
        id: "cap.protocols.http.x.get"
        service: "cap.services.x.get"
      request:
        mapping: {}
      response:
        "204":
          description: "OK"
        "500":
          codes:
            - "platform.errors.COMMON_INTERNAL"
components:
  schemas: {}`)
	writeCapFile(t, root, "contracts/platform/errors/common.yaml", `id: platform.errors.common
errors:
  - { code: COMMON_INTERNAL, biz_code: 1007, status: 500 }
`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {
			visibility: "public",
			errors:     map[string]bool{"platform.errors.COMMON_INTERNAL": true},
		},
	}, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-errors]") {
		t.Fatalf("COMMON_* error not reported: %v", c.errs)
	}
}

func TestHTTPShape(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    get:
      name: "getX"
      contract:
        id: "cap.protocols.http.x.get"
        service: "cap.services.x.get"
      request: {}
      parameters: []
      responses:
        "204":
          description: "OK"
components:
  schemas: {}`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {visibility: "public"},
	}, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-shape]") {
		t.Fatalf("legacy shape not reported: %v", c.errs)
	}
}

func TestProjectAPIVersion(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/http_conventions.yaml", `audiences: [admin]`)
	writeCapFile(t, root, "contracts/project.yaml", `apps:
  good:
    kind: api
    api_version: v1
    audience: admin
    timeout_ms: 5000
    generated: { spec: generated/openapi/good.yaml }
  missing:
    kind: api
    timeout_ms: 5000
    generated: { spec: generated/openapi/missing.yaml }
  invalid:
    kind: api
    api_version: "1"
    audience: admin
    timeout_ms: 5000
    generated: { spec: generated/openapi/invalid.yaml }
  web:
    kind: web
    generated: { spec: ignored }
`)
	c := &Checker{root: root}
	c.checkProjectAPIVersion()
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, "app missing requires api_version") || !strings.Contains(got, "app invalid api_version") {
		t.Fatalf("project api_version not reported: %v", c.errs)
	}
	if strings.Contains(got, "app good") || strings.Contains(got, "app web") {
		t.Fatalf("valid/web app reported: %v", c.errs)
	}
}

func TestHTTPSecurity(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    get:
      name: "getX"
      security:
        - "UnknownAuth": []
      contract:
        id: "cap.protocols.http.x.get"
        service: "cap.services.x.get"
      request:
        headers:
          RequestId:
            $ref: "#/components/parameters/RequestId"
        mapping: {}
        idempotency: "none"
      response:
        "204":
          description: "OK"
          headers:
            X-Request-Id:
              $ref: "#/components/headers/XRequestId"
components:
  parameters:
    RequestId:
      name: "X-Request-Id"
      in: "header"
      required: false
      schema:
        type: "string"
  headers:
    XRequestId:
      schema:
        type: "string"
  schemas: {}`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {visibility: "public"},
	}, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-security]") {
		t.Fatalf("unknown security scheme not reported: %v", c.errs)
	}
}

func TestHTTPIdempotency(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    post:
      name: "createX"
      contract:
        id: "cap.protocols.http.x.create"
        service: "cap.services.x.create"
      request:
        headers:
          RequestId:
            $ref: "#/components/parameters/RequestId"
        mapping: {}
      response:
        "204":
          description: "OK"
          headers:
            X-Request-Id:
              $ref: "#/components/headers/XRequestId"
components:
  parameters:
    RequestId:
      name: "X-Request-Id"
      in: "header"
      required: false
      schema:
        type: "string"
  headers:
    XRequestId:
      schema:
        type: "string"
  schemas: {}`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {visibility: "public"},
	}, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-idempotency]") {
		t.Fatalf("missing x-idempotency not reported: %v", c.errs)
	}
}

func TestHTTPHeaders(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    get:
      name: "getX"
      contract:
        id: "cap.protocols.http.x.get"
        service: "cap.services.x.get"
      request:
        mapping: {}
        idempotency: "none"
      response:
        "204":
          description: "OK"
components:
  schemas: {}`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {visibility: "public"},
	}, map[string]bool{}, map[string]bool{})
	if got := strings.Count(strings.Join(c.errs, "\n"), "[http-headers]"); got < 2 {
		t.Fatalf("want request and response header errors, got %d: %v", got, c.errs)
	}
}

func TestCheckLifecycleDocErrors(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.lifecycles")
	rel := "contracts/capabilities/lifecycles/x.yaml"
	stepIDs := map[string]bool{
		"cap.services.x.create.create":       true,
		"cap.services.x.delete.soft_delete":  true,
		"cap.services.x.activate.transition": true,
	}
	actionSteps := map[string][]string{
		"cap.lifecycles.x.create":               {"cap.services.x.create.create"},
		"cap.lifecycles.x.destroy":              {"cap.services.x.delete.soft_delete"},
		"cap.lifecycles.x.transitions.activate": {"cap.services.x.activate.transition"},
	}
	valid := `id: cap.lifecycles.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
subjects:
  - { entity: data.entities.x, field: status }
states:
  values: [pending, active, deleted]
  initial: pending
  final: [deleted]
create:
  implemented_by: [cap.services.x.create.create]
destroy:
  implemented_by: [cap.services.x.delete.soft_delete]
transitions:
  - name: activate
    description: "a"
    from: [pending]
    to: active
    implemented_by: [cap.services.x.activate.transition]
`
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "final with outgoing edge",
			body: strings.Replace(valid, "from: [pending]", "from: [deleted]", 1),
			want: "[lifecycle-exhaustive]",
		},
		{
			name: "unreachable final",
			body: strings.Replace(valid, "destroy:\n  implemented_by: [cap.services.x.delete.soft_delete]\n", "", 1),
			want: "[lifecycle-exhaustive]",
		},
		{
			name: "unknown implemented_by step",
			body: strings.Replace(valid, "implemented_by: [cap.services.x.activate.transition]", "implemented_by: [cap.services.missing.activate]", 1),
			want: "[lifecycle-anchors]",
		},
		{
			name: "missing create step back-reference",
			body: strings.Replace(valid, "implemented_by: [cap.services.x.create.create]", "implemented_by: [cap.services.x.other.create]", 1),
			want: "[lifecycle-anchors]",
		},
		{
			name: "state not in status enum",
			body: strings.Replace(valid, "values: [pending, active, deleted]", "values: [pending, broken, deleted]", 1),
			want: "[lifecycle-subject]",
		},
		{
			name: "unknown subject field",
			body: strings.Replace(valid, "field: status", "field: missing", 1),
			want: "[lifecycle-subject]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeCapFile(t, root, rel, tc.body)
			doc, err := loadYAML(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			c := &Checker{
				root:       root,
				errCodeSet: map[string]bool{"COMMON_INTERNAL": true},
				enumValues: map[string]map[string]bool{"data.enums.x_status": {"pending": true, "active": true, "deleted": true}},
				entities:   map[string]*entityInfo{"data.entities.x": {OwnerModule: "x", Primary: "data.tables.x", Fields: []entityFieldInfo{{Name: "Status", JSONName: "status", Type: "data.enums.x_status", SourceColumnRef: "data.tables.x.col.status"}}}},
			}
			c.checkLifecycleDoc(doc, rel, schema, stepIDs, actionSteps, map[string]map[string]bool{})
			if !strings.Contains(strings.Join(c.errs, "\n"), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, c.errs)
			}
		})
	}
}

func TestCheckLifecycleRequires(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.lifecycles")
	rel := "contracts/capabilities/lifecycles/x.yaml"
	stepIDs := map[string]bool{"cap.services.x.activate.transition": true}
	actionSteps := map[string][]string{"cap.lifecycles.x.transitions.activate": {"cap.services.x.activate.transition"}}
	lifecycleStates := map[string]map[string]bool{
		"cap.lifecycles.x": {"pending": true, "active": true, "deleted": true},
		"cap.lifecycles.y": {"active": true},
	}
	valid := `id: cap.lifecycles.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
subjects:
  - { entity: data.entities.x, field: status }
states:
  values: [pending, active]
  initial: pending
transitions:
  - name: activate
    description: "a"
    from: [pending]
    to: active
    requires: [ { lifecycle: cap.lifecycles.y, states: [active] } ]
    implemented_by: [cap.services.x.activate.transition]
`
	check := func(body string) string {
		writeCapFile(t, root, rel, body)
		doc, err := loadYAML(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		c := &Checker{
			root:       root,
			errCodeSet: map[string]bool{"COMMON_INTERNAL": true},
			enumValues: map[string]map[string]bool{"data.enums.x_status": {"pending": true, "active": true}},
			entities: map[string]*entityInfo{"data.entities.x": {OwnerModule: "x", Primary: "data.tables.x", Fields: []entityFieldInfo{{
				Name: "Status", JSONName: "status", Type: "data.enums.x_status", SourceColumnRef: "data.tables.x.col.status",
			}}}},
		}
		c.checkLifecycleDoc(doc, rel, schema, stepIDs, actionSteps, lifecycleStates)
		return strings.Join(c.errs, "\n")
	}

	if got := check(valid); strings.Contains(got, "[lifecycle-requires]") {
		t.Fatalf("valid requires reported: %s", got)
	}
	if got := check(strings.Replace(valid, "cap.lifecycles.y", "cap.lifecycles.missing", 1)); !strings.Contains(got, "[lifecycle-requires]") {
		t.Fatalf("unknown requires lifecycle not reported: %s", got)
	}
	if got := check(strings.Replace(valid, "states: [active]", "states: [missing]", 1)); !strings.Contains(got, "[lifecycle-requires]") {
		t.Fatalf("unknown requires state not reported: %s", got)
	}
	if got := check(strings.Replace(valid, "cap.lifecycles.y", "cap.lifecycles.x", 1)); !strings.Contains(got, "requires itself") {
		t.Fatalf("self requires not reported: %s", got)
	}
	repeat := strings.Replace(valid, "requires: [ { lifecycle: cap.lifecycles.y, states: [active] } ]",
		"requires: [ { lifecycle: cap.lifecycles.y, states: [active] }, { lifecycle: cap.lifecycles.y, states: [active] } ]", 1)
	if got := check(repeat); !strings.Contains(got, "repeats lifecycle") {
		t.Fatalf("repeated requires lifecycle not reported: %s", got)
	}
}

func TestCheckLifecycleImplementedByAnchor(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.lifecycles")
	rel := "contracts/capabilities/lifecycles/x.yaml"
	stepIDs := map[string]bool{"cap.services.x.create.create": true}
	actionSteps := map[string][]string{"cap.lifecycles.x.create": {"cap.services.x.create.create"}}
	body := `id: cap.lifecycles.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
subjects:
  - { entity: data.entities.x, field: status }
states:
  values: [active]
  initial: active
  final: [active]
create:
  implemented_by: [cap.services.x.create.create]
`
	check := func(body string) string {
		writeCapFile(t, root, rel, body)
		doc, err := loadYAML(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		c := &Checker{
			root:       root,
			errCodeSet: map[string]bool{},
			enumValues: map[string]map[string]bool{"data.enums.x_status": {"active": true}},
			entities: map[string]*entityInfo{"data.entities.x": {OwnerModule: "x", Primary: "data.tables.x", Fields: []entityFieldInfo{{
				Name: "Status", JSONName: "status", Type: "data.enums.x_status", SourceColumnRef: "data.tables.x.col.status",
			}}}},
		}
		c.checkLifecycleDoc(doc, rel, schema, stepIDs, actionSteps, map[string]map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	if got := check(body); got != "" {
		t.Fatalf("valid implemented_by reported: %s", got)
	}
	wrong := strings.Replace(body, "cap.services.x.create.create", "cap.services.x.create.other", 1)
	if got := check(wrong); !strings.Contains(got, "[lifecycle-anchors]") {
		t.Fatalf("unknown implemented_by step not reported: %s", got)
	}
}

func TestCheckWhenLiteralInteger(t *testing.T) {
	c := &Checker{}
	c.checkWhenLiteral("integer", float64(1), "rel", "step", "field")
	if len(c.errs) != 0 {
		t.Fatalf("integral float literal reported: %v", c.errs)
	}
	c.errs = nil
	c.checkWhenLiteral("integer", float64(1.5), "rel", "step", "field")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-when-types]") {
		t.Fatalf("non-integral float literal not reported: %v", c.errs)
	}
}

func TestLifecycleRequiresCycle(t *testing.T) {
	c := &Checker{}
	c.checkLifecycleRequiresCycles(map[string][]string{
		"cap.lifecycles.a": {"cap.lifecycles.b"},
		"cap.lifecycles.b": {"cap.lifecycles.a"},
		"cap.lifecycles.c": {"cap.lifecycles.d"},
	})
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, "[lifecycle-requires]") || !strings.Contains(got, "cap.lifecycles.a -> cap.lifecycles.b -> cap.lifecycles.a") {
		t.Fatalf("cycle not reported: %v", c.errs)
	}
	if strings.Contains(got, "cap.lifecycles.c") {
		t.Fatalf("acyclic edge reported: %v", c.errs)
	}
}

func TestLifecycleDimensionConflict(t *testing.T) {
	root := t.TempDir()
	for _, typ := range []string{"cap.services", "feature.rules", "cap.lifecycles", "cap.events", "cap.protocols.http"} {
		copyMeta(t, root, typ)
	}
	body := func(id, summary string) string {
		return `id: ` + id + `
summary: "` + summary + `"
owner_module: account
schema_version: 1
stability: draft
do_not: []
subjects:
  - { entity: data.entities.account, field: status }
states:
  values: [active]
  initial: active
`
	}
	writeCapFile(t, root, "contracts/capabilities/lifecycles/a.yaml", body("cap.lifecycles.a", "a"))
	writeCapFile(t, root, "contracts/capabilities/lifecycles/b.yaml", body("cap.lifecycles.b", "b"))

	c := &Checker{
		root:       root,
		errCodeSet: map[string]bool{},
		enumValues: map[string]map[string]bool{"data.enums.account_status": {"active": true}},
		entities: map[string]*entityInfo{"data.entities.account": {OwnerModule: "account", Primary: "data.tables.account", Fields: []entityFieldInfo{{
			Name: "Status", JSONName: "status", Type: "data.enums.account_status", SourceColumnRef: "data.tables.account.col.status",
		}}}},
	}
	c.checkCapabilities()
	if !strings.Contains(strings.Join(c.errs, "\n"), "[lifecycle-dimension]") {
		t.Fatalf("dimension conflict not reported: %v", c.errs)
	}
}

func TestLifecycleEnumCoverage(t *testing.T) {
	c := &Checker{enumValues: map[string]map[string]bool{"data.enums.x": {"a": true, "b": true}}}
	c.checkLifecycleEnumCoverage(
		map[string]map[string]bool{"data.enums.x": {"a": true}},
		map[string]map[string]bool{"data.enums.x": {"cap.lifecycles.x": true}},
	)
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "value b is not declared") || !strings.Contains(got, "[lifecycle-enum-coverage]") {
		t.Fatalf("missing enum value not reported: %v", c.errs)
	}

	c.errs = nil
	c.checkLifecycleEnumCoverage(
		map[string]map[string]bool{"data.enums.x": {"a": true, "b": true}},
		map[string]map[string]bool{"data.enums.x": {"cap.lifecycles.x": true}},
	)
	if len(c.errs) != 0 {
		t.Fatalf("full coverage reported: %v", c.errs)
	}
}

func TestCheckEventPayloadRef(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.events")
	rel := "contracts/capabilities/events/x.yaml"
	valid := `id: cap.events.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
topics:
  - id: cap.events.x.created
    name: x.created
    version: 1
    summary: "x"
    idempotent: false
    publish:
      payload:
        ref: data.entities.account
      emitted_by: [cap.services.x.create]
`
	writeCapFile(t, root, rel, valid)
	c := &Checker{
		root:           root,
		entities:       map[string]*entityInfo{"data.entities.account": {}},
		methodTypeDefs: map[string]map[string]any{"AuthSession": {}},
	}
	c.checkEventsDoc(schema, map[string]serviceMeta{}, map[string]bool{"cap.services.x.create": true})
	if len(c.errs) != 0 {
		t.Fatalf("valid ref reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "data.entities.account", "data.entities.missing", 1))
	c.errs = nil
	c.checkEventsDoc(schema, map[string]serviceMeta{}, map[string]bool{"cap.services.x.create": true})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-ref]") {
		t.Fatalf("unknown ref not reported: %v", c.errs)
	}
}

func TestCheckEventsDoc(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.events")
	rel := "contracts/capabilities/events/x.yaml"
	valid := `id: cap.events.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
topics:
  - id: cap.events.x.created
    name: x.created
    version: 1
    summary: "x"
    idempotent: false
    publish:
      payload:
        type: _types.object
        properties:
          - { name: id, schema: { type: _types.integer }, required: true, description: "id" }
      emitted_by: [cap.services.x.emit]
    subscribe:
      - id: cap.events.x.consume
        name: consume
        summary: "x"
        service: cap.services.x.get
        retry: {max_attempts: 3, backoff: exponential, dead_letter: true}
        concurrency: {key: x_consume, ttl: 1h}
`
	writeCapFile(t, root, rel, valid)
	services := map[string]serviceMeta{
		"cap.services.x.emit": {module: "x"},
		"cap.services.x.get":  {module: "x"},
	}
	serviceIDs := map[string]bool{"cap.services.x.emit": true}
	c := &Checker{root: root}
	asyncServices := c.checkEventsDoc(schema, services, serviceIDs)
	if len(c.errs) != 0 {
		t.Fatalf("valid events reported: %v", c.errs)
	}
	if !asyncServices["cap.services.x.get"] {
		t.Fatalf("subscribe service not collected: %v", asyncServices)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "cap.services.x.get", "cap.services.y.get", 1))
	c.errs = nil
	yServices := map[string]serviceMeta{
		"cap.services.x.emit": {module: "x"},
		"cap.services.y.get":  {module: "y"},
	}
	c.checkEventsDoc(schema, yServices, serviceIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[async-service]") {
		t.Fatalf("cross-module service not reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "emitted_by: [cap.services.x.emit]", "emitted_by: [cap.services.x.missing]", 1))
	c.errs = nil
	c.checkEventsDoc(schema, services, serviceIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[event-structure]") {
		t.Fatalf("unknown emitter not reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "service: cap.services.x.get", "service: cap.services.x.missing", 1))
	c.errs = nil
	c.checkEventsDoc(schema, services, serviceIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[async-service]") {
		t.Fatalf("unknown subscribe service not reported: %v", c.errs)
	}
}

func TestServiceUsedRule(t *testing.T) {
	c := &Checker{}
	c.checkServiceUsed("x.yaml", map[string]any{
		"stability": "stable",
		"services": []any{
			map[string]any{"name": "internal_empty", "used_by": []any{}},
		},
	})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-used]") {
		t.Fatalf("stable empty used_by not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkServiceUsed("x.yaml", map[string]any{
		"stability": "draft",
		"services": []any{
			map[string]any{"name": "scaffold", "used_by": []any{}},
		},
	})
	if strings.Contains(strings.Join(c.errs, "\n"), "[service-used]") {
		t.Fatalf("draft empty used_by must not be reported: %v", c.errs)
	}
}

func TestCheckEventsDispatch(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.events")
	rel := "contracts/capabilities/events/x.yaml"
	valid := `id: cap.events.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
dispatches:
  - id: cap.events.x.send
    name: send
    summary: "x"
    payload:
      type: _types.object
      properties:
        - { name: order_id, schema: { type: _types.integer }, required: true, description: "id" }
    service: cap.services.x.send
    delay: 30m
`
	writeCapFile(t, root, rel, valid)
	services := map[string]serviceMeta{"cap.services.x.send": {module: "x"}}
	c := &Checker{root: root}
	async := c.checkEventsDoc(schema, services, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid dispatch reported: %v", c.errs)
	}
	if !async["cap.services.x.send"] {
		t.Fatalf("dispatch service not collected: %v", async)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "cap.services.x.send", "cap.services.y.send", 1))
	c.errs = nil
	c.checkEventsDoc(schema, map[string]serviceMeta{"cap.services.y.send": {module: "y"}}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[async-service]") {
		t.Fatalf("cross-module dispatch service not reported: %v", c.errs)
	}
}

func TestCheckStepEventsAcceptsDispatch(t *testing.T) {
	c := &Checker{eventPayloads: map[string]map[string]eventField{
		"cap.events.x.send": {"order_id": {typ: "_types.integer", required: true}},
	}}
	step := map[string]any{"events": []any{map[string]any{"ref": "cap.events.x.send"}}}
	input := map[string]any{"order_id": "params.order_id"}
	c.checkStepEvents(step, input, "x.yaml", "emit", map[string]bool{"cap.events.x.send": true}, stepTypeContext{})
	if len(c.errs) != 0 {
		t.Fatalf("dispatch ref rejected: %v", c.errs)
	}

	c.errs = nil
	c.checkStepEvents(step, input, "x.yaml", "emit", map[string]bool{}, stepTypeContext{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-ref]") {
		t.Fatalf("unknown dispatch ref not reported: %v", c.errs)
	}
}

func TestCheckEventsFileShape(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.events")
	rel := "contracts/capabilities/events/x.yaml"
	valid := `id: cap.events.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
topics:
  - id: cap.events.x.created
    name: x.created
    version: 1
    summary: "x"
    idempotent: false
    publish:
      payload: { type: _types.integer }
      emitted_by: [cap.services.x.get]
`
	writeCapFile(t, root, rel, valid)
	services := map[string]serviceMeta{"cap.services.x.get": {module: "x"}}
	serviceIDs := map[string]bool{"cap.services.x.get": true}
	c := &Checker{root: root}
	c.checkEventsDoc(schema, services, serviceIDs)
	if len(c.errs) != 0 {
		t.Fatalf("valid file reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, `id: cap.events.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
`)
	c.errs = nil
	c.checkEventsDoc(schema, services, serviceIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[event-file-shape]") {
		t.Fatalf("empty topics/crons/dispatches not reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, `id: cap.events.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
topics:
  - id: cap.events.x.created
    name: x.created
    version: 1
    summary: "x"
    idempotent: false
    publish:
      payload: { type: _types.integer }
      emitted_by: [cap.services.x.get]
crons:
  - id: cap.events.x.created
    name: created
    summary: "x"
    service: cap.services.x.get
    schedule: "0 3 * * *"
    overlap: forbid
`)
	c.errs = nil
	c.checkEventsDoc(schema, services, serviceIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[event-file-shape]") {
		t.Fatalf("topic/cron id collision not reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, `id: cap.events.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
topics:
  - id: cap.events.x.created
    name: x.created
    version: 1
    summary: "x"
    idempotent: false
    publish:
      payload: { type: _types.integer }
      emitted_by: []
`)
	c.errs = nil
	c.checkEventsDoc(schema, services, serviceIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[event-orphan]") {
		t.Fatalf("orphan topic not reported: %v", c.errs)
	}
}

func TestCheckEventsCronSchedule(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.events")
	rel := "contracts/capabilities/events/x.yaml"
	valid := `id: cap.events.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
crons:
  - id: cap.events.x.cleanup
    name: cleanup
    summary: "x"
    service: cap.services.x.get
    schedule: "*/15 0-6 1,15 * 1-5"
    overlap: forbid
`
	writeCapFile(t, root, rel, valid)
	services := map[string]serviceMeta{"cap.services.x.get": {module: "x"}}
	c := &Checker{root: root}
	c.checkEventsDoc(schema, services, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid cron reported: %v", c.errs)
	}

	for _, bad := range []string{"0 * * *", "60 * * * *", "0 24 * * *", "0 0 0 * *", "0 0 * 13 *", "0 0 * * 8", "*/0 * * * *"} {
		writeCapFile(t, root, rel, strings.Replace(valid, `"*/15 0-6 1,15 * 1-5"`, `"`+bad+`"`, 1))
		c.errs = nil
		c.checkEventsDoc(schema, services, map[string]bool{})
		if !strings.Contains(strings.Join(c.errs, "\n"), "[event-cron-schedule]") {
			t.Fatalf("bad cron %q not reported: %v", bad, c.errs)
		}
	}
}

func TestHTTPJobServiceBinding(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
owner_module: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /job:
    post:
      name: "job"
      contract:
        id: "cap.protocols.http.x.job"
        service: "cap.services.x.job"
      request: {}
      response:
        "200":
          description: "ok"
components:
  schemas: {}`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.job": {visibility: "public", paramTypes: map[string]bool{}},
	}, map[string]bool{}, map[string]bool{"cap.services.x.job": true})
	if !strings.Contains(strings.Join(c.errs, "\n"), "used by a cap.events job") {
		t.Fatalf("job service http binding not reported: %v", c.errs)
	}
}

func TestHTTPDocStructure(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.protocols.http")
	rel := "contracts/capabilities/protocols/http/x.yaml"
	writeCapFile(t, root, rel, `id: "cap.http.x"
summary: "x"
schema_version: 1
stability: "draft"
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

    post:
      name: "postX"
      contract:
        id: "cap.protocols.http.x.get"
        service: "cap.services.x.get"
      request: {}
      response:
        "200":
          description: "OK"
default_error_responses:
  - 418
  - 503`)
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {visibility: "public", paramTypes: map[string]bool{}},
	}, map[string]bool{}, map[string]bool{})
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		`missing required field "do_not"`,
		`id "cap.http.x" must be "cap.protocols.http.x"`,
		"duplicate x-contract-id",
		`unknown field "default_error_responses"`,
		"[http-file]",
		"[http-operation]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestServiceDocConventions(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.services")
	rel := "contracts/capabilities/services/x.yaml"
	writeCapFile(t, root, rel, `id: cap.services.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
schemas: {}
services:
  - name: get
    summary: "x"
    visibility: public
    tx_mode: read_only
    params:
      - {name: id, type: _types.integer}
    returns:
      - {name: err, type: error}
      - {name: value, type: _types.string}
    steps:
      - id: load
        kind: logic
        summary: "x"
        output: []
`)
	doc, err := loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, errCodeSet: map[string]bool{}, permissionSet: map[string]bool{}}
	c.checkServiceDoc(doc, rel, schema, map[string]bool{})
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"params[0] must be ctx",
		"returns must end with err error",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestLogicRulesValidated(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{
		"returns": []any{},
		"steps": []any{map[string]any{
			"id": "t", "kind": "logic", "summary": "x",
			"lifecycles": []any{"cap.lifecycles.x.transitions.t"},
			"rules":      []any{map[string]any{"rule": "feature.rules.x.missing"}},
			"output":     []any{},
			"errors":     []any{},
		}},
	}
	c.checkServiceItem(doc, "contracts/capabilities/services/x.yaml", "cap.services.x.get", "x",
		map[string]serviceMeta{}, map[string]bool{}, map[string]bool{},
		map[string]bool{"cap.lifecycles.x.transitions.t": true}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown rule feature.rules.x.missing") {
		t.Fatalf("logic rule not validated: %v", c.errs)
	}
}

func TestHTTPResponseCodes(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.protocols.http")
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
paths:
  /x:
    get:
      name: "getX"
      id: "cap.protocols.http.x.get"
      service: "cap.services.x.get"
      request: {}
      response:
        "204":
          description: "OK"
        "409":
          codes:
            - "platform.errors.X"
        "422":
          codes:
            - "platform.errors.Y"
        "500":
          codes:
            - "platform.errors.COMMON_INTERNAL"
      x-typo: true`)
	c := &Checker{root: root,
		errCodeSet:  map[string]bool{"X": true, "Y": true, "Z": true, "COMMON_INTERNAL": true},
		errorStatus: map[string]int{"X": 409, "Y": 409, "COMMON_INTERNAL": 500},
	}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {
			visibility: "public",
			errors: map[string]bool{
				"platform.errors.X":               true,
				"platform.errors.Y":               true,
				"platform.errors.Z":               true,
				"platform.errors.COMMON_INTERNAL": true,
			},
		},
	}, map[string]bool{}, map[string]bool{})
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"unknown operation extension x-typo",
		"platform.errors.COMMON_INTERNAL is a platform error",
		"platform.errors.Y has status 409 but is listed under response 422",
		"missing error platform.errors.Z",
		"[http-errors]",
		"[http-operation]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestAccessBindings(t *testing.T) {
	root := t.TempDir()
	rel := "contracts/platform/security/access.yaml"
	writeCapFile(t, root, rel, `id: platform.security.access
summary: "x"
schema_version: 1
stability: draft
do_not: []
bindings:
  - { target: cap.services.x.get, permissions: [platform.security.permissions.missing] }
  - { target: cap.services.x.create, audit: "yes", rate_limt: { requests: 1, window_seconds: 1 } }
  - { target: cap.services.x.missing, audit: true }
`)
	c := &Checker{root: root, permissionSet: map[string]bool{}}
	c.checkAccess(map[string]bool{"cap.services.x.get": true, "cap.services.x.create": true})
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"unknown permission platform.security.permissions.missing",
		"is not a service item",
		"unknown field rate_limt",
		"audit must be a boolean",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestAccessRateLimit(t *testing.T) {
	root := t.TempDir()
	rel := "contracts/platform/security/access.yaml"
	writeCapFile(t, root, rel, `id: platform.security.access
summary: "x"
schema_version: 1
stability: draft
do_not: []
bindings:
  - { target: cap.services.x.list, rate_limit: { requests: 60, window_seconds: 60 } }
  - { target: cap.services.x.get, rate_limit: { requests: 0, window_seconds: 0, extra: 1 } }
`)
	c := &Checker{root: root}
	c.checkAccess(map[string]bool{
		"cap.services.x.list": true,
		"cap.services.x.get":  true,
	})
	if !c.accessBindings["cap.services.x.list"].rateLimited {
		t.Fatalf("valid rate_limit not recorded: %v", c.errs)
	}
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"rate_limit.extra is not allowed",
		"rate_limit.requests must be >= 1",
		"rate_limit.window_seconds must be >= 1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestQueryErrorPropagation(t *testing.T) {
	query := &queryInfo{
		ID:     "data.queries.x.q",
		File:   "contracts/data/queries/x.yaml",
		Errors: []string{"platform.errors.A"},
	}
	base := func(step map[string]any) map[string]any {
		return map[string]any{
			"summary": "x",
			"tx_mode": "required",
			"steps":   []any{step},
		}
	}
	check := func(t *testing.T, doc map[string]any) []string {
		t.Helper()
		c := &Checker{
			errCodeSet: map[string]bool{"A": true, "B": true},
			queries:    map[string]*queryInfo{"data.queries.x.q": query},
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x", map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return c.errs
	}
	want := func(t *testing.T, errs []string, text string) {
		t.Helper()
		if !strings.Contains(strings.Join(errs, "\n"), text) {
			t.Fatalf("want %q, got %v", text, errs)
		}
	}

	t.Run("query step inherits query errors", func(t *testing.T) {
		doc := base(map[string]any{
			"id": "run", "kind": "query", "summary": "x",
			"query": "data.queries.x.q",
		})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("valid query error reported: %v", errs)
		}
	})

	t.Run("query step may add local errors", func(t *testing.T) {
		doc := base(map[string]any{
			"id": "run", "kind": "query", "summary": "x",
			"query":  "data.queries.x.q",
			"errors": []any{"platform.errors.B"},
		})
		want(t, check(t, doc), "does not declare query error platform.errors.A")
	})
}

func TestServiceSchemaTemplate(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.services")
	rel := "contracts/capabilities/services/x.yaml"
	writeCapFile(t, root, rel, `id: cap.services.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
schemas:
  XInput:
    type: _types.object
    properties:
      - name: nested
        schema:
          type: _types.object
          properties:
            - { name: id, schema: { type: _types.integer }, description: "id" }
            - name: typo
              schema:
                type: _types.string
                bogus: 1
              description: "typo"
        description: "nested"
services:
  - name: get
    summary: "x"
    visibility: public
    params:
      - {name: ctx, type: context.Context}
      - {name: input, type: XInput}
    returns:
      - {name: err, type: error}
      - {name: value, type: _types.string}
    tx_mode: read_only
    steps:
      - {id: load, kind: logic, summary: "x", errors: [], output: []}
`)
	doc, err := loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, errCodeSet: map[string]bool{}, permissionSet: map[string]bool{}}
	c.checkServiceDoc(doc, rel, schema, map[string]bool{})
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, `unknown schema key "bogus"`) {
		t.Fatalf("nested schema node not rejected: %v", c.errs)
	}
}

func TestServiceSchemaKeyOrder(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.services")
	rel := "contracts/capabilities/services/x.yaml"
	writeCapFile(t, root, rel, `id: cap.services.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
schemas:
  XInput:
    type: _types.object
    properties:
      - name: nested
        description: "nested"
        schema:
          type: _types.object
          properties:
            - { name: id, schema: { type: _types.integer }, description: "id" }
services:
  - name: get
    summary: "x"
    visibility: public
    params:
      - {name: ctx, type: context.Context}
      - {name: input, type: XInput}
    returns:
      - {name: err, type: error}
      - {name: value, type: _types.string}
    tx_mode: read_only
    steps:
      - {id: load, kind: logic, summary: "x", errors: [], output: []}
`)
	doc, err := loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, errCodeSet: map[string]bool{}, permissionSet: map[string]bool{}}
	c.checkServiceDoc(doc, rel, schema, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "schema.field-order") {
		t.Fatalf("schema key order not enforced: %v", c.errs)
	}
}

func TestSchemaEmptyTypeAndRef(t *testing.T) {
	c := &Checker{nodeKinds: map[string]SchemaKind{
		"scalar": {Keys: []contractspec.Field{{Name: "type"}}},
		"ref":    {Keys: []contractspec.Field{{Name: "ref"}}},
	}}
	c.validateSchemaNode(map[string]any{"type": ""}, "x")
	c.validateSchemaNode(map[string]any{"ref": ""}, "y")
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, "x: schema node must have exactly one of ref or type") || !strings.Contains(got, "y.ref: must not be empty") {
		t.Fatalf("empty type/ref accepted: %v", c.errs)
	}
}

func TestSchemaPropertyDefault(t *testing.T) {
	propertyKeys := []contractspec.Field{
		{Name: "name", Type: "string"},
		{Name: "schema", Types: []string{"object", "array", "scalar", "ref"}},
		{Name: "default", Type: "any"},
		{Name: "description", Type: "string"},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", UniqueKey: "name", ItemFields: propertyKeys},
			}},
			"scalar": {Keys: []contractspec.Field{{Name: "type", Type: "string"}}},
			"ref":    {Keys: []contractspec.Field{{Name: "ref", Type: "string"}}},
		},
		propertyKeys: propertyKeys,
		valueTypes: contractspec.ValueTypes{
			TypeFamilies: map[string]contractspec.ValueTypeFamily{
				"integer": {Description: "integer"},
			},
		},
		enumIDs: map[string]bool{"data.enums.mode": true},
		enumValues: map[string]map[string]bool{
			"data.enums.mode": {"a": true, "b": true},
		},
	}
	c.validateSchemaNode(map[string]any{
		"type": "_types.object",
		"properties": []any{
			map[string]any{"name": "ok_enum", "schema": map[string]any{"ref": "data.enums.mode"}, "default": "a", "description": "ok"},
			map[string]any{"name": "bad_enum", "schema": map[string]any{"ref": "data.enums.mode"}, "default": "z", "description": "bad"},
			map[string]any{"name": "bad_int", "schema": map[string]any{"type": "_types.integer"}, "default": "x", "description": "bad"},
		},
	}, "x")
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		`x.properties[1].default: z is not a value of data.enums.mode`,
		`x.properties[2].default: must be integer for type integer`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %v", want, c.errs)
		}
	}
	if strings.Contains(got, "x.properties[0].default") {
		t.Fatalf("valid enum default rejected: %v", c.errs)
	}
}

func TestSchemaNestedValueIsValidated(t *testing.T) {
	propertyKeys := []contractspec.Field{
		{Name: "name", Type: "string", Required: true},
		{Name: "schema", Types: []string{"object", "array", "scalar", "ref"}, Required: true},
		{Name: "description", Type: "string", Required: true},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", UniqueKey: "name", ItemFields: propertyKeys},
			}},
			"array": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "items", Types: []string{"object", "array", "scalar", "ref"}},
			}},
		},
		propertyKeys: propertyKeys,
	}
	c.validateSchemaNode(map[string]any{"type": "_types.array", "items": 42}, "a")
	c.validateSchemaNode(map[string]any{
		"type": "_types.object",
		"properties": []any{
			map[string]any{"name": "x", "schema": 42, "description": "x"},
		},
	}, "o")
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"a.items: must be a schema object",
		"o.properties[0].schema: must be a schema object",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %v", want, c.errs)
		}
	}
}

func TestSchemaTypesConstraint(t *testing.T) {
	propertyKeys := []contractspec.Field{
		{Name: "name", Type: "string", Required: true},
		{Name: "schema", Types: []string{"scalar"}, Required: true},
		{Name: "description", Type: "string", Required: true},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", UniqueKey: "name", ItemFields: propertyKeys},
			}},
			"scalar": {Keys: []contractspec.Field{{Name: "type", Type: "string"}}},
		},
		propertyKeys: propertyKeys,
		valueTypes: contractspec.ValueTypes{
			TypeFamilies: map[string]contractspec.ValueTypeFamily{
				"string": {Description: "string"},
			},
		},
	}
	c.validateSchemaNode(map[string]any{
		"type": "_types.object",
		"properties": []any{
			map[string]any{"name": "ok", "schema": map[string]any{"type": "_types.string"}, "description": "ok"},
			map[string]any{"name": "bad", "schema": map[string]any{"type": "_types.object", "properties": []any{}}, "description": "bad"},
		},
	}, "x")
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, "x.properties[1].schema: schema kind object is not allowed here (allowed: [scalar])") {
		t.Fatalf("types constraint not enforced: %v", c.errs)
	}
	if strings.Contains(got, "x.properties[0].schema") {
		t.Fatalf("valid scalar rejected: %v", c.errs)
	}
}

func TestValidateFieldTypesSchemaKinds(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		valid bool
	}{
		{"valid kinds", Field{Name: "x", Types: []string{"object", "scalar"}}, true},
		{"valid map", Field{Name: "x", Type: "map", Types: []string{"object"}}, true},
		{"valid types with required", Field{Name: "x", Types: []string{"object"}, Required: true}, true},
		{"unknown kind", Field{Name: "x", Types: []string{"object", "bogus"}}, false},
		{"duplicate kind", Field{Name: "x", Types: []string{"object", "object"}}, false},
		{"empty types", Field{Name: "x", Types: []string{}}, false},
		{"type and types", Field{Name: "x", Type: "string", Types: []string{"scalar"}}, false},
		{"map without types", Field{Name: "x", Type: "map"}, false},
		{"types with pattern", Field{Name: "x", Types: []string{"scalar"}, Pattern: "^a$"}, false},
		{"types multi with default", Field{Name: "x", Types: []string{"scalar", "object"}, Default: 1}, false},
		{"unique_key on non list", Field{Name: "x", Type: "string", UniqueKey: "name"}, false},
		{"unique_key on list", Field{Name: "x", Type: "list<object>", UniqueKey: "name", ItemFields: []Field{{Name: "name"}}}, true},
	}
	for _, tc := range cases {
		err := validateFieldTypes([]Field{tc.field}, testValueTypes(), false)
		if tc.valid && err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestObjectValidation(t *testing.T) {
	propertyKeys := []contractspec.Field{
		{Name: "name", Type: "string", Required: true},
		{Name: "schema", Types: []string{"object", "array", "scalar", "ref"}, Required: true},
		{Name: "description", Type: "string", Required: true},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", ItemFields: propertyKeys},
				{Name: "validation", Type: "list<object>", ItemFields: []contractspec.Field{
					{Name: "rule", Type: "string", Required: true},
					{Name: "message", Type: "string", Required: true},
				}},
			}},
			"scalar": {Keys: []contractspec.Field{{Name: "type", Type: "string"}}},
		},
		propertyKeys: propertyKeys,
	}
	c.validateSchemaNode(map[string]any{
		"type": "_types.object",
		"properties": []any{
			map[string]any{"name": "status", "schema": map[string]any{"type": "_types.string"}, "description": "状态"},
			map[string]any{"name": "own_type", "schema": map[string]any{"type": "_types.string"}, "description": "归属"},
		},
		"validation": []any{
			map[string]any{"rule": "input.status != null || input.own_type != null", "message": "至少一个条件"},
			map[string]any{"rule": "input.missing == 1", "message": "未知属性"},
			map[string]any{"rule": "input.status ==", "message": "语法错误"},
		},
	}, "x")
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"x.validation[1].rule: unknown property missing",
		"x.validation[2].rule: missing right operand",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %v", want, c.errs)
		}
	}
	if strings.Contains(got, "x.validation[0]") {
		t.Fatalf("valid rule rejected: %v", c.errs)
	}
}

func TestNodeValidationKinds(t *testing.T) {
	ruleKeys := []contractspec.Field{
		{Name: "rule", Type: "string", Required: true},
		{Name: "message", Type: "string", Required: true},
	}
	propertyKeys := []contractspec.Field{
		{Name: "name", Type: "string", Required: true},
		{Name: "schema", Types: []string{"scalar", "object", "array", "ref"}, Required: true},
		{Name: "description", Type: "string", Required: true},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", UniqueKey: "name", ItemFields: propertyKeys},
				{Name: "validation", Type: "list<object>", ItemFields: ruleKeys},
			}},
			"array": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "items", Types: []string{"scalar"}},
				{Name: "validation", Type: "list<object>", ItemFields: ruleKeys},
			}},
			"scalar": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "validation", Type: "list<object>", ItemFields: ruleKeys},
			}},
			"ref": {Keys: []contractspec.Field{
				{Name: "ref", Type: "string"},
				{Name: "validation", Type: "list<object>", ItemFields: ruleKeys},
			}},
		},
		propertyKeys:   propertyKeys,
		methodTypeDefs: map[string]map[string]any{"X": {"type": "_types.object"}},
		valueTypes: contractspec.ValueTypes{
			TypeFamilies: map[string]contractspec.ValueTypeFamily{"string": {Description: "string"}},
		},
	}
	c.validateSchemaNode(map[string]any{"type": "_types.string", "validation": []any{
		map[string]any{"rule": "value != null", "message": "ok"},
	}}, "scalar_ok")
	c.validateSchemaNode(map[string]any{"type": "_types.string", "validation": []any{
		map[string]any{"rule": "value.x != null", "message": "bad"},
	}}, "scalar_bad")
	c.validateSchemaNode(map[string]any{"ref": "X", "validation": []any{
		map[string]any{"rule": "value.status != null", "message": "ok"},
	}}, "ref_ok")
	c.validateSchemaNode(map[string]any{"type": "_types.array", "items": map[string]any{"type": "_types.string"}, "validation": []any{
		map[string]any{"rule": "value.x != null", "message": "bad"},
	}}, "array_bad")
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"scalar_bad.validation[0].rule: scalar nodes may only reference value",
		"array_bad.validation[0].rule: array nodes may only reference value",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %v", want, c.errs)
		}
	}
	if strings.Contains(got, "scalar_ok") || strings.Contains(got, "ref_ok") {
		t.Fatalf("valid rules rejected: %v", c.errs)
	}
}

func TestRefValidationEntityPath(t *testing.T) {
	ruleKeys := []contractspec.Field{
		{Name: "rule", Type: "string"},
		{Name: "message", Type: "string"},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"ref": {Keys: []contractspec.Field{
				{Name: "ref", Type: "string"},
				{Name: "validation", Type: "list<object>", ItemFields: ruleKeys},
			}},
		},
		entities: map[string]*entityInfo{
			"data.entities.account": {Fields: []entityFieldInfo{
				{Name: "Status", JSONName: "status", Type: "data.enums.account_status"},
				{Name: "Own", JSONName: "own", Type: "data.entities.own"},
			}},
			"data.entities.own": {Fields: []entityFieldInfo{
				{Name: "Kind", JSONName: "kind"},
			}},
		},
		enumValues: map[string]map[string]bool{
			"data.enums.account_status": {"pending": true, "active": true},
		},
	}
	c.validateSchemaNode(map[string]any{
		"ref": "data.entities.account",
		"validation": []any{
			map[string]any{"rule": "value.status != \"active\"", "message": "ok enum"},
			map[string]any{"rule": "value.own.kind == 1", "message": "ok nested"},
			map[string]any{"rule": "value.own.missing == 1", "message": "bad nested"},
			map[string]any{"rule": "value.missing == 1", "message": "bad"},
			map[string]any{"rule": "value.status.x == 1", "message": "bad traverse"},
			map[string]any{"rule": "value.status == \"deleted\"", "message": "bad enum"},
			map[string]any{"rule": "value.own == \"x\"", "message": "bad entity compare"},
			map[string]any{"rule": "value.own == null", "message": "ok null"},
		},
	}, "x")
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"x.validation[2].rule: unknown entity field missing",
		"x.validation[3].rule: unknown entity field missing",
		"x.validation[4].rule: status is not an entity; cannot traverse",
		`x.validation[5].rule: "deleted" is not a value of data.enums.account_status`,
		"x.validation[6].rule: value.own is an entity; compare with null only",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %v", want, c.errs)
		}
	}
	for _, idx := range []string{"x.validation[0]", "x.validation[1]", "x.validation[7]"} {
		if strings.Contains(got, idx) {
			t.Fatalf("valid rule rejected (%s): %v", idx, c.errs)
		}
	}
}

func TestCustomTypeParamTarget(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "data.tables")
	if schema.ValueTypes.CustomTypes == nil {
		schema.ValueTypes.CustomTypes = map[string]CustomValueType{}
	}
	schema.ValueTypes.CustomTypes["bad"] = CustomValueType{
		Family:      "string",
		Description: "bad",
		Params:      map[string]any{"min_len": 1, "mystery": true},
	}
	c := &Checker{}
	c.checkSharedTypes(schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "has no consumer target") {
		t.Fatalf("unknown param target not reported: %v", c.errs)
	}
}

func TestSchemaCycle(t *testing.T) {
	c := &Checker{methodTypeDefs: map[string]map[string]any{
		"A": {"type": "_types.object", "properties": []any{
			map[string]any{"name": "b", "schema": map[string]any{"ref": "B"}, "description": "b"},
		}},
		"B": {"type": "_types.object", "properties": []any{
			map[string]any{"name": "a", "schema": map[string]any{"ref": "A"}, "description": "a"},
		}},
	}}
	c.checkSchemaCycles()
	if !strings.Contains(strings.Join(c.errs, "\n"), "schema.cycle") {
		t.Fatalf("cycle not reported: %v", c.errs)
	}
}

func TestCustomTypeParamConsistency(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "data.tables")
	if schema.ValueTypes.CustomTypes == nil {
		schema.ValueTypes.CustomTypes = map[string]CustomValueType{}
	}
	schema.ValueTypes.CustomTypes["bad"] = CustomValueType{
		Family:      "string",
		Description: "bad",
		Params:      map[string]any{"min_len": 10, "max_len": 5},
	}
	c := &Checker{}
	c.checkSharedTypes(schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "family-length") {
		t.Fatalf("custom type length mismatch not reported: %v", c.errs)
	}
}

func TestSchemaFieldSpecs(t *testing.T) {
	propertyKeys := []contractspec.Field{
		{Name: "name", Type: "string"},
		{Name: "schema", Types: []string{"object", "array", "scalar", "ref"}},
		{Name: "required", Type: "boolean"},
		{Name: "nullable", Type: "boolean"},
		{Name: "read_only", Type: "boolean"},
		{Name: "write_only", Type: "boolean"},
		{Name: "default", Type: "any"},
		{Name: "description", Type: "string"},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", UniqueKey: "name", ItemFields: propertyKeys},
			}},
			"scalar": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
			}},
		},
		propertyKeys: propertyKeys,
		valueTypes: contractspec.ValueTypes{
			TypeFamilies: map[string]contractspec.ValueTypeFamily{
				"string": {Description: "string"},
			},
		},
	}
	c.validateSchemaNode(map[string]any{
		"type": "_types.object",
		"properties": []any{
			map[string]any{
				"name":        "id",
				"schema":      map[string]any{"type": "_types.string"},
				"nullable":    "yes",
				"description": "id",
				"read_only":   true,
				"write_only":  true,
			},
			map[string]any{
				"name":        "id",
				"schema":      map[string]any{"type": "_types.string"},
				"description": "duplicate",
			},
			map[string]any{
				"name":        "ok",
				"schema":      map[string]any{"type": "_types.string"},
				"description": "missing schema field",
				"required":    "yes",
			},
			map[string]any{
				"name":        "ro_default",
				"schema":      map[string]any{"type": "_types.string"},
				"description": "read_only with default",
				"read_only":   true,
				"default":     "x",
			},
		},
		"read_only": true,
	}, "x")
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"x.properties[0].nullable: must be boolean",
		"read_only and write_only cannot both be true",
		`x.properties[1]: duplicate "id"`,
		"x.properties[2].required: must be boolean",
		`x: "read_only" is a property key and must not appear on a schema node`,
		"x.properties[3]: read_only must not declare default",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %v", want, c.errs)
		}
	}
}

func TestSchemaPropertyRequiresSchemaAndDescription(t *testing.T) {
	propertyKeys := []contractspec.Field{
		{Name: "name", Type: "string", Required: true},
		{Name: "schema", Types: []string{"object", "array", "scalar", "ref"}, Required: true},
		{Name: "description", Type: "string", Required: true},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", UniqueKey: "name", ItemFields: propertyKeys},
			}},
		},
		propertyKeys: propertyKeys,
	}
	c.validateSchemaNode(map[string]any{
		"type": "_types.object",
		"properties": []any{
			map[string]any{"name": "id"},
		},
	}, "x")
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		`x.properties[0]: missing required field "schema"`,
		`x.properties[0]: missing required field "description"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %v", want, c.errs)
		}
	}
}

func TestServiceStepRuleIDs(t *testing.T) {
	baseStep := func(kind string) map[string]any {
		return map[string]any{
			"id": "s", "kind": kind, "summary": "x",
			"input": []any{}, "output": []any{}, "errors": []any{},
			"rules": []any{}, "goto": "$end",
		}
	}
	cases := []struct {
		name string
		step map[string]any
		want string
	}{
		{"target", baseStep("query"), "service-step-target"},
		{"shape", func() map[string]any { s := baseStep("logic"); delete(s, "rules"); return s }(), "service-step-shape"},
		{"fields", func() map[string]any { s := baseStep("logic"); s["query"] = "data.queries.x.q"; return s }(), "service-step-fields"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Checker{errCodeSet: map[string]bool{}, permissionSet: map[string]bool{}}
			doc := map[string]any{
				"name": "get", "summary": "x", "visibility": "public", "tx_mode": "read_only",
				"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
				"returns": []any{map[string]any{"name": "err", "type": "error"}},
				"steps":   []any{tc.step},
			}
			c.checkServiceItem(doc, "contracts/capabilities/services/x.yaml", "cap.services.x.get", "x",
				map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
			got := strings.Join(c.errs, "\n")
			if !strings.Contains(got, "["+tc.want+"]") {
				t.Fatalf("want [%s], got:\n%s", tc.want, got)
			}
		})
	}
}

func TestServiceInputRuleIDs(t *testing.T) {
	tc := stepTypeContext{doc: map[string]any{"params": []any{
		map[string]any{"name": "id", "type": "integer"},
		map[string]any{"name": "maybe", "type": "integer", "nullable": true},
	}}}
	q := &queryInfo{
		ID: "data.queries.x.q", File: "x",
		Params: []queryParam{
			{Name: "ctx", Type: "context.Context"},
			{Name: "id", Type: "integer"},
			{Name: "maybe", Type: "integer", Nullable: true},
		},
	}
	cases := []struct {
		name  string
		q     *queryInfo
		input map[string]any
		want  string
	}{
		{"name", q, map[string]any{"id": "params.id", "extra": "params.id"}, "service-input-name"},
		{"nullable", q, map[string]any{"id": "params.maybe"}, "service-input-nullable"},
		{"required", q, map[string]any{}, "service-input-required"},
		{"type", &queryInfo{ID: "data.queries.x.q", File: "x", Params: []queryParam{{Name: "ctx", Type: "context.Context"}, {Name: "id", Type: "string"}}}, map[string]any{"id": "params.id"}, "service-input-type"},
		{"default", nil, nil, "service-param-default"},
	}
	for _, tcCase := range cases {
		t.Run(tcCase.name, func(t *testing.T) {
			c := &Checker{errCodeSet: map[string]bool{}, permissionSet: map[string]bool{}}
			if tcCase.name == "default" {
				c.checkServiceParamDefault(map[string]any{"name": "id", "type": "integer", "default": 1}, "x", "get")
			} else {
				c.checkQueryInput(tcCase.input, tcCase.q, tcCase.q.ID, "x", "s", tc)
			}
			got := strings.Join(c.errs, "\n")
			if !strings.Contains(got, "["+tcCase.want+"]") {
				t.Fatalf("want [%s], got:\n%s", tcCase.want, got)
			}
		})
	}
}

func TestHTTPServiceReturnMissing(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: "cap.protocols.http.x"
summary: "x"
schema_version: 1
stability: "draft"
do_not: []
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
          body:
            content_type: "application/json"
            schema:
              type: "object"
              additionalProperties: false
              x-service-return: "data.entities.missing"
      `)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {visibility: "public", returns: map[string]returnType{}},
	}, map[string]bool{}, map[string]bool{})
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "[http-service-return]") {
		t.Fatalf("missing service return not reported: %v", c.errs)
	}
}

func TestHTTPAudienceAndConsumer(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.protocols.http")
	writeCapFile(t, root, "contracts/platform/http_conventions.yaml", `audiences: [admin]
error_responses:
  always: [400, 409, 503]
  secured: [401]
unbound_operations:
  - { operation: cap.protocols.http.x.open, reason: "app shell" }
  - { operation: cap.protocols.http.x.stale, reason: "stale" }
`)
	writeCapFile(t, root, "contracts/project.yaml", `apps:
  admin:
    kind: api
    audience: admin
    includes: [cap.protocols.http.x]
`)
	writeCapFile(t, root, "contracts/features/pages/x.yaml", `id: feature.pages.x
data:
  get:
    capability: cap.services.x.get
  page_only:
    capability: cap.services.x.page_only
`)
	writeCapFile(t, root, "contracts/capabilities/protocols/http/shared.yaml", `id: cap.protocols.http.shared
summary: "x"
schema_version: 1
stability: draft
do_not: []
components:
  responses:
    BadRequest: { description: "400" }
    ServiceUnavailable: { description: "503" }
`)
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
audience: [admin]
paths:
  /ok:
    get:
      name: getOk
      id: cap.protocols.http.x.ok
      service: cap.services.x.get
      used_by: [feature.pages.x]
      request: {}
      response:
        "204": { description: "OK" }
  /open:
    get:
      name: getOpen
      id: cap.protocols.http.x.open
      service: cap.services.x.open
      used_by: []
      request: {}
      response:
        "204": { description: "OK" }
  /orphan:
    get:
      name: getOrphan
      id: cap.protocols.http.x.orphan
      service: cap.services.x.orphan
      used_by: []
      request: {}
      response:
        "204": { description: "OK" }
  /missing:
    get:
      name: getMissing
      id: cap.protocols.http.x.missing
      service: cap.services.x.get
      used_by: []
      request: {}
      response:
        "204": { description: "OK" }
  /other:
    get:
      name: getOther
      id: cap.protocols.http.x.other
      service: cap.services.x.get
      used_by: [feature.pages.x]
      audience: [user]
      request: {}
      response:
        "204": { description: "OK" }
`)
	c := &Checker{root: root, strictHTTP: true}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get":       {visibility: "public"},
		"cap.services.x.open":      {visibility: "public"},
		"cap.services.x.orphan":    {visibility: "public"},
		"cap.services.x.page_only": {visibility: "public"},
	}, map[string]bool{}, map[string]bool{})
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"audience user is not declared",
		"operation cap.protocols.http.x.orphan has no page consumer",
		"missing used_by feature.pages.x",
		"unbound_operations entry cap.protocols.http.x.stale is stale",
		"service cap.services.x.page_only is referenced by pages but has no http operation",
		"[http-audience]",
		"[http-consumer]",
		"[http-default-errors]",
		"[http-retry-after]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
