# tRAG Filter Example

Demonstrates tRAG knowledge base filtering with both programmatic and agentic approaches.

## Features

- **WithConditionedFilter**: Programmatic metadata filtering (Equal, AND, OR)
- **AgenticFilterSearchTool**: LLM-driven automatic filter selection based on query

## Prerequisites

When creating the tRAG Collection, configure filter fields in `field_list`:

```json
[
  {"name": "category", "type": "string"},
  {"name": "language", "type": "string"},
  {"name": "type", "type": "string"}
]
```

## Run

```bash
export TRAG_TOKEN=your-token
export TRAG_RAG_CODE=your-rag-code
export TRAG_NAMESPACE_CODE=your-namespace
export TRAG_COLLECTION_CODE=your-collection
export TRAG_EMBEDDING_MODEL=your-embedding-model  # optional
export TRAG_POLICY_CODE=your-policy-code          # optional
export MODEL_NAME=deepseek-chat                   # optional

go run main.go -recreate=true   # first run with data loading
go run main.go -load_data=false # subsequent runs without reloading
```

## Demo Scenarios

### 1. Simple Filter
```go
searchfilter.Equal("metadata.category", "machine-learning")
```

### 2. AND Filter
```go
searchfilter.And(
    searchfilter.Equal("metadata.category", "programming"),
    searchfilter.Equal("metadata.language", "golang"),
)
```

### 3. OR Filter
```go
searchfilter.Or(
    searchfilter.Equal("metadata.category", "ai"),
    searchfilter.Equal("metadata.category", "machine-learning"),
)
```

### 4. Agentic Filter
LLM automatically selects filters based on user query and available metadata.

```go
knowledgetool.NewAgenticFilterSearchTool(kb, source.GetAllMetadata(sources))
```

## Notes

- Field names must use `metadata.` prefix (e.g., `metadata.category`)
- Only metadata fields configured in Collection's `field_list` can be filtered
