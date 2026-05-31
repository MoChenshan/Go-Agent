// Package main demonstrates how to use the KnotAgent with interactive CLI chat.
//
// Usage:
//
//	go run main.go \
//	  -api-url="http://knot.woa.com/apigw/api/v1/agents/agui/{agent_id}" \
//	  -api-key="your-knot-api-key" \
//	  -model="your-model-name" \
//	  -user="your-username"
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

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	knotagent "git.woa.com/trpc-go/trpc-agent-go/trpc/agent/knot"
)

var (
	apiUrl  = flag.String("api-url", getEnvOrDefault("KNOT_API_URL", ""), "Knot API URL")
	apiKey  = flag.String("api-key", getEnvOrDefault("KNOT_API_KEY", ""), "Knot API key")
	kModel  = flag.String("model", getEnvOrDefault("KNOT_MODEL", "deepseek-v3"), "Knot model name")
	apiUser = flag.String("user", getEnvOrDefault("KNOT_USER", "your-rtx"), "Knot API user")
)

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func main() {
	flag.Parse()

	fmt.Println("Interactive Chat with KnotAgent")
	fmt.Printf("Model: %s\n", *kModel)
	fmt.Println(strings.Repeat("=", 50))

	chat := &knotChat{}
	if err := chat.setup(); err != nil {
		log.Fatalf("Setup failed: %v", err)
	}
	defer chat.runner.Close()

	if err := chat.startChat(context.Background()); err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
}

type knotChat struct {
	runner    runner.Runner
	userID    string
	sessionID string
}

func (c *knotChat) setup() error {
	ag := knotagent.New(
		"knot-assistant",
		knotagent.WithDescription("A Knot-powered AI assistant"),
		knotagent.WithKnotApiUrl(*apiUrl),
		knotagent.WithKnotApiKey(*apiKey),
		knotagent.WithKnotModel(*kModel),
		knotagent.WithKnotApiUser(*apiUser),
		knotagent.WithKnotEnableWebSearch(false),
	)

	c.runner = runner.NewRunner("knot-agent-chat", ag)
	c.userID = "user"
	c.sessionID = fmt.Sprintf("session-%d", time.Now().Unix())

	fmt.Printf("Chat ready! Session: %s\n\n", c.sessionID)
	return nil
}

func (c *knotChat) startChat(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("Commands:")
	fmt.Println("   /exit  - End the conversation")
	fmt.Println()

	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}

		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}

		if strings.ToLower(userInput) == "/exit" {
			fmt.Println("Goodbye!")
			return nil
		}

		if err := c.processMessage(ctx, userInput); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("input scanner error: %w", err)
	}
	return nil
}

func (c *knotChat) processMessage(ctx context.Context, userMessage string) error {
	eventChan, err := c.runner.Run(ctx, c.userID, c.sessionID, model.NewUserMessage(userMessage))
	if err != nil {
		return fmt.Errorf("failed to run KnotAgent: %w", err)
	}
	return c.processResponse(eventChan)
}

func (c *knotChat) processResponse(eventChan <-chan *event.Event) error {
	fmt.Print("Assistant: ")

	var fullContent strings.Builder
	for evt := range eventChan {
		if evt.Error != nil {
			fmt.Printf("\nError: %s\n", evt.Error.Message)
			continue
		}
		if len(evt.Response.Choices) > 0 {
			content := evt.Response.Choices[0].Delta.Content
			if content != "" {
				fmt.Print(content)
				fullContent.WriteString(content)
			}
		}
		if evt.Done {
			fmt.Println()
			break
		}
	}
	return nil
}
