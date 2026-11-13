package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
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
		Description: name,
	}
}

// baseScalarField 构造基类字段；source/validation 指向 data.tables.base。
func baseScalarField(name, typ string) entityFieldInfo {
	f := scalarField(name, typ, "data.tables.base", snakeCase(name))
	f.FromBase = true
	return f
}

func relationField(name, targetEntity, fromTable, fromCol, toTable, toCol, load string) entityFieldInfo {
	return entityFieldInfo{
		Name: name, IsRelation: true,
		Type: targetEntity, HasType: true,
		JoinFromRef: fromTable + ".col." + fromCol, JoinFromTable: fromTable, JoinFromColumn: fromCol,
		JoinToRef: toTable + ".col." + toCol, JoinToTable: toTable, JoinToColumn: toCol,
		Load: load, Description: name,
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
					"nickname": {typ: "string", sensitivity: sensitivityPII},
					"secret":   {typ: "string", sensitivity: sensitivitySecret},
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
					return f
				}(),
				scalarField("Own", "string", "data.tables.x", "own"),
				scalarField("Opt", "*string", "data.tables.x", "opt"),
				func() entityFieldInfo {
					f := scalarField("Nickname", "string", "data.tables.x", "nickname")
					f.Sensitivity = sensitivityPII
					return f
				}(),
				func() entityFieldInfo {
					f := scalarField("secret", "string", "data.tables.x", "secret")
					f.Sensitivity = sensitivitySecret
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
	e.Fields[3].Sensitivity = sensitivityPublic
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "[entity-field-sensitive]") {
		t.Fatalf("sensitivity relaxation not reported: %v", c.errs)
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
	e.Fields[4].Sensitivity = sensitivityPII
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "sensitivity") {
		t.Fatalf("sensitivity relaxation not reported: %v", c.errs)
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
				return f
			}(),
			{Name: "Total", Type: "*integer", HasType: true, SourceComputed: "COUNT(*)",
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
			baseScalarField("ID", "string"),
			baseScalarField("Status", "integer"),
			baseScalarField("CreatedAt", "timestamp"),
			baseScalarField("UpdatedAt", "timestamp"),
			baseScalarField("DeletedAt", "*timestamp"),
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
			map[string]any{
				"name": "Status", "type": "data.enums.x_status", "description": "status",
				"source":     map[string]any{"column": "data.tables.x.col.status"},
				"validation": map[string]any{"column": "data.tables.x.col.status"},
			},
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

func TestEntityBaseFieldFirst(t *testing.T) {
	c := &Checker{}
	ed := entityDoc{rel: "x.yaml", doc: map[string]any{"fields": []any{
		map[string]any{"name": "Own"},
		map[string]any{"name": "Status"},
	}}}
	c.checkBaseFieldFirst(ed)
	if !strings.Contains(entityErrs(c), "[entity-base-field-first]") {
		t.Fatalf("base field not first reported: %v", c.errs)
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
	first["type"] = "string"
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
			indexes["data.tables.token"] = []tableIndex{{name: "idx_token_account", columns: []string{"account_id"}}}
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
						return f
					}()},
				},
				"data.entities.token": {
					ID: "data.entities.token", Rel: "token.yaml", Tables: []string{"data.tables.token"}, Primary: "data.tables.token",
					Fields: []entityFieldInfo{func() entityFieldInfo {
						f := scalarField("ID", "string", "data.tables.token", "id")
						return f
					}()},
				},
			},
		}
	}

	c := newChecker(true)
	e := c.entities["data.entities.account"]
	e.Fields = append(e.Fields, relationField("Tokens", "data.entities.token", "data.tables.account", "id", "data.tables.token", "account_id", "eager"))
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if len(c.errs) != 0 {
		t.Fatalf("valid eager relation reported: %v", c.errs)
	}

	c = newChecker(false)
	e = c.entities["data.entities.account"]
	e.Fields = append(e.Fields, relationField("Tokens", "data.entities.token", "data.tables.account", "id", "data.tables.token", "account_id", "eager"))
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if !strings.Contains(entityErrs(c), "[entity-relation-eager-index]") {
		t.Fatalf("missing eager index not reported: %v", c.errs)
	}

	c = newChecker(true)
	e = c.entities["data.entities.token"]
	e.Fields = append(e.Fields, relationField("Account", "data.entities.account", "data.tables.token", "account_id", "data.tables.account", "id", "lazy"))
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
					return f
				}()}},
			"data.entities.token": {ID: "data.entities.token", Tables: []string{"data.tables.token"}, Primary: "data.tables.token",
				Fields: []entityFieldInfo{func() entityFieldInfo {
					f := scalarField("ID", "string", "data.tables.token", "id")
					return f
				}()}},
		},
	}
	e := c.entities["data.entities.account"]
	e.Fields = append(e.Fields, relationField("Tokens", "data.entities.token", "data.tables.account", "id", "data.tables.token", "account_id", "lazy"))
	c.checkEntityRelations(e, map[string]map[string]bool{})
	if !strings.Contains(entityErrs(c), "[entity-relation-via]") || !strings.Contains(entityErrs(c), "does not match") {
		t.Fatalf("join_column type mismatch not reported: %v", c.errs)
	}
}

