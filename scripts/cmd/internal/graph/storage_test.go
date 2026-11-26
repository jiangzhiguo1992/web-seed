package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncStorageDescriptions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, storageRel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `tables:
  postgres:
    - { table: data.tables.account, description: "旧描述" }
    - { table: account_archive, description: "归档说明", archive_of: data.tables.account }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	summaries := map[string]string{"data.tables.account": "账号主表，含逗号"}

	drift, err := storageDescriptionDrift(root, summaries)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || !strings.Contains(drift[0], "旧描述") {
		t.Fatalf("stale description not detected: %v", drift)
	}
	changed, err := syncStorageDescriptions(root, summaries)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("description change not written")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `description: "账号主表，含逗号"`) {
		t.Fatalf("normal entry description not updated:\n%s", got)
	}
	if !strings.Contains(string(got), `description: "归档说明"`) {
		t.Fatalf("archive description must stay manual:\n%s", got)
	}
	drift, err = storageDescriptionDrift(root, summaries)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Fatalf("description still drifts: %v", drift)
	}
}
