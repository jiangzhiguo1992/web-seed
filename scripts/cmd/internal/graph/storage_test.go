package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncStorageDescriptionsFormats(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "block mapping",
			body: `tables:
  postgres:
    - table: data.tables.account
      description: "旧"
      retention: true
`,
		},
		{
			name: "description before table",
			body: `tables:
  postgres:
    - description: "旧"
      table: data.tables.account
`,
		},
		{
			name: "multiline flow mapping",
			body: `tables:
  postgres:
    - {
        table: data.tables.account,
        description: "旧"
      }
`,
		},
		{
			name: "block scalar",
			body: `tables:
  postgres:
    - table: data.tables.account
      description: |
        旧
        第二行
      retention: true
`,
		},
		{
			name: "folded scalar",
			body: `tables:
  postgres:
    - table: data.tables.account
      description: >
        旧
        第二行
`,
		},
		{
			name: "multiline quoted scalar",
			body: `tables:
  postgres:
    - table: data.tables.account
      description: "旧
        第二行"
`,
		},
		{
			name: "escaped quotes",
			body: `tables:
  postgres:
    - { table: data.tables.account, description: "旧 \"引号\"" }
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, storageRel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			changed, err := syncStorageDescriptions(root, map[string]string{"data.tables.account": "新描述"})
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
			if !strings.Contains(string(got), `description: "新描述"`) {
				t.Fatalf("description not updated:\n%s", got)
			}
			if strings.Contains(string(got), "旧") || strings.Contains(string(got), "第二行") {
				t.Fatalf("old scalar content remains:\n%s", got)
			}
			if strings.Contains(tc.body, "retention: true") && !strings.Contains(string(got), "retention: true") {
				t.Fatalf("unrelated field removed:\n%s", got)
			}
		})
	}
}

func TestSyncStorageDescriptionsMissingDescription(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, storageRel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "tables:\n  postgres:\n    - table: data.tables.account\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := syncStorageDescriptions(root, map[string]string{"data.tables.account": "新描述"})
	if err == nil || !strings.Contains(err.Error(), "cannot locate description") {
		t.Fatalf("missing description must fail loudly: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("failed sync must not rewrite the file:\n%s", got)
	}
}

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
