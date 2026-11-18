package check

import (
	"strings"
	"testing"
)

// referenceTestChecker 构造引用校验所需的最小物理表视图：
// a 是被引用表（id 主键 + code 非唯一列），b 是声明 references 的子表。
func referenceTestChecker(column map[string]any, referencedBy []any) *Checker {
	return &Checker{
		baseID: "data.tables.base",
		inheritedCols: map[string]map[string]bool{
			"data.tables.a": {"id": true, "status": true},
			"data.tables.b": {"id": true, "status": true},
		},
		tableColumns: map[string]map[string]columnMeta{
			"data.tables.a": {
				"id":   {typ: "integer", primaryKey: true},
				"code": {typ: "string"},
			},
			"data.tables.b": {
				"id":     {typ: "integer", primaryKey: true},
				"a_id":   {typ: "integer"},
				"a_code": {typ: "string"},
				"opt_id": {typ: "integer", nullable: true},
			},
		},
		tableDocs: map[string]map[string]any{
			"data.tables.a": {
				"table":   map[string]any{"referenced_by": referencedBy},
				"columns": []any{map[string]any{"name": "code", "type": "string"}},
			},
			"data.tables.b": {
				"table":   map[string]any{"referenced_by": []any{}},
				"columns": []any{column},
			},
		},
		tableIndexes: map[string][]tableIndex{},
	}
}

func referenceTestTables() map[string]string {
	return map[string]string{
		"data.tables.a": "contracts/data/tables/a.yaml",
		"data.tables.b": "contracts/data/tables/b.yaml",
	}
}

func TestReferencesValidWithJoin(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name":       "a_id",
		"references": map[string]any{"column": "data.tables.a.col.id", "on_delete": "cascade"},
	}, []any{"data.tables.b.col.a_id"})
	c.entities = map[string]*entityInfo{
		"data.entities.a": {
			ID:  "data.entities.a",
			Rel: "contracts/data/entities/a.yaml",
			Fields: []entityFieldInfo{
				relationField("Items", "data.entities.b", "data.tables.a", "id", "data.tables.b", "a_id", "lazy"),
			},
		},
	}
	c.entityOrder = []string{"data.entities.a"}
	c.checkReferences(referenceTestTables())
	if len(c.errs) != 0 {
		t.Fatalf("valid references reported: %v", c.errs)
	}
}

func TestReferencesTargetMustBeUnique(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name":       "a_code",
		"references": map[string]any{"column": "data.tables.a.col.code", "on_delete": "set_null"},
	}, []any{"data.tables.b.col.a_code"})
	c.checkReferences(referenceTestTables())
	if !strings.Contains(entityErrs(c), "target column data.tables.a.col.code must be a primary key or globally unique single-column index") {
		t.Fatalf("non-unique target not reported: %v", c.errs)
	}
	if !strings.Contains(entityErrs(c), "set_null requires nullable source column data.tables.b.col.a_code") {
		t.Fatalf("set_null on non-null source not reported: %v", c.errs)
	}
}

func TestReferencesTargetRejectsLiveOnlyUnique(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name":       "a_code",
		"references": map[string]any{"column": "data.tables.a.col.code", "on_delete": "no_action"},
	}, []any{"data.tables.b.col.a_code"})
	c.tableIndexes["data.tables.a"] = []tableIndex{
		{name: "uq_a_code", columns: []string{"code"}, unique: true, liveOnly: true},
	}
	c.checkReferences(referenceTestTables())
	if !strings.Contains(entityErrs(c), "globally unique single-column index") {
		t.Fatalf("live_only unique target not rejected: %v", c.errs)
	}
}

func TestReferencesTargetAcceptsGlobalUnique(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name":       "a_code",
		"references": map[string]any{"column": "data.tables.a.col.code", "on_delete": "no_action"},
	}, []any{"data.tables.b.col.a_code"})
	c.tableIndexes["data.tables.a"] = []tableIndex{
		{name: "uq_a_code", columns: []string{"code"}, unique: true, liveOnly: false},
	}
	c.entities = map[string]*entityInfo{
		"data.entities.a": {
			ID:  "data.entities.a",
			Rel: "contracts/data/entities/a.yaml",
			Fields: []entityFieldInfo{
				relationField("Codes", "data.entities.b", "data.tables.a", "code", "data.tables.b", "a_code", "lazy"),
			},
		},
	}
	c.entityOrder = []string{"data.entities.a"}
	c.checkReferences(referenceTestTables())
	if len(c.errs) != 0 {
		t.Fatalf("global unique target reported: %v", c.errs)
	}
}

