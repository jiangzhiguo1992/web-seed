// storage.go 校验 data/storage.yaml 的存储拓扑：
// tables 按 engine 分组；size_class/retention/partition 按表登记；caches 记录非权威副本。
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
		c.rulef("storage-registry", "%s: tables must be a map keyed by engine", rel)
		return
	}
	supported := stringSet(asList(doc["supported_engines"]))
	for _, raw := range asList(doc["supported_engines"]) {
		if engine, _ := raw.(string); engine != "" {
			if _, ok := rawTables[engine]; !ok {
				c.rulef("storage-engine", "%s: tables.%s is required for supported engine", rel, engine)
			}
		}
	}

	registered := map[string]string{}
	for _, engine := range sortedAnyKeys(rawTables) {
		if !supported[engine] {
			c.rulef("storage-engine", "%s: tables.%s is not a supported engine", rel, engine)
			continue
		}
		rawList, ok := rawTables[engine].([]any)
		if !ok {
			c.rulef("storage-registry", "%s: tables.%s must be a list of table IDs", rel, engine)
			continue
		}
		seen := map[string]bool{}
		for _, raw := range rawList {
			id, _ := raw.(string)
			if id == "" {
				c.rulef("storage-registry", "%s: tables.%s must contain non-empty table IDs", rel, engine)
				continue
			}
			if seen[id] {
				c.rulef("storage-registry", "%s: tables.%s duplicates %s", rel, engine, id)
				continue
			}
			seen[id] = true
			if id == c.baseID {
				c.rulef("storage-registry", "%s: %s is the base contract, not a physical table", rel, id)
				continue
			}
			if _, ok := tableIDs[id]; !ok {
				c.rulef("storage-registry", "%s: %s does not match any data.tables contract", rel, id)
				continue
			}
			if prev, ok := registered[id]; ok {
				c.rulef("storage-engine", "%s: table %s is registered under both %s and %s", rel, id, prev, engine)
				continue
			}
			registered[id] = engine
		}
	}
	for _, id := range sortedStringMapKeys(tableIDs) {
		if id == c.baseID {
			continue
		}
		if _, ok := registered[id]; !ok {
			c.rulef("storage-registry", "%s: physical table %s is not registered", rel, id)
		}
	}

	c.checkStorageCaches(doc, tableIDs, rel)
	c.checkStorageMetadata(doc, registered, cachedTables(doc), rel)
}

// cachedTables 收集 caches 中声明的表 ID，用于校验 retention.cache_ttl。
func cachedTables(doc map[string]any) map[string]bool {
	out := map[string]bool{}
	caches, ok := asMap(doc["caches"])
	if !ok {
		return out
	}
	for _, raw := range caches {
		for _, item := range asList(raw) {
			if id, _ := item.(string); id != "" {
				out[id] = true
			}
		}
	}
	return out
}

// checkStorageCaches 校验非权威副本（如 token 的 Redis 缓存）。
func (c *Checker) checkStorageCaches(doc map[string]any, tableIDs map[string]string, rel string) {
	raw, ok := doc["caches"]
	if !ok {
		return
	}
	caches, ok := asMap(raw)
	if !ok {
		c.rulef("storage-registry", "%s: caches must be a map keyed by cache engine", rel)
		return
	}
	for _, engine := range sortedAnyKeys(caches) {
		rawList, ok := caches[engine].([]any)
		if !ok {
			c.rulef("storage-registry", "%s: caches.%s must be a list of table IDs", rel, engine)
			continue
		}
		seen := map[string]bool{}
		for _, raw := range rawList {
			id, _ := raw.(string)
			if id == "" {
				c.rulef("storage-registry", "%s: caches.%s must contain non-empty table IDs", rel, engine)
				continue
			}
			if seen[id] {
				c.rulef("storage-registry", "%s: caches.%s duplicates %s", rel, engine, id)
				continue
			}
			seen[id] = true
			if id == c.baseID {
				c.rulef("storage-registry", "%s: %s is the base contract, not a physical table", rel, id)
				continue
			}
			if _, ok := tableIDs[id]; !ok {
				c.rulef("storage-registry", "%s: caches.%s references unknown table %s", rel, engine, id)
			}
		}
	}
}

// checkStorageMetadata 校验 size_class / retention / partition 的键必须是已登记表。
func (c *Checker) checkStorageMetadata(doc map[string]any, registered map[string]string, cached map[string]bool, rel string) {
	if raw, ok := doc["size_class"]; ok {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("storage-entry", "%s: size_class must be a map keyed by table ID", rel)
		} else {
			for _, id := range sortedAnyKeys(m) {
				if _, ok := registered[id]; !ok {
					c.rulef("storage-entry", "%s: size_class.%s is not a registered table", rel, id)
					continue
				}
				s, _ := m[id].(string)
				if s != "small" && s != "large" {
					c.rulef("storage-entry", "%s: size_class.%s must be small or large, got %v", rel, id, m[id])
				}
			}
		}
	}
	if raw, ok := doc["retention"]; ok {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("storage-retention", "%s: retention must be a map keyed by table ID", rel)
		} else {
			for _, id := range sortedAnyKeys(m) {
				if _, ok := registered[id]; !ok {
					c.rulef("storage-retention", "%s: retention.%s is not a registered table", rel, id)
					continue
				}
				c.checkStorageRetention(id, m[id], cached[id], rel)
			}
		}
	}
	if raw, ok := doc["partition"]; ok {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("storage-partition", "%s: partition must be a map keyed by table ID", rel)
		} else {
			for _, id := range sortedAnyKeys(m) {
				if _, ok := registered[id]; !ok {
					c.rulef("storage-partition", "%s: partition.%s is not a registered table", rel, id)
					continue
				}
				c.checkStoragePartition(id, m[id], rel)
			}
		}
	}
}

