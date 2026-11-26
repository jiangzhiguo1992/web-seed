package check

import (
	"fmt"
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
	ct := ValueTypes{
		TypeFamilies: map[string]ValueTypeFamily{
			"string": {}, "timestamp": {}, "integer": {}, "datetime": {}, "decimal": {}, "float": {},
		},
	}
	cases := []struct {
		in   string
		want bool
	}{
		{"_types.basics.string", true},
		{"string", false},
		{"uuid", false},
		{"varchar", false},
		{"_types.basics.timestamp", true},
		{"_types.basics.datetime.time", true},
		{"_types.basics.decimal", true},
		{"_types.basics.float", true},
		{"date", false},
		{"time", false},
		{"text", false},
		{"timestamptz", false},
		{"_types.basics.integer", true},
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

func testValueTypes() ValueTypes {
	return ValueTypes{
		TypeFamilies: map[string]ValueTypeFamily{
			"string":  {Types: map[string]contractspec.ConcreteType{"string": {}}},
			"integer": {Types: map[string]contractspec.ConcreteType{"8": {}, "16": {}, "32": {}, "64": {}}},
			"boolean": {Types: map[string]contractspec.ConcreteType{"boolean": {}}},
			"any":     {Types: map[string]contractspec.ConcreteType{"any": {}}},
			"float":   {Types: map[string]contractspec.ConcreteType{"32": {}, "64": {}}},
			"decimal": {Types: map[string]contractspec.ConcreteType{"decimal": {}}},
		},
		Structures: map[string]contractspec.Structure{"object": {}, "list": {}, "set": {}, "array": {}, "map": {}},
	}
}

func TestValidateFieldTypes(t *testing.T) {
	if err := validateFieldTypes([]Field{{Name: "x", Type: "uuid"}}, testValueTypes(), false); err == nil {
		t.Fatal("expected invalid schema type to fail")
	}
	if err := validateFieldTypes([]Field{{Name: "x", Type: "_types.basics.float"}}, testValueTypes(), false); err != nil {
		t.Fatalf("float schema type reported: %v", err)
	}
	if err := validateFieldTypes([]Field{{Name: "x", Type: "_types.basics.decimal"}}, testValueTypes(), false); err != nil {
		t.Fatalf("decimal schema type reported: %v", err)
	}
}

func TestFieldOrderUsesTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/data/enums/account.yaml")
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
	c.checkFieldOrder("contracts/data/enums/account.yaml", fields, orderRules{top: "enum-key-order", nested: "enum-key-order"})
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
	c.checkSharedTypes(&Schema{ValueTypes: ValueTypes{TypeFamilies: map[string]ValueTypeFamily{
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
	c.validateValue(1.5, Field{Type: "_types.basics.float"}, "float")
	c.validateValue(1.5, Field{Type: "_types.basics.decimal"}, "decimal")
	if len(c.errs) != 0 {
		t.Fatalf("numeric family param types reported: %v", c.errs)
	}
}

func TestCheckStability(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), []byte("# TODO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, stability := range []string{"draft", "stable", "deprecated"} {
		c := &Checker{root: dir}
		c.checkStability(map[string]any{"stability": stability}, "x.yaml")
		errs := strings.Join(c.errs, "\n")
		if !strings.Contains(errs, "contract contains TODO") {
			t.Fatalf("%s TODO not reported: %v", stability, c.errs)
		}
		if !strings.Contains(errs, "[schema.stability]") {
			t.Fatalf("stability rule id missing for %s: %v", stability, c.errs)
		}
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
			Name: "columns", Type: "_types.structures.list<_types.structures.object>", ItemFields: []Field{
				{Name: "name", Type: "_types.basics.string"},
				{Name: "type", Type: "_types.basics.string"},
				{Name: "min_len", Type: "_types.basics.integer"},
				{Name: "comment", Type: "_types.basics.string"},
			},
		}},
		ValueTypes: ValueTypes{TypeFamilies: map[string]ValueTypeFamily{
			"string": {Params: map[string]Field{"min_len": {Type: "_types.basics.integer"}}},
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
			Name: "columns", Type: "_types.structures.list<_types.structures.object>", ItemFields: []Field{
				{Name: "name", Type: "_types.basics.string"},
				{Name: "type", Type: "_types.basics.string"},
				{Name: "nullable", Type: "_types.basics.boolean"},
			},
		}},
		ValueTypes: ValueTypes{TypeFamilies: map[string]ValueTypeFamily{
			"decimal": {Params: map[string]Field{
				"precision": {Type: "_types.basics.integer", Required: true, Min: 1},
				"scale":     {Type: "_types.basics.integer", Min: 0},
			}},
			"string": {Params: map[string]Field{
				"min_len": {Type: "_types.basics.integer", Min: 0},
				"max_len": {Type: "_types.basics.integer", Min: 0},
				"pattern": {Type: "_types.basics.string"},
				"fixed":   {Type: "_types.basics.boolean"},
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
	fields := []Field{{Name: "id", Type: "_types.basics.string", Required: true}}
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
		ValueTypes: ValueTypes{
			TypeFamilies: map[string]ValueTypeFamily{
				"string": {Params: map[string]Field{
					"min_len": {Type: "_types.basics.integer", Min: 0},
					"max_len": {Type: "_types.basics.integer", Min: 0},
				}},
			},
		},
	}
	doc := map[string]any{
		"id":           "data.tables.x",
		"extends":      []any{"data.tables.base"},
		"owner_module": "x",
		"table":        map[string]any{"name": "x"},
		"columns": []any{
			map[string]any{"name": "name", "type": "string"},
		},
		"constraints": []any{
			map[string]any{
				"name": "ck_x_missing",
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
	mustContainRule(t, c.errs, "index-definition")
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
		{Name: "schema_version", Type: "_types.basics.integer", Required: true, Const: 1},
		{Name: "id", Type: "_types.basics.string", Required: true},
		{Name: "summary", Type: "_types.basics.string", Required: true},
		{Name: "stability", Type: "_types.basics.string", Required: true, Enum: []any{"stable"}},
		{Name: "do_not", Type: "_types.structures.list<_types.basics.string>"},
		{Name: "columns", Type: "_types.structures.list<_types.structures.object>", Required: true, ItemFields: []Field{{Name: "name", Type: "_types.basics.string", Required: true}}},
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
	c := &Checker{root: root, strictHTTP: true}
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
		"table":        map[string]any{"name": "x", "extends": []any{"data.tables.base"}},
		"columns": []any{
			map[string]any{"name": "code", "type": "_types.basics.string"},
		},
	}
}

func minimalTableSchema() *Schema {
	return &Schema{
		Targets: testTargets(),
		ValueTypes: ValueTypes{TypeFamilies: map[string]ValueTypeFamily{
			"string":  {},
			"integer": {},
		}},
	}
}

func TestPhysicalPrimaryKeyRejected(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["columns"] = append(doc["columns"].([]any), map[string]any{
		"name": "pk", "type": "string", "primary_key": true, "nullable": true,
	})
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[physical-primary-key]") {
		t.Fatalf("physical primary key not reported: %v", c.errs)
	}
	if strings.Contains(errs, "[primary-key-nullable]") {
		t.Fatalf("redundant primary-key-nullable reported: %v", c.errs)
	}
}

func TestTableRulesConstraintRequiresExpr(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{"name": "ck_x_code"}}
	c.validateDoc(doc, schema.Fields, "contracts/data/tables/x.yaml")
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "missing required field \"expr\"") {
		t.Fatalf("check without expr not reported: %v", c.errs)
	}
}

func TestConstraintShapeFromMeta(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	constraints, ok := contractspec.FindField(schema.Fields, "constraints")
	if !ok {
		t.Fatal("constraints field missing in meta")
	}
	c := &Checker{}
	c.validateValue([]any{map[string]any{
		"name": "ck_x_code", "type": "check", "expr": "code IS NOT NULL",
	}}, constraints, "constraints")
	if !strings.Contains(strings.Join(c.errs, "\n"), `unknown field "type"`) {
		t.Fatalf("constraint type must be rejected as unknown: %v", c.errs)
	}

	c.errs = nil
	c.validateValue([]any{map[string]any{
		"name": "ck_x_code", "expr": "code IS NOT NULL",
		"on_violation": "platform.errors.COMMON_INTERNAL",
		"used_by":      []any{"data.queries.x.q"},
	}}, constraints, "constraints")
	if len(c.errs) != 0 {
		t.Fatalf("valid constraint reported: %v", c.errs)
	}
}

func TestConstraintUsedBy(t *testing.T) {
	c := &Checker{
		queries: map[string]*queryInfo{
			"data.queries.x.read":  {ID: "data.queries.x.read", Targets: []string{"data.tables.x"}},
			"data.queries.x.write": {ID: "data.queries.x.write", Targets: []string{"data.tables.x"}, Writes: []string{"data.tables.x"}},
		},
		constraintRefs: map[string]map[string]bool{
			"data.tables.x.ck_x_code": {"data.queries.x.write": true},
		},
	}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{
		"name": "ck_x_code", "expr": "code IS NOT NULL",
		"used_by": []any{"data.queries.x.write"},
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if len(c.errs) != 0 {
		t.Fatalf("valid constraint used_by reported: %v", c.errs)
	}

	c.errs = nil
	doc["constraints"] = []any{map[string]any{
		"name": "ck_x_code", "expr": "code IS NOT NULL",
		"used_by": []any{"data.queries.x.read"},
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "does not write table") {
		t.Fatalf("read query accepted as constraint used_by: %v", c.errs)
	}
	mustContainRule(t, c.errs, "constraint-used-by")

	c.errs = nil
	doc["constraints"] = []any{map[string]any{
		"name": "ck_x_code", "expr": "code IS NOT NULL",
		"used_by": []any{"data.queries.x.missing"},
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown query") {
		t.Fatalf("missing query accepted as constraint used_by: %v", c.errs)
	}

	c.errs = nil
	c.constraintRefs = map[string]map[string]bool{}
	doc["constraints"] = []any{map[string]any{
		"name": "ck_x_code", "expr": "code IS NOT NULL",
		"used_by": []any{"data.queries.x.write"},
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "used_by must be []") {
		t.Fatalf("generated drift not reported: %v", c.errs)
	}
}

func TestConstraintOnViolationMustExist(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{
		"name": "ck_x_code", "expr": "code IS NOT NULL",
		"on_violation": "platform.errors.NOT_A_REAL_ERROR",
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown error code") {
		t.Fatalf("unknown on_violation accepted: %v", c.errs)
	}
	mustContainRule(t, c.errs, "constraint-fields")
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
		tableDocs:    map[string]map[string]any{"data.tables.x": {"indexes": []any{map[string]any{"name": "idx_x_code", "unique": true, "live_only": true}}}},
		tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"code": {typ: "string"}}},
	}
	c.checkIndexLiveOnly(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[index-live-only]") {
		t.Fatalf("live_only without deleted_at not reported: %v", c.errs)
	}
}

func TestIndexLiveOnlyRequiresUnique(t *testing.T) {
	c := &Checker{
		baseID:       "data.tables.base",
		tableDocs:    map[string]map[string]any{"data.tables.x": {"indexes": []any{map[string]any{"name": "idx_x_code", "live_only": true}}}},
		tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"deleted_at": {typ: "timestamp", nullable: true}}},
	}
	c.checkIndexLiveOnly(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), "live_only is only allowed on unique indexes") {
		t.Fatalf("non-unique live_only not reported: %v", c.errs)
	}

	c = &Checker{
		baseID:       "data.tables.base",
		tableDocs:    map[string]map[string]any{"data.tables.x": {"indexes": []any{map[string]any{"name": "uq_x_code", "unique": true, "live_only": true}}}},
		tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"deleted_at": {typ: "timestamp", nullable: true}}},
	}
	c.checkIndexLiveOnly(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if len(c.errs) != 0 {
		t.Fatalf("unique live_only reported: %v", c.errs)
	}
}

func TestStorageRegistryAndEngine(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `supported_engines: [postgres, mysql]
not_supported_topologies: [postgres, sharding]
tables:
  postgres:
    - { table: data.tables.x }
    - { table: data.tables.z }
    - { table: data.tables.z }
    - { table: data.tables.base }
    - { table: data.tables.unknown }
    - { table: data.tables.w }
  mysql:
    - { table: data.tables.w }
  mongodb: []
`
	if err := os.WriteFile(filepath.Join(dir, "storage.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, baseID: "data.tables.base"}
	c.checkStorage(map[string]string{
		"data.tables.x": "x.yaml",
		"data.tables.y": "y.yaml",
		"data.tables.z": "z.yaml",
		"data.tables.w": "w.yaml",
	}, nil)
	errs := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"physical table data.tables.y is not registered",
		"data.tables.unknown does not match any data.tables contract",
		"data.tables.base is the base contract, not a physical table",
		"tables.postgres[2]: duplicates data.tables.z",
		"table data.tables.w is registered under both mysql and postgres",
		"tables.mongodb is not a supported engine",
		"postgres cannot be both supported and not_supported",
		"[storage-registry]",
		"[storage-engine]",
		"[storage-entry-description]",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}
}

func TestStorageCacheEntries(t *testing.T) {
	entries := map[string]*storageEntry{
		"data.tables.x": {table: "data.tables.x", engine: "mysql"},
	}
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"account_id": {typ: "integer"}},
	}}
	c.checkStorageCaches(map[string]any{
		"caches": map[string]any{"redis": []any{
			map[string]any{"prefix": "x:a", "cache_of": "data.tables.x", "key": []any{"col.account_id"}},
			map[string]any{"prefix": "x:b", "cache_of": "data.tables.unknown", "key": []any{"data.tables.unknown.col.account_id"}},
			map[string]any{"prefix": "x:c", "cache_of": "data.tables.x", "key": []any{"data.tables.x.col.account_id"}, "size_class": "small"},
			map[string]any{"prefix": "x:d", "cache_of": "data.tables.x", "key": []any{"data.tables.x.col.missing"}},
		}},
	}, entries, "contracts/data/storage.yaml")
	errs := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"duplicates (table,key) data.tables.x",
		"data.tables.unknown is not a registered authority table",
		`field "size_class" is not allowed for cache entries`,
		"key data.tables.x.col.missing is not a column of data.tables.x",
		"[storage-cache-key]",
		"[storage-registry]",
		"[storage-cache-fields]",
		"[storage-retention]",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}
}

