package check

import (
	"path/filepath"
	"strings"
	"testing"
)

func entityErrs(c *Checker) string {
	return strings.Join(c.errs, "\n")
}

// scalarField 构造合法的标量字段：column 来源 + column 校验 + 描述。
func scalarField(name, typ, table, col string) entityFieldInfo {
	ref := table + ".col." + col
	return entityFieldInfo{
		Name: name, Type: typ, HasType: true,
		SourceColumnRef: ref, ColumnTable: table, Column: col,
		ValidationKind: "column", ValidationRef: ref, ValidationCount: 1,
		Mutable: true, Description: name,
	}
}

// baseScalarField 构造基类字段；source/validation 指向 data.tables.base。
func baseScalarField(name, typ string, mutable bool) entityFieldInfo {
	f := scalarField(name, typ, "data.tables.base", snakeCase(name))
	f.Mutable, f.HasMutable = mutable, true
	f.FromBase = true
	return f
}

func relationField(name, targetTable, targetEntity, fromTable, fromCol, toTable, toCol, load string) entityFieldInfo {
	return entityFieldInfo{
		Name: name, IsRelation: true, Target: targetTable,
		Type: targetEntity, HasType: true,
		JoinFromRef: fromTable + ".col." + fromCol, JoinFromTable: fromTable, JoinFromColumn: fromCol,
		JoinToRef: toTable + ".col." + toCol, JoinToTable: toTable, JoinToColumn: toCol,
		Load: load, Description: name, Mutable: true,
	}
}

func TestEntityFieldProjectionRules(t *testing.T) {
	newChecker := func() *Checker {
		return &Checker{
			tableColumns: map[string]map[string]columnMeta{
				"data.tables.x": {
					"id":       {typ: "string", primaryKey: true},
					"own":      {typ: "string"},
					"opt":      {typ: "string", nullable: true},
					"nickname": {typ: "string", pii: true},
					"secret":   {typ: "string", neverExpose: true},
				},
			},
			tableTypeFamilies: map[string]bool{"string": true, "integer": true, "timestamp": true},
		}
	}
	newEntity := func() *entityInfo {
		return &entityInfo{
			ID:      "data.entities.x",
			Rel:     "contracts/data/entities/x.yaml",
			Tables:  []string{"data.tables.x"},
			Primary: "data.tables.x",
			Fields: []entityFieldInfo{
				func() entityFieldInfo {
					f := scalarField("ID", "string", "data.tables.x", "id")
					f.Mutable = false
					return f
				}(),
				scalarField("Own", "string", "data.tables.x", "own"),
				scalarField("Opt", "*string", "data.tables.x", "opt"),
				func() entityFieldInfo {
					f := scalarField("Nickname", "string", "data.tables.x", "nickname")
					f.PII = true
					return f
				}(),
				func() entityFieldInfo {
					f := scalarField("secret", "string", "data.tables.x", "secret")
					f.NeverExpose = true
					return f
				}(),
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
	e.Fields[1].Type = "*string"
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "pointer type") {
		t.Fatalf("pointer/nullable mismatch not reported: %v", c.errs)
	}

	c = newChecker()
	e = newEntity()
	e.Fields[4].NeverExpose = false
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "never_expose") {
		t.Fatalf("never_expose relaxation not reported: %v", c.errs)
	}

	c = newChecker()
	e = newEntity()
	e.Fields[1].Description = ""
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "[entity-field-description]") {
		t.Fatalf("missing description not reported: %v", c.errs)
	}

	c = newChecker()
	e = newEntity()
	e.Fields[1].ValidationCount = 0
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "[entity-field-validation]") {
		t.Fatalf("missing validation not reported: %v", c.errs)
	}
}

