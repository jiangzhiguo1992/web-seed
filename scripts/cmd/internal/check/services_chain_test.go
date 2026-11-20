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
		map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
	return strings.Join(c.errs, "\n")
}

func logicStep(id string) map[string]any {
	return map[string]any{"id": id, "kind": "logic", "summary": "x", "rules": []any{}, "output": []any{}, "errors": []any{}}
}

func TestServiceQueryInputRequiredAndExact(t *testing.T) {
	q := &queryInfo{
		ID:      "data.queries.x.q",
		Params:  []queryParam{{Name: "id"}, {Name: "note", Nullable: true}},
		Returns: []queryReturn{{Name: "row", Kind: "scalar", Type: "string"}, {Name: "err", Kind: "error"}},
	}
	base := func(input map[string]any) map[string]any {
		step := map[string]any{"id": "run", "kind": "query", "summary": "x", "input": inputList(input),
			"query": q.ID, "goto": "$end"}
		return map[string]any{
			"summary": "x", "tx_mode": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
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
		step := map[string]any{"id": "call", "kind": "service", "summary": "x", "service": "cap.services.x.target", "goto": "$end"}
		if input != nil {
			items := make([]any, 0, len(input))
			for name, value := range input {
				items = append(items, map[string]any{"name": name, "value": value, "description": "x"})
			}
			step["input"] = items
		}
		return map[string]any{
			"summary": "x", "tx_mode": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}, map[string]any{"name": "account_id", "type": "integer"}},
			"returns": []any{},
			"steps":   []any{step},
		}
	}
	run := func(doc map[string]any) string {
		c := &Checker{}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{"cap.services.x.target": target}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
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

	t.Run("return requires value", func(t *testing.T) {
		doc := base([]any{map[string]any{
			"id": "run", "kind": "logic", "summary": "x",
			"rules":  []any{},
			"errors": []any{}, "output": []any{},
			"goto": "$end",
		}}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{map[string]any{"name": "value", "type": "string"}})
		if got := run(doc); (!strings.Contains(got, "return value requires value") && !strings.Contains(got, "references value")) || !strings.Contains(got, "[service-output-value]") {
			t.Fatalf("missing return value not reported: %s", got)
		}
	})

	t.Run("return value source must resolve", func(t *testing.T) {
		doc := base([]any{map[string]any{
			"id": "run", "kind": "logic", "summary": "x",
			"rules":  []any{},
			"errors": []any{}, "output": []any{},
			"goto": "$end",
		}}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{map[string]any{"name": "value", "type": "string", "value": "step.run.value"}})
		if got := run(doc); !strings.Contains(got, "unknown step output value") || !strings.Contains(got, "[service-input-ref]") {
			t.Fatalf("unknown return source not reported: %s", got)
		}
	})

	t.Run("explicit output must be consumed or returned", func(t *testing.T) {
		doc := base([]any{map[string]any{
			"id": "run", "kind": "logic", "summary": "x",
			"rules":  []any{},
			"errors": []any{}, "output": []any{map[string]any{"name": "other", "type": "string"}},
			"goto": "$end",
		}}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{})
		if got := run(doc); !strings.Contains(got, "output other is not consumed") || !strings.Contains(got, "[service-chain]") {
			t.Fatalf("unused output not reported: %s", got)
		}
	})

	t.Run("output consumed by later step input", func(t *testing.T) {
		doc := base([]any{
			map[string]any{
				"id": "load", "kind": "logic", "summary": "x",
				"rules": []any{}, "errors": []any{}, "output": []any{map[string]any{"name": "value", "type": "string"}},
				"goto": "use",
			},
			map[string]any{
				"id": "use", "kind": "logic", "summary": "x",
				"rules": []any{}, "errors": []any{}, "input": []any{map[string]any{"name": "v", "value": "step.load.value", "description": "x"}},
				"output": []any{},
				"goto":   "$end",
			},
		}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{})
		if got := run(doc); len(strings.TrimSpace(got)) != 0 {
			t.Fatalf("consumed output reported: %s", got)
		}
	})

	t.Run("return value consumes output", func(t *testing.T) {
		doc := base([]any{
			map[string]any{
				"id": "load", "kind": "logic", "summary": "x",
				"rules": []any{}, "errors": []any{}, "output": []any{map[string]any{"name": "value", "type": "string"}},
				"goto": "$end",
			},
		}, []any{map[string]any{"name": "ctx", "type": "context.Context"}}, []any{map[string]any{"name": "value", "type": "string", "value": "step.load.value"}})
		if got := run(doc); strings.Contains(got, "not consumed") {
			t.Fatalf("return value did not consume output: %s", got)
		}
	})

	t.Run("unused service param", func(t *testing.T) {
		doc := base([]any{map[string]any{
			"id": "run", "kind": "logic", "summary": "x",
			"rules":  []any{},
			"errors": []any{}, "output": []any{},
			"goto": "$end",
		}}, []any{
			map[string]any{"name": "ctx", "type": "context.Context"},
			map[string]any{"name": "value", "type": "string"},
		}, []any{})
		if got := run(doc); !strings.Contains(got, "param value is not consumed") || !strings.Contains(got, "[service-chain]") {
			t.Fatalf("unused param not reported: %s", got)
		}
	})
}

