// service_branch_matrix_test.go 覆盖 service-merge / service-chain 的分支边界。
package check

import (
	"strings"
	"testing"
)

func TestServiceBranchMatrix(t *testing.T) {
	params := func(names ...string) []any {
		out := []any{map[string]any{"name": "ctx", "type": "context.Context"}}
		for _, name := range names {
			out = append(out, map[string]any{"name": name, "type": "boolean"})
		}
		return out
	}
	producer := func(id, out, next string) map[string]any {
		step := logicStep(id)
		step["output"] = []any{map[string]any{"name": out, "type": "string", "description": "x"}}
		step["goto"] = next
		return step
	}
	branch := func(defaultGoto string, conditions ...[2]string) map[string]any {
		step := logicStep("branch")
		step["kind"] = "when"
		step["goto"] = defaultGoto
		inputs := []any{}
		whens := []any{}
		for _, cond := range conditions {
			inputs = append(inputs, map[string]any{"name": cond[0], "value": "params." + cond[0], "description": "x"})
			whens = append(whens, map[string]any{"input": cond[0], "expr": "input." + cond[0] + " == true", "goto": cond[1]})
		}
		step["input"] = inputs
		step["when"] = whens
		return step
	}
	run := func(steps []any, ps []any, returns ...any) string {
		rets := []any{}
		if len(returns) > 0 {
			rets = returns
		}
		doc := map[string]any{
			"summary": "x", "tx_mode": "none",
			"params": ps, "returns": rets, "steps": steps,
		}
		return runServiceItem(t, doc, &Checker{})
	}
	mergeStep := func(inputs ...map[string]any) map[string]any {
		step := logicStep("merge")
		list := make([]any, 0, len(inputs))
		for _, input := range inputs {
			list = append(list, input)
		}
		step["input"] = list
		step["goto"] = "$end"
		return step
	}
	endStep := func(id string) map[string]any {
		step := logicStep(id)
		step["goto"] = "$end"
		return step
	}

	t.Run("merge-three-producers", func(t *testing.T) {
		got := run([]any{
			branch("pc", [2]string{"flag1", "pa"}, [2]string{"flag2", "pb"}),
			producer("pa", "x", "merge"),
			producer("pb", "y", "merge"),
			producer("pc", "z", "merge"),
			mergeStep(
				map[string]any{"name": "a", "value": "step.pa.x", "description": "x"},
				map[string]any{"name": "b", "value": "step.pb.y", "description": "x"},
				map[string]any{"name": "c", "value": "step.pc.z", "description": "x"},
			),
		}, params("flag1", "flag2"))
		if strings.TrimSpace(got) != "" {
			t.Fatalf("complete three-way merge reported: %s", got)
		}
	})

	t.Run("merge-same-path-conflict", func(t *testing.T) {
		got := run([]any{
			branch("merge", [2]string{"flag", "a"}),
			producer("a", "x", "b"),
			producer("b", "y", "merge"),
			mergeStep(
				map[string]any{"name": "a", "value": "step.a.x", "description": "x"},
				map[string]any{"name": "b", "value": "step.b.y", "description": "x"},
			),
		}, params("flag"))
		if !strings.Contains(got, "[service-merge]") || !strings.Contains(got, "both can execute on the same path") {
			t.Fatalf("same-path conflict reported: %s", got)
		}
	})

	t.Run("merge-multi-exit-return", func(t *testing.T) {
		got := run([]any{
			branch("fail", [2]string{"flag", "load"}),
			producer("load", "x", "ok"),
			endStep("ok"),
			endStep("fail"),
		}, params("flag"), map[string]any{"name": "value", "type": "string", "value": "step.load.x"})
		if strings.TrimSpace(got) != "" {
			t.Fatalf("multi-exit return reported: %s", got)
		}
	})

	t.Run("merge-multi-condition-multi-exit", func(t *testing.T) {
		got := run([]any{
			producer("load", "x", "branch"),
			branch("merge", [2]string{"f1", "endA"}, [2]string{"f2", "endB"}),
			endStep("endA"),
			endStep("endB"),
			endStep("merge"),
		}, params("f1", "f2"), map[string]any{"name": "value", "type": "string", "value": "step.load.x"})
		if strings.TrimSpace(got) != "" {
			t.Fatalf("multi-condition multi-exit reported: %s", got)
		}
	})

	t.Run("merge-three-way-bypass", func(t *testing.T) {
		got := run([]any{
			branch("merge", [2]string{"f1", "pa"}, [2]string{"f2", "pb"}, [2]string{"f3", "pc"}),
			producer("pa", "x", "merge"),
			producer("pb", "y", "merge"),
			producer("pc", "z", "merge"),
			mergeStep(
				map[string]any{"name": "a", "value": "step.pa.x", "description": "x"},
				map[string]any{"name": "b", "value": "step.pb.y", "description": "x"},
				map[string]any{"name": "c", "value": "step.pc.z", "description": "x"},
			),
		}, params("f1", "f2", "f3"))
		if !strings.Contains(got, "[service-merge]") || !strings.Contains(got, "without executing any of them") {
			t.Fatalf("three-way bypass not reported: %s", got)
		}
	})

	t.Run("merge-unreachable-return", func(t *testing.T) {
		got := run([]any{
			branch("src", [2]string{"flag", "$end"}),
			producer("src", "x", "sink"),
			func() map[string]any {
				step := logicStep("sink")
				step["goto"] = "sink"
				return step
			}(),
		}, params("flag"), map[string]any{"name": "value", "type": "string", "value": "step.src.x"})
		if !strings.Contains(got, "[service-merge]") || !strings.Contains(got, "cannot reach service end") {
			t.Fatalf("unreachable return not reported: %s", got)
		}
	})

	t.Run("when-duplicate-target", func(t *testing.T) {
		got := run([]any{
			branch("$end", [2]string{"f1", "pa"}, [2]string{"f2", "pa"}),
			producer("pa", "x", "$end"),
		}, params("f1", "f2"), map[string]any{"name": "value", "type": "string", "value": "step.pa.x"})
		if strings.TrimSpace(got) != "" {
			t.Fatalf("duplicate when target reported: %s", got)
		}
	})

	t.Run("when-producer-branch-multi-exit", func(t *testing.T) {
		got := run([]any{
			branch("$end", [2]string{"f1", "pa"}),
			producer("pa", "x", "merge"),
			func() map[string]any {
				step := logicStep("merge")
				step["input"] = []any{map[string]any{"name": "x", "value": "step.pa.x", "description": "x"}}
				step["goto"] = "$end"
				return step
			}(),
		}, params("f1"))
		if strings.TrimSpace(got) != "" {
			t.Fatalf("producer branch multi-exit reported: %s", got)
		}
	})

	t.Run("dominating-producer-across-split", func(t *testing.T) {
		got := run([]any{
			producer("p", "x", "branch"),
			branch("merge", [2]string{"f1", "a"}, [2]string{"f2", "b"}),
			endStep("a"),
			endStep("b"),
			func() map[string]any {
				step := logicStep("merge")
				step["input"] = []any{map[string]any{"name": "x", "value": "step.p.x", "description": "x"}}
				step["goto"] = "$end"
				return step
			}(),
		}, params("f1", "f2"))
		if strings.TrimSpace(got) != "" {
			t.Fatalf("dominating producer reported: %s", got)
		}
	})

	t.Run("when-two-conditions-one-producer", func(t *testing.T) {
		got := run([]any{
			branch("$end", [2]string{"f1", "pa"}, [2]string{"f2", "pb"}),
			producer("pa", "x", "merge"),
			func() map[string]any {
				step := logicStep("pb")
				step["goto"] = "$end"
				return step
			}(),
			func() map[string]any {
				step := logicStep("merge")
				step["input"] = []any{map[string]any{"name": "x", "value": "step.pa.x", "description": "x"}}
				step["goto"] = "$end"
				return step
			}(),
		}, params("f1", "f2"))
		if strings.TrimSpace(got) != "" {
			t.Fatalf("single producer on one condition reported: %s", got)
		}
	})

	t.Run("unreachable-producer-no-fake-merge", func(t *testing.T) {
		got := run([]any{
			branch("$end", [2]string{"f1", "$end"}),
			producer("orphan", "x", "$end"),
			func() map[string]any {
				step := logicStep("use")
				step["input"] = []any{map[string]any{"name": "x", "value": "step.orphan.x", "description": "x"}}
				step["goto"] = "$end"
				return step
			}(),
		}, params("f1"))
		if strings.Contains(got, "[service-merge]") {
			t.Fatalf("unreachable producer produced a merge error: %s", got)
		}
		if !strings.Contains(got, "[service-goto]") {
			t.Fatalf("unreachable steps not reported: %s", got)
		}
	})

	t.Run("when-condition-bypasses-producer", func(t *testing.T) {
		got := run([]any{
			branch("$end", [2]string{"f1", "pa"}, [2]string{"f2", "merge"}),
			producer("pa", "x", "merge"),
			func() map[string]any {
				step := logicStep("merge")
				step["input"] = []any{map[string]any{"name": "x", "value": "step.pa.x", "description": "x"}}
				step["goto"] = "$end"
				return step
			}(),
		}, params("f1", "f2"))
		if !strings.Contains(got, "[service-merge]") || !strings.Contains(got, "without executing any of them") {
			t.Fatalf("when bypass not reported: %s", got)
		}
	})

	t.Run("unreachable-producer-referenced-by-reachable-merge", func(t *testing.T) {
		got := run([]any{
			branch("merge", [2]string{"f1", "$end"}),
			producer("orphan", "x", "$end"),
			func() map[string]any {
				step := logicStep("merge")
				step["input"] = []any{map[string]any{"name": "x", "value": "step.orphan.x", "description": "x"}}
				step["goto"] = "$end"
				return step
			}(),
		}, params("f1"))
		if strings.Contains(got, "[service-merge]") {
			t.Fatalf("unreachable producer produced a merge error: %s", got)
		}
		if !strings.Contains(got, "[service-chain]") || !strings.Contains(got, "not reachable via goto") {
			t.Fatalf("unreachable producer ref not reported: %s", got)
		}
	})

	t.Run("when-back-edge-does-not-fake-merge", func(t *testing.T) {
		got := run([]any{
			branch("$end", [2]string{"flag", "a"}),
			func() map[string]any {
				step := logicStep("a")
				step["goto"] = "b"
				return step
			}(),
			func() map[string]any {
				step := logicStep("b")
				step["goto"] = "a"
				return step
			}(),
		}, params("flag"))
		if strings.Contains(got, "[service-merge]") {
			t.Fatalf("back edge produced a merge error: %s", got)
		}
		if !strings.Contains(got, "[service-goto]") || !strings.Contains(got, "must target a later step") {
			t.Fatalf("back edge not reported by service-goto: %s", got)
		}
	})

	t.Run("unreachable-step-does-not-fake-merge", func(t *testing.T) {
		got := run([]any{
			branch("$end", [2]string{"flag", "$end"}),
			func() map[string]any {
				step := logicStep("orphan")
				step["goto"] = "$end"
				return step
			}(),
		}, params("flag"))
		if strings.Contains(got, "[service-merge]") {
			t.Fatalf("orphan step produced a merge error: %s", got)
		}
		if !strings.Contains(got, "[service-goto]") || !strings.Contains(got, "unreachable from the first step") {
			t.Fatalf("orphan step not reported by service-goto: %s", got)
		}
	})

	t.Run("merge-third-path", func(t *testing.T) {
		got := run([]any{
			branch("merge", [2]string{"flag1", "pa"}, [2]string{"flag2", "pb"}),
			producer("pa", "x", "merge"),
			producer("pb", "y", "merge"),
			func() map[string]any {
				step := logicStep("merge")
				step["input"] = []any{
					map[string]any{"name": "a", "value": "step.pa.x", "description": "x"},
					map[string]any{"name": "b", "value": "step.pb.y", "description": "x"},
				}
				step["goto"] = "$end"
				return step
			}(),
		}, params("flag1", "flag2"))
		if !strings.Contains(got, "[service-merge]") || !strings.Contains(got, "without executing any of them") {
			t.Fatalf("third path not reported: %s", got)
		}
	})

	t.Run("consumer-on-producer-branch", func(t *testing.T) {
		got := run([]any{
			branch("$end", [2]string{"flag", "pa"}),
			producer("pa", "x", "use"),
			func() map[string]any {
				step := logicStep("use")
				step["input"] = []any{map[string]any{"name": "x", "value": "step.pa.x", "description": "x"}}
				step["goto"] = "$end"
				return step
			}(),
		}, params("flag"))
		if strings.TrimSpace(got) != "" {
			t.Fatalf("producer-branch consumption reported: %s", got)
		}
	})

	t.Run("consumer-unreachable-from-producer", func(t *testing.T) {
		got := run([]any{
			branch("use", [2]string{"flag", "pa"}),
			producer("pa", "x", "$end"),
			func() map[string]any {
				step := logicStep("use")
				step["input"] = []any{map[string]any{"name": "x", "value": "step.pa.x", "description": "x"}}
				step["goto"] = "$end"
				return step
			}(),
		}, params("flag"))
		if !strings.Contains(got, "not reachable via goto") || !strings.Contains(got, "[service-chain]") {
			t.Fatalf("unreachable consumption not reported: %s", got)
		}
		if !strings.Contains(got, "[service-merge]") {
			t.Fatalf("bypassing path not reported: %s", got)
		}
	})
}
