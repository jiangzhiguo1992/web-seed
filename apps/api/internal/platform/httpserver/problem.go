// 本文件定义 RFC 9457 错误响应（application/problem+json）。
package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Problem 是 RFC 9457 错误响应的最小实现。
//
// 字段与 contracts/capabilities/http/shared.yaml 的 Problem 保持一致；
// code 为业务错误码，映射表见 contracts/platform/errors.yaml。
type Problem struct {
	Type   string  `json:"type"`
	Title  string  `json:"title"`
	Status int     `json:"status"`
	Detail string  `json:"detail,omitempty"`
	Code   string  `json:"code,omitempty"`
	Errors []Field `json:"errors,omitempty"`

	// RetryAfter 只写响应头，不进入 body；格式为整数秒或 HTTP-date，非法值不写头。
	RetryAfter string `json:"-"`
}

// Field 是字段级校验错误。
type Field struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// WriteProblem 输出 application/problem+json 响应。
// 非法状态码回退为 500，避免 WriteHeader(0) panic。
// 429/503 且 RetryAfter 格式合法时写 Retry-After 响应头；该字段不进入 body。
func WriteProblem(w http.ResponseWriter, p Problem) {
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Status < 100 || p.Status > 599 {
		p.Status = http.StatusInternalServerError
	}
	if p.RetryAfter != "" && (p.Status == http.StatusTooManyRequests || p.Status == http.StatusServiceUnavailable) && validRetryAfter(p.RetryAfter) {
		w.Header().Set("Retry-After", p.RetryAfter)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
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
