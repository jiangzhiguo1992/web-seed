package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadHTTPPolicy(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/platform/http_conventions.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`audiences: [admin, user]
error_responses:
  always: [400, 500]
  secured: [401]
  body: [413]
  deprecated: [410]
transport_headers: [Connection, Content-Length, Expect]
body_types:
  json: application/json
  text: text/plain
  note: "mapping"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadHTTPPolicy(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Audiences) != 2 || policy.Audiences[0] != "admin" {
		t.Fatalf("audiences = %v", policy.Audiences)
	}
	if len(policy.TransportHeaders) != 3 || policy.TransportHeaders[2] != "Expect" {
		t.Fatalf("transport_headers = %v", policy.TransportHeaders)
	}
	if len(policy.BodyTypes) != 2 || policy.BodyTypes["json"] != "application/json" || policy.BodyTypes["text"] != "text/plain" {
		t.Fatalf("body_types = %v", policy.BodyTypes)
	}
	got := policy.EffectiveStatuses([]int{201}, true, true, false)
	want := []int{201, 400, 401, 413, 500}
	if len(got) != len(want) {
		t.Fatalf("statuses = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statuses = %v", got)
		}
	}
}

func TestLoadHTTPPolicyRejectsDuplicateBodyMediaTypes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/platform/http_conventions.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`body_types:
  json: application/json
  text: application/json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadHTTPPolicy(root)
	if err == nil || !strings.Contains(err.Error(), "share media type") {
		t.Fatalf("duplicate body media type not rejected: %v", err)
	}
}

func TestLoadHTTPPolicyValidatesTransportHeaders(t *testing.T) {
	for _, tc := range []struct {
		name      string
		headers   string
		wantError string
	}{
		{"invalid token", "[Connection, \"Bad Header\"]", "invalid header token"},
		{"duplicate case insensitive", "[Connection, connection]", "duplicate header"},
		{"empty name", "[Connection, \"\"]", "invalid header token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "contracts/platform/http_conventions.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			body := "transport_headers: " + tc.headers + "\n"
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadHTTPPolicy(root)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("want %q, got %v", tc.wantError, err)
			}
		})
	}
}
