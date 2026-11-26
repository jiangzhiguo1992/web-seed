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

// when 表达式只支持比较、&&、||、! 和括号；in / 三元等写法必须显式报错，
// 不能被当成未知字段静默忽略。
func TestWhenRejectsUnsupportedSyntax(t *testing.T) {
	for _, expr := range []string{
		`input.x in (1, 2)`,
		`input.x in [1, 2]`,
		`input.x ? 1 : 2`,
	} {
		if _, err := parseWhen(expr); err == nil {
			t.Errorf("parseWhen(%q) must reject unsupported syntax", expr)
		}
	}
}

func TestWhenDiscriminatorValues(t *testing.T) {
	n, err := parseWhen(`step.payment.method == "card" && step.payment.card_number != null`)
	if err != nil {
		t.Fatal(err)
	}
	got := whenDiscriminatorValues(n)
	if c := got["payment"]["method"]; !c.matches("card") {
		t.Fatalf("values = %#v", got)
	}

	n, err = parseWhen(`step.payment.method == "card" || input.x == 1`)
	if err != nil {
		t.Fatal(err)
	}
	if len(whenDiscriminatorValues(n)) != 0 {
		t.Fatalf("|| with different paths must not select a branch: %#v", whenDiscriminatorValues(n))
	}

	n, err = parseWhen(`step.payment.method == "card" || step.payment.method == "bank"`)
	if err != nil {
		t.Fatal(err)
	}
	if c := whenDiscriminatorValues(n)["payment"]["method"]; !c.matches("card") || !c.matches("bank") {
		t.Fatalf("|| values = %#v", c)
	}

	n, err = parseWhen(`step.payment.method != "card"`)
	if err != nil {
		t.Fatal(err)
	}
	if c := whenDiscriminatorValues(n)["payment"]["method"]; c.matches("card") {
		t.Fatalf("!= values = %#v", c)
	}

	n, err = parseWhen(`step.payment.kind == 1`)
	if err != nil {
		t.Fatal(err)
	}
	if c := whenDiscriminatorValues(n)["payment"]["kind"]; !c.matches("1") {
		t.Fatalf("numeric values = %#v", c)
	}

	n, err = parseWhen(`!(step.payment.method == "card")`)
	if err != nil {
		t.Fatal(err)
	}
	if c := whenDiscriminatorValues(n)["payment"]["method"]; c.matches("card") {
		t.Fatalf("negated values = %#v", c)
	}

	n, err = parseWhen(`step.payment.kind > 1`)
	if err != nil {
		t.Fatal(err)
	}
	if c := whenDiscriminatorValues(n)["payment"]["kind"]; !c.matches("2") || c.matches("1") {
		t.Fatalf("range values = %#v", c)
	}

	n, err = parseWhen(`input.payment.method == "card"`)
	if err != nil {
		t.Fatal(err)
	}
	if c := whenDiscriminatorValues(n)["input"]["payment.method"]; !c.matches("card") {
		t.Fatalf("input values = %#v", c)
	}

	n, err = parseWhen(`step.payment.kind > 1 || step.payment.kind > 2`)
	if err != nil {
		t.Fatal(err)
	}
	if c := whenDiscriminatorValues(n)["payment"]["kind"]; !c.matches("2") || c.matches("1") {
		t.Fatalf("range union = %#v", c)
	}

	n, err = parseWhen(`!(step.payment.kind > 1)`)
	if err != nil {
		t.Fatal(err)
	}
	if c := whenDiscriminatorValues(n)["payment"]["kind"]; !c.matches("1") || c.matches("2") {
		t.Fatalf("range negation = %#v", c)
	}

	n, err = parseWhen(`!(step.payment.kind > 1 && step.payment.kind <= 3)`)
	if err != nil {
		t.Fatal(err)
	}
	if c := whenDiscriminatorValues(n)["payment"]["kind"]; !c.matches("1") || c.matches("2") || !c.matches("4") {
		t.Fatalf("range conjunction negation = %#v", c)
	}

	n, err = parseWhen(`(step.payment.method == "card" && step.payment.kind == 1) || (step.payment.method == "bank" && step.payment.kind == 2)`)
	if err != nil {
		t.Fatal(err)
	}
	multi := whenDiscriminatorValues(n)
	if c := multi["payment"]["method"]; !c.matches("card") || !c.matches("bank") {
		t.Fatalf("multi-path method = %#v", c)
	}
	if c := multi["payment"]["kind"]; !c.matches("1") || !c.matches("2") {
		t.Fatalf("multi-path kind = %#v", c)
	}

	n, err = parseWhen(`(step.payment.method == "card" && step.payment.kind == 1) || (input.mode == "card")`)
	if err != nil {
		t.Fatal(err)
	}
	if got := whenDiscriminatorValues(n); len(got) != 0 {
		t.Fatalf("non-common path must be dropped: %#v", got)
	}
}
