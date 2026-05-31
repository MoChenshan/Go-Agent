package retriever

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/retriever"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/searchfilter"
)

func TestBuildFilterExpr(t *testing.T) {
	tests := []struct {
		name     string
		filter   *retriever.QueryFilter
		expected string
		wantErr  bool
	}{
		{
			name:     "nil filter",
			filter:   nil,
			expected: "",
			wantErr:  false,
		},
		{
			name: "metadata only (implicit metadata keys)",
			filter: &retriever.QueryFilter{
				Metadata: map[string]any{
					"source": "wiki",
					"year":   2023,
				},
			},
			// map iteration order is random, check contains
			// expected: "source == 'wiki' && year == 2023",
		},
		{
			name: "simple condition with metadata prefix",
			filter: &retriever.QueryFilter{
				FilterCondition: &searchfilter.UniversalFilterCondition{
					Field:    "metadata.topic",
					Operator: "eq",
					Value:    "AI",
				},
			},
			expected: "(topic = \"AI\")",
			wantErr:  false,
		},
		{
			name: "simple condition invalid field (no prefix)",
			filter: &retriever.QueryFilter{
				FilterCondition: &searchfilter.UniversalFilterCondition{
					Field:    "topic",
					Operator: "eq",
					Value:    "AI",
				},
			},
			wantErr: true, // mimics inmemory strictness
		},
		{
			name: "AND condition",
			filter: &retriever.QueryFilter{
				FilterCondition: &searchfilter.UniversalFilterCondition{
					Operator: "and",
					Value: []*searchfilter.UniversalFilterCondition{
						{Field: "metadata.topic", Operator: "eq", Value: "AI"},
						{Field: "metadata.score", Operator: "gt", Value: 0.8},
					},
				},
			},
			expected: "((topic = \"AI\") and (score > 0.8))",
			wantErr:  false,
		},
		{
			name: "OR condition",
			filter: &retriever.QueryFilter{
				FilterCondition: &searchfilter.UniversalFilterCondition{
					Operator: "or",
					Value: []*searchfilter.UniversalFilterCondition{
						{Field: "metadata.topic", Operator: "eq", Value: "AI"},
						{Field: "metadata.topic", Operator: "eq", Value: "ML"},
					},
				},
			},
			expected: "((topic = \"AI\") or (topic = \"ML\"))",
			wantErr:  false,
		},
		{
			name: "nested condition",
			filter: &retriever.QueryFilter{
				FilterCondition: &searchfilter.UniversalFilterCondition{
					Operator: "and",
					Value: []*searchfilter.UniversalFilterCondition{
						{
							Operator: "or",
							Value: []*searchfilter.UniversalFilterCondition{
								{Field: "metadata.a", Operator: "eq", Value: 1},
								{Field: "metadata.b", Operator: "eq", Value: 2},
							},
						},
						{Field: "metadata.c", Operator: "eq", Value: 3},
					},
				},
			},
			expected: "(((a = 1) or (b = 2)) and (c = 3))",
			wantErr:  false,
		},
		{
			name: "IN condition",
			filter: &retriever.QueryFilter{
				FilterCondition: &searchfilter.UniversalFilterCondition{
					Field:    "metadata.tag",
					Operator: "in",
					Value:    []string{"a", "b"},
				},
			},
			expected: "(tag in (\"a\", \"b\"))",
			wantErr:  false,
		},
		{
			name: "BETWEEN condition",
			filter: &retriever.QueryFilter{
				FilterCondition: &searchfilter.UniversalFilterCondition{
					Field:    "metadata.year",
					Operator: "between",
					Value:    []int{2000, 2020},
				},
			},
			expected: "((year >= 2000 and year <= 2020))",
			wantErr:  false,
		},
		{
			name: "LIKE condition",
			filter: &retriever.QueryFilter{
				FilterCondition: &searchfilter.UniversalFilterCondition{
					Field:    "metadata.title",
					Operator: "like",
					Value:    "hello%",
				},
			},
			expected: "(title like \"hello%\")",
			wantErr:  false,
		},
		{
			name: "Time comparison with metadata field",
			filter: &retriever.QueryFilter{
				FilterCondition: &searchfilter.UniversalFilterCondition{
					Field:    "metadata.created_at",
					Operator: "gt",
					Value:    time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			expected: "(created_at > 1672531200)",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildFilterExpr(tt.filter)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			if tt.name == "metadata only (implicit metadata keys)" {
				// Handle random map order
				assert.Contains(t, got, "source = \"wiki\"")
				assert.Contains(t, got, "year = 2023")
				assert.Contains(t, got, " and ")
			} else {
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestNormalizeField(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"metadata.source", "source"},
		{"metadata.very.deep.key", "very.deep.key"},
		{"metadata.category", "category"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, normalizeField(tt.input))
	}
}
