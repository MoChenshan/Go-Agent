package sdk

import (
	"testing"

	"git.woa.com/trag/trag-sdk/go-trag"
	"github.com/stretchr/testify/assert"
)

func TestCheckTRagOption(t *testing.T) {
	tests := []struct {
		name        string
		option      *TRagOption
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil option",
			option:      nil,
			expectError: true,
			errorMsg:    "tRAG option is nil",
		},
		{
			name: "nil client",
			option: &TRagOption{
				Client:         nil,
				RagCode:        "test-rag",
				NamespaceCode:  "test-ns",
				CollectionCode: "test-col",
			},
			expectError: true,
			errorMsg:    "tRAG client is nil",
		},
		{
			name: "empty rag code",
			option: &TRagOption{
				Client:         &trag.TRag{},
				RagCode:        "",
				NamespaceCode:  "test-ns",
				CollectionCode: "test-col",
			},
			expectError: true,
			errorMsg:    "instance code is empty",
		},
		{
			name: "empty namespace code",
			option: &TRagOption{
				Client:         &trag.TRag{},
				RagCode:        "test-rag",
				NamespaceCode:  "",
				CollectionCode: "test-col",
			},
			expectError: true,
			errorMsg:    "namespace code is empty",
		},
		{
			name: "empty collection code",
			option: &TRagOption{
				Client:         &trag.TRag{},
				RagCode:        "test-rag",
				NamespaceCode:  "test-ns",
				CollectionCode: "",
			},
			expectError: true,
			errorMsg:    "collection code is empty",
		},
		{
			name: "valid option",
			option: &TRagOption{
				Client:         &trag.TRag{},
				RagCode:        "test-rag",
				NamespaceCode:  "test-ns",
				CollectionCode: "test-col",
				EmbeddingModel: "bge-large-en",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckTRagOption(tt.option)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewTRagOption(t *testing.T) {
	client := &trag.TRag{}
	tests := []struct {
		name     string
		options  []Option
		expected *TRagOption
	}{
		{
			name:     "no options",
			options:  []Option{},
			expected: &TRagOption{},
		},
		{
			name: "with client",
			options: []Option{
				WithClient(client),
			},
			expected: &TRagOption{
				Client: client,
			},
		},
		{
			name: "with instance code",
			options: []Option{
				WithInstanceCode("test-instance"),
			},
			expected: &TRagOption{
				RagCode: "test-instance",
			},
		},
		{
			name: "with namespace code",
			options: []Option{
				WithNamespaceCode("test-namespace"),
			},
			expected: &TRagOption{
				NamespaceCode: "test-namespace",
			},
		},
		{
			name: "with collection code",
			options: []Option{
				WithCollectionCode("test-collection"),
			},
			expected: &TRagOption{
				CollectionCode: "test-collection",
			},
		},
		{
			name: "with embedding model",
			options: []Option{
				WithEmbeddingModel("bge-large-en"),
			},
			expected: &TRagOption{
				EmbeddingModel: "bge-large-en",
			},
		},
		{
			name: "with all options",
			options: []Option{
				WithClient(client),
				WithInstanceCode("test-instance"),
				WithNamespaceCode("test-namespace"),
				WithCollectionCode("test-collection"),
				WithEmbeddingModel("bge-large-en"),
			},
			expected: &TRagOption{
				Client:         client,
				RagCode:        "test-instance",
				NamespaceCode:  "test-namespace",
				CollectionCode: "test-collection",
				EmbeddingModel: "bge-large-en",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewTRagOption(tt.options...)
			assert.Equal(t, tt.expected, result)
		})
	}
}
