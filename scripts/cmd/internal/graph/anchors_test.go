package graph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnchorsWriteAndCheck(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "contracts/_meta/cap.services.yaml", `contract: cap.services
targets:
  dir: contracts/capabilities/services
  glob: "*.yaml"
  id: "cap.services.<file>"
`)
	writeFixture(t, root, "contracts/_meta/feature.flows.yaml", `contract: feature.flows
targets:
  dir: contracts/features/flows
  glob: "*.yaml"
  id: "feature.flows.<file>"
`)
	writeFixture(t, root, "contracts/_meta/cap.lifecycles.yaml", `contract: cap.lifecycles
targets:
  dir: contracts/capabilities/lifecycles
  glob: "*.yaml"
  id: "cap.lifecycles.<file>"
`)
	writeFixture(t, root, "contracts/_meta/cap.events.yaml", `contract: cap.events
targets:
  dir: contracts/capabilities/events
  glob: "*.yaml"
  id: "cap.events.<file>"
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
owner_module: x
services:
  - name: create
    steps:
      - id: create
        kind: logic
        rules: [ { rule: feature.flows.x.r1 } ]
        lifecycles: [cap.lifecycles.x.create]
      - id: emit_created
        kind: events
        events: [{ref: cap.events.x.created}]
  - name: activate
    steps:
      - id: transition
        kind: logic
        rules: [ { rule: feature.flows.x.r1 } ]
        lifecycles: [cap.lifecycles.x.transitions.activate]
        state_gate:
          - { lifecycle: cap.lifecycles.x, values: [pending] }
      - id: emit_activated
        kind: events
        events: [{ref: cap.events.x.activated}]
  - name: delete
    steps:
      - id: soft_delete
        kind: logic
        lifecycles: [cap.lifecycles.x.destroy]
        state_gate:
          - { lifecycle: cap.lifecycles.x, values: [pending, active] }
  - name: consume
    steps:
      - id: handle
        kind: logic
`)
	writeFixture(t, root, "contracts/features/flows/x.yaml", `id: feature.flows.x
rules:
  - id: feature.flows.x.r1
    implemented_by: []
`)
	writeFixture(t, root, "contracts/capabilities/lifecycles/x.yaml", `id: cap.lifecycles.x
subjects:
  - { entity: data.entities.x, field: status }
states:
  values: [pending, active, deleted]
  initial: pending
  final: [deleted]
create:
  implemented_by: []
destroy:
  implemented_by: []
transitions:
  - name: activate
    description: "a"
    from: [pending]
    to: active
    implemented_by: []
`)
	writeFixture(t, root, "contracts/capabilities/events/x.yaml", `id: cap.events.x
topics:
  - id: cap.events.x.created
    name: x.created
    version: 1
    summary: "x"
    publish:
      payload: { type: _types.basics.integer }
      emitted_by: []

  - id: cap.events.x.activated
    name: x.activated
    version: 1
    summary: "x"
    publish:
      payload: { type: _types.basics.integer }
      emitted_by: []
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
		"contracts/features/flows/x.yaml": "implemented_by: [ cap.services.x.activate, cap.services.x.create ]",
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
	lifecycle, err := os.ReadFile(filepath.Join(root, "contracts/capabilities/lifecycles/x.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"implemented_by:\n    - cap.services.x.create.create",
		"implemented_by:\n    - cap.services.x.delete.soft_delete",
		"implemented_by:\n      - cap.services.x.activate.transition",
		"{ value: pending, steps: [ cap.services.x.activate.transition, cap.services.x.delete.soft_delete ] }",
		"{ value: active, steps: [ cap.services.x.delete.soft_delete ] }",
		"{ value: deleted, steps: [] }",
	} {
		if !strings.Contains(string(lifecycle), want) {
			t.Fatalf("lifecycle missing %q:\n%s", want, lifecycle)
		}
	}
	events, err := os.ReadFile(filepath.Join(root, "contracts/capabilities/events/x.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"emitted_by:\n        - cap.services.x.create",
		"emitted_by:\n        - cap.services.x.activate",
		"emitted_by:\n        - cap.services.x.create\n\n  - id: cap.events.x.activated",
	} {
		if !strings.Contains(string(events), want) {
			t.Fatalf("events missing %q:\n%s", want, events)
		}
	}
}

func TestAnchorsReplaceMultilineFields(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "contracts/_meta/cap.services.yaml", `contract: cap.services
targets:
  dir: contracts/capabilities/services
  glob: "*.yaml"
  id: "cap.services.<file>"
`)
	writeFixture(t, root, "contracts/_meta/cap.lifecycles.yaml", `contract: cap.lifecycles
targets:
  dir: contracts/capabilities/lifecycles
  glob: "*.yaml"
  id: "cap.lifecycles.<file>"
`)
	writeFixture(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
owner_module: x
services:
  - name: create
    steps:
      - id: create
        kind: logic
        lifecycles: [cap.lifecycles.x.create]
  - name: activate
    steps:
      - id: transition
        kind: logic
        lifecycles: [cap.lifecycles.x.transitions.activate]
  - name: delete
    steps:
      - id: soft_delete
        kind: logic
        lifecycles: [cap.lifecycles.x.destroy]
`)
	writeFixture(t, root, "contracts/capabilities/lifecycles/x.yaml", `id: cap.lifecycles.x
subjects:
  - { entity: data.entities.x, field: status }
states:
  values: [pending, active, deleted]
  initial: pending
  final: [deleted]
create:
  implemented_by:
    - cap.services.x.create.old
destroy:
  implemented_by:
    - cap.services.x.delete.old
transitions:
  - name: activate
    description: "a"
    from: [pending]
    to: active
    implemented_by:
      - cap.services.x.activate.old
`)

	target, err := resolveTarget(root, "cap.services")
	if err != nil {
		t.Fatal(err)
	}
	a, err := computeAnchors(root, target)
	if err != nil {
		t.Fatal(err)
	}
	if drift, err := a.Check(root); err != nil {
		t.Fatal(err)
	} else if len(drift) == 0 {
		t.Fatal("expected drift before write")
	}
	if _, err := a.Write(root); err != nil {
		t.Fatal(err)
	}
	if drift, err := a.Check(root); err != nil {
		t.Fatal(err)
	} else if len(drift) != 0 {
		t.Fatalf("drift after write: %v", drift)
	}
	lifecycle, err := os.ReadFile(filepath.Join(root, "contracts/capabilities/lifecycles/x.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(lifecycle)
	for _, want := range []string{
		"implemented_by:\n    - cap.services.x.create.create",
		"implemented_by:\n    - cap.services.x.delete.soft_delete",
		"implemented_by:\n      - cap.services.x.activate.transition",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("lifecycle missing %q:\n%s", want, text)
		}
	}
	for _, stale := range []string{"create.old", "delete.old", "activate.old"} {
		if strings.Contains(text, stale) {
			t.Fatalf("lifecycle still contains %q:\n%s", stale, text)
		}
	}
}
