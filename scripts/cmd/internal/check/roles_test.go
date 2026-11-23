package check

import (
	"strings"
	"testing"
)

func TestRoles(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/security/permissions.yaml", `id: platform.security.permissions
summary: "x"
schema_version: 1
stability: draft
do_not: []
permissions: []
`)
	writeCapFile(t, root, "contracts/platform/security/roles.yaml", `id: platform.security.roles
summary: "x"
schema_version: 1
stability: draft
do_not: []
roles:
  - { code: admin, name: "x", description: "x", inherits: [], permissions: ["*"] }
  - { code: a, name: "x", description: "x", inherits: [platform.security.roles.b], permissions: [platform.security.permissions.missing] }
  - { code: b, name: "x", description: "x", inherits: [platform.security.roles.a], permissions: ["*"] }
constraints: { max_inheritance_depth: 1, cycle_forbidden: true, unknown_permission_forbidden: true }
default_role: platform.security.roles.missing
`)
	c := &Checker{root: root}
	c.checkRoles()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"cycle", "unknown permission", "only admin", "default_role", "[role-inheritance]", "[role-permissions]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
