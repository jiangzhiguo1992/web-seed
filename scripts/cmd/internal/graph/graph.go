// Package graph 计算契约入边并写回 table/entity 的 used_by。
//
// used_by 是生成物：table 记录 entities/queries，entity 记录 queries/entities。
// 写回时保留文件其余内容；used_by 约定为最后一个顶层键。
package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type TableUsedBy struct {
	Entities []string
	Queries  []string
}

type EntityUsedBy struct {
	Queries  []string
	Entities []string
}

type Snapshot struct {
	Tables      map[string]TableUsedBy
	Entities    map[string]EntityUsedBy
	TableFiles  map[string]string
	EntityFiles map[string]string
}

type tableSet struct {
	entities map[string]bool
	queries  map[string]bool
}

type entitySet struct {
	queries  map[string]bool
	entities map[string]bool
}

// Compute 扫描 tables/entities/queries，计算 used_by 的目标集合。
func Compute(root string) (*Snapshot, error) {
	tables := map[string]*tableSet{}
	entities := map[string]*entitySet{}
	tableFiles := map[string]string{}
	entityFiles := map[string]string{}
	entityDocs := map[string]map[string]any{}

	for _, f := range glob(root, "contracts/data/tables/*.yaml") {
		if filepath.Base(f) == "_base.yaml" {
			continue
		}
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		id, _ := doc["id"].(string)
		if id == "" {
			continue
		}
		tables[id] = &tableSet{entities: map[string]bool{}, queries: map[string]bool{}}
		tableFiles[id] = rel(root, f)
	}
	for _, f := range glob(root, "contracts/data/entities/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		id, _ := doc["id"].(string)
		if id == "" {
			continue
		}
		entities[id] = &entitySet{queries: map[string]bool{}, entities: map[string]bool{}}
		entityFiles[id] = rel(root, f)
		entityDocs[id] = doc
	}
	for id, doc := range entityDocs {
		for _, t := range tableRefs(doc) {
			if ts := tables[t]; ts != nil {
				ts.entities[id] = true
			}
		}
		for _, raw := range asList(doc["relations"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			target, _ := m["target"].(string)
			if es := entities[target]; es != nil {
				es.entities[id] = true
			}
		}
	}
	for _, f := range glob(root, "contracts/data/queries/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["queries"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			qid, _ := m["id"].(string)
			if qid == "" {
				continue
			}
			for _, t := range tableRefs(m) {
				if ts := tables[t]; ts != nil {
					ts.queries[qid] = true
				}
			}
			if e := returnsEntity(m); e != "" {
				if es := entities[e]; es != nil {
					es.queries[qid] = true
				}
			}
		}
	}

	snap := &Snapshot{
		Tables:      map[string]TableUsedBy{},
		Entities:    map[string]EntityUsedBy{},
		TableFiles:  tableFiles,
		EntityFiles: entityFiles,
	}
	for id, ts := range tables {
		snap.Tables[id] = TableUsedBy{Entities: sortedKeys(ts.entities), Queries: sortedKeys(ts.queries)}
	}
	for id, es := range entities {
		snap.Entities[id] = EntityUsedBy{Queries: sortedKeys(es.queries), Entities: sortedKeys(es.entities)}
	}
	return snap, nil
}

// Write 把 used_by 写回文件，返回发生变化的相对路径。
func (s *Snapshot) Write(root string) ([]string, error) {
	var changed []string
	for _, id := range sortedFileKeys(s.TableFiles) {
		file := s.TableFiles[id]
		want := s.Tables[id]
		ok, err := syncFile(filepath.Join(root, file), "table", want.Entities, want.Queries)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
	}
	for _, id := range sortedFileKeys(s.EntityFiles) {
		file := s.EntityFiles[id]
		want := s.Entities[id]
		ok, err := syncFile(filepath.Join(root, file), "entity", want.Queries, want.Entities)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
	}
	return changed, nil
}

