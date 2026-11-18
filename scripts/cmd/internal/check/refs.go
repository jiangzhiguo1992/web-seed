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

func (c *Checker) checkCapabilityWrites(tableIDs map[string]string) {
	c.declaredWrites = map[string]bool{}
	for _, f := range walkYAML(c.root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, f), err)
			continue
		}
		rel := relPath(c.root, f)
		id, _ := doc["id"].(string)
		if !strings.HasPrefix(id, "cap.services.") {
			if doc["persistence"] != nil || doc["writes"] != nil {
				c.rulef("owner-write", "%s: only cap.services may declare persistence/writes", rel)
			}
			continue
		}
		module, _ := doc["owner_module"].(string)
		for _, raw := range asList(doc["services"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			for _, rawStep := range asList(item["steps"]) {
				step, _ := asMap(rawStep)
				kind, _ := step["kind"].(string)
				var qids []string
				switch kind {
				case "query":
					qid, _ := step["query"].(string)
					if qid != "" {
						qids = append(qids, qid)
					}
				case "logic", "transition":
					for _, rawQ := range asList(step["queries"]) {
						qo, _ := asMap(rawQ)
						qid, _ := qo["query"].(string)
						if qid != "" {
							qids = append(qids, qid)
						}
					}
				}
				for _, qid := range qids {
					q := c.queries[qid]
					if q == nil {
						continue
					}
					if queryReturnsAffectedRows(q) && len(q.Writes) == 0 {
						c.rulef("service-owner-write", "%s.%s: query %s returns affected_rows without a write table", rel, name, qid)
					}
					for _, target := range q.Writes {
						c.declaredWrites[qid] = true
						if owner := c.tableOwners[target]; owner != "" && owner != module {
							c.rulef("service-owner-write", "%s.%s: module %q cannot write %s via %s (owner_module %q); cross-module writes must go through kind=service", rel, name, module, target, qid, owner)
						}
					}
				}
			}
		}

	}
	for _, qid := range c.queryOrder {
		q := c.queries[qid]
		if q != nil && (len(q.Writes) > 0 || queryReturnsAffectedRows(q)) && !c.declaredWrites[qid] {
			c.rulef("owner-write", "query %s has write tables or returns affected_rows but is not referenced by any service step", qid)
		}
	}
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
			raw, ok := table["queries"]
			if !ok {
				continue // 缺失由 schema.required 报出
			}
			gotQueries := stringSet(asList(raw))
			if !sameSet(gotQueries, tableQueries[id]) {
				c.rulef("table-queries-generated", "%s: table.queries mismatch: declared %v, actual %v", rel, sortedKeys(gotQueries), sortedKeys(tableQueries[id]))
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
			continue
		}
		// index 的 stability 是投影，必须与契约文件自身一致；文件未声明时跳过。
		if raw, present := m["stability"]; present {
			want, ok := raw.(string)
			if !ok || (want != "stable" && want != "draft" && want != "deprecated") {
				c.rulef("schema.index-stability", "%s: %s stability must be stable/draft/deprecated, got %v", rel, id, raw)
			} else if doc, err := loadYAML(filepath.Join(c.root, "contracts", p)); err == nil {
				if got, _ := doc["stability"].(string); got != "" && got != want {
					c.rulef("schema.index-stability", "%s: %s stability %q must match %s (%q)", rel, id, want, p, got)
				}
			}
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
	// capabilities/features/platform 也必须登记：新增契约不得游离在 index 之外。
	for _, file := range walkYAML(c.root, "contracts") {
		rel := relPath(c.root, file)
		if strings.HasPrefix(rel, "contracts/_meta/") || strings.HasPrefix(rel, "contracts/generated/") ||
			strings.HasPrefix(rel, "contracts/data/") || rel == "contracts/index.yaml" {
			continue
		}
		p := strings.TrimPrefix(rel, "contracts/")
		got, ok := byPath[p]
		if !ok {
			c.rulef("schema.index", "%s: missing entry for %s", rel, p)
			continue
		}
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		if id, _ := doc["id"].(string); id != "" && got != id {
			c.rulef("schema.index", "%s: path %s has id %s, expected %s", rel, p, got, id)
		}
	}
}
