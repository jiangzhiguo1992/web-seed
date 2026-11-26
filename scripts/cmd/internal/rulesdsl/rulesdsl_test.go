package rulesdsl

import (
	"strings"
	"testing"
)

func evalBool(t *testing.T, expr string, env Env) bool {
	t.Helper()
	v, err := Eval(expr, env)
	if err != nil {
		t.Fatalf("Eval(%q): %v", expr, err)
	}
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("Eval(%q) = %T, want bool", expr, v)
	}
	return b
}

func TestLengthInvariant(t *testing.T) {
	const expr = `len(nickname) >= 1 && len(nickname) <= 100`
	if !evalBool(t, expr, Env{"nickname": "Alice"}) {
		t.Fatal("non-empty nickname should pass")
	}
	if evalBool(t, expr, Env{"nickname": ""}) {
		t.Fatal("empty nickname should fail")
	}
	if evalBool(t, expr, Env{"nickname": strings.Repeat("a", 101)}) {
		t.Fatal("101-character nickname should fail")
	}
	if !evalBool(t, expr, Env{"nickname": strings.Repeat("中", 100)}) {
		t.Fatal("len() should count characters, not bytes")
	}
}

func TestConsistencyInvariant(t *testing.T) {
	const expr = `(status == "deleted") == (deleted_at != nil)`
	if !evalBool(t, expr, Env{"status": "active", "deleted_at": nil}) {
		t.Fatal("active + nil should pass")
	}
	if !evalBool(t, expr, Env{"status": "deleted", "deleted_at": "2026-01-01"}) {
		t.Fatal("deleted + non-nil should pass")
	}
	if evalBool(t, expr, Env{"status": "deleted", "deleted_at": nil}) {
		t.Fatal("deleted + nil should fail")
	}
}

func TestListMembership(t *testing.T) {
	if !evalBool(t, `purpose in ["login", "reset_password"]`, Env{"purpose": "login"}) {
		t.Fatal("login should be in list")
	}
	if evalBool(t, `purpose in ["login", "reset_password"]`, Env{"purpose": "register"}) {
		t.Fatal("register should not be in list")
	}
	if !evalBool(t, `purpose not in ["register"]`, Env{"purpose": "login"}) {
		t.Fatal("login should not be register")
	}
}

func TestSyntaxAndUnknownIdentifier(t *testing.T) {
	if err := Validate(`len(name) >= 1 &&`); err == nil {
		t.Fatal("incomplete expression should fail")
	}
	if _, err := Eval(`unknown.field == 1`, Env{}); err == nil {
		t.Fatal("unknown identifier should fail")
	}
}
