package check

import (
	"strings"
	"testing"
)

func TestConstraintRefs(t *testing.T) {
	c := &Checker{
		constraintIDs: map[string]bool{"data.tables.x.ck_x_code": true},
		constraintRefs: map[string]map[string]bool{
			"data.tables.x.ck_x_code": {"data.queries.x.read": true},
		},
		queries: map[string]*queryInfo{
			"data.queries.x.read": {ID: "data.queries.x.read", Targets: []string{"data.tables.x"}},
		},
	}
	c.checkConstraintRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "[constraint-ref]") {
		t.Fatalf("non-writing query not reported: %v", c.errs)
	}
	mustContainRule(t, c.errs, "constraint-ref")

	c.errs = nil
	c.constraintRefs = map[string]map[string]bool{"data.tables.x.ck_missing": {"data.queries.x.read": true}}
	c.checkConstraintRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown constraint") {
		t.Fatalf("unknown constraint not reported: %v", c.errs)
	}
}

func TestValidationRefs(t *testing.T) {
	e := &entityInfo{ID: "data.entities.x", Primary: "data.tables.x", Doc: map[string]any{
		"validation": []any{map[string]any{"name": "v"}},
	}}
	c := &Checker{
		entities:       map[string]*entityInfo{"data.entities.x": e},
		methodReturns:  map[string]map[string]returnType{"cap.services.x.get": {}},
		serviceQueries: map[string]map[string]bool{"cap.services.x.get": {"data.queries.x.get": true}},
		queryTables:    map[string]map[string]bool{"data.queries.x.get": {"data.tables.x": true}},
		validationRefs: map[string]map[string]bool{"data.entities.x.missing": {"cap.services.x.get": true}},
	}
	c.checkValidationRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "[entity-validation-ref]") {
		t.Fatalf("unknown validation not reported: %v", c.errs)
	}
	mustContainRule(t, c.errs, "entity-validation-ref")

	c.errs = nil
	c.validationRefs = map[string]map[string]bool{"data.entities.x.v": {"cap.services.x.get": true}}
	c.queryTables = map[string]map[string]bool{"data.queries.x.get": {"data.tables.y": true}}
	c.checkValidationRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "does not query entity table") {
		t.Fatalf("service without entity query not reported: %v", c.errs)
	}
}
