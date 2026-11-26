package check

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFlowStructureStepAndUsageRules(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "scripts/cmd/.keep", "")
	copyMeta(t, root, "feature.flows")
	copyMeta(t, root, "feature.pages")
	copyMeta(t, root, "feature.routes")
	writeCapFile(t, root, "contracts/index.yaml", `schema_version: 1
entries:
  - { id: feature.routes, path: features/routes.yaml }
`)
	writeCapFile(t, root, "contracts/features/routes.yaml", `id: feature.routes
routes:
  - { id: feature.routes.x, page: feature.pages.x }
`)
	writeCapFile(t, root, "contracts/features/pages/x.yaml", `id: feature.pages.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
page: x
route: feature.routes.x
data: { }
view_states:
  loading: { when: "true" }
  empty: { when: "false" }
  error: { when: "false" }
  forbidden: { when: "false" }
actions:
  submit: { }
`)
	valid := `id: feature.flows.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
scope: ui
entry: feature.routes.x
steps:
  - id: s1
    page: feature.pages.x
    action: submit
    rules: [ feature.flows.x.r1 ]
rules:
  - id: feature.flows.x.r1
    kind: constraint
    summary: "x"
    implemented_by: [ cap.services.x.create ]
`
	writeCapFile(t, root, "contracts/features/flows/x.yaml", valid)
	c := &Checker{root: root}
	c.checkFeatureFlows()
	if len(c.errs) != 0 {
		t.Fatalf("valid flow reported: %v", c.errs)
	}

	writeCapFile(t, root, "contracts/features/flows/x.yaml", `id: feature.flows.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
scope: ui
steps: [ ]
rules:
  - id: feature.flows.x.r1
    kind: policy
    summary: "x"
    implemented_by: [ cap.services.x.create ]
`)
	c.errs = nil
	c.checkFeatureFlows()
	mustContainRule(t, c.errs, "flow-structure")
	mustContainRule(t, c.errs, "flow-rule-usage")

	writeCapFile(t, root, "contracts/features/flows/x.yaml", `id: feature.flows.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
scope: ui
entry: feature.routes.x
steps:
  - id: s1
    page: feature.pages.x
    action: missing
    rules: [ feature.flows.x.r1 ]
rules:
  - id: feature.flows.x.r1
    kind: policy
    summary: "x"
    implemented_by: [ cap.services.x.create ]
`)
	c.errs = nil
	c.checkFeatureFlows()
	mustContainRule(t, c.errs, "flow-step")

	writeCapFile(t, root, "contracts/features/flows/x.yaml", `id: feature.flows.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
scope: ui
entry: feature.routes.missing
steps:
  - id: s1
    page: feature.pages.x
    action: submit
    rules: [ feature.flows.x.r1 ]
rules:
  - id: feature.flows.x.r1
    kind: policy
    summary: "x"
    implemented_by: [ cap.services.x.create ]
`)
	c.errs = nil
	c.checkFeatureFlows()
	mustContainRule(t, c.errs, "flow-refs")

	writeCapFile(t, root, "contracts/features/flows/x.yaml", `id: feature.flows.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
scope: ui
entry: feature.routes.x
steps:
  - id: s1
    page: feature.pages.x
    action: submit
    rules: [ feature.flows.x.p1 ]
rules:
  - id: feature.flows.x.p1
    kind: policy
    summary: "p"
    implemented_by: [ cap.services.x.create ]
`)
	c.errs = nil
	c.checkFeatureFlows()
	mustContainRule(t, c.errs, "flow-rule-verified")

	writeCapFile(t, root, "scripts/cmd/internal/check/fake_test.go", `package check

import "testing"

func TestFake(t *testing.T) {
	mustContainRule(t, []string{"[feature.flows.x.p1]"}, "feature.flows.x.p1")
}
`)
	writeCapFile(t, root, "contracts/features/flows/x.yaml", `id: feature.flows.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
scope: ui
entry: feature.routes.x
steps:
  - id: s1
    page: feature.pages.x
    action: submit
    rules: [ feature.flows.x.p1 ]
rules:
  - id: feature.flows.x.p1
    kind: policy
    summary: "p"
    implemented_by: [ cap.services.x.create ]
    verified_by: [ scripts/cmd/internal/check/fake_test.go ]
`)
	c.errs = nil
	c.checkFeatureFlows()
	if got := strings.Join(c.errs, "\n"); strings.Contains(got, "flow-rule-verified") {
		t.Fatalf("verified policy still reported: %s", got)
	}

	writeCapFile(t, root, "scripts/cmd/internal/check/fake_test.go", `package check
// feature.flows.x.p1
`)
	c.errs = nil
	c.checkFeatureFlows()
	mustContainRule(t, c.errs, "flow-rule-verified")
}

