# TRag Knowledge-Enhanced Chat Demo

This example demonstrates how to use TRag knowledge base with the trpc-agent-go framework, featuring **optimal chunking strategy** to avoid double-chunking issues.

## 🌟 Key Features

### ✅ Proper Chunking Strategy

This demo uses **TRag-specific sources** that skip client-side chunking and let TRag platform handle all chunking:

| Source Type | Function | Chunking Behavior |
|-------------|----------|-------------------|
| **File Source** | `tragsource.NewFileSource()` | ❌ No client chunking<br>✅ TRag server chunking |
| **Directory Source** | `tragsource.NewDirectorySource()` | ❌ No client chunking<br>✅ TRag server chunking |
| **Text Source** | `tragsource.NewTextSource()` | ❌ No client chunking<br>✅ TRag server chunking |
| **URL Source** | `tragsource.NewURLSource()` | ❌ No client chunking<br>✅ TRag server processing |

### ❌ What NOT to Do (Double Chunking)

```go
// DON'T use standard sources with TRag - causes double chunking!
filesource.New([]string{"doc.txt"})     // ❌ Client chunks → TRag chunks again
dirsource.New([]string{"./docs"})       // ❌ Client chunks → TRag chunks again
urlsource.New([]string{"https://..."}) // ❌ Client chunks → TRag chunks again
```

### ✅ What to Do (Single Chunking)

```go
// DO use TRag-specific sources - optimal single chunking!
tragsource.NewFileSource([]string{"doc.txt"})           // ✅ TRag chunks optimally
tragsource.NewDirectorySource("./docs")                 // ✅ TRag chunks optimally
tragsource.NewTextSourceFromStrings([]string{"..."})    // ✅ TRag chunks optimally
tragsource.NewURLSource([]string{"https://..."})        // ✅ TRag fetches & chunks
```

## 📋 Prerequisites

### Environment Variables

Set the following environment variables before running:

```bash
export TRAG_TOKEN="your-trag-token"
export TRAG_RAG_CODE="your-rag-code"
export TRAG_NAMESPACE_CODE="your-namespace-code"
export TRAG_COLLECTION_CODE="your-collection-code"
export TRAG_EMBEDDING_MODEL="your-embedding-model"  # Optional
export TRAG_POLICY_CODE="your-policy-code"          # Optional
```

### Required Files

Ensure these files exist (or modify the paths in `main.go`):

```
examples/knowledge/trpc/trag/
├── main.go
├── ../data/llm.md          # Sample markdown file
└── ../dir/                 # Sample directory with documents
```

## 🚀 Running the Demo

### Build

```bash
cd examples/knowledge/trpc/trag
go build .
```

### Run (Load Data)

First time or when you want to reload data:

```bash
./trag -model claude-4-sonnet-20250514 -load_data=true
```

### Run (Skip Loading)

If data is already loaded in TRag:

```bash
./trag -model claude-4-sonnet-20250514 -load_data=false
```

## 💬 Usage

### Interactive Commands

- **Regular input**: Ask questions naturally
- `/history`: Show conversation history
- `/new`: Start a new session
- `/exit`: End the conversation

### Example Queries

```
👤 You: What is TRag?
👤 You: Explain the Transformer architecture
👤 You: What is a Large Language Model?
👤 You: How does attention mechanism work?
```

## 🔧 Code Structure

### Main Components

```go
// 1. Create TRag client
tragClient := sdk.NewTRPCTRagClient("trpc.test.knowledge.trag", 
    trag.WithToken(tragToken))

// 2. Configure TRag options
tragOption := sdk.NewTRagOption(
    sdk.WithClient(tragClient),
    sdk.WithInstanceCode(tragRagCode),
    sdk.WithPolicyCode(tragPolicyCode),  // Chunking policy!
)

// 3. Create TRag-specific sources (NO client chunking!)
sources := []source.Source{
    tragsource.NewFileSource(files),
    tragsource.NewDirectorySource(dir),
    tragsource.NewTextSource(texts),
    tragsource.NewURLSource(urls),
}

// 4. Create knowledge base (default: uses TRag remote chunking)
kb, _ := knowledge.New(
    knowledge.WithTRagOption(*tragOption),
    knowledge.WithSources(sources),
)

// 5. Load with rate limiting
kb.Load(ctx, knowledge.WithTRagRateLimit(300*time.Millisecond, 5))
```

### Advanced: Local Chunking Mode

