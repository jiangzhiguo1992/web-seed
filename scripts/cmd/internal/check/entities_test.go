package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func entityErrs(c *Checker) string {
	return strings.Join(c.errs, "\n")
}

// scalarField 构造合法的标量字段：column 来源 + 描述。
func scalarField(name, typ, table, col string) entityFieldInfo {
	ref := table + ".col." + col
	return entityFieldInfo{
		Name: name, Type: typ, HasType: true,
		SourceColumnRef: ref, ColumnTable: table, Column: col,
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
		Name: name, IsRelation: true, HasJoin: true,
		Type: targetEntity, HasType: true,
		JoinFromRef: fromTable + ".col." + fromCol, JoinFromTable: fromTable, JoinFromColumn: fromCol,
		JoinToRef: toTable + ".col." + toCol, JoinToTable: toTable, JoinToColumn: toCol,
		Lazy: load == "lazy", HasLazy: true, Description: name,
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
	e.Fields[2].Type = "string"
	e.Fields[2].Nullable = true
	e.Fields[2].HasNullable = true
	c.checkEntityFields(e)
	if len(c.errs) != 0 {
		t.Fatalf("valid nullable field reported: %v", c.errs)
	}

	c = newChecker()
	e = newEntity()
	e.Fields[2].Type = "string"
	e.Fields[2].Nullable = false
	e.Fields[2].HasNullable = true
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "pointer type") {
		t.Fatalf("nullable mismatch not reported: %v", c.errs)
	}

	c = newChecker()
	e = newEntity()
	e.Fields[2].Type = "*string"
	e.Fields[2].Nullable = true
	e.Fields[2].HasNullable = true
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "not both") {
		t.Fatalf("nullable + * prefix not reported: %v", c.errs)
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
	e.Fields[1].HasDescription = true
	c.checkEntityFields(e)
	if !strings.Contains(entityErrs(c), "[entity-field-description]") {
		t.Fatalf("missing description not reported: %v", c.errs)
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
			{Name: "Total", Type: "*integer", HasType: true, SourceComputed: "COUNT(*)", Description: "总数"},
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

func TestEntityComputedNullableFlag(t *testing.T) {
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
			{Name: "Total", Type: "integer", HasType: true, HasNullable: true, Nullable: true,
				SourceComputed: "COUNT(*)", Description: "总数"},
		},
	}
	c.checkEntityFields(e)
	if len(c.errs) != 0 {
		t.Fatalf("computed nullable field reported: %v", c.errs)
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
		t.Fatalf("join column type mismatch not reported: %v", c.errs)
	}
}

func TestEntityRelationRejectsEmptySource(t *testing.T) {
	f := parseEntityField(map[string]any{
		"name":        "Tokens",
		"type":        "data.entities.token",
		"description": "tokens",
		"source":      map[string]any{},
	})
	if f == nil || !f.HasSource || !f.IsRelation {
		t.Fatalf("type-driven relation must be recognized: %#v", f)
	}
	c := &Checker{}
	c.checkRelationFieldShape(*f, "x")
	if !strings.Contains(entityErrs(c), "[entity-field-variant]") {
		t.Fatalf("empty source on relation not reported: %v", c.errs)
	}
}

func TestEntityRelationJoinRequiresLazy(t *testing.T) {
	f := parseEntityField(map[string]any{
		"name":        "Tokens",
		"type":        "data.entities.token",
		"description": "tokens",
		"source": map[string]any{"join": map[string]any{
			"from": "data.tables.account.col.id",
			"to":   "data.tables.token.col.account_id",
		}},
	})
	c := &Checker{}
	c.checkRelationFieldShape(*f, "x")
	errs := entityErrs(c)
	if !strings.Contains(errs, "[entity-field-variant]") || !strings.Contains(errs, "source.join.lazy") {
		t.Fatalf("missing join.lazy not reported: %v", c.errs)
	}
}

