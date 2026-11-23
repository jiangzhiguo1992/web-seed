package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func TestNewRouter_Health(t *testing.T) {
	r := NewRouter(quietLog())

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("%s Content-Type = %q, want application/json", path, got)
		}
	}
}

func TestNewRouter_NotFound(t *testing.T) {
	r := NewRouter(quietLog())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func TestNewRouterWithOptionsRequestValidator(t *testing.T) {
	called := false
	r := NewRouterWithOptions(Options{
		Log: quietLog(),
		RequestValidator: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				next.ServeHTTP(w, r)
			})
		},
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !called {
		t.Fatal("request validator middleware was not invoked")
	}
}

func TestServerRequestIDIgnoresClientHeader(t *testing.T) {
	var got string
	h := serverRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = middleware.GetReqID(r.Context())
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Client-Trace", "client-trace-id")
	h.ServeHTTP(rec, req)

	if got == "" || got == "client-trace-id" {
		t.Fatalf("request id = %q, want server-generated id", got)
	}
	if rec.Header().Get("X-Client-Trace") != "" {
		t.Fatalf("response must not echo X-Client-Trace: %q", rec.Header().Get("X-Client-Trace"))
	}
}

func TestNewRouter_MethodNotAllowed(t *testing.T) {
	r := NewRouter(quietLog())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func TestRequestLoggerLogsPanic(t *testing.T) {
	log, buf := testLogger()
	handler := requestLogger(log)(recoverer(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "http request") || !strings.Contains(buf.String(), "status=500") {
		t.Fatalf("access log missing panic request: %s", buf.String())
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Addr: addr,
			Log:  quietLog(),
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		})
	}()

	waitForTCP(t, addr)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil on graceful shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after context cancel")
	}
}

func TestRun_ShutdownTimeout(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Addr:            addr,
			Log:             quietLog(),
			Handler:         slow,
			ShutdownTimeout: 20 * time.Millisecond,
		})
	}()

	waitForTCP(t, addr)
	go func() { _, _ = http.Get("http://" + addr + "/") }()
	time.Sleep(30 * time.Millisecond) // 让请求进入 handler
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run() error = nil, want shutdown timeout error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after shutdown timeout")
	}
}

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate port: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func waitForTCP(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 20*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("server at %s not ready", addr)
}
