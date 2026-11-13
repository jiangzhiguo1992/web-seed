package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func tableSchemaForTest() *contractspec.Schema {
	return &contractspec.Schema{
		Type:    "data.tables",
		Targets: contractspec.Targets{Dir: "contracts/data/tables", Glob: "*.yaml", ID: "data.tables.<file>"},
	}
}

func writeTable(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTableContract(t *testing.T) {
	root := t.TempDir()
	writeTable(t, root, "note", `id: data.tables.note
owner_module: notes
`)
	owner, tableID, err := loadTableContract(root, tableSchemaForTest(), "note")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "notes" || tableID != "data.tables.note" {
		t.Fatalf("owner/tableID = %q/%q, want notes/data.tables.note", owner, tableID)
	}

	if _, _, err := loadTableContract(root, tableSchemaForTest(), "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing table not reported: %v", err)
	}

	writeTable(t, root, "bad", "id: data.tables.other\n")
	if _, _, err := loadTableContract(root, tableSchemaForTest(), "bad"); err == nil || !strings.Contains(err.Error(), "id must be") {
		t.Fatalf("id mismatch not reported: %v", err)
	}
}
