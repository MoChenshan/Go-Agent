package tool

import (
	"context"
	"fmt"
	"io"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Config represents the configuration for tool adapters.
type Config struct {
	Name        string
	Description string
	Timeout     int // in seconds, 0 means no timeout
}

// CallableTool adapts an eino InvokableTool to implement trpc-agent-go Tool interface.
type CallableTool struct {
	baseTool   einotool.BaseTool
	invokeTool einotool.InvokableTool
	config     *Config
}

// NewCallable creates a new CallableTool adapter for eino InvokableTool.
func NewCallable(baseTool einotool.BaseTool, invokeTool einotool.InvokableTool, config *Config) *CallableTool {
	return &CallableTool{
		baseTool:   baseTool,
		invokeTool: invokeTool,
		config:     config,
	}
}

// Name returns the name of the tool.
func (ct *CallableTool) Name() string {
	if ct.config.Name != "" {
		return ct.config.Name
	}
	ctx := context.Background()
	info, err := ct.baseTool.Info(ctx)
	if err != nil || info == nil {
		return "unknown"
	}
	return info.Name
}

// Description returns the description of the tool.
func (ct *CallableTool) Description() string {
	if ct.config.Description != "" {
		return ct.config.Description
	}
	ctx := context.Background()
	info, err := ct.baseTool.Info(ctx)
	if err != nil || info == nil {
		return "No description available"
	}
	return info.Desc
}

// Declaration returns the tool declaration with name, description and input schema.
func (ct *CallableTool) Declaration() *tool.Declaration {
	ctx := context.Background()
	info, err := ct.baseTool.Info(ctx)
	if err != nil || info == nil {
		// Return basic declaration if we can't get info
		return &tool.Declaration{
			Name:        ct.Name(),
			Description: ct.Description(),
		}
	}

	// Convert eino ToolInfo to trpc-agent-go Declaration
	declaration := &tool.Declaration{
		Name:        ct.Name(),
		Description: ct.Description(),
	}

	// Convert input schema if available
	if info.ParamsOneOf != nil {
		declaration.InputSchema = ConvertEinoParamsToTrpcSchema(info.ParamsOneOf)
	}

	return declaration
}

// Call invokes the tool with the provided JSON arguments.
func (ct *CallableTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	// Apply timeout if configured
	if ct.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ct.config.Timeout)*time.Second)
		defer cancel()
	}

	// Convert JSON args to string as expected by eino InvokableRun
	result, err := ct.invokeTool.InvokableRun(ctx, string(jsonArgs))
	if err != nil {
		return nil, fmt.Errorf("eino tool invocation failed: %w", err)
	}

	return result, nil
}

// StreamableTool adapts an eino StreamableTool to implement trpc-agent-go StreamableTool interface.
type StreamableTool struct {
	baseTool   einotool.BaseTool
	streamTool einotool.StreamableTool
	config     *Config
}

// NewStreamable creates a new StreamableTool adapter for eino StreamableTool.
func NewStreamable(baseTool einotool.BaseTool, streamTool einotool.StreamableTool, config *Config) *StreamableTool {
	return &StreamableTool{
		baseTool:   baseTool,
		streamTool: streamTool,
		config:     config,
	}
}

// Name returns the name of the tool.
func (st *StreamableTool) Name() string {
	if st.config.Name != "" {
		return st.config.Name
	}
	ctx := context.Background()
	info, err := st.baseTool.Info(ctx)
	if err != nil || info == nil {
		return "unknown"
	}
	return info.Name
}

// Description returns the description of the tool.
func (st *StreamableTool) Description() string {
	if st.config.Description != "" {
		return st.config.Description
	}
	ctx := context.Background()
	info, err := st.baseTool.Info(ctx)
	if err != nil || info == nil {
		return "No description available"
	}
	return info.Desc
}

// Declaration returns the tool declaration with name, description and input schema.
func (st *StreamableTool) Declaration() *tool.Declaration {
	ctx := context.Background()
	info, err := st.baseTool.Info(ctx)
	if err != nil || info == nil {
		return &tool.Declaration{
			Name:        st.Name(),
			Description: st.Description(),
		}
	}

	declaration := &tool.Declaration{
		Name:        st.Name(),
		Description: st.Description(),
	}

	if info.ParamsOneOf != nil {
		declaration.InputSchema = ConvertEinoParamsToTrpcSchema(info.ParamsOneOf)
	}

	return declaration
}

