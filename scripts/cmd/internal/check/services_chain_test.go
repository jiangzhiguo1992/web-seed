// services_chain_test.go 覆盖 service steps 的输入/输出数据链硬约束。
package check

import (
	"strings"
	"testing"
)

func runServiceItem(t *testing.T, doc map[string]any, c *Checker) string {
	t.Helper()
	if c.errCodeSet == nil {
		c.errCodeSet = map[string]bool{"COMMON_INTERNAL": true}
	}
	c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
		map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
	return strings.Join(c.errs, "\n")
}

func TestServiceQueryInputRequiredAndExact(t *testing.T) {
	q := &queryInfo{
		ID:      "data.queries.x.q",
		Params:  []queryParam{{Name: "id"}, {Name: "note", Nullable: true}},
		Returns: []queryReturn{{Name: "row", Kind: "scalar", Type: "string"}},
	}
	base := func(input map[string]any) map[string]any {
		step := map[string]any{"id": "run", "kind": "query", "summary": "x", "query": q.ID}
		if input != nil {
			step["input"] = input
		}
		return map[string]any{
			"summary": "x", "transaction": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{map[string]any{"name": "row", "type": "string"}},
			"steps":   []any{step},
		}
	}
	checker := func() *Checker {
		return &Checker{
			queries:          map[string]*queryInfo{q.ID: q},
			queryReturnTypes: map[string]map[string]string{q.ID: {"row": "string"}},
		}
	}

	if got := runServiceItem(t, base(map[string]any{"id": "input.ctx"}), checker()); strings.Contains(got, "missing required param id") {
		t.Fatalf("nullable param treated as required: %s", got)
	}
	if got := runServiceItem(t, base(nil), checker()); !strings.Contains(got, "missing required param id") {
		t.Fatalf("missing required query param not reported: %s", got)
	}
	if got := runServiceItem(t, base(map[string]any{"id": "input.ctx", "nope": "input.ctx"}), checker()); !strings.Contains(got, "has no param nope") {
		t.Fatalf("extra query param not reported: %s", got)
	}
}

func TestServiceCallInputRequiredAndExact(t *testing.T) {
	target := serviceMeta{
		module:         "x",
		visibility:     "public",
		params:         map[string]bool{"account_id": true, "ctx": true},
		requiredParams: map[string]bool{"account_id": true},
		returns:        map[string]returnType{"value": {typ: "string"}},
	}
	base := func(input map[string]any) map[string]any {
		step := map[string]any{"id": "call", "kind": "service", "summary": "x", "service": "cap.services.x.target"}
		if input != nil {
			step["input"] = input
		}
		return map[string]any{
			"summary": "x", "transaction": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}, map[string]any{"name": "account_id", "type": "integer"}},
			"returns": []any{map[string]any{"name": "value", "type": "string"}},
			"steps":   []any{step},
		}
	}
	checker := func() *Checker {
		return &Checker{}
	}
	run := func(doc map[string]any) string {
		c := checker()
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{"cap.services.x.target": target}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}

	if got := run(base(map[string]any{"account_id": "input.account_id"})); strings.Contains(got, "missing required param") || strings.Contains(got, "must not pass ctx") {
		t.Fatalf("valid service input reported: %s", got)
	}
	if got := run(base(nil)); !strings.Contains(got, "missing required param account_id") {
		t.Fatalf("missing required service param not reported: %s", got)
	}
	if got := run(base(map[string]any{"account_id": "input.account_id", "ctx": "input.ctx"})); !strings.Contains(got, "must not pass ctx") {
		t.Fatalf("ctx input not reported: %s", got)
	}
}

