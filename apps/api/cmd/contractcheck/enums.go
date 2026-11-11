// enums.go 校验 data.enums 集合与枚举引用。
package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (c *Checker) checkEnums(schema *Schema) map[string]bool {
	path := filepath.Join(c.root, "contracts/data/enums.yaml")
	rel := relPath(c.root, path)
	doc, err := loadYAML(path)
	if err != nil {
		c.errf("%s: %v", rel, err)
		return map[string]bool{}
	}
	c.validateDoc(doc, schema.Fields, rel)
	if doc["id"] != "data.enums" {
		c.errf("%s: id must be data.enums", rel)
	}
	ids := map[string]bool{}
	names := map[string]bool{}
	rawEnums := asList(doc["enums"])
	if len(rawEnums) == 0 {
		c.errf("%s: enums must not be empty", rel)
	}
	for i, raw := range rawEnums {
		m, ok := asMap(raw)
		if !ok {
			c.errf("%s: enums[%d] must be an object", rel, i)
			continue
		}
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		if id != "data.enums."+name {
			c.errf("%s: enums[%d]: id must be data.enums.%s, got %s", rel, i, name, id)
		}
		if ids[id] {
			c.errf("%s: enums[%d]: duplicate id %s", rel, i, id)
		}
		ids[id] = true
		if names[name] {
			c.errf("%s: enums[%d]: duplicate name %s", rel, i, name)
		}
		names[name] = true
		values := map[string]bool{}
		rawValues := asList(m["values"])
		if len(rawValues) == 0 {
			c.errf("%s: enums[%d] (%s): values must not be empty", rel, i, name)
		}
		for j, rv := range rawValues {
			vm, ok := asMap(rv)
			if !ok {
				c.errf("%s: enums[%d].values[%d] must be an object", rel, i, j)
				continue
			}
			v, _ := vm["value"].(string)
			if values[v] {
				c.errf("%s: enums[%d] (%s): duplicate value %s", rel, i, name, v)
			}
			values[v] = true
		}
	}
	return ids
}

func (c *Checker) checkEnumRefs(tableIDs map[string]string, cols map[string]map[string]bool, enumIDs map[string]bool) {
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
			t, _ := m["type"].(string)
			if strings.HasPrefix(t, "data.enums.") && !enumIDs[t] {
				c.errf("%s: columns[%d] (%s): unknown enum %s", rel, i, m["name"], t)
			}
			if strings.HasPrefix(t, "data.tables.") {
				checkColumnRef(c, t, tableIDs, cols, fmt.Sprintf("%s: columns[%d] (%s)", rel, i, m["name"]))
			}
		}
		for i, raw := range asList(doc["constraints"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			if m["type"] == "enum" {
				if e, _ := m["enum"].(string); e != "" && !enumIDs[e] {
					c.errf("%s: constraints[%d] (%s): unknown enum %s", rel, i, m["name"], e)
				}
			}
			if m["type"] == "foreign_key" {
				ref, _ := m["references"].(string)
				checkColumnRef(c, ref, tableIDs, cols, fmt.Sprintf("%s: constraints[%d] (%s)", rel, i, m["name"]))
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
			if t, _ := m["type"].(string); strings.HasPrefix(t, "data.enums.") && !enumIDs[t] {
				c.errf("%s: fields[%d] (%s): unknown enum %s", rel, i, m["name"], t)
			}
		}
	}
}