func TestEntityRelationRejectsEmptySourceAndValidation(t *testing.T) {
	f := parseEntityField(map[string]any{
		"name":             "Tokens",
		"type":             "data.entities.token",
		"description":      "tokens",
		"source":           map[string]any{},
		"validation":       map[string]any{},
		"join_column_from": "data.tables.account.col.id",
		"join_column_to":   "data.tables.token.col.account_id",
	})
	if f == nil || !f.HasSource || !f.HasValidation {
		t.Fatalf("empty source/validation maps must still count as declared: %#v", f)
	}
	c := &Checker{}
	c.checkRelationFieldShape(*f, "x")
	if !strings.Contains(entityErrs(c), "[entity-field-variant]") {
		t.Fatalf("empty source/validation on relation not reported: %v", c.errs)
	}
}

func TestEntitySoftSource(t *testing.T) {
	c := &Checker{tableTypeFamilies: map[string]bool{"string": true, "integer": true}}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "x.yaml", Tables: []string{"data.tables.x"},
		Fields: []entityFieldInfo{
			{
				Name: "Soft", Type: "integer", HasType: true, Description: "soft",
				SourceSoft: "derived by service or request input", ValidationKind: "soft", ValidationRef: "non-negative", ValidationCount: 1,
			},
		},
	}
	c.checkEntityFields(e)
	if len(c.errs) != 0 {
		t.Fatalf("soft source reported: %v", c.errs)
	}
}

func TestEntityScalarRequiresExplicitMetadata(t *testing.T) {
	c := &Checker{tableTypeFamilies: map[string]bool{"string": true}}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "x.yaml", Tables: []string{"data.tables.x"},
		Fields: []entityFieldInfo{{Name: "Missing", Type: "string", HasType: true}},
	}
	c.checkEntityFields(e)
	errs := entityErrs(c)
	for _, want := range []string{"[entity-field-description]", "[entity-field-source]", "[entity-field-validation]"} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %s in %v", want, c.errs)
		}
	}
}

func TestEntityValidationColumnMustMatchSource(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"a": {typ: "string"}, "b": {typ: "string"}},
	}}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "x.yaml", Tables: []string{"data.tables.x"},
		Fields: []entityFieldInfo{{
			Name: "Value", Type: "string", HasType: true, Description: "value",
			SourceColumnRef: "data.tables.x.col.a", ColumnTable: "data.tables.x", Column: "a",
			ValidationKind: "column", ValidationRef: "data.tables.x.col.b", ValidationCount: 1,
		}},
	}
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "must equal source.column") {
		t.Fatalf("validation/source column mismatch not reported: %v", c.errs)
	}
}

func TestEntityValidationNameSnakeCase(t *testing.T) {
	c := &Checker{errCodeSet: map[string]bool{"X": true}}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "x.yaml",
		Doc: map[string]any{"validation": []any{map[string]any{
			"name": "Bad-Name", "expr": "true", "on_violation": "platform.errors.X",
		}}},
	}
	c.checkEntityValidation(e)
	if !strings.Contains(entityErrs(c), "name must be snake_case") {
		t.Fatalf("invalid validation name not reported: %v", c.errs)
	}
}

