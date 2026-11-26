// flows.go 校验 feature.flows：跨页步骤、页面动作、内嵌规则与场景绑定。
package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/ruletests"
)

// checkFeatureFlows 校验流程结构，并建立 scenario 校验所需的 flow 索引。
func (c *Checker) checkFeatureFlows() {
	schema, err := c.loadSchemaByType("feature.flows")
	if err != nil {
		c.rulef("schema.load", "load feature.flows schema: %v", err)
		return
	}
	c.checkTargets(schema)
	c.checkRules(schema)
	c.flowIDs = map[string]bool{}
	c.flowPages = map[string]map[string]bool{}
	c.flowRules = map[string]map[string]bool{}
	c.flowStepActions = map[string]map[string]map[string]bool{}
	pages := c.pageActionIndex()
	routes := c.pageRouteIndex()
	scenarioRules := c.scenarioRuleIndex()
	for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkTargetFileName(schema, filepath.Base(file), rel)
		c.checkStability(doc, rel)
		fileID, _ := doc["id"].(string)
		if want := schema.Targets.FileID(filepath.Base(file)); fileID != want {
			c.rulef("flow-refs", "%s: id %q must be %q", rel, fileID, want)
		}
		if fileID == "" {
			continue
		}
		c.flowIDs[fileID] = true
		c.flowPages[fileID] = map[string]bool{}
		c.flowStepActions[fileID] = map[string]map[string]bool{}
		rules := map[string]bool{}
		for _, raw := range asList(doc["rules"]) {
			item, _ := asMap(raw)
			if id, _ := item["id"].(string); id != "" {
				rules[id] = true
			}
		}
		c.flowRules[fileID] = rules
		c.checkFlowRuleVerification(rel, doc, scenarioRules)

		scope, _ := doc["scope"].(string)
		steps := asList(doc["steps"])
		effectiveScope := scope
		if effectiveScope == "" {
			if len(steps) > 0 {
				effectiveScope = "ui"
			} else {
				effectiveScope = "api"
			}
		}
		entry, _ := doc["entry"].(string)
		switch effectiveScope {
		case "ui":
			if entry == "" {
				c.rulef("flow-structure", "%s: ui flow requires entry", rel)
			}
			if len(steps) == 0 {
				c.rulef("flow-structure", "%s: ui flow requires steps", rel)
			}
		case "api":
			if entry != "" {
				c.rulef("flow-structure", "%s: api flow must not declare entry", rel)
			}
			if len(steps) > 0 {
				c.rulef("flow-structure", "%s: api flow must not declare steps", rel)
			}
		}
		if entry != "" && routes != nil && !routes[entry] {
			c.rulef("flow-refs", "%s: unknown entry route %s", rel, entry)
		}

		stepIDs := map[string]bool{}
		for _, raw := range steps {
			step, _ := asMap(raw)
			if id, _ := step["id"].(string); id != "" {
				if stepIDs[id] {
					c.rulef("flow-step", "%s: duplicate step id %s", rel, id)
				}
				stepIDs[id] = true
			}
		}
		usedRules := map[string]bool{}
		for i, raw := range steps {
			step, _ := asMap(raw)
			where := fmt.Sprintf("%s.steps[%d]", rel, i)
			sid, _ := step["id"].(string)
			page, _ := step["page"].(string)
			action, _ := step["action"].(string)
			if page == "" {
				c.rulef("flow-step", "%s: page is required", where)
			} else if pages == nil || pages[page] == nil {
				c.rulef("flow-step", "%s: unknown page %s", where, page)
			} else if action == "" || !pages[page][action] {
				c.rulef("flow-step", "%s: action %q is not declared on page %s", where, action, page)
			}
			stepRules := asList(step["rules"])
			if len(stepRules) == 0 {
				c.rulef("flow-step", "%s: step %s requires at least one rule", where, sid)
			}
			for _, rawRule := range stepRules {
				rid, _ := rawRule.(string)
				if rid == "" {
					continue
				}
				if !rules[rid] {
					c.rulef("flow-step", "%s: rule %s is not declared in flow %s", where, rid, fileID)
				}
				usedRules[rid] = true
			}
			if next, _ := step["next"].(string); next != "" && next != "done" && !stepIDs[next] {
				c.rulef("flow-step", "%s: next step %s does not exist", where, next)
			}
			if success, _ := step["on_success"].(string); success != "" && routes != nil && !routes[success] {
				c.rulef("flow-step", "%s: unknown on_success route %s", where, success)
			}
			if page != "" {
				c.flowPages[fileID][page] = true
				if c.flowStepActions[fileID][page] == nil {
					c.flowStepActions[fileID][page] = map[string]bool{}
				}
				if action != "" {
					c.flowStepActions[fileID][page][action] = true
				}
			}
		}
		if effectiveScope == "ui" {
			for rid := range rules {
				if !usedRules[rid] {
					c.rulef("flow-rule-usage", "%s: rule %s is not referenced by any step", rel, rid)
				}
			}
		}
	}
	c.checkPageRuleGuards()
}

