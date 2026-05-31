# TRag Data Management with Hooks

This example demonstrates how to use **Import Hooks** to sync TRag documents with your own database (MemoryStore), enabling document re-import capabilities.

## 🌟 Key Concept

**Problem**: When using TRag, you often need to:
- Track which documents belong to which source
- Re-import a source (delete old documents + import new ones)
- Maintain document metadata in your own database

**Solution**: Use Import Hooks to automatically track `sourceName` and `docID` in MemoryStore during import operations.

## 🎯 Features

- ✅ **Import** - Import documents with automatic sync to MemoryStore
- ✅ **Re-import** - Delete old documents by `source_name` filter and import new ones
- ✅ **Data Sync Hook** - Automatically tracks `sourceName` and `docID` in MemoryStore
- ✅ **Source-based Management** - Group documents by source for easy batch operations

## 📋 Prerequisites

### Environment Variables

```bash
export TRAG_TOKEN="your-trag-token"
export TRAG_RAG_CODE="your-rag-code"
export TRAG_NAMESPACE_CODE="your-namespace-code"
export TRAG_COLLECTION_CODE="your-collection-code"
export TRAG_EMBEDDING_MODEL="your-embedding-model"  # Optional
export TRAG_POLICY_CODE="your-policy-code"          # Optional
```

## 🚀 Quick Start

```bash
cd examples/knowledge/trpc/trag_data_manage

# Run the demo
go run .

# Clear database before running
go run . -clear
```

## 📖 What the Demo Shows

The demo demonstrates a 2-step workflow for source-based document management:

### Step 1: Import Documents

```
Step 1: Import documents from source
----------------------------------------
Importing documents from source: ai_docs
[SYNC] Importing: AI Overview (doc_id: 12345678...)
[SYNC] Synced to MemoryStore: AI Overview
Imported 3 documents to TRag and MemoryStore
```

**What happens**:
1. Create documents with `source_name: "ai_docs"` in metadata
2. Hook generates unique `doc_id` (UUID) for each document
3. Hook adds `doc_id`, `source_name`, `import_timestamp` to metadata
4. Documents are imported to TRag
5. Hook syncs records to MemoryStore

### Step 2: Re-import Source

```
Step 2: Re-import source (update documents)
----------------------------------------
Re-importing source: ai_docs
Deleted 15 chunks from TRag for source: ai_docs
Removed documents from MemoryStore for source: ai_docs
Importing new documents for source: ai_docs
[SYNC] Importing: AI Overview v2 (doc_id: 45678901...)
Re-imported 3 new documents
```

**What happens**:
1. Delete all old documents from TRag using filter: `source_name="ai_docs"`
2. Remove all documents from MemoryStore for source: `ai_docs`
3. Import new documents with same `source_name`
4. Hook syncs new records to MemoryStore

## 🔧 Hook Implementation

The core is the **Data Sync Hook** that syncs imported documents to MemoryStore:

```go
func createDataSyncHook(dataStore *DocumentDataStore, sourceName string) tragknowledge.ImportDocumentHook {
    return func(next tragknowledge.ImportDocumentFunc) tragknowledge.ImportDocumentFunc {
        return func(ctx context.Context, src source.Source, doc *document.Document) (*tragknowledge.ImportResult, error) {
            // 1. Generate unique doc_id
            docID := uuid.New().String()
            doc.Metadata["doc_id"] = docID
            doc.Metadata["source_name"] = sourceName
            doc.Metadata["import_timestamp"] = time.Now().Unix()

            // 2. Call actual import to TRag
            result, err := next(ctx, src, doc)
            if err != nil {
                return result, err
            }

            // 3. Sync to MemoryStore (your database)
            dataStore.Add(&DocumentRecord{
                DocID:       docID,
                SourceName:  sourceName,
                TraceID:     result.TraceID,
                DocumentNum: result.DocumentNum,
                ImportedAt:  time.Now(),
            })

            return result, nil
        }
    }
}
```

### Hook Execution Flow

```
kb.Load()
    ↓
Hook: Generate doc_id, add metadata
    ↓
TRag API: Import document
    ↓
Hook: Sync to MemoryStore
    ↓
Return result
```

## 🔧 Technical Details

### DocumentRecord Structure

```go
type DocumentRecord struct {
    ID          string         // Auto-generated record ID
    DocID       string         // UUID stored in TRag metadata
    SourceName  string         // Source name for grouping
    TraceID     string         // TRag import trace ID
    DocumentNum int            // Number of chunks created
    ImportedAt  time.Time      // Import timestamp
    Metadata    map[string]any // Additional metadata
}
```

### Re-import Pattern

```go
// 1. Delete by source_name filter
filter := fmt.Sprintf(`source_name="%s"`, sourceName)
kb.Delete(ctx, tragknowledge.WithFilterExpr(filter))

// 2. Remove from MemoryStore
dataStore.RemoveBySource(sourceName)

// 3. Import new documents
kb.Load(ctx)
```

## 💡 Use Cases

1. **Document Version Control** - Update documents when content changes
2. **Batch Re-import** - Replace all documents from a source at once
3. **Metadata Tracking** - Maintain document records in your own database
4. **Audit Trail** - Track what was imported, when, and trace IDs

## 📖 Related Files

- `main.go` - Demo implementation
- `document_store.go` - In-memory MemoryStore implementation
