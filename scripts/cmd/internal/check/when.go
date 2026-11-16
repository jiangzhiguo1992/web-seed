package check

import (
	"fmt"
	"strconv"
	"strings"
)

// whenValue 是 when 表达式的三值求值结果：false / true / 未知（依赖运行时输入）。
type whenValue int

const (
	whenFalse whenValue = iota
	whenTrue
	whenUnknown
)

type whenTokenKind int

const (
	whenIdent whenTokenKind = iota
	whenString
	whenNumber
	whenOp
	whenLParen
	whenRParen
	whenAnd
	whenOr
	whenNot
)

type whenToken struct {
	kind whenTokenKind
	text string
}

// whenNode 是 when 表达式的 AST 节点。
type whenNode struct {
	kind  string // literal | field | compare | not | and | or
	value any
	op    string
	left  *whenNode
	right *whenNode
}

// evalWhen 解析并静态求值 when 表达式。
// 语法：or := and ('||' and)*；and := unary ('&&' unary)*；
// unary := '!' unary | primary；primary := 'true' | 'false' | '(' or ')' |
// operand op operand；operand := input.<path> | step.<id>.<path> | 字符串 | 数字 | true | false | null。
// 比较操作符：== != > >= < <=。
func evalWhen(expr string) (whenValue, error) {
	n, err := parseWhen(expr)
	if err != nil {
		return whenUnknown, err
	}
	return evalWhenNode(n), nil
}

func parseWhen(expr string) (*whenNode, error) {
	toks, err := tokenizeWhen(expr)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, fmt.Errorf("empty expression")
	}
	p := &whenParser{tokens: toks}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.tokens) {
		return nil, fmt.Errorf("unexpected token %q", p.tokens[p.pos].text)
	}
	return n, nil
}

// whenStepFields 返回表达式引用的完整 step 字段路径，例如 step.load.account.status。
func whenStepFields(n *whenNode) []string {
	var fields []string
	var walk func(*whenNode)
	walk = func(node *whenNode) {
		if node == nil {
			return
		}
		if node.kind == "field" {
			if path, _ := node.value.(string); strings.HasPrefix(path, "step.") {
				fields = append(fields, path)
			}
		}
		walk(node.left)
		walk(node.right)
	}
	walk(n)
	return fields
}

// whenStepRefs 返回表达式引用的 step ID，用于校验引用是前序步骤。
func whenStepRefs(n *whenNode) []string {
	seen := map[string]bool{}
	var refs []string
	for _, field := range whenStepFields(n) {
		parts := strings.Split(field, ".")
		if len(parts) >= 2 && parts[1] != "" && !seen[parts[1]] {
			seen[parts[1]] = true
			refs = append(refs, parts[1])
		}
	}
	return refs
}

// splitStepField 拆分 step.<id>.<path> 为 id 与 path。
func splitStepField(field string) (string, string, bool) {
	parts := strings.Split(field, ".")
	if len(parts) < 3 || parts[0] != "step" || parts[1] == "" {
		return "", "", false
	}
	return parts[1], strings.Join(parts[2:], "."), true
}

type whenRange struct {
	op    string
	value string
}

type whenSelector struct {
	include map[string]bool
	exclude map[string]bool
	ranges  []whenRange
}

type whenConstraint struct {
	selectors  []whenSelector
	impossible bool
}

const maxWhenSelectors = 64

// matches 判断 mapping key 是否满足约束：selectors 之间是 OR，selector 内部是 AND。
func (c whenConstraint) matches(value string) bool {
	if c.impossible {
		return false
	}
	if len(c.selectors) == 0 {
		return true
	}
	for _, s := range c.selectors {
		if s.matches(value) {
			return true
		}
	}
	return false
}

func (s whenSelector) matches(value string) bool {
	if len(s.include) > 0 && !s.include[value] {
		return false
	}
	if s.exclude[value] {
		return false
	}
	for _, r := range s.ranges {
		if !whenOneRange(value, r) {
			return false
		}
	}
	return true
}