// scenarioRuleIndex 建立 scenario item ID → 显式覆盖的规则集合。
func (c *Checker) scenarioRuleIndex() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, file := range walkYAML(c.root, "contracts/features/scenarios") {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["scenarios"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if fileID == "" || name == "" {
				continue
			}
			set := map[string]bool{}
			for _, rawRule := range asList(item["rules"]) {
				if ruleID, _ := rawRule.(string); ruleID != "" {
					set[ruleID] = true
				}
			}
			out[fileID+"."+name] = set
		}
	}
	return out
}

// checkFlowRuleVerification 强制 policy/invariant 与 scenario 或测试文件关联。
func (c *Checker) checkFlowRuleVerification(rel string, doc map[string]any, scenarioRules map[string]map[string]bool) {
	// contract-new 等局部夹具只拷贝 contracts/，没有测试工具链；与 enforcement gate 一样跳过。
	if _, err := os.Stat(filepath.Join(c.root, "scripts/cmd")); err != nil {
		return
	}
	for _, raw := range asList(doc["rules"]) {
		item, _ := asMap(raw)
		kind, _ := item["kind"].(string)
		if kind != "policy" && kind != "invariant" {
			continue
		}
		ruleID, _ := item["id"].(string)
		verified := asList(item["verified_by"])
		if len(verified) == 0 {
			c.rulef("flow-rule-verified", "%s: %s %s requires verified_by (scenario or test file)", rel, kind, ruleID)
			continue
		}
		for _, rawRef := range verified {
			ref, _ := rawRef.(string)
			switch {
			case strings.HasPrefix(ref, "scenario.http."):
				if !scenarioRules[ref][ruleID] {
					c.rulef("flow-rule-verified", "%s: %s %s is not listed in %s.rules", rel, kind, ruleID, ref)
				}
			case strings.HasPrefix(ref, "scripts/cmd/internal/check/"):
				b, err := os.ReadFile(filepath.Join(c.root, ref))
				if err != nil {
					c.rulef("flow-rule-verified", "%s: %s %s references missing test file %s", rel, kind, ruleID, ref)
					continue
				}
				registry := ""
				switch kind {
				case "policy":
					registry = "policyExpectations"
				case "invariant":
					registry = "invariantAssertions"
				}
				if registry != "" {
					if keys, ok := registryKeys(string(b), registry); ok {
						if keys[ruleID] {
							continue
						}
						c.rulef("flow-rule-verified", "%s: %s %s is not a key in %s (%s)", rel, kind, ruleID, registry, ref)
						continue
					}
				}
				refs := ruletests.RefsIn(string(b), []string{ruleID})
				if refs[ruleID].Asserted == 0 && refs[ruleID].Dynamic == 0 {
					c.rulef("flow-rule-verified", "%s: %s %s is not asserted by test file %s (use %s or mustContainRule)", rel, kind, ruleID, ref, registry)
				}
			default:
				c.rulef("flow-rule-verified", "%s: %s %s has invalid verified_by %s", rel, kind, ruleID, ref)
			}
		}
	}
}

// registryKeys 读取测试文件中的 map[string]... 变量键集合，用于确认规则确实进入行为注册表。
func registryKeys(content, varName string) (map[string]bool, bool) {
	f, err := parser.ParseFile(token.NewFileSet(), "test.go", content, 0)
	if err != nil {
		return nil, false
	}
	var keys map[string]bool
	ast.Inspect(f, func(n ast.Node) bool {
		decl, ok := n.(*ast.GenDecl)
		if !ok || decl.Tok != token.VAR {
			return true
		}
		for _, spec := range decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != varName || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					continue
				}
				keys = map[string]bool{}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					basic, ok := kv.Key.(*ast.BasicLit)
					if !ok || basic.Kind != token.STRING {
						continue
					}
					keys[strings.Trim(basic.Value, `"`)] = true
				}
			}
		}
		return true
	})
	return keys, keys != nil
}