func TestEntityComputedField(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {"id": {typ: "string", primaryKey: true}},
		},
		tableTypeFamilies: map[string]bool{"string": true, "integer": true},
	}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "x.yaml", Tables: []string{"data.tables.x"}, Primary: "data.tables.x",
		Fields: []entityFieldInfo{
			func() entityFieldInfo {
				f := scalarField("ID", "string", "data.tables.x", "id")
				f.Mutable = false
				return f
			}(),
			{Name: "Total", Type: "*integer", HasType: true, SourceComputed: "COUNT(*)", Mutable: false, HasMutable: true,
				ValidationKind: "soft", ValidationRef: "非负", ValidationCount: 1, Description: "总数"},
		},
	}
	c.checkEntityFields(e)
	if len(c.errs) != 0 {
		t.Fatalf("valid computed field reported: %v", c.errs)
	}

	e.Fields[1].Type = "integer"
	c.errs = nil
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "computed field must use a pointer type") {
		t.Fatalf("non-pointer computed field not reported: %v", c.errs)
	}

	e.Fields[1].Type = "*integer"
	e.Fields[1].Mutable = true
	c.errs = nil
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "computed field must set mutable:false") {
		t.Fatalf("mutable computed field not reported: %v", c.errs)
	}

	e.Fields[1].Mutable, e.Fields[1].HasMutable = false, true
	e.Fields[1].SourceComputed = "COUNT(*) + @n"
	c.errs = nil
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "must not reference @params") {
		t.Fatalf("computed param reference not reported: %v", c.errs)
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
			baseScalarField("ID", "string", false),
			baseScalarField("Status", "integer", true),
			baseScalarField("CreatedAt", "timestamp", false),
			baseScalarField("UpdatedAt", "timestamp", false),
			baseScalarField("DeletedAt", "*timestamp", false),
		},
	}
	doc := map[string]any{
		"id": "data.entities.x",
		"model": map[string]any{
			"name":    "x",
			"extends": []any{"data.entities.base"},
			"tables":  []any{"data.tables.x"},
		},
		"fields": []any{
			map[string]any{"name": "Status", "type": "data.enums.x_status"},
			map[string]any{
				"name": "Own", "json_name": "own", "type": "string", "description": "own",
				"source":     map[string]any{"column": "data.tables.x.col.own"},
				"validation": map[string]any{"column": "data.tables.x.col.own"},
			},
		},
	}
	e := c.parseEntity(doc, "contracts/data/entities/x.yaml")
	if len(e.Fields) != 6 {
		t.Fatalf("want 6 effective fields, got %d: %#v", len(e.Fields), e.Fields)
	}
	status := findEntityField(e, "Status")
	if status == nil || !status.OverridesBase || status.Column != "status" {
		t.Fatalf("status override not merged: %#v", status)
	}
	if status.Type != "data.enums.x_status" {
		t.Fatalf("status enum type lost: %#v", status)
	}
	if e.Fields[0].Name != "ID" || !e.Fields[0].FromBase || e.Fields[0].ColumnTable != "data.tables.x" {
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
					Fields: []entityFieldInfo{func() entityFieldInfo {
						f := scalarField("ID", "string", "data.tables.account", "id")
						f.Mutable = false
						return f
					}()},
				},
				"data.entities.token": {
					ID: "data.entities.token", Rel: "token.yaml", Tables: []string{"data.tables.token"}, Primary: "data.tables.token",
					Fields: []entityFieldInfo{func() entityFieldInfo {
						f := scalarField("ID", "string", "data.tables.token", "id")
						f.Mutable = false
						return f
					}()},
				},
			},
		}
	}

	c := newChecker(true)
	e := c.entities["data.entities.account"]
	e.Fields = append(e.Fields, relationField("Tokens", "data.tables.token", "data.entities.token", "data.tables.account", "id", "data.tables.token", "account_id", "eager"))
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid eager relation reported: %v", c.errs)
	}

	c = newChecker(false)
	e = c.entities["data.entities.account"]
	e.Fields = append(e.Fields, relationField("Tokens", "data.tables.token", "data.entities.token", "data.tables.account", "id", "data.tables.token", "account_id", "eager"))
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if !strings.Contains(entityErrs(c), "[entity-relation-eager-index]") {
		t.Fatalf("missing eager index not reported: %v", c.errs)
	}

	c = newChecker(true)
	e = c.entities["data.entities.token"]
	e.Fields = append(e.Fields, relationField("Account", "data.tables.account", "data.entities.account", "data.tables.token", "account_id", "data.tables.account", "id", "lazy"))
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid many_to_one reported: %v", c.errs)
	}
	if got := c.relationCardinality(&entityFieldInfo{
		JoinFromTable: "data.tables.token", JoinFromColumn: "account_id",
		JoinToTable: "data.tables.token", JoinToColumn: "account_id",
	}); got != "" {
		t.Fatalf("non-unique both sides should be invalid, got %s", got)
	}
}

