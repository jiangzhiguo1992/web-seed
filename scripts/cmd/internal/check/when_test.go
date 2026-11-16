package check

import "testing"

func TestEvalWhen(t *testing.T) {
	cases := []struct {
		expr    string
		want    whenValue
		wantErr bool
	}{
		{"true", whenTrue, false},
		{"false", whenFalse, false},
		{"!false", whenTrue, false},
		{"!(false && input.x == 1)", whenTrue, false},
		{"true && false", whenFalse, false},
		{"true || input.x == 1", whenTrue, false},
		{"false && input.x == 1", whenFalse, false},
		{"false || input.x == 1", whenUnknown, false},
		{"input.x == 1", whenUnknown, false},
		{"step.load.status == \"pending\"", whenUnknown, false},
		{"step.load.accounts[0].status == \"ok\"", whenUnknown, false},
		{"input.items[0].id == 1", whenUnknown, false},
		{"input.items[] == 1", whenUnknown, true},
		{"input.items[-1] == 1", whenUnknown, true},
		{"input.items[abc] == 1", whenUnknown, true},
		{"input.items[0][1] == 1", whenUnknown, true},
		{"input.x == \"a\"", whenUnknown, false},
		{"input.x != null", whenUnknown, false},
		{"1 == 1", whenTrue, false},
		{"1 == 2", whenFalse, false},
		{"1 < 2", whenTrue, false},
		{"\"a\" == \"a\"", whenTrue, false},
		{"true == false", whenFalse, false},
		{"", whenUnknown, true},
		{"input.x", whenUnknown, true},
		{"input.x ==", whenUnknown, true},
		{"input..x == 1", whenUnknown, true},
		{"step.load == 1", whenUnknown, true},
		{"step..status == 1", whenUnknown, true},
		{"foo == 1", whenUnknown, true},
		{"(input.x == 1", whenUnknown, true},
		{"input.x == 1 &&", whenUnknown, true},
	}
	for _, tc := range cases {
		got, err := evalWhen(tc.expr)
		if tc.wantErr {
			if err == nil {
				t.Errorf("evalWhen(%q) expected error, got %v", tc.expr, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("evalWhen(%q) error: %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("evalWhen(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestWhenStepRefs(t *testing.T) {
	n, err := parseWhen(`step.load.status == "pending" && (step.verify.ok == true || input.x == 1)`)
	if err != nil {
		t.Fatal(err)
	}
	refs := whenStepRefs(n)
	if len(refs) != 2 || refs[0] != "load" || refs[1] != "verify" {
		t.Fatalf("refs = %v", refs)
	}
}
