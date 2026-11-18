// Package graph 计算 query 入边并写回 table/model/query 的反向索引。
//
// table.entities 与 model.included_by 是显式映射，由 checker 校验；
// gen-graph 维护生成物 table.queries、model.queries 与 query.used_by。
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
	Queries      []string
	ReferencedBy []string
}

type EntityUsedBy struct {
	Queries    []string
	IncludedBy []string
}

type Snapshot struct {
	Tables      map[string]TableUsedBy
	Entities    map[string]EntityUsedBy
	Queries     map[string][]string // query ID → service item IDs
	TableFiles  map[string]string
	EntityFiles map[string]string
	QueryFiles  map[string][]string // query file → query IDs in item order
	Anchors     *Anchors
}

type tableSet struct {
	queries      map[string]bool
	referencedBy map[string]bool
}

type entitySet struct {
	queries    map[string]bool
	includedBy map[string]bool
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

// Compute 扫描 tables/entities/queries/methods，计算反向索引。
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
	serviceTarget, err := resolveTarget(root, "cap.services")
	if err != nil {
		return nil, err
	}
	for name, target := range map[string]scanTarget{
		"data.tables": tableTarget, "data.entities": entityTarget, "data.queries": queryTarget,
		"cap.services": serviceTarget,
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
	queryFiles := map[string][]string{}
	queryServices := map[string]map[string]bool{}

	tableIDTemplate := contractspec.Targets{ID: tableTarget.id}
	tableDocs := map[string]map[string]any{}
	for _, f := range glob(root, filepath.Join(tableTarget.dir, tableTarget.glob)) {
		if filepath.Base(f) == "_base.yaml" {
			continue
		}
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		id := tableIDTemplate.FileID(filepath.Base(f))
		if id == "" {
			continue
		}
		tableDocs[id] = doc
		tables[id] = &tableSet{queries: map[string]bool{}, referencedBy: map[string]bool{}}
		tableFiles[id] = rel(root, f)
	}
	// table.referenced_by：把每条列级 references 的源列登记到目标表。
	for id, doc := range tableDocs {
		for _, raw := range asList(doc["columns"]) {
			col, _ := asMap(raw)
			ref, _ := asMap(col["references"])
			target, _ := ref["column"].(string)
			targetTable, _, ok := splitTableColumnRef(target)
			if !ok {
				continue
			}
			name, _ := col["name"].(string)
			if name == "" {
				continue
			}
			if ts := tables[targetTable]; ts != nil {
				ts.referencedBy[id+".col."+name] = true
			} else {
				return nil, fmt.Errorf("%s: columns.%s.references target %s is not a data.tables contract", tableFiles[id], name, target)
			}
		}
	}
	entityIDTemplate := contractspec.Targets{ID: entityTarget.id}
	entityDocs := map[string]map[string]any{}
	for _, f := range glob(root, filepath.Join(entityTarget.dir, entityTarget.glob)) {
		if filepath.Base(f) == "_base.yaml" {
			continue
		}
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		id := entityIDTemplate.FileID(filepath.Base(f))
		if id == "" {
			continue
		}
		entityDocs[id] = doc
		entities[id] = &entitySet{queries: map[string]bool{}, includedBy: map[string]bool{}}
		entityFiles[id] = rel(root, f)
	}
	// model.included_by：把关系字段的来源实体登记到目标实体。
	for id, doc := range entityDocs {
		for _, raw := range asList(doc["fields"]) {
			field, _ := asMap(raw)
			typ, _ := field["type"].(string)
			if !strings.HasPrefix(typ, "data.entities.") {
				continue
			}
			src, _ := asMap(field["source"])
			if _, ok := asMap(src["join"]); !ok {
				continue
			}
			target := entities[typ]
			if target == nil {
				name, _ := field["name"].(string)
				return nil, fmt.Errorf("%s: fields.%s type %s is not a data.entities contract", entityFiles[id], name, typ)
			}
			target.includedBy[id] = true
		}
	}
	queryIDTemplate := contractspec.Targets{ID: queryTarget.id}
	seenQueries := map[string]string{}
	for _, f := range glob(root, filepath.Join(queryTarget.dir, queryTarget.glob)) {
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		fileID := queryIDTemplate.FileID(filepath.Base(f))
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
			fileRel := rel(root, f)
			queryFiles[fileRel] = append(queryFiles[fileRel], qid)
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

	for _, f := range glob(root, filepath.Join(serviceTarget.dir, serviceTarget.glob)) {
		doc, err := loadYAML(f)
		if err != nil {
			return nil, err
		}
		fileID, _ := doc["id"].(string)
		if fileID == "" {
			continue
		}
		for _, raw := range asList(doc["services"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			serviceID := fileID + "." + name
			for _, rawStep := range asList(item["steps"]) {
				step, _ := asMap(rawStep)
				kind, _ := step["kind"].(string)
				var qids []string
				switch kind {
				case "query":
					qid, _ := step["query"].(string)
					if qid != "" {
						qids = append(qids, qid)
					}
				case "logic", "transition":
					for _, rawQ := range asList(step["queries"]) {
						qo, _ := asMap(rawQ)
						if qid, _ := qo["query"].(string); qid != "" {
							qids = append(qids, qid)
						}
					}
				}
				for _, qid := range qids {
					if queryServices[qid] == nil {
						queryServices[qid] = map[string]bool{}
					}
					queryServices[qid][serviceID] = true
				}
			}
		}
	}

	anchors, err := computeAnchors(root, serviceTarget)
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{
		Anchors:     anchors,
		Tables:      map[string]TableUsedBy{},
		Entities:    map[string]EntityUsedBy{},
		Queries:     map[string][]string{},
		TableFiles:  tableFiles,
		EntityFiles: entityFiles,
		QueryFiles:  queryFiles,
	}
	for id, ts := range tables {
		snap.Tables[id] = TableUsedBy{Queries: sortedKeys(ts.queries), ReferencedBy: sortedKeys(ts.referencedBy)}
	}
	for id, es := range entities {
		snap.Entities[id] = EntityUsedBy{Queries: sortedKeys(es.queries), IncludedBy: sortedKeys(es.includedBy)}
	}
	for id, qs := range queryServices {
		snap.Queries[id] = sortedKeys(qs)
	}
	return snap, nil
}

// Write 把 table.queries / model.queries / query.used_by / method.used_by.flows 写回文件。
func (s *Snapshot) Write(root string) ([]string, error) {
	var changed []string
	for _, id := range sortedFileKeys(s.TableFiles) {
		file := s.TableFiles[id]
		ok, err := syncNestedListFile(filepath.Join(root, file), "table", "entities", "referenced_by", s.Tables[id].ReferencedBy)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
		ok, err = syncNestedListFile(filepath.Join(root, file), "table", "referenced_by", "queries", s.Tables[id].Queries)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
	}
	for _, id := range sortedFileKeys(s.EntityFiles) {
		file := s.EntityFiles[id]
		ok, err := syncNestedListFile(filepath.Join(root, file), "model", "tables", "included_by", s.Entities[id].IncludedBy)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
		ok, err = syncNestedListFile(filepath.Join(root, file), "model", "included_by", "queries", s.Entities[id].Queries)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
	}
	for _, file := range sortedQueryFiles(s.QueryFiles) {
		ids := s.QueryFiles[file]
		usedBy := make([][]string, len(ids))
		for i, qid := range ids {
			usedBy[i] = s.Queries[qid]
		}
		ok, err := syncQueryUsedByFile(filepath.Join(root, file), usedBy)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
	}
	if s.Anchors != nil {
		files, err := s.Anchors.Write(root)
		if err != nil {
			return changed, err
		}
		changed = append(changed, files...)
	}
	return changed, nil
}

// Check 返回与计算结果不一致的反向索引；不写文件。
func (s *Snapshot) Check(root string) ([]string, error) {
	var drift []string
	for _, id := range sortedFileKeys(s.TableFiles) {
		file := s.TableFiles[id]
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		gotRefs := nestedList(doc, "table", "referenced_by")
		wantRefs := s.Tables[id].ReferencedBy
		if !sameSet(gotRefs, wantRefs) {
			drift = append(drift, fmt.Sprintf("%s: table.referenced_by must be %v, got %v", file, wantRefs, gotRefs))
		}
		got := nestedList(doc, "table", "queries")
		want := s.Tables[id].Queries
		if !sameSet(got, want) {
			drift = append(drift, fmt.Sprintf("%s: table.queries must be %v, got %v", file, want, got))
		}
	}
	for _, id := range sortedFileKeys(s.EntityFiles) {
		file := s.EntityFiles[id]
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		gotIncluded := nestedList(doc, "model", "included_by")
		wantIncluded := s.Entities[id].IncludedBy
		if !sameSet(gotIncluded, wantIncluded) {
			drift = append(drift, fmt.Sprintf("%s: model.included_by must be %v, got %v", file, wantIncluded, gotIncluded))
		}
		got := nestedList(doc, "model", "queries")
		want := s.Entities[id].Queries
		if !sameSet(got, want) {
			drift = append(drift, fmt.Sprintf("%s: model.queries must be %v, got %v", file, want, got))
		}
	}
	for _, file := range sortedQueryFiles(s.QueryFiles) {
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		ids := s.QueryFiles[file]
		got := queryItemUsedBy(doc)
		if len(got) != len(ids) {
			drift = append(drift, fmt.Sprintf("%s: query count changed, run make gen-graph", file))
			continue
		}
		for i, qid := range ids {
			want := s.Queries[qid]
			if !sameSet(got[i], want) {
				drift = append(drift, fmt.Sprintf("%s: %s.used_by must be %v, got %v", file, qid, want, got[i]))
			}
		}
	}
	if s.Anchors != nil {
		anchors, err := s.Anchors.Check(root)
		if err != nil {
			return nil, err
		}
		drift = append(drift, anchors...)
	}
	return drift, nil
}

func syncNestedListFile(path, parentKey, afterKey, childKey string, values []string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if sameSet(nestedList(doc, parentKey, childKey), values) {
		return false, nil
	}
	out, err := applyNestedList(src, parentKey, afterKey, childKey, values)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, writeAtomic(path, out)
}

func syncQueryUsedByFile(path string, usedBy [][]string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	got := queryItemUsedBy(doc)
	if len(got) == len(usedBy) {
		ok := true
		for i := range got {
			if !sameSet(got[i], usedBy[i]) {
				ok = false
				break
			}
		}
		if ok {
			return false, nil
		}
	}
	out := src
	for i := len(usedBy) - 1; i >= 0; i-- {
		out, err = applyListItemUsedBy(out, i, usedBy[i])
		if err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
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

// nestedList 读取 table/model 下的列表字段。
func nestedList(doc map[string]any, parentKey, childKey string) []string {
	p, _ := asMap(doc[parentKey])
	return stringList(p[childKey])
}

// splitTableColumnRef 解析 data.tables.<t>.col.<c>。
func splitTableColumnRef(ref string) (table, col string, ok bool) {
	parts := strings.Split(ref, ".col.")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// queryItemUsedBy 按 queries 列表顺序返回每项的 used_by。
func queryItemUsedBy(doc map[string]any) [][]string {
	var out [][]string
	for _, raw := range asList(doc["queries"]) {
		m, _ := asMap(raw)
		out = append(out, stringList(m["used_by"]))
	}
	return out
}

// applyNestedList 在 parentKey 指向的 mapping 内替换或插入 childKey 列表。
func applyNestedList(src []byte, parentKey, afterKey, childKey string, values []string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty document")
	}
	root := doc.Content[0]
	_, parentEnd, parent, ok := topLevelNode(root, parentKey, totalLines(src))
	if !ok || parent.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("missing %s mapping", parentKey)
	}
	start, end, found := mappingKeyRange(parent, childKey, parentEnd)
	block := renderListBlock("  ", childKey, values)
	lines := strings.Split(strings.TrimSuffix(string(src), "\n"), "\n")
	if found {
		return replaceLines(lines, start, end, block), nil
	}
	return insertLines(lines, mappingInsertLine(parent, afterKey, parentEnd), block), nil
}

// applyListItemUsedBy 在 queries[itemIndex] 的 item 内替换或插入 used_by。
func applyListItemUsedBy(src []byte, itemIndex int, values []string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty document")
	}
	root := doc.Content[0]
	_, seqEnd, seq, ok := topLevelNode(root, "queries", totalLines(src))
	if !ok || seq.Kind != yaml.SequenceNode || itemIndex >= len(seq.Content) {
		return nil, fmt.Errorf("missing queries[%d]", itemIndex)
	}
	item := seq.Content[itemIndex]
	if item.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("queries[%d] must be a mapping", itemIndex)
	}
	itemEnd := seqEnd
	if itemIndex+1 < len(seq.Content) {
		if next := seq.Content[itemIndex+1].Line; next > item.Line {
			itemEnd = next
		}
	}
	start, end, found := mappingKeyRange(item, "used_by", itemEnd)
	block := renderListBlock("    ", "used_by", values)
	lines := strings.Split(strings.TrimSuffix(string(src), "\n"), "\n")
	if found {
		return replaceLines(lines, start, end, block), nil
	}
	return insertLines(lines, mappingInsertLine(item, "index_ref", itemEnd), block), nil
}

func topLevelNode(root *yaml.Node, key string, total int) (int, int, *yaml.Node, bool) {
	if root.Kind != yaml.MappingNode {
		return 0, 0, nil, false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		start := root.Content[i].Line
		end := total + 1
		if i+2 < len(root.Content) {
			if next := root.Content[i+2].Line; next > start {
				end = next
			}
		}
		return start, end, root.Content[i+1], true
	}
	return 0, 0, nil, false
}

func mappingKeyRange(m *yaml.Node, key string, parentEnd int) (int, int, bool) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		start := m.Content[i].Line
		end := parentEnd
		if i+2 < len(m.Content) {
			if next := m.Content[i+2].Line; next > start {
				end = next
			}
		}
		// 只替换该键的值块，保留其后的空行与注释。
		if valueEnd := blockEnd(m.Content[i+1], end); valueEnd > start && valueEnd <= end {
			end = valueEnd
		}
		return start, end, true
	}
	return 0, 0, false
}

