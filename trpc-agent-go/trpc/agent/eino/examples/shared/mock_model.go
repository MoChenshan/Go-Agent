// Package shared provides common mock components for all examples.
package shared

import (
	"context"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// MockModel is a simple mock model for demonstration.
type MockModel struct {
	name string
}

// NewMockModel creates a new MockModel instance.
func NewMockModel(name string) *MockModel {
	return &MockModel{name: name}
}

// Name returns the name of the model.
func (m *MockModel) Name() string {
	return m.name
}

// Generate generates a response for the given request.
func (m *MockModel) Generate(ctx context.Context, req *model.Request) (*model.Response, error) {
	// Simple mock response based on input
	userMessage := "Hello"
	if len(req.Messages) > 0 {
		userMessage = req.Messages[len(req.Messages)-1].Content
	}

	content := fmt.Sprintf("Mock response to: %s", userMessage)

	return &model.Response{
		Choices: []model.Choice{{
			Message: model.Message{
				Role:    model.RoleAssistant,
				Content: content,
			},
		}},
		Done: true,
	}, nil
}

// GenerateStream generates a streaming response for the given request.
func (m *MockModel) GenerateStream(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 10)

	go func() {
		defer close(ch)

		userMessage := "Hello"
		if len(req.Messages) > 0 {
			userMessage = req.Messages[len(req.Messages)-1].Content
		}

		content := fmt.Sprintf("Mock streaming response to: %s", userMessage)
		words := strings.Split(content, " ")

		for _, word := range words {
			select {
			case <-ctx.Done():
				return
			case ch <- &model.Response{
				Choices: []model.Choice{{
					Delta: model.Message{
						Role:    model.RoleAssistant,
						Content: word + " ",
					},
				}},
			}:
			}
		}

		// Send final Done response to signal completion
		select {
		case <-ctx.Done():
			return
		case ch <- &model.Response{
			Choices: []model.Choice{{
				Message: model.Message{
					Role:    model.RoleAssistant,
					Content: content,
				},
			}},
			Done: true,
		}:
		}
	}()

	return ch, nil
}

// GenerateContent generates content using either stream or batch mode.
func (m *MockModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	// Unified implementation that works with both Generate and GenerateStream patterns
	return m.GenerateStream(ctx, req)
}

// Info returns information about the model.
func (m *MockModel) Info() model.Info {
	return model.Info{
		Name: m.name,
	}
}

// EinoMockModel is a mock model that implements Eino's model interface.
type EinoMockModel struct{}

// Generate generates a response for the given request.
func (m *EinoMockModel) Generate(ctx context.Context, request any) (any, error) {
	return map[string]any{
		"role":    "assistant",
		"content": "Mock Eino model response",
	}, nil
}
