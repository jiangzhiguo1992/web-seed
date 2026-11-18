// references.go 校验 columns[].references（逻辑外键）：
// 目标列唯一、两端类型一致、set_null 要求源列可空；
// 并与 entity 关系字段的 join 列对双向对账，防止表侧级联策略与实体关系漂移。
package check

import (
	"fmt"
	"sort"
)

// referenceDecl 是一条列级逻辑外键声明。
type referenceDecl struct {
	source   string
	target   string
	onDelete string
	where    string
}

// referencePairKey 生成与方向无关的列对标识。
func referencePairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}

func (c *Checker) checkReferences(tableIDs map[string]string) {
	var decls []referenceDecl
	inbound := map[string]map[string]bool{}
	for _, id := range sortedStringMapKeys(tableIDs) {
		if id == c.baseID {
			continue
		}
		for _, raw := range asList(c.tableDocs[id]["columns"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			rawRef, ok := m["references"]
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			where := fmt.Sprintf("%s: columns.%s.references", tableIDs[id], name)
			ref, ok := asMap(rawRef)
			if !ok {
				continue // 结构错误由 schema 校验报出
			}
			target, _ := ref["column"].(string)
			onDelete, _ := ref["on_delete"].(string)
			if target == "" || onDelete == "" {
				continue
			}
			source := id + ".col." + name
			decls = append(decls, referenceDecl{source: source, target: target, onDelete: onDelete, where: where})
			if targetTable, _, ok := splitColumnRef(target); ok {
				if inbound[targetTable] == nil {
					inbound[targetTable] = map[string]bool{}
				}
				inbound[targetTable][source] = true
			}
			c.checkReferenceDecl(source, target, onDelete, where)
		}
	}
	c.checkTableReferencedBy(tableIDs, inbound)
	c.checkReferenceJoins(decls)
}

// checkTableReferencedBy 校验 table.referenced_by 与列级 references 的双向一致。
func (c *Checker) checkTableReferencedBy(tableIDs map[string]string, inbound map[string]map[string]bool) {
	for _, id := range sortedStringMapKeys(tableIDs) {
		if id == c.baseID {
			continue
		}
		table, _ := asMap(c.tableDocs[id]["table"])
		got := stringSet(asList(table["referenced_by"]))
		want := inbound[id]
		if want == nil {
			want = map[string]bool{}
		}
		if !sameSet(got, want) {
			c.rulef("table-referenced-by", "%s: table.referenced_by must be %v, got %v", tableIDs[id], sortedKeys(want), sortedKeys(got))
		}
	}
}

// checkReferenceDecl 校验单条逻辑外键的目标与动作。
func (c *Checker) checkReferenceDecl(source, target, onDelete, where string) {
	sourceTable, sourceCol, ok := splitColumnRef(source)
	if !ok {
		return
	}
	if c.inheritedCols[sourceTable][sourceCol] {
		c.rulef("reference-target", "%s: source %s must be declared on its own table", where, source)
		return
	}
	sourceMeta, ok := c.tableColumns[sourceTable][sourceCol]
	if !ok {
		return
	}
	targetTable, targetCol, ok := splitColumnRef(target)
	if !ok {
		return
	}
	targetMeta, ok := c.tableColumns[targetTable][targetCol]
	if !ok {
		c.rulef("reference-target", "%s: target column %s not found", where, target)
		return
	}
	if !c.columnStableUnique(targetTable, targetCol) {
		c.rulef("reference-target", "%s: target column %s must be a primary key or globally unique single-column index", where, target)
	}
	if c.resolvedColumnType(sourceMeta.typ) != c.resolvedColumnType(targetMeta.typ) {
		c.rulef("reference-target", "%s: source %s type %s does not match target %s type %s", where, source, c.resolvedColumnType(sourceMeta.typ), target, c.resolvedColumnType(targetMeta.typ))
	}
	if onDelete == "set_null" && !sourceMeta.nullable {
		c.rulef("reference-on-delete", "%s: set_null requires nullable source column %s", where, source)
	}
}

// columnStableUnique 判断列是否为主键或全局唯一（live_only: false）的单列索引。
// 逻辑外键必须指向稳定身份：live_only 唯一列在软删后可重复，不能作为引用目标。
func (c *Checker) columnStableUnique(table, col string) bool {
	if meta, ok := c.tableColumns[table][col]; ok && meta.primaryKey {
		return true
	}
	for _, idx := range c.tableIndexes[table] {
		if idx.unique && !idx.liveOnly && len(idx.columns) == 1 && idx.columns[0] == col {
			return true
		}
	}
	return false
}

// checkReferenceJoins 对账 references 与 entity 关系字段的 join 列对。
// 比较不看 from/to 方向：子侧 references 与父侧 join 指向同一对列。
func (c *Checker) checkReferenceJoins(decls []referenceDecl) {
	joinWhere := map[string][]string{}
	for _, id := range c.entityOrder {
		e := c.entities[id]
		for _, f := range e.Fields {
			if !f.IsRelation || !f.HasJoin || f.JoinFromRef == "" || f.JoinToRef == "" {
				continue
			}
			if _, ok := c.tableColumns[f.JoinFromTable][f.JoinFromColumn]; !ok {
				continue
			}
			if _, ok := c.tableColumns[f.JoinToTable][f.JoinToColumn]; !ok {
				continue
			}
			key := referencePairKey(f.JoinFromRef, f.JoinToRef)
			joinWhere[key] = append(joinWhere[key], fmt.Sprintf("%s: fields.%s", e.Rel, f.Name))
		}
	}

	refPairs := map[string]bool{}
	for _, d := range decls {
		key := referencePairKey(d.source, d.target)
		refPairs[key] = true
		if _, ok := joinWhere[key]; !ok {
			c.rulef("reference-join", "%s: no entity relation join covers %s -> %s", d.where, d.source, d.target)
		}
	}
	keys := make([]string, 0, len(joinWhere))
	for key := range joinWhere {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if refPairs[key] {
			continue
		}
		for _, where := range joinWhere[key] {
			c.rulef("reference-join", "%s: join columns have no matching columns[].references", where)
		}
	}
}
