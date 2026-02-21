// Package core provides the pure feature-flag evaluation engine.
//
// It defines the domain types ([Flag], [Rule], [EvaluationContext]) and the
// stateless evaluation functions ([EvaluateFlag], [EvaluateFlags]). This
// package has no database, network, or transport dependencies — just logic
// and a healthy respect for boolean algebra.
package core

// Operator represents a comparison operator used in rule evaluation.
type Operator string

const (
	// OperatorEquals matches when an attribute value is equal to the rule value.
	OperatorEquals Operator = "equals"
	// OperatorIn matches when an attribute value is contained in the rule value list.
	OperatorIn Operator = "in"
)

// Rule defines a single targeting condition. When evaluated, it checks whether
// the named attribute in the evaluation context satisfies the operator and value.
type Rule struct {
	Attribute string   `json:"attribute"`
	Operator  Operator `json:"operator"`
	Value     any      `json:"value"`
}

// Flag is the core representation of a feature flag used during evaluation.
// Note that Disabled uses inverted polarity compared to [repository.Flag].Enabled;
// the mapping layer handles the conversion so you don't have to think about it
// (most of the time).
type Flag struct {
	Key          string `json:"key"`
	Disabled     bool   `json:"disabled,omitempty"`
	DefaultValue *bool  `json:"default_value,omitempty"`
	Rules        []Rule `json:"rules,omitempty"`
}

// EvaluationContext carries the attribute map provided by a caller at evaluation
// time. Rule conditions are matched against these attributes.
type EvaluationContext struct {
	Attributes map[string]any `json:"attributes,omitempty"`
}
