// 本文件定义 RFC 9457 错误响应（application/problem+json）。
package httpserver

import (
	"encoding/json"
	"net/http"
)

// Problem 是 RFC 9457 错误响应的最小实现。
//
// 字段与 contracts/openapi/shared/problem.yaml 保持一致；
// code 为业务错误码，映射表见 contracts/domain/error-codes.yaml。
type Problem struct {
	Type   string  `json:"type"`
	Title  string  `json:"title"`
	Status int     `json:"status"`
	Detail string  `json:"detail,omitempty"`
	Code   string  `json:"code,omitempty"`
	Errors []Field `json:"errors,omitempty"`
}

// Field 是字段级校验错误。
type Field struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// WriteProblem 输出 application/problem+json 响应。
// 非法状态码回退为 500，避免 WriteHeader(0) panic。
func WriteProblem(w http.ResponseWriter, p Problem) {
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Status < 100 || p.Status > 599 {
		p.Status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