func TestFlowDataBoundary(t *testing.T) {
	c := &Checker{}
	c.scanDataRefs("x.yaml", "", map[string]any{
		"data": map[string]any{
			"bad": map[string]any{"capability": "data.tables.account"},
		},
	})
	mustContainRule(t, c.errs, "flow-data-boundary")
}

func TestPageActionAndFieldRules(t *testing.T) {
	c := &Checker{ruleIDSet: map[string]bool{}, errCodeSet: map[string]bool{}}
	c.checkPageActionLifecycle("x.yaml", map[string]any{
		"data": map[string]any{"list": map[string]any{}},
		"actions": map[string]any{
			"submit": map[string]any{
				"available_when": map[string]any{"rules": []any{"feature.flows.x.missing"}},
				"on_error":       []any{map[string]any{"error": "platform.errors.NOPE", "ui": "toast"}},
				"invalidates":    []any{"missing"},
			},
		},
	})
	mustContainRule(t, c.errs, "page-action-lifecycle")

	c = &Checker{
		entities: map[string]*entityInfo{
			"data.entities.x": {Fields: []entityFieldInfo{
				{JSONName: "known", Sensitivity: "public"},
				{JSONName: "password", Sensitivity: "secret"},
			}},
		},
		methodReturns: map[string]map[string]returnType{
			"cap.services.x.get": {"x": {typ: "data.entities.x"}},
		},
	}
	c.checkPageFieldRefs("x.yaml", map[string]any{
		"data":   map[string]any{"detail": map[string]any{"capability": "cap.services.x.get"}},
		"fields": []any{map[string]any{"name": "ghost"}},
	})
	mustContainRule(t, c.errs, "page-field-refs")

	c.errs = nil
	c.checkPageFieldRefs("x.yaml", map[string]any{
		"data":   map[string]any{"detail": map[string]any{"capability": "cap.services.x.get"}},
		"fields": []any{map[string]any{"name": "password"}},
	})
	mustContainRule(t, c.errs, "page-field-sensitivity")

	c.errs = nil
	c.checkPageFieldRefs("x.yaml", map[string]any{
		"data": map[string]any{"detail": map[string]any{
			"capability": "cap.services.x.get",
			"params":     map[string]any{"q": map[string]any{"from": "form.q"}},
		}},
		"filters": []any{map[string]any{"name": "q"}},
	})
	mustContainRule(t, c.errs, "page-field-refs")

	if got := strings.Join(c.errs, "\n"); strings.Contains(got, "known") {
		t.Fatalf("known field should not be reported: %s", got)
	}
}

func TestFlowRuleDSL(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "feature.flows")
	rel := "contracts/features/flows/x.yaml"
	writeCapFile(t, root, rel, `id: feature.flows.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
scope: api
steps: [ ]
rules:
  - id: feature.flows.x.i1
    kind: invariant
    summary: "bad"
    assert: "len("
    implemented_by: [ cap.services.x.create ]
`)
	doc, err := loadYAML(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, errCodeSet: map[string]bool{}}
	c.checkRuleDoc(doc, rel, schema, map[string]bool{"cap.services.x.create": true})
	mustContainRule(t, c.errs, "flow-rule-dsl")
}

func TestFlowRuleRegistry(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "scripts/cmd/.keep", "")
	ref := "scripts/cmd/internal/check/fake_test.go"
	doc := map[string]any{
		"rules": []any{map[string]any{
			"id":          "feature.flows.x.p1",
			"kind":        "policy",
			"verified_by": []any{ref},
		}},
	}

	writeCapFile(t, root, ref, `package check
var policyExpectations = map[string]struct{}{"other": {}}
`)
	c := &Checker{root: root}
	c.errs = nil
	c.checkFlowRuleVerification("x.yaml", doc, nil)
	mustContainRule(t, c.errs, "flow-rule-verified")

	writeCapFile(t, root, ref, `package check
var policyExpectations = map[string]struct{}{"feature.flows.x.p1": {}}
`)
	c.errs = nil
	c.checkFlowRuleVerification("x.yaml", doc, nil)
	if got := strings.Join(c.errs, "\n"); strings.Contains(got, "flow-rule-verified") {
		t.Fatalf("registered policy still reported: %s", got)
	}
}
