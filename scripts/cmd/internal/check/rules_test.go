package check

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

func typeField(t *testing.T, root, typ string, path ...string) Field {
	t.Helper()
	schema, _, err := contractspec.LoadSchemaByType(root, typ)
	if err != nil {
		t.Fatal(err)
	}
	fields := schema.Fields
	var field Field
	for i, name := range path {
		var ok bool
		field, ok = contractspec.FindField(fields, name)
		if !ok {
			t.Fatalf("%s: field %s not found (path %v)", typ, name, path)
		}
		if i < len(path)-1 {
			fields = field.ItemFields
		}
	}
	if field.Pattern == "" {
		t.Fatalf("%s: field %v has no pattern", typ, path)
	}
	return field
}

func TestTypeExpressionPatterns(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		name   string
		typ    string
		path   []string
		accept []string
		reject []string
	}{
		{
			name: "service param type",
			typ:  "cap.services",
			path: []string{"services", "params", "type"},
			accept: []string{
				"_types.string", "*_types.string", "_types.list<_types.string>",
				"data.enums.account_status", "_types.list<data.enums.account_status>",
				"data.entities.account", "*data.entities.account",
				"cap.services.account.schemas.AccountPage",
				"*cap.services.account.schemas.AccountPage",
				"_types.list<cap.services.account.schemas.AccountPage>",
				"context.Context", "time.Duration", "_types.list<time.Duration>",
			},
			reject: []string{
				"string", "List<string>", "data.tables.account", "@inline/1",
				"*List<string>", "List<*string>", "List<List<string>>", "**string",
				"List<string", "cap.services.account.schemas.bad-name",
				"data.enums.AccountStatus",
			},
		},
		{
			name: "entity field type",
			typ:  "data.entities",
			path: []string{"fields", "type"},
			accept: []string{
				"_types.string", "*_types.timestamp", "data.enums.account_status",
				"data.entities.account", "_types.list<data.entities.account>",
			},
			reject: []string{
				"string", "List<data.entities.account>", "data.tables.account",
				"_types.list<data.enums.account_status>", "@inline/1",
				"data.enums.AccountStatus",
			},
		},
		{
			name: "query param type",
			typ:  "data.queries",
			path: []string{"queries", "params", "type"},
			accept: []string{
				"_types.string", "*_types.string", "_types.list<_types.string>",
				"data.enums.account_status",
				"_types.list<data.enums.account_status>", "context.Context",
			},
			reject: []string{
				"string", "bigint", "List<string>", "data.tables.account",
				"List<data.entities.account>", "@inline/1",
				"*List<string>", "List<*string>",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field := typeField(t, root, tc.typ, tc.path...)
			for _, value := range tc.accept {
				if !match(field.Pattern, value) {
					t.Errorf("pattern rejected %q: %s", value, field.Pattern)
				}
			}
			for _, value := range tc.reject {
				if match(field.Pattern, value) {
					t.Errorf("pattern accepted %q: %s", value, field.Pattern)
				}
			}
		})
	}
}

func TestSoftZonesMarked(t *testing.T) {
	root := repoRoot(t)
	httpSchema, _, err := contractspec.LoadSchemaByType(root, "cap.protocols.http")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := contractspec.FindField(httpSchema.Fields, "components"); ok {
		t.Fatalf("cap.protocols.http.components must be removed; use shared")
	}
	lifecycleSchema, _, err := contractspec.LoadSchemaByType(root, "cap.lifecycles")
	if err != nil {
		t.Fatal(err)
	}
	transitions, ok := contractspec.FindField(lifecycleSchema.Fields, "transitions")
	if !ok {
		t.Fatal("cap.lifecycles.transitions missing")
	}
	for _, soft := range []string{"guard", "on_violation"} {
		if _, ok := contractspec.FindField(transitions.ItemFields, soft); ok {
			t.Fatalf("cap.lifecycles.transitions.%s must not exist", soft)
		}
	}
	entitySchema, _, err := contractspec.LoadSchemaByType(root, "data.entities")
	if err != nil {
		t.Fatal(err)
	}
	validation, ok := contractspec.FindField(entitySchema.Fields, "validation")
	if !ok {
		t.Fatal("data.entities.validation missing")
	}
	expr, ok := contractspec.FindField(validation.ItemFields, "expr")
	if !ok || !expr.Soft {
		t.Fatalf("data.entities.validation.expr must be marked soft")
	}
	tableSchema, _, err := contractspec.LoadSchemaByType(root, "data.tables")
	if err != nil {
		t.Fatal(err)
	}
	constraints, ok := contractspec.FindField(tableSchema.Fields, "constraints")
	if !ok {
		t.Fatal("data.tables.constraints missing")
	}
	constraintExpr, ok := contractspec.FindField(constraints.ItemFields, "expr")
	if !ok || !constraintExpr.Soft {
		t.Fatalf("data.tables.constraints.expr must be marked soft")
	}
}