func storageSchemaForTest(t *testing.T) *contractspec.Schema {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, _, err := contractspec.LoadSchemaByType(root, "data.storage")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestStorageDocShape(t *testing.T) {
	schema := storageSchemaForTest(t)
	c := &Checker{valueTypes: schema.ValueTypes}
	rel := "contracts/data/storage.yaml"
	doc := map[string]any{
		"default_engine":    "postgres",
		"supported_engines": []any{"postgres", 1},
		"engines":           map[string]any{"postgres": map[string]any{"enabled": "false"}},
		"deploymnet":        map[string]any{},
		"size_class":        map[string]any{"data.tables.x": "small"},
		"retention":         map[string]any{},
		"partition":         map[string]any{},
	}
	validate := func() string {
		c.errs = nil
		c.validateDoc(doc, schema.Fields, rel)
		return strings.Join(c.errs, "\n")
	}
	errs := validate()
	for _, want := range []string{
		"unknown field \"deploymnet\"",
		"unknown field \"default_engine\"",
		"unknown field \"engines\"",
		"unknown field \"size_class\"",
		"unknown field \"retention\"",
		"unknown field \"partition\"",
		"must be string",
		"[schema.unknown-field]",
		"[schema.type]",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}

	doc["supported_engines"] = "postgres"
	if got := validate(); !strings.Contains(got, "must be list") || !strings.Contains(got, "[schema.type]") {
		t.Fatalf("non-list supported_engines not reported: %s", got)
	}

	doc["supported_engines"] = []any{"postgres"}
	doc["not_supported_topologies"] = "sharding"
	if got := validate(); !strings.Contains(got, "must be list") || !strings.Contains(got, "[schema.type]") {
		t.Fatalf("non-list not_supported_topologies not reported: %s", got)
	}

	doc["not_supported_topologies"] = []any{"sharding", ""}
	if got := validate(); !strings.Contains(got, "does not match") || !strings.Contains(got, "[schema.pattern]") {
		t.Fatalf("empty not_supported_topologies item not reported: %s", got)
	}

	doc["not_supported_topologies"] = []any{}
	if got := validate(); strings.Contains(got, "not_supported_topologies") {
		t.Fatalf("empty not_supported_topologies must be allowed: %s", got)
	}
}

func TestStorageDocRequiresSupportedEngines(t *testing.T) {
	schema := storageSchemaForTest(t)
	c := &Checker{valueTypes: schema.ValueTypes}
	c.validateDoc(map[string]any{"tables": map[string]any{}}, schema.Fields, "contracts/data/storage.yaml")
	if got := entityErrs(c); !strings.Contains(got, "missing required field \"supported_engines\"") || !strings.Contains(got, "[schema.required]") {
		t.Fatalf("missing supported_engines not reported: %v", c.errs)
	}

	c = &Checker{valueTypes: schema.ValueTypes}
	c.validateDoc(map[string]any{"tables": map[string]any{}, "supported_engines": []any{}}, schema.Fields, "contracts/data/storage.yaml")
	if got := entityErrs(c); !strings.Contains(got, "0 items < min_items 1") || !strings.Contains(got, "[schema.range]") {
		t.Fatalf("empty supported_engines not reported: %v", c.errs)
	}
}

func TestStorageRetentionFlatFields(t *testing.T) {
	c := &Checker{}
	c.parseStorageRetention(map[string]any{"retention": true, "ttl": 7}, "w", false)
	if got := entityErrs(c); !strings.Contains(got, "unit is required when retention is true") || !strings.Contains(got, "[storage-duration]") {
		t.Fatalf("missing unit not reported: %v", c.errs)
	}

	c = &Checker{}
	c.parseStorageRetention(map[string]any{"retention": true, "unit": "days"}, "w", false)
	if !strings.Contains(entityErrs(c), "retention requires ttl") {
		t.Fatalf("missing ttl not reported: %v", c.errs)
	}

	c = &Checker{}
	c.parseStorageRetention(map[string]any{"retention": false, "unit": "days", "ttl": 30}, "w", false)
	errs := entityErrs(c)
	if !strings.Contains(errs, "unit is only allowed when retention is true") || !strings.Contains(errs, "ttl is only allowed when retention is true") {
		t.Fatalf("disabled retention fields not reported: %v", c.errs)
	}

	c = &Checker{}
	c.parseStorageRetention(map[string]any{"retention": "yes"}, "w", false)
	if !strings.Contains(entityErrs(c), "retention must be a boolean") {
		t.Fatalf("non-bool retention not reported: %v", c.errs)
	}

	c = &Checker{}
	r := c.parseStorageRetention(map[string]any{"retention": true, "unit": "days", "ttl": 30}, "w", false)
	if len(c.errs) != 0 || r == nil || r.unit != "days" || r.ttl != 30 {
		t.Fatalf("valid retention not parsed: %#v / %v", r, c.errs)
	}
}

func TestStorageCacheRetentionFlatFields(t *testing.T) {
	authority := &storageEntry{table: "data.tables.x", retention: &storageRetention{unit: "days", ttl: 30}}

	c := &Checker{tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"account_id": {typ: "integer"}}}}
	c.checkStorageCaches(map[string]any{"caches": map[string]any{"redis": []any{
		map[string]any{"prefix": "x", "cache_of": "data.tables.x", "key": []any{"data.tables.x.col.account_id"}, "retention": true, "unit": "days", "cache_ttl": 60},
	}}}, map[string]*storageEntry{"data.tables.x": authority}, "w")
	if !strings.Contains(entityErrs(c), "cache_ttl must not exceed data.tables.x ttl") {
		t.Fatalf("cache_ttl > ttl not reported: %v", c.errs)
	}

	c = &Checker{tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"account_id": {typ: "integer"}}}}
	c.checkStorageCaches(map[string]any{"caches": map[string]any{"redis": []any{
		map[string]any{"prefix": "x", "cache_of": "data.tables.x", "key": []any{"data.tables.x.col.account_id"}, "retention": true, "unit": "hours", "cache_ttl": 720},
	}}}, map[string]*storageEntry{"data.tables.x": authority}, "w")
	if len(c.errs) != 0 {
		t.Fatalf("cache_ttl equal to ttl reported: %v", c.errs)
	}

	c = &Checker{tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"account_id": {typ: "integer"}}}}
	c.checkStorageCaches(map[string]any{"caches": map[string]any{"redis": []any{
		map[string]any{"prefix": "x", "cache_of": "data.tables.x", "key": []any{"data.tables.x.col.account_id"}, "retention": true, "unit": "days"},
	}}}, map[string]*storageEntry{"data.tables.x": authority}, "w")
	if !strings.Contains(entityErrs(c), "cache_ttl is required") {
		t.Fatalf("missing cache_ttl not reported: %v", c.errs)
	}

	c = &Checker{tableColumns: map[string]map[string]columnMeta{"data.tables.x": {"account_id": {typ: "integer"}}}}
	c.checkStorageCaches(map[string]any{"caches": map[string]any{"redis": []any{
		map[string]any{"prefix": "x", "cache_of": "data.tables.x", "key": []any{"data.tables.x.col.account_id"}, "retention": false, "unit": "days", "cache_ttl": 30},
	}}}, map[string]*storageEntry{"data.tables.x": authority}, "w")
	errs := entityErrs(c)
	if !strings.Contains(errs, "retention must be true for cache entries") || !strings.Contains(errs, "cache_ttl is only allowed when retention is true") {
		t.Fatalf("disabled cache retention not reported: %v", c.errs)
	}
}

func TestStorageArchiveEntryShape(t *testing.T) {
	c := &Checker{}
	a := c.parseStorageArchive("postgres", "data.tables.src", map[string]any{
		"archive_of": "data.tables.src", "description": "src 归档", "promotable": true, "on_conflict": "skip", "table": "src_archive",
		"age_by": "created_at", "after_unit": "days", "after": 90,
	}, "storage tables.postgres[0]")
	if len(c.errs) != 0 {
		t.Fatalf("valid archive entry reported: %v", c.errs)
	}
	if a == nil || a.name != "src_archive" || a.source != "data.tables.src" || !a.promotable || a.onConflict != "skip" || a.after != 90 {
		t.Fatalf("unexpected archive entry: %#v", a)
	}

	c = &Checker{}
	c.parseStorageArchive("postgres", "data.tables.src", map[string]any{
		"archive_of": "data.tables.src", "description": "src 归档", "promotable": true, "table": "src_archive",
		"age_by": "created_at", "after_unit": "days", "after": 90,
	}, "storage tables.postgres[0]")
	if got := entityErrs(c); !strings.Contains(got, "promotable archive requires on_conflict") || !strings.Contains(got, "[storage-archive-source]") {
		t.Fatalf("missing on_conflict not reported: %v", c.errs)
	}

	c = &Checker{}
	c.parseStorageArchive("postgres", "data.tables.src", map[string]any{
		"archive_of": "data.tables.src", "description": "src 归档", "promotable": true, "on_conflict": "merge", "table": "src_archive",
		"age_by": "created_at", "after_unit": "days", "after": 90,
	}, "storage tables.postgres[0]")
	if !strings.Contains(entityErrs(c), "on_conflict must be skip/overwrite/error") {
		t.Fatalf("invalid on_conflict not reported: %v", c.errs)
	}

	c = &Checker{}
	c.parseStorageArchive("postgres", "data.tables.src", map[string]any{
		"archive_of": "data.tables.src", "description": "src 归档", "promotable": false, "on_conflict": "skip", "table": "src_archive",
		"age_by": "created_at", "after_unit": "days", "after": 90,
	}, "storage tables.postgres[0]")
	if !strings.Contains(entityErrs(c), "on_conflict is only allowed when promotable is true") {
		t.Fatalf("non-promotable on_conflict not reported: %v", c.errs)
	}

	c = &Checker{}
	c.parseStorageArchive("postgres", "data.tables.src", map[string]any{
		"archive_of": "data.tables.src", "description": "src 归档", "table": "Archive Table",
		"age_by": "deleted_at", "after_unit": "weeks", "after": 0, "entities": []any{},
	}, "storage tables.postgres[0]")
	errs := entityErrs(c)
	for _, want := range []string{
		`field "entities" is not allowed for archive entries`,
		`archive table "Archive Table" must be snake_case`,
		`age_by must be created_at or updated_at, got "deleted_at"`,
		"after_unit must be minutes/hours/days",
		"after must be an integer >= 1",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}

	c = &Checker{}
	c.parseStorageArchive("postgres", "data.tables.src", map[string]any{
		"archive_of": "data.tables.src", "description": "src 归档", "age_by": "created_at", "after_unit": "days", "after": 90,
	}, "storage tables.postgres[0]")
	if !strings.Contains(entityErrs(c), "table is required") {
		t.Fatalf("missing archive table not reported: %v", c.errs)
	}
}

