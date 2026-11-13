// redundancy.go 校验冗余组：组明细在 storage.redundancy_groups，
// 表列只写组 ID；两边必须双向一致，copy 与 source 类型/可空一致。
package check

import "sort"

// redundancyGroup 是 storage.redundancy_groups 的解析结果。
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

// checkRedundancy 校验 columns[].redundancy：
//   - 列上的组 ID 必须在 storage.redundancy_groups 中存在；
//   - storage 组列出的每一列必须声明同一个组 ID；
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
			c.rulef("redundant-ref", "%s: redundancy group %s is not defined in storage.redundancy_groups", ref, groupID)
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
			c.rulef("redundant-ref", "%s: storage group %s lists this column but columns[].redundancy is %s", ref, groupID, got)
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
