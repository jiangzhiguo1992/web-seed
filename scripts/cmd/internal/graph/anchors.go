// anchors.go 从 service steps 反向生成 rules/lifecycles/events 的实现锚点。
//
// 生成物：feature.rules.rules[].implemented_by、cap.lifecycles 的 create.services/destroy.services/
// transitions[].implemented_by、cap.events.topics[].emitted_by / consumers。
// checker 负责结构，gen-graph 负责一致性。
package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
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
	ServiceUsedBy  map[string][]string

	RuleFiles           map[string][]string
	LifecycleFiles      map[string][]lifecycleFileAction
	LifecycleStates     map[string][]string
	LifecycleStateSteps map[string]map[string][]string
	EventFiles          map[string][]string
	DispatchFiles       map[string][]string
}

func newAnchors() *Anchors {
	return &Anchors{
		Rules:               map[string][]string{},
		LifecycleSteps:      map[string][]string{},
		Events:              map[string][]string{},
		ServiceUsedBy:       map[string][]string{},
		RuleFiles:           map[string][]string{},
		LifecycleFiles:      map[string][]lifecycleFileAction{},
		LifecycleStates:     map[string][]string{},
		LifecycleStateSteps: map[string]map[string][]string{},
		EventFiles:          map[string][]string{},
		DispatchFiles:       map[string][]string{},
	}
}

func (a *Anchors) addRule(ruleID, serviceID string) {
	if ruleID == "" || contains(a.Rules[ruleID], serviceID) {
		return
	}
	a.Rules[ruleID] = append(a.Rules[ruleID], serviceID)
}

