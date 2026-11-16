package httpserver

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteProblem(t *testing.T) {
	tests := []struct {
		name       string
		in         Problem
		wantStatus int
		wantType   string
	}{
		{
			name:       "normal",
			in:         Problem{Title: "not found", Status: 404, Code: "NOTE_NOT_FOUND"},
			wantStatus: 404,
			wantType:   "about:blank",
		},
		{
			name:       "zero status falls back to 500",
			in:         Problem{Status: 0},
			wantStatus: 500,
			wantType:   "about:blank",
		},
		{
			name:       "below valid range falls back to 500",
			in:         Problem{Status: 99},
			wantStatus: 500,
			wantType:   "about:blank",
		},
		{
			name:       "above valid range falls back to 500",
			in:         Problem{Status: 600},
			wantStatus: 500,
			wantType:   "about:blank",
		},
		{
			name:       "custom type is preserved",
			in:         Problem{Type: "https://example.com/errors/conflict", Status: 409},
			wantStatus: 409,
			wantType:   "https://example.com/errors/conflict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if err := writeProblem(rec, tt.in); err != nil {
				t.Fatalf("WriteProblem() error: %v", err)
			}

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
				t.Fatalf("Content-Type = %q, want application/problem+json", got)
			}

			var body Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Type != tt.wantType {
				t.Fatalf("body.Type = %q, want %q", body.Type, tt.wantType)
			}
			if body.Status != tt.wantStatus {
				t.Fatalf("body.Status = %d, want %d", body.Status, tt.wantStatus)
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
			problem:    Problem{Status: 503, RetryAfter: "30"},
			wantStatus: 503,
			wantHeader: "30",
		},
		{
			name:       "429 writes header",
			problem:    Problem{Status: 429, RetryAfter: "120"},
			wantStatus: 429,
			wantHeader: "120",
		},
		{
			name:       "503 writes http-date header",
			problem:    Problem{Status: 503, RetryAfter: "Wed, 21 Oct 2015 07:28:00 GMT"},
			wantStatus: 503,
			wantHeader: "Wed, 21 Oct 2015 07:28:00 GMT",
		},
		{
			name:       "503 drops invalid header",
			problem:    Problem{Status: 503, RetryAfter: "soon"},
			wantStatus: 503,
			wantErr:    true,
		},
		{
			name:       "429 drops negative seconds",
			problem:    Problem{Status: 429, RetryAfter: "-1"},
			wantStatus: 429,
			wantErr:    true,
		},
		{
			name:       "503 without retry after",
			problem:    Problem{Status: 503},
			wantStatus: 503,
		},
		{
			name:       "500 ignores retry after",
			problem:    Problem{Status: 500, RetryAfter: "30"},
			wantStatus: 500,
		},
		{
			name:       "invalid status falls back and drops header",
			problem:    Problem{Status: 0, RetryAfter: "30"},
			wantStatus: 500,
		},
		{
			name:       "empty retry after",
			problem:    Problem{Status: 503, RetryAfter: ""},
			wantStatus: 503,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			err := writeProblem(rec, tt.problem)
			if (err != nil) != tt.wantErr {
				t.Fatalf("WriteProblem() error = %v, wantErr %v", err, tt.wantErr)
			}

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Retry-After"); got != tt.wantHeader {
				t.Fatalf("Retry-After = %q, want %q", got, tt.wantHeader)
			}

			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if _, ok := body["RetryAfter"]; ok {
				t.Fatalf("RetryAfter must not appear in body")
			}
			if _, ok := body["retry_after"]; ok {
				t.Fatalf("retry_after must not appear in body")
			}
		})
	}
}

func TestValidRetryAfter(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"30", true},
		{"0", true},
		{"007", true},
		{"Wed, 21 Oct 2015 07:28:00 GMT", true},
		{"", false},
		{"-1", false},
		{" 30", false},
		{"30 ", false},
		{"soon", false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			if got := validRetryAfter(tc.value); got != tc.want {
				t.Fatalf("validRetryAfter(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
