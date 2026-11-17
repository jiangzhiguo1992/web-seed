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
	stepOutputs := []map[string]string{
		{"account": "data.entities.account"},
	}
	c := &Checker{
		entities: map[string]*entityInfo{
			"data.entities.account": {Fields: []entityFieldInfo{
				{Name: "ID", JSONName: "id", Type: "integer"},
			}},
		},
	}
	c.checkServiceParamExpr("input.id", doc, stepIndex, stepOutputs, steps, "f", "s")
	c.checkServiceParamExpr("step.load.account.id", doc, stepIndex, stepOutputs, steps, "f", "s")
	if len(c.errs) != 0 {
		t.Fatalf("valid params reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceParamExpr("input.missing", doc, stepIndex, stepOutputs, steps, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-params]") {
		t.Fatalf("unknown input not reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceParamExpr("step.load.missing", doc, stepIndex, stepOutputs, steps, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-params]") {
		t.Fatalf("unknown step field not reported: %v", c.errs)
	}
}

func TestServiceOutputCoversReturns(t *testing.T) {
	c := &Checker{errCodeSet: map[string]bool{"COMMON_INTERNAL": true}}
	doc := func(output []any) map[string]any {
		return map[string]any{
			"summary": "x",
			"params":  []any{},
			"returns": []any{
				map[string]any{"name": "account", "type": "any"},
				map[string]any{"name": "err", "type": "error"},
			},
			"errors": []any{"platform.errors.COMMON_INTERNAL"},
			"steps": []any{
				map[string]any{
					"id":      "create",
					"kind":    "logic",
					"summary": "x",
					"errors":  []any{"platform.errors.COMMON_INTERNAL"},
					"output":  output,
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
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-output]") {
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
custom_types: {}
`)
	writeCapFile(t, root, "contracts/_meta/_schemas.yaml", string(mustRead(t, filepath.Join("..", "..", "..", "..", "contracts", "_meta", "_schemas.yaml"))))
	writeCapFile(t, root, "contracts/_meta/cap.services.yaml", `type: cap.services
targets:
  dir: contracts/capabilities/services
  glob: "*.yaml"
  id: "cap.services.<file>"
fields:
  - {name: id, type: string}
  - {name: summary, type: string}
  - {name: owner_module, type: string}
  - {name: schema_version, type: integer}
  - {name: stability, type: string}
  - {name: do_not, type: list<string>}
  - {name: interface, type: any}
  - {name: persistence, type: any}
  - {name: services, type: any}
`)
	for _, typ := range []string{"feature.rules", "feature.states", "cap.events", "cap.protocols.http", "cap.async"} {
		writeCapFile(t, root, "contracts/_meta/"+typ+".yaml", "type: "+typ+`
targets:
  dir: contracts/features/unused
  glob: "*.yaml"
  id: "`+typ+`.<file>"
fields:
  - {name: id, type: string}
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
    transaction: read_only
    steps:
      - {id: call, kind: service, summary: "x", service: cap.services.b.get}
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
    transaction: read_only
    steps:
      - {id: call, kind: service, summary: "x", service: cap.services.a.get}
`)

	c := &Checker{root: root}
	c.checkCapabilities()
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-cycle]") {
		t.Fatalf("service cycle not reported: %v", c.errs)
	}
}

func TestHTTPRequestBodyType(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
paths:
  /x:
    post:
      operationId: createX
      x-contract-id: cap.protocols.http.x.create
      x-service: cap.services.x.create
      responses:
        "200":
          description: OK
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/WrongInput" }
components:
  schemas:
    WrongInput: {}
`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {
			visibility: "public",
			paramTypes: map[string]bool{"XInput": true},
		},
	}, map[string]bool{}, map[string]bool{}, map[string]asyncMeta{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-service-type]") {
		t.Fatalf("requestBody type mismatch not reported: %v", c.errs)
	}
}

func TestHTTPRequestBodyTypeMultipleMedia(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
paths:
  /x:
    post:
      operationId: createX
      x-contract-id: cap.protocols.http.x.create
      x-service: cap.services.x.create
      responses:
        "200":
          description: OK
      requestBody:
        required: true
        content:
          application/json:
            schema: { $ref: "#/components/schemas/WrongInput" }
          application/problem+json:
            schema: { $ref: "#/components/schemas/WrongInput" }
components:
  schemas:
    WrongInput: {}
`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {visibility: "public", paramTypes: map[string]bool{"XInput": true}},
	}, map[string]bool{}, map[string]bool{}, map[string]asyncMeta{})
	if got := strings.Count(strings.Join(c.errs, "\n"), "[http-service-type]"); got != 2 {
		t.Fatalf("want 2 http-service-type errors, got %d: %v", got, c.errs)
	}
}

func TestHTTPRequestBodyTypeInlineSkipped(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
paths:
  /x:
    post:
      operationId: createX
      x-contract-id: cap.protocols.http.x.create
      x-service: cap.services.x.create
      responses:
        "200":
          description: OK
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: false
              properties:
                id: { type: integer }
components:
  schemas: {}
`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {visibility: "public", paramTypes: map[string]bool{"XInput": true}},
	}, map[string]bool{}, map[string]bool{}, map[string]asyncMeta{})
	if len(c.errs) != 0 {
		t.Fatalf("inline requestBody reported: %v", c.errs)
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
	for _, name := range []string{"_schemas.yaml", "_types.yaml"} {
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
    severity: error
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
    severity: error
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
    severity: error
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

func TestCheckStateDoc(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "feature.states")
	serviceIDs := map[string]bool{"cap.services.x.activate": true}
	rel := "contracts/features/states/x.yaml"
	valid := `id: feature.states.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
machine: x
subject: data.entities.x
states:
  - name: pending
    description: "p"
    terminal: false
  - name: active
    description: "a"
    terminal: true
initial: pending
transitions:
  - name: activate
    description: "a"
    from: [pending]
    to: active
    guard: true
    on_violation: platform.errors.COMMON_INTERNAL
    implemented_by: [cap.services.x.activate]
`
	writeCapFile(t, root, rel, valid)
	doc, err := loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, errCodeSet: map[string]bool{"COMMON_INTERNAL": true}}
	c.checkStateDoc(doc, rel, schema, serviceIDs, map[string]bool{}, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid state machine reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "initial: pending", "initial: missing", 1))
	doc, err = loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c.errs = nil
	c.checkStateDoc(doc, rel, schema, serviceIDs, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[state-structure]") {
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
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
paths: {}
components:
  schemas:
    X:
      x-service-type: cap.services.x.schemas.X
      type: object
      additionalProperties: false
      properties:
        id: { type: string }
`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {
			visibility: "public",
			paramTypes: map[string]bool{},
			typeSchemas: map[string]map[string]any{
				"X": {"properties": map[string]any{"id": map[string]any{"type": "integer"}}},
			},
		},
	}, map[string]bool{"X": true}, map[string]bool{}, map[string]asyncMeta{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-service-type]") {
		t.Fatalf("component field mismatch not reported: %v", c.errs)
	}
}

func TestCheckStateDocErrors(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "feature.states")
	serviceIDs := map[string]bool{"cap.services.x.activate": true}
	rel := "contracts/features/states/x.yaml"
	valid := `id: feature.states.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
machine: x
subject: data.entities.x
states:
  - name: pending
    description: "p"
    terminal: false
  - name: active
    description: "a"
    terminal: true
initial: pending
transitions:
  - name: activate
    description: "a"
    from: [pending]
    to: active
    guard: true
    on_violation: platform.errors.COMMON_INTERNAL
    implemented_by: [cap.services.x.activate]
`
	cases := []struct {
		name  string
		body  string
		want  string
		rule  string
		extra map[string]bool
	}{
		{
			name: "terminal with outgoing edge",
			body: strings.Replace(strings.Replace(valid, "from: [pending]", "from: [active]", 1), "to: active", "to: pending", 1),
			want: "[state-exhaustive]",
		},
		{
			name: "unknown guard rule",
			body: strings.Replace(valid, "guard: true", "guard: { rule: feature.rules.missing }", 1),
			want: "[state-structure]",
		},
		{
			name: "unknown implemented_by service",
			body: strings.Replace(valid, "implemented_by: [cap.services.x.activate]", "implemented_by: [cap.services.missing.activate]", 1),
			want: "[state-structure]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeCapFile(t, root, rel, tc.body)
			doc, err := loadYAML(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			c := &Checker{root: root}
			c.checkStateDoc(doc, rel, schema, serviceIDs, map[string]bool{}, map[string]bool{})
			if !strings.Contains(strings.Join(c.errs, "\n"), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, c.errs)
			}
		})
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
delivery: at_least_once
events:
  - id: cap.events.x.created
    name: x.created
    version: 1
    payload:
      ref: data.entities.account
    summary: "x"
    emitted_by: []
    consumers: []
`
	writeCapFile(t, root, rel, valid)
	doc, err := loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{
		root:           root,
		entities:       map[string]*entityInfo{"data.entities.account": {}},
		methodTypeDefs: map[string]map[string]any{"AuthSession": {}},
	}
	c.checkEventDoc(doc, rel, schema, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid ref reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "data.entities.account", "data.entities.missing", 1))
	doc, err = loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c.errs = nil
	c.checkEventDoc(doc, rel, schema, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-ref]") {
		t.Fatalf("unknown ref not reported: %v", c.errs)
	}
}

