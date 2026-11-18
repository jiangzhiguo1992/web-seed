// anchors.go 从 service steps 反向生成 rules/lifecycles/events 的实现锚点。
//
// 生成物：feature.rules.rules[].implemented_by、cap.lifecycles 的 create.step/destroy.step/
// transitions[].implemented_by、cap.events.topics[].emitted_by / consumers。
// checker 负责结构，gen-graph 负责一致性。
package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// lifecycleFileAction 是生命周期文件里的一个动作，供 Write/Check 定位。
type lifecycleFileAction struct {
	id   string
	name string
	kind string // create / destroy / transition
}

// Anchors 保存 service step 对 rules / lifecycles / events 的反向索引。
type Anchors struct {
	Rules          map[string][]string
	LifecycleSteps map[string][]string
	Events         map[string][]string

	EventConsumers map[string][]string

	RuleFiles      map[string][]string
	LifecycleFiles map[string][]lifecycleFileAction
	EventFiles     map[string][]string
}

func newAnchors() *Anchors {
	return &Anchors{
		Rules:          map[string][]string{},
		LifecycleSteps: map[string][]string{},
		Events:         map[string][]string{},
		EventConsumers: map[string][]string{},
		RuleFiles:      map[string][]string{},
		LifecycleFiles: map[string][]lifecycleFileAction{},
		EventFiles:     map[string][]string{},
	}
}

func (a *Anchors) addRule(ruleID, serviceID string) {
	if ruleID == "" || contains(a.Rules[ruleID], serviceID) {
		return
	}
	a.Rules[ruleID] = append(a.Rules[ruleID], serviceID)
}

