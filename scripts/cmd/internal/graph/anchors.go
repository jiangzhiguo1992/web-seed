// anchors.go 从 service steps 反向生成 rules/states/events 的实现锚点。
//
// 生成物：feature.rules.rules[].implemented_by、feature.states.transitions[].implemented_by、
// cap.events.events[].emitted_by。checker 负责结构，gen-graph 负责一致性。
package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Anchors 保存 service step 对 rules / states / events 的反向索引。
type Anchors struct {
	Rules  map[string][]string
	States map[string][]string
	Events map[string][]string

	EventConsumers map[string][]string

	RuleFiles  map[string][]string
	StateFiles map[string][][2]string
	EventFiles map[string][]string
}

func newAnchors() *Anchors {
	return &Anchors{
		Rules:          map[string][]string{},
		States:         map[string][]string{},
		Events:         map[string][]string{},
		EventConsumers: map[string][]string{},
		RuleFiles:      map[string][]string{},
		StateFiles:     map[string][][2]string{},
		EventFiles:     map[string][]string{},
	}
}

func (a *Anchors) addRule(ruleID, serviceID string) {
	if ruleID == "" || contains(a.Rules[ruleID], serviceID) {
		return
	}
	a.Rules[ruleID] = append(a.Rules[ruleID], serviceID)
}

func (a *Anchors) addState(stateID, serviceID string) {
	if stateID == "" || contains(a.States[stateID], serviceID) {
		return
	}
	a.States[stateID] = append(a.States[stateID], serviceID)
}

