package ruletests

import "testing"

func TestRefsInIgnoresComments(t *testing.T) {
	content := `package check

// mustContainRule(t, errs, "rule-a")
// "[rule-b]"

func TestX(t *testing.T) {
	// rule-c is only mentioned here
	cases := []struct{ rule string }{{"rule-c"}}
	for _, tc := range cases {
		mustContainRule(t, errs, tc.rule)
	}
}`
	refs := RefsIn(content, []string{"rule-a", "rule-b", "rule-c"})
	if refs["rule-a"].Asserted != 0 || refs["rule-a"].Dynamic != 0 {
		t.Fatalf("comment asserted rule-a: %+v", refs["rule-a"])
	}
	if refs["rule-b"].Asserted != 0 || refs["rule-b"].Dynamic != 0 {
		t.Fatalf("comment asserted rule-b: %+v", refs["rule-b"])
	}
	if refs["rule-c"].Dynamic != 1 {
		t.Fatalf("table rule-c dynamic = %d, want 1", refs["rule-c"].Dynamic)
	}
}
