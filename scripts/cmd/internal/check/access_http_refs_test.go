package check

import (
	"strings"
	"testing"
)

func TestAccessTargetRule(t *testing.T) {
	c := &Checker{
		accessBindings: map[string]accessPolicy{"cap.services.x.get": {}},
		httpOps:        map[string]httpOpInfo{},
	}
	c.checkAccessTargets()
	mustContainRule(t, c.errs, "access-target")

	c = &Checker{
		accessBindings: map[string]accessPolicy{"cap.services.x.get": {}},
		httpOps:        map[string]httpOpInfo{"getX": {Service: "cap.services.x.get"}},
	}
	c.checkAccessTargets()
	if len(c.errs) != 0 {
		t.Fatalf("exposed target reported: %v", c.errs)
	}
}

func TestHTTPAppIncludedRule(t *testing.T) {
	root := t.TempDir()
	schema := copyMeta(t, root, "cap.protocols.http")
	writeCapFile(t, root, "contracts/project.yaml", `id: project
schema_version: 1
apps:
  admin:
    kind: api
    audience: admin
    generated: { spec: generated/openapi/admin.yaml }
    includes: []
`)
	writeCapFile(t, root, "contracts/capabilities/protocols/http/x.yaml", `id: cap.protocols.http.x
summary: "x"
owner_module: x
schema_version: 1
stability: draft
do_not:
  - "x 只用于骨架校验"
mount:
  api_version: v1
  path: /x
  audience: [ admin ]
paths:
  /admin: {}
`)
	c := &Checker{root: root, strictHTTP: true}
	c.checkHTTPBindings(schema, nil, nil, nil)
	if !strings.Contains(strings.Join(c.errs, "\n"), "[http-app-included]") {
		t.Fatalf("orphan http file not reported: %v", c.errs)
	}
}
