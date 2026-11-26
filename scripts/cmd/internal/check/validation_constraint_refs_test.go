package check

import (
	"path/filepath"
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

	// 多表写：写 A+B 的查询引用 A 上的约束合法，引用未写的 C 必须报错。
	c.errs = nil
	c.constraintIDs = map[string]bool{"data.tables.a.ck_a": true, "data.tables.c.ck_c": true}
	c.constraintRefs = map[string]map[string]bool{"data.tables.a.ck_a": {"data.queries.x.write": true}}
	c.queries = map[string]*queryInfo{
		"data.queries.x.write": {ID: "data.queries.x.write", Writes: []string{"data.tables.a", "data.tables.b"}},
	}
	c.checkConstraintRefs()
	if len(c.errs) != 0 {
		t.Fatalf("multi-table write referencing written table reported: %v", c.errs)
	}

	c.errs = nil
	c.constraintRefs = map[string]map[string]bool{"data.tables.c.ck_c": {"data.queries.x.write": true}}
	c.checkConstraintRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "does not write table data.tables.c") {
		t.Fatalf("multi-table write referencing unwritten table not reported: %v", c.errs)
	}
}

func TestMissingGeneratedUsedByIsRequired(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{
		"name": "ck_x", "expr": "x > 0", "on_violation": "platform.errors.COMMON_INTERNAL",
	}}
	c := &Checker{}
	c.validateDoc(doc, schema.Fields, "contracts/data/tables/x.yaml")
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, `[schema.required]`) || !strings.Contains(got, `missing required field "used_by"`) {
		t.Fatalf("missing generated used_by must stay required: %v", c.errs)
	}
}

func TestGeneratedRangeDriftFiltering(t *testing.T) {
	if !isGeneratedDriftError("[schema.generated-range] x.yaml: 0 items < min_items 1") {
		t.Fatal("generated range must be treated as gen-graph drift")
	}
	if isGeneratedDriftError("[constraint-ref] data.tables.x.ck_missing: unknown constraint") {
		t.Fatal("forward constraint ref must not be filtered")
	}
	if isGeneratedDriftError("[entity-validation-ref] data.entities.x.missing: unknown validation") {
		t.Fatal("forward validation ref must not be filtered")
	}
	if !isGeneratedDriftError("[query-index-ref] query data.queries.x.get: index uq_x_name does not list the query in used_by") {
		t.Fatal("index used_by drift must be treated as gen-graph drift")
	}
	if isGeneratedDriftError("[query-index-ref] query data.queries.x.get: index_ref table data.tables.y is not read by the query") {
		t.Fatal("forward index_ref table must not be filtered")
	}
	if !isGeneratedDriftError("[query-index-used-by] query data.queries.x.get: index uq_x_name lists the query but index_ref does not") {
		t.Fatal("reverse index used_by drift must be treated as gen-graph drift")
	}
	if !isGeneratedDriftError("[index-used-by-ref] x: indexes[0] (uq_x_name): unknown query data.queries.x.gone") {
		t.Fatal("stale generated index used_by entry must be treated as gen-graph drift")
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
		serviceStepIDs: map[string]bool{"cap.services.x.get.load": true},
		stepService:    map[string]string{"cap.services.x.get.load": "cap.services.x.get"},
		validationRefs: map[string]map[string]bool{"data.entities.x.missing": {"cap.services.x.get.load": true}},
	}
	c.checkValidationRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "[entity-validation-ref]") {
		t.Fatalf("unknown validation not reported: %v", c.errs)
	}
	mustContainRule(t, c.errs, "entity-validation-ref")

	c.errs = nil
	c.validationRefs = map[string]map[string]bool{"data.entities.x.v": {"cap.services.x.get.load": true}}
	c.queryTables = map[string]map[string]bool{"data.queries.x.get": {"data.tables.y": true}}
	c.checkValidationRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "does not query entity table") {
		t.Fatalf("service without entity query not reported: %v", c.errs)
	}

	// 跨模块 service：模块不参与归属判断，只要它查询实体主表即可。
	c.errs = nil
	c.methodReturns["cap.services.other.get"] = map[string]returnType{}
	c.serviceQueries["cap.services.other.get"] = map[string]bool{"data.queries.other.get": true}
	c.queryTables["data.queries.other.get"] = map[string]bool{"data.tables.x": true}
	c.serviceStepIDs["cap.services.other.get.load"] = true
	c.stepService["cap.services.other.get.load"] = "cap.services.other.get"
	c.validationRefs = map[string]map[string]bool{"data.entities.x.v": {"cap.services.other.get.load": true}}
	c.checkValidationRefs()
	if len(c.errs) != 0 {
		t.Fatalf("cross-module service rejected: %v", c.errs)
	}

	c.errs = nil
	c.queryTables["data.queries.other.get"] = map[string]bool{"data.tables.y": true}
	c.validationRefs = map[string]map[string]bool{"data.entities.x.v": {"cap.services.other.get.load": true}}
	c.checkValidationRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "does not query entity table") {
		t.Fatalf("cross-module service without entity query not reported: %v", c.errs)
	}

	c.errs = nil
	c.validationRefs = map[string]map[string]bool{"data.entities.x.v": {"cap.services.other.get.missing": true}}
	c.checkValidationRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown step") {
		t.Fatalf("unknown step not reported: %v", c.errs)
	}
}
