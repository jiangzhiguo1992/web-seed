package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTotalCoverage(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "cover.out")
	body := `mode: set
example/a.go:1.1,2.2 2 1
example/b.go:1.1,2.2 2 0
`
	if err := os.WriteFile(profile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := totalCoverage(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got != 50 {
		t.Fatalf("coverage = %v, want 50", got)
	}
}

func TestLoadThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contracts/platform/quality.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "coverage:\n  api: { line: 70 }\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadThreshold(dir, "api")
	if err != nil {
		t.Fatal(err)
	}
	if got != 70 {
		t.Fatalf("threshold = %d, want 70", got)
	}
	if _, err := loadThreshold(dir, "web"); err == nil {
		t.Fatal("missing web threshold must fail")
	}
}
