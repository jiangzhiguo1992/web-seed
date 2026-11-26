// targets.go 统一按 _meta targets 扫描契约文件。
// run() 会把各类型 targets 注入 Checker；单元测试直接构造 Checker 时回退到规范路径。
package check

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

var targetNamingPatterns = map[string]*regexp.Regexp{
	"snake_case": regexp.MustCompile(`^[a-z][a-z0-9_]*$`),
}

// checkTargets 校验 _meta 的 targets 声明是否可扫描、可派生 ID。
// 所有按文件派生的契约类型共用；单文件契约不调用。
func (c *Checker) checkTargets(schema *Schema) {
	t := schema.Targets
	where := schema.ID
	if t.Dir == "" || t.Glob == "" {
		c.rulef("schema.targets", "%s: targets.dir/glob must not be empty", where)
	}
	if t.ID == "" || !strings.Contains(t.ID, "<file>") {
		c.rulef("schema.targets", "%s: targets.id must contain <file>", where)
	}
	if _, ok := targetNamingPatterns[t.Naming]; !ok {
		c.rulef("schema.targets", "%s: targets.naming must be snake_case", where)
	}
	if t.Dir != "" {
		if info, err := os.Stat(filepath.Join(c.root, t.Dir)); err != nil || !info.IsDir() {
			c.rulef("schema.targets", "%s: targets.dir %q is not a directory", where, t.Dir)
		}
	}
}

// checkTargetFileName 校验实例文件名符合 targets.naming。
func (c *Checker) checkTargetFileName(schema *Schema, file, rel string) {
	re := targetNamingPatterns[schema.Targets.Naming]
	if re != nil && !re.MatchString(strings.TrimSuffix(file, filepath.Ext(file))) {
		c.rulef("schema.targets", "%s: file name %q does not match targets.naming %s", rel, file, schema.Targets.Naming)
	}
}

// checkStability 拒绝 TODO/FIXME 占位符；draft 不豁免任何校验。
func (c *Checker) checkStability(_ map[string]any, rel string) {
	b, err := os.ReadFile(filepath.Join(c.root, rel))
	if err != nil {
		return
	}
	for _, marker := range []string{"TODO", "FIXME"} {
		if strings.Contains(string(b), marker) {
			c.rulef("schema.stability", "%s: contract contains %s placeholder; stability does not exempt", rel, marker)
		}
	}
}

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
