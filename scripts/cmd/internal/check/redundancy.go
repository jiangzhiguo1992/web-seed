// redundancy.go 校验 data/redundancy.yaml 的冗余组：
// 组明细在 groups，表列只写组 ID；两边必须双向一致，copy 与 source 类型/可空一致。
package check

import (
	"path/filepath"
	"sort"
	"strings"
)

// redundancyGroup 是 data/redundancy.yaml groups 的解析结果。
type redundancyGroup struct {
	source string
	copies []string
}

// refs 返回 source + copies，不修改底层切片。
func (g *redundancyGroup) refs() []string {
	out := make([]string, 0, 1+len(g.copies))
	out = append(out, g.source)
	out = append(out, g.copies...)
	return out
}

func (g *redundancyGroup) has(ref string) bool {
	for _, item := range g.refs() {
		if item == ref {
			return true
		}
	}
	return false
}

// checkRedundancyFile 加载 data/redundancy.yaml；文件缺失或结构非法都直接报错。
func (c *Checker) checkRedundancyFile() {
	rel := "contracts/data/redundancy.yaml"
	c.redundancyGroups = map[string]*redundancyGroup{}
	doc, err := loadYAML(filepath.Join(c.root, rel))
	if err != nil {
		c.rulef("redundant-group", "%s: %v", rel, err)
		return
	}
	allowed := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true, "do_not": true, "groups": true,
	}
	for _, k := range sortedAnyKeys(doc) {
		if !allowed[k] {
			c.rulef("redundant-group", "%s: unknown top-level field %q", rel, k)
		}
	}
	if id, _ := doc["id"].(string); id != "data.redundancy" {
		c.rulef("redundant-group", "%s: id must be data.redundancy, got %q", rel, id)
	}
	if summary, _ := doc["summary"].(string); strings.TrimSpace(summary) == "" {
		c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
	}
	if _, ok := doc["do_not"]; !ok {
		c.rulef("schema.required", "%s: missing required field \"do_not\"", rel)
	}
	if _, ok := doc["groups"]; !ok {
		c.rulef("redundant-group", "%s: groups must be a map keyed by group id", rel)
	}
	c.checkStability(doc, rel)
	c.checkRedundancyGroups(doc, rel)
}

// checkRedundancyGroups 解析 data/redundancy.yaml 的 groups：
// 组只定义 source/copies，表列只写组 ID；source 唯一、copy 至少一个，类型/可空必须一致。
func (c *Checker) checkRedundancyGroups(doc map[string]any, rel string) {
	if c.redundancyGroups == nil {
		c.redundancyGroups = map[string]*redundancyGroup{}
	}
	raw, ok := doc["groups"]
	if !ok {
		return
	}
	groups, ok := asMap(raw)
	if !ok {
		c.rulef("redundant-group", "%s: groups must be a map keyed by group id", rel)
		return
	}
	owners := map[string]string{}
	for _, id := range sortedAnyKeys(groups) {
		m, ok := asMap(groups[id])
		if !ok {
			c.rulef("redundant-group", "%s: groups.%s must be an object", rel, id)
			continue
		}
		if !targetNamingPatterns["snake_case"].MatchString(id) {
			c.rulef("redundant-group", "%s: groups.%s must be snake_case", rel, id)
		}
		allowed := map[string]bool{"source": true, "copies": true}
		for k := range m {
			if !allowed[k] {
				c.rulef("redundant-group", "%s: groups.%s unknown field %q", rel, id, k)
			}
		}
		source, _ := m["source"].(string)
		srcTable, srcCol, srcOK := splitColumnRef(source)
		if source == "" || !srcOK || !c.columnExists(srcTable, srcCol) {
			c.rulef("redundant-ref", "%s: groups.%s.source %q is not a table column", rel, id, source)
			continue
		}
		rawCopies, ok := m["copies"]
		if !ok {
			c.rulef("redundant-group", "%s: groups.%s.copies must list at least one copy", rel, id)
			continue
		}
		list, ok := asList2(rawCopies)
		if !ok || len(list) == 0 {
			c.rulef("redundant-group", "%s: groups.%s.copies must be a non-empty list", rel, id)
			continue
		}
		refs := []string{source}
		valid := true
		for _, rawCopy := range list {
			copyRef, ok := rawCopy.(string)
			if !ok || copyRef == "" {
				c.rulef("redundant-ref", "%s: groups.%s.copies items must be non-empty column refs", rel, id)
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
				c.rulef("redundant-ref", "%s: groups.%s repeats column %s", rel, id, ref)
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
				c.rulef("redundant-ref", "%s: groups.%s copy %q is not a table column", rel, id, copyRef)
				continue
			}
			meta := c.tableColumns[copyTable][copyCol]
			if c.resolvedColumnType(meta.typ) != c.resolvedColumnType(srcMeta.typ) || meta.nullable != srcMeta.nullable {
				c.rulef("redundant-ref", "%s: groups.%s copy %s type/nullable (%s, nullable=%v) must match source %s (%s, nullable=%v)", rel, id, copyRef, c.resolvedColumnType(meta.typ), meta.nullable, source, c.resolvedColumnType(srcMeta.typ), srcMeta.nullable)
			}
		}
		for _, ref := range refs {
			owners[ref] = id
		}
		c.redundancyGroups[id] = &redundancyGroup{source: source, copies: refs[1:]}
	}
}

// checkRedundancy 校验 columns[].redundancy：
//   - 列上的组 ID 必须在 data/redundancy.yaml 的 groups 中存在；
//   - 该文件列出的每一列必须声明同一个组 ID；
//   - 反向：列声明的组必须列出该列。
func (c *Checker) checkRedundancy(tableIDs map[string]string) {
	declared := map[string]string{}
	for _, id := range sortedStringMapKeys(tableIDs) {
		if id == c.baseID {
			continue
		}
		doc := c.tableDocs[id]
		for _, raw := range asList(doc["columns"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			group, _ := m["redundancy"].(string)
			if name == "" || group == "" {
				continue
			}
			declared[id+".col."+name] = group
		}
	}

	for _, ref := range sortedStringMapKeys(declared) {
		groupID := declared[ref]
		group := c.redundancyGroups[groupID]
		if group == nil {
			c.rulef("redundant-ref", "%s: redundancy group %s is not defined in data/redundancy.yaml", ref, groupID)
			continue
		}
		if !group.has(ref) {
			c.rulef("redundant-ref", "%s: redundancy group %s does not list this column", ref, groupID)
		}
	}

	for _, groupID := range sortedRedundancyKeys(c.redundancyGroups) {
		group := c.redundancyGroups[groupID]
		for _, ref := range group.refs() {
			if declared[ref] == groupID {
				continue
			}
			got := declared[ref]
			if got == "" {
				got = "<none>"
			}
			c.rulef("redundant-ref", "%s: data/redundancy.yaml group %s lists this column but columns[].redundancy is %s", ref, groupID, got)
		}
	}
}

func sortedRedundancyKeys(m map[string]*redundancyGroup) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
