package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestErrorFilePlacement(t *testing.T) {
	cases := []struct {
		name, stem string
		doc        map[string]any
		want       string
	}{
		{"base errors", "_base", map[string]any{"errors": []any{}}, "_base.yaml must not declare errors"},
		{"base common", "_base", map[string]any{"common_errors": []any{}}, "_base.yaml must not declare common_errors"},
		{"domain naming", "token", map[string]any{"naming": map[string]any{}}, "naming is only allowed in _base.yaml"},
		{"domain common", "token", map[string]any{"common_errors": []any{}}, "common_errors is only allowed in data_access.yaml"},
		{"data_access errors", "data_access", map[string]any{"errors": []any{}}, "data_access.yaml must use common_errors, not errors"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Checker{}
			c.checkErrorFilePlacement(tc.stem+".yaml", tc.stem, tc.doc)
			if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "[error-file-placement]") || !strings.Contains(got, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, c.errs)
			}
		})
	}
	c := &Checker{}
	c.checkErrorFilePlacement("token.yaml", "token", map[string]any{"errors": []any{}})
	if len(c.errs) != 0 {
		t.Fatalf("valid domain file reported: %v", c.errs)
	}
}

func TestErrorFiles(t *testing.T) {
	root := t.TempDir()
	copyMeta(t, root, "platform.errors")
	writeCapFile(t, root, "contracts/platform/errors/_base.yaml", `id: platform.errors.base
summary: "x"
schema_version: 1
stability: stable
do_not: []
naming: {pattern: "<DOMAIN>_<REASON>", common_prefix: COMMON_, max_total: 5}
biz_code:
  success_code: 0
  ranges: {common: [1000, 1999]}
order: [common]
mapping_rules:
  - {status: 500, when: "x"}
`)
	writeCapFile(t, root, "contracts/platform/errors/common.yaml", `id: platform.errors.common
summary: "x"
schema_version: 1
stability: stable
do_not: []
errors:
  - { code: COMMON_INTERNAL, biz_code: 1001, status: 500, retryable: true, i18n: x, message: "x" }
  - { code: COMMON_INTERNAL, biz_code: 1001, status: 500, retryable: true, i18n: x, message: "x" }
  - { code: WRONG_INTERNAL, biz_code: 1002, status: 400, retryable: true, i18n: x, message: "x" }
`)
	writeCapFile(t, root, "contracts/platform/errors/data_access.yaml", `id: platform.errors.data_access
summary: "x"
schema_version: 1
stability: draft
do_not: []
common_errors: [platform.errors.MISSING]
`)
	c := &Checker{root: root}
	c.checkErrorFiles()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"duplicate \"COMMON_INTERNAL\"",
		"[error-naming]",
		"common error code must start with COMMON_",
		"status 400 is not in mapping_rules",
		"unknown common error platform.errors.MISSING",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestErrorI18nDerivation(t *testing.T) {
	c := &Checker{}
	c.checkErrorI18n("token.yaml", 0, map[string]any{
		"code": "AUTH_TOKEN_INVALID", "i18n": "errors.auth.wrong",
	})
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "[error-i18n-derivation]") || !strings.Contains(got, "errors.auth.token_invalid") {
		t.Fatalf("i18n mismatch not reported: %v", c.errs)
	}
	c.errs = nil
	c.checkErrorI18n("token.yaml", 0, map[string]any{
		"code": "AUTH_TOKEN_INVALID", "i18n": "errors.auth.token_invalid",
	})
	if len(c.errs) != 0 {
		t.Fatalf("valid i18n reported: %v", c.errs)
	}
}

func TestErrorLocale(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "Makefile", "x:\n\t@true\n")
	writeCapFile(t, root, "contracts/project.yaml", "defaults: { locale: zh-CN }\n")
	writeCapFile(t, root, "apps/web/admin/src/shared/locales/zh-CN.json", `{"errors":{"auth":{"token_invalid":"wrong","extra":"y"}}}`)
	c := &Checker{root: root}
	c.checkErrorLocales(map[string]string{
		"errors.auth.token_invalid": "x",
		"errors.auth.missing":       "m",
	})
	got := strings.Join(c.errs, "\n")
	mustContainRule(t, c.errs, "error-i18n-locale")
	for _, want := range []string{
		"missing i18n key errors.auth.missing",
		"i18n message mismatch for errors.auth.token_invalid",
		"unknown i18n key errors.auth.extra",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}

	c.errs = nil
	os.Remove(filepath.Join(root, "apps/web/admin/src/shared/locales/zh-CN.json"))
	c.checkErrorLocales(map[string]string{"errors.auth.token_invalid": "x"})
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "locale catalog missing") {
		t.Fatalf("missing catalog not reported: %v", c.errs)
	}
}

func TestErrorLocalesMulti(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "Makefile", "x:\n\t@true\n")
	writeCapFile(t, root, "contracts/project.yaml", "defaults: { locale: zh-CN, locales: [zh-CN, en] }\n")
	writeCapFile(t, root, "apps/web/admin/src/shared/locales/zh-CN.json", `{"errors":{"auth":{"token_invalid":"x"}}}`)
	writeCapFile(t, root, "apps/web/admin/src/shared/locales/en.json", `{"errors":{"auth":{"token_invalid":"Invalid token"}}}`)
	c := &Checker{root: root}
	c.checkErrorLocales(map[string]string{"errors.auth.token_invalid": "x"})
	if len(c.errs) != 0 {
		t.Fatalf("multi locale valid reported: %v", c.errs)
	}

	writeCapFile(t, root, "apps/web/admin/src/shared/locales/en.json", `{"errors":{"auth":{}}}`)
	c.errs = nil
	c.checkErrorLocales(map[string]string{"errors.auth.token_invalid": "x"})
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "en.json: missing i18n key errors.auth.token_invalid") {
		t.Fatalf("en missing key not reported: %v", c.errs)
	}
}
