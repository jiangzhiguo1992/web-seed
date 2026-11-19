// services_chain_test.go 覆盖 service steps 的输入/输出数据链硬约束。
package check

import (
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func inputList(m map[string]any) []any {
	var items []any
	for name, value := range m {
		items = append(items, map[string]any{"name": name, "value": value, "description": "x"})
	}
	return items
}

func runServiceItem(t *testing.T, doc map[string]any, c *Checker) string {
	t.Helper()
	if c.errCodeSet == nil {
		c.errCodeSet = map[string]bool{"COMMON_INTERNAL": true}
	}
	c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
		map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]lifecycleAction{}, map[string]bool{})
	return strings.Join(c.errs, "\n")
}

func TestServiceQueryInputRequiredAndExact(t *testing.T) {
	q := &queryInfo{
		ID:      "data.queries.x.q",
		Params:  []queryParam{{Name: "id"}, {Name: "note", Nullable: true}},
		Returns: []queryReturn{{Name: "row", Kind: "scalar", Type: "string"}, {Name: "err", Kind: "error"}},
	}
	base := func(input map[string]any) map[string]any {
		step := map[string]any{"id": "run", "kind": "query", "summary": "x", "query": map[string]any{
			"query": q.ID, "input": inputList(input),
		}}
		return map[string]any{
			"summary": "x", "tx_mode": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{map[string]any{"name": "row", "type": "string"}},
			"steps":   []any{step},
		}
	}
	checker := func() *Checker {
		return &Checker{
			queries:          map[string]*queryInfo{q.ID: q},
			queryReturnTypes: map[string]map[string]returnType{q.ID: {"row": {typ: "string"}}},
		}
	}

	if got := runServiceItem(t, base(map[string]any{"id": "params.ctx"}), checker()); strings.Contains(got, "missing required param id") {
		t.Fatalf("nullable param treated as required: %s", got)
	}
	if got := runServiceItem(t, base(nil), checker()); !strings.Contains(got, "missing required param id") {
		t.Fatalf("missing required query param not reported: %s", got)
	}
	if got := runServiceItem(t, base(map[string]any{"id": "params.ctx", "nope": "params.ctx"}), checker()); !strings.Contains(got, "has no param nope") {
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
		item := map[string]any{"service": "cap.services.x.target"}
		if input != nil {
			items := make([]any, 0, len(input))
			for name, value := range input {
				items = append(items, map[string]any{"name": name, "value": value, "description": "x"})
			}
			item["input"] = items
		}
		step := map[string]any{"id": "call", "kind": "service", "summary": "x", "service": item}
		return map[string]any{
			"summary": "x", "tx_mode": "read_only",
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
			map[string]serviceMeta{"cap.services.x.target": target}, map[string]bool{}, map[string]bool{}, map[string]lifecycleAction{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}

	if got := run(base(map[string]any{"account_id": "params.account_id"})); strings.Contains(got, "missing required param") || strings.Contains(got, "must not pass ctx") {
		t.Fatalf("valid service input reported: %s", got)
	}
	if got := run(base(nil)); !strings.Contains(got, "missing required param account_id") {
		t.Fatalf("missing required service param not reported: %s", got)
	}
	if got := run(base(map[string]any{"account_id": "params.account_id", "ctx": "params.ctx"})); !strings.Contains(got, "must not pass ctx") {
		t.Fatalf("ctx input not reported: %s", got)
	}
}

func TestServiceChainOutputAndParamUsage(t *testing.T) {
	base := func(steps []any, params []any, returns []any) map[string]any {
		return map[string]any{
			"summary": "x", "tx_mode": "read_only",
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
				"errors": []any{}, "input": []any{map[string]any{"name": "v", "value": "step.load.value", "description": "x"}},
				"output": []any{},
			},
		}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{})
		if got := run(doc); len(strings.TrimSpace(got)) != 0 {
			t.Fatalf("consumed output reported: %s", got)
		}
	})

	t.Run("step reference does not require depends_on", func(t *testing.T) {
		doc := base([]any{
			map[string]any{
				"id": "load", "kind": "logic", "summary": "x",
				"errors": []any{}, "output": []any{map[string]any{"name": "value", "type": "string"}},
			},
			map[string]any{
				"id": "use", "kind": "logic", "summary": "x",
				"errors": []any{}, "input": []any{map[string]any{"name": "v", "value": "step.load.value", "description": "x"}},
				"output": []any{},
			},
		}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{})
		if got := run(doc); strings.TrimSpace(got) != "" {
			t.Fatalf("step reference without depends_on reported: %s", got)
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
	c.checkServiceParamExpr("params.filter.status", doc, map[string]int{}, nil, nil, "f", "s")
	if len(c.errs) != 0 {
		t.Fatalf("valid nested input reported: %v", c.errs)
	}
	c.errs = nil
	c.checkServiceParamExpr("params.filter.missing", doc, map[string]int{}, nil, nil, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown field missing") {
		t.Fatalf("unknown nested input not reported: %v", c.errs)
	}
}

func TestServiceExprInputRejected(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "ctx", "type": "context.Context"},
	}}
	c.checkServiceParamExpr("expr: hash(password)", doc, map[string]int{}, nil, nil, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "inline expressions are not allowed") {
		t.Fatalf("expr input not rejected: %v", c.errs)
	}
}

