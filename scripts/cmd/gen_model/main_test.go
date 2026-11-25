package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func TestGeneratedFilesUpToDate(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	if err := run(root, true); err != nil {
		t.Fatal(err)
	}
}

func TestScalarMapping(t *testing.T) {
	cases := []struct {
		name     string
		spec     columnSpec
		nullable bool
		goType   string
		tsType   string
	}{
		{"integer 64", columnSpec{Family: "integer", Bits: 64}, false, "int64", "number"},
		{"integer 32", columnSpec{Family: "integer", Bits: 32}, false, "int32", "number"},
		{"nullable string", columnSpec{Family: "string"}, true, "*string", "string | null"},
		{"decimal", columnSpec{Family: "decimal", Precision: 19, Scale: 4}, false, "decimal.Decimal", "string"},
		{"timestamp", columnSpec{Family: "timestamp"}, false, "time.Time", "string"},
		{"json", columnSpec{Family: "json"}, false, "json.RawMessage", "unknown"},
		{"nullable json", columnSpec{Family: "json"}, true, "json.RawMessage", "unknown"},
		{"bytes", columnSpec{Family: "bytes"}, false, "[]byte", "string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := goScalar(tc.spec, tc.nullable); got != tc.goType {
				t.Fatalf("goScalar() = %q, want %q", got, tc.goType)
			}
			if got := tsScalar(tc.spec, tc.nullable); got != tc.tsType {
				t.Fatalf("tsScalar() = %q, want %q", got, tc.tsType)
			}
		})
	}
}

func TestPascal(t *testing.T) {
	for in, want := range map[string]string{
		"pending":           "Pending",
		"bad_password":      "BadPassword",
		"change_email":      "ChangeEmail",
		"attempts_exceeded": "AttemptsExceeded",
	} {
		if got := pascal(in); got != want {
			t.Fatalf("pascal(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecretFieldsNotSerialized(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	goSrc, err := os.ReadFile(filepath.Join(root, goModelPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goSrc), "accessTokenHash") {
		t.Fatal("secret go field accessTokenHash missing")
	}
	if strings.Contains(string(goSrc), `json:"access_token_hash"`) {
		t.Fatal("secret go field accessTokenHash must not have a json tag")
	}
	tsSrc, err := os.ReadFile(filepath.Join(root, tsModelPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(tsSrc), "access_token_hash") || strings.Contains(string(tsSrc), "password_hash") {
		t.Fatal("secret fields leaked into TS model")
	}
}

func TestGeneratedModelRemovedAndRenamedFields(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	goSrc, err := os.ReadFile(filepath.Join(root, goModelPath))
	if err != nil {
		t.Fatal(err)
	}
	tsSrc, err := os.ReadFile(filepath.Join(root, tsModelPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"IconAttachmentID", "json:\"owner_table\"", "icon_attachment_id"} {
		if strings.Contains(string(goSrc), banned) {
			t.Fatalf("generated Go still contains %q", banned)
		}
	}
	for _, banned := range []string{"icon_attachment_id", "owner_table:"} {
		if strings.Contains(string(tsSrc), banned) {
			t.Fatalf("generated TS still contains %q", banned)
		}
	}
	if !strings.Contains(string(tsSrc), "owner_table_name") || !strings.Contains(string(tsSrc), "replaces_id") {
		t.Fatal("generated TS missing renamed/new attachment fields")
	}
}

func TestTypeTableMatchesScalarMapping(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	shared, err := contractspec.LoadSharedTypes(filepath.Join(root, "contracts", "_meta"))
	if err != nil {
		t.Fatal(err)
	}
	for family, fam := range shared.TypeFamilies {
		for variant, ct := range fam.Types {
			spec := columnSpec{Family: family}
			if family == "integer" || family == "float" {
				bits, err := strconv.Atoi(variant)
				if err != nil {
					t.Fatalf("%s.%s: variant is not bits", family, variant)
				}
				spec.Bits = bits
			}
			if got := goScalar(spec, false); got != ct.Go {
				t.Fatalf("%s.%s: goScalar = %q, want %q", family, variant, got, ct.Go)
			}
			if got := tsScalar(spec, false); got != ct.TS {
				t.Fatalf("%s.%s: tsScalar = %q, want %q", family, variant, got, ct.TS)
			}
		}
	}
}
