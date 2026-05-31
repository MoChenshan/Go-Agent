package shared

import (
	"context"
	"io"
	"log"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/schema"
)

// SimpleEinoCallback demonstrates a basic Eino callback implementation.
type SimpleEinoCallback struct {
	name string
}

// NewSimpleEinoCallback creates a new SimpleEinoCallback instance.
func NewSimpleEinoCallback(name string) *SimpleEinoCallback {
	return &SimpleEinoCallback{name: name}
}

// OnStart handles the start event of a callback.
func (c *SimpleEinoCallback) OnStart(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
	log.Printf("[%s] 🚀 OnStart: %s (type: %s)", c.name, info.Name, info.Type)
	return ctx
}

// OnEnd handles the end event of a callback.
func (c *SimpleEinoCallback) OnEnd(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
	log.Printf("[%s] ✅ OnEnd: %s", c.name, info.Name)
	return ctx
}

// OnError handles the error event of a callback.
func (c *SimpleEinoCallback) OnError(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
	log.Printf("[%s] ❌ OnError: %s - %v", c.name, info.Name, err)
	return ctx
}

// OnStartWithStreamInput handles the start event with streaming input.
func (c *SimpleEinoCallback) OnStartWithStreamInput(ctx context.Context, info *callbacks.RunInfo, input *schema.StreamReader[callbacks.CallbackInput]) context.Context {
	input.Close() // Simple implementation
	return ctx
}

// OnEndWithStreamOutput handles the end event with streaming output.
func (c *SimpleEinoCallback) OnEndWithStreamOutput(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
	// Simple stream processing for demonstration
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[%s] ⚠️ Stream processing recovered from: %v", c.name, r)
			}
		}()
		chunkCount := 0
		for {
			chunk, err := output.Recv()
			if err == io.EOF {
				log.Printf("[%s] 📦 Stream ended for %s, processed %d chunks", c.name, info.Name, chunkCount)
				break
			}
			if err != nil {
				log.Printf("[%s] ❌ Stream error: %v", c.name, err)
				break
			}
			chunkCount++
			// Process the chunk
			_ = chunk
			log.Printf("[%s] 📦 Stream chunk %d from %s", c.name, chunkCount, info.Name)
		}
	}()
	return ctx
}

// ChatBufferCallback simulates a more complex streaming callback like ChatBuffer.
type ChatBufferCallback struct {
	name   string
	buffer []string
}

// NewChatBufferCallback creates a new ChatBufferCallback instance.
func NewChatBufferCallback(name string) *ChatBufferCallback {
	return &ChatBufferCallback{
		name:   name,
		buffer: make([]string, 0),
	}
}

// OnStart handles the start event of a callback.
func (c *ChatBufferCallback) OnStart(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
	return ctx // No-op for this demo
}

// OnEnd handles the end event of a callback.
func (c *ChatBufferCallback) OnEnd(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
	return ctx // No-op for this demo
}

// OnError handles the error event of a callback.
func (c *ChatBufferCallback) OnError(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
	return ctx // No-op for this demo
}

// OnStartWithStreamInput handles the start event with streaming input.
func (c *ChatBufferCallback) OnStartWithStreamInput(ctx context.Context, info *callbacks.RunInfo, input *schema.StreamReader[callbacks.CallbackInput]) context.Context {
	input.Close()
	return ctx
}

// OnEndWithStreamOutput handles the end event with streaming output.
func (c *ChatBufferCallback) OnEndWithStreamOutput(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[callbacks.CallbackOutput]) context.Context {
	// Only process specific nodes (e.g., chat model)
	if info.Name != "chat_model" && info.Type != "model" {
		return ctx
	}

	log.Printf("[%s] 💬 Processing streaming output from %s", c.name, info.Name)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[%s] ⚠️ Stream processing recovered from: %v", c.name, r)
			}
		}()
		buffer := ""

		for {
			chunk, err := output.Recv()
			if err == io.EOF {
				if buffer != "" {
					c.sendBufferedContent(buffer)
				}
				log.Printf("[%s] 📝 Chat buffer processing complete", c.name)
				break
			}
			if err != nil {
				log.Printf("[%s] ❌ Stream error: %v", c.name, err)
				break
			}

			// Simulate intelligent buffering logic
			if chunkMap, ok := chunk.(map[string]any); ok {
				if content, exists := chunkMap["content"]; exists {
					if contentStr, ok := content.(string); ok {
						buffer += contentStr

						// Send buffer when we hit certain conditions (demo logic)
						if len(buffer) > 20 || buffer[len(buffer)-1] == '.' {
							c.sendBufferedContent(buffer)
							buffer = ""
						}
					}
				}
			}
		}
	}()

	return ctx
}

func (c *ChatBufferCallback) sendBufferedContent(content string) {
	c.buffer = append(c.buffer, content)
	log.Printf("[%s] 📤 Buffered content: %s", c.name, content)

	// Here you could send to external systems, websockets, etc.
	// For demo, we just log it
}
