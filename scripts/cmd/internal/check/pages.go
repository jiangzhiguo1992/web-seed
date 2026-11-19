// pages.go 校验 feature.pages 对 lifecycle / service / route 的结构化引用。
//
// 顶层与 map/list 槽位结构由 _meta/feature.pages.yaml 的 validateDoc 校验；
// 这里只做跨契约引用：enum / transition / transitions / state_not /
// transition_ui.source / capability / route。
package check

import (
	"path/filepath"
	"strings"
)

func (c *Checker) checkFeaturePages() {
	schema, err := c.loadSchemaByType("feature.pages")
	if err != nil {
		c.rulef("schema.load", "load feature.pages schema: %v", err)
		return
	}
	lifecycleIDs, lifecycleActions, lifecycleStates := c.pageLifecycleIndex()
	serviceIDs := c.pageServiceIndex()
	routeIDs := c.pageRouteIndex()
	for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkTargetFileName(schema, filepath.Base(file), rel)
		c.checkPageViewStates(rel, doc)
		stem := strings.TrimSuffix(filepath.Base(file), ".yaml")
		id, _ := doc["id"].(string)
		if want := schema.Targets.FileID(filepath.Base(file)); id != want {
			c.rulef("schema.ref", "%s: id %q must be %q", rel, id, want)
		}
		if page, _ := doc["page"].(string); page != stem {
			c.rulef("schema.ref", "%s: page %q must be %q", rel, page, stem)
		}
		if route, _ := doc["route"].(string); route != "" && routeIDs != nil && !routeIDs[route] {
			c.rulef("schema.ref", "%s: unknown route %s", rel, route)
		}
		lifecycleID := pageLifecycleID(doc)
		c.checkPageNode(rel, doc, lifecycleID, lifecycleIDs, lifecycleActions, lifecycleStates, serviceIDs)
	}
}

// checkPageViewStates 强制页面四态：loading/error/forbidden，以及 empty 或 idle。
func (c *Checker) checkPageViewStates(rel string, doc map[string]any) {
	states, _ := asMap(doc["view_states"])
	if states == nil {
		return
	}
	for _, key := range []string{"loading", "error", "forbidden"} {
		if _, ok := states[key]; !ok {
			c.rulef("schema.required", "%s: view_states missing %s", rel, key)
		}
	}
	if _, ok := states["empty"]; !ok {
		if _, ok := states["idle"]; !ok {
			c.rulef("schema.required", "%s: view_states requires empty or idle", rel)
		}
	}
}

// pageLifecycleIndex 建立 lifecycle 的 states 与动作索引。
func (c *Checker) pageLifecycleIndex() (map[string]bool, map[string]bool, map[string]map[string]bool) {
	ids := map[string]bool{}
	actions := map[string]bool{}
	states := map[string]map[string]bool{}
	schema, err := c.loadSchemaByType("cap.lifecycles")
	if err != nil {
		return ids, actions, states
	}
	for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		id, _ := doc["id"].(string)
		if id == "" {
			continue
		}
		ids[id] = true
		statesObj, _ := asMap(doc["states"])
		set := map[string]bool{}
		for _, raw := range asList(statesObj["values"]) {
			if name, _ := raw.(string); name != "" {
				set[name] = true
			}
		}
		states[id] = set
		if _, ok := asMap(doc["create"]); ok {
			actions[id+".create"] = true
		}
		if _, ok := asMap(doc["destroy"]); ok {
			actions[id+".destroy"] = true
		}
		for _, raw := range asList(doc["transitions"]) {
			item, _ := asMap(raw)
			if name, _ := item["name"].(string); name != "" {
				actions[id+".transitions."+name] = true
			}
		}
	}
	return ids, actions, states
}

// pageServiceIndex 建立 cap.services.<file>.<service> 索引。
func (c *Checker) pageServiceIndex() map[string]bool {
	ids := map[string]bool{}
	schema, err := c.loadSchemaByType("cap.services")
	if err != nil {
		return nil
	}
	for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["services"]) {
			item, _ := asMap(raw)
			if name, _ := item["name"].(string); fileID != "" && name != "" {
				ids[fileID+"."+name] = true
			}
		}
	}
	return ids
}

// pageRouteIndex 建立 feature.routes.* 索引；路由文件路径从 contracts/index.yaml 的 feature.routes 条目读取。
func (c *Checker) pageRouteIndex() map[string]bool {
	ids := map[string]bool{}
	index, err := loadYAML(filepath.Join(c.root, "contracts/index.yaml"))
	if err != nil {
		return nil
	}
	routePath := ""
	for _, raw := range asList(index["entries"]) {
		item, _ := asMap(raw)
		if id, _ := item["id"].(string); id == "feature.routes" {
			routePath, _ = item["path"].(string)
			break
		}
	}
	if routePath == "" {
		return nil
	}
	doc, err := loadYAML(filepath.Join(c.root, "contracts", routePath))
	if err != nil {
		return nil
	}
	for _, raw := range asList(doc["routes"]) {
		item, _ := asMap(raw)
		if id, _ := item["id"].(string); id != "" {
			ids[id] = true
		}
	}
	return ids
}