// pageActionIndex 建立 feature.pages.<id> → 动作名集合。
func (c *Checker) pageActionIndex() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	schema, err := c.loadSchemaByType("feature.pages")
	if err != nil {
		return nil
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
		set := map[string]bool{}
		actions, _ := asMap(doc["actions"])
		for name := range actions {
			set[name] = true
		}
		out[id] = set
	}
	return out
}

// checkPageRuleGuards 校验页面动作的 available_when.rules 已绑定到对应流程步骤。
func (c *Checker) checkPageRuleGuards() {
	schema, err := c.loadSchemaByType("feature.pages")
	if err != nil {
		return
	}
	for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		pageID, _ := doc["id"].(string)
		actions, _ := asMap(doc["actions"])
		for name, raw := range actions {
			action, _ := asMap(raw)
			guard, _ := asMap(action["available_when"])
			for _, rawRule := range asList(guard["rules"]) {
				ruleID, _ := rawRule.(string)
				if ruleID == "" {
					continue
				}
				found := false
				for flowID, byPage := range c.flowStepActions {
					if byPage[pageID][name] && c.flowRules[flowID][ruleID] {
						found = true
						break
					}
				}
				if !found {
					c.rulef("page-action-lifecycle", "%s: actions.%s available_when.rule %s is not bound to a flow step for page %s action %s", rel, name, ruleID, pageID, name)
				}
			}
		}
	}
}

// checkPageActionLifecycle 校验动作的错误码、刷新 key 与规则引用。
func (c *Checker) checkPageActionLifecycle(rel string, doc map[string]any) {
	dataKeys := map[string]bool{}
	data, _ := asMap(doc["data"])
	for key := range data {
		dataKeys[key] = true
	}
	actions, _ := asMap(doc["actions"])
	for name, raw := range actions {
		action, _ := asMap(raw)
		guard, _ := asMap(action["available_when"])
		for _, rawRule := range asList(guard["rules"]) {
			ruleID, _ := rawRule.(string)
			if ruleID != "" && !c.ruleIDSet[ruleID] {
				c.rulef("page-action-lifecycle", "%s: actions.%s available_when.rule %s does not exist", rel, name, ruleID)
			}
		}
		for _, rawErr := range asList(action["on_error"]) {
			item, _ := asMap(rawErr)
			code, _ := item["error"].(string)
			if code != "" && !c.errorCodeDeclared(code) {
				c.rulef("page-action-lifecycle", "%s: actions.%s on_error references unknown error %s", rel, name, code)
			}
		}
		for _, rawKey := range asList(action["invalidates"]) {
			key, _ := rawKey.(string)
			if key != "" && !dataKeys[key] {
				c.rulef("page-action-lifecycle", "%s: actions.%s invalidates unknown data key %s", rel, name, key)
			}
		}
	}
}

// pageFieldSources 汇总页面字段允许来源：service 返回字段、params 的 from 取值。
// queries 单独返回，供 filters 只接受 query 参数。
func (c *Checker) pageFieldSources(doc map[string]any) (map[string]string, map[string]bool) {
	allowed := map[string]string{}
	queries := map[string]bool{}
	addParams := func(node map[string]any) {
		params, _ := asMap(node["params"])
		for _, raw := range params {
			param, _ := asMap(raw)
			from, _ := param["from"].(string)
			if from == "" {
				continue
			}
			prefix, rest, ok := strings.Cut(from, ".")
			if !ok || rest == "" {
				continue
			}
			leaf := rest
			if idx := strings.LastIndex(rest, "."); idx >= 0 {
				leaf = rest[idx+1:]
			}
			switch prefix {
			case "query":
				queries[leaf] = true
				allowed[leaf] = ""
			case "form", "row", "route", "device", "user", "page":
				allowed[leaf] = ""
			}
		}
	}
	data, _ := asMap(doc["data"])
	for _, raw := range data {
		item, _ := asMap(raw)
		capability, _ := item["capability"].(string)
		for name, sensitivity := range c.serviceResponseFields(capability) {
			allowed[name] = sensitivity
		}
		addParams(item)
	}
	actions, _ := asMap(doc["actions"])
	for _, raw := range actions {
		item, _ := asMap(raw)
		capability, _ := item["capability"].(string)
		for name, sensitivity := range c.serviceResponseFields(capability) {
			allowed[name] = sensitivity
		}
		addParams(item)
	}
	return allowed, queries
}

