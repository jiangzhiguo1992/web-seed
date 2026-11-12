package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFixture(t, root, "contracts/data/tables/a.yaml", `# table a
id: data.tables.a
summary: "a"
`)
	writeFixture(t, root, "contracts/data/tables/b.yaml", `# table b
id: data.tables.b
summary: "b"
used_by:
  entities: [ data.entities.wrong ]
  queries: []
`)
	writeFixture(t, root, "contracts/data/entities/e1.yaml", `# entity e1
id: data.entities.e1
table: data.tables.a
relations:
  - { name: e2, target: data.entities.e2, kind: many_to_one, via: data.tables.a.col.id }
`)
	writeFixture(t, root, "contracts/data/entities/e2.yaml", `# entity e2
id: data.entities.e2
table: data.tables.b
used_by:
  queries: []
  entities: [ data.entities.e1 ]
`)
	writeFixture(t, root, "contracts/data/queries/q.yaml", `id: data.queries.q
queries:
  - id: data.queries.q.get_a
    table: data.tables.a
    returns: data.entities.e1
  - id: data.queries.q.get_b
    table: data.tables.b
    returns: boolean
`)
	return root
}

func TestCompute(t *testing.T) {
	root := fixtureRoot(t)
	snap, err := Compute(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Tables["data.tables.a"].Entities; len(got) != 1 || got[0] != "data.entities.e1" {
		t.Fatalf("table a entities = %v", got)
	}
	if got := snap.Tables["data.tables.a"].Queries; len(got) != 1 || got[0] != "data.queries.q.get_a" {
		t.Fatalf("table a queries = %v", got)
	}
	if got := snap.Tables["data.tables.b"].Entities; len(got) != 1 || got[0] != "data.entities.e2" {
		t.Fatalf("table b entities = %v", got)
	}
	if got := snap.Entities["data.entities.e1"].Queries; len(got) != 1 || got[0] != "data.queries.q.get_a" {
		t.Fatalf("entity e1 queries = %v", got)
	}
	if got := snap.Entities["data.entities.e2"].Entities; len(got) != 1 || got[0] != "data.entities.e1" {
		t.Fatalf("entity e2 inbound entities = %v", got)
	}
}

func TestWriteAndCheck(t *testing.T) {
	root := fixtureRoot(t)
	snap, err := Compute(root)
	if err != nil {
		t.Fatal(err)
	}
	drift, err := snap.Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) == 0 {
		t.Fatal("expected used_by drift before write")
	}
	changed, err := snap.Write(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) == 0 {
		t.Fatal("expected files to change")
	}

	a, _ := os.ReadFile(filepath.Join(root, "contracts/data/tables/a.yaml"))
	if !strings.Contains(string(a), "# table a") {
		t.Fatalf("comment lost:\n%s", a)
	}
	if !strings.Contains(string(a), "used_by:\n  entities:\n    - data.entities.e1\n") {
		t.Fatalf("table used_by not written:\n%s", a)
	}
	b, _ := os.ReadFile(filepath.Join(root, "contracts/data/tables/b.yaml"))
	if strings.Contains(string(b), "data.entities.wrong") {
		t.Fatalf("stale used_by not replaced:\n%s", b)
	}
	e1, _ := os.ReadFile(filepath.Join(root, "contracts/data/entities/e1.yaml"))
	if !strings.Contains(string(e1), "used_by:\n  queries:\n    - data.queries.q.get_a\n") {
		t.Fatalf("entity used_by not written:\n%s", e1)
	}

	drift, err = snap.Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Fatalf("unexpected drift after write: %v", drift)
	}
	changed, err = snap.Write(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("write is not idempotent: %v", changed)
	}
}

func TestUsedByNotLast(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "contracts/data/tables/a.yaml", `id: data.tables.a
used_by:
  entities: []
  queries: []
after: 1
`)
	src, _ := os.ReadFile(filepath.Join(root, "contracts/data/tables/a.yaml"))
	out, err := applyUsedBy(src, renderUsedBy("table", []string{"data.entities.e"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "after: 1") {
		t.Fatalf("following key lost:\n%s", text)
	}
	if !strings.Contains(text, "data.entities.e") {
		t.Fatalf("used_by not replaced:\n%s", text)
	}
}