func TestServiceChainOutputAndParamUsage(t *testing.T) {
	base := func(steps []any, params []any, returns []any) map[string]any {
		return map[string]any{
			"summary": "x", "transaction": "read_only",
			"params": params, "returns": returns, "steps": steps,
		}
	}
	run := func(doc map[string]any) string { return runServiceItem(t, doc, &Checker{}) }

	t.Run("returns must have a producing step", func(t *testing.T) {
		doc := base([]any{map[string]any{
			"id": "run", "kind": "logic", "summary": "x",
			"errors": []any{}, "output": []any{},
		}}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{map[string]any{"name": "value", "type": "string"}})
		if got := run(doc); !strings.Contains(got, "[service-output] f: return value has no producing step") {
			t.Fatalf("missing return source not reported: %s", got)
		}
	})

	t.Run("explicit output must be consumed or returned", func(t *testing.T) {
		doc := base([]any{map[string]any{
			"id": "run", "kind": "logic", "summary": "x",
			"errors": []any{}, "output": []any{map[string]any{"name": "other", "type": "string"}},
		}}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{})
		if got := run(doc); !strings.Contains(got, "output other is not consumed") {
			t.Fatalf("unused output not reported: %s", got)
		}
	})

	t.Run("output consumed by later step input", func(t *testing.T) {
		doc := base([]any{
			map[string]any{
				"id": "load", "kind": "logic", "summary": "x",
				"errors": []any{}, "output": []any{map[string]any{"name": "value", "type": "string"}},
			},
			map[string]any{
				"id": "use", "kind": "logic", "summary": "x", "depends_on": []any{"load"},
				"errors": []any{}, "input": map[string]any{"v": "step.load.value"},
				"output": []any{},
			},
		}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{})
		if got := run(doc); len(strings.TrimSpace(got)) != 0 {
			t.Fatalf("consumed output reported: %s", got)
		}
	})

	t.Run("step reference requires depends_on", func(t *testing.T) {
		doc := base([]any{
			map[string]any{
				"id": "load", "kind": "logic", "summary": "x",
				"errors": []any{}, "output": []any{map[string]any{"name": "value", "type": "string"}},
			},
			map[string]any{
				"id": "use", "kind": "logic", "summary": "x",
				"errors": []any{}, "input": map[string]any{"v": "step.load.value"},
				"output": []any{},
			},
		}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{})
		if got := run(doc); !strings.Contains(got, "not declared in depends_on") {
			t.Fatalf("missing depends_on not reported: %s", got)
		}
	})

	t.Run("unused service param", func(t *testing.T) {
		doc := base([]any{map[string]any{
			"id": "run", "kind": "logic", "summary": "x",
			"errors": []any{}, "output": []any{},
		}}, []any{
			map[string]any{"name": "ctx", "type": "context.Context"},
			map[string]any{"name": "value", "type": "string"},
		}, []any{})
		if got := run(doc); !strings.Contains(got, "param value is not consumed") {
			t.Fatalf("unused param not reported: %s", got)
		}
	})
}

func TestServiceLogicQueriesInput(t *testing.T) {
	q := &queryInfo{ID: "data.queries.x.q", Params: []queryParam{{Name: "id"}}}
	doc := func(input map[string]any) map[string]any {
		item := map[string]any{"query": q.ID}
		if input != nil {
			item["input"] = input
		}
		return map[string]any{
			"summary": "x", "transaction": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}, map[string]any{"name": "id", "type": "integer"}},
			"returns": []any{},
			"steps": []any{map[string]any{
				"id": "run", "kind": "logic", "summary": "x",
				"errors": []any{}, "output": []any{},
				"queries": []any{item},
			}},
		}
	}
	run := func(doc map[string]any) string {
		c := &Checker{queries: map[string]*queryInfo{q.ID: q}}
		return runServiceItem(t, doc, c)
	}

	if got := run(doc(map[string]any{"id": "input.id"})); len(strings.TrimSpace(got)) != 0 {
		t.Fatalf("valid logic query input reported: %s", got)
	}
	if got := run(doc(nil)); !strings.Contains(got, "missing required param id") {
		t.Fatalf("missing logic query input not reported: %s", got)
	}
	if got := run(doc(map[string]any{"id": "input.id", "extra": "input.id"})); !strings.Contains(got, "has no param extra") {
		t.Fatalf("extra logic query input not reported: %s", got)
	}
}

func TestServiceNestedInputPath(t *testing.T) {
	c := &Checker{methodTypeDefs: map[string]map[string]any{
		"Filter": {
			"type": "object",
			"properties": []any{
				map[string]any{"name": "status", "schema": map[string]any{"type": "integer"}},
			},
		},
	}}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "ctx", "type": "context.Context"},
		map[string]any{"name": "filter", "type": "Filter"},
	}}
	c.checkServiceParamExpr("input.filter.status", doc, map[string]int{}, nil, nil, map[string]bool{}, "f", "s")
	if len(c.errs) != 0 {
		t.Fatalf("valid nested input reported: %v", c.errs)
	}
	c.errs = nil
	c.checkServiceParamExpr("input.filter.missing", doc, map[string]int{}, nil, nil, map[string]bool{}, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown field missing") {
		t.Fatalf("unknown nested input not reported: %v", c.errs)
	}
}

func TestServiceExprInput(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "ctx", "type": "context.Context"},
	}}
	c.checkServiceParamExpr("expr: hash(password)", doc, map[string]int{}, nil, nil, nil, "f", "s")
	if len(c.errs) != 0 {
		t.Fatalf("valid expr reported: %v", c.errs)
	}
	c.errs = nil
	c.checkServiceParamExpr("expr: ", doc, map[string]int{}, nil, nil, nil, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "empty expr") {
		t.Fatalf("empty expr not reported: %v", c.errs)
	}
}

