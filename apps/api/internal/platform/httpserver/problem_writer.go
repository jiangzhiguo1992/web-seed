package httpserver

import (
	"context"
	"log/slog"
	"net/http"
)

type problemWriterKey struct{}

// ProblemWriter 集中写 RFC 9457 错误响应，并记录非法 Retry-After。
// handler 使用 ProblemWriterFrom(ctx).Write，而不是直接调用 WriteProblem。
type ProblemWriter struct {
	Log *slog.Logger
}

func (pw ProblemWriter) Write(w http.ResponseWriter, p Problem) {
	if err := writeProblem(w, p); err != nil && pw.Log != nil {
		pw.Log.Warn("invalid Retry-After", "error", err, "status", p.Status)
	}
}

// WithProblemWriter 把 ProblemWriter 注入请求上下文。
func WithProblemWriter(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), problemWriterKey{}, ProblemWriter{Log: log})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ProblemWriterFrom(ctx context.Context) ProblemWriter {
	if pw, ok := ctx.Value(problemWriterKey{}).(ProblemWriter); ok {
		return pw
	}
	return ProblemWriter{Log: slog.Default()}
}