func (a *Anchors) addEvent(eventID, serviceID string) {
	if eventID == "" || contains(a.Events[eventID], serviceID) {
		return
	}
	a.Events[eventID] = append(a.Events[eventID], serviceID)
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// computeAnchors 扫描 services 的 steps，并登记 rules/states/events 的文件归属。
func computeAnchors(root string, serviceTarget scanTarget) (*Anchors, error) {
	a := newAnchors()
	if asyncTarget, err := resolveTarget(root, "cap.async"); err == nil {
		for _, f := range glob(root, filepath.Join(asyncTarget.dir, asyncTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return nil, err
			}
			module, _ := doc["owner_module"].(string)
			for _, raw := range asList(doc["jobs"]) {
				job, _ := asMap(raw)
				if trigger, ok := asMap(job["trigger"]); ok {
					if ev, _ := trigger["event"].(string); ev != "" {
						a.EventConsumers[ev] = append(a.EventConsumers[ev], module)
					}
				}
			}
		}
	}
	for _, f := range glob(root, filepath.Join(serviceTarget.dir, serviceTarget.glob)) {
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		fileID, _ := doc["id"].(string)
		if fileID == "" {
			continue
		}
		for _, raw := range asList(doc["services"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			serviceID := fileID + "." + name
			for _, rawStep := range asList(item["steps"]) {
				step, _ := asMap(rawStep)
				switch kind, _ := step["kind"].(string); kind {
				case "logic":
					for _, rawRule := range asList(step["rules"]) {
						ruleID, _ := rawRule.(string)
						a.addRule(ruleID, serviceID)
					}
					for _, rawEvent := range asList(step["events"]) {
						eventID, _ := rawEvent.(string)
						a.addEvent(eventID, serviceID)
					}
				case "transition":
					for _, rawState := range asList(step["transitions"]) {
						stateID, _ := rawState.(string)
						a.addState(stateID, serviceID)
					}
					for _, rawEvent := range asList(step["events"]) {
						eventID, _ := rawEvent.(string)
						a.addEvent(eventID, serviceID)
					}
				case "event":
					eventID, _ := step["event"].(string)
					a.addEvent(eventID, serviceID)
				}
			}
		}
	}
	for id := range a.Rules {
		a.Rules[id] = uniqueSorted(a.Rules[id])
	}
	for id := range a.States {
		a.States[id] = uniqueSorted(a.States[id])
	}
	for id := range a.Events {
		a.Events[id] = uniqueSorted(a.Events[id])
	}
	for id := range a.EventConsumers {
		a.EventConsumers[id] = uniqueSorted(a.EventConsumers[id])
	}

	// 登记文件归属，供 Write 定位。
	ruleTarget, err := resolveTarget(root, "feature.rules")
	if err == nil {
		for _, f := range glob(root, filepath.Join(ruleTarget.dir, ruleTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return nil, err
			}
			relFile := rel(root, f)
			for _, raw := range asList(doc["rules"]) {
				item, _ := asMap(raw)
				id, _ := item["id"].(string)
				if id != "" {
					a.RuleFiles[relFile] = append(a.RuleFiles[relFile], id)
				}
			}
		}
	}
	eventTarget, err := resolveTarget(root, "cap.events")
	if err == nil {
		for _, f := range glob(root, filepath.Join(eventTarget.dir, eventTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return nil, err
			}
			relFile := rel(root, f)
			for _, raw := range asList(doc["events"]) {
				item, _ := asMap(raw)
				id, _ := item["id"].(string)
				if id != "" {
					a.EventFiles[relFile] = append(a.EventFiles[relFile], id)
					a.EventConsumers[id] = append(a.EventConsumers[id], stringList(item["consumers"])...)
				}
			}
		}
	}
	stateTarget, err := resolveTarget(root, "feature.states")
	if err == nil {
		for _, f := range glob(root, filepath.Join(stateTarget.dir, stateTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return nil, err
			}
			fileID, _ := doc["id"].(string)
			relFile := rel(root, f)
			for _, raw := range asList(doc["transitions"]) {
				item, _ := asMap(raw)
				name, _ := item["name"].(string)
				if name == "" {
					continue
				}
				stateID := fileID + "." + name
				a.StateFiles[relFile] = append(a.StateFiles[relFile], [2]string{stateID, name})
				for _, rawEvent := range asList(item["side_effects"]) {
					eventID, _ := rawEvent.(string)
					for _, svc := range a.States[stateID] {
						a.addEvent(eventID, svc)
					}
				}
			}
		}
	}
	for id := range a.EventConsumers {
		a.EventConsumers[id] = uniqueSorted(a.EventConsumers[id])
	}
	return a, nil
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	for _, v := range values {
		if v != "" {
			set[v] = true
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// Write 把锚点写回 rules/states/events 文件。
func (a *Anchors) Write(root string) ([]string, error) {
	var changed []string
	for _, file := range sortedStringKeys(a.RuleFiles) {
		for _, id := range a.RuleFiles[file] {
			ok, err := syncItemField(filepath.Join(root, file), "rules", "id", id, "implemented_by", a.Rules[id])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
		}
	}
	for _, file := range sortedStateFileKeys(a.StateFiles) {
		for _, item := range a.StateFiles[file] {
			ok, err := syncItemField(filepath.Join(root, file), "transitions", "name", item[1], "implemented_by", a.States[item[0]])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
		}
	}
	for _, file := range sortedStringKeys(a.EventFiles) {
		for _, id := range a.EventFiles[file] {
			ok, err := syncItemField(filepath.Join(root, file), "events", "id", id, "emitted_by", a.Events[id])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
			ok, err = syncItemField(filepath.Join(root, file), "events", "id", id, "consumers", a.EventConsumers[id])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
		}
	}
	return changed, nil
}

// Check 返回锚点漂移；不写文件。
func (a *Anchors) Check(root string) ([]string, error) {
	var drift []string
	for _, file := range sortedStringKeys(a.RuleFiles) {
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["rules"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			if id == "" {
				continue
			}
			got := stringList(item["implemented_by"])
			if !sameSet(got, a.Rules[id]) {
				drift = append(drift, fmt.Sprintf("%s: %s.implemented_by must be %v, got %v", file, id, a.Rules[id], got))
			}
		}
	}
	for _, file := range sortedStateFileKeys(a.StateFiles) {
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["transitions"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			id := fileID + "." + name
			got := stringList(item["implemented_by"])
			if !sameSet(got, a.States[id]) {
				drift = append(drift, fmt.Sprintf("%s: %s.implemented_by must be %v, got %v", file, id, a.States[id], got))
			}
		}
	}
	for _, file := range sortedStringKeys(a.EventFiles) {
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["events"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			if id == "" {
				continue
			}
			got := stringList(item["emitted_by"])
			if !sameSet(got, a.Events[id]) {
				drift = append(drift, fmt.Sprintf("%s: %s.emitted_by must be %v, got %v", file, id, a.Events[id], got))
			}
			gotConsumers := stringList(item["consumers"])
			if !sameSet(gotConsumers, a.EventConsumers[id]) {
				drift = append(drift, fmt.Sprintf("%s: %s.consumers must be %v, got %v", file, id, a.EventConsumers[id], gotConsumers))
			}
		}
	}
	return drift, nil
}

func sortedStringKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedStateFileKeys(m map[string][][2]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// syncItemField 在 listKey 列表中找到 itemKey=itemValue 的块，
// 替换或插入 field 行（flow list）。
func syncItemField(path, listKey, itemKey, itemValue, field string, values []string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	text := strings.TrimSuffix(string(b), "\n")
	lines := strings.Split(text, "\n")
	listStart := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == listKey+":" {
			listStart = i
			break
		}
	}
	if listStart < 0 {
		return false, fmt.Errorf("%s: list %s not found", path, listKey)
	}
	itemStart := -1
	for i := listStart + 1; i < len(lines); i++ {
		trim := strings.TrimPrefix(lines[i], "  - ")
		if strings.HasPrefix(trim, itemKey+": ") && strings.TrimSpace(strings.TrimPrefix(trim, itemKey+": ")) == itemValue {
			itemStart = i
			break
		}
		if strings.TrimSpace(lines[i]) != "" && !strings.HasPrefix(lines[i], " ") {
			break
		}
	}
	if itemStart < 0 {
		return false, fmt.Errorf("%s: item %s=%s not found", path, itemKey, itemValue)
	}
	itemEnd := len(lines)
	for i := itemStart + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "  - ") || (strings.TrimSpace(lines[i]) != "" && !strings.HasPrefix(lines[i], " ")) {
			itemEnd = i
			break
		}
	}
	fieldLine := -1
	for i := itemStart + 1; i < itemEnd; i++ {
		if strings.HasPrefix(lines[i], "    "+field+":") {
			fieldLine = i
			break
		}
	}
	rendered := "    " + field + ": " + renderFlowList(values)
	if fieldLine >= 0 {
		if lines[fieldLine] == rendered {
			return false, nil
		}
		lines[fieldLine] = rendered
	} else {
		anchor := "summary:"
		switch field {
		case "implemented_by":
			anchor = "on_violation:"
		case "consumers":
			anchor = "emitted_by:"
		}
		insert := itemEnd
		for i := itemStart + 1; i < itemEnd; i++ {
			if strings.HasPrefix(lines[i], "    "+anchor) {
				insert = i + 1
				break
			}
		}
		lines = append(lines[:insert], append([]string{rendered}, lines[insert:]...)...)
	}
	out := strings.Join(lines, "\n") + "\n"
	if out == string(b) {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(out), 0o644)
}

func renderFlowList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	return "[ " + strings.Join(values, ", ") + " ]"
}
