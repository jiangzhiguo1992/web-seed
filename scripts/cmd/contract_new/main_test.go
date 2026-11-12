package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
table:
  name: notes
`)
	physical, err := loadTableContract(root, "note")
	if err != nil {
		t.Fatal(err)
	}
	if physical != "notes" {
		t.Fatalf("physical = %q, want notes", physical)
	}

	if _, err := loadTableContract(root, "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing table not reported: %v", err)
	}

	writeTable(t, root, "bad", "id: data.tables.other\n")
	if _, err := loadTableContract(root, "bad"); err == nil || !strings.Contains(err.Error(), "id must be") {
		t.Fatalf("id mismatch not reported: %v", err)
	}
}
