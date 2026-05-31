// Package main demonstrates LingShan knowledge base search.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/lingshan"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"

	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

var (
	defaultLingshanURL     = getEnvOrDefault("LINGSHAN_URL", "")
	defaultServiceName     = getEnvOrDefault("LINGSHAN_SERVICE_NAME", "trpc.test.knowledge.lingshan")
	defaultKnowledgeBaseID = getEnvOrDefault("LINGSHAN_KB_ID", "your-knowledge-base-id")
)

// command line flags
var (
	lingshanURL     = flag.String("url", defaultLingshanURL, "LingShan service URL")
	serviceName     = flag.String("service", defaultServiceName, "LingShan service name (e.g. trpc.lingshan.service)")
	knowledgeBaseID = flag.String("kb_id", defaultKnowledgeBaseID, "LingShan Knowledge Base ID")
	query           = flag.String("query", "query something about llm", "Query text to search")
)

func main() {
	flag.Parse()
	ctx := context.Background()

	fmt.Println("📚 LingShan Knowledge Search Demo")
	fmt.Println("==================================")
	fmt.Printf("LingShan URL: %s\n", *lingshanURL)
	fmt.Printf("Service Name: %s\n", *serviceName)
	fmt.Printf("Knowledge Base ID: %s\n", *knowledgeBaseID)

	trpc.NewServer()

	if *knowledgeBaseID == "" {
		log.Fatal("Knowledge Base ID is required. Set via -kb_id flag or LINGSHAN_KB_ID env var.")
	}

	// Create LingShan knowledge base.
	fmt.Println("\n1️⃣ Creating LingShan knowledge base...")
	kb := createKnowledgeBase()
	fmt.Println("   ✅ Knowledge base created")

	// Search
	fmt.Printf("\n2️⃣ Searching for '%s'...\n", *query)
	result, err := kb.Search(ctx, &knowledge.SearchRequest{
		Query:      *query,
		MaxResults: 3,
		MinScore:   0.0,
	})
	if err != nil {
		log.Fatalf("   ❌ Search failed: %v", err)
	}
	printSearchResults(result)

	fmt.Println("\n✅ Demo completed!")
}

func createKnowledgeBase() *lingshan.Knowledge {
	opts := []lingshan.Option{
		lingshan.WithKnowledgeBaseID(*knowledgeBaseID),
		lingshan.WithHTTPHeaders(http.Header{
			"Content-Type":        []string{"application/json"},
			"X-Gateway-Stage":     []string{"TEST"},
			"X-Gateway-SecretId":  []string{""},
			"X-Gateway-SecretKey": []string{""},
			"X-Gateway-Label":     []string{""},
			"trpc-trans-info":     []string{`{"X-Access-Proxy-User":"your-rtx"}`},
		}),
	}
	if *lingshanURL != "" {
		opts = append(opts, lingshan.WithURL(*lingshanURL))
	}
	if *serviceName != "" {
		opts = append(opts, lingshan.WithServiceName(*serviceName))
	}

	return lingshan.New(opts...)
}

func printSearchResults(result *knowledge.SearchResult) {
	fmt.Printf("   Found %d results:\n", len(result.Documents))
	for i, doc := range result.Documents {
		content := doc.Document.Content
		content = strings.ReplaceAll(content, "\n", " ")
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		fmt.Printf("   %d. score=%.3f: %s\n", i+1, doc.Score, content)
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
