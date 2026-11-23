// architecture.go 校验 go_arch 分层/模块引用。
package check

import "path/filepath"

func (c *Checker) checkArchitecture() {
	path := filepath.Join(c.root, "contracts/platform/architecture/go_arch.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	layers := map[string]bool{}
	for _, raw := range asList(doc["layers"]) {
		item, _ := asMap(raw)
		if name, _ := item["name"].(string); name != "" {
			layers[name] = true
		}
	}
	rules, _ := asMap(doc["rules"])
	for name, raw := range rules {
		if !layers[name] {
			c.rulef("architecture-refs", "%s: rules.%s is not a layer", rel, name)
		}
		item, _ := asMap(raw)
		for _, key := range []string{"may_depend_on", "forbid"} {
			for _, rawDep := range asList(item[key]) {
				dep, _ := rawDep.(string)
				if dep == "" || containsGlob(dep) {
					continue
				}
				if !layers[dep] {
					c.rulef("architecture-refs", "%s: rules.%s.%s references unknown layer %s", rel, name, key, dep)
				}
			}
		}
	}
	modules := map[string]bool{}
	if mod, err := loadYAML(filepath.Join(c.root, "contracts/platform/modules.yaml")); err == nil {
		for _, raw := range asList(mod["modules"]) {
			item, _ := asMap(raw)
			if name, _ := item["name"].(string); name != "" {
				modules[name] = true
			}
		}
	}
	deps, _ := asMap(doc["module_dependencies"])
	for module, raw := range deps {
		if !modules[module] {
			c.rulef("architecture-refs", "%s: module_dependencies.%s is not in platform.modules", rel, module)
		}
		for _, rawLayer := range asList(raw) {
			layer, _ := rawLayer.(string)
			if layer != "" && !layers[layer] {
				c.rulef("architecture-refs", "%s: module_dependencies.%s references unknown layer %s", rel, module, layer)
			}
		}
	}
	for i, raw := range asList(doc["forbidden_imports"]) {
		item, _ := asMap(raw)
		for _, rawLayer := range asList(item["in"]) {
			layer, _ := rawLayer.(string)
			if layer != "" && !layers[layer] {
				c.rulef("architecture-refs", "%s: forbidden_imports[%d] references unknown layer %s", rel, i, layer)
			}
		}
	}
}

func containsGlob(s string) bool {
	for _, r := range s {
		if r == '*' || r == '/' || r == '.' {
			return true
		}
	}
	return false
}
