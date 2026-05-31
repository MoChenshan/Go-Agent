// Package retriever is a knowledge retriever that uses tRAG for semantic search.
package retriever

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/searchfilter"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ConditionConverter converts a filter condition to a TRag filter expression string.
type ConditionConverter struct{}

// Convert converts a filter condition to a TRag filter expression string.
func (c *ConditionConverter) Convert(cond *searchfilter.UniversalFilterCondition) (string, error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			log.Errorf("panic in ConditionConverter Convert: %v\n%s", r, string(stack))
		}
	}()

	if cond == nil {
		return "", nil
	}
	return c.convertCondition(cond)
}

func (c *ConditionConverter) convertCondition(cond *searchfilter.UniversalFilterCondition) (string, error) {
	if cond == nil {
		return "", fmt.Errorf("nil condition")
	}

	switch cond.Operator {
	case searchfilter.OperatorAnd, searchfilter.OperatorOr:
		return c.buildLogicalCondition(cond)
	case searchfilter.OperatorEqual, searchfilter.OperatorNotEqual,
		searchfilter.OperatorGreaterThan, searchfilter.OperatorGreaterThanOrEqual,
		searchfilter.OperatorLessThan, searchfilter.OperatorLessThanOrEqual:
		return c.buildComparisonCondition(cond)
	case searchfilter.OperatorIn, searchfilter.OperatorNotIn:
		return c.buildInCondition(cond)
	case searchfilter.OperatorBetween:
		return c.buildBetweenCondition(cond)
	case searchfilter.OperatorLike, searchfilter.OperatorNotLike:
		return c.buildLikeCondition(cond)
	default:
		return "", fmt.Errorf("unsupported operation: %s", cond.Operator)
	}
}

func (c *ConditionConverter) buildLogicalCondition(cond *searchfilter.UniversalFilterCondition) (string, error) {
	conds, ok := cond.Value.([]*searchfilter.UniversalFilterCondition)
	if !ok {
		// Try to cast from []interface{} if possible
		if valSlice, ok := cond.Value.([]any); ok {
			conds = make([]*searchfilter.UniversalFilterCondition, 0, len(valSlice))
			for _, v := range valSlice {
				if child, ok := v.(*searchfilter.UniversalFilterCondition); ok {
					conds = append(conds, child)
				}
			}
		} else {
			return "", fmt.Errorf("invalid logical condition: value must be of type []*searchfilter.UniversalFilterCondition: %v", cond.Value)
		}
	}

	if len(conds) == 0 {
		return "", fmt.Errorf("no valid sub-conditions in logical condition")
	}

	var childExprs []string
	for _, child := range conds {
		expr, err := c.convertCondition(child)
		if err != nil {
			return "", err
		}
		if expr != "" {
			childExprs = append(childExprs, "("+expr+")")
		}
	}

	if len(childExprs) == 0 {
		return "", nil
	}

	op := " and "
	if cond.Operator == searchfilter.OperatorOr {
		op = " or "
	}

	return strings.Join(childExprs, op), nil
}

func (c *ConditionConverter) buildInCondition(cond *searchfilter.UniversalFilterCondition) (string, error) {
	if !isValidField(cond.Field) {
		return "", fmt.Errorf(`field name must start with "metadata.": %s`, cond.Field)
	}

	val := reflect.ValueOf(cond.Value)
	if val.Kind() != reflect.Slice {
		return "", fmt.Errorf("in operator value must be a slice: %v", cond.Value)
	}

	if val.Len() <= 0 {
		return "", fmt.Errorf("in operator value must be a slice with at least one value: %v", cond.Value)
	}

	var values []string
	for i := 0; i < val.Len(); i++ {
		values = append(values, c.formatValue(val.Index(i).Interface()))
	}

	op := "in"
	if cond.Operator == searchfilter.OperatorNotIn {
		op = "not in"
	}

	field := normalizeField(cond.Field)
	return fmt.Sprintf("%s %s (%s)", field, op, strings.Join(values, ", ")), nil
}