func TestServiceInputSourceIsHardConstrained(t *testing.T) {
	q := &queryInfo{ID: "data.queries.x.q", Params: []queryParam{{Name: "id"}}}
	run := func(input any) string {
		step := map[string]any{"id": "run", "kind": "query", "summary": "x", "query": map[string]any{
			"query": q.ID, "input": input,
		}}
		doc := map[string]any{
			"summary": "x", "tx_mode": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}, map[string]any{"name": "id", "type": "integer"}},
			"returns": []any{},
			"steps":   []any{step},
		}
		c := &Checker{queries: map[string]*queryInfo{q.ID: q}}
		return runServiceItem(t, doc, c)
	}

	if got := run(inputList(map[string]any{"id": "input.id"})); !strings.Contains(got, "must be params.<path>") {
		t.Fatalf("input.* source not rejected: %s", got)
	}
	if got := run(inputList(map[string]any{"id": "params.nope"})); !strings.Contains(got, "references params.nope") {
		t.Fatalf("unknown params source not rejected: %s", got)
	}
	if got := run(inputList(map[string]any{"id": "expr: step.nope.x"})); !strings.Contains(got, "inline expressions are not allowed") {
		t.Fatalf("expr source not rejected: %s", got)
	}
	if got := run(inputList(map[string]any{"id": "step.nope.x"})); !strings.Contains(got, "not an earlier step") {
		t.Fatalf("unknown step source not rejected: %s", got)
	}
	if got := run(map[string]any{"id": "params.id"}); !strings.Contains(got, "input must be a list") {
		t.Fatalf("map input not rejected: %s", got)
	}
}

