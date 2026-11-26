package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func TestColumnSQL(t *testing.T) {
	cases := []struct {
		name string
		col  column
		want string
	}{
		{
			name: "bigint primary key",
			col:  column{Name: "id", Type: "integer", Bits: 64, PrimaryKey: true},
			want: `"id" BIGINT NOT NULL`,
		},
		{
			name: "varchar",
			col:  column{Name: "number", Type: "string", MaxLen: 64, Nullable: false},
			want: `"number" TEXT NOT NULL CHECK (char_length("number") <= 64)`,
		},
		{
			name: "text",
			col:  column{Name: "body", Type: "string", Nullable: true},
			want: `"body" TEXT`,
		},
		{
			name: "timestamp default",
			col:  column{Name: "created_at", Type: "timestamp", DefaultSQL: "now()"},
			want: `"created_at" TIMESTAMPTZ(6) NOT NULL DEFAULT (now())`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := columnSQL(tc.col)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("columnSQL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderVerificationResultConstraint(t *testing.T) {
	tbl := table{
		ID:   "data.tables.verification_action",
		Name: "verification_action",
		Columns: []column{
			{Name: "id", Type: "integer", Bits: 64, PrimaryKey: true},
			{Name: "result", Type: "integer", Bits: 32},
		},
		Checks: []check{{Name: "ck_verification_action_result", Expr: "result IN (1, 2, 3, 4)"}},
	}
	got, err := renderTable(tbl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `CONSTRAINT "ck_verification_action_result" CHECK (result IN (1, 2, 3, 4))`) {
		t.Fatalf("missing result constraint: %s", got)
	}
}

func TestRenderConstraintErrors(t *testing.T) {
	got := string(renderConstraintErrors([]table{{
		Checks: []check{
			{Name: "ck_b", OnViolation: "platform.errors.B"},
			{Name: "ck_a", OnViolation: "platform.errors.A"},
			{Name: "ck_no_error"},
		},
	}}))
	if !strings.Contains(got, `"ck_a": "platform.errors.A"`) || !strings.Contains(got, `"ck_b": "platform.errors.B"`) {
		t.Fatalf("constraint mapping missing: %s", got)
	}
	if strings.Contains(got, "ck_no_error") {
		t.Fatalf("constraint without on_violation must be omitted: %s", got)
	}
	if strings.Index(got, `"ck_a"`) > strings.Index(got, `"ck_b"`) {
		t.Fatalf("constraint mapping must be sorted: %s", got)
	}
	if !strings.Contains(got, "package generated") || !strings.Contains(got, "var ConstraintErrors") {
		t.Fatalf("generated Go shape missing: %s", got)
	}
}

func TestLoadTablesInheritance(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	targets := contractspec.Targets{Dir: "contracts/data/tables", Glob: "*.yaml", ID: "data.tables.<file>"}
	schema, _, err := contractspec.LoadSchemaByType(root, "data.tables")
	if err != nil {
		t.Fatal(err)
	}
	tables, err := loadTables(root, targets, schema.ValueTypes.CustomTypes)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]table{}
	for _, tbl := range tables {
		byName[tbl.Name] = tbl
	}
	if _, ok := byName["base"]; !ok {
		t.Fatal("base column lookup table missing")
	}
	account, ok := byName["account"]
	if !ok {
		t.Fatal("account table missing")
	}
	names := map[string]bool{}
	for _, c := range account.Columns {
		names[c.Name] = true
	}
	for _, want := range []string{"id", "status", "created_at", "updated_at", "deleted_at", "number"} {
		if !names[want] {
			t.Fatalf("account missing inherited/own column %s", want)
		}
	}
	action, ok := byName["verification_action"]
	if !ok {
		t.Fatal("verification_action table missing")
	}
	found := false
	for _, ck := range action.Checks {
		if ck.Name == "ck_verification_action_result" && ck.Expr == "result IN (1, 2, 3, 4, 5)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("verification_action result constraint not loaded: %#v", action.Checks)
	}
}

func TestColumnRefsFromContractFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"_base.yaml": `id: data.tables.base
columns:
  - { name: id, type: _types.basics.integer.64, nullable: false, primary_key: true }
`,
		"source.yaml": `id: data.tables.source
table: { name: source, extends: [ data.tables.base ] }
columns:
  - { name: code, type: _types.basics.string, nullable: false, max_len: 16 }
`,
		"ref.yaml": `id: data.tables.ref
table: { name: ref, extends: [ data.tables.base ] }
columns:
  - { name: copy_code, type: data.tables.source.col.code, nullable: true }
  - { name: base_id, type: data.tables.base.col.id, nullable: false }
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	targets := contractspec.Targets{Dir: "contracts/data/tables", Glob: "*.yaml", ID: "data.tables.<file>"}
	tables, err := loadTables(root, targets, nil)
	if err != nil {
		t.Fatal(err)
	}
	tables, err = resolveColumnRefs(tables)
	if err != nil {
		t.Fatal(err)
	}
	var ref table
	for _, tbl := range tables {
		if tbl.Name == "ref" {
			ref = tbl
		}
	}
	byName := map[string]column{}
	for _, c := range ref.Columns {
		byName[c.Name] = c
	}
	if got := byName["copy_code"]; got.Type != "string" || got.MaxLen != 16 || !got.Nullable {
		t.Fatalf("source ref = %+v", got)
	}
	if got := byName["base_id"]; got.Type != "integer" || got.Bits != 64 || got.Nullable {
		t.Fatalf("base ref = %+v", got)
	}
}

func TestStorageArchiveEntriesStayOutsideTableContracts(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	schema, _, err := contractspec.LoadSchemaByType(root, "data.tables")
	if err != nil {
		t.Fatal(err)
	}
	tables, err := loadTables(root, schema.Targets, schema.ValueTypes.CustomTypes)
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range tables {
		if strings.Contains(tbl.Name, "archive") {
			t.Fatalf("archive table %s must not be a data.tables contract", tbl.Name)
		}
	}
	engine, err := loadEngineTables(root, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	if engine["token_archive"] {
		t.Fatal("archive entry must not enter the postgres DDL table set")
	}
	if !engine["data.tables.token"] {
		t.Fatal("physical token table missing from postgres DDL table set")
	}
}

func TestRenderIncludesIndexComments(t *testing.T) {
	tables := []table{{
		ID: "data.tables.account", Name: "account", Comment: "账号",
		Columns: []column{{Name: "id", Type: "integer", Bits: 64, PrimaryKey: true}},
		Indexes: []index{{Name: "uq_account_id", Columns: []string{"id"}, Unique: true, Comment: "账号主键唯一"}},
	}}
	schemaSQL, _, err := render(tables)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(schemaSQL), `COMMENT ON INDEX "uq_account_id" IS '账号主键唯一';`) {
		t.Fatalf("index comment missing:\n%s", schemaSQL)
	}
}

func TestGeneratedFilesUpToDate(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	if err := run(root, true); err != nil {
		t.Fatal(err)
	}
}

func TestLiveOnlyIndexUsesPartialPredicate(t *testing.T) {
	idx := index{Name: "uq_account_number", Columns: []string{"number"}, Unique: true, LiveOnly: true}
	got := renderIndex("account", idx)
	want := `CREATE UNIQUE INDEX "uq_account_number" ON "account" ("number") WHERE "deleted_at" IS NULL;` + "\n"
	if got != want {
		t.Fatalf("postgres live_only index = %q, want %q", got, want)
	}
	plain := renderIndex("account", index{Name: "idx_account_number", Columns: []string{"number"}})
	if strings.Contains(plain, "WHERE") {
		t.Fatalf("non-live index must not use partial predicate: %s", plain)
	}
}

func TestTypeMapping(t *testing.T) {
	cases := []struct {
		in   column
		want string
	}{
		{column{Type: "integer", Bits: 64}, "BIGINT"},
		{column{Type: "string", MaxLen: 32}, "TEXT"},
		{column{Type: "string", Kind: "tsvector"}, "TSVECTOR"},
		{column{Type: "timestamp"}, "TIMESTAMPTZ(6)"},
		{column{Type: "boolean"}, "BOOLEAN"},
		{column{Type: "decimal", Precision: 10, Scale: 2}, "NUMERIC(10,2)"},
		{column{Type: "json"}, "JSONB"},
		{column{Type: "json", Kind: "json"}, "JSON"},
		{column{Type: "bytes"}, "BYTEA"},
		{column{Type: "bit", Kind: "bit", Length: 8}, "BIT(8)"},
		{column{Type: "string", Kind: "tsvector"}, "TSVECTOR"},
		{column{Type: "json", Kind: "json"}, "JSON"},
		{column{Type: "datetime", Mode: "date"}, "DATE"},
		{column{Type: "datetime", Mode: "time", TimezoneSet: true, Timezone: true}, "TIMETZ(6)"},
	}
	for _, tc := range cases {
		got, err := columnType(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("columnType(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestColumnRefResolution(t *testing.T) {
	tables := []table{
		{
			ID:      "data.tables.base",
			Columns: []column{{Name: "id", Type: "integer", Bits: 64}},
		},
		{
			ID:      "data.tables.source",
			Columns: []column{{Name: "id", Type: "integer", Bits: 64}},
		},
		{
			ID: "data.tables.sample",
			Columns: []column{
				{Name: "copy_id", Type: "data.tables.source.col.id", Nullable: true},
				{Name: "base_id", Type: "data.tables.base.col.id", Nullable: true},
			},
		},
	}
	resolved, err := resolveColumnRefs(tables)
	if err != nil {
		t.Fatal(err)
	}
	got, err := columnType(resolved[1].Columns[0])
	if err != nil {
		t.Fatal(err)
	}
	if got != "BIGINT" {
		t.Fatalf("column ref DDL type = %q, want BIGINT", got)
	}
	if !resolved[2].Columns[0].Nullable {
		t.Fatal("column ref must keep its own nullable flag")
	}
	baseType, err := columnType(resolved[2].Columns[1])
	if err != nil || baseType != "BIGINT" {
		t.Fatalf("base column ref = %q, %v; want BIGINT", baseType, err)
	}

	cycle := []table{{
		ID: "data.tables.cycle",
		Columns: []column{
			{Name: "a", Type: "data.tables.cycle.col.b"},
			{Name: "b", Type: "data.tables.cycle.col.a"},
		},
	}}
	if _, err := resolveColumnRefs(cycle); err == nil || !strings.Contains(err.Error(), "cyclic column ref") {
		t.Fatalf("cycle must be rejected, got %v", err)
	}
}

func TestBitLengthAndFractionalSecondsParsed(t *testing.T) {
	cols, err := parseColumns([]any{
		map[string]any{"name": "flags", "type": "_types.basics.bit", "kind": "bit", "length": 8},
		map[string]any{"name": "at", "type": "_types.basics.timestamp.long", "fractional_seconds": 0},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bitType, err := columnType(cols[0])
	if err != nil {
		t.Fatal(err)
	}
	if bitType != "BIT(8)" {
		t.Fatalf("bit length = %q, want BIT(8)", bitType)
	}
	tsType, err := columnType(cols[1])
	if err != nil {
		t.Fatal(err)
	}
	if tsType != "TIMESTAMP(0)" {
		t.Fatalf("fractional_seconds 0 = %q, want TIMESTAMP(0)", tsType)
	}
}

func TestIndexCommentRendered(t *testing.T) {
	got, err := renderTable(table{
		Name:    "sample",
		Columns: []column{{Name: "id", Type: "integer", Bits: 64}},
		Indexes: []index{{Name: "idx_sample_id", Columns: []string{"id"}, Comment: "索引用途"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `COMMENT ON INDEX "idx_sample_id" IS '索引用途';`) {
		t.Fatalf("index comment missing: %s", got)
	}
}

func TestColumnSQLAutoIncrement(t *testing.T) {
	cases := map[string]string{
		"16": `"id" SMALLSERIAL NOT NULL`,
		"32": `"id" SERIAL NOT NULL`,
		"64": `"id" BIGSERIAL NOT NULL`,
	}
	for variant, want := range cases {
		cols, err := parseColumns([]any{map[string]any{
			"name":           "id",
			"type":           "_types.basics.integer." + variant,
			"auto_increment": true,
			"nullable":       false,
		}}, nil)
		if err != nil {
			t.Fatalf("integer.%s parse: %v", variant, err)
		}
		got, err := columnSQL(cols[0])
		if err != nil {
			t.Fatalf("integer.%s DDL: %v", variant, err)
		}
		if got != want {
			t.Fatalf("integer.%s auto increment DDL = %q, want %q", variant, got, want)
		}
	}

	if _, err := columnSQL(column{Name: "id", Type: "integer", Bits: 64, AutoIncrement: true, Default: 1}); err == nil {
		t.Fatal("auto_increment with default must be rejected")
	}
	if _, err := columnType(column{Name: "id", Type: "integer", Bits: 8, AutoIncrement: true}); err == nil {
		t.Fatal("integer.8 has no serial variant")
	}
}

// TestIntegerVariantMapsToPostgres 锁定类型引用的 variant 到 PostgreSQL 类型的映射；
// 位宽只来自 variant，不再有列级 bits 参数。
func TestIntegerVariantMapsToPostgres(t *testing.T) {
	wantByVariant := map[string]string{
		"8": "SMALLINT", "16": "SMALLINT", "32": "INTEGER", "64": "BIGINT",
	}
	for variant, want := range wantByVariant {
		cols, err := parseColumns([]any{map[string]any{
			"name": "x",
			"type": "_types.basics.integer." + variant,
		}}, nil)
		if err != nil {
			t.Fatalf("integer.%s parse: %v", variant, err)
		}
		got, err := columnType(cols[0])
		if err != nil {
			t.Fatalf("integer.%s DDL: %v", variant, err)
		}
		if got != want {
			t.Fatalf("integer.%s maps to %q, want %q", variant, got, want)
		}
	}
}

func TestTypeTableMatchesColumnType(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	shared, err := contractspec.LoadSharedTypes(filepath.Join(root, "contracts", "_meta"))
	if err != nil {
		t.Fatal(err)
	}
	for family, fam := range shared.TypeFamilies {
		for variant, ct := range fam.Types {
			if family == "any" {
				continue
			}
			col := column{Name: family + "." + variant, Type: family}
			switch family {
			case "integer", "float":
				bits, err := strconv.Atoi(variant)
				if err != nil {
					t.Fatalf("%s.%s: variant is not bits", family, variant)
				}
				col.Bits = bits
			case "string":
				col.MaxLen = 32
			case "decimal":
				col.Precision, col.Scale = 19, 4
			case "datetime":
				col.Mode = "datetime"
				col.TimezoneSet = true
				col.Timezone = variant == "timezone"
			case "timestamp":
				col.TimezoneSet = true
				col.Timezone = variant == "time"
			}
			if len(ct.PG) > 0 && family != "integer" && family != "float" && family != "string" && family != "decimal" && family != "datetime" && family != "timestamp" && family != "json" && family != "bytes" && family != "any" {
				col.Kind = ct.PG[0]
			}
			got, err := columnType(col)
			if err != nil {
				t.Fatalf("%s.%s: %v", family, variant, err)
			}
			base := strings.ToLower(strings.TrimSpace(strings.SplitN(got, "(", 2)[0]))
			if !slices.Contains(ct.PG, base) {
				t.Fatalf("%s.%s: %s is not in pg %v", family, variant, base, ct.PG)
			}
		}
	}
}

// TestGeneratedSchemaTypesAreDeclared 直接扫描生成后的 DDL：
// 列定义里出现的 PostgreSQL 类型必须属于 _types.yaml 某个 basics 族的 pg 集合。
func TestGeneratedSchemaTypesAreDeclared(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	shared, err := contractspec.LoadSharedTypes(filepath.Join(root, "contracts", "_meta"))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, fam := range shared.TypeFamilies {
		for _, ct := range fam.Types {
			for _, pg := range ct.PG {
				declared[strings.ToLower(strings.TrimSpace(pg))] = true
			}
		}
	}
	columnRe := regexp.MustCompile(`^\s+"[^"]+"\s+([A-Z]+(?: [A-Z]+)*)`)
	var sqlFiles []string
	if err := filepath.Walk(filepath.Join(root, "apps/api"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".sql") {
			rel, _ := filepath.Rel(root, path)
			sqlFiles = append(sqlFiles, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(sqlFiles)
	for _, rel := range sqlFiles {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(body), "\n") {
			m := columnRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			typ := m[1]
			for _, suffix := range []string{" NOT NULL", " NULL", " DEFAULT", " PRIMARY KEY", " REFERENCES", " UNIQUE", " CHECK"} {
				if i := strings.Index(typ, suffix); i >= 0 {
					typ = typ[:i]
				}
			}
			typ = strings.ToLower(strings.TrimSpace(typ))
			if !declared[typ] {
				t.Errorf("%s column line uses undeclared PostgreSQL type %q: %s", rel, typ, strings.TrimSpace(line))
			}
		}
	}
}
