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
			Fields: []entityFieldInfo{
				{Name: "ID", ColumnTable: "data.tables.x", Column: "id", Type: "string", Required: true, Mutable: false},
				{Name: "Own", ColumnTable: "data.tables.x", Column: "own", Type: "string", Required: true, Mutable: true},
				{Name: "Opt", ColumnTable: "data.tables.x", Column: "opt", Type: "string", Mutable: true},
				{Name: "Nickname", ColumnTable: "data.tables.x", Column: "nickname", Type: "string", Required: true, Mutable: true, PII: true},
				{Name: "secret", ColumnTable: "data.tables.x", Column: "secret", Type: "string", Required: true, Mutable: true, NeverExpose: true},
			},
		}
	}

	c := newChecker()
	e := newEntity()
	c.checkEntityFields(e)
	c.checkEntityID(e)
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
			{Name: "ID", Type: "string", Required: true, Mutable: false, HasExplicitType: true, FromBase: true},
			{Name: "Status", Type: "integer", Required: true, Mutable: true, HasExplicitType: true, FromBase: true},
			{Name: "CreatedAt", Type: "timestamp", Required: true, Mutable: false, HasExplicitType: true, FromBase: true},
			{Name: "UpdatedAt", Type: "timestamp", Required: true, Mutable: false, HasExplicitType: true, FromBase: true},
			{Name: "DeletedAt", Type: "timestamp", Mutable: false, HasExplicitType: true, FromBase: true},
		},
	}
	doc := map[string]any{
		"id":      "data.entities.x",
		"extends": []any{"data.entities.base"},
		"tables":  []any{"data.tables.x"},
		"fields": []any{
			map[string]any{"name": "Status", "type": "data.enums.x_status"},
			map[string]any{"name": "Own", "type": "string", "required": true},
		},
	}
	e := c.parseEntity(doc, "contracts/data/entities/x.yaml")
	if len(e.Fields) != 6 {
		t.Fatalf("want 6 effective fields, got %d: %#v", len(e.Fields), e.Fields)
	}
	var status *entityFieldInfo
	for i := range e.Fields {
		if e.Fields[i].Name == "Status" {
			status = &e.Fields[i]
		}
	}
	if status == nil || !status.OverridesBase || !status.Required || status.Column != "status" {
		t.Fatalf("status override not merged: %#v", status)
	}
	if e.Fields[0].Name != "ID" || !e.Fields[0].FromBase {
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
					Fields: []entityFieldInfo{{Name: "ID", ColumnTable: "data.tables.account", Column: "id", Type: "string", Required: true, Mutable: false}},
				},
				"data.entities.token": {
					ID: "data.entities.token", Rel: "token.yaml", Tables: []string{"data.tables.token"}, Primary: "data.tables.token",
					Fields: []entityFieldInfo{{Name: "ID", ColumnTable: "data.tables.token", Column: "id", Type: "string", Required: true, Mutable: false}},
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
		SQL: querySQL{Statement: "select", From: "data.tables.x", Select: []string{"id"}, Where: []string{"id = @id", "(sqlc.narg('status')::integer IS NULL OR status = sqlc.narg('status'))"}},
	}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid params reported: %v", c.errs)
	}

	q.SQL = querySQL{Statement: "select", From: "data.tables.x", Select: []string{"id"}, Where: []string{"id = @id", "status = @status"}}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "[query-param-nullable]") {
		t.Fatalf("nullable @param not reported: %v", c.errs)
	}

	c = &Checker{}
	q.SQL = querySQL{Statement: "select", From: "data.tables.x", Select: []string{"id"}, Where: []string{"id = @id", "other = @missing"}}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "[query-param-sql]") {
		t.Fatalf("undeclared SQL param not reported: %v", c.errs)
	}
}