func archiveEntryChecker() (*Checker, map[string]*storageEntry, map[string]string) {
	c := &Checker{
		baseID: "data.tables.base",
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.src": {
				"id":         {typ: "integer", primaryKey: true},
				"created_at": {typ: "timestamp"},
				"updated_at": {typ: "timestamp"},
			},
		},
		tableDocs: map[string]map[string]any{
			"data.tables.src": {"table": map[string]any{"name": "src"}},
		},
	}
	registered := map[string]*storageEntry{
		"data.tables.src": {table: "data.tables.src", engine: "mysql"},
	}
	tableIDs := map[string]string{"data.tables.src": "contracts/data/tables/src.yaml"}
	return c, registered, tableIDs
}

func validArchiveEntry() storageArchiveEntry {
	return storageArchiveEntry{
		engine: "postgres", name: "src_archive", source: "data.tables.src",
		ageBy: "created_at", unit: "days", after: 90, where: "storage tables.postgres[0]",
	}
}

func TestStorageArchiveEntriesValid(t *testing.T) {
	c, registered, tableIDs := archiveEntryChecker()
	c.checkStorageArchives([]storageArchiveEntry{validArchiveEntry()}, registered, tableIDs)
	if len(c.errs) != 0 {
		t.Fatalf("valid archive entry reported: %v", c.errs)
	}
}

func TestStorageArchiveEntriesRejectUnknownSource(t *testing.T) {
	c, registered, tableIDs := archiveEntryChecker()
	a := validArchiveEntry()
	a.source = "data.tables.missing"
	c.checkStorageArchives([]storageArchiveEntry{a}, registered, tableIDs)
	if got := entityErrs(c); !strings.Contains(got, "archive_of data.tables.missing is not a registered authority table") || !strings.Contains(got, "[storage-archive-ref]") {
		t.Fatalf("unknown source not reported: %v", c.errs)
	}
}

func TestStorageArchiveEntriesRejectDuplicateSourceAndName(t *testing.T) {
	c, registered, tableIDs := archiveEntryChecker()
	registered["data.tables.src2"] = &storageEntry{table: "data.tables.src2", engine: "mysql"}
	c.tableDocs["data.tables.src2"] = map[string]any{"table": map[string]any{"name": "src2"}}
	tableIDs["data.tables.src2"] = "contracts/data/tables/src2.yaml"
	c.tableColumns["data.tables.src2"] = c.tableColumns["data.tables.src"]

	dupSource := validArchiveEntry()
	dupSource.name = "src_archive_2"
	dupName := validArchiveEntry()
	dupName.source = "data.tables.src2"
	c.checkStorageArchives([]storageArchiveEntry{validArchiveEntry(), dupSource, dupName}, registered, tableIDs)
	errs := entityErrs(c)
	for _, want := range []string{
		"data.tables.src already has archive src_archive",
		"archive table src_archive already declared by",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}
}

func TestStorageArchiveEntriesRejectPhysicalCollision(t *testing.T) {
	c, registered, tableIDs := archiveEntryChecker()
	tableIDs["data.tables.other"] = "contracts/data/tables/other.yaml"
	c.tableDocs["data.tables.other"] = map[string]any{"table": map[string]any{"name": "src_archive"}}
	c.checkStorageArchives([]storageArchiveEntry{validArchiveEntry()}, registered, tableIDs)
	if !strings.Contains(entityErrs(c), "archive table src_archive conflicts with physical table data.tables.other") {
		t.Fatalf("physical collision not reported: %v", c.errs)
	}
}

func TestStorageArchiveEntriesRequireAgeByColumn(t *testing.T) {
	c, registered, tableIDs := archiveEntryChecker()
	delete(c.tableColumns["data.tables.src"], "created_at")
	c.checkStorageArchives([]storageArchiveEntry{validArchiveEntry()}, registered, tableIDs)
	if !strings.Contains(entityErrs(c), "age_by column created_at not found on data.tables.src") {
		t.Fatalf("missing age_by column not reported: %v", c.errs)
	}
}

func TestValidateListEnum(t *testing.T) {
	c := &Checker{}
	c.validateValue([]any{"desc", "sideways"}, Field{Type: "_types.structures.list<_types.basics.string>", Enum: []any{"asc", "desc"}}, "order")
	if !strings.Contains(strings.Join(c.errs, "\n"), `"sideways" not in enum`) {
		t.Fatalf("list item not checked against enum: %v", c.errs)
	}
}

func TestValidateIntegerMin(t *testing.T) {
	c := &Checker{}
	c.validateValue(0, Field{Type: "_types.basics.integer", Min: 1}, "retention")
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
	mustContainRule(t, c.errs, "base-columns")
}

func TestHTTPCapabilityWriteLayerRejected(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/capabilities/http/x.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `id: cap.protocols.http.x
owner_module: x
persistence:
  tables: [ data.tables.x ]
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, tableOwners: map[string]string{"data.tables.x": "x"}}
	c.checkCapabilityWrites(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), "only cap.services may declare persistence/writes") {
		t.Fatalf("http write layer not rejected: %v", c.errs)
	}

	body = `id: cap.protocols.http.x
owner_module: x
writes:
  - data.queries.x.q
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c = &Checker{root: root, tableOwners: map[string]string{"data.tables.x": "x"}}
	c.checkCapabilityWrites(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), "only cap.services may declare persistence/writes") {
		t.Fatalf("http top-level writes not rejected: %v", c.errs)
	}
}

func TestPersistenceReadOnlyRejected(t *testing.T) {
	c := &Checker{}
	fields := []Field{{Name: "persistence", Type: "_types.structures.object", Required: true, Fields: []Field{
		{Name: "table", Type: "_types.basics.string"},
		{Name: "tables", Type: "_types.structures.list<_types.basics.string>"},
	}}}
	doc := map[string]any{"persistence": map[string]any{
		"tables":           []any{"data.tables.auth_pwd"},
		"read_only_tables": []any{"data.tables.account"},
	}}
	c.validateDoc(doc, fields, "cap.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.unknown-field]") {
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
	write("contracts/data/tables/x.yaml", "id: data.tables.x\ntable:\n  entities: [ data.entities.wrong ]\n  queries: []\n")
	write("contracts/data/entities/e.yaml", "id: data.entities.e\nmodel:\n  tables: [ data.tables.x ]\n")
	c := &Checker{root: root}
	c.checkUsedBy(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[table-entities]") {
		t.Fatalf("table.entities mismatch not reported: %v", c.errs)
	}
}

func TestTableEntitiesUnique(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tableField, ok := contractspec.FindField(schema.Fields, "table")
	if !ok {
		t.Fatal("table field missing")
	}
	entitiesField, ok := contractspec.FindField(tableField.Fields, "entities")
	if !ok {
		t.Fatal("table.entities field missing")
	}
	c := &Checker{}
	c.validateValue([]any{"data.entities.a", "data.entities.a"}, entitiesField, "table.entities")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.unique]") {
		t.Fatalf("duplicate table.entities not reported: %v", c.errs)
	}
}

func TestTableIndexColumnsUnique(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	indexesField, ok := contractspec.FindField(schema.Fields, "indexes")
	if !ok {
		t.Fatal("indexes field missing")
	}
	columnsField, ok := contractspec.FindField(indexesField.ItemFields, "columns")
	if !ok {
		t.Fatal("indexes.columns field missing")
	}
	c := &Checker{}
	c.validateValue([]any{"code", "code"}, columnsField, "indexes.columns")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.unique]") {
		t.Fatalf("duplicate index columns not reported: %v", c.errs)
	}
}

func TestIndexStabilityMatchesContract(t *testing.T) {
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
	write("contracts/index.yaml", `
schema_version: 1
entries:
  - { id: data.tables.x, path: data/tables/x.yaml, summary: "x", consumers: [tools], enforcement: tested, stability: stable }
`)
	write("contracts/data/tables/x.yaml", `
id: data.tables.x
stability: draft
`)
	c := &Checker{root: root}
	c.checkIndex(map[string]string{}, &Schema{}, &Schema{}, &Schema{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.index-stability]") {
		t.Fatalf("index stability mismatch not reported: %v", c.errs)
	}

	c.errs = nil
	write("contracts/index.yaml", `
schema_version: 1
entries:
  - { id: data.tables.x, path: data/tables/x.yaml, summary: "x", consumers: [tools], enforcement: tested, stability: 1 }
`)
	c.checkIndex(map[string]string{}, &Schema{}, &Schema{}, &Schema{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "stability must be stable/draft/deprecated") {
		t.Fatalf("invalid index stability not reported: %v", c.errs)
	}
}

type scaffoldRenderCase struct {
	typ     string
	fullDoc bool
	render  func(*contractspec.Schema) ([]byte, error)
}

func scaffoldRenderCases() []scaffoldRenderCase {
	return []scaffoldRenderCase{
		{"data.tables", true, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderTable(s, "smoke", "smoke", "x")
		}},
		{"data.enums", false, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderEnumItem(s, "smoke", "smoke")
		}},
		{"data.entities", true, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderEntity(s, "smoke", "data.tables.account", "account", "x")
		}},
		{"data.queries", true, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderQuery(s, "smoke", "smoke", "data.tables.account", "x")
		}},
		{"cap.services", true, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderService(s, "smoke", "smoke", "x")
		}},
		{"cap.lifecycles", true, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderLifecycle(s, "smoke", "smoke", "x")
		}},
		{"feature.rules", true, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderRule(s, "smoke", "smoke", "x")
		}},
		{"feature.pages", true, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderPage(s, "smoke", "x")
		}},
		{"cap.events", true, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderEvents(s, "smoke", "smoke", "x")
		}},
		{"cap.protocols.http", true, func(s *contractspec.Schema) ([]byte, error) {
			return contractspec.RenderHTTP(s, "smoke", "smoke", "x", "cap.services.smoke.get", "/smoke", "get")
		}},
	}
}