func TestEntityRelationJoinRequiresFromTo(t *testing.T) {
	cases := map[string]map[string]any{
		"missing from": {"to": "data.tables.token.col.account_id", "lazy": true},
		"missing to":   {"from": "data.tables.account.col.id", "lazy": true},
		"empty join":   {},
	}
	for name, join := range cases {
		f := parseEntityField(map[string]any{
			"name":        "Tokens",
			"type":        "data.entities.token",
			"description": "tokens",
			"source":      map[string]any{"join": join},
		})
		c := &Checker{}
		c.checkRelationFieldShape(*f, "x")
		if !strings.Contains(entityErrs(c), "[entity-field-variant]") {
			t.Fatalf("%s: incomplete join not reported: %v", name, c.errs)
		}
	}
}

func TestEntityRelationRejectsMixedSource(t *testing.T) {
	f := parseEntityField(map[string]any{
		"name":        "Tokens",
		"type":        "data.entities.token",
		"description": "tokens",
		"source": map[string]any{
			"column": "data.tables.account.col.id",
			"join": map[string]any{
				"from": "data.tables.account.col.id",
				"to":   "data.tables.token.col.account_id",
				"lazy": true,
			},
		},
	})
	c := &Checker{}
	c.checkRelationFieldShape(*f, "x")
	if !strings.Contains(entityErrs(c), "source must be join only") {
		t.Fatalf("mixed relation source not reported: %v", c.errs)
	}
}

func TestEntityScalarRejectsJoin(t *testing.T) {
	c := &Checker{tableTypeFamilies: map[string]bool{"string": true}}
	e := &entityInfo{ID: "data.entities.x", Rel: "x.yaml", Tables: []string{"data.tables.x"}}
	f := entityFieldInfo{
		Name: "Value", Type: "string", HasType: true, Description: "value",
		HasJoin: true, SourceSoft: "external input",
	}
	c.checkScalarField(e, f, "x.yaml: fields.Value", map[string]string{})
	if !strings.Contains(entityErrs(c), "scalar field must not declare source.join") {
		t.Fatalf("scalar join not reported: %v", c.errs)
	}
}

func TestEntityRelationRejectsNullable(t *testing.T) {
	f := parseEntityField(map[string]any{
		"name":        "Account",
		"type":        "data.entities.account",
		"nullable":    true,
		"description": "account",
		"source": map[string]any{"join": map[string]any{
			"from": "data.tables.token.col.account_id",
			"to":   "data.tables.account.col.id",
			"lazy": true,
		}},
	})
	c := &Checker{}
	c.checkRelationFieldShape(*f, "x")
	if !strings.Contains(entityErrs(c), "must not declare nullable") {
		t.Fatalf("nullable relation not reported: %v", c.errs)
	}
}

func TestEntitySoftSource(t *testing.T) {
	c := &Checker{tableTypeFamilies: map[string]bool{"string": true, "integer": true}}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "x.yaml", Tables: []string{"data.tables.x"},
		Fields: []entityFieldInfo{
			{
				Name: "Soft", Type: "integer", HasType: true, Description: "soft",
				SourceSoft: "derived by service or request input",
			},
		},
	}
	c.checkEntityFields(e)
	if len(c.errs) != 0 {
		t.Fatalf("soft source reported: %v", c.errs)
	}
}

func TestEntityComputedBlankRejected(t *testing.T) {
	c := &Checker{tableTypeFamilies: map[string]bool{"string": true}}
	e := &entityInfo{ID: "data.entities.x", Rel: "x.yaml", Tables: []string{"data.tables.x"}}
	f := entityFieldInfo{
		Name: "Derived", Type: "*string", HasType: true,
		SourceComputed: "  ", HasSource: true,
		Description: "derived",
	}
	c.checkScalarField(e, f, "x.yaml: fields.Derived", map[string]string{})
	if !strings.Contains(entityErrs(c), "source computed must not be blank") {
		t.Fatalf("blank computed source not reported: %v", c.errs)
	}
}

