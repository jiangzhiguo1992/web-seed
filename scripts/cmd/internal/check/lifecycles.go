// lifecycles.go 校验 cap.lifecycles 的状态维度与转换。
package check

import (
	"path/filepath"
	"sort"
	"strings"
)

// checkLifecycleEnumCoverage 校验被 lifecycle 使用的枚举值没有漏接：
// 每个枚举值至少被一个使用该枚举的 lifecycle states.values 声明。
func (c *Checker) checkLifecycleEnumCoverage(enumStates, enumOwners map[string]map[string]bool) {
	enums := make([]string, 0, len(enumStates))
	for enum := range enumStates {
		enums = append(enums, enum)
	}
	sort.Strings(enums)
	for _, enum := range enums {
		values := make([]string, 0, len(c.enumValues[enum]))
		for value := range c.enumValues[enum] {
			values = append(values, value)
		}
		sort.Strings(values)
		owners := make([]string, 0, len(enumOwners[enum]))
		for fileID := range enumOwners[enum] {
			owners = append(owners, fileID)
		}
		sort.Strings(owners)
		for _, value := range values {
			if !enumStates[enum][value] {
				c.rulef("lifecycle-enum-coverage", "enum %s value %s is not declared by any lifecycle (%s)", enum, value, strings.Join(owners, ", "))
			}
		}
	}
}