func TestIndexAcceptsAllContractTypes(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	cases := scaffoldRenderCases()
	entries := make([]string, 0, len(cases))
	for _, tc := range cases {
		schema, _, err := contractspec.LoadSchemaByType(root, tc.typ)
		if err != nil {
			t.Fatalf("%s: %v", tc.typ, err)
		}
		b, err := tc.render(schema)
		if err != nil {
			t.Fatalf("%s render: %v", tc.typ, err)
		}
		id := schema.Targets.FileID("smoke.yaml")
		if id == "" {
			t.Fatalf("%s: cannot derive id from targets.id", tc.typ)
		}
		if tc.fullDoc {
			var doc map[string]any
			if err := yaml.Unmarshal(b, &doc); err != nil {
				t.Fatalf("%s: %v", tc.typ, err)
			}
			if got, _ := doc["id"].(string); got != id {
				t.Fatalf("%s: rendered id %q, want %q", tc.typ, got, id)
			}
		} else if !strings.Contains(string(b), "data/enums/smoke.yaml") {
			t.Fatalf("%s: enum item target missing:\n%s", tc.typ, b)
		}
		entries = append(entries, fmt.Sprintf(
			"  - { id: %s, path: generated/%s.yaml, summary: \"x\", consumers: [tools], enforcement: tested, stability: draft }\n",
			id, strings.ReplaceAll(tc.typ, ".", "_"),
		))
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "contracts/index.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema_version: 1\nentries:\n" + strings.Join(entries, "")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: dir}
	c.checkIndex(map[string]string{}, &Schema{}, &Schema{}, &Schema{})
	if len(c.errs) != 0 {
		t.Fatalf("all contract types index reported: %v", c.errs)
	}
}

func TestIndexEntryShape(t *testing.T) {
	root := t.TempDir()
	write := func(body string) {
		p := filepath.Join(root, "contracts/index.yaml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check := func(body string) string {
		write(body)
		c := &Checker{root: root}
		c.checkIndex(map[string]string{}, &Schema{}, &Schema{}, &Schema{})
		return strings.Join(c.errs, "\n")
	}

	bad := check(`
schema_version: 2
unknown: true
read_order: nope
groups:
  data:
    description: ""
    layers: []
entries:
  - { id: Bad-ID, path: generated/x.yaml, enforcment: tested }
  - id: data.tables.x
    path: generated/x.yaml
    summary: ""
    consumers: [tools, tools, ""]
    enforcement: sometimes
    stability: forever
    common_mistakes: [""]
`)
	for _, want := range []string{
		"schema_version must be 1",
		"unknown top-level field \"unknown\"",
		"read_order must be a list",
		"groups.data.description is required",
		"groups.data.layers must not be empty",
		"entries[0] has unknown field \"enforcment\"",
		"Bad-ID: missing summary",
		"Bad-ID.consumers is required",
		"Bad-ID: missing enforcement",
		"Bad-ID: missing stability",
		"id must match",
		"must be a non-empty string",
		"has duplicate value",
		"enforcement must be",
		"stability must be",
	} {
		if !strings.Contains(bad, want) {
			t.Fatalf("missing %q in:\n%s", want, bad)
		}
	}

	good := check(`
schema_version: 1
read_order: [docs/index.md]
groups:
  data: { description: "x", layers: [tables] }
entries:
  - { id: data.tables.x, path: generated/x.yaml, summary: "x", consumers: [tools], enforcement: tested, stability: stable, common_mistakes: ["x"] }
`)
	if strings.TrimSpace(good) != "" {
		t.Fatalf("valid index reported: %s", good)
	}

	// 未来条目：不带 common_mistakes、review-only、deprecated、下划线 ID、根白名单。
	future := check(`
schema_version: 1
global_notes: ["x"]
orphan_whitelist: [project]
entries:
  - { id: data.tables.verification_body, path: generated/x.yaml, summary: "x", consumers: [web/admin], enforcement: review-only, stability: deprecated }
`)
	if strings.TrimSpace(future) != "" {
		t.Fatalf("future index entry reported: %s", future)
	}
}

func TestIndexPathAndConsumers(t *testing.T) {
	root := t.TempDir()
	write := func(body string) {
		p := filepath.Join(root, "contracts/index.yaml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check := func(body string) string {
		write(body)
		c := &Checker{root: root}
		c.checkIndex(map[string]string{}, &Schema{}, &Schema{}, &Schema{})
		return strings.Join(c.errs, "\n")
	}

	bad := check(`
schema_version: 1
entries:
  - { id: data.tables.escape, path: ../escape.yaml, summary: "x", consumers: [web/user], enforcement: tested, stability: draft }
  - { id: data.tables.absolute, path: /tmp/x.yaml, summary: "x", consumers: [web/user], enforcement: tested, stability: draft }
  - { id: data.tables.dupsep, path: data//x.yaml, summary: "x", consumers: [web/user], enforcement: tested, stability: draft }
  - { id: data.tables.space, path: "generated/x.yaml ", summary: "x", consumers: [web/user], enforcement: tested, stability: draft }
  - { id: data.tables.colon, path: "generated/x:y.yaml", summary: "x", consumers: [web/user], enforcement: tested, stability: draft }
  - { id: data.tables.users, path: generated/x.yaml, summary: "x", consumers: ["Web Admin"], enforcement: tested, stability: draft }
`)
	for _, want := range []string{
		"path must be a clean relative path, got \"../escape.yaml\"",
		"path must be a clean relative path, got \"/tmp/x.yaml\"",
		"path must be a clean relative path, got \"data//x.yaml\"",
		"path must be a clean relative path, got \"generated/x.yaml \"",
		"path must be a clean relative path, got \"generated/x:y.yaml\"",
		`consumer "Web Admin" must match`,
	} {
		if !strings.Contains(bad, want) {
			t.Fatalf("missing %q in:\n%s", want, bad)
		}
	}

	good := check(`
schema_version: 1
entries:
  - { id: data.tables.verification_body, path: generated/openapi/admin.yaml, summary: "x", consumers: [web/user, api, worker, data-platform, web_admin, web.admin, Web/Admin, API], enforcement: review-only, stability: deprecated }
`)
	if strings.TrimSpace(good) != "" {
		t.Fatalf("valid path/consumer reported: %s", good)
	}

	if err := os.MkdirAll(filepath.Join(root, "contracts/data"), 0o755); err != nil {
		t.Fatal(err)
	}
	dirBad := check(`
schema_version: 1
entries:
  - { id: data.tables.dir, path: data, summary: "x", consumers: [tools], enforcement: tested, stability: draft }
`)
	if !strings.Contains(dirBad, "path must be a file") {
		t.Fatalf("directory path not reported: %s", dirBad)
	}
}

func TestDoNotUnique(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.queries.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doNot, ok := contractspec.FindField(schema.Fields, "do_not")
	if !ok {
		t.Fatal("do_not field missing")
	}
	c := &Checker{}
	c.validateValue([]any{}, doNot, "do_not")
	if len(c.errs) != 0 {
		t.Fatalf("empty do_not must be legal: %v", c.errs)
	}
	c.validateValue([]any{"x", "x"}, doNot, "do_not")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.unique]") {
		t.Fatalf("duplicate do_not not reported: %v", c.errs)
	}
}

func TestDoNotSpecificityAcceptsContractID(t *testing.T) {
	c := &Checker{}
	doc := map[string]any{"do_not": []any{"data.tables.ab 不得为空"}}
	c.checkDoNotSpecificity(doc, "contracts/data/tables/ab.yaml", "ab", nil, map[string]bool{}, map[string]bool{}, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("contract id token not accepted for short stem: %v", c.errs)
	}
}

