// tables.go 校验 data.tables：基类、列、约束、索引与物理表名。
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func (c *Checker) checkTables(schema *Schema) (map[string]string, map[string]map[string]bool) {
	files, _ := filepath.Glob(filepath.Join(c.root, "contracts/data/tables/*.yaml"))
	sort.Strings(files)
	tableIDs := map[string]string{}
	cols := map[string]map[string]bool{}
	physicalNames := map[string]string{}
	c.tableOwners = map[string]string{}
	for _, file := range files {
		rel := relPath(c.root, file)
		doc, err := loadYAML(file)
		if err != nil {
			c.errf("%s: %v", rel, err)
			continue
		}
		id, _ := doc["id"].(string)
		if id == "" {
			c.errf("%s: missing id", rel)
			continue
		}
		if prev, ok := tableIDs[id]; ok {
			c.errf("%s: duplicate table id %s (also %s)", rel, id, prev)
		}
		tableIDs[id] = rel

		baseFile := filepath.Base(rel) == "_base.yaml"
		if id == "data.tables.base" && !baseFile {
			c.errf("%s: data.tables.base must live in _base.yaml", rel)
		}

		if doc["kind"] == "base" {
			c.checkBaseTable(doc, schema, rel)
		} else {
			c.validateDoc(doc, schema.Fields, rel)
			c.checkTableRules(doc, rel, schema)
			if owner, _ := doc["owner_module"].(string); owner != "" {
				c.tableOwners[id] = owner
			}
			phys := strings.TrimSuffix(filepath.Base(rel), ".yaml")
			if table, _ := asMap(doc["table"]); table != nil {
				if n, ok := table["name"].(string); ok && n != "" {
					phys = n
				}
			}
			if prev, ok := physicalNames[phys]; ok {
				c.errf("%s: physical table %q already declared by %s", rel, phys, prev)
			} else {
				physicalNames[phys] = rel
			}
		}

		set := map[string]bool{}
		for _, name := range baseColumns {
			set[name] = true
		}
		for _, raw := range asList(doc["columns"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			if n, ok := m["name"].(string); ok {
				set[n] = true
			}
		}
		cols[id] = set
	}
	if _, ok := tableIDs["data.tables.base"]; !ok {
		c.errf("contracts/data/tables/_base.yaml: missing data.tables.base")
	}
	return tableIDs, cols
}

func (c *Checker) checkBaseTable(doc map[string]any, schema *Schema, rel string) {
	if filepath.Base(rel) != "_base.yaml" {
		c.errf("%s: base table must live in _base.yaml", rel)
	}
	allowed := map[string]bool{
		"schema_version": true, "id": true, "kind": true, "summary": true,
		"stability": true, "do_not": true, "columns": true,
	}
	c.validateDoc(doc, filterFields(schema.Fields, allowed), rel)
	c.checkStability(doc, rel)
	if s, ok := doc["summary"].(string); ok && s == "" {
		c.errf("%s: summary must not be empty", rel)
	}
	if doc["kind"] != "base" {
		c.errf("%s: base table kind must be base", rel)
	}
	if doc["id"] != "data.tables.base" {
		c.errf("%s: base table id must be data.tables.base", rel)
	}
	names := []string{}
	for _, raw := range asList(doc["columns"]) {
		m, ok := asMap(raw)
		if !ok {
			c.errf("%s: columns must be objects", rel)
			continue
		}
		if n, ok := m["name"].(string); ok {
			names = append(names, n)
			if pk, _ := m["primary_key"].(bool); pk {
				if nullable, _ := m["nullable"].(bool); nullable {
					c.errf("%s: column %q: primary_key column must not be nullable", rel, n)
				}
			}
		}
	}
	want := append([]string(nil), baseColumns...)
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		c.errf("%s: base columns must be %v, got %v", rel, want, names)
	}
}

