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
