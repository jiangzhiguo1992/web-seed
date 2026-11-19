package check

import (
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func TestMapKeyPattern(t *testing.T) {
	c := &Checker{nodeKinds: map[string]contractspec.SchemaKind{"scalar": {}}}
	c.validateValue(map[string]any{
		"bad-name": map[string]any{"type": "string"},
	}, Field{Name: "schemas", Type: "map", Types: []string{"scalar"}, KeyPattern: `^[A-Za-z][A-Za-z0-9]*$`}, "schemas")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.pattern]") {
		t.Fatalf("invalid map key not reported: %v", c.errs)
	}
}

func TestLocalSchemaTypeMustBeQualified(t *testing.T) {
	types := map[string]bool{"cap.services.x.schemas.X": true}
	c := &Checker{}
	c.checkTypeName("X", types, "params[0].type")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[service-ref]") {
		t.Fatalf("bare local schema accepted: %v", c.errs)
	}
	c.errs = nil
	c.checkTypeName("cap.services.x.schemas.X", types, "params[0].type")
	if len(c.errs) != 0 {
		t.Fatalf("qualified local schema rejected: %v", c.errs)
	}
}

func TestServiceTypeRefKeepsQualification(t *testing.T) {
	ref := "cap.services.x.schemas.X"
	got, ok := serviceTypeRef(ref)
	if !ok || got != ref {
		t.Fatalf("serviceTypeRef(%q) = %q, %v; want %q, true", ref, got, ok, ref)
	}
}
