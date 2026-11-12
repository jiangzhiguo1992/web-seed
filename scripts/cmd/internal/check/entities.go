// entities.go 校验 data.entities：实体到表的投影、字段、身份、关系、校验与反向索引。
//
// 实体是读模型：字段必须能投影回物理列；多表合成只读，JOIN 由 query 负责。
package check

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	baseEntityID   = "data.entities.base"
	baseEntityPath = "contracts/data/entities/_base.yaml"
)

// columnMeta 是物理表的有效列元数据（含继承列）。
type columnMeta struct {
	typ         string
	nullable    bool
	primaryKey  bool
	pii         bool
	neverExpose bool
}

// entityFieldInfo 是实体字段的解析结果；ColumnTable/Column 为空表示无法解析。
type entityFieldInfo struct {
	Name            string
	ColumnTable     string
	Column          string
	HasColumnRef    bool
	HasExplicitType bool
	HasRequired     bool
	HasMutable      bool
	Type            string
	Required        bool
	Mutable         bool
	PII             bool
	NeverExpose     bool
	JSONName        string
	Examples        []any
	Description     string
	FromBase        bool
	OverridesBase   bool
	BaseRedeclared  bool
}

// entityRelationInfo 是实体关系的解析结果。
type entityRelationInfo struct {
	Name      string
	Target    string
	Kind      string
	ViaTable  string
	ViaColumn string
	Load      string
}

// entityInfo 保存单个 entity 的解析结果，供跨文件检查复用。
type entityInfo struct {
	ID          string
	Rel         string
	Doc         map[string]any
	Name        string
	Tables      []string
	Primary     string
	Fields      []entityFieldInfo
	Relations   []entityRelationInfo
	Validation  []string
	UsedByQuery map[string]bool
	UsedByEnt   map[string]bool
}

// entityDoc 保存原始文件，供结构校验使用。
type entityDoc struct {
	rel    string
	fileID string
	doc    map[string]any
}

func baseColumnMeta() map[string]columnMeta {
	return map[string]columnMeta{
		"id":         {typ: "string", primaryKey: true},
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
				meta.pii, _ = m["pii"].(bool)
				meta.neverExpose, _ = m["never_expose"].(bool)
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
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		f := entityFieldInfo{Name: name, Mutable: true, HasExplicitType: true, FromBase: true}
		f.Type, _ = m["type"].(string)
		f.JSONName, _ = m["json_name"].(string)
		for _, x := range asList(m["examples"]) {
			f.Examples = append(f.Examples, x)
		}
		f.Description, _ = m["description"].(string)
		if v, ok := m["required"].(bool); ok {
			f.Required = v
			f.HasRequired = true
		}
		if v, ok := m["mutable"].(bool); ok {
			f.Mutable = v
			f.HasMutable = true
		}
		out = append(out, f)
	}
	return out
}