func TestEntityRelationJoinColumnTypeMismatch(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.account": {"id": {typ: "string", primaryKey: true}},
			"data.tables.token":   {"id": {typ: "string", primaryKey: true}, "account_id": {typ: "integer"}},
		},
		entities: map[string]*entityInfo{
			"data.entities.account": {ID: "data.entities.account", Tables: []string{"data.tables.account"}, Primary: "data.tables.account",
				Fields: []entityFieldInfo{func() entityFieldInfo {
					f := scalarField("ID", "string", "data.tables.account", "id")
					f.Mutable = false
					return f
				}()}},
			"data.entities.token": {ID: "data.entities.token", Tables: []string{"data.tables.token"}, Primary: "data.tables.token",
				Fields: []entityFieldInfo{func() entityFieldInfo {
					f := scalarField("ID", "string", "data.tables.token", "id")
					f.Mutable = false
					return f
				}()}},
		},
	}
	e := c.entities["data.entities.account"]
	e.Fields = append(e.Fields, relationField("Tokens", "data.tables.token", "data.entities.token", "data.tables.account", "id", "data.tables.token", "account_id", "lazy"))
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if !strings.Contains(entityErrs(c), "[entity-relation-via]") || !strings.Contains(entityErrs(c), "does not match") {
		t.Fatalf("join_column type mismatch not reported: %v", c.errs)
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
			"a": {ID: "a", Fields: []entityFieldInfo{relationField("B", "data.tables.b", "b", "data.tables.a", "b_id", "data.tables.b", "a_id", "eager")}},
			"b": {ID: "b", Fields: []entityFieldInfo{relationField("A", "data.tables.a", "a", "data.tables.b", "a_id", "data.tables.a", "b_id", "eager")}},
		},
	}
	c.checkEagerCycles()
	if !strings.Contains(entityErrs(c), "[entity-relation-eager-cycle]") {
		t.Fatalf("eager cycle not reported: %v", c.errs)
	}
}

func TestQueryEntityCoverageAndSensitivity(t *testing.T) {
	accountEntity := func(withSecret bool) *entityInfo {
		fields := []entityFieldInfo{
			func() entityFieldInfo {
				f := scalarField("ID", "string", "data.tables.account", "id")
				f.Mutable = false
				return f
			}(),
			scalarField("Nickname", "string", "data.tables.account", "nickname"),
		}
		if withSecret {
			f := scalarField("secret", "string", "data.tables.account", "secret")
			f.NeverExpose = true
			fields = append(fields, f)
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
		ID: "data.queries.account.get", File: "account.yaml", OwnerModule: "accounts",
		Targets: []string{"data.tables.account"},
		Return:  queryReturn{Raw: "data.entities.account", Kind: "entity", EntityID: "data.entities.account"},
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
		ID: "data.queries.account.get", File: "account.yaml", OwnerModule: "other",
		Targets:       []string{"data.tables.account"},
		Return:        queryReturn{Raw: "data.entities.account", Kind: "entity", EntityID: "data.entities.account"},
		ReturnColumns: []string{"id", "nickname", "secret"},
	}
	c.checkQueryReturnColumns(q)
	if !strings.Contains(entityErrs(c), "[query-sensitive-columns]") {
		t.Fatalf("cross-module never_expose not reported: %v", c.errs)
	}
}

func TestEntityCoverageComputedSensitive(t *testing.T) {
	e := &entityInfo{ID: "data.entities.x", OwnerModule: "accounts", Tables: []string{"data.tables.x"}, Primary: "data.tables.x",
		Fields: []entityFieldInfo{
			func() entityFieldInfo {
				f := scalarField("ID", "string", "data.tables.x", "id")
				f.Mutable = false
				return f
			}(),
			{Name: "secretTotal", Type: "*integer", HasType: true, SourceComputed: "COUNT(*)", Mutable: false, HasMutable: true,
				NeverExpose: true, ValidationKind: "soft", ValidationRef: "非负", ValidationCount: 1, Description: "敏感聚合"},
		}}
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"id": {typ: "string", primaryKey: true}},
	}}
	q := &queryInfo{ID: "q", File: "q.yaml", OwnerModule: "other", Targets: []string{"data.tables.x"},
		Return: queryReturn{Raw: "data.entities.x", Kind: "entity", EntityID: "data.entities.x"}}
	c.checkEntityCoverage(q, e, nil)
	if !strings.Contains(entityErrs(c), "[query-sensitive-columns]") || !strings.Contains(entityErrs(c), "computed never_expose") {
		t.Fatalf("computed never_expose field not reported: %v", c.errs)
	}

	c.errs = nil
	q.OwnerModule = "accounts"
	c.checkEntityCoverage(q, e, nil)
	if len(c.errs) != 0 {
		t.Fatalf("same-module computed never_expose reported: %v", c.errs)
	}
}

