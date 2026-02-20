package core

import (
	"math"
	"reflect"
)

func EvaluateFlag(flag Flag, context EvaluationContext) bool {
	if flag.Disabled {
		return false
	}

	fallbackValue := true
	if flag.DefaultValue != nil {
		fallbackValue = *flag.DefaultValue
	}

	if len(flag.Rules) == 0 {
		return fallbackValue
	}

	for _, rule := range flag.Rules {
		if evaluateRule(rule, context.Attributes) {
			return true
		}
	}

	return fallbackValue
}

func EvaluateFlags(flags []Flag, context EvaluationContext) map[string]bool {
	results := make(map[string]bool, len(flags))

	for _, flag := range flags {
		results[flag.Key] = EvaluateFlag(flag, context)
	}

	return results
}

func evaluateRule(rule Rule, attributes map[string]any) bool {
	if attributes == nil {
		return false
	}

	attributeValue, ok := attributes[rule.Attribute]
	if !ok {
		return false
	}

	switch rule.Operator {
	case OperatorEquals:
		return valuesEqual(attributeValue, rule.Value)
	case OperatorIn:
		return valueIn(attributeValue, rule.Value)
	default:
		return false
	}
}

func valueIn(value any, ruleValue any) bool {
	values := reflect.ValueOf(ruleValue)
	if !values.IsValid() {
		return false
	}

	if values.Kind() != reflect.Slice && values.Kind() != reflect.Array {
		return false
	}

	for i := 0; i < values.Len(); i++ {
		if valuesEqual(value, values.Index(i).Interface()) {
			return true
		}
	}

	return false
}

func valuesEqual(left any, right any) bool {
	if leftInt, ok := asInt64(left); ok {
		if rightInt, ok := asInt64(right); ok {
			return leftInt == rightInt
		}

		if rightUint, ok := asUint64(right); ok {
			if leftInt < 0 {
				return false
			}
			return uint64(leftInt) == rightUint
		}

		if rightFloat, ok := asFloat64(right); ok {
			return floatEqualsInt64(rightFloat, leftInt)
		}
	}

	if leftUint, ok := asUint64(left); ok {
		if rightUint, ok := asUint64(right); ok {
			return leftUint == rightUint
		}

		if rightInt, ok := asInt64(right); ok {
			if rightInt < 0 {
				return false
			}
			return leftUint == uint64(rightInt)
		}

		if rightFloat, ok := asFloat64(right); ok {
			return floatEqualsUint64(rightFloat, leftUint)
		}
	}

	if leftFloat, ok := asFloat64(left); ok {
		if rightFloat, ok := asFloat64(right); ok {
			return leftFloat == rightFloat
		}

		if rightInt, ok := asInt64(right); ok {
			return floatEqualsInt64(leftFloat, rightInt)
		}

		if rightUint, ok := asUint64(right); ok {
			return floatEqualsUint64(leftFloat, rightUint)
		}
	}

	return reflect.DeepEqual(left, right)
}

func asInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int8:
		return int64(number), true
	case int16:
		return int64(number), true
	case int32:
		return int64(number), true
	case int64:
		return number, true
	default:
		return 0, false
	}
}

func asUint64(value any) (uint64, bool) {
	switch number := value.(type) {
	case uint:
		return uint64(number), true
	case uint8:
		return uint64(number), true
	case uint16:
		return uint64(number), true
	case uint32:
		return uint64(number), true
	case uint64:
		return number, true
	default:
		return 0, false
	}
}

func asFloat64(value any) (float64, bool) {
	switch number := value.(type) {
	case float32:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}

func floatEqualsInt64(left float64, right int64) bool {
	if !isWholeFinite(left) {
		return false
	}

	if left < float64(math.MinInt64) || left > float64(math.MaxInt64) {
		return false
	}

	converted := int64(left)
	return float64(converted) == left && converted == right
}

func floatEqualsUint64(left float64, right uint64) bool {
	if !isWholeFinite(left) {
		return false
	}

	if left < 0 || left > float64(math.MaxUint64) {
		return false
	}

	converted := uint64(left)
	return float64(converted) == left && converted == right
}

func isWholeFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Trunc(value) == value
}
