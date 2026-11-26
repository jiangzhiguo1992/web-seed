// Package rulesdsl 是 feature.flows invariant.assert 与 policy.decision.when 的最小可执行表达式引擎。
//
// 支持：字段路径、字符串/数字/布尔/null、len()、== != > >= < <=、in/not in、&& || !、括号。
// 不支持函数副作用、集合推导或 SQL；policy 的执行效果由 decision.effect 结构化声明。
package rulesdsl

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Env map[string]any

type Node interface {
	Eval(Env) (any, error)
}

// Parse 解析表达式；空表达式报错。
func Parse(expr string) (Node, error) {
	p := &parser{input: expr}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos != len(p.input) {
		return nil, fmt.Errorf("unexpected token at %d: %q", p.pos, p.input[p.pos:])
	}
	return n, nil
}

// Eval 解析并执行表达式。
func Eval(expr string, env Env) (any, error) {
	n, err := Parse(expr)
	if err != nil {
		return nil, err
	}
	return n.Eval(env)
}

// Validate 只解析，返回语法错误。
func Validate(expr string) error {
	_, err := Parse(expr)
	return err
}

type parser struct {
	input string
	pos   int
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if !p.consume("||") {
			return left, nil
		}
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "||", left: left, right: right}
	}
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpace()
		if !p.consume("&&") {
			return left, nil
		}
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "&&", left: left, right: right}
	}
}

func (p *parser) parseUnary() (Node, error) {
	p.skipSpace()
	if p.consume("!") {
		child, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryNode{op: "!", child: child}, nil
	}
	return p.parseComparison()
}

func (p *parser) parseComparison() (Node, error) {
	left, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	for _, op := range []string{"==", "!=", ">=", "<=", ">", "<"} {
		if p.consume(op) {
			right, err := p.parsePrimary()
			if err != nil {
				return nil, err
			}
			return binaryNode{op: op, left: left, right: right}, nil
		}
	}
	if p.consumeWord("in") {
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return binaryNode{op: "in", left: left, right: right}, nil
	}
	if p.consumeWord("not") {
		p.skipSpace()
		if !p.consumeWord("in") {
			return nil, fmt.Errorf("expected in after not at %d", p.pos)
		}
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return binaryNode{op: "not_in", left: left, right: right}, nil
	}
	return left, nil
}

func (p *parser) parsePrimary() (Node, error) {
	p.skipSpace()
	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	ch := p.input[p.pos]
	switch {
	case ch == '(':
		p.pos++
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if !p.consume(")") {
			return nil, fmt.Errorf("missing ) at %d", p.pos)
		}
		return n, nil
	case ch == '[':
		p.pos++
		var items []Node
		for {
			p.skipSpace()
			if p.consume("]") {
				return listNode{items: items}, nil
			}
			item, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
			p.skipSpace()
			if p.consume("]") {
				return listNode{items: items}, nil
			}
			if !p.consume(",") {
				return nil, fmt.Errorf("missing , or ] at %d", p.pos)
			}
		}
	case ch == '"' || ch == '\'':
		return p.parseString(ch)
	case isDigit(ch) || (ch == '-' && p.pos+1 < len(p.input) && isDigit(p.input[p.pos+1])):
		return p.parseNumber()
	case isIdentStart(rune(ch)):
		ident := p.parseIdent()
		switch ident {
		case "true":
			return literalNode{value: true}, nil
		case "false":
			return literalNode{value: false}, nil
		case "null", "nil":
			return literalNode{value: nil}, nil
		}
		p.skipSpace()
		if p.consume("(") {
			arg, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			p.skipSpace()
			if !p.consume(")") {
				return nil, fmt.Errorf("missing ) after %s(", ident)
			}
			return callNode{name: ident, arg: arg}, nil
		}
		return identNode{path: ident}, nil
	default:
		return nil, fmt.Errorf("unexpected character %q at %d", ch, p.pos)
	}
}

func (p *parser) parseString(quote byte) (Node, error) {
	p.pos++
	start := p.pos
	for p.pos < len(p.input) {
		if p.input[p.pos] == quote {
			value := p.input[start:p.pos]
			p.pos++
			return literalNode{value: value}, nil
		}
		p.pos++
	}
	return nil, fmt.Errorf("unterminated string")
}

func (p *parser) parseNumber() (Node, error) {
	start := p.pos
	if p.input[p.pos] == '-' {
		p.pos++
	}
	for p.pos < len(p.input) && (unicode.IsDigit(rune(p.input[p.pos])) || p.input[p.pos] == '.') {
		p.pos++
	}
	n, err := strconv.ParseFloat(p.input[start:p.pos], 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number %q", p.input[start:p.pos])
	}
	return literalNode{value: n}, nil
}