func TestUsedByUsesParsedEntityAndQueryTargets(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "contracts/data/tables/x.yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`id: data.tables.x
table:
  entities: [ data.entities.e ]
  queries: [ data.queries.q.get_x ]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{
		root:     root,
		entities: map[string]*entityInfo{"data.entities.e": {ID: "data.entities.e", Tables: []string{"data.tables.x"}}},
		queries:  map[string]*queryInfo{"data.queries.q.get_x": {ID: "data.queries.q.get_x", Targets: []string{"data.tables.x"}}},
	}
	c.checkUsedBy(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if len(c.errs) != 0 {
		t.Fatalf("parsed entity/query targets not used by used_by scan: %v", c.errs)
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
	write("contracts/data/tables/x.yaml", `
id: data.tables.x
table:
  entities: []
  queries: [ data.queries.q.get_x ]
`)
	tableIDs := map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"}
	parsed := func() *Checker {
		return &Checker{
			root:     root,
			entities: map[string]*entityInfo{},
			queries:  map[string]*queryInfo{"data.queries.q.get_x": {ID: "data.queries.q.get_x", Targets: []string{"data.tables.x"}}},
		}
	}

	c := parsed()
	c.checkUsedBy(tableIDs)
	if len(c.errs) != 0 {
		t.Fatalf("query item ID not accepted: %v", c.errs)
	}

	write("contracts/data/tables/x.yaml", `
id: data.tables.x
table:
  entities: []
  queries: [ data.queries.q ]
`)
	c = parsed()
	c.checkUsedBy(tableIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "table.queries mismatch") {
		t.Fatalf("query file ID should not satisfy table.queries: %v", c.errs)
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
id: data.enums.account
summary: "x"
owner_module: account
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

func TestEnumCodeLiterals(t *testing.T) {
	c := &Checker{
		tableDocs: map[string]map[string]any{
			"data.tables.x": {"constraints": []any{map[string]any{"name": "ck_x_status", "expr": "status IN (0, 1, 9)"}}},
		},
		entities: map[string]*entityInfo{
			"data.entities.x": {
				ID:      "data.entities.x",
				Rel:     "contracts/data/entities/x.yaml",
				Primary: "data.tables.x",
				Fields: []entityFieldInfo{{
					Name: "Status", Type: "data.enums.x_status", HasType: true,
					SourceColumnRef: "data.tables.x.col.status",
					ColumnTable:     "data.tables.x", Column: "status",
				}},
			},
		},
		enumCodes: map[string]map[int]bool{"data.enums.x_status": {0: true, 1: true}},
	}
	c.checkEnumCodeLiterals()
	if !strings.Contains(strings.Join(c.errs, "\n"), "value 9 is not a code of data.enums.x_status") {
		t.Fatalf("invalid enum code not reported: %v", c.errs)
	}

	c.errs = nil
	c.tableDocs["data.tables.x"]["constraints"] = []any{map[string]any{"name": "ck_x_status", "expr": "status IN (0, 1)"}}
	c.checkEnumCodeLiterals()
	if len(c.errs) != 0 {
		t.Fatalf("valid enum code reported: %v", c.errs)
	}
}

func TestEnumCodeUnique(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "contracts/data/enums")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `
id: data.enums.account
summary: "x"
owner_module: account
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
	if err := os.WriteFile(filepath.Join(dir, "account.yaml"), []byte(body), 0o644); err != nil {
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

func TestEnumNewModuleFlow(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "data.enums")
	writeCapFile(t, root, "contracts/data/enums/notes.yaml", `id: data.enums.notes
summary: "备注域枚举"
owner_module: notes
schema_version: 1
stability: draft
do_not:
  - "data.enums.notes 的 value 删除后 code 不得复用"
enums:
  - name: note_status
    description: "备注状态"
    go_type: NoteStatus
    ts_type: NoteStatus
    values:
      - { value: active, code: 0, description: "有效" }
      - { value: deleted, code: 1, description: "已删" }
    used_by:
      - data.entities.note
`)
	writeCapFile(t, root, "contracts/index.yaml", `schema_version: 1
entries:
  - { id: data.enums.notes, path: data/enums/notes.yaml, summary: "备注域枚举", consumers: [api], enforcement: tested, stability: draft }
`)
	c := &Checker{
		root: root,
		entities: map[string]*entityInfo{
			"data.entities.note": {ID: "data.entities.note", Fields: []entityFieldInfo{{
				Name: "Status", Type: "data.enums.note_status",
			}}},
		},
	}
	enums := c.checkEnums(schema)
	if len(c.errs) != 0 {
		t.Fatalf("new module enum reported: %v", c.errs)
	}
	if !enums["data.enums.note_status"]["active"] {
		t.Fatalf("enum values not collected: %v", enums)
	}
	c.errs = nil
	c.checkIndex(map[string]string{}, schema, &Schema{}, &Schema{})
	if len(c.errs) != 0 {
		t.Fatalf("index entry reported: %v", c.errs)
	}
}

func TestEnumCodeInt32Range(t *testing.T) {
	body := `
id: data.enums.account
summary: "x"
owner_module: account
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
	dir := filepath.Join(root, "contracts/data/enums")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "account.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Checker{root: root}
}

func TestEnumDoNotSpecific(t *testing.T) {
	body := `
id: data.enums.account
summary: "x"
owner_module: account
schema_version: 1
stability: stable
do_not:
  - "枚举值 code 不得复用"
enums:
  - name: account_status
    go_type: AccountStatus
    ts_type: AccountStatus
    values:
      - { value: active, code: 0 }
    used_by: { entities: [], capabilities: [] }
`
	c := writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	if !strings.Contains(strings.Join(c.errs, "\n"), "[enum-do-not-specific]") {
		t.Fatalf("generic enum do_not not reported: %v", c.errs)
	}

	body = strings.Replace(body, "枚举值 code 不得复用", "account_status 删除值后 code 不得复用", 1)
	c = writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	if strings.Contains(strings.Join(c.errs, "\n"), "[enum-do-not-specific]") {
		t.Fatalf("specific enum name do_not reported: %v", c.errs)
	}

	body = strings.Replace(body, "account_status 删除值后 code 不得复用", "AccountStatus 删除值后 code 不得复用", 1)
	c = writeEnumContract(t, body)
	c.checkEnums(loadEnumSchemaForTest(t))
	if strings.Contains(strings.Join(c.errs, "\n"), "[enum-do-not-specific]") {
		t.Fatalf("go_type do_not reported: %v", c.errs)
	}
}

func TestEnumListTypeRef(t *testing.T) {
	c := &Checker{}
	c.scanEnumTypeRefs(map[string]any{
		"type": "List<data.enums.missing>",
	}, "x.yaml", "", map[string]map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown enum data.enums.missing") {
		t.Fatalf("List<enum> ref not checked: %v", c.errs)
	}
}

func TestEnumRefsUsesCustomEntityTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "custom/entities/e.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `id: data.entities.e
fields:
  - { type: data.enums.missing }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root, entityTargets: contractspec.Targets{Dir: "custom/entities", Glob: "*.yaml"}}
	c.checkEnumRefs(map[string]string{}, map[string]map[string]bool{}, map[string]map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "unknown enum data.enums.missing") {
		t.Fatalf("custom target enum ref not scanned: %v", c.errs)
	}
}

func TestEnumTypeNamesUnique(t *testing.T) {
	body := `
id: data.enums.account
summary: "x"
owner_module: account
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
id: data.enums.account
summary: "x"
owner_module: account
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
		Name: "enums", Type: "_types.structures.list<_types.structures.object>", ItemFields: []Field{
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
id: data.enums.account
summary: "x"
owner_module: account
schema_version: 1
stability: stable
enums:
  - name: account_status
    go_type: AccountStatus
    ts_type: AccountStatus
    values:
      - { value: active, code: 0 }
    used_by:
      - cap.methods.x
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
	c.loadEntities(&Schema{Contract: "data.entities", Targets: contractspec.Targets{Dir: "contracts/data/entities", Glob: "*.yaml"}})
	c.checkEnums(loadEnumSchemaForTest(t))
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[enum-used-by]") || !strings.Contains(errs, "used_by must be") {
		t.Fatalf("enum used_by mismatch not reported: %v", c.errs)
	}
}

func TestEnumUsedByShape(t *testing.T) {
	schema := loadEnumSchemaForTest(t)
	enums, ok := contractspec.FindField(schema.Fields, "enums")
	if !ok {
		t.Fatal("enums field missing in meta")
	}
	usedBy, ok := contractspec.FindField(enums.ItemFields, "used_by")
	if !ok {
		t.Fatal("used_by field missing in meta")
	}
	if !usedBy.Generated || usedBy.Write != "forbidden" {
		t.Fatalf("used_by must be generated and write-forbidden")
	}
	c := &Checker{}
	c.validateValue([]any{}, usedBy, "enums[].used_by")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.generated-range]") {
		t.Fatalf("empty generated used_by must fail min_items: %v", c.errs)
	}
	c.errs = nil
	c.validateValue([]any{"bad.prefix"}, usedBy, "enums[].used_by")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.pattern]") {
		t.Fatalf("invalid used_by prefix must fail pattern: %v", c.errs)
	}
	c.errs = nil
	c.validateValue([]any{"data.enums.account_status"}, usedBy, "enums[].used_by")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.pattern]") {
		t.Fatalf("enum ID must not be accepted as used_by: %v", c.errs)
	}
	c.errs = nil
	c.validateValue([]any{"cap.services.verification"}, usedBy, "enums[].used_by")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.pattern]") {
		t.Fatalf("file-level service ID must not be accepted as used_by: %v", c.errs)
	}
	c.errs = nil
	c.validateValue([]any{"cap.services.verification.verify"}, usedBy, "enums[].used_by")
	if len(c.errs) != 0 {
		t.Fatalf("specific service item rejected as used_by: %v", c.errs)
	}
}

func TestEnumCapabilityUsage(t *testing.T) {
	root := t.TempDir()
	capPath := filepath.Join(root, "contracts/capabilities/services/x.yaml")
	if err := os.MkdirAll(filepath.Dir(capPath), 0o755); err != nil {
		t.Fatal(err)
	}
	capability := `id: cap.methods.x
types:
  Filter:
    type: _types.structures.object
    properties:
      status: { type: data.enums.x_status }
`
	if err := os.WriteFile(capPath, []byte(capability), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	u := c.enumUsages()["data.enums.x_status"]
	if u == nil || !u.capabilities["cap.methods.x"] {
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
    description: "status"
`
	if err := os.WriteFile(entityPath, []byte(entity), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.loadEntities(&Schema{Contract: "data.entities", Targets: contractspec.Targets{Dir: "contracts/data/entities", Glob: "*.yaml"}})
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
id: data.enums.account
summary: ""
owner_module: account
schema_version: 1
stability: stable
enums:
  - name: x_status
    go_type: XStatus
    ts_type: XStatus
    values:
      - { value: active, code: 0 }
    used_by:
      - cap.methods.x
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
id: cap.methods.x
types:
  Input:
    type: _types.structures.object
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

func TestDuplicateYAMLKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/data/x.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "id: data.tables.x\nid: data.tables.y\ntable:\n  extends: [a]\n  extends: [b]\nqueries:\n  - name: a\n    name: b\n  - name: a\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkContractProseQuotes()
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `[schema.duplicate-key] contracts/data/x.yaml: duplicate key "id"`) ||
		!strings.Contains(errs, `duplicate key "extends" at table.extends`) {
		t.Fatalf("duplicate keys not reported: %v", c.errs)
	}
	if !strings.Contains(errs, `duplicate key "name" at queries[0].name`) {
		t.Fatalf("sequence duplicate key not reported: %v", c.errs)
	}
	if strings.Count(errs, `duplicate key "name"`) != 1 {
		t.Fatalf("same key in different sequence items must not be reported: %v", c.errs)
	}
}

func TestDuplicateKeyDistinguishesTags(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/data/x.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "200: 1\n\"200\": 2\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkContractProseQuotes()
	if strings.Contains(strings.Join(c.errs, "\n"), "[schema.duplicate-key]") {
		t.Fatalf("typed keys wrongly treated as duplicates: %v", c.errs)
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

func TestDoNotRequiredForAllDataTypes(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"data.tables", "data.enums", "data.entities", "data.queries"} {
		schema, _, err := contractspec.LoadSchemaByType(root, typ)
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		c := &Checker{}
		c.validateDoc(map[string]any{}, schema.Fields, typ+".yaml")
		if !strings.Contains(strings.Join(c.errs, "\n"), `missing required field "do_not"`) {
			t.Fatalf("%s: do_not not required: %v", typ, c.errs)
		}
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
  - { name: id, type: _types.basics.string, primary_key: true, nullable: false }
  - { name: status, type: _types.basics.integer, nullable: false }
  - { name: created_at, type: _types.basics.timestamp, nullable: false }
  - { name: updated_at, type: _types.basics.timestamp, nullable: false }
  - { name: deleted_at, type: _types.basics.timestamp, nullable: true }
rules:
  - { id: base-file, text: "x", implemented_by: [base-file] }
  - { id: base-columns, text: "x", implemented_by: [base-columns] }
`)
	write("parent.yaml", `id: data.tables.parent
table:
  extends: [ data.tables.base ]
columns:
   - { name: code, type: _types.basics.string, nullable: false }
`)
	write("child.yaml", `id: data.tables.child
table:
  extends: [ data.tables.parent ]
columns:
   - { name: code, type: _types.basics.string, nullable: false }
`)
	write("grandchild.yaml", `id: data.tables.grandchild
table:
  extends: [ data.tables.child ]
columns:
   - { name: extra, type: _types.basics.string, nullable: false }
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
   - { name: code, type: _types.basics.string, nullable: false }
`)
	writeTableFixture(t, root, "right.yaml", `id: data.tables.right
table:
  extends: [ data.tables.base ]
columns:
   - { name: name, type: _types.basics.string, nullable: false }
`)
	writeTableFixture(t, root, "child.yaml", `id: data.tables.child
table:
  extends: [ data.tables.left, data.tables.right ]
columns:
   - { name: extra, type: _types.basics.string, nullable: false }
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
   - { name: code, type: _types.basics.string, nullable: false }
`)
	writeTableFixture(t, root, "right.yaml", `id: data.tables.right
table:
  extends: [ data.tables.base ]
columns:
   - { name: code, type: _types.basics.string, nullable: false }
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
   - { name: code, type: _types.basics.string, nullable: false }
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
   - { name: gcol, type: _types.basics.string, nullable: false }
`)
	write("a.yaml", `id: data.tables.a
table:
  extends: [ data.tables.b, data.tables.g ]
columns:
   - { name: acol, type: _types.basics.string, nullable: false }
`)
	write("b.yaml", `id: data.tables.b
table:
  extends: [ data.tables.a ]
columns:
   - { name: bcol, type: _types.basics.string, nullable: false }
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
   - { name: x, type: _types.basics.string, nullable: false }
`)
	writeTableFixture(t, root, "b.yaml", `id: data.tables.b
table:
  extends: [ data.tables.a ]
columns:
   - { name: x, type: _types.basics.string, nullable: false }
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
		{Name: "table", Type: "_types.structures.object", Fields: []Field{{Name: "extends", Type: "_types.structures.list<_types.basics.string>"}}},
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
   - { name: code, type: _types.basics.string, nullable: false }
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
	c := &Checker{}
	fields := []Field{{Name: "persistence", Type: "_types.structures.object", Required: true, Fields: []Field{
		{Name: "table", Type: "_types.basics.string"},
		{Name: "tables", Type: "_types.structures.list<_types.basics.string>"},
	}}}
	doc := map[string]any{"persistence": map[string]any{
		"table":   "data.tables.x",
		"queries": map[string]any{"get": "data.queries.q.missing"},
	}}
	c.validateDoc(doc, fields, "cap.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.unknown-field]") {
		t.Fatalf("legacy persistence.queries not rejected: %v", c.errs)
	}
}

func TestResolvedColumnTypeFollowsReferences(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.a": {"x": {typ: "data.tables.b.col.y"}, "loop": {typ: "data.tables.a.col.loop"}},
		"data.tables.b": {"y": {typ: "integer"}},
	}}
	if got := c.resolvedColumnType("data.tables.a.col.x"); got != "integer" {
		t.Fatalf("chained reference = %q, want integer", got)
	}
	if got := c.resolvedColumnType("data.tables.a.col.loop"); got != "data.tables.a.col.loop" {
		t.Fatalf("reference cycle must terminate with the ref, got %q", got)
	}
}

func TestParamFamilyAcceptsFamilyKeysAndAliases(t *testing.T) {
	for _, typ := range []string{
		"string", "text", "varchar", "char",
		"integer", "int", "bigint", "smallint",
		"boolean", "bool",
		"timestamp", "timestamptz", "datetime", "date", "time",
		"float", "double", "decimal", "numeric",
		"json", "jsonb", "bytes", "bytea",
	} {
		if _, ok := paramFamily(typ); !ok {
			t.Fatalf("paramFamily(%q) rejected", typ)
		}
	}
	if _, ok := paramFamily("weird"); ok {
		t.Fatal("unknown param family accepted")
	}

	c := &Checker{}
	for _, typ := range []string{"string", "text", "bytes", "data.enums.x_status"} {
		if !c.scalarReturnType(typ) {
			t.Fatalf("scalarReturnType(%q) rejected", typ)
		}
	}
	if c.scalarReturnType("weird") {
		t.Fatal("unknown scalar return type accepted")
	}
}

func TestValidateEnum(t *testing.T) {
	c := &Checker{}
	c.validateValue("weeks", Field{Type: "_types.basics.string", Enum: []any{"minutes", "hours", "days"}}, "retention.unit")
	if !strings.Contains(strings.Join(c.errs, "\n"), `"weeks" not in enum`) {
		t.Fatalf("enum value not checked: %v", c.errs)
	}
}

func TestScaffoldGeneratedProseCovered(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, key := range contractspec.ScaffoldProseKeys() {
		allowed[key] = true
	}
	allowedList := map[string]bool{}
	for _, key := range contractspec.ScaffoldProseListKeys() {
		allowedList[key] = true
	}
	seen := map[string]bool{}
	seenList := map[string]bool{}
	observed := map[string]bool{}
	observedList := map[string]bool{}
	for _, tc := range scaffoldRenderCases() {
		schema, _, err := contractspec.LoadSchemaByType(root, tc.typ)
		if err != nil {
			t.Fatalf("%s: %v", tc.typ, err)
		}
		b, err := tc.render(schema)
		if err != nil {
			t.Fatalf("%s: %v", tc.typ, err)
		}
		var node yaml.Node
		if err := yaml.Unmarshal(b, &node); err != nil {
			t.Fatalf("%s: %v", tc.typ, err)
		}
		var walk func(*yaml.Node)
		walk = func(n *yaml.Node) {
			switch n.Kind {
			case yaml.DocumentNode, yaml.SequenceNode:
				for _, child := range n.Content {
					walk(child)
				}
			case yaml.MappingNode:
				for i := 0; i+1 < len(n.Content); i += 2 {
					k, v := n.Content[i], n.Content[i+1]
					if proseScalarFields[k.Value] && v.Kind == yaml.ScalarNode {
						observed[k.Value] = true
						if !allowed[k.Value] && !seen[k.Value] {
							seen[k.Value] = true
							t.Errorf("%s: generated prose field %q is missing from ScaffoldProseKeys", tc.typ, k.Value)
						}
					}
					if proseListFields[k.Value] && v.Kind == yaml.SequenceNode {
						for _, item := range v.Content {
							if item.Kind == yaml.ScalarNode {
								observedList[k.Value] = true
								if !allowedList[k.Value] && !seenList[k.Value] {
									seenList[k.Value] = true
									t.Errorf("%s: generated prose list field %q is missing from ScaffoldProseListKeys", tc.typ, k.Value)
								}
								break
							}
						}
					}
					walk(v)
				}
			}
		}
		walk(&node)
	}
	for _, key := range []string{"summary", "description", "when"} {
		if !observed[key] {
			t.Errorf("expected scaffold output to cover prose field %q", key)
		}
	}
	if !observedList["do_not"] {
		t.Error("expected scaffold output to cover prose list field \"do_not\"")
	}
}

func TestScaffoldProseKeysKnownToChecker(t *testing.T) {
	// 这里只查正向漂移；scaffold 产出字段漏加引号由 TestScaffoldPassesStructureCheck 的实际输出捕获。
	for _, key := range contractspec.ScaffoldProseKeys() {
		if !proseScalarFields[key] {
			t.Errorf("scaffold prose key %q is not in checker proseScalarFields", key)
		}
	}
	foundCommonMistakes := false
	for _, key := range contractspec.ScaffoldProseListKeys() {
		if !proseListFields[key] {
			t.Errorf("scaffold prose list key %q is not in checker proseListFields", key)
		}
		if key == "common_mistakes" {
			foundCommonMistakes = true
		}
	}
	if !foundCommonMistakes {
		t.Error("ScaffoldProseListKeys must include common_mistakes")
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
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
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

func TestTargetsMissingDir(t *testing.T) {
	c := &Checker{root: t.TempDir()}
	c.checkTargets(&Schema{ID: "meta.x", Targets: testTargets()})
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not a directory") {
		t.Fatalf("missing target dir not reported: %v", c.errs)
	}
}

func TestTargetsNoFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkQueries(&Schema{
		Targets: contractspec.Targets{Dir: "empty", Glob: "*.yaml", ID: "data.queries.<file>", Naming: "snake_case"},
	})
	if !strings.Contains(strings.Join(c.errs, "\n"), "no contract files match empty/*.yaml") {
		t.Fatalf("empty query target not reported: %v", c.errs)
	}
}

func TestDefaultRules(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["columns"] = append(doc["columns"].([]any), map[string]any{
		"name": "bad", "type": "_types.basics.string", "default": 1,
	})
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if !strings.Contains(strings.Join(c.errs, "\n"), "[default-type]") {
		t.Fatalf("default type not checked: %v", c.errs)
	}

	c = &Checker{}
	doc = minimalTableDoc()
	doc["columns"] = append(doc["columns"].([]any), map[string]any{
		"name": "both", "type": "_types.basics.string", "default": "x", "default_sql": "now()",
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
		"name": "code", "type": "_types.basics.string",
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
	mustContainRule(t, c.errs, "base-file")
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
	mustContainRule(t, c.errs, "base-file")
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
	c.checkImplementedBy("base-only", "meta.x: rules[0]", []string{"constraint-used-by"})
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
	col := func(name string, redundancy []any) map[string]any {
		return map[string]any{"name": name, "redundancy": redundancy}
	}
	ref := func(column string, source bool) map[string]any {
		return map[string]any{"column": column, "source": source}
	}
	newChecker := func(docs map[string]map[string]any, cols map[string]map[string]columnMeta) *Checker {
		return &Checker{tableDocs: docs, tableColumns: cols}
	}
	tables := map[string]string{
		"data.tables.a": "contracts/data/tables/a.yaml",
		"data.tables.b": "contracts/data/tables/b.yaml",
	}
	equalGroup := func() []any {
		return []any{ref("data.tables.a.col.x", true), ref("data.tables.b.col.x", false)}
	}

	t.Run("valid group", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", equalGroup())}},
			"data.tables.b": {"columns": []any{col("x", equalGroup())}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "string"}},
		})
		c.checkRedundancy(tables)
		if len(c.errs) != 0 {
			t.Fatalf("valid redundancy group reported: %v", c.errs)
		}
	})

	t.Run("type mismatch", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", equalGroup())}},
			"data.tables.b": {"columns": []any{col("x", equalGroup())}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "integer"}},
		})
		c.checkRedundancy(tables)
		if !strings.Contains(strings.Join(c.errs, "\n"), "type/nullable") || !strings.Contains(strings.Join(c.errs, "\n"), "[redundant-ref]") {
			t.Fatalf("type mismatch not reported: %v", c.errs)
		}
	})

	t.Run("missing column", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", []any{
				ref("data.tables.a.col.x", true),
				ref("data.tables.b.col.missing", false),
			})}},
			"data.tables.b": {"columns": []any{}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {},
		})
		c.checkRedundancy(tables)
		errs := strings.Join(c.errs, "\n")
		if !strings.Contains(errs, "is not a table column") || !strings.Contains(errs, "[redundant-ref]") {
			t.Fatalf("missing column not reported: %v", c.errs)
		}
		if strings.Contains(errs, "does not declare") {
			t.Fatalf("missing column cascaded into does-not-declare: %v", c.errs)
		}
	})

	t.Run("listed column does not declare", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", equalGroup())}},
			"data.tables.b": {"columns": []any{map[string]any{"name": "x"}}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "string"}},
		})
		c.checkRedundancy(tables)
		if !strings.Contains(strings.Join(c.errs, "\n"), "does not declare columns[].redundancy") {
			t.Fatalf("undeclared group member not reported: %v", c.errs)
		}
	})

	t.Run("source and self", func(t *testing.T) {
		list := []any{ref("data.tables.b.col.x", false), ref("data.tables.c.col.x", false)}
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", list)}},
			"data.tables.b": {"columns": []any{col("x", list)}},
			"data.tables.c": {"columns": []any{col("x", list)}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "string"}},
			"data.tables.c": {"x": {typ: "string"}},
		})
		c.checkRedundancy(map[string]string{
			"data.tables.a": "contracts/data/tables/a.yaml",
			"data.tables.b": "contracts/data/tables/b.yaml",
			"data.tables.c": "contracts/data/tables/c.yaml",
		})
		errs := strings.Join(c.errs, "\n")
		for _, want := range []string{
			"exactly one source",
			"include the declaring column itself",
			"[redundant-group]",
		} {
			if !strings.Contains(errs, want) {
				t.Fatalf("missing %q in redundancy errors: %v", want, c.errs)
			}
		}
	})

	t.Run("duplicate column", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", []any{
				ref("data.tables.a.col.x", true),
				ref("data.tables.a.col.x", false),
			})}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
		})
		c.checkRedundancy(map[string]string{"data.tables.a": "contracts/data/tables/a.yaml"})
		if !strings.Contains(strings.Join(c.errs, "\n"), "repeats column") {
			t.Fatalf("duplicate column not reported: %v", c.errs)
		}
	})

	t.Run("duplicate list does not cascade", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", []any{
				ref("data.tables.a.col.x", true),
				ref("data.tables.b.col.x", false),
				ref("data.tables.b.col.x", false),
			})}},
			"data.tables.b": {"columns": []any{col("x", []any{
				ref("data.tables.a.col.x", true),
				ref("data.tables.b.col.x", false),
			})}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "string"}},
		})
		c.checkRedundancy(tables)
		errs := strings.Join(c.errs, "\n")
		if !strings.Contains(errs, "repeats column") {
			t.Fatalf("duplicate column not reported: %v", c.errs)
		}
		if strings.Contains(errs, "list differs") {
			t.Fatalf("duplicate cascaded into list mismatch: %v", c.errs)
		}
	})

	t.Run("self check per declaration", func(t *testing.T) {
		// C 与 A/B 声明同一个列表，但列表里没有 C 自己；key 相同不能掩盖 C 的自包含错误。
		list := equalGroup()
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", list)}},
			"data.tables.b": {"columns": []any{col("x", list)}},
			"data.tables.c": {"columns": []any{col("x", list)}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "string"}},
			"data.tables.c": {"x": {typ: "string"}},
		})
		c.checkRedundancy(map[string]string{
			"data.tables.a": "contracts/data/tables/a.yaml",
			"data.tables.b": "contracts/data/tables/b.yaml",
			"data.tables.c": "contracts/data/tables/c.yaml",
		})
		if !strings.Contains(strings.Join(c.errs, "\n"), "data.tables.c.col.x: redundancy must include the declaring column itself") {
			t.Fatalf("self check masked by group key: %v", c.errs)
		}
	})

	t.Run("single member is schema-only", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", []any{ref("data.tables.a.col.x", true)})}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
		})
		c.checkRedundancy(map[string]string{"data.tables.a": "contracts/data/tables/a.yaml"})
		if len(c.errs) != 0 {
			t.Fatalf("single member must be left to schema min_items: %v", c.errs)
		}
	})

	t.Run("different lists", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", equalGroup())}},
			"data.tables.b": {"columns": []any{col("x", []any{
				ref("data.tables.b.col.x", true),
				ref("data.tables.a.col.x", false),
			})}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "string"}},
		})
		c.checkRedundancy(tables)
		if !strings.Contains(strings.Join(c.errs, "\n"), "redundancy list differs from") {
			t.Fatalf("conflicting lists not reported: %v", c.errs)
		}
	})

	t.Run("order does not matter", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", equalGroup())}},
			"data.tables.b": {"columns": []any{col("x", []any{
				ref("data.tables.b.col.x", false),
				ref("data.tables.a.col.x", true),
			})}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "string"}},
		})
		c.checkRedundancy(tables)
		if len(c.errs) != 0 {
			t.Fatalf("order-insensitive group reported: %v", c.errs)
		}
	})

	t.Run("column in two groups", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", []any{
				ref("data.tables.a.col.x", true),
				ref("data.tables.b.col.x", false),
			})}},
			"data.tables.b": {"columns": []any{col("x", []any{
				ref("data.tables.a.col.x", true),
				ref("data.tables.b.col.x", false),
			})}},
			"data.tables.c": {"columns": []any{col("x", []any{
				ref("data.tables.c.col.x", true),
				ref("data.tables.b.col.x", false),
			})}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
			"data.tables.b": {"x": {typ: "string"}},
			"data.tables.c": {"x": {typ: "string"}},
		})
		c.checkRedundancy(map[string]string{
			"data.tables.a": "contracts/data/tables/a.yaml",
			"data.tables.b": "contracts/data/tables/b.yaml",
			"data.tables.c": "contracts/data/tables/c.yaml",
		})
		if !strings.Contains(strings.Join(c.errs, "\n"), "redundancy list differs from") {
			t.Fatalf("shared column across groups not reported: %v", c.errs)
		}
	})

	t.Run("inherited column cannot join", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", []any{
				ref("data.tables.a.col.x", true),
				ref("data.tables.b.col.id", false),
			})}},
			"data.tables.b": {"columns": []any{map[string]any{"name": "x"}}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "integer"}},
			"data.tables.b": {"x": {typ: "string"}, "id": {typ: "integer"}},
		})
		c.inheritedCols = map[string]map[string]bool{"data.tables.b": {"id": true}}
		c.checkRedundancy(tables)
		errs := strings.Join(c.errs, "\n")
		if !strings.Contains(errs, "is inherited") {
			t.Fatalf("inherited column accepted: %v", c.errs)
		}
		if strings.Contains(errs, "does not declare") {
			t.Fatalf("inherited column cascaded into does-not-declare: %v", c.errs)
		}
	})

	t.Run("base column cannot join", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", []any{
				ref("data.tables.a.col.x", true),
				ref("data.tables.base.col.id", false),
			})}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "integer"}},
		})
		c.checkRedundancy(map[string]string{"data.tables.a": "contracts/data/tables/a.yaml"})
		errs := strings.Join(c.errs, "\n")
		if !strings.Contains(errs, "is a base column") {
			t.Fatalf("base column accepted: %v", c.errs)
		}
		if strings.Contains(errs, "does not declare") {
			t.Fatalf("base column cascaded into does-not-declare: %v", c.errs)
		}
	})

	t.Run("empty list is schema-only", func(t *testing.T) {
		c := newChecker(map[string]map[string]any{
			"data.tables.a": {"columns": []any{col("x", []any{})}},
		}, map[string]map[string]columnMeta{
			"data.tables.a": {"x": {typ: "string"}},
		})
		c.checkRedundancy(map[string]string{"data.tables.a": "contracts/data/tables/a.yaml"})
		if len(c.errs) != 0 {
			t.Fatalf("empty list must be left to schema min_items: %v", c.errs)
		}
	})
}