func TestServiceParamDefault(t *testing.T) {
	c := &Checker{}
	c.checkServiceParamDefault(map[string]any{"name": "limit", "type": "integer", "nullable": true, "default": 20}, "f", "s")
	if len(c.errs) != 0 {
		t.Fatalf("valid default reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceParamDefault(map[string]any{"name": "limit", "type": "integer", "default": 20}, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "default requires nullable:true") {
		t.Fatalf("non-nullable default not reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceParamDefault(map[string]any{"name": "limit", "type": "integer", "nullable": true, "default": "x"}, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "default must be an integer") {
		t.Fatalf("wrong default type not reported: %v", c.errs)
	}

	c.errs = nil
	c.checkServiceParamDefault(map[string]any{"name": "ctx", "type": "context.Context", "default": "x"}, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "ctx must not declare default") {
		t.Fatalf("ctx default not reported: %v", c.errs)
	}
}

func TestServiceTypeGrammar(t *testing.T) {
	c := &Checker{
		enumIDs:  map[string]bool{"data.enums.x": true},
		entities: map[string]*entityInfo{"data.entities.e": {}},
	}
	for _, typ := range []string{"integer", "context.Context", "error", "any", "*data.entities.e", "List<data.entities.e>", "data.enums.x"} {
		c.errs = nil
		c.checkTypeName(typ, map[string]bool{"Local": true}, "x")
		if len(c.errs) != 0 {
			t.Fatalf("valid type %s reported: %v", typ, c.errs)
		}
	}
	for typ, want := range map[string]string{
		"List<List<integer>>": "malformed type",
		"**integer":           "malformed type",
		"List<>":              "malformed type",
		"Map<integer>":        "malformed type",
		"data.foo.bar":        "unknown type",
		"Local":               "",
	} {
		c.errs = nil
		c.checkTypeName(typ, map[string]bool{"Local": true}, "x")
		got := strings.Join(c.errs, "\n")
		if want == "" {
			if got != "" {
				t.Fatalf("local type reported: %s", got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Fatalf("%s not reported as %q: %s", typ, want, got)
		}
	}
}

func TestServiceNestedRequiredTransactionRejected(t *testing.T) {
	target := serviceMeta{
		module:         "x",
		visibility:     "public",
		txMode:         "required",
		params:         map[string]bool{"ctx": true},
		requiredParams: map[string]bool{},
	}
	doc := map[string]any{
		"summary": "x", "tx_mode": "read_only",
		"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
		"returns": []any{},
		"steps": []any{
			map[string]any{"id": "call", "kind": "service", "summary": "x", "service": map[string]any{
				"service": "cap.services.x.target",
			}},
		},
	}
	c := &Checker{}
	c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
		map[string]serviceMeta{"cap.services.x.target": target}, map[string]bool{}, map[string]bool{}, map[string]lifecycleAction{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "must not call tx_mode=required") {
		t.Fatalf("nested required transaction not reported: %v", c.errs)
	}
}

func TestServiceInputTypeCompatibility(t *testing.T) {
	q := &queryInfo{
		ID:     "data.queries.x.q",
		Params: []queryParam{{Name: "id", Type: "integer"}, {Name: "name", Type: "text"}, {Name: "ids", Type: "List<integer>"}, {Name: "status", Type: "integer"}, {Name: "state", Type: "data.enums.account_status"}},
	}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "ctx", "type": "context.Context"},
		map[string]any{"name": "id", "type": "integer"},
		map[string]any{"name": "name", "type": "string"},
		map[string]any{"name": "code", "type": "page_limit"},
		map[string]any{"name": "ids", "type": "List<integer>"},
		map[string]any{"name": "status", "type": "data.enums.account_status"},
		map[string]any{"name": "state", "type": "data.enums.account_status"},
		map[string]any{"name": "raw", "type": "integer"},
	}}
	tc := stepTypeContext{doc: doc, stepIndex: map[string]int{}, steps: []any{}, stepOutputs: []map[string]returnType{}}
	c := &Checker{valueTypes: contractspec.ValueTypes{
		TypeFamilies: map[string]contractspec.ValueTypeFamily{"integer": {}},
		CustomTypes: map[string]contractspec.CustomValueType{
			"page_limit": {Family: "integer"},
		},
	}}
	c.checkQueryInput(map[string]any{
		"id": "params.id", "name": "params.name", "ids": "params.ids", "status": "params.status", "state": "params.state",
	}, q, q.ID, "f", "s", tc)
	if len(c.errs) != 0 {
		t.Fatalf("valid input types reported: %v", c.errs)
	}

	c.errs = nil
	c.checkQueryInput(map[string]any{"id": "params.name", "ids": "params.id", "state": "params.raw", "name": "params.name", "status": "params.status"}, q, q.ID, "f", "s", tc)
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"input id type string is not compatible with integer",
		"input ids type integer is not compatible with List<integer>",
		"input state type integer is not compatible with data.enums.account_status",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}

	// 自定义类型经类型族比较：page_limit（integer 族）可赋给 integer。
	c.errs = nil
	c.checkQueryInput(map[string]any{
		"id": "params.code", "name": "params.name", "ids": "params.ids", "status": "params.status", "state": "params.state",
	}, q, q.ID, "f", "s", tc)
	if len(c.errs) != 0 {
		t.Fatalf("custom type family reported: %v", c.errs)
	}
}

