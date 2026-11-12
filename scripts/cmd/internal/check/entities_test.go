package check

import (
	"path/filepath"
	"strings"
	"testing"
)

func entityErrs(c *Checker) string {
	return strings.Join(c.errs, "\n")
}

func TestEntityFieldProjectionRules(t *testing.T) {
	newChecker := func() *Checker {
		return &Checker{tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {
				"id":       {typ: "string", primaryKey: true},
				"own":      {typ: "string"},
				"opt":      {typ: "string", nullable: true},
				"nickname": {typ: "string", pii: true},
				"secret":   {typ: "string", neverExpose: true},
			},
		}}
	}
	newEntity := func() *entityInfo {
		return &entityInfo{
			ID:      "data.entities.x",
			Rel:     "contracts/data/entities/x.yaml",
			Tables:  []string{"data.tables.x"},
			Primary: "data.tables.x",
			Doc:     map[string]any{"identity": "id"},
			Fields: []entityFieldInfo{
				{Name: "id", ColumnTable: "data.tables.x", Column: "id", Type: "string", Required: true, Mutable: false},
				{Name: "own", ColumnTable: "data.tables.x", Column: "own", Type: "string", Required: true, Mutable: true},
				{Name: "opt", ColumnTable: "data.tables.x", Column: "opt", Type: "string", Mutable: true},
				{Name: "nickname", ColumnTable: "data.tables.x", Column: "nickname", Type: "string", Required: true, Mutable: true, PII: true},
				{Name: "secret", ColumnTable: "data.tables.x", Column: "secret", Type: "string", Required: true, Mutable: true, NeverExpose: true},
			},
		}
	}

	c := newChecker()
	e := newEntity()
	c.checkEntityFields(e)
	c.checkEntityIdentity(e)
	if len(c.errs) != 0 {
		t.Fatalf("valid entity fields reported: %v", c.errs)
	}

	c = newChecker()
	e = newEntity()
	e.Fields[3].PII = false
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "[entity-field-sensitive]") {
		t.Fatalf("pii relaxation not reported: %v", c.errs)
	}

	c = newChecker()
	e = newEntity()
	e.Fields[1].Required = false
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "[entity-field-required]") {
		t.Fatalf("required/nullable mismatch not reported: %v", c.errs)
	}

	c = newChecker()
	e = newEntity()
	e.Fields[4].NeverExpose = false
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "never_expose") {
		t.Fatalf("never_expose relaxation not reported: %v", c.errs)
	}
}

func TestEntityBaseMerge(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {
				"id":         {typ: "string", primaryKey: true},
				"status":     {typ: "integer"},
				"created_at": {typ: "timestamp"},
				"updated_at": {typ: "timestamp"},
				"deleted_at": {typ: "timestamp", nullable: true},
				"own":        {typ: "string"},
			},
		},
		entityBaseFields: []entityFieldInfo{
			{Name: "id", Type: "string", Required: true, Mutable: false, HasExplicitType: true, FromBase: true},
			{Name: "status", Type: "integer", Required: true, Mutable: true, HasExplicitType: true, FromBase: true},
			{Name: "created_at", Type: "timestamp", Required: true, Mutable: false, HasExplicitType: true, FromBase: true},
			{Name: "updated_at", Type: "timestamp", Required: true, Mutable: false, HasExplicitType: true, FromBase: true},
			{Name: "deleted_at", Type: "timestamp", Mutable: false, HasExplicitType: true, FromBase: true},
		},
	}
	doc := map[string]any{
		"id":       "data.entities.x",
		"extends":  []any{"data.entities.base"},
		"table":    "data.tables.x",
		"identity": "id",
		"fields": []any{
			map[string]any{"name": "status", "type": "data.enums.x_status"},
			map[string]any{"name": "own", "type": "string", "required": true},
		},
	}
	e := c.parseEntity(doc, "contracts/data/entities/x.yaml")
	if len(e.Fields) != 6 {
		t.Fatalf("want 6 effective fields, got %d: %#v", len(e.Fields), e.Fields)
	}
	var status *entityFieldInfo
	for i := range e.Fields {
		if e.Fields[i].Name == "status" {
			status = &e.Fields[i]
		}
	}
	if status == nil || !status.OverridesBase || !status.Required || status.Column != "status" {
		t.Fatalf("status override not merged: %#v", status)
	}
	if e.Fields[0].Name != "id" || !e.Fields[0].FromBase {
		t.Fatalf("base id not inherited: %#v", e.Fields[0])
	}
}

