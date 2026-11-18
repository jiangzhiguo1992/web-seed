// storage.go 校验 data/storage.yaml 的存储拓扑：
// tables 按引擎分组，每个条目内聚 size_class/retention/partition/archive；
// caches 记录非权威副本及其缓存保留；引擎版本、连接池、部署拓扑等运行时配置不在契约内。
package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// storageIdentifierRe 约束逻辑库名，避免把连接串或实例地址写进契约。
var storageIdentifierRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// storageEngineFields 是各权威引擎允许的条目字段（storage-entry-fields）。
// 新引擎必须先在这里登记字段集，避免不同引擎的物理事实被混写。
var storageEngineFields = map[string]map[string]bool{
	"mysql":    storageFieldSet("table", "size_class", "retention", "partition", "archive"),
	"postgres": storageFieldSet("table", "size_class", "retention", "partition", "archive"),
	"mongodb":  storageFieldSet("table", "size_class", "retention"),
}

// storageCacheFields 是非权威副本条目的字段集。
var storageCacheFields = storageFieldSet("table", "retention")

// storageEntry 是一张权威表在 storage.yaml 中的登记。
type storageEntry struct {
	table     string
	engine    string
	retention map[string]any
}

// storageUnitMinutes 把保留/归档时长归一化为分钟；未知单位返回 0。
func storageUnitMinutes(unit string, n int) int {
	switch unit {
	case "minutes":
		return n
	case "hours":
		return n * 60
	case "days":
		return n * 1440
	}
	return 0
}

func storageFieldSet(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}

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
	for _, engine := range sortedAnyKeys(rawTables) {
		if !supported[engine] {
			c.rulef("storage-engine", "%s: tables.%s is not a supported engine", rel, engine)
		}
	}
	for _, engine := range sortedStringSetKeys(supported) {
		if _, ok := rawTables[engine]; !ok {
			c.rulef("storage-engine", "%s: tables.%s is required for supported engine", rel, engine)
		}
	}

	entries := c.checkStorageTables(rawTables, tableIDs, supported, rel)
	c.checkStorageCaches(doc, entries, rel)
}