// pageLifecycleID 取页面唯一的状态机上下文，供 state_not 校验。
func pageLifecycleID(doc map[string]any) string {
	if transitionUI, ok := asMap(doc["transition_ui"]); ok {
		if source, _ := transitionUI["source"].(string); source != "" {
			if id := lifecycleRefID(source); id != "" {
				return id
			}
		}
	}
	found := ""
	var walk func(any)
	walk = func(node any) {
		if found != "" {
			return
		}
		switch v := node.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "enum" || key == "transition" {
					if s, ok := child.(string); ok {
						if id := lifecycleRefID(s); id != "" {
							found = id
							return
						}
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(doc)
	return found
}

// lifecycleRefID 从 cap.lifecycles.<name>[.*] 取 cap.lifecycles.<name>。
func lifecycleRefID(ref string) string {
	parts := strings.Split(ref, ".")
	if len(parts) < 3 || parts[0] != "cap" || parts[1] != "lifecycles" {
		return ""
	}
	return strings.Join(parts[:3], ".")
}

func (c *Checker) checkPageNode(rel string, node any, lifecycleID string, lifecycleIDs, lifecycleActions map[string]bool, lifecycleStates map[string]map[string]bool, serviceIDs map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			switch key {
			case "capability":
				if s, ok := child.(string); ok && s != "" && serviceIDs != nil && !serviceIDs[s] {
					c.rulef("schema.ref", "%s: unknown capability %s", rel, s)
				}
			case "permission":
				if s, ok := child.(string); ok && s != "" && !c.permissionExists(s) {
					c.rulef("schema.ref", "%s: unknown permission %s", rel, s)
				}
			case "enum":
				if s, ok := child.(string); ok && strings.HasPrefix(s, "cap.lifecycles.") {
					c.checkLifecycleStatesRef(rel, s, lifecycleIDs)
				}
			case "transition":
				if s, ok := child.(string); ok && strings.HasPrefix(s, "cap.lifecycles.") {
					c.checkLifecycleActionRef(rel, s, lifecycleActions)
				}
			case "transitions":
				for _, raw := range asList(child) {
					if s, ok := raw.(string); ok && strings.HasPrefix(s, "cap.lifecycles.") {
						c.checkLifecycleActionRef(rel, s, lifecycleActions)
					}
				}
			case "state_not":
				for _, raw := range asList(child) {
					s, _ := raw.(string)
					if s == "" {
						continue
					}
					if lifecycleID == "" {
						c.rulef("schema.ref", "%s: state_not %s has no lifecycle context", rel, s)
						continue
					}
					if !lifecycleStates[lifecycleID][s] {
						c.rulef("schema.ref", "%s: unknown state %s of %s", rel, s, lifecycleID)
					}
				}
			case "source":
				if s, ok := child.(string); ok && strings.HasPrefix(s, "cap.lifecycles.") {
					parts := strings.Split(s, ".")
					if len(parts) != 4 || parts[3] != "transitions" || !lifecycleIDs[strings.Join(parts[:3], ".")] {
						c.rulef("schema.ref", "%s: invalid lifecycle source %s", rel, s)
					}
				}
			}
			c.checkPageNode(rel, child, lifecycleID, lifecycleIDs, lifecycleActions, lifecycleStates, serviceIDs)
		}
	case []any:
		for _, child := range v {
			c.checkPageNode(rel, child, lifecycleID, lifecycleIDs, lifecycleActions, lifecycleStates, serviceIDs)
		}
	}
}

func (c *Checker) checkLifecycleStatesRef(rel, ref string, lifecycleIDs map[string]bool) {
	parts := strings.Split(ref, ".")
	lifecycleID := strings.Join(parts[:3], ".")
	if !lifecycleIDs[lifecycleID] {
		c.rulef("schema.ref", "%s: unknown lifecycle %s", rel, lifecycleID)
		return
	}
	switch {
	case len(parts) == 3:
	case len(parts) == 4 && parts[3] == "states":
	case len(parts) == 5 && parts[3] == "states" && parts[4] == "values":
	default:
		c.rulef("schema.ref", "%s: invalid lifecycle states reference %s", rel, ref)
	}
}

func (c *Checker) checkLifecycleActionRef(rel, ref string, lifecycleActions map[string]bool) {
	if !lifecycleActions[ref] {
		c.rulef("schema.ref", "%s: unknown lifecycle action %s", rel, ref)
	}
}
