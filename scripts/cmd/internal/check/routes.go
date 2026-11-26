// routes.go 校验 feature.routes 的结构、唯一性与页面/权限/布局引用。
package check

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (c *Checker) checkFeatureRoutes() {
	schema, err := c.loadSchemaByType("feature.routes")
	if err != nil {
		c.rulef("schema.load", "load feature.routes schema: %v", err)
		return
	}
	c.checkRules(schema)
	rel := filepath.Join(schema.Targets.Dir, schema.Targets.Glob)
	doc, err := loadYAML(filepath.Join(c.root, rel))
	if err != nil {
		c.rulef("schema.load", "%s: %v", rel, err)
		return
	}
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	c.checkStability(doc, rel)
	if id, _ := doc["id"].(string); id != schema.Targets.ID {
		c.rulef("route-structure", "%s: id must be %s, got %q", rel, schema.Targets.ID, id)
	}
	pages := c.featurePageIDs()
	layouts := map[string]bool{}
	layoutMap, _ := asMap(doc["layout"])
	for key := range layoutMap {
		layouts[key] = true
	}
	ids := map[string]bool{}
	paths := map[string]bool{}
	names := map[string]bool{}
	for i, raw := range asList(doc["routes"]) {
		item, _ := asMap(raw)
		where := fmt.Sprintf("%s.routes[%d]", rel, i)
		id, _ := item["id"].(string)
		path, _ := item["path"].(string)
		name, _ := item["name"].(string)
		if ids[id] {
			c.rulef("route-structure", "%s: duplicate route id %s", where, id)
		}
		ids[id] = true
		if paths[path] {
			c.rulef("route-structure", "%s: duplicate route path %s", where, path)
		}
		paths[path] = true
		if names[name] {
			c.rulef("route-structure", "%s: duplicate route name %s", where, name)
		}
		names[name] = true
		if page, _ := item["page"].(string); page != "" && !pages[page] {
			c.rulef("route-structure", "%s: unknown page %s", where, page)
		}
		if layout, _ := item["layout"].(string); layout != "" && !layouts[layout] {
			c.rulef("route-structure", "%s: unknown layout %s", where, layout)
		}
		permission, _ := item["permission"].(string)
		if permission != "" && !c.permissionExists(permission) {
			c.rulef("route-structure", "%s: unknown permission %s", where, permission)
		}
		if meta, _ := asMap(item["meta"]); meta != nil {
			if public, _ := meta["public"].(bool); public && permission != "" {
				c.rulef("route-structure", "%s: public route must not declare permission", where)
			}
		}
	}
	for i, raw := range asList(doc["redirects"]) {
		item, _ := asMap(raw)
		where := fmt.Sprintf("%s.redirects[%d]", rel, i)
		for _, key := range []string{"from", "to"} {
			value, _ := item[key].(string)
			if !strings.HasPrefix(value, "/") {
				c.rulef("route-redirect", "%s: %s must start with /", where, key)
			}
		}
	}
	if nf, _ := asMap(doc["not_found"]); nf != nil {
		if page, _ := nf["page"].(string); page != "" && !pages[page] {
			c.rulef("route-redirect", "%s: unknown not_found page %s", rel, page)
		}
		if layout, _ := nf["layout"].(string); layout != "" && !layouts[layout] {
			c.rulef("route-redirect", "%s: unknown not_found layout %s", rel, layout)
		}
	}
}

// featurePageIDs 读取 feature.pages targets 下的页面 ID。
func (c *Checker) featurePageIDs() map[string]bool {
	out := map[string]bool{}
	schema, err := c.loadSchemaByType("feature.pages")
	if err != nil {
		return out
	}
	for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		if id, _ := doc["id"].(string); id != "" {
			out[id] = true
		}
	}
	return out
}
