// This example demonstrates how to use Venus Reranker with tRPC framework.
//
// Integration Guide:
//
//	reranker, err := venus.New(
//	    venus.WithEndpoint("/v1/rerank"),
//	    venus.WithServiceName("trpc.venus.Reranker"),
//	    venus.WithModel("default"),
//	    venus.WithTopN(5),
//	)
//	if err != nil {
//	    // handle error
//	}
//	k := knowledge.New(
//	    knowledge.WithReranker(reranker),
//	)
//
// Required environment variables:
//   - VENUS_URL: Venus reranker endpoint URL
//   - VENUS_API_KEY: (Optional) Venus API key
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/reranker/venus"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/reranker"

	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

const serviceName = "trpc.test.venus.reranker"

var (
	endpoint  = flag.String("endpoint", getEnvOrDefault("VENUS_URL", "/v1/rerank"), "Venus endpoint URL")
	modelName = flag.String("model", "default", "Venus reranker model name")
	apiKey    = flag.String("api-key", getEnvOrDefault("VENUS_API_KEY", ""), "Venus API key (optional)")
	topN      = flag.Int("topn", 0, "Return top N results (0 means all)")
)

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

type testCase struct {
	name      string
	query     string
	documents []string
}

func main() {
	flag.Parse()

	// Initialize tRPC framework
	trpc.NewServer()

	ctx := context.Background()

	fmt.Println("Venus Reranker Demo")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Endpoint: %s\n", *endpoint)
	fmt.Printf("Service Name: %s\n", serviceName)
	fmt.Printf("Model: %s\n", *modelName)
	fmt.Println()

	testCases := []testCase{
		{
			name:  "Panda Question",
			query: "what is a panda?",
			documents: []string{
				"Justice Juan M. Merchan will hear arguments over whether the former president violated his gag order.",
				"The giant panda (Ailuropoda melanoleuca), sometimes called a panda bear or simply panda.",
				"Paris is in France.",
			},
		},
		{
			name:  "Technical Question",
			query: "How to optimize database query performance?",
			documents: []string{
				"Database indexing improves query performance by reducing the amount of data scanned.",
				"Python is a popular programming language for data science.",
				"Cloud computing offers scalable resources for applications.",
				"Query optimization involves analyzing execution plans and adding appropriate indexes.",
				"Machine learning models require large datasets for training.",
			},
		},
		{
			name:  "Semantic Understanding",
			query: "What are the benefits of microservices architecture?",
			documents: []string{
				"Microservices allow independent deployment and scaling of individual components.",
				"Monolithic applications are easier to develop initially.",
				"Service-oriented architecture promotes loose coupling between services.",
				"Containerization with Docker simplifies deployment across environments.",
				"Breaking down applications into smaller services enables faster development cycles.",
			},
		},
	}

	for _, tc := range testCases {
		fmt.Printf("\n%s\n", strings.Repeat("=", 70))
		fmt.Printf("Case: %s\n", tc.name)
		fmt.Printf("Query: %s\n", tc.query)
		fmt.Printf("%s\n", strings.Repeat("=", 70))

		runRerank(ctx, tc.query, tc.documents)
	}
}

func runRerank(ctx context.Context, queryText string, documents []string) {
	// Build reranker options
	opts := []venus.Option{
		venus.WithModel(*modelName),
		venus.WithServiceName(serviceName),
	}

	if *endpoint != "" {
		opts = append(opts, venus.WithEndpoint(*endpoint))
	}

	if *apiKey != "" {
		opts = append(opts, venus.WithAPIKey(*apiKey))
	}
	if *topN > 0 {
		opts = append(opts, venus.WithTopN(*topN))
	}

	// Create Venus reranker
	r, err := venus.New(opts...)
	if err != nil {
		log.Printf("Failed to create Venus reranker: %v", err)
		return
	}

	// Prepare candidates
	candidates := make([]*reranker.Result, len(documents))
	for i, doc := range documents {
		candidates[i] = &reranker.Result{
			Document: &document.Document{Content: doc},
			Score:    0, // Initial score
		}
	}

	// Create query
	query := &reranker.Query{
		Text:       queryText,
		FinalQuery: queryText,
	}

	// Perform reranking
	fmt.Println("\n--- Original Order ---")
	for i, doc := range documents {
		fmt.Printf("%d. %s\n", i+1, doc)
	}

	results, err := r.Rerank(ctx, query, candidates)
	if err != nil {
		log.Printf("Rerank failed: %v", err)
		return
	}

	fmt.Println("\n--- Reranked Results (by relevance score) ---")
	for i, res := range results {
		fmt.Printf("%d. [Score: %.7f] %s\n", i+1, res.Score, res.Document.Content)
	}
}