func TestEntityScalarRequiresExplicitMetadata(t *testing.T) {
	c := &Checker{tableTypeFamilies: map[string]bool{"string": true}}
	e := &entityInfo{
		ID: "data.entities.x", Rel: "x.yaml", Tables: []string{"data.tables.x"},
		Fields: []entityFieldInfo{{Name: "Missing", Type: "string", HasType: true, HasDescription: true}},
	}
	c.checkEntityFields(e)
	errs := entityErrs(c)
	for _, want := range []string{"[entity-field-description]", "[entity-field-source]"} {
		if !strings.Contains(errs, want) {
			t.Fatalf("missing %s in %v", want, c.errs)
		}
	}
}

func TestEntityModelTableDuplicateRejectedBySchema(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.entities.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	model, ok := contractspec.FindField(schema.Fields, "model")
	if !ok {
		t.Fatal("model field missing in meta")
	}
	tables, ok := contractspec.FindField(model.Fields, "tables")
	if !ok {
		t.Fatal("model.tables field missing in meta")
	}
	c := &Checker{}
	c.validateValue([]any{"data.tables.x", "data.tables.x"}, tables, "model.tables")
	if !strings.Contains(entityErrs(c), "[schema.unique]") {
		t.Fatalf("duplicate model.tables must be rejected by meta unique: %v", c.errs)
	}
}

func TestEntityValidationUsedBy(t *testing.T) {
	c := &Checker{
		methodReturns:  map[string]map[string]returnType{"cap.services.x.get": {}},
		serviceQueries: map[string]map[string]bool{"cap.services.x.get": {"data.queries.x.get": true}},
		queryTables:    map[string]map[string]bool{"data.queries.x.get": {"data.tables.x": true}},
	}
	e := &entityInfo{ID: "data.entities.x", Rel: "x.yaml", Primary: "data.tables.x", Doc: map[string]any{
		"validation": []any{map[string]any{
			"name": "v", "expr": "x > 0", "on_violation": "platform.errors.COMMON_INTERNAL",
			"used_by": []any{"cap.services.x.get"},
		}},
	}}
	c.checkEntityValidation(e)
	if len(c.errs) != 0 {
		t.Fatalf("valid validation used_by reported: %v", c.errs)
	}

	c.errs = nil
	e.Doc["validation"] = []any{map[string]any{
		"name": "v", "expr": "x > 0", "on_violation": "platform.errors.COMMON_INTERNAL",
		"used_by": []any{"cap.services.x.missing"},
	}}
	c.checkEntityValidation(e)
	if !strings.Contains(entityErrs(c), "unknown service") {
		t.Fatalf("unknown validation consumer not reported: %v", c.errs)
	}

	c.errs = nil
	c.serviceQueries["cap.services.x.get"] = map[string]bool{}
	e.Doc["validation"] = []any{map[string]any{
		"name": "v", "expr": "x > 0", "on_violation": "platform.errors.COMMON_INTERNAL",
		"used_by": []any{"cap.services.x.get"},
	}}
	c.checkEntityValidation(e)
	if !strings.Contains(entityErrs(c), "does not query entity table") {
		t.Fatalf("validation consumer without entity query not reported: %v", c.errs)
	}
}

func TestColumnsReturnShape(t *testing.T) {
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"id": {typ: "integer", primaryKey: true}},
	}, tableOwners: map[string]string{"data.tables.x": "x"}}
	q := &queryInfo{ID: "q", File: "q.yaml", OwnerModule: "x", Targets: []string{"data.tables.x"},
		Returns: []queryReturn{{Name: "row", Kind: "columns"}, {Name: "err", Kind: "error"}}}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "kind=columns requires columns") {
		t.Fatalf("columns without projection not reported: %v", c.errs)
	}

	c.errs = nil
	q.Returns = []queryReturn{
		{Name: "row", Kind: "columns", Columns: []string{"data.tables.x.col.id"}},
		{Name: "total", Kind: "scalar", Type: "integer"},
	}
	c.checkQueryShape(q)
	if !strings.Contains(entityErrs(c), "columns return must be the only non-error return item") {
		t.Fatalf("mixed columns return not reported: %v", c.errs)
	}

	c.errs = nil
	q.Returns = []queryReturn{{Name: "row", Kind: "columns", Columns: []string{"data.tables.x.col.id"}}, {Name: "err", Kind: "error"}}
	c.checkQueryReturns(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid columns return reported: %v", c.errs)
	}
}

