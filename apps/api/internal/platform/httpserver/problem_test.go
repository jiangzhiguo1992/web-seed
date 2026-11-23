package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourorg/web-seed/apps/api/internal/generated"
)

func TestWriteProblem(t *testing.T) {
	tests := []struct {
		name        string
		in          Problem
		wantStatus  int
		wantCode    int
		wantMessage string
	}{
		{
			name:        "normal",
			in:          Problem{Code: 2001, Message: "not found", Status: 404},
			wantStatus:  404,
			wantCode:    2001,
			wantMessage: "not found",
		},
		{
			name:        "zero status falls back to 500",
			in:          Problem{Code: 1007, Status: 0},
			wantStatus:  500,
			wantCode:    generated.CommonInternal,
			wantMessage: "Internal Server Error",
		},
		{
			name:        "below valid range falls back to 500",
			in:          Problem{Code: 1007, Status: 99},
			wantStatus:  500,
			wantCode:    generated.CommonInternal,
			wantMessage: "Internal Server Error",
		},
		{
			name:        "above valid range falls back to 500",
			in:          Problem{Code: 1007, Status: 600},
			wantStatus:  500,
			wantCode:    generated.CommonInternal,
			wantMessage: "Internal Server Error",
		},
		{
			name:        "empty message uses status text",
			in:          Problem{Code: 1003, Status: 409},
			wantStatus:  409,
			wantCode:    1003,
			wantMessage: "Conflict",
		},
		{
			name:        "code outside contract range falls back to internal",
			in:          Problem{Code: 0, Status: 400},
			wantStatus:  400,
			wantCode:    generated.CommonInternal,
			wantMessage: "Bad Request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := writeProblem(rec, tt.in); err != nil {
				t.Fatalf("writeProblem() error: %v", err)
			}

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got, ok := body["code"].(float64); !ok || int(got) != tt.wantCode {
				t.Fatalf("body.code = %v, want %d", body["code"], tt.wantCode)
			}
			if got, _ := body["msg"].(string); got != tt.wantMessage {
				t.Fatalf("body.msg = %q, want %q", got, tt.wantMessage)
			}
			if got, ok := body["data"]; !ok || got != nil {
				t.Fatalf("body.data = %v, want null", got)
			}
			if _, ok := body["status"]; ok {
				t.Fatalf("body must not carry status: %v", body)
			}
			if _, ok := body["type"]; ok {
				t.Fatalf("body must not carry RFC 9457 type: %v", body)
			}
		})
	}
}

func TestWriteProblemRetryAfter(t *testing.T) {
	tests := []struct {
		name       string
		problem    Problem
		wantStatus int
		wantHeader string
		wantErr    bool
	}{
		{
			name:       "503 writes header",
			problem:    Problem{Code: 1010, Status: http.StatusServiceUnavailable, RetryAfter: "30"},
			wantStatus: http.StatusServiceUnavailable,
			wantHeader: "30",
		},
		{
			name:       "429 writes header",
			problem:    Problem{Code: 1006, Status: http.StatusTooManyRequests, RetryAfter: "120"},
			wantStatus: http.StatusTooManyRequests,
			wantHeader: "120",
		},
		{
			name:       "503 writes http-date header",
			problem:    Problem{Code: 1010, Status: http.StatusServiceUnavailable, RetryAfter: "Wed, 21 Oct 2015 07:28:00 GMT"},
			wantStatus: http.StatusServiceUnavailable,
			wantHeader: "Wed, 21 Oct 2015 07:28:00 GMT",
		},
		{
			name:       "503 drops invalid header",
			problem:    Problem{Code: 1010, Status: http.StatusServiceUnavailable, RetryAfter: "soon"},
			wantStatus: http.StatusServiceUnavailable,
			wantErr:    true,
		},
		{
			name:       "429 drops negative seconds",
			problem:    Problem{Code: 1006, Status: http.StatusTooManyRequests, RetryAfter: "-1"},
			wantStatus: http.StatusTooManyRequests,
			wantErr:    true,
		},
		{
			name:       "503 without retry after",
			problem:    Problem{Code: 1010, Status: http.StatusServiceUnavailable},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "500 ignores retry after",
			problem:    Problem{Code: 1007, Status: http.StatusInternalServerError, RetryAfter: "30"},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "zero status ignores retry after",
			problem:    Problem{Code: 1007, Status: 0, RetryAfter: "30"},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "503 empty retry after",
			problem:    Problem{Code: 1010, Status: http.StatusServiceUnavailable, RetryAfter: ""},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			err := writeProblem(rec, tt.problem)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Retry-After"); got != tt.wantHeader {
				t.Fatalf("Retry-After = %q, want %q", got, tt.wantHeader)
			}
		})
	}
}