func TestEntityUsedByCapabilities(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "contracts/capabilities/services/x.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `id: cap.services.x
interface:
  methods:
    - name: Get
      returns:
        - { name: account, type: data.entities.account }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Checker{
		root:        root,
		entityOrder: []string{"data.entities.account"},
		entities: map[string]*entityInfo{
			"data.entities.account": {
				ID: "data.entities.account", Rel: "account.yaml",
				Doc: map[string]any{"used_by": map[string]any{"queries": []any{}, "capabilities": []any{}}},
			},
		},
	}
	c.checkEntityUsedBy()
	if !strings.Contains(entityErrs(c), "used_by.capabilities must be") {
		t.Fatalf("missing used_by capability not reported: %v", c.errs)
	}
	c.errs = nil
	c.entities["data.entities.account"].Doc["used_by"] = map[string]any{
		"queries": []any{}, "capabilities": []any{"cap.services.x"},
	}
	c.checkEntityUsedBy()
	if len(c.errs) != 0 {
		t.Fatalf("valid used_by capabilities reported: %v", c.errs)
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
			"data.queries.account.get": {ID: "data.queries.account.get", Returns: []queryReturn{{Kind: "entity", Entity: "data.entities.account"}}},
		},
	}
	c.checkEntityUsedBy()
	if len(c.errs) != 0 {
		t.Fatalf("valid used_by reported: %v", c.errs)
	}

	c.entities["data.entities.account"].Doc = map[string]any{"used_by": map[string]any{"queries": []any{}}}
	c.checkEntityUsedBy()
	if !strings.Contains(entityErrs(c), "[entity-used-by]") {
		t.Fatalf("missing used_by query not reported: %v", c.errs)
	}
}

func TestEagerCycles(t *testing.T) {
	c := &Checker{
		entityOrder: []string{"a", "b"},
		entities: map[string]*entityInfo{
			"a": {ID: "a", Fields: []entityFieldInfo{relationField("B", "b", "data.tables.a", "b_id", "data.tables.b", "a_id", "eager")}},
			"b": {ID: "b", Fields: []entityFieldInfo{relationField("A", "a", "data.tables.b", "a_id", "data.tables.a", "b_id", "eager")}},
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
				return f
			}(),
			scalarField("Nickname", "string", "data.tables.account", "nickname"),
		}
		if withSecret {
			f := scalarField("secret", "string", "data.tables.account", "secret")
			f.Sensitivity = sensitivitySecret
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
			cols["secret"] = columnMeta{typ: "string", sensitivity: sensitivitySecret}
		}
		return &Checker{
			tableColumns: map[string]map[string]columnMeta{"data.tables.account": cols},
			tableOwners:  map[string]string{"data.tables.account": "account"},
			entities:     map[string]*entityInfo{"data.entities.account": accountEntity(withSecret)},
		}
	}

	c := newChecker(false)
	q := &queryInfo{
		ID: "data.queries.account.get", File: "account.yaml", OwnerModule: "account",
		Targets: []string{"data.tables.account"},
		Returns: []queryReturn{{Name: "account", Kind: "entity", Entity: "data.entities.account"}},
	}
	c.checkQueryReturns(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid entity query reported: %v", c.errs)
	}

	q.Returns[0].Columns = []string{"id"}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "omits required field Nickname") {
		t.Fatalf("missing required coverage not reported: %v", c.errs)
	}

	c = newChecker(true)
	q = &queryInfo{
		ID: "data.queries.account.get", File: "account.yaml", OwnerModule: "other",
		Targets: []string{"data.tables.account"},
		Returns: []queryReturn{{Name: "account", Kind: "entity", Entity: "data.entities.account", Columns: []string{"id", "nickname", "secret"}}},
	}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "[query-sensitive-columns]") {
		t.Fatalf("cross-module secret not reported: %v", c.errs)
	}
}

