// refs.go 校验跨文件表引用、used_by 与 index.yaml。
package check

import (
	"fmt"
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

func (c *Checker) checkCrossFileTableRefs(tableIDs map[string]string, cols map[string]map[string]bool) {
	// entities
	for _, f := range glob(c.root, "contracts/data/entities/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, f), err)
			continue
		}
		rel := relPath(c.root, f)
		if t, _ := doc["table"].(string); t != "" {
			if c.isBaseTableRef(t) {
				c.rulef("base-file", "%s: base contract is not a physical table", rel)
			} else if _, ok := tableIDs[t]; !ok {
				c.rulef("schema.ref", "%s: table %s not found", rel, t)
			}
		} else {
			c.rulef("schema.required", "%s: missing table", rel)
		}
		for i, raw := range asList(doc["relations"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			if via, _ := m["via"].(string); via != "" {
				if c.isBaseTableRef(via) {
					c.rulef("base-file", "%s: relations[%d]: base contract is not a physical table", rel, i)
					continue
				}
				checkColumnRef(c, via, tableIDs, cols, fmt.Sprintf("%s: relations[%d]", rel, i))
			}
		}
	}
	// queries
	queryIDs := map[string]bool{}
	for _, f := range glob(c.root, "contracts/data/queries/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, f), err)
			continue
		}
		rel := relPath(c.root, f)
		for i, raw := range asList(doc["queries"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			qid, _ := m["id"].(string)
			if qid != "" {
				queryIDs[qid] = true
			}
			ts := tableRefs(m)
			for _, t := range ts {
				if c.isBaseTableRef(t) {
					c.rulef("base-file", "%s: queries[%d] (%s): base contract is not a physical table", rel, i, qid)
				} else if _, ok := tableIDs[t]; !ok {
					c.rulef("schema.ref", "%s: queries[%d] (%s): table %s not found", rel, i, qid, t)
				}
			}
			for _, rc := range asList(m["return_columns"]) {
				col, ok := rc.(string)
				if !ok {
					c.rulef("schema.type", "%s: queries[%d] (%s): return_columns must be strings", rel, i, qid)
					continue
				}
				found := false
				for _, t := range ts {
					if cols[t] != nil && cols[t][col] {
						found = true
						break
					}
				}
				if !found {
					c.rulef("schema.ref", "%s: queries[%d] (%s): return column %s not found in %v", rel, i, qid, col, ts)
				}
			}
		}
	}
	// capabilities
	for _, f := range walkYAML(c.root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, f), err)
			continue
		}
		rel := relPath(c.root, f)
		p, _ := asMap(doc["persistence"])
		if p == nil {
			continue
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
					if !queryIDs[qid] {
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
				c.rulef("base-file", "%s: base contract is not a physical table", rel)
			} else if _, ok := tableIDs[t]; !ok {
				c.rulef("schema.ref", "%s: persistence references unknown table %s", rel, t)
			}
		}
	}
}

// checkPersistenceOwnership 校验 capability 的写表归属：
//   - read_only_tables 已移除，跨模块读必须通过 persistence.queries；
//   - 写表（table/tables）的 owner_module 必须等于 capability 的 module；
//   - 有写表但没有 module 时无法判断归属，直接报错。
func (c *Checker) checkPersistenceOwnership(doc, p map[string]any, rel string) {
	if _, ok := p["read_only_tables"]; ok {
		c.rulef("owner-write", "%s: read_only_tables is not supported; use persistence.queries for cross-module reads", rel)
	}
	writes := tableRefs(p)
	if len(writes) == 0 {
		return
	}
	module, _ := doc["module"].(string)
	if module == "" {
		c.rulef("owner-write", "%s: persistence requires module to verify write ownership", rel)
		return
	}
	for _, t := range writes {
		if owner := c.tableOwners[t]; owner != "" && owner != module {
			c.rulef("owner-write", "%s: module %q cannot write %s (owner_module %q)", rel, module, t, owner)
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
	inbound := map[string]map[string]map[string]bool{}
	for id := range tableIDs {
		if id == c.baseID {
			continue
		}
		inbound[id] = map[string]map[string]bool{
			"entities": {}, "queries": {}, "capabilities": {},
		}
	}
	// entities
	for _, f := range glob(c.root, "contracts/data/entities/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil || doc == nil {
			continue
		}
		id, _ := doc["id"].(string)
		if t, _ := doc["table"].(string); t != "" && inbound[t] != nil {
			inbound[t]["entities"][id] = true
		}
	}
	// queries：按查询项 ID 记直接入边，而不是查询文件 ID。
	for _, f := range glob(c.root, "contracts/data/queries/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil || doc == nil {
			continue
		}
		rel := relPath(c.root, f)
		for i, raw := range asList(doc["queries"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			qid, _ := m["id"].(string)
			if qid == "" {
				c.rulef("schema.required", "%s: queries[%d]: missing id", rel, i)
				continue
			}
			for _, t := range tableRefs(m) {
				if inbound[t] != nil {
					inbound[t]["queries"][qid] = true
				}
			}
		}
	}
	// capabilities
	for _, f := range walkYAML(c.root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil || doc == nil {
			continue
		}
		id, _ := doc["id"].(string)
		p, _ := asMap(doc["persistence"])
		if p == nil {
			continue
		}
		for _, t := range tableRefs(p) {
			if inbound[t] != nil {
				inbound[t]["capabilities"][id] = true
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
		u, _ := asMap(doc["used_by"])
		if u == nil {
			c.rulef("used-by-generated", "%s: missing used_by", rel)
			continue
		}
		for _, kind := range []string{"entities", "queries", "capabilities"} {
			got := stringSet(asList(u[kind]))
			want := inbound[id][kind]
			if !sameSet(got, want) {
				c.rulef("used-by-generated", "%s: used_by.%s mismatch: declared %v, actual %v", rel, kind, sortedKeys(got), sortedKeys(want))
			}
		}
	}
}

func (c *Checker) checkIndex(tableIDs map[string]string) {
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
	if got, ok := byPath["data/enums.yaml"]; !ok {
		c.rulef("schema.index", "%s: missing entry for data/enums.yaml", rel)
	} else if got != "data.enums" {
		c.rulef("schema.index", "%s: data/enums.yaml has id %s, expected data.enums", rel, got)
	}
}
