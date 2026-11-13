// refs.go 校验 capabilities 的表/写查询引用、used_by 与 index.yaml。
// entity/query 自身的表引用由 entities.go / queries.go 的类型级规则负责。
package check

import (
	"os"
	"path/filepath"
	"strings"
)

func checkColumnRef(c *Checker, ref string, tableIDs map[string]string, cols map[string]map[string]bool, where string) {
	if ref == "" {
		return
	}
	parts := strings.Split(ref, ".col.")
	if len(parts) != 2 {
		c.rulef("schema.ref", "%s: column ref must be <table-id>.col.<column>, got %s", where, ref)
		return
	}
	tid, col := parts[0], parts[1]
	if _, ok := tableIDs[tid]; !ok {
		c.rulef("schema.ref", "%s: column ref points to unknown table %s", where, tid)
		return
	}
	if set := cols[tid]; set != nil && !set[col] {
		c.rulef("schema.ref", "%s: column ref points to unknown column %s.%s", where, tid, col)
	}
}

// isBaseTableRef 判断引用是否指向非物理的基类表。
// 基类只提供列定义，不能被实体、查询或能力当作物理表引用。
func (c *Checker) isBaseTableRef(ref string) bool {
	return c.baseID != "" && (ref == c.baseID || strings.HasPrefix(ref, c.baseID+".col."))
}

func (c *Checker) checkCrossFileTableRefs(tableIDs map[string]string) {
	c.declaredWrites = map[string]bool{}
	// capabilities
	for _, f := range walkYAML(c.root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, f), err)
			continue
		}
		rel := relPath(c.root, f)
		if rawWrites, ok := doc["writes"]; ok {
			if !capabilityMayWrite(doc) {
				c.rulef("owner-write", "%s: only cap.services/cap.async may declare writes", rel)
			}
			uses := map[string]bool{}
			for _, x := range asList(doc["uses"]) {
				if s, ok := x.(string); ok && s != "" {
					uses[s] = true
				}
			}
			list, ok := rawWrites.([]any)
			if !ok {
				c.rulef("schema.type", "%s: writes must be a list", rel)
			} else {
				if module, _ := doc["module"].(string); module == "" {
					c.rulef("owner-write", "%s: writes requires module to verify write ownership", rel)
				}
				for _, x := range list {
					if qid, ok := x.(string); ok && qid != "" {
						c.declaredWrites[qid] = true
					}
				}
				c.checkWriteQueries(doc, list, uses, nil, "writes", "uses", rel)
			}
		}
		p, _ := asMap(doc["persistence"])
		if p == nil {
			continue
		}
		if !capabilityMayWrite(doc) && (len(tableRefs(p)) > 0 || p["write_queries"] != nil) {
			c.rulef("owner-write", "%s: only cap.services/cap.async may declare table writes", rel)
		}
		c.checkPersistenceOwnership(doc, p, rel)
		if rawQ, ok := p["queries"]; ok {
			qm, ok := asMap(rawQ)
			if !ok {
				c.rulef("schema.type", "%s: persistence.queries must be a map", rel)
			} else {
				for alias, raw := range qm {
					qid, _ := raw.(string)
					if qid == "" {
						c.rulef("schema.required", "%s: persistence.queries.%s must be a query ID", rel, alias)
						continue
					}
					if _, ok := c.queryIDs[qid]; !ok {
						c.rulef("schema.ref", "%s: persistence.queries.%s references unknown query %s", rel, alias, qid)
					}
				}
			}
		}
		if rawCE, ok := p["constraint_errors"]; ok {
			ce, ok := asMap(rawCE)
			if !ok {
				c.rulef("schema.type", "%s: persistence.constraint_errors must be a map", rel)
			} else {
				tables := tableRefs(p)
				for name := range ce {
					if name == "" {
						c.rulef("schema.non-empty", "%s: persistence.constraint_errors key must not be empty", rel)
						continue
					}
					if !c.tableObjectExists(tables, name) {
						c.rulef("constraint-error-ref", "%s: persistence.constraint_errors.%s is not a constraint/index of %v", rel, name, tables)
					}
				}
			}
		}
		for _, t := range tableRefs(p) {
			if c.isBaseTableRef(t) {
				c.rulef("schema.ref", "%s: base contract is not a physical table", rel)
			} else if _, ok := tableIDs[t]; !ok {
				c.rulef("schema.ref", "%s: persistence references unknown table %s", rel, t)
			}
		}
	}
	for _, qid := range c.queryOrder {
		q := c.queries[qid]
		if q != nil && queryReturnsAffectedRows(q) && !c.declaredWrites[qid] {
			c.rulef("owner-write", "query %s returns affected_rows but is not declared in any capability write list", qid)
		}
	}
}

