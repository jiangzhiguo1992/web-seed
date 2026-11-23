// scenario.go 校验 scenario.http 验收场景：引用、响应状态、表覆盖与行形状。
package check

import (
	"fmt"
	"path/filepath"
)

func (c *Checker) checkScenarioContracts() {
	schema, err := c.loadSchemaByType("scenario.http")
	if err != nil {
		c.rulef("schema.load", "load scenario.http schema: %v", err)
		return
	}
	c.checkTargets(schema)
	c.checkRules(schema)
	roles := c.roleIDs()
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
		if want := schema.Targets.FileID(filepath.Base(file)); doc["id"] != want {
			c.rulef("scenario-structure", "%s: id %q must be %q", rel, doc["id"], want)
		}
		names := map[string]bool{}
		for i, raw := range asList(doc["scenarios"]) {
			sc, _ := asMap(raw)
			name, _ := sc["name"].(string)
			where := fmt.Sprintf("%s.scenarios[%d]", rel, i)
			if name != "" && names[name] {
				c.rulef("scenario-structure", "%s: duplicate scenario name %s", where, name)
			}
			names[name] = true
			c.checkScenarioItem(where, sc, roles)
		}
	}
}

// checkScenarioItem 校验单个场景的 operation、响应状态、actor 与表覆盖。
func (c *Checker) checkScenarioItem(where string, sc map[string]any, roles map[string]bool) {
	opID, _ := sc["operation"].(string)
	op, ok := c.httpOps[opID]
	if !ok {
		c.rulef("scenario-refs", "%s: unknown operation %s", where, opID)
		return
	}
	actor, _ := asMap(sc["actor"])
	if role, _ := actor["role"].(string); role != "" && !roles[role] {
		c.rulef("scenario-refs", "%s: unknown role %s", where, role)
	}
	for _, raw := range asList(actor["permissions"]) {
		permission, _ := raw.(string)
		if permission != "" && !c.permissionExists(permission) {
			c.rulef("scenario-refs", "%s: unknown permission %s", where, permission)
		}
	}
	then, _ := asMap(sc["then"])
	response, _ := asMap(then["response"])
	status, ok := toInt(response["status"])
	if !ok {
		c.rulef("scenario-refs", "%s: then.response.status required", where)
	} else if !op.Statuses[status] {
		c.rulef("scenario-refs", "%s: status %d is not an effective response of %s", where, status, opID)
	}
	for _, raw := range asList(then["events"]) {
		event, _ := raw.(string)
		if event != "" {
			if _, ok := c.eventPayloads[event]; !ok {
				c.rulef("scenario-refs", "%s: unknown event %s", where, event)
			}
		}
	}
	db, _ := asMap(then["db"])
	if unchanged, _ := db["unchanged"].(string); unchanged != "strict" {
		c.rulef("scenario-structure", "%s: then.db.unchanged must be strict", where)
	}
	queries := c.serviceQueryClosure[op.Service]
	targets, writes := c.scenarioTables(queries)
	given, _ := asMap(sc["given"])
	givenDB, _ := asMap(given["db"])
	covered := map[string]bool{}
	for table, rawRows := range givenDB {
		covered[table] = true
		if !targets[table] {
			c.rulef("scenario-db", "%s: given.db table %s is not in the service chain of %s", where, table, op.Service)
		}
		c.checkScenarioRows(where, "given.db."+table, table, rawRows, false)
	}
	for _, key := range []string{"added", "changed", "removed"} {
		rowsByTable, _ := asMap(db[key])
		for table, rawRows := range rowsByTable {
			covered[table] = true
			if !writes[table] {
				c.rulef("scenario-db", "%s: then.db.%s table %s is not written by %s", where, key, table, op.Service)
			}
			c.checkScenarioRows(where, "then.db."+key+"."+table, table, rawRows, key == "changed")
		}
	}
	for table := range targets {
		if !covered[table] {
			c.rulef("scenario-db", "%s: table %s is in the service chain but not covered by given/then", where, table)
		}
	}
}

// scenarioTables 返回 service 查询闭包涉及的表与写表。
func (c *Checker) scenarioTables(queries map[string]bool) (targets, writes map[string]bool) {
	targets = map[string]bool{}
	writes = map[string]bool{}
	for qid := range queries {
		q := c.queries[qid]
		if q == nil {
			continue
		}
		for _, table := range q.Targets {
			targets[table] = true
		}
		for _, table := range q.Writes {
			writes[table] = true
		}
	}
	return targets, writes
}

// checkScenarioRows 校验行对象只使用表列；changed 行必须含 key/set。
func (c *Checker) checkScenarioRows(where, path, table string, rawRows any, changed bool) {
	rows, ok := rawRows.([]any)
	if !ok {
		c.rulef("scenario-db", "%s: %s must be a list", where, path)
		return
	}
	columns := c.tableColumns[table]
	for i, rawRow := range rows {
		row, ok := asMap(rawRow)
		if !ok {
			c.rulef("scenario-db", "%s: %s[%d] must be an object", where, path, i)
			continue
		}
		if changed {
			key, _ := asMap(row["key"])
			set, _ := asMap(row["set"])
			if len(key) == 0 || len(set) == 0 {
				c.rulef("scenario-db", "%s: %s[%d] requires non-empty key and set", where, path, i)
				continue
			}
			c.checkScenarioColumns(where, path, table, key, columns, i)
			c.checkScenarioColumns(where, path, table, set, columns, i)
			continue
		}
		c.checkScenarioColumns(where, path, table, row, columns, i)
	}
}

func (c *Checker) checkScenarioColumns(where, path, table string, row map[string]any, columns map[string]columnMeta, index int) {
	if columns == nil {
		return
	}
	for name := range row {
		if _, ok := columns[name]; !ok {
			c.rulef("scenario-db", "%s: %s[%d] uses unknown column %s.%s", where, path, index, table, name)
		}
	}
}

// roleIDs 读取 platform.security.roles 的 code 集合。
func (c *Checker) roleIDs() map[string]bool {
	out := map[string]bool{}
	doc, err := loadYAML(filepath.Join(c.root, "contracts/platform/security/roles.yaml"))
	if err != nil {
		return out
	}
	for _, raw := range asList(doc["roles"]) {
		item, _ := asMap(raw)
		if code, _ := item["code"].(string); code != "" {
			out["platform.security.roles."+code] = true
		}
	}
	return out
}
