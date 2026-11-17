package check

import (
	"strings"
	"testing"
)

func TestErrorFiles(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/platform/errors/_base.yaml", `schema_version: 1
id: platform.errors.base
summary: "x"
stability: stable
naming: {max_total: 5}
order: [common]
`)
	writeCapFile(t, root, "contracts/platform/errors/common.yaml", `id: platform.errors.common
summary: "x"
schema_version: 1
stability: stable
errors:
  - { code: COMMON_INTERNAL, status: 500, retryable: true, i18n: x, message: "x" }
  - { code: COMMON_INTERNAL, status: 500, retryable: true, i18n: x, message: "x" }
`)
	writeCapFile(t, root, "contracts/platform/errors/data_access.yaml", `id: platform.errors.data_access
summary: "x"
schema_version: 1
stability: draft
common_errors: [platform.errors.MISSING]
`)
	c := &Checker{root: root}
	c.checkErrorFiles()
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"duplicate error code COMMON_INTERNAL",
		"unknown common error platform.errors.MISSING",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