func TestServiceCallTypeCompatibility(t *testing.T) {
	target := serviceMeta{
		params:         map[string]bool{"ctx": true, "id": true, "label": true},
		requiredParams: map[string]bool{"id": true},
		paramDecls:     map[string]string{"id": "integer", "label": "*string"},
	}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "ctx", "type": "context.Context"},
		map[string]any{"name": "id", "type": "integer"},
		map[string]any{"name": "label", "type": "string"},
	}}
	tc := stepTypeContext{doc: doc, stepIndex: map[string]int{}, steps: []any{}, stepOutputs: []map[string]returnType{}}
	c := &Checker{}
	c.checkServiceInput(map[string]any{"id": "params.id", "label": "params.label"}, target, "cap.services.x.y", "f", "s", tc)
	if len(c.errs) != 0 {
		t.Fatalf("valid service input types reported: %v", c.errs)
	}
	c.errs = nil
	c.checkServiceInput(map[string]any{"id": "params.label"}, target, "cap.services.x.y", "f", "s", tc)
	if !strings.Contains(strings.Join(c.errs, "\n"), "input id type string is not compatible with integer") {
		t.Fatalf("service input type mismatch not reported: %v", c.errs)
	}
}

func TestServiceWriteClosure(t *testing.T) {
	writes := computeServiceWriteClosure(
		map[string]bool{"cap.services.x.writer": true},
		map[string][]string{
			"cap.services.x.a": {"cap.services.x.b"},
			"cap.services.x.b": {"cap.services.x.writer"},
		},
	)
	if !writes["cap.services.x.a"] || !writes["cap.services.x.b"] || !writes["cap.services.x.writer"] {
		t.Fatalf("write closure not propagated: %v", writes)
	}
	if writes["cap.services.x.reader"] {
		t.Fatalf("unexpected write: %v", writes)
	}
}

func TestServiceReadOnlyRejectsTransitiveWriter(t *testing.T) {
	target := serviceMeta{
		module:     "x",
		visibility: "public",
		txMode:     "none",
		params:     map[string]bool{"ctx": true},
	}
	doc := map[string]any{
		"summary": "x", "tx_mode": "read_only",
		"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
		"returns": []any{},
		"steps": []any{
			map[string]any{"id": "call", "kind": "service", "summary": "x", "service": map[string]any{
				"service": "cap.services.x.writer",
			}},
		},
	}
	c := &Checker{serviceWrites: map[string]bool{"cap.services.x.writer": true}}
	c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
		map[string]serviceMeta{"cap.services.x.writer": target}, map[string]bool{}, map[string]bool{}, map[string]lifecycleAction{}, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "tx_mode=read_only cannot call service cap.services.x.writer that writes tables") {
		t.Fatalf("transitive writer not reported: %v", c.errs)
	}
}