func TestEntityCoverageComputedSensitive(t *testing.T) {
	e := &entityInfo{ID: "data.entities.x", OwnerModule: "account", Tables: []string{"data.tables.x"}, Primary: "data.tables.x",
		Fields: []entityFieldInfo{
			func() entityFieldInfo {
				f := scalarField("ID", "string", "data.tables.x", "id")
				return f
			}(),
			{Name: "secretTotal", Type: "*integer", HasType: true, SourceComputed: "COUNT(*)",
				Sensitivity: sensitivitySecret, ValidationKind: "soft", ValidationRef: "非负", ValidationCount: 1, Description: "敏感聚合"},
		}}
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"id": {typ: "string", primaryKey: true}},
	}}
	q := &queryInfo{ID: "q", File: "q.yaml", OwnerModule: "other", Targets: []string{"data.tables.x"},
		Returns: []queryReturn{{Name: "x", Kind: "entity", Entity: "data.entities.x"}}}
	c.checkEntityCoverage(q, e, nil)
	if !strings.Contains(entityErrs(c), "[query-sensitive-columns]") || !strings.Contains(entityErrs(c), "computed secret") {
		t.Fatalf("computed secret field not reported: %v", c.errs)
	}

	c.errs = nil
	q.OwnerModule = "account"
	c.checkEntityCoverage(q, e, nil)
	if len(c.errs) != 0 {
		t.Fatalf("same-module computed secret reported: %v", c.errs)
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

func TestQueryReturnsShape(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{
		ID: "data.queries.account.count", File: "account.yaml",
		Returns: []queryReturn{
			{Name: "status", Kind: "scalar", Type: "integer"},
			{Name: "total", Kind: "scalar", Type: "integer"},
		},
	}
	c.checkQueryReturns(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid multi scalar return reported: %v", c.errs)
	}

	c = &Checker{}
	q.Returns = []queryReturn{{Name: "total", Kind: "scalar", Type: "integer"}, {Name: "total", Kind: "scalar", Type: "integer"}}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "duplicate return name") {
		t.Fatalf("duplicate return name not reported: %v", c.errs)
	}

	c = &Checker{}
	q.Returns = []queryReturn{{Name: "total", Kind: "unknown"}}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "invalid kind") {
		t.Fatalf("invalid return kind not reported: %v", c.errs)
	}

	c = &Checker{}
	q.Returns = []queryReturn{{Name: "a", Kind: "scalar", Type: "integer", List: true}, {Name: "b", Kind: "scalar", Type: "integer"}}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "list must be consistent") {
		t.Fatalf("inconsistent list not reported: %v", c.errs)
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

func TestQueryParamExprAndPagination(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{
		ID: "q", File: "q.yaml",
		Params: []queryParam{{Name: "older_than_days", Type: "integer", Expr: "created_at < now()"}},
	}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid expr param reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[0].Column = "data.tables.account.col.id"
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "mutually exclusive") {
		t.Fatalf("column+expr not reported: %v", c.errs)
	}

	c = &Checker{}
	q = &queryInfo{
		ID: "q", File: "q.yaml",
		Returns: []queryReturn{{Name: "rows", Kind: "table", Table: "data.tables.x", List: true}},
		Params: []queryParam{
			{Name: "offset", Type: "integer", Nullable: true, HasDefault: true, DefaultValue: 0},
			{Name: "limit", Type: "integer", Nullable: true, HasDefault: true, DefaultValue: 20},
		},
	}
	c.checkQueryPagination(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid pagination reported: %v", c.errs)
	}

	c = &Checker{}
	q.Params[0].DefaultValue = 1
	q.Params[1].DefaultValue = 0
	c.checkQueryPagination(q)
	errs := entityErrs(c)
	if !strings.Contains(errs, "offset default must be 0") || !strings.Contains(errs, "limit default must be positive") {
		t.Fatalf("invalid pagination not reported: %v", c.errs)
	}

	c = &Checker{}
	q = &queryInfo{
		ID: "q", File: "q.yaml",
		Returns: []queryReturn{{Name: "rows", Kind: "table", Table: "data.tables.x", List: true}},
	}
	c.checkQueryPagination(q)
	if !strings.Contains(entityErrs(c), "list return requires offset param") {
		t.Fatalf("missing pagination params not reported: %v", c.errs)
	}
}

