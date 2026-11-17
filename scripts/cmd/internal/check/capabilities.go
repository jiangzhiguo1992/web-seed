// capabilities.go 校验 services / async / events / protocols 与 features(rules/states) 契约：
//   - services：visibility/steps/errors/参数映射/写归属
//   - async：trigger/retry/concurrency 与 handler service
//   - events：payload/版本/生产与消费锚点
//   - protocols：x-service/x-async 绑定、默认错误响应与组件引用
//   - features：规则结构与服务锚点、状态机穷举与转换
package check

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	openapispec "github.com/yourorg/web-seed/scripts/cmd/internal/spec"
)

type asyncMeta struct {
	triggerType string
}

type serviceMeta struct {
	module      string
	visibility  string
	params      map[string]bool
	paramTypes  map[string]bool
	typeSchemas map[string]map[string]any
	errors      map[string]bool
	returns     map[string]string
}

// stepKindFields 是每种 step kind 的专属字段；公共字段由 checkServiceItem 统一放行。
var allowedOperationExtensions = map[string]bool{
	"x-contract-id": true, "x-service": true, "x-async": true,
	"x-timeout-ms": true, "x-default-errors": true,
}

var stepKindFields = map[string][]string{
	"query":      {"query", "params"},
	"service":    {"service", "params"},
	"logic":      {"output", "queries", "events", "rules"},
	"transition": {"transitions", "output", "queries", "events", "rules"},
}

