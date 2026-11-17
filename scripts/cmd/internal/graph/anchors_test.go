package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnchorsWriteAndCheck(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "contracts/_meta/cap.services.yaml", `type: cap.services
targets:
  dir: contracts/capabilities/services
  glob: "*.yaml"
  id: "cap.services.<file>"
`)
	writeFixture(t, root, "contracts/_meta/feature.rules.yaml", `type: feature.rules
targets:
  dir: contracts/features/rules
  glob: "*.yaml"
  id: "feature.rules.<file>"
`)
	writeFixture(t, root, "contracts/_meta/feature.states.yaml", `type: feature.states
targets:
  dir: contracts/features/states
  glob: "*.yaml"
  id: "feature.states.<file>"
`)
	writeFixture(t, root, "contracts/_meta/cap.events.yaml", `type: cap.events
targets:
  dir: contracts/capabilities/events
  glob: "*.yaml"
  id: "cap.events.<file>"
`)
	writeFixture(t, root, "contracts/_meta/cap.async.yaml", `type: cap.async
targets:
  dir: contracts/capabilities/async
  glob: "*.yaml"
  id: "cap.async.<file>"
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
owner_module: x
services:
  - name: create
    steps:
      - id: create
        kind: logic
        rules: [feature.rules.x.r1]
        events: [cap.events.x.created]
  - name: activate
    steps:
      - id: activate
        kind: transition
        transitions: [feature.states.x.activate]
        events: [cap.events.x.activated]
  - name: consume
    steps:
      - id: handle
        kind: logic
`)
	writeFixture(t, root, "contracts/capabilities/async/x.yaml", `id: cap.async.x
owner_module: x
jobs:
  - id: cap.async.x.consume_activated
    name: consume_activated
    service: cap.services.x.consume
    trigger: {type: event, event: cap.events.x.activated}
`)
	writeFixture(t, root, "contracts/features/rules/x.yaml", `id: feature.rules.x
rules:
  - id: feature.rules.x.r1
    implemented_by: []
`)
	writeFixture(t, root, "contracts/features/states/x.yaml", `id: feature.states.x
transitions:
  - name: activate
    side_effects: [cap.events.x.activated]
    implemented_by: []
`)
	writeFixture(t, root, "contracts/capabilities/events/x.yaml", `id: cap.events.x
events:
  - id: cap.events.x.created
    emitted_by: []
    consumers: []
  - id: cap.events.x.activated
    emitted_by: []
    consumers: [audit]
`)

	target, err := resolveTarget(root, "cap.services")
	if err != nil {
		t.Fatal(err)
	}
	a, err := computeAnchors(root, target)
	if err != nil {
		t.Fatal(err)
	}
	drift, err := a.Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) == 0 {
		t.Fatal("expected drift before write")
	}
	if _, err := a.Write(root); err != nil {
		t.Fatal(err)
	}
	drift, err = a.Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Fatalf("drift after write: %v", drift)
	}

	checks := map[string]string{
		"contracts/features/rules/x.yaml":  "implemented_by: [ cap.services.x.create ]",
		"contracts/features/states/x.yaml": "implemented_by: [ cap.services.x.activate ]",
	}
	for file, want := range checks {
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), want) {
			t.Fatalf("%s missing %q:\n%s", file, want, b)
		}
	}
	events, err := os.ReadFile(filepath.Join(root, "contracts/capabilities/events/x.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"emitted_by: [ cap.services.x.create ]",
		"emitted_by: [ cap.services.x.activate ]",
		"consumers: [ audit, x ]",
	} {
		if !strings.Contains(string(events), want) {
			t.Fatalf("events missing %q:\n%s", want, events)
		}
	}
}
