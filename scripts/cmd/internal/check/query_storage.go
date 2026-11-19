// query_storage.go 校验 query.caches / query.archive 与 storage 声明的双向一致。
package check

import (
	"fmt"
	"sort"
	"strings"
)

// checkQueryStorageRefs 校验：
//   - 缓存引用必须是已登记的逻辑缓存，且表在 query.tables 中；
//   - 只读使用要求缓存键列全部由 params[].column 绑定；
//   - 写查询必须列出每个被写表的全部逻辑缓存；
//   - 归档引用必须是 promotable 归档，且源表在 query.tables 中；
//   - 反向：每个逻辑缓存、每个 promotable 归档至少被一个查询引用。
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
			usedCaches[ref] = true
			entry, ok := c.cacheEntries[ref]
			if !ok {
				c.rulef("query-cache-ref", "%s: cache %s is not declared in storage.caches", where, ref)
				continue
			}
			if !q.tableSet()[entry.table] {
				c.rulef("query-cache-ref", "%s: cache %s covers table %s which is not in query tables", where, ref, entry.table)
				continue
			}
			if written[entry.table] {
				continue // 写查询只要求列全（见下方完整性校验），不要求键绑定
			}
			bound := map[string]bool{}
			for _, p := range q.Params {
				table, col, ok := splitColumnRef(p.Column)
				if ok && table == entry.table {
					bound[col] = true
				}
			}
			for _, col := range entry.key {
				if !bound[col] {
					c.rulef("query-cache-ref", "%s: cache %s key column %s is not bound by params", where, ref, col)
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
		for _, ref := range q.Archive {
			source, ok := archiveRefSource(ref)
			if !ok {
				c.rulef("query-archive-ref", "%s: malformed archive ref %q", where, ref)
				continue
			}
			entry, ok := c.archiveEntries[source]
			if !ok {
				c.rulef("query-archive-ref", "%s: archive %s is not declared in storage.tables", where, ref)
				continue
			}
			if !entry.promotable {
				c.rulef("query-archive-ref", "%s: archive %s is not promotable", where, ref)
			}
			if !q.tableSet()[source] {
				c.rulef("query-archive-ref", "%s: archive source %s is not in query tables", where, source)
			}
			usedArchives[source] = true
		}
	}
	for _, ref := range cacheRefs {
		if !usedCaches[ref] {
			c.rulef("query-cache-used-by", "storage cache %s is not referenced by any query", ref)
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
