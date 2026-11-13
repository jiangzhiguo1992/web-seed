// entities.go 校验 data.entities：实体到表的投影、字段、身份、关系、校验与反向索引。
//
// 实体是读模型：字段必须能投影回物理列；多表合成只读，JOIN 由 query 负责。
package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	baseEntityID   = "data.entities.base"
	baseEntityPath = "contracts/data/entities/_base.yaml"
)

// computed 表达式不得引用方法入参；查询参数属于 query 契约。
var sqlParamAtRe = regexp.MustCompile(`@([a-z_][a-z0-9_]*)`)
var sqlParamNargRe = regexp.MustCompile(`sqlc\.narg\('([a-z_][a-z0-9_]*)'\)`)
var sqlParamArgRe = regexp.MustCompile(`sqlc\.arg\('([a-z_][a-z0-9_]*)'\)`)
var entityValidationNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// columnMeta 是物理表的有效列元数据（含继承列）。
type columnMeta struct {
	typ         string
	nullable    bool
	primaryKey  bool
	sensitivity string
}

// entityFieldInfo 是实体字段（标量或关系）的解析结果。
type entityFieldInfo struct {
	Name            string
	ColumnTable     string
	Column          string
	SourceColumnRef string
	SourceComputed  string
	SourceSoft      string
	HasSource       bool
	HasType         bool
	Type            string
	ValidationKind  string
	ValidationRef   string
	ValidationCount int
	HasValidation   bool
	Sensitivity     string
	HasSensitivity  bool
	JSONName        string
	Description     string
	IsRelation      bool
	JoinFromRef     string
	JoinFromTable   string
	JoinFromColumn  string
	JoinToRef       string
	JoinToTable     string
	JoinToColumn    string
	Load            string
	HasLoad         bool
	FromBase        bool
	OverridesBase   bool
	BaseRedeclared  bool
}

// entityInfo 保存单个 entity 的解析结果，供跨文件检查复用。
type entityInfo struct {
	ID          string
	Rel         string
	Doc         map[string]any
	Name        string
	OwnerModule string
	Tables      []string
	Primary     string
	Fields      []entityFieldInfo
}

// entityDoc 保存原始文件，供结构校验使用。
type entityDoc struct {
	rel    string
	fileID string
	doc    map[string]any
}

func baseColumnMeta() map[string]columnMeta {
	return map[string]columnMeta{
		"id":         {typ: "integer", primaryKey: true},
		"status":     {typ: "integer"},
		"created_at": {typ: "timestamp"},
		"updated_at": {typ: "timestamp"},
		"deleted_at": {typ: "timestamp", nullable: true},
	}
}

