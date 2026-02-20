package core

import "testing"

func FuzzValuesEqualSymmetry(f *testing.F) {
	f.Add(int64(1), uint64(1), float64(1), "1")
	f.Add(int64(-1), uint64(2), float64(-1), "")
	f.Add(int64(9007199254740993), uint64(9007199254740992), float64(9007199254740992), "snowflake")

	f.Fuzz(func(t *testing.T, i int64, u uint64, fl float64, value string) {
		if valuesEqual(i, u) != valuesEqual(u, i) {
			t.Fatalf("valuesEqual symmetry failed for int/uint: %d, %d", i, u)
		}
		if valuesEqual(i, fl) != valuesEqual(fl, i) {
			t.Fatalf("valuesEqual symmetry failed for int/float: %d, %f", i, fl)
		}
		if valuesEqual(value, fl) != valuesEqual(fl, value) {
			t.Fatalf("valuesEqual symmetry failed for string/float: %q, %f", value, fl)
		}

		defaultValue := u%2 == 0
		ruleValue := any(value)
		if u%3 == 0 {
			ruleValue = []any{value, i, u, fl}
		}

		operator := OperatorEquals
		if u%5 == 0 {
			operator = OperatorIn
		}
		if u%7 == 0 {
			operator = Operator("unknown")
		}

		attribute := value
		if attribute == "" {
			attribute = "attr"
		}

		flag := Flag{
			Key:          "fuzz-flag",
			Disabled:     u%11 == 0,
			DefaultValue: &defaultValue,
			Rules: []Rule{
				{
					Attribute: attribute,
					Operator:  operator,
					Value:     ruleValue,
				},
			},
		}

		context := EvaluationContext{
			Attributes: map[string]any{
				attribute: value,
			},
		}

		_ = EvaluateFlag(flag, context)
	})
}