// capabilityMayWrite 只允许 service 与 async capability 声明写表/写查询。
func capabilityMayWrite(doc map[string]any) bool {
	id, _ := doc["id"].(string)
	return strings.HasPrefix(id, "cap.services.") || strings.HasPrefix(id, "cap.async.")
}

// checkPersistenceOwnership 校验 capability 的写表与写查询归属：
//   - read_only_tables 已移除，跨模块读必须通过 persistence.queries；
//   - 写表（table/tables）的 owner_module 必须等于 capability 的 module；
//   - write_queries 必须已在 persistence.queries 中登记、单表、目标表归属一致。
func (c *Checker) checkPersistenceOwnership(doc, p map[string]any, rel string) {
	if _, ok := p["read_only_tables"]; ok {
		c.rulef("owner-write", "%s: read_only_tables is not supported; use persistence.queries for cross-module reads", rel)
	}
	if rawTables, ok := p["tables"]; ok {
		if _, ok := rawTables.([]any); !ok {
			c.rulef("schema.type", "%s: persistence.tables must be a list", rel)
		}
	}
	writes := tableRefs(p)
	module, _ := doc["module"].(string)
	if len(writes) > 0 {
		if module == "" {
			c.rulef("owner-write", "%s: persistence requires module to verify write ownership", rel)
		} else {
			for _, t := range writes {
				if owner := c.tableOwners[t]; owner != "" && owner != module {
					c.rulef("owner-write", "%s: module %q cannot write %s (owner_module %q)", rel, module, t, owner)
				}
			}
		}
	}
	declared := map[string]bool{}
	if qm, ok := asMap(p["queries"]); ok {
		for _, raw := range qm {
			if qid, ok := raw.(string); ok && qid != "" {
				declared[qid] = true
			}
		}
	}
	rawWrites, ok := p["write_queries"]
	if !ok {
		return
	}
	list, ok := rawWrites.([]any)
	if !ok {
		c.rulef("schema.type", "%s: persistence.write_queries must be a list", rel)
		return
	}
	if module == "" {
		c.rulef("owner-write", "%s: persistence.write_queries requires module to verify write ownership", rel)
	}
	for _, x := range list {
		if qid, ok := x.(string); ok && qid != "" {
			c.declaredWrites[qid] = true
		}
	}
	c.checkWriteQueries(doc, list, declared, writes, "persistence.write_queries", "persistence.queries", rel)
}

// checkWriteQueries 校验 capability 声明的写查询：
//   - 必须已在 declared 列表（persistence.queries / uses）中登记；
//   - 必须存在且恰好写一张表；
//   - 目标表必须在 persistence.table/tables 中，且 owner_module 与 capability 一致。
func (c *Checker) checkWriteQueries(doc map[string]any, raw []any, declared map[string]bool, tables []string, field, declaredName, rel string) {
	module, _ := doc["module"].(string)
	for i, x := range raw {
		qid, ok := x.(string)
		if !ok || qid == "" {
			c.rulef("owner-write", "%s: %s[%d] must be a query ID", rel, field, i)
			continue
		}
		if !declared[qid] {
			c.rulef("owner-write", "%s: write query %s must be listed in %s", rel, qid, declaredName)
		}
		q := c.queries[qid]
		if q == nil {
			c.rulef("owner-write", "%s: unknown write query %s", rel, qid)
			continue
		}
		if len(q.Targets) != 1 {
			c.rulef("owner-write", "%s: write query %s requires exactly one target", rel, qid)
			continue
		}
		target := q.Targets[0]
		if len(tables) > 0 && !containsString(tables, target) {
			c.rulef("owner-write", "%s: write query %s tables %s outside persistence.table/tables", rel, qid, target)
		}
		if module != "" {
			if owner := c.tableOwners[target]; owner != "" && owner != module {
				c.rulef("owner-write", "%s: module %q cannot write %s via %s (owner_module %q)", rel, module, target, qid, owner)
			}
		}
	}
}

// tableObjectExists 判断约束/索引名是否属于给定表列表中的任一表。
func (c *Checker) tableObjectExists(tables []string, name string) bool {
	for _, t := range tables {
		if c.tableObjects[t][name] {
			return true
		}
	}
	return false
}

