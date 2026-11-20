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
	modules := moduleSet{}
	for _, raw := range asList(doc["modules"]) {
		item, _ := asMap(raw)
		if name, _ := item["name"].(string); name != "" {
			modules[name] = true
		}
	}
	if len(modules) == 0 {
		return modules
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
