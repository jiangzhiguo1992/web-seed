package check

import "testing"

func TestAuditTypeVocabularyRepo(t *testing.T) {
	report, err := AuditTypeVocabulary(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if report.Total == 0 {
		t.Fatal("audit scanned no type keys")
	}
	if report.Exempt == 0 {
		t.Fatal("audit scanned no exempt files")
	}
	if len(report.Invalid) != 0 {
		t.Fatalf("repository type vocabulary invalid:\n%v", report.Invalid)
	}
}
