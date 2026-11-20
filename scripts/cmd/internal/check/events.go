// events.go 校验 cap.events 的 topics（publish/subscribe）与 crons/dispatches。
package check

import (
	"path/filepath"
	"strconv"
	"strings"
)

func (c *Checker) checkEventsDoc(schema *Schema, serviceMeta map[string]serviceMeta, serviceIDs map[string]bool) map[string]bool {
	files := glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))

	// pass 1：全局 ID 与 topic name 索引，topic/subscribe/cron/dispatch 共享 cap.events.* 空间。
	kinds := map[string]string{}
	topicOwner := map[string]string{}
	index := func(rel, id, kind string) {
		if id == "" {
			return
		}
		if prev, ok := kinds[id]; ok {
			c.rulef("event-file-shape", "%s: duplicate cap.events id %s (already declared as %s)", rel, id, prev)
			return
		}
		kinds[id] = kind
	}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		for _, raw := range asList(doc["topics"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			index(rel, id, "topic")
			if name, _ := item["name"].(string); name != "" {
				if prev, ok := topicOwner[name]; ok {
					c.rulef("event-structure", "%s: duplicate topic name %s (already declared in %s)", rel, name, prev)
				} else {
					topicOwner[name] = rel
				}
			}
			for _, rawSub := range asList(item["subscribe"]) {
				sub, _ := asMap(rawSub)
				sid, _ := sub["id"].(string)
				index(rel, sid, "subscribe")
			}
		}
		for _, raw := range asList(doc["crons"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			index(rel, id, "cron")
		}
		for _, raw := range asList(doc["dispatches"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			index(rel, id, "dispatch")
		}
	}

	asyncServices := map[string]bool{}
	bound := map[string]bool{}
	bind := func(rel, slot, name, svc, module string) {
		if svc == "" {
			return
		}
		meta, ok := serviceMeta[svc]
		if !ok {
			c.rulef("async-service", "%s.%s: unknown service %s", rel, name, svc)
			return
		}
		if meta.module != module {
			c.rulef("async-service", "%s.%s: service %s belongs to module %s, not %s", rel, name, svc, meta.module, module)
			return
		}
		key := slot + "\x00" + svc
		if bound[key] {
			c.rulef("async-structure", "%s.%s: duplicate %s binding for service %s", rel, name, slot, svc)
		}
		bound[key] = true
		asyncServices[svc] = true
	}

	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		c.nodeKinds = schema.NodeKinds
		c.propertyKeys = schema.PropertyKeys
		c.valueTypes = schema.ValueTypes
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkTargetFileName(schema, filepath.Base(rel), rel)
		c.checkStability(doc, rel)
		fileID, _ := doc["id"].(string)
		if want := schema.Targets.FileID(filepath.Base(rel)); fileID != want {
			c.rulef("event-file-shape", "%s: id %q must be %q", rel, fileID, want)
		}
		module, _ := doc["owner_module"].(string)
		subject := strings.TrimSuffix(filepath.Base(rel), ".yaml")
		topics := asList(doc["topics"])
		crons := asList(doc["crons"])
		dispatches := asList(doc["dispatches"])
		if len(topics) == 0 && len(crons) == 0 && len(dispatches) == 0 {
			c.rulef("event-file-shape", "%s: topics/crons/dispatches must not be empty", rel)
		}

		subNames := map[string]bool{}
		for _, raw := range topics {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			name, _ := item["name"].(string)
			if name != "" && !strings.HasPrefix(name, subject+".") {
				c.rulef("event-structure", "%s: topic name %s must start with %s.", rel, name, subject)
			}
			if id != "" && name != "" && id != "cap.events."+name {
				c.rulef("event-structure", "%s: topic id %s must be cap.events.%s", rel, id, name)
			}
			publish, _ := asMap(item["publish"])
			emitters := []any(nil)
			if publish != nil {
				emitters = asList(publish["emitted_by"])
			}
			for _, rawSvc := range emitters {
				svc, _ := rawSvc.(string)
				if !serviceIDs[svc] {
					c.rulef("event-structure", "%s: topic %s unknown emitter %s", rel, id, svc)
				}
			}
			subs := asList(item["subscribe"])
			if len(emitters) == 0 && len(subs) == 0 {
				c.rulef("event-orphan", "%s: topic %s is neither published nor subscribed", rel, id)
			}
			for _, rawSub := range subs {
				sub, _ := asMap(rawSub)
				subID, _ := sub["id"].(string)
				subName, _ := sub["name"].(string)
				if subName == "" {
					continue
				}
				if subNames[subName] {
					c.rulef("async-structure", "%s: duplicate subscribe name %s", rel, subName)
				}
				subNames[subName] = true
				if subID != "" && fileID != "" && subID != fileID+"."+subName {
					c.rulef("async-structure", "%s.%s: subscribe id %q must be %s", rel, subName, subID, fileID+"."+subName)
				}
				svc, _ := sub["service"].(string)
				bind(rel, "subscribe", subName, svc, module)
			}
		}

		cronNames := map[string]bool{}
		for _, raw := range crons {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			if cronNames[name] {
				c.rulef("async-structure", "%s: duplicate cron name %s", rel, name)
			}
			cronNames[name] = true
			id, _ := item["id"].(string)
			if id != "" && fileID != "" && id != fileID+"."+name {
				c.rulef("async-structure", "%s.%s: cron id %q must be %s", rel, name, id, fileID+"."+name)
			}
			if schedule, _ := item["schedule"].(string); !validCron(schedule) {
				c.rulef("event-cron-schedule", "%s.%s: invalid cron schedule %q", rel, name, schedule)
			}
			svc, _ := item["service"].(string)
			bind(rel, "cron", name, svc, module)
		}

		dispatchNames := map[string]bool{}
		for _, raw := range dispatches {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			if dispatchNames[name] {
				c.rulef("async-structure", "%s: duplicate dispatch name %s", rel, name)
			}
			dispatchNames[name] = true
			id, _ := item["id"].(string)
			if id != "" && fileID != "" && id != fileID+"."+name {
				c.rulef("async-structure", "%s.%s: dispatch id %q must be %s", rel, name, id, fileID+"."+name)
			}
			svc, _ := item["service"].(string)
			bind(rel, "dispatch", name, svc, module)
		}
	}
	return asyncServices
}

// validCron 校验 5 字段 cron：分 时 日 月 周。支持 *、*/n、a-b、a-b/n、a,b,c 与数字。
func validCron(expr string) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, field := range fields {
		if !validCronField(field, ranges[i][0], ranges[i][1]) {
			return false
		}
	}
	return true
}

func validCronField(field string, min, max int) bool {
	if field == "" {
		return false
	}
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return false
		}
		if part == "*" {
			continue
		}
		if strings.HasPrefix(part, "*/") {
			n, err := strconv.Atoi(strings.TrimPrefix(part, "*/"))
			if err != nil || n <= 0 || n > max-min+1 {
				return false
			}
			continue
		}
		base, step := part, ""
		if idx := strings.Index(part, "/"); idx >= 0 {
			base, step = part[:idx], part[idx+1:]
		}
		if step != "" {
			n, err := strconv.Atoi(step)
			if err != nil || n <= 0 {
				return false
			}
		}
		if strings.Contains(base, "-") {
			se := strings.SplitN(base, "-", 2)
			a, err1 := strconv.Atoi(se[0])
			b, err2 := strconv.Atoi(se[1])
			if err1 != nil || err2 != nil || a < min || b > max || a > b {
				return false
			}
			continue
		}
		n, err := strconv.Atoi(base)
		if err != nil || n < min || n > max {
			return false
		}
	}
	return true
}