// Check 返回与计算结果不一致的 used_by 描述；不写文件。
func (s *Snapshot) Check(root string) ([]string, error) {
	var drift []string
	for _, id := range sortedFileKeys(s.TableFiles) {
		file := s.TableFiles[id]
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		want := s.Tables[id]
		ents, qs, ok := currentUsedBy(doc)
		if !ok {
			drift = append(drift, fmt.Sprintf("%s: missing used_by", file))
			continue
		}
		if !sameSet(ents, want.Entities) {
			drift = append(drift, fmt.Sprintf("%s: used_by.entities must be %v, got %v", file, want.Entities, ents))
		}
		if !sameSet(qs, want.Queries) {
			drift = append(drift, fmt.Sprintf("%s: used_by.queries must be %v, got %v", file, want.Queries, qs))
		}
	}
	for _, id := range sortedFileKeys(s.EntityFiles) {
		file := s.EntityFiles[id]
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		want := s.Entities[id]
		ents, qs, ok := currentUsedBy(doc)
		if !ok {
			drift = append(drift, fmt.Sprintf("%s: missing used_by", file))
			continue
		}
		if !sameSet(qs, want.Queries) {
			drift = append(drift, fmt.Sprintf("%s: used_by.queries must be %v, got %v", file, want.Queries, qs))
		}
		if !sameSet(ents, want.Entities) {
			drift = append(drift, fmt.Sprintf("%s: used_by.entities must be %v, got %v", file, want.Entities, ents))
		}
	}
	return drift, nil
}

// syncFile 仅在 used_by 变化时重写文件。
func syncFile(path, kind string, first, second []string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	var haveFirst, haveSecond []string
	ok := false
	if ub, found := asMap(doc["used_by"]); found {
		ok = true
		if kind == "table" {
			haveFirst = stringList(ub["entities"])
			haveSecond = stringList(ub["queries"])
		} else {
			haveFirst = stringList(ub["queries"])
			haveSecond = stringList(ub["entities"])
		}
	}
	if ok && sameSet(haveFirst, first) && sameSet(haveSecond, second) {
		return false, nil
	}
	block := renderUsedBy(kind, first, second)
	out, err := applyUsedBy(src, block)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, out, info.Mode()); err != nil {
		return false, err
	}
	return true, nil
}

func renderUsedBy(kind string, first, second []string) string {
	var b strings.Builder
	b.WriteString("used_by:\n")
	if kind == "table" {
		writeList(&b, "entities", first)
		writeList(&b, "queries", second)
	} else {
		writeList(&b, "queries", first)
		writeList(&b, "entities", second)
	}
	return b.String()
}

func writeList(b *strings.Builder, name string, items []string) {
	if len(items) == 0 {
		fmt.Fprintf(b, "  %s: []\n", name)
		return
	}
	fmt.Fprintf(b, "  %s:\n", name)
	for _, item := range items {
		fmt.Fprintf(b, "    - %s\n", item)
	}
}

// applyUsedBy 替换最后一个顶层键 used_by；缺失时追加到文件末尾。
func applyUsedBy(src []byte, block string) ([]byte, error) {
	start, end, found, err := usedByRange(src)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(src), "\n")
	lines := strings.Split(text, "\n")
	if !found {
		return []byte(text + "\n" + block), nil
	}
	if start < 1 {
		start = 1
	}
	if end > len(lines)+1 {
		end = len(lines) + 1
	}
	before := append([]string{}, lines[:start-1]...)
	after := append([]string{}, lines[end-1:]...)
	out := append(before, strings.Split(strings.TrimSuffix(block, "\n"), "\n")...)
	out = append(out, after...)
	return []byte(strings.Join(out, "\n") + "\n"), nil
}

// usedByRange 返回 used_by 键的 1-based 起始行和排他结束行。
func usedByRange(src []byte) (int, int, bool, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return 0, 0, false, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return 0, 0, false, nil
	}
	root := doc.Content[0]
	total := strings.Count(string(src), "\n")
	if !strings.HasSuffix(string(src), "\n") {
		total++
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Value != "used_by" {
			continue
		}
		start := key.Line
		end := total + 1
		for j := i + 2; j+1 < len(root.Content); j += 2 {
			if next := root.Content[j]; next.Line > start {
				end = next.Line
				break
			}
		}
		return start, end, true, nil
	}
	return 0, 0, false, nil
}

func currentUsedBy(doc map[string]any) ([]string, []string, bool) {
	ub, ok := asMap(doc["used_by"])
	if !ok {
		return nil, nil, false
	}
	return stringList(ub["entities"]), stringList(ub["queries"]), true
}

func glob(root, pattern string) []string {
	files, _ := filepath.Glob(filepath.Join(root, pattern))
	sort.Strings(files)
	return files
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

func loadYAML(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func tableRefs(m map[string]any) []string {
	var out []string
	if t, ok := m["table"].(string); ok && t != "" {
		out = append(out, t)
	}
	for _, x := range asList(m["tables"]) {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func returnsEntity(m map[string]any) string {
	raw, _ := m["returns"].(string)
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "[]")
	if strings.HasPrefix(s, "data.entities.") {
		return s
	}
	return ""
}

func stringList(v any) []string {
	var out []string
	for _, raw := range asList(v) {
		if s, ok := raw.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedFileKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}
	return true
}