func TestQueryEntityCoverageAndSensitivity(t *testing.T) {
	accountEntity := func(withSecret bool) *entityInfo {
		fields := []entityFieldInfo{
			{Name: "ID", ColumnTable: "data.tables.account", Column: "id", Type: "string", Required: true, Mutable: false},
			{Name: "Nickname", ColumnTable: "data.tables.account", Column: "nickname", Type: "string", Required: true, Mutable: true},
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
		SQL:    querySQL{Statement: "select", From: "data.tables.account", Select: []string{"id", "nickname"}, Where: []string{"id = @id"}},
	}
	c.checkQueryReturns(q)
	c.checkQueryReturnColumns(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid entity query reported: %v", c.errs)
	}

	q.ReturnColumns = []string{"id"}
	c.checkQueryReturnColumns(q)
	if !strings.Contains(entityErrs(c), "omits required field Nickname") {
		t.Fatalf("missing required coverage not reported: %v", c.errs)
	}

	c = newChecker(true)
	q = &queryInfo{
		ID: "data.queries.account.get", File: "account.yaml", Module: "other", Kind: "one",
		Tables:        []string{"data.tables.account"},
		Return:        queryReturn{Raw: "data.entities.account", Kind: "entity", EntityID: "data.entities.account"},
		ReturnColumns: []string{"id", "nickname", "secret"},
		SQL:           querySQL{Statement: "select", From: "data.tables.account", Select: []string{"id", "nickname", "secret"}, Where: []string{"id = @id"}},
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
		tableIndexes:    map[string][]tableIndex{},
	}
	entityReturn := queryReturn{Raw: "data.entities.account", Kind: "entity", EntityID: "data.entities.account"}
	q := &queryInfo{
		ID: "data.queries.account.create", File: "account.yaml", Module: "accounts", Kind: "one",
		Tables: []string{"data.tables.account"}, Return: entityReturn,
		SQL: querySQL{Statement: "insert", Into: "data.tables.account", Columns: []string{"id"}, Values: []string{"@id"}, Returning: []string{"id"}},
	}
	c.checkQueryKindSQL(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid INSERT RETURNING reported: %v", c.errs)
	}

	q.SQL = querySQL{Statement: "insert", Into: "data.tables.account", Columns: []string{"id"}, Values: []string{"@id"}}
	c.checkQueryKindSQL(q)
	if !strings.Contains(entityErrs(c), "must use RETURNING") {
		t.Fatalf("missing RETURNING not reported: %v", c.errs)
	}

	c = &Checker{tableOwners: map[string]string{"data.tables.account": "accounts"}}
	q = &queryInfo{ID: "data.queries.account.x", File: "account.yaml", Module: "auth", Kind: "execrows", Tables: []string{"data.tables.account"}, SQL: querySQL{Statement: "update", From: "data.tables.account", Set: []string{"x = 1"}, Where: []string{"id = @id"}}}
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
		SQL: querySQL{Statement: "select", From: "data.tables.account", Select: []string{"id"}, Where: []string{"nickname = @nickname"}},
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
		tableOwners: map[string]string{"data.tables.account": "accounts", "data.tables.other": "other"},
	}
	q = &queryInfo{
		ID: "data.queries.account.update", File: "account.yaml", Module: "accounts", Kind: "execrows",
		Tables: []string{"data.tables.account", "data.tables.other"},
		SQL:    querySQL{Statement: "update", From: "data.tables.account", Set: []string{"x = 1"}, Where: []string{"id = @id"}, Joins: []queryJoin{{Table: "data.tables.other", On: []string{"other.id = account.id"}}}},
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
		SQL:    querySQL{Statement: "select", From: "data.tables.x", Select: []string{"id"}, Where: []string{"id = @id AND note = 'a@b.com'"}},
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
	c := &Checker{tableTypeFamilies: map[string]bool{"integer": true}}
	q := &queryInfo{
		ID: "data.queries.account.count", File: "account.yaml", Kind: "many",
		Tables: []string{"data.tables.account"},
		Return: queryReturn{Raw: "[]object", IsList: true, Kind: "object"},
		OutputFields: []queryOutputField{
			{Name: "status", Type: "integer"},
			{Name: "total", Type: "integer"},
		},
		SQL: querySQL{Statement: "select", From: "data.tables.account", Select: []string{"status", "count(*) AS total"}, Where: []string{"status = @status"}},
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

func TestQuerySQLShape(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.account": {"id": {typ: "string"}, "nickname": {typ: "string"}},
			"data.tables.token":   {"id": {typ: "string"}, "account_id": {typ: "string"}},
		},
	}
	q := &queryInfo{
		ID: "data.queries.account.get", File: "account.yaml", Kind: "one",
		Tables: []string{"data.tables.account"},
		Return: queryReturn{Raw: "data.entities.account", Kind: "entity"},
		SQL: querySQL{
			Statement: "select", From: "data.tables.account",
			Select: []string{"id", "nickname"}, Where: []string{"id = @id"},
		},
	}
	c.checkQuerySQLShape(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid select reported: %v", c.errs)
	}

	q.SQL.Raw = "SELECT * FROM accounts"
	c.checkQuerySQLShape(q)
	if !strings.Contains(entityErrs(c), "sql must be an object") {
		t.Fatalf("raw sql not reported: %v", c.errs)
	}

	c.errs = nil
	q.SQL = querySQL{Statement: "insert", Into: "data.tables.account", Columns: []string{"id"}, Values: []string{"@id", "@x"}}
	c.checkQuerySQLShape(q)
	if !strings.Contains(entityErrs(c), "columns/values length mismatch") {
		t.Fatalf("value length mismatch not reported: %v", c.errs)
	}

	c.errs = nil
	q.SQL = querySQL{Statement: "update", From: "data.tables.account", Set: []string{"nickname = @nickname"}}
	c.checkQuerySQLShape(q)
	if !strings.Contains(entityErrs(c), "update requires where") {
		t.Fatalf("update without where not reported: %v", c.errs)
	}

	c.errs = nil
	q.SQL = querySQL{Statement: "select", From: "data.tables.account", Select: []string{"*"}}
	c.checkQuerySQLShape(q)
	if !strings.Contains(entityErrs(c), "must not use *") {
		t.Fatalf("select star not reported: %v", c.errs)
	}

	c.errs = nil
	q.SQL = querySQL{Statement: "select", From: "data.tables.account", Select: []string{"id, nickname"}}
	c.checkQuerySQLShape(q)
	if !strings.Contains(entityErrs(c), "plain column") {
		t.Fatalf("composite select not reported: %v", c.errs)
	}
}

func TestNeverExposeFieldMustBeUnexported(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {
			"id":     {typ: "string", primaryKey: true},
			"secret": {typ: "string", neverExpose: true},
		},
	}}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "contracts/data/entities/x.yaml",
		Tables: []string{"data.tables.x"}, Primary: "data.tables.x",
		Fields: []entityFieldInfo{
			{Name: "ID", ColumnTable: "data.tables.x", Column: "id", Type: "string", Required: true, Mutable: false},
			{Name: "Secret", ColumnTable: "data.tables.x", Column: "secret", Type: "string", Required: true, NeverExpose: true},
		},
	}
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "never_expose field must start lowercase") {
		t.Fatalf("exported never_expose field not reported: %v", c.errs)
	}
}

