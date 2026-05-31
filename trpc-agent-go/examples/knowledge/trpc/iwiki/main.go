// Package main demonstrates iWiki knowledge base search with Rio authentication.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/iwiki"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"

	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

var (
	defaultURL    = getEnvOrDefault("IWIKI_URL", "http://api-idc.sgw.woa.com/ebus/iwiki/prod")
	defaultPaasID = getEnvOrDefault("IWIKI_PAAS_ID", "")
	defaultToken  = getEnvOrDefault("IWIKI_TOKEN", "")

	// Optional iWiki space ID
	defaultSpaceID = getEnvOrDefault("IWIKI_SPACE_ID", "")
)

// command line flags
var (
	iwikiURL = flag.String("url", defaultURL, "iWiki base URL (e.g. http://api-idc.sgw.woa.com/ebus/iwiki/prod)")
	paasID   = flag.String("paas_id", defaultPaasID, "TAI platform PaasID")
	token    = flag.String("token", defaultToken, "TAI platform application token")
	spaceID  = flag.String("space_id", defaultSpaceID, "iWiki space ID (optional)")

	serviceName = flag.String("service", "trpc.test.knowledge.iwiki", "tRPC service name")
	query       = flag.String("query", "trpc pgvector score calculation", "Query text to search")
	topK        = flag.Int("top_k", 5, "Number of results to return")
	identity    = flag.String("identity", "", "x-tai-identity for passthrough (optional)")
	noTruncate  = flag.Bool("no_truncate", false, "Do not truncate search result content")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	fmt.Println("iWiki Knowledge Search Demo")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("URL: %s\n", *iwikiURL)
	fmt.Printf("Service Name: %s\n", *serviceName)
	fmt.Printf("PaasID: %s\n", *paasID)

	trpc.NewServer()

	if err := validateConfig(); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// Create iWiki knowledge base.
	fmt.Println("\nCreating iWiki knowledge base...")
	kb := createKnowledgeBase()
	fmt.Println("Knowledge base created")

	// Search
	fmt.Printf("\nSearching for '%s' (top_k=%d)...\n", *query, *topK)
	result, err := kb.Search(ctx, &knowledge.SearchRequest{
		Query:      *query,
		MaxResults: *topK,
	})
	if err != nil {
		log.Fatalf("Search failed: %v", err)
	}
	printSearchResults(result)

	fmt.Println("\nDemo completed!")
}

func validateConfig() error {
	if *paasID == "" {
		return fmt.Errorf("PaasID is required. Set via -paas_id flag or IWIKI_PAAS_ID env var")
	}
	if *token == "" {
		return fmt.Errorf("Token is required. Set via -token flag or IWIKI_TOKEN env var")
	}
	return nil
}

func createKnowledgeBase() *iwiki.Knowledge {
	opts := []iwiki.Option{
		iwiki.WithPaasID(*paasID),
		iwiki.WithToken(*token),
	}

	if *iwikiURL != "" {
		opts = append(opts, iwiki.WithURL(*iwikiURL))
	}
	if *serviceName != "" {
		opts = append(opts, iwiki.WithServiceName(*serviceName))
	}

	// Configure search scope.
	searchConf := &iwiki.SearchConf{}
	if *spaceID != "" {
		sid, err := strconv.Atoi(*spaceID)
		if err != nil {
			log.Fatalf("invalid space_id: %v", err)
		}
		searchConf.SpaceIDs = []int{sid}
	}
	opts = append(opts, iwiki.WithSearchConf(searchConf))

	// Set optional identity passthrough header.
	if *identity != "" {
		headers := http.Header{}
		headers.Set("x-tai-identity", *identity)
		opts = append(opts, iwiki.WithHTTPHeaders(headers))
	}

	return iwiki.New(opts...)
}

func printSearchResults(result *knowledge.SearchResult) {
	fmt.Printf("Found %d results:\n", len(result.Documents))
	for i, doc := range result.Documents {
		content := doc.Document.Content
		content = strings.ReplaceAll(content, "\n", " ")
		if !*noTruncate && len(content) > 200 {
			content = content[:200] + "..."
		}
		fmt.Printf("  %d. [score=%.3f] %s\n", i+1, doc.Score, doc.Document.Name)
		fmt.Printf("     %s\n", content)
		if url, ok := doc.Document.Metadata["url"].(string); ok && url != "" {
			fmt.Printf("     URL: %s\n", url)
		}
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
