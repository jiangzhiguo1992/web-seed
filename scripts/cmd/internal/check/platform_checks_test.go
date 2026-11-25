package check

import (
	"path/filepath"
	"strings"
	"testing"
)

func authSecretDoc(name string) map[string]any {
	return map[string]any{"verification": map[string]any{"code": map[string]any{"secret_env": name}}}
}

func TestAuthSecretEnv(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/config/env.json", `{"properties":{"AUTH_X":{"secret":true},"PLAIN":{"secret":false}}}`)
	c := &Checker{root: root}
	c.checkAuthSecretEnv("x.yaml", authSecretDoc("AUTH_X"))
	if len(c.errs) != 0 {
		t.Fatalf("valid secret env rejected: %v", c.errs)
	}
	c.errs = nil
	c.checkAuthSecretEnv("x.yaml", authSecretDoc("PLAIN"))
	if !strings.Contains(strings.Join(c.errs, "\n"), "must set secret: true") {
		t.Fatalf("non-secret env not reported: %v", c.errs)
	}
	c.errs = nil
	c.checkAuthSecretEnv("x.yaml", authSecretDoc("MISSING"))
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not declared") {
		t.Fatalf("unknown env not reported: %v", c.errs)
	}
}

func TestAuthHashFieldSensitivity(t *testing.T) {
	c := &Checker{
		root:        t.TempDir(),
		tableOwners: map[string]string{"data.tables.x": "x"},
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {"h": {sensitivity: "public"}},
		},
	}
	doc := map[string]any{"password": map[string]any{"hash_field": "data.tables.x.col.h"}}
	c.checkAuthStorage("x.yaml", doc)
	if !strings.Contains(strings.Join(c.errs, "\n"), "must have sensitivity: secret") {
		t.Fatalf("public hash field not reported: %v", c.errs)
	}
	c.errs = nil
	c.tableColumns["data.tables.x"]["h"] = columnMeta{sensitivity: "pii"}
	c.checkAuthStorage("x.yaml", doc)
	if !strings.Contains(strings.Join(c.errs, "\n"), "must have sensitivity: secret") {
		t.Fatalf("pii hash field not reported: %v", c.errs)
	}
	c.errs = nil
	c.tableColumns["data.tables.x"]["h"] = columnMeta{sensitivity: "secret"}
	c.checkAuthStorage("x.yaml", doc)
	if len(c.errs) != 0 {
		t.Fatalf("secret hash field rejected: %v", c.errs)
	}
}

func TestQualityBaseURLSource(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/index.yaml", "entries:\n  - { id: platform.environments }\n")
	body := "e2e:\n  required_scenarios: []\n  base_url_source: platform.environments\ngates: []\nmust_test: []\n"
	writeCapFile(t, root, "contracts/platform/quality.yaml", body)
	c := &Checker{root: root}
	c.checkQuality()
	if len(c.errs) != 0 {
		t.Fatalf("valid base_url_source rejected: %v", c.errs)
	}
	writeCapFile(t, root, "contracts/platform/quality.yaml", strings.Replace(body, "platform.environments", "platform.missing", 1))
	c = &Checker{root: root}
	c.checkQuality()
	if !strings.Contains(strings.Join(c.errs, "\n"), "e2e.base_url_source references unknown contract") {
		t.Fatalf("unknown base_url_source not reported: %v", c.errs)
	}
}

func TestVersioningBaselineCommand(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "Makefile", "gen:\n\ttrue\ncontract-compat-baseline:\n\ttrue\n")
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", "mount: { api_version: v1 }\n")
	body := "api: { current: v1 }\ndetection:\n  command: make gen\n  baseline_command: make contract-compat-baseline\ncompatibility: {}\ndeprecation: {}\n"
	writeCapFile(t, root, "contracts/platform/versioning.yaml", body)
	c := &Checker{root: root}
	c.checkVersioning()
	if len(c.errs) != 0 {
		t.Fatalf("valid baseline_command rejected: %v", c.errs)
	}
	writeCapFile(t, root, "contracts/platform/versioning.yaml", strings.Replace(body, "make contract-compat-baseline", "make nope", 1))
	c = &Checker{root: root}
	c.checkVersioning()
	if !strings.Contains(strings.Join(c.errs, "\n"), "detection.baseline_command target nope is not in Makefile") {
		t.Fatalf("unknown baseline target not reported: %v", c.errs)
	}
}

