// Package main demonstrates tRAG knowledge filter capabilities.
// Shows: WithConditionedFilter and AgenticFilterSearchTool.
//
// IMPORTANT: To use metadata filtering, you must configure the filter fields
// in your tRAG Collection's field_list when creating the collection.
// For example, if you want to filter by "category", "language", "type",
// these fields must be declared in the Collection configuration.
//
// Example Collection field_list configuration:
//
//	[
//	  {"name": "category", "type": "string"},
//	  {"name": "language", "type": "string"},
//	  {"name": "type", "type": "string"}
//	]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.woa.com/trag/trag-sdk/go-trag"
	util "git.woa.com/trpc-go/trpc-agent-go/examples/knowledge"
	knowledge "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/sdk"
	tragsource "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/source"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/searchfilter"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	knowledgetool "trpc.group/trpc-go/trpc-agent-go/knowledge/tool"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

var (
	modelName = flag.String("model", util.GetEnvOrDefault("MODEL_NAME", "deepseek-chat"), "Model name")
	loadData  = flag.Bool("load_data", false, "Load data to tRAG")
	recreate  = flag.Bool("recreate", false, "Clear existing data before loading")
)

var (
	tragToken          = util.GetEnvOrDefault("TRAG_TOKEN", "")
	tragRagCode        = util.GetEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode  = util.GetEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode = util.GetEnvOrDefault("TRAG_COLLECTION_CODE", "")
	tragEmbeddingModel = util.GetEnvOrDefault("TRAG_EMBEDDING_MODEL", "")
	tragPolicyCode     = util.GetEnvOrDefault("TRAG_POLICY_CODE", "")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	fmt.Println("tRAG Filter Demo")
	fmt.Println("================")
	fmt.Printf("Model: %s\n", *modelName)

	_ = trpc.NewServer()

	if err := validateConfig(); err != nil {
		log.Fatalf("Config error: %v", err)
	}

	// Setup knowledge base
	kb, sources, err := setupKnowledgeBase(ctx)
	if err != nil {
		log.Fatalf("Setup failed: %v", err)
	}

	// Demo 1: Simple filter (category=machine-learning)
	fmt.Println("\n1. WithConditionedFilter: metadata.category=machine-learning")
	mlTool := knowledgetool.NewKnowledgeSearchTool(
		kb,
		knowledgetool.WithToolName("search_ml"),
		knowledgetool.WithToolDescription("Search machine learning content"),
		knowledgetool.WithConditionedFilter(
			searchfilter.Equal("metadata.category", "machine-learning"),
		),
	)
	runDemo(ctx, mlTool, "What is deep learning?")

	// Demo 2: AND filter (category=programming AND language=golang)
	fmt.Println("\n2. WithConditionedFilter: metadata.category=programming AND metadata.language=golang")
	goTool := knowledgetool.NewKnowledgeSearchTool(
		kb,
		knowledgetool.WithToolName("search_go"),
		knowledgetool.WithToolDescription("Search Go programming content"),
		knowledgetool.WithConditionedFilter(
			searchfilter.And(
				searchfilter.Equal("metadata.category", "programming"),
				searchfilter.Equal("metadata.language", "golang"),
			),
		),
	)
	runDemo(ctx, goTool, "How does Go handle concurrency?")

	// Demo 3: OR filter (category=ai OR category=machine-learning)
	fmt.Println("\n3. WithConditionedFilter: metadata.category=ai OR metadata.category=machine-learning")
	aiMlTool := knowledgetool.NewKnowledgeSearchTool(
		kb,
		knowledgetool.WithToolName("search_ai_ml"),
		knowledgetool.WithToolDescription("Search AI and ML content"),
		knowledgetool.WithConditionedFilter(
			searchfilter.Or(
				searchfilter.Equal("metadata.category", "ai"),
				searchfilter.Equal("metadata.category", "machine-learning"),
			),
		),
	)
	runDemo(ctx, aiMlTool, "What are the latest AI developments?")

	// Demo 4: Agentic filter (LLM decides the filter based on metadata)
	fmt.Println("\n4. AgenticFilterSearchTool: LLM decides filter based on query")
	sourcesMetadata := source.GetAllMetadata(sources)
	agenticTool := knowledgetool.NewAgenticFilterSearchTool(
		kb,
		sourcesMetadata,
		knowledgetool.WithToolName("smart_search"),
		knowledgetool.WithToolDescription("Smart search with auto-filtering by category, language, type, etc."),
	)
	runDemo(ctx, agenticTool, "Show me Go programming documentation")

	fmt.Println("\nDemo completed")
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

func setupKnowledgeBase(ctx context.Context) (*knowledge.Knowledge, []source.Source, error) {
	tragClient := sdk.NewTRPCTRagClient("trpc.test.knowledge.trag", trag.WithToken(tragToken))

	if *recreate {
		log.Printf("Clearing tRAG database...")
		if err := clearDatabase(ctx, tragClient); err != nil {
			return nil, nil, err
		}
		time.Sleep(3 * time.Second)
	}

	tragOption := sdk.NewTRagOption(
		sdk.WithClient(tragClient),
		sdk.WithInstanceCode(tragRagCode),
		sdk.WithNamespaceCode(tragNamespaceCode),
		sdk.WithCollectionCode(tragCollectionCode),
		sdk.WithEmbeddingModel(tragEmbeddingModel),
		sdk.WithPolicyCode(tragPolicyCode),
	)

	// Create sources with metadata for filtering
	sources := []source.Source{
		tragsource.NewTextSource(
			[]tragsource.TextContent{
				{ID: "ml_intro", Name: "ML Introduction", Content: "Machine Learning is a field devoted to building methods that learn from data."},
				{ID: "ml_deep", Name: "Deep Learning", Content: "Deep learning uses artificial neural networks with multiple layers."},
			},
			tragsource.WithTextSourceName("ML_Docs"),
			tragsource.WithTextMetadata(map[string]any{
				"category": "machine-learning",
				"type":     "concept",
			}),
		),
		tragsource.NewTextSource(
			[]tragsource.TextContent{
				{ID: "go_intro", Name: "Go Programming", Content: "Go is a statically typed, compiled language designed at Google."},
				{ID: "go_concurrency", Name: "Go Concurrency", Content: "Go supports concurrent execution via goroutines and channels."},
			},
			tragsource.WithTextSourceName("Go Docs"),
			tragsource.WithTextMetadata(map[string]any{
				"category": "programming",
				"language": "golang",
				"type":     "documentation",
			}),
		),
		tragsource.NewTextSource(
			[]tragsource.TextContent{
				{ID: "ai_2024", Name: "AI in 2024", Content: "In 2024, multimodal AI models made significant advancements."},
			},
			tragsource.WithTextSourceName("AI News"),
			tragsource.WithTextMetadata(map[string]any{
				"category": "ai",
			}),
		),
	}

	kb, err := knowledge.New(
		knowledge.WithTRagOption(*tragOption),
		knowledge.WithSources(sources),
	)
	if err != nil {
		return nil, nil, err
	}

	if *loadData {
		if err := kb.Load(ctx, knowledge.WithTRagRateLimit(300*time.Millisecond, 5)); err != nil {
			return nil, nil, err
		}
		log.Printf("Loaded documents to tRAG")
	}

	return kb, sources, nil
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
		return fmt.Errorf("clean failed: %s", resp.Message)
	}
	return nil
}

func runDemo(ctx context.Context, searchTool tool.Tool, query string) {
	agent := llmagent.New(
		"filter-demo",
		llmagent.WithModel(openai.New(*modelName)),
		llmagent.WithTools([]tool.Tool{searchTool}),
	)

	r := runner.NewRunner(
		"trag-filter-demo",
		agent,
		runner.WithSessionService(inmemory.NewSessionService()),
	)
	defer r.Close()

	fmt.Printf("   Query: %s\n", query)
	eventChan, err := r.Run(ctx, "user", "session", model.NewUserMessage(query))
	if err != nil {
		log.Printf("   Error: %v", err)
		return
	}

	for evt := range eventChan {
		util.PrintEventWithToolCalls(evt)
	}
}
