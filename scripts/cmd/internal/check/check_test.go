package check

import (
	"os"
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
	}
}

func TestIsColumnType(t *testing.T) {
	ct := ColumnTypes{
		ValueTypeFamilies: map[string]ValueTypeFamily{
			"string": {}, "timestamp": {}, "integer": {}, "datetime": {}, "decimal": {}, "float": {},
		},
	}
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
		{"data.enums.account_status", false},
		{"data.enums.", false},
		{"data.tables.account.col.id", true},
		{"data.tables.account", false},
		{"data.tables.account.col.", false},
		{"data.tables.Account.col.id", false},
	}
	for _, tc := range cases {
		if got := isColumnType(tc.in, ct); got != tc.want {
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

func TestFieldOrderUsesTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/data/enums.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `schema_version: 1
summary: "x"
id: data.enums
stability: stable
enums:
  - name: x
    go_type: X
    ts_type: X
    values:
      - { code: 0, value: x }
    used_by: { entities: [], capabilities: [] }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	fields := []Field{
		{Name: "id"},
		{Name: "summary"},
		{Name: "schema_version"},
		{Name: "stability"},
		{Name: "enums", ItemFields: []Field{
			{Name: "name"}, {Name: "go_type"}, {Name: "ts_type"},
			{Name: "values", ItemFields: []Field{{Name: "value"}, {Name: "code"}}},
			{Name: "used_by", Fields: []Field{{Name: "entities"}, {Name: "capabilities"}}},
		}},
	}
	c := &Checker{root: root}
	c.checkFieldOrder("contracts/data/enums.yaml", fields, orderRules{top: "enum-key-order", nested: "enum-key-order"})
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[enum-key-order]") ||
		!strings.Contains(errs, "field summary must come before schema_version") ||
		!strings.Contains(errs, "field id must come before summary") ||
		!strings.Contains(errs, "field value must come before code") {
		t.Fatalf("template order not checked: %v", c.errs)
	}
}

func TestCheckSharedTypes(t *testing.T) {
	c := &Checker{}
	c.checkSharedTypes(&Schema{ColumnTypes: ColumnTypes{ValueTypeFamilies: map[string]ValueTypeFamily{
		"string": {
			Description: "string",
			Params:      map[string]Field{"bad": {Type: "integre"}},
		},
	}}})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.types]") {
		t.Fatalf("bad type family param not reported: %v", c.errs)
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
	}
	doc := map[string]any{
		"id":           "data.tables.x",
		"extends":      []any{"data.tables.base"},
		"owner_module": "x",
		"table":        map[string]any{"comment": "x", "size_class": "small"},
		"columns": []any{
			map[string]any{"name": "name", "type": "string"},
		},
		"constraints": []any{
			map[string]any{
				"name": "ck_x_missing",
				"type": "check",
				"expr": "missing",
			},
		},
		"indexes": []any{
			map[string]any{
				"name":    "idx_x_missing",
				"columns": []any{"missing"},
				"comment": "test",
			},
		},
	}
	c := &Checker{}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", schema)
	errs := strings.Join(c.errs, "\n")
	if strings.Count(errs, `unknown column "missing"`) != 1 {
		t.Fatalf("expected one unknown-column error, got:\n%s", errs)
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
	schema := &Schema{Targets: testTargets(), Fields: []Field{
		{Name: "schema_version", Type: "integer", Required: true, Const: 1},
		{Name: "id", Type: "string", Required: true},
		{Name: "summary", Type: "string", Required: true},
		{Name: "stability", Type: "string", Required: true, Enum: []any{"stable"}},
		{Name: "do_not", Type: "list<string>"},
		{Name: "columns", Type: "list<object>", Required: true, ItemFields: []Field{{Name: "name", Type: "string", Required: true}}},
	}}
	doc := map[string]any{
		"schema_version": 1,
		"id":             "data.tables.base",
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
		"table":        map[string]any{"comment": "x", "extends": []any{"data.tables.base"}},
		"columns": []any{
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

func TestTableRulesConstraintRequiresExpr(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{
		"name": "ck_x_code", "type": "check",
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "check requires expr") {
		t.Fatalf("check without expr not reported: %v", c.errs)
	}
}

func TestTableRulesUniqueConstraintRejected(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{
		"name": "uq_x_code", "type": "unique",
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `unknown type "unique"`) || !strings.Contains(errs, "[constraint-fields]") {
		t.Fatalf("unique constraint not rejected: %v", c.errs)
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

func TestUniqueIndexLiveOnlyOptional(t *testing.T) {
	c := &Checker{
		queryIDs:    map[string]string{"data.queries.x.q": "contracts/data/queries/x.yaml"},
		queryTables: map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true}},
	}
	doc := minimalTableDoc()
	doc["indexes"] = []any{map[string]any{
		"name": "uq_x_code", "columns": []any{"code"}, "unique": true, "comment": "test",
		"used_by": []any{"data.queries.x.q"},
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if len(c.errs) != 0 {
		t.Fatalf("unique index without live_only reported: %v", c.errs)
	}
}

func TestIndexLiveOnlyRequiresDeletedAt(t *testing.T) {
	c := &Checker{
		baseID:       "data.tables.base",
		tableDocs:    map[string]map[string]any{"data.tables.x": {"indexes": []any{map[string]any{"name": "idx_x_code", "live_only": true}}}},
		tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"code": {typ: "string"}}},
	}
	c.checkIndexLiveOnly(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[index-live-only]") {
		t.Fatalf("live_only without deleted_at not reported: %v", c.errs)
	}
}

func TestStorageRegistryAndEngine(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `default_engine: postgres
supported_engines: [postgres]
engines:
  postgres: { enabled: true }
tables:
  data.tables.x: { engine: mysql }
  data.tables.z: { size_class: small }
`
	if err := os.WriteFile(filepath.Join(dir, "storage.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkStorage(map[string]string{"data.tables.x": "x.yaml", "data.tables.y": "y.yaml"})
	errs := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"physical table data.tables.y is not registered",
		"does not match any data.tables contract",
		"engine \"mysql\" is not supported",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}
}

func TestStorageDocShape(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{
		"default_engine":    "postgres",
		"supported_engines": []any{"postgres", 1},
		"engines":           map[string]any{"postgres": map[string]any{"enabled": "false"}},
		"deploymnet":        map[string]any{},
	}
	c.checkStorageDoc(doc, "contracts/data/storage.yaml")
	errs := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"unknown top-level field \"deploymnet\"",
		"engines.postgres.enabled must be a bool",
		"supported_engines[1] must be a non-empty string",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}
	if c.storageEngineEnabled(doc, "postgres") {
		t.Fatal("non-bool enabled must not count as enabled")
	}

	c.errs = nil
	doc["supported_engines"] = "postgres"
	c.checkStorageDoc(doc, "contracts/data/storage.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "supported_engines must be a list") {
		t.Fatalf("non-list supported_engines not reported: %v", c.errs)
	}
}

func TestStorageRetentionUnitType(t *testing.T) {
	c := &Checker{}
	c.checkStorageEntry(map[string]any{}, "data.tables.x", map[string]any{
		"retention": map[string]any{"unit": 5},
	}, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "retention.unit must be a string") {
		t.Fatalf("non-string retention unit not reported: %v", c.errs)
	}
}

func TestStoragePartitionIntervalType(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"created_at": {typ: "timestamp"}},
	}}
	c.checkStorageEntry(map[string]any{}, "data.tables.x", map[string]any{
		"partition": map[string]any{"strategy": "range", "columns": []any{"created_at"}, "interval": 5},
	}, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "partition.interval must be a string") {
		t.Fatalf("non-string partition interval not reported: %v", c.errs)
	}
}

func TestStoragePartitionRangeInterval(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"created_at": {typ: "timestamp"}},
	}}
	c.checkStorageEntry(map[string]any{}, "data.tables.x", map[string]any{
		"partition": map[string]any{"strategy": "range", "columns": []any{"created_at"}},
	}, "contracts/data/storage.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "partition.interval is required for range") {
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

func TestBasePrimaryKeyNullable(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{
		"id": "data.tables.base", "summary": "base", "stability": "stable",
		"columns": []any{
			map[string]any{"name": "id", "primary_key": true, "nullable": true},
			map[string]any{"name": "status"}, map[string]any{"name": "created_at"},
			map[string]any{"name": "updated_at"}, map[string]any{"name": "deleted_at"},
		},
	}
	c.checkBaseTable(doc, &Schema{Targets: testTargets()}, "contracts/data/tables/_base.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), `base column "id" nullable must be false`) {
		t.Fatalf("nullable base primary key not reported: %v", c.errs)
	}
}

func TestPersistenceOwnership(t *testing.T) {
	c := &Checker{tableOwners: map[string]string{
		"data.tables.account":  "account",
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

func TestTableEntitiesMismatch(t *testing.T) {
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
	write("contracts/data/tables/x.yaml", "id: data.tables.x\ntable:\n  entities: [ data.entities.wrong ]\nused_by:\n  queries: []\n")
	write("contracts/data/entities/e.yaml", "id: data.entities.e\nmodel:\n  tables: [ data.tables.x ]\n")
	c := &Checker{root: root}
	c.checkUsedBy(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[table-entities]") {
		t.Fatalf("table.entities mismatch not reported: %v", c.errs)
	}
}

func TestUsedByEntitiesAndQueries(t *testing.T) {
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
  - name: GetX
    tables: [ data.tables.x ]
`)
	write("contracts/data/tables/x.yaml", `
id: data.tables.x
table:
  entities: []
used_by:
  queries: [ data.queries.q.get_x ]
`)
	tableIDs := map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"}

	c := &Checker{root: root}
	c.checkUsedBy(tableIDs)
	if len(c.errs) != 0 {
		t.Fatalf("query item ID not accepted: %v", c.errs)
	}

	write("contracts/data/tables/x.yaml", `
id: data.tables.x
table:
  entities: []
used_by:
  queries: [ data.queries.q ]
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
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[column-type]") {
		t.Fatalf("table enum column not rejected: %v", c.errs)
	}
}

func TestStatusMustNotBeRedeclared(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["columns"] = []any{
		map[string]any{"name": "status", "type": "integer"},
		map[string]any{"name": "code", "type": "string"},
	}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "[no-base-columns]") {
		t.Fatalf("status redeclare not rejected: %v", c.errs)
	}
}

func TestEnumItemIDRejected(t *testing.T) {
	body := `
id: data.enums
summary: "x"
schema_version: 1
stability: stable
enums:
  - id: data.enums.x_status
    name: x_status
    go_type: XStatus
    ts_type: XStatus
    values:
      - { value: active, code: 0 }
    used_by: { entities: [], capabilities: [] }
`
	c := writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	if !strings.Contains(strings.Join(c.errs, "\n"), `unknown field "id"`) {
		t.Fatalf("enum item id must be rejected: %v", c.errs)
	}
}

func TestEnumCodeUnique(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
id: data.enums
summary: "x"
schema_version: 1
stability: stable
enums:
  - name: x_status
    go_type: XStatus
    ts_type: XStatus
    values:
      - { value: active, code: 0 }
      - { value: disabled, code: 0 }
    used_by: { entities: [], capabilities: [] }
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

func TestEnumCodeInt32Range(t *testing.T) {
	body := `
id: data.enums
summary: "x"
schema_version: 1
stability: stable
enums:
  - name: x_status
    go_type: XStatus
    ts_type: XStatus
    values:
      - { value: active, code: 0 }
      - { value: overflow, code: 2147483648 }
      - { value: negative, code: -1 }
    used_by: { entities: [], capabilities: [] }
`
	c := writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[schema.range]") ||
		!strings.Contains(errs, "2147483648 > max 2147483647") ||
		!strings.Contains(errs, "-1 < min 0") {
		t.Fatalf("enum code range not enforced from meta: %v", c.errs)
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
id: data.enums
summary: "x"
schema_version: 1
stability: stable
enums:
  - name: x_status
    go_type: Status
    ts_type: Status
    values:
      - { value: active, code: 0 }
    used_by: { entities: [], capabilities: [] }
  - name: y_status
    go_type: Status
    ts_type: Status
    values:
      - { value: active, code: 0 }
    used_by: { entities: [], capabilities: [] }
`
	c := writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[enum-type-unique]") || !strings.Contains(errs, "duplicate go_type Status") || !strings.Contains(errs, "duplicate ts_type Status") {
		t.Fatalf("duplicate enum type names not reported: %v", c.errs)
	}
}

