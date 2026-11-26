// Package graph 计算 query 入边并写回 table/model/query 的反向索引。
//
// gen-graph 维护生成物 table.entities、table.references/referenced_by、table.queries、
// model.includes/included_by/queries 与 query.used_by；checker 负责双向对账。
package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/yourorg/web-seed/scripts/cmd/internal/check"
	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

type TableUsedBy struct {
	Entities     []string
	Queries      []string
	References   []string
	ReferencedBy []string
}

type EntityRelation struct {
	Field       string
	Entity      string
	Cardinality string
}

func relationKey(entity, field string) string {
	return entity + "\x00" + field
}

type EntityUsedBy struct {
	Queries    []string
	Includes   []EntityRelation
	IncludedBy []EntityRelation
}

type Snapshot struct {
	Tables      map[string]TableUsedBy
	Entities    map[string]EntityUsedBy
	Queries     map[string][]string // query ID → service item IDs
	Enums       map[string][]string // enum ID → entity/capability IDs
	Validations map[string][]string // validation ID → service item IDs
	Constraints map[string][]string // constraint ID → query IDs
	TableFiles  map[string]string
	EntityFiles map[string]string
	QueryFiles  map[string][]string // query file → query IDs in item order
	EnumFiles   map[string][]string // enum file → enum IDs in item order
	Anchors     *Anchors
}

type tableSet struct {
	entities     map[string]bool
	queries      map[string]bool
	references   map[string]bool
	referencedBy map[string]bool
}

