package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildManifestProjectsPolicies(t *testing.T) {
	docs := []eventsDoc{
		{
			Topics: []topic{
				{
					ID: "cap.events.x.b", Name: "x.b", Version: 1, Idempotent: true,
					Subscribe: []subscribe{{
						ID: "cap.events.x.b.sub", Name: "sub", Service: "cap.services.x.sub",
						Retry:       &retryPolicy{MaxAttempts: 3, Backoff: "exponential", DeadLetter: true},
						Concurrency: &concurrencyPolicy{Key: "k", TTL: "1m"},
					}},
				},
				{ID: "cap.events.x.a", Name: "x.a", Version: 1},
			},
			Crons: []cron{{
				ID: "cap.events.x.cron", Name: "cron", Service: "cap.services.x.cron",
				Schedule: "0 * * * *", Overlap: "forbid",
				Retry: &retryPolicy{MaxAttempts: 2, Backoff: "fixed", DeadLetter: false},
			}},
			Dispatches: []dispatch{{
				ID: "cap.events.x.d", Name: "d", Service: "cap.services.x.d",
				EmittedBy: []string{"cap.services.x.a"}, Delay: "1s", Timeout: "1h",
			}},
		},
	}
	m := buildManifest(docs)
	if len(m.Topics) != 2 || m.Topics[0].ID != "cap.events.x.a" {
		t.Fatalf("topics not sorted: %+v", m.Topics)
	}
	if len(m.Topics[1].Subscribers) != 1 || m.Topics[1].Subscribers[0].ID != "cap.events.x.b.sub" {
		t.Fatalf("subscriber not projected: %+v", m.Topics[1])
	}
	if len(m.Crons) != 1 || m.Crons[0].Overlap != "forbid" {
		t.Fatalf("cron not projected: %+v", m.Crons)
	}
	if len(m.Dispatches) != 1 || m.Dispatches[0].Timeout != "1h" || m.Dispatches[0].Delay != "1s" {
		t.Fatalf("dispatch not projected: %+v", m.Dispatches)
	}
	if m.Topics[1].Version != 1 || !m.Topics[1].Idempotent {
		t.Fatalf("topic version/idempotent not projected: %+v", m.Topics[1])
	}
	b, err := marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"max_attempts: 3", "overlap: forbid", "timeout: 1h", "delay: 1s", "idempotent: true", "concurrency:"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("manifest missing %q:\n%s", want, b)
		}
	}
}

func writeMetaFixture(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"_types.yaml", "cap.events.yaml"} {
		src := filepath.Join("..", "..", "..", "contracts", "_meta", name)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, "contracts", "_meta", name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildReadsTargets(t *testing.T) {
	root := t.TempDir()
	writeMetaFixture(t, root)
	event := `id: cap.events.x
topics:
  - id: cap.events.x.t
    name: x.t
    version: 1
    idempotent: true
    publish: { emitted_by: [] }
`
	dst := filepath.Join(root, "contracts", "capabilities", "events", "x.yaml")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(event), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := build(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "cap.events.x.t") {
		t.Fatalf("topic missing:\n%s", b)
	}
}

func TestCheckManifestDetectsDrift(t *testing.T) {
	target := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(target, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkManifest(target, []byte("a: 1\n")); err != nil {
		t.Fatalf("matching manifest rejected: %v", err)
	}
	if err := checkManifest(target, []byte("a: 2\n")); err == nil || !strings.Contains(err.Error(), "not up to date") {
		t.Fatalf("drift not reported: %v", err)
	}
	if err := checkManifest(filepath.Join(t.TempDir(), "missing.yaml"), []byte("a: 1\n")); err == nil || !strings.Contains(err.Error(), "run make gen-async") {
		t.Fatalf("missing manifest not reported: %v", err)
	}
}

func TestBuildRejectsInvalidYAML(t *testing.T) {
	root := t.TempDir()
	writeMetaFixture(t, root)
	dst := filepath.Join(root, "contracts", "capabilities", "events", "x.yaml")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("topics: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := build(root); err == nil {
		t.Fatal("invalid event YAML accepted")
	}
}

func TestBuildManifestDeterministic(t *testing.T) {
	a := eventsDoc{Topics: []topic{{ID: "cap.events.x.a"}, {ID: "cap.events.x.b"}}}
	b := eventsDoc{Topics: []topic{{ID: "cap.events.x.b"}, {ID: "cap.events.x.a"}}}
	left, err := marshal(buildManifest([]eventsDoc{a}))
	if err != nil {
		t.Fatal(err)
	}
	right, err := marshal(buildManifest([]eventsDoc{b}))
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(right) {
		t.Fatalf("manifest is order-sensitive:\n%s\n%s", left, right)
	}
}

func TestBuildManifestEmpty(t *testing.T) {
	b, err := marshal(buildManifest(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "schema_version: 1") || strings.Contains(string(b), "topics:") {
		t.Fatalf("unexpected empty manifest:\n%s", b)
	}
}

func TestBuildManifestTopicWithoutSubscribers(t *testing.T) {
	m := buildManifest([]eventsDoc{{Topics: []topic{{ID: "cap.events.x.t", Name: "x.t", Version: 1}}}})
	b, err := marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "subscribers:") {
		t.Fatalf("empty subscribers emitted:\n%s", b)
	}
}
