package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/client"
	"git.code.oa.com/trpc-go/trpc-go/codec"
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/server/taijia2a/config"
	"github.com/r3labs/sse/v2"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
)

// TaijiProxy taiji proxy impl.
type TaijiProxy struct {
	cfg *config.ProxyConfig

	taskID      string
	subscriber  taskmanager.TaskSubscriber
	taskHandler taskmanager.TaskHandler
}

// New taiji proxy.
func New(cfg *config.ProxyConfig) *TaijiProxy {
	return &TaijiProxy{cfg: cfg}
}

var httpCli = thttp.NewClientProxy("hunyuan_openapi")

// ProcessMessage processes the message.
func (p *TaijiProxy) ProcessMessage(
	ctx context.Context,
	message protocol.Message,
	options taskmanager.ProcessOptions,
	taskHandler taskmanager.TaskHandler,
) (*taskmanager.MessageProcessingResult, error) {
	p.taskHandler = taskHandler

	// Create a task for streaming
	var err error
	p.taskID, err = taskHandler.BuildTask(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	// Subscribe to the task for streaming
	p.subscriber, err = taskHandler.SubscribeTask(&p.taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to task: %w", err)
	}

	qb, err := p.newAppCreateRequestByte(ctx, &message)
	if err != nil {
		return nil, fmt.Errorf("newAppCreateRequestByte failed, err: %v", err)
	}

	_ = trpc.Go(ctx, time.Minute, func(ctx context.Context) {
		defer func() {
			p.subscriber.Close()
			_ = taskHandler.CleanTask(&p.taskID)
		}()

		p.sendWorkingStatus(p.taskID, p.taskHandler)

		req := &codec.Body{Data: qb}
		rsp := &codec.Body{}
		opts := buildDefaultOptions(
			client.WithTarget(p.cfg.RemoteTarget),
			client.WithReqHead(p.newReqHead()),
			client.WithRspHead(p.newClientRspHeader()),
		)
		if err = httpCli.Post(ctx, p.cfg.Path, req, rsp, opts...); err != nil {
			log.ErrorContextf(ctx, "HTTP POST failed to path %s: %v", p.cfg.Path, err) // 添加错误日志记录
			return
		}

		p.sendCompletionStatus(p.taskID, p.taskHandler)
	})

	return &taskmanager.MessageProcessingResult{StreamingEvents: p.subscriber}, nil
}

// sendWorkingStatus sends the working status for streaming
func (p *TaijiProxy) sendWorkingStatus(taskID string, taskHandler taskmanager.TaskHandler) {
	workingMsg := protocol.NewMessage(protocol.MessageRoleAgent, []protocol.Part{})
	if err := taskHandler.UpdateTaskState(&taskID, protocol.TaskStateWorking, &workingMsg); err != nil {
		log.Infof("Error updating task state: %v", err)
		return
	}
}

// sendCompletionStatus sends the completion status for streaming
func (p *TaijiProxy) sendCompletionStatus(taskID string, taskHandler taskmanager.TaskHandler) {
	finalMsg := protocol.NewMessage(protocol.MessageRoleAgent, []protocol.Part{})
	if err := taskHandler.UpdateTaskState(&taskID, protocol.TaskStateCompleted, &finalMsg); err != nil {
		log.Infof("Error updating final task state: %v", err)
		return
	}
}

func (p *TaijiProxy) sseHandlerToArtifact(e *sse.Event) error {
	var r AppCreateResponse
	if err := json.Unmarshal(e.Data, &r); err != nil {
		return fmt.Errorf("sse unmarshal err: %v", err)
	}

	artifact := protocol.Artifact{
		Name:  stringPtr("hunyuan agent result"),
		Parts: make([]protocol.Part, 0, len(r.Choices)),
	}

	for i := 0; i < len(r.Choices); i++ {
		choice := r.Choices[i]
		artifact.Parts = append(artifact.Parts, protocol.NewTextPart(choice.Delta.Content))
	}

	if err := p.taskHandler.AddArtifact(&p.taskID, artifact, true, false); err != nil {
		log.Infof("Error adding artifact: %v", err)
		return fmt.Errorf("error adding artifact: %v", err)
	}

	return nil
}

func (p *TaijiProxy) newReqHead() *thttp.ClientReqHeader {
	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", p.cfg.Authorization))
	header.Set("Accept", "text/event-stream") // Indicate that we want to receive SSE.
	header.Set("Cache-Control", "no-cache")
	header.Set(thttp.Connection, "keep-alive")
	header.Set("Content-Type", "application/json")
	return &thttp.ClientReqHeader{
		Method: http.MethodPost,
		Header: header,
	}
}

func (p *TaijiProxy) newClientRspHeader() *thttp.ClientRspHeader {
	return &thttp.ClientRspHeader{
		// Set ManualReadBody to false in order to handle the stream response automatically.
		ManualReadBody: false, // Default is false.
		// Register SSEHandler to the callback in order to handle the stream response
		SSEHandler: &sseHandler{p.sseHandlerToArtifact},
	}
}

type sseHandler struct {
	fn func(e *sse.Event) error
}

// Handle handles the given SSE event.
func (h *sseHandler) Handle(e *sse.Event) error { return h.fn(e) }

func (p *TaijiProxy) newAppCreateRequestByte(ctx context.Context, message *protocol.Message) ([]byte, error) {
	var (
		query    string
		messages []Message
	)

	for _, part := range message.Parts {
		if textPart, ok := part.(*protocol.TextPart); ok {
			if len(query) == 0 {
				query = textPart.Text
			}
			messages = append(messages, Message{Role: "user", Content: textPart.Text})
			break
		}
	}
	// Construct a request.
	q := AppCreateRequest{
		Query:          query,
		ForwardService: fmt.Sprintf("hyaide-application-%s", p.cfg.AgentID),
		QueryID:        message.MessageID,
		Stream:         true,
		Messages:       messages,
	}
	qb, err := json.Marshal(q)
	if err != nil {
		log.ErrorContextf(ctx, "marshal query: %q\n", qb)
		return nil, fmt.Errorf("marshal err: %v", err)
	}
	return qb, nil
}

func buildDefaultOptions(opts ...client.Option) []client.Option {
	result := []client.Option{
		client.WithNetwork("tcp"),
		client.WithProtocol("http"),
		client.WithCurrentSerializationType(codec.SerializationTypeNoop),
		client.WithSerializationType(codec.SerializationTypeNoop),
		client.WithCurrentCompressType(codec.CompressTypeNoop),
		client.WithTimeout(-1),
	}
	return append(result, opts...)
}

// stringPtr returns a pointer to the given string
func stringPtr(s string) *string {
	return &s
}
