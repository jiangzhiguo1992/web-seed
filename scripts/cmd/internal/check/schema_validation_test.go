package check

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

func TestValueValidationLiterals(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, _, err := contractspec.LoadSchemaByType(root, "cap.services")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "scalar string",
			body: "type: _types.basics.string\nvalidation:\n  - { rule: 'value != \"x\"', message: \"m\" }\n",
		},
		{
			name: "scalar integer",
			body: "type: _types.basics.integer.32\nvalidation:\n  - { rule: 'value != \"x\"', message: \"m\" }\n",
			want: "is not an integer for _types.basics.integer.32",
		},
		{
			name: "scalar custom integer",
			body: "type: _types.basics.id\nvalidation:\n  - { rule: 'value != \"x\"', message: \"m\" }\n",
			want: "is not an integer for _types.basics.id",
		},
		{
			name: "scalar null is allowed",
			body: "type: _types.basics.integer.32\nvalidation:\n  - { rule: 'value != null', message: \"m\" }\n",
		},
		{
			name: "scalar precedence",
			body: "type: _types.basics.integer.32\nvalidation:\n  - { rule: 'value != 0 && (value != 1 || value != \"x\")', message: \"m\" }\n",
			want: "is not an integer for _types.basics.integer.32",
		},
		{
			name: "array null only",
			body: "kind: _types.schemas.array\nitems: { type: _types.basics.string }\nvalidation:\n  - { rule: 'value != \"x\"', message: \"m\" }\n",
			want: "array must be compared with null only",
		},
		{
			name: "ref null only",
			body: "type: data.entities.account\nvalidation:\n  - { rule: 'value != \"x\"', message: \"m\" }\n",
			want: "ref must be compared with null only",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var node map[string]any
			if err := yaml.Unmarshal([]byte(tc.body), &node); err != nil {
				t.Fatal(err)
			}
			c := &Checker{
				nodeKinds:    schema.NodeKinds,
				propertyKeys: schema.PropertyKeys,
				valueTypes:   schema.ValueTypes,
				entities:     map[string]*entityInfo{"data.entities.account": {ID: "data.entities.account"}},
			}
			c.validateSchemaNode(node, "schemas.X")
			got := strings.Join(c.errs, "\n")
			if tc.want == "" {
				if got != "" {
					t.Fatalf("unexpected errors: %s", got)
				}
				return
			}
			if !strings.Contains(got, "[schema.validation]") || !strings.Contains(got, tc.want) {
				t.Fatalf("value literal not validated: %s", got)
			}
		})
	}
}

func TestObjectValidationLiterals(t *testing.T) {
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	schema, _, err := contractspec.LoadSchemaByType(root, "cap.services")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		rule string
		want string
	}{
		{name: "valid enum", rule: `input.auth_type != "pwd"`},
		{name: "invalid enum", rule: `input.auth_type != "bogus"`, want: `"bogus" is not a value of data.enums.auth_type`},
		{name: "enum requires string", rule: `input.auth_type != 1`, want: "requires a string literal"},
		{name: "integer type", rule: `input.count != "x"`, want: "is not an integer for _types.basics.integer.32"},
		{name: "custom integer type", rule: `input.account_id != "x"`, want: "is not an integer for _types.basics.id"},
		{name: "entity null only", rule: `input.account != "x"`, want: "data.entities.account must be compared with null only"},
		{name: "container null only", rule: `input.tags != "x"`, want: "_types.schemas.array must be compared with null only"},
		{name: "custom string type", rule: `input.identifier != 1`, want: "is not a string for _types.basics.auth_identifier"},
		{name: "null is allowed", rule: `input.account != null`},
		{name: "container null is allowed", rule: `input.tags != null`},
		{name: "nested expression enum", rule: `(input.auth_type == "pwd" && input.password != null) || input.auth_type == "bogus"`, want: `"bogus" is not a value of data.enums.auth_type`},
		{name: "nested expression valid", rule: `(input.auth_type == "pwd" || input.auth_type == "phone") && input.password != null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var node map[string]any
			body := `kind: _types.schemas.object
properties:
  - { name: auth_type, schema: { type: data.enums.auth_type }, required: true, description: "认证类型" }
  - { name: count, schema: { type: _types.basics.integer.32 }, description: "计数" }
  - { name: account_id, schema: { type: _types.basics.id }, description: "账号 ID" }
  - { name: account, schema: { type: data.entities.account }, description: "账号" }
  - { name: tags, schema: { kind: _types.schemas.array, items: { type: _types.basics.string } }, description: "标签" }
  - { name: identifier, schema: { type: _types.basics.auth_identifier }, description: "标识" }
  - { name: password, schema: { type: _types.basics.string }, nullable: true, description: "密码" }
validation:
  - { rule: '` + tc.rule + `', message: "校验失败" }
`
			if err := yaml.Unmarshal([]byte(body), &node); err != nil {
				t.Fatal(err)
			}
			c := &Checker{
				nodeKinds:    schema.NodeKinds,
				propertyKeys: schema.PropertyKeys,
				valueTypes:   schema.ValueTypes,
				enumValues:   map[string]map[string]bool{"data.enums.auth_type": {"pwd": true, "phone": true}},
				entities:     map[string]*entityInfo{"data.entities.account": {ID: "data.entities.account"}},
			}
			c.validateSchemaNode(node, "schemas.X")
			got := strings.Join(c.errs, "\n")
			if tc.want == "" {
				if got != "" {
					t.Fatalf("unexpected errors: %s", got)
				}
				return
			}
			if !strings.Contains(got, "[schema.validation]") || !strings.Contains(got, tc.want) {
				t.Fatalf("literal not validated: %s", got)
			}
		})
	}
}