func mappingInsertLine(m *yaml.Node, afterKey string, parentEnd int) int {
	if afterKey != "" {
		if _, end, ok := mappingKeyRange(m, afterKey, parentEnd); ok {
			return end
		}
	}
	if len(m.Content) >= 2 {
		return blockEnd(m.Content[len(m.Content)-1], parentEnd)
	}
	return m.Line + 1
}

func blockEnd(n *yaml.Node, fallback int) int {
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		if len(n.Content) == 0 {
			return n.Line + 1
		}
		return blockEnd(n.Content[len(n.Content)-1], fallback)
	case yaml.ScalarNode:
		return n.Line + strings.Count(n.Value, "\n") + 1
	default:
		return fallback
	}
}

func totalLines(src []byte) int {
	n := strings.Count(string(src), "\n")
	if !strings.HasSuffix(string(src), "\n") {
		n++
	}
	return n
}

func replaceLines(lines []string, start, end int, block []string) []byte {
	if start < 1 {
		start = 1
	}
	if end > len(lines)+1 {
		end = len(lines) + 1
	}
	before := append([]string{}, lines[:start-1]...)
	after := append([]string{}, lines[end-1:]...)
	out := append(before, block...)
	out = append(out, after...)
	return []byte(strings.Join(out, "\n") + "\n")
}

func insertLines(lines []string, at int, block []string) []byte {
	if at < 1 {
		at = 1
	}
	if at > len(lines)+1 {
		at = len(lines) + 1
	}
	for at > 1 && strings.TrimSpace(lines[at-2]) == "" {
		at--
	}
	before := append([]string{}, lines[:at-1]...)
	after := append([]string{}, lines[at-1:]...)
	out := append(before, block...)
	out = append(out, after...)
	return []byte(strings.Join(out, "\n") + "\n")
}

func renderListBlock(indent, key string, values []string) []string {
	if len(values) == 0 {
		return []string{indent + key + ": []"}
	}
	out := []string{indent + key + ":"}
	for _, v := range values {
		out = append(out, indent+"  - "+v)
	}
	return out
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

// queryTableRefs 读取查询的 tables；兼容旧 string 形状，生产契约使用 {table, write}。
func queryTableRefs(m map[string]any) []string {
	var out []string
	for _, raw := range asList(m["tables"]) {
		if s, ok := raw.(string); ok && s != "" {
			out = append(out, s)
			continue
		}
		tm, ok := asMap(raw)
		if !ok {
			continue
		}
		if s, _ := tm["table"].(string); s != "" {
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

func sortedQueryFiles(m map[string][]string) []string {
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
