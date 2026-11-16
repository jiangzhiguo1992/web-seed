package graph

import (
	"fmt"
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

// writeTargets 为 graph.Compute 提供 _meta targets，避免测试依赖生产回退。
func writeTargets(t *testing.T, root, tableDir, entityDir, queryDir string) {
	t.Helper()
	writeFixture(t, root, "contracts/_meta/data.tables.yaml", fmt.Sprintf("type: data.tables\ntargets:\n  dir: %q\n  glob: \"*.yaml\"\n  id: \"data.tables.<file>\"\n", tableDir))
	writeFixture(t, root, "contracts/_meta/data.entities.yaml", fmt.Sprintf("type: data.entities\ntargets:\n  dir: %q\n  glob: \"*.yaml\"\n  id: \"data.entities.<file>\"\n", entityDir))
	writeFixture(t, root, "contracts/_meta/data.queries.yaml", fmt.Sprintf("type: data.queries\ntargets:\n  dir: %q\n  glob: \"*.yaml\"\n  id: \"data.queries.<file>\"\n", queryDir))
	serviceDir := "contracts/capabilities/services"
	writeFixture(t, root, "contracts/_meta/cap.services.yaml", fmt.Sprintf("type: cap.services\ntargets:\n  dir: %q\n  glob: \"*.yaml\"\n  id: \"cap.services.<file>\"\n", serviceDir))
	ensureDirs(t, root, serviceDir)
}

func ensureDirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	writeFixture(t, root, "contracts/data/tables/a.yaml", `# table a
id: data.tables.a
summary: "a"
table:
  comment: "a"
  extends: [ data.tables.base ]
  entities: [ data.entities.e1 ]
  queries: []
`)
	writeFixture(t, root, "contracts/data/tables/b.yaml", `# table b
id: data.tables.b
summary: "b"
table:
  comment: "b"
  extends: [ data.tables.base ]
  entities: []
  queries: []
`)
	writeFixture(t, root, "contracts/data/entities/e1.yaml", `# entity e1
id: data.entities.e1
model:
  name: e1
  extends: [ data.entities.base ]
  tables: [ data.tables.a ]
  queries: []
  included_by: []
fields:
  - { name: E2, target: data.tables.b, type: data.entities.e2, join_column_from: data.tables.a.col.id, join_column_to: data.tables.b.col.id, description: "e2" }
`)
	writeFixture(t, root, "contracts/data/entities/e2.yaml", `# entity e2
id: data.entities.e2
model:
  name: e2
  extends: [ data.entities.base ]
  tables: [ data.tables.b ]
  queries: []
  included_by: [ data.entities.e1 ]
`)
	writeFixture(t, root, "contracts/data/queries/q.yaml", `id: data.queries.q
queries:
  - name: GetA
    returns:
      - { name: e1, kind: entity, entity: data.entities.e1 }
    tables: [ data.tables.a ]
    used_by: []
  - name: GetB
    returns:
      - { name: exists, kind: scalar, type: boolean }
    tables: [ data.tables.b ]
    used_by: []
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
services:
  - name: get
    steps:
      - id: load
        kind: logic
        queries:
          - data.queries.q.get_a
`)
	return root
}