// Call invokes the tool with the provided JSON arguments.
func (st *StreamableTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	// Apply timeout if configured
	if st.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(st.config.Timeout)*time.Second)
		defer cancel()
	}

	// For streaming tools, we can either use stream or fallback to invoke
	if invokeTool, ok := st.streamTool.(einotool.InvokableTool); ok {
		result, err := invokeTool.InvokableRun(ctx, string(jsonArgs))
		if err != nil {
			return nil, fmt.Errorf("eino stream tool invocation failed: %w", err)
		}
		return result, nil
	}

	return nil, fmt.Errorf("eino stream tool does not support non-streaming invocation")
}

// StreamableCall invokes the tool with streaming support.
func (st *StreamableTool) StreamableCall(ctx context.Context, jsonArgs []byte) (*tool.StreamReader, error) {
	// Apply timeout if configured
	if st.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(st.config.Timeout)*time.Second)
		defer cancel()
	}

	// Use eino's streaming capability
	stream, err := st.streamTool.StreamableRun(ctx, string(jsonArgs))
	if err != nil {
		return nil, fmt.Errorf("eino stream tool streaming failed: %w", err)
	}

	// Create a new trpc-agent-go stream and bridge data from eino stream
	trpcStream := tool.NewStream(10) // Buffer size 10

	go func() {
		defer trpcStream.Writer.Close()
		defer stream.Close()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				data, err := stream.Recv()
				if err != nil {
					if err == io.EOF {
						return // End of stream
					}
					// Check if context is cancelled before sending error
					select {
					case <-ctx.Done():
						return // Context cancelled, no need to send error
					default:
						// Send error and close
						if closed := trpcStream.Writer.Send(tool.StreamChunk{}, err); closed {
							// Stream already closed, nothing more to do
							return
						}
						return
					}
				}

				chunk := tool.StreamChunk{
					Content: data,
				}

				// Check if context is cancelled before sending data
				select {
				case <-ctx.Done():
					return // Context cancelled, stop sending
				default:
					// Send to trpc stream
					if closed := trpcStream.Writer.Send(chunk, nil); closed {
						return
					}
				}
			}
		}
	}()

	return trpcStream.Reader, nil
}

// ReadOnlyTool is a fallback adapter for basic eino tools.
type ReadOnlyTool struct {
	baseTool einotool.BaseTool
	config   *Config
}

// NewReadOnly creates a new ReadOnlyTool adapter for basic eino tools.
func NewReadOnly(baseTool einotool.BaseTool, config *Config) *ReadOnlyTool {
	return &ReadOnlyTool{
		baseTool: baseTool,
		config:   config,
	}
}

// Name returns the name of the tool.
func (rt *ReadOnlyTool) Name() string {
	if rt.config.Name != "" {
		return rt.config.Name
	}
	ctx := context.Background()
	info, err := rt.baseTool.Info(ctx)
	if err != nil || info == nil {
		return "unknown"
	}
	return info.Name
}

// Description returns the description of the tool.
func (rt *ReadOnlyTool) Description() string {
	if rt.config.Description != "" {
		return rt.config.Description
	}
	ctx := context.Background()
	info, err := rt.baseTool.Info(ctx)
	if err != nil || info == nil {
		return "No description available"
	}
	return info.Desc
}

// Declaration returns the tool declaration with name, description and input schema.
func (rt *ReadOnlyTool) Declaration() *tool.Declaration {
	ctx := context.Background()
	info, err := rt.baseTool.Info(ctx)
	if err != nil || info == nil {
		return &tool.Declaration{
			Name:        rt.Name(),
			Description: rt.Description(),
		}
	}

	declaration := &tool.Declaration{
		Name:        rt.Name(),
		Description: rt.Description(),
	}

	if info.ParamsOneOf != nil {
		declaration.InputSchema = ConvertEinoParamsToTrpcSchema(info.ParamsOneOf)
	}

	return declaration
}

// Call returns an error as read-only tools do not support invocation.
func (rt *ReadOnlyTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return nil, fmt.Errorf("read-only eino tool does not support direct invocation")
}