func TestServiceReadOnlyRejectsWriteQuery(t *testing.T) {
	q := &queryInfo{
		ID:      "data.queries.x.write",
		Targets: []string{"data.tables.x"},
		Writes:  []string{"data.tables.x"},
	}
	base := func(transaction string) map[string]any {
		return map[string]any{
			"summary": "x", "transaction": transaction,
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps": []any{map[string]any{
				"id": "run", "kind": "query", "summary": "x", "query": q.ID,
			}},
		}
	}
	run := func(transaction string) string {
		c := &Checker{
			queries:          map[string]*queryInfo{q.ID: q},
			queryReturnTypes: map[string]map[string]string{q.ID: {}},
		}
		c.checkServiceItem(base(transaction), "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}

	if got := run("read_only"); !strings.Contains(got, "calls write query") {
		t.Fatalf("read_only write query not reported: %s", got)
	}
	if got := run("required"); strings.Contains(got, "calls write query") {
		t.Fatalf("required write query reported: %s", got)
	}
}

func TestServiceRuleOnViolationMustBeDeclared(t *testing.T) {
	base := func(errors []any) map[string]any {
		return map[string]any{
			"summary": "x", "transaction": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps": []any{map[string]any{
				"id": "run", "kind": "logic", "summary": "x",
				"errors": errors, "output": []any{},
				"rules": []any{"feature.rules.x.r"},
			}},
		}
	}
	run := func(errors []any) string {
		serviceErrors := map[string]bool{}
		if len(errors) > 0 {
			serviceErrors["platform.errors.A"] = true
		}
		meta := map[string]serviceMeta{
			"cap.services.x.run": {module: "x", visibility: "public", errors: serviceErrors},
		}
		c := &Checker{ruleErrors: map[string]string{"feature.rules.x.r": "platform.errors.A"}}
		c.checkServiceItem(base(errors), "f", "cap.services.x.run", "x",
			meta, map[string]bool{"feature.rules.x.r": true}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}

	if got := run([]any{}); !strings.Contains(got, "rule feature.rules.x.r error platform.errors.A is not declared") {
		t.Fatalf("rule error not reported: %s", got)
	}
	if got := run([]any{"platform.errors.A"}); strings.Contains(got, "is not declared by service") {
		t.Fatalf("declared rule error reported: %s", got)
	}
}

func TestServiceReturnTypeMismatch(t *testing.T) {
	base := func(retType, outType string) map[string]any {
		return map[string]any{
			"summary": "x", "transaction": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{map[string]any{"name": "account", "type": retType}},
			"steps": []any{map[string]any{
				"id": "run", "kind": "logic", "summary": "x",
				"errors": []any{},
				"output": []any{map[string]any{"name": "account", "type": outType}},
			}},
		}
	}
	run := func(retType, outType string) string {
		return runServiceItem(t, base(retType, outType), &Checker{})
	}

	if got := run("data.entities.account", "string"); !strings.Contains(got, "does not match producing types") {
		t.Fatalf("type mismatch not reported: %s", got)
	}
	if got := run("List<data.entities.account>", "List<data.entities.account>"); strings.Contains(got, "does not match") {
		t.Fatalf("same list type reported: %s", got)
	}
	if got := run("*TokenPair", "TokenPair"); strings.Contains(got, "does not match") {
		t.Fatalf("pointer-equivalent type reported: %s", got)
	}
}

func TestServiceReturnNullability(t *testing.T) {
	base := func(retNullable, outNullable bool) map[string]any {
		ret := map[string]any{"name": "account", "type": "string"}
		if retNullable {
			ret["nullable"] = true
		}
		out := map[string]any{"name": "account", "type": "string"}
		if outNullable {
			out["nullable"] = true
		}
		return map[string]any{
			"summary": "x", "transaction": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{ret},
			"steps": []any{map[string]any{
				"id": "run", "kind": "logic", "summary": "x",
				"errors": []any{}, "output": []any{out},
			}},
		}
	}
	run := func(retNullable, outNullable bool) string {
		return runServiceItem(t, base(retNullable, outNullable), &Checker{})
	}

	if got := run(false, false); strings.Contains(got, "nullability") {
		t.Fatalf("matching non-nullable reported: %s", got)
	}
	if got := run(true, true); strings.Contains(got, "nullability") {
		t.Fatalf("matching nullable reported: %s", got)
	}
	if got := run(true, false); !strings.Contains(got, "nullability") {
		t.Fatalf("nullable return from non-nullable output not reported: %s", got)
	}
	if got := run(false, true); !strings.Contains(got, "nullability") {
		t.Fatalf("non-nullable return from nullable output not reported: %s", got)
	}
}
