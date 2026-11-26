// web_deps.go 校验 platform/architecture/web_deps.json 的层与依赖引用。
package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var (
	webDepsTopKeys    = map[string]bool{"$comment": true, "id": true, "summary": true, "stability": true, "enforcement": true, "consumers": true, "generates": true, "do_not": true, "layers": true, "rules": true, "forbiddenModules": true}
	webDepsLayerKeys  = map[string]bool{"name": true, "in": true}
	webDepsRuleKeys   = map[string]bool{"from": true, "mayDependOn": true, "forbid": true}
	webDepsForbidKeys = map[string]bool{"name": true, "from": true, "to": true, "reason": true, "comment": true}
	webDepsPathKeys   = map[string]bool{"path": true, "pathNot": true}
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
	c.checkJSONKeys(rel, "web_deps", doc, webDepsTopKeys)
	layers := map[string]bool{}
	for i, raw := range asList(doc["layers"]) {
		item, _ := asMap(raw)
		c.checkJSONKeys(rel, fmt.Sprintf("layers[%d]", i), item, webDepsLayerKeys)
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
		c.checkJSONKeys(rel, fmt.Sprintf("rules[%d]", i), item, webDepsRuleKeys)
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
		where := fmt.Sprintf("forbiddenModules[%d]", i)
		c.checkJSONKeys(rel, where, item, webDepsForbidKeys)
		for _, side := range []string{"from", "to"} {
			if sideMap, ok := asMap(item[side]); ok {
				c.checkJSONKeys(rel, where+"."+side, sideMap, webDepsPathKeys)
			}
		}
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

// checkJSONKeys 拒绝 JSON 契约中未声明的字段；这些契约没有 _meta 字段树。
func (c *Checker) checkJSONKeys(rel, where string, doc map[string]any, allowed map[string]bool) {
	for key := range doc {
		if !allowed[key] {
			c.rulef("schema.unknown-field", "%s: %s has unknown field %q", rel, where, key)
		}
	}
}
