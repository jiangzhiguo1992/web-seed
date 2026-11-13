// query_indexes.go 校验 query.index_ref 与表索引的双向引用。
package check

import (
	"path/filepath"
	"sort"
)

type tableIndex struct {
	name    string
	columns []string
	unique  bool
	usedBy  map[string]bool
}

// loadTableIndexes 收集物理表的显式索引与主键，供 query.index_ref 校验。
func (c *Checker) loadTableIndexes(tableIDs map[string]string) {
	c.tableIndexes = map[string][]tableIndex{}
	c.tablePrimaryKey = map[string][]string{}
	for tableID, rel := range tableIDs {
		if tableID == c.baseID {
			continue
		}
		doc, err := loadYAML(filepath.Join(c.root, rel))
		if err != nil {
			continue
		}
		c.tablePrimaryKey[tableID] = []string{"id"}
		for _, raw := range asList(doc["columns"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			if pk, _ := m["primary_key"].(bool); pk {
				if name, _ := m["name"].(string); name != "" {
					c.tablePrimaryKey[tableID] = append(c.tablePrimaryKey[tableID], name)
				}
			}
		}
		for _, raw := range asList(doc["indexes"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			cols := stringList(m["columns"])
			name, _ := m["name"].(string)
			used := map[string]bool{}
			for _, q := range asList(m["used_by"]) {
				if s, ok := q.(string); ok && s != "" {
					used[s] = true
				}
			}
			unique, _ := m["unique"].(bool)
			c.tableIndexes[tableID] = append(c.tableIndexes[tableID], tableIndex{
				name: name, columns: cols, unique: unique, usedBy: used,
			})
		}
	}
}

func stringList(v any) []string {
	list := asList(v)
	out := make([]string, 0, len(list))
	for _, raw := range list {
		if s, ok := raw.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// checkQueryIndexRefs 校验：
//   - 单表 query 至少声明一个 index_ref；
//   - 每个 ref 指向查询表的 PK 或 indexes[].name；
//   - 显式索引必须在 used_by 中列出该查询；
//   - 反向：索引 used_by 的查询必须在 index_ref 中引用该索引。
func (c *Checker) checkQueryIndexRefs() {
	for _, qid := range c.queryOrder {
		q := c.queries[qid]
		tables := c.queryTables[qid]
		if len(tables) == 0 {
			continue // 表引用缺失由 query-table-shape 报出
		}
		if len(q.Targets) == 1 && len(q.IndexRef) == 0 {
			c.rulef("query-index-used-by", "query %s: single-table query must declare at least one index_ref", qid)
			continue
		}
		for _, ref := range q.IndexRef {
			table, name, ok := splitIndexRef(ref)
			if !ok {
				c.rulef("query-index-ref", "query %s: malformed index_ref %q", qid, ref)
				continue
			}
			if !tables[table] {
				c.rulef("query-index-ref", "query %s: index_ref table %s is not read by the query", qid, table)
				continue
			}
			if name == "pk" {
				continue
			}
			idx := c.findTableIndex(table, name)
			if idx == nil {
				c.rulef("query-index-ref", "query %s: index %s is not declared on %s", qid, name, table)
				continue
			}
			if !idx.usedBy[qid] {
				c.rulef("query-index-ref", "query %s: index %s does not list the query in used_by", qid, name)
			}
		}
	}
	tableIDs := make([]string, 0, len(c.tableIndexes))
	for table := range c.tableIndexes {
		tableIDs = append(tableIDs, table)
	}
	sort.Strings(tableIDs)
	for _, table := range tableIDs {
		for _, idx := range c.tableIndexes[table] {
			for _, qid := range sortedKeys(idx.usedBy) {
				q := c.queries[qid]
				if q == nil || !c.queryTables[qid][table] {
					continue // 未知查询/表不匹配由 index-used-by-ref 报出
				}
				if !refInList(q.IndexRef, table+"."+idx.name) {
					c.rulef("query-index-used-by", "query %s: index %s lists the query but index_ref does not", qid, idx.name)
				}
			}
		}
	}
}

func (c *Checker) findTableIndex(table, name string) *tableIndex {
	for i := range c.tableIndexes[table] {
		if c.tableIndexes[table][i].name == name {
			return &c.tableIndexes[table][i]
		}
	}
	return nil
}

func refInList(refs []string, want string) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}
