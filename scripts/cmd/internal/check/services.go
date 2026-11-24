// services.go 校验 cap.services 的文档、服务项与步骤。
package check

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// stepKindFields 是每种 step kind 的专属字段；公共字段由 checkServiceItem 统一放行。
var stepKindFields = map[string][]string{
	"query":   {"input", "output", "query"},
	"service": {"input", "output", "service"},
	"logic":   {"input", "output", "rules"},
	"events":  {"input", "output", "events"},
	"when":    {"input", "output", "rules", "when"},
}

const (
	queryRefPattern   = `^data\.queries\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`
	serviceRefPattern = `^cap\.services\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`
)

func (c *Checker) checkServiceDoc(doc map[string]any, rel string, schema *Schema, typeNames map[string]bool) {
	c.nodeKinds = schema.NodeKinds
	c.propertyKeys = schema.PropertyKeys
	c.valueTypes = schema.ValueTypes
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	c.checkTargetFileName(schema, filepath.Base(rel), rel)
	c.checkStability(doc, rel)
	fileID, _ := doc["id"].(string)
	want := schema.Targets.FileID(filepath.Base(rel))
	if fileID != want {
		c.rulef("service-id", "%s: id %q must be %q", rel, fileID, want)
	}
	if types, ok := asMap(doc["schemas"]); ok {
		c.checkTypeRefs(types, typeNames, rel)
		c.checkPageSchemas(types, rel)
	}
	seen := map[string]bool{}
	c.checkServiceUsed(rel, doc)
	for _, raw := range asList(doc["services"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if seen[name] {
			c.rulef("service-unique", "%s: duplicate service %s", rel, name)
		}
		seen[name] = true
		params := asList(item["params"])
		if len(params) == 0 {
			c.rulef("service-signature", "%s.%s: params[0] must be ctx", rel, name)
		} else if first, _ := asMap(params[0]); first["name"] != "ctx" || first["type"] != "context.Context" {
			c.rulef("service-signature", "%s.%s: params[0] must be ctx context.Context", rel, name)
		}
		seenParams := map[string]bool{}
		for _, rawParam := range params {
			pm, _ := asMap(rawParam)
			if pn, _ := pm["name"].(string); pn != "" {
				if seenParams[pn] {
					c.rulef("service-signature", "%s.%s: duplicate param %s", rel, name, pn)
				}
				seenParams[pn] = true
			}
			c.checkServiceParamDefault(pm, rel, name)
		}
		returns := asList(item["returns"])
		if len(returns) == 0 {
			c.rulef("service-signature", "%s.%s: returns must end with err error", rel, name)
		} else if last, _ := asMap(returns[len(returns)-1]); last["name"] != "err" || last["type"] != "error" {
			c.rulef("service-signature", "%s.%s: returns must end with err error", rel, name)
		}
		seenReturns := map[string]bool{}
		for _, rawReturn := range returns {
			rm, _ := asMap(rawReturn)
			rn, _ := rm["name"].(string)
			if rn != "" {
				if seenReturns[rn] {
					c.rulef("service-signature", "%s.%s: duplicate return %s", rel, name, rn)
				}
				seenReturns[rn] = true
			}
			if rn == "err" {
				if value, _ := rm["value"].(string); value != "*" {
					c.rulef("service-signature", "%s.%s: return err must declare value \"*\"", rel, name)
				}
			}
		}
		for _, rawParam := range asList(item["params"]) {
			pm, _ := asMap(rawParam)
			c.checkTypeName(pm["type"], typeNames, rel+":"+name)
		}
		for _, rawReturn := range asList(item["returns"]) {
			rm, _ := asMap(rawReturn)
			if rn, _ := rm["name"].(string); rn == "err" {
				continue
			}
			c.checkTypeName(rm["type"], typeNames, rel+":"+name)
		}
	}
}

// checkPageSchemas 要求 *Page schema 使用统一分页形状。
func (c *Checker) checkPageSchemas(types map[string]any, rel string) {
	for name, raw := range types {
		if !strings.HasSuffix(name, "Page") {
			continue
		}
		schema, _ := asMap(raw)
		props := map[string]map[string]any{}
		for _, rawProp := range asList(schema["properties"]) {
			p, _ := asMap(rawProp)
			if field, _ := p["name"].(string); field != "" {
				props[field] = p
			}
		}
		want := map[string]string{
			"items": "_types.array", "offset": "_types.page_offset",
			"limit": "_types.page_limit", "has_more": "_types.boolean",
		}
		for field, typ := range want {
			p := props[field]
			if p == nil {
				c.rulef("service-page-schema", "%s: schema %s is missing pagination field %s", rel, name, field)
				continue
			}
			if required, _ := p["required"].(bool); !required {
				c.rulef("service-page-schema", "%s: schema %s pagination field %s must be required", rel, name, field)
			}
			if got, _ := asMap(p["schema"]); got != nil {
				if value, _ := got["type"].(string); value != typ {
					c.rulef("service-page-schema", "%s: schema %s field %s type must be %s, got %s", rel, name, field, typ, value)
				}
			}
		}
	}
}

// checkServiceUsed 要求 stable 文件的 service 至少有一个使用点。
func (c *Checker) checkServiceUsed(rel string, doc map[string]any) {
	if stability, _ := doc["stability"].(string); stability != "stable" {
		return
	}
	for _, raw := range asList(doc["services"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if rawUsed, ok := item["used_by"]; ok && len(asList(rawUsed)) == 0 {
			c.rulef("service-used", "%s.%s: stable service has empty used_by", rel, name)
		}
	}
}

func (c *Checker) checkServiceItem(doc map[string]any, rel, id, module string, serviceMeta map[string]serviceMeta, ruleIDs, eventIDs map[string]bool, lifecycleActions map[string]bool, typeNames map[string]bool) {
	steps := asList(doc["steps"])
	if len(steps) == 0 {
		return // 空列表由 schema.min_len 报出
	}
	stepOutputs := make([]map[string]returnType, len(steps))
	stepIndex := map[string]int{}
	allIndex := map[string]int{}
	for idx, raw := range steps {
		step, _ := asMap(raw)
		if sid, _ := step["id"].(string); sid != "" {
			allIndex[sid] = idx
		}
	}
	tc := stepTypeContext{doc: doc, stepIndex: stepIndex, steps: steps, stepOutputs: stepOutputs}
	reach := buildStepReach(steps, allIndex)
	stepRefs := make([]map[string]bool, len(steps))
	exitRefs := map[string]bool{}
	consumed := map[string]bool{}
	usedParams := map[string]bool{}
	for i, raw := range steps {
		step, _ := asMap(raw)
		stepID, _ := step["id"].(string)
		if _, exists := stepIndex[stepID]; exists {
			c.rulef("service-unique", "%s: duplicate step id %s", rel, stepID)
		} else {
			stepIndex[stepID] = i
		}
		kind, _ := step["kind"].(string)
		txMode, _ := doc["tx_mode"].(string)
		stepRefs[i] = map[string]bool{}
		input := c.parseInputMap(step["input"], rel, stepID, "step")
		checkInputValues := func(refs map[string]any) {
			for _, rawExpr := range refs {
				c.checkServiceParamExpr(rawExpr, doc, stepIndex, allIndex, reach, stepOutputs, steps, rel, stepID)
				expr, _ := rawExpr.(string)
				if root, ok := inputExprRoot(expr); ok {
					usedParams[root] = true
				}
				if ref, field, ok := splitStepField(expr); ok {
					consumed[ref+"."+outputRoot(field)] = true
					stepRefs[i][ref] = true
				}
			}
		}
		checkInputValues(input)
		c.checkStepWhen(step, input, kind, tc, rel, stepID, allIndex, i)
		if target, _ := step["goto"].(string); target != "" {
			c.checkGoto(rel, stepID, target, allIndex, i)
		}
		c.checkServiceStateGate(step, rel, stepID)
		allowed := map[string]bool{
			"id": true, "kind": true, "summary": true, "state_gate": true, "errors": true, "lifecycles": true, "goto": true,
		}
		for _, field := range stepKindFields[kind] {
			allowed[field] = true
		}
		for field := range step {
			if !allowed[field] {
				c.rulef("service-step-fields", "%s: step %s kind=%s does not allow field %s", rel, stepID, kind, field)
			}
		}

		var targetErrors, queryErrors map[string]bool
		switch kind {
		case "query":
			queryErrors = map[string]bool{}
			qid, _ := step["query"].(string)
			if qid == "" {
				c.rulef("service-step-target", "%s: step %s kind=query requires query ref", rel, stepID)
			} else if !match(queryRefPattern, qid) {
				c.rulef("service-ref", "%s: step %s query ref %s does not match data.queries.<file>.<name>", rel, stepID, qid)
			} else if q, ok := c.queries[qid]; !ok {
				c.rulef("service-ref", "%s: step %s unknown query %s", rel, stepID, qid)
			} else {
				c.checkQueryInput(input, q, qid, rel, stepID, tc)
				c.checkStepOutputTarget(step, c.queryReturnTypes[qid], "query "+qid, rel, stepID)
				for code := range q.errorSet() {
					queryErrors[code] = true
				}
				if txMode == "read_only" && (len(q.Writes) > 0 || queryReturnsAffectedRows(q)) {
					c.rulef("service-transaction", "%s: step %s calls write query %s under tx_mode=read_only", rel, stepID, qid)
				}
			}
		case "service":
			targetErrors = map[string]bool{}
			ref, _ := step["service"].(string)
			if ref == "" {
				c.rulef("service-step-target", "%s: step %s kind=service requires service ref", rel, stepID)
			} else if !match(serviceRefPattern, ref) {
				c.rulef("service-ref", "%s: step %s service ref %s does not match cap.services.<file>.<name>", rel, stepID, ref)
			} else if target, ok := serviceMeta[ref]; !ok {
				c.rulef("service-ref", "%s: step %s unknown service %s", rel, stepID, ref)
			} else {
				if target.module != module && target.visibility != "public" {
					c.rulef("service-visibility", "%s: step %s cannot call internal %s from another module", rel, stepID, ref)
				}
				if txMode != "none" && target.txMode == "required" {
					c.rulef("service-transaction", "%s: step %s tx_mode=%s must not call tx_mode=required service %s (nested transaction)", rel, stepID, txMode, ref)
				}
				if txMode == "read_only" && c.serviceWrites[ref] {
					c.rulef("service-transaction", "%s: step %s tx_mode=read_only cannot call service %s that writes tables", rel, stepID, ref)
				}
				c.checkServiceInput(input, target, ref, rel, stepID, tc)
				c.checkStepOutputTarget(step, target.returns, "service "+ref, rel, stepID)
				for code := range target.errors {
					targetErrors[code] = true
				}
			}
		case "logic":
			if step["output"] == nil {
				c.rulef("service-step-shape", "%s: step %s kind=logic requires output", rel, stepID)
			}
			if step["errors"] == nil {
				c.rulef("service-step-shape", "%s: step %s kind=logic requires errors", rel, stepID)
			}
			if step["rules"] == nil {
				c.rulef("service-step-shape", "%s: step %s kind=logic requires rules (may be empty)", rel, stepID)
			}
			c.checkLogicRefs(step, rel, stepID, ruleIDs, eventIDs)
		case "events":
			if len(asList(step["output"])) > 0 {
				c.rulef("service-step-shape", "%s: step %s kind=events output must be empty", rel, stepID)
			}
			c.checkStepEvents(step, input, rel, stepID, eventIDs, tc)
		case "when":
			if len(asList(step["output"])) > 0 {
				c.rulef("service-step-shape", "%s: step %s kind=when output must be empty", rel, stepID)
			}
			if step["when"] == nil {
				c.rulef("service-step-shape", "%s: step %s kind=when requires when", rel, stepID)
			}
			c.checkLogicRefs(step, rel, stepID, ruleIDs, eventIDs)
		}

		lifecycles := asList(step["lifecycles"])
		seenLc := map[string]bool{}
		for _, rawL := range lifecycles {
			actionID, _ := rawL.(string)
			if seenLc[actionID] {
				c.rulef("service-unique", "%s: step %s duplicate lifecycle action %s", rel, stepID, actionID)
			}
			seenLc[actionID] = true
			if _, ok := lifecycleActions[actionID]; !ok {
				c.rulef("service-ref", "%s: step %s unknown lifecycle action %s", rel, stepID, actionID)
			}
		}

		stepOutput := map[string]returnType{}
		if step["output"] != nil {
			seenOut := map[string]bool{}
			for _, rawOut := range asList(step["output"]) {
				om, _ := asMap(rawOut)
				name, _ := om["name"].(string)
				typ, _ := om["type"].(string)
				if seenOut[name] {
					c.rulef("service-unique", "%s: step %s duplicate output %s", rel, stepID, name)
				}
				seenOut[name] = true
				c.checkTypeName(om["type"], typeNames, rel+":"+stepID)
				if name != "" && typ != "" {
					nullable, _ := om["nullable"].(bool)
					stepOutput[name] = returnType{typ: typ, nullable: nullable}
				}
			}
		}
		stepOutputs[i] = stepOutput

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

		if kind == "logic" || kind == "when" {
			for _, rawRule := range asList(step["rules"]) {
				rm, _ := asMap(rawRule)
				rid, _ := rm["rule"].(string)
				for _, code := range c.ruleErrors[rid] {
					if !stepErrors[code] {
						c.rulef("service-errors", "%s: step %s rule %s error %s is not declared by this step", rel, stepID, rid, code)
					}
				}
			}
		}

	}

	// 业务返回值必须有显式来源；来源的静态类型与 nullable 必须与 returns 声明一致。
	for _, rawReturn := range asList(doc["returns"]) {
		rm, _ := asMap(rawReturn)
		rn, _ := rm["name"].(string)
		if rn == "" {
			continue
		}
		if rn == "err" {
			if value, _ := rm["value"].(string); value != "*" {
				c.rulef("service-signature", "%s: return err must declare value \"*\"", rel)
			}
			continue
		}
		value, _ := rm["value"].(string)
		if value == "*" {
			c.rulef("service-output-value", "%s: return %s must not use \"*\"; only err may", rel, rn)
			continue
		}
		if strings.TrimSpace(value) == "" {
			c.rulef("service-output-value", "%s: return %s requires value (params.<path> or step.<id>.<path>)", rel, rn)
			continue
		}
		c.checkServiceParamExpr(value, doc, stepIndex, allIndex, reach, stepOutputs, steps, rel, "returns."+rn)
		rtyp, _ := rm["type"].(string)
		rnullable, _ := rm["nullable"].(bool)
		src, err := c.inputValueType(value, tc)
		if err != nil {
			continue
		}
		if !sameServiceType(src, rtyp) {
			c.rulef("service-output-type", "%s: return %s type %s does not match source %s type %s", rel, rn, rtyp, value, src)
		} else if nullable, known := c.inputValueNullable(value, tc); known && nullable != rnullable {
			c.rulef("service-output-nullable", "%s: return %s nullability (nullable=%t) does not match source %s nullable=%t", rel, rn, rnullable, value, nullable)
		}
		if ref, field, ok := splitStepField(value); ok {
			consumed[ref+"."+outputRoot(field)] = true
			exitRefs[ref] = true
		}
	}
	c.checkServiceMerge(rel, steps, allIndex, reach, stepRefs, exitRefs)

	// 显式 output 必须被后续 step 的 input 消费，或被 returns.value 引用。
	for i, raw := range steps {
		step, _ := asMap(raw)
		stepID, _ := step["id"].(string)
		kind, _ := step["kind"].(string)
		if kind == "events" {
			continue
		}
		for name := range stepOutputs[i] {
			if consumed[stepID+"."+name] {
				continue
			}
			c.rulef("service-chain", "%s: step %s output %s is not consumed and is not a service return", rel, stepID, name)
		}
	}

	for _, rawParam := range asList(doc["params"]) {
		pm, _ := asMap(rawParam)
		name, _ := pm["name"].(string)
		if name == "" || name == "ctx" {
			continue
		}
		if !usedParams[name] {
			c.rulef("service-chain", "%s: param %s is not consumed by any step input", rel, name)
		}
	}

	// 每个 step 必须能从首步经 goto/when 分支到达。
	for i := 1; i < len(steps); i++ {
		if !reach[0][i] {
			step, _ := asMap(steps[i])
			stepID, _ := step["id"].(string)
			c.rulef("service-goto", "%s: step %s is unreachable from the first step", rel, stepID)
		}
	}
}

// parseInputMap 把 {name, value, description} 列表解析为 name -> value。
// description 的必填由 _meta 的 item_fields 校验。
func (c *Checker) parseInputMap(v any, rel, stepID, where string) map[string]any {
	out := map[string]any{}
	if v == nil {
		return out
	}
	items, ok := asList2(v)
	if !ok {
		c.rulef("service-step-fields", "%s: step %s %s input must be a list", rel, stepID, where)
		return out
	}
	seen := map[string]bool{}
	for _, raw := range items {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		value, _ := item["value"].(string)
		if name == "" || value == "" {
			c.rulef("service-step-fields", "%s: step %s %s input item requires name and value", rel, stepID, where)
			continue
		}
		if seen[name] {
			c.rulef("service-unique", "%s: step %s %s duplicate input %s", rel, stepID, where, name)
		}
		seen[name] = true
		out[name] = value
	}
	return out
}

// checkStepEvents 校验 kind=events 的唯一事件引用与 payload 映射。
func (c *Checker) checkStepEvents(step map[string]any, input map[string]any, rel, stepID string, eventIDs map[string]bool, tc stepTypeContext) {
	items, ok := asList2(step["events"])
	if !ok {
		c.rulef("service-step-target", "%s: step %s kind=events requires events", rel, stepID)
		return
	}
	if len(items) == 0 {
		return // 空列表由 schema.min_len 报出
	}
	refs := make([]string, 0, len(items))
	seen := map[string]bool{}
	union := map[string]eventField{}
	for _, raw := range items {
		item, _ := asMap(raw)
		if item == nil {
			c.rulef("service-step-shape", "%s: step %s events item must be an object", rel, stepID)
			continue
		}
		ref, _ := item["ref"].(string)
		if ref == "" {
			c.rulef("service-step-shape", "%s: step %s events item requires ref", rel, stepID)
			continue
		}
		if seen[ref] {
			c.rulef("service-unique", "%s: step %s duplicate event %s", rel, stepID, ref)
			continue
		}
		seen[ref] = true
		if !eventIDs[ref] {
			c.rulef("service-ref", "%s: step %s unknown event %s", rel, stepID, ref)
			continue
		}
		refs = append(refs, ref)
		for name, field := range c.eventPayloads[ref] {
			union[name] = field
		}
	}
	if len(refs) == 0 {
		return
	}
	for name := range input {
		if _, ok := union[name]; !ok {
			c.rulef("service-input-name", "%s: step %s event input %s is not a payload field of any event in this step", rel, stepID, name)
		}
	}
	for _, ref := range refs {
		c.checkEventInput(input, ref, rel, stepID, tc)
	}
}

// checkEventInput 校验事件 payload 映射：字段必须存在、必填字段必须出现、类型必须兼容。
func (c *Checker) checkEventInput(input map[string]any, eventID, rel, stepID string, tc stepTypeContext) {
	fields := c.eventPayloads[eventID]
	for name, raw := range input {
		field, ok := fields[name]
		if !ok {
			continue
		}
		value, _ := raw.(string)
		src, err := c.inputValueType(value, tc)
		if err != nil {
			continue
		}
		if field.typ != "" && !c.serviceTypesCompatible(src, field.typ) {
			c.rulef("service-input-type", "%s: step %s event %s input %s type %s is not compatible with %s", rel, stepID, eventID, name, src, field.typ)
		}
		if nullable, known := c.inputValueNullable(value, tc); known && nullable && !field.nullable {
			c.rulef("service-input-nullable", "%s: step %s event %s input %s source is nullable but target payload field is not", rel, stepID, eventID, name)
		}
	}
	for name, field := range fields {
		if !field.required {
			continue
		}
		if _, ok := input[name]; !ok {
			c.rulef("service-input-required", "%s: step %s event %s missing required payload field %s", rel, stepID, eventID, name)
		}
	}
}

// checkGoto 校验跳转目标是同 service 内更靠后的 step（前向，无环）。
func (c *Checker) checkGoto(rel, stepID, target string, allIndex map[string]int, pos int) {
	if target == "$end" {
		return
	}
	t, ok := allIndex[target]
	if !ok {
		c.rulef("service-goto", "%s: step %s goto unknown step %s", rel, stepID, target)
		return
	}
	if t <= pos {
		c.rulef("service-goto", "%s: step %s goto %s must target a later step", rel, stepID, target)
	}
}

// checkStepWhen 校验 kind=when 的条件分支：input 引用本步 input，expr 只能引用本步 input。
// checkServiceStateGate 校验 step.state_gate：省略表示全部允许；
// transition 必须写且等于 transition.from；destroy 必须写且等于全部非终态；
// create 可省略，写了必须等于全部状态。
func (c *Checker) checkServiceStateGate(step map[string]any, rel, stepID string) {
	expected := map[string]map[string]bool{}
	allAllowed := map[string]bool{}
	for _, raw := range asList(step["lifecycles"]) {
		action, _ := raw.(string)
		for lifecycleID, states := range c.lifecycleActionStates[action] {
			if expected[lifecycleID] == nil {
				expected[lifecycleID] = map[string]bool{}
			}
			for state := range states {
				expected[lifecycleID][state] = true
			}
			allAllowed[lifecycleID] = sameSet(expected[lifecycleID], c.lifecycleStates[lifecycleID])
		}
	}
	related := c.stepTouchedLifecycles(step)
	for _, raw := range asList(step["lifecycles"]) {
		action, _ := raw.(string)
		for lifecycleID := range c.lifecycleActionStates[action] {
			related[lifecycleID] = true
		}
	}
	declared := map[string]map[string]bool{}
	for i, raw := range asList(step["state_gate"]) {
		item, ok := asMap(raw)
		if !ok {
			c.rulef("service-state-gate", "%s: step %s state_gate[%d] must be an object", rel, stepID, i)
			continue
		}
		lifecycleID, _ := item["lifecycle"].(string)
		if lifecycleID == "" {
			c.rulef("service-state-gate", "%s: step %s state_gate[%d] requires lifecycle", rel, stepID, i)
			continue
		}
		if !related[lifecycleID] {
			c.rulef("service-state-gate", "%s: step %s state_gate lifecycle %s is not related to the step", rel, stepID, lifecycleID)
		}
		if declared[lifecycleID] != nil {
			c.rulef("service-state-gate", "%s: step %s repeats lifecycle %s in state_gate", rel, stepID, lifecycleID)
		}
		values := map[string]bool{}
		for _, rawValue := range asList(item["values"]) {
			value, _ := rawValue.(string)
			if value == "" {
				continue
			}
			if value == "*" {
				c.rulef("service-state-gate", "%s: step %s state_gate for %s must omit * when all states are allowed", rel, stepID, lifecycleID)
				continue
			}
			if !c.lifecycleStates[lifecycleID][value] {
				c.rulef("service-state-gate", "%s: step %s state_gate unknown state %s of %s", rel, stepID, value, lifecycleID)
				continue
			}
			values[value] = true
		}
		if len(values) == 0 {
			c.rulef("service-state-gate", "%s: step %s state_gate for %s must not be empty", rel, stepID, lifecycleID)
		} else if sameSet(values, c.lifecycleStates[lifecycleID]) {
			c.rulef("service-state-gate", "%s: step %s state_gate for %s must be omitted when all states are allowed", rel, stepID, lifecycleID)
		}
		declared[lifecycleID] = values
	}
	for lifecycleID, want := range expected {
		got := declared[lifecycleID]
		if allAllowed[lifecycleID] {
			if got != nil {
				c.rulef("service-state-gate", "%s: step %s state_gate for %s must be omitted when all states are allowed", rel, stepID, lifecycleID)
			}
			continue
		}
		if got == nil {
			c.rulef("service-state-gate", "%s: step %s action on %s requires state_gate", rel, stepID, lifecycleID)
			continue
		}
		if !sameSet(got, want) {
			c.rulef("service-state-gate", "%s: step %s state_gate for %s must be %v, got %v", rel, stepID, lifecycleID, sortedSetKeys(want), sortedSetKeys(got))
		}
	}
}

// stepTouchedLifecycles 返回 step 通过 query 表或 entity 输出触及的 lifecycle。
func (c *Checker) stepTouchedLifecycles(step map[string]any) map[string]bool {
	touched := map[string]bool{}
	for _, raw := range stepQueryItems(step) {
		qo, _ := asMap(raw)
		qid, _ := qo["ref"].(string)
		q := c.queries[qid]
		if q == nil {
			continue
		}
		for _, table := range q.Targets {
			for lifecycleID, tables := range c.lifecycleSubjectTables {
				if tables[table] {
					touched[lifecycleID] = true
				}
			}
		}
	}
	for _, raw := range asList(step["output"]) {
		om, _ := asMap(raw)
		typ, _ := om["type"].(string)
		entityID, ok := entityRef(typ)
		if !ok {
			continue
		}
		for lifecycleID, entities := range c.lifecycleSubjectEntities {
			if entities[entityID] {
				touched[lifecycleID] = true
			}
		}
	}
	return touched
}

func setKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	return out
}

func sortedSetKeys(m map[string]bool) []string {
	out := setKeys(m)
	sort.Strings(out)
	return out
}

func (c *Checker) checkStepWhen(step map[string]any, input map[string]any, kind string, tc stepTypeContext, rel, stepID string, allIndex map[string]int, pos int) {
	whens := asList(step["when"])
	if len(whens) == 0 {
		return
	}
	if kind != "when" {
		c.rulef("service-when-shape", "%s: step %s when is only allowed on kind=when", rel, stepID)
	}
	allReferenced := map[string]bool{}
	for _, rawWhen := range whens {
		wm, _ := asMap(rawWhen)
		if wm == nil {
			c.rulef("service-when-shape", "%s: step %s when item must be an object", rel, stepID)
			continue
		}
		subject, _ := wm["input"].(string)
		if subject == "" {
			c.rulef("service-when-shape", "%s: step %s when item requires input", rel, stepID)
		} else if _, ok := input[subject]; !ok {
			c.rulef("service-when-refs", "%s: step %s when input %s is not declared by step input", rel, stepID, subject)
		}
		expr, _ := wm["expr"].(string)
		if strings.TrimSpace(expr) == "" {
			c.rulef("service-when-shape", "%s: step %s when item requires expr", rel, stepID)
			continue
		}
		if target, _ := wm["goto"].(string); target == "" {
			c.rulef("service-when-shape", "%s: step %s when input %s requires goto", rel, stepID, subject)
		} else {
			c.checkGoto(rel, stepID, target, allIndex, pos)
		}
		node, err := parseWhen(expr)
		if err != nil {
			c.rulef("service-when-shape", "%s: step %s when expr %q: %v", rel, stepID, expr, err)
			continue
		}
		referenced := false
		for _, field := range whenFields(node, "") {
			if !strings.HasPrefix(field, "input.") {
				c.rulef("service-when-shape", "%s: step %s when expr %q may only reference input.<name>, got %s", rel, stepID, expr, field)
				continue
			}
			path := strings.TrimPrefix(field, "input.")
			name := strings.Split(path, ".")[0]
			allReferenced[name] = true
			if subject != "" && name == subject {
				referenced = true
			}
			if _, err := c.whenInputFieldType(path, input, tc); err != nil {
				c.rulef("service-when-shape", "%s: step %s when expr %q: %v", rel, stepID, expr, err)
			}
		}
		if subject != "" && !referenced {
			c.rulef("service-when-refs", "%s: step %s when input %s must be referenced by expr %q", rel, stepID, subject, expr)
		}
		walkWhenCompares(node, func(cmp *whenNode) {
			field, lit := cmp.left, cmp.right
			if field.kind != "field" && lit.kind == "field" {
				field, lit = lit, field
			}
			if field.kind != "field" || lit.kind != "literal" {
				return
			}
			path, _ := field.value.(string)
			if !strings.HasPrefix(path, "input.") {
				return
			}
			typ, err := c.whenInputFieldType(strings.TrimPrefix(path, "input."), input, tc)
			if err != nil {
				return
			}
			c.checkWhenLiteral(typ, lit.value, rel, stepID, path)
		})
	}
	for name := range input {
		if !allReferenced[name] {
			c.rulef("service-when-refs", "%s: step %s input %s is not referenced by any when branch", rel, stepID, name)
		}
	}
}

// whenInputFieldType 解析 when 表达式引用的 input.<name>[.<field>] 静态类型。
func (c *Checker) whenInputFieldType(path string, input map[string]any, tc stepTypeContext) (string, error) {
	segs := strings.Split(path, ".")
	name := segs[0]
	raw, ok := input[name]
	if !ok {
		return "", fmt.Errorf("input %s is not declared by step input", name)
	}
	value, _ := raw.(string)
	typ, err := c.inputValueType(value, tc)
	if err != nil {
		return "", err
	}
	prefix := "input." + name
	for _, seg := range segs[1:] {
		base := normalizeContractType(typ)
		if strings.HasPrefix(base, "List<") {
			return "", fmt.Errorf("input %s is a list; index it before .%s", prefix, seg)
		}
		next, ok := c.resolveTypeField(base, seg, whenFormula{}, "", prefix)
		if !ok {
			return "", fmt.Errorf("unknown field %s of %s", seg, typ)
		}
		typ = next
		prefix += "." + seg
	}
	return typ, nil
}

func (c *Checker) checkWhenLiteral(typ string, value any, rel, stepID, name string) {
	if value == nil {
		return
	}
	shape, ok := parseTypeShape(typ)
	if !ok {
		return
	}
	if shape.array {
		c.rulef("service-when-types", "%s: step %s when field %s is a list and cannot be compared", rel, stepID, name)
		return
	}
	if strings.HasPrefix(shape.base, "data.enums.") {
		s, ok := value.(string)
		if !ok {
			c.rulef("service-when-types", "%s: step %s when field %s expects enum name, got %T", rel, stepID, name, value)
			return
		}
		if values, ok := c.enumValues[shape.base]; ok && !values[s] {
			c.rulef("service-when-types", "%s: step %s when field %s has unknown enum value %s", rel, stepID, name, s)
		}
		return
	}
	family, ok := paramFamily(shape.base)
	if !ok {
		return
	}
	switch family {
	case "boolean":
		if _, ok := value.(bool); !ok {
			c.rulef("service-when-types", "%s: step %s when field %s expects boolean, got %T", rel, stepID, name, value)
		}
	case "integer":
		switch v := value.(type) {
		case int, int64, uint64:
		case float64:
			if v != float64(int64(v)) {
				c.rulef("service-when-types", "%s: step %s when field %s expects integer, got %T", rel, stepID, name, value)
			}
		default:
			c.rulef("service-when-types", "%s: step %s when field %s expects integer, got %T", rel, stepID, name, value)
		}
	case "string":
		if _, ok := value.(string); !ok {
			c.rulef("service-when-types", "%s: step %s when field %s expects string, got %T", rel, stepID, name, value)
		}
	}
}

// checkStepOutputTarget 校验 query/service step 的 output 与目标 returns 完全一致。
func (c *Checker) checkStepOutputTarget(step map[string]any, target map[string]returnType, where, rel, stepID string) {
	for _, raw := range asList(step["output"]) {
		om, _ := asMap(raw)
		name, _ := om["name"].(string)
		typ, _ := om["type"].(string)
		if name == "" || typ == "" {
			continue // 结构错误由通用校验报
		}
		rt, ok := target[name]
		if !ok {
			c.rulef("service-output", "%s: step %s %s has no return %s", rel, stepID, where, name)
			continue
		}
		if !sameServiceType(typ, rt.typ) {
			c.rulef("service-output-type", "%s: step %s output %s type %s does not match %s return type %s", rel, stepID, name, typ, where, rt.typ)
			continue
		}
		nullable, _ := om["nullable"].(bool)
		targetNullable := rt.nullable || strings.HasPrefix(strings.TrimSpace(rt.typ), "*")
		if nullable != targetNullable {
			c.rulef("service-output-nullable", "%s: step %s output %s nullable=%t does not match %s return nullable=%t", rel, stepID, name, nullable, where, targetNullable)
		}
	}
}

func (c *Checker) checkLogicRefs(step map[string]any, rel, stepID string, ruleIDs, eventIDs map[string]bool) {
	seen := map[string]bool{}
	for _, rawRule := range asList(step["rules"]) {
		rm, _ := asMap(rawRule)
		if rm == nil {
			c.rulef("service-step-shape", "%s: step %s rules item must be an object", rel, stepID)
			continue
		}
		rid, _ := rm["rule"].(string)
		if rid == "" {
			c.rulef("service-step-shape", "%s: step %s rules item requires rule", rel, stepID)
			continue
		}
		if seen[rid] {
			c.rulef("service-unique", "%s: step %s duplicate rule %s", rel, stepID, rid)
		}
		seen[rid] = true
		if !ruleIDs[rid] {
			c.rulef("service-ref", "%s: step %s unknown rule %s", rel, stepID, rid)
		}
	}
}