func TestEntityBaseShapeRejectsDrift(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.entities.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := loadYAML(filepath.Join(root, "contracts/data/entities/_base.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	fields := asList(doc["fields"])
	first, _ := asMap(fields[0])
	first["mutable"] = true
	c := &Checker{root: root, entityBaseDoc: doc}
	c.checkEntityBase(schema)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[entity-base-shape]") {
		t.Fatalf("base drift not reported: %v", c.errs)
	}
}

func TestEntityRelationDirectionAndEager(t *testing.T) {
	newChecker := func(withIndex bool) *Checker {
		indexes := map[string][]tableIndex{}
		if withIndex {
			indexes["data.tables.token"] = []tableIndex{{name: "idx_tokens_account", columns: []string{"account_id"}}}
		}
		return &Checker{
			tableColumns: map[string]map[string]columnMeta{
				"data.tables.account": {"id": {typ: "string", primaryKey: true}},
				"data.tables.token":   {"id": {typ: "string", primaryKey: true}, "account_id": {typ: "string"}},
			},
			tableIndexes: indexes,
			entities: map[string]*entityInfo{
				"data.entities.account": {
					ID: "data.entities.account", Rel: "account.yaml", Tables: []string{"data.tables.account"}, Primary: "data.tables.account",
					Doc:    map[string]any{"identity": "id"},
					Fields: []entityFieldInfo{{Name: "id", ColumnTable: "data.tables.account", Column: "id", Type: "string", Required: true, Mutable: false}},
				},
				"data.entities.token": {
					ID: "data.entities.token", Rel: "token.yaml", Tables: []string{"data.tables.token"}, Primary: "data.tables.token",
					Doc:    map[string]any{"identity": "id"},
					Fields: []entityFieldInfo{{Name: "id", ColumnTable: "data.tables.token", Column: "id", Type: "string", Required: true, Mutable: false}},
				},
			},
		}
	}

	c := newChecker(true)
	e := c.entities["data.entities.account"]
	e.Relations = []entityRelationInfo{{Name: "tokens", Target: "data.entities.token", Kind: "one_to_many", ViaTable: "data.tables.token", ViaColumn: "account_id", Load: "eager"}}
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid eager relation reported: %v", c.errs)
	}

	c = newChecker(false)
	e = c.entities["data.entities.account"]
	e.Relations = []entityRelationInfo{{Name: "tokens", Target: "data.entities.token", Kind: "one_to_many", ViaTable: "data.tables.token", ViaColumn: "account_id", Load: "eager"}}
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if !strings.Contains(entityErrs(c), "[entity-relation-eager-index]") {
		t.Fatalf("missing eager index not reported: %v", c.errs)
	}

	c = newChecker(true)
	e = c.entities["data.entities.account"]
	e.Relations = []entityRelationInfo{{Name: "tokens", Target: "data.entities.token", Kind: "many_to_one", ViaTable: "data.tables.token", ViaColumn: "account_id", Load: "lazy"}}
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if !strings.Contains(entityErrs(c), "[entity-relation-direction]") {
		t.Fatalf("wrong relation direction not reported: %v", c.errs)
	}
}

