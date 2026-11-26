// web_deps.go 校验 platform/architecture/web_deps.json 的字段、层与依赖引用。
package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const webDepsID = "platform.architecture.web_deps"

var (
	webDepsTopKeys     = map[string]bool{"$comment": true, "id": true, "summary": true, "stability": true, "enforcement": true, "consumers": true, "generates": true, "do_not": true, "layers": true, "rules": true, "forbiddenModules": true}
	webDepsLayerKeys   = map[string]bool{"name": true, "in": true}
	webDepsRuleKeys    = map[string]bool{"from": true, "mayDependOn": true, "forbid": true}
	webDepsForbidKeys  = map[string]bool{"name": true, "from": true, "to": true, "reason": true, "comment": true}
	webDepsPathKeys    = map[string]bool{"path": true, "pathNot": true}
	webDepsStability   = map[string]bool{"stable": true, "draft": true, "deprecated": true}
	webDepsEnforcement = map[string]bool{"generated": true, "tested": true, "generated+tested": true, "review-only": true}
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
	const top = "web_deps"
	c.checkJSONKeys(rel, top, doc, webDepsTopKeys)
	if raw, ok := doc["$comment"]; ok {
		if _, ok := raw.(string); !ok {
			c.rulef("schema.type", "%s: web_deps.$comment must be a string", rel)
		}
	}
	if id, ok := c.webDepsString(rel, top, doc, "id", true); ok && id != webDepsID {
		c.rulef("schema.ref", "%s: web_deps.id must be %s, got %s", rel, webDepsID, id)
	}
	c.webDepsString(rel, top, doc, "summary", true)
	if stability, ok := c.webDepsString(rel, top, doc, "stability", true); ok && !webDepsStability[stability] {
		c.rulef("schema.enum", "%s: web_deps.stability must be one of [stable draft deprecated], got %s", rel, stability)
	}
	if enforcement, ok := c.webDepsString(rel, top, doc, "enforcement", true); ok && !webDepsEnforcement[enforcement] {
		c.rulef("schema.enum", "%s: web_deps.enforcement must be one of [generated tested generated+tested review-only], got %s", rel, enforcement)
	}
	c.webDepsStrings(rel, top, doc, "consumers", true)
	c.webDepsStrings(rel, top, doc, "do_not", true)
	if generates, ok := c.webDepsStrings(rel, top, doc, "generates", true); ok && len(generates) == 0 {
		c.rulef("schema.range", "%s: web_deps.generates must not be empty", rel)
	}

	layers := map[string]bool{}
	layerItems, layerOK := c.webDepsObjects(rel, top, doc, "layers", true)
	if layerOK && len(layerItems) == 0 {
		c.rulef("schema.range", "%s: web_deps.layers must not be empty", rel)
	}
	for i, item := range layerItems {
		where := fmt.Sprintf("layers[%d]", i)
		c.checkJSONKeys(rel, where, item, webDepsLayerKeys)
		c.webDepsString(rel, where, item, "in", true)
		name, ok := c.webDepsString(rel, where, item, "name", true)
		if !ok {
			continue
		}
		if layers[name] {
			c.rulef("schema.unique", "%s: %s duplicate %s", rel, where, name)
		}
		layers[name] = true
	}

	seenFrom := map[string]bool{}
	ruleItems, ruleOK := c.webDepsObjects(rel, top, doc, "rules", true)
	if ruleOK && len(ruleItems) == 0 {
		c.rulef("schema.range", "%s: web_deps.rules must not be empty", rel)
	}
	for i, item := range ruleItems {
		where := fmt.Sprintf("rules[%d]", i)
		c.checkJSONKeys(rel, where, item, webDepsRuleKeys)
		from, ok := c.webDepsString(rel, where, item, "from", true)
		if ok {
			if !layers[from] {
				c.rulef("schema.ref", "%s: %s.from %s is not a layer", rel, where, from)
			}
			if seenFrom[from] {
				c.rulef("schema.unique", "%s: %s.from %s is duplicated", rel, where, from)
			}
			seenFrom[from] = true
		}
		may := map[string]bool{}
		if deps, ok := c.webDepsStrings(rel, where, item, "mayDependOn", false); ok {
			for _, dep := range deps {
				may[dep] = true
				if !layers[dep] {
					c.rulef("schema.ref", "%s: %s.mayDependOn %s is not a layer", rel, where, dep)
				}
			}
		}
		if deps, ok := c.webDepsStrings(rel, where, item, "forbid", false); ok {
			for _, dep := range deps {
				if !layers[dep] {
					c.rulef("schema.ref", "%s: %s.forbid %s is not a layer", rel, where, dep)
				}
				if may[dep] {
					c.rulef("schema.ref", "%s: %s both allows and forbids %s", rel, where, dep)
				}
			}
		}
	}

	seen := map[string]bool{}
	for i, item := range webDepsObjectsOrEmpty(c, rel, top, doc, "forbiddenModules", false) {
		where := fmt.Sprintf("forbiddenModules[%d]", i)
		c.checkJSONKeys(rel, where, item, webDepsForbidKeys)
		if name, ok := c.webDepsString(rel, where, item, "name", true); ok {
			if seen[name] {
				c.rulef("schema.unique", "%s: %s duplicate %s", rel, where, name)
			}
			seen[name] = true
		}
		for _, side := range []string{"from", "to"} {
			sideDoc, ok := c.webDepsObject(rel, where, item, side, true)
			if !ok {
				continue
			}
			sideWhere := where + "." + side
			c.checkJSONKeys(rel, sideWhere, sideDoc, webDepsPathKeys)
			_, hasPath := c.webDepsOptionalString(rel, sideWhere, sideDoc, "path")
			_, hasPathNot := c.webDepsOptionalString(rel, sideWhere, sideDoc, "pathNot")
			if !hasPath && !hasPathNot {
				c.rulef("schema.required", "%s: %s requires path or pathNot", rel, sideWhere)
			}
		}
		c.webDepsOptionalString(rel, where, item, "reason")
		c.webDepsOptionalString(rel, where, item, "comment")
	}
}

