package check

import (
	"strings"
	"testing"
)

func TestFeatureRoutes(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/features/pages/account_list.yaml", `id: feature.pages.account_list
summary: "x"
schema_version: 1
stability: draft
do_not: []
page: account_list
route: null
`)
	writeCapFile(t, root, "contracts/features/routes.yaml", `id: feature.routes
summary: "x"
schema_version: 1
stability: draft
do_not: []
app: web/admin
layout: { default: default, auth: blank }
routes:
  - id: feature.routes.account_list
    path: /account
    name: account-list
    page: feature.pages.account_list
    layout: default
    permission: platform.security.permissions.missing
    menu: null
redirects:
  - { from: /, to: /account }
not_found: { page: feature.pages.missing, layout: default }
`)
	copyMeta(t, root, "feature.routes")
	copyMeta(t, root, "feature.pages")
	writeCapFile(t, root, "contracts/platform/security/permissions.yaml", `id: platform.security.permissions
summary: "x"
schema_version: 1
stability: draft
do_not: []
permissions: []
`)
	c := &Checker{root: root}
	c.checkFeatureRoutes()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"unknown permission", "unknown not_found page", "[route-structure]", "[route-redirect]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestPageActionTarget(t *testing.T) {
	c := &Checker{}
	routes := map[string]bool{"feature.routes.account_list": true}
	c.checkPageActionTarget("x.yaml", "navigate(feature.flows.account_create)", routes)
	c.checkPageActionTarget("x.yaml", "navigate(feature.routes.missing)", routes)
	got := strings.Join(c.errs, "\n")
	if strings.Count(got, "[page-action-target]") != 2 {
		t.Fatalf("page action target errors = %v", c.errs)
	}
}

func TestPageParams(t *testing.T) {
	c := &Checker{pageParamTypes: map[string]map[string]string{
		"cap.services.x.get": {"id": "_types.integer"},
	}}
	c.checkPageParams("x.yaml", "cap.services.x.get", map[string]any{
		"params": map[string]any{
			"bad": map[string]any{"type": "_types.string"},
			"id":  map[string]any{"type": "_types.string"},
		},
	})
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"is not a parameter", "type _types.string does not match", "[page-params]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestServicePageSchema(t *testing.T) {
	c := &Checker{}
	c.checkPageSchemas(map[string]any{
		"BadPage": map[string]any{"properties": []any{}},
	}, "x.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-page-schema]") {
		t.Fatalf("page schema not reported: %v", c.errs)
	}
}

func TestEnvironmentShape(t *testing.T) {
	c := &Checker{}
	c.checkEnvironmentShape("x.yaml", map[string]any{
		"environments": []any{
			map[string]any{"name": "dev"},
			map[string]any{"name": "dev"},
		},
		"differences": []any{map[string]any{"key": ""}},
	})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[environment-shape]") {
		t.Fatalf("environment shape not reported: %v", c.errs)
	}
}

func TestQualityRefs(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/quality.yaml", `schema_version: 1
id: platform.quality
summary: "x"
stability: draft
do_not: []
coverage: {}
must_test:
  - { id: x }
  - { id: x }
e2e:
  required_scenarios: [scenario.http.x.missing]
forbidden: [x]
gates:
  - { name: x, command: "make missing-target", blocking: true }
`)
	writeCapFile(t, root, "Makefile", "present:\n\ttrue\n")
	c := &Checker{root: root}
	c.checkQuality()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"unknown scenario.http.x.missing", "missing-target", "duplicate id", "[quality-refs]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestAnalyticsRefs(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/analytics/events.yaml", `schema_version: 1
id: platform.analytics.events
summary: "x"
stability: draft
do_not: []
naming: { pattern: "x", property_case: snake_case, id_policy: x }
common_properties: []
events:
  - name: BadName
    description: "x"
    trigger: api_success
    ref: cap.protocols.http.x.missing
    properties:
      - { name: email, type: _types.string, pii: false }
forbidden_properties: [email]
provider: { default: noop, options: [noop] }
`)
	c := &Checker{root: root}
	c.checkAnalytics()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"must match", "not an HTTP operation", "forbidden", "[analytics-refs]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestVersioningRefs(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/project.yaml", `apps:
  admin: { kind: api, api_version: v1, audience: admin, timeout_ms: 5000, includes: [] }
`)
	writeCapFile(t, root, "contracts/platform/http_conventions.yaml", `headers: { response: [ { name: Deprecation } ] }
`)
	writeCapFile(t, root, "Makefile", "present:\n\ttrue\n")
	writeCapFile(t, root, "contracts/platform/versioning.yaml", `schema_version: 1
id: platform.versioning
summary: "x"
stability: draft
do_not: []
api: { current: v9 }
contract: {}
compatibility: {}
deprecation: { headers: [ { name: Sunset } ] }
detection: { command: "make missing" }
client_requirements: [x]
`)
	c := &Checker{root: root}
	c.checkVersioning()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"api.current v9", "target missing", "header Sunset", "[versioning-refs]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestObservabilityRefs(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/observability/log_fields.yaml", `fields:
  - { name: x, required_for: [bogus] }
  - { name: x }
`)
	writeCapFile(t, root, "contracts/platform/observability/metrics.yaml", `label_rules: { forbidden_labels: [user_id] }
metrics:
  - { name: x, kind: gauge, labels: [user_id] }
  - { name: x, kind: bogus }
`)
	c := &Checker{root: root}
	c.checkLogFields()
	c.checkMetrics()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"[log-field-refs]", "[metric-shape]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestArchitectureRefs(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/modules.yaml", `modules: [ { name: account, summary: "x" } ]
`)
	writeCapFile(t, root, "contracts/platform/architecture/go_arch.yaml", `layers:
  - { name: modules, in: internal/modules/** }
rules:
  modules: { may_depend_on: [missing] }
module_dependencies: { ghost: [missing] }
forbidden_imports:
  - { path: x, in: [missing] }
`)
	c := &Checker{root: root}
	c.checkArchitecture()
	got := strings.Join(c.errs, "\n")
	if strings.Count(got, "[architecture-refs]") < 3 {
		t.Fatalf("architecture refs not reported: %v", c.errs)
	}
}

func TestAuthPolicyRefs(t *testing.T) {
	root := t.TempDir()
	copyMeta(t, root, "feature.pages")
	writeCapFile(t, root, "contracts/features/pages/login.yaml", `id: feature.pages.login
summary: "x"
schema_version: 1
stability: draft
do_not: []
page: login
route: null
fields:
  - { name: password, widget: string, minLength: 6 }
`)
	writeCapFile(t, root, "contracts/platform/http_conventions.yaml", `audiences: [admin]
`)
	writeCapFile(t, root, "contracts/platform/security/auth_policy.yaml", `csrf:
  exempt_paths: [/v1/auth/missing]
token: { access: { audience: [ghost] } }
password: { min_length: 12 }
`)
	c := &Checker{root: root, httpOps: map[string]httpOpInfo{
		"cap.protocols.http.auth.login": {Path: "/auth/login"},
	}}
	c.checkAuthPolicy()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"[auth-policy-paths]", "[auth-policy-audience]", "[auth-policy-password]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestConfigEnvValid(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/config/env.json", `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "additionalProperties": false,
  "required": ["APP_ENV", "HTTP_ADDR"],
  "properties": {
    "APP_ENV": { "type": "string", "enum": ["dev"] },
    "HTTP_ADDR": { "type": "string" },
    "LOG_LEVEL": { "type": "string", "enum": ["info"] }
  }
}`)
	writeCapFile(t, root, "contracts/platform/environments.yaml", `environments: [ { name: dev } ]
`)
	writeCapFile(t, root, "contracts/platform/observability/log_fields.yaml", `required: [ { name: level, enum: [info] } ]
`)
	c := &Checker{root: root}
	c.checkConfigEnv()
	if len(c.errs) != 0 {
		t.Fatalf("valid config env reported: %v", c.errs)
	}
}

func TestWebDepsValid(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/architecture/web_deps.json", `{
  "layers": [ { "name": "app", "in": "src/app/**" }, { "name": "shared", "in": "src/shared/**" } ],
  "rules": [ { "from": "app", "mayDependOn": ["shared"] } ],
  "generates": [".dependency-cruiser.js"],
  "forbiddenModules": [ { "name": "no-fetch" } ]
}`)
	c := &Checker{root: root}
	c.checkWebDeps()
	if len(c.errs) != 0 {
		t.Fatalf("valid web deps reported: %v", c.errs)
	}
}

func TestConfigEnvInvalid(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/config/env.json", `{
  "type": "object",
  "properties": { "APP_ENV": { "type": "string", "enum": ["ghost"] } }
}`)
	writeCapFile(t, root, "contracts/platform/environments.yaml", `environments: [ { name: dev } ]
`)
	c := &Checker{root: root}
	c.checkConfigEnv()
	if !strings.Contains(strings.Join(c.errs, "\n"), "APP_ENV enum ghost") {
		t.Fatalf("invalid config env not reported: %v", c.errs)
	}
}

func TestWebDepsInvalid(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/architecture/web_deps.json", `{
  "layers": [ { "name": "app", "in": "src/app/**" } ],
  "rules": [ { "from": "app", "mayDependOn": ["missing"], "forbid": ["missing"] } ],
  "generates": ["x"],
  "forbiddenModules": [ { "name": "x" }, { "name": "x" } ]
}`)
	c := &Checker{root: root}
	c.checkWebDeps()
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, "mayDependOn missing") || !strings.Contains(got, "duplicate x") {
		t.Fatalf("invalid web deps not reported: %v", c.errs)
	}
}

func TestAnalyticsExtra(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/analytics/events.yaml", `schema_version: 1
id: platform.analytics.events
summary: "x"
stability: draft
do_not: []
naming: { pattern: "x", property_case: snake_case, id_policy: x }
common_properties:
  - { name: email, type: _types.string, pii: false }
  - { name: email, type: _types.string, pii: false }
events:
  - { name: page.x_viewed, description: "x", trigger: mount, properties: [] }
  - { name: auth.login_succeeded, description: "x", trigger: api_success, properties: [ { name: x, type: _types.string, pii: false }, { name: x, type: _types.string, pii: false } ] }
forbidden_properties: [email]
provider: { default: noop, options: [noop] }
`)
	c := &Checker{root: root}
	c.checkAnalytics()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"common property email is forbidden", "requires page", "requires ref", "duplicate common property", "duplicate property x"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestAnalyticsTypes(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/_meta/_types.yaml", `basic_types:
  string: { description: "x" }
  integer: { description: "x" }
`)
	writeCapFile(t, root, "contracts/data/types.yaml", `custom_types:
  account_number: { family: string, description: "x" }
`)
	writeCapFile(t, root, "contracts/data/enums/account.yaml", `enums:
  - name: account_status
    values: [ { value: active, code: 1 } ]
`)
	writeCapFile(t, root, "contracts/platform/analytics/events.yaml", `schema_version: 1
id: platform.analytics.events
summary: "x"
stability: draft
do_not: []
naming: { pattern: "x", property_case: snake_case, id_policy: x }
common_properties: []
events:
  - name: auth.login_clicked
    description: "x"
    trigger: click
    properties:
      - { name: a, type: _types.string, pii: false }
      - { name: b, type: data.types.account_number, pii: false }
      - { name: c, type: data.enums.account_status, pii: false }
      - { name: d, type: _types.ghost, pii: false }
      - { name: e, type: data.types.ghost, pii: false }
forbidden_properties: []
provider: { default: noop, options: [noop] }
`)
	c := &Checker{root: root}
	c.checkAnalytics()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"_types.ghost", "data.types.ghost"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Count(got, "[analytics-types]") != 2 {
		t.Fatalf("analytics type errors = %v", c.errs)
	}
}

func TestAnalyticsAnyJSON(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/_meta/_types.yaml", `basic_types:
  string: { description: "x" }
  any: { description: "x" }
  json: { description: "x" }
`)
	writeCapFile(t, root, "contracts/platform/analytics/events.yaml", `schema_version: 1
id: platform.analytics.events
summary: "x"
stability: draft
do_not: []
naming: { pattern: "x", property_case: snake_case, id_policy: x }
common_properties: []
events:
  - name: auth.login_clicked
    description: "x"
    trigger: click
    properties:
      - { name: a, type: _types.any, pii: false }
      - { name: b, type: _types.json, pii: false }
forbidden_properties: []
provider: { default: noop, options: [noop] }
`)
	c := &Checker{root: root}
	c.checkAnalytics()
	got := strings.Join(c.errs, "\n")
	if strings.Count(got, "[analytics-types]") != 2 {
		t.Fatalf("any/json not rejected: %v", c.errs)
	}
}

func TestAnalyticsPII(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/_meta/_types.yaml", `basic_types:
  string: { description: "x" }
`)
	writeCapFile(t, root, "contracts/platform/analytics/events.yaml", `schema_version: 1
id: platform.analytics.events
summary: "x"
stability: draft
do_not: []
naming: { pattern: "x", property_case: snake_case, id_policy: x }
common_properties:
  - { name: email, type: _types.string, pii: true }
events:
  - name: auth.login_clicked
    description: "x"
    trigger: click
    properties:
      - { name: email, type: _types.string, pii: false }
forbidden_properties: [email, "Email Address"]
provider: { default: noop, options: [noop] }
`)
	c := &Checker{root: root}
	c.checkAnalytics()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"cannot be pii: true", "pii false conflicts", "must be snake_case"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "[analytics-pii]") {
		t.Fatalf("missing analytics-pii rule: %v", c.errs)
	}
}

func TestLogFieldsNeverLog(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/observability/log_fields.yaml", `fields: []
never_log: [password, "*_hash", "Email Address"]
`)
	writeCapFile(t, root, "contracts/data/tables/x.yaml", `id: data.tables.x
columns:
  - { name: request_ip, sensitivity: pii }
  - { name: password_hash, sensitivity: secret }
`)
	c := &Checker{root: root}
	c.checkLogFields()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"request_ip", "not covered", "must be snake_case"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestAnalyticsNeverLog(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/_meta/_types.yaml", `basic_types:
  string: { description: "x" }
`)
	writeCapFile(t, root, "contracts/platform/observability/log_fields.yaml", `never_log: [secret_code]
`)
	writeCapFile(t, root, "contracts/platform/analytics/events.yaml", `schema_version: 1
id: platform.analytics.events
summary: "x"
stability: draft
do_not: []
naming: { pattern: "x", property_case: snake_case, id_policy: x }
common_properties:
  - { name: secret_code, type: _types.string, pii: true }
events:
  - name: auth.login_clicked
    description: "x"
    trigger: click
    properties:
      - { name: other_pii, type: _types.string, pii: true }
      - { name: secret_code, type: _types.string, pii: false }
forbidden_properties: []
provider: { default: noop, options: [noop] }
`)
	c := &Checker{root: root}
	c.checkAnalytics()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"other_pii must be listed", "secret_code is in log_fields.never_log"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestNeverLogPatterns(t *testing.T) {
	patterns := []string{"*_body", "*_hash", "*_token", "*_password", "*_email", "*_phone", "email"}
	cases := map[string]bool{
		"has_body":       true,
		"body_count":     false,
		"password_hash":  true,
		"access_token":   true,
		"token_type":     false,
		"old_password":   true,
		"contact_email":  true,
		"email_verified": false,
		"primary_phone":  true,
		"phone_code":     false,
	}
	for name, want := range cases {
		if got := matchesNeverLog(name, patterns); got != want {
			t.Fatalf("matchesNeverLog(%q) = %v, want %v", name, got, want)
		}
	}
}
