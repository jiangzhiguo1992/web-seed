// storage.go 校验 data/storage.yaml 的存储拓扑：
// tables 按引擎分组；普通条目内聚 description/retention/ttl，归档条目用 archive_of；
// caches 记录非权威副本及其缓存保留；引擎版本、连接池、部署拓扑等运行时配置不在契约内。
// gen_db 只消费 tables.<engine> 的权威表列表，不生成 retention/caches 配置。
package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

// projectName 读取 contracts/project.yaml 的项目标识；缺失时返回空串，
// 供 storage cache prefix 的命名空间校验使用。
func (c *Checker) projectName() string {
	if c.root == "" {
		return ""
	}
	doc, err := loadYAML(filepath.Join(c.root, "contracts/project.yaml"))
	if err != nil {
		return ""
	}
	project, _ := asMap(doc["project"])
	name, _ := project["name"].(string)
	return name
}

var storageNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// storageEntryFields 是普通表条目的字段集（storage-entry-fields）。
// 当前各引擎没有字段差异；出现差异时再按引擎拆分。
var storageEntryFields = storageFieldSet("table", "description", "retention", "unit", "ttl")

// storageArchiveFields 是归档条目的字段集：archive_of 指向原表，promotable 决定能否回迁，
// on_conflict 是回迁冲突策略，table 是归档物理表名。
var storageArchiveFields = storageFieldSet("archive_of", "description", "promotable", "on_conflict", "table", "retention", "unit", "ttl", "age_by", "after_unit", "after")

// storageCacheFields 是非权威副本条目的字段集。
var storageCacheFields = storageFieldSet("cache_of", "key", "prefix", "retention", "unit", "cache_ttl")

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

// storageArchiveEntry 是一条归档条目：把 archive_of 的冷数据封存到 table；
// promotable 表示归档数据能否回迁热表。
type storageArchiveEntry struct {
	engine     string
	name       string
	source     string
	promotable bool
	onConflict string
	ageBy      string
	unit       string
	after      int
	where      string
}

// storageCacheEntry 是一条逻辑缓存：同表同 key 跨引擎视为同一逻辑缓存。
type storageCacheEntry struct {
	table string
	key   []string
}

