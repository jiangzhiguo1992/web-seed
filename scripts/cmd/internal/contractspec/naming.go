// naming.go 是契约命名约定的单一实现：query 项 ID 与 snake_case 派生。
package contractspec

import "strings"

// SnakeCase 把 Go 导出名转换为 snake_case；query 项 ID 与实体默认列名共用。
// 例：ID→id、OwnID→own_id、userID→user_id、TLD→tld、GetAccount→get_account。
func SnakeCase(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prev := rs[i-1]
				if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
					b.WriteByte('_')
				}
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// QueryID 由查询文件 ID 与 item name 派生 query 项 ID；name 为空时返回空。
func QueryID(fileID, name string) string {
	if fileID == "" || name == "" {
		return ""
	}
	return fileID + "." + SnakeCase(name)
}