func (c *Checker) checkUsedBy(tableIDs map[string]string) {
	tableEntities := map[string]map[string]bool{}
	tableQueries := map[string]map[string]bool{}
	for id := range tableIDs {
		if id == c.baseID {
			continue
		}
		tableEntities[id] = map[string]bool{}
		tableQueries[id] = map[string]bool{}
	}
	// table.entities 来自 entity.model.tables；查询按查询项 ID 记直接入边。
	for _, e := range c.entities {
		for _, t := range e.Tables {
			if tableEntities[t] != nil {
				tableEntities[t][e.ID] = true
			}
		}
	}
	for _, q := range c.queries {
		for t := range q.tableSet() {
			if tableQueries[t] != nil {
				tableQueries[t][q.ID] = true
			}
		}
	}

	for id, rel := range tableIDs {
		if id == c.baseID {
			continue
		}
		doc, err := loadYAML(filepath.Join(c.root, rel))
		if err != nil {
			continue
		}
		table, _ := asMap(doc["table"])
		if table != nil {
			if raw, ok := table["entities"]; ok {
				gotEntities := stringSet(asList(raw))
				if !sameSet(gotEntities, tableEntities[id]) {
					c.rulef("table-entities", "%s: table.entities mismatch: declared %v, actual %v", rel, sortedKeys(gotEntities), sortedKeys(tableEntities[id]))
				}
			}
		}
		ub, _ := asMap(doc["used_by"])
		if ub == nil {
			continue // 缺失由 schema.required 报出
		}
		if raw, ok := ub["queries"]; ok {
			gotQueries := stringSet(asList(raw))
			if !sameSet(gotQueries, tableQueries[id]) {
				c.rulef("used-by-generated", "%s: used_by.queries mismatch: declared %v, actual %v", rel, sortedKeys(gotQueries), sortedKeys(tableQueries[id]))
			}
		}
	}
}

func (c *Checker) checkIndex(tableIDs map[string]string, enumSchema, entitySchema, querySchema *Schema) {
	path := filepath.Join(c.root, "contracts/index.yaml")
	rel := relPath(c.root, path)
	doc, err := loadYAML(path)
	if err != nil {
		c.rulef("schema.load", "%s: %v", rel, err)
		return
	}
	byPath := map[string]string{}
	byID := map[string]string{}
	rawEntries := asList(doc["entries"])
	if len(rawEntries) == 0 {
		c.rulef("schema.non-empty", "%s: entries must not be empty", rel)
	}
	for i, raw := range rawEntries {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("schema.type", "%s: entries[%d] must be an object", rel, i)
			continue
		}
		id, _ := m["id"].(string)
		p, _ := m["path"].(string)
		if id == "" {
			c.rulef("schema.required", "%s: entries[%d]: missing id", rel, i)
			continue
		}
		if p == "" {
			c.rulef("schema.required", "%s: %s: missing path", rel, id)
			continue
		}
		if prev, ok := byID[id]; ok {
			c.rulef("schema.unique", "%s: duplicate id %s (%s / %s)", rel, id, prev, p)
		}
		byID[id] = p
		if prev, ok := byPath[p]; ok {
			c.rulef("schema.unique", "%s: duplicate path %s (%s / %s)", rel, p, prev, id)
		}
		byPath[p] = id
		if strings.HasPrefix(p, "generated/") {
			continue
		}
		if _, err := os.Stat(filepath.Join(c.root, "contracts", p)); err != nil {
			c.rulef("schema.ref", "%s: %s path missing %s", rel, id, p)
		}
	}
	for id, file := range tableIDs {
		p := strings.TrimPrefix(file, "contracts/")
		got, ok := byPath[p]
		if !ok {
			c.rulef("schema.index", "%s: missing entry for %s (%s)", rel, id, p)
			continue
		}
		if got != id {
			c.rulef("schema.index", "%s: path %s has id %s, expected %s", rel, p, got, id)
		}
	}
	if enumSchema.Targets.File != "" && enumSchema.Targets.ID != "" {
		p := strings.TrimPrefix(enumSchema.Targets.File, "contracts/")
		if got, ok := byPath[p]; !ok {
			c.rulef("schema.index", "%s: missing entry for %s", rel, p)
		} else if got != enumSchema.Targets.ID {
			c.rulef("schema.index", "%s: %s has id %s, expected %s", rel, p, got, enumSchema.Targets.ID)
		}
	}
	for _, schema := range []*Schema{entitySchema, querySchema} {
		if schema == nil || schema.Targets.Dir == "" || schema.Targets.Glob == "" {
			continue
		}
		for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
			wantID := schema.Targets.FileID(filepath.Base(file))
			if schema.Type == "data.entities" && filepath.Base(file) == "_base.yaml" {
				wantID = baseEntityID
			}
			if wantID == "" {
				continue
			}
			p := strings.TrimPrefix(relPath(c.root, file), "contracts/")
			got, ok := byPath[p]
			if !ok {
				c.rulef("schema.index", "%s: missing entry for %s (%s)", rel, wantID, p)
				continue
			}
			if got != wantID {
				c.rulef("schema.index", "%s: path %s has id %s, expected %s", rel, p, got, wantID)
			}
		}
	}
}
