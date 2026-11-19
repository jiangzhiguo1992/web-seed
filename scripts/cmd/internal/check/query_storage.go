// query_storage.go 校验 query.caches / query.archive 与 storage 声明的双向一致。
package check

import (
	"fmt"
	"sort"
	"strings"
)

// checkQueryStorageRefs 校验：
//   - 缓存引用必须是已登记的逻辑缓存，且表在 query.tables 中；
//   - 读查询的每个缓存键列必须出现在 params[].column 或同表 index_ref[].columns 中；
//   - 读查询由 params 绑定全部缓存键时必须声明该缓存；
//   - 写查询必须列出每个被写表的全部逻辑缓存（写同步项不要求键出现在 params/index_ref）；
//   - 归档引用必须是 promotable 归档，且源表在 query.tables 中；
//   - 反向：每个逻辑缓存、每个 promotable 归档至少被一个查询有效引用。
func (c *Checker) checkQueryStorageRefs() {
	usedCaches := map[string]bool{}
	usedArchives := map[string]bool{}
	cacheRefs := c.sortedCacheRefs()
	for _, qid := range c.queryOrder {
		q := c.queries[qid]
		if q == nil {
			continue
		}
		where := fmt.Sprintf("%s: query %s", q.File, qid)
		written := map[string]bool{}
		for _, table := range q.Writes {
			written[table] = true
		}
		for _, ref := range q.Caches {
			entry, ok := c.cacheEntries[ref]
			if !ok {
				c.rulef("query-cache-ref", "%s: cache %s is not declared in storage.caches", where, ref)
				continue
			}
			if !q.tableSet()[entry.table] {
				c.rulef("query-cache-ref", "%s: cache %s covers table %s which is not in query tables", where, ref, entry.table)
				continue
			}
			usedCaches[ref] = true
			if written[entry.table] {
				continue // 写同步项没有 key 查找语义
			}
			sources := c.cacheKeySources(q, entry.table)
			for _, col := range entry.key {
				if !sources[col] {
					c.rulef("query-cache-ref", "%s: cache %s key column %s must appear in params[].column or index_ref[].columns of %s", where, ref, col, entry.table)
				}
			}
		}
		for _, table := range q.Writes {
			for _, ref := range cacheRefs {
				if c.cacheEntries[ref].table == table && !containsString(q.Caches, ref) {
					c.rulef("query-cache-ref", "%s: write to %s must list cache %s", where, table, ref)
				}
			}
		}
		for _, ref := range cacheRefs {
			entry := c.cacheEntries[ref]
			if !q.tableSet()[entry.table] || written[entry.table] {
				continue
			}
			if c.cacheKeysBound(q, entry) && !containsString(q.Caches, ref) {
				c.rulef("query-cache-ref", "%s: cache %s key is fully bound by params but the query does not declare it", where, ref)
			}
		}
		for _, ref := range q.Archive {
			source, ok := archiveRefSource(ref.Archive)
			if !ok {
				c.rulef("query-archive-ref", "%s: malformed archive ref %q", where, ref.Archive)
				continue
			}
			entry, ok := c.archiveEntries[source]
			if !ok {
				c.rulef("query-archive-ref", "%s: archive %s is not declared in storage.tables", where, ref.Archive)
				continue
			}
			if !entry.promotable {
				c.rulef("query-archive-ref", "%s: archive %s is not promotable", where, ref.Archive)
			}
			if !q.tableSet()[source] {
				c.rulef("query-archive-ref", "%s: archive source %s is not in query tables", where, source)
				continue
			}
			if entry.promotable {
				usedArchives[source] = true
			}
			if ref.AgeBy != entry.ageBy {
				c.rulef("query-archive-ref", "%s: archive %s age_by %q must match storage age_by %q", where, ref.Archive, ref.AgeBy, entry.ageBy)
			}
		}
	}
	for _, ref := range cacheRefs {
		if !usedCaches[ref] {
			c.rulef("storage-cache-used-by", "storage cache %s is not referenced by any query", ref)
		}
	}
	sources := make([]string, 0, len(c.archiveEntries))
	for source := range c.archiveEntries {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		if c.archiveEntries[source].promotable && !usedArchives[source] {
			c.rulef("query-archive-used-by", "archive %s.archive is promotable but no query references it", source)
		}
	}
}

// cacheKeySources 返回读查询可用于缓存键的列：params[].column 与同表 index_ref[].columns。
func (c *Checker) cacheKeySources(q *queryInfo, table string) map[string]bool {
	out := map[string]bool{}
	for _, p := range q.Params {
		t, col, ok := splitColumnRef(p.Column)
		if ok && t == table {
			out[col] = true
		}
	}
	for _, ref := range q.IndexRef {
		t, _, ok := splitIndexRef(ref.Index)
		if !ok || t != table {
			continue
		}
		for _, col := range ref.Columns {
			out[col] = true
		}
	}
	return out
}

// cacheKeysBound 判断缓存键是否全部由 params[].column 绑定。
func (c *Checker) cacheKeysBound(q *queryInfo, entry storageCacheEntry) bool {
	if len(entry.key) == 0 {
		return false
	}
	bound := map[string]bool{}
	for _, p := range q.Params {
		t, col, ok := splitColumnRef(p.Column)
		if ok && t == entry.table {
			bound[col] = true
		}
	}
	for _, col := range entry.key {
		if !bound[col] {
			return false
		}
	}
	return true
}

// sortedCacheRefs 返回排序后的逻辑缓存引用，保证错误输出稳定。
func (c *Checker) sortedCacheRefs() []string {
	out := make([]string, 0, len(c.cacheEntries))
	for ref := range c.cacheEntries {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

// archiveRefSource 解析 data.tables.<t>.archive 的源表 ID。
func archiveRefSource(ref string) (string, bool) {
	const prefix = "data.tables."
	if !strings.HasPrefix(ref, prefix) || !strings.HasSuffix(ref, ".archive") {
		return "", false
	}
	source := strings.TrimSuffix(ref, ".archive")
	if source == prefix {
		return "", false
	}
	return source, true
}