// whenDiscriminatorValues 收集 when 中可静态确定的判别字段约束。
// 支持 && / || / ! / == / != / 范围比较；字符串、数字、布尔值都可作为判别值。
func whenDiscriminatorValues(n *whenNode) map[string]map[string]whenConstraint {
	return collectWhenConstraints(n)
}

func collectWhenConstraints(n *whenNode) map[string]map[string]whenConstraint {
	if n == nil {
		return nil
	}
	switch n.kind {
	case "and":
		return mergeConstraintsAnd(collectWhenConstraints(n.left), collectWhenConstraints(n.right))
	case "or":
		return mergeConstraintsOr(collectWhenConstraints(n.left), collectWhenConstraints(n.right))
	case "compare":
		return compareConstraint(n)
	case "not":
		return collectNegatedConstraints(n.left)
	default:
		return nil
	}
}

func collectNegatedConstraints(n *whenNode) map[string]map[string]whenConstraint {
	if n == nil {
		return nil
	}
	switch n.kind {
	case "not":
		return collectWhenConstraints(n.left)
	case "and":
		return mergeConstraintsOr(collectNegatedConstraints(n.left), collectNegatedConstraints(n.right))
	case "or":
		return mergeConstraintsAnd(collectNegatedConstraints(n.left), collectNegatedConstraints(n.right))
	case "compare":
		return invertConstraints(compareConstraint(n))
	default:
		return nil
	}
}

func invertConstraints(in map[string]map[string]whenConstraint) map[string]map[string]whenConstraint {
	if in == nil {
		return nil
	}
	out := map[string]map[string]whenConstraint{}
	for step, paths := range in {
		out[step] = map[string]whenConstraint{}
		for path, c := range paths {
			out[step][path] = invertConstraint(c)
		}
	}
	return out
}

// invertConstraint 对约束取反：selectors 是 OR，取反后变成 AND。
func invertConstraint(c whenConstraint) whenConstraint {
	if c.impossible {
		return whenConstraint{}
	}
	if len(c.selectors) == 0 {
		return whenConstraint{impossible: true}
	}
	result := whenConstraint{selectors: []whenSelector{{}}}
	for _, s := range c.selectors {
		comp, ok := complementSelector(s)
		if !ok {
			return whenConstraint{impossible: true}
		}
		result = andConstraints(result, whenConstraint{selectors: comp})
		if result.impossible {
			return result
		}
	}
	return result
}

// complementSelector 返回 selector 的补集（OR 形式）；无原子的 selector 是 true，补集为 false。
func complementSelector(s whenSelector) ([]whenSelector, bool) {
	var out []whenSelector
	for v := range s.include {
		out = append(out, whenSelector{exclude: map[string]bool{v: true}})
	}
	for v := range s.exclude {
		out = append(out, whenSelector{include: map[string]bool{v: true}})
	}
	for _, r := range s.ranges {
		out = append(out, whenSelector{ranges: []whenRange{{op: invertRangeOp(r.op), value: r.value}}})
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func invertRangeOp(op string) string {
	switch op {
	case "<":
		return ">="
	case "<=":
		return ">"
	case ">":
		return "<="
	case ">=":
		return "<"
	}
	return op
}

func compareConstraint(n *whenNode) map[string]map[string]whenConstraint {
	switch n.op {
	case "==", "!=", "<", "<=", ">", ">=":
	default:
		return nil
	}
	if n.left == nil || n.right == nil {
		return nil
	}
	field, lit := n.left, n.right
	if field.kind != "field" && lit.kind == "field" {
		field, lit = lit, field
	}
	if field.kind != "field" || lit.kind != "literal" {
		return nil
	}
	val, ok := whenLiteralString(lit.value)
	if !ok {
		return nil
	}
	path, _ := field.value.(string)
	step, rest, ok := splitStepField(path)
	if !ok {
		rest, ok = splitInputField(path)
		if !ok {
			return nil
		}
		step = "input"
	}
	s := whenSelector{}
	switch n.op {
	case "==":
		s.include = map[string]bool{val: true}
	case "!=":
		s.exclude = map[string]bool{val: true}
	case "<", "<=", ">", ">=":
		s.ranges = []whenRange{{op: n.op, value: val}}
	default:
		return nil
	}
	return map[string]map[string]whenConstraint{step: {rest: {selectors: []whenSelector{s}}}}
}

func splitInputField(field string) (string, bool) {
	if strings.HasPrefix(field, "input.") {
		rest := strings.TrimPrefix(field, "input.")
		if rest != "" {
			return rest, true
		}
	}
	return "", false
}

func whenLiteralString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10), true
		}
		return strconv.FormatFloat(x, 'f', -1, 64), true
	}
	return "", false
}

