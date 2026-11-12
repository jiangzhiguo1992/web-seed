// refs.go 校验跨文件表引用、used_by 与 index.yaml。
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func checkColumnRef(c *Checker, ref string, tableIDs map[string]string, cols map[string]map[string]bool, where string) {
	if ref == "" {
		return
	}
	parts := strings.Split(ref, ".col.")
	if len(parts) != 2 {
		c.errf("%s: references must be data.tables.<file>.col.<column>, got %s", where, ref)
		return
	}
	tid, col := parts[0], parts[1]
	if _, ok := tableIDs[tid]; !ok {
		c.errf("%s: references unknown table %s", where, tid)
		return
	}
	if set := cols[tid]; set != nil && !set[col] {
		c.errf("%s: references unknown column %s.%s", where, tid, col)
	}
}

func (c *Checker) checkCrossFileTableRefs(tableIDs map[string]string, cols map[string]map[string]bool) {
	// entities
	for _, f := range glob(c.root, "contracts/data/entities/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			c.errf("%s: %v", relPath(c.root, f), err)
			continue
		}
		rel := relPath(c.root, f)
		if t, _ := doc["table"].(string); t != "" {
			if _, ok := tableIDs[t]; !ok {
				c.errf("%s: table %s not found", rel, t)
			}
		} else {
			c.errf("%s: missing table", rel)
		}
		for i, raw := range asList(doc["relations"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			if via, _ := m["via"].(string); via != "" {
				checkColumnRef(c, via, tableIDs, cols, fmt.Sprintf("%s: relations[%d]", rel, i))
			}
		}
	}
	// queries
	for _, f := range glob(c.root, "contracts/data/queries/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			c.errf("%s: %v", relPath(c.root, f), err)
			continue
		}
		rel := relPath(c.root, f)
		for i, raw := range asList(doc["queries"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			qid, _ := m["id"].(string)
			ts := tableRefs(m)
			for _, t := range ts {
				if _, ok := tableIDs[t]; !ok {
					c.errf("%s: queries[%d] (%s): table %s not found", rel, i, qid, t)
				}
			}
			for _, rc := range asList(m["return_columns"]) {
				col, ok := rc.(string)
				if !ok {
					c.errf("%s: queries[%d] (%s): return_columns must be strings", rel, i, qid)
					continue
				}
				found := false
				for _, t := range ts {
					if cols[t] != nil && cols[t][col] {
						found = true
						break
					}
				}
				if !found {
					c.errf("%s: queries[%d] (%s): return column %s not found in %v", rel, i, qid, col, ts)
				}
			}
		}
	}
	// capabilities
	for _, f := range walkYAML(c.root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil {
			c.errf("%s: %v", relPath(c.root, f), err)
			continue
		}
		rel := relPath(c.root, f)
		p, _ := asMap(doc["persistence"])
		if p == nil {
			continue
		}
		for _, t := range tableRefs(p) {
			if _, ok := tableIDs[t]; !ok {
				c.errf("%s: persistence references unknown table %s", rel, t)
			}
		}
	}
}

func (c *Checker) checkUsedBy(tableIDs map[string]string) {
	inbound := map[string]map[string]map[string]bool{}
	for id := range tableIDs {
		if id == "data.tables.base" {
			continue
		}
		inbound[id] = map[string]map[string]bool{
			"entities": {}, "queries": {}, "capabilities": {},
		}
	}
	// entities
	for _, f := range glob(c.root, "contracts/data/entities/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil || doc == nil {
			continue
		}
		id, _ := doc["id"].(string)
		if t, _ := doc["table"].(string); t != "" && inbound[t] != nil {
			inbound[t]["entities"][id] = true
		}
	}
	// queries
	for _, f := range glob(c.root, "contracts/data/queries/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil || doc == nil {
			continue
		}
		id, _ := doc["id"].(string)
		for _, raw := range asList(doc["queries"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			for _, t := range tableRefs(m) {
				if inbound[t] != nil {
					inbound[t]["queries"][id] = true
				}
			}
		}
	}
	// capabilities
	for _, f := range walkYAML(c.root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil || doc == nil {
			continue
		}
		id, _ := doc["id"].(string)
		p, _ := asMap(doc["persistence"])
		if p == nil {
			continue
		}
		for _, t := range tableRefs(p) {
			if inbound[t] != nil {
				inbound[t]["capabilities"][id] = true
			}
		}
	}

	for id, rel := range tableIDs {
		if id == "data.tables.base" {
			continue
		}
		doc, err := loadYAML(filepath.Join(c.root, rel))
		if err != nil {
			continue
		}
		u, _ := asMap(doc["used_by"])
		if u == nil {
			c.errf("%s: missing used_by", rel)
			continue
		}
		for _, kind := range []string{"entities", "queries", "capabilities"} {
			got := stringSet(asList(u[kind]))
			want := inbound[id][kind]
			if !sameSet(got, want) {
				c.errf("%s: used_by.%s mismatch: declared %v, actual %v", rel, kind, sortedKeys(got), sortedKeys(want))
			}
		}
	}
}

func (c *Checker) checkIndex(tableIDs map[string]string) {
	path := filepath.Join(c.root, "contracts/index.yaml")
	rel := relPath(c.root, path)
	doc, err := loadYAML(path)
	if err != nil {
		c.errf("%s: %v", rel, err)
		return
	}
	byPath := map[string]string{}
	byID := map[string]string{}
	rawEntries := asList(doc["entries"])
	if len(rawEntries) == 0 {
		c.errf("%s: entries must not be empty", rel)
	}
	for i, raw := range rawEntries {
		m, ok := asMap(raw)
		if !ok {
			c.errf("%s: entries[%d] must be an object", rel, i)
			continue
		}
		id, _ := m["id"].(string)
		p, _ := m["path"].(string)
		if id == "" {
			c.errf("%s: entries[%d]: missing id", rel, i)
			continue
		}
		if p == "" {
			c.errf("%s: %s: missing path", rel, id)
			continue
		}
		if prev, ok := byID[id]; ok {
			c.errf("%s: duplicate id %s (%s / %s)", rel, id, prev, p)
		}
		byID[id] = p
		if prev, ok := byPath[p]; ok {
			c.errf("%s: duplicate path %s (%s / %s)", rel, p, prev, id)
		}
		byPath[p] = id
		if strings.HasPrefix(p, "generated/") {
			continue
		}
		if _, err := os.Stat(filepath.Join(c.root, "contracts", p)); err != nil {
			c.errf("%s: %s path missing %s", rel, id, p)
		}
	}
	for id, file := range tableIDs {
		p := strings.TrimPrefix(file, "contracts/")
		got, ok := byPath[p]
		if !ok {
			c.errf("%s: missing entry for %s (%s)", rel, id, p)
			continue
		}
		if got != id {
			c.errf("%s: path %s has id %s, expected %s", rel, p, got, id)
		}
	}
	if got, ok := byPath["data/enums.yaml"]; !ok {
		c.errf("%s: missing entry for data/enums.yaml", rel)
	} else if got != "data.enums" {
		c.errf("%s: data/enums.yaml has id %s, expected data.enums", rel, got)
	}
}
