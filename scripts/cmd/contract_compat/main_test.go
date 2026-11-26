package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareEnumsAppendOnly(t *testing.T) {
	before := enumSet{
		"account_status": {
			GoType: "AccountStatus", TSType: "AccountStatus",
			Values: map[string]int{"active": 1, "suspended": 2},
		},
		"token_status": {GoType: "TokenStatus", TSType: "TokenStatus", Values: map[string]int{"active": 1}},
	}
	after := enumSet{
		"account_status": {
			GoType: "AccountStatus", TSType: "AccountStatus",
			Values: map[string]int{"active": 1, "suspended": 2, "deleted": 3},
		},
		"token_status": {GoType: "TokenStatus", TSType: "TokenStatus", Values: map[string]int{"active": 1}},
	}
	if got := compareEnums(before, after); len(got) != 0 {
		t.Fatalf("append-only change reported: %v", got)
	}
}

func TestCompareEnumsRejectsRemovalAndCodeChange(t *testing.T) {
	before := enumSet{
		"account_status": {Values: map[string]int{"active": 1, "suspended": 2}},
		"token_status":   {Values: map[string]int{"active": 1}},
	}
	after := enumSet{
		"account_status": {Values: map[string]int{"active": 9}},
	}
	got := strings.Join(compareEnums(before, after), "\n")
	for _, want := range []string{
		"enum token_status was removed",
		"enum account_status value suspended was removed",
		"enum account_status value active code changed from 1 to 9",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in violations: %s", want, got)
		}
	}
}

func TestCompareEnumsRejectsTypeRename(t *testing.T) {
	before := enumSet{
		"account_status": {GoType: "AccountStatus", TSType: "AccountStatus", Values: map[string]int{"active": 1}},
	}
	after := enumSet{
		"account_status": {GoType: "AccountState", TSType: "AccountState", Values: map[string]int{"active": 1}},
	}
	got := strings.Join(compareEnums(before, after), "\n")
	for _, want := range []string{
		"enum account_status go_type changed from AccountStatus to AccountState",
		"enum account_status ts_type changed from AccountStatus to AccountState",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in violations: %s", want, got)
		}
	}
}

func TestMergeEnums(t *testing.T) {
	set := enumSet{}
	err := mergeEnums(set, []byte(`enums:
  - name: account_status
    go_type: AccountStatus
    ts_type: AccountStatus
    values:
      - { value: active, code: 1 }
      - { value: suspended, code: 2 }
`))
	if err != nil {
		t.Fatal(err)
	}
	if set["account_status"].Values["active"] != 1 || set["account_status"].Values["suspended"] != 2 {
		t.Fatalf("unexpected enum set: %#v", set)
	}
	if set["account_status"].GoType != "AccountStatus" || set["account_status"].TSType != "AccountStatus" {
		t.Fatalf("unexpected enum types: %#v", set["account_status"])
	}
}

func TestGitBaselineDetectsEnumChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "compat@example.com")
	runGit(t, root, "config", "user.name", "compat")
	runGit(t, root, "config", "commit.gpgsign", "false")
	dir := filepath.Join(root, "contracts/data/enums")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := `enums:
  - name: account_status
    go_type: AccountStatus
    ts_type: AccountStatus
    values:
      - { value: active, code: 1 }
      - { value: suspended, code: 2 }
`
	file := filepath.Join(dir, "account.yaml")
	if err := os.WriteFile(file, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "contracts/data/enums/account.yaml")
	runGit(t, root, "commit", "-m", "baseline")

	before, err := loadBaseline(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	current := `enums:
  - name: account_status
    go_type: AccountState
    ts_type: AccountState
    values:
      - { value: active, code: 1 }
`
	if err := os.WriteFile(file, []byte(current), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := loadCurrent(root)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(compareEnums(before, after), "\n")
	for _, want := range []string{
		"enum account_status value suspended was removed",
		"enum account_status go_type changed from AccountStatus to AccountState",
		"enum account_status ts_type changed from AccountStatus to AccountState",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in violations: %s", want, got)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLoadCurrentIndexUsesStagedContent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "compat@example.com")
	runGit(t, root, "config", "user.name", "compat")
	runGit(t, root, "config", "commit.gpgsign", "false")
	dir := filepath.Join(root, "contracts/data/enums")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := `enums:
  - name: account_status
    go_type: AccountStatus
    ts_type: AccountStatus
    values:
      - { value: active, code: 1 }
      - { value: suspended, code: 2 }
`
	file := filepath.Join(dir, "account.yaml")
	if err := os.WriteFile(file, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "contracts/data/enums/account.yaml")
	runGit(t, root, "commit", "-m", "baseline")

	staged := `enums:
  - name: account_status
    go_type: AccountStatus
    ts_type: AccountStatus
    values:
      - { value: active, code: 1 }
`
	if err := os.WriteFile(file, []byte(staged), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "contracts/data/enums/account.yaml")
	if err := os.WriteFile(file, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}

	before, err := loadBaseline(root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	index, err := loadCurrentIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(compareEnums(before, index), "\n"); !strings.Contains(got, "enum account_status value suspended was removed") {
		t.Fatalf("staged removal not detected: %s", got)
	}
	worktree, err := loadCurrent(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := compareEnums(before, worktree); len(got) != 0 {
		t.Fatalf("worktree should still be baseline, got %v", got)
	}
}
