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
			"id": true, "kind": true, "summary": true, "errors": true, "lifecycles": true, "goto": true,
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
					if field != "ref" && field != "input" {
						c.rulef("service-steps-kind", "%s: step %s query does not allow field %s", rel, stepID, field)
					}
				}
				qid, _ := qo["ref"].(string)
				if qid == "" {
					c.rulef("service-steps-kind", "%s: step %s query requires ref", rel, stepID)
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
				}
			}
		case "service":
			so, _ := asMap(step["service"])
			targetErrors = map[string]bool{}
			if so == nil {
				c.rulef("service-steps-kind", "%s: step %s kind=service requires service", rel, stepID)
			} else {
				for field := range so {
					if field != "ref" && field != "input" {
						c.rulef("service-steps-kind", "%s: step %s service does not allow field %s", rel, stepID, field)
					}
				}
				ref, _ := so["ref"].(string)
				if ref == "" {
					c.rulef("service-steps-kind", "%s: step %s service requires ref", rel, stepID)
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
			c.checkStepEvents(step, rel, stepID, eventIDs, tc, checkInputValues)
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

	// 业务返回值必须有显式来源；来源的静态类型与 nullable 必须与 returns 声明一致。
	for _, rawReturn := range asList(doc["returns"]) {
		rm, _ := asMap(rawReturn)
		rn, _ := rm["name"].(string)
		if rn == "" {
			continue
		}
		if rn == "err" {
			if _, ok := rm["value"]; ok {
				c.rulef("service-structure", "%s: return err must not declare value", rel)
			}
			continue
		}
		value, _ := rm["value"].(string)
		if strings.TrimSpace(value) == "" {
			c.rulef("service-output", "%s: return %s requires value (params.<path> or step.<id>.<path>)", rel, rn)
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
		}
	}

	// 显式 output 必须被后续 step 的 input 消费、被自身 when 条件引用，或被 returns.value 引用。
	for i, raw := range steps {
		step, _ := asMap(raw)
		stepID, _ := step["id"].(string)
		kind, _ := step["kind"].(string)
		if kind != "logic" && kind != "when" {
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

// checkStepEvents 校验 kind=events 的事件项与 payload 映射。
func (c *Checker) checkStepEvents(step map[string]any, rel, stepID string, eventIDs map[string]bool, tc stepTypeContext, checkInputValues func(map[string]any)) {
	items, ok := asList2(step["events"])
	if !ok || len(items) == 0 {
		c.rulef("service-steps-kind", "%s: step %s kind=events requires events", rel, stepID)
		return
	}
	seen := map[string]bool{}
	for _, raw := range items {
		item, _ := asMap(raw)
		if item == nil {
			c.rulef("service-structure", "%s: step %s events item must be an object", rel, stepID)
			continue
		}
		for field := range item {
			if field != "ref" && field != "input" {
				c.rulef("service-steps-kind", "%s: step %s events item does not allow field %s", rel, stepID, field)
			}
		}
		ref, _ := item["ref"].(string)
		if ref == "" {
			c.rulef("service-structure", "%s: step %s events item requires ref", rel, stepID)
			continue
		}
		if seen[ref] {
			c.rulef("service-structure", "%s: step %s duplicate event %s", rel, stepID, ref)
		}
		seen[ref] = true
		if !eventIDs[ref] {
			c.rulef("service-ref", "%s: step %s unknown event %s", rel, stepID, ref)
			continue
		}
		input := c.parseInputMap(item["input"], rel, stepID, "event "+ref)
		c.checkEventInput(input, ref, rel, stepID, tc)
		checkInputValues(input)
	}
}

// checkEventInput 校验事件 payload 映射：字段必须存在、必填字段必须出现、类型必须兼容。
func (c *Checker) checkEventInput(input map[string]any, eventID, rel, stepID string, tc stepTypeContext) {
	fields := c.eventPayloads[eventID]
	for name, raw := range input {
		field, ok := fields[name]
		if !ok {
			c.rulef("service-params", "%s: step %s event %s has no payload field %s", rel, stepID, eventID, name)
			continue
		}
		value, _ := raw.(string)
		src, err := c.inputValueType(value, tc)
		if err != nil {
			continue
		}
		if field.typ != "" && !c.serviceTypesCompatible(src, field.typ) {
			c.rulef("service-params", "%s: step %s event %s input %s type %s is not compatible with %s", rel, stepID, eventID, name, src, field.typ)
		}
	}
	for name, field := range fields {
		if !field.required {
			continue
		}
		if _, ok := input[name]; !ok {
			c.rulef("service-params", "%s: step %s event %s missing required payload field %s", rel, stepID, eventID, name)
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

// checkStepWhen 校验 kind=when 的条件分支：expr 只能引用本步 input，
// 字面量类型必须与字段静态类型兼容；每个分支必须有指向后续 step 的 goto。
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
	for _, rawWhen := range whens {
		wm, _ := asMap(rawWhen)
		if wm == nil {
			c.rulef("service-when", "%s: step %s when item must be an object", rel, stepID)
			continue
		}
		for field := range wm {
			if field != "expr" && field != "goto" {
				c.rulef("service-when", "%s: step %s when item does not allow field %s", rel, stepID, field)
			}
		}
		expr, _ := wm["expr"].(string)
		if strings.TrimSpace(expr) == "" {
			c.rulef("service-when", "%s: step %s when item requires expr", rel, stepID)
			continue
		}
		if target, _ := wm["goto"].(string); target == "" {
			c.rulef("service-when", "%s: step %s when expr %q requires goto", rel, stepID, expr)
		} else {
			c.checkGoto(rel, stepID, target, allIndex, pos)
		}
		node, err := parseWhen(expr)
		if err != nil {
			c.rulef("service-when", "%s: step %s when expr %q: %v", rel, stepID, expr, err)
			continue
		}
		for _, field := range whenFields(node, "") {
			if !strings.HasPrefix(field, "input.") {
				c.rulef("service-when", "%s: step %s when expr %q may only reference input.<name>, got %s", rel, stepID, expr, field)
				continue
			}
			if _, err := c.whenInputFieldType(strings.TrimPrefix(field, "input."), input, tc); err != nil {
				c.rulef("service-when", "%s: step %s when expr %q: %v", rel, stepID, expr, err)
			}
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
		base := strings.TrimPrefix(strings.TrimSpace(typ), "*")
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
		c.rulef("service-when", "%s: step %s when field %s is a list and cannot be compared", rel, stepID, name)
		return
	}
	if strings.HasPrefix(shape.base, "data.enums.") {
		s, ok := value.(string)
		if !ok {
			c.rulef("service-when", "%s: step %s when field %s expects enum name, got %T", rel, stepID, name, value)
			return
		}
		if values, ok := c.enumValues[shape.base]; ok && !values[s] {
			c.rulef("service-when", "%s: step %s when field %s has unknown enum value %s", rel, stepID, name, s)
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
			c.rulef("service-when", "%s: step %s when field %s expects boolean, got %T", rel, stepID, name, value)
		}
	case "integer":
		if _, ok := value.(int); !ok {
			c.rulef("service-when", "%s: step %s when field %s expects integer, got %T", rel, stepID, name, value)
		}
	case "string":
		if _, ok := value.(string); !ok {
			c.rulef("service-when", "%s: step %s when field %s expects string, got %T", rel, stepID, name, value)
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