// checkStorageDoc 校验 storage.yaml 顶层键与 engine 允许列表，避免拼写错误静默通过。
func (c *Checker) checkStorageDoc(doc map[string]any, rel string) {
	allowed := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true, "do_not": true,
		"supported_engines": true, "tables": true, "caches": true,
		"size_class": true, "retention": true, "partition": true,
		"not_supported_topologies": true,
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
	raw, ok = doc["not_supported_topologies"]
	if !ok {
		return
	}
	list, ok = asList2(raw)
	if !ok {
		c.rulef("storage-doc", "%s: not_supported_topologies must be a list", rel)
		return
	}
	if len(list) == 0 {
		c.rulef("storage-doc", "%s: not_supported_topologies must not be empty when present", rel)
		return
	}
	for i, v := range list {
		if s, _ := v.(string); s == "" {
			c.rulef("storage-doc", "%s: not_supported_topologies[%d] must be a non-empty string", rel, i)
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

func (c *Checker) checkStorageRetention(id string, raw any, cached bool, rel string) {
	r, ok := asMap(raw)
	if !ok {
		c.rulef("storage-retention", "%s: retention.%s must be an object", rel, id)
		return
	}
	allowed := map[string]bool{"unit": true, "pending": true, "soft_delete": true, "hard_delete": true, "ttl": true, "cache_ttl": true}
	for k := range r {
		if !allowed[k] {
			c.rulef("storage-retention", "%s: retention.%s unknown field %q", rel, id, k)
		}
	}
	rawUnit, hasUnit := r["unit"]
	if !hasUnit {
		c.rulef("storage-retention", "%s: retention.%s.unit is required", rel, id)
	} else if unit, ok := rawUnit.(string); !ok {
		c.rulef("storage-retention", "%s: retention.%s.unit must be a string", rel, id)
	} else if unit != "minutes" && unit != "hours" && unit != "days" {
		c.rulef("storage-retention", "%s: retention.%s.unit must be minutes/hours/days, got %q", rel, id, unit)
	}
	hasDuration := false
	for _, key := range []string{"pending", "soft_delete", "hard_delete", "ttl", "cache_ttl"} {
		rawDuration, ok := r[key]
		if !ok {
			continue
		}
		hasDuration = true
		if n, ok := toInt(rawDuration); !ok || n < 1 {
			c.rulef("storage-retention", "%s: retention.%s.%s must be an integer >= 1", rel, id, key)
		}
	}
	if !hasDuration {
		c.rulef("storage-retention", "%s: retention.%s must declare at least one duration", rel, id)
	}
	if _, ok := r["cache_ttl"]; ok && !cached {
		c.rulef("storage-retention", "%s: retention.%s.cache_ttl requires the table in caches", rel, id)
	}
	if rawCache, ok := r["cache_ttl"]; ok {
		if cacheTTL, ok := toInt(rawCache); ok {
			if rawTTL, ok := r["ttl"]; ok {
				if ttl, ok := toInt(rawTTL); ok && cacheTTL > ttl {
					c.rulef("storage-retention", "%s: retention.%s.cache_ttl must not exceed ttl", rel, id)
				}
			}
		}
	}
}

func (c *Checker) checkStoragePartition(id string, raw any, rel string) {
	p, ok := asMap(raw)
	if !ok {
		c.rulef("storage-partition", "%s: partition.%s must be an object", rel, id)
		return
	}
	allowed := map[string]bool{"strategy": true, "columns": true, "interval": true}
	for k := range p {
		if !allowed[k] {
			c.rulef("storage-partition", "%s: partition.%s unknown field %q", rel, id, k)
		}
	}
	strategy, _ := p["strategy"].(string)
	switch strategy {
	case "range", "list", "hash":
	default:
		c.rulef("storage-partition", "%s: partition.%s.strategy must be range/list/hash", rel, id)
	}
	cols := asList(p["columns"])
	if len(cols) == 0 {
		c.rulef("storage-partition", "%s: partition.%s.columns must not be empty", rel, id)
	}
	for _, rawCol := range cols {
		col, ok := rawCol.(string)
		if !ok || col == "" {
			c.rulef("storage-partition", "%s: partition.%s.columns must be strings", rel, id)
			continue
		}
		if set := c.tableColumns[id]; set != nil {
			if _, ok := set[col]; !ok {
				c.rulef("storage-partition", "%s: partition.%s column %q not found", rel, id, col)
			}
		}
	}
	interval := ""
	if rawInterval, ok := p["interval"]; ok {
		s, ok := rawInterval.(string)
		if !ok {
			c.rulef("storage-partition", "%s: partition.%s.interval must be a string", rel, id)
		} else {
			interval = s
		}
	}
	if strategy == "range" && interval == "" {
		c.rulef("storage-partition", "%s: partition.%s.interval is required for range", rel, id)
	}
	if strategy != "range" && interval != "" {
		c.rulef("storage-partition", "%s: partition.%s.interval is only allowed for range", rel, id)
	}
}
