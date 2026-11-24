package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnforcementGates(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "Makefile", "gen-x:\n\t@true\n")
	writeCapFile(t, root, "scripts/cmd/internal/check/x_test.go", "package check\n")
	writeCapFile(t, root, "contracts/_meta/x.yaml", "type: x\n")
	doc := map[string]any{
		"enforcement_gates": map[string]any{
			"generated": map[string]any{"x": []any{"make gen-x"}},
			"tested":    map[string]any{"x": []any{"scripts/cmd/internal/check/x_test.go"}},
		},
	}
	entries := []any{map[string]any{
		"id": "x.a", "path": "x/a.yaml", "enforcement": "generated+tested",
	}}
	c := &Checker{root: root}
	c.checkEnforcementGates("contracts/index.yaml", entries, doc)
	if len(c.errs) != 0 {
		t.Fatalf("valid gates reported: %v", c.errs)
	}

	delete(doc["enforcement_gates"].(map[string]any)["generated"].(map[string]any), "x")
	c.errs = nil
	c.checkEnforcementGates("contracts/index.yaml", entries, doc)
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "[schema.enforcement]") || !strings.Contains(got, "enforcement_gates.generated.x is missing") {
		t.Fatalf("missing generated gate not reported: %v", c.errs)
	}

	os.Remove(filepath.Join(root, "Makefile"))
	c.errs = nil
	c.checkEnforcementGates("contracts/index.yaml", entries, doc)
	if len(c.errs) != 0 {
		t.Fatalf("fixture without Makefile must skip gates: %v", c.errs)
	}
}

func TestEnforcementGateRequiresRuleAssertion(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "Makefile", "gen-x:\n\t@true\n")
	writeCapFile(t, root, "contracts/_meta/y.yaml", "type: y\nrules:\n  - { id: rule-y, text: \"x\", implemented_by: [ rule-y ] }\n")
	writeCapFile(t, root, "scripts/cmd/internal/check/y_test.go", "package check\n")
	doc := map[string]any{
		"enforcement_gates": map[string]any{
			"tested": map[string]any{"y": []any{"scripts/cmd/internal/check/y_test.go"}},
		},
	}
	entries := []any{map[string]any{"id": "y.a", "path": "y/a.yaml", "enforcement": "tested"}}
	c := &Checker{root: root}
	c.checkEnforcementGates("contracts/index.yaml", entries, doc)
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "does not assert any y rule") {
		t.Fatalf("missing assertion not reported: %v", c.errs)
	}

	writeCapFile(t, root, "scripts/cmd/internal/check/y_test.go", "package check\nfunc TestY(t *testing.T) { mustContainRule(t, errs, \"rule-y\") }\n")
	c.errs = nil
	c.checkEnforcementGates("contracts/index.yaml", entries, doc)
	if len(c.errs) != 0 {
		t.Fatalf("valid assertion reported: %v", c.errs)
	}
}

func makeRecipe(content, target string) string {
	var b strings.Builder
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, target+":") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		if !strings.HasPrefix(line, "\t") {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func TestQueriesEnforcementFollowsSQLC(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	makefile := filepath.Join(root, "Makefile")
	makeBytes, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatal(err)
	}
	targets := makeTargets(makefile)
	_, hasGen := targets["gen-sqlc"]
	_, hasCheck := targets["gen-sqlc-check"]
	doc, err := loadYAML(filepath.Join(root, "contracts/index.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	gates, _ := asMap(doc["enforcement_gates"])
	generated, _ := asMap(gates["generated"])
	_, hasGate := generated["data.queries"]
	if hasGen != hasGate {
		t.Fatalf("gen-sqlc=%v but enforcement_gates.generated.data.queries=%v", hasGen, hasGate)
	}
	if hasGen && !hasCheck {
		t.Fatal("gen-sqlc exists without gen-sqlc-check")
	}
	if hasGen {
		commands := asList(generated["data.queries"])
		found := false
		for _, raw := range commands {
			if command, _ := raw.(string); command == "make gen-sqlc" {
				found = true
			}
		}
		if !found {
			t.Fatalf("enforcement_gates.generated.data.queries = %v, want make gen-sqlc", commands)
		}
		for _, target := range []string{"gen", "gen-check-core"} {
			recipe := makeRecipe(string(makeBytes), target)
			want := "gen-sqlc"
			if target == "gen-check-core" {
				want = "gen-sqlc-check"
			}
			if !strings.Contains(recipe, want) {
				t.Fatalf("Makefile target %s does not run %s:\n%s", target, want, recipe)
			}
		}
	}
	want := "tested"
	if hasGen {
		want = "generated+tested"
	}
	for _, raw := range asList(doc["entries"]) {
		entry, _ := asMap(raw)
		id, _ := entry["id"].(string)
		if !strings.HasPrefix(id, "data.queries.") {
			continue
		}
		if got, _ := entry["enforcement"].(string); got != want {
			t.Fatalf("%s enforcement = %s, want %s (gen-sqlc=%v)", id, got, want, hasGen)
		}
	}
}
