package engine

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

type ExpectationKind uint8

const (
	ExpectEqual ExpectationKind = iota + 1
	ExpectContains
	ExpectRegexp
	ExpectPrefix
	ExpectSuffix
	ExpectNumber
	ExpectEmpty
	ExpectAll
	ExpectAny
	ExpectNot
	ExpectAnything
)

type Expectation struct {
	Kind     ExpectationKind
	Expected any
	Operator string
	Children []Expectation
}

func MatchExpectation(actual any, expectation Expectation) (bool, error) {
	switch expectation.Kind {
	case ExpectEqual:
		if actualNumber, ok := number(actual); ok {
			if expectedNumber, expectedOK := number(expectation.Expected); expectedOK {
				return actualNumber == expectedNumber, nil
			}
		}
		return reflect.DeepEqual(actual, expectation.Expected), nil
	case ExpectContains, ExpectRegexp, ExpectPrefix, ExpectSuffix:
		actualString, actualOK := actual.(string)
		expectedString, expectedOK := expectation.Expected.(string)
		if !actualOK || !expectedOK {
			return false, nil
		}
		switch expectation.Kind {
		case ExpectContains:
			return strings.Contains(actualString, expectedString), nil
		case ExpectRegexp:
			expression, err := regexp.Compile(expectedString)
			if err != nil {
				return false, fmt.Errorf("invalid regular expression %q: %w", expectedString, err)
			}
			return expression.MatchString(actualString), nil
		case ExpectPrefix:
			return strings.HasPrefix(actualString, expectedString), nil
		default:
			return strings.HasSuffix(actualString, expectedString), nil
		}
	case ExpectNumber:
		actualNumber, actualOK := number(actual)
		expectedNumber, expectedOK := number(expectation.Expected)
		if !actualOK || !expectedOK {
			return false, nil
		}
		switch expectation.Operator {
		case "==", "=":
			return actualNumber == expectedNumber, nil
		case "!=":
			return actualNumber != expectedNumber, nil
		case ">":
			return actualNumber > expectedNumber, nil
		case ">=":
			return actualNumber >= expectedNumber, nil
		case "<":
			return actualNumber < expectedNumber, nil
		case "<=":
			return actualNumber <= expectedNumber, nil
		default:
			return false, fmt.Errorf("unsupported numeric operator %q", expectation.Operator)
		}
	case ExpectEmpty:
		if actual == nil {
			return true, nil
		}
		value := reflect.ValueOf(actual)
		switch value.Kind() {
		case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
			return value.Len() == 0, nil
		default:
			return false, nil
		}
	case ExpectAll:
		for _, child := range expectation.Children {
			matched, err := MatchExpectation(actual, child)
			if err != nil || !matched {
				return matched, err
			}
		}
		return true, nil
	case ExpectAny:
		for _, child := range expectation.Children {
			matched, err := MatchExpectation(actual, child)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
		return false, nil
	case ExpectNot:
		if len(expectation.Children) != 1 {
			return false, fmt.Errorf("not expectation requires exactly one child")
		}
		matched, err := MatchExpectation(actual, expectation.Children[0])
		return !matched, err
	case ExpectAnything:
		return true, nil
	default:
		return false, fmt.Errorf("unsupported expectation kind %d", expectation.Kind)
	}
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}
