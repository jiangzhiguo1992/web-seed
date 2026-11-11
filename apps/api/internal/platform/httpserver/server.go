// Package httpserver 提供 HTTP 服务的地基：路由、中间件、错误信封、生命周期。
//
// 设计决策：
//   - 使用 chi：零反射、贴近 net/http，必要时可退化为标准库
//   - 错误响应统一 RFC 9457（application/problem+json）
//   - 健康检查分离：/healthz 不查依赖，/readyz 查依赖（P3 接入 DB）
//   - P2 起在路由层挂 OapiRequestValidator，handler 由契约生成
//
// 职责边界：
//   - 只负责传输层装配，不包含业务逻辑
//   - 不直接依赖 modules；模块通过 apps 层注入
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Options 是启动 HTTP 服务所需的参数。
type Options struct {
	Addr string
	Log  *slog.Logger

	// Handler 为空时使用 NewRouter(Log)。测试或自定义装配可直接注入。
	Handler http.Handler
	// ShutdownTimeout 为空或非正数时使用 10s。
	ShutdownTimeout time.Duration
}

// NewRouter 构造路由与中间件链。
//
// 中间件顺序即为执行顺序：RequestID → RealIP → Recoverer → 访问日志。
// P2 将在 Recoverer 之后插入契约请求校验中间件。
func NewRouter(log *slog.Logger) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// RealIP 信任 X-Forwarded-For / X-Real-IP，仅在可信反代之后启用。
	// 直接暴露公网时必须移除，否则客户端可伪造来源 IP。
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(log))

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
		handler = NewRouter(opts.Log)
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

// requestLogger 输出结构化访问日志，字段名与 contracts/observability/log-fields.yaml 对齐。
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