func TestRedundancySchema(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	columns, ok := contractspec.FindField(schema.Fields, "columns")
	if !ok {
		t.Fatal("columns field missing in meta")
	}
	redundancy, ok := contractspec.FindField(columns.ItemFields, "redundancy")
	if !ok {
		t.Fatal("columns.redundancy field missing in meta")
	}
	if redundancy.Type != "_types.structures.list<_types.structures.object>" {
		t.Fatalf("redundancy type = %q, want _types.structures.list<_types.structures.object>", redundancy.Type)
	}
	column, ok := contractspec.FindField(redundancy.ItemFields, "column")
	if !ok || column.Type != "_types.basics.string" || column.Pattern == "" || !column.Required {
		t.Fatalf("redundancy.column shape = %#v", column)
	}
	source, ok := contractspec.FindField(redundancy.ItemFields, "source")
	if !ok || source.Type != "_types.basics.boolean" || !source.Required {
		t.Fatalf("redundancy.source shape = %#v", source)
	}

	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"single member", []any{map[string]any{"column": "data.tables.a.col.x", "source": true}}, "[schema.range]"},
		{"legacy group id", "account_own_type", "[schema.type]"},
		{"bad column ref", []any{map[string]any{"column": "token.own_type", "source": true}}, "[schema.pattern]"},
		{"bad source type", []any{map[string]any{"column": "data.tables.a.col.x", "source": "yes"}}, "[schema.type]"},
		{"missing source", []any{map[string]any{"column": "data.tables.a.col.x"}}, "[schema.required]"},
	}
	for _, tc := range cases {
		c := &Checker{}
		c.validateValue(tc.value, redundancy, "columns[0].redundancy")
		if !strings.Contains(strings.Join(c.errs, "\n"), tc.want) {
			t.Fatalf("%s: missing %q in schema errors: %v", tc.name, tc.want, c.errs)
		}
	}
}