func TestQueryDefaultRequiresNullable(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{ID: "q", File: "q.yaml", Params: []queryParam{
		{Name: "page_size", Type: "integer", HasDefault: true},
	}}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "default requires nullable:true") {
		t.Fatalf("default without nullable not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[0].Nullable = true
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid nullable default reported: %v", c.errs)
	}
}

func TestQueryParamTypeAndDefaultValidation(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{ID: "q", File: "q.yaml", Params: []queryParam{
		{Name: "mode", Type: "unknown_type"},
	}}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "cannot map to a type family") {
		t.Fatalf("unknown param type not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[0] = queryParam{Name: "status", Type: "data.enums.account_status"}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("enum param reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[0] = queryParam{Name: "page_size", Type: "integer", Nullable: true, HasDefault: true, DefaultValue: "20"}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "default must be an integer") {
		t.Fatalf("wrong default type not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[0] = queryParam{Name: "ids", Type: "List<integer>", Nullable: true, HasDefault: true, DefaultValue: 1}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "default must be a list") {
		t.Fatalf("non-list default for array not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[0] = queryParam{Name: "since", Type: "timestamp", Nullable: true, HasDefault: true, DefaultValue: "2026-01-01T00:00:00Z"}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid timestamp default reported: %v", c.errs)
	}
}

func TestQueryEnumParamBindsIntegerColumn(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"status": {typ: "integer"}},
	}}
	q := &queryInfo{ID: "q", File: "q.yaml", Targets: []string{"data.tables.x"},
		Params: []queryParam{{Name: "status", Type: "data.enums.x_status", Column: "data.tables.x.col.status"}}}
	c.checkQueryParamColumns(q)
	if len(c.errs) != 0 {
		t.Fatalf("enum param bound to integer column reported: %v", c.errs)
	}
}

func TestRenderedQueryParamBindsIDColumn(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, _, err := contractspec.LoadSchemaByType(root, "data.queries")
	if err != nil {
		t.Fatal(err)
	}
	b, err := contractspec.RenderQuery(schema, "note", "notes", "note", "note", "备注查询")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "q.yaml")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := loadYAML(path)
	if err != nil {
		t.Fatal(err)
	}
	queries := asList(doc["queries"])
	if len(queries) != 1 {
		t.Fatalf("rendered query count = %d", len(queries))
	}
	qm, _ := asMap(queries[0])
	q := parseQuery(qm, "q.yaml", "data.queries.note", "notes")
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.note": {"id": {typ: "integer", primaryKey: true}},
	}}
	c.checkQueryParamColumns(q)
	if len(c.errs) != 0 {
		t.Fatalf("rendered query param must bind the integer id column: %v", c.errs)
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
			"id":         {typ: "string", primaryKey: true},
			"nickname":   {typ: "string", sensitivity: sensitivityPII},
			"deleted_at": {typ: "timestamp", nullable: true},
		},
	}}
	e := &entityInfo{Tables: []string{"data.tables.x"}, Primary: "data.tables.x", Fields: []entityFieldInfo{
		{Name: "ID", SourceColumnRef: "data.tables.x.col.id", ColumnTable: "data.tables.x", Column: "id"},
		{Name: "Nickname", SourceColumnRef: "data.tables.x.col.nickname", ColumnTable: "data.tables.x", Column: "nickname"},
		{Name: "DeletedAt", SourceColumnRef: "data.tables.x.col.deleted_at", ColumnTable: "data.tables.x", Column: "deleted_at"},
	}}
	c.applyEntityFieldDefaults(e)
	if e.Fields[0].JSONName != "id" || e.Fields[0].Type != "string" || e.Fields[0].ValidationKind != "" || e.Fields[0].Description != "" {
		t.Fatalf("only mechanical defaults should apply: %#v", e.Fields[0])
	}
	if e.Fields[1].Sensitivity != sensitivityPII || e.Fields[1].Type != "string" || e.Fields[1].JSONName != "nickname" || e.Fields[1].Description != "" {
		t.Fatalf("sensitivity/type defaults not applied: %#v", e.Fields[1])
	}
	if e.Fields[2].Type != "*timestamp" || e.Fields[2].JSONName != "deleted_at" || e.Fields[2].Description != "" {
		t.Fatalf("nullable defaults not applied: %#v", e.Fields[2])
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
	if !strings.Contains(entityErrs(c), "tables references data.tables.base") {
		t.Fatalf("base target not reported: %v", c.errs)
	}
}

