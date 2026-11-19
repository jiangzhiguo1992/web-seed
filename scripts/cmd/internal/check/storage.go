// storage.go 校验 data/storage.yaml 的存储拓扑：
// tables 按引擎分组；普通条目内聚 description/retention/partition，归档条目用 archive_of；
// caches 记录非权威副本及其缓存保留；引擎版本、连接池、部署拓扑等运行时配置不在契约内。
// gen_db 只消费 tables.<engine> 的权威表列表，不生成 retention/partition/caches 配置。
package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var storageNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// storageEngineFields 是各权威引擎允许的条目字段（storage-entry-fields）。
// 新引擎必须先在这里登记字段集，避免不同引擎的物理事实被混写。
var storageEngineFields = map[string]map[string]bool{
	"mysql":    storageFieldSet("table", "description", "retention", "unit", "pending", "soft_delete", "hard_delete", "ttl", "partition"),
	"postgres": storageFieldSet("table", "description", "retention", "unit", "pending", "soft_delete", "hard_delete", "ttl", "partition"),
	"mongodb":  storageFieldSet("table", "description", "retention", "unit", "pending", "soft_delete", "hard_delete", "ttl"),
}

// storageArchiveFields 是归档条目的字段集：archive_of 指向原表，table 是归档物理表名。
var storageArchiveFields = storageFieldSet("archive_of", "table", "retention", "unit", "pending", "soft_delete", "hard_delete", "ttl", "age_by", "after_unit", "after")

// storageCacheFields 是非权威副本条目的字段集。
var storageCacheFields = storageFieldSet("cache_of", "key", "retention", "unit", "cache_ttl")

// storageEntry 是一张权威表在 storage.yaml 中的登记。
type storageEntry struct {
	table     string
	engine    string
	retention *storageRetention
}

// storageRetention 是扁平化 retention 的解析结果；ttl 为 0 表示未声明。
type storageRetention struct {
	unit string
	ttl  int
}

// storageArchiveEntry 是一条归档条目：把 archive_of 的冷数据封存到 table。
type storageArchiveEntry struct {
	engine string
	name   string
	source string
	ageBy  string
	unit   string
	after  int
	where  string
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

	entries, archives := c.checkStorageTables(rawTables, tableIDs, rel)
	c.checkStorageArchives(archives, entries, tableIDs)
	c.checkStorageCaches(doc, entries, rel)
}