func TestMetricsDuplicateLabel(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/observability/metrics.yaml", `label_rules: { forbidden_labels: [] }
metrics:
  - { name: x, kind: counter, labels: [a, a], description: "x" }
exporter: { default: none, options: [none] }
`)
	c := &Checker{root: root}
	c.checkMetrics()
	if !strings.Contains(strings.Join(c.errs, "\n"), "duplicate label a") {
		t.Fatalf("duplicate label not reported: %v", c.errs)
	}
}

func TestPageViewEvents(t *testing.T) {
	root := t.TempDir()
	meta := mustRead(t, filepath.Join("..", "..", "..", "..", "contracts", "_meta", "feature.pages.yaml"))
	writeCapFile(t, root, "contracts/_meta/feature.pages.yaml", string(meta))
	writeCapFile(t, root, "contracts/features/pages/x.yaml", `id: feature.pages.x
page: x
analytics:
  page_view: page.x_viewed
`)
	c := &Checker{root: root}
	if got := c.pageViewEvents()["feature.pages.x"]; got != "page.x_viewed" {
		t.Fatalf("page view event = %q", got)
	}
}

func TestAuthPolicyProjectAudience(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/project.yaml", `apps:
  admin: { kind: api, audience: admin, generated: { spec: generated/openapi/admin.yaml } }
`)
	writeCapFile(t, root, "contracts/platform/http_conventions.yaml", "audiences: [admin]\n")
	writeCapFile(t, root, "contracts/platform/security/auth_policy.yaml", `token:
  persistence: data.tables.missing
  issuer: ""
  access: { format: opaque, audience: [ghost], pii_in_token: true, claims_required: [] }
  refresh: { format: opaque, table: data.tables.missing, pii_in_token: true, claims_required: [] }
  revocation: { strategy: none }
password: { min_length: 12, hash_field: data.tables.missing.col.password_hash }
verification: { table: data.tables.missing }
mfa: { required_for_roles: [] }
`)
	c := &Checker{root: root}
	c.checkAuthPolicy()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not a project api app audience") {
		t.Fatalf("unknown project audience not reported: %v", c.errs)
	}
}

func TestVersioningDeprecationHeaderFormat(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "Makefile", "gen:\n\ttrue\n")
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", "mount: { api_version: v1 }\n")
	writeCapFile(t, root, "contracts/platform/versioning.yaml", `api: { current: v1 }
detection: { command: make gen }
compatibility: {}
deprecation:
  headers:
    - { name: Deprecation, format: RFC9745, example: nope }
    - { name: Sunset, format: HTTP-date, example: "Wed, 11 Mar 2027 00:00:00 GMT" }
    - { name: BadDate, format: HTTP-date, example: nope }
    - { name: Unknown, format: Foo, example: x }
`)
	c := &Checker{root: root}
	c.checkVersioning()
	got := strings.Join(c.errs, "\n")
	if !strings.Contains(got, "must be @<unix-seconds>") {
		t.Fatalf("bad RFC9745 example not reported: %v", c.errs)
	}
	if strings.Contains(got, "Sunset example") {
		t.Fatalf("valid HTTP-date rejected: %v", c.errs)
	}
	if !strings.Contains(got, "BadDate example must be HTTP-date") {
		t.Fatalf("bad HTTP-date not reported: %v", c.errs)
	}
	if !strings.Contains(got, `Unknown has unknown format "Foo"`) {
		t.Fatalf("unknown format not reported: %v", c.errs)
	}
}

func TestAnalyticsPageViewMismatch(t *testing.T) {
	root := t.TempDir()
	meta := mustRead(t, filepath.Join("..", "..", "..", "..", "contracts", "_meta", "feature.pages.yaml"))
	writeCapFile(t, root, "contracts/_meta/feature.pages.yaml", string(meta))
	writeCapFile(t, root, "contracts/features/pages/x.yaml", `id: feature.pages.x
page: x
analytics:
  page_view: page.other
`)
	writeCapFile(t, root, "contracts/platform/analytics/events.yaml", `events:
  - name: page.x_viewed
    trigger: mount
    page: feature.pages.x
    properties: []
`)
	c := &Checker{root: root}
	c.checkAnalytics()
	if !strings.Contains(strings.Join(c.errs, "\n"), "analytics.page_view must be page.x_viewed") {
		t.Fatalf("page view mismatch not reported: %v", c.errs)
	}
}

func TestMetricNaming(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/observability/metrics.yaml", `naming: { pattern: "x", unit_suffixes: [_total, _seconds] }
label_rules: { forbidden_labels: [] }
metrics:
  - { name: bad_counter, kind: counter, labels: [], description: "x" }
  - { name: bad_histogram, kind: histogram, labels: [], description: "x" }
  - { name: good_counter_total, kind: counter, labels: [], description: "x" }
  - { name: good_duration_seconds, kind: histogram, labels: [], description: "x" }
  - { name: bad_histogram_total, kind: histogram, labels: [], description: "x" }
exporter: { default: none, options: [none] }
`)
	c := &Checker{root: root}
	c.checkMetrics()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{"metrics[0]: counter name must end with _total", "metrics[1]: histogram name must end with a unit suffix", "metrics[4]: histogram name must end with a unit suffix"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %v", want, c.errs)
		}
	}
	if strings.Contains(got, "good_counter_total") || strings.Contains(got, "good_duration_seconds") {
		t.Fatalf("valid metric reported: %v", c.errs)
	}
}

func TestQualityCoverageAndMustTest(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "apps/api/.keep", "")
	writeCapFile(t, root, "apps/web/admin/.keep", "")
	writeCapFile(t, root, "contracts/index.yaml", "entries: []\n")
	writeCapFile(t, root, "contracts/platform/quality.yaml", `coverage:
  api: { line: 1, packages: [internal/missing] }
  web: { line: 1, packages: [src/missing] }
  exclude: []
must_test:
  - { id: required_missing, why: "x", where: apps/api/missing_test.go, status: required }
  - { id: planned_missing, why: "x", where: apps/api/planned_test.go, status: planned }
e2e: { tool: playwright, required_scenarios: [], base_url_source: platform.foo }
gates: []
`)
	c := &Checker{root: root}
	c.checkQuality()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"coverage.api.packages internal/missing does not exist",
		"coverage.web.packages src/missing does not exist",
		"must_test[0] where apps/api/missing_test.go does not match any file",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %v", want, c.errs)
		}
	}
	if strings.Contains(got, "planned_missing") {
		t.Fatalf("planned test reported: %v", c.errs)
	}
}

func TestAnalyticsPageViewMissing(t *testing.T) {
	root := t.TempDir()
	meta := mustRead(t, filepath.Join("..", "..", "..", "..", "contracts", "_meta", "feature.pages.yaml"))
	writeCapFile(t, root, "contracts/_meta/feature.pages.yaml", string(meta))
	writeCapFile(t, root, "contracts/features/pages/x.yaml", "id: feature.pages.x\npage: x\n")
	writeCapFile(t, root, "contracts/platform/analytics/events.yaml", `events:
  - name: page.x_viewed
    trigger: mount
    page: feature.pages.x
    properties: []
`)
	c := &Checker{root: root}
	c.checkAnalytics()
	if !strings.Contains(strings.Join(c.errs, "\n"), `analytics.page_view must be page.x_viewed, got ""`) {
		t.Fatalf("missing page view not reported: %v", c.errs)
	}
}