func TestQueryDerivedIDAndReturns(t *testing.T) {
	q := parseQuery(map[string]any{
		"name": "GetAccount",
		"returns": []any{
			map[string]any{"name": "account", "kind": "entity", "entity": "data.entities.account"},
		},
	}, "q.yaml", "data.queries.account", "account")
	if q.ID != "data.queries.account.get_account" {
		t.Fatalf("derived query id = %q", q.ID)
	}
	if len(q.Returns) != 1 || q.Returns[0].Kind != "entity" || q.Returns[0].Entity != "data.entities.account" {
		t.Fatalf("returns not parsed: %#v", q.Returns)
	}
}

func TestQueryClauseShape(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{ID: "q", File: "q.yaml", Targets: []string{"data.tables.x"},
		Returns: []queryReturn{{Name: "exists", Kind: "scalar", Type: "boolean"}}}
	c.checkQueryShape(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid scalar reported: %v", c.errs)
	}

	q.Returns = []queryReturn{{Name: "affected_rows", Kind: "affected_rows"}}
	c.checkQueryShape(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid affected_rows reported: %v", c.errs)
	}

	q.Returns[0].List = true
	c.errs = nil
	c.checkQueryShape(q)
	if !strings.Contains(entityErrs(c), "affected_rows must not be a list") {
		t.Fatalf("list affected_rows not reported: %v", c.errs)
	}

	q = &queryInfo{ID: "q", File: "q.yaml", Targets: []string{"data.tables.x"},
		Returns: []queryReturn{{Name: "affected_rows", Kind: "affected_rows"}, {Name: "x", Kind: "scalar", Type: "integer"}}}
	c.errs = nil
	c.checkQueryShape(q)
	if !strings.Contains(entityErrs(c), "must be the only return item") {
		t.Fatalf("affected_rows mixed with other returns not reported: %v", c.errs)
	}
}

func TestSecretFieldMustBeUnexported(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {
			"id":     {typ: "string", primaryKey: true},
			"secret": {typ: "string", sensitivity: sensitivitySecret},
		},
	}}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "contracts/data/entities/x.yaml",
		Tables: []string{"data.tables.x"}, Primary: "data.tables.x",
		Fields: []entityFieldInfo{
			func() entityFieldInfo {
				f := scalarField("ID", "string", "data.tables.x", "id")
				return f
			}(),
			func() entityFieldInfo {
				f := scalarField("Secret", "string", "data.tables.x", "secret")
				f.Sensitivity = sensitivitySecret
				return f
			}(),
		},
	}
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "secret field must start lowercase") {
		t.Fatalf("exported secret field not reported: %v", c.errs)
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
			{Name: "type", Type: "string", JSONName: "type"},
			{Name: "Status", Type: "data.enums.x_status", JSONName: "status"},
		},
	}
	c.checkEntityFieldMetadata(e)
	errs := entityErrs(c)
	for _, want := range []string{
		`json_name "emailAddress" already used by field Email`,
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

func TestStatusOverrideDoesNotInheritDescription(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {
				"id":     {typ: "string", primaryKey: true},
				"status": {typ: "integer"},
			},
		},
		entityBaseFields: []entityFieldInfo{
			baseScalarField("ID", "string"),
			baseScalarField("Status", "integer"),
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
	if status == nil || !status.OverridesBase || status.Description != "" || status.SourceColumnRef != "" {
		t.Fatalf("status override must not inherit source/description: %#v", status)
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
