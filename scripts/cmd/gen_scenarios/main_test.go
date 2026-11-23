package main

import "testing"

func TestCoverageGaps(t *testing.T) {
	coverage := []coverageEntry{
		{Operation: "cap.protocols.http.x.none", Scenarios: 0},
		{
			Operation:       "cap.protocols.http.x.partial",
			Scenarios:       1,
			MissingStatuses: []int{200},
			Codes:           []codeCoverage{{Code: "X", Status: 409, Covered: false}},
		},
	}
	gaps := coverageGaps(coverage)
	if len(gaps) != 3 {
		t.Fatalf("gaps = %v", gaps)
	}
	if gaps[0] != "cap.protocols.http.x.none: no scenario" {
		t.Fatalf("gaps[0] = %q", gaps[0])
	}
}