func TestServiceNestedInputPath(t *testing.T) {
	c := &Checker{methodTypeDefs: map[string]map[string]any{
		"Filter": {
			"type": "_types.object",
			"properties": []any{
				map[string]any{"name": "status", "schema": map[string]any{"type": "integer"}},
			},
		},
	}}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "ctx", "type": "context.Context"},
		map[string]any{"name": "filter", "type": "Filter"},
	}}
	c.checkServiceParamExpr("params.filter.status", doc, map[string]int{}, map[string]int{}, nil, nil, nil, "f", "s")
	if len(c.errs) != 0 {
		t.Fatalf("valid nested input reported: %v", c.errs)
	}
	c.errs = nil
	c.checkServiceParamExpr("params.filter.missing", doc, map[string]int{}, map[string]int{}, nil, nil, nil, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown field missing") {
		t.Fatalf("unknown nested input not reported: %v", c.errs)
	}
}

func TestServiceExprInputRejected(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "ctx", "type": "context.Context"},
	}}
	c.checkServiceParamExpr("expr: hash(password)", doc, map[string]int{}, map[string]int{}, nil, nil, nil, "f", "s")
	if !strings.Contains(strings.Join(c.errs, "\n"), "inline expressions are not allowed") {
		t.Fatalf("expr input not rejected: %v", c.errs)
	}
}

func TestServiceInputSourceIsHardConstrained(t *testing.T) {
	q := &queryInfo{ID: "data.queries.x.q", Params: []queryParam{{Name: "id"}}}
	run := func(input any) string {
		step := map[string]any{"id": "run", "kind": "query", "summary": "x", "input": input,
			"query": q.ID, "goto": "$end"}
		doc := map[string]any{
			"summary": "x", "tx_mode": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}, map[string]any{"name": "id", "type": "integer"}},
			"returns": []any{},
			"steps":   []any{step},
		}
		c := &Checker{queries: map[string]*queryInfo{q.ID: q}}
		return runServiceItem(t, doc, c)
	}

	if got := run(inputList(map[string]any{"id": "input.id"})); !strings.Contains(got, "must be params.<path>") || !strings.Contains(got, "[service-input-format]") {
		t.Fatalf("input.* source not rejected: %s", got)
	}
	if got := run(inputList(map[string]any{"id": "params.nope"})); !strings.Contains(got, "references params.nope") || !strings.Contains(got, "[service-input-ref]") {
		t.Fatalf("unknown params source not rejected: %s", got)
	}
	if got := run(inputList(map[string]any{"id": "expr: step.nope.x"})); !strings.Contains(got, "inline expressions are not allowed") || !strings.Contains(got, "[service-input-format]") {
		t.Fatalf("expr source not rejected: %s", got)
	}
	if got := run(inputList(map[string]any{"id": "step.nope.x"})); !strings.Contains(got, "not an earlier step") || !strings.Contains(got, "[service-input-order]") {
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
	cases := []struct{ name, typ, want string }{
		{"list-nesting", "List<List<integer>>", "malformed type"},
		{"double-pointer", "**integer", "malformed type"},
		{"empty-list", "List<>", "malformed type"},
		{"map", "Map<integer>", "malformed type"},
		{"unknown-data-type", "data.foo.bar", "unknown type"},
		{"forbidden-data-tables", "data.tables.account", "data.tables.* is not a method type"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c.errs = nil
			c.checkTypeName(tc.typ, map[string]bool{"Local": true}, "x")
			got := strings.Join(c.errs, "\n")
			if !strings.Contains(got, tc.want) || !strings.Contains(got, "[service-param-type]") {
				t.Fatalf("%s not reported as %q with service-param-type: %s", tc.typ, tc.want, got)
			}
		})
	}
	t.Run("local-type", func(t *testing.T) {
		c.errs = nil
		c.checkTypeName("Local", map[string]bool{"Local": true}, "x")
		if len(c.errs) != 0 {
			t.Fatalf("local type reported: %v", c.errs)
		}
	})
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
			map[string]any{"id": "call", "kind": "service", "summary": "x", "goto": "$end", "service": "cap.services.x.target"},
		},
	}
	c := &Checker{}
	c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
		map[string]serviceMeta{"cap.services.x.target": target}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "must not call tx_mode=required") || !strings.Contains(got, "[service-transaction]") {
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

	// 列表不能隐式收窄为标量。
	c.errs = nil
	c.checkQueryInput(map[string]any{
		"id": "params.ids", "name": "params.name", "ids": "params.ids", "status": "params.status", "state": "params.state",
	}, q, q.ID, "f", "s", tc)
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "input id type List<integer> is not compatible with integer") {
		t.Fatalf("list-to-scalar not reported: %s", got)
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
			map[string]any{"id": "call", "kind": "service", "summary": "x", "goto": "$end", "service": "cap.services.x.writer"},
		},
	}
	c := &Checker{serviceWrites: map[string]bool{"cap.services.x.writer": true}}
	c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
		map[string]serviceMeta{"cap.services.x.writer": target}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "tx_mode=read_only cannot call service cap.services.x.writer that writes tables") || !strings.Contains(got, "[service-transaction]") {
		t.Fatalf("transitive writer not reported: %v", c.errs)
	}
}