func TestCompute(t *testing.T) {
	root := fixtureRoot(t)
	snap, err := Compute(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Tables["data.tables.a"].Queries; len(got) != 1 || got[0] != "data.queries.q.get_a" {
		t.Fatalf("table a queries = %v", got)
	}
	if got := snap.Entities["data.entities.e1"].Queries; len(got) != 1 || got[0] != "data.queries.q.get_a" {
		t.Fatalf("entity e1 queries = %v", got)
	}
	if got := snap.Queries["data.queries.q.get_a"]; len(got) != 1 || got[0] != "cap.services.x.get" {
		t.Fatalf("query get_a used_by = %v", got)
	}
}

func TestComputeUsesTargets(t *testing.T) {
	root := t.TempDir()
	writeTargets(t, root, "custom/tables", "custom/entities", "custom/queries")
	writeFixture(t, root, "custom/tables/a.yaml", `id: data.tables.a
table:
  entities: [ data.entities.e1 ]
  queries: []
`)
	writeFixture(t, root, "custom/entities/e1.yaml", `id: data.entities.e1
model:
  tables: [ data.tables.a ]
  queries: []
  included_by: []
`)
	writeFixture(t, root, "custom/queries/q.yaml", `id: data.queries.q
queries:
  - name: GetA
    returns:
      - { name: e1, kind: entity, entity: data.entities.e1 }
    tables: [ data.tables.a ]
    used_by: []
`)
	snap, err := Compute(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Tables["data.tables.a"].Queries; len(got) != 1 || got[0] != "data.queries.q.get_a" {
		t.Fatalf("custom table queries = %v", got)
	}
	if got := snap.Entities["data.entities.e1"].Queries; len(got) != 1 || got[0] != "data.queries.q.get_a" {
		t.Fatalf("custom entity queries = %v", got)
	}
}

func TestResolveTargetRejectsDuplicate(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "contracts/_meta/a.yaml", "type: data.tables\ntargets:\n  dir: contracts/data/tables\n  glob: \"*.yaml\"\n  id: \"data.tables.<file>\"\n")
	writeFixture(t, root, "contracts/_meta/b.yaml", "type: data.tables\ntargets:\n  dir: contracts/data/tables\n  glob: \"*.yaml\"\n  id: \"data.tables.<file>\"\n")
	if _, err := Compute(root); err == nil || !strings.Contains(err.Error(), "duplicate meta type") {
		t.Fatalf("duplicate meta not reported: %v", err)
	}
}

func TestComputeRejectsMissingTargetDir(t *testing.T) {
	root := t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, root, "contracts/data/entities", "contracts/data/queries")
	if _, err := Compute(root); err == nil || !strings.Contains(err.Error(), "missing data.tables target dir") {
		t.Fatalf("missing table target dir not reported: %v", err)
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
		t.Fatal("expected nested queries drift before write")
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
	if !strings.Contains(string(a), "queries:\n    - data.queries.q.get_a") {
		t.Fatalf("table a queries not written:\n%s", a)
	}
	b, _ := os.ReadFile(filepath.Join(root, "contracts/data/tables/b.yaml"))
	if !strings.Contains(string(b), "data.queries.q.get_b") {
		t.Fatalf("table b queries not written:\n%s", b)
	}
	e1, _ := os.ReadFile(filepath.Join(root, "contracts/data/entities/e1.yaml"))
	if !strings.Contains(string(e1), "queries:\n    - data.queries.q.get_a") {
		t.Fatalf("entity e1 queries not written:\n%s", e1)
	}
	q, _ := os.ReadFile(filepath.Join(root, "contracts/data/queries/q.yaml"))
	if !strings.Contains(string(q), "used_by:\n      - cap.services.x.get") {
		t.Fatalf("query used_by not written:\n%s", q)
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

func TestEmptyRoot(t *testing.T) {
	root := t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	if _, err := Compute(root); err != nil {
		t.Fatalf("empty tables dir should be valid: %v", err)
	}
	missing := t.TempDir()
	writeTargets(t, missing, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, missing, "contracts/data/entities", "contracts/data/queries")
	if _, err := Compute(missing); err == nil {
		t.Fatal("missing contracts/data/tables should fail")
	}
}

func TestDuplicateQueryIDs(t *testing.T) {
	root := t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	writeFixture(t, root, "contracts/data/queries/a.yaml", "queries:\n  - name: GetAccount\n    tables: [ data.tables.a ]\n  - name: GetACCOUNT\n    tables: [ data.tables.a ]\n")
	if _, err := Compute(root); err == nil || !strings.Contains(err.Error(), "duplicate query id") {
		t.Fatalf("duplicate derived query id not reported: %v", err)
	}
}

func TestMalformedYAML(t *testing.T) {
	root := t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	writeFixture(t, root, "contracts/data/tables/a.yaml", "id: [\n")
	if _, err := Compute(root); err == nil {
		t.Fatal("malformed YAML should fail")
	}
}

func TestApplyNestedListReplacesAndInserts(t *testing.T) {
	src := []byte("id: data.tables.a\ntable:\n  name: a\n  entities: []\n\ncolumns: []\n")
	out, err := applyNestedList(src, "table", "entities", "queries", []string{"data.queries.q.get_a"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "entities: []\n  queries:\n    - data.queries.q.get_a\n\ncolumns: []") {
		t.Fatalf("nested queries not inserted after entities:\n%s", text)
	}
	out, err = applyNestedList(out, "table", "entities", "queries", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "queries: []") {
		t.Fatalf("nested queries not replaced with empty list:\n%s", out)
	}
	if !strings.HasSuffix(string(out), "\n") {
		t.Fatalf("output must end with newline:\n%s", out)
	}
}