// webDepsObjectsOrEmpty 读取对象列表；字段不存在或类型错误时返回空列表（错误已由 webDepsObjects 报出）。
func webDepsObjectsOrEmpty(c *Checker, rel, where string, doc map[string]any, key string, required bool) []map[string]any {
	items, _ := c.webDepsObjects(rel, where, doc, key, required)
	return items
}

func (c *Checker) webDepsString(rel, where string, doc map[string]any, key string, required bool) (string, bool) {
	if _, ok := doc[key]; !ok {
		if required {
			c.rulef("schema.required", "%s: %s missing required field %q", rel, where, key)
		}
		return "", false
	}
	return c.webDepsOptionalString(rel, where, doc, key)
}

func (c *Checker) webDepsOptionalString(rel, where string, doc map[string]any, key string) (string, bool) {
	raw, ok := doc[key]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	if !ok || strings.TrimSpace(s) == "" {
		c.rulef("schema.type", "%s: %s.%s must be a non-empty string", rel, where, key)
		return "", false
	}
	return s, true
}

func (c *Checker) webDepsStrings(rel, where string, doc map[string]any, key string, required bool) ([]string, bool) {
	raw, ok := doc[key]
	if !ok {
		if required {
			c.rulef("schema.required", "%s: %s missing required field %q", rel, where, key)
		}
		return nil, false
	}
	list, ok := raw.([]any)
	if !ok {
		c.rulef("schema.type", "%s: %s.%s must be a list", rel, where, key)
		return nil, false
	}
	out := make([]string, 0, len(list))
	for i, item := range list {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			c.rulef("schema.type", "%s: %s.%s[%d] must be a non-empty string", rel, where, key, i)
			continue
		}
		out = append(out, s)
	}
	return out, true
}

func (c *Checker) webDepsObjects(rel, where string, doc map[string]any, key string, required bool) ([]map[string]any, bool) {
	raw, ok := doc[key]
	if !ok {
		if required {
			c.rulef("schema.required", "%s: %s missing required field %q", rel, where, key)
		}
		return nil, false
	}
	list, ok := raw.([]any)
	if !ok {
		c.rulef("schema.type", "%s: %s.%s must be a list", rel, where, key)
		return nil, false
	}
	out := make([]map[string]any, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			c.rulef("schema.type", "%s: %s.%s[%d] must be an object", rel, where, key, i)
			continue
		}
		out = append(out, m)
	}
	return out, true
}

func (c *Checker) webDepsObject(rel, where string, doc map[string]any, key string, required bool) (map[string]any, bool) {
	raw, ok := doc[key]
	if !ok {
		if required {
			c.rulef("schema.required", "%s: %s missing required field %q", rel, where, key)
		}
		return nil, false
	}
	m, ok := raw.(map[string]any)
	if !ok {
		c.rulef("schema.type", "%s: %s.%s must be an object", rel, where, key)
		return nil, false
	}
	return m, true
}

// checkJSONKeys 拒绝 JSON 契约中未声明的字段；这些契约没有 _meta 字段树。
func (c *Checker) checkJSONKeys(rel, where string, doc map[string]any, allowed map[string]bool) {
	for key := range doc {
		if !allowed[key] {
			c.rulef("schema.unknown-field", "%s: %s has unknown field %q", rel, where, key)
		}
	}
}
