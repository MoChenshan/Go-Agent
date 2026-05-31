// Package main demonstrates tRAG knowledge integration with the LLM agent.
package main

import (
	"bufio"
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
	tragsource "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/trag/source"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"

	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

// command line flags
var (
	modelName = flag.String("model", "claude-4-sonnet-20250514", "Name of the model to use")
	loadData  = flag.Bool("load_data", true, "Load data from tRAG, if you have loaded data, set this flag to false")
	recreate  = flag.Bool("recreate", false, "Clear existing tRAG database before loading new data")
)

// environment variables to configure tRAG
var (
	tragToken          = getEnvOrDefault("TRAG_TOKEN", "")
	tragRagCode        = getEnvOrDefault("TRAG_RAG_CODE", "")
	tragNamespaceCode  = getEnvOrDefault("TRAG_NAMESPACE_CODE", "")
	tragCollectionCode = getEnvOrDefault("TRAG_COLLECTION_CODE", "")
	tragEmbeddingModel = getEnvOrDefault("TRAG_EMBEDDING_MODEL", "")
	tragPolicyCode     = getEnvOrDefault("TRAG_POLICY_CODE", "")
)

func main() {
	// Parse command line flags.
	flag.Parse()
	fmt.Printf("🧠 tRAG Knowledge-Enhanced Chat Demo\n")
	fmt.Printf("Model: %s\n", *modelName)
	fmt.Printf("Load Data: %v\n", *loadData)
	fmt.Printf("Recreate Database: %v\n", *recreate)
	fmt.Printf("Type 'exit' to end the conversation\n")
	fmt.Printf("Available tools: knowledge_search, calculator, current_time\n")
	fmt.Println(strings.Repeat("=", 50))

	_ = trpc.NewServer()

	// Validate tRAG configuration.
	if err := validateTRagConfig(); err != nil {
		log.Fatalf("tRAG configuration error: %v", err)
	}
	// Create and run the chat.
	chat := &tragChat{
		modelName: *modelName,
	}
	if err := chat.run(); err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
}

// validateTRagConfig validates required tRAG environment variables.
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
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return nil
}

// tragChat manages the conversation with tRAG knowledge integration.
type tragChat struct {
	modelName string
	runner    runner.Runner
	userID    string
	sessionID string
	kb        *knowledge.Knowledge
}

// run starts the interactive chat session.
func (c *tragChat) run() error {
	ctx := context.Background()

	// Setup the runner with tRAG knowledge base.
	if err := c.setup(ctx); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}

	// Start interactive chat.
	return c.startChat(ctx)
}

// setup creates the runner with LLM agent, tRAG knowledge base, and tools.
func (c *tragChat) setup(ctx context.Context) error {
	// Create OpenAI model.
	modelInstance := openai.New(c.modelName)

	// Create tRAG knowledge base with sample documents.
	if err := c.setupTRagKnowledgeBase(ctx); err != nil {
		return fmt.Errorf("failed to setup tRAG knowledge base: %w", err)
	}

	// Create LLM agent with knowledge and tools.
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(2000),
		Temperature: floatPtr(0.7),
		Stream:      true, // Enable streaming
	}

	agentName := "trag-assistant"
	llmAgent := llmagent.New(
		agentName,
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("A helpful AI assistant with tRAG knowledge base access and calculator tools"),
		llmagent.WithInstruction("Use the knowledge_search tool to find relevant information from the tRAG knowledge base. Use calculator and current_time tools when appropriate. Be helpful and conversational."),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithKnowledge(c.kb), // This will automatically add the knowledge_search tool.
	)

	// Create session service.
	sessionService := inmemory.NewSessionService()

	// Create runner.
	appName := "trag-chat"
	c.runner = runner.NewRunner(
		appName,
		llmAgent,
		runner.WithSessionService(sessionService),
	)

	// Setup identifiers.
	c.userID = "user"
	c.sessionID = fmt.Sprintf("trag-session-%d", time.Now().Unix())

	fmt.Printf("✅ tRAG chat ready! Session: %s\n", c.sessionID)
	fmt.Printf("📚 tRAG knowledge base loaded with sample documents\n\n")

	return nil
}