func TestQueryIndexRefAllowsMultiTable(t *testing.T) {
	c := &Checker{
		queryOrder: []string{"data.queries.x.q"},
		queries: map[string]*queryInfo{
			"data.queries.x.q": {ID: "data.queries.x.q", Targets: []string{"data.tables.x", "data.tables.y"},
				IndexRef: []string{"data.tables.x.idx_x_code"}},
		},
		queryTables: map[string]map[string]bool{"data.queries.x.q": {"data.tables.x": true, "data.tables.y": true}},
		tableIndexes: map[string][]tableIndex{"data.tables.x": {
			{name: "idx_x_code", columns: []string{"code"}, usedBy: map[string]bool{"data.queries.x.q": true}},
		}},
	}
	c.checkQueryIndexRefs()
	if len(c.errs) != 0 {
		t.Fatalf("multi-table index ref reported: %v", c.errs)
	}

	c.queries["data.queries.x.q"].IndexRef = nil
	c.tableIndexes["data.tables.x"][0].usedBy = map[string]bool{}
	c.checkQueryIndexRefs()
	if len(c.errs) != 0 {
		t.Fatalf("multi-table query without index_ref reported: %v", c.errs)
	}
}

func TestQueryOutputFields(t *testing.T) {
	c := &Checker{tableTypeFamilies: map[string]bool{"integer": true}}
	q := &queryInfo{
		ID: "data.queries.account.count", File: "account.yaml",
		Return: queryReturn{Raw: "[]object", IsList: true, Kind: "object"},
		OutputFields: []queryOutputField{
			{Name: "status", Type: "integer"},
			{Name: "total", Type: "integer"},
		},
	}
	c.checkQueryOutputFields(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid object return reported: %v", c.errs)
	}

	c = &Checker{}
	q.OutputFields = nil
	c.checkQueryOutputFields(q)
	if !strings.Contains(entityErrs(c), "output_fields is empty") {
		t.Fatalf("missing output_fields not reported: %v", c.errs)
	}

	c = &Checker{}
	q.OutputFields = []queryOutputField{{Name: "total", Type: "data.tables.account.col.id"}}
	c.checkQueryOutputFields(q)
	if !strings.Contains(entityErrs(c), "unsupported type") {
		t.Fatalf("invalid output type not reported: %v", c.errs)
	}

	c = &Checker{}
	q.OutputFields = []queryOutputField{{Name: "total", Type: "integer"}, {Name: "total", Type: "integer"}}
	c.checkQueryOutputFields(q)
	if !strings.Contains(entityErrs(c), "duplicate output field") {
		t.Fatalf("duplicate output field not reported: %v", c.errs)
	}
}

