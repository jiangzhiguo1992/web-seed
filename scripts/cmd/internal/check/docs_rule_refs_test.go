package check

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDocsRuleReferences 校验文档里的 [强制: <rule-id>] 都指向真实存在的 checker 规则。
// 规则被删除/改名后，文档引用必须同步，否则 AI 会把已失效的规则当成硬约束。
func TestDocsRuleReferences(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\[强制:\s*([a-z0-9_.-]+)\]`)
	for _, dir := range []string{"contracts", "docs"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel := relPath(root, path)
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				if !knownRuleIDs[m[1]] {
					t.Errorf("%s: unknown rule reference [强制: %s]", rel, m[1])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
