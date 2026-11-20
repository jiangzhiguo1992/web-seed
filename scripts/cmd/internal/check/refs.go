// refs.go 校验 capabilities 的表/写查询引用、used_by 与 index.yaml。
// entity/query 自身的表引用由 entities.go / queries.go 的类型级规则负责。
package check

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	indexIDRe          = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
	indexConsumerRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*([-/.][A-Za-z][A-Za-z0-9_]*)*$`)
	indexEnforcement   = map[string]bool{"generated": true, "generated+tested": true, "tested": true, "review-only": true}
	indexEntryKeys     = map[string]bool{"id": true, "path": true, "summary": true, "consumers": true, "enforcement": true, "stability": true, "common_mistakes": true}
	indexTopLevelKeys  = map[string]bool{"schema_version": true, "read_order": true, "groups": true, "entries": true, "global_notes": true, "orphan_whitelist": true}
	indexStabilityEnum = map[string]bool{"stable": true, "draft": true, "deprecated": true}
)

// validIndexPath 要求 index path 是相对 contracts/ 的干净路径，禁止绝对路径、目录逃逸与跨平台分隔符。
func validIndexPath(p string) bool {
	if p == "" || strings.TrimSpace(p) != p || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:") {
		return false
	}
	clean := pathpkg.Clean(p)
	return clean == p && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

// checkStringList 校验非空字符串列表；required 为 true 时缺失也报错。
func (c *Checker) checkStringList(raw any, rel, where string, required bool) {
	if raw == nil {
		if required {
			c.rulef("schema.required", "%s: %s is required", rel, where)
		}
		return
	}
	list, ok := asList2(raw)
	if !ok {
		c.rulef("schema.type", "%s: %s must be a list", rel, where)
		return
	}
	if len(list) == 0 {
		if required {
			c.rulef("schema.len", "%s: %s must not be empty", rel, where)
		}
		return
	}
	seen := map[string]bool{}
	for i, v := range list {
		s, _ := v.(string)
		if strings.TrimSpace(s) == "" {
			c.rulef("schema.type", "%s: %s[%d] must be a non-empty string", rel, where, i)
			continue
		}
		if seen[s] {
			c.rulef("schema.unique", "%s: %s has duplicate value %q", rel, where, s)
		}
		seen[s] = true
	}
}

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

func (c *Checker) checkCapabilityWrites(tableIDs map[string]string) {
	c.declaredWrites = map[string]bool{}
	for _, f := range walkYAML(c.root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, f), err)
			continue
		}
		rel := relPath(c.root, f)
		id, _ := doc["id"].(string)
		if !strings.HasPrefix(id, "cap.services.") {
			if doc["persistence"] != nil || doc["writes"] != nil {
				c.rulef("owner-write", "%s: only cap.services may declare persistence/writes", rel)
			}
			continue
		}
		module, _ := doc["owner_module"].(string)
		for _, raw := range asList(doc["services"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			for _, rawStep := range asList(item["steps"]) {
				step, _ := asMap(rawStep)
				var qids []string
				for _, rawQ := range stepQueryItems(step) {
					qo, _ := asMap(rawQ)
					if qid, _ := qo["ref"].(string); qid != "" {
						qids = append(qids, qid)
					}
				}
				for _, qid := range qids {
					q := c.queries[qid]
					if q == nil {
						continue
					}
					if queryReturnsAffectedRows(q) && len(q.Writes) == 0 {
						c.rulef("service-owner-write", "%s.%s: query %s returns affected_rows without a write table", rel, name, qid)
					}
					for _, target := range q.Writes {
						c.declaredWrites[qid] = true
						if owner := c.tableOwners[target]; owner != "" && owner != module {
							c.rulef("service-owner-write", "%s.%s: module %q cannot write %s via %s (owner_module %q); cross-module writes must go through kind=service", rel, name, module, target, qid, owner)
						}
					}
				}
			}
		}

	}
	for _, qid := range c.queryOrder {
		q := c.queries[qid]
		if q != nil && (len(q.Writes) > 0 || queryReturnsAffectedRows(q)) && !c.declaredWrites[qid] {
			c.rulef("owner-write", "query %s has write tables or returns affected_rows but is not referenced by any service step", qid)
		}
	}
}

func (c *Checker) checkUsedBy(tableIDs map[string]string) {
	tableEntities := map[string]map[string]bool{}
	tableQueries := map[string]map[string]bool{}
	for id := range tableIDs {
		if id == c.baseID {
			continue
		}
		tableEntities[id] = map[string]bool{}
		tableQueries[id] = map[string]bool{}
	}
	// table.entities 来自 entity.model.tables；查询按查询项 ID 记直接入边。
	for _, e := range c.entities {
		for _, t := range e.Tables {
			if tableEntities[t] != nil {
				tableEntities[t][e.ID] = true
			}
		}
	}
	for _, q := range c.queries {
		for t := range q.tableSet() {
			if tableQueries[t] != nil {
				tableQueries[t][q.ID] = true
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
		table, _ := asMap(doc["table"])
		if table != nil {
			if raw, ok := table["entities"]; ok {
				gotEntities := stringSet(asList(raw))
				if !sameSet(gotEntities, tableEntities[id]) {
					c.rulef("table-entities", "%s: table.entities mismatch: declared %v, actual %v", rel, sortedKeys(gotEntities), sortedKeys(tableEntities[id]))
				}
			}
			raw, ok := table["queries"]
			if !ok {
				continue // 缺失由 schema.required 报出
			}
			gotQueries := stringSet(asList(raw))
			if !sameSet(gotQueries, tableQueries[id]) {
				c.rulef("table-queries-generated", "%s: table.queries mismatch: declared %v, actual %v", rel, sortedKeys(gotQueries), sortedKeys(tableQueries[id]))
			}
		}
	}
}

func (c *Checker) checkIndex(tableIDs map[string]string, enumSchema, entitySchema, querySchema *Schema) {
	path := filepath.Join(c.root, "contracts/index.yaml")
	rel := relPath(c.root, path)
	doc, err := loadYAML(path)
	if err != nil {
		c.rulef("schema.load", "%s: %v", rel, err)
		return
	}
	for key := range doc {
		if !indexTopLevelKeys[key] {
			c.rulef("schema.unknown-field", "%s: unknown top-level field %q", rel, key)
		}
	}
	if v, ok := toInt(doc["schema_version"]); !ok || v != 1 {
		c.rulef("schema.const", "%s: schema_version must be 1", rel)
	}
	c.checkStringList(doc["read_order"], rel, "read_order", false)
	c.checkStringList(doc["global_notes"], rel, "global_notes", false)
	c.checkStringList(doc["orphan_whitelist"], rel, "orphan_whitelist", false)
	groups, _ := asMap(doc["groups"])
	for name, raw := range groups {
		group, _ := asMap(raw)
		if group == nil {
			c.rulef("schema.type", "%s: groups.%s must be an object", rel, name)
			continue
		}
		for key := range group {
			if key != "description" && key != "layers" {
				c.rulef("schema.unknown-field", "%s: groups.%s has unknown field %q", rel, name, key)
			}
		}
		if desc, _ := group["description"].(string); strings.TrimSpace(desc) == "" {
			c.rulef("schema.required", "%s: groups.%s.description is required", rel, name)
		}
		c.checkStringList(group["layers"], rel, "groups."+name+".layers", true)
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
		where := fmt.Sprintf("entries[%d]", i)
		for key := range m {
			if !indexEntryKeys[key] {
				c.rulef("schema.unknown-field", "%s: %s has unknown field %q", rel, where, key)
			}
		}
		id, _ := m["id"].(string)
		p, _ := m["path"].(string)
		if id == "" {
			c.rulef("schema.required", "%s: %s: missing id", rel, where)
			continue
		}
		where = id
		if !indexIDRe.MatchString(id) {
			c.rulef("schema.pattern", "%s: %s id must match ^[a-z][a-z0-9_.-]*$", rel, where)
		}
		if p == "" {
			c.rulef("schema.required", "%s: %s: missing path", rel, id)
			continue
		}
		if !validIndexPath(p) {
			c.rulef("schema.pattern", "%s: %s path must be a clean relative path, got %q", rel, id, p)
			continue
		}
		if summary, _ := m["summary"].(string); strings.TrimSpace(summary) == "" {
			c.rulef("schema.required", "%s: %s: missing summary", rel, id)
		}
		c.checkStringList(m["consumers"], rel, id+".consumers", true)
		for _, raw := range asList(m["consumers"]) {
			consumer, _ := raw.(string)
			if consumer != "" && !indexConsumerRe.MatchString(consumer) {
				c.rulef("schema.pattern", "%s: %s consumer %q must match ^[A-Za-z][A-Za-z0-9_]*([-/.][A-Za-z][A-Za-z0-9_]*)*$", rel, id, consumer)
			}
		}
		if raw, ok := m["common_mistakes"]; ok {
			c.checkStringList(raw, rel, id+".common_mistakes", true)
		}
		rawEnforcement, hasEnforcement := m["enforcement"]
		enforcement, enforcementOK := rawEnforcement.(string)
		if !hasEnforcement {
			c.rulef("schema.required", "%s: %s: missing enforcement", rel, id)
		} else if !enforcementOK || !indexEnforcement[enforcement] {
			c.rulef("schema.enum", "%s: %s enforcement must be generated/generated+tested/tested/review-only, got %v", rel, id, rawEnforcement)
		}
		rawStability, hasStability := m["stability"]
		stability, stabilityOK := rawStability.(string)
		if !hasStability {
			c.rulef("schema.required", "%s: %s: missing stability", rel, id)
		} else if !stabilityOK || !indexStabilityEnum[stability] {
			c.rulef("schema.enum", "%s: %s stability must be stable/draft/deprecated, got %v", rel, id, rawStability)
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
		info, err := os.Stat(filepath.Join(c.root, "contracts", p))
		if err != nil {
			c.rulef("schema.ref", "%s: %s path missing %s", rel, id, p)
			continue
		}
		if info.IsDir() {
			c.rulef("schema.ref", "%s: %s path must be a file, got directory %s", rel, id, p)
			continue
		}
		// index 的 stability 是投影，必须与契约文件自身一致；文件未声明时跳过。
		if stability != "" && indexStabilityEnum[stability] {
			if doc, err := loadYAML(filepath.Join(c.root, "contracts", p)); err == nil {
				if got, _ := doc["stability"].(string); got != "" && got != stability {
					c.rulef("schema.index-stability", "%s: %s stability %q must match %s (%q)", rel, id, stability, p, got)
				}
			}
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
	if enumSchema.Targets.Dir != "" && enumSchema.Targets.Glob != "" {
		for _, file := range glob(c.root, filepath.Join(enumSchema.Targets.Dir, enumSchema.Targets.Glob)) {
			wantID := enumSchema.Targets.FileID(filepath.Base(file))
			if wantID == "" {
				continue
			}
			p := strings.TrimPrefix(relPath(c.root, file), "contracts/")
			got, ok := byPath[p]
			if !ok {
				c.rulef("schema.index", "%s: missing entry for %s (%s)", rel, wantID, p)
				continue
			}
			if got != wantID {
				c.rulef("schema.index", "%s: path %s has id %s, expected %s", rel, p, got, wantID)
			}
		}
	}
	for _, schema := range []*Schema{entitySchema, querySchema} {
		if schema == nil || schema.Targets.Dir == "" || schema.Targets.Glob == "" {
			continue
		}
		for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
			wantID := schema.Targets.FileID(filepath.Base(file))
			if schema.Type == "data.entities" && filepath.Base(file) == "_base.yaml" {
				wantID = baseEntityID
			}
			if wantID == "" {
				continue
			}
			p := strings.TrimPrefix(relPath(c.root, file), "contracts/")
			got, ok := byPath[p]
			if !ok {
				c.rulef("schema.index", "%s: missing entry for %s (%s)", rel, wantID, p)
				continue
			}
			if got != wantID {
				c.rulef("schema.index", "%s: path %s has id %s, expected %s", rel, p, got, wantID)
			}
		}
	}
	// capabilities/features/platform 也必须登记：新增契约不得游离在 index 之外。
	for _, file := range walkYAML(c.root, "contracts") {
		rel := relPath(c.root, file)
		if strings.HasPrefix(rel, "contracts/_meta/") || strings.HasPrefix(rel, "contracts/generated/") ||
			strings.HasPrefix(rel, "contracts/data/") || rel == "contracts/index.yaml" {
			continue
		}
		p := strings.TrimPrefix(rel, "contracts/")
		got, ok := byPath[p]
		if !ok {
			c.rulef("schema.index", "%s: missing entry for %s", rel, p)
			continue
		}
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		if id, _ := doc["id"].(string); id != "" && got != id {
			c.rulef("schema.index", "%s: path %s has id %s, expected %s", rel, p, got, id)
		}
	}
}
