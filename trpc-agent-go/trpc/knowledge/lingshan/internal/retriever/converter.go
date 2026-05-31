// Package retriever implements the knowledge retriever interface for LingShan.
package retriever

import (
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/lingshan/internal/client"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/searchfilter"
)

// ConditionConverter converts a UniversalFilterCondition to a LingShan FilterCondition.
type ConditionConverter struct{}

// Convert implements the searchfilter.Converter interface.
func (c *ConditionConverter) Convert(cond *searchfilter.UniversalFilterCondition) (*client.FilterCondition, error) {
	if cond == nil {
		return nil, nil
	}

	pbCond := &client.FilterCondition{
		Field: cond.Field,
		Value: cond.Value,
	}

	// Recursive conversion for logical operators
	if cond.Operator == searchfilter.OperatorAnd || cond.Operator == searchfilter.OperatorOr {
		if cond.Operator == searchfilter.OperatorAnd {
			pbCond.Operator = client.FilterOperatorAND
		} else {
			pbCond.Operator = client.FilterOperatorOR
		}

		var childConds []*searchfilter.UniversalFilterCondition
		if children, ok := cond.Value.([]*searchfilter.UniversalFilterCondition); ok {
			childConds = children
		} else if childrenIfce, ok := cond.Value.([]any); ok {
			for _, child := range childrenIfce {
				if c, ok := child.(*searchfilter.UniversalFilterCondition); ok {
					childConds = append(childConds, c)
				}
			}
		}

		for _, child := range childConds {
			converted, err := c.Convert(child)
			if err != nil {
				return nil, err
			}
			if converted != nil {
				pbCond.Conditions = append(pbCond.Conditions, converted)
			}
		}
		pbCond.Value = nil // Clear value for logic ops
	} else {
		// Map comparison operators
		pbCond.Operator = mapOperator(cond.Operator)
	}

	return pbCond, nil
}

func mapOperator(op string) string {
	switch op {
	case searchfilter.OperatorEqual:
		return client.FilterOperatorEQ
	case searchfilter.OperatorNotEqual:
		return client.FilterOperatorNE
	case searchfilter.OperatorGreaterThan:
		return client.FilterOperatorGT
	case searchfilter.OperatorGreaterThanOrEqual:
		return client.FilterOperatorGTE
	case searchfilter.OperatorLessThan:
		return client.FilterOperatorLT
	case searchfilter.OperatorLessThanOrEqual:
		return client.FilterOperatorLTE
	case searchfilter.OperatorIn:
		return client.FilterOperatorIN
	case searchfilter.OperatorNotIn:
		return client.FilterOperatorNotIN
	case searchfilter.OperatorBetween:
		return client.FilterOperatorBetween
	case searchfilter.OperatorLike:
		return client.FilterOperatorLike
	case searchfilter.OperatorNotLike:
		return client.FilterOperatorNotLike
	default:
		return op
	}
}
