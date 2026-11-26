//go:build rules

// invariants_test.go 是 feature.flows invariant 规则的可执行断言。
//
// invariantAssertions 是 flow-rule-verified 识别的行为注册表：每个 invariant
// 必须在表中有一个键，并且用真值表执行 assert，而不是只检查字符串非空。
package check

import (
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/rulesdsl"
)

type invariantCase struct {
	env  rulesdsl.Env
	want bool
}

var invariantAssertions = map[string][]invariantCase{
	"feature.flows.account.nickname_length": {
		{env: rulesdsl.Env{"nickname": "Alice"}, want: true},
		{env: rulesdsl.Env{"nickname": ""}, want: false},
	},
	"feature.flows.dictionary.name_length": {
		{env: rulesdsl.Env{"name": "x"}, want: true},
		{env: rulesdsl.Env{"name": ""}, want: false},
	},
	"feature.flows.account.deleted_status_consistent": {
		{env: rulesdsl.Env{"status": "deleted", "deleted_at": "2026-01-01"}, want: true},
		{env: rulesdsl.Env{"status": "deleted", "deleted_at": nil}, want: false},
	},
	"feature.flows.auth.verification_consumed_consistent": {
		{env: rulesdsl.Env{"status": "consumed", "consumed_at": "2026-01-01"}, want: true},
		{env: rulesdsl.Env{"status": "consumed", "consumed_at": nil}, want: false},
	},
}

func TestInvariantRulesDeclared(t *testing.T) {
	for id := range invariantAssertions {
		id := id
		t.Run(id, func(t *testing.T) {
			rule, err := loadPolicyRule(id)
			if err != nil {
				t.Fatal(err)
			}
			if rule.Kind != "invariant" {
				t.Fatalf("rule %s kind = %s, want invariant", id, rule.Kind)
			}
			if strings.TrimSpace(rule.Assert) == "" {
				t.Fatalf("invariant %s has empty assert", id)
			}
		})
	}
}

func TestInvariantAssertions(t *testing.T) {
	for id, cases := range invariantAssertions {
		id, cases := id, cases
		t.Run(id, func(t *testing.T) {
			rule, err := loadPolicyRule(id)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range cases {
				v, err := rulesdsl.Eval(rule.Assert, tc.env)
				if err != nil {
					t.Fatalf("eval %s: %v", rule.Assert, err)
				}
				got, ok := v.(bool)
				if !ok {
					t.Fatalf("eval %s = %T, want bool", rule.Assert, v)
				}
				if got != tc.want {
					t.Fatalf("eval %s with %v = %v, want %v", rule.Assert, tc.env, got, tc.want)
				}
			}
		})
	}
}
