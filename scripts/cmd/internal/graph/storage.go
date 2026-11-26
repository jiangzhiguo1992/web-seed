package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const storageRel = "contracts/data/storage.yaml"

type storageDescription struct {
	table string
	desc  string
}

// storageNormalEntries 返回 storage.tables 中非归档条目的 table 与 description。
func storageNormalEntries(doc map[string]any) []storageDescription {
	tables, _ := asMap(doc["tables"])
	var out []storageDescription
	for _, raw := range tables {
		for _, rawEntry := range asList(raw) {
			entry, _ := asMap(rawEntry)
			if entry == nil {
				continue
			}
			if _, archive := entry["archive_of"]; archive {
				continue
			}
			table, _ := entry["table"].(string)
			if table == "" {
				continue
			}
			desc, _ := entry["description"].(string)
			out = append(out, storageDescription{table: table, desc: desc})
		}
	}
	return out
}

// storageDescriptionDrift 对比普通条目 description 与表 summary。
func storageDescriptionDrift(root string, summaries map[string]string) ([]string, error) {
	path := filepath.Join(root, storageRel)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var drift []string
	for _, entry := range storageNormalEntries(doc) {
		want := summaries[entry.table]
		if want == "" {
			continue // 表自身缺失 summary 由 contract-check 报出
		}
		if entry.desc != want {
			drift = append(drift, fmt.Sprintf("%s: tables entry %s description must be %q, got %q", storageRel, entry.table, want, entry.desc))
		}
	}
	return drift, nil
}

// syncStorageDescriptions 把普通条目的 description 写回表 summary；归档条目保持手写。
// 用 YAML 节点位置只替换 description 标量，支持 flow / block / 多行条目并保留其余排版。
func syncStorageDescriptions(root string, summaries map[string]string) (bool, error) {
	path := filepath.Join(root, storageRel)
	src, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	wanted := map[string]string{}
	for _, entry := range storageNormalEntries(doc) {
		if want := summaries[entry.table]; want != "" && entry.desc != want {
			wanted[entry.table] = want
		}
	}
	if len(wanted) == 0 {
		return false, nil
	}

	var rootNode yaml.Node
	if err := yaml.Unmarshal(src, &rootNode); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	var replacements []textReplacement
	for _, entry := range storageDescriptionNodes(&rootNode) {
		want, ok := wanted[entry.table]
		if !ok {
			continue
		}
		delete(wanted, entry.table)
		if entry.value.Value == want {
			continue
		}
		start, end, err := descriptionSpan(string(src), entry.key, entry.value)
		if err != nil {
			return false, fmt.Errorf("%s: tables entry %s: %w", storageRel, entry.table, err)
		}
		text := strconv.Quote(want)
		if strings.Contains(string(src[start:end]), "\n") {
			text += "\n" // 多行/块标量替换时连同后续内容行一起收成一行
		}
		replacements = append(replacements, textReplacement{start: start, end: end, text: text})
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for table := range wanted {
			missing = append(missing, table)
		}
		sort.Strings(missing)
		return false, fmt.Errorf("%s: cannot locate description for normal entries %v", storageRel, missing)
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	out := string(src)
	for _, r := range replacements {
		out = out[:r.start] + r.text + out[r.end:]
	}
	if err := writeAtomic(path, []byte(out)); err != nil {
		return false, err
	}
	return true, nil
}

type storageDescriptionNode struct {
	table string
	key   *yaml.Node
	value *yaml.Node
}

type textReplacement struct {
	start int
	end   int
	text  string
}

// storageDescriptionNodes 从 YAML AST 中取出普通条目的 description 键/值节点。
func storageDescriptionNodes(root *yaml.Node) []storageDescriptionNode {
	doc := root
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		doc = doc.Content[0]
	}
	_, tables := mappingEntry(doc, "tables")
	if tables == nil || tables.Kind != yaml.MappingNode {
		return nil
	}
	var out []storageDescriptionNode
	for i := 0; i+1 < len(tables.Content); i += 2 {
		seq := tables.Content[i+1]
		if seq.Kind != yaml.SequenceNode {
			continue
		}
		for _, entry := range seq.Content {
			if entry.Kind != yaml.MappingNode {
				continue
			}
			if _, archive := mappingEntry(entry, "archive_of"); archive != nil {
				continue
			}
			_, tableNode := mappingEntry(entry, "table")
			key, value := mappingEntry(entry, "description")
			if tableNode == nil || tableNode.Value == "" || key == nil || value == nil {
				continue
			}
			out = append(out, storageDescriptionNode{table: tableNode.Value, key: key, value: value})
		}
	}
	return out
}

func mappingEntry(n *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i], n.Content[i+1]
		}
	}
	return nil, nil
}

// descriptionSpan 返回 description 标量在源文件中的字节区间；块标量覆盖其内容行。
func descriptionSpan(src string, key, value *yaml.Node) (int, int, error) {
	if key == nil || value == nil || value.Line <= 0 || value.Column <= 0 {
		return 0, 0, fmt.Errorf("description has no source position")
	}
	lines := strings.Split(src, "\n")
	starts := lineStarts(src)
	if value.Line > len(lines) || value.Line-1 >= len(starts) {
		return 0, 0, fmt.Errorf("description line %d is outside the file", value.Line)
	}
	lineStart := starts[value.Line-1]
	start := lineStart + value.Column - 1
	if start < lineStart || start > len(src) {
		return 0, 0, fmt.Errorf("description column %d is outside line %d", value.Column, value.Line)
	}
	block := value.Style == yaml.LiteralStyle || value.Style == yaml.FoldedStyle || strings.Contains(value.Value, "\n")
	if !block {
		end := lineStart + flowScalarEnd(lines[value.Line-1], value.Column-1)
		if end > start && end <= len(src) {
			return start, end, nil
		}
		// 未闭合的引号跨多行时，按块标量处理。
	}
	keyIndent := key.Column - 1
	endLine := value.Line // line index after the description key (0-based)
	for ; endLine < len(lines); endLine++ {
		line := lines[endLine]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if leadingSpaces(line) <= keyIndent {
			break
		}
	}
	end := len(src)
	if endLine < len(lines) {
		end = starts[endLine]
	}
	if end <= start {
		return 0, 0, fmt.Errorf("description span is empty")
	}
	return start, end, nil
}

func lineStarts(src string) []int {
	starts := []int{0}
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func leadingSpaces(line string) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}

// flowScalarEnd 返回 flow mapping 中标量的结束位置（不含）。
func flowScalarEnd(s string, start int) int {
	if start >= len(s) {
		return -1
	}
	switch s[start] {
	case '"':
		escaped := false
		for i := start + 1; i < len(s); i++ {
			if escaped {
				escaped = false
				continue
			}
			switch s[i] {
			case '\\':
				escaped = true
			case '"':
				return i + 1
			}
		}
	case '\'':
		for i := start + 1; i < len(s); i++ {
			if s[i] != '\'' {
				continue
			}
			if i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			return i + 1
		}
	default:
		for i := start; i < len(s); i++ {
			if s[i] == ',' || s[i] == '}' {
				return i
			}
		}
	}
	return -1
}