// constraintAllowedFields 声明每种约束允许出现的字段。
// 约束是平铺的 union，必须按 type 收窄，避免把 expr 写在 unique 上。
var constraintAllowedFields = map[string]map[string]bool{
	"check":   {"name": true, "type": true, "expr": true},
	"enum":    {"name": true, "type": true, "column": true, "enum": true},
	"unique":  {"name": true, "type": true, "columns": true},
	"exclude": {"name": true, "type": true, "expr": true},
}

// constraintKnownFields 是 item_fields 的并集；完全未知的字段交给 validateDoc 报错，
// 这里只处理“字段存在但不适用于当前 type”的情况，避免重复报错。
var constraintKnownFields = map[string]bool{
	"name": true, "type": true, "expr": true, "column": true, "enum": true, "columns": true,
}

func (c *Checker) checkTableRules(doc map[string]any, rel string, schema *Schema) {
	id, _ := doc["id"].(string)
	if s, ok := doc["summary"].(string); ok && s == "" {
		c.errf("%s: summary must not be empty", rel)
	}
	c.checkStability(doc, rel)
	stem := strings.TrimSuffix(filepath.Base(rel), ".yaml")
	if want := "data.tables." + stem; id != want {
		c.errf("%s: id must be %s, got %s (file name is the source)", rel, want, id)
	}
	ext := asList(doc["extends"])
	if len(ext) != 1 || ext[0] != "data.tables.base" {
		c.errf("%s: extends must be [data.tables.base]", rel)
	}
	table, _ := asMap(doc["table"])
	if table != nil {
		if n, ok := table["name"].(string); ok && n == "" {
			c.errf("%s: table.name must not be empty", rel)
		}
		if _, ok := table["comment"]; ok && !hasNonEmptyString(table, "comment") {
			c.errf("%s: table.comment must not be empty", rel)
		}
	}

	columns := asList(doc["columns"])
	if len(columns) == 0 {
		c.errf("%s: columns must not be empty", rel)
	}
	colNames := map[string]bool{}
	allCols := map[string]bool{}
	for _, name := range baseColumns {
		allCols[name] = true
	}
	for i, raw := range columns {
		m, ok := asMap(raw)
		if !ok {
			c.errf("%s: columns[%d] must be an object", rel, i)
			continue
		}
		name, _ := m["name"].(string)
		if colNames[name] {
			c.errf("%s: columns[%d]: duplicate column %q", rel, i, name)
		}
		colNames[name] = true
		allCols[name] = true
		switch name {
		case "id", "created_at", "updated_at", "deleted_at":
			c.errf("%s: columns[%d]: base column %q must not be redeclared", rel, i, name)
		case "status":
			t, _ := m["type"].(string)
			if !strings.HasPrefix(t, "data.enums.") {
				c.errf("%s: columns[%d]: status must reference data.enums.*, got %q", rel, i, t)
			}
		}
		if pk, _ := m["primary_key"].(bool); pk {
			if nullable, _ := m["nullable"].(bool); nullable {
				c.errf("%s: columns[%d] (%s): primary_key column must not be nullable", rel, i, name)
			}
		}
		if _, ok := m["default"]; ok {
			if _, ok2 := m["default_sql"]; ok2 {
				c.errf("%s: columns[%d] (%s): default and default_sql are mutually exclusive", rel, i, name)
			}
		}
		if t, _ := m["type"].(string); !isColumnType(t, schema.ColumnTypes, schema.ColumnRefs.Prefixes) {
			c.errf("%s: columns[%d] (%s): unsupported type %q (families %v or column_refs.prefixes %v)", rel, i, name, t, familyNames(schema.ColumnTypes), schema.ColumnRefs.Prefixes)
		} else {
			c.checkFamilyParams(m, t, rel, i, name, schema)
		}
	}
	if !colNames["status"] {
		c.errf("%s: status column must be declared and reference data.enums.*", rel)
	}

	checkCols := func(where string, list []any) {
		for _, x := range list {
			s, ok := x.(string)
			if !ok {
				c.errf("%s: column name must be string", where)
				continue
			}
			if !allCols[s] {
				c.errf("%s: unknown column %q", where, s)
			}
		}
	}
	if table != nil {
		if p, ok := asMap(table["partition"]); ok {
			pcols := asList(p["columns"])
			if len(pcols) == 0 {
				c.errf("%s: table.partition.columns must not be empty", rel)
			}
			checkCols(fmt.Sprintf("%s: table.partition", rel), pcols)
			strategy, _ := p["strategy"].(string)
			interval, _ := p["interval"].(string)
			if strategy == "range" && strings.TrimSpace(interval) == "" {
				c.errf("%s: table.partition.interval is required for range", rel)
			}
			if strategy != "range" && strings.TrimSpace(interval) != "" {
				c.errf("%s: table.partition.interval is only allowed for range", rel)
			}
		}
	}

	constraintNames := map[string]bool{}
	for i, raw := range asList(doc["constraints"]) {
		m, ok := asMap(raw)
		if !ok {
			c.errf("%s: constraints[%d] must be an object", rel, i)
			continue
		}
		name, _ := m["name"].(string)
		if !constraintNames[name] {
			constraintNames[name] = true
		} else {
			c.errf("%s: constraints[%d]: duplicate constraint %q", rel, i, name)
		}
		typ, _ := m["type"].(string)
		if allowed, known := constraintAllowedFields[typ]; known {
			for k := range m {
				if !constraintKnownFields[k] {
					continue
				}
				if !allowed[k] {
					c.errf("%s: constraints[%d] (%s): field %q is not allowed for type %s", rel, i, name, k, typ)
				}
			}
		}
		switch typ {
		case "check":
			if !hasNonEmptyString(m, "expr") {
				c.errf("%s: constraints[%d] (%s): check requires expr", rel, i, name)
			}
			if !strings.HasPrefix(name, "ck_") {
				c.errf("%s: constraints[%d] (%s): check name must start with ck_", rel, i, name)
			}
		case "enum":
			if !hasNonEmptyString(m, "column") || !hasNonEmptyString(m, "enum") {
				c.errf("%s: constraints[%d] (%s): enum requires column and enum", rel, i, name)
			} else if col, _ := m["column"].(string); !allCols[col] {
				c.errf("%s: constraints[%d] (%s): unknown column %q", rel, i, name, col)
			}
			if !strings.HasPrefix(name, "ck_") {
				c.errf("%s: constraints[%d] (%s): enum name must start with ck_", rel, i, name)
			}
		case "unique":
			cols := asList(m["columns"])
			if len(cols) == 0 {
				c.errf("%s: constraints[%d] (%s): unique requires columns", rel, i, name)
			}
			checkCols(fmt.Sprintf("%s: constraints[%d] (%s)", rel, i, name), cols)
			if !strings.HasPrefix(name, "uq_") {
				c.errf("%s: constraints[%d] (%s): unique name must start with uq_", rel, i, name)
			}
		case "exclude":
			if !hasNonEmptyString(m, "expr") {
				c.errf("%s: constraints[%d] (%s): exclude requires expr", rel, i, name)
			}
			if !strings.HasPrefix(name, "ex_") {
				c.errf("%s: constraints[%d] (%s): exclude name must start with ex_", rel, i, name)
			}
		default:
			c.errf("%s: constraints[%d] (%s): unknown type %q", rel, i, name, typ)
		}
	}

	indexNames := map[string]bool{}
	sizeClass := ""
	if table != nil {
		sizeClass, _ = table["size_class"].(string)
	}
	for i, raw := range asList(doc["indexes"]) {
		m, ok := asMap(raw)
		if !ok {
			c.errf("%s: indexes[%d] must be an object", rel, i)
			continue
		}
		name, _ := m["name"].(string)
		if !indexNames[name] {
			indexNames[name] = true
		} else {
			c.errf("%s: indexes[%d]: duplicate index %q", rel, i, name)
		}
		if _, ok := m["reason"]; ok && !hasNonEmptyString(m, "reason") {
			c.errf("%s: indexes[%d] (%s): reason must not be empty", rel, i, name)
		}
		hasCols := len(asList(m["columns"])) > 0
		hasExpr := hasNonEmptyString(m, "expression")
		if hasCols == hasExpr {
			c.errf("%s: indexes[%d] (%s): exactly one of columns/expression is required", rel, i, name)
		}
		if hasCols {
			checkCols(fmt.Sprintf("%s: indexes[%d] (%s)", rel, i, name), asList(m["columns"]))
		}
		if order := asList(m["order"]); len(order) > 0 {
			if !hasCols {
				c.errf("%s: indexes[%d] (%s): order requires columns", rel, i, name)
			} else if len(order) != len(asList(m["columns"])) {
				c.errf("%s: indexes[%d] (%s): order length must match columns", rel, i, name)
			}
		}
		if sizeClass == "large" {
			if conc, _ := m["concurrently"].(string); conc == "false" {
				c.errf("%s: indexes[%d] (%s): large table must not disable CONCURRENTLY", rel, i, name)
			}
		}
		checkCols(fmt.Sprintf("%s: indexes[%d] (%s).include", rel, i, name), asList(m["include"]))
		unique, _ := m["unique"].(bool)
		if unique {
			if !strings.HasPrefix(name, "uq_") {
				c.errf("%s: indexes[%d] (%s): unique name must start with uq_", rel, i, name)
			}
			where, _ := m["where"].(string)
			if !strings.Contains(where, "deleted_at IS NULL") {
				c.errf("%s: indexes[%d] (%s): unique index must filter deleted_at IS NULL", rel, i, name)
			}
		} else if !strings.HasPrefix(name, "idx_") {
			c.errf("%s: indexes[%d] (%s): non-unique name must start with idx_", rel, i, name)
		}
	}
	c.checkDoNotSpecificity(doc, rel, stem, table, colNames, constraintNames, indexNames)
}

