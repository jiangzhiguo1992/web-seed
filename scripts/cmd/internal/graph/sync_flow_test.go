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

func TestSyncItemFieldBlockFlowMultiline(t *testing.T) {
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

	write("constraints:\n  - { name: ck_m,\n      expr: \"m > 0\",\n      used_by: [] }\n")
	changed, err := syncItemFieldBlock(path, "constraints", "name", "ck_m", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected multiline flow item update")
	}
	got := read()
	if !strings.Contains(got, "used_by: [ data.queries.q.get ]") {
		t.Fatalf("multiline used_by not replaced:\n%s", got)
	}
	if !strings.Contains(got, "\n      expr: \"m > 0\"") {
		t.Fatalf("multiline layout lost:\n%s", got)
	}

	write("constraints:\n  - { name: ck_n,\n      expr: \"n > 0\" }\n")
	changed, err = syncItemFieldBlock(path, "constraints", "name", "ck_n", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected multiline flow item insertion")
	}
	got = read()
	if !strings.Contains(got, ", used_by: [ data.queries.q.get ]") {
		t.Fatalf("multiline used_by not inserted:\n%s", got)
	}
	if !strings.Contains(got, "\n      expr: \"n > 0\"") {
		t.Fatalf("multiline insertion lost fields:\n%s", got)
	}

	write("constraints:\n  - { name: ck_s, expr: \"a,b:c\", used_by: [] }\n")
	changed, err = syncItemFieldBlock(path, "constraints", "name", "ck_s", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected quoted value update")
	}
	got = read()
	if !strings.Contains(got, "expr: \"a,b:c\"") {
		t.Fatalf("quoted comma/colon value corrupted:\n%s", got)
	}

	write("constraints:\n  - { name: ck_c, used_by: [] } # keep\n")
	changed, err = syncItemFieldBlock(path, "constraints", "name", "ck_c", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected trailing comment update")
	}
	got = read()
	if !strings.Contains(got, "# keep") {
		t.Fatalf("trailing comment lost:\n%s", got)
	}

	write("constraints:\n  - { \"name\": \"ck_q\", \"used_by\": [] }\n")
	changed, err = syncItemFieldBlock(path, "constraints", "name", "ck_q", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected quoted key update")
	}
	got = read()
	if !strings.Contains(got, "\"used_by\": [ data.queries.q.get ]") {
		t.Fatalf("quoted key not preserved:\n%s", got)
	}

	write("constraints:\n  -  { name: ck_sp, used_by: [] }\n")
	changed, err = syncItemFieldBlock(path, "constraints", "name", "ck_sp", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected extra-space flow item update")
	}
	got = read()
	if !strings.Contains(got, "used_by: [ data.queries.q.get ]") {
		t.Fatalf("extra-space flow item not updated:\n%s", got)
	}
}

func TestSyncItemFieldBlockFlowSkipsOtherMultilineItem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	if err := os.WriteFile(path, []byte("constraints:\n  - { name: other,\n      used_by: [] }\n  - { name: ck_t, used_by: [] }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := syncItemFieldBlock(path, "constraints", "name", "ck_t", "used_by", []string{"data.queries.q.get"})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected target item update")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "- { name: other,\n      used_by: [] }") {
		t.Fatalf("other multiline item changed:\n%s", got)
	}
	if !strings.Contains(got, "- { name: ck_t, used_by: [ data.queries.q.get ] }") {
		t.Fatalf("target item not updated:\n%s", got)
	}
}
