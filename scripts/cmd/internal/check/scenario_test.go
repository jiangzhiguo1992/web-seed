package check

import (
	"strings"
	"testing"
)

func TestScenarioRules(t *testing.T) {
	c := &Checker{
		httpOps: map[string]httpOpInfo{
			"cap.protocols.http.x.get": {
				Path: "/x", Method: "get", Service: "cap.services.x.get",
				Statuses: map[int]bool{200: true, 404: true},
				Codes:    map[string]bool{"platform.errors.X": true},
			},
		},
		serviceQueryClosure: map[string]map[string]bool{
			"cap.services.x.get": {"data.queries.x.get": true},
		},
		queries: map[string]*queryInfo{
			"data.queries.x.get": {Targets: []string{"data.tables.x"}},
		},
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {"id": {}, "name": {}},
		},
		permissionSet: map[string]bool{"platform.security.permissions.x": true},
		eventPayloads: map[string]map[string]eventField{"cap.events.x.created": {}},
	}
	c.checkScenarioItem("where", map[string]any{
		"operation": "cap.protocols.http.x.get",
		"actor": map[string]any{
			"anonymous":   false,
			"permissions": []any{"platform.security.permissions.missing"},
		},
		"given": map[string]any{
			"db": map[string]any{
				"data.tables.other": []any{},
			},
		},
		"then": map[string]any{
			"response": map[string]any{"status": 500},
			"db": map[string]any{
				"added":     map[string]any{},
				"changed":   map[string]any{},
				"removed":   map[string]any{"data.tables.other": []any{}},
				"unchanged": "loose",
			},
			"events": []any{"cap.events.x.missing"},
		},
	}, map[string]bool{"platform.security.roles.admin": true})
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"unknown permission",
		"unknown event",
		"status 500 is not an effective response",
		"given.db table data.tables.other is not in the service chain",
		"then.db.removed table data.tables.other is not written",
		"then.db.unchanged must be strict",
		"table data.tables.x is in the service chain but not covered",
		"[scenario-refs]",
		"[scenario-db]",
		"[scenario-structure]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestScenarioUnknownOperation(t *testing.T) {
	c := &Checker{httpOps: map[string]httpOpInfo{}}
	c.checkScenarioItem("where", map[string]any{
		"operation": "cap.protocols.http.x.missing",
		"actor":     map[string]any{"anonymous": true},
		"given":     map[string]any{"db": map[string]any{}},
		"then":      map[string]any{"response": map[string]any{"status": 200}, "db": map[string]any{"added": map[string]any{}, "changed": map[string]any{}, "removed": map[string]any{}, "unchanged": "strict"}},
	}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown operation") {
		t.Fatalf("unknown operation not reported: %v", c.errs)
	}
}
