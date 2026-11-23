package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHTTPPolicy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/platform/http_conventions.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`audiences: [admin, user]
error_responses:
  always: [400, 500]
  secured: [401]
  body: [413]
  deprecated: [410]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadHTTPPolicy(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Audiences) != 2 || policy.Audiences[0] != "admin" {
		t.Fatalf("audiences = %v", policy.Audiences)
	}
	got := policy.EffectiveStatuses([]int{201}, true, true, false)
	want := []int{201, 400, 401, 413, 500}
	if len(got) != len(want) {
		t.Fatalf("statuses = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statuses = %v", got)
		}
	}
}
