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

	badRefQ := *readQ
	badRefQ.Caches = []string{"data.tables.x.cache.missing"}
	c = newQueryStorageChecker(&badRefQ, caches, nil)
	c.checkQueryStorageRefs()
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "is not declared in storage.caches") || !strings.Contains(got, "[query-cache-ref]") {
		t.Fatalf("undeclared cache ref not reported: %v", c.errs)
	}

	unboundQ := *readQ
	unboundQ.Params = nil
	c = newQueryStorageChecker(&unboundQ, caches, nil)
	c.checkQueryStorageRefs()
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "must appear in params[].column or index_ref[].columns") || !strings.Contains(got, "[query-cache-keys]") {
		t.Fatalf("cache key without column source not reported: %v", c.errs)
	}

	indexOnlyQ := unboundQ
	indexOnlyQ.IndexRef = []queryIndexRef{{Index: "data.tables.x.idx_x_code", Columns: []string{"code"}}}
	c = newQueryStorageChecker(&indexOnlyQ, caches, nil)
	c.checkQueryStorageRefs()
	if len(c.errs) != 0 {
		t.Fatalf("index_ref column as cache key source must be allowed: %v", c.errs)
	}

	undeclaredQ := *readQ
	undeclaredQ.Caches = nil
	c = newQueryStorageChecker(&undeclaredQ, caches, nil)
	c.checkQueryStorageRefs()
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "fully bound by params but the query does not declare it") || !strings.Contains(got, "[query-cache-keys]") {
		t.Fatalf("read query with fully bound cache key must declare the cache: %v", c.errs)
	}

	writeQ := &queryInfo{
		ID: "data.queries.x.w", File: "x.yaml",
		Targets: []string{"data.tables.x"}, Writes: []string{"data.tables.x"},
	}
	c = newQueryStorageChecker(writeQ, caches, nil)
	c.checkQueryStorageRefs()
	if got := strings.Join(c.errs, "\n"); !strings.Contains(got, "must list cache "+cacheRef) || !strings.Contains(got, "[query-cache-write]") {
		t.Fatalf("write cache completeness not reported: %v", c.errs)
	}

	writeWithCacheQ := *writeQ
	writeWithCacheQ.Caches = []string{cacheRef}
	c = newQueryStorageChecker(&writeWithCacheQ, caches, nil)
	c.checkQueryStorageRefs()
	if len(c.errs) != 0 {
		t.Fatalf("write sync cache must not require key in params/index_ref: %v", c.errs)
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
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not referenced by any query") {
		t.Fatalf("invalid cache ref must not count as a valid reference: %v", c.errs)
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
	archive := storageArchiveEntry{source: "data.tables.x", promotable: true, ageBy: "created_at"}
	archives := map[string]storageArchiveEntry{"data.tables.x": archive}
	ref := queryArchiveRef{Archive: "data.tables.x.archive", Promotable: true, HasPromotable: true, AgeBy: "created_at"}
	readQ := &queryInfo{
		ID: "data.queries.x.q", File: "x.yaml",
		Targets: []string{"data.tables.x"},
		Archive: []queryArchiveRef{ref},
	}

	c := newQueryStorageChecker(readQ, nil, archives)
	c.checkQueryStorageRefs()
	if len(c.errs) != 0 {
		t.Fatalf("valid archive ref reported: %v", c.errs)
	}

	mismatchQ := *readQ
	mismatchQ.Archive = []queryArchiveRef{{Archive: ref.Archive, Promotable: false, HasPromotable: true}}
	c = newQueryStorageChecker(&mismatchQ, nil, archives)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "must match storage promotable") {
		t.Fatalf("archive promotable mismatch not reported: %v", c.errs)
	}

	missingFlagQ := *readQ
	missingFlagQ.Archive = []queryArchiveRef{{Archive: ref.Archive, AgeBy: "created_at"}}
	c = newQueryStorageChecker(&missingFlagQ, nil, archives)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "requires promotable") {
		t.Fatalf("missing promotable not reported: %v", c.errs)
	}

	missingAgeQ := *readQ
	missingAgeQ.Archive = []queryArchiveRef{{Archive: ref.Archive, Promotable: true, HasPromotable: true}}
	c = newQueryStorageChecker(&missingAgeQ, nil, archives)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "requires age_by") {
		t.Fatalf("missing age_by not reported: %v", c.errs)
	}

	ageMismatchQ := *readQ
	ageMismatchQ.Archive = []queryArchiveRef{{Archive: ref.Archive, Promotable: true, HasPromotable: true, AgeBy: "updated_at"}}
	c = newQueryStorageChecker(&ageMismatchQ, nil, archives)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "must match storage age_by") {
		t.Fatalf("archive age_by mismatch not reported: %v", c.errs)
	}

	notPromotable := map[string]storageArchiveEntry{"data.tables.x": {source: "data.tables.x", promotable: false, ageBy: "created_at"}}
	readOnlyRef := queryArchiveRef{Archive: ref.Archive, Promotable: false, HasPromotable: true}
	readOnlyQ := *readQ
	readOnlyQ.Archive = []queryArchiveRef{readOnlyRef}
	c = newQueryStorageChecker(&readOnlyQ, nil, notPromotable)
	c.checkQueryStorageRefs()
	if len(c.errs) != 0 {
		t.Fatalf("valid non-promotable archive ref reported: %v", c.errs)
	}

	ageOnReadOnlyQ := *readQ
	ageOnReadOnlyQ.Archive = []queryArchiveRef{{Archive: ref.Archive, Promotable: false, HasPromotable: true, AgeBy: "created_at"}}
	c = newQueryStorageChecker(&ageOnReadOnlyQ, nil, notPromotable)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "age_by is only allowed when promotable is true") {
		t.Fatalf("age_by on non-promotable ref not reported: %v", c.errs)
	}

	unknownQ := *readQ
	unknownQ.Archive = []queryArchiveRef{{Archive: "data.tables.y.archive", Promotable: true, HasPromotable: true, AgeBy: "created_at"}}
	c = newQueryStorageChecker(&unknownQ, nil, archives)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "is not declared in storage.tables") {
		t.Fatalf("unknown archive not reported: %v", c.errs)
	}

	noRefQ := *readQ
	noRefQ.Archive = nil
	c = newQueryStorageChecker(&noRefQ, nil, archives)
	c.checkQueryStorageRefs()
	if !strings.Contains(strings.Join(c.errs, "\n"), "no query declares archive.promotable:true") {
		t.Fatalf("unreferenced promotable archive not reported: %v", c.errs)
	}

	malformedQ := *readQ
	malformedQ.Archive = []queryArchiveRef{{Archive: "data.tables.x.archived", Promotable: true, HasPromotable: true, AgeBy: "created_at"}}
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