func TestListListStringMinItems(t *testing.T) {
	c := &Checker{}
	c.validateValue([]any{[]any{"a", "b"}}, Field{Type: "list<list<string>>", MinItems: 1}, "list")
	if len(c.errs) != 0 {
		t.Fatalf("valid nested list reported: %v", c.errs)
	}

	c = &Checker{}
	c.validateValue([]any{}, Field{Type: "list<list<string>>", MinItems: 1}, "list")
	if !strings.Contains(entityErrs(c), "[schema.range]") {
		t.Fatalf("min_items not reported: %v", c.errs)
	}

	c = &Checker{}
	c.validateValue([]any{[]any{1}}, Field{Type: "list<list<string>>"}, "list")
	if !strings.Contains(entityErrs(c), "[schema.type]") {
		t.Fatalf("non-string inner list not reported: %v", c.errs)
	}
}

func TestQueryParamColumnBinding(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {
			"id":     {typ: "string", primaryKey: true},
			"number": {typ: "integer"},
		},
	}}
	q := &queryInfo{ID: "q", File: "q.yaml", Targets: []string{"data.tables.x"},
		Params: []queryParam{{Name: "id", Type: "text", Column: "data.tables.x.col.id"}}}
	c.checkQueryParamColumns(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid param column reported: %v", c.errs)
	}

	q.Params[0].Type = "integer"
	c.errs = nil
	c.checkQueryParamColumns(q)
	if !strings.Contains(entityErrs(c), "[query-param-column]") {
		t.Fatalf("param/column type mismatch not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[0] = queryParam{Name: "id", Type: "text", Column: "data.tables.y.col.id"}
	c.checkQueryParamColumns(q)
	if !strings.Contains(entityErrs(c), "does not belong to query tables") {
		t.Fatalf("foreign param column not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[0] = queryParam{Name: "ids", Type: "List<text>", Column: "data.tables.x.col.id"}
	c.checkQueryParamColumns(q)
	if len(c.errs) != 0 {
		t.Fatalf("array param element binding reported: %v", c.errs)
	}
}

func TestEntityFieldDefaults(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {
			"id":         {typ: "string", primaryKey: true, comment: "主键"},
			"nickname":   {typ: "string", pii: true, comment: "昵称"},
			"deleted_at": {typ: "timestamp", nullable: true, comment: "删除时间"},
		},
	}}
	e := &entityInfo{Tables: []string{"data.tables.x"}, Primary: "data.tables.x", Fields: []entityFieldInfo{
		{Name: "ID", SourceColumnRef: "data.tables.x.col.id", ColumnTable: "data.tables.x", Column: "id", Mutable: false, HasMutable: true},
		{Name: "Nickname", SourceColumnRef: "data.tables.x.col.nickname", ColumnTable: "data.tables.x", Column: "nickname"},
		{Name: "DeletedAt", SourceColumnRef: "data.tables.x.col.deleted_at", ColumnTable: "data.tables.x", Column: "deleted_at"},
	}}
	c.applyEntityFieldDefaults(e)
	if e.Fields[0].JSONName != "id" || e.Fields[0].Type != "string" || e.Fields[0].ValidationKind != "column" || e.Fields[0].Description != "主键" {
		t.Fatalf("base defaults not applied: %#v", e.Fields[0])
	}
	if e.Fields[1].PII != true || e.Fields[1].Type != "string" || e.Fields[1].JSONName != "nickname" {
		t.Fatalf("pii/type defaults not applied: %#v", e.Fields[1])
	}
	if e.Fields[2].Type != "*timestamp" || e.Fields[2].JSONName != "deleted_at" || e.Fields[2].Description != "删除时间" {
		t.Fatalf("nullable defaults not applied: %#v", e.Fields[2])
	}
}

func TestQueryPreviousNames(t *testing.T) {
	q := parseQuery(map[string]any{
		"name":           "GetAccount",
		"previous_names": []any{"OldAccount"},
	}, "q.yaml", "data.queries.account", "accounts")
	if q.ID != "data.queries.account.get_account" || len(q.PreviousNames) != 1 {
		t.Fatalf("previous_names not parsed: %#v", q)
	}
	c := &Checker{
		queries:      map[string]*queryInfo{q.ID: q},
		queryAliases: map[string]string{"data.queries.account.old_account": q.ID},
	}
	if got := c.resolveQueryID("data.queries.account.old_account"); got != q.ID {
		t.Fatalf("alias resolved to %q", got)
	}
	if got := c.resolveQueryID(q.ID); got != q.ID {
		t.Fatalf("current id resolved to %q", got)
	}

	names := map[string]string{}
	c.checkQueryIdentity(q, names)
	if len(c.errs) != 0 {
		t.Fatalf("valid previous_names reported: %v", c.errs)
	}
	q.PreviousNames = []string{"GetAccount"}
	c.errs = nil
	c.checkQueryIdentity(q, names)
	if !strings.Contains(entityErrs(c), "must not repeat current name") {
		t.Fatalf("previous name equal to current not reported: %v", c.errs)
	}
}

func TestWriteQueries(t *testing.T) {
	q := &queryInfo{ID: "data.queries.x.create", Targets: []string{"data.tables.x"}}
	c := &Checker{
		tableOwners: map[string]string{"data.tables.x": "x"},
		queries:     map[string]*queryInfo{q.ID: q},
	}
	declared := map[string]bool{q.ID: true}
	c.checkWriteQueries(map[string]any{"module": "x"}, []any{q.ID}, declared, []string{"data.tables.x"}, "persistence.write_queries", "persistence.queries", "cap.yaml")
	if len(c.errs) != 0 {
		t.Fatalf("valid write query reported: %v", c.errs)
	}

	c.errs = nil
	c.checkWriteQueries(map[string]any{"module": "other"}, []any{q.ID}, declared, []string{"data.tables.x"}, "persistence.write_queries", "persistence.queries", "cap.yaml")
	if !strings.Contains(entityErrs(c), "cannot write") {
		t.Fatalf("cross-module write not reported: %v", c.errs)
	}

	c.errs = nil
	c.checkWriteQueries(map[string]any{"module": "x"}, []any{q.ID}, map[string]bool{}, []string{"data.tables.x"}, "persistence.write_queries", "persistence.queries", "cap.yaml")
	if !strings.Contains(entityErrs(c), "must be listed") {
		t.Fatalf("undeclared write query not reported: %v", c.errs)
	}
}

func TestQueryTableShape(t *testing.T) {
	c := &Checker{
		baseID: "data.tables.base",
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {"id": {typ: "string", primaryKey: true}},
			"data.tables.y": {"id": {typ: "string", primaryKey: true}},
		},
	}
	q := &queryInfo{ID: "q", File: "q.yaml", Targets: []string{"data.tables.x", "data.tables.y"}}
	c.checkQueryTable(q)
	if len(c.errs) != 0 {
		t.Fatalf("multi-table read reported: %v", c.errs)
	}

	c.errs = nil
	q = &queryInfo{ID: "q", File: "q.yaml", Targets: []string{"data.tables.base"}}
	c.checkQueryTable(q)
	if !strings.Contains(entityErrs(c), "targets references data.tables.base") {
		t.Fatalf("base target not reported: %v", c.errs)
	}
}