func (p *parser) parseIdent() string {
	start := p.pos
	for p.pos < len(p.input) && (isIdentPart(rune(p.input[p.pos])) || p.input[p.pos] == '.') {
		p.pos++
	}
	return p.input[start:p.pos]
}

func (p *parser) skipSpace() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

func (p *parser) consume(s string) bool {
	if strings.HasPrefix(p.input[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}

func (p *parser) consumeWord(s string) bool {
	if !strings.HasPrefix(p.input[p.pos:], s) {
		return false
	}
	end := p.pos + len(s)
	if end < len(p.input) && isIdentPart(rune(p.input[end])) {
		return false
	}
	p.pos = end
	return true
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

type literalNode struct{ value any }

func (n literalNode) Eval(Env) (any, error) { return n.value, nil }

type listNode struct{ items []Node }

func (n listNode) Eval(env Env) (any, error) {
	out := make([]any, 0, len(n.items))
	for _, item := range n.items {
		v, err := item.Eval(env)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

type identNode struct{ path string }

func (n identNode) Eval(env Env) (any, error) {
	if v, ok := env[n.path]; ok {
		return v, nil
	}
	parts := strings.Split(n.path, ".")
	if len(parts) > 1 {
		var current any = map[string]any(env)
		for _, part := range parts {
			m, ok := current.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("cannot resolve %s", n.path)
			}
			v, ok := m[part]
			if !ok {
				return nil, fmt.Errorf("unknown identifier %s", n.path)
			}
			current = v
		}
		return current, nil
	}
	return nil, fmt.Errorf("unknown identifier %s", n.path)
}

type unaryNode struct {
	op    string
	child Node
}

func (n unaryNode) Eval(env Env) (any, error) {
	v, err := n.child.Eval(env)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "!":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("! expects bool, got %T", v)
		}
		return !b, nil
	}
	return nil, fmt.Errorf("unknown unary operator %s", n.op)
}

type binaryNode struct {
	op          string
	left, right Node
}

func (n binaryNode) Eval(env Env) (any, error) {
	left, err := n.left.Eval(env)
	if err != nil {
		return nil, err
	}
	if n.op == "&&" || n.op == "||" {
		lb, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("%s expects bool, got %T", n.op, left)
		}
		if n.op == "&&" && !lb {
			return false, nil
		}
		if n.op == "||" && lb {
			return true, nil
		}
		right, err := n.right.Eval(env)
		if err != nil {
			return nil, err
		}
		rb, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("%s expects bool, got %T", n.op, right)
		}
		return rb, nil
	}
	right, err := n.right.Eval(env)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "==":
		return equal(left, right), nil
	case "!=":
		return !equal(left, right), nil
	case "in", "not_in":
		items, ok := right.([]any)
		if !ok {
			return nil, fmt.Errorf("%s expects a list on the right, got %T", n.op, right)
		}
		found := false
		for _, item := range items {
			if equal(left, item) {
				found = true
				break
			}
		}
		if n.op == "not_in" {
			return !found, nil
		}
		return found, nil
	}
	cmp, err := compare(left, right)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case ">":
		return cmp > 0, nil
	case ">=":
		return cmp >= 0, nil
	case "<":
		return cmp < 0, nil
	case "<=":
		return cmp <= 0, nil
	}
	return nil, fmt.Errorf("unknown binary operator %s", n.op)
}

type callNode struct {
	name string
	arg  Node
}

func (n callNode) Eval(env Env) (any, error) {
	if n.name != "len" {
		return nil, fmt.Errorf("unknown function %s", n.name)
	}
	v, err := n.arg.Eval(env)
	if err != nil {
		return nil, err
	}
	switch value := v.(type) {
	case string:
		return float64(utf8.RuneCountInString(value)), nil
	case []any:
		return float64(len(value)), nil
	case map[string]any:
		return float64(len(value)), nil
	default:
		return nil, fmt.Errorf("len() expects string/list/map, got %T", v)
	}
}

func equal(a, b any) bool {
	if af, ok := asNumber(a); ok {
		if bf, ok := asNumber(b); ok {
			return af == bf
		}
	}
	return reflect.DeepEqual(a, b)
}

func compare(a, b any) (int, error) {
	if af, ok := asNumber(a); ok {
		if bf, ok := asNumber(b); ok {
			switch {
			case af < bf:
				return -1, nil
			case af > bf:
				return 1, nil
			default:
				return 0, nil
			}
		}
	}
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return strings.Compare(as, bs), nil
	}
	return 0, fmt.Errorf("cannot compare %T and %T", a, b)
}

func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