func TestServiceQueryStepRejectsStepInput(t *testing.T) {
	q := &queryInfo{ID: "data.queries.x.q", Params: []queryParam{{Name: "id"}}}
	doc := map[string]any{
		"summary": "x", "tx_mode": "none",
		"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}, map[string]any{"name": "id", "type": "integer"}},
		"returns": []any{},
		"steps": []any{map[string]any{
			"id": "run", "kind": "query", "summary": "x",
			"input": []any{map[string]any{"name": "id", "value": "params.id", "description": "x"}},
			"query": map[string]any{"query": q.ID, "input": inputList(map[string]any{"id": "params.id"})},
		}},
	}
	c := &Checker{queries: map[string]*queryInfo{q.ID: q}}
	if got := runServiceItem(t, doc, c); !strings.Contains(got, "kind=query does not allow field input") {
		t.Fatalf("query step input not rejected: %s", got)
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
			"summary": "x", "tx_mode": transaction,
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps": []any{map[string]any{
				"id": "run", "kind": "query", "summary": "x",
				"query": map[string]any{"query": q.ID},
			}},
		}
	}
	run := func(transaction string) string {
		c := &Checker{
			queries:          map[string]*queryInfo{q.ID: q},
			queryReturnTypes: map[string]map[string]returnType{q.ID: {}},
		}
		c.checkServiceItem(base(transaction), "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]lifecycleAction{}, map[string]bool{})
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
			"summary": "x", "tx_mode": "read_only",
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
			meta, map[string]bool{"feature.rules.x.r": true}, map[string]bool{}, map[string]lifecycleAction{}, map[string]bool{})
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
			"summary": "x", "tx_mode": "read_only",
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
			"summary": "x", "tx_mode": "read_only",
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

func TestServiceRuleOwnerCoverage(t *testing.T) {
	doc := map[string]any{
		"summary": "x", "tx_mode": "read_only",
		"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
		"returns": []any{},
		"steps": []any{map[string]any{
			"id": "run", "kind": "logic", "summary": "x",
			"errors": []any{}, "output": []any{},
			"rules": []any{"feature.rules.x.r"},
		}},
	}
	meta := map[string]serviceMeta{
		"cap.services.x.run":   {module: "x", visibility: "public", errors: map[string]bool{}},
		"cap.services.x.owner": {module: "x", visibility: "public", errors: map[string]bool{"platform.errors.A": true}},
	}
	c := &Checker{
		ruleErrors: map[string]string{"feature.rules.x.r": "platform.errors.A"},
		ruleOwners: map[string][]string{"feature.rules.x.r": {"cap.services.x.owner"}},
	}
	c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
		meta, map[string]bool{"feature.rules.x.r": true}, map[string]bool{}, map[string]lifecycleAction{}, map[string]bool{})
	if got := strings.Join(c.errs, "\n"); strings.Contains(got, "is not declared by service") {
		t.Fatalf("owner-declared rule error reported: %s", got)
	}
}