// checkStorageTables 校验 tables.<engine> 的条目形状、覆盖与表级物理事实。
func (c *Checker) checkStorageTables(rawTables map[string]any, tableIDs map[string]string, supported map[string]bool, rel string) map[string]*storageEntry {
	registered := map[string]*storageEntry{}
	for _, engine := range sortedAnyKeys(rawTables) {
		list, ok := asList2(rawTables[engine])
		if !ok {
			c.rulef("storage-registry", "%s: tables.%s must be a list of table entries", rel, engine)
			continue
		}
		fields := storageEngineFields[engine]
		if fields == nil {
			c.rulef("storage-entry-fields", "%s: tables.%s has no entry field spec", rel, engine)
			fields = storageFieldSet("table")
		}
		seen := map[string]bool{}
		for i, raw := range list {
			where := fmt.Sprintf("%s: tables.%s[%d]", rel, engine, i)
			entry, ok := asMap(raw)
			if !ok {
				c.rulef("storage-registry", "%s must be an object", where)
				continue
			}
			for _, key := range sortedAnyKeys(entry) {
				if !fields[key] {
					c.rulef("storage-entry-fields", "%s: field %q is not allowed for engine %s", where, key, engine)
				}
			}
			id, _ := entry["table"].(string)
			if id == "" {
				c.rulef("storage-registry", "%s: table is required", where)
				continue
			}
			if seen[id] {
				c.rulef("storage-registry", "%s: duplicates %s", where, id)
				continue
			}
			seen[id] = true
			if id == c.baseID {
				c.rulef("storage-registry", "%s: %s is the base contract, not a physical table", where, id)
				continue
			}
			if _, ok := tableIDs[id]; !ok {
				c.rulef("storage-registry", "%s: %s does not match any data.tables contract", where, id)
				continue
			}
			if prev, ok := registered[id]; ok {
				c.rulef("storage-engine", "%s: table %s is registered under both %s and %s", where, id, prev.engine, engine)
				continue
			}
			e := &storageEntry{table: id, engine: engine}
			if rawClass, ok := entry["size_class"]; ok {
				class, _ := rawClass.(string)
				if class != "small" && class != "large" {
					c.rulef("storage-entry", "%s: size_class must be small or large, got %v", where, rawClass)
				}
			}
			if rawRetention, ok := entry["retention"]; ok {
				if m, ok := asMap(rawRetention); ok {
					e.retention = m
				}
				c.checkStorageRetention(rawRetention, where)
			}
			if rawPartition, ok := entry["partition"]; ok {
				c.checkStoragePartition(id, rawPartition, where)
			}
			if rawArchive, ok := entry["archive"]; ok {
				c.checkStorageArchive(rawArchive, supported, where)
			}
			registered[id] = e
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
	return registered
}

// checkStorageCaches 校验非权威副本：条目形状、权威登记与 cache_ttl 上限。
func (c *Checker) checkStorageCaches(doc map[string]any, entries map[string]*storageEntry, rel string) {
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
		list, ok := asList2(caches[engine])
		if !ok {
			c.rulef("storage-registry", "%s: caches.%s must be a list of cache entries", rel, engine)
			continue
		}
		seen := map[string]bool{}
		for i, rawEntry := range list {
			where := fmt.Sprintf("%s: caches.%s[%d]", rel, engine, i)
			entry, ok := asMap(rawEntry)
			if !ok {
				c.rulef("storage-registry", "%s must be an object", where)
				continue
			}
			for _, key := range sortedAnyKeys(entry) {
				if !storageCacheFields[key] {
					c.rulef("storage-entry-fields", "%s: field %q is not allowed for cache entries", where, key)
				}
			}
			id, _ := entry["table"].(string)
			if id == "" {
				c.rulef("storage-registry", "%s: table is required", where)
				continue
			}
			if seen[id] {
				c.rulef("storage-registry", "%s: duplicates %s", where, id)
				continue
			}
			seen[id] = true
			authority, ok := entries[id]
			if !ok {
				c.rulef("storage-registry", "%s: %s is not a registered authority table", where, id)
				continue
			}
			rawRetention, ok := entry["retention"]
			if !ok {
				c.rulef("storage-retention", "%s: retention is required for cache entries", where)
				continue
			}
			c.checkStorageCacheRetention(id, authority, rawRetention, where)
		}
	}
}

// checkStorageDoc 校验 storage.yaml 顶层键与 engine 允许列表，避免拼写错误静默通过。
func (c *Checker) checkStorageDoc(doc map[string]any, rel string) {
	allowed := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true, "do_not": true,
		"supported_engines": true, "tables": true, "caches": true,
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

// checkStorageRetention 校验权威表条目的保留策略。
func (c *Checker) checkStorageRetention(raw any, where string) {
	r, ok := asMap(raw)
	if !ok {
		c.rulef("storage-retention", "%s: retention must be an object", where)
		return
	}
	allowed := storageFieldSet("unit", "pending", "soft_delete", "hard_delete", "ttl")
	for k := range r {
		if !allowed[k] {
			c.rulef("storage-retention", "%s: retention unknown field %q", where, k)
		}
	}
	c.retentionUnit(r, where)
	hasDuration := false
	for _, key := range []string{"pending", "soft_delete", "hard_delete", "ttl"} {
		rawDuration, ok := r[key]
		if !ok {
			continue
		}
		hasDuration = true
		if n, ok := toInt(rawDuration); !ok || n < 1 {
			c.rulef("storage-retention", "%s: retention.%s must be an integer >= 1", where, key)
		}
	}
	if !hasDuration {
		c.rulef("storage-retention", "%s: retention must declare at least one duration", where)
	}
}

// checkStorageCacheRetention 校验缓存条目的 cache_ttl，并与权威表 ttl 比较。
func (c *Checker) checkStorageCacheRetention(id string, authority *storageEntry, raw any, where string) {
	r, ok := asMap(raw)
	if !ok {
		c.rulef("storage-retention", "%s: retention must be an object", where)
		return
	}
	allowed := storageFieldSet("unit", "cache_ttl")
	for k := range r {
		if !allowed[k] {
			c.rulef("storage-retention", "%s: retention unknown field %q", where, k)
		}
	}
	unit := c.retentionUnit(r, where)
	rawTTL, hasTTL := r["cache_ttl"]
	if !hasTTL {
		c.rulef("storage-retention", "%s: retention.cache_ttl is required", where)
		return
	}
	cacheTTL, ok := toInt(rawTTL)
	if !ok || cacheTTL < 1 {
		c.rulef("storage-retention", "%s: retention.cache_ttl must be an integer >= 1", where)
		return
	}
	if authority == nil || authority.retention == nil {
		return
	}
	authorityUnit, _ := authority.retention["unit"].(string)
	rawTableTTL, ok := authority.retention["ttl"]
	if !ok {
		return
	}
	tableTTL, ok := toInt(rawTableTTL)
	if !ok || tableTTL < 1 || authorityUnit == "" {
		return
	}
	if storageUnitMinutes(unit, cacheTTL) > storageUnitMinutes(authorityUnit, tableTTL) {
		c.rulef("storage-retention", "%s: cache_ttl must not exceed %s ttl", where, id)
	}
}

// retentionUnit 校验并返回 unit；缺失或非法时返回空串。
func (c *Checker) retentionUnit(r map[string]any, where string) string {
	rawUnit, ok := r["unit"]
	if !ok {
		c.rulef("storage-retention", "%s: retention.unit is required", where)
		return ""
	}
	unit, ok := rawUnit.(string)
	if !ok {
		c.rulef("storage-retention", "%s: retention.unit must be a string", where)
		return ""
	}
	if unit != "minutes" && unit != "hours" && unit != "days" {
		c.rulef("storage-retention", "%s: retention.unit must be minutes/hours/days, got %q", where, unit)
		return ""
	}
	return unit
}

// checkStoragePartition 校验表条目的分区策略。
func (c *Checker) checkStoragePartition(id string, raw any, where string) {
	p, ok := asMap(raw)
	if !ok {
		c.rulef("storage-partition", "%s: partition must be an object", where)
		return
	}
	allowed := storageFieldSet("strategy", "columns", "interval")
	for k := range p {
		if !allowed[k] {
			c.rulef("storage-partition", "%s: partition unknown field %q", where, k)
		}
	}
	strategy, _ := p["strategy"].(string)
	switch strategy {
	case "range", "list", "hash":
	default:
		c.rulef("storage-partition", "%s: partition.strategy must be range/list/hash", where)
	}
	cols := asList(p["columns"])
	if len(cols) == 0 {
		c.rulef("storage-partition", "%s: partition.columns must not be empty", where)
	}
	for _, rawCol := range cols {
		col, ok := rawCol.(string)
		if !ok || col == "" {
			c.rulef("storage-partition", "%s: partition.columns must be strings", where)
			continue
		}
		if set := c.tableColumns[id]; set != nil {
			if _, ok := set[col]; !ok {
				c.rulef("storage-partition", "%s: partition column %q not found", where, col)
			}
		}
	}
	interval := ""
	if rawInterval, ok := p["interval"]; ok {
		s, ok := rawInterval.(string)
		if !ok {
			c.rulef("storage-partition", "%s: partition.interval must be a string", where)
		} else {
			interval = s
		}
	}
	if strategy == "range" && interval == "" {
		c.rulef("storage-partition", "%s: partition.interval is required for range", where)
	}
	if strategy != "range" && interval != "" {
		c.rulef("storage-partition", "%s: partition.interval is only allowed for range", where)
	}
}

// checkStorageArchive 校验归档目标与触发阈值。
func (c *Checker) checkStorageArchive(raw any, supported map[string]bool, where string) {
	a, ok := asMap(raw)
	if !ok {
		c.rulef("storage-archive", "%s: archive must be an object", where)
		return
	}
	allowed := storageFieldSet("engine", "database", "unit", "after")
	for k := range a {
		if !allowed[k] {
			c.rulef("storage-archive", "%s: archive unknown field %q", where, k)
		}
	}
	engine, _ := a["engine"].(string)
	if engine == "" {
		c.rulef("storage-archive", "%s: archive.engine is required", where)
	} else if !supported[engine] {
		c.rulef("storage-archive", "%s: archive.engine %q is not a supported engine", where, engine)
	}
	database, _ := a["database"].(string)
	if database == "" {
		c.rulef("storage-archive", "%s: archive.database is required", where)
	} else if !storageIdentifierRe.MatchString(database) {
		c.rulef("storage-archive", "%s: archive.database %q must be snake_case", where, database)
	}
	unit, _ := a["unit"].(string)
	if unit != "minutes" && unit != "hours" && unit != "days" {
		c.rulef("storage-archive", "%s: archive.unit must be minutes/hours/days, got %q", where, unit)
	}
	rawAfter, ok := a["after"]
	if !ok {
		c.rulef("storage-archive", "%s: archive.after is required", where)
		return
	}
	after, ok := toInt(rawAfter)
	if !ok || after < 1 {
		c.rulef("storage-archive", "%s: archive.after must be an integer >= 1", where)
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

func sortedStringSetKeys(m map[string]bool) []string {
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
