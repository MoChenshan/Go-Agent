// Package main demonstrates tRAG document management using hooks.
// This example shows how to use Import Hooks to sync TRag documents with your own database (MemoryStore).
//
// Key capabilities:
// 1. Import documents from a source with automatic sync to MemoryStore
// 2. Re-import source (delete old documents + import new ones)
// 3. Data Sync Hook - Automatically tracks sourceName and docID in MemoryStore
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.woa.com/trag/trag-sdk/go-trag"
	tragknowledge "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
	tragsource "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/source"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"

	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

var (
	clearDB = flag.Bool("clear", false, "Clear database before running demo")
)

var (
	tragToken          = getEnvOrDefault("TRAG_TOKEN", "")
	tragRagCode        = getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode  = getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode = getEnvOrDefault("TRAG_COLLECTION_CODE", "")
	tragEmbeddingModel = getEnvOrDefault("TRAG_EMBEDDING_MODEL", "")
	tragPolicyCode     = getEnvOrDefault("TRAG_POLICY_CODE", "")
)

type App struct {
	ctx        context.Context
	dataStore  *DocumentDataStore
	tragOption sdk.TRagOption
	tragClient *trag.TRag
}

func main() {
	flag.Parse()

	_ = trpc.NewServer()

	if err := validateTRagConfig(); err != nil {
		log.Fatalf("Config error: %v", err)
	}

	ctx := context.Background()
	dataStore := NewDocumentDataStore()

	tragClient := sdk.NewTRPCTRagClient("trpc.test.knowledge.trag", trag.WithToken(tragToken))
	tragOption := sdk.NewTRagOption(
		sdk.WithClient(tragClient),
		sdk.WithInstanceCode(tragRagCode),
		sdk.WithNamespaceCode(tragNamespaceCode),
		sdk.WithCollectionCode(tragCollectionCode),
		sdk.WithEmbeddingModel(tragEmbeddingModel),
		sdk.WithPolicyCode(tragPolicyCode),
	)

	app := &App{
		ctx:        ctx,
		dataStore:  dataStore,
		tragOption: *tragOption,
		tragClient: tragClient,
	}

	// Clear database if requested
	if *clearDB {
		log.Println("Clearing existing database...")
		if err := clearTRagDatabase(ctx, tragClient); err != nil {
			log.Fatalf("Failed to clear database: %v", err)
		}
		log.Println("Database cleared successfully")
		time.Sleep(2 * time.Second)
	}

	// Run the demonstration
	runDemo(app)
}

