package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoverageGaps(t *testing.T) {
	coverage := []coverageEntry{
		{Operation: "cap.protocols.http.x.none", Scenarios: 0},
		{
			Operation:       "cap.protocols.http.x.partial",
			Scenarios:       1,
			MissingStatuses: []int{200},
			Codes:           []codeCoverage{{Code: "X", Status: 409, Covered: false}},
		},
	}
	gaps := coverageGaps(coverage)
	if len(gaps) != 3 {
		t.Fatalf("gaps = %v", gaps)
	}
	if gaps[0] != "cap.protocols.http.x.none: no scenario" {
		t.Fatalf("gaps[0] = %q", gaps[0])
	}
}

func TestLoadHTTPOpsRejectsTransportHeaders(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("contracts/platform/http_conventions.yaml", `transport_headers: [Connection]
audiences: [admin]
body_types: { json: application/json, form: application/x-www-form-urlencoded, multipart: multipart/form-data, text: text/plain, binary: application/octet-stream }
`)
	write("contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
mount: { api_version: v1, path: /, audience: [admin] }
paths:
  /x:
    get:
      id: cap.protocols.http.x.get
      name: getX
      service: cap.services.x.get
      summary: "x"
      used_by: [feature.pages.x]
      request:
        headers:
          - { name: Connection, required: false, value: keep-alive, description: "transport" }
      response:
        "204":
          msg: "OK"
`)
	policy, err := loadPolicy(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadHTTPOps(root, policy); err == nil || !strings.Contains(err.Error(), "transport-managed") {
		t.Fatalf("transport header not rejected: %v", err)
	}
}
