// pages.go 校验 feature.pages 对 lifecycle / service / route 的结构化引用。
//
// 顶层与 map/list 槽位结构由 _meta/feature.pages.yaml 的 validateDoc 校验；
// 这里只做跨契约引用：enum / transition / transitions / state_not /
// transition_ui.source / capability / route。
package check

import (
	"path/filepath"
	"regexp"
	"strings"
)

func (c *Checker) checkFeaturePages() {
	schema, err := c.loadSchemaByType("feature.pages")
	if err != nil {
		c.rulef("schema.load", "load feature.pages schema: %v", err)
		return
	}
	c.checkTargets(schema)
	c.checkRules(schema)
	lifecycleIDs, lifecycleActions, lifecycleStates := c.pageLifecycleIndex()
	serviceIDs := c.pageServiceIndex()
	routeIDs := c.pageRouteIndex()
	eventNames := c.analyticsEventNames()
	c.pageParamTypes = c.buildPageParamTypes()
	c.pageTypeFamilies = c.loadPageTypeFamilies()
	for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkTargetFileName(schema, filepath.Base(file), rel)
		c.checkStability(doc, rel)
		c.checkPageViewStates(rel, doc)
		c.checkPageLimits(rel, doc)
		stem := strings.TrimSuffix(filepath.Base(file), ".yaml")
		id, _ := doc["id"].(string)
		if want := schema.Targets.FileID(filepath.Base(file)); id != want {
			c.rulef("page-structure", "%s: id %q must be %q", rel, id, want)
		}
		if page, _ := doc["page"].(string); page != stem {
			c.rulef("page-structure", "%s: page %q must be %q", rel, page, stem)
		}
		if route, _ := doc["route"].(string); route != "" && routeIDs != nil && !routeIDs[route] {
			c.rulef("page-refs", "%s: unknown route %s", rel, route)
		}
		lifecycleID := pageLifecycleID(doc)
		c.checkPageTransitionUI(rel, doc, lifecycleIDs)
		c.checkPageAnalytics(rel, doc, eventNames)
		c.checkPageNode(rel, doc, lifecycleID, lifecycleIDs, lifecycleActions, lifecycleStates, serviceIDs, routeIDs)
	}
}

// analyticsEventNames 返回 platform.analytics.events 声明的全部事件名。
func (c *Checker) analyticsEventNames() map[string]bool {
	doc, err := loadYAML(filepath.Join(c.root, "contracts/platform/analytics/events.yaml"))
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, raw := range asList(doc["events"]) {
		item, _ := asMap(raw)
		if name, _ := item["name"].(string); name != "" {
			out[name] = true
		}
	}
	return out
}

// checkPageTransitionUI 校验 transition_ui.source 指向已声明的 lifecycle。
func (c *Checker) checkPageTransitionUI(rel string, doc map[string]any, lifecycleIDs map[string]bool) {
	ui, _ := asMap(doc["transition_ui"])
	if ui == nil {
		return
	}
	source, _ := ui["source"].(string)
	if source == "" || lifecycleIDs == nil {
		return
	}
	id := lifecycleRefID(source)
	if id == "" || !lifecycleIDs[id] {
		c.rulef("page-refs", "%s: unknown lifecycle %s in transition_ui.source", rel, source)
	}
}

// checkPageAnalytics 校验页面埋点引用已声明的事件名。
func (c *Checker) checkPageAnalytics(rel string, doc map[string]any, eventNames map[string]bool) {
	analytics, _ := asMap(doc["analytics"])
	if analytics == nil || eventNames == nil {
		return
	}
	if name, _ := analytics["page_view"].(string); name != "" && !eventNames[name] {
		c.rulef("page-refs", "%s: unknown analytics event %s", rel, name)
	}
	actions, _ := asMap(analytics["actions"])
	for key, raw := range actions {
		name, _ := raw.(string)
		if name != "" && !eventNames[name] {
			c.rulef("page-refs", "%s: analytics.actions.%s references unknown event %s", rel, key, name)
		}
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
			c.rulef("page-structure", "%s: view_states missing %s", rel, key)
		}
	}
	if _, ok := states["empty"]; !ok {
		if _, ok := states["idle"]; !ok {
			c.rulef("page-structure", "%s: view_states requires empty or idle", rel)
		}
	}
}

