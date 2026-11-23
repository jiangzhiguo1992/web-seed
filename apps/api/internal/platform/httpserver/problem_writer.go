package httpserver

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

type problemWriterKey struct{}

// ProblemWriter 集中写契约错误信封 {code,msg,data}，并记录非法 Retry-After。
// handler 使用 ProblemWriterFrom(ctx).Write，而不是直接调用 WriteProblem。
type ProblemWriter struct {
	Log       *slog.Logger
	RequestID string
}

func (pw ProblemWriter) Write(w http.ResponseWriter, p Problem) {
	if err := writeProblem(w, p); err != nil && pw.Log != nil {
		pw.Log.Warn("invalid Retry-After", "error", err, "status", p.Status, "request_id", pw.RequestID)
	}
}

// WithProblemWriter 把 ProblemWriter 注入请求上下文。
func WithProblemWriter(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), problemWriterKey{}, ProblemWriter{Log: log, RequestID: middleware.GetReqID(r.Context())})
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