func (c *ConditionConverter) buildBetweenCondition(cond *searchfilter.UniversalFilterCondition) (string, error) {
	if !isValidField(cond.Field) {
		return "", fmt.Errorf(`field name must start with "metadata.": %s`, cond.Field)
	}

	val := reflect.ValueOf(cond.Value)
	if val.Kind() != reflect.Slice || val.Len() != 2 {
		return "", fmt.Errorf("between operator value must be a slice with two elements: %v", cond.Value)
	}

	field := normalizeField(cond.Field)
	start := c.formatValue(val.Index(0).Interface())
	end := c.formatValue(val.Index(1).Interface())

	// TRag might not support BETWEEN natively, so we convert to >= AND <=
	return fmt.Sprintf("(%s >= %s and %s <= %s)", field, start, field, end), nil
}

func (c *ConditionConverter) buildLikeCondition(cond *searchfilter.UniversalFilterCondition) (string, error) {
	if !isValidField(cond.Field) {
		return "", fmt.Errorf(`field name must start with "metadata.": %s`, cond.Field)
	}

	pattern, ok := cond.Value.(string)
	if !ok {
		return "", fmt.Errorf("like operator requires a string pattern")
	}

	// Validate pattern (optional, just to match inmemory logic roughly)
	// inmemory converts to regex, we just keep it as string for TRag SQL-like syntax
	// But we might want to check if it's a valid string

	op := "like"
	if cond.Operator == searchfilter.OperatorNotLike {
		op = "not like"
	}

	field := normalizeField(cond.Field)
	return fmt.Sprintf("%s %s \"%s\"", field, op, pattern), nil
}

func (c *ConditionConverter) buildComparisonCondition(cond *searchfilter.UniversalFilterCondition) (string, error) {
	if !isValidField(cond.Field) {
		return "", fmt.Errorf(`field name must start with "metadata.": %s`, cond.Field)
	}

	op := c.mapOperator(cond.Operator)
	if op == "" {
		return "", fmt.Errorf("unsupported comparison operator: %s", cond.Operator)
	}

	field := normalizeField(cond.Field)
	return fmt.Sprintf("%s %s %s", field, op, c.formatValue(cond.Value)), nil
}

func (c *ConditionConverter) mapOperator(op string) string {
	switch op {
	case searchfilter.OperatorEqual:
		return "="
	case searchfilter.OperatorNotEqual:
		return "!="
	case searchfilter.OperatorGreaterThan:
		return ">"
	case searchfilter.OperatorGreaterThanOrEqual:
		return ">="
	case searchfilter.OperatorLessThan:
		return "<"
	case searchfilter.OperatorLessThanOrEqual:
		return "<="
	default:
		return ""
	}
}

func (c *ConditionConverter) formatValue(v any) string {
	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.String:
		return fmt.Sprintf("\"%s\"", strings.ReplaceAll(val.String(), "\"", "\\\""))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", val.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", val.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%v", val.Float())
	case reflect.Bool:
		return fmt.Sprintf("%v", val.Bool())
	default:
		if t, ok := v.(time.Time); ok {
			return fmt.Sprintf("%d", t.Unix())
		}
		return fmt.Sprintf("\"%v\"", v)
	}
}

func isValidField(field string) bool {
	return strings.HasPrefix(field, source.MetadataFieldPrefix)
}

// normalizeField strips metadata prefix if present, as TRag uses flattened keys.
func normalizeField(field string) string {
	if strings.HasPrefix(field, source.MetadataFieldPrefix) {
		return strings.TrimPrefix(field, source.MetadataFieldPrefix)
	}
	return field
}

// buildFilterExpr converts a QueryFilter to a TRag filter expression string.
func buildFilterExpr(filter *retriever.QueryFilter) (string, error) {
	if filter == nil {
		return "", nil
	}

	var parts []string

	// Handle Metadata map (implicit AND)
	// We convert this to UniversalFilterConditions to be processed by Converter
	if len(filter.Metadata) > 0 {
		converter := &ConditionConverter{}
		for k, v := range filter.Metadata {
			parts = append(parts, fmt.Sprintf("%s = %s", k, converter.formatValue(v)))
		}
	}

	// Handle UniversalFilterCondition
	if filter.FilterCondition != nil {
		converter := &ConditionConverter{}
		condExpr, err := converter.Convert(filter.FilterCondition)
		if err != nil {
			return "", err
		}
		if condExpr != "" {
			parts = append(parts, "("+condExpr+")")
		}
	}

	if len(parts) == 0 {
		return "", nil
	}

	return strings.Join(parts, " and "), nil
}
