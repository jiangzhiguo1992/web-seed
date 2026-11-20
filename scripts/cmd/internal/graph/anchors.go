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

	RuleFiles      map[string][]string
	LifecycleFiles map[string][]lifecycleFileAction
	EventFiles     map[string][]string
}

func newAnchors() *Anchors {
	return &Anchors{
		Rules:          map[string][]string{},
		LifecycleSteps: map[string][]string{},
		Events:         map[string][]string{},
		ServiceUsedBy:  map[string][]string{},
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
					svc, _ := op["x-service"].(string)
					cid, _ := op["x-contract-id"].(string)
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
		}
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
			if strings.TrimSpace(lines[j]) == "" {
				j++
				continue
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
	if field == "used_by" {
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