// storageCacheRef 由 cache_of 与 key 派生缓存引用：data.tables.<t>.cache.<k>[+<k>]。
func storageCacheRef(table string, key []string) string {
	return table + ".cache." + strings.Join(key, "+")
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

func (c *Checker) checkStorage(tableIDs map[string]string, schema *Schema) {
	rel := "contracts/data/storage.yaml"
	if schema != nil && schema.Targets.Dir != "" && schema.Targets.Glob != "" {
		rel = filepath.Join(schema.Targets.Dir, schema.Targets.Glob)
	}
	c.cacheEntries = map[string]storageCacheEntry{}
	c.archiveEntries = map[string]storageArchiveEntry{}
	doc, err := loadYAML(filepath.Join(c.root, rel))
	if err != nil {
		c.rulef("storage-registry", "%s: %v", rel, err)
		return
	}
	if schema != nil {
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkStorageFieldSubset(schema)
	}
	c.checkStability(doc, rel)

	rawTables, ok := asMap(doc["tables"])
	if !ok {
		c.rulef("storage-registry", "%s: tables must be a map keyed by engine", rel)
		return
	}
	supported := stringSet(asList(doc["supported_engines"]))
	supportedCache := stringSet(asList(doc["supported_cache_engines"]))
	notSupported := stringSet(asList(doc["not_supported_topologies"]))
	for _, name := range sortedStringSetKeys(supported) {
		if notSupported[name] {
			c.rulef("storage-engine", "%s: %s cannot be both supported and not_supported", rel, name)
		}
	}
	for _, name := range sortedStringSetKeys(supportedCache) {
		if notSupported[name] {
			c.rulef("storage-engine", "%s: %s cannot be both supported and not_supported", rel, name)
		}
	}
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
	if rawCaches, ok := asMap(doc["caches"]); ok {
		for _, engine := range sortedAnyKeys(rawCaches) {
			if !supportedCache[engine] {
				c.rulef("storage-engine", "%s: caches.%s is not a supported cache engine", rel, engine)
			}
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
				if !storageEntryFields[key] {
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
		seenPrefix := map[string]bool{}
		for i, rawEntry := range list {
			where := fmt.Sprintf("%s: caches.%s[%d]", rel, engine, i)
			entry, ok := asMap(rawEntry)
			if !ok {
				c.rulef("storage-registry", "%s must be an object", where)
				continue
			}
			for _, key := range sortedAnyKeys(entry) {
				if !storageCacheFields[key] {
					c.rulef("storage-cache-fields", "%s: field %q is not allowed for cache entries", where, key)
				}
			}
			prefix, _ := entry["prefix"].(string)
			if prefix == "" {
				c.rulef("storage-cache-prefix", "%s: prefix is required", where)
			} else if !match(`^[a-z][a-z0-9_:-]*$`, prefix) {
				c.rulef("storage-cache-prefix", "%s: prefix %q must match ^[a-z][a-z0-9_:-]*$", where, prefix)
			} else if seenPrefix[prefix] {
				c.rulef("storage-cache-prefix", "%s: prefix %q duplicates within engine %s", where, prefix, engine)
			} else {
				seenPrefix[prefix] = true
				if project := c.projectName(); project != "" && !strings.HasPrefix(prefix, project+":") {
					c.rulef("storage-cache-prefix", "%s: prefix %q must start with %q", where, prefix, project+":")
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
			ref := storageCacheRef(id, cols)
			if c.cacheEntries == nil {
				c.cacheEntries = map[string]storageCacheEntry{}
			}
			if _, ok := c.cacheEntries[ref]; !ok {
				c.cacheEntries[ref] = storageCacheEntry{table: id, key: cols}
			}
			retention := c.parseStorageRetention(entry, where, true)
			if retention == nil || authority.retention == nil || authority.retention.ttl < 1 {
				continue
			}
			if storageUnitMinutes(retention.unit, retention.ttl) > storageUnitMinutes(authority.retention.unit, authority.retention.ttl) {
				c.rulef("storage-ttl-bound", "%s: cache_ttl must not exceed %s ttl", where, id)
			}
		}
	}
}

// checkStorageFieldSubset 防止 Go 的精确字段集与模板声明的条目键集漂移。
func (c *Checker) checkStorageFieldSubset(schema *Schema) {
	tables, ok := contractspec.FindField(schema.Fields, "tables")
	if !ok {
		c.rulef("schema.types", "data.storage: fields.tables is missing")
		return
	}
	tableKeys := fieldNameSet(tables.ItemFields)
	for name := range storageEntryFields {
		if !tableKeys[name] {
			c.rulef("schema.types", "data.storage: entry field %q is missing from _meta/data.storage.yaml", name)
		}
	}
	for name := range storageArchiveFields {
		if !tableKeys[name] {
			c.rulef("schema.types", "data.storage: archive field %q is missing from _meta/data.storage.yaml", name)
		}
	}
	for name := range tableKeys {
		if !storageEntryFields[name] && !storageArchiveFields[name] {
			c.rulef("schema.types", "data.storage: _meta/data.storage.yaml declares unknown table entry field %q", name)
		}
	}
	caches, ok := contractspec.FindField(schema.Fields, "caches")
	if !ok {
		return
	}
	cacheKeys := fieldNameSet(caches.ItemFields)
	for name := range storageCacheFields {
		if !cacheKeys[name] {
			c.rulef("schema.types", "data.storage: cache field %q is missing from _meta/data.storage.yaml", name)
		}
	}
	for name := range cacheKeys {
		if !storageCacheFields[name] {
			c.rulef("schema.types", "data.storage: _meta/data.storage.yaml declares unknown cache field %q", name)
		}
	}
}

func fieldNameSet(fields []Field) map[string]bool {
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		out[f.Name] = true
	}
	return out
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
		c.rejectRetentionFields(entry, where, cache)
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
			c.rulef("storage-duration", "%s: cache_ttl is required when retention is true", where)
			return nil
		}
		ttl, ok := toInt(rawTTL)
		if !ok || ttl < 1 {
			c.rulef("storage-duration", "%s: cache_ttl must be an integer >= 1", where)
			return nil
		}
		return &storageRetention{unit: unit, ttl: ttl}
	}
	rawTTL, ok := entry["ttl"]
	if !ok {
		c.rulef("storage-duration", "%s: retention requires ttl", where)
		return &storageRetention{unit: unit}
	}
	ttl, ok := toInt(rawTTL)
	if !ok || ttl < 1 {
		c.rulef("storage-duration", "%s: ttl must be an integer >= 1", where)
	}
	return &storageRetention{unit: unit, ttl: ttl}
}

// rejectRetentionFields 拒绝 retention 未开启时出现的保留字段。
func (c *Checker) rejectRetentionFields(entry map[string]any, where string, cache bool) {
	keys := []string{"unit", "ttl"}
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
		c.rulef("storage-duration", "%s: unit is required when retention is true", where)
		return ""
	}
	unit, ok := rawUnit.(string)
	if !ok {
		c.rulef("storage-duration", "%s: unit must be a string", where)
		return ""
	}
	if unit != "minutes" && unit != "hours" && unit != "days" {
		c.rulef("storage-duration", "%s: unit must be minutes/hours/days, got %q", where, unit)
		return ""
	}
	return unit
}

// parseStorageArchive 解析归档条目：archive_of 指向原表，table 是归档物理表名。
func (c *Checker) parseStorageArchive(engine string, rawArchiveOf any, entry map[string]any, where string) *storageArchiveEntry {
	archiveOf, _ := rawArchiveOf.(string)
	if archiveOf == "" {
		c.rulef("storage-archive-source", "%s: archive_of must be a non-empty table ID", where)
		return nil
	}
	if desc, _ := entry["description"].(string); strings.TrimSpace(desc) == "" {
		c.rulef("storage-entry-description", "%s: description is required", where)
	}
	for _, key := range sortedAnyKeys(entry) {
		if !storageArchiveFields[key] {
			c.rulef("storage-archive-fields", "%s: field %q is not allowed for archive entries", where, key)
		}
	}
	promotable := false
	if rawPromotable, ok := entry["promotable"]; !ok {
		c.rulef("storage-archive-source", "%s: promotable is required", where)
	} else if p, ok := rawPromotable.(bool); !ok {
		c.rulef("storage-archive-source", "%s: promotable must be a boolean", where)
	} else {
		promotable = p
	}
	onConflict := ""
	if raw, ok := entry["on_conflict"]; ok {
		switch v, _ := raw.(string); v {
		case "skip", "overwrite", "error":
			onConflict = v
		default:
			c.rulef("storage-archive-source", "%s: on_conflict must be skip/overwrite/error", where)
		}
	}
	if promotable {
		if _, ok := entry["on_conflict"]; !ok {
			c.rulef("storage-archive-source", "%s: promotable archive requires on_conflict", where)
		}
	} else if _, ok := entry["on_conflict"]; ok {
		c.rulef("storage-archive-source", "%s: on_conflict is only allowed when promotable is true", where)
	}
	name, _ := entry["table"].(string)
	if name == "" {
		c.rulef("storage-archive-source", "%s: table is required", where)
		return nil
	}
	if !storageNameRe.MatchString(name) {
		c.rulef("storage-archive-source", "%s: archive table %q must be snake_case", where, name)
	}
	c.parseStorageRetention(entry, where, false)
	ageBy, _ := entry["age_by"].(string)
	if ageBy != "created_at" && ageBy != "updated_at" {
		c.rulef("storage-archive-source", "%s: age_by must be created_at or updated_at, got %q", where, ageBy)
	}
	afterUnit, _ := entry["after_unit"].(string)
	if afterUnit != "minutes" && afterUnit != "hours" && afterUnit != "days" {
		c.rulef("storage-archive-source", "%s: after_unit must be minutes/hours/days, got %q", where, afterUnit)
	}
	rawAfter, ok := entry["after"]
	if !ok {
		c.rulef("storage-archive-source", "%s: after is required", where)
		return nil
	}
	after, ok := toInt(rawAfter)
	if !ok || after < 1 {
		c.rulef("storage-archive-source", "%s: after must be an integer >= 1", where)
		return nil
	}
	return &storageArchiveEntry{engine: engine, name: name, source: archiveOf, promotable: promotable, onConflict: onConflict, ageBy: ageBy, unit: afterUnit, after: after, where: where}
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
			c.rulef("storage-archive-ref", "%s: archive_of %s is not a registered authority table", a.where, a.source)
			continue
		}
		if prev, ok := sources[a.source]; ok {
			c.rulef("storage-archive-ref", "%s: %s already has archive %s", a.where, a.source, prev)
			continue
		}
		sources[a.source] = a.name
		if prev, ok := names[a.name]; ok {
			c.rulef("storage-archive-ref", "%s: archive table %s already declared by %s", a.where, a.name, prev)
			continue
		}
		names[a.name] = a.where
		if prev, ok := physical[a.name]; ok {
			c.rulef("storage-archive-ref", "%s: archive table %s conflicts with physical table %s", a.where, a.name, prev)
		}
		if _, ok := c.tableColumns[a.source][a.ageBy]; !ok {
			c.rulef("storage-archive-ref", "%s: age_by column %s not found on %s", a.where, a.ageBy, a.source)
		}
		if c.archiveEntries == nil {
			c.archiveEntries = map[string]storageArchiveEntry{}
		}
		c.archiveEntries[a.source] = a
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
