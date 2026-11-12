// query_indexes.go 校验 query.index_columns 与表索引的最左前缀。
package check

import (
	"path/filepath"
)

type tableIndex struct {
	name    string
	columns []string
	unique  bool
	usedBy  map[string]bool
}

// loadTableIndexes 收集物理表的显式索引与主键，供 query.index_columns 校验。
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
			if len(cols) == 0 {
				continue
			}
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

func stringGroups(v any) [][]string {
	list := asList(v)
	out := make([][]string, 0, len(list))
	for _, raw := range list {
		if g := stringList(raw); len(g) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// checkQueryIndexColumns 校验：
//   - 每个 query 必须写 index_columns，且只对应一张表；
//   - 每组列必须是主键前缀，或某条显式索引的最左前缀；
//   - 匹配的显式索引必须在 used_by 中列出该查询；
//   - 反向：索引 used_by 的查询必须有对应的 index_columns 前缀组。
func (c *Checker) checkQueryIndexColumns(tableIDs map[string]string, cols map[string]map[string]bool) {
	for qid, groups := range c.queryIndexCols {
		tables := c.queryTables[qid]
		if len(tables) == 0 {
			continue // 表引用缺失由 query-table-shape 报出
		}
		if len(tables) > 1 {
			if len(groups) > 0 {
				c.rulef("query-index-columns", "query %s: multi-table query must not declare index_columns", qid)
			}
			continue
		}
		if len(groups) == 0 {
			c.rulef("query-index-columns", "query %s: must declare index_columns", qid)
			continue
		}
		var tableID string
		for t := range tables {
			tableID = t
		}
		if _, ok := tableIDs[tableID]; !ok {
			c.rulef("query-index-columns", "query %s: unknown table %s", qid, tableID)
			continue
		}
		for _, group := range groups {
			if len(group) == 0 {
				c.rulef("query-index-columns", "query %s: empty index_columns group", qid)
				continue
			}
			for _, col := range group {
				if set := cols[tableID]; set != nil && !set[col] {
					c.rulef("query-index-columns", "query %s: index_columns references unknown column %s.%s", qid, tableID, col)
				}
			}
			if hasPrefix(group, c.tablePrimaryKey[tableID]) {
				continue
			}
			matched := false
			for _, idx := range c.tableIndexes[tableID] {
				if !hasPrefix(group, idx.columns) {
					continue
				}
				matched = true
				if !idx.usedBy[qid] {
					c.rulef("query-index-used-by", "query %s: index %s starts with %v but does not list query in used_by", qid, idx.name, group)
				}
			}
			if !matched {
				c.rulef("query-index-columns", "query %s: no index on %s starts with %v", qid, tableID, group)
			}
		}
	}
	// 反向：索引 used_by 的查询必须有一个能匹配该索引的最左前缀组。
	for _, indexes := range c.tableIndexes {
		for _, idx := range indexes {
			for qid := range idx.usedBy {
				if t := c.queryTables[qid]; len(t) > 1 {
					c.rulef("query-index-columns", "query %s: multi-table query must not appear in index.used_by", qid)
					continue
				}
				groups := c.queryIndexCols[qid]
				if len(groups) == 0 {
					c.rulef("query-index-columns", "query %s: index %s uses it but index_columns is missing", qid, idx.name)
					continue
				}
				matched := false
				for _, group := range groups {
					if hasPrefix(group, idx.columns) {
						matched = true
						break
					}
				}
				if !matched {
					c.rulef("query-index-columns", "query %s: index_columns %v does not match index %s %v", qid, groups, idx.name, idx.columns)
				}
			}
		}
	}
}

func hasPrefix(group, columns []string) bool {
	if len(group) == 0 || len(group) > len(columns) {
		return false
	}
	for i := range group {
		if group[i] != columns[i] {
			return false
		}
	}
	return true
}