func TestQueryIndexRefs(t *testing.T) {
	newChecker := func() *Checker {
		return &Checker{
			queryOrder: []string{"data.queries.x.q"},
			queries: map[string]*queryInfo{
				"data.queries.x.q": {ID: "data.queries.x.q", Targets: []string{"data.tables.x"}, IndexRef: []queryIndexRef{
					{Index: "data.tables.x.pk", Columns: []string{"id"}},
					{Index: "data.tables.x.idx_x_code", Columns: []string{"code"}},
				}},
			},
			queryTables: map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true}},
			tableIndexes: map[string][]tableIndex{"data.tables.x": {
				{name: "idx_x_code", columns: []string{"code"}, usedBy: map[string]bool{"data.queries.x.q": true}},
				{name: "idx_x_code_region", columns: []string{"code", "region"}},
				{name: "idx_x_expr", usedBy: map[string]bool{}},
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
	if !strings.Contains(strings.Join(c.errs, "\n"), "[query-index-ref]") {
		t.Fatalf("missing index_ref must use query-index-ref: %v", c.errs)
	}

	c = newChecker()
	c.queries["data.queries.x.q"].IndexRef = []queryIndexRef{{Index: "data.tables.x.idx_missing", Columns: []string{"code"}}}
	c.checkQueryIndexRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not declared") {
		t.Fatalf("unknown index ref not reported: %v", c.errs)
	}

	c = newChecker()
	c.queries["data.queries.x.q"].IndexRef = []queryIndexRef{{Index: "data.tables.x.idx_x_code", Columns: []string{"code"}}}
	c.checkQueryIndexRefs()
	if len(c.errs) != 0 {
		t.Fatalf("explicit index ref with used_by reported: %v", c.errs)
	}

	c = newChecker()
	c.tableIndexes["data.tables.x"][1].usedBy = map[string]bool{"data.queries.x.q": true}
	c.queries["data.queries.x.q"].IndexRef = []queryIndexRef{
		{Index: "data.tables.x.idx_x_code", Columns: []string{"code"}},
		{Index: "data.tables.x.idx_x_code_region", Columns: []string{"code"}},
	}
	c.checkQueryIndexRefs()
	if len(c.errs) != 0 {
		t.Fatalf("leftmost prefix must be allowed: %v", c.errs)
	}

	c = newChecker()
	c.queries["data.queries.x.q"].IndexRef = []queryIndexRef{{Index: "data.tables.x.idx_x_code_region", Columns: []string{"region", "code"}}}
	c.checkQueryIndexRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "leftmost prefix") {
		t.Fatalf("wrong column order not reported: %v", c.errs)
	}

	c = newChecker()
	c.queries["data.queries.x.q"].IndexRef = []queryIndexRef{{Index: "data.tables.x.idx_x_code_region", Columns: []string{"code", "region", "extra"}}}
	c.checkQueryIndexRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "leftmost prefix") {
		t.Fatalf("columns longer than index not reported: %v", c.errs)
	}

	c = newChecker()
	c.tableIndexes["data.tables.x"][0].usedBy = nil
	c.queries["data.queries.x.q"].IndexRef = []queryIndexRef{{Index: "data.tables.x.idx_x_expr", Columns: []string{"code"}}}
	c.checkQueryIndexRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "has no column list") {
		t.Fatalf("pure expression index ref not reported: %v", c.errs)
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

func TestValidateScalarEnumAndBooleanConst(t *testing.T) {
	c := &Checker{}
	c.validateValue("x", Field{Type: "_types.basics.string", Enum: []any{"a", "b"}}, "f")
	if !strings.Contains(strings.Join(c.errs, "\n"), `"x" not in enum`) {
		t.Fatalf("scalar enum not checked: %v", c.errs)
	}

	c = &Checker{}
	c.validateValue(1, Field{Type: "_types.basics.integer", Enum: []any{2, 3}}, "n")
	if !strings.Contains(strings.Join(c.errs, "\n"), "1 not in enum") {
		t.Fatalf("numeric enum not checked: %v", c.errs)
	}

	c = &Checker{}
	c.validateValue(true, Field{Type: "_types.basics.boolean", Const: false}, "b")
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
	if !strings.Contains(errs, `base column "id" must be _types.basics.integer.64`) {
		t.Fatalf("base id type drift not reported: %v", c.errs)
	}
	if !strings.Contains(errs, `base column "deleted_at" nullable must be true`) {
		t.Fatalf("base deleted_at nullability drift not reported: %v", c.errs)
	}
}

func TestBaseIDMustBeInteger64(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	newDoc := func() map[string]any {
		return map[string]any{
			"id": "data.tables.base", "summary": "base", "schema_version": 1, "stability": "stable",
			"columns": []any{
				map[string]any{"name": "id", "type": "_types.basics.integer.32", "primary_key": true},
				map[string]any{"name": "status", "type": "_types.basics.integer.32", "default": 0},
				map[string]any{"name": "created_at", "type": "_types.basics.timestamp.long"},
				map[string]any{"name": "updated_at", "type": "_types.basics.timestamp.long"},
				map[string]any{"name": "deleted_at", "type": "_types.basics.timestamp.long", "nullable": true},
			},
		}
	}
	c := &Checker{root: root}
	c.checkBaseTable(newDoc(), schema, "contracts/data/tables/_base.yaml")
	errs := strings.Join(c.errs, "\n")
	if strings.Contains(errs, "unknown field") {
		t.Fatalf("base test schema is incomplete: %v", c.errs)
	}
	if !strings.Contains(errs, `base column "id" must be _types.basics.integer.64`) {
		t.Fatalf("base id type drift not reported: %v", c.errs)
	}

}

func TestBaseColumnsRejectRedundancy(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{
		"id": "data.tables.base", "summary": "base", "schema_version": 1, "stability": "stable", "do_not": []any{},
		"columns": []any{
			map[string]any{"name": "id", "type": "integer", "primary_key": true, "bits": 64, "redundancy": []any{
				map[string]any{"column": "data.tables.base.col.id", "source": true},
				map[string]any{"column": "data.tables.a.col.id", "source": false},
			}},
			map[string]any{"name": "status", "type": "integer", "default": 0},
			map[string]any{"name": "created_at", "type": "timestamp"},
			map[string]any{"name": "updated_at", "type": "timestamp"},
			map[string]any{"name": "deleted_at", "type": "timestamp", "nullable": true},
		},
	}
	c := &Checker{root: root}
	c.checkBaseTable(doc, schema, "contracts/data/tables/_base.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), `base column "id" must not declare redundancy`) {
		t.Fatalf("base redundancy declaration not reported: %v", c.errs)
	}
}

func TestCollectTableObjectsGlobalNames(t *testing.T) {
	c := &Checker{
		tableObjects: map[string]map[string]bool{},
		objectNames:  map[string]string{},
	}
	c.collectTableObjects(map[string]any{
		"constraints": []any{map[string]any{"name": "ck_a_code"}},
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

func TestQueryErrors(t *testing.T) {
	c := &Checker{errCodeSet: map[string]bool{"X": true}}
	q := &queryInfo{
		ID:     "data.queries.x.q",
		File:   "contracts/data/queries/x.yaml",
		Writes: []string{"data.tables.x"},
		Errors: []string{"platform.errors.X"},
	}
	c.checkQueryErrors(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid query errors reported: %v", c.errs)
	}

	c.errs = nil
	q.Errors = []string{"platform.errors.MISSING"}
	c.checkQueryErrors(q)
	if !strings.Contains(strings.Join(c.errs, "\n"), "references unknown error platform.errors.MISSING") {
		t.Fatalf("unknown query error not reported: %v", c.errs)
	}

	c.errs = nil
	c.dataAccessErrSet = map[string]bool{"platform.errors.COMMON_INTERNAL": true}
	q.Errors = []string{"platform.errors.COMMON_INTERNAL"}
	c.checkQueryErrors(q)
	if !strings.Contains(strings.Join(c.errs, "\n"), "must not repeat data-access baseline error") {
		t.Fatalf("baseline duplicate not reported: %v", c.errs)
	}
}

func TestMethodTypeRefs(t *testing.T) {
	c := &Checker{
		enumIDs:  map[string]bool{"data.enums.x": true},
		entities: map[string]*entityInfo{"data.entities.e": {}},
	}
	for _, tt := range []struct {
		typ  string
		want string
	}{
		{"data.enums.missing", "unknown enum"},
		{"data.entities.missing", "unknown entity"},
		{"List<data.entities.missing>", "unknown entity"},
		{"*data.entities.missing", "unknown entity"},
		{"data.tables.x", "not a method type"},
	} {
		c.errs = nil
		c.checkTypeName(tt.typ, nil, "x")
		if !strings.Contains(strings.Join(c.errs, "\n"), tt.want) {
			t.Fatalf("%s not reported: %v", tt.typ, c.errs)
		}
	}
}

func TestRetryAfterHeader(t *testing.T) {
	cases := []struct {
		name string
		resp map[string]any
		want bool
	}{
		{"missing headers", map[string]any{}, false},
		{"missing retry-after", map[string]any{"headers": map[string]any{}}, false},
		{
			"ref",
			map[string]any{"headers": map[string]any{"Retry-After": map[string]any{"$ref": "#/components/headers/RetryAfter"}}},
			true,
		},
		{
			"wrong ref",
			map[string]any{"headers": map[string]any{"Retry-After": map[string]any{"$ref": "#/components/headers/Other"}}},
			false,
		},
		{
			"inline schema",
			map[string]any{"headers": map[string]any{"Retry-After": map[string]any{"schema": map[string]any{"type": "string"}}}},
			true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryAfterHeader(tc.resp); got != tc.want {
				t.Fatalf("retryAfterHeader() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTableTextLengthLimits(t *testing.T) {
	c := &Checker{}
	doc := minimalTableDoc()
	doc["summary"] = strings.Repeat("a", tableSummaryMaxRunes+1)
	doc["columns"] = []any{map[string]any{
		"name": "code", "type": "_types.basics.string", "comment": strings.Repeat("b", columnCommentMaxRunes+1),
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	errs := entityErrs(c)
	for _, want := range []string{
		"summary must be <= 2048 characters",
		"columns.code.comment must be <= 1024 characters",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}

	c = &Checker{}
	doc = minimalTableDoc()
	doc["summary"] = strings.Repeat("a", tableSummaryMaxRunes)
	doc["columns"] = []any{map[string]any{
		"name": "code", "type": "_types.basics.string", "comment": strings.Repeat("b", columnCommentMaxRunes),
	}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", minimalTableSchema())
	if len(c.errs) != 0 {
		t.Fatalf("boundary lengths reported: %v", c.errs)
	}
}

func TestStorageCacheKeyOrderMatters(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"a": {typ: "integer"}, "b": {typ: "integer"}},
	}}
	entries := map[string]*storageEntry{"data.tables.x": {table: "data.tables.x", engine: "mysql"}}
	c.checkStorageCaches(map[string]any{"caches": map[string]any{"redis": []any{
		map[string]any{"prefix": "x", "cache_of": "data.tables.x", "key": []any{"a", "b"}, "retention": true, "unit": "days", "cache_ttl": 30},
		map[string]any{"prefix": "x:b", "cache_of": "data.tables.x", "key": []any{"b", "a"}, "retention": true, "unit": "days", "cache_ttl": 30},
	}}}, entries, "w")
	if len(c.errs) != 0 {
		t.Fatalf("key order reported as duplicate: %v", c.errs)
	}
}

func TestValidateListMapObject(t *testing.T) {
	c := &Checker{}
	f := Field{
		Name: "x",
		Type: "_types.structures.list<_types.structures.map<_types.structures.object>>",
		ItemFields: []Field{
			{Name: "id", Type: "_types.basics.string", Required: true},
		},
	}
	c.validateValue([]any{map[string]any{"a": map[string]any{"id": "x"}}}, f, "x")
	if len(c.errs) != 0 {
		t.Fatalf("list<map<object>> rejected: %v", c.errs)
	}
	c.errs = nil
	c.validateValue([]any{map[string]any{"a": map[string]any{}}}, f, "x")
	if len(c.errs) == 0 {
		t.Fatal("missing required map value field must fail")
	}
}

func TestValidateSetObject(t *testing.T) {
	vt := testValueTypes()
	f := Field{Name: "x", Type: "_types.structures.set<_types.structures.object>"}
	if err := validateFieldTypes([]Field{f}, vt, false); err == nil {
		t.Fatal("set<object> without unique_key must fail")
	}
	f.UniqueKey = "name"
	f.ItemFields = []Field{{Name: "name", Type: "_types.basics.string"}}
	if err := validateFieldTypes([]Field{f}, vt, false); err != nil {
		t.Fatalf("set<object> with unique_key rejected: %v", err)
	}
	f.UniqueKey = "missing"
	if err := validateFieldTypes([]Field{f}, vt, false); err == nil || !strings.Contains(err.Error(), "unique_key missing not found") {
		t.Fatalf("missing unique_key field: %v", err)
	}
	c := &Checker{}
	obj := Field{
		Name:       "x",
		Type:       "_types.structures.set<_types.structures.object>",
		UniqueKey:  "name",
		ItemFields: []Field{{Name: "name", Type: "_types.basics.string", Required: true}},
	}
	c.validateValue([]any{
		map[string]any{"name": "a"},
		map[string]any{"name": "a"},
	}, obj, "x")
	if !strings.Contains(strings.Join(c.errs, "\n"), "duplicate") {
		t.Fatalf("set<object> duplicate not rejected: %v", c.errs)
	}
}

func TestValidateArrayLen(t *testing.T) {
	f := Field{Name: "x", Type: "_types.structures.array<_types.basics.string>", Len: 2}
	if err := validateFieldTypes([]Field{f}, testValueTypes(), false); err != nil {
		t.Fatalf("array with len rejected: %v", err)
	}
	c := &Checker{}
	c.validateValue([]any{"a"}, f, "x")
	if !strings.Contains(strings.Join(c.errs, "\n"), "length 1 must equal 2") {
		t.Fatalf("array len not enforced: %v", c.errs)
	}
	f.MinLen = 1
	if err := validateFieldTypes([]Field{f}, testValueTypes(), false); err == nil || !strings.Contains(err.Error(), "array uses len") {
		t.Fatalf("array min_len not rejected: %v", err)
	}
}

func TestValidateNestedSetObject(t *testing.T) {
	f := Field{
		Name:       "x",
		Type:       "_types.structures.list<_types.structures.set<_types.structures.object>>",
		UniqueKey:  "name",
		ItemFields: []Field{{Name: "name", Type: "_types.basics.string", Required: true}},
	}
	c := &Checker{}
	c.validateValue([]any{
		[]any{map[string]any{"name": "a"}, map[string]any{"name": "a"}},
	}, f, "x")
	if !strings.Contains(strings.Join(c.errs, "\n"), "duplicate") {
		t.Fatalf("nested set<object> duplicate not rejected: %v", c.errs)
	}
}

func TestValidateMapIntegerKeyPatterns(t *testing.T) {
	f := Field{Name: "x", Type: "_types.structures.map<_types.basics.integer, _types.basics.string>", KeyPatterns: []string{"^[0-9]+$"}}
	if err := validateFieldTypes([]Field{f}, testValueTypes(), false); err == nil || !strings.Contains(err.Error(), "key_patterns only allowed") {
		t.Fatalf("integer key with key_patterns must fail: %v", err)
	}
}

func TestValidateMinLenExceedsMaxLen(t *testing.T) {
	f := Field{Name: "x", Type: "_types.basics.string", MinLen: 3, MaxLen: 2}
	if err := validateFieldTypes([]Field{f}, testValueTypes(), false); err == nil || !strings.Contains(err.Error(), "min_len must not exceed max_len") {
		t.Fatalf("min_len > max_len must fail: %v", err)
	}
}

func TestValidateMaxLenZero(t *testing.T) {
	f := Field{Name: "x", Type: "_types.basics.string", MaxLen: 0}
	if err := validateFieldTypes([]Field{f}, testValueTypes(), false); err == nil || !strings.Contains(err.Error(), "max_len must be a positive integer") {
		t.Fatalf("max_len 0 must fail: %v", err)
	}
}

func TestValidateMinItemsExceedsMaxItems(t *testing.T) {
	f := Field{Name: "x", Type: "_types.structures.list<_types.basics.string>", MinItems: 3, MaxItems: 2}
	if err := validateFieldTypes([]Field{f}, testValueTypes(), false); err == nil || !strings.Contains(err.Error(), "min_items must not exceed max_items") {
		t.Fatalf("min_items > max_items must fail: %v", err)
	}
}

func TestValidateCollectionBoundsOnString(t *testing.T) {
	f := Field{Name: "x", Type: "_types.basics.string", MinItems: 1}
	if err := validateFieldTypes([]Field{f}, testValueTypes(), false); err == nil || !strings.Contains(err.Error(), "only allowed for list/set/map") {
		t.Fatalf("min_items on string must fail: %v", err)
	}
}

func TestValidateBooleanUniqueKey(t *testing.T) {
	f := Field{
		Name:       "x",
		Type:       "_types.structures.set<_types.structures.object>",
		MinItems:   1,
		UniqueKey:  "flag",
		ItemFields: []Field{{Name: "flag", Type: "_types.basics.boolean", Required: true}},
	}
	if err := validateFieldTypes([]Field{f}, testValueTypes(), false); err == nil || !strings.Contains(err.Error(), "boolean unique_key is not allowed") {
		t.Fatalf("boolean unique_key must fail: %v", err)
	}
}

func TestEnumOwnerModuleMatchesFile(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "data.enums")
	writeCapFile(t, root, "contracts/data/enums/notes.yaml", `id: data.enums.notes
summary: "备注域枚举"
owner_module: other
schema_version: 1
stability: draft
do_not:
  - "data.enums.notes 的 value 删除后 code 不得复用"
enums:
  - name: note_status
    description: "备注状态"
    go_type: NoteStatus
    ts_type: NoteStatus
    values:
      - { value: active, code: 0, description: "有效" }
    used_by:
      - data.entities.note
`)
	c := &Checker{root: root, entities: map[string]*entityInfo{
		"data.entities.note": {ID: "data.entities.note", Fields: []entityFieldInfo{{Name: "Status", Type: "data.enums.note_status"}}},
	}}
	c.checkEnums(schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[enum-owner-module]") {
		t.Fatalf("owner_module/file stem mismatch not reported: %v", c.errs)
	}
}