func TestCheckAsyncDoc(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.async")
	rel := "contracts/capabilities/async/x.yaml"
	valid := `id: cap.async.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
jobs:
  - id: cap.async.x.job
    name: job
    summary: "x"
    service: cap.services.x.get
    trigger: {type: cron, schedule: "0 3 * * *"}
    retry: {max_attempts: 3, backoff: exponential, dead_letter: true}
    concurrency: {key: x_job, ttl: 1h}
`
	writeCapFile(t, root, rel, valid)
	if _, err := loadYAML(filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	async, jobs := c.checkAsyncDoc(schema, map[string]serviceMeta{"cap.services.x.get": {module: "x"}}, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid async reported: %v", c.errs)
	}
	if !async["cap.services.x.get"] {
		t.Fatalf("async service not collected: %v", async)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "cap.services.x.get", "cap.services.y.get", 1))
	if _, err := loadYAML(filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
	c.errs = nil
	c.checkAsyncDoc(schema, map[string]serviceMeta{"cap.services.y.get": {module: "y"}}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[async-service]") {
		t.Fatalf("cross-module service not reported: %v", c.errs)
	}

	writeCapFile(t, root, rel, strings.Replace(valid, "{type: cron, schedule: \"0 3 * * *\"}", "{type: webhook}", 1))
	if _, err := loadYAML(filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}
	c.errs = nil
	_, jobs = c.checkAsyncDoc(schema, map[string]serviceMeta{"cap.services.x.get": {module: "x"}}, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("webhook trigger reported: %v", c.errs)
	}
	if jobs["cap.async.x.job"].triggerType != "webhook" {
		t.Fatalf("webhook trigger not collected: %v", jobs)
	}
}

func TestHTTPAsyncBinding(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
paths:
  /hook:
    post:
      operationId: hook
      x-contract-id: cap.protocols.http.x.hook
      x-async: cap.async.x.job
      responses:
        "202": { description: "accepted" }
components:
  schemas: {}
`)
	schema := copyMeta(t, root, "cap.protocols.http")
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]asyncMeta{
		"cap.async.x.job": {triggerType: "webhook"},
	})
	if len(c.errs) != 0 {
		t.Fatalf("valid x-async reported: %v", c.errs)
	}

	c.errs = nil
	c.checkHTTPBindings(schema, map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]asyncMeta{
		"cap.async.x.job": {triggerType: "cron"},
	})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-binding]") {
		t.Fatalf("cron async job not reported: %v", c.errs)
	}
}

func TestHTTPDocStructure(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.protocols.http")
	rel := "contracts/capabilities/protocols/http/x.yaml"
	writeCapFile(t, root, rel, `id: cap.http.x
summary: "x"
schema_version: 1
stability: draft
paths:
  /x:
    get:
      operationId: getX
      x-contract-id: cap.protocols.http.x.get
      x-service: cap.services.x.get
      responses:
        "200": { description: OK }
    post:
      operationId: postX
      x-contract-id: cap.protocols.http.x.get
      x-service: cap.services.x.get
      responses:
        "200": { description: OK }
default_error_responses: [418]
`)
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {visibility: "public", paramTypes: map[string]bool{}},
	}, map[string]bool{}, map[string]bool{}, map[string]asyncMeta{})
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		`missing required field "do_not"`,
		`id "cap.http.x" must be "cap.protocols.http.x"`,
		"duplicate x-contract-id",
		"default status 418 has no components.responses mapping",
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
    params:
      - {name: id, type: integer}
    returns:
      - {name: value, type: string}
    errors: []
    transaction: read_only
    steps:
      - id: load
        kind: logic
        summary: "x"
        errors: []
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
		"returns must end with err",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestTransitionRulesValidated(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{
		"returns": []any{map[string]any{"name": "err", "type": "error"}},
		"steps": []any{map[string]any{
			"id": "t", "kind": "transition", "summary": "x",
			"transitions": []any{"feature.states.x.t"},
			"rules":       []any{"feature.rules.x.missing"},
			"output":      []any{},
		}},
	}
	c.checkServiceItem(doc, "contracts/capabilities/services/x.yaml", "cap.services.x.get", "x",
		map[string]serviceMeta{}, map[string]bool{}, map[string]bool{},
		map[string]bool{"feature.states.x.t": true}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown rule feature.rules.x.missing") {
		t.Fatalf("transition rule not validated: %v", c.errs)
	}
}

func TestHTTPOperationDefaults(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.protocols.http")
	writeCapFile(t, root, "contracts/capabilities/protocols/http/shared.yaml", `id: cap.protocols.http.shared
summary: "x"
schema_version: 1
stability: draft
do_not: []
components:
  responses:
    UnprocessableEntity: { description: "422" }
    Locked: { description: "423" }
`)
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
default_error_responses: [422]
paths:
  /x:
    get:
      operationId: getX
      x-contract-id: cap.protocols.http.x.get
      x-service: cap.services.x.get
      x-default-errors: [423]
      x-typo: true
      responses:
        "200": { description: OK }
        "423": { description: "should be injected" }
`)
	c := &Checker{root: root}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {visibility: "public", paramTypes: map[string]bool{}},
	}, map[string]bool{}, map[string]bool{}, map[string]asyncMeta{})
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"unknown operation extension x-typo",
		"status 423 is injected by default errors",
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
	base := func(step map[string]any, serviceErrors []any) map[string]any {
		return map[string]any{
			"summary":     "x",
			"transaction": "required",
			"errors":      serviceErrors,
			"steps":       []any{step},
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
			"id": "run", "kind": "query", "summary": "x", "query": "data.queries.x.q",
		}, []any{"platform.errors.A"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("valid query error reported: %v", errs)
		}
	})

	t.Run("query step missing service error", func(t *testing.T) {
		doc := base(map[string]any{
			"id": "run", "kind": "query", "summary": "x", "query": "data.queries.x.q",
		}, []any{})
		want(t, check(t, doc), "missing error platform.errors.A produced")
	})

	t.Run("query step may add local errors", func(t *testing.T) {
		doc := base(map[string]any{
			"id": "run", "kind": "query", "summary": "x", "query": "data.queries.x.q",
			"errors": []any{"platform.errors.B"},
		}, []any{"platform.errors.A", "platform.errors.B"})
		want(t, check(t, doc), "does not declare query error platform.errors.A")
	})

	t.Run("logic step must declare query errors", func(t *testing.T) {
		doc := base(map[string]any{
			"id": "run", "kind": "logic", "summary": "x",
			"errors": []any{}, "output": []any{},
			"queries": []any{"data.queries.x.q"},
		}, []any{})
		want(t, check(t, doc), "step run does not declare query data.queries.x.q error platform.errors.A")
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
    type: object
    properties:
      - name: nested
        schema:
          type: object
          properties:
            - { name: id, schema: { type: integer }, description: "id" }
            - name: typo
              schema:
                type: string
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
      - {name: value, type: string}
      - {name: err, type: error}
    errors: []
    transaction: read_only
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
    type: object
    properties:
      - name: nested
        description: "nested"
        schema:
          type: object
          properties:
            - { name: id, schema: { type: integer }, description: "id" }
services:
  - name: get
    summary: "x"
    visibility: public
    params:
      - {name: ctx, type: context.Context}
      - {name: input, type: XInput}
    returns:
      - {name: value, type: string}
      - {name: err, type: error}
    errors: []
    transaction: read_only
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
		{Name: "schema", Type: "schema"},
		{Name: "default", Type: "any"},
		{Name: "description", Type: "string"},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", ItemFields: propertyKeys},
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
		"type": "object",
		"properties": []any{
			map[string]any{"name": "ok_enum", "schema": map[string]any{"ref": "data.enums.mode"}, "default": "a", "description": "ok"},
			map[string]any{"name": "bad_enum", "schema": map[string]any{"ref": "data.enums.mode"}, "default": "z", "description": "bad"},
			map[string]any{"name": "bad_int", "schema": map[string]any{"type": "integer"}, "default": "x", "description": "bad"},
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
		{Name: "schema", Type: "schema", Required: true},
		{Name: "description", Type: "string", Required: true},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", ItemFields: propertyKeys},
			}},
			"array": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "items", Type: "schema"},
			}},
		},
		propertyKeys: propertyKeys,
	}
	c.validateSchemaNode(map[string]any{"type": "array", "items": 42}, "a")
	c.validateSchemaNode(map[string]any{
		"type": "object",
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
		{Name: "schema", Type: "schema"},
		{Name: "required", Type: "boolean"},
		{Name: "nullable", Type: "boolean"},
		{Name: "read_only", Type: "boolean"},
		{Name: "write_only", Type: "boolean"},
		{Name: "description", Type: "string"},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", ItemFields: propertyKeys},
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
		"type": "object",
		"properties": []any{
			map[string]any{
				"name":        "id",
				"schema":      map[string]any{"type": "string"},
				"nullable":    "yes",
				"description": "id",
				"read_only":   true,
				"write_only":  true,
			},
			map[string]any{
				"name":        "id",
				"schema":      map[string]any{"type": "string"},
				"description": "duplicate",
			},
			map[string]any{
				"name":        "ok",
				"schema":      map[string]any{"type": "string"},
				"description": "missing schema field",
				"required":    "yes",
			},
		},
		"read_only": true,
	}, "x")
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"x.properties[0].nullable: must be boolean",
		"read_only and write_only cannot both be true",
		`x: duplicate property "id"`,
		"x.properties[2].required: must be boolean",
		`x: "read_only" is a property key and must not appear on a schema node`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %v", want, c.errs)
		}
	}
}

func TestSchemaPropertyRequiresSchemaAndDescription(t *testing.T) {
	propertyKeys := []contractspec.Field{
		{Name: "name", Type: "string", Required: true},
		{Name: "schema", Type: "schema", Required: true},
		{Name: "description", Type: "string", Required: true},
	}
	c := &Checker{
		nodeKinds: map[string]SchemaKind{
			"object": {Keys: []contractspec.Field{
				{Name: "type", Type: "string"},
				{Name: "properties", Type: "list<object>", ItemFields: propertyKeys},
			}},
		},
		propertyKeys: propertyKeys,
	}
	c.validateSchemaNode(map[string]any{
		"type": "object",
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
