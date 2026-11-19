// lifecycles.go 校验 cap.lifecycles 的状态维度与转换。
package check

import (
	"path/filepath"
	"sort"
	"strings"
)

// lifecycleSubject 是一个状态维度：实体上的某个状态字段。
type lifecycleSubject struct {
	Entity    string
	Field     string
	Table     string
	ColumnRef string
	Enum      string
}

type lifecycleAction struct {
	kind     string
	to       string
	initial  string
	subjects []lifecycleSubject
}

// checkLifecycleEnumCoverage 校验被 lifecycle 使用的枚举值没有漏接：
// 每个枚举值至少被一个使用该枚举的 lifecycle states 声明。
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

// findEntityFieldByJSONName 按 json_name（snake_case）查实体字段。
func findEntityFieldByJSONName(ent *entityInfo, jsonName string) *entityFieldInfo {
	if ent == nil || jsonName == "" {
		return nil
	}
	for i := range ent.Fields {
		if ent.Fields[i].JSONName == jsonName {
			return &ent.Fields[i]
		}
	}
	return nil
}

func (c *Checker) checkLifecycleDoc(doc map[string]any, rel string, schema *Schema, stepIDs map[string]bool, actionSteps map[string][]string, lifecycleStates map[string]map[string]bool, ruleIDs map[string]bool) {
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
	stateNames := map[string]bool{}
	for _, raw := range asList(doc["states"]) {
		name, _ := raw.(string)
		if name == "" {
			continue
		}
		if stateNames[name] {
			c.rulef("lifecycle-structure", "%s: duplicate state %s", rel, name)
		}
		stateNames[name] = true
		if enumValues != nil && !enumValues[name] {
			c.rulef("lifecycle-subject", "%s: state %s is not a value of %s", rel, name, statusEnum)
		}
	}
	initial, _ := doc["initial"].(string)
	if !stateNames[initial] {
		c.rulef("lifecycle-structure", "%s: initial %s is not a declared state", rel, initial)
	}
	final, _ := doc["final"].(string)
	if final != "" && !stateNames[final] {
		c.rulef("lifecycle-structure", "%s: final %s is not a declared state", rel, final)
	}

	checkStep := func(actionID, step string) {
		if step == "" {
			c.rulef("lifecycle-steps", "%s: %s requires step", rel, actionID)
			return
		}
		if !stepIDs[step] {
			c.rulef("lifecycle-steps", "%s: %s unknown step %s", rel, actionID, step)
			return
		}
		if !containsString(actionSteps[actionID], step) {
			c.rulef("lifecycle-steps", "%s: %s step %s must declare lifecycles %s", rel, actionID, step, actionID)
		}
	}
	checkSteps := func(actionID string, steps []string) {
		if len(steps) == 0 {
			c.rulef("lifecycle-steps", "%s: %s requires steps", rel, actionID)
			return
		}
		set := map[string]bool{}
		for _, step := range steps {
			set[step] = true
			checkStep(actionID, step)
		}
		actionSet := map[string]bool{}
		for _, step := range actionSteps[actionID] {
			actionSet[step] = true
		}
		if !sameSet(set, actionSet) {
			c.rulef("lifecycle-steps", "%s: %s steps %v must match step.lifecycles %v", rel, actionID, steps, actionSteps[actionID])
		}
	}

	create, hasCreate := asMap(doc["create"])
	if hasCreate {
		checkSteps(fileID+".create", stringList(create["steps"]))
	}
	destroy, hasDestroy := asMap(doc["destroy"])
	if hasDestroy {
		to, _ := destroy["to"].(string)
		checkSteps(fileID+".destroy", stringList(destroy["steps"]))
		for _, rawFrom := range asList(destroy["from"]) {
			from, _ := rawFrom.(string)
			if !stateNames[from] {
				c.rulef("lifecycle-structure", "%s: destroy unknown from state %s", rel, from)
			}
			if final != "" && from == final {
				c.rulef("lifecycle-exhaustive", "%s: final state %s must not be a destroy source", rel, final)
			}
		}
		if final != "" {
			if to != final {
				c.rulef("lifecycle-structure", "%s: destroy.to %q must equal final %q", rel, to, final)
			}
		} else if to != "" {
			c.rulef("lifecycle-structure", "%s: destroy.to requires final", rel)
		}
	}
	transitionNames := map[string]bool{}
	outgoing := map[string]bool{}
	for _, raw := range asList(doc["transitions"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		actionID := fileID + "." + name
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
			if final != "" && s == final {
				c.rulef("lifecycle-exhaustive", "%s: final state %s must not be a transition source", rel, final)
			}
		}
		to, _ := item["to"].(string)
		if !stateNames[to] {
			c.rulef("lifecycle-structure", "%s: transition %s unknown to state %s", rel, name, to)
		}
		if final != "" && to == final {
			c.rulef("lifecycle-exhaustive", "%s: transition %s must not target final %s; use destroy", rel, name, final)
		}
		if code, _ := item["on_violation"].(string); code != "" && !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
			c.rulef("lifecycle-structure", "%s: transition %s unknown on_violation %s", rel, name, code)
		}
		guard, _ := asMap(item["guard"])
		if guard == nil {
			if _, ok := item["guard"].(bool); !ok {
				c.rulef("lifecycle-structure", "%s: transition %s guard must be true or { rule: feature.rules.* }", rel, name)
			}
		} else if rule, _ := guard["rule"].(string); !ruleIDs[rule] {
			c.rulef("lifecycle-structure", "%s: transition %s unknown guard rule %s", rel, name, rule)
		}
		seenRequires := map[string]bool{}
		for _, rawReq := range asList(item["requires"]) {
			rm, _ := asMap(rawReq)
			ref, _ := rm["lifecycle"].(string)
			state, _ := rm["state"].(string)
			if ref == fileID {
				c.rulef("lifecycle-requires", "%s: transition %s requires itself", rel, name)
				continue
			}
			refStates, ok := lifecycleStates[ref]
			if !ok {
				c.rulef("lifecycle-requires", "%s: transition %s requires unknown lifecycle %s", rel, name, ref)
				continue
			}
			if !refStates[state] {
				c.rulef("lifecycle-requires", "%s: transition %s requires unknown state %s of %s", rel, name, state, ref)
			}
			if seenRequires[ref] {
				c.rulef("lifecycle-requires", "%s: transition %s repeats lifecycle %s in requires", rel, name, ref)
			}
			seenRequires[ref] = true
		}
		impl := stringList(item["implemented_by"])
		if len(impl) == 0 {
			c.rulef("lifecycle-steps", "%s: transition %s requires implemented_by", rel, name)
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
			c.rulef("lifecycle-steps", "%s: transition %s implemented_by %v must match step.lifecycles %v", rel, name, impl, actionSteps[actionID])
		}
	}
	for _, rawFrom := range asList(destroy["from"]) {
		from, _ := rawFrom.(string)
		outgoing[from] = true
	}
	for state := range stateNames {
		if state == final {
			if outgoing[state] {
				c.rulef("lifecycle-exhaustive", "%s: final state %s must not have outgoing transitions", rel, state)
			}
			continue
		}
		if !outgoing[state] {
			c.rulef("lifecycle-exhaustive", "%s: non-final state %s has no outgoing transition", rel, state)
		}
	}
	for _, raw := range asList(doc["forbidden_examples"]) {
		item, _ := asMap(raw)
		from, _ := item["from"].(string)
		tr, _ := item["transition"].(string)
		if !stateNames[from] {
			c.rulef("lifecycle-structure", "%s: forbidden example unknown from state %s", rel, from)
		}
		if !transitionNames[tr] {
			c.rulef("lifecycle-structure", "%s: forbidden example unknown transition %s", rel, tr)
			continue
		}
		allowed := false
		for _, rawT := range asList(doc["transitions"]) {
			t, _ := asMap(rawT)
			if n, _ := t["name"].(string); n != tr {
				continue
			}
			for _, rawFrom := range asList(t["from"]) {
				if s, _ := rawFrom.(string); s == from {
					allowed = true
				}
			}
		}
		if allowed {
			c.rulef("lifecycle-structure", "%s: forbidden example %s from %s is actually allowed", rel, tr, from)
		}
	}
	for _, raw := range asList(doc["idempotent_transitions"]) {
		name, _ := raw.(string)
		if !transitionNames[name] {
			c.rulef("lifecycle-structure", "%s: idempotent transition %s does not exist", rel, name)
		}
	}
}