func TestQueryIndexRefAlias(t *testing.T) {
	current := "data.queries.x.get"
	old := "data.queries.x.old_get"
	c := &Checker{
		queryOrder:   []string{current},
		queries:      map[string]*queryInfo{current: {ID: current, Targets: []string{"data.tables.x"}, IndexRef: []string{"data.tables.x.idx_x_code"}}},
		queryAliases: map[string]string{old: current},
		queryTables:  map[string]map[string]bool{current: {"data.tables.x": true}},
		tableIndexes: map[string][]tableIndex{"data.tables.x": {
			{name: "idx_x_code", columns: []string{"code"}, usedBy: map[string]bool{old: true}},
		}},
	}
	c.checkQueryIndexRefs()
	if len(c.errs) != 0 {
		t.Fatalf("alias in index.used_by reported: %v", c.errs)
	}
}

func TestQueryDerivedIDAndListSyntax(t *testing.T) {
	q := parseQuery(map[string]any{"name": "GetAccount"}, "q.yaml", "data.queries.account", "accounts")
	if q.ID != "data.queries.account.get_account" {
		t.Fatalf("derived query id = %q", q.ID)
	}
	r := parseQueryReturn("List<data.entities.account>")
	if !r.IsList || r.Kind != "entity" || r.EntityID != "data.entities.account" {
		t.Fatalf("List<entity> parsed as %#v", r)
	}
	r = parseQueryReturn("List<object>")
	if !r.IsList || r.Kind != "object" {
		t.Fatalf("List<object> parsed as %#v", r)
	}
	if r := parseQueryReturn("List<affected_rows>"); !r.IsList || r.Raw != "affected_rows" {
		t.Fatalf("List<affected_rows> parsed as %#v", r)
	}
}