func (a *Anchors) addLifecycleStep(actionID, stepID string) {
	if actionID == "" || stepID == "" || contains(a.LifecycleSteps[actionID], stepID) {
		return
	}
	a.LifecycleSteps[actionID] = append(a.LifecycleSteps[actionID], stepID)
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
	if eventTarget, err := resolveTarget(root, "cap.events"); err == nil {
		for _, f := range glob(root, filepath.Join(eventTarget.dir, eventTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return nil, err
			}
			fileID, _ := doc["id"].(string)
			for _, raw := range asList(doc["jobs"]) {
				job, _ := asMap(raw)
				name, _ := job["name"].(string)
				if name == "" {
					continue
				}
				if trigger, ok := asMap(job["trigger"]); ok {
					if topic, _ := trigger["topic"].(string); topic != "" {
						a.EventConsumers[topic] = append(a.EventConsumers[topic], fileID+"."+name)
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
				stepID, _ := step["id"].(string)
				fullStepID := serviceID + "." + stepID
				for _, rawL := range asList(step["lifecycle"]) {
					actionID, _ := rawL.(string)
					a.addLifecycleStep(actionID, fullStepID)
				}
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
					for _, rawRule := range asList(step["rules"]) {
						ruleID, _ := rawRule.(string)
						a.addRule(ruleID, serviceID)
					}
					for _, rawEvent := range asList(step["events"]) {
						eventID, _ := rawEvent.(string)
						a.addEvent(eventID, serviceID)
					}
				}
			}
		}
	}
	for id := range a.Rules {
		a.Rules[id] = uniqueSorted(a.Rules[id])
	}
	for id := range a.LifecycleSteps {
		a.LifecycleSteps[id] = uniqueSorted(a.LifecycleSteps[id])
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
			for _, raw := range asList(doc["topics"]) {
				item, _ := asMap(raw)
				id, _ := item["id"].(string)
				if id != "" {
					a.EventFiles[relFile] = append(a.EventFiles[relFile], id)
				}
			}
		}
	}
	lifecycleTarget, err := resolveTarget(root, "cap.lifecycles")
	if err == nil {
		for _, f := range glob(root, filepath.Join(lifecycleTarget.dir, lifecycleTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return nil, err
			}
			fileID, _ := doc["id"].(string)
			relFile := rel(root, f)
			if _, ok := asMap(doc["create"]); ok {
				a.LifecycleFiles[relFile] = append(a.LifecycleFiles[relFile], lifecycleFileAction{id: fileID + ".create", name: "create", kind: "create"})
			}
			if destroy, ok := asMap(doc["destroy"]); ok {
				actionID := fileID + ".destroy"
				a.LifecycleFiles[relFile] = append(a.LifecycleFiles[relFile], lifecycleFileAction{id: actionID, name: "destroy", kind: "destroy"})
				for _, rawEvent := range asList(destroy["side_effects"]) {
					eventID, _ := rawEvent.(string)
					for _, stepID := range a.LifecycleSteps[actionID] {
						a.addEvent(eventID, serviceIDFromStepID(stepID))
					}
				}
			}
			for _, raw := range asList(doc["transitions"]) {
				item, _ := asMap(raw)
				name, _ := item["name"].(string)
				if name == "" {
					continue
				}
				actionID := fileID + "." + name
				a.LifecycleFiles[relFile] = append(a.LifecycleFiles[relFile], lifecycleFileAction{id: actionID, name: name, kind: "transition"})
				for _, rawEvent := range asList(item["side_effects"]) {
					eventID, _ := rawEvent.(string)
					for _, stepID := range a.LifecycleSteps[actionID] {
						a.addEvent(eventID, serviceIDFromStepID(stepID))
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
	for _, file := range sortedLifecycleFileKeys(a.LifecycleFiles) {
		for _, item := range a.LifecycleFiles[file] {
			if item.kind == "transition" {
				ok, err := syncItemField(filepath.Join(root, file), "transitions", "name", item.name, "implemented_by", a.LifecycleSteps[item.id])
				if err != nil {
					return changed, err
				}
				if ok {
					changed = append(changed, file)
				}
				continue
			}
			step := ""
			if steps := a.LifecycleSteps[item.id]; len(steps) > 0 {
				step = steps[0]
			}
			ok, err := syncNestedField(filepath.Join(root, file), item.kind, "step", step)
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
			ok, err := syncItemField(filepath.Join(root, file), "topics", "id", id, "emitted_by", a.Events[id])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
			ok, err = syncItemField(filepath.Join(root, file), "topics", "id", id, "consumers", a.EventConsumers[id])
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
	for _, file := range sortedLifecycleFileKeys(a.LifecycleFiles) {
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		fileID, _ := doc["id"].(string)
		for _, item := range a.LifecycleFiles[file] {
			if item.kind == "transition" {
				for _, raw := range asList(doc["transitions"]) {
					tr, _ := asMap(raw)
					name, _ := tr["name"].(string)
					if fileID+"."+name != item.id {
						continue
					}
					got := stringList(tr["implemented_by"])
					if !sameSet(got, a.LifecycleSteps[item.id]) {
						drift = append(drift, fmt.Sprintf("%s: %s.implemented_by must be %v, got %v", file, item.id, a.LifecycleSteps[item.id], got))
					}
				}
				continue
			}
			obj, _ := asMap(doc[item.kind])
			got := ""
			if obj != nil {
				got, _ = obj["step"].(string)
			}
			want := ""
			if steps := a.LifecycleSteps[item.id]; len(steps) > 0 {
				want = steps[0]
			}
			if got != want {
				drift = append(drift, fmt.Sprintf("%s: %s.step must be %s, got %s", file, item.id, want, got))
			}
		}
	}
	for _, file := range sortedStringKeys(a.EventFiles) {
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["topics"]) {
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

func sortedLifecycleFileKeys(m map[string][]lifecycleFileAction) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// serviceIDFromStepID 从 cap.services.<file>.<service>.<step> 取回 service item ID。
func serviceIDFromStepID(stepID string) string {
	parts := strings.Split(stepID, ".")
	if len(parts) < 5 {
		return ""
	}
	return strings.Join(parts[:4], ".")
}

// syncNestedField 在 top-level objectKey 块内设置 field。
func syncNestedField(path, objectKey, field, value string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	objStart := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == objectKey+":" {
			objStart = i
			break
		}
	}
	if objStart < 0 {
		return false, fmt.Errorf("%s: object %s not found", path, objectKey)
	}
	end := len(lines)
	for i := objStart + 1; i < len(lines); i++ {
		if lines[i] != "" && !strings.HasPrefix(lines[i], " ") {
			end = i
			break
		}
	}
	for i := objStart + 1; i < end; i++ {
		trim := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trim, field+":") {
			if strings.TrimSpace(strings.TrimPrefix(trim, field+":")) == value {
				return false, nil
			}
			lines[i] = "  " + field + ": " + value
			return true, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		}
	}
	lines = append(lines[:objStart+1], append([]string{"  " + field + ": " + value}, lines[objStart+1:]...)...)
	return true, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
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
