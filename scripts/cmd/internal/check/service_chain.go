// service_chain.go 校验 service 的参数、返回值、调用链与事务。
package check

import (
	"fmt"
	"sort"
	"strings"
)

// checkServiceParamExpr 校验 input 值：params.* 必须存在于 service params，
// step.<id>.* 必须存在于前序步骤输出。
func (c *Checker) checkServiceParamExpr(v any, doc map[string]any, stepIndex, allIndex map[string]int, reach []map[int]bool, stepOutputs []map[string]returnType, steps []any, rel, stepID string) {
	expr, ok := v.(string)
	if !ok || strings.TrimSpace(expr) == "" {
		c.rulef("service-input-format", "%s: step %s input value must be a non-empty expression", rel, stepID)
		return
	}
	switch {
	case strings.HasPrefix(expr, "params."):
		if err := c.checkInputPath(doc, expr); err != nil {
			c.rulef("service-input-ref", "%s: step %s input references %s: %v", rel, stepID, expr, err)
		}
	case strings.HasPrefix(expr, "step."):
		ref, path, ok := splitStepField(expr)
		if !ok {
			c.rulef("service-input-format", "%s: step %s input %q is not a valid step reference", rel, stepID, expr)
			return
		}
		idx, exists := stepIndex[ref]
		if !exists {
			c.rulef("service-input-order", "%s: step %s input references step %s that is not an earlier step", rel, stepID, ref)
			return
		}
		cur, hasCur := allIndex[stepID]
		if hasCur && idx >= cur {
			c.rulef("service-input-order", "%s: step %s input references step %s that is not an earlier step", rel, stepID, ref)
			return
		}
		if hasCur && idx < len(reach) && !reach[idx][cur] {
			c.rulef("service-chain", "%s: step %s input references step %s that is not reachable via goto", rel, stepID, ref)
			return
		}
		target, _ := asMap(steps[idx])
		if err := c.checkStepOutputPath(target, stepOutputs[idx], path, whenFormula{}, ref); err != nil {
			c.rulef("service-input-ref", "%s: step %s input references %s: %v", rel, stepID, expr, err)
		}
	default:
		c.rulef("service-input-format", "%s: step %s input value %q must be params.<path> or step.<id>.<path> (inline expressions are not allowed; declare a logic step and reference its output)", rel, stepID, expr)
	}
}

// buildStepEdges 按 goto/when 边构建步骤图；$end 记为虚拟出口的前驱。
// 控制流完全由显式 goto 声明，不存在隐式顺序边。
func buildStepEdges(steps []any, allIndex map[string]int) (adj [][]int, exitPreds []int) {
	n := len(steps)
	adj = make([][]int, n)
	for i, raw := range steps {
		step, _ := asMap(raw)
		addEdge := func(target string) {
			if target == "" {
				return
			}
			if target == "$end" {
				exitPreds = append(exitPreds, i)
				return
			}
			if idx, ok := allIndex[target]; ok && idx > i {
				adj[i] = append(adj[i], idx)
			}
		}
		target, _ := step["goto"].(string)
		addEdge(target)
		if kind, _ := step["kind"].(string); kind == "when" {
			for _, rawWhen := range asList(step["when"]) {
				wm, _ := asMap(rawWhen)
				branch, _ := wm["goto"].(string)
				addEdge(branch)
			}
		}
	}
	return adj, exitPreds
}

