// storage.go 校验 data/storage.yaml 的表级物理事实：
// engine / size_class / retention / partition 统一登记在 storage，而不是表契约。
package check

import (
	"path/filepath"
	"sort"
)

func (c *Checker) checkStorage(tableIDs map[string]string) {
	rel := "contracts/data/storage.yaml"
	c.redundancyGroups = map[string]*redundancyGroup{}
	doc, err := loadYAML(filepath.Join(c.root, rel))
	if err != nil {
		c.rulef("storage-registry", "%s: %v", rel, err)
		return
	}
	c.checkStorageDoc(doc, rel)
	c.checkRedundancyGroups(doc, rel)
	if def, _ := doc["default_engine"].(string); def == "" || !c.storageEngineEnabled(doc, def) {
		c.rulef("storage-engine", "%s: default_engine %q is not supported/enabled", rel, def)
	}
	rawTables, ok := asMap(doc["tables"])
	if !ok {
		c.rulef("storage-registry", "%s: tables must be a map keyed by table contract id", rel)
		return
	}
	entries := map[string]map[string]any{}
	for _, id := range sortedAnyKeys(rawTables) {
		m, ok := asMap(rawTables[id])
		if !ok {
			c.rulef("storage-entry", "%s: tables.%s must be an object", rel, id)
			continue
		}
		if id == c.baseID {
			c.rulef("storage-registry", "%s: tables.%s is the base contract, not a physical table", rel, id)
			continue
		}
		if _, ok := tableIDs[id]; !ok {
			c.rulef("storage-registry", "%s: tables.%s does not match any data.tables contract", rel, id)
			continue
		}
		entries[id] = m
		c.checkStorageEntry(doc, id, m, rel)
	}
	for _, id := range sortedStringMapKeys(tableIDs) {
		if id == c.baseID {
			continue
		}
		if _, ok := entries[id]; !ok {
			c.rulef("storage-registry", "%s: physical table %s is not registered", rel, id)
		}
	}
}

// checkStorageDoc 校验 storage.yaml 顶层键与引擎开关类型，避免拼写错误静默通过。
func (c *Checker) checkStorageDoc(doc map[string]any, rel string) {
	allowed := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true, "do_not": true,
		"default_engine": true, "supported_engines": true, "engines": true,
		"deployment": true, "tables": true, "redundancy_groups": true, "not_supported": true,
	}
	for _, k := range sortedAnyKeys(doc) {
		if !allowed[k] {
			c.rulef("storage-doc", "%s: unknown top-level field %q", rel, k)
		}
	}
	if raw, ok := doc["default_engine"]; ok {
		if s, _ := raw.(string); s == "" {
			c.rulef("storage-doc", "%s: default_engine must be a non-empty string", rel)
		}
	}
	if raw, ok := doc["supported_engines"]; ok {
		list, ok := asList2(raw)
		if !ok {
			c.rulef("storage-doc", "%s: supported_engines must be a list", rel)
		}
		for i, v := range list {
			if s, _ := v.(string); s == "" {
				c.rulef("storage-doc", "%s: supported_engines[%d] must be a non-empty string", rel, i)
			}
		}
	}
	engines, ok := asMap(doc["engines"])
	if !ok {
		c.rulef("storage-doc", "%s: engines must be a map", rel)
		return
	}
	for _, name := range sortedAnyKeys(engines) {
		m, ok := asMap(engines[name])
		if !ok {
			c.rulef("storage-doc", "%s: engines.%s must be an object", rel, name)
			continue
		}
		if raw, ok := m["enabled"]; ok {
			if _, ok := raw.(bool); !ok {
				c.rulef("storage-doc", "%s: engines.%s.enabled must be a bool", rel, name)
			}
		}
	}
}

