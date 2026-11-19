// services.go 校验 cap.services 的文档、服务项与步骤。
package check

import (
	"fmt"
	"path/filepath"
	"strings"
)

// stepKindFields 是每种 step kind 的专属字段；公共字段由 checkServiceItem 统一放行。
var stepKindFields = map[string][]string{
	"query":   {"query"},
	"service": {"service"},
	"logic":   {"input", "output", "rules"},
	"events":  {"events"},
	"when":    {"input", "output", "rules", "when"},
}

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
		for name := range types {
			if !match(`^[A-Za-z][A-Za-z0-9]*$`, name) {
				c.rulef("service-structure", "%s: schema name %q must be a Go identifier", rel, name)
			}
		}
		c.checkTypeRefs(types, typeNames, rel)
	}
	seen := map[string]bool{}
	for _, raw := range asList(doc["services"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if seen[name] {
			c.rulef("service-structure", "%s: duplicate service %s", rel, name)
		}
		seen[name] = true
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
			c.checkServiceParamDefault(pm, rel, name)
		}
		returns := asList(item["returns"])
		if len(returns) == 0 {
			c.rulef("service-structure", "%s.%s: returns must end with err error", rel, name)
		} else if last, _ := asMap(returns[len(returns)-1]); last["name"] != "err" || last["type"] != "error" {
			c.rulef("service-structure", "%s.%s: returns must end with err error", rel, name)
		}
		seenReturns := map[string]bool{}
		for _, rawReturn := range returns {
			rm, _ := asMap(rawReturn)
			rn, _ := rm["name"].(string)
			if rn != "" {
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
			if rn, _ := rm["name"].(string); rn == "err" {
				continue
			}
			c.checkTypeName(rm["type"], typeNames, rel+":"+name)
		}
	}
}

