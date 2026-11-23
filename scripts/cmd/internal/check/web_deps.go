// web_deps.go 校验 platform/architecture/web_deps.json 的层与依赖引用。
package check

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func (c *Checker) checkWebDeps() {
	path := filepath.Join(c.root, "contracts/platform/architecture/web_deps.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		c.rulef("schema.ref", "%s: %v", rel, err)
		return
	}
	layers := map[string]bool{}
	for i, raw := range asList(doc["layers"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		if layers[name] {
			c.rulef("schema.ref", "%s: layers[%d] duplicate %s", rel, i, name)
		}
		layers[name] = true
		if in, _ := item["in"].(string); in == "" {
			c.rulef("schema.ref", "%s: layers[%d].in must not be empty", rel, i)
		}
	}
	seenFrom := map[string]bool{}
	for i, raw := range asList(doc["rules"]) {
		item, _ := asMap(raw)
		from, _ := item["from"].(string)
		if from != "" && !layers[from] {
			c.rulef("schema.ref", "%s: rules[%d].from %s is not a layer", rel, i, from)
		}
		if seenFrom[from] {
			c.rulef("schema.ref", "%s: rules[%d].from %s is duplicated", rel, i, from)
		}
		seenFrom[from] = true
		may := map[string]bool{}
		for _, rawDep := range asList(item["mayDependOn"]) {
			dep, _ := rawDep.(string)
			may[dep] = true
			if dep != "" && !layers[dep] {
				c.rulef("schema.ref", "%s: rules[%d].mayDependOn %s is not a layer", rel, i, dep)
			}
		}
		for _, rawDep := range asList(item["forbid"]) {
			dep, _ := rawDep.(string)
			if dep != "" && !layers[dep] {
				c.rulef("schema.ref", "%s: rules[%d].forbid %s is not a layer", rel, i, dep)
			}
			if may[dep] {
				c.rulef("schema.ref", "%s: rules[%d] both allows and forbids %s", rel, i, dep)
			}
		}
	}
	if len(asList(doc["generates"])) == 0 {
		c.rulef("schema.ref", "%s: generates must not be empty", rel)
	}
	seen := map[string]bool{}
	for i, raw := range asList(doc["forbiddenModules"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		if seen[name] {
			c.rulef("schema.ref", "%s: forbiddenModules[%d] duplicate %s", rel, i, name)
		}
		seen[name] = true
	}
}
