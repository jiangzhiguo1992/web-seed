// Package httpserver 提供 HTTP 服务的地基：路由、中间件、错误信封、生命周期。
//
// 设计决策：
//   - 使用 chi：零反射、贴近 net/http，必要时可退化为标准库
//   - 错误响应统一契约信封 {code,msg,data}（application/json）
//   - 健康检查分离：/healthz 不查依赖，/readyz 查依赖（P3 接入 DB）
//   - P2 起在路由层挂 OapiRequestValidator，handler 由契约生成
//
// 职责边界：
//   - 只负责传输层装配，不包含业务逻辑
//   - 不直接依赖 modules；模块通过 apps 层注入
package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/yourorg/web-seed/apps/api/internal/generated"
)

// Options 是启动 HTTP 服务所需的参数。
type Options struct {
	Addr string
	Log  *slog.Logger

	// Handler 为空时使用 NewRouter(Log)。测试或自定义装配可直接注入。
	Handler http.Handler
	// RequestValidator 由 composition 注入 oapi request validator；为空时跳过。
	RequestValidator func(http.Handler) http.Handler
	// ShutdownTimeout 为空或非正数时使用 10s。
	ShutdownTimeout time.Duration
}

// NewRouter 构造路由与中间件链。
func NewRouter(log *slog.Logger) chi.Router {
	return NewRouterWithOptions(Options{Log: log})
}

// NewRouterWithOptions 构造路由与中间件链。
//
// 中间件顺序即为执行顺序：请求 ID（服务端生成）→ ProblemWriter → RealIP → 访问日志 → Recoverer → 请求校验。
// RequestValidator 由 composition 注入生成的 OpenAPI 校验器；为空时跳过。
func NewRouterWithOptions(opts Options) chi.Router {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	r := chi.NewRouter()
	r.Use(serverRequestID)
	r.Use(WithProblemWriter(log))
	// RealIP 信任 X-Forwarded-For / X-Real-IP，仅在可信反代之后启用。
	// 直接暴露公网时必须移除，否则客户端可伪造来源 IP。
	r.Use(middleware.RealIP)
	r.Use(requestLogger(log))
	r.Use(recoverer(log))
	if opts.RequestValidator != nil {
		r.Use(opts.RequestValidator)
	}
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		ProblemWriterFrom(r.Context()).Write(w, Problem{Code: generated.CommonNotFound, Message: "资源不存在", Status: http.StatusNotFound})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		ProblemWriterFrom(r.Context()).Write(w, Problem{Code: generated.CommonMethodNotAllowed, Message: "方法不允许", Status: http.StatusMethodNotAllowed})
	})

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, `{"status":"ok"}`)
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// P3 接入数据库探活后，此处改为真实检查。
		writeJSON(w, http.StatusOK, `{"status":"ready"}`)
	})

	return r
}

// Run 启动服务并阻塞，直到 ctx 取消或服务异常。
//
// 关闭流程：停止接收新请求 → 等待在途请求（最多 10s）→ 返回。
func Run(ctx context.Context, opts Options) error {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	handler := opts.Handler
	if handler == nil {
		handler = NewRouterWithOptions(opts)
	}
	shutdownTimeout := opts.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = 10 * time.Second
	}

	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		opts.Log.Info("http server listening", "addr", opts.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("httpserver: shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("httpserver: listen: %w", err)
	}
}

// serverRequestID 为每个请求生成服务端 request id，写入 context 供日志与下游调用使用。
// 不读取、不回写客户端请求头；该 ID 只用于服务端排错，客户端不参与。
func serverRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, newRequestID())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// recoverer 把 panic 统一写成契约错误信封，而不是 chi 默认的空 500。
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("http panic", "error", rec, "stack", string(debug.Stack()), "request_id", middleware.GetReqID(r.Context()))
					ProblemWriterFrom(r.Context()).Write(w, Problem{Code: generated.CommonInternal, Message: "服务端错误", Status: http.StatusInternalServerError})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// requestLogger 输出结构化访问日志，字段名与 contracts/platform/observability/log-fields.yaml 对齐。
// 它包在 recoverer 外层，panic 被 recoverer 接住后仍会输出访问日志。
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			log.Info("http request",
				"request_id", middleware.GetReqID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