// checkPageLimits 校验页面数值边界的顺序关系。
func (c *Checker) checkPageLimits(rel string, doc map[string]any) {
	if p, ok := asMap(doc["pagination"]); ok {
		if def, ok := toInt(p["limit_default"]); ok {
			if max, ok := toInt(p["limit_max"]); ok && def > max {
				c.rulef("page-limits", "%s: pagination.limit_default %d must be <= limit_max %d", rel, def, max)
			}
		}
	}
	for i, raw := range asList(doc["fields"]) {
		f, _ := asMap(raw)
		if min, ok := toInt(f["minLength"]); ok {
			if max, ok := toInt(f["maxLength"]); ok && min > max {
				c.rulef("page-limits", "%s: fields[%d].minLength %d must be <= maxLength %d", rel, i, min, max)
			}
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
		for _, name := range pageLifecycleStateValues(statesObj["values"]) {
			set[name] = true
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

// checkPageParams 校验 data/actions.params 的 key 与类型来自 service 参数或对象字段。
func (c *Checker) checkPageParams(rel, service string, node map[string]any) {
	params, _ := asMap(node["params"])
	if params == nil {
		return
	}
	fields := c.pageParamTypes[service]
	for key, raw := range params {
		p, _ := asMap(raw)
		fieldType, ok := fields[key]
		if !ok {
			c.rulef("page-params", "%s: %s.%s is not a parameter or field of %s", rel, service, key, service)
			continue
		}
		pageType, _ := p["type"].(string)
		if pageType != "" && fieldType != "" && !c.pageTypeCompatible(pageType, fieldType) {
			c.rulef("page-params", "%s: %s.%s type %s does not match service type %s", rel, service, key, pageType, fieldType)
		}
		if rawEnum, ok := p["enum"]; ok {
			switch v := rawEnum.(type) {
			case string:
				if v == "" {
					c.rulef("page-params", "%s: %s.%s enum must not be empty", rel, service, key)
				}
			case []any:
				if len(v) == 0 {
					c.rulef("page-params", "%s: %s.%s enum list must not be empty", rel, service, key)
				}
				for _, item := range v {
					if s, _ := item.(string); s == "" {
						c.rulef("page-params", "%s: %s.%s enum list items must be non-empty strings", rel, service, key)
						break
					}
				}
			default:
				c.rulef("page-params", "%s: %s.%s enum must be a reference string or string list", rel, service, key)
			}
		}
		if _, hasRequired := p["required"]; hasRequired {
			if _, hasOptional := p["optional"]; hasOptional {
				c.rulef("page-params", "%s: %s.%s must not declare both required and optional", rel, service, key)
			}
		}
	}
}

func (c *Checker) pageTypeCompatible(pageType, serviceType string) bool {
	pageType = c.pageBaseType(pageType)
	serviceType = c.pageBaseType(serviceType)
	if pageType == serviceType {
		return true
	}
	if strings.HasPrefix(serviceType, "data.enums.") && pageType == "_types.basics.string" {
		return true
	}
	return false
}

func (c *Checker) pageBaseType(t string) string {
	name, ok := strings.CutPrefix(t, "_types.basics.")
	if !ok {
		return t
	}
	if family := c.pageTypeFamilies[name]; family != "" {
		return "_types.basics." + family
	}
	return t
}

// loadPageTypeFamilies 读取 data.types.custom_types 的 family。
func (c *Checker) loadPageTypeFamilies() map[string]string {
	out := map[string]string{}
	doc, err := loadYAML(filepath.Join(c.root, "contracts/data/types.yaml"))
	if err != nil {
		return out
	}
	custom, _ := asMap(doc["custom_types"])
	for name, raw := range custom {
		item, _ := asMap(raw)
		if family, _ := item["family"].(string); family != "" {
			out[name] = family
		}
	}
	return out
}

// buildPageParamTypes 建立 service → 参数/对象字段 → 类型索引。
func (c *Checker) buildPageParamTypes() map[string]map[string]string {
	out := map[string]map[string]string{}
	schema, err := c.loadSchemaByType("cap.services")
	if err != nil {
		return out
	}
	for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		fileID, _ := doc["id"].(string)
		schemas, _ := asMap(doc["schemas"])
		for _, raw := range asList(doc["services"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			fields := map[string]string{}
			for _, rawParam := range asList(item["params"]) {
				pm, _ := asMap(rawParam)
				pname, _ := pm["name"].(string)
				if pname == "" || pname == "ctx" {
					continue
				}
				ptype, _ := pm["type"].(string)
				fields[pname] = ptype
				for field, typ := range objectFields(schemas, ptype) {
					if _, exists := fields[field]; !exists {
						fields[field] = typ
					}
				}
			}
			out[fileID+"."+name] = fields
		}
	}
	return out
}

// objectFields 解析 cap.services.<file>.schemas.<Type> 的 properties。
func objectFields(schemas map[string]any, typ string) map[string]string {
	out := map[string]string{}
	parts := strings.Split(typ, ".")
	if len(parts) < 5 || parts[0] != "cap" || parts[1] != "services" || parts[3] != "schemas" {
		return out
	}
	schema, _ := asMap(schemas[parts[4]])
	if schema == nil {
		return out
	}
	for _, raw := range asList(schema["properties"]) {
		p, _ := asMap(raw)
		name, _ := p["name"].(string)
		if name == "" {
			continue
		}
		if s, _ := asMap(p["schema"]); s != nil {
			if t, _ := s["type"].(string); t != "" {
				out[name] = t
			} else if r, _ := s["ref"].(string); r != "" {
				out[name] = r
			}
		}
	}
	return out
}

// checkPageActionTarget 校验 on_success 的导航函数与目标路由。
func (c *Checker) checkPageActionTarget(rel, value string, routeIDs map[string]bool) {
	re := regexp.MustCompile(`^(navigate|close_and_redirect)\((feature\.routes\.[a-z][a-z0-9_]*)\)$`)
	m := re.FindStringSubmatch(value)
	if m == nil {
		c.rulef("page-action-target", "%s: on_success %q must be navigate(feature.routes.*) or close_and_redirect(feature.routes.*)", rel, value)
		return
	}
	if routeIDs != nil && !routeIDs[m[2]] {
		c.rulef("page-action-target", "%s: on_success target %s is not a declared route", rel, m[2])
	}
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

func (c *Checker) checkPageNode(rel string, node any, lifecycleID string, lifecycleIDs, lifecycleActions map[string]bool, lifecycleStates map[string]map[string]bool, serviceIDs map[string]bool, routeIDs map[string]bool) {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			switch key {
			case "capability":
				if s, ok := child.(string); ok && s != "" {
					if serviceIDs != nil && !serviceIDs[s] {
						c.rulef("page-refs", "%s: unknown capability %s", rel, s)
					}
					c.checkPageParams(rel, s, v)
				}
			case "on_success":
				if s, ok := child.(string); ok && s != "" {
					c.checkPageActionTarget(rel, s, routeIDs)
				}
			case "permission":
				if s, ok := child.(string); ok && s != "" && !c.permissionExists(s) {
					c.rulef("page-refs", "%s: unknown permission %s", rel, s)
				}
			case "enum":
				if s, ok := child.(string); ok {
					switch {
					case strings.HasPrefix(s, "cap.lifecycles."):
						c.checkLifecycleStatesRef(rel, s, lifecycleIDs)
					case strings.HasPrefix(s, "data.enums."):
						if !c.enumIDs[s] {
							c.rulef("page-refs", "%s: unknown enum %s", rel, s)
						}
					default:
						c.rulef("page-refs", "%s: enum %q must be a data.enums.* or cap.lifecycles.* reference", rel, s)
					}
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
						c.rulef("page-refs", "%s: state_not %s has no lifecycle context", rel, s)
						continue
					}
					if !lifecycleStates[lifecycleID][s] {
						c.rulef("page-refs", "%s: unknown state %s of %s", rel, s, lifecycleID)
					}
				}
			case "source":
				if s, ok := child.(string); ok && strings.HasPrefix(s, "cap.lifecycles.") {
					parts := strings.Split(s, ".")
					if len(parts) != 4 || parts[3] != "transitions" || !lifecycleIDs[strings.Join(parts[:3], ".")] {
						c.rulef("page-refs", "%s: invalid lifecycle source %s", rel, s)
					}
				}
			}
			c.checkPageNode(rel, child, lifecycleID, lifecycleIDs, lifecycleActions, lifecycleStates, serviceIDs, routeIDs)
		}
	case []any:
		for _, child := range v {
			c.checkPageNode(rel, child, lifecycleID, lifecycleIDs, lifecycleActions, lifecycleStates, serviceIDs, routeIDs)
		}
	}
}

func pageLifecycleStateValues(raw any) []string {
	var out []string
	for _, item := range asList(raw) {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
			continue
		}
		if m, ok := asMap(item); ok {
			if v, _ := m["value"].(string); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

func (c *Checker) checkLifecycleStatesRef(rel, ref string, lifecycleIDs map[string]bool) {
	parts := strings.Split(ref, ".")
	lifecycleID := strings.Join(parts[:3], ".")
	if !lifecycleIDs[lifecycleID] {
		c.rulef("page-refs", "%s: unknown lifecycle %s", rel, lifecycleID)
		return
	}
	switch {
	case len(parts) == 3:
	case len(parts) == 4 && parts[3] == "states":
	case len(parts) == 5 && parts[3] == "states" && parts[4] == "values":
	default:
		c.rulef("page-refs", "%s: invalid lifecycle states reference %s", rel, ref)
	}
}

func (c *Checker) checkLifecycleActionRef(rel, ref string, lifecycleActions map[string]bool) {
	if !lifecycleActions[ref] {
		c.rulef("page-refs", "%s: unknown lifecycle action %s", rel, ref)
	}
}
