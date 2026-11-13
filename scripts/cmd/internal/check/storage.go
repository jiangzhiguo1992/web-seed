// storage.go 校验 data/storage.yaml 的表级物理事实：
// engine / size_class / retention / partition 统一登记在 storage，而不是表契约。
package check

import (
	"path/filepath"
	"sort"
)

func (c *Checker) checkStorage(tableIDs map[string]string) {
	rel := "contracts/data/storage.yaml"
	doc, err := loadYAML(filepath.Join(c.root, rel))
	if err != nil {
		c.rulef("storage-registry", "%s: %v", rel, err)
		return
	}
	c.checkStorageDoc(doc, rel)
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
	c.checkStorageLargeIndexes(entries, rel)
}

// checkStorageDoc 校验 storage.yaml 顶层键与引擎开关类型，避免拼写错误静默通过。
func (c *Checker) checkStorageDoc(doc map[string]any, rel string) {
	allowed := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true, "do_not": true,
		"default_engine": true, "supported_engines": true, "engines": true,
		"deployment": true, "tables": true, "not_supported": true,
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

// checkStorageLargeIndexes 落实 large 表不得显式禁用 CONCURRENTLY。
func (c *Checker) checkStorageLargeIndexes(entries map[string]map[string]any, rel string) {
	for _, id := range sortedEntryKeys(entries) {
		sizeClass, _ := entries[id]["size_class"].(string)
		if sizeClass != "large" {
			continue
		}
		for i, raw := range asList(c.tableDocs[id]["indexes"]) {
			idx, ok := asMap(raw)
			if !ok {
				continue
			}
			if conc, _ := idx["concurrently"].(string); conc == "false" {
				c.rulef("large-index-concurrently", "%s: tables.%s size_class=large but indexes[%d] disables CONCURRENTLY", rel, id, i)
			}
		}
	}
}

func sortedAnyKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedEntryKeys(m map[string]map[string]any) []string {
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
