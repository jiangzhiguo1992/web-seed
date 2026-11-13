// Package graph 计算 query 入边并写回 table/entity 的 used_by.queries。
//
// table.entities 与 model.included_by 是显式映射，由 checker 校验；
// gen-graph 只维护生成物 used_by.queries。
package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

type TableUsedBy struct {
	Queries []string
}

type EntityUsedBy struct {
	Queries []string
}

type Snapshot struct {
	Tables      map[string]TableUsedBy
	Entities    map[string]EntityUsedBy
	TableFiles  map[string]string
	EntityFiles map[string]string
}

type tableSet struct {
	queries map[string]bool
}

type entitySet struct {
	queries map[string]bool
}

// Compute 扫描 tables/entities/queries，计算 used_by.queries。
func Compute(root string) (*Snapshot, error) {
	if info, err := os.Stat(filepath.Join(root, "contracts/data/tables")); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s: missing contracts/data/tables (not a contract root?)", root)
	}
	tables := map[string]*tableSet{}
	entities := map[string]*entitySet{}
	tableFiles := map[string]string{}
	entityFiles := map[string]string{}

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
		if prev, ok := tableFiles[id]; ok {
			return nil, fmt.Errorf("duplicate table id %s: %s and %s", id, prev, rel(root, f))
		}
		tables[id] = &tableSet{queries: map[string]bool{}}
		tableFiles[id] = rel(root, f)
	}
	for _, f := range glob(root, "contracts/data/entities/*.yaml") {
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
		if prev, ok := entityFiles[id]; ok {
			return nil, fmt.Errorf("duplicate entity id %s: %s and %s", id, prev, rel(root, f))
		}
		entities[id] = &entitySet{queries: map[string]bool{}}
		entityFiles[id] = rel(root, f)
	}
	seenQueries := map[string]string{}
	for _, f := range glob(root, "contracts/data/queries/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["queries"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			qid := contractspec.QueryID(fileID, name)
			if qid == "" {
				continue
			}
			if prev, ok := seenQueries[qid]; ok {
				return nil, fmt.Errorf("duplicate query id %s: %s and %s", qid, prev, rel(root, f))
			}
			seenQueries[qid] = rel(root, f)
			for _, t := range queryTableRefs(m) {
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
		snap.Tables[id] = TableUsedBy{Queries: sortedKeys(ts.queries)}
	}
	for id, es := range entities {
		snap.Entities[id] = EntityUsedBy{Queries: sortedKeys(es.queries)}
	}
	return snap, nil
}

// Write 把 used_by.queries 写回文件，返回发生变化的相对路径。
func (s *Snapshot) Write(root string) ([]string, error) {
	var changed []string
	for _, id := range sortedFileKeys(s.TableFiles) {
		file := s.TableFiles[id]
		ok, err := syncUsedByFile(filepath.Join(root, file), s.Tables[id].Queries)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
	}
	for _, id := range sortedFileKeys(s.EntityFiles) {
		file := s.EntityFiles[id]
		ok, err := syncUsedByFile(filepath.Join(root, file), s.Entities[id].Queries)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
	}
	return changed, nil
}

// Check 返回与计算结果不一致的 used_by.queries；不写文件。
func (s *Snapshot) Check(root string) ([]string, error) {
	var drift []string
	for _, id := range sortedFileKeys(s.TableFiles) {
		file := s.TableFiles[id]
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		got := usedByQueries(doc)
		want := s.Tables[id].Queries
		if !usedByOnlyQueries(doc) {
			drift = append(drift, fmt.Sprintf("%s: used_by must only contain queries", file))
		} else if !sameSet(got, want) {
			drift = append(drift, fmt.Sprintf("%s: used_by.queries must be %v, got %v", file, want, got))
		}
	}
	for _, id := range sortedFileKeys(s.EntityFiles) {
		file := s.EntityFiles[id]
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		got := usedByQueries(doc)
		want := s.Entities[id].Queries
		if !usedByOnlyQueries(doc) {
			drift = append(drift, fmt.Sprintf("%s: used_by must only contain queries", file))
		} else if !sameSet(got, want) {
			drift = append(drift, fmt.Sprintf("%s: used_by.queries must be %v, got %v", file, want, got))
		}
	}
	return drift, nil
}

func syncUsedByFile(path string, queries []string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if sameSet(usedByQueries(doc), queries) && usedByOnlyQueries(doc) {
		return false, nil
	}
	out, err := applyUsedBy(src, renderUsedByQueries(queries))
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, writeAtomic(path, out)
}

func writeAtomic(path string, out []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".graph-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func renderUsedByQueries(queries []string) string {
	var b strings.Builder
	b.WriteString("used_by:\n")
	if len(queries) == 0 {
		b.WriteString("  queries: []\n")
		return b.String()
	}
	b.WriteString("  queries:\n")
	for _, q := range queries {
		fmt.Fprintf(&b, "    - %s\n", q)
	}
	return b.String()
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

func usedByQueries(doc map[string]any) []string {
	ub, _ := asMap(doc["used_by"])
	return stringList(ub["queries"])
}

func usedByOnlyQueries(doc map[string]any) bool {
	ub, ok := asMap(doc["used_by"])
	if !ok {
		return false
	}
	for k := range ub {
		if k != "queries" {
			return false
		}
	}
	return true
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

// queryTableRefs 读取查询的 targets。
func queryTableRefs(m map[string]any) []string {
	var out []string
	for _, raw := range asList(m["targets"]) {
		if s, ok := raw.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func returnsEntity(m map[string]any) string {
	raw, _ := m["returns"].(string)
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "List<") && strings.HasSuffix(s, ">") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "List<"), ">")
	}
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
