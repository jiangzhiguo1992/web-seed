// targets.go 统一按 _meta targets 扫描契约文件。
// run() 会把各类型 targets 注入 Checker；单元测试直接构造 Checker 时回退到规范路径。
package check

import (
	"path/filepath"
	"sort"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func (c *Checker) tableFiles() []string {
	return c.targetFiles(c.tableTargets, "contracts/data/tables")
}

func (c *Checker) entityFiles() []string {
	return c.targetFiles(c.entityTargets, "contracts/data/entities")
}

func (c *Checker) queryFiles() []string {
	return c.targetFiles(c.queryTargets, "contracts/data/queries")
}

// contractFiles 返回 contracts 下的全部 YAML，并合并可能位于 contracts 外的 targets 文件。
func (c *Checker) contractFiles() []string {
	files := walkYAML(c.root, "contracts")
	seen := make(map[string]bool, len(files))
	for _, file := range files {
		seen[file] = true
	}
	for _, file := range append(append(c.tableFiles(), c.entityFiles()...), c.queryFiles()...) {
		if seen[file] {
			continue
		}
		seen[file] = true
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

// tableDir 返回表契约扫描目录；checkEnumRefs 需要跳过表文件。
func (c *Checker) tableDir() string {
	if c.tableTargets.Dir != "" {
		return c.tableTargets.Dir
	}
	return "contracts/data/tables"
}

// targetFiles 优先使用 meta targets；dir/glob 都缺失时仅供直接调用 checker 的测试回退。
func (c *Checker) targetFiles(t contractspec.Targets, fallbackDir string) []string {
	if t.Dir == "" && t.Glob == "" {
		return glob(c.root, filepath.Join(fallbackDir, "*.yaml"))
	}
	if t.Dir == "" || t.Glob == "" {
		return nil
	}
	return glob(c.root, filepath.Join(t.Dir, t.Glob))
}