// checkLifecycleRequiresCycles 校验 requires 图没有环。
func (c *Checker) checkLifecycleRequiresCycles(graph map[string][]string) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var visit func(string)
	visit = func(id string) {
		color[id] = gray
		stack = append(stack, id)
		for _, ref := range graph[id] {
			switch color[ref] {
			case gray:
				start := 0
				for i, node := range stack {
					if node == ref {
						start = i
						break
					}
				}
				cycle := append(append([]string{}, stack[start:]...), ref)
				c.rulef("lifecycle-requires", "requires cycle: %s", strings.Join(cycle, " -> "))
			case white:
				visit(ref)
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
	}
	ids := make([]string, 0, len(graph))
	for id := range graph {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if color[id] == white {
			visit(id)
		}
	}
}

// lifecycleSubjects 解析 subjects[].{entity, field}，并用实体字段补齐维度事实。
func (c *Checker) lifecycleSubjects(raw any) []lifecycleSubject {
	var out []lifecycleSubject
	for _, item := range asList(raw) {
		m, _ := asMap(item)
		entity, _ := m["entity"].(string)
		field, _ := m["field"].(string)
		subj := lifecycleSubject{Entity: entity, Field: field}
		if ent := c.entities[entity]; ent != nil {
			subj.Table = ent.Primary
			if f := findEntityFieldByJSONName(ent, field); f != nil {
				subj.ColumnRef = f.SourceColumnRef
				if enum := strings.TrimPrefix(f.Type, "*"); isEnumType(enum) {
					subj.Enum = enum
				}
			}
		}
		out = append(out, subj)
	}
	return out
}

// lifecycleStateValues 读取 states.values；兼容旧的字符串列表与新的 {value, steps} 对象列表。
func cloneStringSet(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range in {
		if v {
			out[k] = true
		}
	}
	return out
}

func lifecycleStateValues(raw any) []string {
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

// checkLifecycleStateSteps 对账 states.values[].steps：由 gen-graph 从 step.state_gate 反推。
// 字段缺失由 schema.required 报出；这里只比较已声明值，避免重复报错。
func (c *Checker) checkLifecycleStateSteps(rel, fileID string, rawValues any) {
	declared := map[string]map[string]bool{}
	present := map[string]bool{}
	for _, raw := range asList(rawValues) {
		item, ok := asMap(raw)
		if !ok {
			continue
		}
		state, _ := item["value"].(string)
		rawSteps, ok := item["steps"]
		if state == "" || !ok {
			continue
		}
		present[state] = true
		declared[state] = stringSet(asList(rawSteps))
	}
	states := make([]string, 0, len(present))
	for state := range present {
		states = append(states, state)
	}
	sort.Strings(states)
	for _, state := range states {
		want := c.lifecycleStateSteps[fileID][state]
		if want == nil {
			want = map[string]bool{}
		}
		if !sameSet(declared[state], want) {
			c.rulef("lifecycle-anchors", "%s: states.values[%s].steps must be %v, got %v", rel, state, sortedKeys(want), sortedKeys(declared[state]))
		}
	}
}

// findEntityFieldByJSONName 按 json_name（snake_case）查实体字段。
func (c *Checker) checkLifecycleDoc(doc map[string]any, rel string, schema *Schema, stepIDs map[string]bool, actionSteps map[string][]string, lifecycleStates map[string]map[string]bool) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	c.checkTargetFileName(schema, filepath.Base(rel), rel)
	c.checkStability(doc, rel)
	fileID, _ := doc["id"].(string)
	want := schema.Targets.FileID(filepath.Base(rel))
	if fileID != want {
		c.rulef("lifecycle-structure", "%s: id %q must be %q", rel, fileID, want)
	}

	ownerModule, _ := doc["owner_module"].(string)
	statusEnum := ""
	for _, subj := range c.lifecycleSubjects(doc["subjects"]) {
		ent := c.entities[subj.Entity]
		if ent == nil {
			c.rulef("lifecycle-subject", "%s: unknown subject %s", rel, subj.Entity)
			continue
		}
		if subj.Field == "" {
			c.rulef("lifecycle-subject", "%s: subject %s requires field", rel, subj.Entity)
			continue
		}
		f := findEntityFieldByJSONName(ent, subj.Field)
		if f == nil {
			c.rulef("lifecycle-subject", "%s: subject %s has no field %s", rel, subj.Entity, subj.Field)
			continue
		}
		if f.IsRelation {
			c.rulef("lifecycle-subject", "%s: subject %s field %s must be scalar, got relation", rel, subj.Entity, subj.Field)
			continue
		}
		typ := strings.TrimPrefix(f.Type, "*")
		if typ == "" {
			c.rulef("lifecycle-subject", "%s: subject %s field %s has no type", rel, subj.Entity, subj.Field)
			continue
		}
		if !strings.HasPrefix(typ, "data.enums.") {
			c.rulef("lifecycle-subject", "%s: subject %s field %s must be data.enums.*, got %s", rel, subj.Entity, subj.Field, typ)
			continue
		}
		if f.SourceColumnRef == "" {
			c.rulef("lifecycle-subject", "%s: subject %s field %s must source from a column", rel, subj.Entity, subj.Field)
			continue
		}
		if ownerModule != "" && ent.OwnerModule != ownerModule {
			c.rulef("lifecycle-subject", "%s: owner_module %q must equal subject %s owner_module %q", rel, ownerModule, subj.Entity, ent.OwnerModule)
		}
		if statusEnum == "" {
			statusEnum = typ
		} else if statusEnum != typ {
			c.rulef("lifecycle-subject", "%s: subjects must share one status enum, got %s and %s", rel, statusEnum, typ)
		}
	}
	enumValues := c.enumValues[statusEnum]
	if statusEnum != "" && !c.enumDeclared(statusEnum) {
		c.rulef("lifecycle-subject", "%s: subject enum %s does not exist", rel, statusEnum)
	}

	states, _ := asMap(doc["states"])
	stateList := lifecycleStateValues(states["values"])
	c.checkLifecycleStateSteps(rel, fileID, states["values"])
	stateNames := map[string]bool{}
	for _, name := range stateList {
		if stateNames[name] {
			c.rulef("lifecycle-structure", "%s: duplicate state %s", rel, name)
		}
		stateNames[name] = true
		if enumValues != nil && !enumValues[name] {
			c.rulef("lifecycle-subject", "%s: state %s is not a value of %s", rel, name, statusEnum)
		}
	}
	initial, _ := states["initial"].(string)
	if !stateNames[initial] {
		c.rulef("lifecycle-structure", "%s: initial %s is not a declared state", rel, initial)
	}
	finalSet := map[string]bool{}
	for _, name := range stringList(states["final"]) {
		if !stateNames[name] {
			c.rulef("lifecycle-structure", "%s: final %s is not a declared state", rel, name)
			continue
		}
		finalSet[name] = true
	}
	_, hasDestroy := asMap(doc["destroy"])
	if hasDestroy && len(finalSet) > 1 {
		c.rulef("lifecycle-structure", "%s: destroy requires at most one final state", rel)
	}

	checkStep := func(actionID, step string) {
		if step == "" {
			c.rulef("lifecycle-anchors", "%s: %s requires step", rel, actionID)
			return
		}
		if !stepIDs[step] {
			c.rulef("lifecycle-anchors", "%s: %s unknown step %s", rel, actionID, step)
			return
		}
		if !containsString(actionSteps[actionID], step) {
			c.rulef("lifecycle-anchors", "%s: %s step %s must declare lifecycles %s", rel, actionID, step, actionID)
		}
	}
	checkImplementedBy := func(actionID string, action map[string]any) {
		steps := stringList(action["implemented_by"])
		if len(steps) == 0 {
			return // 空值由 schema.generated-range 报出
		}
		got := map[string]bool{}
		for _, step := range steps {
			got[step] = true
		}
		wantSet := map[string]bool{}
		for _, step := range actionSteps[actionID] {
			wantSet[step] = true
		}
		if !sameSet(got, wantSet) {
			c.rulef("lifecycle-anchors", "%s: %s implemented_by %v must match step.lifecycles %v", rel, actionID, steps, sortedKeys(wantSet))
		}
	}
	checkRequires := func(actionID string, item map[string]any) {
		seen := map[string]bool{}
		for _, rawReq := range asList(item["requires"]) {
			rm, _ := asMap(rawReq)
			ref, _ := rm["lifecycle"].(string)
			states := stringList(rm["states"])
			if len(states) == 0 {
				c.rulef("lifecycle-requires", "%s: %s requires states", rel, actionID)
			}
			if ref == fileID {
				c.rulef("lifecycle-requires", "%s: %s requires itself", rel, actionID)
				continue
			}
			refStates, ok := lifecycleStates[ref]
			if !ok {
				c.rulef("lifecycle-requires", "%s: %s requires unknown lifecycle %s", rel, actionID, ref)
				continue
			}
			for _, state := range states {
				if !refStates[state] {
					c.rulef("lifecycle-requires", "%s: %s requires unknown state %s of %s", rel, actionID, state, ref)
				}
			}
			if code, _ := rm["on_violation"].(string); code == "" {
				c.rulef("lifecycle-requires", "%s: %s requires on_violation", rel, actionID)
			} else if errs := c.errorCodes(); len(errs) > 0 && !c.errorCodeDeclared(code) {
				c.rulef("lifecycle-requires", "%s: %s requires unknown error %s", rel, actionID, code)
			}
			if c.ruleIDSet != nil {
				for _, rawRule := range asList(rm["rules"]) {
					ruleID, _ := rawRule.(string)
					if ruleID != "" && !c.ruleIDSet[ruleID] {
						c.rulef("lifecycle-requires", "%s: %s requires unknown rule %s", rel, actionID, ruleID)
					}
				}
			}
			if seen[ref] {
				c.rulef("lifecycle-requires", "%s: %s repeats lifecycle %s in requires", rel, actionID, ref)
			}
			seen[ref] = true
		}
	}

	if create, hasCreate := asMap(doc["create"]); hasCreate {
		checkImplementedBy(fileID+".create", create)
		checkRequires(fileID+".create", create)
	}
	if destroy, ok := asMap(doc["destroy"]); ok {
		checkImplementedBy(fileID+".destroy", destroy)
		checkRequires(fileID+".destroy", destroy)
	}

	transitionNames := map[string]bool{}
	outgoing := map[string]bool{}
	for _, raw := range asList(doc["transitions"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		actionID := fileID + ".transitions." + name
		if transitionNames[name] {
			c.rulef("lifecycle-exhaustive", "%s: duplicate transition %s", rel, name)
		}
		transitionNames[name] = true
		for _, rawFrom := range asList(item["from"]) {
			s, _ := rawFrom.(string)
			if !stateNames[s] {
				c.rulef("lifecycle-structure", "%s: transition %s unknown from state %s", rel, name, s)
			}
			outgoing[s] = true
			if finalSet[s] {
				c.rulef("lifecycle-structure", "%s: final state %s must not be a transition source", rel, s)
			}
		}
		to, _ := item["to"].(string)
		if !stateNames[to] {
			c.rulef("lifecycle-structure", "%s: transition %s unknown to state %s", rel, name, to)
		}
		if hasDestroy && len(finalSet) == 1 && finalSet[to] {
			c.rulef("lifecycle-structure", "%s: transition %s must not target final %s; use destroy", rel, name, to)
		}
		checkRequires(actionID, item)
		impl := stringList(item["implemented_by"])
		if len(impl) == 0 {
			continue // 空值由 schema.generated-range 报出
		}
		for _, step := range impl {
			checkStep(actionID, step)
		}
		implSet := map[string]bool{}
		for _, step := range impl {
			implSet[step] = true
		}
		actionSet := map[string]bool{}
		for _, step := range actionSteps[actionID] {
			actionSet[step] = true
		}
		if !sameSet(implSet, actionSet) {
			c.rulef("lifecycle-anchors", "%s: transition %s implemented_by %v must match step.lifecycles %v", rel, name, impl, actionSteps[actionID])
		}
	}

	// 可达性：从 initial 沿 transition 边；有 destroy 且唯一 final 时，destroy 进入该 final。
	if stateNames[initial] {
		reachable := map[string]bool{initial: true}
		for changed := true; changed; {
			changed = false
			for _, raw := range asList(doc["transitions"]) {
				item, _ := asMap(raw)
				to, _ := item["to"].(string)
				if !stateNames[to] || reachable[to] {
					continue
				}
				for _, rawFrom := range asList(item["from"]) {
					from, _ := rawFrom.(string)
					if stateNames[from] && reachable[from] {
						reachable[to] = true
						changed = true
						break
					}
				}
			}
		}
		if hasDestroy && len(finalSet) == 1 {
			for state := range stateNames {
				if !finalSet[state] && reachable[state] {
					for name := range finalSet {
						reachable[name] = true
					}
					break
				}
			}
		}
		for _, state := range stateList {
			if !reachable[state] {
				c.rulef("lifecycle-exhaustive", "%s: state %s is not reachable from initial %s", rel, state, initial)
			}
		}
	}

	for _, state := range stateList {
		if finalSet[state] {
			continue
		}
		if !hasDestroy && !outgoing[state] {
			c.rulef("lifecycle-exhaustive", "%s: non-final state %s has no outgoing transition", rel, state)
		}
	}
}
