package check

import (
	"strings"
	"testing"
)

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
		"unknown common error platform.errors.MISSING",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
