package vedas

import (
	"context"
	"io"

	ihttp "git.woa.com/trpc-go/trpc-agent-go/trpc/internal/http"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/internal/vedas"
	"trpc.group/trpc-go/trpc-agent-go/agent"
)

// AgentBuilder is the builder of Agent.
type AgentBuilder struct {
	Agent      agent.Agent
	token      string
	appGroupID int // app group id
	client     *vedas.Client
}

// NewBuilder creates a new vedas agent builder.
// vedas agent token & user appGroupID are required.
func New(token string, appGroupID int) *AgentBuilder {
	return &AgentBuilder{
		token:      token,
		appGroupID: appGroupID,
		client: vedas.NewClient(
			vedas.WithToken(token),
			vedas.WithAppGroupID(appGroupID),
			vedas.WithHTTPClient(ihttp.NewRequestHandler("")),
		),
	}
}

// Build build a new vedas agent
func (m *AgentBuilder) Build(opts ...Option) (*Agent, error) {
	agent := &Agent{
		channelBufSize: defaultChannelBufSize,
	}
	for _, o := range opts {
		o(agent)
	}
	// set required options
	clientOpts := []vedas.Option{
		vedas.WithAppGroupID(m.appGroupID),
		vedas.WithToken(m.token),
		vedas.WithHTTPClient(ihttp.NewRequestHandler(agent.name)),
	}
	if agent.maxEventSize > 0 {
		clientOpts = append(clientOpts,
			vedas.WithMaxEventSize(agent.maxEventSize))
	}
	agent.client = vedas.NewClient(clientOpts...)
	if err := agent.validate(); err != nil {
		return nil, err
	}
	m.Agent = agent
	return agent, nil
}

// CreateFile creates a new vedas attachment
func (m *AgentBuilder) CreateFile(
	ctx context.Context,
	name string,
	size int64,
) (fileID string, UploadURL string, err error) {
	resp, err := m.client.CreateAttachment(ctx, &vedas.AttachmentPresignRequest{
		AppGroupID: m.appGroupID,
		FileName:   name,
		FileSize:   size,
	})
	if err != nil {
		return "", "", err
	}
	return resp.FileID, resp.UploadURL, nil
}

// CompleteFile completes a vedas attachment upload
func (m *AgentBuilder) CompleteFile(ctx context.Context, fileID string) error {
	_, err := m.client.CompleteAttachment(ctx, &vedas.AttachmentUpdateRequest{
		FileID: fileID,
	})
	if err != nil {
		return err
	}
	return nil
}

// FileList lists vedas files grouped by planID
// process: true for process files, false for result files
func (m *AgentBuilder) FileList(
	ctx context.Context,
	planID string,
	process bool,
) (vedas.FileListResponse, error) {
	cate := vedas.ResultFile
	if process {
		cate = vedas.ProcessFile
	}
	resp, err := m.client.FileList(ctx, &vedas.FileListRequest{
		PlanID:   planID,
		Category: cate,
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Download downloads a vedas file
func (m *AgentBuilder) Download(ctx context.Context, fileID, planID string) (io.ReadCloser, error) {
	return m.client.DownloadFile(ctx, &vedas.DownloadFileRequest{
		FileID: fileID,
		PlanID: planID,
	})
}
