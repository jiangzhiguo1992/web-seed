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
		c.rulef("service-params", "%s: step %s input value must be a non-empty expression", rel, stepID)
		return
	}
	switch {
	case strings.HasPrefix(expr, "params."):
		if err := c.checkInputPath(doc, expr); err != nil {
			c.rulef("service-params", "%s: step %s input references %s: %v", rel, stepID, expr, err)
		}
	case strings.HasPrefix(expr, "step."):
		ref, path, ok := splitStepField(expr)
		if !ok {
			c.rulef("service-params", "%s: step %s input %q is not a valid step reference", rel, stepID, expr)
			return
		}
		idx, exists := stepIndex[ref]
		if !exists {
			c.rulef("service-params", "%s: step %s input references step %s that is not an earlier step", rel, stepID, ref)
			return
		}
		cur, hasCur := allIndex[stepID]
		if hasCur && idx >= cur {
			c.rulef("service-params", "%s: step %s input references step %s that is not an earlier step", rel, stepID, ref)
			return
		}
		if hasCur && idx < len(reach) && !reach[idx][cur] {
			c.rulef("service-chain", "%s: step %s input references step %s that is not reachable via goto", rel, stepID, ref)
			return
		}
		target, _ := asMap(steps[idx])
		if err := c.checkStepOutputPath(target, stepOutputs[idx], path, whenFormula{}, ref); err != nil {
			c.rulef("service-params", "%s: step %s input references %s: %v", rel, stepID, expr, err)
		}
	default:
		c.rulef("service-params", "%s: step %s input value %q must be params.<path> or step.<id>.<path> (inline expressions are not allowed; declare a logic step and reference its output)", rel, stepID, expr)
	}
}

// buildStepReach 按 goto/when 边构建步骤图，返回每个 step 可达的后续 step 集合。
// 控制流完全由显式 goto 声明，不存在隐式顺序边。
func buildStepReach(steps []any, allIndex map[string]int) []map[int]bool {
	n := len(steps)
	adj := make([][]int, n)
	for i, raw := range steps {
		step, _ := asMap(raw)
		addEdge := func(target string) {
			if target == "" || target == "$end" {
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

// inputValueNullable 解析 input 值根字段的 nullable；嵌套字段返回 known=false。
func (c *Checker) inputValueNullable(value string, tc stepTypeContext) (nullable bool, known bool) {
	switch {
	case strings.HasPrefix(value, "params."):
		path, ok := splitInputField(value)
		if !ok || strings.Contains(path, ".") || strings.Contains(path, "[") {
			return false, false
		}
		for _, raw := range asList(tc.doc["params"]) {
			pm, _ := asMap(raw)
			if name, _ := pm["name"].(string); name != path {
				continue
			}
			typ, _ := pm["type"].(string)
			n, _ := pm["nullable"].(bool)
			return n || strings.HasPrefix(strings.TrimSpace(typ), "*"), true
		}
		return false, false
	case strings.HasPrefix(value, "step."):
		ref, path, ok := splitStepField(value)
		if !ok || strings.Contains(path, ".") || strings.Contains(path, "[") {
			return false, false
		}
		idx, ok := tc.stepIndex[ref]
		if !ok {
			return false, false
		}
		rt, ok := tc.stepOutputs[idx][path]
		if !ok {
			return false, false
		}
		return rt.nullable || strings.HasPrefix(strings.TrimSpace(rt.typ), "*"), true
	}
	return false, false
}

// parseTypeShape 拆掉指针与单层 List<>；指针只表达可空，不参与赋值兼容判断。
func parseTypeShape(typ string) (typeShape, bool) {
	base := strings.TrimSpace(typ)
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
			c.rulef("service-params", "%s: step %s query %s must not pass ctx", rel, stepID, qid)
			continue
		}
		if !params[name] {
			c.rulef("service-params", "%s: step %s query %s has no param %s", rel, stepID, qid, name)
			continue
		}
		value, _ := raw.(string)
		src, err := c.inputValueType(value, tc)
		if err != nil {
			continue // 路径错误由 checkServiceParamExpr 报出
		}
		p := byName[name]
		if p.Type != "" && !c.serviceTypesCompatible(src, p.Type) {
			c.rulef("service-params", "%s: step %s query %s input %s type %s is not compatible with %s", rel, stepID, qid, name, src, p.Type)
		}
	}
	for name := range required {
		if _, ok := input[name]; !ok {
			c.rulef("service-params", "%s: step %s query %s missing required param %s", rel, stepID, qid, name)
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
		c.rulef("service-params", "%s: ctx must not declare default", where)
		return
	}
	if nullable, _ := pm["nullable"].(bool); !nullable {
		c.rulef("service-params", "%s: default requires nullable:true (omitted means use default)", where)
	}
	typ, _ := pm["type"].(string)
	base := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(typ), "*"))
	for _, msg := range c.paramDefaultErrors(base, def, where) {
		c.rulef("service-params", "%s", msg)
	}
}

// checkServiceInput 校验 service 调用的 input：key 必须是目标参数名，必填参数必须出现，值类型必须兼容。
func (c *Checker) checkServiceInput(input map[string]any, target serviceMeta, ref, rel, stepID string, tc stepTypeContext) {
	for name, raw := range input {
		if name == "ctx" {
			c.rulef("service-params", "%s: step %s service %s must not pass ctx", rel, stepID, ref)
			continue
		}
		if !target.params[name] {
			c.rulef("service-params", "%s: step %s service %s has no param %s", rel, stepID, ref, name)
			continue
		}
		value, _ := raw.(string)
		src, err := c.inputValueType(value, tc)
		if err != nil {
			continue
		}
		if dst := target.paramDecls[name]; dst != "" && !c.serviceTypesCompatible(src, dst) {
			c.rulef("service-params", "%s: step %s service %s input %s type %s is not compatible with %s", rel, stepID, ref, name, src, dst)
		}
	}
	for name := range target.requiredParams {
		if _, ok := input[name]; !ok {
			c.rulef("service-params", "%s: step %s service %s missing required param %s", rel, stepID, ref, name)
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