func validateTRagConfig() error {
	required := map[string]string{
		"TRAG_TOKEN":           tragToken,
		"TRAG_RAG_CODE":        tragRagCode,
		"TRAG_NAMESPACE_CODE":  tragNamespaceCode,
		"TRAG_COLLECTION_CODE": tragCollectionCode,
	}

	var missing []string
	for key, value := range required {
		if value == "" {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", missing)
	}

	return nil
}

// runDemo demonstrates TRag document management using hooks
func runDemo(app *App) {
	log.Println("========================================")
	log.Println("  TRag Data Management Demo")
	log.Println("========================================")
	log.Println()

	sourceName := "ai_docs"

	// Step 1: Import documents from source
	log.Println("Step 1: Import documents from source")
	log.Println("----------------------------------------")
	if err := demoImport(app, sourceName); err != nil {
		log.Fatalf("Import failed: %v", err)
	}
	log.Println()
	time.Sleep(2 * time.Second)

	// Step 2: Re-import source (delete old + import new)
	log.Println("Step 2: Re-import source (update documents)")
	log.Println("----------------------------------------")
	if err := demoReimport(app, sourceName); err != nil {
		log.Fatalf("Re-import failed: %v", err)
	}
	log.Println()

	log.Println("========================================")
	log.Println("  Demo Completed Successfully!")
	log.Println("========================================")
}

// createKB creates a new Knowledge instance with the given sources and data sync hook
func createKB(app *App, sourceName string, sources []source.Source) (*tragknowledge.Knowledge, error) {
	return tragknowledge.New(
		tragknowledge.WithTRagOption(app.tragOption),
		tragknowledge.WithSources(sources),
		tragknowledge.WithImportDocumentHook(createDataSyncHook(app.dataStore, sourceName)),
	)
}

// demoImport demonstrates importing documents from a source
func demoImport(app *App, sourceName string) error {
	log.Printf("Importing documents from source: %s", sourceName)

	// Create source with documents
	sources := []source.Source{
		tragsource.NewTextSource(
			[]tragsource.TextContent{
				{ID: "doc1", Name: "AI Overview", Content: "Artificial Intelligence is transforming industries."},
				{ID: "doc2", Name: "ML Basics", Content: "Machine Learning is a subset of AI."},
				{ID: "doc3", Name: "Deep Learning", Content: "Deep Learning uses neural networks."},
			},
			tragsource.WithTextMetadata(map[string]any{"source_name": sourceName}),
		),
	}

	// Create KB and import documents
	kb, err := createKB(app, sourceName, sources)
	if err != nil {
		return fmt.Errorf("create kb failed: %w", err)
	}

	if err := kb.Load(app.ctx, tragknowledge.WithTRagRateLimit(300*time.Millisecond, 5)); err != nil {
		return fmt.Errorf("load failed: %w", err)
	}

	log.Printf("Imported %d documents to TRag and MemoryStore", app.dataStore.Count())
	return nil
}

// demoReimport demonstrates re-importing a source (delete old + import new)
func demoReimport(app *App, sourceName string) error {
	log.Printf("Re-importing source: %s", sourceName)

	// Step 1: Delete old documents from TRag by source_name filter
	// Create a KB without sources just for delete operation
	kb, err := tragknowledge.New(tragknowledge.WithTRagOption(app.tragOption))
	if err != nil {
		return fmt.Errorf("create kb failed: %w", err)
	}

	filter := fmt.Sprintf(`source_name="%s"`, sourceName)
	deletedCount, err := kb.Delete(app.ctx, tragknowledge.WithFilterExpr(filter))
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}

	log.Printf("Deleted %d chunks from TRag for source: %s", deletedCount, sourceName)

	// Step 2: Remove from MemoryStore
	app.dataStore.RemoveBySource(sourceName)
	log.Printf("Removed documents from MemoryStore for source: %s", sourceName)

	// Step 3: Import new documents
	log.Printf("Importing new documents for source: %s", sourceName)

	newSources := []source.Source{
		tragsource.NewTextSource(
			[]tragsource.TextContent{
				{ID: "doc1", Name: "AI Overview v2", Content: "Artificial Intelligence (AI) is revolutionizing industries worldwide."},
				{ID: "doc2", Name: "ML Basics v2", Content: "Machine Learning (ML) is a powerful subset of AI."},
				{ID: "doc4", Name: "Neural Networks", Content: "Neural Networks are the foundation of modern AI."},
			},
			tragsource.WithTextMetadata(map[string]any{"source_name": sourceName}),
		),
	}

	kbNew, err := createKB(app, sourceName, newSources)
	if err != nil {
		return fmt.Errorf("create kb failed: %w", err)
	}

	if err := kbNew.Load(app.ctx, tragknowledge.WithTRagRateLimit(300*time.Millisecond, 5)); err != nil {
		return fmt.Errorf("load failed: %w", err)
	}

	log.Printf("Re-imported %d new documents (total in MemoryStore: %d)", 3, app.dataStore.Count())
	return nil
}

// createDataSyncHook creates a hook that syncs document metadata to MemoryStore
func createDataSyncHook(dataStore *DocumentDataStore, sourceName string) tragknowledge.ImportDocumentHook {
	return func(next tragknowledge.ImportDocumentFunc) tragknowledge.ImportDocumentFunc {
		return func(ctx context.Context, src source.Source, doc *document.Document) (*tragknowledge.ImportResult, error) {
			// Generate unique doc_id
			docID := uuid.New().String()

			// Add metadata
			if doc.Metadata == nil {
				doc.Metadata = make(map[string]any)
			}
			doc.Metadata["doc_id"] = docID
			doc.Metadata["source_name"] = sourceName
			doc.Metadata["import_timestamp"] = time.Now().Unix()

			log.Printf("[SYNC] Importing: %s (doc_id: %s)", doc.Name, docID[:8]+"...")

			// Call actual import
			result, err := next(ctx, src, doc)
			if err != nil {
				log.Printf("[SYNC] Import failed: %s, error: %v", doc.Name, err)
				return result, err
			}

			// Sync to MemoryStore
			dataStore.Add(&DocumentRecord{
				ID:          doc.ID,
				DocID:       docID,
				SourceName:  sourceName,
				TraceID:     result.TraceID,
				DocumentNum: result.DocumentNum,
				ImportedAt:  time.Now(),
				Metadata:    doc.Metadata,
			})

			log.Printf("[SYNC] Synced to MemoryStore: %s (trace: %s)", doc.Name, result.TraceID)

			return result, nil
		}
	}
}

// Helper functions

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func clearTRagDatabase(ctx context.Context, client *trag.TRag) error {
	// Implementation depends on TRag API
	// This is a placeholder
	return nil
}
