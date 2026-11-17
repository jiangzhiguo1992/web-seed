// modules.go 校验模块注册表，并拒绝未登记的 owner_module。
package check

import (
	"path/filepath"
	"strings"
)

type moduleSet map[string]bool

// checkModules 加载 contracts/platform/modules.yaml，并校验所有契约的 owner_module。
func (c *Checker) checkModules() moduleSet {
	path := filepath.Join(c.root, "contracts/platform/modules.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		c.rulef("schema.load", "%s: %v", relPath(c.root, path), err)
		return nil
	}
	rel := relPath(c.root, path)
	if s, _ := doc["id"].(string); s != "platform.modules" {
		c.rulef("schema.required", "%s: id must be platform.modules", rel)
	}
	if s, _ := doc["summary"].(string); strings.TrimSpace(s) == "" {
		c.rulef("schema.required", "%s: summary must not be empty", rel)
	}
	if _, ok := doc["schema_version"]; !ok {
		c.rulef("schema.required", "%s: missing required field schema_version", rel)
	}
	if s, _ := doc["stability"].(string); s == "" {
		c.rulef("schema.required", "%s: missing required field stability", rel)
	}
	if _, ok := doc["do_not"]; !ok {
		c.rulef("schema.required", "%s: missing required field do_not", rel)
	}
	modules := moduleSet{}
	rawModules := asList(doc["modules"])
	if len(rawModules) == 0 {
		c.rulef("schema.required", "%s: modules must not be empty", rel)
	}
	for _, raw := range rawModules {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if !match(`^[a-z][a-z0-9_]*$`, name) {
			c.rulef("schema.pattern", "%s: module name %q must be snake_case", rel, name)
			continue
		}
		if modules[name] {
			c.rulef("schema.unique", "%s: duplicate module %s", rel, name)
			continue
		}
		modules[name] = true
	}
	for _, file := range walkYAML(c.root, "contracts") {
		frel := relPath(c.root, file)
		if strings.HasPrefix(frel, "contracts/_meta/") || strings.HasPrefix(frel, "contracts/generated/") {
			continue
		}
		fdoc, err := loadYAML(file)
		if err != nil {
			continue
		}
		module, _ := fdoc["owner_module"].(string)
		if module != "" && !modules[module] {
			c.rulef("schema.ref", "%s: unknown owner_module %s (not in platform.modules)", frel, module)
		}
	}
	return modules
}
