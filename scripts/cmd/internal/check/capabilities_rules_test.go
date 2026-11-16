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
	c.checkServiceItem(doc([]any{map[string]any{"name": "account", "type": "any"}}), "f", "cap.services.x.create", "x", map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]map[string]bool{}, map[string]bool{}, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("covered output reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceItem(doc([]any{}), "f", "cap.services.x.create", "x", map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]map[string]bool{}, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-output]") {
		t.Fatalf("missing return not reported: %v", c.errs)
	}
}

func TestServiceEventPayload(t *testing.T) {
	c := &Checker{}
	doc := func(payload map[string]any) map[string]any {
		return map[string]any{
			"summary": "x",
			"params":  []any{},
			"returns": []any{},
			"errors":  []any{},
			"steps": []any{
				map[string]any{
					"id":      "emit",
					"kind":    "event",
					"summary": "x",
					"event":   "cap.events.x.created",
					"payload": payload,
				},
			},
		}
	}
	eventIDs := map[string]bool{"cap.events.x.created": true}
	payloads := map[string]map[string]bool{"cap.events.x.created": {"id": true}}
	c.checkServiceItem(doc(map[string]any{"id": "input.id"}), "f", "cap.services.x.create", "x", map[string]serviceMeta{}, map[string]bool{}, eventIDs, payloads, map[string]bool{}, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid payload reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceItem(doc(map[string]any{"missing": "input.id"}), "f", "cap.services.x.create", "x", map[string]serviceMeta{}, map[string]bool{}, eventIDs, payloads, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-event-payload]") {
		t.Fatalf("unknown payload field not reported: %v", c.errs)
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
	for _, typ := range []string{"feature.rules", "feature.states", "cap.events", "cap.http"} {
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
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.http.x
summary: "x"
paths:
  /x:
    post:
      operationId: createX
      x-contract-id: cap.http.x.create
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
	c := &Checker{}
	schema := &Schema{Targets: Targets{
		Dir:  filepath.Join(root, "contracts/capabilities/protocols/http"),
		Glob: "*.yaml",
	}}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {
			visibility: "public",
			paramTypes: map[string]bool{"XInput": true},
		},
	}, map[string]bool{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-service-type]") {
		t.Fatalf("requestBody type mismatch not reported: %v", c.errs)
	}
}

func TestHTTPRequestBodyTypeMultipleMedia(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.http.x
summary: "x"
paths:
  /x:
    post:
      operationId: createX
      x-contract-id: cap.http.x.create
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
	c := &Checker{}
	schema := &Schema{Targets: Targets{
		Dir:  filepath.Join(root, "contracts/capabilities/protocols/http"),
		Glob: "*.yaml",
	}}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {visibility: "public", paramTypes: map[string]bool{"XInput": true}},
	}, map[string]bool{}, map[string]bool{})
	if got := strings.Count(strings.Join(c.errs, "\n"), "[http-service-type]"); got != 2 {
		t.Fatalf("want 2 http-service-type errors, got %d: %v", got, c.errs)
	}
}

func TestHTTPRequestBodyTypeInlineSkipped(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.http.x
summary: "x"
paths:
  /x:
    post:
      operationId: createX
      x-contract-id: cap.http.x.create
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
	c := &Checker{}
	schema := &Schema{Targets: Targets{
		Dir:  filepath.Join(root, "contracts/capabilities/protocols/http"),
		Glob: "*.yaml",
	}}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.create": {visibility: "public", paramTypes: map[string]bool{"XInput": true}},
	}, map[string]bool{}, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("inline requestBody reported: %v", c.errs)
	}
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
	c := &Checker{root: root}
	c.checkRuleDoc(doc, rel, schema, serviceIDs, map[string]bool{})
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
	c.checkRuleDoc(doc, rel, schema, serviceIDs, map[string]bool{})
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
	c.checkRuleDoc(doc, rel, schema, serviceIDs, map[string]bool{})
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
	c := &Checker{root: root}
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
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.http.x
summary: "x"
paths: {}
components:
  schemas:
    X:
      x-service-type: cap.services.x.types.X
      type: object
      additionalProperties: false
      properties:
        id: { type: string }
`)
	c := &Checker{}
	schema := &Schema{Targets: Targets{
		Dir:  filepath.Join(root, "contracts/capabilities/protocols/http"),
		Glob: "*.yaml",
	}}
	c.checkHTTPBindings(schema, map[string]serviceMeta{
		"cap.services.x.get": {
			visibility: "public",
			paramTypes: map[string]bool{},
			typeSchemas: map[string]map[string]any{
				"X": {"properties": map[string]any{"id": map[string]any{"type": "integer"}}},
			},
		},
	}, map[string]bool{}, map[string]bool{"X": true})
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
