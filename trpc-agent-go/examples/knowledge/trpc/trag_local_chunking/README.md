# tRAG Local Chunking Example

This example demonstrates how to enable local chunking mode using `WithDisableRemoteChunking(true)`.

## Chunking Mode Comparison

| Mode | Configuration | Data Source | Description |
|------|---------------|-------------|-------------|
| **Remote Chunking (Default)** | No config needed | `tragsource.*` | Upload raw text, tRAG server handles chunking |
| **Local Chunking** | `WithDisableRemoteChunking(true)` | `filesource`/`urlsource` | Client pre-chunks before upload |

## Use Cases

- When custom chunking strategy is needed
- When full control over the chunking process is required
- When pre-chunked data is already available

## Run

```bash
# Set environment variables
export TRAG_TOKEN="your-token"
export TRAG_RAG_CODE="your-rag-code"
export TRAG_NAMESPACE_CODE="your-namespace-code"
export TRAG_COLLECTION_CODE="your-collection-code"

# Run example
go run .

# Recreate database and run
go run . -recreate
```

## Core Code

```go
// Use generic source (not tragsource)
sources := []source.Source{
    filesource.New(files),  // Client-side chunking
}

// Enable local chunking mode
kb, _ := knowledge.New(
    knowledge.WithTRagOption(*tragOption),
    knowledge.WithSources(sources),
    knowledge.WithDisableRemoteChunking(true),  // Key configuration
)
```

## Notes

⚠️ **Do not mix**: When `WithDisableRemoteChunking(true)` is enabled, use generic sources (`filesource`, `urlsource`) instead of `tragsource`, otherwise large unchunked text may be uploaded directly.

