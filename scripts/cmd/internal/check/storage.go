// storage.go 校验 data/storage.yaml 的表级物理事实：
// engine / size_class / retention / partition 统一登记在 storage，而不是表契约。
// 引擎版本、连接池、部署拓扑等运行时配置不在契约内。
package check

import (
	"path/filepath"
	"sort"
	"strings"
)

func (c *Checker) checkStorage(tableIDs map[string]string) {
	rel := "contracts/data/storage.yaml"
	doc, err := loadYAML(filepath.Join(c.root, rel))
	if err != nil {
		c.rulef("storage-registry", "%s: %v", rel, err)
		return
	}
	c.checkStorageDoc(doc, rel)
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

// checkStorageDoc 校验 storage.yaml 顶层键与 engine 允许列表，避免拼写错误静默通过。
func (c *Checker) checkStorageDoc(doc map[string]any, rel string) {
	allowed := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true, "do_not": true,
		"supported_engines": true, "tables": true, "not_supported": true,
	}
	for _, k := range sortedAnyKeys(doc) {
		if !allowed[k] {
			c.rulef("storage-doc", "%s: unknown top-level field %q", rel, k)
		}
	}
	if id, ok := doc["id"].(string); !ok {
		c.rulef("schema.required", "%s: missing required field \"id\"", rel)
	} else if id != "data.storage" {
		c.rulef("schema.const", "%s: id must be data.storage, got %q", rel, id)
	}
	if _, ok := doc["summary"]; !ok {
		c.rulef("schema.required", "%s: missing required field \"summary\"", rel)
	} else if summary, _ := doc["summary"].(string); strings.TrimSpace(summary) == "" {
		c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
	}
	if _, ok := doc["do_not"]; !ok {
		c.rulef("schema.required", "%s: missing required field \"do_not\"", rel)
	}
	c.checkStability(doc, rel)
	raw, ok := doc["supported_engines"]
	if !ok {
		c.rulef("storage-doc", "%s: supported_engines is required", rel)
		return
	}
	list, ok := asList2(raw)
	if !ok {
		c.rulef("storage-doc", "%s: supported_engines must be a list", rel)
		return
	}
	if len(list) == 0 {
		c.rulef("storage-doc", "%s: supported_engines must not be empty", rel)
	}
	for i, v := range list {
		if s, _ := v.(string); s == "" {
			c.rulef("storage-doc", "%s: supported_engines[%d] must be a non-empty string", rel, i)
		}
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
	engine, ok := m["engine"]
	if !ok {
		c.rulef("storage-entry", "%s: tables.%s engine is required", rel, id)
	} else if s, _ := engine.(string); s == "" {
		c.rulef("storage-engine", "%s: tables.%s engine must not be empty", rel, id)
	} else if !stringSet(asList(storageDoc["supported_engines"]))[s] {
		c.rulef("storage-engine", "%s: tables.%s engine %q is not supported", rel, id, s)
	}
	c.checkStorageRetention(id, m, rel)
	c.checkStoragePartition(id, m, rel)
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
	raw, hasUnit := r["unit"]
	if !hasUnit {
		c.rulef("storage-retention", "%s: tables.%s retention.unit is required", rel, id)
	} else if unit, ok := raw.(string); !ok {
		c.rulef("storage-retention", "%s: tables.%s retention.unit must be a string", rel, id)
	} else if unit != "minutes" && unit != "hours" && unit != "days" {
		c.rulef("storage-retention", "%s: tables.%s retention.unit must be minutes/hours/days, got %q", rel, id, unit)
	}
	hasDuration := false
	for _, key := range []string{"pending", "soft_delete", "hard_delete", "ttl"} {
		raw, ok := r[key]
		if !ok {
			continue
		}
		hasDuration = true
		if n, ok := toInt(raw); !ok || n < 1 {
			c.rulef("storage-retention", "%s: tables.%s retention.%s must be an integer >= 1", rel, id, key)
		}
	}
	if !hasDuration {
		c.rulef("storage-retention", "%s: tables.%s retention must declare at least one duration", rel, id)
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
