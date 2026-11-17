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

func TestConstraintTypeOwnedByMetaEnum(t *testing.T) {
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
	typeField, ok := contractspec.FindField(constraints.ItemFields, "type")
	if !ok {
		t.Fatal("constraints.type field missing in meta")
	}
	c := &Checker{}
	c.validateValue("unique", typeField, "constraints[0].type")
	if !strings.Contains(strings.Join(c.errs, "\n"), "[schema.enum]") {
		t.Fatalf("constraint type must be rejected by meta enum: %v", c.errs)
	}

	c = &Checker{}
	doc := minimalTableDoc()
	doc["constraints"] = []any{map[string]any{"name": "uq_x_code", "type": "unique"}}
	c.checkTableRules(doc, "contracts/data/tables/x.yaml", schema)
	if len(c.errs) != 0 {
		t.Fatalf("constraint type must not be re-checked: %v", c.errs)
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
	body := `supported_engines: [postgres, mysql]
tables:
  postgres:
    - data.tables.x
    - data.tables.z
    - data.tables.z
    - data.tables.base
    - data.tables.unknown
    - data.tables.w
  mysql:
    - data.tables.w
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
	})
	errs := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"physical table data.tables.y is not registered",
		"does not match any data.tables contract",
		"is the base contract, not a physical table",
		"tables.postgres duplicates data.tables.z",
		"table data.tables.w is registered under both mysql and postgres",
		"tables.mongodb is not a supported engine",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}
}

func TestStorageCachesAndMetadata(t *testing.T) {
	c := &Checker{}
	c.checkStorageCaches(map[string]any{
		"caches": map[string]any{"redis": []any{"data.tables.x", "data.tables.unknown"}},
	}, map[string]string{"data.tables.x": "x.yaml"}, "contracts/data/storage.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "caches.redis references unknown table data.tables.unknown") {
		t.Fatalf("unknown cache table not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkStorageMetadata(map[string]any{
		"size_class": map[string]any{"data.tables.unknown": "small"},
		"retention":  map[string]any{"data.tables.unknown": map[string]any{"unit": "days"}},
		"partition":  map[string]any{"data.tables.unknown": map[string]any{}},
	}, map[string]string{"data.tables.x": "mysql"}, map[string]bool{}, "contracts/data/storage.yaml")
	errs := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"size_class.data.tables.unknown is not a registered table",
		"retention.data.tables.unknown is not a registered table",
		"partition.data.tables.unknown is not a registered table",
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
		"unknown top-level field \"default_engine\"",
		"unknown top-level field \"engines\"",
		"supported_engines[1] must be a non-empty string",
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}

	c.errs = nil
	doc["supported_engines"] = "postgres"
	c.checkStorageDoc(doc, "contracts/data/storage.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "supported_engines must be a list") {
		t.Fatalf("non-list supported_engines not reported: %v", c.errs)
	}

	c.errs = nil
	doc["supported_engines"] = []any{"postgres"}
	doc["not_supported_topologies"] = "sharding"
	c.checkStorageDoc(doc, "contracts/data/storage.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "not_supported_topologies must be a list") {
		t.Fatalf("non-list not_supported_topologies not reported: %v", c.errs)
	}

	c.errs = nil
	doc["not_supported_topologies"] = []any{"sharding", ""}
	c.checkStorageDoc(doc, "contracts/data/storage.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "not_supported_topologies[1] must be a non-empty string") {
		t.Fatalf("empty not_supported_topologies item not reported: %v", c.errs)
	}

	c.errs = nil
	doc["not_supported_topologies"] = []any{}
	c.checkStorageDoc(doc, "contracts/data/storage.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "not_supported_topologies must not be empty when present") {
		t.Fatalf("empty not_supported_topologies list not reported: %v", c.errs)
	}
}

func TestStorageDocRequiresSupportedEngines(t *testing.T) {
	c := &Checker{}
	c.checkStorageDoc(map[string]any{"tables": map[string]any{}}, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "supported_engines is required") {
		t.Fatalf("missing supported_engines not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkStorageDoc(map[string]any{"supported_engines": []any{}}, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "supported_engines must not be empty") {
		t.Fatalf("empty supported_engines not reported: %v", c.errs)
	}
}

func TestStorageRetentionRequiresUnitAndDuration(t *testing.T) {
	c := &Checker{}
	c.checkStorageRetention("data.tables.x", map[string]any{"pending": 7}, false, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "retention.data.tables.x.unit is required") {
		t.Fatalf("missing retention unit not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkStorageRetention("data.tables.x", map[string]any{"unit": "days"}, false, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "retention.data.tables.x must declare at least one duration") {
		t.Fatalf("retention without duration not reported: %v", c.errs)
	}
}

func TestStorageRetentionCacheTTL(t *testing.T) {
	c := &Checker{}
	c.checkStorageRetention("data.tables.x", map[string]any{"unit": "days", "ttl": 30, "cache_ttl": 30}, false, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "cache_ttl requires the table in caches") {
		t.Fatalf("cache_ttl without cache not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkStorageRetention("data.tables.x", map[string]any{"unit": "days", "ttl": 30, "cache_ttl": 60}, true, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "cache_ttl must not exceed ttl") {
		t.Fatalf("cache_ttl > ttl not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkStorageRetention("data.tables.x", map[string]any{"unit": "days", "ttl": 30, "cache_ttl": 30}, true, "contracts/data/storage.yaml")
	if len(c.errs) != 0 {
		t.Fatalf("valid cache_ttl reported: %v", c.errs)
	}
}

func TestStorageRetentionUnitType(t *testing.T) {
	c := &Checker{}
	c.checkStorageRetention("data.tables.x", map[string]any{"unit": 5}, false, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "retention.data.tables.x.unit must be a string") {
		t.Fatalf("non-string retention unit not reported: %v", c.errs)
	}
}

func TestStoragePartitionIntervalType(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"created_at": {typ: "timestamp"}},
	}}
	c.checkStoragePartition("data.tables.x", map[string]any{"strategy": "range", "columns": []any{"created_at"}, "interval": 5}, "contracts/data/storage.yaml")
	if !strings.Contains(entityErrs(c), "partition.data.tables.x.interval must be a string") {
		t.Fatalf("non-string partition interval not reported: %v", c.errs)
	}
}

func TestStoragePartitionRangeInterval(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"created_at": {typ: "timestamp"}},
	}}
	c.checkStoragePartition("data.tables.x", map[string]any{"strategy": "range", "columns": []any{"created_at"}}, "contracts/data/storage.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "partition.data.tables.x.interval is required for range") {
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
	c.checkCapabilityPersistence(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
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
	c.checkCapabilityPersistence(map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), "only cap.services may declare persistence/writes") {
		t.Fatalf("http top-level writes not rejected: %v", c.errs)
	}
}

func TestPersistenceOwnership(t *testing.T) {
	c := &Checker{tableOwners: map[string]string{
		"data.tables.account":  "account",
		"data.tables.auth_pwd": "auth",
	}}
	doc := map[string]any{
		"owner_module": "auth",
		"persistence": map[string]any{
			"tables": []any{"data.tables.account", "data.tables.auth_pwd"},
		},
	}
	c.checkMethodPersistence(doc, "contracts/capabilities/methods/auth.yaml", map[string]string{"data.tables.account": "contracts/data/tables/account.yaml", "data.tables.auth_pwd": "contracts/data/tables/auth_pwd.yaml"})
	if !strings.Contains(strings.Join(c.errs, "\n"), `cannot write data.tables.account`) {
		t.Fatalf("cross-module write not reported: %v", c.errs)
	}
}

func TestPersistenceReadOnlyRejected(t *testing.T) {
	c := &Checker{}
	fields := []Field{{Name: "persistence", Type: "object", Required: true, Fields: []Field{
		{Name: "table", Type: "string"},
		{Name: "tables", Type: "list<string>"},
		{Name: "constraint_errors", Type: "map<string>"},
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
  - { id: data.tables.x, path: data/tables/x.yaml, stability: stable }
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
  - { id: data.tables.x, path: data/tables/x.yaml, stability: 1 }
`)
	c.checkIndex(map[string]string{}, &Schema{}, &Schema{}, &Schema{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "stability must be stable/draft/deprecated") {
		t.Fatalf("invalid index stability not reported: %v", c.errs)
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

func TestEnumDoNotSpecific(t *testing.T) {
	body := `
id: data.enums
summary: "x"
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
	c.loadEntities(&Schema{Type: "data.entities", Targets: contractspec.Targets{Dir: "contracts/data/entities", Glob: "*.yaml"}})
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
	capability := `id: cap.methods.x
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
    validation: { column: data.tables.x.col.status }
    description: "status"
`
	if err := os.WriteFile(entityPath, []byte(entity), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.loadEntities(&Schema{Type: "data.entities", Targets: contractspec.Targets{Dir: "contracts/data/entities", Glob: "*.yaml"}})
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
id: cap.methods.x
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

func TestDuplicateYAMLKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/data/x.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "id: data.tables.x\nid: data.tables.y\ntable:\n  comment: \"a\"\n  comment: \"b\"\nqueries:\n  - name: a\n    name: b\n  - name: a\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.checkContractProseQuotes()
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, `[schema.duplicate-key] contracts/data/x.yaml: duplicate key "id"`) ||
		!strings.Contains(errs, `duplicate key "comment" at table.comment`) {
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
	c := &Checker{}
	fields := []Field{{Name: "persistence", Type: "object", Required: true, Fields: []Field{
		{Name: "table", Type: "string"},
		{Name: "tables", Type: "list<string>"},
		{Name: "constraint_errors", Type: "map<string>"},
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
	c.checkTableRules(base([]any{}), "contracts/data/tables/x.yaml", schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[index-used-by]") {
		t.Fatalf("empty index used_by not reported: %v", c.errs)
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
		if !strings.Contains(strings.Join(c.errs, "\n"), "type/nullable") {
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
		if !strings.Contains(errs, "is not a table column") {
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
	if redundancy.Type != "list<object>" {
		t.Fatalf("redundancy type = %q, want list<object>", redundancy.Type)
	}
	column, ok := contractspec.FindField(redundancy.ItemFields, "column")
	if !ok || column.Type != "string" || column.Pattern == "" || !column.Required {
		t.Fatalf("redundancy.column shape = %#v", column)
	}
	source, ok := contractspec.FindField(redundancy.ItemFields, "source")
	if !ok || source.Type != "boolean" || !source.Required {
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
	if !strings.Contains(strings.Join(c.errs, "\n"), "[query-index-ref]") {
		t.Fatalf("missing index_ref must use query-index-ref: %v", c.errs)
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

func TestBaseIDBitsMustBe64(t *testing.T) {
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
				map[string]any{"name": "id", "type": "integer", "primary_key": true, "bits": 32},
				map[string]any{"name": "status", "type": "integer", "default": 0},
				map[string]any{"name": "created_at", "type": "timestamp"},
				map[string]any{"name": "updated_at", "type": "timestamp"},
				map[string]any{"name": "deleted_at", "type": "timestamp", "nullable": true},
			},
		}
	}
	c := &Checker{root: root}
	c.checkBaseTable(newDoc(), schema, "contracts/data/tables/_base.yaml")
	errs := strings.Join(c.errs, "\n")
	if strings.Contains(errs, "unknown field") {
		t.Fatalf("base test schema is incomplete: %v", c.errs)
	}
	if !strings.Contains(errs, `base id bits must be 64`) {
		t.Fatalf("base id bits drift not reported: %v", c.errs)
	}

	doc := newDoc()
	doc["columns"].([]any)[0].(map[string]any)["bits"] = 64
	doc["columns"].([]any)[0].(map[string]any)["unsigned"] = true
	c = &Checker{root: root}
	c.checkBaseTable(doc, schema, "contracts/data/tables/_base.yaml")
	if !strings.Contains(strings.Join(c.errs, "\n"), "base id must not be unsigned") {
		t.Fatalf("base id unsigned not reported: %v", c.errs)
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
owner_module: x
persistence:
  tables: [data.tables.x]
  constraint_errors:
    ck_missing: platform.errors.X
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tableIDs := map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"}
	c := &Checker{
		root:         root,
		tableOwners:  map[string]string{"data.tables.x": "x"},
		tableObjects: map[string]map[string]bool{"data.tables.x": {"uq_x_code": true}},
	}
	c.checkCapabilityPersistence(tableIDs)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[constraint-error-ref]") {
		t.Fatalf("dangling constraint_errors key not reported: %v", c.errs)
	}

	c = &Checker{
		root:         root,
		tableOwners:  map[string]string{"data.tables.x": "x"},
		tableObjects: map[string]map[string]bool{"data.tables.x": {"ck_missing": true}},
		errCodeSet:   map[string]bool{"X": true},
	}
	c.checkCapabilityPersistence(tableIDs)
	if len(c.errs) != 0 {
		t.Fatalf("valid constraint_errors key reported: %v", c.errs)
	}
}

func TestFlowImplementationErrors(t *testing.T) {
	methodErrors := map[string]map[string]bool{
		"cap.methods.x.get": {"platform.errors.A": true, "platform.errors.B": true},
	}
	methodIDs := map[string]bool{"cap.methods.x.get": true}
	c := &Checker{
		errCodeSet: map[string]bool{
			"A": true, "B": true, "C": true,
			"COMMON_FORBIDDEN": true, "COMMON_RATE_LIMITED": true,
			"COMMON_AUDIT_FAILED": true, "COMMON_IDEMPOTENCY_CONFLICT": true,
			"COMMON_LOOP_EXHAUSTED": true,
		},
		permissionSet: map[string]bool{"platform.security.permissions.x": true},
		methodReturns: map[string]map[string]string{
			"cap.methods.x.get": {
				"account":  "data.entities.account",
				"accounts": "List<data.entities.account>",
			},
		},
		methodTypeDefs: map[string]map[string]any{
			"Wrapper": {
				"type": "object",
				"properties": map[string]any{
					"account": map[string]any{"$ref": "#/types/AccountRef"},
				},
			},
			"AccountRef": {
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{"type": "data.enums.account_status"},
				},
			},
			"OneOfValue": {
				"oneOf": []any{
					map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}},
					map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}},
				},
			},
			"OneOfMixed": {
				"oneOf": []any{
					map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}},
					map[string]any{"type": "object", "properties": map[string]any{"other": map[string]any{"type": "string"}}},
				},
			},
			"AllOfValue": {
				"allOf": []any{
					map[string]any{"type": "object", "properties": map[string]any{"left": map[string]any{"type": "string"}}},
					map[string]any{"type": "object", "properties": map[string]any{"right": map[string]any{"type": "integer"}}},
				},
			},
			"Payment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/CardPayment"},
					map[string]any{"$ref": "#/types/BankPayment"},
				},
				"discriminator": map[string]any{
					"propertyName": "method",
					"mapping": map[string]any{
						"card": "#/types/CardPayment",
						"bank": "#/types/BankPayment",
					},
				},
			},
			"CardPayment": {
				"type": "object",
				"properties": map[string]any{
					"method":      map[string]any{"type": "string"},
					"card_number": map[string]any{"type": "string"},
					"shared":      map[string]any{"type": "string"},
				},
			},
			"BankPayment": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string"},
					"iban":   map[string]any{"type": "string"},
					"shared": map[string]any{"type": "integer"},
				},
			},
			"BadDiscriminator": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/CardPayment"},
					map[string]any{"type": "object", "properties": map[string]any{"other": map[string]any{"type": "string"}}},
				},
				"discriminator": map[string]any{"propertyName": "method"},
			},
			"WrongPayment": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string"},
				},
			},
			"BadMappingPayment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/CardPayment"},
					map[string]any{"$ref": "#/types/BankPayment"},
				},
				"discriminator": map[string]any{
					"propertyName": "method",
					"mapping": map[string]any{
						"card": "#/types/CardPayment",
						"bank": "#/types/WrongPayment",
					},
				},
			},
			"TypeMismatchPayment": {
				"oneOf": []any{
					map[string]any{"type": "object", "properties": map[string]any{
						"kind":  map[string]any{"type": "string"},
						"value": map[string]any{"type": "string"},
					}},
					map[string]any{"type": "object", "properties": map[string]any{
						"kind":  map[string]any{"type": "integer"},
						"value": map[string]any{"type": "string"},
					}},
				},
				"discriminator": map[string]any{"propertyName": "kind"},
			},
			"NumericPayment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/NumericOne"},
					map[string]any{"$ref": "#/types/NumericTwo"},
				},
				"discriminator": map[string]any{
					"propertyName": "kind",
					"mapping": map[string]any{
						"1": "#/types/NumericOne",
						"2": "#/types/NumericTwo",
					},
				},
			},
			"NumericOne": {
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{"type": "integer"},
					"one":  map[string]any{"type": "string"},
				},
			},
			"NumericTwo": {
				"type": "object",
				"properties": map[string]any{
					"kind": map[string]any{"type": "integer"},
					"two":  map[string]any{"type": "string"},
				},
			},
			"ConstrainedPayment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/ConstrainedCard"},
					map[string]any{"$ref": "#/types/ConstrainedBank"},
				},
				"discriminator": map[string]any{
					"propertyName": "method",
					"mapping": map[string]any{
						"card": "#/types/ConstrainedCard",
						"bank": "#/types/ConstrainedBank",
					},
				},
			},
			"ConstrainedCard": {
				"type": "object",
				"properties": map[string]any{
					"method":      map[string]any{"type": "string", "const": "card"},
					"kind":        map[string]any{"type": "integer", "const": 1},
					"card_number": map[string]any{"type": "string"},
				},
			},
			"ConstrainedBank": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "bank"},
					"kind":   map[string]any{"type": "integer", "const": 2},
					"iban":   map[string]any{"type": "string"},
				},
			},
			"EnumPayment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/EnumCard"},
					map[string]any{"$ref": "#/types/EnumBank"},
				},
				"discriminator": map[string]any{
					"propertyName": "method",
					"mapping": map[string]any{
						"card": "#/types/EnumCard",
						"bank": "#/types/EnumBank",
					},
				},
			},
			"EnumCard": {
				"type": "object",
				"properties": map[string]any{
					"method":      map[string]any{"type": "string", "const": "card"},
					"mode":        map[string]any{"type": "integer", "enum": []any{1, 2}},
					"card_number": map[string]any{"type": "string"},
				},
			},
			"EnumBank": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "bank"},
					"mode":   map[string]any{"type": "integer", "enum": []any{3}},
					"iban":   map[string]any{"type": "string"},
				},
			},
			"NestedPayment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/NestedCard"},
					map[string]any{"$ref": "#/types/NestedBank"},
				},
				"discriminator": map[string]any{
					"propertyName": "method",
					"mapping": map[string]any{
						"card": "#/types/NestedCard",
						"bank": "#/types/NestedBank",
					},
				},
			},
			"NestedCard": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "card"},
					"meta": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"kind": map[string]any{"type": "integer", "const": 2},
						},
					},
					"card_number": map[string]any{"type": "string"},
				},
			},
			"NestedBank": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "bank"},
					"meta": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"kind": map[string]any{"type": "integer", "const": 3},
						},
					},
					"iban": map[string]any{"type": "string"},
				},
			},
			"UnionPayment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/UnionCard"},
					map[string]any{"$ref": "#/types/UnionBank"},
				},
				"discriminator": map[string]any{
					"propertyName": "method",
					"mapping": map[string]any{
						"card": "#/types/UnionCard",
						"bank": "#/types/UnionBank",
					},
				},
			},
			"UnionCard": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "card"},
					"payload": map[string]any{
						"oneOf": []any{
							map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 1}}},
							map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 2}}},
						},
					},
					"card_number": map[string]any{"type": "string"},
				},
			},
			"UnionBank": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "bank"},
					"payload": map[string]any{
						"oneOf": []any{
							map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 3}}},
						},
					},
					"iban": map[string]any{"type": "string"},
				},
			},
			"ArrayPayment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/ArrayCard"},
					map[string]any{"$ref": "#/types/ArrayBank"},
				},
				"discriminator": map[string]any{
					"propertyName": "method",
					"mapping": map[string]any{
						"card": "#/types/ArrayCard",
						"bank": "#/types/ArrayBank",
					},
				},
			},
			"ArrayCard": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "card"},
					"items": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":       "object",
							"properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 2}},
						},
					},
					"card_number": map[string]any{"type": "string"},
				},
			},
			"ArrayBank": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "bank"},
					"items": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":       "object",
							"properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 3}},
						},
					},
					"iban": map[string]any{"type": "string"},
				},
			},
			"TuplePayment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/TupleCard"},
					map[string]any{"$ref": "#/types/TupleBank"},
				},
				"discriminator": map[string]any{
					"propertyName": "method",
					"mapping": map[string]any{
						"card": "#/types/TupleCard",
						"bank": "#/types/TupleBank",
					},
				},
			},
			"TupleCard": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "card"},
					"items": map[string]any{
						"type":        "array",
						"prefixItems": []any{map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 2}}}},
						"items":       map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 4}}},
					},
					"card_number": map[string]any{"type": "string"},
				},
			},
			"TupleBank": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "bank"},
					"items": map[string]any{
						"type":        "array",
						"prefixItems": []any{map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 3}}}},
						"items":       map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 5}}},
					},
					"iban": map[string]any{"type": "string"},
				},
			},
			"DraftTuplePayment": {
				"oneOf": []any{
					map[string]any{"$ref": "#/types/DraftTupleCard"},
					map[string]any{"$ref": "#/types/DraftTupleBank"},
				},
				"discriminator": map[string]any{
					"propertyName": "method",
					"mapping": map[string]any{
						"card": "#/types/DraftTupleCard",
						"bank": "#/types/DraftTupleBank",
					},
				},
			},
			"DraftTupleCard": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "card"},
					"items": []any{
						map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 2}}},
						map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 4}}},
					},
					"card_number": map[string]any{"type": "string"},
				},
			},
			"DraftTupleBank": {
				"type": "object",
				"properties": map[string]any{
					"method": map[string]any{"type": "string", "const": "bank"},
					"items": []any{
						map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 3}}},
						map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "integer", "const": 5}}},
					},
					"iban": map[string]any{"type": "string"},
				},
			},
		},
		entities: map[string]*entityInfo{
			"data.entities.account": {Fields: []entityFieldInfo{
				{Name: "Status", JSONName: "status", Type: "data.enums.account_status"},
			}},
		},
	}
	check := func(t *testing.T, doc map[string]any) []string {
		t.Helper()
		c.errs = nil
		c.checkFlowItem(doc, "f", "cap.flows.x.get", "x", methodIDs, methodErrors, map[string]bool{
			"Wrapper": true, "AccountRef": true,
			"OneOfValue": true, "OneOfMixed": true, "AllOfValue": true,
			"Payment": true, "CardPayment": true, "BankPayment": true, "BadDiscriminator": true,
			"WrongPayment": true, "BadMappingPayment": true, "TypeMismatchPayment": true,
			"NumericPayment": true, "NumericOne": true, "NumericTwo": true,
			"ConstrainedPayment": true, "ConstrainedCard": true, "ConstrainedBank": true,
			"EnumPayment": true, "EnumCard": true, "EnumBank": true,
			"NestedPayment": true, "NestedCard": true, "NestedBank": true,
			"UnionPayment": true, "UnionCard": true, "UnionBank": true,
			"ArrayPayment": true, "ArrayCard": true, "ArrayBank": true,
			"TuplePayment": true, "TupleCard": true, "TupleBank": true,
			"DraftTuplePayment": true, "DraftTupleCard": true, "DraftTupleBank": true,
		})
		return c.errs
	}
	step := func(id, use string, fields map[string]any) map[string]any {
		m := map[string]any{"id": id}
		if use != "" {
			m["use"] = use
		}
		for k, v := range fields {
			m[k] = v
		}
		return m
	}
	base := func(steps []any, errors []any) map[string]any {
		return map[string]any{
			"summary": "x", "implementation": "flow",
			"transaction": "required", "permissions": []any{},
			"steps": steps, "errors": errors,
		}
	}
	want := func(t *testing.T, errs []string, text string) {
		t.Helper()
		if !strings.Contains(strings.Join(errs, "\n"), text) {
			t.Fatalf("want %q, got %v", text, errs)
		}
	}
	onError := func(code, action, target string) map[string]any {
		m := map[string]any{"error": code, "action": action}
		if target != "" {
			m["target"] = target
		}
		return m
	}

	t.Run("unhandled errors must all be exposed", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", nil)}, []any{"platform.errors.A", "platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("valid flow errors reported: %v", errs)
		}
	})

	t.Run("missing unhandled error", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", nil)}, []any{"platform.errors.A"})
		want(t, check(t, doc), "missing error platform.errors.B")
	})

	t.Run("extra error", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", nil)}, []any{"platform.errors.A", "platform.errors.B", "platform.errors.C"})
		want(t, check(t, doc), "not produced by a reachable, unhandled step")
	})

	t.Run("handled error is excluded", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"on_error": []any{onError("platform.errors.B", "continue", "")},
		})}, []any{"platform.errors.A"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("handled error reported: %v", errs)
		}
	})

	t.Run("handled error must not be exposed", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"on_error": []any{onError("platform.errors.B", "continue", "")},
		})}, []any{"platform.errors.A", "platform.errors.B"})
		want(t, check(t, doc), "not produced by a reachable, unhandled step")
	})

	t.Run("when false is unreachable", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"when": "false", "errors": []any{"platform.errors.A"},
		})}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("unreachable step reported: %v", errs)
		}
	})

	t.Run("unreachable step error must not be exposed", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"when": "false", "errors": []any{"platform.errors.A"},
		})}, []any{"platform.errors.A"})
		want(t, check(t, doc), "not produced by a reachable, unhandled step")
	})

	t.Run("dependency on unreachable step", func(t *testing.T) {
		doc := base([]any{
			step("off", "cap.methods.x.get", map[string]any{"when": "false", "errors": []any{"platform.errors.A"}}),
			step("after", "cap.methods.x.get", map[string]any{"depends_on": []any{"off"}, "errors": []any{"platform.errors.B"}}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("dependency chain reported: %v", errs)
		}
	})

	t.Run("dependency error must not be exposed", func(t *testing.T) {
		doc := base([]any{
			step("off", "cap.methods.x.get", map[string]any{"when": "false", "errors": []any{"platform.errors.A"}}),
			step("after", "cap.methods.x.get", map[string]any{"depends_on": []any{"off"}, "errors": []any{"platform.errors.B"}}),
		}, []any{"platform.errors.B"})
		want(t, check(t, doc), "not produced by a reachable, unhandled step")
	})

	t.Run("depends_on must reference earlier step", func(t *testing.T) {
		doc := base([]any{step("after", "cap.methods.x.get", map[string]any{"depends_on": []any{"missing"}})}, []any{})
		want(t, check(t, doc), "must reference an earlier step")
	})

	t.Run("permissions expose forbidden", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{}})}, []any{"platform.errors.COMMON_FORBIDDEN"})
		doc["permissions"] = []any{"platform.security.permissions.x"}
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("permission forbidden reported: %v", errs)
		}
	})

	t.Run("internal step errors", func(t *testing.T) {
		doc := base([]any{step("internal", "", map[string]any{"errors": []any{"platform.errors.C"}})}, []any{"platform.errors.C"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("internal step errors reported: %v", errs)
		}
	})

	t.Run("step errors must be declared by method", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{"platform.errors.C"}})}, []any{"platform.errors.A"})
		want(t, check(t, doc), "not declared by target service")
	})

	t.Run("on_error abort rethrows", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"on_error": []any{onError("platform.errors.B", "abort", "")},
		})}, []any{"platform.errors.A", "platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("abort error reported: %v", errs)
		}
	})

	t.Run("on_error goto branches", func(t *testing.T) {
		doc := base([]any{
			step("call", "cap.methods.x.get", map[string]any{
				"errors":   []any{"platform.errors.A"},
				"on_error": []any{onError("platform.errors.A", "goto", "fallback")},
			}),
			step("fallback", "cap.methods.x.get", map[string]any{"errors": []any{"platform.errors.B"}}),
		}, []any{"platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("goto flow reported: %v", errs)
		}
	})

	t.Run("goto target must exist", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"errors":   []any{"platform.errors.A"},
			"on_error": []any{onError("platform.errors.A", "goto", "missing")},
		})}, []any{})
		want(t, check(t, doc), "goto target missing not found")
	})

	t.Run("self goto requires max_iterations", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"errors":   []any{"platform.errors.A"},
			"on_error": []any{onError("platform.errors.A", "goto", "call")},
		})}, []any{})
		want(t, check(t, doc), "backward/self goto requires service.max_iterations")
	})

	t.Run("bounded self goto", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"errors":   []any{"platform.errors.A"},
			"on_error": []any{onError("platform.errors.A", "goto", "call")},
		})}, []any{"platform.errors.COMMON_LOOP_EXHAUSTED"})
		doc["max_iterations"] = 3
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("bounded self goto reported: %v", errs)
		}
	})

	t.Run("bounded self goto requires loop exhausted", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"errors":   []any{"platform.errors.A"},
			"on_error": []any{onError("platform.errors.A", "goto", "call")},
		})}, []any{})
		doc["max_iterations"] = 3
		want(t, check(t, doc), "missing service-level error platform.errors.COMMON_LOOP_EXHAUSTED")
	})

	t.Run("continue target is forbidden", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"errors":   []any{"platform.errors.A"},
			"on_error": []any{onError("platform.errors.A", "continue", "fallback")},
		})}, []any{})
		want(t, check(t, doc), "target is only allowed for goto")
	})

	t.Run("when static false", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"when": "false && input.x == 1", "errors": []any{"platform.errors.A"},
		})}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("static false reported: %v", errs)
		}
	})

	t.Run("when unknown is reachable", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"when": "input.x == 1", "errors": []any{"platform.errors.A"},
		})}, []any{"platform.errors.A"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("unknown when reported: %v", errs)
		}
	})

	t.Run("when syntax error", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"when": "input.x", "errors": []any{"platform.errors.A"},
		})}, []any{})
		want(t, check(t, doc), "operand must be compared")
	})

	t.Run("rate_limit allows rate limited", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{}})}, []any{"platform.errors.COMMON_RATE_LIMITED"})
		doc["rate_limit"] = map[string]any{"requests": 1, "window_seconds": 1}
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("rate limited reported: %v", errs)
		}
	})

	t.Run("audit allows audit failed", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{}})}, []any{"platform.errors.COMMON_AUDIT_FAILED"})
		doc["audit"] = true
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("audit failed reported: %v", errs)
		}
	})

	t.Run("idempotency allows idempotency conflict", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{}})}, []any{"platform.errors.COMMON_IDEMPOTENCY_CONFLICT"})
		doc["idempotency"] = map[string]any{"key": "x", "window": "1h"}
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("idempotency conflict reported: %v", errs)
		}
	})

	t.Run("flow level error without feature", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{}})}, []any{"platform.errors.COMMON_RATE_LIMITED"})
		want(t, check(t, doc), "not produced by a reachable, unhandled step")
	})

	t.Run("rate_limit requires rate limited", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{}})}, []any{})
		doc["rate_limit"] = map[string]any{"requests": 1, "window_seconds": 1}
		want(t, check(t, doc), "missing service-level error platform.errors.COMMON_RATE_LIMITED")
	})

	t.Run("audit requires audit failed", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{}})}, []any{})
		doc["audit"] = true
		want(t, check(t, doc), "missing service-level error platform.errors.COMMON_AUDIT_FAILED")
	})

	t.Run("idempotency requires idempotency conflict", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{}})}, []any{})
		doc["idempotency"] = map[string]any{"key": "x", "window": "1h"}
		want(t, check(t, doc), "missing service-level error platform.errors.COMMON_IDEMPOTENCY_CONFLICT")
	})

	t.Run("when references earlier step", func(t *testing.T) {
		doc := base([]any{
			step("load", "cap.methods.x.get", map[string]any{"errors": []any{"platform.errors.A"}}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{"platform.errors.B"},
				"when":   `step.load.account.status == "ok"`,
			}),
		}, []any{"platform.errors.A", "platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("step reference reported: %v", errs)
		}
	})

	t.Run("step output unknown field", func(t *testing.T) {
		doc := base([]any{
			step("load", "cap.methods.x.get", map[string]any{"errors": []any{}}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.load.account.missing == 1`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field missing")
	})

	t.Run("step output unknown root", func(t *testing.T) {
		doc := base([]any{
			step("load", "cap.methods.x.get", map[string]any{"errors": []any{}}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.load.missing == 1`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown output missing")
	})

	t.Run("list output index", func(t *testing.T) {
		doc := base([]any{
			step("load", "cap.methods.x.get", map[string]any{"errors": []any{}}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{"platform.errors.B"},
				"when":   `step.load.accounts[0].status == "ok"`,
			}),
		}, []any{"platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("list output reported: %v", errs)
		}
	})

	t.Run("list output requires index", func(t *testing.T) {
		doc := base([]any{
			step("load", "cap.methods.x.get", map[string]any{"errors": []any{}}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.load.accounts.status == "ok"`,
			}),
		}, []any{})
		want(t, check(t, doc), "list output requires index")
	})

	t.Run("index on non-list", func(t *testing.T) {
		doc := base([]any{
			step("load", "cap.methods.x.get", map[string]any{"errors": []any{}}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.load.account[0].status == "ok"`,
			}),
		}, []any{})
		want(t, check(t, doc), "not a list")
	})

	t.Run("method step output is forbidden", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{
			"errors": []any{},
			"output": []any{map[string]any{"name": "value", "type": "data.entities.account"}},
		})}, []any{})
		want(t, check(t, doc), "does not allow field output")
	})

	t.Run("internal step output path", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "result", "type": "data.entities.account"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{"platform.errors.B"},
				"when":   `step.internal.result.status == "ok"`,
			}),
		}, []any{"platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("internal output reported: %v", errs)
		}
	})

	t.Run("ref output path", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "wrapper", "type": "Wrapper"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{"platform.errors.B"},
				"when":   `step.internal.wrapper.account.status == "ok"`,
			}),
		}, []any{"platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("ref output reported: %v", errs)
		}
	})

	t.Run("oneOf output field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "one", "type": "OneOfValue"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{"platform.errors.B"},
				"when":   `step.internal.one.value == "ok"`,
			}),
		}, []any{"platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("oneOf output reported: %v", errs)
		}
	})

	t.Run("oneOf missing branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "mixed", "type": "OneOfMixed"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.mixed.value == "ok"`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field value of OneOfMixed")
	})

	t.Run("allOf output field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "all", "type": "AllOfValue"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{"platform.errors.B"},
				"when":   `step.internal.all.right == 1`,
			}),
		}, []any{"platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("allOf output reported: %v", errs)
		}
	})

	t.Run("discriminator variant field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "payment", "type": "Payment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{"platform.errors.B"},
				"when":   `step.internal.payment.card_number != null && step.internal.payment.method == "card"`,
			}),
		}, []any{"platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("discriminator field reported: %v", errs)
		}
	})

	t.Run("discriminator selects branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "payment", "type": "Payment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.payment.method == "bank" && step.internal.payment.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number")
	})

	t.Run("discriminator selects shared field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "payment", "type": "Payment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.payment.method == "card" && step.internal.payment.shared == "x"`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("discriminator shared reported: %v", errs)
		}
	})

	t.Run("discriminator or alternatives", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "payment", "type": "Payment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `(step.internal.payment.method == "card" || step.internal.payment.method == "bank") && step.internal.payment.shared != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field shared")
	})

	t.Run("discriminator exclude value", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "payment", "type": "Payment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.payment.method != "card" && step.internal.payment.shared != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("discriminator exclude reported: %v", errs)
		}
	})

	t.Run("discriminator input value", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "payment", "type": "Payment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `input.payment.method == "card" && step.internal.payment.card_number != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("discriminator input reported: %v", errs)
		}
	})

	t.Run("discriminator negated value", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "payment", "type": "Payment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `!(step.internal.payment.method == "card") && step.internal.payment.shared != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("discriminator negation reported: %v", errs)
		}
	})

	t.Run("discriminator numeric range", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "numeric", "type": "NumericPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.numeric.kind > 1 && step.internal.numeric.two != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("discriminator range reported: %v", errs)
		}
	})

	t.Run("discriminator numeric range miss", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "numeric", "type": "NumericPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.numeric.kind > 1 && step.internal.numeric.one != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field one")
	})

	t.Run("discriminator range union", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "numeric", "type": "NumericPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `(step.internal.numeric.kind > 1 || step.internal.numeric.kind > 2) && step.internal.numeric.two != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("range union reported: %v", errs)
		}
	})

	t.Run("discriminator range negation", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "numeric", "type": "NumericPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `!(step.internal.numeric.kind > 1) && step.internal.numeric.one != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("range negation reported: %v", errs)
		}
	})

	t.Run("discriminator range conjunction negation", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "numeric", "type": "NumericPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `!(step.internal.numeric.kind > 1 && step.internal.numeric.kind <= 3) && step.internal.numeric.one != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("range conjunction negation reported: %v", errs)
		}
	})

	t.Run("cross-field clause selects branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "constrained", "type": "ConstrainedPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.constrained.kind == 2 && step.internal.constrained.iban != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("cross-field clause reported: %v", errs)
		}
	})

	t.Run("cross-field clause rejects other branch field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "constrained", "type": "ConstrainedPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.constrained.kind == 2 && step.internal.constrained.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number")
	})

	t.Run("multi-value enum selects branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "enum", "type": "EnumPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.enum.mode == 2 && step.internal.enum.card_number != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("multi-value enum reported: %v", errs)
		}
	})

	t.Run("multi-value enum rejects other branch field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "enum", "type": "EnumPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.enum.mode == 3 && step.internal.enum.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number")
	})

	t.Run("nested branch const selects branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "nested", "type": "NestedPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.nested.meta.kind == 2 && step.internal.nested.card_number != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("nested const reported: %v", errs)
		}
	})

	t.Run("nested branch const rejects other branch field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "nested", "type": "NestedPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.nested.meta.kind == 3 && step.internal.nested.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number")
	})

	t.Run("oneOf nested const selects branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "union", "type": "UnionPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.union.payload.kind == 2 && step.internal.union.card_number != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("oneOf nested const reported: %v", errs)
		}
	})

	t.Run("oneOf nested const rejects other branch field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "union", "type": "UnionPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.union.payload.kind == 3 && step.internal.union.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number")
	})

	t.Run("array nested const selects branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "array", "type": "ArrayPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.array.items[0].kind == 2 && step.internal.array.card_number != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("array nested const reported: %v", errs)
		}
	})

	t.Run("array nested const rejects other branch field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "array", "type": "ArrayPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.array.items[0].kind == 3 && step.internal.array.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number")
	})

	t.Run("prefixItems tuple selects branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "tuple", "type": "TuplePayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.tuple.items[0].kind == 2 && step.internal.tuple.card_number != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("prefixItems tuple reported: %v", errs)
		}
	})

	t.Run("prefixItems additional item selects branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "tuple", "type": "TuplePayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.tuple.items[1].kind == 5 && step.internal.tuple.iban != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("prefixItems additional reported: %v", errs)
		}
	})

	t.Run("draft tuple selects branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "draft", "type": "DraftTuplePayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.draft.items[1].kind == 4 && step.internal.draft.card_number != null`,
			}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("draft tuple reported: %v", errs)
		}
	})

	t.Run("draft tuple rejects other branch field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "draft", "type": "DraftTuplePayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.draft.items[1].kind == 5 && step.internal.draft.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number")
	})

	t.Run("discriminator unknown mapping value", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "payment", "type": "Payment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.payment.method == "wire" && step.internal.payment.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number")
	})

	t.Run("discriminator ambiguous field", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "payment", "type": "Payment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.payment.shared != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field shared of Payment")
	})

	t.Run("discriminator invalid branch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "bad", "type": "BadDiscriminator"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.bad.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number of BadDiscriminator")
	})

	t.Run("discriminator mapping points outside branches", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "bad", "type": "BadMappingPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.bad.card_number != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field card_number of BadMappingPayment")
	})

	t.Run("discriminator property type mismatch", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{
				"errors": []any{},
				"output": []any{map[string]any{"name": "mismatch", "type": "TypeMismatchPayment"}},
			}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.mismatch.value != null`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown field value of TypeMismatchPayment")
	})

	t.Run("internal step without output", func(t *testing.T) {
		doc := base([]any{
			step("internal", "", map[string]any{"errors": []any{}}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.internal.result.status == "ok"`,
			}),
		}, []any{})
		want(t, check(t, doc), "unknown output result")
	})

	t.Run("when references unknown step", func(t *testing.T) {
		doc := base([]any{step("check", "cap.methods.x.get", map[string]any{
			"errors": []any{},
			"when":   `step.missing.status == "ok"`,
		})}, []any{})
		want(t, check(t, doc), "references step missing that is not an earlier step")
	})

	t.Run("when references forward step", func(t *testing.T) {
		doc := base([]any{
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{},
				"when":   `step.later.status == "ok"`,
			}),
			step("later", "cap.methods.x.get", map[string]any{"errors": []any{}}),
		}, []any{})
		want(t, check(t, doc), "references step later that is not an earlier step")
	})

	t.Run("when references unreachable step", func(t *testing.T) {
		doc := base([]any{
			step("off", "cap.methods.x.get", map[string]any{"when": "false", "errors": []any{}}),
			step("check", "cap.methods.x.get", map[string]any{
				"errors": []any{"platform.errors.B"},
				"when":   `step.off.account.status == "ok"`,
			}),
		}, []any{"platform.errors.B"})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("unreachable reference reported: %v", errs)
		}
	})

	t.Run("backward goto requires max_iterations", func(t *testing.T) {
		doc := base([]any{
			step("load", "cap.methods.x.get", map[string]any{"errors": []any{}}),
			step("call", "cap.methods.x.get", map[string]any{
				"errors":   []any{"platform.errors.A"},
				"on_error": []any{onError("platform.errors.A", "goto", "load")},
			}),
		}, []any{})
		want(t, check(t, doc), "backward/self goto requires service.max_iterations")
	})

	t.Run("bounded backward goto", func(t *testing.T) {
		doc := base([]any{
			step("load", "cap.methods.x.get", map[string]any{"errors": []any{}}),
			step("call", "cap.methods.x.get", map[string]any{
				"errors":   []any{"platform.errors.A"},
				"on_error": []any{onError("platform.errors.A", "goto", "load")},
			}),
		}, []any{"platform.errors.COMMON_LOOP_EXHAUSTED"})
		doc["max_iterations"] = 3
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("bounded backward goto reported: %v", errs)
		}
	})

	t.Run("forward goto does not require max_iterations", func(t *testing.T) {
		doc := base([]any{
			step("call", "cap.methods.x.get", map[string]any{
				"errors":   []any{"platform.errors.A"},
				"on_error": []any{onError("platform.errors.A", "goto", "fallback")},
			}),
			step("fallback", "cap.methods.x.get", map[string]any{"errors": []any{}}),
		}, []any{})
		if errs := check(t, doc); len(errs) != 0 {
			t.Fatalf("forward goto reported: %v", errs)
		}
	})

	t.Run("flow max_iterations must be positive", func(t *testing.T) {
		doc := base([]any{step("call", "cap.methods.x.get", map[string]any{"errors": []any{}})}, []any{})
		doc["max_iterations"] = 0
		want(t, check(t, doc), "max_iterations must be >= 1")
	})
}

func TestDirectFlowLevelErrors(t *testing.T) {
	methodErrors := map[string]map[string]bool{"cap.methods.x.get": {"platform.errors.A": true}}
	methodIDs := map[string]bool{"cap.methods.x.get": true}
	c := &Checker{errCodeSet: map[string]bool{"A": true, "COMMON_RATE_LIMITED": true}}
	doc := map[string]any{
		"summary": "x", "implementation": "direct",
		"method": "cap.methods.x.get", "transaction": "read_only",
		"permissions": []any{},
		"rate_limit":  map[string]any{"requests": 1, "window_seconds": 1},
		"errors":      []any{"platform.errors.A", "platform.errors.COMMON_RATE_LIMITED"},
	}
	c.checkFlowItem(doc, "f", "cap.flows.x.get", "x", methodIDs, methodErrors, map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("direct flow level error reported: %v", c.errs)
	}

	doc["errors"] = []any{"platform.errors.A"}
	c.errs = nil
	c.checkFlowItem(doc, "f", "cap.flows.x.get", "x", methodIDs, methodErrors, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "missing service-level error platform.errors.COMMON_RATE_LIMITED") {
		t.Fatalf("missing rate limit error not reported: %v", c.errs)
	}

	doc["rate_limit"] = nil
	doc["permissions"] = []any{"platform.security.permissions.x"}
	c.permissionSet = map[string]bool{"platform.security.permissions.x": true}
	c.errs = nil
	c.checkFlowItem(doc, "f", "cap.flows.x.get", "x", methodIDs, methodErrors, map[string]bool{})
	if !strings.Contains(strings.Join(c.errs, "\n"), "missing service-level error platform.errors.COMMON_FORBIDDEN") {
		t.Fatalf("missing forbidden error not reported: %v", c.errs)
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
