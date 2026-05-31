package tmemory

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/memory"
	memorytool "trpc.group/trpc-go/trpc-agent-go/memory/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func buildReadOnlyTools(s *Service) []tool.Tool {
	return []tool.Tool{newSearchTool(s)}
}

func newSearchTool(s *Service) tool.Tool {
	searchFunc := func(ctx context.Context, req *memorytool.SearchMemoryRequest) (*memorytool.SearchMemoryResponse, error) {
		if req == nil || strings.TrimSpace(req.Query) == "" {
			return &memorytool.SearchMemoryResponse{Query: "", Results: []memorytool.Result{}, Count: 0}, nil
		}
		appName, userID, err := memorytool.GetAppAndUserFromContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("tmemory search tool: %w", err)
		}

		bizID := s.opts.bizID
		if bizID == "" {
			bizID = appName
		}

		// Use user-scoped recall by default so the agent can benefit from
		// memories created in earlier sessions as well.
		resp, err := s.recall(ctx, bizID, userID, "", req.Query)
		if err != nil {
			return nil, fmt.Errorf("tmemory search tool: %w", err)
		}

		results := flattenRecallResults(resp)
		return &memorytool.SearchMemoryResponse{
			Query:   req.Query,
			Results: results,
			Count:   len(results),
		}, nil
	}
	return function.NewFunctionTool(
		searchFunc,
		function.WithName(memory.SearchToolName),
		function.WithDescription("Search for relevant memories stored in tMemory for the current user."),
	)
}

// recall performs a memory recall query against tMemory. It is unexported:
// external callers should go through the memory_search tool exposed via
// Service.Tools, which is the supported integration path for agents.
func (s *Service) recall(ctx context.Context, bizID, userID, sessionID, query string) (*recallResult, error) {
	req := recallRequest{
		BizID:      bizID,
		UserID:     userID,
		SessionID:  sessionID,
		StrategyID: s.opts.strategyID,
		Query:      query,
		Config:     s.opts.recallConfig,
	}
	var resp recallResponse
	// Recall is read-only and inherently idempotent: retry transient
	// failures (429/5xx/network) on the POST.
	if err := s.c.doJSONIdempotent(ctx, http.MethodPost, "/v1/memories/recall", req, &resp); err != nil {
		return nil, err
	}
	if resp.Code != 0 {
		return nil, fmt.Errorf("tmemory: recall failed: code=%d message=%s", resp.Code, resp.Message)
	}
	return &resp.Data, nil
}

func flattenRecallResults(data *recallResult) []memorytool.Result {
	if data == nil {
		return nil
	}
	var results []memorytool.Result
	now := time.Now()

	for memName, items := range data.RetrievedMemories {
		for _, item := range items {
			if strings.TrimSpace(item.Content) == "" {
				continue
			}
			r := memorytool.Result{
				ID:      item.ID,
				Memory:  item.Content,
				Kind:    memName,
				Created: now,
			}
			if item.Score != nil {
				r.Score = *item.Score
			}
			results = append(results, r)
		}
	}
	return results
}
