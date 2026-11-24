package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTestRefs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "scripts/cmd/internal/check")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("rule_ids_gen.go", `var knownRuleIDs = map[string]bool{
  "rule-a": true,
  "rule-b": true,
  "rule-c": true,
  "rule-d": true,
}`)
	write("assert_test.go", `func TestAssert(t *testing.T) {
  mustContainRule(t, errs, "rule-a")
  _ = "[rule-b]"
  rule := "rule-c"
  _ = "[" + rule + "]"
}`)
	write("mention_test.go", `func TestMention(t *testing.T) {
  // rule-d is mentioned but never asserted
}`)

	refs := loadTestRefs(root, []string{"rule-a", "rule-b", "rule-c", "rule-d"})
	if got := refs["rule-a"].asserted; got != 1 {
		t.Fatalf("rule-a asserted = %d, want 1", got)
	}
	if got := refs["rule-b"].asserted; got != 1 {
		t.Fatalf("rule-b asserted = %d, want 1", got)
	}
	if got := refs["rule-c"].dynamic; got != 1 {
		t.Fatalf("rule-c dynamic = %d, want 1", got)
	}
	if got := refs["rule-d"]; got.asserted != 0 || got.dynamic != 0 {
		t.Fatalf("rule-d refs = %+v, want zero", got)
	}
}