// checkRedundancyGroups 解析 storage.redundancy_groups：
// 组只定义 source/copies，表列只写组 ID；source 唯一、copy 至少一个，类型/可空必须一致。
func (c *Checker) checkRedundancyGroups(doc map[string]any, rel string) {
	if c.redundancyGroups == nil {
		c.redundancyGroups = map[string]*redundancyGroup{}
	}
	raw, ok := doc["redundancy_groups"]
	if !ok {
		return
	}
	groups, ok := asMap(raw)
	if !ok {
		c.rulef("redundant-group", "%s: redundancy_groups must be a map keyed by group id", rel)
		return
	}
	owners := map[string]string{}
	for _, id := range sortedAnyKeys(groups) {
		m, ok := asMap(groups[id])
		if !ok {
			c.rulef("redundant-group", "%s: redundancy_groups.%s must be an object", rel, id)
			continue
		}
		if !targetNamingPatterns["snake_case"].MatchString(id) {
			c.rulef("redundant-group", "%s: redundancy_groups.%s must be snake_case", rel, id)
		}
		allowed := map[string]bool{"source": true, "copies": true}
		for k := range m {
			if !allowed[k] {
				c.rulef("redundant-group", "%s: redundancy_groups.%s unknown field %q", rel, id, k)
			}
		}
		source, _ := m["source"].(string)
		srcTable, srcCol, srcOK := splitColumnRef(source)
		if source == "" || !srcOK || !c.columnExists(srcTable, srcCol) {
			c.rulef("redundant-ref", "%s: redundancy_groups.%s.source %q is not a table column", rel, id, source)
			continue
		}
		rawCopies, ok := m["copies"]
		if !ok {
			c.rulef("redundant-group", "%s: redundancy_groups.%s.copies must list at least one copy", rel, id)
			continue
		}
		list, ok := asList2(rawCopies)
		if !ok || len(list) == 0 {
			c.rulef("redundant-group", "%s: redundancy_groups.%s.copies must be a non-empty list", rel, id)
			continue
		}
		refs := []string{source}
		valid := true
		for _, rawCopy := range list {
			copyRef, ok := rawCopy.(string)
			if !ok || copyRef == "" {
				c.rulef("redundant-ref", "%s: redundancy_groups.%s.copies items must be non-empty column refs", rel, id)
				valid = false
				continue
			}
			refs = append(refs, copyRef)
		}
		if !valid {
			continue
		}
		seen := map[string]bool{}
		for _, ref := range refs {
			if seen[ref] {
				c.rulef("redundant-ref", "%s: redundancy_groups.%s repeats column %s", rel, id, ref)
				valid = false
			}
			seen[ref] = true
		}
		for _, ref := range refs {
			if prev, ok := owners[ref]; ok && prev != id {
				c.rulef("redundant-ref", "%s: column %s already belongs to redundancy group %s", rel, ref, prev)
				valid = false
			}
		}
		if !valid {
			continue
		}
		srcMeta := c.tableColumns[srcTable][srcCol]
		for _, copyRef := range refs[1:] {
			copyTable, copyCol, ok := splitColumnRef(copyRef)
			if !ok || !c.columnExists(copyTable, copyCol) {
				c.rulef("redundant-ref", "%s: redundancy_groups.%s copy %q is not a table column", rel, id, copyRef)
				continue
			}
			meta := c.tableColumns[copyTable][copyCol]
			if c.resolvedColumnType(meta.typ) != c.resolvedColumnType(srcMeta.typ) || meta.nullable != srcMeta.nullable {
				c.rulef("redundant-ref", "%s: redundancy_groups.%s copy %s type/nullable (%s, nullable=%v) must match source %s (%s, nullable=%v)", rel, id, copyRef, c.resolvedColumnType(meta.typ), meta.nullable, source, c.resolvedColumnType(srcMeta.typ), srcMeta.nullable)
			}
		}
		for _, ref := range refs {
			owners[ref] = id
		}
		c.redundancyGroups[id] = &redundancyGroup{source: source, copies: refs[1:]}
	}
}

// columnExists 判断列是否存在于已加载的物理表（含继承列）。
func (c *Checker) columnExists(table, col string) bool {
	_, ok := c.tableColumns[table][col]
	return ok
}

func sortedAnyKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStringMapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c *Checker) checkStorageEntry(storageDoc map[string]any, id string, m map[string]any, rel string) {
	allowed := map[string]bool{"engine": true, "size_class": true, "retention": true, "partition": true}
	for k := range m {
		if !allowed[k] {
			c.rulef("storage-entry", "%s: tables.%s unknown field %q", rel, id, k)
		}
	}
	if raw, ok := m["size_class"]; ok {
		s, _ := raw.(string)
		if s != "small" && s != "large" {
			c.rulef("storage-entry", "%s: tables.%s size_class must be small or large, got %v", rel, id, raw)
		}
	}
	if raw, ok := m["engine"]; ok {
		s, _ := raw.(string)
		if s == "" {
			c.rulef("storage-engine", "%s: tables.%s engine must not be empty", rel, id)
		} else if !c.storageEngineEnabled(storageDoc, s) {
			c.rulef("storage-engine", "%s: tables.%s engine %q is not supported/enabled", rel, id, s)
		}
	}
	c.checkStorageRetention(id, m, rel)
	c.checkStoragePartition(id, m, rel)
}