func (c *Checker) parseEntity(doc map[string]any, rel string) *entityInfo {
	e := &entityInfo{
		Rel:         rel,
		Doc:         doc,
		UsedByQuery: map[string]bool{},
		UsedByEnt:   map[string]bool{},
	}
	e.ID, _ = doc["id"].(string)
	e.Name, _ = doc["name"].(string)
	e.Tables = stringList(doc["tables"])
	if len(e.Tables) > 0 {
		e.Primary = e.Tables[0]
	}
	for _, raw := range asList(doc["fields"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		f := entityFieldInfo{Name: name, Mutable: true}
		if v, ok := m["required"].(bool); ok {
			f.Required = v
			f.HasRequired = true
		}
		if v, ok := m["mutable"].(bool); ok {
			f.Mutable = v
			f.HasMutable = true
		}
		f.PII, _ = m["pii"].(bool)
		f.NeverExpose, _ = m["never_expose"].(bool)
		f.JSONName, _ = m["json_name"].(string)
		for _, x := range asList(m["examples"]) {
			f.Examples = append(f.Examples, x)
		}
		f.Description, _ = m["description"].(string)
		f.Type, _ = m["type"].(string)
		f.HasExplicitType = f.Type != ""
		if ref, _ := m["column"].(string); ref != "" {
			f.HasColumnRef = true
			if t, col, ok := splitColumnRef(ref); ok {
				f.ColumnTable, f.Column = t, col
			}
		} else if len(e.Tables) == 1 {
			f.ColumnTable, f.Column = e.Tables[0], snakeCase(name)
		}
		if f.ColumnTable != "" {
			if meta, ok := c.tableColumns[f.ColumnTable][f.Column]; ok && f.Type == "" {
				f.Type = meta.typ
			}
		}
		e.Fields = append(e.Fields, f)
	}
	// 基类字段由 _base 继承；Status 允许用枚举类型特化。
	byName := map[string]*entityFieldInfo{}
	for i := range e.Fields {
		byName[e.Fields[i].Name] = &e.Fields[i]
	}
	var inherited []entityFieldInfo
	for _, bf := range c.entityBaseFields {
		if cf := byName[bf.Name]; cf != nil {
			if bf.Name == "Status" && strings.HasPrefix(cf.Type, "data.enums.") {
				cf.OverridesBase = true
				if !cf.HasColumnRef && cf.ColumnTable == "" {
					cf.ColumnTable, cf.Column = e.Primary, snakeCase(bf.Name)
				}
				if !cf.HasRequired {
					cf.Required = bf.Required
				}
				if !cf.HasMutable {
					cf.Mutable = bf.Mutable
				}
				if cf.Description == "" {
					cf.Description = bf.Description
				}
				if cf.JSONName == "" {
					cf.JSONName = bf.JSONName
				}
				if len(cf.Examples) == 0 {
					cf.Examples = bf.Examples
				}
			} else {
				cf.BaseRedeclared = true
			}
			continue
		}
		bf.ColumnTable, bf.Column = e.Primary, snakeCase(bf.Name)
		inherited = append(inherited, bf)
	}
	e.Fields = append(inherited, e.Fields...)
	for _, raw := range asList(doc["relations"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		r := entityRelationInfo{Name: name, Load: "lazy"}
		r.Target, _ = m["target"].(string)
		r.Kind, _ = m["kind"].(string)
		if v, _ := m["load"].(string); v != "" {
			r.Load = v
		}
		if via, _ := m["via"].(string); via != "" {
			if t, col, ok := splitColumnRef(via); ok {
				r.ViaTable, r.ViaColumn = t, col
			}
		}
		e.Relations = append(e.Relations, r)
	}
	for _, raw := range asList(doc["validation"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		if id, _ := m["id"].(string); id != "" {
			e.Validation = append(e.Validation, id)
		}
	}
	if ub, ok := asMap(doc["used_by"]); ok {
		for _, x := range asList(ub["queries"]) {
			if s, ok := x.(string); ok && s != "" {
				e.UsedByQuery[s] = true
			}
		}
		for _, x := range asList(ub["entities"]) {
			if s, ok := x.(string); ok && s != "" {
				e.UsedByEnt[s] = true
			}
		}
	}
	return e
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
		if name, _ := ed.doc["name"].(string); name != "" {
			if prev, ok := seenNames[name]; ok {
				c.rulef("entity-name-unique", "%s: duplicate entity name %s (also %s)", ed.rel, name, prev)
			} else {
				seenNames[name] = ed.rel
			}
		}
		short := strings.TrimSuffix(filepath.Base(ed.rel), filepath.Ext(ed.rel))
		c.checkEntityDoNot(ed, short)
		ext := stringList(ed.doc["extends"])
		if len(ext) != 1 || ext[0] != baseEntityID {
			c.rulef("entity-base-only", "%s: extends must be [%s], got %v", ed.rel, baseEntityID, ext)
		}
	}

	expectedEnt := map[string]map[string]bool{}
	for _, e := range c.entities {
		c.checkEntityShape(e)
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
		ub, ok := asMap(e.Doc["used_by"])
		if !ok {
			continue // schema.required 报缺失
		}
		got := stringSet(asList(ub["entities"]))
		want := expectedEnt[e.ID]
		if want == nil {
			want = map[string]bool{}
		}
		if !sameSet(got, want) {
			c.rulef("entity-used-by", "%s: used_by.entities must be %v, got %v", e.Rel, sortedKeys(want), sortedKeys(got))
		}
	}
	c.checkEagerCycles()
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
	typ      string
	required bool
	mutable  bool
}

var entityBaseShape = map[string]entityBaseFieldShape{
	"ID":        {"string", true, false},
	"Status":    {"integer", true, true},
	"CreatedAt": {"timestamp", true, false},
	"UpdatedAt": {"timestamp", true, false},
	"DeletedAt": {"timestamp", false, false},
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
		typ, _ := m["type"].(string)
		required, _ := m["required"].(bool)
		mutable := true
		if v, ok := m["mutable"].(bool); ok {
			mutable = v
		}
		if typ != shape.typ {
			c.rulef("entity-base-shape", "%s: type must be %s, got %s", where, shape.typ, typ)
		}
		if required != shape.required {
			c.rulef("entity-base-shape", "%s: required must be %v", where, shape.required)
		}
		if mutable != shape.mutable {
			c.rulef("entity-base-shape", "%s: mutable must be %v", where, shape.mutable)
		}
		if _, has := m["column"]; has {
			c.rulef("entity-base-shape", "%s: base fields must not declare column", where)
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

func (c *Checker) checkEntityShape(e *entityInfo) {
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
		meta, ok := c.tableColumns[f.ColumnTable][f.Column]
		if f.ColumnTable == "" || f.Column == "" {
			switch {
			case f.HasColumnRef:
				c.rulef("entity-field-column", "%s: malformed column ref", where)
			case len(e.Tables) > 1:
				c.rulef("entity-field-column", "%s: multi-table entity field must declare column", where)
			default:
				c.rulef("entity-field-column", "%s: cannot resolve source column", where)
			}
			continue
		}
		if !containsString(e.Tables, f.ColumnTable) {
			c.rulef("entity-field-column", "%s: column %s.%s does not belong to entity tables %v", where, f.ColumnTable, f.Column, e.Tables)
			continue
		}
		if !ok {
			c.rulef("entity-field-column", "%s: column %s.%s not found", where, f.ColumnTable, f.Column)
			continue
		}
		key := f.ColumnTable + "." + f.Column
		if prev, ok := cols[key]; ok {
			c.rulef("entity-field-column", "%s: column %s already used by field %s", where, key, prev)
		}
		cols[key] = f.Name

		colType := c.resolvedColumnType(meta.typ)
		typ := f.Type
		if f.HasExplicitType && typ != "" {
			switch {
			case strings.Contains(typ, "[]") || strings.HasPrefix(typ, "list<"):
				c.rulef("entity-field-type", "%s: list types are not allowed in entity fields", where)
			case strings.HasPrefix(typ, "data.enums."):
				if colType != "integer" {
					c.rulef("entity-field-type", "%s: enum requires integer column, got %s", where, colType)
				}
			case strings.HasPrefix(typ, "data.tables."):
				c.rulef("entity-field-type", "%s: entity fields must not reference table row types", where)
			case c.isEntityType(typ):
				if colType != typ {
					c.rulef("entity-field-type", "%s: type %s does not match column type %s", where, typ, colType)
				}
			default:
				c.rulef("entity-field-type", "%s: unknown entity type %s", where, typ)
			}
		}
		if f.Required && meta.nullable {
			c.rulef("entity-field-required", "%s: required field maps to nullable column", where)
		}
		if !f.Required && !meta.nullable {
			c.rulef("entity-field-required", "%s: non-null column must be required:true", where)
		}
		if meta.pii && !f.PII {
			c.rulef("entity-field-sensitive", "%s: column is pii; entity field must set pii:true", where)
		}
		if meta.neverExpose && !f.NeverExpose {
			c.rulef("entity-field-sensitive", "%s: column is never_expose; entity field must set never_expose:true", where)
		}
		if f.NeverExpose && isExportedName(f.Name) {
			c.rulef("entity-field-sensitive", "%s: never_expose field must start lowercase (unexported)", where)
		}
		if f.Name == "CreatedAt" || f.Name == "UpdatedAt" || f.Name == "DeletedAt" {
			if f.Mutable {
				c.rulef("entity-field-mutable", "%s: %s must be mutable:false", where, f.Name)
			}
		}
	}
	// 多表合成的每张表都必须真正提供字段，否则该表在 entity 中是多余的。
	if len(e.Tables) > 1 {
		used := map[string]bool{}
		for _, f := range e.Fields {
			if f.ColumnTable != "" {
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

func (c *Checker) checkEntityID(e *entityInfo) {
	idField := findEntityField(e, "ID")
	if idField == nil {
		c.rulef("entity-id", "%s: base ID field is missing", e.Rel)
		return
	}
	if !idField.Required {
		c.rulef("entity-id", "%s: ID must be required:true", e.Rel)
	}
	if idField.Mutable {
		c.rulef("entity-id", "%s: ID must be mutable:false", e.Rel)
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

// checkEntityFieldMetadata 校验 json_name/examples 这类生成语义。
func (c *Checker) checkEntityFieldMetadata(e *entityInfo) {
	jsonNames := map[string]string{}
	for _, f := range e.Fields {
		where := fmt.Sprintf("%s: fields.%s", e.Rel, f.Name)
		if !isGoIdent(f.Name) || goKeywords[f.Name] {
			c.rulef("entity-field-name", "%s: field name %q must be a valid non-keyword Go identifier", where, f.Name)
		}
		jsonName := f.JSONName
		if jsonName == "" {
			jsonName = snakeCase(f.Name)
		}
		if prev, ok := jsonNames[jsonName]; ok {
			c.rulef("entity-field-json-name", "%s: json_name %q already used by field %s", where, jsonName, prev)
		} else {
			jsonNames[jsonName] = f.Name
		}
		for _, ex := range f.Examples {
			if !entityExampleMatches(f.Type, ex) {
				c.rulef("entity-field-examples", "%s: example %v does not match type %s", where, ex, f.Type)
			}
		}
	}
}

func entityExampleMatches(typ string, v any) bool {
	if strings.HasPrefix(typ, "data.enums.") {
		return isIntegerExample(v)
	}
	switch typ {
	case "string", "bytes":
		_, ok := v.(string)
		return ok
	case "timestamp", "datetime":
		switch v.(type) {
		case string, time.Time:
			return true
		}
		return false
	case "integer":
		return isIntegerExample(v)
	case "float", "decimal":
		switch v.(type) {
		case int, int64, uint64, float64:
			return true
		}
		return false
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "json":
		return true
	}
	return true
}

func isIntegerExample(v any) bool {
	switch v.(type) {
	case int, int64, uint64:
		return true
	}
	return false
}

func (c *Checker) checkEntityRelations(e *entityInfo, expected map[string]map[string]bool) {
	names := map[string]bool{}
	for _, r := range e.Relations {
		where := fmt.Sprintf("%s: relations.%s", e.Rel, r.Name)
		if names[r.Name] {
			c.rulef("schema.unique-names", "%s: duplicate relation name %s", e.Rel, r.Name)
		}
		names[r.Name] = true
		target := c.entities[r.Target]
		if target == nil {
			c.rulef("entity-relation-target", "%s: target %s not found", where, r.Target)
			continue
		}
		if expected[r.Target] == nil {
			expected[r.Target] = map[string]bool{}
		}
		expected[r.Target][e.ID] = true

		vmeta, vok := c.tableColumns[r.ViaTable][r.ViaColumn]
		if r.ViaTable == "" || r.ViaColumn == "" || !vok {
			c.rulef("entity-relation-via", "%s: via %s.%s not found", where, r.ViaTable, r.ViaColumn)
			continue
		}
		inSelf := containsString(e.Tables, r.ViaTable)
		inTarget := containsString(target.Tables, r.ViaTable)
		if !inSelf && !inTarget {
			c.rulef("entity-relation-via", "%s: via table %s belongs to neither %s nor %s", where, r.ViaTable, e.ID, target.ID)
			continue
		}
		switch r.Kind {
		case "many_to_one":
			if !inSelf {
				c.rulef("entity-relation-direction", "%s: many_to_one via must be on %s", where, e.ID)
			}
		case "one_to_many":
			if !inTarget {
				c.rulef("entity-relation-direction", "%s: one_to_many via must be on %s", where, target.ID)
			}
		case "one_to_one":
			// 两边均可
		default:
			c.rulef("entity-relation-direction", "%s: unknown kind %s", where, r.Kind)
		}
		counterpart := c.counterpartID(e, target, inSelf)
		if counterpart != nil {
			if meta, ok := c.tableColumns[counterpart.ColumnTable][counterpart.Column]; ok {
				counterType := c.resolvedColumnType(meta.typ)
				if counterType != c.resolvedColumnType(vmeta.typ) {
					c.rulef("entity-relation-via", "%s: via type %s does not match %s.%s type %s", where, c.resolvedColumnType(vmeta.typ), counterpart.ColumnTable, counterpart.Column, counterType)
				}
			}
		}
		if r.Load == "eager" {
			if r.Target == e.ID {
				c.rulef("entity-relation-eager-cycle", "%s: self relation must be lazy", where)
			}
			if inTarget {
				if !c.hasIndex(r.ViaTable, r.ViaColumn) {
					c.rulef("entity-relation-eager-index", "%s: eager relation requires index on %s.%s", where, r.ViaTable, r.ViaColumn)
				}
			}
		}
	}
}

// counterpartID 返回关系另一端用于类型比较的 id 字段。
func (c *Checker) counterpartID(e, target *entityInfo, inSelf bool) *entityFieldInfo {
	var owner *entityInfo
	if inSelf {
		owner = target
	} else {
		owner = e
	}
	return findEntityField(owner, "id")
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
		id, _ := m["id"].(string)
		where := fmt.Sprintf("%s: validation.%s", e.Rel, id)
		if id != "" && !strings.HasPrefix(id, e.ID+".") {
			c.rulef("entity-validation", "%s: id must start with %s.", where, e.ID)
		}
		if seen[id] {
			c.rulef("schema.unique-names", "%s: duplicate validation id %s", e.Rel, id)
		}
		seen[id] = true
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
		for _, r := range e.Relations {
			if r.Load == "eager" && r.Target != "" {
				adj[id] = append(adj[id], r.Target)
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

// checkEntityUsedByQueries 在 query 解析完成后对账 used_by.queries。
func (c *Checker) checkEntityUsedByQueries() {
	expected := map[string]map[string]bool{}
	for _, q := range c.queries {
		if q.Return.Kind == "entity" && q.Return.EntityID != "" {
			if expected[q.Return.EntityID] == nil {
				expected[q.Return.EntityID] = map[string]bool{}
			}
			expected[q.Return.EntityID][q.ID] = true
		}
	}
	for _, id := range c.entityOrder {
		e := c.entities[id]
		ub, ok := asMap(e.Doc["used_by"])
		if !ok {
			continue
		}
		got := stringSet(asList(ub["queries"]))
		want := expected[id]
		if want == nil {
			want = map[string]bool{}
		}
		if !sameSet(got, want) {
			c.rulef("entity-used-by", "%s: used_by.queries must be %v, got %v", e.Rel, sortedKeys(want), sortedKeys(got))
		}
	}
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
