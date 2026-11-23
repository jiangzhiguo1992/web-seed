package httpserver

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestProblemWriterLogsInvalidRetryAfter(t *testing.T) {
	log, buf := testLogger()
	rec := httptest.NewRecorder()

	ProblemWriter{Log: log}.Write(rec, Problem{Status: http.StatusServiceUnavailable, RetryAfter: "soon"})

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want empty", got)
	}
	if !strings.Contains(buf.String(), "invalid Retry-After") {
		t.Fatalf("log missing invalid Retry-After: %s", buf.String())
	}
}

func TestProblemWriterValidRetryAfterNoLog(t *testing.T) {
	log, buf := testLogger()
	rec := httptest.NewRecorder()

	ProblemWriter{Log: log}.Write(rec, Problem{Status: http.StatusServiceUnavailable, RetryAfter: "30"})

	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Fatalf("Retry-After = %q, want 30", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("unexpected log: %s", buf.String())
	}
}

func TestProblemWriterMiddlewareInjects(t *testing.T) {
	log, buf := testLogger()
	handler := WithProblemWriter(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ProblemWriterFrom(r.Context()).Write(w, Problem{Status: http.StatusServiceUnavailable, RetryAfter: "soon"})
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(buf.String(), "invalid Retry-After") {
		t.Fatalf("log missing invalid Retry-After: %s", buf.String())
	}
}

func TestProblemWriterLogsRequestID(t *testing.T) {
	log, buf := testLogger()
	handler := serverRequestID(WithProblemWriter(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ProblemWriterFrom(r.Context()).Write(w, Problem{Status: http.StatusServiceUnavailable, RetryAfter: "soon"})
	})))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), "request_id=") {
		t.Fatalf("log missing request_id: %s", buf.String())
	}
}

func TestProblemWriterFromMissing(t *testing.T) {
	rec := httptest.NewRecorder()
	ProblemWriterFrom(t.Context()).Write(rec, Problem{Status: http.StatusInternalServerError})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