// buildStepReach 返回每个 step 可达的后续 step 集合。
func buildStepReach(steps []any, allIndex map[string]int) []map[int]bool {
	adj, _ := buildStepEdges(steps, allIndex)
	n := len(steps)
	reach := make([]map[int]bool, n)
	for i := 0; i < n; i++ {
		seen := map[int]bool{}
		queue := []int{i}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, next := range adj[cur] {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
		reach[i] = seen
	}
	return reach
}

// computeDominators 计算步骤图的必经点集合；exit = n 是虚拟出口。
// 不可达节点保持全 true，调用方先用 reach 过滤。
func computeDominators(n int, adj [][]int, exitPreds []int) []map[int]bool {
	total := n + 1
	preds := make([][]int, total)
	for i, nexts := range adj {
		for _, j := range nexts {
			preds[j] = append(preds[j], i)
		}
	}
	preds[n] = append(preds[n], exitPreds...)
	dom := make([]map[int]bool, total)
	for i := range dom {
		dom[i] = map[int]bool{}
		for j := 0; j < total; j++ {
			dom[i][j] = true
		}
	}
	dom[0] = map[int]bool{0: true}
	for changed := true; changed; {
		changed = false
		for v := 1; v < total; v++ {
			if len(preds[v]) == 0 {
				continue
			}
			inter := map[int]bool{}
			for k := range dom[preds[v][0]] {
				inter[k] = true
			}
			for _, p := range preds[v][1:] {
				for k := range inter {
					if !dom[p][k] {
						delete(inter, k)
					}
				}
			}
			inter[v] = true
			if !sameIntSet(inter, dom[v]) {
				dom[v] = inter
				changed = true
			}
		}
	}
	return dom
}

func sameIntSet(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// reachableAvoiding 判断 target 是否能从 start 到达且不经过 blocked 中任何节点。
func reachableAvoiding(adj [][]int, start, target int, blocked []int) bool {
	if start == target {
		return true
	}
	block := map[int]bool{}
	for _, b := range blocked {
		block[b] = true
	}
	seen := map[int]bool{start: true}
	queue := []int{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if block[next] || seen[next] {
				continue
			}
			if next == target {
				return true
			}
			seen[next] = true
			queue = append(queue, next)
		}
	}
	return false
}

// checkServiceMerge 校验分支汇聚：引用的前序 step 要么必然执行，要么构成互斥且完备的切面。
// returns.value 只要求来源能到达 service 终点，不要求支配所有出口：错误路径允许返回零值，
// 例如 (session, err) 在认证失败时 session 为空。
func (c *Checker) checkServiceMerge(rel string, steps []any, allIndex map[string]int, reach []map[int]bool, stepRefs []map[string]bool, exitRefs map[string]bool) {
	n := len(steps)
	stepID := func(i int) string {
		step, _ := asMap(steps[i])
		id, _ := step["id"].(string)
		return id
	}
	adj, exitPreds := buildStepEdges(steps, allIndex)
	dom := computeDominators(n, adj, exitPreds)
	for i := 0; i < n; i++ {
		if i > 0 && !reach[0][i] {
			continue // 不可达由 service-goto 报
		}
		var unsafe []int
		for ref := range stepRefs[i] {
			idx, ok := allIndex[ref]
			if !ok || idx >= i {
				continue // 前序/存在性由 service-params 报
			}
			if !reach[0][idx] {
				continue // 不可达由 service-goto 报
			}
			if !dom[i][idx] {
				unsafe = append(unsafe, idx)
			}
		}
		if len(unsafe) == 0 {
			continue
		}
		sort.Ints(unsafe)
		conflict := false
		for a := 0; a < len(unsafe) && !conflict; a++ {
			for b := a + 1; b < len(unsafe); b++ {
				if reach[unsafe[a]][unsafe[b]] || reach[unsafe[b]][unsafe[a]] {
					c.rulef("service-merge", "%s: step %s references %s and %s, but both can execute on the same path", rel, stepID(i), stepID(unsafe[a]), stepID(unsafe[b]))
					conflict = true
					break
				}
			}
		}
		if conflict {
			continue
		}
		if reachableAvoiding(adj, 0, i, unsafe) {
			names := make([]string, 0, len(unsafe))
			for _, idx := range unsafe {
				names = append(names, stepID(idx))
			}
			c.rulef("service-merge", "%s: step %s references %v but some path reaches it without executing any of them", rel, stepID(i), names)
		}
	}
	exitSet := map[int]bool{}
	for _, p := range exitPreds {
		exitSet[p] = true
	}
	for ref := range exitRefs {
		idx, ok := allIndex[ref]
		if !ok {
			continue
		}
		reachesExit := exitSet[idx]
		for p := range exitSet {
			if reachesExit {
				break
			}
			reachesExit = reach[idx][p]
		}
		if !reachesExit {
			c.rulef("service-merge", "%s: return source step %s cannot reach service end", rel, stepID(idx))
		}
	}
}

// inputExprRoot 返回 input.<root> 的根参数名。
func inputExprRoot(expr string) (string, bool) {
	if !strings.HasPrefix(expr, "params.") {
		return "", false
	}
	root := strings.TrimPrefix(expr, "params.")
	if i := strings.IndexByte(root, '.'); i >= 0 {
		root = root[:i]
	}
	if i := strings.IndexByte(root, '['); i >= 0 {
		root = root[:i]
	}
	if root == "" {
		return "", false
	}
	return root, true
}

// sameServiceType 比较返回类型，忽略指针与 List<> 包装差异。
func sameServiceType(a, b string) bool {
	a = normalizeContractType(a)
	b = normalizeContractType(b)
	a = strings.TrimPrefix(strings.TrimSpace(a), "*")
	b = strings.TrimPrefix(strings.TrimSpace(b), "*")
	if a == b {
		return true
	}
	if strings.HasPrefix(a, "List<") && strings.HasSuffix(a, ">") &&
		strings.HasPrefix(b, "List<") && strings.HasSuffix(b, ">") {
		return sameServiceType(a[5:len(a)-1], b[5:len(b)-1])
	}
	return false
}

// outputRoot 返回 step 输出路径的根字段名。
func outputRoot(path string) string {
	root := path
	if i := strings.IndexByte(root, '.'); i >= 0 {
		root = root[:i]
	}
	if i := strings.IndexByte(root, '['); i >= 0 {
		root = root[:i]
	}
	return root
}

// inputValueType 解析 input 值的静态类型：params.<path> 或 step.<id>.<path>。
func (c *Checker) inputValueType(value string, tc stepTypeContext) (string, error) {
	switch {
	case strings.HasPrefix(value, "params."):
		return c.inputPathType(tc.doc, value)
	case strings.HasPrefix(value, "step."):
		ref, path, ok := splitStepField(value)
		if !ok {
			return "", fmt.Errorf("invalid step reference")
		}
		idx, ok := tc.stepIndex[ref]
		if !ok {
			return "", fmt.Errorf("step %s is not an earlier step", ref)
		}
		target, _ := asMap(tc.steps[idx])
		return c.stepOutputPathType(target, tc.stepOutputs[idx], path, whenFormula{}, ref)
	default:
		return "", fmt.Errorf("value must be params.<path> or step.<id>.<path>")
	}
}

// inputValueNullable 解析 input 值的 nullable；params/step 的嵌套字段按 schema 递归。
func (c *Checker) inputValueNullable(value string, tc stepTypeContext) (nullable bool, known bool) {
	switch {
	case strings.HasPrefix(value, "params."):
		path, ok := splitInputField(value)
		if !ok {
			return false, false
		}
		segs, err := parseWhenPath(path)
		if err != nil || len(segs) == 0 {
			return false, false
		}
		root := segs[0].name
		for _, raw := range asList(tc.doc["params"]) {
			pm, _ := asMap(raw)
			if name, _ := pm["name"].(string); name != root {
				continue
			}
			typ, _ := pm["type"].(string)
			n, _ := pm["nullable"].(bool)
			return c.walkNullableType(typ, n || strings.HasPrefix(strings.TrimSpace(typ), "*"), segs[1:])
		}
		return false, false
	case strings.HasPrefix(value, "step."):
		ref, path, ok := splitStepField(value)
		if !ok {
			return false, false
		}
		idx, ok := tc.stepIndex[ref]
		if !ok {
			return false, false
		}
		segs, err := parseWhenPath(path)
		if err != nil || len(segs) == 0 {
			return false, false
		}
		rt, ok := tc.stepOutputs[idx][segs[0].name]
		if !ok {
			return false, false
		}
		return c.walkNullableType(rt.typ, rt.nullable || strings.HasPrefix(strings.TrimSpace(rt.typ), "*"), segs[1:])
	}
	return false, false
}

// walkNullableType 沿嵌套字段解析 nullable；列表元素或不可解析字段返回 known=false。
func (c *Checker) walkNullableType(typ string, curNullable bool, segs []whenPathSegment) (bool, bool) {
	for _, seg := range segs {
		base := strings.TrimPrefix(strings.TrimSpace(typ), "*")
		if strings.HasPrefix(base, "List<") {
			return false, false
		}
		nullable, ok := c.resolveTypeFieldNullable(base, seg.name)
		if !ok {
			return false, false
		}
		curNullable = nullable
		next, ok := c.resolveTypeField(base, seg.name, whenFormula{}, "", "")
		if !ok {
			return false, false
		}
		typ = next
		if seg.hasIndex {
			return false, false
		}
	}
	return curNullable, true
}

// resolveTypeFieldNullable 返回 entity/local schema 字段的 nullable；未知类型不判定。
func (c *Checker) resolveTypeFieldNullable(typ, name string) (bool, bool) {
	if resolved, ok := resolveTypeRef(typ); ok {
		typ = resolved
	}
	typ = strings.TrimPrefix(strings.TrimSpace(typ), "*")
	if entity := c.entities[typ]; entity != nil {
		if f := findEntityField(entity, name); f != nil {
			return strings.HasPrefix(strings.TrimSpace(f.Type), "*"), true
		}
		for i := range entity.Fields {
			if entity.Fields[i].JSONName == name {
				return strings.HasPrefix(strings.TrimSpace(entity.Fields[i].Type), "*"), true
			}
		}
		return false, false
	}
	if schema := c.methodTypeDefs[typ]; schema != nil {
		for _, raw := range asList(schema["properties"]) {
			prop, _ := asMap(raw)
			if pn, _ := prop["name"].(string); pn != name {
				continue
			}
			nullable, _ := prop["nullable"].(bool)
			return nullable, true
		}
	}
	return false, false
}

// parseTypeShape 拆掉指针与单层 List<>// parseTypeShape 拆掉指针与单层 List<>；指针只表达可空，不参与赋值兼容判断。
func parseTypeShape(typ string) (typeShape, bool) {
	base := normalizeContractType(typ)
	for strings.HasPrefix(base, "*") {
		base = strings.TrimSpace(strings.TrimPrefix(base, "*"))
	}
	array := false
	if strings.HasPrefix(base, "List<") {
		if !strings.HasSuffix(base, ">") {
			return typeShape{}, false
		}
		base = strings.TrimSpace(base[len("List<") : len(base)-1])
		array = true
	}
	if base == "" || strings.ContainsAny(base, "<>") {
		return typeShape{}, false
	}
	return typeShape{base: base, array: array}, true
}

// serviceTypeFamily 把类型名归一到可比较的类型族；枚举按 integer 持久化。
func (c *Checker) serviceTypeFamily(base string) (string, bool) {
	if strings.HasPrefix(base, "data.enums.") {
		return "integer", true
	}
	if fam := c.valueType(base); fam != "" {
		return fam, true
	}
	return paramFamily(base)
}

// serviceTypesCompatible 判断 input 值类型能否赋给目标参数类型。
// 无法解析的类型不做断言，避免与路径错误重复报错。
func (c *Checker) serviceTypesCompatible(src, dst string) bool {
	s, ok := parseTypeShape(src)
	if !ok {
		return true
	}
	d, ok := parseTypeShape(dst)
	if !ok {
		return true
	}
	if s.base == "any" || d.base == "any" {
		return true
	}
	if s.array != d.array {
		return false
	}
	if s.base == d.base {
		return true
	}
	// 枚举值可以按 integer 持久化，但任意 integer 不能反喂枚举参数。
	if strings.HasPrefix(d.base, "data.enums.") {
		return false
	}
	sf, sok := c.serviceTypeFamily(s.base)
	df, dok := c.serviceTypeFamily(d.base)
	return sok && dok && sf == df
}

// computeServiceWriteClosure 在 service 调用图上传播写表能力（直接写或经调用间接写）。
// 图可能有环；用不动点迭代，不依赖拓扑序。
func computeServiceWriteClosure(direct map[string]bool, edges map[string][]string) map[string]bool {
	writes := map[string]bool{}
	for id, w := range direct {
		if w {
			writes[id] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for id, refs := range edges {
			if writes[id] {
				continue
			}
			for _, ref := range refs {
				if writes[ref] {
					writes[id] = true
					changed = true
					break
				}
			}
		}
	}
	return writes
}

// checkQueryInput 校验 query 的 input：key 必须是参数名，必填参数必须出现，值类型必须兼容。
func (c *Checker) checkQueryInput(input map[string]any, q *queryInfo, qid, rel, stepID string, tc stepTypeContext) {
	params := map[string]bool{}
	required := map[string]bool{}
	byName := map[string]queryParam{}
	for _, p := range q.Params {
		if p.Name == "ctx" {
			continue
		}
		params[p.Name] = true
		byName[p.Name] = p
		if !p.Nullable && !p.HasDefault {
			required[p.Name] = true
		}
	}
	for name, raw := range input {
		if name == "ctx" {
			c.rulef("service-input-name", "%s: step %s query %s must not pass ctx", rel, stepID, qid)
			continue
		}
		if !params[name] {
			c.rulef("service-input-name", "%s: step %s query %s has no param %s", rel, stepID, qid, name)
			continue
		}
		value, _ := raw.(string)
		src, err := c.inputValueType(value, tc)
		if err != nil {
			continue // 路径错误由 checkServiceParamExpr 报出
		}
		p := byName[name]
		if p.Type != "" && !c.serviceTypesCompatible(src, p.Type) {
			c.rulef("service-input-type", "%s: step %s query %s input %s type %s is not compatible with %s", rel, stepID, qid, name, src, p.Type)
		}
		if nullable, known := c.inputValueNullable(value, tc); known && nullable && !p.Nullable {
			c.rulef("service-input-nullable", "%s: step %s query %s input %s source is nullable but target param is not", rel, stepID, qid, name)
		}
	}
	for name := range required {
		if _, ok := input[name]; !ok {
			c.rulef("service-input-required", "%s: step %s query %s missing required param %s", rel, stepID, qid, name)
		}
	}
}

// checkServiceParamDefault 校验 params.default 的放置与可判定的标量类型。
func (c *Checker) checkServiceParamDefault(pm map[string]any, rel, svcName string) {
	def, has := pm["default"]
	if !has {
		return
	}
	name, _ := pm["name"].(string)
	where := fmt.Sprintf("%s.%s: param %s", rel, svcName, name)
	if name == "ctx" {
		c.rulef("service-param-default", "%s: ctx must not declare default", where)
		return
	}
	if nullable, _ := pm["nullable"].(bool); !nullable {
		c.rulef("service-param-default", "%s: default requires nullable:true (omitted means use default)", where)
	}
	typ, _ := pm["type"].(string)
	base := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(typ), "*"))
	for _, msg := range c.paramDefaultErrors(base, def, where) {
		c.rulef("service-param-default", "%s", msg)
	}
}