func (c *Checker) checkCapabilities() {
	serviceSchema, err := c.loadSchemaByType("cap.services")
	if err != nil {
		c.rulef("schema.load", "load cap.services schema: %v", err)
		return
	}
	ruleSchema, err := c.loadSchemaByType("feature.rules")
	if err != nil {
		c.rulef("schema.load", "load feature.rules schema: %v", err)
		return
	}
	stateSchema, err := c.loadSchemaByType("feature.states")
	if err != nil {
		c.rulef("schema.load", "load feature.states schema: %v", err)
		return
	}
	eventSchema, err := c.loadSchemaByType("cap.events")
	if err != nil {
		c.rulef("schema.load", "load cap.events schema: %v", err)
		return
	}
	httpSchema, err := c.loadSchemaByType("cap.protocols.http")
	if err != nil {
		c.rulef("schema.load", "load cap.protocols.http schema: %v", err)
		return
	}
	asyncSchema, err := c.loadSchemaByType("cap.async")
	if err != nil {
		c.rulef("schema.load", "load cap.async schema: %v", err)
		return
	}
	for _, s := range []*Schema{serviceSchema, ruleSchema, stateSchema, eventSchema, httpSchema, asyncSchema} {
		c.checkRules(s)
		c.checkTargets(s)
	}

	serviceFiles := glob(c.root, filepath.Join(serviceSchema.Targets.Dir, serviceSchema.Targets.Glob))
	typeNames := map[string]bool{}
	typeOwners := map[string]string{}
	c.methodTypeDefs = map[string]map[string]any{}
	c.methodReturns = map[string]map[string]string{}
	for _, file := range serviceFiles {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		types, _ := asMap(doc["schemas"])
		for name, raw := range types {
			if prev, ok := typeOwners[name]; ok && prev != rel {
				c.rulef("service-structure", "%s: type %s already defined in %s", rel, name, prev)
			}
			typeOwners[name] = rel
			typeNames[name] = true
			if def, ok := asMap(raw); ok {
				c.methodTypeDefs[name] = def
			}
		}
	}

	// pass 1：收集 service item 元数据与返回类型。
	serviceIDs := map[string]bool{}
	serviceEdges := map[string][]string{}
	serviceMetaByID := map[string]serviceMeta{}
	serviceItemIDs := map[string]bool{}
	for _, file := range serviceFiles {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		fileID, _ := doc["id"].(string)
		module, _ := doc["owner_module"].(string)
		for _, raw := range asList(doc["services"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			id := fileID + "." + name
			if serviceIDs[id] {
				c.rulef("service-id", "%s: duplicate service id %s", relPath(c.root, file), id)
			}
			serviceIDs[id] = true
			serviceItemIDs[id] = true
			visibility, _ := item["visibility"].(string)
			params := map[string]bool{}
			paramTypes := map[string]bool{}
			typeSchemas := map[string]map[string]any{}
			if types, ok := asMap(doc["schemas"]); ok {
				for name, raw := range types {
					if def, ok := asMap(raw); ok {
						typeSchemas[name] = def
					}
				}
			}
			for _, rawParam := range asList(item["params"]) {
				pm, _ := asMap(rawParam)
				pn, _ := pm["name"].(string)
				if pn != "" {
					params[pn] = true
				}
				pt, _ := pm["type"].(string)
				pt = strings.TrimPrefix(pt, "*")
				if strings.HasPrefix(pt, "List<") && strings.HasSuffix(pt, ">") {
					pt = strings.TrimSuffix(strings.TrimPrefix(pt, "List<"), ">")
				}
				if pt != "" {
					paramTypes[pt] = true
				}
			}
			errs := map[string]bool{}
			for _, rawErr := range asList(item["errors"]) {
				code, _ := rawErr.(string)
				errs[code] = true
			}
			returns := map[string]string{}
			for _, rawReturn := range asList(item["returns"]) {
				rm, _ := asMap(rawReturn)
				rn, _ := rm["name"].(string)
				rt, _ := rm["type"].(string)
				if rn != "" && rt != "" && rn != "err" {
					returns[rn] = rt
				}
			}
			c.methodReturns[id] = returns
			serviceMetaByID[id] = serviceMeta{module: module, visibility: visibility, params: params, paramTypes: paramTypes, typeSchemas: typeSchemas, errors: errs, returns: returns}
			for _, rawStep := range asList(item["steps"]) {
				step, _ := asMap(rawStep)
				if k, _ := step["kind"].(string); k == "service" {
					ref, _ := step["service"].(string)
					if ref != "" {
						serviceEdges[id] = append(serviceEdges[id], ref)
					}
				}
			}
		}
	}

	c.checkAccess(serviceItemIDs)

	// query 返回类型：供 kind=query 的 when 路径解析。
	c.queryReturnTypes = map[string]map[string]string{}
	for qid, q := range c.queries {
		m := map[string]string{}
		for _, r := range q.Returns {
			typ := r.Type
			switch r.Kind {
			case "entity":
				typ = r.Entity
			case "table":
				typ = r.Table
			case "scalar":
				typ = r.Type
			case "affected_rows":
				typ = "integer"
			case "column":
				typ = "string"
			}
			if typ == "" {
				typ = "any"
			}
			if r.List {
				typ = "List<" + typ + ">"
			}
			m[r.Name] = typ
		}
		c.queryReturnTypes[qid] = m
	}

	// pass 2：校验 services 文档。
	for _, file := range serviceFiles {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		c.checkServiceDoc(doc, relPath(c.root, file), serviceSchema, typeNames)
	}

	// rules / states / events 先收集 ID，再互相校验。
	ruleIDs := map[string]bool{}
	for _, file := range glob(c.root, filepath.Join(ruleSchema.Targets.Dir, ruleSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		for _, raw := range asList(doc["rules"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			if id == "" {
				continue
			}
			if ruleIDs[id] {
				c.rulef("rule-id-unique", "%s: duplicate rule id %s", relPath(c.root, file), id)
			}
			ruleIDs[id] = true
		}
	}
	eventIDs := map[string]bool{}
	for _, file := range glob(c.root, filepath.Join(eventSchema.Targets.Dir, eventSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		for _, raw := range asList(doc["events"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			if id == "" {
				continue
			}
			if eventIDs[id] {
				c.rulef("event-structure", "%s: duplicate event id %s", relPath(c.root, file), id)
			}
			eventIDs[id] = true
		}
	}
	transitionIDs := map[string]bool{}
	for _, file := range glob(c.root, filepath.Join(stateSchema.Targets.Dir, stateSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["transitions"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name != "" {
				transitionIDs[fileID+"."+name] = true
			}
		}
	}

	// pass 3：按 kind 回查 step 引用。
	for _, file := range serviceFiles {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		fileID, _ := doc["id"].(string)
		module, _ := doc["owner_module"].(string)
		for _, raw := range asList(doc["services"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			c.checkServiceItem(item, relPath(c.root, file), fileID+"."+name, module, serviceMetaByID, ruleIDs, eventIDs, transitionIDs, typeNames)
		}
	}

	c.checkServiceCycles(serviceEdges)

	// pass 4：rules / states / events。
	for _, file := range glob(c.root, filepath.Join(ruleSchema.Targets.Dir, ruleSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		c.checkRuleDoc(doc, relPath(c.root, file), ruleSchema, serviceItemIDs)
	}
	for _, file := range glob(c.root, filepath.Join(stateSchema.Targets.Dir, stateSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		c.checkStateDoc(doc, relPath(c.root, file), stateSchema, serviceItemIDs, eventIDs, ruleIDs)
	}
	for _, file := range glob(c.root, filepath.Join(eventSchema.Targets.Dir, eventSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		c.checkEventDoc(doc, relPath(c.root, file), eventSchema, serviceItemIDs)
	}
	asyncServices, asyncJobs := c.checkAsyncDoc(asyncSchema, serviceMetaByID, eventIDs)
	c.checkHTTPBindings(httpSchema, serviceMetaByID, typeNames, asyncServices, asyncJobs)
}

func (c *Checker) checkServiceDoc(doc map[string]any, rel string, schema *Schema, typeNames map[string]bool) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	c.checkTargetFileName(schema, filepath.Base(rel), rel)
	if s, _ := doc["summary"].(string); strings.TrimSpace(s) == "" {
		c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
	}
	fileID, _ := doc["id"].(string)
	want := schema.Targets.FileID(filepath.Base(rel))
	if fileID != want {
		c.rulef("service-id", "%s: id %q must be %q", rel, fileID, want)
	}
	if types, ok := asMap(doc["schemas"]); ok {
		c.checkTypeRefs(types, typeNames, rel)
		c.checkServiceTypes(types, rel)
	}
	seen := map[string]bool{}
	for _, raw := range asList(doc["services"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if seen[name] {
			c.rulef("service-structure", "%s: duplicate service %s", rel, name)
		}
		seen[name] = true
		if s, _ := item["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s.%s: summary must not be empty", rel, name)
		}
		params := asList(item["params"])
		if len(params) == 0 {
			c.rulef("service-structure", "%s.%s: params[0] must be ctx", rel, name)
		} else if first, _ := asMap(params[0]); first["name"] != "ctx" || first["type"] != "context.Context" {
			c.rulef("service-structure", "%s.%s: params[0] must be ctx context.Context", rel, name)
		}
		seenParams := map[string]bool{}
		for _, rawParam := range params {
			pm, _ := asMap(rawParam)
			if pn, _ := pm["name"].(string); pn != "" {
				if seenParams[pn] {
					c.rulef("service-structure", "%s.%s: duplicate param %s", rel, name, pn)
				}
				seenParams[pn] = true
			}
		}
		returns := asList(item["returns"])
		if len(returns) == 0 {
			c.rulef("service-structure", "%s.%s: returns must end with err", rel, name)
		} else if last, _ := asMap(returns[len(returns)-1]); last["name"] != "err" || last["type"] != "error" {
			c.rulef("service-structure", "%s.%s: returns must end with err error", rel, name)
		}
		seenReturns := map[string]bool{}
		for _, rawReturn := range returns {
			rm, _ := asMap(rawReturn)
			if rn, _ := rm["name"].(string); rn != "" {
				if seenReturns[rn] {
					c.rulef("service-structure", "%s.%s: duplicate return %s", rel, name, rn)
				}
				seenReturns[rn] = true
			}
		}
		for _, rawParam := range asList(item["params"]) {
			pm, _ := asMap(rawParam)
			c.checkTypeName(pm["type"], typeNames, rel+":"+name)
		}
		for _, rawReturn := range asList(item["returns"]) {
			rm, _ := asMap(rawReturn)
			c.checkTypeName(rm["type"], typeNames, rel+":"+name)
		}
		for _, rawErr := range asList(item["errors"]) {
			code, _ := rawErr.(string)
			if !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
				c.rulef("service-ref", "%s.%s: unknown error %s", rel, name, code)
			}
		}
	}
}

func (c *Checker) checkServiceItem(doc map[string]any, rel, id, module string, serviceMeta map[string]serviceMeta, ruleIDs, eventIDs map[string]bool, transitionIDs map[string]bool, typeNames map[string]bool) {
	steps := asList(doc["steps"])
	if len(steps) == 0 {
		c.rulef("service-structure", "%s: service requires steps", rel)
		return
	}
	stepOutputs := make([]map[string]string, len(steps))
	allStepIndex := map[string]int{}
	for i, raw := range steps {
		step, _ := asMap(raw)
		sid, _ := step["id"].(string)
		if sid != "" {
			if _, exists := allStepIndex[sid]; !exists {
				allStepIndex[sid] = i
			}
		}
	}
	stepIndex := map[string]int{}
	reachableByIdx := map[int]bool{}
	requiredErrors := map[string]bool{}
	hasBackwardGoto := false
	for i, raw := range steps {
		step, _ := asMap(raw)
		stepID, _ := step["id"].(string)
		if _, exists := stepIndex[stepID]; exists {
			c.rulef("service-structure", "%s: duplicate step id %s", rel, stepID)
		} else {
			stepIndex[stepID] = i
		}
		kind, _ := step["kind"].(string)
		if s, _ := step["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: step %s summary must not be empty", rel, stepID)
		}
		allowed := map[string]bool{
			"id": true, "kind": true, "summary": true, "depends_on": true,
			"when": true, "on_error": true, "errors": true,
		}
		for _, field := range stepKindFields[kind] {
			allowed[field] = true
		}
		for field := range step {
			if !allowed[field] {
				c.rulef("service-steps-kind", "%s: step %s kind=%s does not allow field %s", rel, stepID, kind, field)
			}
		}

		var targetErrors, queryErrors map[string]bool
		switch kind {
		case "query":
			qid, _ := step["query"].(string)
			if qid == "" {
				c.rulef("service-steps-kind", "%s: step %s kind=query requires query", rel, stepID)
			} else if q, ok := c.queries[qid]; !ok {
				c.rulef("service-ref", "%s: step %s unknown query %s", rel, stepID, qid)
			} else {
				queryErrors = q.errorSet()
				if refs, ok := asMap(step["params"]); ok {
					for name, rawExpr := range refs {
						found := false
						for _, p := range q.Params {
							if p.Name == name {
								found = true
								break
							}
						}
						if !found {
							c.rulef("service-steps-kind", "%s: step %s query %s has no param %s", rel, stepID, qid, name)
						}
						c.checkServiceParamExpr(rawExpr, doc, stepIndex, stepOutputs, steps, rel, stepID)
					}
				}
			}
		case "service":
			ref, _ := step["service"].(string)
			if ref == "" {
				c.rulef("service-steps-kind", "%s: step %s kind=service requires service", rel, stepID)
			} else if target, ok := serviceMeta[ref]; !ok {
				c.rulef("service-ref", "%s: step %s unknown service %s", rel, stepID, ref)
			} else {
				targetErrors = target.errors
				if target.module != module && target.visibility != "public" {
					c.rulef("service-visibility", "%s: step %s cannot call internal %s from another module", rel, stepID, ref)
				}
				if refs, ok := asMap(step["params"]); ok {
					for name, rawExpr := range refs {
						if !target.params[name] {
							c.rulef("service-steps-kind", "%s: step %s service %s has no param %s", rel, stepID, ref, name)
						}
						c.checkServiceParamExpr(rawExpr, doc, stepIndex, stepOutputs, steps, rel, stepID)
					}
				}
			}
		case "logic":
			if step["output"] == nil {
				c.rulef("service-steps-kind", "%s: step %s kind=logic requires output", rel, stepID)
			}
			if step["errors"] == nil {
				c.rulef("service-steps-kind", "%s: step %s kind=logic requires errors", rel, stepID)
			}
			c.checkLogicRefs(step, rel, stepID, ruleIDs, eventIDs)
		case "transition":
			trs := asList(step["transitions"])
			if len(trs) == 0 {
				c.rulef("service-steps-kind", "%s: step %s kind=transition requires transitions", rel, stepID)
			}
			for _, rawTr := range trs {
				tr, _ := rawTr.(string)
				if !transitionIDs[tr] {
					c.rulef("service-ref", "%s: step %s unknown transition %s", rel, stepID, tr)
				}
			}
			c.checkLogicRefs(step, rel, stepID, ruleIDs, eventIDs)
		default:
			c.rulef("service-structure", "%s: step %s kind must be query/service/logic/event/transition", rel, stepID)
		}

		stepOutput := map[string]string{}
		if step["output"] != nil {
			if kind == "logic" || kind == "transition" {
				seenOut := map[string]bool{}
				for _, rawOut := range asList(step["output"]) {
					om, _ := asMap(rawOut)
					name, _ := om["name"].(string)
					typ, _ := om["type"].(string)
					if seenOut[name] {
						c.rulef("service-structure", "%s: step %s duplicate output %s", rel, stepID, name)
					}
					seenOut[name] = true
					c.checkTypeName(om["type"], typeNames, rel+":"+stepID)
					if name != "" && typ != "" {
						stepOutput[name] = typ
					}
				}
			}
		}
		if kind == "logic" {
			for _, rawReturn := range asList(doc["returns"]) {
				rm, _ := asMap(rawReturn)
				rn, _ := rm["name"].(string)
				if rn == "" || rn == "err" {
					continue
				}
				if _, ok := stepOutput[rn]; !ok {
					c.rulef("service-output", "%s: step %s logic output missing return %s", rel, stepID, rn)
				}
			}
		}
		stepOutputs[i] = stepOutput

		reachable := true
		for _, rawDep := range asList(step["depends_on"]) {
			depID, _ := rawDep.(string)
			idx, ok := stepIndex[depID]
			if !ok {
				c.rulef("service-structure", "%s: step %s depends_on %s must reference an earlier step", rel, stepID, depID)
				reachable = false
				continue
			}
			if !reachableByIdx[idx] {
				reachable = false
			}
		}
		if when, ok := step["when"]; ok {
			s, _ := when.(string)
			if strings.TrimSpace(s) == "" {
				c.rulef("service-when", "%s: step %s when must not be empty", rel, stepID)
			} else if node, err := parseWhen(s); err != nil {
				c.rulef("service-when", "%s: step %s when %q: %v", rel, stepID, s, err)
			} else {
				if evalWhenNode(node) == whenFalse {
					reachable = false
				}
				formula := whenFormulaFromNode(node)
				for _, ref := range whenStepRefs(node) {
					if _, ok := stepIndex[ref]; !ok {
						c.rulef("service-when", "%s: step %s when references step %s that is not an earlier step", rel, stepID, ref)
					}
				}
				for _, field := range whenStepFields(node) {
					ref, path, ok := splitStepField(field)
					if !ok {
						continue
					}
					idx, ok := stepIndex[ref]
					if !ok {
						continue
					}
					target, _ := asMap(steps[idx])
					if err := c.checkStepOutputPath(target, stepOutputs[idx], path, formula, ref); err != nil {
						c.rulef("service-when", "%s: step %s when references %s: %v", rel, stepID, field, err)
					}
				}
				for _, field := range whenInputFields(node) {
					if err := c.checkInputPath(doc, field); err != nil {
						c.rulef("service-when", "%s: step %s when references %s: %v", rel, stepID, field, err)
					}
				}
			}
		}

		stepErrors := map[string]bool{}
		switch {
		case targetErrors != nil:
			if step["errors"] != nil {
				for _, rawErr := range asList(step["errors"]) {
					code, _ := rawErr.(string)
					if !targetErrors[code] {
						c.rulef("service-errors", "%s: step %s error %s is not declared by target service", rel, stepID, code)
					}
					stepErrors[code] = true
				}
			} else {
				for code := range targetErrors {
					stepErrors[code] = true
				}
			}
		case queryErrors != nil:
			if step["errors"] == nil {
				for code := range queryErrors {
					stepErrors[code] = true
				}
			} else {
				for _, rawErr := range asList(step["errors"]) {
					code, _ := rawErr.(string)
					if !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
						c.rulef("service-ref", "%s: step %s unknown error %s", rel, stepID, code)
					}
					stepErrors[code] = true
				}
				for code := range queryErrors {
					if !stepErrors[code] {
						c.rulef("service-errors", "%s: step %s does not declare query error %s", rel, stepID, code)
						stepErrors[code] = true
					}
				}
			}
		default:
			for _, rawErr := range asList(step["errors"]) {
				code, _ := rawErr.(string)
				if !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
					c.rulef("service-ref", "%s: step %s unknown error %s", rel, stepID, code)
				}
				stepErrors[code] = true
			}
		}
		if kind == "logic" || kind == "transition" {
			c.checkStepQueryErrors(step, rel, stepID)
		}

		handled := map[string]bool{}
		seenOnError := map[string]bool{}
		for _, rawErr := range asList(step["on_error"]) {
			entry, _ := asMap(rawErr)
			code, _ := entry["error"].(string)
			action, _ := entry["action"].(string)
			target, _ := entry["target"].(string)
			if !stepErrors[code] {
				c.rulef("service-control", "%s: step %s on_error %s is not declared by the step error set", rel, stepID, code)
			}
			if seenOnError[code] {
				c.rulef("service-control", "%s: step %s duplicate on_error for %s", rel, stepID, code)
			}
			seenOnError[code] = true
			switch action {
			case "abort":
				if target != "" {
					c.rulef("service-control", "%s: step %s on_error %s target is only allowed for goto", rel, stepID, code)
				}
			case "continue":
				if target != "" {
					c.rulef("service-control", "%s: step %s on_error %s target is only allowed for goto", rel, stepID, code)
				}
				handled[code] = true
			case "goto":
				if target == "" {
					c.rulef("service-control", "%s: step %s on_error %s goto requires target", rel, stepID, code)
				} else if targetIdx, ok := allStepIndex[target]; !ok {
					c.rulef("service-control", "%s: step %s on_error %s goto target %s not found", rel, stepID, code, target)
				} else if targetIdx <= i {
					hasBackwardGoto = true
				}
				handled[code] = true
			default:
				c.rulef("service-control", "%s: step %s on_error %s action must be abort, continue or goto", rel, stepID, code)
			}
		}
		if reachable {
			for code := range stepErrors {
				if !handled[code] {
					requiredErrors[code] = true
				}
			}
		}
		reachableByIdx[i] = reachable
	}

	if rawMax, ok := doc["max_iterations"]; ok {
		budget, ok := toInt(rawMax)
		if !ok || budget < 1 {
			c.rulef("service-control", "%s: max_iterations must be >= 1", rel)
		}
	}
	if hasBackwardGoto && doc["max_iterations"] == nil {
		c.rulef("service-control", "%s: backward/self goto requires service.max_iterations", rel)
	}
	req := c.serviceLevelErrors(id, hasBackwardGoto)
	for code := range req {
		requiredErrors[code] = true
	}
	allowedErrors := map[string]bool{}
	for code := range requiredErrors {
		allowedErrors[code] = true
	}
	serviceErrors := map[string]bool{}
	for _, raw := range asList(doc["errors"]) {
		code, _ := raw.(string)
		serviceErrors[code] = true
		if !allowedErrors[code] {
			c.rulef("service-errors", "%s: error %s is not produced by a reachable, unhandled step", rel, code)
		}
	}
	for code := range requiredErrors {
		if !serviceErrors[code] {
			if req[code] {
				c.rulef("service-errors", "%s: missing service-level error %s", rel, code)
			} else {
				c.rulef("service-errors", "%s: missing error %s produced by a reachable, unhandled step", rel, code)
			}
		}
	}
}

// checkStepQueryErrors 要求 logic/transition step 显式覆盖其 queries 的领域错误。
func (c *Checker) checkStepQueryErrors(step map[string]any, rel, stepID string) {
	declared := map[string]bool{}
	for _, raw := range asList(step["errors"]) {
		if code, _ := raw.(string); code != "" {
			declared[code] = true
		}
	}
	seen := map[string]bool{}
	for _, raw := range asList(step["queries"]) {
		qid, _ := raw.(string)
		q := c.queries[qid]
		if q == nil {
			continue
		}
		for _, code := range q.Errors {
			if seen[code] {
				continue
			}
			seen[code] = true
			if !declared[code] {
				c.rulef("service-errors", "%s: step %s does not declare query %s error %s", rel, stepID, qid, code)
			}
		}
	}
}

func (c *Checker) checkLogicRefs(step map[string]any, rel, stepID string, ruleIDs, eventIDs map[string]bool) {
	for _, rawQ := range asList(step["queries"]) {
		qid, _ := rawQ.(string)
		if _, ok := c.queries[qid]; !ok {
			c.rulef("service-ref", "%s: step %s unknown query %s", rel, stepID, qid)
		}
	}
	for _, rawRule := range asList(step["rules"]) {
		rid, _ := rawRule.(string)
		if !ruleIDs[rid] {
			c.rulef("service-ref", "%s: step %s unknown rule %s", rel, stepID, rid)
		}
	}
	for _, rawEvent := range asList(step["events"]) {
		ev, _ := rawEvent.(string)
		if !eventIDs[ev] {
			c.rulef("service-ref", "%s: step %s unknown event %s", rel, stepID, ev)
		}
	}
}

func (c *Checker) checkRuleDoc(doc map[string]any, rel string, schema *Schema, serviceIDs map[string]bool) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	fileID, _ := doc["id"].(string)
	want := schema.Targets.FileID(filepath.Base(rel))
	if fileID != want {
		c.rulef("rule-id-unique", "%s: id %q must be %q", rel, fileID, want)
	}
	seen := map[string]bool{}
	for _, raw := range asList(doc["rules"]) {
		item, _ := asMap(raw)
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		if seen[id] {
			c.rulef("rule-id-unique", "%s: duplicate rule id %s", rel, id)
		}
		seen[id] = true
		if !strings.HasPrefix(id, fileID+".") {
			c.rulef("rule-id-unique", "%s: rule id %s must start with %s.", rel, id, fileID)
		}
		kind, _ := item["kind"].(string)
		switch kind {
		case "constraint":
			if item["field"] == nil || item["op"] == nil || item["value"] == nil {
				c.rulef("rule-structure", "%s: constraint %s requires field/op/value", rel, id)
			}
		case "invariant":
			if s, _ := item["assert"].(string); strings.TrimSpace(s) == "" {
				c.rulef("rule-structure", "%s: invariant %s requires assert", rel, id)
			}
		case "policy":
		default:
			c.rulef("rule-structure", "%s: rule %s kind must be constraint/invariant/policy", rel, id)
		}
		if code, _ := item["on_violation"].(string); code != "" && !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
			c.rulef("rule-structure", "%s: rule %s unknown on_violation %s", rel, id, code)
		}
		for _, rawSvc := range asList(item["implemented_by"]) {
			svc, _ := rawSvc.(string)
			if !serviceIDs[svc] {
				c.rulef("rule-structure", "%s: rule %s unknown service %s", rel, id, svc)
			}
		}
	}
}

func (c *Checker) checkStateDoc(doc map[string]any, rel string, schema *Schema, serviceIDs, eventIDs, ruleIDs map[string]bool) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	c.checkTargetFileName(schema, filepath.Base(rel), rel)
	fileID, _ := doc["id"].(string)
	want := schema.Targets.FileID(filepath.Base(rel))
	if fileID != want {
		c.rulef("state-structure", "%s: id %q must be %q", rel, fileID, want)
	}
	stateNames := map[string]bool{}
	for _, raw := range asList(doc["states"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if stateNames[name] {
			c.rulef("state-structure", "%s: duplicate state %s", rel, name)
		}
		stateNames[name] = true
	}
	initial, _ := doc["initial"].(string)
	if !stateNames[initial] {
		c.rulef("state-structure", "%s: initial %s is not a declared state", rel, initial)
	}
	nonTerminalOut := map[string]bool{}
	transitionNames := map[string]bool{}
	for _, raw := range asList(doc["transitions"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if transitionNames[name] {
			c.rulef("state-exhaustive", "%s: duplicate transition %s", rel, name)
		}
		transitionNames[name] = true
		from := asList(item["from"])
		fromNames := map[string]bool{}
		for _, rawFrom := range from {
			s, _ := rawFrom.(string)
			fromNames[s] = true
			if !stateNames[s] {
				c.rulef("state-structure", "%s: transition %s unknown from state %s", rel, name, s)
			}
			nonTerminalOut[s] = true
		}
		to, _ := item["to"].(string)
		if !stateNames[to] {
			c.rulef("state-structure", "%s: transition %s unknown to state %s", rel, name, to)
		}
		if code, _ := item["on_violation"].(string); code != "" && !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
			c.rulef("state-structure", "%s: transition %s unknown on_violation %s", rel, name, code)
		}
		guard, _ := asMap(item["guard"])
		if guard == nil {
			if _, ok := item["guard"].(bool); !ok {
				c.rulef("state-structure", "%s: transition %s guard must be true or { rule: feature.rules.* }", rel, name)
			}
		} else if rule, _ := guard["rule"].(string); !ruleIDs[rule] {
			c.rulef("state-structure", "%s: transition %s unknown guard rule %s", rel, name, rule)
		}
		for _, rawEvent := range asList(item["side_effects"]) {
			ev, _ := rawEvent.(string)
			if !eventIDs[ev] {
				c.rulef("state-structure", "%s: transition %s unknown event %s", rel, name, ev)
			}
		}
		for _, rawSvc := range asList(item["implemented_by"]) {
			svc, _ := rawSvc.(string)
			if !serviceIDs[svc] {
				c.rulef("state-structure", "%s: transition %s unknown service %s", rel, name, svc)
			}
		}
	}
	for _, raw := range asList(doc["states"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		terminal, _ := item["terminal"].(bool)
		if !terminal && !nonTerminalOut[name] {
			c.rulef("state-exhaustive", "%s: non-terminal state %s has no outgoing transition", rel, name)
		}
		if terminal && nonTerminalOut[name] {
			c.rulef("state-exhaustive", "%s: terminal state %s must not have outgoing transitions", rel, name)
		}
	}
	for _, raw := range asList(doc["forbidden_examples"]) {
		item, _ := asMap(raw)
		from, _ := item["from"].(string)
		tr, _ := item["transition"].(string)
		if !stateNames[from] {
			c.rulef("state-structure", "%s: forbidden example unknown from state %s", rel, from)
		}
		if !transitionNames[tr] {
			c.rulef("state-structure", "%s: forbidden example unknown transition %s", rel, tr)
		}
	}
}

func (c *Checker) checkEventDoc(doc map[string]any, rel string, schema *Schema, serviceIDs map[string]bool) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	c.checkTargetFileName(schema, filepath.Base(rel), rel)
	fileID, _ := doc["id"].(string)
	want := schema.Targets.FileID(filepath.Base(rel))
	if fileID != want {
		c.rulef("event-structure", "%s: id %q must be %q", rel, fileID, want)
	}
	names := map[string]bool{}
	subject := strings.TrimSuffix(filepath.Base(rel), ".yaml")
	for _, raw := range asList(doc["events"]) {
		item, _ := asMap(raw)
		id, _ := item["id"].(string)
		name, _ := item["name"].(string)
		if names[name] {
			c.rulef("event-structure", "%s: duplicate event name %s", rel, name)
		}
		names[name] = true
		if name != "" && !strings.HasPrefix(name, subject+".") {
			c.rulef("event-structure", "%s: event name %s must start with %s.", rel, name, subject)
		}
		if id != "" && name != "" && id != "cap.events."+name {
			c.rulef("event-structure", "%s: event id %s must be cap.events.%s", rel, id, name)
		}
		if payload, ok := asMap(item["payload"]); ok {
			if props, ok := asMap(payload["properties"]); ok {
				for name, rawProp := range props {
					prop, _ := asMap(rawProp)
					typ, _ := prop["type"].(string)
					switch {
					case strings.HasPrefix(typ, "data.enums.") && !c.enumIDs[typ]:
						c.rulef("event-payload-ref", "%s: event %s payload.%s references unknown enum %s", rel, id, name, typ)
					case strings.HasPrefix(typ, "data.entities.") && c.entities[typ] == nil:
						c.rulef("event-payload-ref", "%s: event %s payload.%s references unknown entity %s", rel, id, name, typ)
					}
				}
			}
			if ref, _ := payload["$ref"].(string); ref != "" {
				if strings.HasPrefix(ref, "data.enums.") {
					c.rulef("event-payload-ref", "%s: event %s payload $ref %s must be an object type; use payload.properties.<field>.type for enums", rel, id, ref)
				} else if !c.eventPayloadRef(ref) {
					c.rulef("event-payload-ref", "%s: event %s payload $ref %s is unknown", rel, id, ref)
				}
			} else if typ, _ := payload["type"].(string); typ == "object" {
				if v, ok := payload["additionalProperties"]; !ok || v != false {
					c.rulef("event-structure", "%s: event %s payload must set additionalProperties: false", rel, id)
				}
			}
		}
		for _, rawSvc := range asList(item["emitted_by"]) {
			svc, _ := rawSvc.(string)
			if !serviceIDs[svc] {
				c.rulef("event-structure", "%s: event %s unknown service %s", rel, id, svc)
			}
		}
	}
}

func (c *Checker) eventPayloadRef(ref string) bool {
	if strings.HasPrefix(ref, "data.entities.") {
		return c.entities[ref] != nil
	}
	if strings.HasPrefix(ref, "cap.services.") && strings.Contains(ref, ".schemas.") {
		parts := strings.Split(ref, ".schemas.")
		if len(parts) == 2 && parts[1] != "" {
			return c.methodTypeDefs[parts[1]] != nil
		}
	}
	return false
}

func (c *Checker) checkAsyncDoc(schema *Schema, serviceMeta map[string]serviceMeta, eventIDs map[string]bool) (map[string]bool, map[string]asyncMeta) {
	asyncServices := map[string]bool{}
	asyncJobs := map[string]asyncMeta{}
	bound := map[string]bool{}
	for _, file := range glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkTargetFileName(schema, filepath.Base(rel), rel)
		fileID, _ := doc["id"].(string)
		if want := schema.Targets.FileID(filepath.Base(rel)); fileID != want {
			c.rulef("async-structure", "%s: id %q must be %q", rel, fileID, want)
		}
		module, _ := doc["owner_module"].(string)
		names := map[string]bool{}
		for _, raw := range asList(doc["jobs"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			if names[name] {
				c.rulef("async-structure", "%s: duplicate job name %s", rel, name)
			}
			names[name] = true
			id, _ := item["id"].(string)
			if id != "" && fileID != "" && id != fileID+"."+name {
				c.rulef("async-structure", "%s.%s: job id %q must be %s", rel, name, id, fileID+"."+name)
			}
			if id != "" {
				if _, exists := asyncJobs[id]; exists {
					c.rulef("async-structure", "%s.%s: duplicate job id %s", rel, name, id)
				}
			}
			svc, _ := item["service"].(string)
			if svc != "" {
				if meta, ok := serviceMeta[svc]; !ok {
					c.rulef("async-service", "%s.%s: unknown service %s", rel, name, svc)
				} else {
					if meta.module != module {
						c.rulef("async-service", "%s.%s: service %s belongs to module %s, not %s", rel, name, svc, meta.module, module)
					}
					asyncServices[svc] = true
				}
			}
			trigger, _ := asMap(item["trigger"])
			triggerType := ""
			if trigger != nil {
				triggerType, _ = trigger["type"].(string)
				switch triggerType {
				case "cron":
					if v, _ := trigger["schedule"].(string); strings.TrimSpace(v) == "" {
						c.rulef("async-trigger", "%s.%s: trigger.schedule required for cron", rel, name)
					}
					if trigger["event"] != nil {
						c.rulef("async-trigger", "%s.%s: cron trigger must not declare event", rel, name)
					}
				case "event":
					ev, _ := trigger["event"].(string)
					if ev == "" {
						c.rulef("async-trigger", "%s.%s: trigger.event required for event", rel, name)
					} else if !eventIDs[ev] {
						c.rulef("async-trigger", "%s.%s: unknown event %s", rel, name, ev)
					}
					if trigger["schedule"] != nil {
						c.rulef("async-trigger", "%s.%s: event trigger must not declare schedule", rel, name)
					}
				case "webhook":
					if trigger["schedule"] != nil || trigger["event"] != nil {
						c.rulef("async-trigger", "%s.%s: webhook trigger must not declare schedule/event; bind path via cap.protocols.http x-async", rel, name)
					}
				}
			}
			if triggerType != "" && svc != "" {
				key := triggerType + "\x00" + svc
				if bound[key] {
					c.rulef("async-structure", "%s.%s: duplicate (trigger, service) %s/%s", rel, name, triggerType, svc)
				}
				bound[key] = true
			}
			if id != "" {
				asyncJobs[id] = asyncMeta{triggerType: triggerType}
			}
		}
	}
	return asyncServices, asyncJobs
}

func (c *Checker) checkHTTPBindings(schema *Schema, serviceMeta map[string]serviceMeta, typeNames map[string]bool, asyncServices map[string]bool, asyncJobs map[string]asyncMeta) {
	httpFiles := glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))
	httpSchemas := map[string]bool{}
	httpHeaders := map[string]bool{}
	httpResponses := map[string]bool{}
	var serviceUnavailable map[string]any
	hasDefault503 := false
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkTargetFileName(schema, filepath.Base(rel), rel)
		if want := schema.Targets.FileID(filepath.Base(rel)); doc["id"] != want {
			c.rulef("http-binding", "%s: id %q must be %q", rel, doc["id"], want)
		}
		if s, _ := doc["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
		}
		components, _ := asMap(doc["components"])
		headers, _ := asMap(components["headers"])
		for name := range headers {
			httpHeaders[name] = true
		}
		responses, _ := asMap(components["responses"])
		for name := range responses {
			httpResponses[name] = true
		}
		if raw, ok := responses["ServiceUnavailable"]; ok {
			serviceUnavailable, _ = asMap(raw)
		}
		for _, raw := range asList(doc["default_error_responses"]) {
			if n, ok := toInt(raw); ok && n == 503 {
				hasDefault503 = true
			}
		}
		schemas, _ := asMap(components["schemas"])
		for name, raw := range schemas {
			httpSchemas[name] = true
			s, _ := asMap(raw)
			if ref, _ := s["x-service-type"].(string); ref != "" {
				if !match(`^cap\.services\.[a-z][a-z0-9_]*\.schemas\.[A-Z][A-Za-z0-9]*$`, ref) {
					c.rulef("http-components", "%s: components.schemas.%s.x-service-type must match cap.services.<file>.schemas.<Type>", rel, name)
				} else if typ, ok := serviceTypeRef(ref); !ok || !typeNames[typ] {
					c.rulef("http-components", "%s: components.schemas.%s.x-service-type references unknown %s", rel, name, ref)
				} else if props, ok := asMap(s["properties"]); ok {
					if def := findTypeSchema(serviceMeta, typ); def != nil {
						defProps, _ := asMap(def["properties"])
						if !sameSchemaFields(props, defProps) {
							c.rulef("http-service-type", "%s: components.schemas.%s fields differ from %s", rel, name, ref)
						}
					}
				}
			}
			if ref, _ := s["x-enum"].(string); ref != "" {
				if !c.enumIDs[ref] {
					c.rulef("http-components", "%s: components.schemas.%s.x-enum references unknown enum %s", rel, name, ref)
				}
			}
			if ref, _ := s["x-entity"].(string); ref != "" {
				if _, ok := c.entities[ref]; !ok {
					c.rulef("http-components", "%s: components.schemas.%s.x-entity references unknown entity %s", rel, name, ref)
				}
			}
			c.checkClosedObjects(s, rel, "components.schemas."+name)
		}
	}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		components, _ := asMap(doc["components"])
		collectRefs(components, func(ref string) {
			if strings.HasPrefix(ref, "#/components/schemas/") {
				if name := strings.TrimPrefix(ref, "#/components/schemas/"); !httpSchemas[name] {
					c.rulef("http-components", "%s: unknown schema $ref %s", rel, ref)
				}
			}
		})
	}
	if hasDefault503 {
		if !httpHeaders["RetryAfter"] {
			c.rulef("http-retry-after", "default_error_responses includes 503 but components.headers.RetryAfter is missing")
		}
		if serviceUnavailable == nil {
			c.rulef("http-retry-after", "default_error_responses includes 503 but components.responses.ServiceUnavailable is missing")
		} else if !retryAfterHeader(serviceUnavailable) {
			c.rulef("http-retry-after", "components.responses.ServiceUnavailable must declare Retry-After")
		}
	}
	contractIDs := map[string]string{}
	operationIDs := map[string]string{}
	operations := map[string]string{}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		components, _ := asMap(doc["components"])
		schemas, _ := asMap(components["schemas"])
		defaultErrors := map[string]bool{}
		for _, raw := range asList(doc["default_error_responses"]) {
			n, ok := toInt(raw)
			if !ok || n < 100 || n > 599 {
				c.rulef("http-binding", "%s: default_error_responses must contain HTTP status codes 100-599, got %v", rel, raw)
				continue
			}
			name, ok := openapispec.DefaultResponseName(n)
			if !ok {
				c.rulef("http-binding", "%s: default status %d has no components.responses mapping", rel, n)
			} else if !httpResponses[name] {
				c.rulef("http-binding", "%s: default status %d requires components.responses.%s", rel, n, name)
			}
			defaultErrors[fmt.Sprint(raw)] = true
		}
		paths, _ := asMap(doc["paths"])
		for path, raw := range paths {
			p, _ := asMap(raw)
			for method, opRaw := range p {
				op, ok := asMap(opRaw)
				if !ok {
					continue
				}
				for key := range op {
					if !strings.HasPrefix(key, "x-") || key == "x-flow" {
						continue
					}
					if !allowedOperationExtensions[key] {
						c.rulef("http-binding", "%s %s %s: unknown operation extension %s", rel, method, path, key)
					}
				}
				if prev, exists := operations[path+"\x00"+method]; exists {
					c.rulef("http-binding", "%s %s %s: duplicate operation (%s)", rel, method, path, prev)
				} else {
					operations[path+"\x00"+method] = rel
				}
				opID, _ := op["operationId"].(string)
				if opID == "" {
					c.rulef("http-binding", "%s %s %s: operationId required", rel, method, path)
				} else if prev, exists := operationIDs[opID]; exists {
					c.rulef("http-binding", "%s %s %s: duplicate operationId %s (also in %s)", rel, method, path, opID, prev)
				} else {
					operationIDs[opID] = rel
				}
				contractID, _ := op["x-contract-id"].(string)
				switch {
				case contractID == "":
					c.rulef("http-binding", "%s %s %s: x-contract-id required", rel, method, path)
				case !match(`^cap\.protocols\.http\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`, contractID):
					c.rulef("http-binding", "%s %s %s: x-contract-id %q must match cap.protocols.http.<file>.<operation>", rel, method, path, contractID)
				default:
					if prev, exists := contractIDs[contractID]; exists {
						c.rulef("http-binding", "%s %s %s: duplicate x-contract-id %s (also in %s)", rel, method, path, contractID, prev)
					} else {
						contractIDs[contractID] = rel
					}
				}
				if raw, ok := op["x-timeout-ms"]; ok {
					if n, ok := toInt(raw); !ok || n <= 0 {
						c.rulef("http-binding", "%s %s %s: x-timeout-ms must be a positive integer", rel, method, path)
					}
				}
				effectiveDefaults := defaultErrors
				if raw, ok := op["x-default-errors"]; ok {
					effectiveDefaults = map[string]bool{}
					for _, item := range asList(raw) {
						n, ok := toInt(item)
						if !ok {
							c.rulef("http-binding", "%s %s %s: x-default-errors must contain HTTP status codes", rel, method, path)
							continue
						}
						name, ok := openapispec.DefaultResponseName(n)
						if !ok {
							c.rulef("http-binding", "%s %s %s: x-default-errors status %d has no components.responses mapping", rel, method, path, n)
						} else if !httpResponses[name] {
							c.rulef("http-binding", "%s %s %s: x-default-errors status %d requires components.responses.%s", rel, method, path, n, name)
						}
						effectiveDefaults[fmt.Sprint(n)] = true
					}
				}
				responses, _ := asMap(op["responses"])
				for status, rawResp := range responses {
					if effectiveDefaults[status] {
						c.rulef("http-binding", "%s %s %s: status %s is injected by default errors; do not repeat it", rel, method, path, status)
					}
					resp, _ := asMap(rawResp)
					if content, ok := asMap(resp["content"]); ok {
						for _, rawMedia := range content {
							media, _ := asMap(rawMedia)
							mediaSchema, _ := asMap(media["schema"])
							c.checkClosedObjects(mediaSchema, rel, method+" "+path+" responses")
							ret, _ := mediaSchema["x-service-return"].(string)
							if ret == "" {
								continue
							}
							ref, _ := op["x-service"].(string)
							target, foundOK := serviceMeta[ref]
							if !foundOK {
								continue
							}
							found := false
							for _, typ := range target.returns {
								if typ == ret {
									found = true
									break
								}
							}
							if !found {
								c.rulef("http-service-return", "%s %s %s: x-service-return %s is not a return of %s", rel, method, path, ret, ref)
							}
						}
					}
				}
				if raw, ok := responses["503"]; ok {
					resp, _ := asMap(raw)
					if !retryAfterHeader(resp) {
						c.rulef("http-retry-after", "%s %s %s: 503 response must declare Retry-After", rel, method, path)
					}
				}
				if op["x-flow"] != nil {
					c.rulef("http-binding", "%s %s %s: x-flow removed; use x-service", rel, method, path)
				}
				collectRefs(op, func(ref string) {
					if strings.HasPrefix(ref, "#/components/schemas/") {
						name := strings.TrimPrefix(ref, "#/components/schemas/")
						if !httpSchemas[name] {
							c.rulef("http-components", "%s %s %s: unknown schema $ref %s", rel, method, path, ref)
						}
					}
				})
				serviceRef, _ := op["x-service"].(string)
				asyncRef, _ := op["x-async"].(string)
				if asyncRef != "" && !match(`^cap\.async\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`, asyncRef) {
					c.rulef("http-binding", "%s %s %s: x-async %q must match cap.async.<file>.<job>", rel, method, path, asyncRef)
				}
				if serviceRef == "" && asyncRef == "" {
					c.rulef("http-binding", "%s %s %s: x-service or x-async required", rel, method, path)
					continue
				}
				if serviceRef != "" && asyncRef != "" {
					c.rulef("http-binding", "%s %s %s: x-service and x-async are mutually exclusive", rel, method, path)
					continue
				}
				if asyncRef != "" {
					job, ok := asyncJobs[asyncRef]
					if !ok {
						c.rulef("http-binding", "%s %s %s: unknown async job %s", rel, method, path, asyncRef)
						continue
					}
					if job.triggerType != "webhook" {
						c.rulef("http-binding", "%s %s %s: async job %s is not webhook-triggered", rel, method, path, asyncRef)
					}
					continue
				}
				target, ok := serviceMeta[serviceRef]
				if !ok {
					c.rulef("http-binding", "%s %s %s: unknown service %s", rel, method, path, serviceRef)
					continue
				}
				if asyncServices[serviceRef] {
					c.rulef("http-binding", "%s %s %s: async service %s cannot be bound to http", rel, method, path, serviceRef)
					continue
				}
				if target.visibility != "public" {
					c.rulef("service-visibility", "%s %s %s: service %s is not public", rel, method, path, serviceRef)
				}
				if rb, ok := asMap(op["requestBody"]); ok {
					if content, ok := asMap(rb["content"]); ok {
						for _, rawMedia := range content {
							media, _ := asMap(rawMedia)
							mediaSchema, _ := asMap(media["schema"])
							c.checkClosedObjects(mediaSchema, rel, method+" "+path+" requestBody")
							if ref, _ := mediaSchema["$ref"].(string); ref != "" {
								name := strings.TrimPrefix(ref, "#/components/schemas/")
								if !target.paramTypes[name] {
									c.rulef("http-service-type", "%s %s %s: requestBody $ref %s is not a param of %s", rel, method, path, name, serviceRef)
								} else if comp, ok := asMap(schemas[name]); ok {
									fileRef := serviceRef
									if i := strings.LastIndex(serviceRef, "."); i > 0 {
										fileRef = serviceRef[:i]
									}
									if xt, _ := comp["x-service-type"].(string); xt != "" && !strings.HasPrefix(xt, fileRef+".schemas.") {
										c.rulef("http-service-type", "%s %s %s: requestBody $ref %s points at %s, not %s.schemas.*", rel, method, path, name, xt, fileRef)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// checkClosedObjects 递归校验 schema 中的 object 均关闭额外字段。
func (c *Checker) checkClosedObjects(v any, rel, where string) {
	m, ok := asMap(v)
	if !ok {
		return
	}
	if typ, _ := m["type"].(string); typ == "object" {
		if ap, ok := m["additionalProperties"]; !ok || ap != false {
			c.rulef("http-components", "%s: %s must set additionalProperties: false", rel, where)
		}
	}
	if props, ok := asMap(m["properties"]); ok {
		for name, raw := range props {
			c.checkClosedObjects(raw, rel, where+"."+name)
		}
	}
	if items, ok := asMap(m["items"]); ok {
		c.checkClosedObjects(items, rel, where+".items")
	}
	if ap, ok := asMap(m["additionalProperties"]); ok {
		c.checkClosedObjects(ap, rel, where+".additionalProperties")
	}
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		for i, raw := range asList(m[key]) {
			c.checkClosedObjects(raw, rel, fmt.Sprintf("%s.%s[%d]", where, key, i))
		}
	}
}

func retryAfterHeader(resp map[string]any) bool {
	headers, _ := asMap(resp["headers"])
	ra, _ := asMap(headers["Retry-After"])
	if ra == nil {
		return false
	}
	if ref, _ := ra["$ref"].(string); ref != "" {
		return ref == "#/components/headers/RetryAfter"
	}
	_, ok := asMap(ra["schema"])
	return ok
}

func (c *Checker) checkServiceTypes(types map[string]any, rel string) {
	for name, raw := range types {
		c.checkServiceType(raw, rel, "schemas."+name)
	}
}

func (c *Checker) checkServiceType(v any, rel, where string) {
	m, ok := asMap(v)
	if !ok {
		return
	}
	if typ, _ := m["type"].(string); typ == "object" {
		if ap, ok := m["additionalProperties"]; !ok || ap != false {
			c.rulef("service-structure", "%s: %s must set additionalProperties: false", rel, where)
		}
		props, _ := asMap(m["properties"])
		for _, rawReq := range asList(m["required"]) {
			field, _ := rawReq.(string)
			if props[field] == nil {
				c.rulef("service-structure", "%s: %s required field %q not in properties", rel, where, field)
			}
		}
	}
	if props, ok := asMap(m["properties"]); ok {
		for name, raw := range props {
			c.checkServiceType(raw, rel, where+"."+name)
		}
	}
	if items, ok := asMap(m["items"]); ok {
		c.checkServiceType(items, rel, where+".items")
	}
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		for i, raw := range asList(m[key]) {
			c.checkServiceType(raw, rel, fmt.Sprintf("%s.%s[%d]", where, key, i))
		}
	}
}

func findTypeSchema(meta map[string]serviceMeta, typ string) map[string]any {
	for _, m := range meta {
		if def := m.typeSchemas[typ]; def != nil {
			return def
		}
	}
	return nil
}

func sameSchemaFields(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, rawA := range a {
		rawB, ok := b[k]
		if !ok {
			return false
		}
		ma, _ := asMap(rawA)
		mb, _ := asMap(rawB)
		ta, _ := ma["type"].(string)
		tb, _ := mb["type"].(string)
		if ta != tb {
			return false
		}
	}
	return true
}

func serviceTypeRef(ref string) (string, bool) {
	if !strings.HasPrefix(ref, "cap.services.") {
		return "", false
	}
	rest := strings.TrimPrefix(ref, "cap.services.")
	parts := strings.Split(rest, ".schemas.")
	if len(parts) != 2 || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func (c *Checker) serviceLevelErrors(id string, hasBackwardGoto bool) map[string]bool {
	required := map[string]bool{}
	if policy, ok := c.accessBindings[id]; ok && len(policy.permissions) > 0 {
		required["platform.errors.COMMON_FORBIDDEN"] = true
	}
	if policy, ok := c.accessBindings[id]; ok && policy.rateLimited {
		required["platform.errors.COMMON_RATE_LIMITED"] = true
	}
	if policy, ok := c.accessBindings[id]; ok && policy.audit {
		required["platform.errors.COMMON_AUDIT_FAILED"] = true
	}
	if hasBackwardGoto {
		required["platform.errors.COMMON_LOOP_EXHAUSTED"] = true
	}
	return required
}

// checkInputPath 校验 when 的 input.<path>：根字段必须是 service param，嵌套字段按 param 类型解析。
func (c *Checker) checkInputPath(doc map[string]any, field string) error {
	path, ok := splitInputField(field)
	if !ok {
		return fmt.Errorf("field %q must be input.<path>", field)
	}
	segs, err := parseWhenPath(path)
	if err != nil {
		return err
	}
	root := segs[0]
	var typ string
	found := false
	for _, raw := range asList(doc["params"]) {
		pm, _ := asMap(raw)
		if name, _ := pm["name"].(string); name == root.name {
			typ, _ = pm["type"].(string)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown input %s", root.name)
	}
	if typ == "" {
		return fmt.Errorf("input %s has no type", root.name)
	}
	prefix := whenSegmentPath(root)
	if root.hasIndex {
		typ, err = indexWhenType(typ)
		if err != nil {
			return fmt.Errorf("input %s: %v", root.name, err)
		}
	}
	for _, seg := range segs[1:] {
		typ = strings.TrimPrefix(typ, "*")
		if strings.HasPrefix(typ, "List<") && strings.HasSuffix(typ, ">") {
			return fmt.Errorf("list input requires index before %s", seg.name)
		}
		next, ok := c.resolveTypeField(typ, seg.name, whenFormula{}, "", prefix)
		if !ok {
			return fmt.Errorf("unknown field %s of %s", seg.name, typ)
		}
		typ = next
		prefix += "." + whenSegmentPath(seg)
		if seg.hasIndex {
			typ, err = indexWhenType(typ)
			if err != nil {
				return fmt.Errorf("field %s: %v", seg.name, err)
			}
		}
	}
	return nil
}

func (c *Checker) checkStepOutputPath(step map[string]any, output map[string]string, path string, formula whenFormula, stepRef string) error {
	segs, err := parseWhenPath(path)
	if err != nil {
		return err
	}
	root := segs[0]
	kind, _ := step["kind"].(string)
	var typ string
	switch kind {
	case "service":
		ref, _ := step["service"].(string)
		typ = c.methodReturns[ref][root.name]
		if typ == "" {
			return fmt.Errorf("unknown output %s", root.name)
		}
	case "query":
		ref, _ := step["query"].(string)
		typ = c.queryReturnTypes[ref][root.name]
		if typ == "" {
			return fmt.Errorf("unknown output %s", root.name)
		}
	case "logic", "transition":
		typ = output[root.name]
		if typ == "" {
			return fmt.Errorf("unknown output %s", root.name)
		}
	default:
		return fmt.Errorf("unknown step kind %q", kind)
	}
	if root.hasIndex {
		typ, err = indexWhenType(typ)
		if err != nil {
			return fmt.Errorf("output %s: %v", root.name, err)
		}
	}
	prefix := whenSegmentPath(root)
	for _, seg := range segs[1:] {
		typ = strings.TrimPrefix(typ, "*")
		if strings.HasPrefix(typ, "List<") && strings.HasSuffix(typ, ">") {
			return fmt.Errorf("list output requires index before %s", seg.name)
		}
		next, ok := c.resolveTypeField(typ, seg.name, formula, stepRef, prefix)
		if !ok {
			return fmt.Errorf("unknown field %s of %s", seg.name, typ)
		}
		typ = next
		prefix += "." + whenSegmentPath(seg)
		if seg.hasIndex {
			typ, err = indexWhenType(typ)
			if err != nil {
				return fmt.Errorf("field %s: %v", seg.name, err)
			}
		}
	}
	return nil
}

func indexWhenType(typ string) (string, error) {
	typ = strings.TrimPrefix(typ, "*")
	if !strings.HasPrefix(typ, "List<") || !strings.HasSuffix(typ, ">") {
		return "", fmt.Errorf("%s is not a list", typ)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(typ, "List<"), ">")
	return strings.TrimPrefix(inner, "*"), nil
}

func (c *Checker) resolveTypeField(typ, name string, formula whenFormula, stepRef, prefix string) (string, bool) {
	return c.resolveTypeFieldDepth(typ, name, map[string]bool{}, formula, stepRef, prefix)
}

func (c *Checker) resolveTypeFieldDepth(typ, name string, seen map[string]bool, formula whenFormula, stepRef, prefix string) (string, bool) {
	if resolved, ok := resolveTypeRef(typ); ok {
		typ = resolved
	}
	if strings.HasPrefix(typ, "data.entities.") {
		return c.entityFieldType(typ, name)
	}
	if seen[typ] {
		return "", false
	}
	seen[typ] = true
	defer delete(seen, typ)
	def := c.methodTypeDefs[typ]
	if def == nil {
		return "", false
	}
	return c.resolveSchemaField(def, name, seen, formula, stepRef, prefix)
}

func (c *Checker) resolveSchemaField(schema map[string]any, name string, seen map[string]bool, formula whenFormula, stepRef, prefix string) (string, bool) {
	if props, ok := asMap(schema["properties"]); ok {
		if raw, exists := props[name]; exists {
			if prop, ok := schemaFromAny(raw); ok {
				return c.schemaTypeName(prop)
			}
		}
	}
	if branches, ok := asList2(schema["allOf"]); ok {
		var got string
		for _, raw := range branches {
			branch, _ := asMap(raw)
			typ, ok := c.resolveSchemaField(branch, name, seen, formula, stepRef, prefix)
			if !ok {
				continue
			}
			if got == "" {
				got = typ
			} else if got != typ {
				return "", false
			}
		}
		if got != "" {
			return got, true
		}
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if branches, ok := asList2(schema[key]); ok {
			if discSchema, ok := asMap(schema["discriminator"]); ok {
				if !c.validDiscriminator(branches, discSchema, seen) {
					return "", false
				}
				if formula.impossible || len(formula.clauses) > 0 {
					if _, hasMapping := asMap(discSchema["mapping"]); !hasMapping {
						return c.resolveVariantField(branches, name, seen, formula, stepRef, prefix)
					}
					selected, ok := c.selectDiscriminatorBranches(branches, discSchema, formula, stepRef, prefix)
					if !ok {
						return "", false
					}
					return c.resolveSelectedBranches(selected, name, seen, formula, stepRef, prefix)
				}
				return c.resolveVariantField(branches, name, seen, formula, stepRef, prefix)
			}
			var got string
			for _, raw := range branches {
				branch, _ := asMap(raw)
				typ, ok := c.resolveSchemaField(branch, name, seen, formula, stepRef, prefix)
				if !ok {
					return "", false
				}
				if got == "" {
					got = typ
				} else if got != typ {
					return "", false
				}
			}
			if got != "" {
				return got, true
			}
		}
	}
	if ref, _ := schema["$ref"].(string); ref != "" {
		if typ, ok := resolveTypeRef(ref); ok {
			return c.resolveTypeFieldDepth(typ, name, seen, formula, stepRef, prefix)
		}
	}
	return "", false
}

// validDiscriminator 校验判别字段存在于所有分支且类型一致；mapping 的 ref 必须可解析。
func (c *Checker) validDiscriminator(branches []any, disc map[string]any, seen map[string]bool) bool {
	prop, _ := disc["propertyName"].(string)
	if prop == "" {
		return false
	}
	var got string
	for _, raw := range branches {
		branch, _ := asMap(raw)
		typ, ok := c.resolveSchemaField(branch, prop, seen, whenFormula{}, "", "")
		if !ok {
			return false
		}
		if got == "" {
			got = typ
		} else if got != typ {
			return false
		}
	}
	if mapping, ok := asMap(disc["mapping"]); ok {
		branchNames := map[string]bool{}
		for _, raw := range branches {
			branch, _ := asMap(raw)
			if name, ok := branchTypeName(branch); ok {
				branchNames[name] = true
			}
		}
		for _, raw := range mapping {
			ref, _ := raw.(string)
			name, ok := resolveTypeRef(ref)
			if !ok {
				return false
			}
			if strings.HasPrefix(name, "data.") {
				continue
			}
			if c.methodTypeDefs[name] == nil || !branchNames[name] {
				return false
			}
		}
	}
	return true
}

// resolveVariantField 在 discriminator 下允许分支特有字段：
// 只在一个分支存在即返回；多个分支都有时必须类型一致。
func (c *Checker) resolveVariantField(branches []any, name string, seen map[string]bool, formula whenFormula, stepRef, prefix string) (string, bool) {
	var got string
	found := false
	for _, raw := range branches {
		branch, _ := asMap(raw)
		typ, ok := c.resolveSchemaField(branch, name, seen, formula, stepRef, prefix)
		if !ok {
			continue
		}
		if found && typ != got {
			return "", false
		}
		got = typ
		found = true
	}
	if found {
		return got, true
	}
	return "", false
}

// selectDiscriminatorBranches 按完整 when clause 选择 oneOf/anyOf 分支：
// clause 中同一 step 的判别路径必须匹配候选值，其他路径若能在分支 const/enum 中求值也一并校验。
func (c *Checker) selectDiscriminatorBranches(branches []any, disc map[string]any, formula whenFormula, stepRef, prefix string) ([]map[string]any, bool) {
	if formula.impossible {
		return nil, false
	}
	mapping, _ := asMap(disc["mapping"])
	if mapping == nil {
		return nil, false
	}
	prop, _ := disc["propertyName"].(string)
	var out []map[string]any
	seen := map[string]bool{}
	for value, rawRef := range mapping {
		ref, _ := rawRef.(string)
		name, ok := resolveTypeRef(ref)
		if !ok {
			return nil, false
		}
		var branch map[string]any
		for _, raw := range branches {
			b, _ := asMap(raw)
			if branchName, ok := branchTypeName(b); ok && branchName == name {
				branch = c.branchSchema(b)
				break
			}
		}
		if branch == nil {
			return nil, false
		}
		if !c.formulaAllows(formula, stepRef, prefix, prop, value, branch) {
			continue
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, branch)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func (c *Checker) resolveSelectedBranches(branches []map[string]any, name string, seen map[string]bool, formula whenFormula, stepRef, prefix string) (string, bool) {
	var got string
	for _, branch := range branches {
		typ, ok := c.resolveSchemaField(branch, name, seen, formula, stepRef, prefix)
		if !ok {
			return "", false
		}
		if got == "" {
			got = typ
		} else if got != typ {
			return "", false
		}
	}
	if got == "" {
		return "", false
	}
	return got, true
}

// formulaAllows 判断 clause 是否可能容纳该分支：不同 step 的条件视为未知，不否决。
func (c *Checker) formulaAllows(f whenFormula, stepRef, prefix, discProp, value string, branch map[string]any) bool {
	if f.impossible {
		return false
	}
	if len(f.clauses) == 0 {
		return true
	}
	discPath := discProp
	if prefix != "" {
		discPath = prefix + "." + discProp
	}
	for _, clause := range f.clauses {
		ok := true
		for key, sel := range clause {
			if key.step == "input" {
				if key.path == discPath || key.path == discProp {
					if !sel.matches(value) {
						ok = false
						break
					}
				}
				continue
			}
			if key.step != stepRef {
				continue
			}
			if key.path == discPath || key.path == discProp {
				if !sel.matches(value) {
					ok = false
					break
				}
				continue
			}
			constVals, found := c.branchConstValues(branch, key.path)
			if !found && prefix != "" {
				if field := strings.TrimPrefix(key.path, prefix+"."); field != key.path {
					constVals, found = c.branchConstValues(branch, field)
				}
			}
			if found {
				possible := false
				for _, constVal := range constVals {
					if sel.matches(constVal) {
						possible = true
						break
					}
				}
				if !possible {
					ok = false
					break
				}
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (c *Checker) branchConstValues(branch map[string]any, field string) ([]string, bool) {
	schemas := c.propertySchemas(c.branchSchema(branch), strings.Split(field, "."), 0)
	if len(schemas) == 0 {
		return nil, false
	}
	var vals []string
	for _, schema := range schemas {
		vs, ok := constValues(schema)
		if !ok {
			// 任一可能 schema 没有常量，整个字段视为未知，避免错误排除分支。
			return nil, false
		}
		vals = append(vals, vs...)
	}
	if len(vals) == 0 {
		return nil, false
	}
	return vals, true
}

// propertySchemas 返回字段路径可能命中的 schema 集合：
// properties / $ref / allOf / oneOf / anyOf / 数组元素都按并集处理。
func (c *Checker) propertySchemas(schema map[string]any, parts []string, depth int) []map[string]any {
	if depth > 32 {
		return nil
	}
	if len(parts) == 0 {
		return []map[string]any{schema}
	}
	schema = c.branchSchema(schema)
	if isArraySchema(schema) {
		var out []map[string]any
		for _, elem := range c.arrayElementSchemas(schema, -1, false) {
			out = append(out, c.propertySchemas(elem, parts, depth+1)...)
		}
		return out
	}
	name := parts[0]
	var out []map[string]any
	if idx := strings.IndexByte(name, '['); idx >= 0 {
		base := name[:idx]
		index, ok := parseArrayIndex(name[idx:])
		if !ok {
			return nil
		}
		for _, prop := range c.propertySchemas(schema, []string{base}, depth+1) {
			for _, elem := range c.arrayElementSchemas(prop, index, true) {
				out = append(out, c.propertySchemas(elem, parts[1:], depth+1)...)
			}
		}
		return out
	}
	if props, ok := asMap(schema["properties"]); ok {
		if raw, exists := props[name]; exists {
			if prop, ok := schemaFromAny(raw); ok {
				out = append(out, c.propertySchemas(prop, parts[1:], depth+1)...)
			}
		}
	}
	if branches, ok := asList2(schema["allOf"]); ok {
		for _, raw := range branches {
			branch, _ := asMap(raw)
			out = append(out, c.propertySchemas(branch, parts, depth+1)...)
		}
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if branches, ok := asList2(schema[key]); ok {
			for _, raw := range branches {
				branch, _ := asMap(raw)
				out = append(out, c.propertySchemas(branch, parts, depth+1)...)
			}
		}
	}
	return out
}

func parseArrayIndex(s string) (int, bool) {
	if len(s) < 3 || s[0] != '[' || s[len(s)-1] != ']' {
		return 0, false
	}
	n, err := strconv.Atoi(s[1 : len(s)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func schemaFromAny(v any) (map[string]any, bool) {
	if m, ok := asMap(v); ok {
		return m, true
	}
	if l, ok := asList2(v); ok {
		return map[string]any{"type": "array", "items": l}, true
	}
	return nil, false
}

func isArraySchema(schema map[string]any) bool {
	if typ, _ := schema["type"].(string); typ == "array" {
		return true
	}
	if _, ok := asList2(schema["prefixItems"]); ok {
		return true
	}
	if _, ok := asList2(schema["items"]); ok {
		return true
	}
	return false
}

// arrayElementSchemas 返回数组所有元素 schema；
// hasIndex=true 时只返回该下标的元素，tuple/prefixItems 越界回退到 items/additionalItems。
func (c *Checker) arrayElementSchemas(schema map[string]any, index int, hasIndex bool) []map[string]any {
	schema = c.branchSchema(schema)
	var prefix []map[string]any
	if raw, ok := asList2(schema["prefixItems"]); ok {
		for _, r := range raw {
			if m, ok := asMap(r); ok {
				prefix = append(prefix, m)
			}
		}
	}
	itemsSchema, hasItemsMap := asMap(schema["items"])
	itemsList, hasItemsList := asList2(schema["items"])
	additional, _ := asMap(schema["additionalItems"])
	if hasIndex {
		if index < len(prefix) {
			return []map[string]any{prefix[index]}
		}
		if hasItemsMap {
			return []map[string]any{itemsSchema}
		}
		if hasItemsList {
			if index < len(itemsList) {
				if m, ok := asMap(itemsList[index]); ok {
					return []map[string]any{m}
				}
			}
			if additional != nil {
				return []map[string]any{additional}
			}
		}
		if additional != nil {
			return []map[string]any{additional}
		}
		return nil
	}
	var out []map[string]any
	out = append(out, prefix...)
	if hasItemsMap {
		out = append(out, itemsSchema)
	}
	if hasItemsList {
		for _, raw := range itemsList {
			if m, ok := asMap(raw); ok {
				out = append(out, m)
			}
		}
	}
	if additional != nil {
		out = append(out, additional)
	}
	return out
}

func constValues(schema map[string]any) ([]string, bool) {
	if v, ok := schema["const"]; ok {
		if s, ok := whenLiteralString(v); ok {
			return []string{s}, true
		}
	}
	if enum, ok := asList2(schema["enum"]); ok {
		var vals []string
		for _, e := range enum {
			if s, ok := whenLiteralString(e); ok {
				vals = append(vals, s)
			}
		}
		if len(vals) > 0 {
			return vals, true
		}
	}
	return nil, false
}

func (c *Checker) branchSchema(branch map[string]any) map[string]any {
	if ref, _ := branch["$ref"].(string); ref != "" {
		if name, ok := resolveTypeRef(ref); ok {
			if def := c.methodTypeDefs[name]; def != nil {
				return def
			}
		}
	}
	return branch
}

func branchTypeName(branch map[string]any) (string, bool) {
	if ref, _ := branch["$ref"].(string); ref != "" {
		return resolveTypeRef(ref)
	}
	if typ, _ := branch["type"].(string); typ != "" {
		return typ, true
	}
	return "", false
}

func (c *Checker) schemaTypeName(schema map[string]any) (string, bool) {
	if ref, _ := schema["$ref"].(string); ref != "" {
		return resolveTypeRef(ref)
	}
	if isArraySchema(schema) {
		elements := c.arrayElementSchemas(schema, -1, false)
		if len(elements) == 1 {
			if inner, ok := c.schemaTypeName(elements[0]); ok {
				return "List<" + inner + ">", true
			}
		}
		if len(elements) > 1 {
			union := make([]any, 0, len(elements))
			for _, elem := range elements {
				union = append(union, elem)
			}
			if inner, ok := c.inlineType(map[string]any{"oneOf": union}); ok {
				return "List<" + inner + ">", true
			}
		}
	}
	if _, ok := asList2(schema["oneOf"]); ok {
		return c.inlineType(schema)
	}
	if _, ok := asList2(schema["anyOf"]); ok {
		return c.inlineType(schema)
	}
	if _, ok := asList2(schema["allOf"]); ok {
		return c.inlineType(schema)
	}
	if props, ok := asMap(schema["properties"]); ok && len(props) > 0 {
		return c.inlineType(schema)
	}
	if typ, _ := schema["type"].(string); typ != "" {
		return typ, true
	}
	return "", false
}

func (c *Checker) inlineType(schema map[string]any) (string, bool) {
	if c.methodTypeDefs == nil {
		c.methodTypeDefs = map[string]map[string]any{}
	}
	c.inlineSeq++
	name := fmt.Sprintf("@inline/%d", c.inlineSeq)
	c.methodTypeDefs[name] = schema
	return name, true
}

func resolveTypeRef(ref string) (string, bool) {
	for _, prefix := range []string{"#/types/", "#/components/schemas/"} {
		if strings.HasPrefix(ref, prefix) {
			name := strings.TrimPrefix(ref, prefix)
			if name != "" && !strings.ContainsAny(name, "/#") {
				return name, true
			}
		}
	}
	if strings.HasPrefix(ref, "data.") {
		return ref, true
	}
	if ref != "" && !strings.ContainsAny(ref, "/#") {
		return ref, true
	}
	return "", false
}

func (c *Checker) entityFieldType(entityID, name string) (string, bool) {
	e := c.entities[entityID]
	if e == nil {
		return "", false
	}
	for _, f := range e.Fields {
		if f.JSONName == name || f.Name == name {
			return f.Type, true
		}
	}
	return "", false
}

func (c *Checker) permissionExists(id string) bool {
	if c.permissionSet == nil {
		c.permissionSet = map[string]bool{}
		doc, err := loadYAML(filepath.Join(c.root, "contracts/platform/security/permissions.yaml"))
		if err == nil {
			for _, raw := range asList(doc["permissions"]) {
				if m, ok := asMap(raw); ok {
					if code, _ := m["code"].(string); code != "" {
						c.permissionSet["platform.security.permissions."+code] = true
					}
				}
			}
		}
	}
	return c.permissionSet[id]
}

func (c *Checker) checkTypeName(v any, typeNames map[string]bool, where string) {
	typ, _ := v.(string)
	if typ == "" {
		return
	}
	base := strings.TrimPrefix(strings.TrimSpace(typ), "*")
	if strings.HasPrefix(base, "List<") && strings.HasSuffix(base, ">") {
		base = strings.TrimSuffix(strings.TrimPrefix(base, "List<"), ">")
	}
	if strings.HasPrefix(base, "data.enums.") {
		if !c.enumIDs[base] {
			c.rulef("service-ref", "%s: unknown enum %s", where, base)
		}
		return
	}
	if strings.HasPrefix(base, "data.entities.") {
		if _, ok := c.entities[base]; !ok {
			c.rulef("service-ref", "%s: unknown entity %s", where, base)
		}
		return
	}
	if strings.HasPrefix(base, "data.tables.") {
		c.rulef("service-ref", "%s: data.tables.* is not a method type; use data.entities.*", where)
		return
	}
	if isBuiltinType(base) {
		return
	}
	if !typeNames[base] {
		c.rulef("service-ref", "%s: unknown type %s", where, base)
	}
}

func (c *Checker) checkTypeRefs(v any, typeNames map[string]bool, where string) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if k == "type" {
				c.checkTypeName(val, typeNames, where)
			}
			c.checkTypeRefs(val, typeNames, where)
		}
	case []any:
		for _, item := range x {
			c.checkTypeRefs(item, typeNames, where)
		}
	}
}

func isBuiltinType(t string) bool {
	switch t {
	case "context.Context", "error", "string", "integer", "number", "boolean", "object", "array", "any":
		return true
	}
	return strings.HasPrefix(t, "*") || strings.HasPrefix(t, "List<") || strings.HasPrefix(t, "data.") || strings.HasPrefix(t, "time.")
}

// collectRefs 递归收集 map/list 中的 $ref 字符串。
func collectRefs(v any, fn func(string)) {
	if m, ok := asMap(v); ok {
		for k, child := range m {
			if k == "$ref" {
				if s, ok := child.(string); ok {
					fn(s)
				}
			}
			collectRefs(child, fn)
		}
		return
	}
	for _, item := range asList(v) {
		collectRefs(item, fn)
	}
}

// checkServiceParamExpr 校验 params 表达式：input.* 必须存在于 service 入参，
// step.<id>.* 必须存在于前序步骤输出。
func (c *Checker) checkServiceParamExpr(v any, doc map[string]any, stepIndex map[string]int, stepOutputs []map[string]string, steps []any, rel, stepID string) {
	expr, ok := v.(string)
	if !ok || expr == "" {
		return
	}
	switch {
	case strings.HasPrefix(expr, "input."):
		field := strings.TrimPrefix(expr, "input.")
		if i := strings.IndexByte(field, '.'); i >= 0 {
			field = field[:i]
		}
		found := false
		for _, raw := range asList(doc["params"]) {
			pm, _ := asMap(raw)
			name, _ := pm["name"].(string)
			if name == field {
				found = true
				break
			}
		}
		if !found {
			c.rulef("service-params", "%s: step %s param references unknown input %s", rel, stepID, field)
		}
	case strings.HasPrefix(expr, "step."):
		ref, path, ok := splitStepField(expr)
		if !ok {
			c.rulef("service-params", "%s: step %s param %q is not a valid step reference", rel, stepID, expr)
			return
		}
		idx, exists := stepIndex[ref]
		if !exists {
			c.rulef("service-params", "%s: step %s param references step %s that is not an earlier step", rel, stepID, ref)
			return
		}
		target, _ := asMap(steps[idx])
		if err := c.checkStepOutputPath(target, stepOutputs[idx], path, whenFormula{}, ref); err != nil {
			c.rulef("service-params", "%s: step %s param references %s: %v", rel, stepID, expr, err)
		}
	}
}

// checkServiceCycles 在 kind=service 调用图上做 DFS 环检测。
func canonicalCycle(cycle []string) string {
	nodes := append([]string{}, cycle[:len(cycle)-1]...)
	sort.Strings(nodes)
	return strings.Join(nodes, "|")
}

func indexOf(values []string, value string) int {
	for i, v := range values {
		if v == value {
			return i
		}
	}
	return 0
}

func (c *Checker) checkServiceCycles(edges map[string][]string) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	seen := map[string]bool{}
	var stack []string
	var visit func(string)
	visit = func(n string) {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range edges[n] {
			switch color[m] {
			case gray:
				idx := indexOf(stack, m)
				cycle := append(append([]string{}, stack[idx:]...), m)
				key := canonicalCycle(cycle)
				if !seen[key] {
					seen[key] = true
					c.rulef("service-cycle", "service call cycle: %s", strings.Join(cycle, " -> "))
				}
			case white:
				visit(m)
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}
	for n := range edges {
		if color[n] == white {
			visit(n)
		}
	}
}
