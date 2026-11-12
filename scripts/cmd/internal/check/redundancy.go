// redundancy.go 校验显式冗余字段：source 是权威列，copy 指向 source 且类型/可空一致。
package check

import (
	"fmt"
	"path/filepath"
)

type redundancyCol struct {
	rel      string
	name     string
	group    string
	role     string
	ref      string
	typ      string
	nullable bool
}

// checkRedundancy 校验 columns[].redundancy：
//   - 每个 group 只能有一个 source，且至少有一个 copy；
//   - copy 必须写 ref，ref 指向同组 source；
//   - copy 与 source 的 type/nullable 必须一致；
//   - source 不得写 ref。
func (c *Checker) checkRedundancy(tableIDs map[string]string, cols map[string]map[string]bool) {
	entries := map[string]*redundancyCol{}
	for tableID, rel := range tableIDs {
		if tableID == c.baseID {
			continue
		}
		doc, err := loadYAML(filepath.Join(c.root, rel))
		if err != nil {
			continue
		}
		for _, raw := range asList(doc["columns"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			red, ok := asMap(m["redundancy"])
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			group, _ := red["group"].(string)
			role, _ := red["role"].(string)
			ref, _ := red["ref"].(string)
			typ, _ := m["type"].(string)
			nullable, _ := m["nullable"].(bool)
			if name == "" || group == "" || role == "" {
				continue
			}
			entries[tableID+".col."+name] = &redundancyCol{
				rel: rel, name: name, group: group,
				role: role, ref: ref, typ: typ, nullable: nullable,
			}
		}
	}

	sources := map[string]int{}
	copies := map[string]int{}
	for _, col := range entries {
		where := fmt.Sprintf("%s: columns[%s].redundancy", col.rel, col.name)
		switch col.role {
		case "source":
			if col.ref != "" {
				c.rulef("redundant-ref", "%s: source must not write ref", where)
			}
			sources[col.group]++
		case "copy":
			copies[col.group]++
			if col.ref == "" {
				c.rulef("redundant-ref", "%s: copy requires ref", where)
				continue
			}
			checkColumnRef(c, col.ref, tableIDs, cols, where)
			source, ok := entries[col.ref]
			if !ok {
				c.rulef("redundant-ref", "%s: ref %s is not a redundancy source", where, col.ref)
				continue
			}
			if source.role != "source" || source.group != col.group {
				c.rulef("redundant-ref", "%s: ref %s must be the source in group %s", where, col.ref, col.group)
			}
			if source.typ != col.typ || source.nullable != col.nullable {
				c.rulef("redundant-ref", "%s: copy type/nullable (%s, nullable=%v) must match source %s (%s, nullable=%v)", where, col.typ, col.nullable, col.ref, source.typ, source.nullable)
			}
		}
	}
	for group, count := range sources {
		if count > 1 {
			c.rulef("redundant-group", "group %s has %d sources; exactly one source is required", group, count)
		}
		if copies[group] == 0 {
			c.rulef("redundant-group", "group %s has a source but no copy", group)
		}
	}
	for group := range copies {
		if sources[group] == 0 {
			c.rulef("redundant-group", "group %s has copies but no source", group)
		}
	}
}
