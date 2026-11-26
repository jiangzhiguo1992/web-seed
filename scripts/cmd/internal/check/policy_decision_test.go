package check

import (
	"path/filepath"
	"strings"
	"testing"
)

func policyDecisionDoc(decision string) string {
	return `id: feature.flows.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not: []
scope: api
steps: [ ]
rules:
  - id: feature.flows.x.p1
    kind: policy
    summary: "p"
    errors: [ platform.errors.COMMON_FORBIDDEN ]
` + decision + `
    implemented_by: [ cap.services.x.create ]
`
}

func TestPolicyDecisionRules(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "feature.flows")
	rel := "contracts/features/flows/x.yaml"
	services := map[string]bool{"cap.services.x.create": true, "cap.services.x.call": true}
	cases := []struct {
		name     string
		decision string
		rule     string
		want     bool
	}{
		{"missing", "", "policy-decision", true},
		{"deny without error", `    decision:
      - when: "true"
        effect: deny
`, "policy-decision", true},
		{"bad when", `    decision:
      - when: "len("
        effect: deny
        error: platform.errors.COMMON_FORBIDDEN
`, "flow-rule-dsl", true},
		{"unknown call target", `    decision:
      - when: "true"
        effect: call
        target: cap.services.x.missing
`, "policy-decision", true},
		{"bad forbid value", `    decision:
      - when: "true"
        effect: forbid
        target: verification_action
        value: patch
`, "policy-decision", true},
		{"allow with target", `    decision:
      - when: "true"
        effect: allow
        target: cap.services.x.call
`, "policy-decision", true},
		{"valid deny", `    decision:
      - when: "true"
        effect: deny
        error: platform.errors.COMMON_FORBIDDEN
`, "policy-decision", false},
		{"valid call", `    decision:
      - when: "true"
        effect: call
        target: cap.services.x.call
        on_failure: rollback
`, "policy-decision", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			writeCapFile(t, root, rel, policyDecisionDoc(tc.decision))
			doc, err := loadYAML(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			c := &Checker{root: root, errCodeSet: map[string]bool{"COMMON_FORBIDDEN": true}}
			c.checkRuleDoc(doc, rel, schema, services)
			got := strings.Contains(strings.Join(c.errs, "\n"), tc.rule)
			if tc.want {
				mustContainRule(t, c.errs, tc.rule)
			} else if got {
				t.Fatalf("%s reported: %s", tc.rule, strings.Join(c.errs, "\n"))
			}
		})
	}
}
