package check

import (
	"strings"
	"testing"
)

func whenStep(id, when string) map[string]any {
	step := map[string]any{
		"id":     id,
		"kind":   "logic",
		"output": []any{},
	}
	if when != "" {
		step["when"] = when
	}
	return step
}

func TestWhenExhaustiveBoolean(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{"params": []any{}}
	steps := []any{
		map[string]any{"id": "check", "kind": "logic", "output": []any{
			map[string]any{"name": "success", "type": "boolean"},
		}},
		whenStep("ok", "step.check.success == true"),
		whenStep("fail", "step.check.success == false"),
	}
	stepOutputs := []map[string]returnType{
		{"success": {typ: "boolean"}},
		{}, {},
	}
	c.checkWhenExhaustive(doc, steps, stepOutputs, map[string]int{"check": 0, "ok": 1, "fail": 2}, "f")
	if len(c.errs) != 0 {
		t.Fatalf("complete boolean branches reported: %v", c.errs)
	}

	c.errs = nil
	steps[2] = whenStep("ok2", "step.check.success == true")
	c.checkWhenExhaustive(doc, steps, stepOutputs, map[string]int{"check": 0, "ok": 1, "ok2": 2}, "f")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-when-exhaustive]") {
		t.Fatalf("missing boolean branch not reported: %v", c.errs)
	}
}

func TestWhenExhaustiveEnum(t *testing.T) {
	c := &Checker{
		enumCodes: map[string]map[int]bool{
			"data.enums.auth_type": {0: true, 1: true, 2: true},
		},
	}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "input", "type": "data.enums.auth_type"},
	}}
	steps := []any{
		whenStep("pwd", "input.input == 0"),
		whenStep("phone", "input.input == 1"),
		whenStep("email", "input.input == 2"),
	}
	c.checkWhenExhaustive(doc, steps, []map[string]returnType{{}, {}, {}}, map[string]int{"pwd": 0, "phone": 1, "email": 2}, "f")
	if len(c.errs) != 0 {
		t.Fatalf("complete enum branches reported: %v", c.errs)
	}

	c.errs = nil
	steps[2] = whenStep("pwd2", "input.input == 0")
	c.checkWhenExhaustive(doc, steps, []map[string]returnType{{}, {}, {}}, map[string]int{"pwd": 0, "phone": 1, "pwd2": 2}, "f")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[service-when-exhaustive]") || !strings.Contains(errs, "missing 2") {
		t.Fatalf("missing enum code not reported: %v", c.errs)
	}
}

func TestWhenExhaustiveSkips(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "n", "type": "integer"},
	}}
	// 单分支、非枚举、组合条件都不检查。
	steps := []any{
		whenStep("a", "input.n == 1"),
		whenStep("b", "input.n == 2"),
		whenStep("c", "input.n == 1 && input.n == 2"),
	}
	c.checkWhenExhaustive(doc, steps, []map[string]returnType{{}, {}, {}}, map[string]int{"a": 0, "b": 1, "c": 2}, "f")
	if len(c.errs) != 0 {
		t.Fatalf("non-enum when branches reported: %v", c.errs)
	}
}

func TestWhenDefault(t *testing.T) {
	c := &Checker{
		enumCodes: map[string]map[int]bool{
			"data.enums.auth_type": {0: true, 1: true, 2: true},
		},
	}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "input", "type": "data.enums.auth_type"},
	}}
	// 缺 code 2，但 default 兜底。
	steps := []any{
		whenStep("pwd", "input.input == 0"),
		whenStep("phone", "input.input == 1"),
		whenStep("other", "default"),
	}
	c.checkWhenExhaustive(doc, steps, []map[string]returnType{{}, {}, {}}, map[string]int{"pwd": 0, "phone": 1, "other": 2}, "f")
	if len(c.errs) != 0 {
		t.Fatalf("default fallback reported: %v", c.errs)
	}

	// 两个 default。
	c.errs = nil
	steps[1] = whenStep("dup", "default")
	c.checkWhenExhaustive(doc, steps, []map[string]returnType{{}, {}, {}}, map[string]int{"pwd": 0, "dup": 1, "other": 2}, "f")
	if !strings.Contains(strings.Join(c.errs, "\n"), "at most one") {
		t.Fatalf("duplicate default not reported: %v", c.errs)
	}

	// default 与多个分支组共存。
	c.errs = nil
	c.enumCodes = map[string]map[int]bool{"data.enums.auth_type": {0: true, 1: true, 2: true}}
	doc = map[string]any{"params": []any{
		map[string]any{"name": "input", "type": "data.enums.auth_type"},
		map[string]any{"name": "flag", "type": "boolean"},
	}}
	steps = []any{
		whenStep("pwd", "input.input == 0"),
		whenStep("phone", "input.input == 1"),
		whenStep("other", "default"),
		whenStep("flagOn", "input.flag == true"),
	}
	c.checkWhenExhaustive(doc, steps, []map[string]returnType{{}, {}, {}, {}}, map[string]int{"pwd": 0, "phone": 1, "other": 2, "flagOn": 3}, "f")
	if !strings.Contains(strings.Join(c.errs, "\n"), "exactly one == branch group") {
		t.Fatalf("default with multiple groups not reported: %v", c.errs)
	}

	// default 单独。
	c.errs = nil
	steps = []any{whenStep("other", "default")}
	c.checkWhenExhaustive(doc, steps, []map[string]returnType{{}}, map[string]int{"other": 0}, "f")
	if !strings.Contains(strings.Join(c.errs, "\n"), "found 0") {
		t.Fatalf("lone default not reported: %v", c.errs)
	}
}