// checkServiceInput 校验 service 调用的 input：key 必须是目标参数名，必填参数必须出现，值类型必须兼容。
func (c *Checker) checkServiceInput(input map[string]any, target serviceMeta, ref, rel, stepID string, tc stepTypeContext) {
	for name, raw := range input {
		if name == "ctx" {
			c.rulef("service-input-name", "%s: step %s service %s must not pass ctx", rel, stepID, ref)
			continue
		}
		if !target.params[name] {
			c.rulef("service-input-name", "%s: step %s service %s has no param %s", rel, stepID, ref, name)
			continue
		}
		value, _ := raw.(string)
		src, err := c.inputValueType(value, tc)
		if err != nil {
			continue
		}
		if dst := target.paramDecls[name]; dst != "" && !c.serviceTypesCompatible(src, dst) {
			c.rulef("service-input-type", "%s: step %s service %s input %s type %s is not compatible with %s", rel, stepID, ref, name, src, dst)
		}
		if target.paramNullable != nil {
			if nullable, known := c.inputValueNullable(value, tc); known && nullable && !target.paramNullable[name] {
				c.rulef("service-input-nullable", "%s: step %s service %s input %s source is nullable but target param is not", rel, stepID, ref, name)
			}
		}
	}
	for name := range target.requiredParams {
		if _, ok := input[name]; !ok {
			c.rulef("service-input-required", "%s: step %s service %s missing required param %s", rel, stepID, ref, name)
		}
	}
}

// checkServiceCycles 在 kind=service 调用图上做 DFS 环检测。
func canonicalCycle(cycle []string) string {
	nodes := append([]string{}, cycle[:len(cycle)-1]...)
	sort.Strings(nodes)
	return strings.Join(nodes, "|")
}

func indexOf(values []string, value string) int {
	for i, v := range values {
		if v == value {
			return i
		}
	}
	return 0
}

func (c *Checker) checkServiceCycles(edges map[string][]string) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	seen := map[string]bool{}
	var stack []string
	var visit func(string)
	visit = func(n string) {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range edges[n] {
			switch color[m] {
			case gray:
				idx := indexOf(stack, m)
				cycle := append(append([]string{}, stack[idx:]...), m)
				key := canonicalCycle(cycle)
				if !seen[key] {
					seen[key] = true
					c.rulef("service-cycle", "service call cycle: %s", strings.Join(cycle, " -> "))
				}
			case white:
				visit(m)
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}
	for n := range edges {
		if color[n] == white {
			visit(n)
		}
	}
}
