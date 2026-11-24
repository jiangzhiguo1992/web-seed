package check

import (
	"strings"
	"testing"
)

func TestPermissionCodeShape(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/security/permissions.yaml", `naming:
  pattern: "<resource>_<action>"
  actions: [read, write]
permissions:
  - { code: account_read, resource: account, action: read }
  - { code: account, resource: account, action: read }
  - { code: role_manage, resource: role, action: manage }
`)
	c := &Checker{root: root}
	c.checkPermissions()
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, "[permission-code-shape]") {
		t.Fatalf("missing rule id: %v", c.errs)
	}
	if !strings.Contains(got, "code account must be account_read") {
		t.Fatalf("code/resource/action mismatch not reported: %v", c.errs)
	}
	if !strings.Contains(got, "action manage is not in naming.actions") {
		t.Fatalf("unknown action not reported: %v", c.errs)
	}
}

func TestPermissionPatternShape(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/security/permissions.yaml", `naming:
  pattern: "<resource>.<action>"
  actions: [read]
permissions:
  - { code: account_read, resource: account, action: read }
`)
	c := &Checker{root: root}
	c.checkPermissions()
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "naming.pattern must be <resource>_<action>") {
		t.Fatalf("pattern mismatch not reported: %v", c.errs)
	}
}
