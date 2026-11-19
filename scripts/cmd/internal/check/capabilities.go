// capabilities.go 是 capabilities 的主流程与共享类型；各领域校验见 services/lifecycles/events/http/rules。
package check

import (
	"path/filepath"
	"strings"
)

type returnType struct {
	typ      string
	nullable bool
}

// lifecycleSubject 是一个状态维度：实体上的某个状态字段。
type lifecycleSubject struct {
	Entity    string
	Field     string
	Table     string
	ColumnRef string
	Enum      string
}

type serviceMeta struct {
	module         string
	visibility     string
	txMode         string
	params         map[string]bool
	requiredParams map[string]bool
	paramTypes     map[string]bool
	paramDecls     map[string]string
	paramNullable  map[string]bool
	typeSchemas    map[string]map[string]any
	errors         map[string]bool
	returns        map[string]returnType
}

// stepTypeContext 提供 input 值静态类型解析所需的步骤上下文。
type stepTypeContext struct {
	doc         map[string]any
	stepIndex   map[string]int
	steps       []any
	stepOutputs []map[string]returnType
}

// typeShape 是类型名去掉指针/List 包装后的名义形状。
type typeShape struct {
	base  string
	array bool
}

// stepQueryItems 返回 step 声明的 query 项；仅 kind=query 使用单值 query。
func stepQueryItems(step map[string]any) []any {
	items := []any{}
	if qid, _ := step["query"].(string); qid != "" {
		items = append(items, map[string]any{"ref": qid})
	} else if q, ok := asMap(step["query"]); ok && q != nil {
		items = append(items, q)
	}
	return items
}

// stepServiceItems 返回 step 声明的 service 项：kind=service 用单值 service，logic 用 services 列表。
func stepServiceItems(step map[string]any) []any {
	items := []any{}
	if ref, _ := step["service"].(string); ref != "" {
		items = append(items, map[string]any{"ref": ref})
	} else if s, ok := asMap(step["service"]); ok && s != nil {
		items = append(items, s)
	}
	return append(items, asList(step["services"])...)
}

// eventField 是事件 payload 字段的静态类型与必填性。
type eventField struct {
	typ      string
	required bool
	nullable bool
}

// eventPayloadFields 把 topic payload 的 object.properties 解析为字段表。
func eventPayloadFields(raw any) map[string]eventField {
	out := map[string]eventField{}
	node, ok := asMap(raw)
	if !ok {
		return out
	}
	for _, rawProp := range asList(node["properties"]) {
		prop, _ := asMap(rawProp)
		name, _ := prop["name"].(string)
		if name == "" {
			continue
		}
		required, _ := prop["required"].(bool)
		nullable, _ := prop["nullable"].(bool)
		out[name] = eventField{typ: payloadPropertyType(prop["schema"]), required: required, nullable: nullable}
	}
	return out
}