type entitySet struct {
	queries    map[string]bool
	includes   map[string]EntityRelation
	includedBy map[string]EntityRelation
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
			Contract string `yaml:"contract"`
			Targets  struct {
				Dir  string `yaml:"dir"`
				Glob string `yaml:"glob"`
				ID   string `yaml:"id"`
			} `yaml:"targets"`
		}
		if err := yaml.Unmarshal(b, &head); err != nil {
			return scanTarget{}, fmt.Errorf("%s: %w", file, err)
		}
		if head.Contract != typ {
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
	validationServices := map[string]map[string]bool{}
	constraintQueries := map[string]map[string]bool{}

	tableIDTemplate := contractspec.Targets{ID: tableTarget.id}
	tableDocs := map[string]map[string]any{}
	tableUnique := map[string]map[string]bool{}
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
		tables[id] = &tableSet{entities: map[string]bool{}, queries: map[string]bool{}, references: map[string]bool{}, referencedBy: map[string]bool{}}
		tableFiles[id] = rel(root, f)
		unique := map[string]bool{"id": true}
		for _, raw := range asList(doc["indexes"]) {
			idx, _ := asMap(raw)
			uniqueFlag, _ := idx["unique"].(bool)
			liveOnly, _ := idx["live_only"].(bool)
			cols := stringList(idx["columns"])
			if uniqueFlag && !liveOnly && len(cols) == 1 {
				unique[cols[0]] = true
			}
		}
		tableUnique[id] = unique
	}
	// constraints：先登记全部声明的约束，再接收 query.constraints 入边。
	for id, doc := range tableDocs {
		for _, raw := range asList(doc["constraints"]) {
			m, _ := asMap(raw)
			name, _ := m["name"].(string)
			if name != "" {
				constraintQueries[id+"."+name] = map[string]bool{}
			}
		}
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
				tables[id].references[target] = true
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
		entities[id] = &entitySet{queries: map[string]bool{}, includes: map[string]EntityRelation{}, includedBy: map[string]EntityRelation{}}
		entityFiles[id] = rel(root, f)
		model, _ := asMap(doc["model"])
		for _, table := range stringList(model["tables"]) {
			ts := tables[table]
			if ts == nil {
				return nil, fmt.Errorf("%s: model.tables %s is not a data.tables contract", entityFiles[id], table)
			}
			ts.entities[id] = true
		}
	}
	// validation：先登记全部声明的校验，再接收 service step.validations 入边。
	for id, doc := range entityDocs {
		for _, raw := range asList(doc["validation"]) {
			m, _ := asMap(raw)
			name, _ := m["name"].(string)
			if name != "" {
				validationServices[id+"."+name] = map[string]bool{}
			}
		}
	}
	// model.included_by：把关系字段的来源实体登记到目标实体。
	for id, doc := range entityDocs {
		for _, raw := range asList(doc["fields"]) {
			field, _ := asMap(raw)
			typ, _ := field["type"].(string)
			targetID := entityTypeID(typ)
			if targetID == "" {
				continue
			}
			src, _ := asMap(field["source"])
			if _, ok := asMap(src["join"]); !ok {
				continue
			}
			target := entities[targetID]
			if target == nil {
				name, _ := field["name"].(string)
				return nil, fmt.Errorf("%s: fields.%s type %s is not a data.entities contract", entityFiles[id], name, typ)
			}
			card := relationCardinalityFromDoc(field, tableUnique)
			if card == "" {
				continue
			}
			fieldName, _ := field["name"].(string)
			key := relationKey(targetID, fieldName)
			target.includedBy[relationKey(id, fieldName)] = EntityRelation{Field: fieldName, Entity: id, Cardinality: inverseCardinality(card)}
			entities[id].includes[key] = EntityRelation{Field: fieldName, Entity: targetID, Cardinality: card}
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
			for _, rawC := range asList(m["constraints"]) {
				cid, _ := rawC.(string)
				if cid == "" {
					continue
				}
				if constraintQueries[cid] == nil {
					return nil, fmt.Errorf("%s: constraints %s is not a data.tables.*.ck_* constraint", fileRel, cid)
				}
				constraintQueries[cid][qid] = true
			}
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
				var qids []string
				if qid, _ := step["query"].(string); qid != "" {
					qids = append(qids, qid)
				}
				for _, rawQ := range asList(step["queries"]) {
					qo, _ := asMap(rawQ)
					if qid, _ := qo["ref"].(string); qid != "" {
						qids = append(qids, qid)
					}
				}
				stepID, _ := step["id"].(string)
				for _, rawV := range asList(step["validations"]) {
					vid, _ := rawV.(string)
					if vid == "" {
						continue
					}
					if validationServices[vid] == nil {
						return nil, fmt.Errorf("%s: validations %s is not a data.entities.* validation", rel(root, f), vid)
					}
					if stepID != "" {
						validationServices[vid][serviceID+"."+stepID] = true
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
		Validations: map[string][]string{},
		Constraints: map[string][]string{},
		TableFiles:  tableFiles,
		EntityFiles: entityFiles,
		QueryFiles:  queryFiles,
	}
	for id, ts := range tables {
		snap.Tables[id] = TableUsedBy{Entities: sortedKeys(ts.entities), Queries: sortedKeys(ts.queries), References: sortedKeys(ts.references), ReferencedBy: sortedKeys(ts.referencedBy)}
	}
	for id, es := range entities {
		snap.Entities[id] = EntityUsedBy{Queries: sortedKeys(es.queries), Includes: sortedRelations(es.includes), IncludedBy: sortedRelations(es.includedBy)}
	}
	for id, qs := range queryServices {
		snap.Queries[id] = sortedKeys(qs)
	}
	for id, ss := range validationServices {
		snap.Validations[id] = sortedKeys(ss)
	}
	for id, qs := range constraintQueries {
		snap.Constraints[id] = sortedKeys(qs)
	}
	return snap, nil
}

// AddEnums 计算枚举 used_by 并登记 enum 文件。
// 只在完整仓库上调用：它需要 checker 解析 entity/capability 的引用。
func (s *Snapshot) AddEnums(root string) error {
	enumTarget, err := resolveTarget(root, "data.enums")
	if err != nil {
		return err
	}
	usedBy, err := check.EnumUsedBy(root)
	if err != nil {
		return err
	}
	files := map[string][]string{}
	for _, f := range glob(root, filepath.Join(enumTarget.dir, enumTarget.glob)) {
		doc, err := loadYAML(f)
		if err != nil {
			return err
		}
		relFile := rel(root, f)
		var ids []string
		for _, raw := range asList(doc["enums"]) {
			m, _ := asMap(raw)
			if name, _ := m["name"].(string); name != "" {
				ids = append(ids, "data.enums."+name)
			}
		}
		files[relFile] = ids
	}
	s.Enums = usedBy
	s.EnumFiles = files
	return nil
}

// Write 把 table.queries / model.queries / query.used_by / method.used_by.flows 写回文件。
func (s *Snapshot) Write(root string) ([]string, error) {
	var changed []string
	for _, id := range sortedFileKeys(s.TableFiles) {
		file := s.TableFiles[id]
		ok, err := syncNestedListFile(filepath.Join(root, file), "table", "extends", "entities", s.Tables[id].Entities)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
		ok, err = syncNestedListFile(filepath.Join(root, file), "table", "entities", "references", s.Tables[id].References)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
		ok, err = syncNestedListFile(filepath.Join(root, file), "table", "references", "referenced_by", s.Tables[id].ReferencedBy)
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
	for _, id := range sortedFileKeys(s.TableFiles) {
		file := s.TableFiles[id]
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return changed, err
		}
		for _, raw := range asList(doc["constraints"]) {
			m, _ := asMap(raw)
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			ok, err := syncItemFieldBlock(filepath.Join(root, file), "constraints", "name", name, "used_by", s.Constraints[id+"."+name])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
		}
	}
	for _, id := range sortedFileKeys(s.EntityFiles) {
		file := s.EntityFiles[id]
		ok, err := syncNestedRelationListFile(filepath.Join(root, file), "model", "tables", "includes", s.Entities[id].Includes, true)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, file)
		}
		ok, err = syncNestedRelationListFile(filepath.Join(root, file), "model", "includes", "included_by", s.Entities[id].IncludedBy, false)
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
	for _, id := range sortedFileKeys(s.EntityFiles) {
		file := s.EntityFiles[id]
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return changed, err
		}
		for _, raw := range asList(doc["validation"]) {
			m, _ := asMap(raw)
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			ok, err := syncItemFieldBlock(filepath.Join(root, file), "validation", "name", name, "used_by", s.Validations[id+"."+name])
			if err != nil {
				return changed, err
			}
			if ok {
				changed = append(changed, file)
			}
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
	for _, file := range sortedQueryFiles(s.EnumFiles) {
		ids := s.EnumFiles[file]
		usedBy := make([][]string, len(ids))
		for i, id := range ids {
			usedBy[i] = s.Enums[id]
		}
		ok, err := syncEnumUsedByFile(filepath.Join(root, file), usedBy)
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
		gotEntities, entitiesPresent := nestedListValue(doc, "table", "entities")
		wantEntities := s.Tables[id].Entities
		if !entitiesPresent || !sameSet(gotEntities, wantEntities) {
			drift = append(drift, fmt.Sprintf("%s: table.entities must be %v, got %v", file, wantEntities, gotEntities))
		}
		gotRefsOut, refsOutPresent := nestedListValue(doc, "table", "references")
		wantRefsOut := s.Tables[id].References
		if !refsOutPresent || !sameSet(gotRefsOut, wantRefsOut) {
			drift = append(drift, fmt.Sprintf("%s: table.references must be %v, got %v", file, wantRefsOut, gotRefsOut))
		}
		gotRefs, refsPresent := nestedListValue(doc, "table", "referenced_by")
		wantRefs := s.Tables[id].ReferencedBy
		if !refsPresent || !sameSet(gotRefs, wantRefs) {
			drift = append(drift, fmt.Sprintf("%s: table.referenced_by must be %v, got %v", file, wantRefs, gotRefs))
		}
		got, queriesPresent := nestedListValue(doc, "table", "queries")
		want := s.Tables[id].Queries
		if !queriesPresent || !sameSet(got, want) {
			drift = append(drift, fmt.Sprintf("%s: table.queries must be %v, got %v", file, want, got))
		}
		for _, raw := range asList(doc["constraints"]) {
			m, _ := asMap(raw)
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			gotUsed := stringList(m["used_by"])
			wantUsed := s.Constraints[id+"."+name]
			if !sameSet(gotUsed, wantUsed) {
				drift = append(drift, fmt.Sprintf("%s: constraints[%s].used_by must be %v, got %v", file, name, wantUsed, gotUsed))
			}
		}
	}
	for _, id := range sortedFileKeys(s.EntityFiles) {
		file := s.EntityFiles[id]
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		gotIncludes, includesPresent := nestedRelationListValue(doc, "model", "includes")
		wantIncludes := s.Entities[id].Includes
		if !includesPresent || !sameRelations(gotIncludes, wantIncludes) {
			drift = append(drift, fmt.Sprintf("%s: model.includes must be %v, got %v", file, formatRelations(wantIncludes), formatRelations(gotIncludes)))
		}
		gotIncluded, includedPresent := nestedRelationListValue(doc, "model", "included_by")
		wantIncluded := s.Entities[id].IncludedBy
		if !includedPresent || !sameRelations(gotIncluded, wantIncluded) {
			drift = append(drift, fmt.Sprintf("%s: model.included_by must be %v, got %v", file, formatRelations(wantIncluded), formatRelations(gotIncluded)))
		}
		got, queriesPresent := nestedListValue(doc, "model", "queries")
		want := s.Entities[id].Queries
		if !queriesPresent || !sameSet(got, want) {
			drift = append(drift, fmt.Sprintf("%s: model.queries must be %v, got %v", file, want, got))
		}
		for _, raw := range asList(doc["validation"]) {
			m, _ := asMap(raw)
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			gotUsed := stringList(m["used_by"])
			wantUsed := s.Validations[id+"."+name]
			if !sameSet(gotUsed, wantUsed) {
				drift = append(drift, fmt.Sprintf("%s: validation[%s].used_by must be %v, got %v", file, name, wantUsed, gotUsed))
			}
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
	for _, file := range sortedQueryFiles(s.EnumFiles) {
		doc, err := loadYAML(filepath.Join(root, file))
		if err != nil {
			return nil, err
		}
		ids := s.EnumFiles[file]
		got := enumItemUsedBy(doc)
		if len(got) != len(ids) {
			drift = append(drift, fmt.Sprintf("%s: enum count changed, run make gen-graph", file))
			continue
		}
		for i, id := range ids {
			want := s.Enums[id]
			if !sameSet(got[i], want) {
				drift = append(drift, fmt.Sprintf("%s: %s.used_by must be %v, got %v", file, id, want, got[i]))
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
	cur, present := nestedListValue(doc, parentKey, childKey)
	if present && sameSet(cur, values) {
		return false, nil
	}
	out, err := applyNestedList(src, parentKey, afterKey, childKey, values)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, writeAtomic(path, out)
}

// syncNestedRelationListFile 写入关系对象列表；用 flow mapping 保持一行一项。
// fieldFirst=true 渲染 { field, entity, cardinality }（includes）；false 渲染 { entity, field, cardinality }（included_by）。
func syncNestedRelationListFile(path, parentKey, afterKey, childKey string, values []EntityRelation, fieldFirst bool) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if cur, present := nestedRelationListValue(doc, parentKey, childKey); present && sameRelations(cur, values) {
		return false, nil
	}
	items := make([]string, len(values))
	for i, v := range values {
		if fieldFirst {
			items[i] = fmt.Sprintf("{ field: %s, entity: %s, cardinality: %s }", v.Field, v.Entity, v.Cardinality)
		} else {
			items[i] = fmt.Sprintf("{ entity: %s, field: %s, cardinality: %s }", v.Entity, v.Field, v.Cardinality)
		}
	}
	out, err := applyNestedList(src, parentKey, afterKey, childKey, items)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, writeAtomic(path, out)
}

func nestedRelationListValue(doc map[string]any, parentKey, childKey string) ([]EntityRelation, bool) {
	p, ok := asMap(doc[parentKey])
	if !ok {
		return nil, false
	}
	raw, ok := p[childKey]
	if !ok {
		return nil, false
	}
	var out []EntityRelation
	for _, item := range asList(raw) {
		m, _ := asMap(item)
		out = append(out, EntityRelation{
			Field:       stringValue(m["field"]),
			Entity:      stringValue(m["entity"]),
			Cardinality: stringValue(m["cardinality"]),
		})
	}
	return out, true
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func sameRelations(a, b []EntityRelation) bool {
	if len(a) != len(b) {
		return false
	}
	index := map[string]string{}
	for _, r := range a {
		index[relationKey(r.Entity, r.Field)] = r.Cardinality
	}
	for _, r := range b {
		if index[relationKey(r.Entity, r.Field)] != r.Cardinality {
			return false
		}
	}
	return true
}

func formatRelations(values []EntityRelation) string {
	out := make([]string, len(values))
	for i, r := range values {
		out[i] = r.Entity + "." + r.Field + "(" + r.Cardinality + ")"
	}
	return "[" + strings.Join(out, ", ") + "]"
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
		out, err = applyListItemUsedBy(out, "queries", "archive", i, usedBy[i])
		if err != nil {
			return false, fmt.Errorf("%s: %w", path, err)
		}
	}
	return true, writeAtomic(path, out)
}

func syncEnumUsedByFile(path string, usedBy [][]string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	got := enumItemUsedBy(doc)
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
		out, err = applyListItemUsedBy(out, "enums", "values", i, usedBy[i])
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

// nestedListValue 读取 table/model 下的列表字段，并区分“缺失”与“空列表”。
func nestedListValue(doc map[string]any, parentKey, childKey string) ([]string, bool) {
	p, ok := asMap(doc[parentKey])
	if !ok {
		return nil, false
	}
	raw, ok := p[childKey]
	if !ok {
		return nil, false
	}
	return stringList(raw), true
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

func enumItemUsedBy(doc map[string]any) [][]string {
	var out [][]string
	for _, raw := range asList(doc["enums"]) {
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

// applyListItemUsedBy 在 parentKey[itemIndex] 的 item 内替换或插入 used_by。
func applyListItemUsedBy(src []byte, parentKey, afterKey string, itemIndex int, values []string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, fmt.Errorf("empty document")
	}
	root := doc.Content[0]
	_, seqEnd, seq, ok := topLevelNode(root, parentKey, totalLines(src))
	if !ok || seq.Kind != yaml.SequenceNode || itemIndex >= len(seq.Content) {
		return nil, fmt.Errorf("missing %s[%d]", parentKey, itemIndex)
	}
	item := seq.Content[itemIndex]
	if item.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s[%d] must be a mapping", parentKey, itemIndex)
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
	return insertLines(lines, mappingInsertLine(item, afterKey, itemEnd), block), nil
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
		entity, _ := r["type"].(string)
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

func sortedRelations(set map[string]EntityRelation) []EntityRelation {
	out := make([]EntityRelation, 0, len(set))
	for _, rel := range set {
		out = append(out, rel)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Entity != out[j].Entity {
			return out[i].Entity < out[j].Entity
		}
		return out[i].Field < out[j].Field
	})
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

// relationCardinalityFromDoc 从实体字段与表唯一索引推导关系基数；through 固定 many_to_many。
func relationCardinalityFromDoc(field map[string]any, tableUnique map[string]map[string]bool) string {
	src, _ := asMap(field["source"])
	join, _ := asMap(src["join"])
	if join == nil {
		return ""
	}
	if _, ok := asMap(join["through"]); ok {
		return "many_to_many"
	}
	from, _ := join["from"].(string)
	to, _ := join["to"].(string)
	fromTable, fromCol, ok1 := splitTableColumnRef(from)
	toTable, toCol, ok2 := splitTableColumnRef(to)
	if !ok1 || !ok2 {
		return ""
	}
	fromUnique := tableUnique[fromTable][fromCol]
	toUnique := tableUnique[toTable][toCol]
	switch {
	case fromUnique && toUnique:
		return "one_to_one"
	case fromUnique:
		return "one_to_many"
	case toUnique:
		return "many_to_one"
	}
	return ""
}

func inverseCardinality(card string) string {
	switch card {
	case "one_to_many":
		return "many_to_one"
	case "many_to_one":
		return "one_to_many"
	}
	return card
}

// entityTypeID 从实体字段 type 提取目标 entity ID，兼容 *T、List<T> 与 _types.structures.list<T>。
func entityTypeID(typ string) string {
	typ = strings.TrimSpace(typ)
	typ = strings.TrimPrefix(typ, "*")
	if strings.HasPrefix(typ, "_types.structures.list<") && strings.HasSuffix(typ, ">") {
		typ = "List<" + strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(typ, "_types.structures.list<"), ">")) + ">"
	}
	if strings.HasPrefix(typ, "List<") && strings.HasSuffix(typ, ">") {
		typ = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(typ, "List<"), ">"))
		typ = strings.TrimPrefix(typ, "*")
	}
	if strings.HasPrefix(typ, "data.entities.") {
		return typ
	}
	return ""
}
