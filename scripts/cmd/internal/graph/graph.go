// Package graph 计算 query/capability 入边并写回 table/entity 的 used_by。
//
// table.entities 与 model.included_by 是显式映射，由 checker 校验；
// gen-graph 只维护生成物 used_by.queries（table/entity）与 used_by.capabilities（entity）。
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
	Queries      []string
	Capabilities []string
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
	queries      map[string]bool
	capabilities map[string]bool
}

// scanTarget 是 _meta targets 的扫描路径与 ID 模板。
type scanTarget struct {
	dir  string
	glob string
	id   string
}

// resolveTarget 从 contracts/_meta/*.yaml 读取类型 targets。
// 生产路径必须严格：缺失/重复/解析失败/targets 不完整都直接报错，避免静默清空 used_by。
func resolveTarget(root, typ string) (scanTarget, error) {
	files, err := filepath.Glob(filepath.Join(root, "contracts/_meta/*.yaml"))
	if err != nil {
		return scanTarget{}, err
	}
	var found *scanTarget
	var foundPath string
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return scanTarget{}, err
		}
		var head struct {
			Type    string `yaml:"type"`
			Targets struct {
				Dir  string `yaml:"dir"`
				Glob string `yaml:"glob"`
				ID   string `yaml:"id"`
			} `yaml:"targets"`
		}
		if err := yaml.Unmarshal(b, &head); err != nil {
			return scanTarget{}, fmt.Errorf("%s: %w", file, err)
		}
		if head.Type != typ {
			continue
		}
		if found != nil {
			return scanTarget{}, fmt.Errorf("duplicate meta type %q: %s and %s", typ, foundPath, file)
		}
		if head.Targets.Dir == "" || head.Targets.Glob == "" || head.Targets.ID == "" {
			return scanTarget{}, fmt.Errorf("%s: targets.dir/glob/id must not be empty", file)
		}
		if !strings.Contains(head.Targets.ID, "<file>") {
			return scanTarget{}, fmt.Errorf("%s: targets.id must contain <file>", file)
		}
		found = &scanTarget{dir: head.Targets.Dir, glob: head.Targets.Glob, id: head.Targets.ID}
		foundPath = file
	}
	if found == nil {
		return scanTarget{}, fmt.Errorf("no meta target for type %q under contracts/_meta", typ)
	}
	return *found, nil
}