// checkPageFieldRefs 校验页面字段/筛选项来自 data/actions 的返回字段或取值来源。
func (c *Checker) checkPageFieldRefs(rel string, doc map[string]any) {
	allowed, queries := c.pageFieldSources(doc)
	if len(allowed) == 0 {
		return
	}
	for i, raw := range asList(doc["fields"]) {
		field, _ := asMap(raw)
		name, _ := field["name"].(string)
		if name == "" {
			continue
		}
		sensitivity, ok := allowed[name]
		if !ok {
			c.rulef("page-field-refs", "%s: fields[%d] %s is not provided by any data/action source", rel, i, name)
			continue
		}
		if sensitivity == "secret" {
			secret, _ := field["secret"].(bool)
			if !secret {
				c.rulef("page-field-sensitivity", "%s: fields[%d] %s is secret; only an input field with secret: true is allowed", rel, i, name)
			}
		}
	}
	for i, raw := range asList(doc["filters"]) {
		filter, _ := asMap(raw)
		name, _ := filter["name"].(string)
		if name == "" {
			continue
		}
		if !queries[name] {
			c.rulef("page-field-refs", "%s: filters[%d] %s is not a query parameter in data/actions", rel, i, name)
		}
	}
}

// serviceResponseFields 展开 service 返回类型中的字段名与敏感级别。
func (c *Checker) serviceResponseFields(serviceID string) map[string]string {
	out := map[string]string{}
	for _, rt := range c.methodReturns[serviceID] {
		c.collectTypeFields(rt.typ, out, map[string]bool{})
	}
	return out
}

func (c *Checker) collectTypeFields(typ string, out map[string]string, seen map[string]bool) {
	typ = strings.TrimPrefix(normalizeContractType(typ), "*")
	for strings.HasPrefix(typ, "List<") && strings.HasSuffix(typ, ">") {
		typ = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(typ, "List<"), ">"))
	}
	if strings.HasPrefix(typ, "Map<") && strings.HasSuffix(typ, ">") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(typ, "Map<"), ">"))
		if idx := strings.LastIndex(inner, ","); idx >= 0 {
			typ = strings.TrimSpace(inner[idx+1:])
		}
	}
	if typ == "" || seen[typ] {
		return
	}
	seen[typ] = true
	if strings.HasPrefix(typ, "data.entities.") {
		entity := c.entities[typ]
		if entity == nil {
			return
		}
		for _, field := range entity.Fields {
			name := field.JSONName
			if name == "" {
				name = snakeCase(field.Name)
			}
			out[name] = field.Sensitivity
			if field.IsRelation && field.Type != "" {
				c.collectTypeFields(field.Type, out, seen)
			}
		}
		return
	}
	if strings.HasPrefix(typ, "cap.services.") && strings.Contains(typ, ".schemas.") {
		def := c.methodTypeDefs[typ]
		if def == nil {
			return
		}
		for _, raw := range asList(def["properties"]) {
			prop, _ := asMap(raw)
			name, _ := prop["name"].(string)
			if name == "" {
				continue
			}
			out[name] = ""
			schema, _ := asMap(prop["schema"])
			if schema == nil {
				continue
			}
			if t, _ := schema["type"].(string); t != "" {
				c.collectTypeFields(t, out, seen)
			}
			if r, _ := schema["ref"].(string); r != "" {
				c.collectTypeFields(r, out, seen)
			}
		}
	}
}

// checkFeatureDataBoundary 强制只有 features/scenarios 可以引用 data 持久化对象。
func (c *Checker) checkFeatureDataBoundary() {
	for _, file := range walkYAML(c.root, "contracts/features") {
		rel := relPath(c.root, file)
		if strings.Contains(rel, "/scenarios/") {
			continue
		}
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		c.scanDataRefs(rel, "", doc)
	}
}

func (c *Checker) scanDataRefs(rel, path string, value any) {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if dataBoundaryProseKeys[key] {
				continue
			}
			c.scanDataRefs(rel, joinPath(path, key), child)
		}
	case []any:
		for i, child := range node {
			c.scanDataRefs(rel, fmt.Sprintf("%s[%d]", path, i), child)
		}
	case string:
		if strings.HasPrefix(node, "data.tables.") || strings.HasPrefix(node, "data.queries.") || node == "data.storage" {
			c.rulef("flow-data-boundary", "%s: %s references %s; only features/scenarios/** may reference data.*", rel, path, node)
		}
	}
}

var dataBoundaryProseKeys = map[string]bool{
	"assert": true, "description": true, "do_not": true, "given": true,
	"summary": true, "then": true, "when": true,
}