func cloneColumnMeta(src map[string]columnMeta) map[string]columnMeta {
	out := make(map[string]columnMeta, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// loadTableColumns 构建每张物理表的有效列（含继承），并记录物理表名。
func (c *Checker) loadTableColumns(tableIDs map[string]string) {
	c.tableColumns = map[string]map[string]columnMeta{}
	docs := map[string]map[string]any{}
	for id, rel := range tableIDs {
		if id == c.baseID {
			continue
		}
		doc, err := loadYAML(filepath.Join(c.root, rel))
		if err != nil {
			continue
		}
		docs[id] = doc
	}
	base := baseColumnMeta()
	var resolve func(id string, stack map[string]bool) map[string]columnMeta
	resolve = func(id string, stack map[string]bool) map[string]columnMeta {
		out := cloneColumnMeta(base)
		if stack[id] {
			return out
		}
		stack[id] = true
		doc := docs[id]
		if doc != nil {
			for _, parent := range extendsList(doc) {
				if parent == c.baseID || docs[parent] == nil {
					continue
				}
				for name, meta := range resolve(parent, stack) {
					out[name] = meta
				}
			}
			for _, raw := range asList(doc["columns"]) {
				m, ok := asMap(raw)
				if !ok {
					continue
				}
				name, _ := m["name"].(string)
				if name == "" {
					continue
				}
				if _, isBase := base[name]; isBase {
					continue
				}
				typ, _ := m["type"].(string)
				if typ == "" {
					typ = "string"
				}
				meta := columnMeta{typ: typ}
				meta.nullable, _ = m["nullable"].(bool)
				meta.primaryKey, _ = m["primary_key"].(bool)
				meta.sensitivity, _ = m["sensitivity"].(string)
				meta.sensitivity = normalizeSensitivity(meta.sensitivity)
				out[name] = meta
			}
		}
		delete(stack, id)
		return out
	}
	for id := range docs {
		c.tableColumns[id] = resolve(id, map[string]bool{})
	}
}

func splitColumnRef(ref string) (table, col string, ok bool) {
	parts := strings.Split(ref, ".col.")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// loadEntities 读取 entity 文件并建立解析索引；结构错误在 checkEntities 中报。
func (c *Checker) loadEntities(schema *Schema) {
	c.entityDocs = nil
	c.entityBaseDoc = nil
	c.entityBaseFields = nil
	c.entities = map[string]*entityInfo{}
	c.entityOrder = nil
	if doc, err := loadYAML(filepath.Join(c.root, baseEntityPath)); err == nil {
		c.entityBaseDoc = doc
		c.entityBaseFields = parseEntityBaseFields(doc)
	}
	if schema.Targets.Dir == "" || schema.Targets.Glob == "" {
		return
	}
	files := glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))
	sort.Strings(files)
	for _, file := range files {
		if filepath.Base(file) == "_base.yaml" {
			continue
		}
		rel := relPath(c.root, file)
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		fileID := schema.Targets.FileID(filepath.Base(file))
		if fileID == "" {
			fileID = strings.TrimSuffix(rel, filepath.Ext(rel))
		}
		c.entityDocs = append(c.entityDocs, entityDoc{rel: rel, fileID: fileID, doc: doc})
		e := c.parseEntity(doc, rel)
		if e.ID == "" {
			continue
		}
		if _, exists := c.entities[e.ID]; !exists {
			c.entities[e.ID] = e
			c.entityOrder = append(c.entityOrder, e.ID)
		}
	}
}

func parseEntityBaseFields(doc map[string]any) []entityFieldInfo {
	var out []entityFieldInfo
	for _, raw := range asList(doc["fields"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		f := parseEntityField(m)
		if f == nil {
			continue
		}
		f.FromBase = true
		out = append(out, *f)
	}
	return out
}

// parseEntityField 解析字段的公共部分；标量/关系变体在 checkEntityFields 中收窄。
func parseEntityField(m map[string]any) *entityFieldInfo {
	name, _ := m["name"].(string)
	if name == "" {
		return nil
	}
	f := &entityFieldInfo{Name: name, Load: "lazy"}
	f.JSONName, _ = m["json_name"].(string)
	f.Description, _ = m["description"].(string)
	if v, ok := m["type"].(string); ok && v != "" {
		f.Type, f.HasType = v, true
	}
	if src, ok := asMap(m["source"]); ok {
		f.HasSource = true
		f.SourceColumnRef, _ = src["column"].(string)
		f.SourceComputed, _ = src["computed"].(string)
		f.SourceSoft, _ = src["soft"].(string)
		if t, col, ok := splitColumnRef(f.SourceColumnRef); ok {
			f.ColumnTable, f.Column = t, col
		}
	}
	if val, ok := asMap(m["validation"]); ok {
		f.HasValidation = true
		for _, kind := range []string{"column", "pattern", "soft"} {
			if v, ok := val[kind].(string); ok && v != "" {
				f.ValidationCount++
				f.ValidationKind, f.ValidationRef = kind, v
			}
		}
	}
	if v, ok := m["sensitivity"].(string); ok {
		f.Sensitivity, f.HasSensitivity = v, true
	}
	if v, ok := m["join_column_from"].(string); ok && v != "" {
		f.JoinFromRef, f.IsRelation = v, true
		if t, col, ok := splitColumnRef(v); ok {
			f.JoinFromTable, f.JoinFromColumn = t, col
		}
	}
	if v, ok := m["join_column_to"].(string); ok && v != "" {
		f.JoinToRef, f.IsRelation = v, true
		if t, col, ok := splitColumnRef(v); ok {
			f.JoinToTable, f.JoinToColumn = t, col
		}
	}
	if v, ok := m["load"].(string); ok && v != "" {
		f.Load, f.HasLoad = v, true
	}
	return f
}

func (c *Checker) parseEntity(doc map[string]any, rel string) *entityInfo {
	e := &entityInfo{Rel: rel, Doc: doc}
	e.ID, _ = doc["id"].(string)
	e.OwnerModule, _ = doc["owner_module"].(string)
	if model, ok := asMap(doc["model"]); ok {
		e.Name, _ = model["name"].(string)
		e.Tables = stringList(model["tables"])
	}
	if len(e.Tables) > 0 {
		e.Primary = e.Tables[0]
	}
	for _, raw := range asList(doc["fields"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		if f := parseEntityField(m); f != nil {
			e.Fields = append(e.Fields, *f)
		}
	}
	// 基类字段由 _base 继承；Status 允许用枚举类型特化。
	byName := map[string]*entityFieldInfo{}
	for i := range e.Fields {
		byName[e.Fields[i].Name] = &e.Fields[i]
	}
	var inherited []entityFieldInfo
	for _, bf := range c.entityBaseFields {
		if cf := byName[bf.Name]; cf != nil {
			if bf.Name == "Status" && isEnumType(cf.Type) {
				cf.OverridesBase = true
				// source/validation/description 必须显式；其余机械事实可继承基类。
				if !cf.HasType {
					cf.Type, cf.HasType = bf.Type, true
				}
				if cf.JSONName == "" {
					cf.JSONName = bf.JSONName
				}
			} else {
				cf.BaseRedeclared = true
			}
			continue
		}
		nf := bf
		if nf.Column != "" {
			nf.ColumnTable = e.Primary
		}
		inherited = append(inherited, nf)
	}
	e.Fields = append(inherited, e.Fields...)
	c.applyEntityFieldDefaults(e)
	return e
}

// applyEntityFieldDefaults 用来源列补齐实体字段的机械事实：
// json_name/type/sensitivity；description/validation/source 必须显式。
// 只补可确定的事实，不猜语义（枚举/关系/computed/soft 仍需显式）。
func (c *Checker) applyEntityFieldDefaults(e *entityInfo) {
	for i := range e.Fields {
		f := &e.Fields[i]
		if f.JSONName == "" {
			f.JSONName = snakeCase(f.Name)
		}
		if f.IsRelation {
			continue
		}
		if f.SourceComputed != "" {
			if f.Sensitivity == "" {
				f.Sensitivity = sensitivityPublic
			}
			continue
		}
		if f.ColumnTable == "" || f.Column == "" {
			continue // 缺失来源由 checkEntityFieldSource 报出
		}
		meta, ok := c.tableColumns[f.ColumnTable][f.Column]
		if !ok {
			continue
		}
		if !f.HasType {
			typ := meta.typ
			if meta.nullable {
				typ = "*" + typ
			}
			f.Type, f.HasType = typ, true
		}
		if f.Sensitivity == "" {
			f.Sensitivity = normalizeSensitivity(meta.sensitivity)
		}
	}
}

// isEnumType 判断类型（可带 * 前缀）是否引用 data.enums.*。
func isEnumType(typ string) bool {
	base, _ := splitPointer(typ)
	return strings.HasPrefix(base, "data.enums.")
}

// splitPointer 拆分 type 的 * 前缀；* 表示指针（等价可空）。
func splitPointer(typ string) (string, bool) {
	if strings.HasPrefix(typ, "*") {
		return strings.TrimPrefix(typ, "*"), true
	}
	return typ, false
}

// checkEntities 校验实体文件的结构与语义，并建立 relation 反向索引。
func (c *Checker) checkEntities(schema *Schema) {
	c.checkTargets(schema)
	c.checkEntityBase(schema)
	seenIDs := map[string]string{}
	seenNames := map[string]string{}
	usedTables := map[string]bool{}
	for _, ed := range c.entityDocs {
		c.checkTargetFileName(schema, filepath.Base(ed.rel), ed.rel)
		c.validateDoc(ed.doc, schema.Fields, ed.rel)
		c.checkStability(ed.doc, ed.rel)
		if s, _ := ed.doc["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: summary must not be empty", ed.rel)
		}
		id, _ := ed.doc["id"].(string)
		want := schema.Targets.FileID(filepath.Base(ed.rel))
		if want != "" && id != want {
			c.rulef("schema.id-from-file", "%s: id must be %s, got %s (file name is the source)", ed.rel, want, id)
		}
		if prev, ok := seenIDs[id]; ok {
			c.rulef("schema.contract-id-unique", "%s: duplicate contract id %s (also %s)", ed.rel, id, prev)
		} else if id != "" {
			seenIDs[id] = ed.rel
		}
		if model, ok := asMap(ed.doc["model"]); ok {
			if name, _ := model["name"].(string); name != "" {
				if prev, ok := seenNames[name]; ok {
					c.rulef("entity-name-unique", "%s: duplicate entity name %s (also %s)", ed.rel, name, prev)
				} else {
					seenNames[name] = ed.rel
				}
			}
		}
		c.checkFieldOrder(ed.rel, schema.Fields, orderRules{top: "entity-key-order", nested: "entity-key-order", model: "entity-model-order"})
		c.checkBaseFieldFirst(ed)
		short := strings.TrimSuffix(filepath.Base(ed.rel), filepath.Ext(ed.rel))
		c.checkEntityDoNot(ed, short)
		var ext []string
		if model, ok := asMap(ed.doc["model"]); ok {
			ext = stringList(model["extends"])
		}
		if len(ext) != 1 || ext[0] != baseEntityID {
			c.rulef("entity-base-only", "%s: extends must be [%s], got %v", ed.rel, baseEntityID, ext)
		}
	}

	expectedEnt := map[string]map[string]bool{}
	for _, e := range c.entities {
		c.checkEntityModel(e)
		c.checkEntityOwnerModule(e)
		c.checkEntityFields(e)
		c.checkEntityID(e)
		c.checkEntityName(e)
		c.checkEntityFieldMetadata(e)
		c.checkEntityRelations(e, expectedEnt)
		c.checkEntityValidation(e)
		for _, t := range e.Tables {
			usedTables[t] = true
		}
	}
	for id := range c.tableColumns {
		if id == c.baseID {
			continue
		}
		if !usedTables[id] {
			c.rulef("entity-table-covered", "physical table %s has no data.entities mapping", id)
		}
	}
	for _, e := range c.entities {
		model, _ := asMap(e.Doc["model"])
		got := stringSet(asList(model["included_by"]))
		want := expectedEnt[e.ID]
		if want == nil {
			want = map[string]bool{}
		}
		if !sameSet(got, want) {
			c.rulef("entity-included-by", "%s: model.included_by must be %v, got %v", e.Rel, sortedKeys(want), sortedKeys(got))
		}
	}
	c.checkEagerCycles()
}

// checkBaseFieldFirst 要求重声明的基类字段（当前仅 Status）必须是 fields 第一项。
func (c *Checker) checkBaseFieldFirst(ed entityDoc) {
	for i, raw := range asList(ed.doc["fields"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if _, isBase := entityBaseShape[name]; isBase && i != 0 {
			c.rulef("entity-base-field-first", "%s: base field %s must be the first field", ed.rel, name)
		}
	}
}

func (c *Checker) checkEntityDoNot(ed entityDoc, short string) {
	// 实体可用自身短名、契约 ID 或任一字段名作为具体标识。
	names := map[string]bool{short: true, "data.entities." + short: true}
	for _, bf := range c.entityBaseFields {
		if bf.Name != "" {
			names[bf.Name] = true
		}
	}
	for _, raw := range asList(ed.doc["fields"]) {
		if m, ok := asMap(raw); ok {
			if n, _ := m["name"].(string); n != "" {
				names[n] = true
			}
		}
	}
	for _, raw := range asList(ed.doc["do_not"]) {
		s, _ := raw.(string)
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			c.rulef("entity-do-not-specific", "%s: do_not entries must not be empty", ed.rel)
			continue
		}
		specific := false
		for name := range names {
			if strings.Contains(trimmed, name) {
				specific = true
				break
			}
		}
		if !specific {
			c.rulef("entity-do-not-specific", "%s: do_not must mention the entity or one of its fields: %s", ed.rel, trimmed)
		}
	}
}

type entityBaseFieldShape struct {
	typ string
}

var entityBaseShape = map[string]entityBaseFieldShape{
	"ID":        {typ: "integer"},
	"Status":    {typ: "integer"},
	"CreatedAt": {typ: "timestamp"},
	"UpdatedAt": {typ: "timestamp"},
	"DeletedAt": {typ: "*timestamp"},
}

// checkEntityBase 校验基类路径/ID/5 字段形状；基类不参与实体映射和 used_by。
func (c *Checker) checkEntityBase(schema *Schema) {
	if c.entityBaseDoc == nil {
		c.rulef("entity-base-file", "%s: missing base contract", baseEntityPath)
		return
	}
	allowed := map[string]bool{
		"schema_version": true, "id": true, "summary": true,
		"stability": true, "do_not": true, "fields": true,
	}
	c.checkFieldOrder(baseEntityPath, schema.Fields, orderRules{top: "entity-key-order", nested: "entity-key-order", model: "entity-model-order"})
	c.validateDoc(c.entityBaseDoc, filterFields(schema.Fields, allowed), baseEntityPath)
	c.checkStability(c.entityBaseDoc, baseEntityPath)
	if s, _ := c.entityBaseDoc["summary"].(string); strings.TrimSpace(s) == "" {
		c.rulef("schema.non-empty", "%s: summary must not be empty", baseEntityPath)
	}
	if c.entityBaseDoc["id"] != baseEntityID {
		c.rulef("entity-base-file", "%s: id must be %s, got %v", baseEntityPath, baseEntityID, c.entityBaseDoc["id"])
	}
	seen := map[string]bool{}
	count := 0
	for i, raw := range asList(c.entityBaseDoc["fields"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		count++
		where := fmt.Sprintf("%s: fields[%d] (%s)", baseEntityPath, i, name)
		shape, known := entityBaseShape[name]
		if !known {
			c.rulef("entity-base-shape", "%s: unexpected base field", where)
			continue
		}
		if seen[name] {
			c.rulef("entity-base-shape", "%s: duplicate base field", where)
		}
		seen[name] = true
		f := parseEntityField(m)
		if f == nil {
			continue
		}
		ref := "data.tables.base.col." + snakeCase(name)
		if f.HasType {
			c.rulef("entity-base-shape", "%s: type must be inherited from tables/_base; do not redeclare", where)
		} else if meta, ok := baseColumnMeta()[snakeCase(name)]; ok {
			derived := c.resolvedColumnType(meta.typ)
			if meta.nullable {
				derived = "*" + derived
			}
			if derived != shape.typ {
				c.rulef("entity-base-shape", "%s: inherited type must be %s, got %s", where, shape.typ, derived)
			}
		}
		if f.SourceColumnRef != ref || f.SourceComputed != "" {
			c.rulef("entity-base-shape", "%s: source must be %s", where, ref)
		}
		if f.ValidationCount != 1 || f.ValidationKind != "column" || f.ValidationRef != ref {
			c.rulef("entity-base-shape", "%s: validation must be column %s", where, ref)
		}
		if strings.TrimSpace(f.Description) == "" {
			c.rulef("entity-base-shape", "%s: description must not be empty", where)
		}
	}
	if count != len(entityBaseShape) {
		c.rulef("entity-base-shape", "%s: must declare exactly %d fields, got %d", baseEntityPath, len(entityBaseShape), count)
	}
	for name := range entityBaseShape {
		if !seen[name] {
			c.rulef("entity-base-shape", "%s: missing base field %s", baseEntityPath, name)
		}
	}
}

func (c *Checker) checkEntityModel(e *entityInfo) {
	if len(e.Tables) == 0 {
		c.rulef("entity-table-shape", "%s: tables must not be empty", e.Rel)
		return
	}
	seen := map[string]bool{}
	for _, t := range e.Tables {
		if t == c.baseID {
			c.rulef("entity-table-shape", "%s: data.tables.base is not a physical table", e.Rel)
			continue
		}
		if _, ok := c.tableColumns[t]; !ok {
			c.rulef("entity-table-shape", "%s: table %s not found", e.Rel, t)
		}
		if seen[t] {
			c.rulef("entity-table-shape", "%s: duplicate table %s", e.Rel, t)
		}
		seen[t] = true
	}
}

// isEntityType 复用 data.tables 的类型族；测试/独立调用时从已加载列兜底。
func (c *Checker) isEntityType(typ string) bool {
	if c.tableTypeFamilies[typ] {
		return true
	}
	for _, cols := range c.tableColumns {
		for _, meta := range cols {
			if c.resolvedColumnType(meta.typ) == typ {
				return true
			}
		}
	}
	return false
}

func (c *Checker) checkEntityFields(e *entityInfo) {
	names := map[string]bool{}
	cols := map[string]string{}
	for _, f := range e.Fields {
		if names[f.Name] {
			c.rulef("schema.unique-names", "%s: duplicate field name %s", e.Rel, f.Name)
		}
		names[f.Name] = true
		where := fmt.Sprintf("%s: fields.%s", e.Rel, f.Name)
		if f.BaseRedeclared {
			c.rulef("entity-no-base-fields", "%s: redeclares base field; only status may specialize to data.enums.*", where)
		}
		if strings.TrimSpace(f.Description) == "" {
			c.rulef("entity-field-description", "%s: description must not be empty", where)
		}
		if f.IsRelation {
			c.checkRelationFieldShape(f, where)
			continue
		}
		c.checkScalarField(e, f, where, cols)
	}
	// 多表合成的每张表都必须真正提供字段，否则该表在 entity 中是多余的。
	if len(e.Tables) > 1 {
		used := map[string]bool{}
		for _, f := range e.Fields {
			if !f.IsRelation && f.ColumnTable != "" {
				used[f.ColumnTable] = true
			}
		}
		for _, t := range e.Tables {
			if !used[t] {
				c.rulef("entity-field-column", "%s: table %s is declared but no field references it", e.Rel, t)
			}
		}
	}
}

// checkRelationFieldShape 校验关系字段的键集合与必填项。
func (c *Checker) checkRelationFieldShape(f entityFieldInfo, where string) {
	if f.HasSource || f.HasValidation || f.HasSensitivity {
		c.rulef("entity-field-variant", "%s: relation field must not declare source/validation/sensitivity", where)
	}
	if !f.HasType || f.JoinFromRef == "" || f.JoinToRef == "" {
		c.rulef("entity-field-variant", "%s: relation field requires type/join_column_from/join_column_to", where)
		return
	}
	if _, pointer := splitPointer(f.Type); pointer || !strings.HasPrefix(f.Type, "data.entities.") {
		c.rulef("entity-relation-type", "%s: relation type must be a data.entities.* ID, got %s", where, f.Type)
	}
}

// checkScalarField 校验标量字段：来源、类型、指针/可空、敏感标记与字段校验。
func (c *Checker) checkScalarField(e *entityInfo, f entityFieldInfo, where string, cols map[string]string) {
	if f.JoinFromRef != "" || f.JoinToRef != "" || f.HasLoad {
		c.rulef("entity-field-variant", "%s: scalar field must not declare join_column_from/join_column_to/load", where)
	}
	hasCol := f.SourceColumnRef != ""
	hasComputed := f.SourceComputed != ""
	hasSoft := f.SourceSoft != ""
	sourceCount := 0
	for _, present := range []bool{hasCol, hasComputed, hasSoft} {
		if present {
			sourceCount++
		}
	}
	if sourceCount != 1 {
		c.rulef("entity-field-source", "%s: source must declare exactly one of column/computed/soft", where)
	}
	if hasSoft && strings.TrimSpace(f.SourceSoft) == "" {
		c.rulef("entity-field-source", "%s: source soft must not be blank", where)
	}
	if !f.HasType {
		c.rulef("entity-field-type", "%s: type is required", where)
		c.checkFieldValidation(e, f, where, "")
		return
	}
	base, pointer := splitPointer(f.Type)
	if base == "" {
		c.rulef("entity-field-type", "%s: empty type", where)
		return
	}
	if strings.Contains(base, "[]") || strings.HasPrefix(base, "list<") {
		c.rulef("entity-field-type", "%s: list types are not allowed in entity fields", where)
		return
	}
	if strings.HasPrefix(base, "data.tables.") {
		c.rulef("entity-field-type", "%s: entity fields must not reference table row types", where)
		return
	}
	enumRef := strings.HasPrefix(base, "data.enums.")
	if !enumRef && !c.isEntityType(base) {
		c.rulef("entity-field-type", "%s: unknown entity type %s", where, base)
		return
	}
	if hasComputed {
		if !pointer {
			c.rulef("entity-field-source", "%s: computed field must use a pointer type (*T) because queries may not load it", where)
		}
		if sqlParamAtRe.MatchString(f.SourceComputed) || sqlParamNargRe.MatchString(f.SourceComputed) || sqlParamArgRe.MatchString(f.SourceComputed) {
			c.rulef("entity-field-source", "%s: computed expression must not reference @params", where)
		}
	} else if hasCol {
		meta, ok := c.tableColumns[f.ColumnTable][f.Column]
		switch {
		case f.ColumnTable == "" || f.Column == "":
			c.rulef("entity-field-column", "%s: malformed column ref %q", where, f.SourceColumnRef)
		case !containsString(e.Tables, f.ColumnTable):
			c.rulef("entity-field-column", "%s: column %s.%s does not belong to entity tables %v", where, f.ColumnTable, f.Column, e.Tables)
		case !ok:
			c.rulef("entity-field-column", "%s: column %s.%s not found", where, f.ColumnTable, f.Column)
		default:
			key := f.ColumnTable + "." + f.Column
			if prev, ok := cols[key]; ok {
				c.rulef("entity-field-column", "%s: column %s already used by field %s", where, key, prev)
			}
			cols[key] = f.Name
			colType := c.resolvedColumnType(meta.typ)
			if enumRef {
				if colType != "integer" {
					c.rulef("entity-field-type", "%s: enum requires integer column, got %s", where, colType)
				}
			} else if base != colType {
				c.rulef("entity-field-type", "%s: type %s does not match column type %s", where, base, colType)
			}
			if pointer != meta.nullable {
				c.rulef("entity-field-type", "%s: pointer type %q must match column nullable=%v", where, f.Type, meta.nullable)
			}
			if sensitivityRank(f.Sensitivity) < sensitivityRank(meta.sensitivity) {
				c.rulef("entity-field-sensitive", "%s: sensitivity %s must not be lower than source column %s", where, normalizeSensitivity(f.Sensitivity), normalizeSensitivity(meta.sensitivity))
			}
		}
	}
	if normalizeSensitivity(f.Sensitivity) == sensitivitySecret && isExportedName(f.Name) {
		c.rulef("entity-field-sensitive", "%s: secret field must start lowercase (unexported)", where)
	}
	c.checkFieldValidation(e, f, where, base)
}

// checkFieldValidation 校验 validation 三选一以及列/正则引用。
func (c *Checker) checkFieldValidation(e *entityInfo, f entityFieldInfo, where, baseType string) {
	if f.ValidationCount != 1 {
		c.rulef("entity-field-validation", "%s: validation must declare exactly one of column/pattern/soft", where)
		return
	}
	switch f.ValidationKind {
	case "column":
		if f.SourceColumnRef == "" {
			c.rulef("entity-field-validation", "%s: validation column requires source.column", where)
			return
		}
		if f.ValidationRef != f.SourceColumnRef {
			c.rulef("entity-field-validation", "%s: validation column %s must equal source.column %s", where, f.ValidationRef, f.SourceColumnRef)
		}
		t, col, ok := splitColumnRef(f.ValidationRef)
		if !ok || t == "" || col == "" {
			c.rulef("entity-field-validation", "%s: malformed validation column %q", where, f.ValidationRef)
			return
		}
		if t == c.baseID {
			if !isBaseColumn(col) {
				c.rulef("entity-field-validation", "%s: validation column %s not found", where, f.ValidationRef)
			}
			return
		}
		if !containsString(e.Tables, t) {
			c.rulef("entity-field-validation", "%s: validation column %s does not belong to entity tables", where, f.ValidationRef)
			return
		}
		if _, ok := c.tableColumns[t][col]; !ok {
			c.rulef("entity-field-validation", "%s: validation column %s not found", where, f.ValidationRef)
		}
	case "pattern":
		if baseType != "string" {
			c.rulef("entity-field-validation", "%s: pattern validation requires string type, got %s", where, baseType)
		}
		if _, err := regexp.Compile(f.ValidationRef); err != nil {
			c.rulef("entity-field-validation", "%s: invalid pattern %q: %v", where, f.ValidationRef, err)
		}
	case "soft":
		if strings.TrimSpace(f.ValidationRef) == "" {
			c.rulef("entity-field-validation", "%s: soft validation must not be empty", where)
		}
	}
}

func (c *Checker) checkEntityID(e *entityInfo) {
	idField := findEntityField(e, "ID")
	if idField == nil {
		c.rulef("entity-id", "%s: base ID field is missing", e.Rel)
		return
	}
	if _, pointer := splitPointer(idField.Type); pointer {
		c.rulef("entity-id", "%s: ID must not be a pointer type", e.Rel)
	}
	if idField.ColumnTable != e.Primary {
		c.rulef("entity-id", "%s: ID must come from primary table %s", e.Rel, e.Primary)
	}
	if meta, ok := c.tableColumns[idField.ColumnTable][idField.Column]; ok && !meta.primaryKey {
		c.rulef("entity-id", "%s: ID column %s.%s must be a primary key", e.Rel, idField.ColumnTable, idField.Column)
	}
	if pks := c.tablePrimaryKey[e.Primary]; len(pks) > 0 && (len(pks) != 1 || pks[0] != idField.Column) {
		c.rulef("entity-id", "%s: ID column must be the sole primary key of %s, got %v", e.Rel, e.Primary, pks)
	}
}

// checkEntityOwnerModule 要求实体归属与主表 owner_module 一致。
func (c *Checker) checkEntityOwnerModule(e *entityInfo) {
	if e.OwnerModule == "" || e.Primary == "" {
		return
	}
	if owner := c.tableOwners[e.Primary]; owner != "" && owner != e.OwnerModule {
		c.rulef("entity-owner-module", "%s: owner_module %s must equal primary table owner %s", e.Rel, e.OwnerModule, owner)
	}
}

// checkEntityName 校验实体名是 Go 标识符；首字母大小写表达 public/internal。
func (c *Checker) checkEntityName(e *entityInfo) {
	if e.Name == "" {
		c.rulef("entity-name", "%s: name is required", e.Rel)
		return
	}
	if !isGoIdent(e.Name) || goKeywords[e.Name] {
		c.rulef("entity-name", "%s: name %q must be a valid non-keyword Go identifier", e.Rel, e.Name)
	}
}

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// isGoIdent 判断是否是仅含字母数字、且以字母开头的 Go 标识符。
func isGoIdent(s string) bool {
	if s == "" || !isASCIILetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isASCIILetter(s[i]) && (s[i] < '0' || s[i] > '9') {
			return false
		}
	}
	return true
}

func isASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isExportedName(s string) bool {
	return s != "" && s[0] >= 'A' && s[0] <= 'Z'
}

// checkEntityFieldMetadata 校验字段名与 json_name 这类生成语义。
func (c *Checker) checkEntityFieldMetadata(e *entityInfo) {
	jsonNames := map[string]string{}
	for _, f := range e.Fields {
		where := fmt.Sprintf("%s: fields.%s", e.Rel, f.Name)
		if !isGoIdent(f.Name) || goKeywords[f.Name] {
			c.rulef("entity-field-name", "%s: field name %q must be a valid non-keyword Go identifier", where, f.Name)
		}
		jsonName := f.JSONName
		if jsonName == "" {
			c.rulef("entity-field-json-name", "%s: json_name is required", where)
			continue
		}
		if prev, ok := jsonNames[jsonName]; ok {
			c.rulef("entity-field-json-name", "%s: json_name %q already used by field %s", where, jsonName, prev)
		} else {
			jsonNames[jsonName] = f.Name
		}
	}
}

func (c *Checker) checkEntityRelations(e *entityInfo, expected map[string]map[string]bool) {
	for i := range e.Fields {
		r := &e.Fields[i]
		if !r.IsRelation {
			continue
		}
		where := fmt.Sprintf("%s: fields.%s", e.Rel, r.Name)
		target := c.entities[r.Type]
		if target == nil {
			c.rulef("entity-relation-target", "%s: type %s is not an entity", where, r.Type)
			continue
		}
		if r.JoinToTable == "" || c.tableColumns[r.JoinToTable] == nil {
			c.rulef("entity-relation-target", "%s: join_column_to table %s not found", where, r.JoinToTable)
			continue
		}
		if !containsString(target.Tables, r.JoinToTable) {
			c.rulef("entity-relation-target", "%s: join_column_to table %s is not mapped by %s", where, r.JoinToTable, r.Type)
			continue
		}
		if expected[r.Type] == nil {
			expected[r.Type] = map[string]bool{}
		}
		expected[r.Type][e.ID] = true

		fromMeta, fromOK := c.tableColumns[r.JoinFromTable][r.JoinFromColumn]
		if !fromOK {
			c.rulef("entity-relation-via", "%s: join_column_from %s not found", where, r.JoinFromRef)
			continue
		}
		toMeta, toOK := c.tableColumns[r.JoinToTable][r.JoinToColumn]
		if !toOK {
			c.rulef("entity-relation-via", "%s: join_column_to %s not found", where, r.JoinToRef)
			continue
		}
		if !containsString(e.Tables, r.JoinFromTable) {
			c.rulef("entity-relation-via", "%s: join_column_from table %s is not mapped by %s", where, r.JoinFromTable, e.ID)
			continue
		}
		if c.resolvedColumnType(fromMeta.typ) != c.resolvedColumnType(toMeta.typ) {
			c.rulef("entity-relation-via", "%s: join column type %s does not match %s type %s", where, c.resolvedColumnType(fromMeta.typ), r.JoinToRef, c.resolvedColumnType(toMeta.typ))
			continue
		}
		card := c.relationCardinality(r)
		if card == "" {
			c.rulef("entity-relation-cardinality", "%s: one side of join columns must be unique (PK or unique index)", where)
			continue
		}
		if r.Load == "eager" {
			if r.Type == e.ID {
				c.rulef("entity-relation-eager-cycle", "%s: self relation must be lazy", where)
			}
			if !c.hasIndex(r.JoinToTable, r.JoinToColumn) {
				c.rulef("entity-relation-eager-index", "%s: eager relation requires index on %s.%s", where, r.JoinToTable, r.JoinToColumn)
			}
		}
	}
}

// relationCardinality 由两端列的唯一性推断关系基数：
//   - 两端唯一: one_to_one
//   - from 唯一: one_to_many
//   - to 唯一: many_to_one
func (c *Checker) relationCardinality(r *entityFieldInfo) string {
	fromUnique := c.columnUnique(r.JoinFromTable, r.JoinFromColumn)
	toUnique := c.columnUnique(r.JoinToTable, r.JoinToColumn)
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

// columnUnique 判断列是否为主键或单列唯一索引。
func (c *Checker) columnUnique(table, col string) bool {
	if meta, ok := c.tableColumns[table][col]; ok && meta.primaryKey {
		return true
	}
	for _, idx := range c.tableIndexes[table] {
		if idx.unique && len(idx.columns) == 1 && idx.columns[0] == col {
			return true
		}
	}
	return false
}

// resolvedColumnType 把列引用解析为实际类型族；非引用原样返回。
func (c *Checker) resolvedColumnType(typ string) string {
	if !strings.HasPrefix(typ, "data.tables.") {
		return typ
	}
	if t, col, ok := splitColumnRef(typ); ok {
		if meta, found := c.tableColumns[t][col]; found {
			return meta.typ
		}
	}
	return typ
}

func (c *Checker) hasIndex(tableID, col string) bool {
	if meta, ok := c.tableColumns[tableID][col]; ok && meta.primaryKey {
		return true
	}
	for _, idx := range c.tableIndexes[tableID] {
		if len(idx.columns) > 0 && idx.columns[0] == col {
			return true
		}
	}
	return false
}

func (c *Checker) checkEntityValidation(e *entityInfo) {
	seen := map[string]bool{}
	for _, raw := range asList(e.Doc["validation"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		where := fmt.Sprintf("%s: validation.%s", e.Rel, name)
		if !entityValidationNameRe.MatchString(name) {
			c.rulef("entity-validation", "%s: name must be snake_case", where)
		}
		if seen[name] {
			c.rulef("schema.unique-names", "%s: duplicate validation name %s", e.Rel, name)
		}
		seen[name] = true
		if s, _ := m["expr"].(string); strings.TrimSpace(s) == "" {
			c.rulef("entity-validation", "%s: expr must not be empty", where)
		}
		if code, _ := m["on_violation"].(string); code != "" {
			if errs := c.errorCodes(); len(errs) > 0 && !errs[strings.TrimPrefix(code, "platform.errors.")] {
				c.rulef("entity-validation", "%s: unknown error code %s", where, code)
			}
		}
	}
}

// checkEagerCycles 检测 eager 关系图是否成环。
func (c *Checker) checkEagerCycles() {
	adj := map[string][]string{}
	for id, e := range c.entities {
		for _, r := range e.Fields {
			if r.IsRelation && r.Load == "eager" && r.Type != "" {
				adj[id] = append(adj[id], r.Type)
			}
		}
	}
	state := map[string]int{}
	var walk func(id string, stack []string) bool
	walk = func(id string, stack []string) bool {
		if state[id] == 1 {
			c.rulef("entity-relation-eager-cycle", "eager relation cycle: %s", strings.Join(append(stack, id), " -> "))
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, next := range adj[id] {
			if walk(next, append(stack, id)) {
				state[id] = 2
				return true
			}
		}
		state[id] = 2
		return false
	}
	for _, id := range c.entityOrder {
		walk(id, nil)
	}
}

// checkEntityUsedBy 在 query/capability 解析完成后对账 used_by。
func (c *Checker) checkEntityUsedBy() {
	expectedQueries := map[string]map[string]bool{}
	for _, q := range c.queries {
		for _, r := range q.Returns {
			if r.Kind != "entity" || r.Entity == "" {
				continue
			}
			if expectedQueries[r.Entity] == nil {
				expectedQueries[r.Entity] = map[string]bool{}
			}
			expectedQueries[r.Entity][q.ID] = true
		}
	}
	expectedCapabilities := c.entityCapabilityUsages()
	for _, id := range c.entityOrder {
		e := c.entities[id]
		ub, ok := asMap(e.Doc["used_by"])
		if !ok {
			continue
		}
		gotQueries := stringSet(asList(ub["queries"]))
		wantQueries := expectedQueries[id]
		if wantQueries == nil {
			wantQueries = map[string]bool{}
		}
		if !sameSet(gotQueries, wantQueries) {
			c.rulef("entity-used-by", "%s: used_by.queries must be %v, got %v", e.Rel, sortedKeys(wantQueries), sortedKeys(gotQueries))
		}
		gotCapabilities := stringSet(asList(ub["capabilities"]))
		wantCapabilities := expectedCapabilities[id]
		if wantCapabilities == nil {
			wantCapabilities = map[string]bool{}
		}
		if !sameSet(gotCapabilities, wantCapabilities) {
			c.rulef("entity-used-by", "%s: used_by.capabilities must be %v, got %v", e.Rel, sortedKeys(wantCapabilities), sortedKeys(gotCapabilities))
		}
	}
}

// entityCapabilityUsages 扫描 capability 中 type/x-entity/returns 的实体引用。
func (c *Checker) entityCapabilityUsages() map[string]map[string]bool {
	usages := map[string]map[string]bool{}
	for _, f := range walkYAML(c.root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil {
			continue
		}
		capabilityID, _ := doc["id"].(string)
		if capabilityID == "" {
			continue
		}
		c.collectEntityRefs(doc, func(entityID string) {
			if usages[entityID] == nil {
				usages[entityID] = map[string]bool{}
			}
			usages[entityID][capabilityID] = true
		})
	}
	return usages
}

// collectEntityRefs 收集结构化键中的 data.entities.* 引用；不扫描 do_not 等散文。
func (c *Checker) collectEntityRefs(v any, fn func(string)) {
	if m, ok := asMap(v); ok {
		for k, child := range m {
			if k == "type" || k == "x-entity" || k == "returns" {
				if s, ok := child.(string); ok {
					if id, ok := entityRef(s); ok {
						fn(id)
					}
				}
			}
			c.collectEntityRefs(child, fn)
		}
		return
	}
	if list, ok := asList2(v); ok {
		for _, item := range list {
			c.collectEntityRefs(item, fn)
		}
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

// errorCodes 读取 platform/errors.yaml 的错误码集合；文件缺失时返回空。
func (c *Checker) errorCodes() map[string]bool {
	if c.errCodeSet != nil {
		return c.errCodeSet
	}
	c.errCodeSet = map[string]bool{}
	doc, err := loadYAML(filepath.Join(c.root, "contracts/platform/errors.yaml"))
	if err != nil {
		return c.errCodeSet
	}
	for _, raw := range asList(doc["errors"]) {
		if m, ok := asMap(raw); ok {
			if code, _ := m["code"].(string); code != "" {
				c.errCodeSet[code] = true
			}
		}
	}
	return c.errCodeSet
}
