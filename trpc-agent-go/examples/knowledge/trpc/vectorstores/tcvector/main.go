//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package main demonstrates using Tencent VectorDB for vector storage with tRPC.
//
// Two connection modes are demonstrated:
//  1. Config mode: Uses trpc_go.yaml configuration with service name.
//  2. HTTPURL mode: Passes URL/username/password directly via options.
//
// Required environment variables:
//   - OPENAI_API_KEY: Your OpenAI API key for LLM and embeddings
//   - OPENAI_BASE_URL: (Optional) Custom OpenAI API endpoint
//   - MODEL_NAME: (Optional) Model name to use, defaults to deepseek-chat
//   - TCVECTOR_URL: Tencent VectorDB URL
//   - TCVECTOR_USERNAME: Tencent VectorDB username
//   - TCVECTOR_PASSWORD: Tencent VectorDB password
//
// Example usage:
//
//	export OPENAI_BASE_URL=xxx
//	export OPENAI_API_KEY=xxx
//	export MODEL_NAME=xxx
//	export TCVECTOR_URL=http://localhost:8080
//	export TCVECTOR_USERNAME=root
//	export TCVECTOR_PASSWORD=xxx
//	go run main.go
package main

import (
	"context"
	"fmt"
	"log"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/client"
	util "git.woa.com/trpc-go/trpc-agent-go/examples/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/openai"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source/file"
	knowledgetool "trpc.group/trpc-go/trpc-agent-go/knowledge/tool"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/tcvector"
	"trpc.group/trpc-go/trpc-agent-go/model"
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/storage/tcvector"
)

var (
	modelName = util.GetEnvOrDefault("MODEL_NAME", "deepseek-chat")
	url       = util.GetEnvOrDefault("TCVECTOR_URL", "")
	username  = util.GetEnvOrDefault("TCVECTOR_USERNAME", "")
	password  = util.GetEnvOrDefault("TCVECTOR_PASSWORD", "")
)

func main() {
	// load trpc config
	_ = trpc.NewServer()

	fmt.Println("🔮 Tencent VectorDB Demo")
	fmt.Println("========================")

	// Test both connection modes.
	fmt.Println("\n[1] Run Config mode (build tcvector client from trpc_go.yaml)...")
	if err := runConfigMode(); err != nil {
		log.Printf("Config mode failed: %v", err)
	} else {
		fmt.Println("✅ Config mode: OK")
	}

	fmt.Println("\n[2] Run HTTPURL mode (build tcvector client from HTTPURL, username, password)...")
	if err := runHTTPURLMode(); err != nil {
		log.Printf("HTTPURL mode failed: %v", err)
	} else {
		fmt.Println("✅ HTTPURL mode: OK")
	}

	fmt.Println("\n🎉 Demo completed!")
}

// runConfigMode demonstrates using trpc_go.yaml configuration with service name.
func runConfigMode() error {
	ctx := context.Background()

	fmt.Println("📊 Connecting via config: trpc.agent.knowledge.tcvector")

	// Create TCVector store using trpc_go.yaml configuration.
	vs, err := tcvector.New(
		tcvector.WithExtraOptions(client.WithServiceName("trpc.agent.knowledge.tcvector")),
	)
	if err != nil {
		return fmt.Errorf("failed to create vector store: %w", err)
	}

	// Create file source
	src := file.New(
		[]string{util.ExampleDataPath("file/llm.md")},
		file.WithName("LLM Docs"),
	)

	// Create knowledge base
	kb := knowledge.New(
		knowledge.WithVectorStore(vs),
		knowledge.WithEmbedder(openai.New()),
		knowledge.WithSources([]source.Source{src}),
	)

	fmt.Println("📥 Loading knowledge into Tencent VectorDB...")
	if err := kb.Load(ctx, knowledge.WithShowProgress(true)); err != nil {
		return fmt.Errorf("failed to load: %w", err)
	}

	// Create knowledge search tool
	searchTool := knowledgetool.NewKnowledgeSearchTool(kb)

	// Create agent
	agent := llmagent.New(
		"tcvector-config-assistant",
		llmagent.WithModel(openaimodel.New(modelName)),
		llmagent.WithTools([]tool.Tool{searchTool}),
	)

	// Create runner
	r := runner.NewRunner(
		"tcvector-config-chat",
		agent,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
	)
	defer r.Close()

	// Test query
	fmt.Println("🔍 Querying knowledge from Tencent VectorDB...")
	eventChan, err := r.Run(ctx, "user", "session-config",
		model.NewUserMessage("What are Large Language Models?"))
	if err != nil {
		return fmt.Errorf("run failed: %w", err)
	}

	fmt.Print("🤖 Response: ")
	for evt := range eventChan {
		util.PrintEventWithToolCalls(evt)
		if evt.IsFinalResponse() && len(evt.Response.Choices) > 0 {
			fmt.Println(evt.Response.Choices[0].Message.Content)
		}
	}

	return nil
}

// runHTTPURLMode demonstrates passing URL/username/password directly via options.
func runHTTPURLMode() error {
	ctx := context.Background()

	if url == "" {
		return fmt.Errorf("TCVECTOR_URL is required")
	}
	fmt.Printf("📊 Connecting via HTTPURL: %s\n", url)

	// Create TCVector store using direct URL/username/password.
	vs, err := tcvector.New(
		tcvector.WithURL(url),
		tcvector.WithUsername(username),
		tcvector.WithPassword(password),
	)
	if err != nil {
		return fmt.Errorf("failed to create vector store: %w", err)
	}

	// Create file source
	src := file.New(
		[]string{util.ExampleDataPath("file/llm.md")},
		file.WithName("LLM Docs"),
	)

	// Create knowledge base
	kb := knowledge.New(
		knowledge.WithVectorStore(vs),
		knowledge.WithEmbedder(openai.New()),
		knowledge.WithSources([]source.Source{src}),
	)

	fmt.Println("📥 Loading knowledge into Tencent VectorDB...")
	if err := kb.Load(ctx, knowledge.WithShowProgress(true)); err != nil {
		return fmt.Errorf("failed to load: %w", err)
	}

	// Create knowledge search tool
	searchTool := knowledgetool.NewKnowledgeSearchTool(kb)

	// Create agent
	agent := llmagent.New(
		"tcvector-httpurl-assistant",
		llmagent.WithModel(openaimodel.New(modelName)),
		llmagent.WithTools([]tool.Tool{searchTool}),
	)

	// Create runner
	r := runner.NewRunner(
		"tcvector-httpurl-chat",
		agent,
		runner.WithSessionService(sessioninmemory.NewSessionService()),
	)
	defer r.Close()

	// Test query
	fmt.Println("🔍 Querying knowledge from Tencent VectorDB...")
	eventChan, err := r.Run(ctx, "user", "session-httpurl",
		model.NewUserMessage("What are Large Language Models?"))
	if err != nil {
		return fmt.Errorf("run failed: %w", err)
	}

	fmt.Print("🤖 Response: ")
	for evt := range eventChan {
		util.PrintEventWithToolCalls(evt)
		if evt.IsFinalResponse() && len(evt.Response.Choices) > 0 {
			fmt.Println(evt.Response.Choices[0].Message.Content)
		}
	}

	return nil
}
