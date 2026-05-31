// Package main demonstrates Taiji knowledge integration with the LLM agent.
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
	knowledge "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/taiji/sdk"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	dirsource "trpc.group/trpc-go/trpc-agent-go/knowledge/source/dir"
	filesource "trpc.group/trpc-go/trpc-agent-go/knowledge/source/file"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"

	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

// command line flags
var (
	modelName = flag.String("model", "deepseek-v3-0324", "Name of the model to use")
	loadData  = flag.Bool("load_data", false, "Load data from Taiji, if you have loaded data, set this flag to false")
)

// environment variables to configure Taiji
var (
	// refer https://iwiki.woa.com/p/4008515885, is devcloud environment URL here
	// taijiURL              = getEnvOrDefault("TAIJI_URL", "http://stream-server-online-openapi.turbotke.production.polaris:1081")
	taijiServiceName      = getEnvOrDefault("TAIJI_SERVICE", "trpc.test.knowledge.taiji")
	taijiToken            = getEnvOrDefault("TAIJI_TOKEN", "7auG*****")
	taijiWSID             = getEnvOrDefault("TAIJI_WSID", "10144")
	taijiEmbeddingIndexID = getEnvOrDefault("TAIJI_EMBEDDING_INDEX_ID", "7878")

	// refer https://iwiki.woa.com/p/4010689738
	taijiHYAPIToken = getEnvOrDefault("TAIJI_HY_API_TOKEN", "*******")
	taijiHYAPIURL   = getEnvOrDefault("TAIJI_HY_API_URL", "http://hunyuanaide.taiji.woa.com")
)

func main() {
	// Parse command line flags.
	flag.Parse()
	fmt.Printf("🧠 Taiji Knowledge-Enhanced Chat Demo\n")
	fmt.Printf("Model: %s\n", *modelName)
	fmt.Printf("Type 'exit' to end the conversation\n")
	fmt.Printf("Available tools: knowledge_search, calculator, current_time\n")
	fmt.Println(strings.Repeat("=", 50))

	trpc.NewServer()

	// Validate Taiji configuration.
	if err := validateTaijiConfig(); err != nil {
		log.Fatalf("Taiji configuration error: %v", err)
	}

	// Create and run the chat.
	chat := &taijiChat{
		modelName: *modelName,
	}

	if err := chat.run(); err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
}

// validateTaijiConfig validates required Taiji environment variables.
func validateTaijiConfig() error {
	required := map[string]string{
		"TAIJI_SERVICE_NAME": taijiServiceName,
		"TAIJI_TOKEN":        taijiToken,
		"TAIJI_WSID":         taijiWSID,
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

// taijiChat manages the conversation with Taiji knowledge integration.
type taijiChat struct {
	modelName string
	runner    runner.Runner
	userID    string
	sessionID string
	kb        *knowledge.Knowledge
}

// run starts the interactive chat session.
func (c *taijiChat) run() error {
	ctx := context.Background()

	// Setup the runner with Taiji knowledge base.
	if err := c.setup(ctx); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}

	// Start interactive chat.
	return c.startChat(ctx)
}

// setup creates the runner with LLM agent, Taiji knowledge base, and tools.
func (c *taijiChat) setup(ctx context.Context) error {
	// Create OpenAI model.
	modelInstance := openai.New(c.modelName)

	// Create Taiji knowledge base with sample documents.
	if err := c.setupTaijiKnowledgeBase(ctx); err != nil {
		return fmt.Errorf("failed to setup Taiji knowledge base: %w", err)
	}

	// Create LLM agent with knowledge and tools.
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(2000),
		Temperature: floatPtr(0.7),
		Stream:      true, // Enable streaming
	}

	agentName := "taiji-assistant"
	llmAgent := llmagent.New(
		agentName,
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("A helpful AI assistant with Taiji knowledge base access and calculator tools"),
		llmagent.WithInstruction("Use the knowledge_search tool to find relevant information from the Taiji knowledge base. Use calculator and current_time tools when appropriate. Be helpful and conversational."),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithKnowledge(c.kb), // This will automatically add the knowledge_search tool.
	)

	// Create session service.
	sessionService := inmemory.NewSessionService()

	// Create runner.
	appName := "taiji-chat"
	c.runner = runner.NewRunner(
		appName,
		llmAgent,
		runner.WithSessionService(sessionService),
	)

	// Setup identifiers.
	c.userID = "user"
	c.sessionID = fmt.Sprintf("taiji-session-%d", time.Now().Unix())

	fmt.Printf("✅ Taiji chat ready! Session: %s\n", c.sessionID)
	fmt.Printf("📚 Taiji knowledge base loaded with sample documents\n\n")

	return nil
}