func TestEntityName(t *testing.T) {
	c := &Checker{}
	c.checkEntityName(&entityInfo{Rel: "x.yaml"})
	if !strings.Contains(entityErrs(c), "name is required") {
		t.Fatalf("missing entity name not reported: %v", c.errs)
	}
	c.errs = nil
	c.checkEntityName(&entityInfo{Rel: "x.yaml", Name: "bad_name"})
	if !strings.Contains(entityErrs(c), "must be a valid non-keyword Go identifier") {
		t.Fatalf("invalid entity name not reported: %v", c.errs)
	}
	c.errs = nil
	c.checkEntityName(&entityInfo{Rel: "x.yaml", Name: "type"})
	if !strings.Contains(entityErrs(c), "non-keyword") {
		t.Fatalf("keyword entity name not reported: %v", c.errs)
	}
	c.errs = nil
	c.checkEntityName(&entityInfo{Rel: "x.yaml", Name: "Account"})
	if len(c.errs) != 0 {
		t.Fatalf("valid entity name reported: %v", c.errs)
	}
}

func TestEntityTypeReusesTableFamilies(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {"id": {typ: "string", primaryKey: true}, "code": {typ: "string"}},
		},
		tableTypeFamilies: map[string]bool{"string": true, "integer": true},
	}
	e := &entityInfo{ID: "data.entities.x", Rel: "x.yaml", Tables: []string{"data.tables.x"}, Primary: "data.tables.x", Fields: []entityFieldInfo{
		{Name: "ID", ColumnTable: "data.tables.x", Column: "id", Type: "string", Required: true, Mutable: false},
		{Name: "Code", ColumnTable: "data.tables.x", Column: "code", Type: "uuid", HasExplicitType: true, Required: true},
	}}
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "unknown entity type uuid") {
		t.Fatalf("non-table-family type not reported: %v", c.errs)
	}
}

