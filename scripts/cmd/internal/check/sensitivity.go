// sensitivity.go 定义列/实体字段的敏感级别与比较规则。
package check

const (
	sensitivityPublic = "public"
	sensitivityPII    = "pii"
	sensitivitySecret = "secret"
)

func normalizeSensitivity(s string) string {
	if s == "" {
		return sensitivityPublic
	}
	return s
}

// sensitivityRank 把敏感级别映射为可比较的序：public < pii < secret。
func sensitivityRank(s string) int {
	switch normalizeSensitivity(s) {
	case sensitivitySecret:
		return 2
	case sensitivityPII:
		return 1
	default:
		return 0
	}
}
