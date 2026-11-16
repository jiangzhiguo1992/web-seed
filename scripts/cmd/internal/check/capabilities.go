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
		c.checkMethodDoc(doc, rel, id, methodIDs)
	}

	for _, file := range glob(c.root, filepath.Join(flowSchema.Targets.Dir, flowSchema.Targets.Glob)) {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		id, _ := doc["id"].(string)
		want := flowSchema.Targets.FileID(filepath.Base(file))
		if id != want {
			c.rulef("cap-id", "%s: id %q must be %q", rel, id, want)
		}
		if flowIDs[id] {
			c.rulef("cap-id", "%s: duplicate flow id %s", rel, id)
		}
		flowIDs[id] = true
		if doc["trigger"] != nil {
			flowAsync[id] = true
		}
		c.checkFlowDoc(doc, rel, methodIDs)
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

func (c *Checker) checkMethodDoc(doc map[string]any, rel, fileID string, methodIDs map[string]bool) {
	iface, _ := asMap(doc["interface"])
	if iface == nil {
		c.rulef("method-structure", "%s: interface is required", rel)
		return
	}
	name, _ := iface["name"].(string)
	if !isPascalCase(name) {
		c.rulef("method-structure", "%s: interface.name must be PascalCase", rel)
	}
	pkg, _ := iface["package"].(string)
	if !strings.HasPrefix(pkg, "internal/modules/") {
		c.rulef("method-structure", "%s: interface.package must be under internal/modules/", rel)
	}
	seen := map[string]bool{}
	for _, raw := range asList(iface["methods"]) {
		m, _ := asMap(raw)
		mname, _ := m["name"].(string)
		if !isPascalCase(mname) {
			c.rulef("method-structure", "%s: method name must be PascalCase", rel)
		}
		if seen[mname] {
			c.rulef("method-structure", "%s: duplicate method %s", rel, mname)
		}
		seen[mname] = true
		methodIDs[fileID+"."+contractspec.SnakeCase(mname)] = true
		tx, _ := m["transaction"].(string)
		if tx != "none" && tx != "read_only" {
			c.rulef("method-structure", "%s.%s: transaction must be none or read_only", rel, mname)
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
	}
}

func (c *Checker) checkFlowDoc(doc map[string]any, rel string, methodIDs map[string]bool) {
	module, _ := doc["module"].(string)
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
			if !c.validFlowUse(use, module, methodIDs) {
				c.rulef("cap-ref", "%s: step use %q is not a method, module query, or permission", rel, use)
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

func (c *Checker) validFlowUse(use, module string, methodIDs map[string]bool) bool {
	switch {
	case strings.HasPrefix(use, "cap.methods."):
		return methodIDs[use]
	case strings.HasPrefix(use, "platform.security.permissions."):
		return c.permissionExists(use)
	case strings.HasPrefix(use, "data.queries."):
		if _, ok := c.queryIDs[use]; !ok {
			return false
		}
		for table := range c.queryTables[use] {
			if c.tableOwners[table] != module {
				return false
			}
		}
		return true
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

func isPascalCase(s string) bool {
	if s == "" {
		return false
	}
	r := rune(s[0])
	return r >= 'A' && r <= 'Z'
}