If you want full control over chunking and prefer to use local chunking instead of TRag's remote chunking:

```go
// Create knowledge base with local chunking
kb, _ := knowledge.New(
    knowledge.WithTRagOption(*tragOption),
    knowledge.WithSources(sources),
    knowledge.WithDisableRemoteChunking(true),  // Use ImportDocument instead of ImportFile
)
```

**When to use local chunking:**
- You need custom chunking strategies specific to your domain
- You want to pre-process documents before chunking
- You have already chunked content

**Note:** With local chunking enabled:
- Documents are sent via `ImportDocumentRequest` (synchronous)
- TRag will NOT perform additional chunking
- You should ensure your content is properly prepared before import

### Source Examples

#### File Source
```go
tragsource.NewFileSource(
    []string{"../data/llm.md"},
    tragsource.WithFileSourceName("LLM Documentation"),
    tragsource.WithFileMetadata(map[string]any{
        "type": "documentation",
    }),
)
```

#### Directory Source
```go
tragsource.NewDirectorySource(
    "../dir",
    tragsource.WithRecursive(true),
    tragsource.WithFileExtFilter([]string{".txt", ".md", ".pdf"}),
    tragsource.WithDirSourceName("Knowledge Directory"),
)
```

#### Text Source
```go
tragsource.NewTextSourceFromStrings(
    []string{
        "TRag is a powerful knowledge platform...",
        "Transformer architecture uses attention...",
    },
    tragsource.WithTextSourceName("Generated Docs"),
)
```

#### URL Source
```go
tragsource.NewURLSource(
    []string{"https://example.com/doc.pdf"},
    tragsource.WithURLName("External Document"),
)
```

## 🎯 Benefits of This Approach

### 1. **No Double Chunking**
- ❌ **Before**: Client chunks (1024 chars) → TRag chunks again = over-chunked
- ✅ **After**: TRag chunks once based on policy = optimal

### 2. **Better Retrieval Quality**
- Proper chunk boundaries based on TRag policy
- No fragmented micro-chunks
- Improved semantic coherence

### 3. **Reduced Network Overhead**
- Upload whole documents instead of many small chunks
- Fewer API calls to TRag ImportFiles

### 4. **Simplified Configuration**
- All chunking controlled by TRag policy
- No client-side chunk size configuration needed

## 📊 Performance

### Rate Limiting

The demo uses rate limiting to prevent API throttling:

```go
kb.Load(ctx, knowledge.WithTRagRateLimit(
    300*time.Millisecond,  // 3 QPS
    5,                      // Burst of 5
))
```

Adjust these values based on your TRag instance limits.

## 🐛 Troubleshooting

### Error: Missing Environment Variables

```bash
export TRAG_TOKEN="your-token"
export TRAG_RAG_CODE="your-code"
# ... etc
```

### Error: Files Not Found

Update file paths in `setupTRagKnowledgeBase()`:

```go
tragsource.NewFileSource(
    []string{"./your-actual-file.md"},  // Update path
)
```

### Error: Rate Limit Exceeded

Increase the rate limit interval:

```go
knowledge.WithTRagRateLimit(500*time.Millisecond, 3)  // Slower
```

## 📚 Related Documentation

- [TRag Chunking Guide](../../../../trpc/knowledge/trag/README_CHUNKING.md)
- [TRag Source API](../../../../trpc/knowledge/trag/source/)
- [Knowledge Base Integration](https://git.woa.com/trpc-go/trpc-agent-go)

## 🔗 Source Code

Key files in this demo:

```
examples/knowledge/trpc/trag/
├── main.go              # Main demo application
└── README.md            # This file

trpc/knowledge/trag/
├── knowledge.go         # TRag knowledge implementation
├── source/
│   ├── trag_file.go     # File source (no chunking)
│   ├── trag_directory.go # Directory source (no chunking)
│   ├── trag_text.go     # Text source (no chunking)
│   └── trag_url.go      # URL source (server-side)
└── README_CHUNKING.md   # Detailed chunking guide
```

## ✅ Summary

This demo showcases **best practices** for TRag integration:

1. ✅ Use TRag-specific sources (`tragsource.*`)
2. ✅ Let TRag platform handle all chunking
3. ✅ Configure chunking via TRag policy
4. ✅ Use rate limiting to prevent throttling
5. ✅ Support multiple source types (file, dir, text, URL)

**Result**: Optimal retrieval quality with no double-chunking overhead! 🚀