func (c *Checker) checkServiceItem(doc map[string]any, rel, id, module string, serviceMeta map[string]serviceMeta, ruleIDs, eventIDs map[string]bool, lifecycleActions map[string]lifecycleAction, typeNames map[string]bool) {
	steps := asList(doc["steps"])
	if len(steps) == 0 {
		c.rulef("service-structure", "%s: service requires steps", rel)
		return
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
	produced := map[string][]returnType{}
	consumed := map[string]bool{}
	usedParams := map[string]bool{}
	for i, raw := range steps {
		step, _ := asMap(raw)
		stepID, _ := step["id"].(string)
		if _, exists := stepIndex[stepID]; exists {
			c.rulef("service-structure", "%s: duplicate step id %s", rel, stepID)
		} else {
			stepIndex[stepID] = i
		}
		kind, _ := step["kind"].(string)
		txMode, _ := doc["tx_mode"].(string)
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
				}
			}
		}
		checkInputValues(input)
		c.checkStepWhen(step, input, kind, tc, rel, stepID, allIndex, i)
		if target, _ := step["goto"].(string); target != "" {
			c.checkGoto(rel, stepID, target, allIndex, i)
		}
		allowed := map[string]bool{
			"id": true, "kind": true, "summary": true, "depends_on": true, "errors": true, "lifecycles": true, "goto": true,
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
			qo, _ := asMap(step["query"])
			queryErrors = map[string]bool{}
			if qo == nil {
				c.rulef("service-steps-kind", "%s: step %s kind=query requires query", rel, stepID)
			} else {
				for field := range qo {
					if field != "query" && field != "input" {
						c.rulef("service-steps-kind", "%s: step %s query does not allow field %s", rel, stepID, field)
					}
				}
				qid, _ := qo["query"].(string)
				if qid == "" {
					c.rulef("service-steps-kind", "%s: step %s query requires query id", rel, stepID)
				} else if q, ok := c.queries[qid]; !ok {
					c.rulef("service-ref", "%s: step %s unknown query %s", rel, stepID, qid)
				} else {
					qInput := c.parseInputMap(qo["input"], rel, stepID, "query "+qid)
					c.checkQueryInput(qInput, q, qid, rel, stepID, tc)
					checkInputValues(qInput)
					for code := range q.errorSet() {
						queryErrors[code] = true
					}
					if txMode == "read_only" && (len(q.Writes) > 0 || queryReturnsAffectedRows(q)) {
						c.rulef("service-transaction", "%s: step %s calls write query %s under tx_mode=read_only", rel, stepID, qid)
					}
					for name, rt := range c.queryReturnTypes[qid] {
						produced[name] = append(produced[name], rt)
					}
				}
			}
		case "service":
			so, _ := asMap(step["service"])
			targetErrors = map[string]bool{}
			if so == nil {
				c.rulef("service-steps-kind", "%s: step %s kind=service requires service", rel, stepID)
			} else {
				for field := range so {
					if field != "service" && field != "input" {
						c.rulef("service-steps-kind", "%s: step %s service does not allow field %s", rel, stepID, field)
					}
				}
				ref, _ := so["service"].(string)
				if ref == "" {
					c.rulef("service-steps-kind", "%s: step %s service requires service id", rel, stepID)
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
					sInput := map[string]any{}
					if rawIn, ok := so["input"]; ok && rawIn != nil {
						items, ok := asList2(rawIn)
						if !ok {
							c.rulef("service-steps-kind", "%s: step %s service %s input must be a list", rel, stepID, ref)
						} else {
							for _, rawItem := range items {
								item, _ := asMap(rawItem)
								name, _ := item["name"].(string)
								value, _ := item["value"].(string)
								if name == "" || value == "" {
									c.rulef("service-steps-kind", "%s: step %s service %s input item requires name and value", rel, stepID, ref)
									continue
								}
								sInput[name] = value
							}
						}
					}
					c.checkServiceInput(sInput, target, ref, rel, stepID, tc)
					checkInputValues(sInput)
					for code := range target.errors {
						targetErrors[code] = true
					}
					for name, rt := range target.returns {
						produced[name] = append(produced[name], rt)
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
			if step["rules"] == nil {
				c.rulef("service-steps-kind", "%s: step %s kind=logic requires rules (may be empty)", rel, stepID)
			}
			c.checkLogicRefs(step, rel, stepID, ruleIDs, eventIDs)
		case "events":
			evs := asList(step["events"])
			if len(evs) == 0 {
				c.rulef("service-steps-kind", "%s: step %s kind=events requires events", rel, stepID)
			}
			seen := map[string]bool{}
			for _, raw := range evs {
				ev, _ := raw.(string)
				if ev == "" || !eventIDs[ev] {
					c.rulef("service-ref", "%s: step %s unknown event %s", rel, stepID, ev)
				}
				if seen[ev] {
					c.rulef("service-structure", "%s: step %s duplicate event %s", rel, stepID, ev)
				}
				seen[ev] = true
			}
		case "when":
			if len(asList(step["when"])) == 0 {
				c.rulef("service-steps-kind", "%s: step %s kind=when requires when", rel, stepID)
			}
			c.checkLogicRefs(step, rel, stepID, ruleIDs, eventIDs)
		default:
			c.rulef("service-structure", "%s: step %s kind must be query/service/logic/events/when", rel, stepID)
		}

		lifecycles := asList(step["lifecycles"])
		seenLc := map[string]bool{}
		for _, rawL := range lifecycles {
			actionID, _ := rawL.(string)
			if seenLc[actionID] {
				c.rulef("service-structure", "%s: step %s duplicate lifecycle action %s", rel, stepID, actionID)
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
					c.rulef("service-structure", "%s: step %s duplicate output %s", rel, stepID, name)
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
		for name, rt := range stepOutput {
			produced[name] = append(produced[name], rt)
		}

		seenDeps := map[string]bool{}
		for _, rawDep := range asList(step["depends_on"]) {
			depID, _ := rawDep.(string)
			if seenDeps[depID] {
				c.rulef("service-structure", "%s: step %s duplicate depends_on %s", rel, stepID, depID)
			}
			seenDeps[depID] = true
			if idx, ok := stepIndex[depID]; !ok || idx >= i {
				c.rulef("service-structure", "%s: step %s depends_on %s must reference an earlier step", rel, stepID, depID)
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
		if kind == "logic" || kind == "when" {
			for _, rawRule := range asList(step["rules"]) {
				rm, _ := asMap(rawRule)
				rid, _ := rm["rule"].(string)
				code := c.ruleErrors[rid]
				if code == "" {
					continue
				}
				owners := c.ruleOwners[rid]
				if len(owners) == 0 {
					owners = []string{id}
				}
				declared := false
				for _, owner := range owners {
					if serviceMeta[owner].errors[code] {
						declared = true
						break
					}
				}
				if !declared {
					c.rulef("service-errors", "%s: rule %s error %s is not declared by service(s) %v", rel, rid, code, owners)
				}
			}
		}

	}

	returns := map[string]bool{}
	for _, rawReturn := range asList(doc["returns"]) {
		rm, _ := asMap(rawReturn)
		rn, _ := rm["name"].(string)
		if rn == "" || rn == "err" {
			continue
		}
		returns[rn] = true
		rtyp, _ := rm["type"].(string)
		rnullable, _ := rm["nullable"].(bool)
		candidates := produced[rn]
		if len(candidates) == 0 {
			c.rulef("service-output", "%s: return %s has no producing step", rel, rn)
			continue
		}
		matched := false
		nullableMismatch := false
		var types []string
		for _, cand := range candidates {
			types = append(types, cand.typ)
			if !sameServiceType(cand.typ, rtyp) {
				continue
			}
			candNullable := cand.nullable || strings.HasPrefix(strings.TrimSpace(cand.typ), "*")
			if candNullable != rnullable {
				nullableMismatch = true
				continue
			}
			matched = true
			break
		}
		if !matched {
			if nullableMismatch {
				c.rulef("service-output-nullable", "%s: return %s nullability (nullable=%t) does not match producing types %v", rel, rn, rnullable, types)
			} else {
				c.rulef("service-output-type", "%s: return %s type %s does not match producing types %v", rel, rn, rtyp, types)
			}
		}
	}

	// 显式 output 必须被后续 step 的 input 消费，或作为 returns 的业务字段；when 的 output 还可以被自身 when 条件引用。
	for i, raw := range steps {
		step, _ := asMap(raw)
		stepID, _ := step["id"].(string)
		kind, _ := step["kind"].(string)
		if kind != "logic" && kind != "when" {
			continue
		}
		selfRefs := map[string]bool{}
		if kind == "when" {
			for _, rawWhen := range asList(step["when"]) {
				wm, _ := asMap(rawWhen)
				if ref, _ := wm["input"].(string); ref != "" {
					selfRefs[ref] = true
				}
			}
		}
		for name := range stepOutputs[i] {
			if returns[name] || consumed[stepID+"."+name] || selfRefs[name] {
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
		c.rulef("service-steps-kind", "%s: step %s %s input must be a list", rel, stepID, where)
		return out
	}
	seen := map[string]bool{}
	for _, raw := range items {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		value, _ := item["value"].(string)
		if name == "" || value == "" {
			c.rulef("service-steps-kind", "%s: step %s %s input item requires name and value", rel, stepID, where)
			continue
		}
		if seen[name] {
			c.rulef("service-structure", "%s: step %s %s duplicate input %s", rel, stepID, where, name)
		}
		seen[name] = true
		out[name] = value
	}
	return out
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

// checkStepWhen 校验 kind=when 的条件分支：条件引用本步 input，equals/in 恰一，
// 值类型与 input 静态类型兼容；每个分支必须有指向后续 step 的 goto。
func (c *Checker) checkStepWhen(step map[string]any, input map[string]any, kind string, tc stepTypeContext, rel, stepID string, allIndex map[string]int, pos int) {
	whens := asList(step["when"])
	if step["when"] != nil && len(whens) == 0 {
		c.rulef("service-when", "%s: step %s when must not be empty", rel, stepID)
		return
	}
	if len(whens) == 0 {
		return
	}
	if kind != "when" {
		c.rulef("service-when", "%s: step %s when is only allowed on kind=when", rel, stepID)
	}
	inputTypes := map[string]string{}
	for name, raw := range input {
		value, _ := raw.(string)
		if value == "" {
			continue
		}
		if typ, err := c.inputValueType(value, tc); err == nil {
			inputTypes[name] = typ
		}
	}
	outputTypes := map[string]string{}
	for _, rawOut := range asList(step["output"]) {
		om, _ := asMap(rawOut)
		name, _ := om["name"].(string)
		typ, _ := om["type"].(string)
		if name != "" && typ != "" {
			outputTypes[name] = typ
		}
	}
	hasContinue := false
	if target, _ := step["goto"].(string); target != "" {
		if target != "$end" {
			hasContinue = true
		}
	} else if pos+1 < len(allIndex) {
		hasContinue = true
	}
	for _, rawWhen := range whens {
		wm, _ := asMap(rawWhen)
		if wm == nil {
			c.rulef("service-when", "%s: step %s when item must be an object", rel, stepID)
			continue
		}
		if target, _ := wm["goto"].(string); target != "" && target != "$end" {
			hasContinue = true
		}
		for field := range wm {
			if field != "input" && field != "equals" && field != "in" && field != "goto" {
				c.rulef("service-when", "%s: step %s when item does not allow field %s", rel, stepID, field)
			}
		}
		name, _ := wm["input"].(string)
		if name == "" {
			c.rulef("service-when", "%s: step %s when item requires input", rel, stepID)
			continue
		}
		_, inInput := input[name]
		outType, inOutput := outputTypes[name]
		if !inInput && !inOutput {
			c.rulef("service-when", "%s: step %s when input %s is not declared by step input or output", rel, stepID, name)
			continue
		}
		if target, _ := wm["goto"].(string); target == "" {
			c.rulef("service-when", "%s: step %s when input %s requires goto", rel, stepID, name)
		} else {
			c.checkGoto(rel, stepID, target, allIndex, pos)
		}
		_, hasEquals := wm["equals"]
		_, hasIn := wm["in"]
		if hasEquals == hasIn {
			c.rulef("service-when", "%s: step %s when input %s requires exactly one of equals/in", rel, stepID, name)
			continue
		}
		typ, ok := inputTypes[name]
		if !ok && inOutput {
			typ, ok = outType, true
		}
		if !ok {
			continue
		}
		if hasEquals {
			c.checkWhenValue(typ, wm["equals"], rel, stepID, name)
			continue
		}
		values := asList(wm["in"])
		if len(values) == 0 {
			c.rulef("service-when", "%s: step %s when input %s in must not be empty", rel, stepID, name)
			continue
		}
		seenValues := map[string]bool{}
		for _, value := range values {
			key := fmt.Sprint(value)
			if seenValues[key] {
				c.rulef("service-when", "%s: step %s when input %s in value %v is duplicated", rel, stepID, name, value)
			}
			seenValues[key] = true
			c.checkWhenValue(typ, value, rel, stepID, name)
		}
	}
	if !hasContinue {
		c.rulef("service-when", "%s: step %s when must have at least one branch that continues to a later step", rel, stepID)
	}
}

func (c *Checker) checkWhenValue(typ string, value any, rel, stepID, name string) {
	shape, ok := parseTypeShape(typ)
	if !ok {
		return
	}
	if shape.array {
		c.rulef("service-when", "%s: step %s when input %s is a list and cannot be compared", rel, stepID, name)
		return
	}
	if strings.HasPrefix(shape.base, "data.enums.") {
		s, ok := value.(string)
		if !ok {
			c.rulef("service-when", "%s: step %s when input %s expects enum name, got %T", rel, stepID, name, value)
			return
		}
		if values, ok := c.enumValues[shape.base]; ok && !values[s] {
			c.rulef("service-when", "%s: step %s when input %s has unknown enum value %s", rel, stepID, name, s)
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
			c.rulef("service-when", "%s: step %s when input %s expects boolean, got %T", rel, stepID, name, value)
		}
	case "integer":
		if _, ok := value.(int); !ok {
			c.rulef("service-when", "%s: step %s when input %s expects integer, got %T", rel, stepID, name, value)
		}
	case "string":
		if _, ok := value.(string); !ok {
			c.rulef("service-when", "%s: step %s when input %s expects string, got %T", rel, stepID, name, value)
		}
	}
}

func (c *Checker) checkLogicRefs(step map[string]any, rel, stepID string, ruleIDs, eventIDs map[string]bool) {
	seen := map[string]bool{}
	for _, rawRule := range asList(step["rules"]) {
		rm, _ := asMap(rawRule)
		if rm == nil {
			c.rulef("service-structure", "%s: step %s rules item must be an object", rel, stepID)
			continue
		}
		rid, _ := rm["rule"].(string)
		if rid == "" {
			c.rulef("service-structure", "%s: step %s rules item requires rule", rel, stepID)
			continue
		}
		if seen[rid] {
			c.rulef("service-structure", "%s: step %s duplicate rule %s", rel, stepID, rid)
		}
		seen[rid] = true
		if !ruleIDs[rid] {
			c.rulef("service-ref", "%s: step %s unknown rule %s", rel, stepID, rid)
		}
	}
}
