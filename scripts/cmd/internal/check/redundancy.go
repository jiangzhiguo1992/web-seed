// redundancy.go 校验 columns[].redundancy：
// 列直接列出同组全部列引用，source 标记权威列；组内各列必须声明完全相同的列表。
package check

import (
	"sort"
	"strconv"
	"strings"
)

// redundancyRef 是 columns[].redundancy 的一项。
type redundancyRef struct {
	column string
	source bool
}

// redundancyDecl 是一列声明的整组冗余引用。
type redundancyDecl struct {
	owner      string
	refs       []redundancyRef
	key        string
	wellFormed bool
}

// checkRedundancy 校验冗余列组：
//   - 每组恰好一个 source，声明列必须列出自身；
//   - 组内每列必须声明相同的列表，同一列不得跨组；
//   - 列引用必须存在，copy 与 source 的 type/nullable 必须一致。
func (c *Checker) checkRedundancy(tableIDs map[string]string) {
	decls := map[string]*redundancyDecl{}
	var order []string
	for _, id := range sortedStringMapKeys(tableIDs) {
		if id == c.baseID {
			continue
		}
		for _, raw := range asList(c.tableDocs[id]["columns"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			rawList, ok := m["redundancy"]
			if name == "" || !ok {
				continue
			}
			list, ok := asList2(rawList)
			if !ok {
				continue // 结构错误由 schema 校验报出
			}
			refs := make([]redundancyRef, 0, len(list))
			valid := true
			for _, item := range list {
				entry, ok := asMap(item)
				if !ok {
					valid = false
					break
				}
				column, _ := entry["column"].(string)
				source, _ := entry["source"].(bool)
				if column == "" {
					valid = false
					break
				}
				refs = append(refs, redundancyRef{column: column, source: source})
			}
			if !valid {
				continue // 结构错误由 schema 校验报出
			}
			owner := id + ".col." + name
			key := redundancyKey(refs)
			wellFormed := len(refs) >= 2
			seen := map[string]bool{}
			for _, ref := range refs {
				if seen[ref.column] {
					c.rulef("redundant-group", "%s: redundancy repeats column %s", owner, ref.column)
					wellFormed = false
				}
				seen[ref.column] = true
			}
			decls[owner] = &redundancyDecl{owner: owner, refs: refs, key: key, wellFormed: wellFormed}
			order = append(order, owner)
		}
	}
	sort.Strings(order)

	checkedGroup := map[string]bool{}
	reportedRef := map[string]bool{}
	reportedUndeclared := map[string]bool{}
	reportedMismatch := map[string]bool{}
	for _, owner := range order {
		decl := decls[owner]
		// 结构非法（空/单成员/重复列）的声明不参与组比较，避免一个根因级联出 list differs。
		if decl.wellFormed {
			// 组 key 只描述集合，不能代替逐声明的自包含校验。
			self := false
			for _, ref := range decl.refs {
				if ref.column == owner {
					self = true
					break
				}
			}
			if !self {
				c.rulef("redundant-group", "%s: redundancy must include the declaring column itself", owner)
			}
		}
		if decl.wellFormed && !checkedGroup[decl.key] {
			checkedGroup[decl.key] = true
			sources := 0
			sourceRef := ""
			for _, ref := range decl.refs {
				if ref.source {
					sources++
					sourceRef = ref.column
				}
			}
			metas := c.redundancyMemberMetas(decl, reportedRef)
			if sources != 1 {
				c.rulef("redundant-group", "%s: redundancy must mark exactly one source (got %d)", owner, sources)
			} else {
				c.checkRedundancyTypes(decl, metas, sourceRef)
			}
		}
		if !decl.wellFormed {
			continue
		}
		for _, ref := range decl.refs {
			if reportedRef[ref.column] {
				continue // 缺失/继承列已由 redundant-ref 报出
			}
			other, ok := decls[ref.column]
			if !ok {
				if !reportedUndeclared[ref.column] {
					c.rulef("redundant-group", "%s: redundancy lists %s but that column does not declare columns[].redundancy", owner, ref.column)
					reportedUndeclared[ref.column] = true
				}
				continue
			}
			if !other.wellFormed {
				continue // 对方结构非法，已由 schema/duplicate 报出
			}
			if other.key != decl.key {
				pair := owner + "\x00" + ref.column
				if owner > ref.column {
					pair = ref.column + "\x00" + owner
				}
				if !reportedMismatch[pair] {
					c.rulef("redundant-group", "%s: redundancy list differs from %s", owner, ref.column)
					reportedMismatch[pair] = true
				}
			}
		}
	}
}

// checkRedundancyTypes 比较 copy 与 source 的类型/可空。
func (c *Checker) checkRedundancyTypes(decl *redundancyDecl, metas map[string]columnMeta, sourceRef string) {
	srcMeta, ok := metas[sourceRef]
	if !ok {
		return
	}
	for _, ref := range decl.refs {
		if ref.source {
			continue
		}
		meta, ok := metas[ref.column]
		if !ok {
			continue
		}
		if c.resolvedColumnType(meta.typ) != c.resolvedColumnType(srcMeta.typ) || meta.nullable != srcMeta.nullable {
			c.rulef("redundant-ref", "%s: copy %s type/nullable (%s, nullable=%v) must match source %s (%s, nullable=%v)", decl.owner, ref.column, c.resolvedColumnType(meta.typ), meta.nullable, sourceRef, c.resolvedColumnType(srcMeta.typ), srcMeta.nullable)
		}
	}
}

// redundancyMemberMetas 校验组内全部列引用，返回可用列的元数据。
func (c *Checker) redundancyMemberMetas(decl *redundancyDecl, reported map[string]bool) map[string]columnMeta {
	metas := map[string]columnMeta{}
	for _, ref := range decl.refs {
		table, col, ok := c.checkRedundancyRef(decl.owner, ref.column, reported)
		if !ok {
			continue
		}
		metas[ref.column] = c.tableColumns[table][col]
	}
	return metas
}

// checkRedundancyRef 校验列引用存在且是本表自声明列；继承列与基类列不允许进入冗余组。
func (c *Checker) checkRedundancyRef(owner, ref string, reported map[string]bool) (string, string, bool) {
	table, col, ok := splitColumnRef(ref)
	if ok && table == baseContractID {
		if !reported[ref] {
			c.rulef("redundant-ref", "%s: redundancy column %q is a base column; members must be physical table columns", owner, ref)
			reported[ref] = true
		}
		return "", "", false
	}
	if !ok || !c.columnExists(table, col) {
		if !reported[ref] {
			c.rulef("redundant-ref", "%s: redundancy column %q is not a table column", owner, ref)
			reported[ref] = true
		}
		return "", "", false
	}
	if c.inheritedCols[table][col] {
		if !reported[ref] {
			c.rulef("redundant-ref", "%s: redundancy column %q is inherited; members must be declared on their own table", owner, ref)
			reported[ref] = true
		}
		return "", "", false
	}
	return table, col, true
}

// redundancyKey 生成与顺序无关的组标识：列引用 + source 标记。
func redundancyKey(refs []redundancyRef) string {
	items := make([]string, 0, len(refs))
	for _, ref := range refs {
		items = append(items, ref.column+"="+strconv.FormatBool(ref.source))
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}
