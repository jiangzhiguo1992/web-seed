package check

import (
	"strings"
	"testing"
)

func TestCheckFeaturePages(t *testing.T) {
	root := t.TempDir()
	copyMeta(t, root, "cap.services")
	copyMeta(t, root, "cap.lifecycles")
	copyMeta(t, root, "feature.pages")
	writeCapFile(t, root, "contracts/capabilities/lifecycles/x.yaml", `id: cap.lifecycles.x
states:
  values: [active, deleted]
  initial: active
  final: [deleted]
create:
  requires: [ ]
  implemented_by: [cap.services.x.create.step]
destroy:
  requires: [ ]
  implemented_by: [cap.services.x.delete.step]
transitions:
  - { name: activate, description: "a", from: [active], to: deleted, requires: [ ], implemented_by: [cap.services.x.create.step] }
`)
	writeCapFile(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
services:
  - name: create
    params:
      - { name: ctx, type: context.Context }
      - { name: status, type: _types.basics.string }
  - name: delete
`)
	writeCapFile(t, root, "contracts/features/routes.yaml", `id: feature.routes
routes:
  - { id: feature.routes.x, page: feature.pages.x }
`)
	writeCapFile(t, root, "contracts/index.yaml", `schema_version: 1
entries:
  - { id: feature.routes, path: features/routes.yaml }
`)
	writeCapFile(t, root, "contracts/platform/security/permissions.yaml", `id: platform.security.permissions
permissions:
  - { code: account_read }
`)
	valid := `id: feature.pages.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
page: x
route: feature.routes.x
data:
  detail:
    capability: cap.services.x.create
    params:
      status: { from: query.status, enum: cap.lifecycles.x.states.values }
view_states:
  loading: { when: "true" }
  empty: { when: "false" }
  error: { when: "false" }
  forbidden: { when: "false" }
actions:
  delete:
    capability: cap.services.x.delete
    permission: platform.security.permissions.account_read
    available_when: { transition: cap.lifecycles.x.destroy, state_not: [deleted] }
fields:
  - name: q
    widget: string
    minLength: 1
    maxLength: 10
pagination:
  style: "offset"
  param: "offset"
  limit_default: 20
  limit_max: 100
transition_ui:
  source: cap.lifecycles.x.transitions
  rule: "rule"
`
	check := func(body string) string {
		writeCapFile(t, root, "contracts/features/pages/x.yaml", body)
		c := &Checker{root: root}
		c.checkFeaturePages()
		return strings.Join(c.errs, "\n")
	}
	if got := check(valid); got != "" {
		t.Fatalf("valid page reported: %s", got)
	}
	cases := []struct {
		name string
		body string
		want string
		rule string
	}{
		{"unknown transition", strings.Replace(valid, "cap.lifecycles.x.destroy", "cap.lifecycles.x.missing", 1), "unknown lifecycle action", "page-refs"},
		{"unknown transition source", strings.Replace(valid, "source: cap.lifecycles.x.transitions", "source: cap.lifecycles.missing.transitions", 1), "unknown lifecycle", "page-refs"},
		{"unknown state", strings.Replace(valid, "state_not: [deleted]", "state_not: [missing]", 1), "unknown state missing", "page-refs"},
		{"unknown capability", strings.Replace(valid, "capability: cap.services.x.create", "capability: cap.services.x.missing", 1), "unknown capability", "page-refs"},
		{"unknown permission", strings.Replace(valid, "platform.security.permissions.account_read", "platform.security.permissions.missing", 1), "unknown permission", "page-refs"},
		{"unknown enum reference", strings.Replace(valid, "enum: cap.lifecycles.x.states.values", "enum: garbage", 1), "must be a data.enums.* or cap.lifecycles.* reference", "page-refs"},
		{"unknown route", strings.Replace(valid, "route: feature.routes.x", "route: feature.routes.missing", 1), "unknown route", "page-refs"},
		{"missing forbidden state", strings.Replace(valid, "  forbidden: { when: \"false\" }\n", "", 1), "view_states missing forbidden", "page-structure"},
		{"page mismatch", strings.Replace(valid, "page: x", "page: y", 1), `page "y" must be "x"`, "page-structure"},
		{"id mismatch", strings.Replace(valid, "id: feature.pages.x", "id: feature.pages.y", 1), `id "feature.pages.y" must be "feature.pages.x"`, "page-structure"},
		{"top-level order", strings.Replace(valid, "id: feature.pages.x\nsummary: \"x\"", "summary: \"x\"\nid: feature.pages.x", 1), "must come before", "schema.field-order"},
		{"limits", strings.Replace(strings.Replace(valid, "limit_default: 20", "limit_default: 200", 1), "minLength: 1", "minLength: 20", 1), "must be <=", "page-limits"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := check(tc.body)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
			if !strings.Contains(got, "["+tc.rule+"]") {
				t.Fatalf("want rule [%s], got %q", tc.rule, got)
			}
		})
	}
}

func TestPageAnalyticsRefs(t *testing.T) {
	root := t.TempDir()
	copyMeta(t, root, "feature.pages")
	writeCapFile(t, root, "contracts/platform/analytics/events.yaml", `events:
  - { name: page.x_viewed, description: "x", trigger: mount, properties: [] }
`)
	writeCapFile(t, root, "contracts/features/pages/x.yaml", `id: feature.pages.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
page: x
route: null
view_states:
  loading: { when: "true" }
  empty: { when: "false" }
  error: { when: "false" }
  forbidden: { when: "false" }
analytics:
  page_view: page.missing_viewed
  actions:
    create: page.x_viewed
    delete: page.missing_clicked
`)
	c := &Checker{root: root}
	c.checkFeaturePages()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"unknown analytics event page.missing_viewed",
		"actions.delete references unknown event page.missing_clicked",
		"[page-refs]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "page.x_viewed") && strings.Contains(got, "unknown analytics event page.x_viewed") {
		t.Fatalf("declared event reported as unknown: %v", c.errs)
	}
}
