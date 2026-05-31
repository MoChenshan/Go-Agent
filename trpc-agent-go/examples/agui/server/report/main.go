//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

// Package main is the main package for the AG-UI server.
package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

var (
	modelName = flag.String("model", "deepseek-chat", "Model to use")
	isStream  = flag.Bool("stream", true, "Whether to stream the response")
	address   = flag.String("address", "127.0.0.1:8080", "Listen address")
	path      = flag.String("path", "/agui", "HTTP path")
)

const reportInstruction = `You are reportAgent, responsible for drafting structured business reports.
Workflow:
1. Before any tool calls, send a short assistant sentence explaining that you are preparing a document.
2. Then call open_report_document and pick the title from the latest user request.
3. After the open tool call succeeds, write the full report as Assistant text. Keep it concise but actionable.
4. Call close_report_document once the report is streamed.
5. After closing, send one final assistant line summarizing the takeaway and noting the doc is done.
Only use English in tool inputs; the visible report can mirror the user's language.`

func main() {
	flag.Parse()
	agent := newAgent()
	sessionService := inmemory.NewSessionService()
	runner := runner.NewRunner(agent.Info().Name, agent, runner.WithSessionService(sessionService))
	defer runner.Close()
	log.Infof("AG-UI: serving agent %q on http://%s%s", agent.Info().Name, *address, *path)

	// Create tRPC-Go server.
	server := trpc.NewServer()
	// Create AG-UI server.
	aguiServer, err := agui.New(
		runner,
		agui.WithPath(*path),
		agui.WithAppName(agent.Info().Name),
		agui.WithSessionService(sessionService),
		agui.WithMessagesSnapshotEnabled(true),
	)
	if err != nil {
		log.Fatalf("failed to create AG-UI server: %v", err)
	}
	// Register the AG-UI server into the tRPC-Go server.
	if err := tagui.RegisterAGUIServer(server, "trpc.test.report.agui", aguiServer); err != nil {
		log.Fatalf("failed to register AG-UI server: %v", err)
	}
	// Start the tRPC-Go server.
	if err := server.Serve(); err != nil {
		log.Fatalf("server stopped with error: %v", err)
	}
}

func newAgent() agent.Agent {
	modelInstance := openai.New(*modelName)
	generationConfig := model.GenerationConfig{
		MaxTokens:   intPtr(800),
		Temperature: floatPtr(0.4),
		Stream:      *isStream,
	}

	openTool := function.NewFunctionTool(
		openReportDocument,
		function.WithName("open_report_document"),
		function.WithDescription("Open a document box in the AG-UI frontend before emitting the textual report."),
	)
	closeTool := function.NewFunctionTool(
		closeReportDocument,
		function.WithName("close_report_document"),
		function.WithDescription("Close the active AG-UI document box after the report is delivered."),
	)

	return llmagent.New(
		"report-agent",
		llmagent.WithTools([]tool.Tool{openTool, closeTool}),
		llmagent.WithModel(modelInstance),
		llmagent.WithGenerationConfig(generationConfig),
		llmagent.WithInstruction(reportInstruction),
	)
}

func intPtr(i int) *int { return &i }

func floatPtr(f float64) *float64 { return &f }

type openReportArgs struct {
	Title string `json:"title" description:"Document box title"`
}

type openReportResult struct {
	Title      string `json:"title"`
	DocumentID string `json:"documentId"`
	CreatedAt  string `json:"createdAt"`
}

type closeReportArgs struct {
	Reason string `json:"reason" description:"Why the document is being closed"`
}

type closeReportResult struct {
	Closed   bool   `json:"closed"`
	Message  string `json:"message"`
	ClosedAt string `json:"closedAt"`
}

func openReportDocument(ctx context.Context, args openReportArgs) (openReportResult, error) {
	_ = ctx
	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = "Auto generated report"
	}
	return openReportResult{
		Title:      title,
		DocumentID: uuid.NewString(),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func closeReportDocument(ctx context.Context, args closeReportArgs) (closeReportResult, error) {
	_ = ctx
	reason := strings.TrimSpace(args.Reason)
	if reason == "" {
		reason = "report_completed"
	}
	msg := fmt.Sprintf("document box closed: %s", reason)
	return closeReportResult{
		Closed:   true,
		Message:  msg,
		ClosedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}
