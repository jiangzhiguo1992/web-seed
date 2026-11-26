package check

import (
	"strings"
	"testing"
)

func TestPageRouteBindings(t *testing.T) {
	root := t.TempDir()
	copyMeta(t, root, "feature.pages")
	writeCapFile(t, root, "contracts/features/routes.yaml", `id: feature.routes
routes:
  - { id: feature.routes.r1, page: feature.pages.p2 }
`)
	writeCapFile(t, root, "contracts/features/pages/p1.yaml", `id: feature.pages.p1
page: p1
route: feature.routes.r1
`)
	writeCapFile(t, root, "contracts/features/pages/p2.yaml", `id: feature.pages.p2
page: p2
route: feature.routes.r2
`)
	c := &Checker{root: root}
	c.checkPageRouteBindings()
	errs := strings.Join(c.errs, "\n")
	if !strings.Contains(errs, "[page-refs]") {
		t.Fatalf("page.route mismatch not reported: %v", c.errs)
	}
	if !strings.Contains(errs, "[route-structure]") {
		t.Fatalf("route.page mismatch not reported: %v", c.errs)
	}
}