func TestSoftFieldTypes(t *testing.T) {
	if err := validateFieldTypes([]Field{{Name: "x", Type: "string", Soft: true}}, testValueTypes(), false); err != nil {
		t.Fatalf("soft string field rejected: %v", err)
	}
	if err := validateFieldTypes([]Field{{Name: "x", Type: "any", Soft: true}}, testValueTypes(), false); err != nil {
		t.Fatalf("soft any field rejected: %v", err)
	}
	if err := validateFieldTypes([]Field{{Name: "x", Type: "integer", Soft: true}}, testValueTypes(), false); err == nil {
		t.Fatal("soft on integer field must fail")
	}
}

func TestRuleValueValidation(t *testing.T) {
	cases := []struct {
		name   string
		field  *entityFieldInfo
		op     string
		value  any
		reject bool
	}{
		{"valid pattern", &entityFieldInfo{Type: "string"}, "pattern", `^[A-Za-z0-9_-]{4,64}$`, false},
		{"invalid pattern", &entityFieldInfo{Type: "string"}, "pattern", `[`, true},
		{"pattern on integer", &entityFieldInfo{Type: "integer"}, "pattern", `^1$`, true},
		{"in list", &entityFieldInfo{Type: "string"}, "in", []any{"a", "b"}, false},
		{"in scalar", &entityFieldInfo{Type: "string"}, "in", "a", true},
		{"in empty", &entityFieldInfo{Type: "string"}, "in", []any{}, true},
		{"min_len string", &entityFieldInfo{Type: "string"}, "min_len", 4, false},
		{"min_len not int", &entityFieldInfo{Type: "string"}, "min_len", "4", true},
		{"min_len on integer", &entityFieldInfo{Type: "integer"}, "min_len", 4, true},
		{"required bool", &entityFieldInfo{Type: "string"}, "required", true, false},
		{"required string", &entityFieldInfo{Type: "string"}, "required", "true", true},
		{"eq scalar", &entityFieldInfo{Type: "string"}, "eq", "x", false},
		{"eq map", &entityFieldInfo{Type: "string"}, "eq", map[string]any{"x": 1}, true},
		{"gt integer", &entityFieldInfo{Type: "integer"}, "gt", 3, false},
		{"gt string field", &entityFieldInfo{Type: "string"}, "gt", 3, true},
		{"gt non-number", &entityFieldInfo{Type: "integer"}, "gt", "3", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Checker{}
			c.checkRuleValue("contracts/features/rules/x.yaml", "feature.rules.x.r", tc.field, tc.op, tc.value)
			got := strings.Contains(strings.Join(c.errs, "\n"), "[rule-value]")
			if got != tc.reject {
				t.Fatalf("checkRuleValue(%s, %v) = %v, want reject=%v; errs=%v", tc.op, tc.value, got, tc.reject, c.errs)
			}
		})
	}
}

func TestEnumUsageUsesConcreteOwner(t *testing.T) {
	root := t.TempDir()
	writeCapFile(t, root, "contracts/capabilities/services/x.yaml", `id: cap.services.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
schemas:
  Filter:
    type: _types.object
    properties:
      - { name: status, schema: { ref: data.enums.x_status }, required: true, description: "status" }
services:
  - name: get
    summary: "x"
    visibility: public
    tx_mode: readonly
    params:
      - { name: ctx, type: context.Context }
      - { name: filter, type: cap.services.x.schemas.Filter }
    returns:
      - { name: err, type: error }
    steps: []
`)
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
paths:
  /x:
    get:
      name: list
      id: cap.protocols.http.x.list
      service: cap.services.x.get
      request:
        query:
          - name: status
            schema: data.enums.x_status
            required: false
            description: "status"
      response:
        "204": { description: OK }
`)
	writeCapFile(t, root, "contracts/capabilities/events/x.yaml", `id: cap.events.x
summary: "x"
schema_version: 1
stability: draft
do_not: []
topics:
  - id: cap.events.x.created
    name: x.created
    version: 1
    summary: "x"
    publish:
      payload:
        type: _types.object
        properties:
          - { name: status, schema: { ref: data.enums.x_status }, required: true, description: "status" }
      emitted_by: []
`)
	c := &Checker{root: root}
	u := c.enumUsages()["data.enums.x_status"]
	if u == nil {
		t.Fatal("enum usage not recorded")
	}
	if !u.capabilities["cap.services.x.get"] {
		t.Fatalf("service item usage missing: %v", u.capabilities)
	}
	if u.capabilities["cap.services.x"] {
		t.Fatalf("file-level service usage must not be recorded: %v", u.capabilities)
	}
	if !u.capabilities["cap.events.x.created"] {
		t.Fatalf("event topic usage missing: %v", u.capabilities)
	}
	if u.capabilities["cap.events.x"] {
		t.Fatalf("file-level event usage must not be recorded: %v", u.capabilities)
	}
	if !u.capabilities["cap.protocols.http.x.list"] {
		t.Fatalf("http operation usage missing: %v", u.capabilities)
	}
	if u.capabilities["cap.protocols.http.x"] {
		t.Fatalf("file-level http usage must not be recorded: %v", u.capabilities)
	}
}