func (c *Checker) storageEngineEnabled(doc map[string]any, name string) bool {
	supported := stringSet(asList(doc["supported_engines"]))
	if !supported[name] {
		return false
	}
	engines, _ := asMap(doc["engines"])
	engine, _ := asMap(engines[name])
	if engine == nil {
		return false
	}
	if raw, ok := engine["enabled"]; ok {
		enabled, ok := raw.(bool)
		if !ok || !enabled {
			return false
		}
	}
	return true
}

func (c *Checker) checkStorageRetention(id string, m map[string]any, rel string) {
	raw, ok := m["retention"]
	if !ok {
		return
	}
	r, ok := asMap(raw)
	if !ok {
		c.rulef("storage-retention", "%s: tables.%s retention must be an object", rel, id)
		return
	}
	allowed := map[string]bool{"unit": true, "pending": true, "soft_delete": true, "hard_delete": true, "ttl": true}
	for k := range r {
		if !allowed[k] {
			c.rulef("storage-retention", "%s: tables.%s retention unknown field %q", rel, id, k)
		}
	}
	if raw, ok := r["unit"]; ok {
		unit, ok := raw.(string)
		if !ok {
			c.rulef("storage-retention", "%s: tables.%s retention.unit must be a string", rel, id)
		} else if unit != "" && unit != "minutes" && unit != "hours" && unit != "days" {
			c.rulef("storage-retention", "%s: tables.%s retention.unit must be minutes/hours/days, got %q", rel, id, unit)
		}
	}
	for _, key := range []string{"pending", "soft_delete", "hard_delete", "ttl"} {
		raw, ok := r[key]
		if !ok {
			continue
		}
		if n, ok := toInt(raw); !ok || n < 1 {
			c.rulef("storage-retention", "%s: tables.%s retention.%s must be an integer >= 1", rel, id, key)
		}
	}
}

func (c *Checker) checkStoragePartition(id string, m map[string]any, rel string) {
	raw, ok := m["partition"]
	if !ok {
		return
	}
	p, ok := asMap(raw)
	if !ok {
		c.rulef("storage-partition", "%s: tables.%s partition must be an object", rel, id)
		return
	}
	allowed := map[string]bool{"strategy": true, "columns": true, "interval": true}
	for k := range p {
		if !allowed[k] {
			c.rulef("storage-partition", "%s: tables.%s partition unknown field %q", rel, id, k)
		}
	}
	strategy, _ := p["strategy"].(string)
	switch strategy {
	case "range", "list", "hash":
	default:
		c.rulef("storage-partition", "%s: tables.%s partition.strategy must be range/list/hash", rel, id)
	}
	cols := asList(p["columns"])
	if len(cols) == 0 {
		c.rulef("storage-partition", "%s: tables.%s partition.columns must not be empty", rel, id)
	}
	for _, raw := range cols {
		col, ok := raw.(string)
		if !ok || col == "" {
			c.rulef("storage-partition", "%s: tables.%s partition.columns must be strings", rel, id)
			continue
		}
		if set := c.tableColumns[id]; set != nil {
			if _, ok := set[col]; !ok {
				c.rulef("storage-partition", "%s: tables.%s partition column %q not found", rel, id, col)
			}
		}
	}
	interval := ""
	if raw, ok := p["interval"]; ok {
		s, ok := raw.(string)
		if !ok {
			c.rulef("storage-partition", "%s: tables.%s partition.interval must be a string", rel, id)
		} else {
			interval = s
		}
	}
	if strategy == "range" && interval == "" {
		c.rulef("storage-partition", "%s: tables.%s partition.interval is required for range", rel, id)
	}
	if strategy != "range" && interval != "" {
		c.rulef("storage-partition", "%s: tables.%s partition.interval is only allowed for range", rel, id)
	}
}