func mergeConstraintsAnd(a, b map[string]map[string]whenConstraint) map[string]map[string]whenConstraint {
	out := cloneConstraints(a)
	for step, paths := range b {
		if out[step] == nil {
			out[step] = map[string]whenConstraint{}
		}
		for path, cb := range paths {
			if ca, ok := out[step][path]; ok {
				out[step][path] = andConstraints(ca, cb)
			} else {
				out[step][path] = cb
			}
		}
	}
	return out
}

// mergeConstraintsOr 处理 ||：只保留两侧都约束的 step/path，值/区间取并集。
// 单侧路径在 || 下可能不参与，丢弃以免误导分支选择。
func mergeConstraintsOr(a, b map[string]map[string]whenConstraint) map[string]map[string]whenConstraint {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	out := map[string]map[string]whenConstraint{}
	for step, paths := range a {
		bp, ok := b[step]
		if !ok {
			continue
		}
		for path, ca := range paths {
			cb, ok := bp[path]
			if !ok {
				continue
			}
			if out[step] == nil {
				out[step] = map[string]whenConstraint{}
			}
			out[step][path] = orConstraints(ca, cb)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func andConstraints(a, b whenConstraint) whenConstraint {
	if a.impossible || b.impossible {
		return whenConstraint{impossible: true}
	}
	if len(a.selectors) == 0 {
		return b
	}
	if len(b.selectors) == 0 {
		return a
	}
	var out []whenSelector
	for _, sa := range a.selectors {
		for _, sb := range b.selectors {
			if s, ok := mergeSelectors(sa, sb); ok {
				out = append(out, s)
			}
		}
	}
	if len(out) == 0 {
		return whenConstraint{impossible: true}
	}
	if len(out) > maxWhenSelectors {
		return whenConstraint{}
	}
	return whenConstraint{selectors: out}
}

func mergeSelectors(a, b whenSelector) (whenSelector, bool) {
	out := whenSelector{
		include: map[string]bool{},
		exclude: map[string]bool{},
		ranges:  append(append([]whenRange{}, a.ranges...), b.ranges...),
	}
	for v := range a.exclude {
		out.exclude[v] = true
	}
	for v := range b.exclude {
		out.exclude[v] = true
	}
	hasInclude := len(a.include) > 0 || len(b.include) > 0
	switch {
	case len(a.include) > 0 && len(b.include) > 0:
		for v := range a.include {
			if b.include[v] {
				out.include[v] = true
			}
		}
	case len(a.include) > 0:
		for v := range a.include {
			out.include[v] = true
		}
	case len(b.include) > 0:
		for v := range b.include {
			out.include[v] = true
		}
	}
	for v := range out.exclude {
		delete(out.include, v)
	}
	if hasInclude && len(out.include) == 0 {
		return whenSelector{}, false
	}
	return out, true
}

func orConstraints(a, b whenConstraint) whenConstraint {
	if a.impossible {
		return b
	}
	if b.impossible {
		return a
	}
	if len(a.selectors) == 0 || len(b.selectors) == 0 {
		return whenConstraint{}
	}
	out := append(cloneSelectors(a.selectors), cloneSelectors(b.selectors)...)
	if len(out) > maxWhenSelectors {
		return whenConstraint{}
	}
	return whenConstraint{selectors: out}
}

func cloneSelectors(in []whenSelector) []whenSelector {
	out := make([]whenSelector, 0, len(in))
	for _, s := range in {
		out = append(out, cloneSelector(s))
	}
	return out
}

func cloneSelector(s whenSelector) whenSelector {
	out := whenSelector{
		include: map[string]bool{},
		exclude: map[string]bool{},
		ranges:  append([]whenRange{}, s.ranges...),
	}
	for v := range s.include {
		out.include[v] = true
	}
	for v := range s.exclude {
		out.exclude[v] = true
	}
	return out
}

func whenOneRange(value string, r whenRange) bool {
	lv, lerr := strconv.ParseFloat(value, 64)
	rv, rerr := strconv.ParseFloat(r.value, 64)
	if lerr == nil && rerr == nil {
		switch r.op {
		case "<":
			return lv < rv
		case "<=":
			return lv <= rv
		case ">":
			return lv > rv
		case ">=":
			return lv >= rv
		}
	}
	switch r.op {
	case "<":
		return value < r.value
	case "<=":
		return value <= r.value
	case ">":
		return value > r.value
	case ">=":
		return value >= r.value
	}
	return false
}

func cloneConstraints(in map[string]map[string]whenConstraint) map[string]map[string]whenConstraint {
	out := map[string]map[string]whenConstraint{}
	for step, paths := range in {
		out[step] = map[string]whenConstraint{}
		for path, c := range paths {
			out[step][path] = whenConstraint{
				selectors:  cloneSelectors(c.selectors),
				impossible: c.impossible,
			}
		}
	}
	return out
}

func whenSegmentPath(seg whenPathSegment) string {
	if seg.hasIndex {
		return fmt.Sprintf("%s[%d]", seg.name, seg.index)
	}
	return seg.name
}

func tokenizeWhen(s string) ([]whenToken, error) {
	var toks []whenToken
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, whenToken{kind: whenLParen, text: "("})
			i++
		case c == ')':
			toks = append(toks, whenToken{kind: whenRParen, text: ")"})
			i++
		case c == '&' && i+1 < len(s) && s[i+1] == '&':
			toks = append(toks, whenToken{kind: whenAnd, text: "&&"})
			i += 2
		case c == '|' && i+1 < len(s) && s[i+1] == '|':
			toks = append(toks, whenToken{kind: whenOr, text: "||"})
			i += 2
		case c == '!' && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, whenToken{kind: whenOp, text: "!="})
			i += 2
		case c == '!':
			toks = append(toks, whenToken{kind: whenNot, text: "!"})
			i++
		case c == '=' && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, whenToken{kind: whenOp, text: "=="})
			i += 2
		case c == '>' || c == '<':
			if i+1 < len(s) && s[i+1] == '=' {
				toks = append(toks, whenToken{kind: whenOp, text: s[i : i+2]})
				i += 2
			} else {
				toks = append(toks, whenToken{kind: whenOp, text: string(c)})
				i++
			}
		case c == '"':
			start := i
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i += 2
					continue
				}
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("unterminated string")
			}
			i++
			raw := s[start:i]
			v, err := strconv.Unquote(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid string %s", raw)
			}
			toks = append(toks, whenToken{kind: whenString, text: v})
		case c == '-' || (c >= '0' && c <= '9'):
			start := i
			if c == '-' {
				i++
				if i >= len(s) || s[i] < '0' || s[i] > '9' {
					return nil, fmt.Errorf("invalid number")
				}
			}
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			if i < len(s) && s[i] == '.' {
				i++
				if i >= len(s) || s[i] < '0' || s[i] > '9' {
					return nil, fmt.Errorf("invalid number")
				}
				for i < len(s) && s[i] >= '0' && s[i] <= '9' {
					i++
				}
			}
			toks = append(toks, whenToken{kind: whenNumber, text: s[start:i]})
		case isWhenIdentStart(c):
			start := i
			i++
			for i < len(s) && isWhenIdentPart(s[i]) {
				i++
			}
			toks = append(toks, whenToken{kind: whenIdent, text: s[start:i]})
		default:
			return nil, fmt.Errorf("unexpected character %q", string(c))
		}
	}
	return toks, nil
}

func isWhenIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isWhenIdentPart(c byte) bool {
	return isWhenIdentStart(c) || (c >= '0' && c <= '9') || c == '.' || c == '[' || c == ']'
}

type whenPathSegment struct {
	name     string
	index    int
	hasIndex bool
}

func parseWhenPath(path string) ([]whenPathSegment, error) {
	parts := strings.Split(path, ".")
	segs := make([]whenPathSegment, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty path segment")
		}
		seg := whenPathSegment{name: part}
		if i := strings.IndexByte(part, '['); i >= 0 {
			if !strings.HasSuffix(part, "]") || strings.Count(part, "[") != 1 || strings.Count(part, "]") != 1 {
				return nil, fmt.Errorf("invalid index in %q", part)
			}
			name := part[:i]
			idxStr := part[i+1 : len(part)-1]
			if name == "" || idxStr == "" {
				return nil, fmt.Errorf("invalid index in %q", part)
			}
			for _, c := range idxStr {
				if c < '0' || c > '9' {
					return nil, fmt.Errorf("invalid index in %q", part)
				}
			}
			idx, err := strconv.Atoi(idxStr)
			if err != nil || idx < 0 {
				return nil, fmt.Errorf("invalid index in %q", part)
			}
			seg = whenPathSegment{name: name, index: idx, hasIndex: true}
		} else if strings.ContainsAny(part, "[]") {
			return nil, fmt.Errorf("invalid index in %q", part)
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

type whenParser struct {
	tokens []whenToken
	pos    int
}

func (p *whenParser) peek() (whenToken, bool) {
	if p.pos >= len(p.tokens) {
		return whenToken{}, false
	}
	return p.tokens[p.pos], true
}

func (p *whenParser) next() (whenToken, bool) {
	t, ok := p.peek()
	if ok {
		p.pos++
	}
	return t, ok
}

func (p *whenParser) parseOr() (*whenNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != whenOr {
			return left, nil
		}
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &whenNode{kind: "or", left: left, right: right}
	}
}

func (p *whenParser) parseAnd() (*whenNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != whenAnd {
			return left, nil
		}
		p.pos++
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &whenNode{kind: "and", left: left, right: right}
	}
}

