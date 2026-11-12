// tables.go 校验 data.tables：基类、列、约束、索引与物理表名。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yourorg/web-seed/apps/api/internal/contractspec"
	"gopkg.in/yaml.v3"
)

func (c *Checker) checkTables(schema *Schema) (map[string]string, map[string]map[string]bool) {
	files, _ := filepath.Glob(filepath.Join(c.root, "contracts/data/tables/*.yaml"))
	sort.Strings(files)
	tableIDs := map[string]string{}
	cols := map[string]map[string]bool{}
	physicalNames := map[string]string{}
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

		if doc["kind"] == "base" {
			c.checkBaseTable(doc, schema, rel)
		} else {
			c.validateDoc(doc, schema.Fields, rel)
			c.checkTableRules(doc, rel, schema)
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
		}
	}
	want := append([]string(nil), baseColumns...)
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		c.errf("%s: base columns must be %v, got %v", rel, want, names)
	}
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
		if r, ok := asMap(table["retention"]); ok {
			for k, v := range r {
				if i, ok := toInt(v); ok && i < 1 {
					c.errf("%s: table.retention.%s must be >= 1", rel, k)
				}
			}
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
			checkCols(fmt.Sprintf("%s: table.partition", rel), asList(p["columns"]))
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
		switch typ {
		case "check":
			if !hasNonEmptyString(m, "expr") {
				c.errf("%s: constraints[%d] (%s): check requires expr", rel, i, name)
			}
			if !strings.HasPrefix(name, "ck_") {
				c.errf("%s: constraints[%d]: check name must start with ck_", rel, i)
			}
		case "enum":
			if !hasNonEmptyString(m, "column") || !hasNonEmptyString(m, "enum") {
				c.errf("%s: constraints[%d] (%s): enum requires column and enum", rel, i, name)
			} else if col, _ := m["column"].(string); !allCols[col] {
				c.errf("%s: constraints[%d] (%s): unknown column %q", rel, i, name, col)
			}
			if !strings.HasPrefix(name, "ck_") {
				c.errf("%s: constraints[%d]: enum name must start with ck_", rel, i)
			}
		case "foreign_key":
			if len(asList(m["columns"])) == 0 || !hasNonEmptyString(m, "references") {
				c.errf("%s: constraints[%d] (%s): foreign_key requires columns and references", rel, i, name)
			}
			checkCols(fmt.Sprintf("%s: constraints[%d] (%s)", rel, i, name), asList(m["columns"]))
			if !strings.HasPrefix(name, "fk_") {
				c.errf("%s: constraints[%d]: foreign_key name must start with fk_", rel, i)
			}
		case "unique":
			if len(asList(m["columns"])) == 0 {
				c.errf("%s: constraints[%d] (%s): unique requires columns", rel, i, name)
			}
			checkCols(fmt.Sprintf("%s: constraints[%d] (%s)", rel, i, name), asList(m["columns"]))
			if !strings.HasPrefix(name, "uq_") {
				c.errf("%s: constraints[%d]: unique name must start with uq_", rel, i)
			}
		case "exclude":
			if !hasNonEmptyString(m, "expr") {
				c.errf("%s: constraints[%d] (%s): exclude requires expr", rel, i, name)
			}
			if !strings.HasPrefix(name, "ex_") {
				c.errf("%s: constraints[%d]: exclude name must start with ex_", rel, i)
			}
		default:
			c.errf("%s: constraints[%d] (%s): unknown type %q", rel, i, name, typ)
		}
	}

	indexNames := map[string]bool{}
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
		checkCols(fmt.Sprintf("%s: indexes[%d] (%s).include", rel, i, name), asList(m["include"]))
		unique, _ := m["unique"].(bool)
		if unique {
			if !strings.HasPrefix(name, "uq_") {
				c.errf("%s: indexes[%d]: unique name must start with uq_", rel, i)
			}
			where, _ := m["where"].(string)
			if !strings.Contains(where, "deleted_at IS NULL") {
				c.errf("%s: indexes[%d] (%s): unique index must filter deleted_at IS NULL", rel, i, name)
			}
		} else if !strings.HasPrefix(name, "idx_") {
			c.errf("%s: indexes[%d]: non-unique name must start with idx_", rel, i)
		}
	}
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
	if prev, ok := c.headStability(rel); ok && !stabilityTransitionAllowed(prev, stability) {
		c.errf("%s: invalid stability transition %s -> %s", rel, prev, stability)
	}
}

// stabilityTransitionAllowed 只允许向前流转：
// draft → stable、draft → deprecated、stable → deprecated。
// deprecated 不能回到 draft/stable；stable 不能回退到 draft。
func stabilityTransitionAllowed(prev, cur string) bool {
	if prev == cur {
		return true
	}
	switch prev {
	case "draft":
		return cur == "stable" || cur == "deprecated"
	case "stable":
		return cur == "deprecated"
	case "deprecated":
		return false
	}
	return true
}

// headStability 读取 HEAD 版本的状态；没有历史或不是 git 仓库时返回 false，
// 不阻断本地编辑和新文件。
func (c *Checker) headStability(rel string) (string, bool) {
	if c.root == "" {
		return "", false
	}
	out, err := exec.Command("git", "-C", c.root, "show", "HEAD:"+rel).Output()
	if err != nil {
		return "", false
	}
	var prev struct {
		Stability string `yaml:"stability"`
	}
	if yaml.Unmarshal(out, &prev) != nil || prev.Stability == "" {
		return "", false
	}
	return prev.Stability, true
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
//   - 出现时按族定义校验类型、枚举、范围。
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
	}
}
