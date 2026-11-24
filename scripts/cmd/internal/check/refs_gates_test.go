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
