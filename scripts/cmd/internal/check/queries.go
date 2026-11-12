// queries.go 建立查询项索引，供 index.used_by 与跨文件引用校验使用。
package check

// loadQueryIndex 收集 query 项 ID 与它引用的表，避免每个检查重复扫描。
func (c *Checker) loadQueryIndex() {
	c.queryIDs = map[string]string{}
	c.queryTables = map[string]map[string]bool{}
	c.querySQL = map[string]string{}
	for _, f := range glob(c.root, "contracts/data/queries/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			continue
		}
		rel := relPath(c.root, f)
		for _, raw := range asList(doc["queries"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			qid, _ := m["id"].(string)
			if qid == "" {
				continue
			}
			c.queryIDs[qid] = rel
			c.querySQL[qid], _ = m["sql"].(string)
			tables := map[string]bool{}
			for _, t := range tableRefs(m) {
				tables[t] = true
			}
			c.queryTables[qid] = tables
		}
	}
}