func TestQueryReturnsRejectsRemovedKinds(t *testing.T) {
	for _, kind := range []string{"table", "column"} {
		c := &Checker{}
		q := &queryInfo{ID: "q", File: "q.yaml", Returns: []queryReturn{{Name: "row", Kind: kind}, {Name: "err", Kind: "error"}}}
		c.checkQueryReturns(q)
		if !strings.Contains(entityErrs(c), `invalid kind "`+kind+`"`) {
			t.Fatalf("kind %s not rejected: %v", kind, c.errs)
		}
	}
}

func TestEntityValidationNameRejectedBySchema(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := loadSchema(filepath.Join(root, "contracts/_meta/data.entities.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	validation, ok := contractspec.FindField(schema.Fields, "validation")
	if !ok {
		t.Fatal("validation field missing in meta")
	}
	name, ok := contractspec.FindField(validation.ItemFields, "name")
	if !ok {
		t.Fatal("validation.name field missing in meta")
	}
	c := &Checker{}
	c.validateValue("Bad-Name", name, "validation.name")
	if !strings.Contains(entityErrs(c), "[schema.pattern]") {
		t.Fatalf("invalid validation name must be rejected by meta pattern: %v", c.errs)
	}
}

func TestEntityUsedByQueries(t *testing.T) {
	c := &Checker{
		entityOrder: []string{"data.entities.account"},
		entities: map[string]*entityInfo{
			"data.entities.account": {
				ID:  "data.entities.account",
				Rel: "account.yaml",
				Doc: map[string]any{"model": map[string]any{"queries": []any{"data.queries.account.get"}}},
			},
		},
		queries: map[string]*queryInfo{
			"data.queries.account.get": {ID: "data.queries.account.get", Returns: []queryReturn{{Kind: "entity", Entity: "data.entities.account"}, {Name: "err", Kind: "error"}}},
		},
	}
	c.checkEntityUsedBy()
	if len(c.errs) != 0 {
		t.Fatalf("valid model.queries reported: %v", c.errs)
	}

	c.entities["data.entities.account"].Doc = map[string]any{"model": map[string]any{"queries": []any{}}}
	c.checkEntityUsedBy()
	if !strings.Contains(entityErrs(c), "[entity-used-by]") {
		t.Fatalf("missing model.queries not reported: %v", c.errs)
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

func TestQueryEntityAndColumnsSensitivity(t *testing.T) {
	secretField := func() entityFieldInfo {
		f := scalarField("secret", "string", "data.tables.account", "secret")
		f.Sensitivity = sensitivitySecret
		return f
	}()
	e := &entityInfo{
		ID: "data.entities.account", OwnerModule: "account",
		Tables: []string{"data.tables.account"}, Primary: "data.tables.account",
		Fields: []entityFieldInfo{
			scalarField("ID", "string", "data.tables.account", "id"),
			scalarField("Nickname", "string", "data.tables.account", "nickname"),
			secretField,
		},
	}
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{"data.tables.account": {
			"id": {typ: "string", primaryKey: true}, "nickname": {typ: "string"},
			"secret": {typ: "string", sensitivity: sensitivitySecret},
		}},
		tableOwners: map[string]string{"data.tables.account": "account"},
		entities:    map[string]*entityInfo{"data.entities.account": e},
	}
	q := &queryInfo{
		ID: "data.queries.account.get", File: "account.yaml", OwnerModule: "other",
		Targets: []string{"data.tables.account"},
		Returns: []queryReturn{{Name: "account", Kind: "entity", Entity: "data.entities.account"}, {Name: "err", Kind: "error"}},
	}
	c.checkQueryReturns(q)
	if len(c.errs) != 0 {
		t.Fatalf("entity default projection reported: %v", c.errs)
	}

	q.Returns[0].Columns = []string{"data.tables.account.col.id"}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "kind=entity forbids type/columns") {
		t.Fatalf("entity columns not rejected: %v", c.errs)
	}

	c.errs = nil
	q = &queryInfo{
		ID: "data.queries.account.cols", File: "account.yaml", OwnerModule: "other",
		Targets: []string{"data.tables.account"},
		Returns: []queryReturn{{Name: "row", Kind: "columns", Columns: []string{"data.tables.account.col.secret"}}, {Name: "err", Kind: "error"}},
	}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "[query-sensitive-columns]") {
		t.Fatalf("cross-module columns secret not reported: %v", c.errs)
	}

	c.errs = nil
	q.OwnerModule = "account"
	c.checkQueryReturns(q)
	if len(c.errs) != 0 {
		t.Fatalf("same-module columns secret reported: %v", c.errs)
	}
}

func TestColumnProjectionMultiTableQualified(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.a": {"account_id": {typ: "integer"}},
			"data.tables.b": {"account_id": {typ: "integer"}},
		},
		tableOwners: map[string]string{"data.tables.a": "x", "data.tables.b": "x"},
	}
	q := &queryInfo{ID: "q", File: "q.yaml", OwnerModule: "x",
		Targets: []string{"data.tables.a", "data.tables.b"}}

	c.checkColumnProjection("q", q, []string{
		"data.tables.a.col.account_id", "data.tables.b.col.account_id",
	})
	if len(c.errs) != 0 {
		t.Fatalf("qualified multi-table columns reported: %v", c.errs)
	}

	c.errs = nil
	c.checkColumnProjection("q", q, []string{"account_id"})
	if len(c.errs) != 1 || !strings.Contains(entityErrs(c), "must be data.tables.<t>.col.<c>") {
		t.Fatalf("bare multi-table column not rejected cleanly: %v", c.errs)
	}

	c.errs = nil
	c.checkColumnProjection("q", q, []string{"data.tables.c.col.account_id"})
	if len(c.errs) != 1 || !strings.Contains(entityErrs(c), "does not belong to query tables") {
		t.Fatalf("foreign column not rejected cleanly: %v", c.errs)
	}
}

