// capabilities.go 校验 methods / flows / protocols 三层：
//   - ID 与 targets 派生一致、全局唯一
//   - method 的 interface/transaction/errors
//   - flow 的 implementation 互斥、同步/异步字段、steps 引用
//   - protocol 的 x-flow 绑定、异步 flow 不得绑定、同步 flow 必须有入边
package check

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func (c *Checker) checkCapabilities() {
	methodSchema, err := c.loadSchemaByType("cap.methods")
	if err != nil {
		c.rulef("schema.load", "load cap.methods schema: %v", err)
		return
	}
	flowSchema, err := c.loadSchemaByType("cap.flows")
	if err != nil {
		c.rulef("schema.load", "load cap.flows schema: %v", err)
		return
	}
	httpSchema, err := c.loadSchemaByType("cap.http")
	if err != nil {
		c.rulef("schema.load", "load cap.http schema: %v", err)
		return
	}
	c.checkRules(methodSchema)
	c.checkRules(flowSchema)
	c.checkRules(httpSchema)

	methodIDs := map[string]bool{}
	methodErrors := map[string]map[string]bool{}
	methodTypeIDs := map[string]bool{}
	flowIDs := map[string]bool{}
	flowAsync := map[string]bool{}
	flowInbound := map[string]bool{}
	httpContractIDs := map[string]bool{}

	typeNames := map[string]bool{}
	c.methodReturns = map[string]map[string]string{}
	c.methodTypeDefs = map[string]map[string]any{}
	for _, file := range glob(c.root, filepath.Join(methodSchema.Targets.Dir, methodSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		types, _ := asMap(doc["types"])
		id, _ := doc["id"].(string)
		for name, raw := range types {
			typeNames[name] = true
			if def, ok := asMap(raw); ok {
				c.methodTypeDefs[name] = def
			}
			if id != "" {
				methodTypeIDs[id+".types."+name] = true
			}
		}
	}
	for _, file := range glob(c.root, filepath.Join(methodSchema.Targets.Dir, methodSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		id, _ := doc["id"].(string)
		want := methodSchema.Targets.FileID(filepath.Base(file))
		if id != want {
			c.rulef("cap-id", "%s: id %q must be %q", rel, id, want)
		}
		if methodIDs[id] {
			c.rulef("cap-id", "%s: duplicate method id %s", rel, id)
		}
		methodIDs[id] = true
		c.checkMethodDoc(doc, rel, id, methodIDs, methodErrors, methodSchema, typeNames)
	}

	for _, file := range glob(c.root, filepath.Join(flowSchema.Targets.Dir, flowSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		if s, _ := doc["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
		}
		fileID, _ := doc["id"].(string)
		want := flowSchema.Targets.FileID(filepath.Base(file))
		if fileID != want {
			c.rulef("cap-id", "%s: id %q must be %q", rel, fileID, want)
		}
		module, _ := doc["module"].(string)
		for _, raw := range asList(doc["flows"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			id := fileID + "." + contractspec.SnakeCase(name)
			if flowIDs[id] {
				c.rulef("cap-id", "%s: duplicate flow id %s", rel, id)
			}
			flowIDs[id] = true
			if item["trigger"] != nil {
				flowAsync[id] = true
			}
			c.checkFlowItem(item, rel+":"+id, id, module, methodIDs, methodErrors, typeNames)
		}
	}

	httpFiles := glob(c.root, filepath.Join(httpSchema.Targets.Dir, httpSchema.Targets.Glob))
	httpSchemas := map[string]bool{}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		components, _ := asMap(doc["components"])
		schemas, _ := asMap(components["schemas"])
		for name, raw := range schemas {
			httpSchemas[name] = true
			s, _ := asMap(raw)
			if ref, _ := s["x-method-type"].(string); ref != "" {
				if !match(`^cap\.methods\.[a-z][a-z0-9_]*\.types\.[A-Z][A-Za-z0-9]*$`, ref) {
					c.rulef("http-components", "%s: components.schemas.%s.x-method-type must match cap.methods.<file>.types.<Type>", rel, name)
				} else if !methodTypeIDs[ref] {
					c.rulef("http-components", "%s: components.schemas.%s.x-method-type references unknown %s", rel, name, ref)
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
			if typ, _ := s["type"].(string); typ == "object" {
				if v, ok := s["additionalProperties"]; !ok || v != false {
					c.rulef("http-components", "%s: components.schemas.%s must set additionalProperties: false", rel, name)
				}
			}
		}
	}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		if s, _ := doc["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
		}
		defaultErrors := map[string]bool{}
		for _, raw := range asList(doc["default_error_responses"]) {
			n, ok := toInt(raw)
			if !ok || n < 100 || n > 599 {
				c.rulef("http-binding", "%s: default_error_responses must contain HTTP status codes 100-599, got %v", rel, raw)
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
				opID, _ := op["operationId"].(string)
				if opID == "" {
					c.rulef("http-binding", "%s %s %s: operationId required", rel, method, path)
				}
				contractID, _ := op["x-contract-id"].(string)
				switch {
				case contractID == "":
					c.rulef("http-binding", "%s %s %s: x-contract-id required", rel, method, path)
				case !match(`^cap\.http\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`, contractID):
					c.rulef("http-binding", "%s %s %s: x-contract-id %q must match cap.http.<file>.<operation>", rel, method, path, contractID)
				case httpContractIDs[contractID]:
					c.rulef("http-binding", "%s %s %s: duplicate x-contract-id %s", rel, method, path, contractID)
				}
				if contractID != "" {
					httpContractIDs[contractID] = true
				}
				if raw, ok := op["x-timeout-ms"]; ok {
					if n, ok := toInt(raw); !ok || n <= 0 {
						c.rulef("http-binding", "%s %s %s: x-timeout-ms must be a positive integer", rel, method, path)
					}
				}
				responses, _ := asMap(op["responses"])
				for status := range responses {
					if defaultErrors[status] {
						c.rulef("http-binding", "%s %s %s: status %s is injected by default_error_responses; do not repeat it", rel, method, path, status)
					}
				}
				if op["x-service"] != nil {
					c.rulef("http-binding", "%s %s %s: x-service removed; use x-flow", rel, method, path)
				}
				collectRefs(op, func(ref string) {
					if strings.HasPrefix(ref, "#/components/schemas/") {
						name := strings.TrimPrefix(ref, "#/components/schemas/")
						if !httpSchemas[name] {
							c.rulef("http-components", "%s %s %s: unknown schema $ref %s", rel, method, path, ref)
						}
					}
				})
				flowRef, _ := op["x-flow"].(string)
				if flowRef == "" {
					c.rulef("http-binding", "%s %s %s: x-flow required", rel, method, path)
					continue
				}
				if !flowIDs[flowRef] {
					c.rulef("http-binding", "%s %s %s: unknown flow %s", rel, method, path, flowRef)
					continue
				}
				if flowAsync[flowRef] {
					c.rulef("http-binding", "%s %s %s: async flow %s cannot be bound to http", rel, method, path, flowRef)
					continue
				}
				flowInbound[flowRef] = true
			}
		}
	}

	for id := range flowIDs {
		if !flowAsync[id] && !flowInbound[id] {
			c.rulef("http-binding", "sync flow %s has no protocol inbound", id)
		}
	}
}

func (c *Checker) checkMethodDoc(doc map[string]any, rel, fileID string, methodIDs map[string]bool, methodErrors map[string]map[string]bool, schema *Schema, typeNames map[string]bool) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	if s, _ := doc["summary"].(string); strings.TrimSpace(s) == "" {
		c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
	}
	if types, ok := asMap(doc["types"]); ok {
		c.checkTypeRefs(types, typeNames, rel)
		c.checkMethodTypes(types, rel)
	}
	iface, _ := asMap(doc["interface"])
	if iface == nil {
		return
	}
	seen := map[string]bool{}
	for _, raw := range asList(iface["methods"]) {
		m, _ := asMap(raw)
		mname, _ := m["name"].(string)
		if seen[mname] {
			c.rulef("method-structure", "%s: duplicate method %s", rel, mname)
		}
		seen[mname] = true
		if s, _ := m["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s.%s: summary must not be empty", rel, mname)
		}
		methodID := fileID + "." + contractspec.SnakeCase(mname)
		methodIDs[methodID] = true
		errs := map[string]bool{}
		for _, rawParam := range asList(m["params"]) {
			pm, _ := asMap(rawParam)
			c.checkTypeName(pm["type"], typeNames, rel+":"+mname)
		}
		returns := map[string]string{}
		for _, rawReturn := range asList(m["returns"]) {
			rm, _ := asMap(rawReturn)
			name, _ := rm["name"].(string)
			typ, _ := rm["type"].(string)
			c.checkTypeName(rm["type"], typeNames, rel+":"+mname)
			if name != "" && typ != "" && name != "err" {
				returns[name] = typ
			}
		}
		c.methodReturns[methodID] = returns
		for _, rawErr := range asList(m["errors"]) {
			code, _ := rawErr.(string)
			if !strings.HasPrefix(code, "platform.errors.") {
				c.rulef("cap-ref", "%s.%s: error %q must be platform.errors.*", rel, mname, code)
				continue
			}
			if !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
				c.rulef("cap-ref", "%s.%s: unknown error %s", rel, mname, code)
			}
			errs[code] = true
		}
		methodErrors[methodID] = errs
		for _, rawQuery := range asList(m["queries"]) {
			qid, _ := rawQuery.(string)
			if qid == "" {
				c.rulef("cap-ref", "%s.%s: queries must be a query ID", rel, mname)
				continue
			}
			if _, ok := c.queryIDs[qid]; !ok {
				c.rulef("cap-ref", "%s.%s: unknown query %s", rel, mname, qid)
			}
		}
	}
}

func (c *Checker) checkFlowItem(doc map[string]any, rel, id, module string, methodIDs map[string]bool, methodErrors map[string]map[string]bool, typeNames map[string]bool) {
	if s, _ := doc["summary"].(string); strings.TrimSpace(s) == "" {
		c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
	}
	impl, _ := doc["implementation"].(string)
	methodRef, _ := doc["method"].(string)
	steps := asList(doc["steps"])

	switch impl {
	case "direct":
		if methodRef == "" {
			c.rulef("flow-structure", "%s: direct flow requires method", rel)
		}
		if len(steps) > 0 {
			c.rulef("flow-structure", "%s: direct flow must not declare steps", rel)
		}
		if methodRef != "" && !methodIDs[methodRef] {
			c.rulef("cap-ref", "%s: unknown method %s", rel, methodRef)
		}
		if methodRef != "" && methodIDs[methodRef] {
			allowedErrors := map[string]bool{}
			for code := range methodErrors[methodRef] {
				allowedErrors[code] = true
			}
			req := flowLevelErrors(doc, false)
			for code := range req {
				allowedErrors[code] = true
			}
			flowErrors := map[string]bool{}
			for _, raw := range asList(doc["errors"]) {
				code, _ := raw.(string)
				flowErrors[code] = true
				if !allowedErrors[code] {
					c.rulef("flow-errors", "%s: error %s is not declared by %s or a flow-level feature", rel, code, methodRef)
				}
			}
			for code := range req {
				if !flowErrors[code] {
					c.rulef("flow-errors", "%s: missing flow-level error %s", rel, code)
				}
			}
		}
	case "flow":
		if methodRef != "" {
			c.rulef("flow-structure", "%s: flow implementation must not declare method", rel)
		}
		if len(steps) == 0 {
			c.rulef("flow-structure", "%s: flow implementation requires steps", rel)
		}
		stepOutputs := make([]map[string]string, len(steps))
		allStepIndex := map[string]int{}
		for i, raw := range steps {
			step, _ := asMap(raw)
			id, _ := step["id"].(string)
			if id != "" {
				if _, exists := allStepIndex[id]; !exists {
					allStepIndex[id] = i
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
				c.rulef("flow-structure", "%s: duplicate step id %s", rel, stepID)
			} else {
				stepIndex[stepID] = i
			}
			use, _ := step["use"].(string)
			if !c.validFlowUse(use, methodIDs) {
				if strings.HasPrefix(use, "data.queries.") {
					c.rulef("flow-no-query", "%s: step use %q is not a method or permission; data.queries is forbidden", rel, use)
				} else {
					c.rulef("cap-ref", "%s: step use %q is not a method or permission; data.queries is forbidden", rel, use)
				}
			}
			stepOutput := map[string]string{}
			if step["output"] != nil {
				if use != "" {
					c.rulef("flow-structure", "%s: step %s output is only allowed for internal steps", rel, stepID)
				} else {
					seenOut := map[string]bool{}
					for _, rawOut := range asList(step["output"]) {
						om, _ := asMap(rawOut)
						name, _ := om["name"].(string)
						typ, _ := om["type"].(string)
						if seenOut[name] {
							c.rulef("flow-structure", "%s: step %s duplicate output %s", rel, stepID, name)
						}
						seenOut[name] = true
						c.checkTypeName(om["type"], typeNames, rel+":"+stepID)
						if name != "" && typ != "" {
							stepOutput[name] = typ
						}
					}
				}
			}
			stepOutputs[i] = stepOutput
			reachable := true
			for _, rawDep := range asList(step["depends_on"]) {
				depID, _ := rawDep.(string)
				idx, ok := stepIndex[depID]
				if !ok {
					c.rulef("flow-structure", "%s: step %s depends_on %s must reference an earlier step", rel, stepID, depID)
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
					c.rulef("flow-when", "%s: step %s when must not be empty", rel, stepID)
				} else if node, err := parseWhen(s); err != nil {
					c.rulef("flow-when", "%s: step %s when %q: %v", rel, stepID, s, err)
				} else {
					if evalWhenNode(node) == whenFalse {
						reachable = false
					}
					for _, ref := range whenStepRefs(node) {
						if _, ok := stepIndex[ref]; !ok {
							c.rulef("flow-when", "%s: step %s when references step %s that is not an earlier step", rel, stepID, ref)
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
						if err := c.checkStepOutputPath(target, stepOutputs[idx], path); err != nil {
							c.rulef("flow-when", "%s: step %s when references %s: %v", rel, stepID, field, err)
						}
					}
				}
			}
			stepErrors := map[string]bool{}
			if strings.HasPrefix(use, "cap.methods.") && methodErrors[use] != nil {
				if step["errors"] != nil {
					for _, rawErr := range asList(step["errors"]) {
						code, _ := rawErr.(string)
						if !methodErrors[use][code] {
							c.rulef("flow-errors", "%s: step %s error %s is not declared by %s", rel, stepID, code, use)
						}
						stepErrors[code] = true
					}
				} else {
					for code := range methodErrors[use] {
						stepErrors[code] = true
					}
				}
			} else {
				for _, rawErr := range asList(step["errors"]) {
					code, _ := rawErr.(string)
					if !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
						c.rulef("cap-ref", "%s: step %s unknown error %s", rel, stepID, code)
					}
					stepErrors[code] = true
				}
			}
			handled := map[string]bool{}
			seenOnError := map[string]bool{}
			for _, rawErr := range asList(step["on_error"]) {
				entry, _ := asMap(rawErr)
				code, _ := entry["error"].(string)
				action, _ := entry["action"].(string)
				target, _ := entry["target"].(string)
				if !stepErrors[code] {
					c.rulef("flow-control", "%s: step %s on_error %s is not declared by the step error set", rel, stepID, code)
				}
				if seenOnError[code] {
					c.rulef("flow-control", "%s: step %s duplicate on_error for %s", rel, stepID, code)
				}
				seenOnError[code] = true
				switch action {
				case "abort":
					if target != "" {
						c.rulef("flow-control", "%s: step %s on_error %s target is only allowed for goto", rel, stepID, code)
					}
				case "continue":
					if target != "" {
						c.rulef("flow-control", "%s: step %s on_error %s target is only allowed for goto", rel, stepID, code)
					}
					handled[code] = true
				case "goto":
					if target == "" {
						c.rulef("flow-control", "%s: step %s on_error %s goto requires target", rel, stepID, code)
					} else if targetIdx, ok := allStepIndex[target]; !ok {
						c.rulef("flow-control", "%s: step %s on_error %s goto target %s not found", rel, stepID, code, target)
					} else if targetIdx <= i {
						hasBackwardGoto = true
					}
					handled[code] = true
				default:
					c.rulef("flow-control", "%s: step %s on_error %s action must be abort, continue or goto", rel, stepID, code)
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
				c.rulef("flow-control", "%s: max_iterations must be >= 1", rel)
			}
		}
		if hasBackwardGoto && doc["max_iterations"] == nil {
			c.rulef("flow-control", "%s: backward/self goto requires flow.max_iterations", rel)
		}
		req := flowLevelErrors(doc, hasBackwardGoto)
		for code := range req {
			requiredErrors[code] = true
		}
		allowedErrors := map[string]bool{}
		for code := range requiredErrors {
			allowedErrors[code] = true
		}
		flowErrors := map[string]bool{}
		for _, raw := range asList(doc["errors"]) {
			code, _ := raw.(string)
			flowErrors[code] = true
			if !allowedErrors[code] {
				c.rulef("flow-errors", "%s: error %s is not produced by a reachable, unhandled step", rel, code)
			}
		}
		for code := range requiredErrors {
			if !flowErrors[code] {
				if req[code] {
					c.rulef("flow-errors", "%s: missing flow-level error %s", rel, code)
				} else {
					c.rulef("flow-errors", "%s: missing error %s produced by a reachable, unhandled step", rel, code)
				}
			}
		}
	default:
		c.rulef("flow-structure", "%s: implementation must be direct or flow", rel)
	}

	if doc["trigger"] == nil {
		if doc["transaction"] == nil {
			c.rulef("flow-structure", "%s: sync flow requires transaction", rel)
		}
		if doc["permissions"] == nil {
			c.rulef("flow-structure", "%s: sync flow requires permissions", rel)
		}
		if doc["retry"] != nil || doc["concurrency"] != nil {
			c.rulef("flow-structure", "%s: sync flow must not declare retry/concurrency", rel)
		}
	} else {
		for _, field := range []string{"input", "output", "idempotency", "retry", "concurrency"} {
			if doc[field] == nil {
				c.rulef("flow-structure", "%s: async flow requires %s", rel, field)
			}
		}
		if doc["transaction"] != nil {
			c.rulef("flow-structure", "%s: async flow must not declare transaction", rel)
		}
		trigger, _ := asMap(doc["trigger"])
		switch typ, _ := trigger["type"].(string); typ {
		case "cron":
			if s, _ := trigger["schedule"].(string); strings.TrimSpace(s) == "" {
				c.rulef("flow-structure", "%s: trigger.schedule required for cron", rel)
			}
		case "event":
			if s, _ := trigger["event"].(string); strings.TrimSpace(s) == "" {
				c.rulef("flow-structure", "%s: trigger.event required for event", rel)
			}
		}
	}

	for _, raw := range asList(doc["errors"]) {
		code, _ := raw.(string)
		if !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
			c.rulef("cap-ref", "%s: unknown error %s", rel, code)
		}
	}
	for _, raw := range asList(doc["permissions"]) {
		pid, _ := raw.(string)
		if !c.permissionExists(pid) {
			c.rulef("cap-ref", "%s: unknown permission %s", rel, pid)
		}
	}
}

// checkStepOutputPath 校验 step.<id>.<path> 的字段链路：
// 方法步骤从 method returns 取根字段；内部步骤从 step.output 取根字段。
func (c *Checker) checkStepOutputPath(step map[string]any, output map[string]string, path string) error {
	segs, err := parseWhenPath(path)
	if err != nil {
		return err
	}
	root := segs[0]
	use, _ := step["use"].(string)
	var typ string
	switch {
	case strings.HasPrefix(use, "cap.methods."):
		typ = c.methodReturns[use][root.name]
		if typ == "" {
			return fmt.Errorf("unknown output %s", root.name)
		}
	case use == "":
		typ = output[root.name]
		if typ == "" {
			return fmt.Errorf("unknown output %s", root.name)
		}
	default:
		return fmt.Errorf("permission step has no output")
	}
	if root.hasIndex {
		typ, err = indexWhenType(typ)
		if err != nil {
			return fmt.Errorf("output %s: %v", root.name, err)
		}
	}
	for _, seg := range segs[1:] {
		typ = strings.TrimPrefix(typ, "*")
		if strings.HasPrefix(typ, "List<") && strings.HasSuffix(typ, ">") {
			return fmt.Errorf("list output requires index before %s", seg.name)
		}
		next, ok := c.resolveTypeField(typ, seg.name)
		if !ok {
			return fmt.Errorf("unknown field %s of %s", seg.name, typ)
		}
		typ = next
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

func (c *Checker) resolveTypeField(typ, name string) (string, bool) {
	return c.resolveTypeFieldDepth(typ, name, map[string]bool{})
}

func (c *Checker) resolveTypeFieldDepth(typ, name string, seen map[string]bool) (string, bool) {
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
	return c.resolveSchemaField(def, name, seen)
}

func (c *Checker) resolveSchemaField(schema map[string]any, name string, seen map[string]bool) (string, bool) {
	if props, ok := asMap(schema["properties"]); ok {
		if prop, ok := asMap(props[name]); ok {
			return schemaTypeName(prop)
		}
	}
	if branches, ok := asList2(schema["allOf"]); ok {
		var got string
		for _, raw := range branches {
			branch, _ := asMap(raw)
			typ, ok := c.resolveSchemaField(branch, name, seen)
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
			var got string
			for _, raw := range branches {
				branch, _ := asMap(raw)
				typ, ok := c.resolveSchemaField(branch, name, seen)
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
			return c.resolveTypeFieldDepth(typ, name, seen)
		}
	}
	return "", false
}

func schemaTypeName(schema map[string]any) (string, bool) {
	if typ, _ := schema["type"].(string); typ != "" {
		return typ, true
	}
	if ref, _ := schema["$ref"].(string); ref != "" {
		return resolveTypeRef(ref)
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		branches, ok := asList2(schema[key])
		if !ok {
			continue
		}
		var got string
		for _, raw := range branches {
			branch, _ := asMap(raw)
			typ, ok := schemaTypeName(branch)
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
	return "", false
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

// gotoCycle 在顺序边 i→i+1 与 goto 边上做 DFS，返回一个环（节点索引）或 nil。
// flowLevelErrors 返回 flow 自身必须暴露的错误：
// permissions 拒绝→COMMON_FORBIDDEN；rate_limit→COMMON_RATE_LIMITED；
// audit 失败→COMMON_AUDIT_FAILED；idempotency 冲突→COMMON_IDEMPOTENCY_CONFLICT；
// 存在后向/自环 goto→COMMON_LOOP_EXHAUSTED。
func flowLevelErrors(doc map[string]any, hasBackwardGoto bool) map[string]bool {
	required := map[string]bool{}
	if len(asList(doc["permissions"])) > 0 {
		required["platform.errors.COMMON_FORBIDDEN"] = true
	}
	if doc["rate_limit"] != nil {
		required["platform.errors.COMMON_RATE_LIMITED"] = true
	}
	if audit, _ := doc["audit"].(bool); audit {
		required["platform.errors.COMMON_AUDIT_FAILED"] = true
	}
	if doc["idempotency"] != nil {
		required["platform.errors.COMMON_IDEMPOTENCY_CONFLICT"] = true
	}
	if hasBackwardGoto {
		required["platform.errors.COMMON_LOOP_EXHAUSTED"] = true
	}
	return required
}

func (c *Checker) validFlowUse(use string, methodIDs map[string]bool) bool {
	if use == "" {
		return true // 无 use 的步骤是 flow 内部逻辑
	}
	switch {
	case strings.HasPrefix(use, "cap.methods."):
		return methodIDs[use]
	case strings.HasPrefix(use, "platform.security.permissions."):
		return c.permissionExists(use)
	default:
		return false
	}
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
			c.rulef("cap-ref", "%s: unknown enum %s", where, base)
		}
		return
	}
	if strings.HasPrefix(base, "data.entities.") {
		if _, ok := c.entities[base]; !ok {
			c.rulef("cap-ref", "%s: unknown entity %s", where, base)
		}
		return
	}
	if strings.HasPrefix(base, "data.tables.") {
		c.rulef("cap-ref", "%s: data.tables.* is not a method type; use data.entities.*", where)
		return
	}
	if isBuiltinType(base) {
		return
	}
	if !typeNames[base] {
		c.rulef("cap-ref", "%s: unknown type %s", where, base)
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

// checkMethodTypes 校验本地类型：object 必须闭合，required 必须出现在 properties。
func (c *Checker) checkMethodTypes(types map[string]any, rel string) {
	for name, raw := range types {
		m, _ := asMap(raw)
		if typ, _ := m["type"].(string); typ != "object" {
			continue
		}
		if v, ok := m["additionalProperties"]; !ok || v != false {
			c.rulef("method-structure", "%s: types.%s must set additionalProperties: false", rel, name)
		}
		props, _ := asMap(m["properties"])
		for _, rawReq := range asList(m["required"]) {
			field, _ := rawReq.(string)
			if props[field] == nil {
				c.rulef("method-structure", "%s: types.%s required field %q not in properties", rel, name, field)
			}
		}
	}
}