// setupTaijiKnowledgeBase creates a Taiji knowledge base with sample documents.
func (c *taijiChat) setupTaijiKnowledgeBase(ctx context.Context) error {
	// Create Taiji options using functional option pattern.
	taijiOption := sdk.NewTaijiOption(
		sdk.WithEmbIndex(taijiEmbeddingIndexID),
		sdk.WithToken(taijiToken),
		sdk.WithWSID(taijiWSID),

		// Taiji HY API Token and URL are used to upsert your documents to Taiji
		sdk.WithTaijiHYAPIToken(taijiHYAPIToken),
		sdk.WithTaijiHYAPIURL(taijiHYAPIURL),

		// you can specify Taiji Host By target of trpc_go.yaml
		// or you can specify Taiji Host By WithServiceName
		// WithServiceName has lower priority than WithURL
		sdk.WithServiceName(taijiServiceName),
		// sdk.WithURL(taijiURL),
	)

	// Create diverse sources showcasing different types.
	sources := []source.Source{
		filesource.New(
			[]string{
				"../../exampledata/file/llm.md",
			},
			filesource.WithName("Large Language Model"),
			filesource.WithMetadataValue("type", "documentation"),
		),
		dirsource.New(
			[]string{
				"../../exampledata/dir",
			},
			dirsource.WithName("Data Directory"),
		),
	}

	var err error
	// Create Taiji knowledge base.
	c.kb, err = knowledge.New(
		knowledge.WithTaijiOption(taijiOption),
		knowledge.WithSources(sources),
	)
	if err != nil {
		return fmt.Errorf("failed to create Taiji knowledge: %w", err)
	}
	if *loadData {
		// Load the knowledge base with rate limiting to prevent API throttling.
		// Set rate limit to 3 QPS (300ms interval) with burst of 5 to handle occasional spikes.
		documentIDs, err := c.kb.Load(ctx, knowledge.WithTaijiRateLimit(300*time.Millisecond, 5))
		if err != nil {
			return fmt.Errorf("failed to load Taiji knowledge base: %w", err)
		}
		log.Printf("Successfully loaded %d documents to Taiji knowledge base", len(documentIDs))
		if len(documentIDs) > 0 {
			log.Printf("Document IDs: %v", documentIDs)
		}
	}
	return nil
}

// startChat runs the interactive conversation loop.
func (c *taijiChat) startChat(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("💡 Special commands:")
	fmt.Println("   /history  - Show conversation history")
	fmt.Println("   /new      - Start a new session")
	fmt.Println("   /exit      - End the conversation")
	fmt.Println()
	fmt.Println("🔍 Try asking questions like:")
	fmt.Println("   - What is Taiji?")
	fmt.Println("   - Explain the Transformer architecture.")
	fmt.Println("   - What is a Large Language Model?")
	fmt.Println("   - How does Byte-pair encoding work?")
	fmt.Println("   - What is an N-gram model?")
	fmt.Println("   - Calculate 15 * 23")
	fmt.Println("   - What time is it in PST?")
	fmt.Println("   - What tools are available in this chat demo?")

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
func (c *taijiChat) processMessage(ctx context.Context, userMessage string) error {
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
func (c *taijiChat) processStreamingResponse(eventChan <-chan *event.Event) error {
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
func (c *taijiChat) isToolEvent(event *event.Event) bool {
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
func (c *taijiChat) startNewSession() {
	c.sessionID = fmt.Sprintf("taiji-session-%d", time.Now().Unix())
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
