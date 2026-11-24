package check

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func TestRoles(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/security/permissions.yaml", `id: platform.security.permissions
summary: "x"
schema_version: 1
stability: draft
do_not: []
permissions: []
boundaries:
  wildcard:
    allowed_for: [platform.security.roles.admin]
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
	for _, want := range []string{"cycle", "unknown permission", "uses wildcard", "default_role", "[role-inheritance]", "[role-permissions]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestPlatformMetaRejectsUnknownNestedField(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		typ, rel string
		mutate   func(map[string]any)
	}{
		{"platform.quality", "contracts/platform/quality.yaml", func(doc map[string]any) {
			coverage, _ := asMap(doc["coverage"])
			coverage["bogus"] = true
		}},
		{"platform.versioning", "contracts/platform/versioning.yaml", func(doc map[string]any) {
			api, _ := asMap(doc["api"])
			api["bogus"] = true
		}},
		{"platform.observability.log_fields", "contracts/platform/observability/log_fields.yaml", func(doc map[string]any) {
			fields := asList(doc["fields"])
			item, _ := asMap(fields[0])
			item["bogus"] = true
		}},
		{"platform.observability.metrics", "contracts/platform/observability/metrics.yaml", func(doc map[string]any) {
			metrics := asList(doc["metrics"])
			item, _ := asMap(metrics[0])
			item["bogus"] = true
		}},
		{"platform.architecture.go_arch", "contracts/platform/architecture/go_arch.yaml", func(doc map[string]any) {
			layers := asList(doc["layers"])
			item, _ := asMap(layers[0])
			item["bogus"] = true
		}},
		{"platform.analytics.events", "contracts/platform/analytics/events.yaml", func(doc map[string]any) {
			events := asList(doc["events"])
			item, _ := asMap(events[0])
			item["bogus"] = true
		}},
		{"platform.environments", "contracts/platform/environments.yaml", func(doc map[string]any) {
			envs := asList(doc["environments"])
			item, _ := asMap(envs[0])
			item["bogus"] = true
		}},
		{"platform.http_conventions", "contracts/platform/http_conventions.yaml", func(doc map[string]any) {
			components, _ := asMap(doc["components"])
			components["bogus"] = map[string]any{}
		}},
		{"platform.errors", "contracts/platform/errors/token.yaml", func(doc map[string]any) {
			errs := asList(doc["errors"])
			item, _ := asMap(errs[0])
			item["bogus"] = true
		}},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			schema, _, err := contractspec.LoadSchemaByType(root, tc.typ)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := loadYAML(filepath.Join(root, tc.rel))
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(doc)
			c := &Checker{root: root, valueTypes: schema.ValueTypes, nodeKinds: schema.NodeKinds, propertyKeys: schema.PropertyKeys}
			c.validateDoc(doc, schema.Fields, tc.rel)
			if !strings.Contains(strings.Join(c.errs, "\n"), "schema.unknown-field") {
				t.Fatalf("unknown nested field was accepted: %v", c.errs)
			}
		})
	}
}