func TestEnumDuplicateName(t *testing.T) {
	body := `
id: data.enums
summary: "x"
schema_version: 1
stability: stable
enums:
  - name: x_status
    go_type: XStatus
    ts_type: XStatus
    values:
      - { value: active, code: 0 }
    used_by: { entities: [], capabilities: [] }
  - name: x_status
    go_type: YStatus
    ts_type: YStatus
    values:
      - { value: active, code: 0 }
    used_by: { entities: [], capabilities: [] }
`
	c := writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[schema.unique-names]") {
		t.Fatalf("duplicate enum name must use generic rule: %v", c.errs)
	}
}

func TestEnumSchemaShape(t *testing.T) {
	schema := &Schema{ID: "meta.data.enums", Fields: []Field{{
		Name: "enums", Type: "list<object>", ItemFields: []Field{
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

func TestEnumUsedByMismatch(t *testing.T) {
	body := `
id: data.enums
summary: "x"
schema_version: 1
stability: stable
enums:
  - name: account_status
    go_type: AccountStatus
    ts_type: AccountStatus
    values:
      - { value: active, code: 0 }
    used_by:
      entities: []
      capabilities: []
`
	c := writeEnumContract(t, body)
	entityPath := filepath.Join(c.root, "contracts/data/entities/account.yaml")
	if err := os.MkdirAll(filepath.Dir(entityPath), 0o755); err != nil {
		t.Fatal(err)
	}
	entity := `id: data.entities.account
model:
  name: account
  extends: [ data.entities.base ]
  tables: [ data.tables.account ]
fields:
  - { name: Status, json_name: status, type: data.enums.account_status }
`
	if err := os.WriteFile(entityPath, []byte(entity), 0o644); err != nil {
		t.Fatal(err)
	}
	c.checkEnums(loadEnumSchemaForTest(t))
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[enum-used-by]") || !strings.Contains(errs, "used_by.entities must be") {
		t.Fatalf("enum used_by mismatch not reported: %v", c.errs)
	}
}

func TestEnumCapabilityUsage(t *testing.T) {
	root := t.TempDir()
	capPath := filepath.Join(root, "contracts/capabilities/services/x.yaml")
	if err := os.MkdirAll(filepath.Dir(capPath), 0o755); err != nil {
		t.Fatal(err)
	}
	capability := `id: cap.services.x
types:
  Filter:
    type: object
    properties:
      status: { type: data.enums.x_status }
`
	if err := os.WriteFile(capPath, []byte(capability), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	u := c.enumUsages()["data.enums.x_status"]
	if u == nil || !u.capabilities["cap.services.x"] {
		t.Fatalf("capability enum usage not recorded: %#v", u)
	}
}

func TestEnumPointerUsage(t *testing.T) {
	root := t.TempDir()
	entityPath := filepath.Join(root, "contracts/data/entities/x.yaml")
	if err := os.MkdirAll(filepath.Dir(entityPath), 0o755); err != nil {
		t.Fatal(err)
	}
	entity := `id: data.entities.x
model:
  name: x
  extends: [ data.entities.base ]
  tables: [ data.tables.x ]
fields:
  - name: Status
    json_name: status
    type: "*data.enums.x_status"
    source: { column: data.tables.x.col.status }
    validation: { column: data.tables.x.col.status }
    description: "status"
`
	if err := os.WriteFile(entityPath, []byte(entity), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	usages := c.enumUsages()
	u := usages["data.enums.x_status"]
	if u == nil || !u.entities["data.entities.x"] {
		t.Fatalf("pointer enum usage not recorded: %#v", usages)
	}

	c.scanEnumTypeRefs(map[string]any{
		"fields": []any{map[string]any{"type": "*data.enums.missing"}},
	}, "x.yaml", "", map[string]map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown enum data.enums.missing") {
		t.Fatalf("pointer enum ref not checked: %v", c.errs)
	}
}

func TestEnumSummaryAndStability(t *testing.T) {
	body := `# TODO
id: data.enums
summary: ""
schema_version: 1
stability: stable
enums:
  - name: x_status
    go_type: XStatus
    ts_type: XStatus
    values:
      - { value: active, code: 0 }
    used_by:
      entities: []
      capabilities: []
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
	c.checkEnumRefs(map[string]string{}, map[string]map[string]bool{}, map[string]map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown enum data.enums.missing_status") {
		t.Fatalf("enum ref outside entity not checked: %v", c.errs)
	}

	c = &Checker{root: root}
	c.checkEnumRefs(map[string]string{}, map[string]map[string]bool{}, map[string]map[string]bool{"data.enums.missing_status": {}})
	if len(c.errs) != 0 {
		t.Fatalf("known enum ref reported: %v", c.errs)
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
	tableIDs, _ := c.checkTables(schema)
	if tableIDs["data.tables.x"] == "" {
		t.Fatalf("targets.dir/glob not used: %v", tableIDs)
	}
}

func TestErrorsHaveRuleIDs(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["table"].(map[string]any)["extends"] = []any{}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[base-only]") {
		t.Fatalf("rule id missing: %s", errs)
	}
}

func TestExtendsChainInheritsColumns(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("_base.yaml", `id: data.tables.base
summary: "base"
stability: "stable"
columns:
  - { name: id, type: string, primary_key: true, nullable: false }
  - { name: status, type: integer, nullable: false }
  - { name: created_at, type: timestamp, nullable: false }
  - { name: updated_at, type: timestamp, nullable: false }
  - { name: deleted_at, type: timestamp, nullable: true }
rules:
  - { id: base-file, text: "x", implemented_by: [base-file] }
  - { id: base-columns, text: "x", implemented_by: [base-columns] }
`)
	write("parent.yaml", `id: data.tables.parent
table:
  extends: [ data.tables.base ]
columns:
   - { name: code, type: string, nullable: false }
`)
	write("child.yaml", `id: data.tables.child
table:
  extends: [ data.tables.parent ]
columns:
   - { name: code, type: string, nullable: false }
`)
	write("grandchild.yaml", `id: data.tables.grandchild
table:
  extends: [ data.tables.child ]
columns:
   - { name: extra, type: string, nullable: false }
`)
	c := &Checker{root: root}
	_, cols := c.checkTables(&Schema{Targets: testTargets()})
	if !cols["data.tables.child"]["code"] {
		t.Fatalf("child did not inherit parent column: %v", cols["data.tables.child"])
	}
	if !cols["data.tables.grandchild"]["code"] || !cols["data.tables.grandchild"]["extra"] {
		t.Fatalf("grandchild did not inherit chain columns: %v", cols["data.tables.grandchild"])
	}
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[no-base-columns]") || !strings.Contains(errs, `column "code"`) {
		t.Fatalf("inherited column redeclare not reported: %v", c.errs)
	}
}

func TestExtendsMultipleParents(t *testing.T) {
	root := t.TempDir()
	writeTableFixture(t, root, "_base.yaml", "id: data.tables.base\nsummary: \"base\"\nstability: \"stable\"\ncolumns: []\n")
	writeTableFixture(t, root, "left.yaml", `id: data.tables.left
table:
  extends: [ data.tables.base ]
columns:
   - { name: code, type: string, nullable: false }
`)
	writeTableFixture(t, root, "right.yaml", `id: data.tables.right
table:
  extends: [ data.tables.base ]
columns:
   - { name: name, type: string, nullable: false }
`)
	writeTableFixture(t, root, "child.yaml", `id: data.tables.child
table:
  extends: [ data.tables.left, data.tables.right ]
columns:
   - { name: extra, type: string, nullable: false }
`)
	c := &Checker{root: root}
	_, cols := c.checkTables(&Schema{Targets: testTargets()})
	if !cols["data.tables.child"]["code"] || !cols["data.tables.child"]["name"] {
		t.Fatalf("child did not inherit both parents: %v", cols["data.tables.child"])
	}
	if strings.Contains(strings.Join(c.errs, "\n"), "[extends-conflict]") {
		t.Fatalf("distinct parent columns reported as conflict: %v", c.errs)
	}
}

func TestExtendsConflict(t *testing.T) {
	root := t.TempDir()
	writeTableFixture(t, root, "_base.yaml", "id: data.tables.base\nsummary: \"base\"\nstability: \"stable\"\ncolumns: []\n")
	writeTableFixture(t, root, "left.yaml", `id: data.tables.left
table:
  extends: [ data.tables.base ]
columns:
   - { name: code, type: string, nullable: false }
`)
	writeTableFixture(t, root, "right.yaml", `id: data.tables.right
table:
  extends: [ data.tables.base ]
columns:
   - { name: code, type: string, nullable: false }
`)
	writeTableFixture(t, root, "child.yaml", `id: data.tables.child
table:
  extends: [ data.tables.left, data.tables.right ]
columns:
 `)
	c := &Checker{root: root}
	c.checkTables(&Schema{Targets: testTargets()})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[extends-conflict]") {
		t.Fatalf("same-name different-origin column not reported: %v", c.errs)
	}
}

func TestExtendsDiamond(t *testing.T) {
	root := t.TempDir()
	writeTableFixture(t, root, "_base.yaml", "id: data.tables.base\nsummary: \"base\"\nstability: \"stable\"\ncolumns: []\n")
	writeTableFixture(t, root, "root.yaml", `id: data.tables.root
table:
  extends: [ data.tables.base ]
columns:
   - { name: code, type: string, nullable: false }
`)
	writeTableFixture(t, root, "left.yaml", `id: data.tables.left
table:
  extends: [ data.tables.root ]
columns:
 `)
	writeTableFixture(t, root, "right.yaml", `id: data.tables.right
table:
  extends: [ data.tables.root ]
columns:
 `)
	writeTableFixture(t, root, "child.yaml", `id: data.tables.child
table:
  extends: [ data.tables.left, data.tables.right ]
columns:
 `)
	c := &Checker{root: root}
	_, cols := c.checkTables(&Schema{Targets: testTargets()})
	if !cols["data.tables.child"]["code"] {
		t.Fatalf("diamond inheritance lost root column: %v", cols["data.tables.child"])
	}
	if strings.Contains(strings.Join(c.errs, "\n"), "[extends-conflict]") {
		t.Fatalf("diamond inheritance must not conflict: %v", c.errs)
	}
}

func writeTableFixture(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtendsCycle(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("_base.yaml", "id: data.tables.base\nsummary: \"base\"\nstability: \"stable\"\ncolumns: []\n")
	write("g.yaml", `id: data.tables.g
table:
  extends: [ data.tables.base ]
columns:
   - { name: gcol, type: string, nullable: false }
`)
	write("a.yaml", `id: data.tables.a
table:
  extends: [ data.tables.b, data.tables.g ]
columns:
   - { name: acol, type: string, nullable: false }
`)
	write("b.yaml", `id: data.tables.b
table:
  extends: [ data.tables.a ]
columns:
   - { name: bcol, type: string, nullable: false }
`)
	write("c.yaml", `id: data.tables.c
table:
  extends: [ data.tables.b ]
columns:
 `)
	c := &Checker{root: root}
	_, cols := c.checkTables(&Schema{Targets: testTargets()})
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[extends-ref]") {
		t.Fatalf("extends cycle not reported: %v", c.errs)
	}
	if !strings.Contains(errs, "data.tables.a -> data.tables.b -> data.tables.a") {
		t.Fatalf("cycle path not reported: %v", c.errs)
	}
	if !cols["data.tables.c"]["id"] || !cols["data.tables.c"]["deleted_at"] {
		t.Fatalf("downstream table lost base columns: %v", cols["data.tables.c"])
	}
	if !cols["data.tables.c"]["acol"] || !cols["data.tables.c"]["bcol"] || !cols["data.tables.c"]["gcol"] {
		t.Fatalf("downstream table lost non-base ancestor columns: %v", cols["data.tables.c"])
	}
}

func TestExtendsUnknownParent(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/tables")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("_base.yaml", "id: data.tables.base\nsummary: \"base\"\nstability: \"stable\"\ncolumns: []\n")
	write("x.yaml", "id: data.tables.x\ntable:\n  extends: [ data.tables.missing ]\n")
	c := &Checker{root: root}
	c.checkTables(&Schema{Targets: testTargets()})
	if !strings.Contains(strings.Join(c.errs, "\n"), `extends "data.tables.missing" is not a data.tables contract`) {
		t.Fatalf("unknown extend parent not reported: %v", c.errs)
	}
}

func TestExtendsCycleRedeclare(t *testing.T) {
	root := t.TempDir()
	writeTableFixture(t, root, "_base.yaml", "id: data.tables.base\nsummary: \"base\"\nstability: \"stable\"\ncolumns: []\n")
	writeTableFixture(t, root, "a.yaml", `id: data.tables.a
table:
  extends: [ data.tables.b ]
columns:
   - { name: x, type: string, nullable: false }
`)
	writeTableFixture(t, root, "b.yaml", `id: data.tables.b
table:
  extends: [ data.tables.a ]
columns:
   - { name: x, type: string, nullable: false }
`)
	c := &Checker{root: root}
	c.checkTables(&Schema{Targets: testTargets()})
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[no-base-columns]") || !strings.Contains(errs, `column "x"`) {
		t.Fatalf("cycle redeclare not reported: %v", c.errs)
	}
}

func TestExtendsScalarIsTypeErrorNotBaseOnly(t *testing.T) {
	schema := &Schema{Targets: testTargets(), Fields: []Field{
		{Name: "table", Type: "object", Fields: []Field{{Name: "extends", Type: "list<string>"}}},
	}}
	doc := map[string]any{"id": "data.tables.x", "table": map[string]any{"extends": "data.tables.base"}}
	c := &Checker{}
	c.checkPhysicalTable(doc, "contracts/data/tables/x.yaml", schema)
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[schema.type]") {
		t.Fatalf("scalar extends type error not reported: %v", c.errs)
	}
	if strings.Contains(errs, "[base-only]") {
		t.Fatalf("scalar extends must not be reported as empty: %v", c.errs)
	}
}

func TestDoNotSpecificityUsesInheritedColumns(t *testing.T) {
	root := t.TempDir()
	writeTableFixture(t, root, "_base.yaml", "id: data.tables.base\nsummary: \"base\"\nstability: \"stable\"\ncolumns: []\n")
	writeTableFixture(t, root, "parent.yaml", `id: data.tables.parent
table:
  extends: [ data.tables.base ]
columns:
   - { name: code, type: string, nullable: false }
`)
	writeTableFixture(t, root, "child.yaml", `id: data.tables.child
table:
  extends: [ data.tables.parent ]
do_not:
  - "code 必须小写"
columns:
 `)
	c := &Checker{root: root}
	c.checkTables(&Schema{Targets: testTargets()})
	if strings.Contains(strings.Join(c.errs, "\n"), "[do-not-specific]") {
		t.Fatalf("inherited column must count as a specific token: %v", c.errs)
	}
}

func TestMissingBaseKeepsPhysicalCols(t *testing.T) {
	root := t.TempDir()
	writeTableFixture(t, root, "x.yaml", "id: data.tables.x\n")
	c := &Checker{root: root}
	_, cols := c.checkTables(&Schema{Targets: testTargets()})
	if !cols["data.tables.x"]["id"] || !cols["data.tables.x"]["deleted_at"] {
		t.Fatalf("missing base must not erase physical base columns: %v", cols["data.tables.x"])
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
		"name":       "fk_account_x",
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

func TestTargetID(t *testing.T) {
	targets := testTargets()
	if got := targets.FileID("note.yaml"); got != "data.tables.note" {
		t.Fatalf("template id = %q", got)
	}
	if got := targets.FileID("_base.yaml"); got != "data.tables._base" {
		t.Fatalf("base file must not get a special id, got %q", got)
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
	c.checkTables(&Schema{Targets: testTargets()})
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
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.unique-names]") {
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
	c.checkTables(&Schema{Targets: testTargets()})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.contract-id-unique]") {
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

func TestBaseFileMustBeFixed(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{
		"id": "data.tables.base", "summary": "base", "stability": "stable",
		"columns": []any{
			map[string]any{"name": "id"}, map[string]any{"name": "status"},
			map[string]any{"name": "created_at"}, map[string]any{"name": "updated_at"},
			map[string]any{"name": "deleted_at"},
		},
	}
	c.checkBaseTable(doc, &Schema{Targets: testTargets()}, "contracts/data/tables/foo.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "base contract must live in _base.yaml") {
		t.Fatalf("base file location not reported: %v", c.errs)
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
	c.checkTables(&Schema{Targets: testTargets()})
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
	for _, id := range []string{"storage-registry", "constraint-fields", "index-definition", "schema.targets"} {
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
			"name": "idx_x_code", "columns": []any{"code"}, "comment": "test", "used_by": usedBy,
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

}

func TestRedundancyValidation(t *testing.T) {
	valid := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "string"}},
		},
		tableDocs: map[string]map[string]any{
			"data.tables.a": {"columns": []any{map[string]any{"name": "x", "redundancy": "g"}}},
			"data.tables.b": {"columns": []any{map[string]any{"name": "x", "redundancy": "g"}}},
		},
	}
	valid.checkRedundancyGroups(map[string]any{"redundancy_groups": map[string]any{
		"g": map[string]any{"source": "data.tables.a.col.x", "copies": []any{"data.tables.b.col.x"}},
	}}, "contracts/data/storage.yaml")
	valid.checkRedundancy(map[string]string{
		"data.tables.a": "contracts/data/tables/a.yaml",
		"data.tables.b": "contracts/data/tables/b.yaml",
	})
	if len(valid.errs) != 0 {
		t.Fatalf("valid redundancy group reported: %v", valid.errs)
	}

	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}, "no_copy": {typ: "string"}},
			"data.tables.b": {"x": {typ: "integer"}},
		},
		tableDocs: map[string]map[string]any{
			"data.tables.a": {"columns": []any{
				map[string]any{"name": "x", "redundancy": "g"},
				map[string]any{"name": "no_copy", "redundancy": "g2"},
			}},
			"data.tables.b": {"columns": []any{map[string]any{"name": "x", "redundancy": "g"}}},
		},
	}
	c.checkRedundancyGroups(map[string]any{"redundancy_groups": map[string]any{
		"g":  map[string]any{"source": "data.tables.a.col.x", "copies": []any{"data.tables.b.col.x"}},
		"g2": map[string]any{"source": "data.tables.a.col.no_copy"},
		"g3": map[string]any{"source": "data.tables.a.col.missing", "copies": []any{"data.tables.b.col.x"}},
	}}, "contracts/data/storage.yaml")
	c.checkRedundancy(map[string]string{
		"data.tables.a": "contracts/data/tables/a.yaml",
		"data.tables.b": "contracts/data/tables/b.yaml",
	})
	errs := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"type/nullable",
		"copies must list at least one copy",
		"is not a table column",
		"not defined in storage.redundancy_groups",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in redundancy errors: %v", want, c.errs)
		}
	}
}

func TestQueryIndexRefs(t *testing.T) {
	newChecker := func() *Checker {
		return &Checker{
			queryOrder: []string{"data.queries.x.q"},
			queries: map[string]*queryInfo{
				"data.queries.x.q": {ID: "data.queries.x.q", Targets: []string{"data.tables.x"}, IndexRef: []string{"data.tables.x.pk", "data.tables.x.idx_x_code"}},
			},
			queryTables: map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true}},
			tableIndexes: map[string][]tableIndex{"data.tables.x": {
				{name: "idx_x_code", columns: []string{"code"}, usedBy: map[string]bool{"data.queries.x.q": true}},
			}},
		}
	}

	c := newChecker()
	c.checkQueryIndexRefs()
	if len(c.errs) != 0 {
		t.Fatalf("valid index ref reported: %v", c.errs)
	}

	c = newChecker()
	c.queries["data.queries.x.q"].IndexRef = nil
	c.checkQueryIndexRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "must declare at least one index_ref") {
		t.Fatalf("missing index_ref not reported: %v", c.errs)
	}

	c = newChecker()
	c.queries["data.queries.x.q"].IndexRef = []string{"data.tables.x.idx_missing"}
	c.checkQueryIndexRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not declared") {
		t.Fatalf("unknown index ref not reported: %v", c.errs)
	}

	c = newChecker()
	c.queries["data.queries.x.q"].IndexRef = []string{"data.tables.x.idx_x_code"}
	c.checkQueryIndexRefs()
	if len(c.errs) != 0 {
		t.Fatalf("explicit index ref with used_by reported: %v", c.errs)
	}

	c = newChecker()
	c.tableIndexes["data.tables.x"][0].usedBy = map[string]bool{}
	c.checkQueryIndexRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "does not list the query in used_by") {
		t.Fatalf("index ref without used_by not reported: %v", c.errs)
	}

	c = newChecker()
	c.queries["data.queries.x.q"].IndexRef = nil
	c.tableIndexes["data.tables.x"] = []tableIndex{
		{name: "idx_x_code", columns: []string{"code"}, usedBy: map[string]bool{"data.queries.x.q": true}},
	}
	c.checkQueryIndexRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "lists the query but index_ref does not") {
		t.Fatalf("reverse index ref mismatch not reported: %v", c.errs)
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
		"id": "data.tables.base", "summary": "base", "stability": "stable",
		"columns": []any{
			map[string]any{"name": "id", "type": "string", "primary_key": true},
			map[string]any{"name": "status", "type": "integer"},
			map[string]any{"name": "created_at", "type": "timestamp"},
			map[string]any{"name": "updated_at", "type": "timestamp"},
			map[string]any{"name": "deleted_at", "type": "timestamp"},
		},
	}
	c.checkBaseTable(doc, &Schema{Targets: testTargets()}, "contracts/data/tables/_base.yaml")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `base column "id" must be integer`) {
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
	}
	c.collectTableObjects(map[string]any{
		"constraints": []any{map[string]any{"name": "ck_a_code", "type": "check"}},
		"indexes":     []any{map[string]any{"name": "idx_a_code"}},
	}, "contracts/data/tables/a.yaml", "data.tables.a")
	if !c.tableObjects["data.tables.a"]["ck_a_code"] || !c.tableObjects["data.tables.a"]["idx_a_code"] {
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
