package core

import (
	"reflect"
	"testing"
)

func boolPtr(value bool) *bool {
	return &value
}

func TestEvaluateFlag(t *testing.T) {
	tests := []struct {
		name    string
		flag    Flag
		context EvaluationContext
		want    bool
	}{
		{
			name: "disabled flag always resolves false",
			flag: Flag{
				Disabled: true,
			},
			want: false,
		},
		{
			name: "no rules resolves true when flag is enabled",
			flag: Flag{},
			want: true,
		},
		{
			name: "equals rule matches",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorEquals, Value: "US"},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "US"},
			},
			want: true,
		},
		{
			name: "equals rule mismatch",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorEquals, Value: "US"},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "CA"},
			},
			want: true,
		},
		{
			name: "equals rule missing attribute",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorEquals, Value: "US"},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"role": "admin"},
			},
			want: true,
		},
		{
			name: "in rule matches from list",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorIn, Value: []any{"US", "CA"}},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "CA"},
			},
			want: true,
		},
		{
			name: "in rule supports typed slices",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorIn, Value: []string{"US", "CA"}},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "US"},
			},
			want: true,
		},
		{
			name: "in rule no match",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorIn, Value: []any{"US", "CA"}},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "GB"},
			},
			want: true,
		},
		{
			name: "in rule with non-list value fails",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorIn, Value: "US"},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "US"},
			},
			want: true,
		},
		{
			name: "unknown operator fails",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: Operator("contains"), Value: "US"},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "US"},
			},
			want: true,
		},
		{
			name: "multiple rules resolve true when first rule matches",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorEquals, Value: "US"},
					{Attribute: "plan", Operator: OperatorIn, Value: []string{"pro", "team"}},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "US", "plan": "free"},
			},
			want: true,
		},
		{
			name: "multiple rules resolve true when later rule matches",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorEquals, Value: "US"},
					{Attribute: "plan", Operator: OperatorIn, Value: []string{"pro", "team"}},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "CA", "plan": "pro"},
			},
			want: true,
		},
		{
			name: "multiple rules resolve false when none match",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorEquals, Value: "US"},
					{Attribute: "plan", Operator: OperatorIn, Value: []string{"pro", "team"}},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "CA", "plan": "free"},
			},
			want: true,
		},
		{
			name: "rules with nil attributes fail",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorEquals, Value: "US"},
				},
			},
			context: EvaluationContext{
				Attributes: nil,
			},
			want: true,
		},
		{
			name: "numeric equals supports mixed numeric types",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "cohort", Operator: OperatorEquals, Value: 1.0},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"cohort": int32(1)},
			},
			want: true,
		},
		{
			name: "numeric in supports mixed numeric types",
			flag: Flag{
				Rules: []Rule{
					{Attribute: "cohort", Operator: OperatorIn, Value: []any{1.0, 2.0}},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"cohort": int64(2)},
			},
			want: true,
		},
		{
			name: "numeric equals keeps precision for large integers mismatch",
			flag: Flag{
				DefaultValue: boolPtr(false),
				Rules: []Rule{
					{Attribute: "snowflake", Operator: OperatorEquals, Value: uint64(9007199254740992)},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"snowflake": int64(9007199254740993)},
			},
			want: false,
		},
		{
			name: "numeric equals keeps precision for large integers match",
			flag: Flag{
				DefaultValue: boolPtr(false),
				Rules: []Rule{
					{Attribute: "snowflake", Operator: OperatorEquals, Value: uint64(9007199254740993)},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"snowflake": int64(9007199254740993)},
			},
			want: true,
		},
		{
			name: "no rules respects default override",
			flag: Flag{
				DefaultValue: boolPtr(false),
			},
			want: false,
		},
		{
			name: "rule mismatch respects default override",
			flag: Flag{
				DefaultValue: boolPtr(false),
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorEquals, Value: "US"},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "CA"},
			},
			want: false,
		},
		{
			name: "rule match overrides default override",
			flag: Flag{
				DefaultValue: boolPtr(false),
				Rules: []Rule{
					{Attribute: "country", Operator: OperatorEquals, Value: "US"},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"country": "US"},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EvaluateFlag(test.flag, test.context)
			if got != test.want {
				t.Fatalf("EvaluateFlag() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestEvaluateFlags(t *testing.T) {
	tests := []struct {
		name    string
		flags   []Flag
		context EvaluationContext
		want    map[string]bool
	}{
		{
			name:  "empty input returns empty results",
			flags: nil,
			want:  map[string]bool{},
		},
		{
			name: "evaluates multiple flags",
			flags: []Flag{
				{Key: "new-ui"},
				{Key: "admin-panel", Disabled: true},
				{
					Key: "pro-feature",
					Rules: []Rule{
						{Attribute: "plan", Operator: OperatorIn, Value: []string{"pro", "team"}},
					},
				},
				{
					Key: "us-only",
					Rules: []Rule{
						{Attribute: "country", Operator: OperatorEquals, Value: "US"},
					},
				},
			},
			context: EvaluationContext{
				Attributes: map[string]any{"plan": "pro", "country": "CA"},
			},
			want: map[string]bool{
				"new-ui":      true,
				"admin-panel": false,
				"pro-feature": true,
				"us-only":     true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EvaluateFlags(test.flags, test.context)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("EvaluateFlags() = %#v, want %#v", got, test.want)
			}
		})
	}
}