func (p *whenParser) parseUnary() (*whenNode, error) {
	t, ok := p.peek()
	if ok && t.kind == whenNot {
		p.pos++
		n, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &whenNode{kind: "not", left: n}, nil
	}
	return p.parsePrimary()
}

func (p *whenParser) parsePrimary() (*whenNode, error) {
	t, ok := p.next()
	if !ok {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	switch t.kind {
	case whenLParen:
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		closeTok, ok := p.next()
		if !ok || closeTok.kind != whenRParen {
			return nil, fmt.Errorf("missing )")
		}
		return n, nil
	case whenIdent:
		left, err := whenOperand(t)
		if err != nil {
			return nil, err
		}
		return p.parseComparison(left)
	case whenString, whenNumber:
		left, err := whenOperand(t)
		if err != nil {
			return nil, err
		}
		return p.parseComparison(left)
	default:
		return nil, fmt.Errorf("unexpected token %q", t.text)
	}
}

func (p *whenParser) parseComparison(left *whenNode) (*whenNode, error) {
	t, ok := p.peek()
	if !ok || t.kind != whenOp {
		if left.kind == "literal" {
			if _, isBool := left.value.(bool); isBool {
				return left, nil
			}
		}
		return nil, fmt.Errorf("operand must be compared")
	}
	p.pos++
	rightTok, ok := p.next()
	if !ok {
		return nil, fmt.Errorf("missing right operand")
	}
	right, err := whenOperand(rightTok)
	if err != nil {
		return nil, err
	}
	return &whenNode{kind: "compare", op: t.text, left: left, right: right}, nil
}

func isWhenStepID(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func whenOperand(t whenToken) (*whenNode, error) {
	switch t.kind {
	case whenIdent:
		switch t.text {
		case "true":
			return &whenNode{kind: "literal", value: true}, nil
		case "false":
			return &whenNode{kind: "literal", value: false}, nil
		case "null":
			return &whenNode{kind: "literal", value: nil}, nil
		}
		parts := strings.Split(t.text, ".")
		if len(parts) < 2 || (parts[0] != "input" && parts[0] != "step") {
			return nil, fmt.Errorf("field %q must be input.<path> or step.<id>.<path>", t.text)
		}
		var path string
		if parts[0] == "step" {
			if len(parts) < 3 {
				return nil, fmt.Errorf("field %q must be step.<id>.<path>", t.text)
			}
			if !isWhenStepID(parts[1]) {
				return nil, fmt.Errorf("field %q has invalid step id %q", t.text, parts[1])
			}
			path = strings.Join(parts[2:], ".")
		} else {
			path = strings.Join(parts[1:], ".")
		}
		if _, err := parseWhenPath(path); err != nil {
			return nil, fmt.Errorf("field %q: %v", t.text, err)
		}
		return &whenNode{kind: "field", value: t.text}, nil
	case whenString:
		return &whenNode{kind: "literal", value: t.text}, nil
	case whenNumber:
		v, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", t.text)
		}
		return &whenNode{kind: "literal", value: v}, nil
	default:
		return nil, fmt.Errorf("unexpected operand %q", t.text)
	}
}

func evalWhenNode(n *whenNode) whenValue {
	if n == nil {
		return whenUnknown
	}
	switch n.kind {
	case "literal":
		if v, ok := n.value.(bool); ok {
			if v {
				return whenTrue
			}
			return whenFalse
		}
		return whenUnknown
	case "field":
		return whenUnknown
	case "not":
		switch evalWhenNode(n.left) {
		case whenTrue:
			return whenFalse
		case whenFalse:
			return whenTrue
		default:
			return whenUnknown
		}
	case "and":
		left := evalWhenNode(n.left)
		if left == whenFalse {
			return whenFalse
		}
		right := evalWhenNode(n.right)
		if right == whenFalse {
			return whenFalse
		}
		if left == whenTrue && right == whenTrue {
			return whenTrue
		}
		return whenUnknown
	case "or":
		left := evalWhenNode(n.left)
		if left == whenTrue {
			return whenTrue
		}
		right := evalWhenNode(n.right)
		if right == whenTrue {
			return whenTrue
		}
		if left == whenFalse && right == whenFalse {
			return whenFalse
		}
		return whenUnknown
	case "compare":
		lv, lok := whenOperandValue(n.left)
		rv, rok := whenOperandValue(n.right)
		if !lok || !rok {
			return whenUnknown
		}
		return whenCompare(n.op, lv, rv)
	default:
		return whenUnknown
	}
}

func whenOperandValue(n *whenNode) (any, bool) {
	if n == nil || n.kind != "literal" {
		return nil, false
	}
	return n.value, true
}

func whenCompare(op string, left, right any) whenValue {
	switch op {
	case "==":
		return whenBool(left == right)
	case "!=":
		return whenBool(left != right)
	}
	switch l := left.(type) {
	case float64:
		r, ok := right.(float64)
		if !ok {
			return whenUnknown
		}
		switch op {
		case ">":
			return whenBool(l > r)
		case ">=":
			return whenBool(l >= r)
		case "<":
			return whenBool(l < r)
		case "<=":
			return whenBool(l <= r)
		}
	case string:
		r, ok := right.(string)
		if !ok {
			return whenUnknown
		}
		switch op {
		case ">":
			return whenBool(l > r)
		case ">=":
			return whenBool(l >= r)
		case "<":
			return whenBool(l < r)
		case "<=":
			return whenBool(l <= r)
		}
	}
	return whenUnknown
}

func whenBool(v bool) whenValue {
	if v {
		return whenTrue
	}
	return whenFalse
}
