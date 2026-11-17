package main

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	doc := errorFile{
		ID: "platform.errors",
		Errors: []errorEntry{
			{Code: "COMMON_BAD_REQUEST", Status: 400, I18n: "errors.common.bad_request", Message: "请求参数不合法"},
			{Code: "COMMON_AUDIT_FAILED", Status: 500, Retryable: true, I18n: "errors.common.audit_failed", Message: "审计写入失败"},
			{Code: "COMMON_PIPE", Status: 500, I18n: "errors.common.pipe", Message: "a|b"},
		},
	}
	out := render(doc.Errors, []string{"platform.errors.COMMON_INTERNAL"})
	for _, want := range []string{
		"`platform.errors`",
		"`COMMON_AUDIT_FAILED`",
		"`errors.common.audit_failed`",
		"a\\|b",
		"数据访问基线",
		"`platform.errors.COMMON_INTERNAL`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
}
