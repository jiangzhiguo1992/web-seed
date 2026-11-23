package main

import (
	"strings"
	"testing"
)

func TestRenderGo(t *testing.T) {
	out := renderGo([]errorEntry{
		{Code: "COMMON_BAD_REQUEST", BizCode: 1001, Status: 400},
		{Code: "AUTH_TOKEN_INVALID", BizCode: 3001, Status: 401, Retryable: true},
	})
	for _, want := range []string{
		"CommonBadRequest = 1001",
		"AuthTokenInvalid = 3001",
		`"platform.errors.COMMON_BAD_REQUEST": {BizCode: CommonBadRequest, Status: 400, Retryable: false}`,
		`"platform.errors.AUTH_TOKEN_INVALID": {BizCode: AuthTokenInvalid, Status: 401, Retryable: true}`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderGo missing %q:\n%s", want, out)
		}
	}
}

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