func TestServiceEventsStep(t *testing.T) {
	run := func(step map[string]any, eventIDs map[string]bool) string {
		c := &Checker{}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps":   []any{step},
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, eventIDs, map[string]lifecycleAction{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	events := map[string]bool{"cap.events.x.created": true}

	if got := run(map[string]any{
		"id": "emit", "kind": "events", "summary": "x",
		"events": []any{"cap.events.x.created"},
	}, events); strings.Contains(got, "service-steps-kind") || strings.Contains(got, "service-ref") {
		t.Fatalf("valid events step reported: %s", got)
	}
	if got := run(map[string]any{"id": "emit", "kind": "events", "summary": "x"}, events); !strings.Contains(got, "kind=events requires events") {
		t.Fatalf("missing events not reported: %s", got)
	}
	if got := run(map[string]any{
		"id": "emit", "kind": "events", "summary": "x",
		"events": []any{"cap.events.x.created"}, "output": []any{},
	}, events); !strings.Contains(got, "does not allow field output") {
		t.Fatalf("events step extra field not reported: %s", got)
	}
	if got := run(map[string]any{
		"id": "emit", "kind": "events", "summary": "x",
		"events": []any{"cap.events.x.missing"},
	}, events); !strings.Contains(got, "unknown event") {
		t.Fatalf("unknown event not reported: %s", got)
	}
}

func TestServiceWhenBranches(t *testing.T) {
	run := func(steps []any) string {
		c := &Checker{enumValues: map[string]map[string]bool{
			"data.enums.auth_type": {"phone": true, "email": true},
		}}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params": []any{
				map[string]any{"name": "ctx", "type": "context.Context"},
				map[string]any{"name": "auth_type", "type": "data.enums.auth_type"},
			},
			"returns": []any{},
			"steps":   steps,
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]lifecycleAction{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	logicStep := func(id string) map[string]any {
		return map[string]any{"id": id, "kind": "logic", "summary": "x", "output": []any{}, "errors": []any{}}
	}
	doc := func(when []any, gotoStep string) []any {
		branch := map[string]any{
			"id": "branch", "kind": "when", "summary": "x",
			"input": []any{
				map[string]any{"name": "auth_type", "value": "params.auth_type", "description": "x"},
				map[string]any{"name": "verified", "value": "step.check.verified", "description": "x"},
			},
			"when": when,
		}
		if gotoStep != "" {
			branch["goto"] = gotoStep
		}
		return []any{
			map[string]any{
				"id": "check", "kind": "logic", "summary": "x",
				"output": []any{map[string]any{"name": "verified", "type": "boolean", "description": "x"}},
				"errors": []any{},
			},
			branch,
			logicStep("a"),
			logicStep("b"),
			logicStep("done"),
		}
	}
	valid := []any{
		map[string]any{"input": "verified", "equals": false, "goto": "done"},
		map[string]any{"input": "auth_type", "equals": "phone", "goto": "a"},
		map[string]any{"input": "auth_type", "equals": "email", "goto": "b"},
	}

	if got := run(doc(valid, "done")); strings.Contains(got, "service-when") || strings.Contains(got, "service-goto") {
		t.Fatalf("valid when reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "equals": "phone", "goto": "a"}}, "")); strings.Contains(got, "service-when") || strings.Contains(got, "service-goto") {
		t.Fatalf("valid single branch reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "equals": "phone"}}, "done")); !strings.Contains(got, "requires goto") {
		t.Fatalf("missing branch goto not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "equals": "phone", "goto": "missing"}}, "done")); !strings.Contains(got, "goto unknown step") {
		t.Fatalf("unknown goto not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "equals": "phone", "goto": "check"}}, "done")); !strings.Contains(got, "must target a later step") {
		t.Fatalf("backward goto not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "verified", "equals": true, "in": []any{true}, "goto": "a"}}, "done")); !strings.Contains(got, "requires exactly one of equals/in") {
		t.Fatalf("equals/in both not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "verified", "equals": "yes", "goto": "a"}}, "done")); !strings.Contains(got, "expects boolean") {
		t.Fatalf("type mismatch not reported: %s", got)
	}
}

func TestServiceGoto(t *testing.T) {
	run := func(steps []any) string {
		c := &Checker{}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps":   steps,
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]lifecycleAction{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	logicStep := func(id string, gotoStep string) map[string]any {
		step := map[string]any{"id": id, "kind": "logic", "summary": "x", "output": []any{}, "errors": []any{}}
		if gotoStep != "" {
			step["goto"] = gotoStep
		}
		return step
	}

	if got := run([]any{logicStep("a", "b"), logicStep("b", "")}); strings.Contains(got, "service-goto") {
		t.Fatalf("valid forward goto reported: %s", got)
	}
	if got := run([]any{logicStep("a", "missing")}); !strings.Contains(got, "goto unknown step") {
		t.Fatalf("unknown goto not reported: %s", got)
	}
	if got := run([]any{logicStep("a", "a")}); !strings.Contains(got, "must target a later step") {
		t.Fatalf("self goto not reported: %s", got)
	}
	if got := run([]any{logicStep("a", "b"), logicStep("b", "a")}); !strings.Contains(got, "must target a later step") {
		t.Fatalf("backward goto not reported: %s", got)
	}
}

func TestServiceAtomicKinds(t *testing.T) {
	q := &queryInfo{ID: "data.queries.x.q"}
	target := serviceMeta{module: "x", visibility: "public", txMode: "none"}
	run := func(step map[string]any) string {
		c := &Checker{queries: map[string]*queryInfo{q.ID: q}}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps":   []any{step},
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{"cap.services.x.target": target}, map[string]bool{},
			map[string]bool{"cap.events.x.created": true}, map[string]lifecycleAction{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}

	if got := run(map[string]any{
		"id": "run", "kind": "query", "summary": "x",
	}); !strings.Contains(got, "kind=query requires query") {
		t.Fatalf("missing query not reported: %s", got)
	}
	if got := run(map[string]any{
		"id": "run", "kind": "service", "summary": "x",
	}); !strings.Contains(got, "kind=service requires service") {
		t.Fatalf("missing service not reported: %s", got)
	}
	if got := run(map[string]any{
		"id": "emit", "kind": "events", "summary": "x",
	}); !strings.Contains(got, "kind=events requires events") {
		t.Fatalf("missing events not reported: %s", got)
	}
}
