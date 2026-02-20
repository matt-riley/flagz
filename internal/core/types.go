package core

type Operator string

const (
	OperatorEquals Operator = "equals"
	OperatorIn     Operator = "in"
)

type Rule struct {
	Attribute string   `json:"attribute"`
	Operator  Operator `json:"operator"`
	Value     any      `json:"value"`
}

type Flag struct {
	Key          string `json:"key"`
	Disabled     bool   `json:"disabled,omitempty"`
	DefaultValue *bool  `json:"default_value,omitempty"`
	Rules        []Rule `json:"rules,omitempty"`
}

type EvaluationContext struct {
	Attributes map[string]any `json:"attributes,omitempty"`
}