// payloadPropertyType 取属性 schema 的静态类型；object/array 内联值域不参与类型断言。
func payloadPropertyType(raw any) string {
	node, ok := asMap(raw)
	if !ok {
		return ""
	}
	if ref, _ := node["ref"].(string); ref != "" {
		return ref
	}
	typ, _ := node["type"].(string)
	switch typ {
	case "object", "array", "":
		return ""
	}
	return typ
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
	lifecycleSchema, err := c.loadSchemaByType("cap.lifecycles")
	if err != nil {
		c.rulef("schema.load", "load cap.lifecycles schema: %v", err)
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
	for _, s := range []*Schema{serviceSchema, ruleSchema, lifecycleSchema, eventSchema, httpSchema} {
		c.checkRules(s)
		c.checkTargets(s)
	}

	serviceFiles := glob(c.root, filepath.Join(serviceSchema.Targets.Dir, serviceSchema.Targets.Glob))
	typeNames := map[string]bool{}
	typeOwners := map[string]string{}
	bareTypeOwners := map[string]string{}
	c.methodTypeDefs = map[string]map[string]any{}
	c.methodReturns = map[string]map[string]returnType{}
	c.serviceQueries = map[string]map[string]bool{}
	for _, file := range serviceFiles {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		fileID, _ := doc["id"].(string)
		types, _ := asMap(doc["schemas"])
		for name, raw := range types {
			if prev, ok := bareTypeOwners[name]; ok && prev != rel {
				c.rulef("service-schema", "%s: schema name %s already defined in %s; OpenAPI component names are global", rel, name, prev)
			}
			bareTypeOwners[name] = rel
			id := fileID + ".schemas." + name
			if prev, ok := typeOwners[id]; ok && prev != rel {
				c.rulef("service-schema", "%s: type %s already defined in %s", rel, id, prev)
			}
			typeOwners[id] = rel
			typeNames[id] = true
			if def, ok := asMap(raw); ok {
				c.methodTypeDefs[id] = def
			}
		}
	}

	// pass 1：收集 service item 元数据与返回类型。
	serviceIDs := map[string]bool{}
	serviceEdges := map[string][]string{}
	directWrites := map[string]bool{}
	serviceMetaByID := map[string]serviceMeta{}
	serviceItemIDs := map[string]bool{}
	stepIDs := map[string]bool{}
	stepLifecycle := map[string][]string{}
	serviceEvents := map[string]map[string]bool{}
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
			requiredParams := map[string]bool{}
			paramTypes := map[string]bool{}
			paramDecls := map[string]string{}
			paramNullable := map[string]bool{}
			typeSchemas := map[string]map[string]any{}
			if types, ok := asMap(doc["schemas"]); ok {
				for name, raw := range types {
					if def, ok := asMap(raw); ok {
						typeSchemas[fileID+".schemas."+name] = def
					}
				}
			}
			for _, rawParam := range asList(item["params"]) {
				pm, _ := asMap(rawParam)
				pn, _ := pm["name"].(string)
				if pn != "" {
					params[pn] = true
					// ctx 由生成器注入，不作为调用方 input。
					if pn != "ctx" {
						if nullable, _ := pm["nullable"].(bool); !nullable {
							requiredParams[pn] = true
						}
					}
				}
				nullable, _ := pm["nullable"].(bool)
				paramNullable[pn] = nullable
				pt, _ := pm["type"].(string)
				if pt != "" {
					paramDecls[pn] = pt
				}
				pt = strings.TrimPrefix(pt, "*")
				if strings.HasPrefix(pt, "List<") && strings.HasSuffix(pt, ">") {
					pt = strings.TrimSuffix(strings.TrimPrefix(pt, "List<"), ">")
				}
				if pt != "" {
					paramTypes[pt] = true
				}
			}
			// service 不再声明 errors：错误集由各 step 的声明与 query 默认值推导，
			// 用于 kind=service 的 step.errors 子集校验。
			errs := map[string]bool{}
			for _, rawStep := range asList(item["steps"]) {
				step, _ := asMap(rawStep)
				kind, _ := step["kind"].(string)
				if kind == "query" && step["errors"] == nil {
					for _, rawQ := range stepQueryItems(step) {
						qo, _ := asMap(rawQ)
						qid, _ := qo["ref"].(string)
						if q, ok := c.queries[qid]; ok {
							for _, code := range q.Errors {
								errs[code] = true
							}
						}
					}
					continue
				}
				for _, rawErr := range asList(step["errors"]) {
					code, _ := rawErr.(string)
					if code != "" {
						errs[code] = true
					}
				}
			}
			txMode, _ := item["tx_mode"].(string)
			returns := map[string]returnType{}
			for _, rawReturn := range asList(item["returns"]) {
				rm, _ := asMap(rawReturn)
				rn, _ := rm["name"].(string)
				rt, _ := rm["type"].(string)
				nullable, _ := rm["nullable"].(bool)
				if rn != "" && rt != "" && rn != "err" {
					returns[rn] = returnType{typ: rt, nullable: nullable}
				}
			}
			c.methodReturns[id] = returns
			serviceMetaByID[id] = serviceMeta{module: module, visibility: visibility, txMode: txMode, params: params, requiredParams: requiredParams, paramTypes: paramTypes, paramDecls: paramDecls, paramNullable: paramNullable, typeSchemas: typeSchemas, errors: errs, returns: returns}
			stepQueries := map[string]bool{}
			for _, rawStep := range asList(item["steps"]) {
				step, _ := asMap(rawStep)
				for _, rawS := range stepServiceItems(step) {
					so, _ := asMap(rawS)
					if ref, _ := so["ref"].(string); ref != "" {
						serviceEdges[id] = append(serviceEdges[id], ref)
					}
				}
				for _, rawQ := range stepQueryItems(step) {
					qo, _ := asMap(rawQ)
					qid, _ := qo["ref"].(string)
					if qid == "" {
						continue
					}
					stepQueries[qid] = true
					if q, ok := c.queries[qid]; ok && (len(q.Writes) > 0 || queryReturnsAffectedRows(q)) {
						directWrites[id] = true
					}
				}
			}
			c.serviceQueries[id] = stepQueries
			for _, rawStep := range asList(item["steps"]) {
				step, _ := asMap(rawStep)
				sid, _ := step["id"].(string)
				if sid == "" {
					continue
				}
				stepID := id + "." + sid
				stepIDs[stepID] = true
				for _, rawL := range asList(step["lifecycles"]) {
					action, _ := rawL.(string)
					if action != "" {
						stepLifecycle[stepID] = append(stepLifecycle[stepID], action)
					}
				}
				for _, rawE := range asList(step["events"]) {
					em, _ := asMap(rawE)
					ref, _ := em["ref"].(string)
					if ref == "" {
						continue
					}
					if serviceEvents[id] == nil {
						serviceEvents[id] = map[string]bool{}
					}
					serviceEvents[id][ref] = true
				}
			}
		}
	}

	c.serviceWrites = computeServiceWriteClosure(directWrites, serviceEdges)
	c.checkAccess(serviceItemIDs)

	// query 返回类型：供 step 输出路径解析；err 不参与结果形状。
	c.queryReturnTypes = map[string]map[string]returnType{}
	for qid, q := range c.queries {
		m := map[string]returnType{}
		for _, r := range q.Returns {
			if r.Kind == "error" {
				continue
			}
			typ := r.Type
			switch r.Kind {
			case "entity":
				typ = r.Entity
			case "scalar":
				typ = r.Type
			case "affected_rows":
				typ = "integer"
			}
			if typ == "" {
				typ = "any"
			}
			if r.List {
				typ = "List<" + typ + ">"
			}
			m[r.Name] = returnType{typ: typ, nullable: r.Nullable}
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
	c.checkSchemaCycles()

	// rules / states / events 先收集 ID，再互相校验。
	ruleIDs := map[string]bool{}
	ruleErrors := map[string]string{}
	ruleOwners := map[string][]string{}
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
			if code, _ := item["on_violation"].(string); code != "" {
				ruleErrors[id] = code
			}
			for _, rawOwner := range asList(item["implemented_by"]) {
				if owner, _ := rawOwner.(string); owner != "" {
					ruleOwners[id] = append(ruleOwners[id], owner)
				}
			}
		}
	}
	c.ruleErrors = ruleErrors
	c.ruleOwners = ruleOwners
	eventIDs := map[string]bool{}
	c.eventPayloads = map[string]map[string]eventField{}
	for _, file := range glob(c.root, filepath.Join(eventSchema.Targets.Dir, eventSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		for _, raw := range asList(doc["topics"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			if id == "" {
				continue
			}
			// 重复 ID 由 checkEventsDoc 的全局索引统一报出。
			eventIDs[id] = true
			c.eventPayloads[id] = eventPayloadFields(item["payload"])
		}
	}
	lifecycleActions := map[string]bool{}
	lifecycleStates := map[string]map[string]bool{}
	requiresGraph := map[string][]string{}
	dimensions := map[string]string{}
	enumStates := map[string]map[string]bool{}
	enumOwners := map[string]map[string]bool{}
	for _, file := range glob(c.root, filepath.Join(lifecycleSchema.Targets.Dir, lifecycleSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		fileID, _ := doc["id"].(string)
		subjects := c.lifecycleSubjects(doc["subjects"])
		states := map[string]bool{}
		statesObj, _ := asMap(doc["states"])
		for _, rawState := range asList(statesObj["values"]) {
			if name, _ := rawState.(string); name != "" {
				states[name] = true
			}
		}
		lifecycleStates[fileID] = states
		for _, subj := range subjects {
			if subj.Entity == "" || subj.Field == "" {
				continue
			}
			key := subj.Entity + "." + subj.Field
			if prev, ok := dimensions[key]; ok {
				c.rulef("lifecycle-dimension", "%s: dimension %s already declared by %s", rel, key, prev)
			}
			dimensions[key] = fileID
			if subj.Enum != "" && len(states) > 0 {
				if enumStates[subj.Enum] == nil {
					enumStates[subj.Enum] = map[string]bool{}
				}
				for name := range states {
					enumStates[subj.Enum][name] = true
				}
				if enumOwners[subj.Enum] == nil {
					enumOwners[subj.Enum] = map[string]bool{}
				}
				enumOwners[subj.Enum][fileID] = true
			}
		}
		if _, ok := asMap(doc["create"]); ok {
			lifecycleActions[fileID+".create"] = true
		}
		if _, ok := asMap(doc["destroy"]); ok {
			lifecycleActions[fileID+".destroy"] = true
		}
		collectRequires := func(item map[string]any) {
			for _, rawReq := range asList(item["requires"]) {
				rm, _ := asMap(rawReq)
				ref, _ := rm["lifecycle"].(string)
				if ref != "" && ref != fileID {
					requiresGraph[fileID] = append(requiresGraph[fileID], ref)
				}
			}
		}
		if create, ok := asMap(doc["create"]); ok {
			collectRequires(create)
		}
		if destroy, ok := asMap(doc["destroy"]); ok {
			collectRequires(destroy)
		}
		for _, raw := range asList(doc["transitions"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name != "" {
				lifecycleActions[fileID+".transitions."+name] = true
			}
			collectRequires(item)
		}
	}

	c.checkLifecycleEnumCoverage(enumStates, enumOwners)
	c.checkLifecycleRequiresCycles(requiresGraph)

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
			c.checkServiceItem(item, relPath(c.root, file), fileID+"."+name, module, serviceMetaByID, ruleIDs, eventIDs, lifecycleActions, typeNames)
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
	actionSteps := map[string][]string{}
	for stepID, actions := range stepLifecycle {
		for _, action := range actions {
			actionSteps[action] = append(actionSteps[action], stepID)
		}
	}
	for _, file := range glob(c.root, filepath.Join(lifecycleSchema.Targets.Dir, lifecycleSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		c.checkLifecycleDoc(doc, relPath(c.root, file), lifecycleSchema, stepIDs, actionSteps, serviceEvents, eventIDs, lifecycleStates)
	}
	jobServices := c.checkEventsDoc(eventSchema, serviceMetaByID, serviceItemIDs)
	c.checkHTTPBindings(httpSchema, serviceMetaByID, typeNames, jobServices)
}
