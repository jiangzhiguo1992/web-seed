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
			WriteProblem(rec, tt.in)

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