func TestReferencesTypeMismatch(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name":       "opt_id",
		"references": map[string]any{"column": "data.tables.a.col.code", "on_delete": "no_action"},
	}, []any{"data.tables.b.col.opt_id"})
	c.checkReferences(referenceTestTables())
	if !strings.Contains(entityErrs(c), "does not match target data.tables.a.col.code type string") {
		t.Fatalf("type mismatch not reported: %v", c.errs)
	}
}

func TestReferencesSourceMustBeOwnColumn(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name":       "id",
		"references": map[string]any{"column": "data.tables.a.col.id", "on_delete": "cascade"},
	}, []any{"data.tables.b.col.id"})
	c.checkReferences(referenceTestTables())
	if !strings.Contains(entityErrs(c), "source data.tables.b.col.id must be declared on its own table") {
		t.Fatalf("inherited source not reported: %v", c.errs)
	}
}

func TestReferencesTargetMustExist(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name":       "a_id",
		"references": map[string]any{"column": "data.tables.a.col.missing", "on_delete": "cascade"},
	}, []any{"data.tables.b.col.a_id"})
	c.checkReferences(referenceTestTables())
	if !strings.Contains(entityErrs(c), "target column data.tables.a.col.missing not found") {
		t.Fatalf("missing target not reported: %v", c.errs)
	}
}

func TestReferencesRequireEntityJoin(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name":       "a_id",
		"references": map[string]any{"column": "data.tables.a.col.id", "on_delete": "cascade"},
	}, []any{"data.tables.b.col.a_id"})
	c.checkReferences(referenceTestTables())
	if !strings.Contains(entityErrs(c), "no entity relation join covers data.tables.b.col.a_id -> data.tables.a.col.id") {
		t.Fatalf("orphan references not reported: %v", c.errs)
	}
}

func TestEntityJoinRequiresReferences(t *testing.T) {
	c := referenceTestChecker(map[string]any{"name": "a_id"}, []any{})
	c.entities = map[string]*entityInfo{
		"data.entities.a": {
			ID:  "data.entities.a",
			Rel: "contracts/data/entities/a.yaml",
			Fields: []entityFieldInfo{
				relationField("Items", "data.entities.b", "data.tables.a", "id", "data.tables.b", "a_id", "lazy"),
			},
		},
	}
	c.entityOrder = []string{"data.entities.a"}
	c.checkReferences(referenceTestTables())
	if !strings.Contains(entityErrs(c), "join columns have no matching columns[].references") {
		t.Fatalf("orphan join not reported: %v", c.errs)
	}
}

func TestTableReferencedByMismatch(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name":       "a_id",
		"references": map[string]any{"column": "data.tables.a.col.id", "on_delete": "cascade"},
	}, []any{})
	c.checkReferences(referenceTestTables())
	if !strings.Contains(entityErrs(c), "table.referenced_by must be [data.tables.b.col.a_id], got []") {
		t.Fatalf("missing referenced_by not reported: %v", c.errs)
	}

	c = referenceTestChecker(map[string]any{
		"name":       "a_id",
		"references": map[string]any{"column": "data.tables.a.col.id", "on_delete": "cascade"},
	}, []any{"data.tables.b.col.opt_id"})
	c.checkReferences(referenceTestTables())
	if !strings.Contains(entityErrs(c), "table.referenced_by must be [data.tables.b.col.a_id], got [data.tables.b.col.opt_id]") {
		t.Fatalf("extra referenced_by not reported: %v", c.errs)
	}
}

func TestReferencesCoexistWithRedundancy(t *testing.T) {
	c := referenceTestChecker(map[string]any{
		"name": "a_id",
		"redundancy": []any{
			map[string]any{"column": "data.tables.a.col.id", "source": true},
			map[string]any{"column": "data.tables.b.col.a_id", "source": false},
		},
		"references": map[string]any{"column": "data.tables.a.col.id", "on_delete": "cascade"},
	}, []any{"data.tables.b.col.a_id"})
	c.entities = map[string]*entityInfo{
		"data.entities.a": {
			ID:  "data.entities.a",
			Rel: "contracts/data/entities/a.yaml",
			Fields: []entityFieldInfo{
				relationField("Items", "data.entities.b", "data.tables.a", "id", "data.tables.b", "a_id", "lazy"),
			},
		},
	}
	c.entityOrder = []string{"data.entities.a"}
	c.checkReferences(referenceTestTables())
	if len(c.errs) != 0 {
		t.Fatalf("redundancy + references reported: %v", c.errs)
	}
}