func TestEntityUsedByQueries(t *testing.T) {
	c := &Checker{
		entityOrder: []string{"data.entities.account"},
		entities: map[string]*entityInfo{
			"data.entities.account": {
				ID:  "data.entities.account",
				Rel: "account.yaml",
				Doc: map[string]any{"used_by": map[string]any{"queries": []any{"data.queries.account.get"}}},
			},
		},
		queries: map[string]*queryInfo{
			"data.queries.account.get": {ID: "data.queries.account.get", Return: queryReturn{Kind: "entity", EntityID: "data.entities.account"}},
		},
	}
	c.checkEntityUsedByQueries()
	if len(c.errs) != 0 {
		t.Fatalf("valid used_by reported: %v", c.errs)
	}

	c.entities["data.entities.account"].Doc = map[string]any{"used_by": map[string]any{"queries": []any{}}}
	c.checkEntityUsedByQueries()
	if !strings.Contains(entityErrs(c), "[entity-used-by]") {
		t.Fatalf("missing used_by query not reported: %v", c.errs)
	}
}

func TestEagerCycles(t *testing.T) {
	c := &Checker{
		entityOrder: []string{"a", "b"},
		entities: map[string]*entityInfo{
			"a": {ID: "a", Relations: []entityRelationInfo{{Name: "b", Target: "b", Load: "eager"}}},
			"b": {ID: "b", Relations: []entityRelationInfo{{Name: "a", Target: "a", Load: "eager"}}},
		},
	}
	c.checkEagerCycles()
	if !strings.Contains(entityErrs(c), "[entity-relation-eager-cycle]") {
		t.Fatalf("eager cycle not reported: %v", c.errs)
	}
}

func TestQueryParamsSQL(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{
		ID: "data.queries.x.q", File: "q.yaml",
		Params: []queryParam{
			{Name: "id", Type: "text"},
			{Name: "status", Type: "integer", Nullable: true},
		},
		SQL: "SELECT id FROM x WHERE id = @id AND (sqlc.narg('status')::integer IS NULL OR status = sqlc.narg('status'))",
	}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid params reported: %v", c.errs)
	}

	q.SQL = "SELECT id FROM x WHERE id = @id AND status = @status"
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "[query-param-nullable]") {
		t.Fatalf("nullable @param not reported: %v", c.errs)
	}

	c = &Checker{}
	q.SQL = "SELECT id FROM x WHERE id = @id AND other = @missing"
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "[query-param-sql]") {
		t.Fatalf("undeclared SQL param not reported: %v", c.errs)
	}
}

func TestQueryEntityCoverageAndSensitivity(t *testing.T) {
	accountEntity := func(withSecret bool) *entityInfo {
		fields := []entityFieldInfo{
			{Name: "id", ColumnTable: "data.tables.account", Column: "id", Type: "string", Required: true, Mutable: false},
			{Name: "nickname", ColumnTable: "data.tables.account", Column: "nickname", Type: "string", Required: true, Mutable: true},
		}
		if withSecret {
			fields = append(fields, entityFieldInfo{Name: "secret", ColumnTable: "data.tables.account", Column: "secret", Type: "string", Required: true, Mutable: true, NeverExpose: true})
		}
		return &entityInfo{ID: "data.entities.account", Tables: []string{"data.tables.account"}, Primary: "data.tables.account", Fields: fields}
	}
	newChecker := func(withSecret bool) *Checker {
		cols := map[string]columnMeta{
			"id":       {typ: "string", primaryKey: true},
			"nickname": {typ: "string"},
		}
		if withSecret {
			cols["secret"] = columnMeta{typ: "string", neverExpose: true}
		}
		return &Checker{
			tableColumns: map[string]map[string]columnMeta{"data.tables.account": cols},
			tableOwners:  map[string]string{"data.tables.account": "accounts"},
			entities:     map[string]*entityInfo{"data.entities.account": accountEntity(withSecret)},
		}
	}

	c := newChecker(false)
	q := &queryInfo{
		ID: "data.queries.account.get", File: "account.yaml", Module: "accounts", Kind: "one",
		Tables: []string{"data.tables.account"},
		Return: queryReturn{Raw: "data.entities.account", Kind: "entity", EntityID: "data.entities.account"},
		SQL:    "SELECT id, nickname FROM accounts WHERE id = @id",
	}
	c.checkQueryReturns(q)
	c.checkQueryReturnColumns(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid entity query reported: %v", c.errs)
	}

	q.ReturnColumns = []string{"id"}
	c.checkQueryReturnColumns(q)
	if !strings.Contains(entityErrs(c), "omits required field nickname") {
		t.Fatalf("missing required coverage not reported: %v", c.errs)
	}

	c = newChecker(true)
	q = &queryInfo{
		ID: "data.queries.account.get", File: "account.yaml", Module: "other", Kind: "one",
		Tables:        []string{"data.tables.account"},
		Return:        queryReturn{Raw: "data.entities.account", Kind: "entity", EntityID: "data.entities.account"},
		ReturnColumns: []string{"id", "nickname", "secret"},
		SQL:           "SELECT id, nickname, secret FROM accounts WHERE id = @id",
	}
	c.checkQueryReturnColumns(q)
	if !strings.Contains(entityErrs(c), "[query-sensitive-columns]") {
		t.Fatalf("cross-module never_expose not reported: %v", c.errs)
	}
}