func TestQuerySecretParamScope(t *testing.T) {
	c := &Checker{
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.x": {"secret": {typ: "string", sensitivity: sensitivitySecret}},
		},
		tableOwners: map[string]string{"data.tables.x": "x"},
	}
	q := &queryInfo{ID: "q", File: "q.yaml", OwnerModule: "other", Targets: []string{"data.tables.x"},
		Params: []queryParam{{Name: "secret", Type: "text", Column: "data.tables.x.col.secret"}}}
	c.checkQueryParamColumns(q)
	if !strings.Contains(entityErrs(c), "param binds secret column") {
		t.Fatalf("cross-module secret param not reported: %v", c.errs)
	}

	c.errs = nil
	q.OwnerModule = "x"
	c.checkQueryParamColumns(q)
	if len(c.errs) != 0 {
		t.Fatalf("same-module secret param reported: %v", c.errs)
	}
}

func TestEntityComputedSecretSensitivity(t *testing.T) {
	e := &entityInfo{ID: "data.entities.x", OwnerModule: "account", Tables: []string{"data.tables.x"}, Primary: "data.tables.x",
		Fields: []entityFieldInfo{
			func() entityFieldInfo {
				f := scalarField("ID", "string", "data.tables.x", "id")
				return f
			}(),
			{Name: "secretTotal", Type: "*integer", HasType: true, SourceComputed: "COUNT(*)",
				Sensitivity: sensitivitySecret, Description: "敏感聚合"},
		}}
	c := &Checker{tableColumns: map[string]map[string]columnMeta{
		"data.tables.x": {"id": {typ: "string", primaryKey: true}},
	}}
	q := &queryInfo{ID: "q", File: "q.yaml", OwnerModule: "other", Targets: []string{"data.tables.x"},
		Returns: []queryReturn{{Name: "x", Kind: "entity", Entity: "data.entities.x"}, {Name: "err", Kind: "error"}}}
	c.checkEntitySecretProjection("q", q, e)
	if !strings.Contains(entityErrs(c), "[query-sensitive-columns]") || !strings.Contains(entityErrs(c), "computed secret") {
		t.Fatalf("computed secret field not reported: %v", c.errs)
	}

	c.errs = nil
	q.OwnerModule = "account"
	c.checkEntitySecretProjection("q", q, e)
	if len(c.errs) != 0 {
		t.Fatalf("same-module computed secret reported: %v", c.errs)
	}
}

