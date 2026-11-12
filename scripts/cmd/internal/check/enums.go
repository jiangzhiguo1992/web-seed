// enums.go 校验 data.enums 集合、code 唯一性与 entity 枚举引用。
package check

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (c *Checker) checkEnums(schema *Schema) map[string]map[string]bool {
	path := filepath.Join(c.root, "contracts/data/enums.yaml")
	rel := relPath(c.root, path)
	doc, err := loadYAML(path)
	if err != nil {
		c.rulef("schema.load", "%s: %v", rel, err)
		return map[string]map[string]bool{}
	}
	c.validateDoc(doc, schema.Fields, rel)
	if doc["id"] != "data.enums" {
		c.rulef("enum-id", "%s: id must be data.enums", rel)
	}
	enums := map[string]map[string]bool{}
	names := map[string]bool{}
	rawEnums := asList(doc["enums"])
	if len(rawEnums) == 0 {
		c.rulef("schema.non-empty", "%s: enums must not be empty", rel)
	}
	for i, raw := range rawEnums {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("schema.type", "%s: enums[%d] must be an object", rel, i)
			continue
		}
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		if id != "data.enums."+name {
			c.rulef("enum-id-matches-name", "%s: enums[%d]: id must be data.enums.%s, got %s", rel, i, name, id)
		}
		if _, ok := enums[id]; ok {
			c.rulef("enum-id-matches-name", "%s: enums[%d]: duplicate id %s", rel, i, id)
		}
		if names[name] {
			c.rulef("enum-id-matches-name", "%s: enums[%d]: duplicate name %s", rel, i, name)
		}
		names[name] = true
		values := map[string]bool{}
		codes := map[int]bool{}
		rawValues := asList(m["values"])
		if len(rawValues) == 0 {
			c.rulef("schema.non-empty", "%s: enums[%d] (%s): values must not be empty", rel, i, name)
		}
		for j, rv := range rawValues {
			vm, ok := asMap(rv)
			if !ok {
				c.rulef("schema.type", "%s: enums[%d].values[%d] must be an object", rel, i, j)
				continue
			}
			v, _ := vm["value"].(string)
			if values[v] {
				c.rulef("enum-values-unique", "%s: enums[%d] (%s): duplicate value %s", rel, i, name, v)
			}
			values[v] = true
			if n, ok := toInt(vm["code"]); ok {
				if codes[n] {
					c.rulef("enum-code-unique", "%s: enums[%d] (%s): duplicate code %d", rel, i, name, n)
				}
				codes[n] = true
			}
		}
		enums[id] = values
	}
	return enums
}

func (c *Checker) checkEnumRefs(tableIDs map[string]string, cols map[string]map[string]bool, enums map[string]map[string]bool) {
	for _, rel := range tableIDs {
		doc, err := loadYAML(filepath.Join(c.root, rel))
		if err != nil {
			continue
		}
		for i, raw := range asList(doc["columns"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); strings.HasPrefix(t, "data.tables.") {
				checkColumnRef(c, t, tableIDs, cols, fmt.Sprintf("%s: columns[%d] (%s)", rel, i, m["name"]))
			}
		}
	}
	for _, f := range glob(c.root, "contracts/data/entities/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			continue
		}
		rel := relPath(c.root, f)
		for i, raw := range asList(doc["fields"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); strings.HasPrefix(t, "data.enums.") {
				if _, ok := enums[t]; !ok {
					c.rulef("schema.ref", "%s: fields[%d] (%s): unknown enum %s", rel, i, m["name"], t)
				}
			}
		}
	}
}
