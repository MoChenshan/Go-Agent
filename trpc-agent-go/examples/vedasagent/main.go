//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

// Package main demonstrates multi-turn chat using the Taiji Agent with streaming
// output and session management.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/agent/vedas"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

var (
	vedasToken      = getEnvOrDefault("VEDAS_TOKEN", "7xxxxxxx")
	vedasAppGroupID = getEnvOrDefault("VEDAS_APP_GROUP_ID", "0")
)

type vedasPlan struct {
	// Agent builder. build vedas agent & control agent file
	agentBuilder *vedas.AgentBuilder

	// runner for vedas agent
	runner runner.Runner

	// single session id for vedas agent
	sessionID string
	// planID for a single vedas agent task
	planID string
	// projectID for a vedas agent conversation contains multiple plans
	projectID string

	// attachments for a single vedas agent conversation
	attachments []string
	// planFiles for a single vedas agent conversation
	planFiles map[string]string
}

func main() {
	// Parse command line flags.
	flag.Parse()

	fmt.Printf("🚀 Plan Chat with Vedas Agent\n")
	fmt.Printf("Type '/exit' to end the conversation\n")
	fmt.Printf("Type '/new' to start a new conversation\n")
	fmt.Printf("Type '/file result' to check last conversation result files\n")
	fmt.Printf("Type '/file process' to check last conversation result files\n")
	fmt.Printf("Type '/download <file_id>' to download a specific file\n")
	fmt.Printf("Type '/upload <file_path>' to upload a specific file\n")
	fmt.Println(strings.Repeat("=", 50))
	appGroupID, _ := strconv.Atoi(vedasAppGroupID)
	// create vedas plan
	plan := &vedasPlan{
		agentBuilder: vedas.New(vedasToken, appGroupID),
	}
	if err := plan.start(context.Background()); err != nil {
		log.Fatalf("Chat failed: %v", err)
	}
}

// start starts the main loop for the agent.
func (v *vedasPlan) start(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("👤 You: ")
		if !scanner.Scan() {
			break
		}
		userInput := strings.TrimSpace(scanner.Text())
		if userInput == "" {
			continue
		}
		if userInput == "/exit" {
			fmt.Println("👋 Goodbye!")
			break
		}
		command, err := v.userInputHandle(ctx, userInput)
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
		if command {
			continue
		}
		if err := v.processMessage(ctx, userInput); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
	}
	return nil
}

func (v *vedasPlan) userInputHandle(
	ctx context.Context,
	input string,
) (bool, error) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false, nil
	}

	command := parts[0]
	args := parts[1:]

	switch command {
	case "/new":
		return true, v.run(ctx, "")
	case "/file":
		return true, v.fileList(ctx, args)
	case "/download":
		return true, v.downloadFile(ctx, args)
	case "/upload":
		return true, v.uploadFiles(ctx, args)
	default:
		return false, nil
	}
}

func (v *vedasPlan) uploadFiles(ctx context.Context, args []string) error {
	for _, path := range args {
		if err := v.singleUpload(ctx, path); err != nil {
			return err
		}
	}
	return nil
}

func (v *vedasPlan) singleUpload(ctx context.Context, path string) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	fid, url, err := v.agentBuilder.CreateFile(ctx, fileInfo.Name(), fileInfo.Size())
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %v", path, err)
	}
	defer file.Close()

	if err := v.uploadFileContent(ctx, url, file, fileInfo.Size()); err != nil {
		return fmt.Errorf("upload process for %s failed: %w", fileInfo.Name(), err)
	}
	if err := v.agentBuilder.CompleteFile(ctx, fid); err != nil {
		return err
	}
	v.attachments = append(v.attachments, fid)
	return nil
}

func (v *vedasPlan) uploadFileContent(ctx context.Context, url string, file io.Reader, size int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, file)
	if err != nil {
		return fmt.Errorf("failed to create PUT request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = size

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload failed with status %s", resp.Status)
	}
	return nil
}

func (v *vedasPlan) run(ctx context.Context, _ string) error {
	sessionID := fmt.Sprintf("vedas-session-%d", time.Now().Unix())
	fmt.Printf("start a new vedas agent, session id: %s\n", sessionID)
	if err := v.setup(ctx, sessionID); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}
	return nil
}

