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
// storage.yaml 的表格条目目前是一行 flow mapping；找不到对应行时明确失败，避免静默漏写。
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
	pending := map[string]string{}
	for _, entry := range storageNormalEntries(doc) {
		if want := summaries[entry.table]; want != "" && entry.desc != want {
			pending[entry.table] = want
		}
	}
	if len(pending) == 0 {
		return false, nil
	}

	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		for table, summary := range pending {
			if !strings.Contains(line, "table: "+table+",") && !strings.Contains(line, "table: "+table+" }") {
				continue
			}
			updated, ok := replaceFlowDescription(line, summary)
			if !ok {
				return false, fmt.Errorf("%s: cannot locate description for %s on one line", storageRel, table)
			}
			lines[i] = updated
			delete(pending, table)
			break
		}
	}
	if len(pending) > 0 {
		missing := make([]string, 0, len(pending))
		for table := range pending {
			missing = append(missing, table)
		}
		sort.Strings(missing)
		return false, fmt.Errorf("%s: cannot locate table entries %v", storageRel, missing)
	}
	out := []byte(strings.Join(lines, "\n"))
	if err := writeAtomic(path, out); err != nil {
		return false, err
	}
	return true, nil
}

// replaceFlowDescription 替换一行 flow mapping 中的 description 标量，保留其余排版。
func replaceFlowDescription(line, summary string) (string, bool) {
	idx := strings.Index(line, "description:")
	if idx < 0 {
		return line, false
	}
	start := idx + len("description:")
	for start < len(line) && (line[start] == ' ' || line[start] == '\t') {
		start++
	}
	end := flowScalarEnd(line, start)
	if end < 0 {
		return line, false
	}
	return line[:idx] + "description: " + strconv.Quote(summary) + line[end:], true
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
