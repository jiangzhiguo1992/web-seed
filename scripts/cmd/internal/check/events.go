// events.go 校验 cap.events 的 topics 与 jobs。
package check

import (
	"path/filepath"
	"strings"
)

func (c *Checker) checkEventsDoc(schema *Schema, serviceMeta map[string]serviceMeta, serviceIDs map[string]bool) map[string]bool {
	files := glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))
	topicIDs := map[string]bool{}
	jobIDs := map[string]bool{}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		for _, raw := range asList(doc["topics"]) {
			item, _ := asMap(raw)
			if id, _ := item["id"].(string); id != "" {
				if topicIDs[id] {
					c.rulef("event-structure", "%s: duplicate topic id %s", relPath(c.root, file), id)
				}
				topicIDs[id] = true
			}
		}
		for _, raw := range asList(doc["jobs"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			if id == "" {
				continue
			}
			if jobIDs[id] {
				c.rulef("async-structure", "%s: duplicate job id %s", relPath(c.root, file), id)
			}
			jobIDs[id] = true
		}
	}
	for id := range topicIDs {
		if jobIDs[id] {
			c.rulef("event-file-shape", "cap.events id %s is used by both a topic and a job", id)
		}
	}

	jobServices := map[string]bool{}
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
			c.rulef("event-structure", "%s: id %q must be %q", rel, fileID, want)
		}
		module, _ := doc["owner_module"].(string)
		subject := strings.TrimSuffix(filepath.Base(rel), ".yaml")
		topics := asList(doc["topics"])
		jobs := asList(doc["jobs"])
		if len(topics) == 0 && len(jobs) == 0 {
			c.rulef("event-file-shape", "%s: topics or jobs must not be empty", rel)
		}
		if len(topics) > 0 && doc["delivery"] == nil {
			c.rulef("event-file-shape", "%s: delivery is required when topics are declared", rel)
		}

		topicNames := map[string]bool{}
		for _, raw := range topics {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			name, _ := item["name"].(string)
			if topicNames[name] {
				c.rulef("event-structure", "%s: duplicate topic name %s", rel, name)
			}
			topicNames[name] = true
			if name != "" && !strings.HasPrefix(name, subject+".") {
				c.rulef("event-structure", "%s: topic name %s must start with %s.", rel, name, subject)
			}
			if id != "" && name != "" && id != "cap.events."+name {
				c.rulef("event-structure", "%s: topic id %s must be cap.events.%s", rel, id, name)
			}
			for _, rawSvc := range asList(item["emitted_by"]) {
				svc, _ := rawSvc.(string)
				if !serviceIDs[svc] {
					c.rulef("event-structure", "%s: topic %s unknown service %s", rel, id, svc)
				}
			}
			for _, rawConsumer := range asList(item["consumers"]) {
				consumer, _ := rawConsumer.(string)
				if consumer != "" && !jobIDs[consumer] {
					c.rulef("event-structure", "%s: topic %s unknown consumer %s", rel, id, consumer)
				}
			}
		}

		jobNames := map[string]bool{}
		bound := map[string]bool{}
		for _, raw := range jobs {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			if jobNames[name] {
				c.rulef("async-structure", "%s: duplicate job name %s", rel, name)
			}
			jobNames[name] = true
			id, _ := item["id"].(string)
			if id != "" && fileID != "" && id != fileID+"."+name {
				c.rulef("async-structure", "%s.%s: job id %q must be %s", rel, name, id, fileID+"."+name)
			}
			svc, _ := item["service"].(string)
			if svc != "" {
				if meta, ok := serviceMeta[svc]; !ok {
					c.rulef("async-service", "%s.%s: unknown service %s", rel, name, svc)
				} else {
					if meta.module != module {
						c.rulef("async-service", "%s.%s: service %s belongs to module %s, not %s", rel, name, svc, meta.module, module)
					}
					jobServices[svc] = true
				}
			}
			trigger, _ := asMap(item["trigger"])
			triggerType := ""
			if trigger != nil {
				triggerType, _ = trigger["type"].(string)
				switch triggerType {
				case "cron":
					if v, _ := trigger["schedule"].(string); strings.TrimSpace(v) == "" {
						c.rulef("async-trigger", "%s.%s: trigger.schedule required for cron", rel, name)
					}
					if trigger["topic"] != nil {
						c.rulef("async-trigger", "%s.%s: cron trigger must not declare topic", rel, name)
					}
				case "topic":
					topic, _ := trigger["topic"].(string)
					if topic == "" {
						c.rulef("async-trigger", "%s.%s: trigger.topic required for topic", rel, name)
					} else if !topicIDs[topic] {
						c.rulef("async-trigger", "%s.%s: unknown topic %s", rel, name, topic)
					}
					if trigger["schedule"] != nil {
						c.rulef("async-trigger", "%s.%s: topic trigger must not declare schedule", rel, name)
					}
				}
			}
			if triggerType != "" && svc != "" {
				key := triggerType + "\x00" + svc
				if bound[key] {
					c.rulef("async-structure", "%s.%s: duplicate (trigger, service) %s/%s", rel, name, triggerType, svc)
				}
				bound[key] = true
			}
		}
	}
	return jobServices
}