func (v *vedasPlan) setup(_ context.Context, sessionID string) error {
	var (
		agentName = "vedas-agent"
		appName   = "vedas-plan"
	)
	v.sessionID = sessionID
	v.planFiles = make(map[string]string)
	v.attachments = make([]string, 0)
	v.planID = ""
	v.projectID = ""
	// use sessionID as projectID to control chat flow
	vedasConfig := vedas.NewConfigs(
		vedas.WithAttachments(v.attachments),
	)
	agent, err := v.agentBuilder.Build(
		vedas.WithName(agentName),
		vedas.WithDescription("A helpful AI assistant powered by Vedas."),
		vedas.WithConfigs(vedasConfig),
	)
	if err != nil {
		return err
	}
	v.runner = runner.NewRunner(
		appName,
		agent,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
	)
	return nil
}

// processMessage handles a single message exchange.
func (v *vedasPlan) processMessage(ctx context.Context, userMessage string) error {
	if v.runner == nil {
		return fmt.Errorf("runner is not initialized, please run /new command first")
	}
	// Create a new message with the user input.
	message := model.NewUserMessage(userMessage)
	// Run the agent through the runner.
	eventChan, err := v.runner.Run(ctx, "user", v.sessionID, message, agent.WithRequestID(v.projectID))
	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}

	// Process response.
	return v.processResponse(eventChan)
}

// processResponse handles both streaming and non-streaming responses.
func (c *vedasPlan) processResponse(eventChan <-chan *event.Event) error {
	fmt.Print("🤖 Assistant: ")

	var (
		fullContent      string
		assistantStarted bool
	)

	for event := range eventChan {
		if err := c.handleEvent(event, &assistantStarted, &fullContent); err != nil {
			return err
		}

		// Check if this is the final event.
		if event.Done {
			fmt.Printf("\n")
			break
		}
	}

	return nil
}

// handleEvent processes a single event from the event channel.
func (c *vedasPlan) handleEvent(
	event *event.Event,
	assistantStarted *bool,
	fullContent *string,
) error {
	// Handle errors.
	if event.Error != nil {
		fmt.Printf("\n❌ Error: %s\n", event.Error.Message)
		return nil
	}
	c.planID = event.Response.ID
	c.projectID = event.ID
	if len(event.Choices) > 0 {
		choice := event.Choices[0]
		content, thought := choice.Delta.Content, choice.Delta.ReasoningContent
		c.displayContent(content, thought, assistantStarted, fullContent)
	}

	return nil
}

// displayContent prints content to console.
func (c *vedasPlan) displayContent(
	content string,
	thought string,
	assistantStarted *bool,
	fullContent *string,
) {
	if !*assistantStarted {
		*assistantStarted = true
	}

	// Print reasoning content in gray color
	if thought != "" {
		fmt.Printf("\033[90m%s\033[0m", thought)
	}

	fmt.Print(content)
	*fullContent += content
}

// fileList lists vedas files grouped by planID
func (v *vedasPlan) fileList(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing argument for /file")
	}
	process := args[0] == "process"
	items, err := v.agentBuilder.FileList(ctx, v.planID, process)
	if err != nil {
		return err
	}
	if v.planFiles == nil {
		v.planFiles = make(map[string]string)
	}
	for _, item := range items {
		v.planFiles[item.FileID] = item.FileName
	}

	for _, item := range items {
		fmt.Println("vedas plan file list:")
		fmt.Printf("%s: %s\n", item.FileID, item.FileName)
	}
	return nil
}

// downloadFile downloads a vedas file
func (c *vedasPlan) downloadFile(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing argument for /download")
	}
	fid := args[0]
	fName, ok := c.planFiles[fid]
	if !ok {
		return fmt.Errorf("❌ Error: file id %s not found", fid)
	}
	reader, err := c.agentBuilder.Download(ctx, fid, c.planID)
	if err != nil {
		return fmt.Errorf("❌ Error: %v", err)
	}
	defer reader.Close()

	localFile, err := os.Create(fName)
	if err != nil {
		return fmt.Errorf("❌ Error: %v", err)
	}
	defer localFile.Close()

	_, err = io.Copy(localFile, reader)
	if err != nil {
		return fmt.Errorf("❌ Error: %v", err)
	}
	return nil
}

func getEnvOrDefault(envVar string, defaultValue string) string {
	if value := os.Getenv(envVar); value != "" {
		return value
	}
	return defaultValue
}