// checkDoNotSpecificity 落实 contracts/README §4.5.1：
// 每条 do_not 必须含本表的具体标识，通用规则应上移 _types.yaml。
func (c *Checker) checkDoNotSpecificity(doc map[string]any, rel, stem string, table map[string]any, colNames, constraintNames, indexNames map[string]bool) {
	tokens := map[string]bool{}
	add := func(s string) {
		if utf8.RuneCountInString(s) >= 3 {
			tokens[s] = true
		}
	}
	add(stem)
	if table != nil {
		if n, ok := table["name"].(string); ok {
			add(n)
		}
	}
	for n := range colNames {
		if !isBaseColumn(n) {
			add(n)
		}
	}
	for n := range constraintNames {
		add(n)
	}
	for n := range indexNames {
		add(n)
	}
	for _, raw := range asList(doc["columns"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		if t, _ := m["type"].(string); strings.HasPrefix(t, "data.enums.") {
			add(strings.TrimPrefix(t, "data.enums."))
		}
	}
	for _, raw := range asList(doc["constraints"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		if e, _ := m["enum"].(string); strings.HasPrefix(e, "data.enums.") {
			add(strings.TrimPrefix(e, "data.enums."))
		}
	}
	for i, raw := range asList(doc["do_not"]) {
		s, ok := raw.(string)
		if !ok {
			continue
		}
		found := false
		for tok := range tokens {
			if strings.Contains(s, tok) {
				found = true
				break
			}
		}
		if !found {
			c.errf("%s: do_not[%d] must mention a specific identifier of table %q (file stem, physical name, column, constraint, index or enum)", rel, i, stem)
		}
	}
}

func isBaseColumn(name string) bool {
	for _, n := range baseColumns {
		if n == name {
			return true
		}
	}
	return false
}

func (c *Checker) checkStability(doc map[string]any, rel string) {
	stability, _ := doc["stability"].(string)
	if stability == "stable" {
		b, err := os.ReadFile(filepath.Join(c.root, rel))
		if err == nil {
			for _, marker := range []string{"TODO", "FIXME"} {
				if strings.Contains(string(b), marker) {
					c.errf("%s: stable contract contains %s placeholder", rel, marker)
				}
			}
		}
	}
}

// commonColumnFields 从 schema 的 columns.item_fields 派生通用列字段。
// 类型族扩展字段由 families.params 注入，必须排除，避免把
// “只属于某个族”的字段误认为所有列都可写。
func commonColumnFields(schema *Schema) map[string]bool {
	cols, ok := contractspec.FindField(schema.Fields, "columns")
	if !ok {
		return nil
	}
	familyParams := map[string]bool{}
	for _, fam := range schema.ColumnTypes.ValueTypeFamilies {
		for k := range fam.Params {
			familyParams[k] = true
		}
	}
	out := map[string]bool{}
	for _, f := range cols.ItemFields {
		if !familyParams[f.Name] {
			out[f.Name] = true
		}
	}
	return out
}

func familyNames(ct ColumnTypes) []string {
	out := make([]string, 0, len(ct.ValueTypeFamilies))
	for k := range ct.ValueTypeFamilies {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkFamilyParams 校验列上的类型族扩展字段：
//   - 该族不允许的扩展字段直接报错；
//   - required: true 的扩展字段必须出现；
//   - 出现时按族定义校验类型、枚举、范围；
//   - default 的 YAML 类型、长度、范围必须与族定义兼容。
func (c *Checker) checkFamilyParams(m map[string]any, t, rel string, i int, name string, schema *Schema) {
	common := commonColumnFields(schema)
	fam, ok := schema.ColumnTypes.ValueTypeFamilies[t]
	if !ok {
		if common != nil {
			for k := range m {
				if !common[k] {
					c.errf("%s: columns[%d] (%s): param %q not allowed for type %s", rel, i, name, k, t)
				}
			}
		}
		return
	}
	allowed := map[string]bool{}
	for k := range common {
		allowed[k] = true
	}
	for k := range fam.Params {
		allowed[k] = true
	}
	if common != nil {
		for k := range m {
			if !allowed[k] {
				c.errf("%s: columns[%d] (%s): param %q not allowed for type %s", rel, i, name, k, t)
			}
		}
	}
	for pk, def := range fam.Params {
		v, present := m[pk]
		if def.Required && !present {
			c.errf("%s: columns[%d] (%s): missing required param %q for type %s", rel, i, name, pk, t)
			continue
		}
		if present {
			c.validateValue(v, def, fmt.Sprintf("%s.columns[%d].%s", rel, i, pk))
			if pk == "pattern" {
				if s, ok := v.(string); ok {
					if _, err := regexp.Compile(s); err != nil {
						c.errf("%s: columns[%d] (%s): invalid pattern %q: %v", rel, i, name, s, err)
					}
				}
			}
		}
	}
	c.checkColumnDefaultType(m, t, rel, i, name)
	switch t {
	case "string", "bytes":
		if a, ok := toInt(m["min_len"]); ok {
			if b, ok2 := toInt(m["max_len"]); ok2 && a > b {
				c.errf("%s: columns[%d] (%s): min_len > max_len", rel, i, name)
			}
		}
		if fixed, ok := m["fixed"].(bool); ok && fixed {
			if _, ok := m["max_len"]; !ok {
				c.errf("%s: columns[%d] (%s): fixed requires max_len", rel, i, name)
			} else if minLen, ok := toInt(m["min_len"]); ok {
				if maxLen, ok2 := toInt(m["max_len"]); ok2 && minLen != maxLen {
					c.errf("%s: columns[%d] (%s): fixed requires min_len == max_len", rel, i, name)
				}
			}
		}
		if s, ok := m["default"].(string); ok {
			n := utf8.RuneCountInString(s)
			if t == "bytes" {
				n = len(s)
			}
			if minLen, ok := toInt(m["min_len"]); ok && n < minLen {
				c.errf("%s: columns[%d] (%s): default length %d < min_len %d", rel, i, name, n, minLen)
			}
			if maxLen, ok := toInt(m["max_len"]); ok && n > maxLen {
				c.errf("%s: columns[%d] (%s): default length %d > max_len %d", rel, i, name, n, maxLen)
			}
		}
	case "integer", "float", "decimal":
		if a, ok := toFloat(m["min"]); ok {
			if b, ok2 := toFloat(m["max"]); ok2 && a > b {
				c.errf("%s: columns[%d] (%s): min > max", rel, i, name)
			}
		}
		if t == "decimal" {
			if p, ok := toInt(m["precision"]); ok {
				if s, ok2 := toInt(m["scale"]); ok2 && s > p {
					c.errf("%s: columns[%d] (%s): scale > precision", rel, i, name)
				}
			}
		}
		if t == "integer" {
			if unsigned, _ := m["unsigned"].(bool); unsigned {
				if v, ok := toInt(m["min"]); ok && v < 0 {
					c.errf("%s: columns[%d] (%s): unsigned integer min must be >= 0", rel, i, name)
				}
				if v, ok := toInt(m["default"]); ok && v < 0 {
					c.errf("%s: columns[%d] (%s): unsigned integer default must be >= 0", rel, i, name)
				}
			}
			if def, ok := toInt(m["default"]); ok {
				if min, ok := toInt(m["min"]); ok && def < min {
					c.errf("%s: columns[%d] (%s): default %d < min %d", rel, i, name, def, min)
				}
				if max, ok := toInt(m["max"]); ok && def > max {
					c.errf("%s: columns[%d] (%s): default %d > max %d", rel, i, name, def, max)
				}
			}
		} else if def, ok := toFloat(m["default"]); ok {
			if min, ok := toFloat(m["min"]); ok && def < min {
				c.errf("%s: columns[%d] (%s): default %v < min %v", rel, i, name, def, min)
			}
			if max, ok := toFloat(m["max"]); ok && def > max {
				c.errf("%s: columns[%d] (%s): default %v > max %v", rel, i, name, def, max)
			}
		}
	}
}

// checkColumnDefaultType 校验字面量 default 的 YAML 类型与列类型族兼容。
// 枚举列的默认值由 checkEnumRefs 结合枚举值校验；表列引用暂不展开校验。
func (c *Checker) checkColumnDefaultType(m map[string]any, t, rel string, i int, name string) {
	def, ok := m["default"]
	if !ok || def == nil {
		return
	}
	switch t {
	case "string", "bytes":
		if _, ok := def.(string); !ok {
			c.errf("%s: columns[%d] (%s): default must be string for type %s", rel, i, name, t)
		}
	case "datetime", "timestamp":
		switch def.(type) {
		case string, time.Time:
		default:
			c.errf("%s: columns[%d] (%s): default must be string/time for type %s", rel, i, name, t)
		}
	case "boolean":
		if _, ok := def.(bool); !ok {
			c.errf("%s: columns[%d] (%s): default must be boolean for type %s", rel, i, name, t)
		}
	case "integer":
		if _, ok := toInt(def); !ok {
			c.errf("%s: columns[%d] (%s): default must be integer for type %s", rel, i, name, t)
		}
	case "float", "decimal":
		if _, ok := toFloat(def); !ok {
			c.errf("%s: columns[%d] (%s): default must be number for type %s", rel, i, name, t)
		}
	case "json":
		// JSON 文档允许任意 YAML 结构。
	}
}
