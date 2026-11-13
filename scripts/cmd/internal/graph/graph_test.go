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

// writeTargets 为 graph.Compute 提供三份 _meta targets，避免测试依赖生产回退。
func writeTargets(t *testing.T, root, tableDir, entityDir, queryDir string) {
	t.Helper()
	writeFixture(t, root, "contracts/_meta/data.tables.yaml", fmt.Sprintf("type: data.tables\ntargets:\n  dir: %q\n  glob: \"*.yaml\"\n", tableDir))
	writeFixture(t, root, "contracts/_meta/data.entities.yaml", fmt.Sprintf("type: data.entities\ntargets:\n  dir: %q\n  glob: \"*.yaml\"\n", entityDir))
	writeFixture(t, root, "contracts/_meta/data.queries.yaml", fmt.Sprintf("type: data.queries\ntargets:\n  dir: %q\n  glob: \"*.yaml\"\n", queryDir))
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
used_by:
  queries: []
`)
	writeFixture(t, root, "contracts/data/tables/b.yaml", `# table b
id: data.tables.b
summary: "b"
table:
  comment: "b"
  extends: [ data.tables.base ]
  entities: []
used_by:
  queries: []
`)
	writeFixture(t, root, "contracts/data/entities/e1.yaml", `# entity e1
id: data.entities.e1
model:
  name: e1
  extends: [ data.entities.base ]
  tables: [ data.tables.a ]
  included_by: []
fields:
  - { name: E2, target: data.tables.b, type: data.entities.e2, join_column_from: data.tables.a.col.id, join_column_to: data.tables.b.col.id, description: "e2" }
used_by:
  queries: []
`)
	writeFixture(t, root, "contracts/data/entities/e2.yaml", `# entity e2
id: data.entities.e2
model:
  name: e2
  extends: [ data.entities.base ]
  tables: [ data.tables.b ]
  included_by: [ data.entities.e1 ]
used_by:
  queries: []
`)
	writeFixture(t, root, "contracts/data/queries/q.yaml", `id: data.queries.q
queries:
  - name: GetA
    returns:
      - { name: e1, kind: entity, entity: data.entities.e1 }
    tables: [ data.tables.a ]
  - name: GetB
    returns:
      - { name: exists, kind: scalar, type: boolean }
    tables: [ data.tables.b ]
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
interface:
  methods:
    - name: Get
      returns:
        - { name: e, type: data.entities.e1 }
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
	if got := snap.Entities["data.entities.e1"].Capabilities; len(got) != 1 || got[0] != "cap.services.x" {
		t.Fatalf("entity e1 capabilities = %v", got)
	}
}

