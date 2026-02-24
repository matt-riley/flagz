package core

import (
"fmt"
"testing"
)

func BenchmarkEvaluateFlag_NoRules(b *testing.B) {
defaultVal := true
flag := Flag{
Key:          "feature-no-rules",
Disabled:     false,
DefaultValue: &defaultVal,
}
ctx := EvaluationContext{
Attributes: map[string]any{"country": "US", "plan": "pro"},
}

b.ResetTimer()
for b.Loop() {
EvaluateFlag(flag, ctx)
}
}

func BenchmarkEvaluateFlag_SingleRule(b *testing.B) {
defaultVal := false
flag := Flag{
Key:          "feature-single-rule",
Disabled:     false,
DefaultValue: &defaultVal,
Rules: []Rule{
{Attribute: "country", Operator: OperatorEquals, Value: "US"},
},
}
ctx := EvaluationContext{
Attributes: map[string]any{"country": "US"},
}

b.ResetTimer()
for b.Loop() {
EvaluateFlag(flag, ctx)
}
}

func BenchmarkEvaluateFlag_ManyRules(b *testing.B) {
defaultVal := false
rules := make([]Rule, 15)
for i := range rules {
rules[i] = Rule{
Attribute: fmt.Sprintf("attr-%d", i),
Operator:  OperatorEquals,
Value:     fmt.Sprintf("val-%d", i),
}
}

flag := Flag{
Key:          "feature-many-rules",
Disabled:     false,
DefaultValue: &defaultVal,
Rules:        rules,
}

b.Run("MatchFirst", func(b *testing.B) {
ctx := EvaluationContext{
Attributes: map[string]any{"attr-0": "val-0"},
}
b.ResetTimer()
for b.Loop() {
EvaluateFlag(flag, ctx)
}
})

b.Run("MatchMiddle", func(b *testing.B) {
ctx := EvaluationContext{
Attributes: map[string]any{"attr-7": "val-7"},
}
b.ResetTimer()
for b.Loop() {
EvaluateFlag(flag, ctx)
}
})

b.Run("MatchLast", func(b *testing.B) {
ctx := EvaluationContext{
Attributes: map[string]any{"attr-14": "val-14"},
}
b.ResetTimer()
for b.Loop() {
EvaluateFlag(flag, ctx)
}
})

b.Run("NoMatch", func(b *testing.B) {
ctx := EvaluationContext{
Attributes: map[string]any{"country": "XX"},
}
b.ResetTimer()
for b.Loop() {
EvaluateFlag(flag, ctx)
}
})
}

func BenchmarkEvaluateFlags_Batch(b *testing.B) {
defaultVal := true
flags := make([]Flag, 100)
for i := range flags {
var rules []Rule
if i%2 == 0 {
rules = []Rule{
{Attribute: "plan", Operator: OperatorIn, Value: []string{"pro", "enterprise"}},
}
}
flags[i] = Flag{
Key:          fmt.Sprintf("flag-%03d", i),
Disabled:     i%10 == 0,
DefaultValue: &defaultVal,
Rules:        rules,
}
}
ctx := EvaluationContext{
Attributes: map[string]any{
"country": "US",
"plan":    "pro",
"user_id": "user-42",
},
}

b.ResetTimer()
for b.Loop() {
EvaluateFlags(flags, ctx)
}
}
