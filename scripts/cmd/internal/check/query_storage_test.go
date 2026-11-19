package check

import (
	"strings"
	"testing"
)

func newQueryStorageChecker(q *queryInfo, caches map[string]storageCacheEntry, archives map[string]storageArchiveEntry) *Checker {
	return &Checker{
		queryOrder:     []string{q.ID},
		queries:        map[string]*queryInfo{q.ID: q},
		queryTables:    map[string]map[string]bool{q.ID: q.tableSet()},
		cacheEntries:   caches,
		archiveEntries: archives,
	}
}

func TestQueryCacheRefs(t *testing.T) {
	cacheRef := "data.tables.x.cache.code"
	caches := map[string]storageCacheEntry{
		cacheRef: {table: "data.tables.x", key: []string{"code"}},
	}
	readQ := &queryInfo{
		ID: "data.queries.x.q", File: "x.yaml",
		Targets: []string{"data.tables.x"},
		Params:  []queryParam{{Name: "code", Column: "data.tables.x.col.code"}},
		Caches:  []string{cacheRef},
	}

	c := newQueryStorageChecker(readQ, caches, nil)
	c.checkQueryStorageRefs()
	if len(c.errs) != 0 {
		t.Fatalf("valid read-through cache reported: %v", c.errs)
	}

	unboundQ := *readQ
	unboundQ.Params = nil
	c = newQueryStorageChecker(&unboundQ, caches, nil)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not bound by params") {
		t.Fatalf("unbound cache key not reported: %v", c.errs)
	}

	writeQ := &queryInfo{
		ID: "data.queries.x.w", File: "x.yaml",
		Targets: []string{"data.tables.x"}, Writes: []string{"data.tables.x"},
	}
	c = newQueryStorageChecker(writeQ, caches, nil)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "must list cache "+cacheRef) {
		t.Fatalf("write cache completeness not reported: %v", c.errs)
	}

	unknownQ := *readQ
	unknownQ.Caches = []string{"data.tables.x.cache.missing"}
	c = newQueryStorageChecker(&unknownQ, caches, nil)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not declared in storage.caches") {
		t.Fatalf("unknown cache not reported: %v", c.errs)
	}

	foreignQ := *readQ
	foreignQ.Targets = []string{"data.tables.y"}
	foreignQ.Caches = []string{cacheRef}
	c = newQueryStorageChecker(&foreignQ, caches, nil)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "which is not in query tables") {
		t.Fatalf("foreign cache table not reported: %v", c.errs)
	}

	noRefQ := *readQ
	noRefQ.Caches = nil
	c = newQueryStorageChecker(&noRefQ, caches, nil)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not referenced by any query") {
		t.Fatalf("unused cache not reported: %v", c.errs)
	}
}

func TestQueryArchiveRefs(t *testing.T) {
	archive := storageArchiveEntry{source: "data.tables.x", promotable: true}
	archives := map[string]storageArchiveEntry{"data.tables.x": archive}
	ref := "data.tables.x.archive"
	readQ := &queryInfo{
		ID: "data.queries.x.q", File: "x.yaml",
		Targets: []string{"data.tables.x"},
		Archive: []string{ref},
	}

	c := newQueryStorageChecker(readQ, nil, archives)
	c.checkQueryStorageRefs()
	if len(c.errs) != 0 {
		t.Fatalf("valid archive ref reported: %v", c.errs)
	}

	notPromotable := map[string]storageArchiveEntry{"data.tables.x": {source: "data.tables.x", promotable: false}}
	c = newQueryStorageChecker(readQ, nil, notPromotable)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not promotable") {
		t.Fatalf("non-promotable archive not reported: %v", c.errs)
	}
	if strings.Contains(strings.Join(c.errs, "\n"), "no query references it") {
		t.Fatalf("non-promotable archive must not require a query: %v", c.errs)
	}

	unknownQ := *readQ
	unknownQ.Archive = []string{"data.tables.y.archive"}
	c = newQueryStorageChecker(&unknownQ, nil, archives)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not declared in storage.tables") {
		t.Fatalf("unknown archive not reported: %v", c.errs)
	}

	noRefQ := *readQ
	noRefQ.Archive = nil
	c = newQueryStorageChecker(&noRefQ, nil, archives)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is promotable but no query references it") {
		t.Fatalf("unreferenced promotable archive not reported: %v", c.errs)
	}

	malformedQ := *readQ
	malformedQ.Archive = []string{"data.tables.x.archived"}
	c = newQueryStorageChecker(&malformedQ, nil, archives)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "malformed archive ref") {
		t.Fatalf("malformed archive ref not reported: %v", c.errs)
	}
}

func TestArchiveRefSource(t *testing.T) {
	cases := []struct {
		ref  string
		want string
		ok   bool
	}{
		{"data.tables.x.archive", "data.tables.x", true},
		{"data.tables.x.cache.code", "", false},
		{"data.tables..archive", "", false},
		{"archive", "", false},
	}
	for _, tc := range cases {
		got, ok := archiveRefSource(tc.ref)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("archiveRefSource(%q) = (%q, %v), want (%q, %v)", tc.ref, got, ok, tc.want, tc.ok)
		}
	}
}