func TestServiceQueryOutputMatchesReturns(t *testing.T) {
	q := &queryInfo{ID: "data.queries.x.q"}
	run := func(output []any) string {
		c := &Checker{
			queries:          map[string]*queryInfo{q.ID: q},
			queryReturnTypes: map[string]map[string]returnType{q.ID: {"value": {typ: "string"}}},
		}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps": []any{map[string]any{
				"id": "run", "kind": "query", "summary": "x", "goto": "$end",
				"output": output,
				"query":  q.ID,
			}},
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	if got := run([]any{map[string]any{"name": "nope", "type": "string", "description": "x"}}); !strings.Contains(got, "has no return nope") || !strings.Contains(got, "[service-output]") {
		t.Fatalf("unknown query return not reported: %s", got)
	}
	if got := run([]any{map[string]any{"name": "value", "type": "integer", "description": "x"}}); !strings.Contains(got, "does not match") {
		t.Fatalf("query output type mismatch not reported: %s", got)
	}
	if got := run([]any{map[string]any{"name": "value", "type": "string", "description": "x"}}); !strings.Contains(got, "not consumed") {
		t.Fatalf("unconsumed query output not reported: %s", got)
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
				"id": "run", "kind": "query", "summary": "x", "goto": "$end",
				"query": q.ID,
			}},
		}
	}
	run := func(transaction string) string {
		c := &Checker{
			queries:          map[string]*queryInfo{q.ID: q},
			queryReturnTypes: map[string]map[string]returnType{q.ID: {}},
		}
		c.checkServiceItem(base(transaction), "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}

	if got := run("read_only"); !strings.Contains(got, "calls write query") || !strings.Contains(got, "[service-transaction]") {
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
				"id": "run", "kind": "logic", "summary": "x", "goto": "$end",
				"errors": errors, "output": []any{},
				"rules": []any{map[string]any{"rule": "feature.rules.x.r"}},
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

	if got := run([]any{}); !strings.Contains(got, "rule feature.rules.x.r error platform.errors.A is not declared") || !strings.Contains(got, "[service-errors]") {
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
			"returns": []any{map[string]any{"name": "account", "type": retType, "value": "step.run.account"}},
			"steps": []any{map[string]any{
				"id": "run", "kind": "logic", "summary": "x", "goto": "$end",
				"errors": []any{},
				"output": []any{map[string]any{"name": "account", "type": outType}},
			}},
		}
	}
	run := func(retType, outType string) string {
		return runServiceItem(t, base(retType, outType), &Checker{})
	}

	if got := run("data.entities.account", "string"); !strings.Contains(got, "does not match source") {
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
		ret := map[string]any{"name": "account", "type": "string", "value": "step.run.account"}
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
				"id": "run", "kind": "logic", "summary": "x", "goto": "$end",
				"errors": []any{}, "rules": []any{}, "output": []any{out},
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
			"id": "run", "kind": "logic", "summary": "x", "goto": "$end",
			"errors": []any{}, "output": []any{},
			"rules": []any{map[string]any{"rule": "feature.rules.x.r"}},
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
		meta, map[string]bool{"feature.rules.x.r": true}, map[string]bool{}, map[string]bool{}, map[string]bool{})
	if got := strings.Join(c.errs, "\n"); strings.Contains(got, "is not declared by service") {
		t.Fatalf("owner-declared rule error reported: %s", got)
	}
}

func TestServiceEventsStep(t *testing.T) {
	run := func(step map[string]any, eventIDs map[string]bool, payloads map[string]map[string]eventField) string {
		c := &Checker{eventPayloads: payloads}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params": []any{
				map[string]any{"name": "ctx", "type": "context.Context"},
				map[string]any{"name": "id", "type": "integer"},
				map[string]any{"name": "code", "type": "string"},
			},
			"returns": []any{},
			"steps":   []any{step},
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, eventIDs, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	events := map[string]bool{"cap.events.x.created": true}
	payloads := map[string]map[string]eventField{
		"cap.events.x.created": {"id": {typ: "integer"}},
	}

	if got := run(map[string]any{
		"id": "emit", "kind": "events", "summary": "x", "goto": "$end",
		"input":  []any{map[string]any{"name": "id", "value": "params.id", "description": "x"}},
		"events": []any{map[string]any{"ref": "cap.events.x.created"}},
	}, events, payloads); strings.Contains(got, "service-step-") || strings.Contains(got, "service-ref") {
		t.Fatalf("valid events step reported: %s", got)
	}
	if got := run(map[string]any{"id": "emit", "kind": "events", "summary": "x", "goto": "$end"}, events, payloads); !strings.Contains(got, "kind=events requires events") {
		t.Fatalf("missing events not reported: %s", got)
	}
	if got := run(map[string]any{
		"id": "emit", "kind": "events", "summary": "x", "goto": "$end",
		"events": []any{map[string]any{"ref": "cap.events.x.created"}},
		"output": []any{map[string]any{"name": "x", "type": "string", "description": "x"}},
	}, events, payloads); !strings.Contains(got, "output must be empty") {
		t.Fatalf("events step output not reported: %s", got)
	}
	if got := run(map[string]any{
		"id": "emit", "kind": "events", "summary": "x", "goto": "$end",
		"events": []any{map[string]any{"ref": "cap.events.x.missing"}},
	}, events, payloads); !strings.Contains(got, "unknown event") {
		t.Fatalf("unknown event not reported: %s", got)
	}
	if got := run(map[string]any{
		"id": "emit", "kind": "events", "summary": "x", "goto": "$end",
		"input":  []any{map[string]any{"name": "nope", "value": "params.ctx", "description": "x"}},
		"events": []any{map[string]any{"ref": "cap.events.x.created"}},
	}, events, payloads); !strings.Contains(got, "is not a payload field of any event in this step") {
		t.Fatalf("unknown payload field not reported: %s", got)
	}

	multiEvents := map[string]bool{"cap.events.x.created": true, "cap.events.x.updated": true}
	multiPayloads := map[string]map[string]eventField{
		"cap.events.x.created": {"id": {typ: "integer", required: true}},
		"cap.events.x.updated": {"id": {typ: "integer", required: true}, "note": {typ: "string"}},
	}
	multi := map[string]any{
		"id": "emit", "kind": "events", "summary": "x", "goto": "$end",
		"input": []any{
			map[string]any{"name": "id", "value": "params.id", "description": "x"},
			map[string]any{"name": "note", "value": "params.code", "description": "x"},
		},
		"events": []any{map[string]any{"ref": "cap.events.x.created"}, map[string]any{"ref": "cap.events.x.updated"}},
	}
	if got := run(multi, multiEvents, multiPayloads); got != "" {
		t.Fatalf("valid multi-event step reported: %s", got)
	}
	missing := map[string]any{
		"id": "emit", "kind": "events", "summary": "x", "goto": "$end",
		"input":  []any{map[string]any{"name": "note", "value": "params.code", "description": "x"}},
		"events": []any{map[string]any{"ref": "cap.events.x.created"}, map[string]any{"ref": "cap.events.x.updated"}},
	}
	if got := run(missing, multiEvents, multiPayloads); !strings.Contains(got, "missing required payload field id") {
		t.Fatalf("multi-event missing required not reported: %s", got)
	}
	dup := map[string]any{
		"id": "emit", "kind": "events", "summary": "x", "goto": "$end",
		"input":  []any{map[string]any{"name": "id", "value": "params.id", "description": "x"}},
		"events": []any{map[string]any{"ref": "cap.events.x.created"}, map[string]any{"ref": "cap.events.x.created"}},
	}
	if got := run(dup, multiEvents, multiPayloads); !strings.Contains(got, "duplicate event") {
		t.Fatalf("duplicate event not reported: %s", got)
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
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
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
		branch["goto"] = gotoStep
		return []any{
			map[string]any{
				"id": "check", "kind": "logic", "summary": "x", "goto": "branch",
				"rules":  []any{},
				"output": []any{map[string]any{"name": "verified", "type": "boolean", "description": "x"}},
				"errors": []any{},
			},
			branch,
			func() map[string]any { s := logicStep("a"); s["goto"] = "b"; return s }(),
			func() map[string]any { s := logicStep("b"); s["goto"] = "done"; return s }(),
			func() map[string]any { s := logicStep("done"); s["goto"] = "$end"; return s }(),
		}
	}
	valid := []any{
		map[string]any{"input": "verified", "expr": "input.verified == false", "goto": "done"},
		map[string]any{"input": "auth_type", "expr": "input.auth_type == 'phone'", "goto": "a"},
		map[string]any{"input": "auth_type", "expr": "input.auth_type == 'email'", "goto": "b"},
	}

	if got := run(doc(valid, "done")); strings.Contains(got, "service-when") || strings.Contains(got, "service-goto") {
		t.Fatalf("valid when reported: %s", got)
	}
	allEnd := doc([]any{
		map[string]any{"input": "verified", "expr": "input.verified == false", "goto": "$end"},
		map[string]any{"input": "verified", "expr": "input.verified == true", "goto": "$end"},
	}, "$end")
	if b, ok := allEnd[1].(map[string]any); ok {
		b["input"] = []any{map[string]any{"name": "verified", "value": "step.check.verified", "description": "x"}}
	}
	if got := run(allEnd); strings.Contains(got, "service-when") {
		t.Fatalf("valid all-$end when reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "expr": "input.auth_type == 'phone'"}}, "done")); !strings.Contains(got, "requires goto") || !strings.Contains(got, "[service-when-shape]") {
		t.Fatalf("missing branch goto not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "expr": "input.auth_type == 'phone'", "goto": "missing"}}, "done")); !strings.Contains(got, "goto unknown step") {
		t.Fatalf("unknown goto not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "expr": "input.auth_type == 'phone'", "goto": "check"}}, "done")); !strings.Contains(got, "must target a later step") {
		t.Fatalf("backward goto not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "expr": "output.auth_type == 'phone'", "goto": "a"}}, "done")); (!strings.Contains(got, "must be input.<path>") && !strings.Contains(got, "may only reference input")) || !strings.Contains(got, "[service-when-shape]") {
		t.Fatalf("non-input reference not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "expr": "input.missing == 'phone'", "goto": "a"}}, "done")); !strings.Contains(got, "is not declared by step input") || !strings.Contains(got, "[service-when-refs]") {
		t.Fatalf("unknown condition not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "verified", "expr": "input.verified == 'yes'", "goto": "a"}}, "done")); !strings.Contains(got, "expects boolean") || !strings.Contains(got, "[service-when-types]") {
		t.Fatalf("type mismatch not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "expr": "input.auth_type == 'nope'", "goto": "a"}}, "done")); !strings.Contains(got, "unknown enum value") {
		t.Fatalf("unknown enum value not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"expr": "input.auth_type == 'phone'", "goto": "a"}}, "done")); !strings.Contains(got, "requires input") || !strings.Contains(got, "[service-when-shape]") {
		t.Fatalf("missing when input not reported: %s", got)
	}
	if got := run(doc([]any{map[string]any{"input": "auth_type", "expr": "input.missing == 'phone'", "goto": "a"}}, "done")); !strings.Contains(got, "is not declared by step input") || !strings.Contains(got, "[service-when-refs]") {
		t.Fatalf("undeclared when input not reported: %s", got)
	}
	if got := run(doc([]any{
		map[string]any{"input": "verified", "expr": "input.auth_type == 'phone'", "goto": "a"},
	}, "done")); !strings.Contains(got, "must be referenced by expr") || !strings.Contains(got, "[service-when-refs]") {
		t.Fatalf("unreferenced when input not reported: %s", got)
	}
}

func TestServiceQueryOutputRejected(t *testing.T) {
	q := &queryInfo{ID: "data.queries.x.q"}
	run := func(output any, extra []any) string {
		c := &Checker{
			queries:          map[string]*queryInfo{q.ID: q},
			queryReturnTypes: map[string]map[string]returnType{q.ID: {"value": {typ: "string"}}},
		}
		step := map[string]any{"id": "q", "kind": "query", "summary": "x", "goto": "$end", "query": q.ID}
		if output != nil {
			step["output"] = output
		}
		if len(extra) > 0 {
			step["goto"] = "use"
		}
		steps := append([]any{step}, extra...)
		doc := map[string]any{
			"summary": "x", "tx_mode": "read_only",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps":   steps,
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	use := func(value string) map[string]any {
		return map[string]any{
			"id": "use", "kind": "logic", "summary": "x", "goto": "$end",
			"input":  []any{map[string]any{"name": "v", "value": value, "description": "x"}},
			"output": []any{}, "errors": []any{}, "rules": []any{},
		}
	}
	if got := run([]any{map[string]any{"name": "value", "type": "string", "description": "x"}}, []any{use("step.q.value")}); got != "" {
		t.Fatalf("declared query return reported: %s", got)
	}
	if got := run(nil, []any{use("step.q.value")}); !strings.Contains(got, "unknown step output value") {
		t.Fatalf("undeclared query return not reported: %s", got)
	}
	if got := run(nil, []any{use("step.q.missing")}); !strings.Contains(got, "unknown step output missing") {
		t.Fatalf("unknown query return not reported: %s", got)
	}
}

func TestServiceServiceReturnReference(t *testing.T) {
	target := serviceMeta{module: "x", visibility: "public", txMode: "none", returns: map[string]returnType{"value": {typ: "string"}}}
	run := func(steps []any) string {
		if len(steps) > 1 {
			if first, ok := steps[0].(map[string]any); ok {
				first["goto"] = "use"
			}
		}
		c := &Checker{methodReturns: map[string]map[string]returnType{
			"cap.services.x.target": {"value": {typ: "string"}},
		}}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps":   steps,
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{"cap.services.x.target": target}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	call := map[string]any{"id": "call", "kind": "service", "summary": "x", "goto": "$end",
		"output":  []any{map[string]any{"name": "value", "type": "string", "description": "x"}},
		"service": "cap.services.x.target"}
	use := func(value string) map[string]any {
		return map[string]any{
			"id": "use", "kind": "logic", "summary": "x", "goto": "$end",
			"input":  []any{map[string]any{"name": "v", "value": value, "description": "x"}},
			"output": []any{}, "errors": []any{}, "rules": []any{},
		}
	}
	if got := run([]any{call}); !strings.Contains(got, "not consumed") {
		t.Fatalf("unconsumed service return not reported: %s", got)
	}
	if got := run([]any{call, use("step.call.value")}); got != "" {
		t.Fatalf("known service return reported: %s", got)
	}
	if got := run([]any{call, use("step.call.missing")}); !strings.Contains(got, "unknown step output missing") {
		t.Fatalf("unknown service return not reported: %s", got)
	}
}

func TestServiceStepReachability(t *testing.T) {
	run := func(steps []any) string {
		c := &Checker{}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps":   steps,
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	a := logicStep("a")
	a["output"] = []any{map[string]any{"name": "x", "type": "string", "description": "x"}}
	a["goto"] = "d"
	b := logicStep("b")
	b["goto"] = "d"
	c := logicStep("c")
	c["input"] = []any{map[string]any{"name": "v", "value": "step.a.x", "description": "x"}}
	c["goto"] = "$end"
	d := logicStep("d")
	d["goto"] = "$end"

	if got := run([]any{a, b, c, d}); !strings.Contains(got, "not reachable") {
		t.Fatalf("unreachable branch reference not reported: %s", got)
	}
	if got := run([]any{a, b, d}); strings.Contains(got, "not reachable") {
		t.Fatalf("reachable steps reported: %s", got)
	}
}

func TestServiceLogicRequiresRules(t *testing.T) {
	run := func(rules any) string {
		c := &Checker{}
		step := map[string]any{"id": "run", "kind": "logic", "summary": "x", "goto": "$end", "output": []any{}, "errors": []any{}}
		if rules != nil {
			step["rules"] = rules
		}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps":   []any{step},
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	if got := run(nil); !strings.Contains(got, "kind=logic requires rules") {
		t.Fatalf("missing rules not reported: %s", got)
	}
	if got := run([]any{}); strings.Contains(got, "kind=logic requires rules") {
		t.Fatalf("empty rules reported: %s", got)
	}
}

func TestServiceWhenConditionsOnlyUseInput(t *testing.T) {
	run := func(output []any) string {
		c := &Checker{}
		branch := map[string]any{
			"id": "branch", "kind": "when", "summary": "x", "goto": "$end",
			"rules": []any{},
			"input": []any{map[string]any{"name": "status", "value": "params.ctx", "description": "x"}},
			"when":  []any{map[string]any{"input": "status", "expr": "output.status == 'active'", "goto": "$end"}},
		}
		if output != nil {
			branch["output"] = output
		}
		done := logicStep("done")
		done["goto"] = "$end"
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params":  []any{map[string]any{"name": "ctx", "type": "context.Context"}},
			"returns": []any{},
			"steps":   []any{branch, done},
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	statusOut := []any{map[string]any{"name": "status", "type": "string", "description": "x"}}

	if got := run(statusOut); !strings.Contains(got, "must be input.<path>") && !strings.Contains(got, "may only reference input") {
		t.Fatalf("output condition not rejected: %s", got)
	}
	if got := run(nil); !strings.Contains(got, "must be input.<path>") && !strings.Contains(got, "may only reference input") {
		t.Fatalf("output condition not reported without output: %s", got)
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
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	logicStep := func(id string, gotoStep string) map[string]any {
		step := map[string]any{"id": id, "kind": "logic", "summary": "x", "rules": []any{}, "output": []any{}, "errors": []any{}}
		if gotoStep != "" {
			step["goto"] = gotoStep
		}
		return step
	}

	if got := run([]any{logicStep("a", "b"), logicStep("b", "$end")}); strings.Contains(got, "service-goto") {
		t.Fatalf("valid forward goto reported: %s", got)
	}
	if got := run([]any{logicStep("a", "$end")}); strings.Contains(got, "service-goto") {
		t.Fatalf("valid $end goto reported: %s", got)
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
	if got := run([]any{logicStep("a", "$end"), logicStep("b", "$end")}); !strings.Contains(got, "is unreachable from the first step") {
		t.Fatalf("unreachable step not reported: %s", got)
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
			map[string]bool{"cap.events.x.created": true}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}

	if got := run(map[string]any{
		"id": "run", "kind": "query", "summary": "x", "goto": "$end",
	}); !strings.Contains(got, "kind=query requires query") {
		t.Fatalf("missing query not reported: %s", got)
	}
	if got := run(map[string]any{
		"id": "run", "kind": "service", "summary": "x", "goto": "$end",
	}); !strings.Contains(got, "kind=service requires service") {
		t.Fatalf("missing service not reported: %s", got)
	}
	if got := run(map[string]any{
		"id": "emit", "kind": "events", "summary": "x", "goto": "$end",
	}); !strings.Contains(got, "kind=events requires events") {
		t.Fatalf("missing events not reported: %s", got)
	}
}

func TestServiceMergeCut(t *testing.T) {
	params := []any{
		map[string]any{"name": "ctx", "type": "context.Context"},
		map[string]any{"name": "flag", "type": "boolean"},
	}
	run := func(steps []any) string {
		c := &Checker{}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params": params, "returns": []any{}, "steps": steps,
		}
		c.checkServiceItem(doc, "f", "cap.services.x.run", "x",
			map[string]serviceMeta{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{})
		return strings.Join(c.errs, "\n")
	}
	branch := func(defaultGoto string) map[string]any {
		return map[string]any{
			"id": "branch", "kind": "when", "summary": "x", "goto": defaultGoto,
			"input": []any{map[string]any{"name": "flag", "value": "params.flag", "description": "x"}},
			"when":  []any{map[string]any{"input": "flag", "expr": "input.flag == true", "goto": "pa"}},
		}
	}
	producer := func(id, out, next string) map[string]any {
		s := logicStep(id)
		s["output"] = []any{map[string]any{"name": out, "type": "string", "description": "x"}}
		s["goto"] = next
		return s
	}
	merge := func(inputs ...map[string]any) map[string]any {
		s := logicStep("merge")
		list := make([]any, 0, len(inputs))
		for _, in := range inputs {
			list = append(list, in)
		}
		s["input"] = list
		s["goto"] = "$end"
		return s
	}

	bad := run([]any{
		branch("merge"),
		producer("pa", "x", "merge"),
		merge(map[string]any{"name": "a", "value": "step.pa.x", "description": "x"}),
	})
	if !strings.Contains(bad, "[service-merge]") || !strings.Contains(bad, "without executing any of them") {
		t.Fatalf("missing cut not reported: %s", bad)
	}

	good := run([]any{
		branch("pb"),
		producer("pa", "x", "merge"),
		producer("pb", "y", "merge"),
		merge(
			map[string]any{"name": "a", "value": "step.pa.x", "description": "x"},
			map[string]any{"name": "b", "value": "step.pb.y", "description": "x"},
		),
	})
	if strings.Contains(good, "[service-merge]") {
		t.Fatalf("valid mutually exclusive merge reported: %s", good)
	}
}

func TestServiceNullableInput(t *testing.T) {
	q := &queryInfo{
		ID:     "data.queries.x.q",
		Params: []queryParam{{Name: "id"}, {Name: "note", Nullable: true}},
	}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "ctx", "type": "context.Context"},
		map[string]any{"name": "opt", "type": "string", "nullable": true},
		map[string]any{"name": "num", "type": "integer", "nullable": true},
		map[string]any{"name": "n", "type": "integer"},
	}}
	tc := stepTypeContext{doc: doc, stepIndex: map[string]int{}, steps: []any{}, stepOutputs: []map[string]returnType{}}
	c := &Checker{}
	c.checkQueryInput(map[string]any{"id": "params.opt"}, q, q.ID, "f", "s", tc)
	if !strings.Contains(strings.Join(c.errs, "\n"), "source is nullable but target param is not") {
		t.Fatalf("nullable query input not reported: %v", c.errs)
	}
	c.errs = nil
	c.checkQueryInput(map[string]any{"id": "params.n", "note": "params.opt"}, q, q.ID, "f", "s", tc)
	if len(c.errs) != 0 {
		t.Fatalf("nullable target reported: %v", c.errs)
	}

	target := serviceMeta{
		params:        map[string]bool{"ctx": true, "id": true, "note": true},
		paramDecls:    map[string]string{"id": "string", "note": "string"},
		paramNullable: map[string]bool{"id": false, "note": true},
	}
	c.errs = nil
	c.checkServiceInput(map[string]any{"id": "params.opt"}, target, "cap.services.x.y", "f", "s", tc)
	if !strings.Contains(strings.Join(c.errs, "\n"), "source is nullable but target param is not") {
		t.Fatalf("nullable service input not reported: %v", c.errs)
	}
	c.errs = nil
	c.checkServiceInput(map[string]any{"note": "params.opt"}, target, "cap.services.x.y", "f", "s", tc)
	if len(c.errs) != 0 {
		t.Fatalf("nullable service target reported: %v", c.errs)
	}

	c.errs = nil
	c.eventPayloads = map[string]map[string]eventField{
		"cap.events.x.created": {"id": {typ: "integer"}},
	}
	c.checkEventInput(map[string]any{"id": "params.num"}, "cap.events.x.created", "f", "s", tc)
	if !strings.Contains(strings.Join(c.errs, "\n"), "target payload field is not") {
		t.Fatalf("nullable event input not reported: %v", c.errs)
	}
}
