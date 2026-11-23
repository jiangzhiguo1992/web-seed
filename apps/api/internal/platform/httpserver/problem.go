// 本文件定义契约统一错误信封（application/json）。
package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/yourorg/web-seed/apps/api/internal/generated"
)

// Problem 是 contracts/platform/http_conventions.yaml 的 ApiErrorResponse 实现。
//
// 错误信封固定为 {code, msg, data}；HTTP status 只写响应行，不进入 body。
// 业务错误码映射表见 contracts/platform/errors/。
type Problem struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Data    any    `json:"data"`
	Status  int    `json:"-"`

	// RetryAfter 只写响应头，不进入 body；格式为整数秒或 HTTP-date，非法值不写头并返回参数错误。
	RetryAfter string `json:"-"`
}

// writeProblem 输出 application/json 错误信封；handler 统一通过 ProblemWriter.Write 调用。
// 非法状态码回退为 500，避免 WriteHeader(0) panic。
// 429/503 且 RetryAfter 格式合法时写 Retry-After 响应头；该字段不进入 body。
// RetryAfter 非空但格式非法时仍写响应，但返回参数错误供调用方记录。
func writeProblem(w http.ResponseWriter, p Problem) error {
	if p.Status < 100 || p.Status > 599 {
		p.Status = http.StatusInternalServerError
	}
	if p.Code < 1000 || p.Code > 9999 {
		p.Code = generated.CommonInternal
	}
	if p.Message == "" {
		p.Message = http.StatusText(p.Status)
	}
	// 契约错误信封的 data 固定为 null；错误详情通过 msg 与 platform.errors 表达。
	p.Data = nil
	var retryErr error
	if p.RetryAfter != "" && (p.Status == http.StatusTooManyRequests || p.Status == http.StatusServiceUnavailable) {
		if validRetryAfter(p.RetryAfter) {
			w.Header().Set("Retry-After", p.RetryAfter)
		} else {
			retryErr = fmt.Errorf("invalid Retry-After %q", p.RetryAfter)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
	return retryErr
}

// validRetryAfter 接受 delay-seconds（非负整数）或 HTTP-date（RFC 7231）。
func validRetryAfter(v string) bool {
	if v == "" || strings.TrimSpace(v) != v {
		return false
	}
	digits := true
	for _, c := range v {
		if c < '0' || c > '9' {
			digits = false
			break
		}
	}
	if digits {
		return true
	}
	_, err := http.ParseTime(v)
	return err == nil
}