func TestSecretEntityProjectionBoundary(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	c := &Checker{root: root}
	c.run()
	c.errs = nil
	for _, id := range []string{
		"data.entities.token",
		"data.entities.auth_pwd",
		"data.entities.verification_body",
		"data.entities.verification_action",
	} {
		e := c.entities[id]
		if e == nil {
			t.Fatalf("entity %s not loaded", id)
		}
		var secretRefs []string
		for _, f := range e.Fields {
			if !f.IsRelation && f.Column != "" && normalizeSensitivity(f.Sensitivity) == sensitivitySecret {
				secretRefs = append(secretRefs, f.ColumnTable+".col."+f.Column)
			}
		}
		if len(secretRefs) == 0 {
			t.Fatalf("%s: expected at least one secret field", id)
		}
		q := &queryInfo{ID: "q", File: "q.yaml", OwnerModule: "other", Targets: append([]string(nil), e.Tables...)}
		c.errs = nil
		c.checkEntitySecretProjection("q.returns[0]", q, e)
		if strings.Contains(entityErrs(c), "[query-sensitive-columns]") {
			t.Fatalf("%s: entity default projection must exclude secret fields: %v", id, c.errs)
		}

		c.errs = nil
		c.checkColumnProjection("q.returns[0]", q, secretRefs)
		if !strings.Contains(entityErrs(c), "[query-sensitive-columns]") {
			t.Fatalf("%s: columns secret projection across modules must fail: %v", id, c.errs)
		}

		sameModule := *q
		sameModule.OwnerModule = e.OwnerModule
		c.errs = nil
		c.checkColumnProjection("q.returns[0]", &sameModule, secretRefs)
		if strings.Contains(entityErrs(c), "[query-sensitive-columns]") {
			t.Fatalf("%s: same-module columns secret projection must be allowed: %v", id, c.errs)
		}
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
			{Name: "err", Kind: "error"},
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
	q.Returns = []queryReturn{{Name: "err", Kind: "scalar", Type: "string"}}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "err return must use kind=error") {
		t.Fatalf("explicit err return not reported: %v", c.errs)
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
		Params: []queryParam{{Name: "ctx", Type: "context.Context"}, {Name: "older_than_days", Type: "integer", Expr: "created_at < now()", HasExpr: true}},
	}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid expr param reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[1].Column = "data.tables.account.col.id"
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "mutually exclusive") {
		t.Fatalf("column+expr not reported: %v", c.errs)
	}

	c = &Checker{}
	q = &queryInfo{
		ID: "q", File: "q.yaml",
		Params: []queryParam{{Name: "ctx", Type: "context.Context"}, {Name: "older_than_days", Type: "integer", Expr: "  ", HasExpr: true}},
	}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "expr must not be blank") {
		t.Fatalf("blank expr not reported: %v", c.errs)
	}

	c = &Checker{}
	q = &queryInfo{
		ID: "q", File: "q.yaml",
		Returns: []queryReturn{{Name: "rows", Kind: "entity", Entity: "data.entities.x", List: true}, {Name: "err", Kind: "error"}},
		Params: []queryParam{
			{Name: "ctx", Type: "context.Context"},
			{Name: "offset", Type: "integer", Nullable: true, HasDefault: true, DefaultValue: 0},
			{Name: "limit", Type: "integer", Nullable: true, HasDefault: true, DefaultValue: 20},
		},
	}
	c.checkQueryPagination(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid pagination reported: %v", c.errs)
	}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid pagination reported by params: %v", c.errs)
	}

	c = &Checker{}
	q.Params[1].DefaultValue = 1
	q.Params[2].DefaultValue = 0
	c.checkQueryPagination(q)
	errs := entityErrs(c)
	if !strings.Contains(errs, "offset default must be 0") || !strings.Contains(errs, "limit default must be positive") {
		t.Fatalf("invalid pagination not reported: %v", c.errs)
	}

	c = &Checker{}
	q = &queryInfo{
		ID: "q", File: "q.yaml",
		Returns: []queryReturn{{Name: "rows", Kind: "entity", Entity: "data.entities.x", List: true}, {Name: "err", Kind: "error"}},
	}
	c.checkQueryPagination(q)
	if !strings.Contains(entityErrs(c), "list return requires offset param") {
		t.Fatalf("missing pagination params not reported: %v", c.errs)
	}
}