func (a *Anchors) addLifecycleStep(actionID, stepID string) {
	if actionID == "" || stepID == "" {
		return
	}
	if !contains(a.LifecycleSteps[actionID], stepID) {
		a.LifecycleSteps[actionID] = append(a.LifecycleSteps[actionID], stepID)
	}
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
	stepStates := map[string]map[string][]string{}
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
				if stepID != "" {
					if parsed := parseStateGate(step["state_gate"]); len(parsed) > 0 {
						stepStates[fullStepID] = parsed
					}
				}
				for _, rawL := range asList(step["lifecycles"]) {
					actionID, _ := rawL.(string)
					a.addLifecycleStep(actionID, fullStepID)
				}
				switch kind, _ := step["kind"].(string); kind {
				case "logic", "when":
					for _, rawRule := range asList(step["rules"]) {
						rm, _ := rawRule.(map[string]any)
						ruleID, _ := rm["rule"].(string)
						if ruleID != "" {
							a.addRule(ruleID, serviceID)
						}
					}
				case "events":
					for _, rawEvent := range asList(step["events"]) {
						ev, _ := asMap(rawEvent)
						eventID, _ := ev["ref"].(string)
						if eventID != "" {
							a.addEvent(eventID, serviceID)
						}
					}
				case "service":
					if ref, _ := step["service"].(string); ref != "" {
						a.ServiceUsedBy[ref] = append(a.ServiceUsedBy[ref], serviceID)
					}
				}
			}
		}
	}
	if protocolTarget, err := resolveTarget(root, "cap.protocols.http"); err == nil {
		for _, f := range glob(root, filepath.Join(protocolTarget.dir, protocolTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return nil, err
			}
			paths, _ := asMap(doc["paths"])
			for _, rawOps := range paths {
				ops, _ := asMap(rawOps)
				for _, rawOp := range ops {
					op, _ := asMap(rawOp)
					svc, _ := op["service"].(string)
					cid, _ := op["id"].(string)
					if contract, ok := asMap(op["contract"]); ok {
						svc, _ = contract["service"].(string)
						cid, _ = contract["id"].(string)
					}
					if svc != "" && cid != "" {
						a.ServiceUsedBy[svc] = append(a.ServiceUsedBy[svc], cid)
					}
				}
			}
		}
	}
	if eventTarget, err := resolveTarget(root, "cap.events"); err == nil {
		for _, f := range glob(root, filepath.Join(eventTarget.dir, eventTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return nil, err
			}
			for _, raw := range asList(doc["topics"]) {
				topic, _ := asMap(raw)
				for _, rawSub := range asList(topic["subscribe"]) {
					sub, _ := asMap(rawSub)
					svc, _ := sub["service"].(string)
					id, _ := sub["id"].(string)
					if svc != "" && id != "" {
						a.ServiceUsedBy[svc] = append(a.ServiceUsedBy[svc], id)
					}
				}
			}
			for _, key := range []string{"crons", "dispatches"} {
				for _, raw := range asList(doc[key]) {
					item, _ := asMap(raw)
					svc, _ := item["service"].(string)
					id, _ := item["id"].(string)
					if svc != "" && id != "" {
						a.ServiceUsedBy[svc] = append(a.ServiceUsedBy[svc], id)
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
	for id := range a.ServiceUsedBy {
		a.ServiceUsedBy[id] = uniqueSorted(a.ServiceUsedBy[id])
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
			for _, raw := range asList(doc["dispatches"]) {
				item, _ := asMap(raw)
				id, _ := item["id"].(string)
				if id != "" {
					a.DispatchFiles[relFile] = append(a.DispatchFiles[relFile], id)
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
				actionID := fileID + ".create"
				a.LifecycleFiles[relFile] = append(a.LifecycleFiles[relFile], lifecycleFileAction{id: actionID, name: "create", kind: "create"})
			}
			if _, ok := asMap(doc["destroy"]); ok {
				actionID := fileID + ".destroy"
				a.LifecycleFiles[relFile] = append(a.LifecycleFiles[relFile], lifecycleFileAction{id: actionID, name: "destroy", kind: "destroy"})
			}
			for _, raw := range asList(doc["transitions"]) {
				item, _ := asMap(raw)
				name, _ := item["name"].(string)
				if name == "" {
					continue
				}
				actionID := fileID + ".transitions." + name
				a.LifecycleFiles[relFile] = append(a.LifecycleFiles[relFile], lifecycleFileAction{id: actionID, name: name, kind: "transition"})
			}
			statesObj, _ := asMap(doc["states"])
			order := lifecycleStateNames(statesObj["values"])
			a.LifecycleStates[fileID] = order
			stepsByState := map[string][]string{}
			for stepID, byLifecycle := range stepStates {
				values, ok := byLifecycle[fileID]
				if !ok {
					continue
				}
				for _, state := range values {
					stepsByState[state] = append(stepsByState[state], stepID)
				}
			}
			for state := range stepsByState {
				stepsByState[state] = uniqueSorted(stepsByState[state])
			}
			a.LifecycleStateSteps[fileID] = stepsByState
		}
	}
	return a, nil
}

func lifecycleStateNames(raw any) []string {
	var out []string
	for _, item := range asList(raw) {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
			continue
		}
		if m, ok := asMap(item); ok {
			if v, _ := m["value"].(string); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// parseStateGate 读取 step.state_gate 的 lifecycle -> values；省略表示全部允许。
func parseStateGate(raw any) map[string][]string {
	out := map[string][]string{}
	for _, item := range asList(raw) {
		m, _ := asMap(item)
		lifecycleID, _ := m["lifecycle"].(string)
		if lifecycleID == "" {
			continue
		}
		out[lifecycleID] = stringList(m["values"])
	}
	return out
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
				ok, err := syncItemFieldBlock(filepath.Join(root, file), "transitions", "name", item.name, "implemented_by", a.LifecycleSteps[item.id])
				if err != nil {
					return changed, err
				}
				if ok {
					changed = append(changed, file)
				}
				continue
			}
			ok, err := syncNestedField(filepath.Join(root, file), item.kind, "implemented_by", a.LifecycleSteps[item.id])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
		}
	}
	for _, file := range sortedLifecycleFileKeys(a.LifecycleFiles) {
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return changed, err
		}
		fileID, _ := doc["id"].(string)
		ok, err := syncLifecycleStateFile(filepath.Join(root, file), a.LifecycleStates[fileID], a.LifecycleStateSteps[fileID])
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
	}
	for _, file := range sortedStringKeys(a.EventFiles) {
		for _, id := range a.EventFiles[file] {
			ok, err := syncItemNestedField(filepath.Join(root, file), "topics", "id", id, "publish", "emitted_by", a.Events[id])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
		}
	}
	for _, file := range sortedStringKeys(a.DispatchFiles) {
		for _, id := range a.DispatchFiles[file] {
			ok, err := syncItemField(filepath.Join(root, file), "dispatches", "id", id, "emitted_by", a.Events[id])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
		}
	}
	if serviceTarget, err := resolveTarget(root, "cap.services"); err == nil {
		for _, f := range glob(root, filepath.Join(serviceTarget.dir, serviceTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return changed, err
			}
			fileID, _ := doc["id"].(string)
			relFile := rel(root, f)
			for _, raw := range asList(doc["services"]) {
				item, _ := asMap(raw)
				name, _ := item["name"].(string)
				if name == "" {
					continue
				}
				ok, err := syncItemFieldBlock(filepath.Join(root, relFile), "services", "name", name, "used_by", a.ServiceUsedBy[fileID+"."+name])
				if err != nil {
					return changed, err
				}
				if ok {
					changed = append(changed, relFile)
				}
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
					if fileID+".transitions."+name != item.id {
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
			got := []string{}
			if obj != nil {
				got = stringList(obj["implemented_by"])
			}
			want := a.LifecycleSteps[item.id]
			if !sameSet(got, want) {
				drift = append(drift, fmt.Sprintf("%s: %s.implemented_by must be %v, got %v", file, item.id, want, got))
			}
		}
		states, _ := asMap(doc["states"])
		if gotOrder := lifecycleStateNames(states["values"]); !sameStringOrder(gotOrder, a.LifecycleStates[fileID]) {
			drift = append(drift, fmt.Sprintf("%s: states.values order must be %v, got %v", file, a.LifecycleStates[fileID], gotOrder))
		}
		if !sameLifecycleSteps(states["values"], a.LifecycleStateSteps[fileID]) {
			drift = append(drift, fmt.Sprintf("%s: states.values[].steps is out of date", file))
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
			publish, _ := asMap(item["publish"])
			got := []string{}
			if publish != nil {
				got = stringList(publish["emitted_by"])
			}
			if !sameSet(got, a.Events[id]) {
				drift = append(drift, fmt.Sprintf("%s: %s.publish.emitted_by must be %v, got %v", file, id, a.Events[id], got))
			}
		}
	}
	for _, file := range sortedStringKeys(a.DispatchFiles) {
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["dispatches"]) {
			item, _ := asMap(raw)
			id, _ := item["id"].(string)
			if id == "" {
				continue
			}
			got := stringList(item["emitted_by"])
			if !sameSet(got, a.Events[id]) {
				drift = append(drift, fmt.Sprintf("%s: %s.emitted_by must be %v, got %v", file, id, a.Events[id], got))
			}
		}
	}
	if serviceTarget, err := resolveTarget(root, "cap.services"); err == nil {
		for _, f := range glob(root, filepath.Join(serviceTarget.dir, serviceTarget.glob)) {
			doc, err := loadYAML(f)
			if err != nil {
				return nil, err
			}
			fileID, _ := doc["id"].(string)
			relFile := rel(root, f)
			for _, raw := range asList(doc["services"]) {
				item, _ := asMap(raw)
				name, _ := item["name"].(string)
				if name == "" {
					continue
				}
				got := stringList(item["used_by"])
				want := a.ServiceUsedBy[fileID+"."+name]
				if !sameSet(got, want) {
					drift = append(drift, fmt.Sprintf("%s: %s.used_by must be %v, got %v", relFile, fileID+"."+name, want, got))
				}
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

// syncLifecycleStateFile 重写 states.values 为 [{value, steps}]，保持状态顺序。
func syncLifecycleStateFile(path string, order []string, steps map[string][]string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	states, _ := asMap(doc["states"])
	gotOrder := lifecycleStateNames(states["values"])
	if sameStringOrder(gotOrder, order) && sameLifecycleSteps(states["values"], steps) {
		return false, nil
	}
	items := make([]string, 0, len(order))
	for _, state := range order {
		items = append(items, fmt.Sprintf("{ value: %s, steps: %s }", state, renderFlowList(steps[state])))
	}
	out, err := applyNestedList(src, "states", "initial", "values", items)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, writeAtomic(path, out)
}

func sameStringOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameLifecycleSteps(raw any, want map[string][]string) bool {
	for _, item := range asList(raw) {
		m, ok := asMap(item)
		if !ok {
			return false // 旧字符串列表需要迁移为 {value, steps}
		}
		state, _ := m["value"].(string)
		if state == "" {
			continue
		}
		if !sameSet(stringList(m["steps"]), want[state]) {
			return false
		}
	}
	return true
}

// syncNestedField 在 top-level objectKey 块内设置字段（块风格列表）。
func syncNestedField(path, objectKey, field string, values []string) (bool, error) {
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
	rendered := []string{"  " + field + ":"}
	if len(values) == 0 {
		rendered = []string{"  " + field + ": []"}
	} else {
		for _, value := range values {
			rendered = append(rendered, "    - "+value)
		}
	}
	for i := objStart + 1; i < end; i++ {
		trim := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trim, field+":") {
			j := i + 1
			for j < end {
				if strings.TrimSpace(lines[j]) == "" {
					j++
					continue
				}
				indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
				if indent <= 2 {
					break
				}
				j++
			}
			if sameLines(lines[i:j], rendered) {
				return false, nil
			}
			lines = append(lines[:i], append(rendered, lines[j:]...)...)
			return true, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		}
	}
	lines = append(lines[:objStart+1], append(rendered, lines[objStart+1:]...)...)
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
		j := fieldLine + 1
		for j < itemEnd {
			if strings.TrimSpace(lines[j]) == "" {
				j++
				continue
			}
			indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
			if indent <= 4 {
				break
			}
			j++
		}
		if j > fieldLine+1 {
			lines = append(lines[:fieldLine], append([]string{rendered}, lines[j:]...)...)
		} else {
			lines[fieldLine] = rendered
		}
	} else {
		anchor := "summary:"
		switch field {
		case "implemented_by":
			anchor = "on_violation:"
		case "consumed_by", "consumers":
			anchor = "emitted_by:"
		case "emitted_by":
			anchor = "service:"
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

// syncItemNestedField 在 listKey 列表中找到 itemKey=itemValue 的块，
// 在其 parentKey 子对象内设置 field 行（flow list）。
func syncItemNestedField(path, listKey, itemKey, itemValue, parentKey, field string, values []string) (bool, error) {
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
	parentStart := -1
	for i := itemStart + 1; i < itemEnd; i++ {
		if strings.HasPrefix(lines[i], "    "+parentKey+":") {
			parentStart = i
			break
		}
	}
	if parentStart < 0 {
		return false, fmt.Errorf("%s: %s.%s not found", path, itemKey, parentKey)
	}
	parentEnd := itemEnd
	for i := parentStart + 1; i < itemEnd; i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		if indent <= 4 {
			parentEnd = i
			break
		}
	}
	fieldLine := -1
	for i := parentStart + 1; i < parentEnd; i++ {
		if strings.HasPrefix(lines[i], "      "+field+":") {
			fieldLine = i
			break
		}
	}
	rendered := []string{"      " + field + ": []"}
	if len(values) > 0 {
		rendered = []string{"      " + field + ":"}
		for _, value := range values {
			rendered = append(rendered, "        - "+value)
		}
	}
	if fieldLine >= 0 {
		j := fieldLine + 1
		for j < parentEnd {
			// 空行属于文件格式，不并入字段块，避免生成时吞掉用户保留的分隔行。
			if strings.TrimSpace(lines[j]) == "" {
				break
			}
			indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
			if indent <= 6 {
				break
			}
			j++
		}
		if sameLines(lines[fieldLine:j], rendered) {
			return false, nil
		}
		lines = append(lines[:fieldLine], append(rendered, lines[j:]...)...)
	} else {
		lines = append(lines[:parentEnd], append(rendered, lines[parentEnd:]...)...)
	}
	out := strings.Join(lines, "\n") + "\n"
	if out == string(b) {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(out), 0o644)
}

// syncItemFieldBlock 在 listKey 列表中找到 itemKey=itemValue 的块，
// 以块风格替换或插入字段（每项一行），用于 transitions[].implemented_by。
func syncItemFieldBlock(path, listKey, itemKey, itemValue, field string, values []string) (bool, error) {
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
		if newLine, matched, err := syncFlowItemLine(lines[i], itemKey, itemValue, field, values); matched {
			if err != nil {
				return false, err
			}
			if newLine == lines[i] {
				return false, nil
			}
			lines[i] = newLine
			out := strings.Join(lines, "\n") + "\n"
			if out == string(b) {
				return false, nil
			}
			return true, os.WriteFile(path, []byte(out), 0o644)
		}
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
	rendered := []string{"    " + field + ":"}
	if len(values) == 0 {
		rendered = []string{"    " + field + ": []"}
	} else {
		for _, value := range values {
			rendered = append(rendered, "      - "+value)
		}
	}
	if field == "used_by" && itemEnd < len(lines) {
		rendered = append(rendered, "")
	}
	fieldLine := -1
	for i := itemStart + 1; i < itemEnd; i++ {
		if strings.HasPrefix(lines[i], "    "+field+":") {
			fieldLine = i
			break
		}
	}
	if fieldLine >= 0 {
		j := fieldLine + 1
		for j < itemEnd {
			if strings.TrimSpace(lines[j]) == "" {
				j++
				continue
			}
			indent := len(lines[j]) - len(strings.TrimLeft(lines[j], " "))
			if indent <= 4 {
				break
			}
			j++
		}
		if sameLines(lines[fieldLine:j], rendered) {
			return false, nil
		}
		lines = append(lines[:fieldLine], append(rendered, lines[j:]...)...)
	} else {
		lines = append(lines[:itemEnd], append(rendered, lines[itemEnd:]...)...)
	}
	out := strings.Join(lines, "\n") + "\n"
	if out == string(b) {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(out), 0o644)
}

// syncFlowItemLine 处理单行 flow mapping 列表项：- { name: x, used_by: [] }。
// 找到 itemKey=itemValue 的项后，替换或插入 field；保留行内其他内容与缩进。
func syncFlowItemLine(line, itemKey, itemValue, field string, values []string) (string, bool, error) {
	open := strings.Index(line, "{")
	if open < 0 || !strings.HasPrefix(strings.TrimSpace(line), "- {") {
		return line, false, nil
	}
	close := flowClose(line, open)
	if close < 0 {
		return line, false, nil
	}
	body := line[open+1 : close]
	entries := flowEntries(body)
	matched := false
	for _, e := range entries {
		if e.key == itemKey && flowScalar(e.value) == itemValue {
			matched = true
			break
		}
	}
	if !matched {
		return line, false, nil
	}
	rendered := renderFlowList(values)
	for _, e := range entries {
		if e.key == field {
			newBody := body[:e.valueStart] + rendered + body[e.valueEnd:]
			return line[:open+1] + newBody + line[close:], true, nil
		}
	}
	trimmed := strings.TrimRight(body, " ")
	trailing := body[len(trimmed):]
	insert := field + ": " + rendered
	if strings.TrimSpace(trimmed) != "" {
		if strings.HasSuffix(strings.TrimSpace(trimmed), ",") {
			insert = " " + insert
		} else {
			insert = ", " + insert
		}
	}
	return line[:open+1] + trimmed + insert + trailing + line[close:], true, nil
}

// flowClose 返回与 open 处 { 匹配的 } 下标；支持嵌套与引号。
func flowClose(s string, open int) int {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if quote == '"' && c == '\\' {
				i++
				continue
			}
			if c == quote {
				if quote == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

type flowEntry struct {
	start, end           int
	valueStart, valueEnd int
	key, value           string
}

// flowEntries 按顶层逗号切分 flow mapping 内容，忽略嵌套 {} / [] 与引号内的逗号。
func flowEntries(body string) []flowEntry {
	var out []flowEntry
	start := 0
	depth := 0
	var quote byte
	for i := 0; i < len(body); i++ {
		c := body[i]
		if quote != 0 {
			if quote == '"' && c == '\\' {
				i++
				continue
			}
			if c == quote {
				if quote == '\'' && i+1 < len(body) && body[i+1] == '\'' {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '{', '[':
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if e, ok := parseFlowEntry(body, start, i); ok {
					out = append(out, e)
				}
				start = i + 1
			}
		}
	}
	if e, ok := parseFlowEntry(body, start, len(body)); ok {
		out = append(out, e)
	}
	return out
}

func parseFlowEntry(body string, start, end int) (flowEntry, bool) {
	depth := 0
	var quote byte
	colon := -1
	for i := start; i < end; i++ {
		c := body[i]
		if quote != 0 {
			if quote == '"' && c == '\\' {
				i++
				continue
			}
			if c == quote {
				if quote == '\'' && i+1 < end && body[i+1] == '\'' {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '{', '[':
			depth++
		case '}', ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				colon = i
				i = end
			}
		}
	}
	if colon < 0 {
		return flowEntry{}, false
	}
	key := strings.TrimSpace(body[start:colon])
	if key == "" {
		return flowEntry{}, false
	}
	valueStart := colon + 1
	for valueStart < end && (body[valueStart] == ' ' || body[valueStart] == '\t') {
		valueStart++
	}
	valueEnd := end
	for valueEnd > valueStart && (body[valueEnd-1] == ' ' || body[valueEnd-1] == '\t') {
		valueEnd--
	}
	return flowEntry{start: start, end: end, valueStart: valueStart, valueEnd: valueEnd, key: key, value: body[valueStart:valueEnd]}, true
}

func flowScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func sameLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func renderFlowList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	return "[ " + strings.Join(values, ", ") + " ]"
}