func TestComputeUsesTargets(t *testing.T) {
	root := t.TempDir()
	writeTargets(t, root, "custom/tables", "custom/entities", "custom/queries")
	writeFixture(t, root, "custom/tables/a.yaml", `id: data.tables.a
table:
  entities: [ data.entities.e1 ]
used_by:
  queries: []
`)
	writeFixture(t, root, "custom/entities/e1.yaml", `id: data.entities.e1
model:
  tables: [ data.tables.a ]
  included_by: []
used_by:
  queries: []
`)
	writeFixture(t, root, "custom/queries/q.yaml", `id: data.queries.q
queries:
  - name: GetA
    tables: [ data.tables.a ]
    returns:
      - { name: e1, kind: entity, entity: data.entities.e1 }
`)
	snap, err := Compute(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Tables["data.tables.a"].Queries; len(got) != 1 || got[0] != "data.queries.q.get_a" {
		t.Fatalf("table edge not computed from custom targets: %v", got)
	}
	if got := snap.Entities["data.entities.e1"].Queries; len(got) != 1 || got[0] != "data.queries.q.get_a" {
		t.Fatalf("entity edge not computed from custom targets: %v", got)
	}
}

func TestComputeRejectsMissingMeta(t *testing.T) {
	root := t.TempDir()
	ensureDirs(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	if _, err := Compute(root); err == nil || !strings.Contains(err.Error(), "no meta target") {
		t.Fatalf("missing meta not reported: %v", err)
	}
}

func TestComputeRejectsMalformedMeta(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "contracts/_meta/data.tables.yaml", "type: [\n")
	if _, err := Compute(root); err == nil {
		t.Fatal("malformed meta should fail")
	}
}

func TestComputeRejectsDuplicateMeta(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "contracts/_meta/a.yaml", "type: data.tables\ntargets:\n  dir: contracts/data/tables\n  glob: \"*.yaml\"\n")
	writeFixture(t, root, "contracts/_meta/b.yaml", "type: data.tables\ntargets:\n  dir: contracts/data/tables\n  glob: \"*.yaml\"\n")
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
	b, _ := os.ReadFile(filepath.Join(root, "contracts/data/tables/b.yaml"))
	if !strings.Contains(string(b), "data.queries.q.get_b") {
		t.Fatalf("table b used_by not written:\n%s", b)
	}
	e1, _ := os.ReadFile(filepath.Join(root, "contracts/data/entities/e1.yaml"))
	if !strings.Contains(string(e1), "used_by:\n  queries:\n    - data.queries.q.get_a\n  capabilities:\n    - cap.services.x\n") {
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

func TestEmptyRoot(t *testing.T) {
	root := t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	// 只有空表目录：合法空快照。
	if _, err := Compute(root); err != nil {
		t.Fatalf("empty tables dir should be valid: %v", err)
	}
	// 缺表目录：必须报错，避免 -root 指错时静默成功。
	missing := t.TempDir()
	writeTargets(t, missing, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, missing, "contracts/data/entities", "contracts/data/queries")
	if _, err := Compute(missing); err == nil {
		t.Fatal("missing contracts/data/tables should fail")
	}
}

func TestDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	writeFixture(t, root, "contracts/data/tables/a.yaml", "id: data.tables.a\n")
	writeFixture(t, root, "contracts/data/tables/b.yaml", "id: data.tables.a\n")
	if _, err := Compute(root); err == nil || !strings.Contains(err.Error(), "duplicate table id") {
		t.Fatalf("duplicate table id not reported: %v", err)
	}

	root = t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	writeFixture(t, root, "contracts/data/entities/a.yaml", "id: data.entities.x\ntable: data.tables.a\n")
	writeFixture(t, root, "contracts/data/entities/b.yaml", "id: data.entities.x\ntable: data.tables.a\n")
	if _, err := Compute(root); err == nil || !strings.Contains(err.Error(), "duplicate entity id") {
		t.Fatalf("duplicate entity id not reported: %v", err)
	}

	root = t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	writeFixture(t, root, "contracts/data/queries/a.yaml", "id: data.queries.x\nqueries:\n  - name: Q\n    tables: [ data.tables.a ]\n")
	writeFixture(t, root, "contracts/data/queries/b.yaml", "id: data.queries.x\nqueries:\n  - name: Q\n    tables: [ data.tables.a ]\n")
	if _, err := Compute(root); err == nil || !strings.Contains(err.Error(), "duplicate query id") {
		t.Fatalf("duplicate query id not reported: %v", err)
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

func TestUsedByDropsLegacyEntities(t *testing.T) {
	root := t.TempDir()
	writeTargets(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	ensureDirs(t, root, "contracts/data/tables", "contracts/data/entities", "contracts/data/queries")
	writeFixture(t, root, "contracts/data/tables/a.yaml", `id: data.tables.a
used_by:
  entities: [ data.entities.e ]
  queries: []
`)
	snap, err := Compute(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snap.Write(root); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(filepath.Join(root, "contracts/data/tables/a.yaml"))
	if strings.Contains(string(src), "entities:") {
		t.Fatalf("legacy used_by.entities not removed:\n%s", src)
	}
	if !strings.Contains(string(src), "used_by:\n  queries: []\n") {
		t.Fatalf("used_by.queries not written:\n%s", src)
	}
}

func TestUsedByNoTrailingNewline(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "contracts/data/tables/a.yaml", "id: data.tables.a\nused_by:\n  queries: []")
	src, _ := os.ReadFile(filepath.Join(root, "contracts/data/tables/a.yaml"))
	out, err := applyUsedBy(src, renderUsedByQueries([]string{"data.queries.q.get_a"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "\n") {
		t.Fatalf("output must end with newline:\n%s", out)
	}
	if !strings.Contains(string(out), "data.queries.q.get_a") {
		t.Fatalf("used_by not updated:\n%s", out)
	}
}

func TestUsedByNotLast(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "contracts/data/tables/a.yaml", `id: data.tables.a
used_by:
  queries: []
after: 1
`)
	src, _ := os.ReadFile(filepath.Join(root, "contracts/data/tables/a.yaml"))
	out, err := applyUsedBy(src, renderUsedByQueries([]string{"data.queries.q.get_a"}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "after: 1") {
		t.Fatalf("following key lost:\n%s", text)
	}
	if !strings.Contains(text, "data.queries.q.get_a") {
		t.Fatalf("used_by not replaced:\n%s", text)
	}
}
