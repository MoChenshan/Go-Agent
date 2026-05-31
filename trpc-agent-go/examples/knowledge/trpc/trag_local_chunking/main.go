// Package main demonstrates tRAG with local chunking mode (DisableRemoteChunking).
// In this mode, documents are chunked on the client side before uploading to tRAG.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.woa.com/trag/trag-sdk/go-trag"
	knowledge "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	filesource "trpc.group/trpc-go/trpc-agent-go/knowledge/source/file"

	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

// command line flags
var (
	recreate = flag.Bool("recreate", false, "Clear existing tRAG database before loading")
)

// environment variables
var (
	tragToken          = getEnvOrDefault("TRAG_TOKEN", "")
	tragRagCode        = getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode  = getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode = getEnvOrDefault("TRAG_COLLECTION_CODE", "")
	tragEmbeddingModel = getEnvOrDefault("TRAG_EMBEDDING_MODEL", "")
	tragPolicyCode     = getEnvOrDefault("TRAG_POLICY_CODE", "")
)

func main() {
	flag.Parse()
	fmt.Println("=== tRAG Local Chunking Demo ===")
	fmt.Println("This demo uses WithDisableRemoteChunking(true) to enable client-side chunking.")
	fmt.Println()

	_ = trpc.NewServer()

	if err := validateConfig(); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	if err := run(); err != nil {
		log.Fatalf("Demo failed: %v", err)
	}

	fmt.Println("\n✅ Demo completed successfully!")
}

func run() error {
	ctx := context.Background()

	// Create tRAG client
	tragClient := sdk.NewTRPCTRagClient("trpc.test.knowledge.trag", trag.WithToken(tragToken))

	// Clear database if requested
	if *recreate {
		fmt.Println("🗑️  Clearing existing database...")
		if err := clearDatabase(ctx, tragClient); err != nil {
			return fmt.Errorf("failed to clear database: %w", err)
		}
		time.Sleep(5 * time.Second)
	}

	// Create tRAG options
	tragOption := sdk.NewTRagOption(
		sdk.WithClient(tragClient),
		sdk.WithInstanceCode(tragRagCode),
		sdk.WithNamespaceCode(tragNamespaceCode),
		sdk.WithCollectionCode(tragCollectionCode),
		sdk.WithEmbeddingModel(tragEmbeddingModel),
		sdk.WithPolicyCode(tragPolicyCode),
	)

	// Use generic file source (NOT tragsource)
	// With DisableRemoteChunking, these will be chunked on client side
	sources := []source.Source{
		filesource.New(
			[]string{"../../exampledata/file/llm.md"},
			filesource.WithName("LLM Documentation"),
			filesource.WithMetadataValue("type", "documentation"),
		),
	}

	fmt.Println("📄 Creating knowledge base with local chunking...")
	fmt.Println("   - Using: filesource.New (generic source)")
	fmt.Println("   - Mode:  WithDisableRemoteChunking(true)")
	fmt.Println()

	// Create knowledge base with local chunking enabled
	kb, err := knowledge.New(
		knowledge.WithTRagOption(*tragOption),
		knowledge.WithSources(sources),
		knowledge.WithDisableRemoteChunking(true), // Enable local chunking
	)
	if err != nil {
		return fmt.Errorf("failed to create knowledge base: %w", err)
	}

	// Load documents
	fmt.Println("📤 Loading documents (client-side chunking)...")
	if err := kb.Load(ctx, knowledge.WithTRagRateLimit(300*time.Millisecond, 5)); err != nil {
		return fmt.Errorf("failed to load documents: %w", err)
	}

	fmt.Println("✅ Documents loaded with local chunking!")
	return nil
}

func clearDatabase(ctx context.Context, client *trag.TRag) error {
	resp, err := client.CleanDocumentRequest(ctx, &trag.CleanDocumentsRequest{
		RagCode:        tragRagCode,
		NamespaceCode:  tragNamespaceCode,
		CollectionCode: tragCollectionCode,
	})
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("code=%d, message=%s", resp.Code, resp.Message)
	}
	return nil
}

func validateConfig() error {
	required := map[string]string{
		"TRAG_TOKEN":           tragToken,
		"TRAG_RAG_CODE":        tragRagCode,
		"TRAG_NAMESPACE_CODE":  tragNamespaceCode,
		"TRAG_COLLECTION_CODE": tragCollectionCode,
	}
	var missing []string
	for k, v := range required {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing: %s", strings.Join(missing, ", "))
	}
	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