// setupTRagKnowledgeBase creates a tRAG knowledge base with sample documents.
func (c *tragChat) setupTRagKnowledgeBase(ctx context.Context) error {
	// Create tRAG client.
	tragClient := sdk.NewTRPCTRagClient("trpc.test.knowledge.trag", trag.WithToken(tragToken))

	// Clear existing database if recreate flag is set
	if *recreate {
		log.Printf("Clearing existing tRAG database...")
		if err := c.clearTRagDatabase(ctx, tragClient); err != nil {
			return fmt.Errorf("failed to clear tRAG database: %w", err)
		}
		time.Sleep(10 * time.Second)
		log.Printf("Successfully cleared tRAG database")
	}

	// Create tRAG options.
	tragOption := sdk.NewTRagOption(
		sdk.WithClient(tragClient),
		sdk.WithInstanceCode(tragRagCode),
		sdk.WithNamespaceCode(tragNamespaceCode),
		sdk.WithCollectionCode(tragCollectionCode),
		sdk.WithEmbeddingModel(tragEmbeddingModel),
		sdk.WithPolicyCode(tragPolicyCode),
	)

	// Create diverse sources showcasing different types.
	// IMPORTANT: Use TRag-specific sources to avoid double chunking!
	// TRag platform will handle all chunking based on the configured policy.
	sources := []source.Source{
		// TRag file source - NO client-side chunking, TRag handles it
		tragsource.NewFileSource(
			[]string{
				"../../exampledata/file/llm.md",
			},
			tragsource.WithFileSourceName("LLM Documentation"),
			tragsource.WithFileMetadata(map[string]any{
				"type":     "documentation",
				"category": "machine-learning",
			}),
		),

		// TRag directory source - NO client-side chunking
		tragsource.NewDirectorySource(
			"../../exampledata/dir",
			tragsource.WithRecursive(true),
			tragsource.WithFileExtFilter([]string{".txt", ".md", ".pdf"}),
			tragsource.WithDirSourceName("Knowledge Directory"),
		),

		// TRag text source - for programmatically generated content
		tragsource.NewTextSource(
			[]tragsource.TextContent{
				{
					ID:      "text_1",
					Name:    "TRag Overview",
					Content: "TRag is a powerful knowledge base platform that supports semantic search and RAG applications.",
				},
				{
					ID:      "text_2",
					Name:    "Transformer Architecture",
					Content: "Transformer architecture revolutionized natural language processing with self-attention mechanisms.",
				},
				{
					ID:      "text_3",
					Name:    "Large Language Models",
					Content: "Large Language Models are trained on massive text corpora to understand and generate human language.",
				},
			},
			tragsource.WithTextSourceName("Generated Documentation"),
			tragsource.WithTextMetadata(map[string]any{
				"generated": true,
				"version":   "1.0",
			}),
		),
	}

	var err error
	// Create tRAG knowledge base.
	// IMPORTANT: Using TRag-specific sources ensures no double chunking.
	// TRag platform will handle all chunking based on the policy configuration.
	// Benefits:
	//   - Optimal chunk sizes based on TRag policy
	//   - No over-chunking (chunk of chunks)
	//   - Better retrieval quality
	//   - Reduced network overhead
	c.kb, err = knowledge.New(
		knowledge.WithTRagOption(*tragOption),
		knowledge.WithSources(sources),
	)
	if err != nil {
		return fmt.Errorf("failed to create tRAG knowledge: %w", err)
	}
	if *loadData {
		// Load the knowledge base with rate limiting to prevent API throttling.
		// Set rate limit to 3 QPS (300ms interval) with burst of 5 to handle occasional spikes.
		err := c.kb.Load(ctx, knowledge.WithTRagRateLimit(300*time.Millisecond, 5))
		if err != nil {
			return fmt.Errorf("failed to load TRag knowledge base: %w", err)
		}
		log.Printf("Successfully loaded documents to TRag knowledge base")
	}
	return nil
}

// clearTRagDatabase clears all documents from the tRAG database.
func (c *tragChat) clearTRagDatabase(ctx context.Context, client *trag.TRag) error {
	req := &trag.CleanDocumentsRequest{
		RagCode:        tragRagCode,
		NamespaceCode:  tragNamespaceCode,
		CollectionCode: tragCollectionCode,
	}

	resp, err := client.CleanDocumentRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("clean document request failed: %w", err)
	}

	if resp.Code != 0 {
		return fmt.Errorf("clean document failed: code=%d, message=%s, trace=%s",
			resp.Code, resp.Message, resp.TraceID)
	}

	return nil
}