func TestQueryKindSQLAndOneUnique(t *testing.T) {
	c := &Checker{
		tableOwners:     map[string]string{"data.tables.account": "accounts"},
		tablePrimaryKey: map[string][]string{"data.tables.account": {"id"}},
		tablePhysical:   map[string]string{"data.tables.account": "accounts"},
		tableIndexes:    map[string][]tableIndex{},
	}
	entityReturn := queryReturn{Raw: "data.entities.account", Kind: "entity", EntityID: "data.entities.account"}
	q := &queryInfo{
		ID: "data.queries.account.create", File: "account.yaml", Module: "accounts", Kind: "one",
		Tables: []string{"data.tables.account"}, Return: entityReturn,
		SQL: "INSERT INTO accounts (id) VALUES (@id) RETURNING id",
	}
	c.checkQueryKindSQL(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid INSERT RETURNING reported: %v", c.errs)
	}

	q.SQL = "INSERT INTO accounts (id) VALUES (@id)"
	c.checkQueryKindSQL(q)
	if !strings.Contains(entityErrs(c), "must use RETURNING") {
		t.Fatalf("missing RETURNING not reported: %v", c.errs)
	}

	c = &Checker{tableOwners: map[string]string{"data.tables.account": "accounts"}}
	q = &queryInfo{ID: "data.queries.account.x", File: "account.yaml", Module: "auth", Kind: "execrows", Tables: []string{"data.tables.account"}, SQL: "UPDATE accounts SET x = 1"}
	c.checkQueryKindSQL(q)
	if !strings.Contains(entityErrs(c), "[query-write-owner]") {
		t.Fatalf("cross-module write not reported: %v", c.errs)
	}

	c = &Checker{
		tablePrimaryKey: map[string][]string{"data.tables.account": {"id"}},
	}
	q = &queryInfo{
		ID: "data.queries.account.get", File: "account.yaml", Kind: "one",
		Tables: []string{"data.tables.account"}, Return: entityReturn,
		SQL: "SELECT id FROM accounts WHERE nickname = @nickname",
	}
	c.checkQueryKindSQL(q)
	if !strings.Contains(entityErrs(c), "[query-one-unique]") {
		t.Fatalf("non-unique one query not reported: %v", c.errs)
	}
	q.IndexColumns = [][]string{{"id"}}
	c.errs = nil
	c.checkQueryKindSQL(q)
	if len(c.errs) != 0 {
		t.Fatalf("unique prefix reported: %v", c.errs)
	}

	// 写目标只校验被写的表；FROM/JOIN 的读表可以跨模块。
	c = &Checker{
		tableOwners:   map[string]string{"data.tables.account": "accounts", "data.tables.other": "other"},
		tablePhysical: map[string]string{"data.tables.account": "accounts", "data.tables.other": "other"},
	}
	q = &queryInfo{
		ID: "data.queries.account.update", File: "account.yaml", Module: "accounts", Kind: "execrows",
		Tables: []string{"data.tables.account", "data.tables.other"},
		SQL:    "UPDATE accounts SET x = 1 FROM other WHERE other.id = accounts.id",
	}
	c.checkQueryKindSQL(q)
	if strings.Contains(entityErrs(c), "[query-write-owner]") {
		t.Fatalf("read-only join table should not trigger write owner: %v", c.errs)
	}
}

