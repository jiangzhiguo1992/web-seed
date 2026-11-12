package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIsColumnType(t *testing.T) {
	ct := ColumnTypes{
		ValueTypeFamilies: map[string]ValueTypeFamily{
			"string": {}, "timestamp": {}, "integer": {}, "datetime": {}, "decimal": {}, "float": {},
		},
	}
	refPrefixes := []string{"data.enums.", "data.tables."}
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
		if got := isColumnType(tc.in, ct, refPrefixes); got != tc.want {
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
	if !strings.Contains(strings.Join(c.errs, "\n"), "stable contract contains TODO") {
		t.Fatalf("stable TODO not reported: %v", c.errs)
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
			map[string]any{"name": "status", "type": "data.enums.x_status"},
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
		if got := isBaseTableRef(tc.ref); got != tc.want {
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
	schema := &Schema{Fields: []Field{
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
			map[string]any{"name": "status", "type": "data.enums.x_status"},
			map[string]any{"name": "code", "type": "string"},
		},
	}
}

func minimalTableSchema() *Schema {
	return &Schema{
		ColumnTypes: ColumnTypes{ValueTypeFamilies: map[string]ValueTypeFamily{"string": {}}},
		ColumnRefs:  ColumnRefs{Prefixes: []string{"data.enums."}},
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
	if !strings.Contains(errs, `unknown type "foreign_key"`) {
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
	c.checkBaseTable(doc, &Schema{}, "contracts/data/tables/foo.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "base table must live in _base.yaml") {
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
	c.checkBaseTable(doc, &Schema{}, "contracts/data/tables/_base.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "primary_key column must not be nullable") {
		t.Fatalf("nullable base primary key not reported: %v", c.errs)
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

func TestEnumDefaultAndConstraintMatch(t *testing.T) {
	root := t.TempDir()
	tableFile := filepath.Join(root, "contracts/data/tables/x.yaml")
	if err := os.MkdirAll(filepath.Dir(tableFile), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
columns:
  - name: status
    type: data.enums.x_status
    default: missing
  - name: code
    type: string
constraints:
  - name: ck_x_status
    type: enum
    column: status
    enum: data.enums.y_status
  - name: ck_x_code
    type: enum
    column: code
    enum: data.enums.x_status
`
	if err := os.WriteFile(tableFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkEnumRefs(
		map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"},
		map[string]map[string]bool{"data.tables.x": {"status": true, "code": true}},
		map[string]map[string]bool{
			"data.enums.x_status": {"active": true},
			"data.enums.y_status": {"active": true},
		},
	)
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `default "missing" not in data.enums.x_status`) {
		t.Fatalf("enum default value not checked: %v", c.errs)
	}
	if !strings.Contains(errs, "does not match column status type") {
		t.Fatalf("enum constraint/column mismatch not checked: %v", c.errs)
	}
	if !strings.Contains(errs, "must be data.enums.*") {
		t.Fatalf("enum constraint on non-enum column not checked: %v", c.errs)
	}
}

func TestMetaProseIsDoubleQuoted(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkMetaQuotes()
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
	schema := &Schema{Targets: Targets{Dir: "custom/tables", Glob: "*.yaml"}}
	c := &Checker{root: root}
	tableIDs, _ := c.checkTables(schema)
	if tableIDs["data.tables.x"] == "" {
		t.Fatalf("targets.dir/glob not used: %v", tableIDs)
	}
}

func TestErrorsHaveRuleIDs(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["extends"] = []any{"other"}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[base-only]") {
		t.Fatalf("rule id missing: %s", errs)
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
