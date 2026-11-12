package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlacementFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoNotDuplicate(t *testing.T) {
	root := t.TempDir()
	writePlacementFile(t, root, "contracts/data/README.md", "## 规则\n\n- \"generic rule\"\n")
	writePlacementFile(t, root, "contracts/data/entities/dup.yaml", "do_not:\n  - \"generic rule\"\n")
	writePlacementFile(t, root, "contracts/data/entities/ok.yaml", "do_not:\n  - \"ok 专属规则\"\n")
	writePlacementFile(t, root, "contracts/data/entities/dup1.yaml", "do_not:\n  - \"跨文件重复\"\n")
	writePlacementFile(t, root, "contracts/data/entities/dup2.yaml", "do_not:\n  - \"跨文件重复\"\n")

	c := &Checker{root: root}
	c.checkDoNotDuplicates()
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "dup.yaml: do_not duplicates group guidance") {
		t.Fatalf("guidance duplicate not reported: %v", c.errs)
	}
	if !strings.Contains(errs, "do_not duplicates contracts/data/entities/dup1.yaml") {
		t.Fatalf("cross-file duplicate not reported: %v", c.errs)
	}
	if strings.Contains(errs, "ok.yaml") {
		t.Fatalf("specific do_not wrongly reported: %v", c.errs)
	}
}

func TestFieldPlacement(t *testing.T) {
	root := t.TempDir()
	writePlacementFile(t, root, "contracts/_meta/ok.yaml", "rules:\n  - { id: x }\n")
	writePlacementFile(t, root, "contracts/_meta/style_ok.yaml", "fields:\n  - name: schema_version\n    type: integer\n")
	writePlacementFile(t, root, "contracts/_meta/style_bad.yaml", "fields:\n  - { name: schema_version, type: integer }\n")
	writePlacementFile(t, root, "contracts/_meta/style_bad2.yaml", "fields: [{ name: schema_version }]\n")
	writePlacementFile(t, root, "contracts/_meta/bad.yaml", "types:\n  x:\n    do_not:\n      - \"x\"\n")
	writePlacementFile(t, root, "contracts/data/entities/x.yaml", "rules:\n  - { id: x }\n")
	writePlacementFile(t, root, "contracts/data/entities/style_bad.yaml", "fields:\n  - { name: id, type: string }\n")
	writePlacementFile(t, root, "contracts/data/entities/style_ok.yaml", "fields:\n  - name: id\n    type: string\n")
	writePlacementFile(t, root, "contracts/business/rules/x.yaml", "rules:\n  - { id: x }\n")
	writePlacementFile(t, root, "contracts/platform/architecture/go_arch.yaml", "rules:\n  - { id: x }\n")
	writePlacementFile(t, root, "contracts/platform/architecture/other.yaml", "rules:\n  - { id: x }\n")
	writePlacementFile(t, root, "contracts/features/pages/nested.yaml", "page:\n  rules:\n    - x\n")

	c := &Checker{root: root}
	c.checkFieldPlacement()
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "_meta must not declare do_not") {
		t.Fatalf("meta do_not not reported: %v", c.errs)
	}
	if strings.Count(errs, "concrete contract must not declare rules") != 3 {
		t.Fatalf("want entity + other platform + nested reported: %v", c.errs)
	}
	if strings.Count(errs, "top-level fields must be block style") != 3 {
		t.Fatalf("want three flow-style files reported: %v", c.errs)
	}
	if strings.Contains(errs, "style_ok.yaml") {
		t.Fatalf("block-style fields wrongly reported: %v", c.errs)
	}
	if strings.Contains(errs, "business/rules/x.yaml") || strings.Contains(errs, "architecture/go_arch.yaml") {
		t.Fatalf("domain rules wrongly reported: %v", c.errs)
	}
}