func TestWhenPathTypes(t *testing.T) {
	c := &Checker{
		enumCodes: map[string]map[int]bool{"data.enums.kind": {0: true, 1: true}},
		methodTypeDefs: map[string]map[string]any{
			"Row": {"type": "object", "properties": []any{
				map[string]any{"name": "status", "schema": map[string]any{"ref": "data.enums.kind"}},
			}},
			"LoginInput": {"type": "object", "properties": []any{
				map[string]any{"name": "nested", "schema": map[string]any{"ref": "Nested"}},
			}},
			"Nested": {"type": "object", "properties": []any{
				map[string]any{"name": "value", "schema": map[string]any{"ref": "data.enums.kind"}},
			}},
			"Payload": {"oneOf": []any{
				map[string]any{"type": "object", "properties": []any{
					map[string]any{"name": "kind", "schema": map[string]any{"ref": "data.enums.kind"}},
					map[string]any{"name": "a", "schema": map[string]any{"type": "string"}},
				}},
				map[string]any{"type": "object", "properties": []any{
					map[string]any{"name": "kind", "schema": map[string]any{"ref": "data.enums.kind"}},
					map[string]any{"name": "b", "schema": map[string]any{"type": "string"}},
				}},
			}},
		},
	}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "input", "type": "LoginInput"},
	}}
	steps := []any{
		map[string]any{"id": "load", "kind": "logic", "output": []any{
			map[string]any{"name": "items", "type": "List<Row>"},
			map[string]any{"name": "row", "type": "*Row"},
			map[string]any{"name": "payload", "type": "Payload"},
		}},
	}
	stepOutputs := []map[string]returnType{{
		"items":   {typ: "List<Row>"},
		"row":     {typ: "*Row"},
		"payload": {typ: "Payload"},
	}}
	stepIndex := map[string]int{"load": 0}
	for _, path := range []string{
		"input.input.nested.value",  // 多层嵌套
		"step.load.items[0].status", // List 索引
		"step.load.row.status",      // 指针
		"step.load.payload.kind",    // oneOf 公共字段
	} {
		typ, ok := c.whenPathType(doc, steps, stepOutputs, stepIndex, path)
		if !ok || typ != "data.enums.kind" {
			t.Fatalf("%s: typ=%q ok=%v, want data.enums.kind", path, typ, ok)
		}
	}
	// oneOf 分支特有字段无法判定，跳过而不是误报。
	if _, ok := c.whenPathType(doc, steps, stepOutputs, stepIndex, "step.load.payload.a"); ok {
		t.Fatal("oneOf branch-specific field should be undecidable")
	}
}

func TestWhenExhaustiveNestedEnum(t *testing.T) {
	c := &Checker{
		enumCodes: map[string]map[int]bool{
			"data.enums.auth_type": {0: true, 1: true, 2: true},
		},
		methodTypeDefs: map[string]map[string]any{
			"LoginInput": {
				"type": "object",
				"properties": []any{
					map[string]any{"name": "auth_type", "schema": map[string]any{"ref": "data.enums.auth_type"}},
				},
			},
		},
	}
	doc := map[string]any{"params": []any{
		map[string]any{"name": "input", "type": "LoginInput"},
	}}
	steps := []any{
		whenStep("pwd", "input.input.auth_type == 0"),
		whenStep("phone", "input.input.auth_type == 1"),
	}
	c.checkWhenExhaustive(doc, steps, []map[string]returnType{{}, {}}, map[string]int{"pwd": 0, "phone": 1}, "f")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[service-when-exhaustive]") || !strings.Contains(errs, "missing 2") {
		t.Fatalf("nested enum missing code not reported: %v", c.errs)
	}
}