func TestQueryClauseShape(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{ID: "q", File: "q.yaml", Targets: []string{"data.tables.x"},
		Return: queryReturn{Raw: "boolean", Kind: "scalar"}}
	c.checkQueryShape(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid scalar reported: %v", c.errs)
	}

	q.Return = queryReturn{Raw: "affected_rows", Kind: "scalar"}
	c.checkQueryShape(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid affected_rows reported: %v", c.errs)
	}

	q.Return.IsList = true
	c.errs = nil
	c.checkQueryShape(q)
	if !strings.Contains(entityErrs(c), "affected_rows must not be a list") {
		t.Fatalf("list affected_rows not reported: %v", c.errs)
	}

	q = &queryInfo{ID: "q", File: "q.yaml", Targets: []string{"data.tables.x"},
		Return: queryReturn{Raw: "affected_rows", Kind: "scalar"}, ReturnColumns: []string{"id"}}
	c.errs = nil
	c.checkQueryShape(q)
	if !strings.Contains(entityErrs(c), "must not declare return_columns") {
		t.Fatalf("affected_rows with return_columns not reported: %v", c.errs)
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
			func() entityFieldInfo {
				f := scalarField("ID", "string", "data.tables.x", "id")
				f.Mutable = false
				return f
			}(),
			func() entityFieldInfo {
				f := scalarField("Secret", "string", "data.tables.x", "secret")
				f.NeverExpose = true
				return f
			}(),
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
		func() entityFieldInfo {
			f := scalarField("ID", "string", "data.tables.x", "id")
			f.Mutable = false
			return f
		}(),
		func() entityFieldInfo {
			f := scalarField("Code", "uuid", "data.tables.x", "code")
			return f
		}(),
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
			{Name: "ID", Type: "string", JSONName: "id"},
			{Name: "Email", Type: "string", JSONName: "emailAddress"},
			{Name: "DupJSON", Type: "string", JSONName: "emailAddress"},
			{Name: "BadExample", Type: "integer", JSONName: "bad_example", Examples: []any{"x"}},
			{Name: "type", Type: "string", JSONName: "type"},
			{Name: "Status", Type: "data.enums.x_status", JSONName: "status", Examples: []any{1}},
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
			baseScalarField("ID", "string", false),
			baseScalarField("Status", "integer", true),
		},
	}
	for i := range c.entityBaseFields {
		if c.entityBaseFields[i].Name == "Status" {
			c.entityBaseFields[i].Description = "base status"
		}
	}
	doc := map[string]any{
		"id": "data.entities.x",
		"model": map[string]any{
			"name":    "x",
			"extends": []any{"data.entities.base"},
			"tables":  []any{"data.tables.x"},
		},
		"fields": []any{
			map[string]any{"name": "Status", "json_name": "status", "type": "data.enums.x_status"},
		},
	}
	e := c.parseEntity(doc, "contracts/data/entities/x.yaml")
	status := findEntityField(e, "Status")
	if status == nil || !status.OverridesBase || status.Description != "base status" {
		t.Fatalf("status not merged: %#v", status)
	}
}

func TestMultiTableEntityProjection(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {"id": {typ: "string", primaryKey: true}, "own": {typ: "string"}},
			"data.tables.y": {"extra": {typ: "string"}},
		},
		tableTypeFamilies: map[string]bool{"string": true},
	}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "contracts/data/entities/x.yaml",
		Tables: []string{"data.tables.x", "data.tables.y"}, Primary: "data.tables.x",
		Fields: []entityFieldInfo{
			func() entityFieldInfo {
				f := scalarField("ID", "string", "data.tables.x", "id")
				f.Mutable = false
				return f
			}(),
			scalarField("Own", "string", "data.tables.x", "own"),
			scalarField("Extra", "string", "data.tables.y", "extra"),
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