// Compute 扫描 tables/entities/queries，计算 used_by.queries。
func Compute(root string) (*Snapshot, error) {
	tableTarget, err := resolveTarget(root, "data.tables")
	if err != nil {
		return nil, err
	}
	entityTarget, err := resolveTarget(root, "data.entities")
	if err != nil {
		return nil, err
	}
	queryTarget, err := resolveTarget(root, "data.queries")
	if err != nil {
		return nil, err
	}
	for name, target := range map[string]scanTarget{
		"data.tables": tableTarget, "data.entities": entityTarget, "data.queries": queryTarget,
	} {
		info, err := os.Stat(filepath.Join(root, target.dir))
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%s: missing %s target dir %s", root, name, target.dir)
		}
	}
	tables := map[string]*tableSet{}
	entities := map[string]*entitySet{}
	tableFiles := map[string]string{}
	entityFiles := map[string]string{}

	tableIDs := contractspec.Targets{ID: tableTarget.id}
	for _, f := range glob(root, filepath.Join(tableTarget.dir, tableTarget.glob)) {
		if filepath.Base(f) == "_base.yaml" {
			continue
		}
		if _, err := loadYAML(f); err != nil {
			return nil, err
		}
		id := tableIDs.FileID(filepath.Base(f))
		if id == "" {
			continue
		}
		tables[id] = &tableSet{queries: map[string]bool{}}
		tableFiles[id] = rel(root, f)
	}
	entityIDs := contractspec.Targets{ID: entityTarget.id}
	for _, f := range glob(root, filepath.Join(entityTarget.dir, entityTarget.glob)) {
		if filepath.Base(f) == "_base.yaml" {
			continue
		}
		if _, err := loadYAML(f); err != nil {
			return nil, err
		}
		id := entityIDs.FileID(filepath.Base(f))
		if id == "" {
			continue
		}
		entities[id] = &entitySet{queries: map[string]bool{}, capabilities: map[string]bool{}}
		entityFiles[id] = rel(root, f)
	}
	queryIDs := contractspec.Targets{ID: queryTarget.id}
	seenQueries := map[string]string{}
	for _, f := range glob(root, filepath.Join(queryTarget.dir, queryTarget.glob)) {
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		fileID := queryIDs.FileID(filepath.Base(f))
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
			for _, e := range returnsEntities(m) {
				if es := entities[e]; es != nil {
					es.queries[qid] = true
				}
			}
		}
	}
	for _, f := range walkYAML(root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		capabilityID, _ := doc["id"].(string)
		if capabilityID == "" {
			continue
		}
		for _, entityID := range capabilityEntityRefs(doc) {
			if es := entities[entityID]; es != nil {
				es.capabilities[capabilityID] = true
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
		snap.Entities[id] = EntityUsedBy{
			Queries:      sortedKeys(es.queries),
			Capabilities: sortedKeys(es.capabilities),
		}
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
		e := s.Entities[id]
		ok, err := syncEntityUsedByFile(filepath.Join(root, file), e.Queries, e.Capabilities)
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
		gotQueries, gotCapabilities, ok := entityUsedBy(doc)
		want := s.Entities[id]
		if !ok {
			drift = append(drift, fmt.Sprintf("%s: used_by must contain queries/capabilities only", file))
		} else if !sameSet(gotQueries, want.Queries) {
			drift = append(drift, fmt.Sprintf("%s: used_by.queries must be %v, got %v", file, want.Queries, gotQueries))
		} else if !sameSet(gotCapabilities, want.Capabilities) {
			drift = append(drift, fmt.Sprintf("%s: used_by.capabilities must be %v, got %v", file, want.Capabilities, gotCapabilities))
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

func syncEntityUsedByFile(path string, queries, capabilities []string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	gotQueries, gotCapabilities, ok := entityUsedBy(doc)
	if ok && sameSet(gotQueries, queries) && sameSet(gotCapabilities, capabilities) {
		return false, nil
	}
	out, err := applyUsedBy(src, renderEntityUsedBy(queries, capabilities))
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
	renderList(&b, "queries", queries)
	return b.String()
}

func renderEntityUsedBy(queries, capabilities []string) string {
	var b strings.Builder
	b.WriteString("used_by:\n")
	renderList(&b, "queries", queries)
	renderList(&b, "capabilities", capabilities)
	return b.String()
}

func renderList(b *strings.Builder, key string, values []string) {
	if len(values) == 0 {
		fmt.Fprintf(b, "  %s: []\n", key)
		return
	}
	fmt.Fprintf(b, "  %s:\n", key)
	for _, v := range values {
		fmt.Fprintf(b, "    - %s\n", v)
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

func usedByQueries(doc map[string]any) []string {
	ub, _ := asMap(doc["used_by"])
	return stringList(ub["queries"])
}

// entityUsedBy 读取 entity 的 used_by；必须同时包含 queries/capabilities，且不得有其他键。
func entityUsedBy(doc map[string]any) ([]string, []string, bool) {
	ub, ok := asMap(doc["used_by"])
	if !ok {
		return nil, nil, false
	}
	if _, ok := ub["queries"]; !ok {
		return nil, nil, false
	}
	if _, ok := ub["capabilities"]; !ok {
		return nil, nil, false
	}
	for k := range ub {
		if k != "queries" && k != "capabilities" {
			return nil, nil, false
		}
	}
	return stringList(ub["queries"]), stringList(ub["capabilities"]), true
}

// capabilityEntityRefs 收集 capability 中 type/x-entity/returns 的实体引用。
func capabilityEntityRefs(doc map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	collectEntityRefs(doc, func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	})
	return out
}

func collectEntityRefs(v any, fn func(string)) {
	if m, ok := v.(map[string]any); ok {
		for k, child := range m {
			if k == "type" || k == "x-entity" || k == "returns" {
				if s, ok := child.(string); ok {
					if id, ok := entityRef(s); ok {
						fn(id)
					}
				}
			}
			collectEntityRefs(child, fn)
		}
		return
	}
	for _, item := range asList(v) {
		collectEntityRefs(item, fn)
	}
}

func entityRef(s string) (string, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "*")
	if strings.HasPrefix(s, "List<") && strings.HasSuffix(s, ">") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "List<"), ">")
	}
	if strings.HasPrefix(s, "data.entities.") {
		return s, true
	}
	return "", false
}

func walkYAML(root, dir string) []string {
	var files []string
	_ = filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
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

// queryTableRefs 读取查询的 tables。
func queryTableRefs(m map[string]any) []string {
	var out []string
	for _, raw := range asList(m["tables"]) {
		if s, ok := raw.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func returnsEntities(m map[string]any) []string {
	var out []string
	for _, raw := range asList(m["returns"]) {
		r, ok := asMap(raw)
		if !ok {
			continue
		}
		kind, _ := r["kind"].(string)
		entity, _ := r["entity"].(string)
		if kind == "entity" && entity != "" {
			out = append(out, entity)
		}
	}
	return out
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