// checkStorageTables 校验 tables.<engine> 的条目形状、覆盖与表级物理事实。
func (c *Checker) checkStorageTables(rawTables map[string]any, tableIDs map[string]string, rel string) (map[string]*storageEntry, []storageArchiveEntry) {
	registered := map[string]*storageEntry{}
	var archives []storageArchiveEntry
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
			if archiveOf, isArchive := entry["archive_of"]; isArchive {
				if a := c.parseStorageArchive(engine, archiveOf, entry, where); a != nil {
					archives = append(archives, *a)
				}
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
			desc, _ := entry["description"].(string)
			if desc == "" {
				c.rulef("storage-entry-description", "%s: description is required", where)
			} else if summary, _ := c.tableDocs[id]["summary"].(string); desc != summary {
				c.rulef("storage-entry-description", "%s: description %q must equal summary %q", where, desc, summary)
			}
			e.retention = c.parseStorageRetention(entry, where, false)
			if rawPartition, ok := entry["partition"]; ok {
				c.checkStoragePartition(id, rawPartition, where)
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
	return registered, archives
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
			id, _ := entry["cache_of"].(string)
			if id == "" {
				c.rulef("storage-registry", "%s: cache_of is required", where)
				continue
			}
			authority, ok := entries[id]
			if !ok {
				c.rulef("storage-registry", "%s: %s is not a registered authority table", where, id)
				continue
			}
			rawKey, ok := entry["key"]
			if !ok {
				c.rulef("storage-cache-key", "%s: key is required", where)
				continue
			}
			keyList, ok := asList2(rawKey)
			if !ok {
				c.rulef("storage-cache-key", "%s: key must be a list", where)
				continue
			}
			keyRefs := stringList(keyList)
			if len(keyRefs) == 0 {
				c.rulef("storage-cache-key", "%s: key must not be empty", where)
				continue
			}
			validKey := true
			seenRefs := map[string]bool{}
			cols := make([]string, 0, len(keyRefs))
			for _, ref := range keyRefs {
				col, ok := cacheKeyColumn(ref, id)
				if !ok {
					c.rulef("storage-cache-key", "%s: key %s is not a column of %s", where, ref, id)
					validKey = false
					continue
				}
				if seenRefs[col] {
					c.rulef("storage-cache-key", "%s: key repeats %s", where, ref)
					validKey = false
					continue
				}
				seenRefs[col] = true
				if !c.columnExists(id, col) {
					c.rulef("storage-cache-key", "%s: key %s is not a column of %s", where, ref, id)
					validKey = false
					continue
				}
				cols = append(cols, col)
			}
			keyID := id + "\x00" + strings.Join(cols, ",")
			if seen[keyID] {
				c.rulef("storage-cache-key", "%s: duplicates (table,key) %s", where, id)
				continue
			}
			seen[keyID] = true
			if !validKey {
				continue
			}
			retention := c.parseStorageRetention(entry, where, true)
			if retention == nil || authority.retention == nil || authority.retention.ttl < 1 {
				continue
			}
			if storageUnitMinutes(retention.unit, retention.ttl) > storageUnitMinutes(authority.retention.unit, authority.retention.ttl) {
				c.rulef("storage-retention", "%s: cache_ttl must not exceed %s ttl", where, id)
			}
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

// parseStorageRetention 解析扁平 retention：retention 是开关，unit 与时长是同级字段。
func (c *Checker) parseStorageRetention(entry map[string]any, where string, cache bool) *storageRetention {
	raw, ok := entry["retention"]
	if !ok {
		if cache {
			c.rulef("storage-retention", "%s: retention is required for cache entries", where)
		}
		c.rejectRetentionFields(entry, where, cache)
		return nil
	}
	enabled, ok := raw.(bool)
	if !ok {
		c.rulef("storage-retention", "%s: retention must be a boolean", where)
		return nil
	}
	if !enabled {
		if cache {
			c.rulef("storage-retention", "%s: retention must be true for cache entries", where)
		}
		c.rejectRetentionFields(entry, where, cache)
		return nil
	}
	unit := c.retentionUnit(entry, where)
	if cache {
		rawTTL, ok := entry["cache_ttl"]
		if !ok {
			c.rulef("storage-retention", "%s: cache_ttl is required when retention is true", where)
			return nil
		}
		ttl, ok := toInt(rawTTL)
		if !ok || ttl < 1 {
			c.rulef("storage-retention", "%s: cache_ttl must be an integer >= 1", where)
			return nil
		}
		return &storageRetention{unit: unit, ttl: ttl}
	}
	hasDuration := false
	for _, key := range []string{"pending", "soft_delete", "hard_delete", "ttl"} {
		rawTime, ok := entry[key]
		if !ok {
			continue
		}
		hasDuration = true
		if n, ok := toInt(rawTime); !ok || n < 1 {
			c.rulef("storage-retention", "%s: %s must be an integer >= 1", where, key)
		}
	}
	if !hasDuration {
		c.rulef("storage-retention", "%s: retention requires at least one of pending/soft_delete/hard_delete/ttl", where)
	}
	ttl, _ := toInt(entry["ttl"])
	return &storageRetention{unit: unit, ttl: ttl}
}

// rejectRetentionFields 拒绝 retention 未开启时出现的保留字段。
func (c *Checker) rejectRetentionFields(entry map[string]any, where string, cache bool) {
	keys := []string{"unit", "pending", "soft_delete", "hard_delete", "ttl"}
	if cache {
		keys = []string{"unit", "cache_ttl"}
	}
	for _, key := range keys {
		if _, ok := entry[key]; ok {
			c.rulef("storage-retention", "%s: %s is only allowed when retention is true", where, key)
		}
	}
}

// retentionUnit 校验并返回 unit；缺失或非法时返回空串。
func (c *Checker) retentionUnit(entry map[string]any, where string) string {
	rawUnit, ok := entry["unit"]
	if !ok {
		c.rulef("storage-retention", "%s: unit is required when retention is true", where)
		return ""
	}
	unit, ok := rawUnit.(string)
	if !ok {
		c.rulef("storage-retention", "%s: unit must be a string", where)
		return ""
	}
	if unit != "minutes" && unit != "hours" && unit != "days" {
		c.rulef("storage-retention", "%s: unit must be minutes/hours/days, got %q", where, unit)
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

// parseStorageArchive 解析归档条目：archive_of 指向原表，table 是归档物理表名。
func (c *Checker) parseStorageArchive(engine string, rawArchiveOf any, entry map[string]any, where string) *storageArchiveEntry {
	archiveOf, _ := rawArchiveOf.(string)
	if archiveOf == "" {
		c.rulef("storage-archive", "%s: archive_of must be a non-empty table ID", where)
		return nil
	}
	for _, key := range sortedAnyKeys(entry) {
		if !storageArchiveFields[key] {
			c.rulef("storage-entry-fields", "%s: field %q is not allowed for archive entries", where, key)
		}
	}
	name, _ := entry["table"].(string)
	if name == "" {
		c.rulef("storage-archive", "%s: table is required", where)
		return nil
	}
	if !storageNameRe.MatchString(name) {
		c.rulef("storage-archive", "%s: archive table %q must be snake_case", where, name)
	}
	c.parseStorageRetention(entry, where, false)
	ageBy, _ := entry["age_by"].(string)
	if ageBy != "created_at" && ageBy != "updated_at" {
		c.rulef("storage-archive", "%s: age_by must be created_at or updated_at, got %q", where, ageBy)
	}
	afterUnit, _ := entry["after_unit"].(string)
	if afterUnit != "minutes" && afterUnit != "hours" && afterUnit != "days" {
		c.rulef("storage-archive", "%s: after_unit must be minutes/hours/days, got %q", where, afterUnit)
	}
	rawAfter, ok := entry["after"]
	if !ok {
		c.rulef("storage-archive", "%s: after is required", where)
		return nil
	}
	after, ok := toInt(rawAfter)
	if !ok || after < 1 {
		c.rulef("storage-archive", "%s: after must be an integer >= 1", where)
		return nil
	}
	return &storageArchiveEntry{engine: engine, name: name, source: archiveOf, ageBy: ageBy, unit: afterUnit, after: after, where: where}
}

// checkStorageArchives 校验归档条目：原表已登记、一表一条、表名唯一、age_by 存在。
func (c *Checker) checkStorageArchives(archives []storageArchiveEntry, registered map[string]*storageEntry, tableIDs map[string]string) {
	sources := map[string]string{}
	names := map[string]string{}
	physical := map[string]string{}
	for _, id := range sortedStringMapKeys(tableIDs) {
		if id == c.baseID {
			continue
		}
		if name := c.physicalTableName(id); name != "" {
			physical[name] = id
		}
	}
	for _, a := range archives {
		if _, ok := registered[a.source]; !ok {
			c.rulef("storage-archive", "%s: archive_of %s is not a registered authority table", a.where, a.source)
			continue
		}
		if prev, ok := sources[a.source]; ok {
			c.rulef("storage-archive", "%s: %s already has archive %s", a.where, a.source, prev)
			continue
		}
		sources[a.source] = a.name
		if prev, ok := names[a.name]; ok {
			c.rulef("storage-archive", "%s: archive table %s already declared by %s", a.where, a.name, prev)
			continue
		}
		names[a.name] = a.where
		if prev, ok := physical[a.name]; ok {
			c.rulef("storage-archive", "%s: archive table %s conflicts with physical table %s", a.where, a.name, prev)
		}
		if _, ok := c.tableColumns[a.source][a.ageBy]; !ok {
			c.rulef("storage-archive", "%s: age_by column %s not found on %s", a.where, a.ageBy, a.source)
		}
	}
}

// cacheKeyColumn 解析缓存 key：支持 account_id、col.account_id 与完整列引用。
func cacheKeyColumn(ref, table string) (string, bool) {
	if strings.HasPrefix(ref, "col.") {
		col := strings.TrimPrefix(ref, "col.")
		return col, col != ""
	}
	if t, col, ok := splitColumnRef(ref); ok {
		return col, t == table
	}
	return ref, ref != ""
}

// physicalTableName 取物理表名；缺失时回退到契约 ID 的 file stem。
func (c *Checker) physicalTableName(id string) string {
	if table, ok := asMap(c.tableDocs[id]["table"]); ok {
		if name, _ := table["name"].(string); name != "" {
			return name
		}
	}
	return strings.TrimPrefix(id, "data.tables.")
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
