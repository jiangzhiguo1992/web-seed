package check

import "strings"

// legacy_test.go 保存旧 flow/method 测试适配器。
// 生产契约已改为 cap.services，本文件只服务历史用例，不参与运行时校验。

// checkFlowItem 是旧测试的兼容入口：把旧 flow 文档适配为 service item。
func (c *Checker) checkFlowItem(doc map[string]any, rel, id, module string, methodIDs map[string]bool, methodErrors map[string]map[string]bool, typeNames map[string]bool) {
	metaByID := map[string]serviceMeta{}
	for mid := range methodIDs {
		metaByID[mid] = serviceMeta{
			module:     module,
			visibility: "public",
			params:     map[string]bool{},
			errors:     methodErrors[mid],
			returns:    c.methodReturns[mid],
		}
	}
	steps := []any{}
	for _, raw := range asList(doc["steps"]) {
		old, _ := asMap(raw)
		step := map[string]any{"summary": "x"}
		for k, v := range old {
			if k == "use" {
				continue
			}
			step[k] = v
		}
		use, _ := old["use"].(string)
		switch {
		case use == "":
			step["kind"] = "logic"
			if step["output"] == nil {
				step["output"] = []any{}
			}
			if step["errors"] == nil {
				step["errors"] = []any{}
			}
		case strings.HasPrefix(use, "cap.methods."):
			step["kind"] = "service"
			step["service"] = use
		default:
			step["kind"] = "logic"
			if step["output"] == nil {
				step["output"] = []any{}
			}
			if step["errors"] == nil {
				step["errors"] = []any{}
			}
		}
		steps = append(steps, step)
	}
	if method, _ := doc["method"].(string); method != "" {
		steps = append(steps, map[string]any{
			"id":      "call",
			"kind":    "service",
			"summary": "x",
			"service": method,
		})
	}
	doc2 := map[string]any{
		"summary":        doc["summary"],
		"transaction":    doc["transaction"],
		"errors":         doc["errors"],
		"params":         doc["params"],
		"steps":          steps,
		"max_iterations": doc["max_iterations"],
		"trigger":        doc["trigger"],
		"retry":          doc["retry"],
		"concurrency":    doc["concurrency"],
	}
	for k, v := range doc2 {
		if v == nil {
			delete(doc2, k)
		}
	}
	if c.accessBindings == nil {
		c.accessBindings = map[string]accessPolicy{}
	}
	policy := accessPolicy{}
	for _, rawPerm := range asList(doc["permissions"]) {
		perm, _ := rawPerm.(string)
		policy.permissions = append(policy.permissions, perm)
	}
	policy.audit, _ = doc["audit"].(bool)
	policy.rateLimited = doc["rate_limit"] != nil
	if len(policy.permissions) > 0 || policy.audit || policy.rateLimited {
		c.accessBindings[id] = policy
	} else {
		delete(c.accessBindings, id)
	}
	c.checkServiceItem(doc2, rel, id, module, metaByID, map[string]bool{}, map[string]bool{}, map[string]bool{}, typeNames)
}

// checkServicePersistenceDoc 校验单个 service 文档的写归属。
func (c *Checker) checkServicePersistenceDoc(doc map[string]any, rel, module string, tableIDs map[string]string) {
	if c.declaredWrites == nil {
		c.declaredWrites = map[string]bool{}
	}
	p, _ := asMap(doc["persistence"])
	tables := tableRefs(p)
	for _, t := range tables {
		if c.isBaseTableRef(t) {
			c.rulef("schema.ref", "%s: base contract is not a physical table", rel)
		} else if _, ok := tableIDs[t]; !ok {
			c.rulef("schema.ref", "%s: persistence references unknown table %s", rel, t)
		}
		if owner := c.tableOwners[t]; owner != "" && owner != module {
			c.rulef("service-owner-write", "%s: module %q cannot write %s (owner_module %q)", rel, module, t, owner)
		}
	}

	for _, raw := range asList(doc["services"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		for _, rawStep := range asList(item["steps"]) {
			step, _ := asMap(rawStep)
			kind, _ := step["kind"].(string)
			var qids []string
			switch kind {
			case "query":
				qid, _ := step["query"].(string)
				if qid != "" {
					qids = append(qids, qid)
				}
			case "logic", "transition":
				for _, rawQ := range asList(step["queries"]) {
					qid, _ := rawQ.(string)
					if qid != "" {
						qids = append(qids, qid)
					}
				}
			}
			for _, qid := range qids {
				q := c.queries[qid]
				if q == nil {
					continue
				}
				if queryReturnsAffectedRows(q) && len(q.Writes) == 0 {
					c.rulef("service-owner-write", "%s.%s: query %s returns affected_rows without a write table", rel, name, qid)
				}
				for _, target := range q.Writes {
					c.declaredWrites[qid] = true
					if owner := c.tableOwners[target]; owner != "" && owner != module {
						c.rulef("service-owner-write", "%s.%s: module %q cannot write %s via %s (owner_module %q); cross-module writes must go through kind=service", rel, name, module, target, qid, owner)
					}
				}
			}
		}
	}
}

// checkMethodPersistence 是旧测试的兼容入口：把 interface.methods[].queries 适配为 logic steps。
func (c *Checker) checkMethodPersistence(doc map[string]any, rel string, tableIDs map[string]string) {
	module, _ := doc["owner_module"].(string)
	iface, _ := asMap(doc["interface"])
	var services []any
	for _, raw := range asList(iface["methods"]) {
		m, _ := asMap(raw)
		services = append(services, map[string]any{
			"name": m["name"],
			"steps": []any{map[string]any{
				"kind":    "logic",
				"queries": m["queries"],
			}},
		})
	}
	c.checkServicePersistenceDoc(map[string]any{
		"owner_module": module,
		"persistence":  doc["persistence"],
		"services":     services,
	}, rel, module, tableIDs)
}

func (c *Checker) tableObjectExists(tables []string, name string) bool {
	for _, t := range tables {
		if c.tableObjects[t][name] {
			return true
		}
	}
	return false
}