// startChat runs the interactive conversation loop.
func (c *tragChat) startChat(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("💡 Special commands:")
	fmt.Println("   /history  - Show conversation history")
	fmt.Println("   /new      - Start a new session")
	fmt.Println("   /exit      - End the conversation")
	fmt.Println()
	fmt.Println("🔍 Try asking questions like:")
	fmt.Println("   - What is tRAG?")
	fmt.Println("   - Explain the Transformer architecture.")
	fmt.Println("   - What is a Large Language Model?")
	fmt.Println("   - How does attention mechanism work?")
	fmt.Println()
	fmt.Println("📝 This demo uses TRag-specific sources to avoid double chunking:")
	fmt.Println("   ✅ tragsource.NewFileSource    - Files without client chunking")
	fmt.Println("   ✅ tragsource.NewDirectorySource - Directories without client chunking")
	fmt.Println("   ✅ tragsource.NewTextSource    - In-memory text without chunking")
	fmt.Println("   ✅ tragsource.NewURLSource     - URLs processed by TRag server")
	fmt.Println()

	for {
		fmt.Print("👤 You: ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}

		// Handle special commands.
		switch strings.ToLower(userInput) {
		case "/exit":
			fmt.Println("👋 Goodbye!")
			return nil
		case "/history":
			userInput = "show our conversation history"
		case "/new":
			c.startNewSession()
			continue
		}

		// Process the user message.
		if err := c.processMessage(ctx, userInput); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}

		fmt.Println() // Add spacing between turns
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("input scanner error: %w", err)
	}

	return nil
}

// processMessage handles a single message exchange.
func (c *tragChat) processMessage(ctx context.Context, userMessage string) error {
	message := model.NewUserMessage(userMessage)

	// Run the agent through the runner.
	eventChan, err := c.runner.Run(ctx, c.userID, c.sessionID, message)
	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}

	// Process streaming response.
	return c.processStreamingResponse(eventChan)
}

// processStreamingResponse handles the streaming response from the agent.
func (c *tragChat) processStreamingResponse(eventChan <-chan *event.Event) error {
	fmt.Print("🤖 Assistant: ")

	var assistantStarted bool
	var fullContent string

	for event := range eventChan {
		if event == nil {
			continue
		}

		// Handle errors.
		if event.Error != nil {
			fmt.Printf("\n❌ Error: %s\n", event.Error.Message)
			continue
		}

		// Detect and display tool calls.
		if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
			if assistantStarted {
				fmt.Printf("\n")
			}
			fmt.Printf("🔧 Tool calls initiated:\n")
			for _, toolCall := range event.Choices[0].Message.ToolCalls {
				fmt.Printf("   • %s (ID: %s)\n", toolCall.Function.Name, toolCall.ID)
				if len(toolCall.Function.Arguments) > 0 {
					fmt.Printf("     Args: %s\n", string(toolCall.Function.Arguments))
				}
			}
			fmt.Printf("\n🔄 Executing tools...\n")
		}

		// Detect tool responses.
		if event.Response != nil && len(event.Response.Choices) > 0 {
			hasToolResponse := false
			for _, choice := range event.Response.Choices {
				if choice.Message.Role == model.RoleTool && choice.Message.ToolID != "" {
					fmt.Printf("✅ Tool response (ID: %s): %s\n",
						choice.Message.ToolID,
						strings.TrimSpace(choice.Message.Content))
					hasToolResponse = true
				}
			}
			if hasToolResponse {
				continue
			}
		}

		// Process streaming content.
		if len(event.Choices) > 0 {
			choice := event.Choices[0]

			// Handle streaming delta content.
			if choice.Delta.Content != "" {
				if !assistantStarted {
					assistantStarted = true
				}
				fmt.Print(choice.Delta.Content)
				fullContent += choice.Delta.Content
			}
		}

		// Check if this is the final event.
		// Don't break on tool response events (Done=true but not final assistant response).
		if event.Done && !c.isToolEvent(event) {
			fmt.Printf("\n")
			break
		}
	}

	return nil
}

// isToolEvent checks if an event is a tool response (not a final response).
func (c *tragChat) isToolEvent(event *event.Event) bool {
	if event.Response == nil {
		return false
	}
	if len(event.Choices) > 0 && len(event.Choices[0].Message.ToolCalls) > 0 {
		return true
	}
	if len(event.Choices) > 0 && event.Choices[0].Message.ToolID != "" {
		return true
	}

	// Check if this is a tool response by examining choices.
	for _, choice := range event.Response.Choices {
		if choice.Message.Role == model.RoleTool {
			return true
		}
	}

	return false
}

// startNewSession creates a new chat session.
func (c *tragChat) startNewSession() {
	c.sessionID = fmt.Sprintf("trag-session-%d", time.Now().Unix())
	fmt.Printf("🔄 New session started: %s\n\n", c.sessionID)
}

func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
