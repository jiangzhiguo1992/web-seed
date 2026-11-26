package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncItemFieldBlockFlow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")

	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func() string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	write("constraints:\n  - { name: ck_x, expr: \"x > 0\", used_by: [] }\n")
	changed, err := syncItemFieldBlock(path, "constraints", "name", "ck_x", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected flow item update")
	}
	got := read()
	if !strings.Contains(got, "used_by: [ data.queries.q.get ]") {
		t.Fatalf("flow used_by not replaced:\n%s", got)
	}
	if !strings.Contains(got, "expr: \"x > 0\"") {
		t.Fatalf("other fields lost:\n%s", got)
	}

	changed, err = syncItemFieldBlock(path, "constraints", "name", "ck_x", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("flow update must be idempotent")
	}

	write("constraints:\n  - { name: ck_y, expr: \"y > 0\", source: { join: { from: a, to: b } } }\n")
	changed, err = syncItemFieldBlock(path, "constraints", "name", "ck_y", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected flow item insertion")
	}
	got = read()
	if !strings.Contains(got, "source: { join: { from: a, to: b } }") {
		t.Fatalf("nested flow map lost:\n%s", got)
	}
	if !strings.Contains(got, ", used_by: [ data.queries.q.get ]") {
		t.Fatalf("flow used_by not inserted:\n%s", got)
	}

	changed, err = syncItemFieldBlock(path, "constraints", "name", "ck_y", "used_by", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected flow item empty update")
	}
	got = read()
	if !strings.Contains(got, "used_by: []") {
		t.Fatalf("flow used_by not emptied:\n%s", got)
	}
}
