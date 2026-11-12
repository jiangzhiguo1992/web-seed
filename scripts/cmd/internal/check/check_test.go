package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

func testTargets() Targets {
	return Targets{
		Dir:    "contracts/data/tables",
		Glob:   "*.yaml",
		ID:     "data.tables.<file>",
		Naming: "snake_case",
		Exception: []TargetException{
			{File: "_base.yaml", ID: "data.tables.base"},
		},
	}
}

func testBaseTargets() Targets {
	return Targets{File: "contracts/data/tables/_base.yaml", ID: "data.tables.base"}
}

func TestIsColumnType(t *testing.T) {
	ct := ColumnTypes{
		ValueTypeFamilies: map[string]ValueTypeFamily{
			"string": {}, "timestamp": {}, "integer": {}, "datetime": {}, "decimal": {}, "float": {},
		},
	}
	refs := ColumnRefs{Prefixes: []string{"data.enums.", "data.tables."}, RequireCol: []string{"data.tables."}}
	cases := []struct {
		in   string
		want bool
	}{
		{"string", true},
		{"uuid", false},
		{"varchar", false},
		{"timestamp", true},
		{"datetime", true},
		{"decimal", true},
		{"float", true},
		{"date", false},
		{"time", false},
		{"text", false},
		{"timestamptz", false},
		{"integer", true},
		{"int", false},
		{"int32", false},
		{"double", false},
		{"data.enums.account_status", true},
		{"data.enums.", false},
		{"data.tables.account.col.id", true},
		{"data.tables.account", false},
		{"data.tables.account.col.", false},
	}
	for _, tc := range cases {
		if got := isColumnType(tc.in, ct, refs); got != tc.want {
			t.Errorf("isColumnType(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestValidateFieldTypes(t *testing.T) {
	if err := validateFieldTypes([]Field{{Name: "x", Type: "uuid"}}); err == nil {
		t.Fatal("expected invalid schema type to fail")
	}
	if err := validateFieldTypes([]Field{{Name: "x", Type: "float"}}); err != nil {
		t.Fatalf("float schema type reported: %v", err)
	}
	if err := validateFieldTypes([]Field{{Name: "x", Type: "decimal"}}); err != nil {
		t.Fatalf("decimal schema type reported: %v", err)
	}
}

func TestNumericFamilyParamTypes(t *testing.T) {
	c := &Checker{}
	c.validateValue(1.5, Field{Type: "float"}, "float")
	c.validateValue(1.5, Field{Type: "decimal"}, "decimal")
	if len(c.errs) != 0 {
		t.Fatalf("numeric family param types reported: %v", c.errs)
	}
}

func TestCheckStability(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte("# TODO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: dir}
	c.checkStability(map[string]any{"stability": "stable"}, "x.yaml")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "stable contract contains TODO") {
		t.Fatalf("stable TODO not reported: %v", c.errs)
	}
	if !strings.Contains(errs, "[schema.stability]") {
		t.Fatalf("stability rule id missing: %v", c.errs)
	}

	c = &Checker{}
	c.checkStability(map[string]any{"stability": "deprecated"}, "x.yaml")
	if len(c.errs) != 0 {
		t.Fatalf("deprecated without history reported: %v", c.errs)
	}
}

func TestStabilityTransition(t *testing.T) {
	// stability 现在允许双向流转；contract-check 只校验 stable 文件的 TODO/FIXME。
	c := &Checker{}
	c.checkStability(map[string]any{"stability": "draft"}, "x.yaml")
	c.checkStability(map[string]any{"stability": "deprecated"}, "x.yaml")
	if len(c.errs) != 0 {
		t.Fatalf("non-stable stability reported: %v", c.errs)
	}
}

func TestCommonColumnFieldsExcludesFamilyParams(t *testing.T) {
	schema := &Schema{
		Fields: []Field{{
			Name: "columns", Type: "list<object>", ItemFields: []Field{
				{Name: "name", Type: "string"},
				{Name: "type", Type: "string"},
				{Name: "min_len", Type: "integer"},
				{Name: "comment", Type: "string"},
			},
		}},
		ColumnTypes: ColumnTypes{ValueTypeFamilies: map[string]ValueTypeFamily{
			"string": {Params: map[string]Field{"min_len": {Type: "integer"}}},
		}},
	}
	common := commonColumnFields(schema)
	if !common["name"] || !common["type"] || !common["comment"] {
		t.Fatalf("common fields missing: %v", common)
	}
	if common["min_len"] {
		t.Fatalf("family param leaked into common fields: %v", common)
	}
}

func TestFamilyParams(t *testing.T) {
	schema := &Schema{
		Fields: []Field{{
			Name: "columns", Type: "list<object>", ItemFields: []Field{
				{Name: "name", Type: "string"},
				{Name: "type", Type: "string"},
				{Name: "nullable", Type: "boolean"},
			},
		}},
		ColumnTypes: ColumnTypes{ValueTypeFamilies: map[string]ValueTypeFamily{
			"decimal": {Params: map[string]Field{
				"precision": {Type: "integer", Required: true, Min: 1},
				"scale":     {Type: "integer", Min: 0},
			}},
			"string": {Params: map[string]Field{
				"min_len": {Type: "integer", Min: 0},
				"max_len": {Type: "integer", Min: 0},
				"pattern": {Type: "string"},
				"fixed":   {Type: "boolean"},
			}},
		}}}

	c := &Checker{}
	c.checkFamilyParams(map[string]any{"name": "amount", "type": "decimal", "scale": 2}, "decimal", "x", 0, "amount", schema)
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `missing required param "precision"`) {
		t.Fatalf("missing required family param not reported: %s", errs)
	}

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"name": "amount", "type": "decimal", "precision": 19, "scale": 4}, "decimal", "x", 0, "amount", schema)
	if len(c.errs) != 0 {
		t.Fatalf("valid decimal params reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"name": "amount", "type": "decimal", "precision": 19, "scale": 4, "min_len": 1}, "decimal", "x", 0, "amount", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), `param "min_len" not allowed for type decimal`) {
		t.Fatalf("family-mismatched param not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"name": "nickname", "type": "string", "min_len": 10, "max_len": 2}, "string", "x", 0, "nickname", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "min_len > max_len") {
		t.Fatalf("min_len > max_len not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"name": "code", "type": "string", "pattern": "["}, "string", "x", 0, "code", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "invalid pattern") {
		t.Fatalf("invalid pattern not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkFamilyParams(map[string]any{"name": "code", "type": "string", "fixed": true}, "string", "x", 0, "code", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "fixed requires max_len") {
		t.Fatalf("fixed without max_len not reported: %v", c.errs)
	}
}

func TestValidateDocRequiredAndUnknown(t *testing.T) {
	c := &Checker{}
	fields := []Field{{Name: "id", Type: "string", Required: true}}
	c.validateDoc(map[string]any{"extra": 1}, fields, "x")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `missing required field "id"`) {
		t.Fatalf("missing required field not reported: %s", errs)
	}
	if !strings.Contains(errs, `unknown field "extra"`) {
		t.Fatalf("unknown field not reported: %s", errs)
	}
}

func TestTableRulesRejectUnknownColumn(t *testing.T) {
	schema := &Schema{
		Targets: testTargets(),
		ColumnTypes: ColumnTypes{
			ValueTypeFamilies: map[string]ValueTypeFamily{
				"string": {Params: map[string]Field{
					"min_len": {Type: "integer", Min: 0},
					"max_len": {Type: "integer", Min: 0},
				}},
			},
		},
		ColumnRefs: ColumnRefs{Prefixes: []string{"data.enums."}},
	}
	doc := map[string]any{
		"id":           "data.tables.x",
		"extends":      []any{"data.tables.base"},
		"owner_module": "x",
		"table":        map[string]any{"comment": "x", "size_class": "small"},
		"columns": []any{
			map[string]any{"name": "status", "type": "integer"},
			map[string]any{"name": "name", "type": "string"},
		},
		"constraints": []any{
			map[string]any{
				"name":    "uq_x_missing",
				"type":    "unique",
				"columns": []any{"missing"},
			},
		},
		"indexes": []any{
			map[string]any{
				"name":    "idx_x_missing",
				"columns": []any{"missing"},
				"reason":  "test",
			},
		},
	}
	c := &Checker{}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", schema)
	errs := strings.Join(c.errs, "\n")
	if strings.Count(errs, `unknown column "missing"`) != 2 {
		t.Fatalf("expected two unknown-column errors, got:\n%s", errs)
	}
}

func TestIsBaseTableRef(t *testing.T) {
	c := &Checker{baseID: "data.tables.base"}
	cases := []struct {
		ref  string
		want bool
	}{
		{"data.tables.base", true},
		{"data.tables.base.col.id", true},
		{"data.tables.account", false},
		{"data.tables.account.col.id", false},
	}
	for _, tc := range cases {
		if got := c.isBaseTableRef(tc.ref); got != tc.want {
			t.Errorf("isBaseTableRef(%q) = %v, want %v", tc.ref, got, tc.want)
		}
	}
}

func TestCheckColumnRef(t *testing.T) {
	c := &Checker{}
	tables := map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"}
	cols := map[string]map[string]bool{"data.tables.x": {"id": true}}
	checkColumnRef(c, "data.tables.x.col.id", tables, cols, "ok")
	if len(c.errs) != 0 {
		t.Fatalf("valid ref reported error: %v", c.errs)
	}
	checkColumnRef(c, "data.tables.x.col.missing", tables, cols, "bad")
	if len(c.errs) != 1 || !strings.Contains(c.errs[0], "unknown column") {
		t.Fatalf("invalid ref not reported: %v", c.errs)
	}
}

func TestBaseRequiresSummary(t *testing.T) {
	schema := &Schema{Targets: testBaseTargets(), Fields: []Field{
		{Name: "schema_version", Type: "integer", Required: true, Const: 1},
		{Name: "id", Type: "string", Required: true},
		{Name: "kind", Type: "string", Enum: []any{"base"}},
		{Name: "summary", Type: "string", Required: true},
		{Name: "stability", Type: "string", Required: true, Enum: []any{"stable"}},
		{Name: "do_not", Type: "list<string>"},
		{Name: "columns", Type: "list<object>", Required: true, ItemFields: []Field{{Name: "name", Type: "string", Required: true}}},
	}}
	doc := map[string]any{
		"schema_version": 1,
		"id":             "data.tables.base",
		"kind":           "base",
		"stability":      "stable",
		"columns": []any{
			map[string]any{"name": "id"},
			map[string]any{"name": "status"},
			map[string]any{"name": "created_at"},
			map[string]any{"name": "updated_at"},
			map[string]any{"name": "deleted_at"},
		},
	}
	c := &Checker{}
	c.checkBaseTable(doc, schema, "contracts/data/tables/_base.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), `missing required field "summary"`) {
		t.Fatalf("base missing summary not reported: %v", c.errs)
	}
}

func TestRepoContracts(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.run()
	if len(c.errs) > 0 {
		t.Fatalf("repository contracts failed:\n%s", strings.Join(c.errs, "\n"))
	}
}

func minimalTableDoc() map[string]any {
	return map[string]any{
		"id":           "data.tables.x",
		"summary":      "x",
		"stability":    "draft",
		"owner_module": "x",
		"extends":      []any{"data.tables.base"},
		"table":        map[string]any{"comment": "x", "size_class": "small"},
		"columns": []any{
			map[string]any{"name": "status", "type": "integer"},
			map[string]any{"name": "code", "type": "string"},
		},
	}
}

func minimalTableSchema() *Schema {
	return &Schema{
		Targets: testTargets(),
		ColumnTypes: ColumnTypes{ValueTypeFamilies: map[string]ValueTypeFamily{
			"string":  {},
			"integer": {},
		}},
		ColumnRefs: ColumnRefs{Prefixes: []string{"data.tables."}, RequireCol: []string{"data.tables."}},
	}
}

func TestTableRulesPrimaryKeyNullable(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["columns"] = append(doc["columns"].([]any), map[string]any{
		"name": "pk", "type": "string", "primary_key": true, "nullable": true,
	})
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "primary_key column must not be nullable") {
		t.Fatalf("nullable primary key not reported: %v", c.errs)
	}
}

func TestTableRulesConstraintFields(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{
		"name": "uq_x_code", "type": "unique", "columns": []any{"code"}, "expr": "true",
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), `field "expr" is not allowed for type unique`) {
		t.Fatalf("constraint field not narrowed by type: %v", c.errs)
	}
}

func TestTableRulesUniqueConstraint(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{
		"name": "uq_x_code", "type": "unique", "columns": []any{"code"},
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if len(c.errs) != 0 {
		t.Fatalf("valid unique constraint reported: %v", c.errs)
	}
}

func TestTableRulesUniqueConstraintRequiresColumns(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{
		"name": "uq_x_code", "type": "unique",
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "unique requires columns") {
		t.Fatalf("unique without columns not reported: %v", c.errs)
	}
}

func TestTableRulesForeignKeyRejected(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{
		"name": "fk_x_other", "type": "foreign_key",
		"columns": []any{"code"}, "references": []any{"data.tables.other.col.id"},
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "foreign_key is forbidden") {
		t.Fatalf("foreign_key not rejected: %v", c.errs)
	}
	if !strings.Contains(errs, "[no-foreign-key]") {
		t.Fatalf("foreign_key rule id missing: %v", c.errs)
	}
}

func TestTableRulesIndexOrder(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["indexes"] = []any{map[string]any{
		"name": "idx_x_code", "columns": []any{"code"}, "order": []any{"desc", "asc"}, "reason": "test",
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "order length must match columns") {
		t.Fatalf("index order length mismatch not reported: %v", c.errs)
	}
}

func TestTableRulesLargeIndexConcurrently(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["table"].(map[string]any)["size_class"] = "large"
	doc["indexes"] = []any{map[string]any{
		"name": "idx_x_code", "columns": []any{"code"}, "reason": "test", "concurrently": "false",
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "large table must not disable CONCURRENTLY") {
		t.Fatalf("large-table concurrently=false not reported: %v", c.errs)
	}
}

func TestTableRulesPartitionRangeInterval(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["table"].(map[string]any)["partition"] = map[string]any{
		"strategy": "range", "columns": []any{"created_at"},
	}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "table.partition.interval is required for range") {
		t.Fatalf("range partition without interval not reported: %v", c.errs)
	}
}

func TestValidateListAllowed(t *testing.T) {
	c := &Checker{}
	c.validateValue([]any{"desc", "sideways"}, Field{Type: "list<string>", Allowed: []any{"asc", "desc"}}, "order")
	if !strings.Contains(strings.Join(c.errs, "\n"), `"sideways" not allowed`) {
		t.Fatalf("list item not checked against allowed: %v", c.errs)
	}
}

func TestValidateIntegerMin(t *testing.T) {
	c := &Checker{}
	c.validateValue(0, Field{Type: "integer", Min: 1}, "retention")
	if !strings.Contains(strings.Join(c.errs, "\n"), "0 < min 1") {
		t.Fatalf("integer min not checked: %v", c.errs)
	}
}

func TestDoNotSpecificity(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["do_not"] = []any{"不要手写 migrations"}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "do_not[0] must mention a specific identifier") {
		t.Fatalf("generic do_not not reported: %v", c.errs)
	}

	c = &Checker{}
	doc = minimalTableDoc()
	doc["do_not"] = []any{"code 必须大写归一"}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if len(c.errs) != 0 {
		t.Fatalf("specific do_not reported: %v", c.errs)
	}
}

func TestBaseTableMustLiveInBaseFile(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{
		"id": "data.tables.base", "kind": "base", "summary": "base", "stability": "stable",
		"columns": []any{
			map[string]any{"name": "id"}, map[string]any{"name": "status"},
			map[string]any{"name": "created_at"}, map[string]any{"name": "updated_at"},
			map[string]any{"name": "deleted_at"},
		},
	}
	c.checkBaseTable(doc, &Schema{Targets: testBaseTargets()}, "contracts/data/tables/foo.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "base contract must live in _base.yaml") {
		t.Fatalf("base file location not reported: %v", c.errs)
	}
}

func TestBasePrimaryKeyNullable(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{
		"id": "data.tables.base", "kind": "base", "summary": "base", "stability": "stable",
		"columns": []any{
			map[string]any{"name": "id", "primary_key": true, "nullable": true},
			map[string]any{"name": "status"}, map[string]any{"name": "created_at"},
			map[string]any{"name": "updated_at"}, map[string]any{"name": "deleted_at"},
		},
	}
	c.checkBaseTable(doc, &Schema{Targets: testBaseTargets()}, "contracts/data/tables/_base.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), `base column "id" nullable must be false`) {
		t.Fatalf("nullable base primary key not reported: %v", c.errs)
	}
}

func TestBaseSchemaShape(t *testing.T) {
	c := &Checker{}
	c.checkBaseSchema(&Schema{ID: "meta.data.tables.base", Fields: []Field{{Name: "id", Required: true}}, Rules: []Rule{{ID: "base-file", Text: "x"}}})
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "kind must be required") || !strings.Contains(errs, "rules must declare base-columns") {
		t.Fatalf("base schema erosion not reported: %v", c.errs)
	}
}

func TestPersistenceOwnership(t *testing.T) {
	c := &Checker{tableOwners: map[string]string{
		"data.tables.account":  "accounts",
		"data.tables.auth_pwd": "auth",
	}}
	doc := map[string]any{"module": "auth"}
	p := map[string]any{
		"tables": []any{"data.tables.account", "data.tables.auth_pwd"},
	}
	c.checkPersistenceOwnership(doc, p, "contracts/capabilities/services/auth.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), `cannot write data.tables.account`) {
		t.Fatalf("cross-module write not reported: %v", c.errs)
	}
}

func TestPersistenceReadOnlyRejected(t *testing.T) {
	c := &Checker{tableOwners: map[string]string{"data.tables.auth_pwd": "auth"}}
	doc := map[string]any{"module": "auth"}
	p := map[string]any{
		"tables":           []any{"data.tables.auth_pwd"},
		"read_only_tables": []any{"data.tables.account"},
	}
	c.checkPersistenceOwnership(doc, p, "contracts/capabilities/services/auth.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "read_only_tables is not supported") {
		t.Fatalf("read_only_tables not rejected: %v", c.errs)
	}
}

func TestUsedByQueriesUseQueryIDs(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("contracts/data/queries/q.yaml", `
id: data.queries.q
queries:
  - id: data.queries.q.get_x
    table: data.tables.x
`)
	write("contracts/data/tables/x.yaml", `
id: data.tables.x
used_by:
  entities: []
  queries: [data.queries.q.get_x]
  capabilities: []
`)
	tableIDs := map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"}

	c := &Checker{root: root}
	c.checkUsedBy(tableIDs)
	if len(c.errs) != 0 {
		t.Fatalf("query item ID not accepted: %v", c.errs)
	}

	write("contracts/data/tables/x.yaml", `
id: data.tables.x
used_by:
  entities: []
  queries: [data.queries.q]
  capabilities: []
`)
	c = &Checker{root: root}
	c.checkUsedBy(tableIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "used_by.queries mismatch") {
		t.Fatalf("query file ID should not satisfy used_by: %v", c.errs)
	}
}

func TestTableEnumTypeRejected(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["columns"] = append(doc["columns"].([]any), map[string]any{
		"name": "bad", "type": "data.enums.x_status",
	})
	schema := minimalTableSchema()
	schema.ColumnRefs = ColumnRefs{Prefixes: []string{"data.tables."}, RequireCol: []string{"data.tables."}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[column-type]") {
		t.Fatalf("table enum column not rejected: %v", c.errs)
	}
}

func TestTableStatusMustBeInteger(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["columns"] = []any{
		map[string]any{"name": "status", "type": "string"},
		map[string]any{"name": "code", "type": "string"},
	}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "[status-integer]") {
		t.Fatalf("non-integer status not rejected: %v", c.errs)
	}
}

func TestEnumCodeUnique(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
schema_version: 1
id: data.enums
summary: "x"
stability: stable
enums:
  - id: data.enums.x_status
    name: x_status
    go_type: XStatus
    ts_type: XStatus
    values:
      - { value: active, code: 0 }
      - { value: disabled, code: 0 }
`
	if err := os.WriteFile(filepath.Join(dir, "enums.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(repoRoot, "contracts/_meta/data.enums.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkEnums(schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[enum-code-unique]") {
		t.Fatalf("duplicate enum code not reported: %v", c.errs)
	}
}

func loadEnumSchemaForTest(t *testing.T) *Schema {
	t.Helper()
	repoRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(repoRoot, "contracts/_meta/data.enums.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func writeEnumContract(t *testing.T, body string) *Checker {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "enums.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Checker{root: root}
}

func TestEnumTypeNamesUnique(t *testing.T) {
	body := `
schema_version: 1
id: data.enums
summary: "x"
stability: stable
enums:
  - id: data.enums.x_status
    name: x_status
    go_type: Status
    ts_type: Status
    values:
      - { value: active, code: 0 }
  - id: data.enums.y_status
    name: y_status
    go_type: Status
    ts_type: Status
    values:
      - { value: active, code: 0 }
`
	c := writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[enum-type-unique]") || !strings.Contains(errs, "duplicate go_type Status") || !strings.Contains(errs, "duplicate ts_type Status") {
		t.Fatalf("duplicate enum type names not reported: %v", c.errs)
	}
}

func TestEnumDuplicateNameAndIDRules(t *testing.T) {
	body := `
schema_version: 1
id: data.enums
summary: "x"
stability: stable
enums:
  - id: data.enums.x_status
    name: x_status
    go_type: XStatus
    ts_type: XStatus
    values:
      - { value: active, code: 0 }
  - id: data.enums.x_status
    name: x_status
    go_type: YStatus
    ts_type: YStatus
    values:
      - { value: active, code: 0 }
`
	c := writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[contract-id-unique]") || !strings.Contains(errs, "[unique-names]") {
		t.Fatalf("duplicate enum id/name must use generic rules: %v", c.errs)
	}
}

func TestEnumSchemaShape(t *testing.T) {
	schema := &Schema{ID: "meta.data.enums", Fields: []Field{{
		Name: "enums", Type: "list<object>", ItemFields: []Field{
			{Name: "id", Required: true},
			{Name: "name", Required: true},
			{Name: "values", Required: true, ItemFields: []Field{{Name: "value", Required: true}}},
		},
	}}}
	c := &Checker{}
	c.checkEnumSchema(schema)
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "enums.go_type must be required") || !strings.Contains(errs, "values.code must be required") {
		t.Fatalf("enum schema erosion not reported: %v", c.errs)
	}
}

func TestEnumSummaryAndStability(t *testing.T) {
	body := `# TODO
schema_version: 1
id: data.enums
summary: ""
stability: stable
enums:
  - id: data.enums.x_status
    name: x_status
    go_type: XStatus
    ts_type: XStatus
    values:
      - { value: active, code: 0 }
`
	c := writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[schema.non-empty]") || !strings.Contains(errs, "[schema.stability]") {
		t.Fatalf("enum summary/stability not checked: %v", c.errs)
	}
}

func TestEnumRefsScanAllContracts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/capabilities/services/x.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
id: cap.services.x
types:
  Input:
    type: object
    properties:
      status: { type: data.enums.missing_status }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkEnumRefs(map[string]string{}, map[string]map[string]bool{}, map[string]map[string]bool{}, ColumnRefs{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown enum data.enums.missing_status") {
		t.Fatalf("enum ref outside entity not checked: %v", c.errs)
	}

	c = &Checker{root: root}
	c.checkEnumRefs(map[string]string{}, map[string]map[string]bool{}, map[string]map[string]bool{"data.enums.missing_status": {}}, ColumnRefs{})
	if len(c.errs) != 0 {
		t.Fatalf("known enum ref reported: %v", c.errs)
	}
}

func TestEnumBaselineRef(t *testing.T) {
	cases := map[string]string{
		"abc":         "HEAD",
		"abc def":     "HEAD",
		"abc def ghi": "HEAD^1",
	}
	for in, want := range cases {
		if got := enumBaselineRef(in); got != want {
			t.Errorf("enumBaselineRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnumAppendOnlyUsesMergeBaseline(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeEnums := func(values string) {
		t.Helper()
		path := filepath.Join(root, "contracts/data/enums.yaml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "schema_version: 1\nid: data.enums\nsummary: \"x\"\nstability: stable\nenums:\n  - id: data.enums.x\n    name: x\n    go_type: X\n    ts_type: X\n    values:\n" + values
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit("init", "-q")
	runGit("symbolic-ref", "HEAD", "refs/heads/main")
	writeEnums("      - { value: active, code: 0 }\n")
	runGit("add", ".")
	runGit("commit", "-q", "-m", "base")
	runGit("checkout", "-q", "-b", "feature")
	writeEnums("      - { value: added, code: 1 }\n")
	runGit("commit", "-qam", "feature")
	runGit("checkout", "-q", "main")
	runGit("merge", "-q", "--no-ff", "-m", "merge", "feature")

	c := &Checker{root: root}
	c.checkEnumAppendOnly("contracts/data/enums.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "active was removed") {
		t.Fatalf("merge baseline not used: %v", c.errs)
	}
}

func TestMetaProseIsDoubleQuoted(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkContractProseQuotes()
	if len(c.errs) != 0 {
		t.Fatalf("meta prose quoting failed:\n%s", strings.Join(c.errs, "\n"))
	}
}

func TestProseQuoteCheckDetectsPlainScalar(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("description: plain\n"), &doc); err != nil {
		t.Fatal(err)
	}
	c := &Checker{}
	c.checkProseQuotes(&doc, "x")
	if len(c.errs) == 0 {
		t.Fatal("plain description not reported")
	}
}

func TestProseQuoteCheckDetectsMapping(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("description:\n  key: value\n"), &doc); err != nil {
		t.Fatal(err)
	}
	c := &Checker{}
	c.checkProseQuotes(&doc, "x")
	if !strings.Contains(strings.Join(c.errs, "\n"), "must be a double-quoted string") {
		t.Fatalf("description mapping not reported: %v", c.errs)
	}
}

func TestCheckTablesUsesTargets(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "custom/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte("id: data.tables.x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	targets := testTargets()
	targets.Dir = "custom/tables"
	schema := &Schema{Targets: targets}
	c := &Checker{root: root}
	tableIDs, _ := c.checkTables(schema, nil)
	if tableIDs["data.tables.x"] == "" {
		t.Fatalf("targets.dir/glob not used: %v", tableIDs)
	}
}

func TestErrorsHaveRuleIDs(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["extends"] = []any{}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[base-only]") {
		t.Fatalf("rule id missing: %s", errs)
	}
}

func TestBaseOnlyDoesNotDuplicateAllowed(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := loadYAML(filepath.Join(root, "contracts/data/tables/account.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc["extends"] = []any{"data.tables.other"}
	c := &Checker{}
	c.checkPhysicalTable(doc, "contracts/data/tables/account.yaml", schema)
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[schema.enum]") {
		t.Fatalf("extends allowed not enforced: %v", c.errs)
	}
	if strings.Contains(errs, "[base-only]") {
		t.Fatalf("extends value reported twice: %v", c.errs)
	}
}

func TestPersistenceQueriesExist(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("contracts/data/queries/q.yaml", `
id: data.queries.q
queries:
  - id: data.queries.q.get
    table: data.tables.x
`)
	write("contracts/capabilities/services/x.yaml", `
id: cap.services.x
module: x
persistence:
  table: data.tables.x
  queries:
    get: data.queries.q.missing
`)
	c := &Checker{root: root}
	c.checkCrossFileTableRefs(
		map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"},
		map[string]map[string]bool{"data.tables.x": {"id": true}},
	)
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown query data.queries.q.missing") {
		t.Fatalf("missing persistence query not reported: %v", c.errs)
	}
}

func TestValidateEnum(t *testing.T) {
	c := &Checker{}
	c.validateValue("weeks", Field{Type: "string", Enum: []any{"minutes", "hours", "days"}}, "retention.unit")
	if !strings.Contains(strings.Join(c.errs, "\n"), `"weeks" not in enum`) {
		t.Fatalf("enum value not checked: %v", c.errs)
	}
}

func TestTypeMetaRequiresSchemaSemantics(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/_meta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_types.yaml"), []byte("types: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkTypeMeta()
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.meta]") {
		t.Fatalf("missing schema_semantics not reported: %v", c.errs)
	}
}

func TestForeignKeyReportsOnlyNoForeignKey(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := loadYAML(filepath.Join(root, "contracts/data/tables/account.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc["constraints"] = append(asList(doc["constraints"]), map[string]any{
		"name":       "fk_accounts_x",
		"type":       "foreign_key",
		"columns":    []any{"account_id"},
		"references": []any{"data.tables.account.col.id"},
	})
	c := &Checker{}
	c.checkPhysicalTable(doc, "contracts/data/tables/account.yaml", schema)
	errs := strings.Join(c.errs, "\n")
	if strings.Count(errs, "[no-foreign-key]") != 1 {
		t.Fatalf("expected exactly one no-foreign-key error, got:\n%s", errs)
	}
	for _, bad := range []string{"[schema.unknown-field]", "[schema.pattern]", "[schema.enum]"} {
		if strings.Contains(errs, bad) {
			t.Fatalf("foreign_key produced extra %s error:\n%s", bad, errs)
		}
	}
}

func TestTypeMetaRejectsDuplicateSchemaRule(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/_meta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema_semantics: {field_keys_hard: [name], field_keys_soft: [description], type_keys: [title]}\n" +
		"schema_rules:\n  - {id: x, text: \"a\"}\n  - {id: x, text: \"b\"}\n"
	if err := os.WriteFile(filepath.Join(dir, "_types.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkTypeMeta()
	if !strings.Contains(strings.Join(c.errs, "\n"), "duplicate schema rule") {
		t.Fatalf("duplicate schema rule not reported: %v", c.errs)
	}
}

func TestStripForeignKeysDoesNotMutateOriginal(t *testing.T) {
	doc := minimalTableDoc()
	doc["constraints"] = []any{
		map[string]any{"name": "fk_x_other", "type": "foreign_key"},
		map[string]any{"name": "uq_x_code", "type": "unique", "columns": []any{"code"}},
	}
	clean := stripForeignKeys(doc)
	if len(asList(doc["constraints"])) != 2 {
		t.Fatalf("original doc mutated: %v", doc["constraints"])
	}
	if len(asList(clean["constraints"])) != 1 {
		t.Fatalf("foreign key not stripped: %v", clean["constraints"])
	}
}

func TestProseQuoteCheckDetectsList(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("do_not:\n  - plain\n"), &doc); err != nil {
		t.Fatal(err)
	}
	c := &Checker{}
	c.checkProseQuotes(&doc, "x")
	if !strings.Contains(strings.Join(c.errs, "\n"), "list items must be double-quoted") {
		t.Fatalf("unquoted list item not reported: %v", c.errs)
	}
}

func TestTargetIDAndException(t *testing.T) {
	targets := testTargets()
	if got := targets.FileID("note.yaml"); got != "data.tables.note" {
		t.Fatalf("template id = %q", got)
	}
	if got := targets.FileID("_base.yaml"); got != "data.tables.base" {
		t.Fatalf("exception id = %q", got)
	}
	if ex, ok := targets.ExceptionFor("_base.yaml"); !ok || ex.ID != "data.tables.base" {
		t.Fatalf("base exception not found: %#v", ex)
	}
}

func TestTargetNamingRejectsBadFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Bad-Name.yaml"), []byte("id: data.tables.bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkTables(&Schema{Targets: testTargets()}, nil)
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[schema.targets]") || !strings.Contains(errs, "does not match targets.naming") {
		t.Fatalf("target naming not reported: %v", c.errs)
	}
}

func TestTargetsValidation(t *testing.T) {
	c := &Checker{}
	targets := testTargets()
	targets.ID = "bad"
	targets.Naming = "camel"
	c.checkTargets(&Schema{ID: "meta.x", Targets: targets})
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "targets.id must contain <file>") {
		t.Fatalf("id template not checked: %v", c.errs)
	}
	if !strings.Contains(errs, "targets.naming must be snake_case") {
		t.Fatalf("naming not checked: %v", c.errs)
	}
}

func TestDefaultRules(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["columns"] = append(doc["columns"].([]any), map[string]any{
		"name": "bad", "type": "string", "default": 1,
	})
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "[default-type]") {
		t.Fatalf("default type not checked: %v", c.errs)
	}

	c = &Checker{}
	doc = minimalTableDoc()
	doc["columns"] = append(doc["columns"].([]any), map[string]any{
		"name": "both", "type": "string", "default": "x", "default_sql": "now()",
	})
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "[default-vs-default-sql]") {
		t.Fatalf("default/default_sql exclusivity not checked: %v", c.errs)
	}
}

func TestUniqueColumnNames(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["columns"] = append(doc["columns"].([]any), map[string]any{
		"name": "code", "type": "string",
	})
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "[unique-names]") {
		t.Fatalf("duplicate column name not checked: %v", c.errs)
	}
}

func TestContractIDUnique(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte("id: data.tables.x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &Checker{root: root}
	c.checkTables(&Schema{Targets: testTargets()}, nil)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[contract-id-unique]") {
		t.Fatalf("duplicate contract id not checked: %v", c.errs)
	}
}

func TestRulesShape(t *testing.T) {
	c := &Checker{}
	c.checkRules(&Schema{ID: "meta.x", Rules: []Rule{
		{ID: "a", Text: "x"},
		{ID: "a", Text: ""},
	}})
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[schema.rules]") || !strings.Contains(errs, "duplicate rule id") {
		t.Fatalf("rules shape not checked: %v", c.errs)
	}
}

func TestTargetsDuplicateException(t *testing.T) {
	c := &Checker{}
	targets := testTargets()
	targets.Exception = append(targets.Exception, targets.Exception[0])
	c.checkTargets(&Schema{ID: "meta.x", Targets: targets})
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "duplicate targets.exception file") {
		t.Fatalf("duplicate exception file not reported: %v", c.errs)
	}
}

func TestMissingBaseContract(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte("id: data.tables.x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	baseSchema := &Schema{ID: "meta.data.tables.base", Targets: testBaseTargets()}
	c.checkTables(&Schema{Targets: testTargets()}, baseSchema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "missing base contract") {
		t.Fatalf("missing base contract not reported: %v", c.errs)
	}
}

func TestColumnRefRequiresCol(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["columns"] = append(doc["columns"].([]any),
		map[string]any{"name": "bad", "type": "data.tables.x"},
		map[string]any{"name": "good", "type": "data.tables.x.col.id"},
	)
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "(bad): unsupported type") || strings.Contains(errs, "(good): unsupported type") {
		t.Fatalf("require_col not applied correctly: %v", c.errs)
	}
}

func TestCheckRulesImplementedBy(t *testing.T) {
	c := &Checker{}
	c.checkRules(&Schema{ID: "meta.x", Rules: []Rule{
		{ID: "base-only", Text: "x", ImplementedBy: []string{"base-only"}},
	}})
	if len(c.errs) != 0 {
		t.Fatalf("valid implemented_by reported: %v", c.errs)
	}
	c = &Checker{}
	c.checkRules(&Schema{ID: "meta.x", Rules: []Rule{
		{ID: "base-only", Text: "x"},
		{ID: "other", Text: "x", ImplementedBy: []string{"missing-id"}},
	}})
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "must declare implemented_by") || !strings.Contains(errs, `implemented_by "missing-id" is not emitted`) {
		t.Fatalf("implemented_by not checked: %v", c.errs)
	}
}

func TestEnumAppendOnlyRegressions(t *testing.T) {
	oldDoc := map[string]any{"enums": []any{
		map[string]any{"id": "data.enums.x", "values": []any{
			map[string]any{"value": "active", "code": 0},
			map[string]any{"value": "removed", "code": 1},
		}},
	}}
	curDoc := map[string]any{"enums": []any{
		map[string]any{"id": "data.enums.x", "values": []any{
			map[string]any{"value": "active", "code": 9},
			map[string]any{"value": "added", "code": 2},
		}},
	}}
	c := &Checker{}
	c.reportEnumRegressions(oldDoc, curDoc, "contracts/data/enums.yaml")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "removed was removed") || !strings.Contains(errs, "code changed from 0 to 9") {
		t.Fatalf("enum regressions not reported: %v", c.errs)
	}
}

func TestColumnRefsRequireColSubset(t *testing.T) {
	c := &Checker{}
	c.checkColumnRefs(&Schema{ID: "meta.x", ColumnRefs: ColumnRefs{
		Prefixes:   []string{"data.tables"},
		RequireCol: []string{"data.enums."},
	}})
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "must end with '.'") || !strings.Contains(errs, "must also appear in prefixes") {
		t.Fatalf("column_refs shape not checked: %v", c.errs)
	}
}

func TestImplementedByMustIncludeRuleID(t *testing.T) {
	c := &Checker{}
	c.checkImplementedBy("base-only", "meta.x: rules[0]", []string{"no-foreign-key"})
	if !strings.Contains(strings.Join(c.errs, "\n"), `must include rule id "base-only"`) {
		t.Fatalf("own rule id not enforced: %v", c.errs)
	}
}

func TestUndeclaredRuleIDs(t *testing.T) {
	old := knownRuleIDs
	knownRuleIDs = map[string]bool{
		"custom-rule":  true,
		"schema.load":  true,
		"schema.rules": true,
	}
	defer func() { knownRuleIDs = old }()
	c := &Checker{declaredRuleIDs: map[string]bool{}}
	c.checkUndeclaredRuleIDs()
	if !strings.Contains(strings.Join(c.errs, "\n"), `rule id "custom-rule" is emitted`) {
		t.Fatalf("undeclared rule not reported: %v", c.errs)
	}
	c = &Checker{declaredRuleIDs: map[string]bool{"custom-rule": true}}
	c.checkUndeclaredRuleIDs()
	if len(c.errs) != 0 {
		t.Fatalf("declared rule reported: %v", c.errs)
	}
}

func TestRulefRejectsUnregisteredID(t *testing.T) {
	c := &Checker{}
	c.rulef("not-a-rule", "x")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `unregistered rule id "not-a-rule"`) {
		t.Fatalf("unregistered rule id not reported: %v", c.errs)
	}
}

func TestGeneratedRuleIDsCoverDynamicCalls(t *testing.T) {
	for _, id := range []string{"partition", "constraint-fields", "index-definition", "schema.targets"} {
		if !knownRuleIDs[id] {
			t.Fatalf("generated rule_ids_gen.go missing %q", id)
		}
	}
}

func TestIndexUsedBy(t *testing.T) {
	schema := minimalTableSchema()
	base := func(usedBy any) map[string]any {
		m := minimalTableDoc()
		m["indexes"] = []any{map[string]any{
			"name": "idx_x_code", "columns": []any{"code"}, "reason": "test", "used_by": usedBy,
		}}
		return m
	}
	c := &Checker{
		queryIDs:    map[string]string{"data.queries.x.q": "contracts/data/queries/x.yaml"},
		queryTables: map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true}},
	}
	c.checkTableRules(base([]any{"data.queries.x.q"}), "contracts/data/tables/x.yaml", schema)
	if len(c.errs) != 0 {
		t.Fatalf("valid index used_by reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkTableRules(base(nil), "contracts/data/tables/x.yaml", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[index-used-by]") {
		t.Fatalf("missing index used_by not reported: %v", c.errs)
	}

	c = &Checker{queryIDs: map[string]string{}, queryTables: map[string]map[string]bool{}}
	c.checkTableRules(base([]any{"data.queries.missing.q"}), "contracts/data/tables/x.yaml", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown query") {
		t.Fatalf("unknown index query not reported: %v", c.errs)
	}

	c = &Checker{
		queryIDs:    map[string]string{"data.queries.x.q": "contracts/data/queries/x.yaml"},
		queryTables: map[string]map[string]bool{"data.queries.x.q": {"data.tables.other": true}},
	}
	c.checkTableRules(base([]any{"data.queries.x.q"}), "contracts/data/tables/x.yaml", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "does not reference table") {
		t.Fatalf("wrong-table index query not reported: %v", c.errs)
	}

	c = &Checker{
		queryIDs:    map[string]string{"data.queries.x.q": "contracts/data/queries/x.yaml"},
		queryTables: map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true}},
		querySQL:    map[string]string{"data.queries.x.q": "SELECT 1"},
	}
	c.checkTableRules(base([]any{"data.queries.x.q"}), "contracts/data/tables/x.yaml", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "SQL does not mention index columns") {
		t.Fatalf("index query SQL mismatch not reported: %v", c.errs)
	}
}

func TestRedundancyValidation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := `id: data.tables.a
columns:
  - name: x
    type: string
    redundancy: { group: g, role: source }
  - name: no_copy
    type: string
    redundancy: { group: g2, role: source }
`
	b := `id: data.tables.b
columns:
  - name: x
    type: integer
    redundancy: { group: g, role: copy, ref: data.tables.a.col.x }
  - name: no_ref
    type: string
    redundancy: { group: g3, role: copy }
`
	for name, body := range map[string]string{"a.yaml": a, "b.yaml": b} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &Checker{root: root}
	c.checkRedundancy(
		map[string]string{"data.tables.a": "contracts/data/tables/a.yaml", "data.tables.b": "contracts/data/tables/b.yaml"},
		map[string]map[string]bool{"data.tables.a": {"x": true, "no_copy": true}, "data.tables.b": {"x": true, "no_ref": true}},
	)
	errs := strings.Join(c.errs, "\n")
	for _, want := range []string{"copy type/nullable", "copy requires ref", "has a source but no copy", "has copies but no source"} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in redundancy errors: %v", want, c.errs)
		}
	}
}

func TestQueryIndexColumns(t *testing.T) {
	c := &Checker{
		queryIndexCols: map[string][][]string{
			"data.queries.x.q": {[]string{"created_at", "id"}, []string{"own_type"}},
		},
		queryTables: map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true}},
		tableIndexes: map[string][]tableIndex{"data.tables.x": {
			{name: "idx_created", columns: []string{"created_at", "id"}, usedBy: map[string]bool{"data.queries.x.q": true}},
			{name: "idx_own", columns: []string{"own_type", "own_id"}, usedBy: map[string]bool{"data.queries.x.q": true}},
		}},
		tablePrimaryKey: map[string][]string{"data.tables.x": {"id"}},
	}
	c.checkQueryIndexColumns(
		map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"},
		map[string]map[string]bool{"data.tables.x": {"id": true, "created_at": true, "own_type": true, "own_id": true}},
	)
	if len(c.errs) != 0 {
		t.Fatalf("valid query index columns reported: %v", c.errs)
	}

	c = &Checker{
		queryIndexCols:  map[string][][]string{"data.queries.x.q": {[]string{"own_type", "created_at"}}},
		queryTables:     map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true}},
		tableIndexes:    map[string][]tableIndex{},
		tablePrimaryKey: map[string][]string{"data.tables.x": {"id"}},
	}
	c.checkQueryIndexColumns(
		map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"},
		map[string]map[string]bool{"data.tables.x": {"id": true, "created_at": true, "own_type": true}},
	)
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "no index on data.tables.x starts with") {
		t.Fatalf("wrong index order not reported: %v", c.errs)
	}
}

func TestTableIDPatternApplied(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	idField, ok := contractspec.FindField(schema.Fields, "id")
	if !ok || idField.Pattern == "" {
		t.Fatal("id field pattern is missing")
	}
	c := &Checker{}
	c.validateValue("data.table.account", idField, "id")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.pattern]") {
		t.Fatalf("id pattern not applied: %v", c.errs)
	}
	c = &Checker{}
	c.validateValue("data.tables.account", idField, "id")
	if len(c.errs) != 0 {
		t.Fatalf("valid id reported: %v", c.errs)
	}
}

func TestValidateScalarAllowedAndBooleanConst(t *testing.T) {
	c := &Checker{}
	c.validateValue("x", Field{Type: "string", Allowed: []any{"a", "b"}}, "f")
	if !strings.Contains(strings.Join(c.errs, "\n"), `"x" not allowed`) {
		t.Fatalf("scalar allowed not checked: %v", c.errs)
	}

	c = &Checker{}
	c.validateValue(1, Field{Type: "integer", Allowed: []any{2, 3}}, "n")
	if !strings.Contains(strings.Join(c.errs, "\n"), "1 not allowed") {
		t.Fatalf("numeric allowed not checked: %v", c.errs)
	}

	c = &Checker{}
	c.validateValue(true, Field{Type: "boolean", Const: false}, "b")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.const]") {
		t.Fatalf("boolean const not checked: %v", c.errs)
	}
}

func TestBaseColumnShapeRejectsDrift(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{
		"id": "data.tables.base", "kind": "base", "summary": "base", "stability": "stable",
		"columns": []any{
			map[string]any{"name": "id", "type": "integer", "primary_key": true},
			map[string]any{"name": "status", "type": "integer"},
			map[string]any{"name": "created_at", "type": "timestamp"},
			map[string]any{"name": "updated_at", "type": "timestamp"},
			map[string]any{"name": "deleted_at", "type": "timestamp"},
		},
	}
	c.checkBaseTable(doc, &Schema{Targets: testBaseTargets()}, "contracts/data/tables/_base.yaml")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `base column "id" must be string`) {
		t.Fatalf("base id type drift not reported: %v", c.errs)
	}
	if !strings.Contains(errs, `base column "deleted_at" nullable must be true`) {
		t.Fatalf("base deleted_at nullability drift not reported: %v", c.errs)
	}
}

func TestCollectTableObjectsGlobalNames(t *testing.T) {
	c := &Checker{
		tableObjects: map[string]map[string]bool{},
		objectNames:  map[string]string{},
		objectKinds:  map[string]string{},
	}
	c.collectTableObjects(map[string]any{
		"constraints": []any{map[string]any{"name": "uq_a_code", "type": "unique"}},
		"indexes":     []any{map[string]any{"name": "idx_a_code"}},
	}, "contracts/data/tables/a.yaml", "data.tables.a")
	if !c.tableObjects["data.tables.a"]["uq_a_code"] || !c.tableObjects["data.tables.a"]["idx_a_code"] {
		t.Fatalf("table objects not collected: %v", c.tableObjects["data.tables.a"])
	}
	if len(c.errs) != 0 {
		t.Fatalf("first table reported: %v", c.errs)
	}

	c.collectTableObjects(map[string]any{
		"indexes": []any{map[string]any{"name": "idx_a_code"}},
	}, "contracts/data/tables/b.yaml", "data.tables.b")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[index-name-unique]") {
		t.Fatalf("global index name collision not reported: %v", c.errs)
	}
}

func TestConstraintErrorRef(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "contracts/capabilities/services/x.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
id: cap.services.x
module: x
persistence:
  table: data.tables.x
  constraint_errors:
    ck_missing: platform.errors.X
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tableIDs := map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"}
	cols := map[string]map[string]bool{"data.tables.x": {"id": true}}
	c := &Checker{
		root:         root,
		tableOwners:  map[string]string{"data.tables.x": "x"},
		tableObjects: map[string]map[string]bool{"data.tables.x": {"uq_x_code": true}},
	}
	c.checkCrossFileTableRefs(tableIDs, cols)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[constraint-error-ref]") {
		t.Fatalf("dangling constraint_errors key not reported: %v", c.errs)
	}

	c = &Checker{
		root:         root,
		tableOwners:  map[string]string{"data.tables.x": "x"},
		tableObjects: map[string]map[string]bool{"data.tables.x": {"ck_missing": true}},
	}
	c.checkCrossFileTableRefs(tableIDs, cols)
	if len(c.errs) != 0 {
		t.Fatalf("valid constraint_errors key reported: %v", c.errs)
	}
}
