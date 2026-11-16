// capabilities.go 校验 methods / flows / protocols 三层：
//   - ID 与 targets 派生一致、全局唯一
//   - method 的 interface/transaction/errors
//   - flow 的 implementation 互斥、同步/异步字段、steps 引用
//   - protocol 的 x-flow 绑定、异步 flow 不得绑定、同步 flow 必须有入边
package check

import (
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
	flowIDs := map[string]bool{}
	flowAsync := map[string]bool{}
	flowInbound := map[string]bool{}

	typeNames := map[string]bool{}
	for _, file := range glob(c.root, filepath.Join(methodSchema.Targets.Dir, methodSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		types, _ := asMap(doc["types"])
		for name := range types {
			typeNames[name] = true
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
		c.checkMethodDoc(doc, rel, id, methodIDs, methodSchema, typeNames)
	}

	for _, file := range glob(c.root, filepath.Join(flowSchema.Targets.Dir, flowSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
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
			c.checkFlowItem(item, rel+":"+id, id, module, methodIDs)
		}
	}

	for _, file := range glob(c.root, filepath.Join(httpSchema.Targets.Dir, httpSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
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
				if op["x-service"] != nil {
					c.rulef("http-binding", "%s %s %s: x-service removed; use x-flow", rel, method, path)
				}
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

func (c *Checker) checkMethodDoc(doc map[string]any, rel, fileID string, methodIDs map[string]bool, schema *Schema, typeNames map[string]bool) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	if types, ok := asMap(doc["types"]); ok {
		c.checkTypeRefs(types, typeNames, rel)
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
		methodIDs[fileID+"."+contractspec.SnakeCase(mname)] = true
		for _, rawParam := range asList(m["params"]) {
			pm, _ := asMap(rawParam)
			c.checkTypeName(pm["type"], typeNames, rel+":"+mname)
		}
		for _, rawReturn := range asList(m["returns"]) {
			rm, _ := asMap(rawReturn)
			c.checkTypeName(rm["type"], typeNames, rel+":"+mname)
		}
		for _, rawErr := range asList(m["errors"]) {
			code, _ := rawErr.(string)
			if !strings.HasPrefix(code, "platform.errors.") {
				c.rulef("cap-ref", "%s.%s: error %q must be platform.errors.*", rel, mname, code)
				continue
			}
			if !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
				c.rulef("cap-ref", "%s.%s: unknown error %s", rel, mname, code)
			}
		}
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

func (c *Checker) checkFlowItem(doc map[string]any, rel, id, module string, methodIDs map[string]bool) {
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
	case "flow":
		if methodRef != "" {
			c.rulef("flow-structure", "%s: flow implementation must not declare method", rel)
		}
		if len(steps) == 0 {
			c.rulef("flow-structure", "%s: flow implementation requires steps", rel)
		}
		for _, raw := range steps {
			step, _ := asMap(raw)
			use, _ := step["use"].(string)
			if !c.validFlowUse(use, methodIDs) {
				c.rulef("cap-ref", "%s: step use %q is not a method or permission; data.queries is forbidden", rel, use)
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
	if typ == "" || isBuiltinType(typ) {
		return
	}
	if !typeNames[typ] {
		c.rulef("cap-ref", "%s: unknown type %s", where, typ)
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