func TestQueryDefaultRequiresNullable(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{ID: "q", File: "q.yaml", Params: []queryParam{
		{Name: "ctx", Type: "context.Context"},
		{Name: "page_size", Type: "integer", HasDefault: true},
	}}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "default requires nullable:true") {
		t.Fatalf("default without nullable not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[1].Nullable = true
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid nullable default reported: %v", c.errs)
	}
}

func TestQueryParamTypeAndDefaultValidation(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{ID: "q", File: "q.yaml", Params: []queryParam{
		{Name: "ctx", Type: "context.Context"},
		{Name: "mode", Type: "unknown_type"},
	}}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "cannot map to a type family") {
		t.Fatalf("unknown param type not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[1] = queryParam{Name: "status", Type: "data.enums.account_status"}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("enum param reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[1] = queryParam{Name: "page_size", Type: "integer", Nullable: true, HasDefault: true, DefaultValue: "20"}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "default must be an integer") {
		t.Fatalf("wrong default type not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[1] = queryParam{Name: "ids", Type: "List<integer>", Nullable: true, HasDefault: true, DefaultValue: 1}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "default must be a list") {
		t.Fatalf("non-list default for array not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[1] = queryParam{Name: "since", Type: "timestamp", Nullable: true, HasDefault: true, DefaultValue: "2026-01-01T00:00:00Z"}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("valid timestamp default reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[1] = queryParam{Name: "statuses", Type: "List<data.enums.account_status>", Nullable: true}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("List<enum> param reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[1] = queryParam{Name: "statuses", Type: "List<data.enums.account_status>", Nullable: true, HasDefault: true, DefaultValue: "bad"}
	c.checkQueryParams(q)
	if !strings.Contains(entityErrs(c), "default must be a list") {
		t.Fatalf("enum array default outer type not reported: %v", c.errs)
	}

	c.errs = nil
	q.Params[1] = queryParam{Name: "since", Type: "timestamp", Nullable: true, HasDefault: true, DefaultValue: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	c.checkQueryParams(q)
	if len(c.errs) != 0 {
		t.Fatalf("time.Time default reported: %v", c.errs)
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
	b, err := contractspec.RenderQuery(schema, "note", "notes", "data.tables.note", "备注查询")
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
	if e.Fields[0].JSONName != "id" || e.Fields[0].Type != "string" || e.Fields[0].Description != "" {
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
	q := &queryInfo{ID: "data.queries.x.create", Targets: []string{"data.tables.x"}, Writes: []string{"data.tables.x"}}
	c := &Checker{
		tableOwners: map[string]string{"data.tables.x": "x"},
		queries:     map[string]*queryInfo{q.ID: q},
	}
	doc := func(module string) map[string]any {
		return map[string]any{
			"owner_module": module,
			"interface": map[string]any{"methods": []any{
				map[string]any{"name": "Create", "queries": []any{q.ID}},
			}},
			"persistence": map[string]any{"tables": []any{"data.tables.x"}},
		}
	}
	tables := map[string]string{"data.tables.x": "contracts/data/tables/x.yaml"}
	c.checkMethodPersistence(doc("x"), "cap.yaml", tables)
	if len(c.errs) != 0 {
		t.Fatalf("valid write query reported: %v", c.errs)
	}

	c.errs = nil
	c.checkMethodPersistence(doc("other"), "cap.yaml", tables)
	if !strings.Contains(entityErrs(c), "cannot write") {
		t.Fatalf("cross-module write not reported: %v", c.errs)
	}

	c.errs = nil
	q.Writes = nil
	q.Returns = []queryReturn{{Kind: "affected_rows"}}
	c.checkMethodPersistence(doc("x"), "cap.yaml", tables)
	if !strings.Contains(entityErrs(c), "returns affected_rows without a write table") {
		t.Fatalf("write query without write table not reported: %v", c.errs)
	}

	c.errs = nil
	q.Writes = []string{"data.tables.x", "data.tables.y"}
	q.Returns = nil
	c.tableOwners["data.tables.y"] = "x"
	c.checkMethodPersistence(doc("x"), "cap.yaml", map[string]string{"data.tables.x": "contracts/data/tables/x.yaml", "data.tables.y": "contracts/data/tables/y.yaml"})
	if len(c.errs) != 0 {
		t.Fatalf("multi-table write reported: %v", c.errs)
	}
}

func TestUndeclaredWriteQueryRejected(t *testing.T) {
	c := &Checker{
		root:       t.TempDir(),
		queryOrder: []string{"data.queries.x.create"},
		queries: map[string]*queryInfo{
			"data.queries.x.create": {ID: "data.queries.x.create", Writes: []string{"data.tables.x"}},
		},
	}
	c.checkCapabilityWrites(map[string]string{})
	if !strings.Contains(entityErrs(c), "not referenced by any service step") {
		t.Fatalf("undeclared write table query not reported: %v", c.errs)
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

	c.errs = nil
	q = &queryInfo{ID: "q", File: "q.yaml", Targets: []string{"data.tables.x", "data.tables.x"}}
	c.checkQueryTable(q)
	if !strings.Contains(entityErrs(c), "duplicate table data.tables.x") {
		t.Fatalf("duplicate table not reported: %v", c.errs)
	}
}

func TestQueryTablesWriteFlag(t *testing.T) {
	q := parseQuery(map[string]any{
		"name": "CreateX",
		"tables": []any{
			map[string]any{"table": "data.tables.x", "write": true},
			map[string]any{"table": "data.tables.y"},
		},
	}, "q.yaml", "data.queries.x", "x")
	if len(q.Targets) != 2 || q.Targets[0] != "data.tables.x" || q.Targets[1] != "data.tables.y" {
		t.Fatalf("tables not parsed: %#v", q.Targets)
	}
	if len(q.Writes) != 1 || q.Writes[0] != "data.tables.x" {
		t.Fatalf("write tables not parsed: %#v", q.Writes)
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
		Returns: []queryReturn{{Name: "exists", Kind: "scalar", Type: "boolean"}, {Name: "err", Kind: "error"}}}
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
		Returns: []queryReturn{{Name: "affected_rows", Kind: "affected_rows"}, {Name: "x", Kind: "scalar", Type: "integer"}, {Name: "err", Kind: "error"}}}
	c.errs = nil
	c.checkQueryShape(q)
	if !strings.Contains(entityErrs(c), "must be the only non-error return item") {
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

func TestQueryReturnNullable(t *testing.T) {
	c := &Checker{}
	q := &queryInfo{ID: "q", File: "q.yaml", Returns: []queryReturn{
		{Name: "x", Kind: "scalar", Type: "integer", Nullable: true},
		{Name: "err", Kind: "error"},
	}}
	c.checkQueryReturns(q)
	if len(c.errs) != 0 {
		t.Fatalf("nullable scalar return reported: %v", c.errs)
	}
	c = &Checker{}
	q.Returns = []queryReturn{
		{Name: "affected_rows", Kind: "affected_rows", Nullable: true},
		{Name: "err", Kind: "error"},
	}
	c.checkQueryReturns(q)
	if !strings.Contains(entityErrs(c), "affected_rows forbids nullable") {
		t.Fatalf("affected_rows nullable not reported: %v", c.errs)
	}
}