func TestQueryParamStringLiteralIgnored(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{
		ID: "data.queries.x.q", File: "q.yaml",
		Params: []queryParam{{Name: "id", Type: "text"}},
		SQL:    "SELECT id FROM x WHERE id = @id AND note = 'a@b.com'",
	}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("string literal treated as param: %v", c.errs)
	}
}

func TestMultiTableQueryRejectsIndexColumns(t *testing.T) {
	c := &Checker{
		queryIndexCols: map[string][][]string{"data.queries.x.q": {{"created_at"}}},
		queryTables:    map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true, "data.tables.y": true}},
	}
	c.checkQueryIndexColumns(
		map[string]string{"data.tables.x": "x.yaml", "data.tables.y": "y.yaml"},
		map[string]map[string]bool{"data.tables.x": {"created_at": true}, "data.tables.y": {"created_at": true}},
	)
	if !strings.Contains(entityErrs(c), "multi-table query must not declare index_columns") {
		t.Fatalf("multi-table index_columns not reported: %v", c.errs)
	}
}

func TestQueryOutputFields(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{
		ID: "data.queries.account.count", File: "account.yaml", Kind: "many",
		Tables: []string{"data.tables.account"},
		Return: queryReturn{Raw: "[]object", IsList: true, Kind: "object"},
		OutputFields: []queryOutputField{
			{Name: "status", Type: "integer"},
			{Name: "total", Type: "integer"},
		},
		SQL: "SELECT status, count(*) AS total FROM accounts GROUP BY status",
	}
	c.checkQueryReturns(q)
	c.checkQueryReturnColumns(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid object return reported: %v", c.errs)
	}

	c = &Checker{}
	q.OutputFields = nil
	c.checkQueryReturns(q)
	c.checkQueryReturnColumns(q)
	if !strings.Contains(entityErrs(c), "output_fields is empty") {
		t.Fatalf("missing output_fields not reported: %v", c.errs)
	}

	c = &Checker{}
	q.OutputFields = []queryOutputField{{Name: "total", Type: "data.tables.account.col.id"}}
	c.checkQueryReturnColumns(q)
	if !strings.Contains(entityErrs(c), "unsupported type") {
		t.Fatalf("invalid output type not reported: %v", c.errs)
	}
}

func TestListListStringMinItems(t *testing.T) {
	c := &Checker{}
	c.validateValue([]any{[]any{"a", "b"}}, Field{Type: "list<list<string>>", MinItems: 1}, "index_columns")
	if len(c.errs) != 0 {
		t.Fatalf("valid nested list reported: %v", c.errs)
	}

	c = &Checker{}
	c.validateValue([]any{}, Field{Type: "list<list<string>>", MinItems: 1}, "index_columns")
	if !strings.Contains(entityErrs(c), "[schema.range]") {
		t.Fatalf("min_items not reported: %v", c.errs)
	}

	c = &Checker{}
	c.validateValue([]any{[]any{1}}, Field{Type: "list<list<string>>"}, "index_columns")
	if !strings.Contains(entityErrs(c), "[schema.type]") {
		t.Fatalf("non-string inner list not reported: %v", c.errs)
	}
}
