package main

import (
	"path/filepath"
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
			want: "`id` BIGINT NOT NULL",
		},
		{
			name: "unsigned int",
			col:  column{Name: "count", Type: "integer", Bits: 64, Unsigned: true, Nullable: true},
			want: "`count` BIGINT UNSIGNED",
		},
		{
			name: "varchar",
			col:  column{Name: "number", Type: "string", MaxLen: 64, Nullable: false},
			want: "`number` VARCHAR(64) NOT NULL",
		},
		{
			name: "text",
			col:  column{Name: "body", Type: "string", Nullable: true},
			want: "`body` TEXT",
		},
		{
			name: "timestamp default",
			col:  column{Name: "created_at", Type: "timestamp", DefaultSQL: "now()"},
			want: "`created_at` DATETIME(6) NOT NULL DEFAULT (now())",
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

func TestLiveOnlyIndexUsesFunctionalKey(t *testing.T) {
	idx := index{Name: "uq_account_number", Columns: []string{"number"}, Unique: true, LiveOnly: true}
	got := renderIndex("account", idx)
	if !strings.Contains(got, "CREATE UNIQUE INDEX `uq_account_number` ON `account` ((CASE WHEN `deleted_at` IS NULL THEN 1 ELSE NULL END), `number`);") {
		t.Fatalf("unexpected live_only index: %s", got)
	}

	plain := renderIndex("account", index{Name: "idx_account_number", Columns: []string{"number"}})
	if strings.Contains(plain, "CASE WHEN") {
		t.Fatalf("non-live index must not use functional key: %s", plain)
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
	if !strings.Contains(got, "CONSTRAINT `ck_verification_action_result` CHECK (result IN (1, 2, 3, 4))") {
		t.Fatalf("missing result constraint: %s", got)
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
		if ck.Name == "ck_verification_action_result" && ck.Expr == "result IN (1, 2, 3, 4)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("verification_action result constraint not loaded: %#v", action.Checks)
	}
}

func TestGeneratedFilesUpToDate(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	if err := run(root, true); err != nil {
		t.Fatal(err)
	}
}