func TestEntityFieldMetadata(t *testing.T) {
	c := &Checker{}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "contracts/data/entities/x.yaml",
		Fields: []entityFieldInfo{
			{Name: "ID", Type: "string"},
			{Name: "Email", Type: "string", JSONName: "emailAddress"},
			{Name: "DupJSON", Type: "string", JSONName: "emailAddress"},
			{Name: "BadExample", Type: "integer", Examples: []any{"x"}},
			{Name: "type", Type: "string"},
			{Name: "Status", Type: "data.enums.x_status", Examples: []any{1}},
		},
	}
	c.checkEntityFieldMetadata(e)
	errs := entityErrs(c)
	for _, want := range []string{
		`json_name "emailAddress" already used by field Email`,
		"example x does not match type integer",
		`field name "type" must be a valid non-keyword Go identifier`,
	} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %q in:\n%s", want, errs)
		}
	}
	if strings.Contains(errs, "Status") {
		t.Fatalf("enum example reported: %v", c.errs)
	}
}

func TestStatusOverrideInheritsBaseMetadata(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {
				"id":     {typ: "string", primaryKey: true},
				"status": {typ: "integer"},
			},
		},
		entityBaseFields: []entityFieldInfo{
			{Name: "ID", Type: "string", Required: true, Mutable: false},
			{Name: "Status", Type: "integer", Required: true, Mutable: true, Description: "base status"},
		},
	}
	doc := map[string]any{
		"id":     "data.entities.x",
		"tables": []any{"data.tables.x"},
		"fields": []any{
			map[string]any{"name": "Status", "type": "data.enums.x_status"},
		},
	}
	e := c.parseEntity(doc, "contracts/data/entities/x.yaml")
	status := findEntityField(e, "Status")
	if status == nil || !status.OverridesBase {
		t.Fatalf("status not merged: %#v", status)
	}
	if status.Description != "base status" {
		t.Fatalf("base metadata not inherited: %#v", status)
	}
}

func TestMultiTableEntityProjection(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"id": {typ: "string", primaryKey: true}, "own": {typ: "string"}},
		"data.tables.y": {"extra": {typ: "string"}},
	}}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "contracts/data/entities/x.yaml",
		Tables: []string{"data.tables.x", "data.tables.y"}, Primary: "data.tables.x",
		Fields: []entityFieldInfo{
			{Name: "ID", ColumnTable: "data.tables.x", Column: "id", Type: "string", Required: true, Mutable: false},
			{Name: "Own", ColumnTable: "data.tables.x", Column: "own", Type: "string", Required: true},
			{Name: "Extra", ColumnTable: "data.tables.y", Column: "extra", Type: "string", Required: true},
		},
	}
	c.checkEntityFields(e)
	c.checkEntityID(e)
	if len(c.errs) != 0 {
		t.Fatalf("valid multi-table entity reported: %v", c.errs)
	}
}

func TestSnakeCase(t *testing.T) {
	cases := map[string]string{
		"ID": "id", "OwnID": "own_id", "UserID": "user_id", "TLD": "tld",
		"CreatedAt": "created_at", "AccountID": "account_id", "PasswordHash": "password_hash",
	}
	for in, want := range cases {
		if got := snakeCase(in); got != want {
			t.Fatalf("snakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}
