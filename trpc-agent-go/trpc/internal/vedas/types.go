package vedas

import (
	"fmt"
)

// API endpoints
const (
	// PlanCreateEndpoint is the endpoint for creating a Vedas task.
	PlanCreateEndpoint = "/vedas/v1/plan/create"
	// PlanQueryEndpoint is the endpoint for querying task status.
	PlanQueryEndpoint = "/vedas/v1/plan/query"
	// PlanFilesEndpoint is the endpoint for getting task files.
	PlanFilesEndpoint = "/vedas/v1/plan/files"
	// PlanFilesDownloadEndpoint is the endpoint for downloading files.
	PlanFilesDownloadEndpoint = "/vedas/v1/plan/files/download"
	// PlanTerminateEndpoint is the endpoint for terminating a task.
	PlanTerminateEndpoint = "/vedas/v1/plan/terminate"
	// ProjectsUpdateEndpoint is the endpoint for updating project name.
	ProjectsUpdateEndpoint = "/vedas/v1/projects/update"
	// AttachmentsPresignEndpoint is the endpoint for getting upload presigned URL.
	AttachmentsPresignEndpoint = "/vedas/attachments/presign"
	// AttachmentsStatusEndpoint is the endpoint for updating attachment status.
	AttachmentsStatusEndpoint = "/vedas/attachments/status"
)

// successCode is the code for a successful response.
const (
	successCode = 0
)

// CommonResponse is the common response struct.
type CommonResponse[T any] struct {
	TraceID string `json:"trace_id,omitempty"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data,omitempty"`
}

// Error returns an error based on the response code.
func (r CommonResponse[T]) Error() error {
	if r.Code == successCode {
		return nil
	}
	return fmt.Errorf("request failed with code %d: %s", r.Code, r.Message)
}

// CreatePlanRequest is the request struct for creating a Vedas task.
type CreatePlanRequest struct {
	// Prompt is the user's question content.
	Prompt string `json:"prompt"`
	// AppGroupID is the user's application group.
	AppGroupID int `json:"app_group_id"`
	// McpInstances MCP instance list, required field, can be empty array.
	McpInstances []string `json:"mcp_instances"`
	// ExtraParams parameter configuration.
	ExtraParams *ExtraParams `json:"extra_params"`
	// ProjectID identifies a whole conversation. If not passed, it starts a new conversation; if passed, it's a continuous conversation.
	ProjectID string `json:"project_id,omitempty"`
	// AttachmentIDs attachment list, obtained through file upload interface.
	AttachmentIDs []string `json:"attachment_ids,omitempty"`
	// ProjectName identifies the name of the entire conversation.
	ProjectName string `json:"project_name,omitempty"`
}

// PlanMode is the model mode for this conversation.
type PlanMode string

const (
	PlanModeSmart PlanMode = "smart" // smart, external API
	PlanModeSafe  PlanMode = "safe"  // safe, internal private deployment model with no data security issues
)

// TriggerType is the trigger type for this conversation.
type TriggerType string

const (
	TriggerTypeAPI TriggerType = "api" // external API
	TriggerTypeWeb TriggerType = "web" // web request
)

// ForceIntention is the force intention for this conversation.
type ForceIntention string

const (
	// ForceIntentionAuto means "do not force intention": when empty / not passed, the agent will judge the task intention automatically.
	ForceIntentionAuto   ForceIntention = ""                     // auto, let agent decide
	ForceIntentionMulti  ForceIntention = "multi_stage_research" // multi_stage_research, multi-stage conversation
	ForceIntentionSingle ForceIntention = "one_stage_task"       // one_stage_task, one-stage conversation
)

// ExtraParams is the extra parameter configuration.
type ExtraParams struct {
	// Mode is the model mode for this conversation. smart, external API; safe, internal private deployment model with no data security issues
	Mode PlanMode `json:"mode"`
	// TriggerType is the identifier for the conversation source. api, external API call, can avoid interaction behavior; web, vedas web request
	TriggerType TriggerType `json:"trigger_type,omitempty"`
	// ForceIntention is the field that identifies the task intent when not passed, the agent will judge it automatically; when passed, it identifies the intent recognition type for this conversation
	ForceIntention ForceIntention `json:"force_intention,omitempty"`
}

// CreatePlanResponse is the response struct for creating a Vedas task.
type CreatePlanResponse struct {
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
	TaskID    string `json:"task_id"`
	SSEURL    string `json:"sse_url"`
	APISSEURL string `json:"api_sse_url"`
	Prompt    string `json:"prompt"`
}

// PlanQueryRequest represents data for querying task status.
type PlanQueryRequest struct {
	PlanID string `json:"plan_id"`
}

// PlanStatus represents task status.
type PlanStatus string

const (
	// PlanStatusInit represents a created task
	PlanStatusInit PlanStatus = "created"
	// PlanStatusRunning represents a running task
	PlanStatusRunning PlanStatus = "in_progress"
	// PlanStatusCompleted represents a successful task
	PlanStatusCompleted PlanStatus = "completed"
	// PlanStatusTerminated represents a terminated task
	PlanStatusTerminated PlanStatus = "terminated"
	// PlanStatusFailed represents a failed task
	PlanStatusFailed PlanStatus = "failed"
)

// PlanQueryData represents data for querying task status.
type PlanQueryResponse struct {
	PlanID     string     `json:"plan_id"`      // PlanID is the task ID
	ProjectID  string     `json:"project_id"`   // ProjectID is the project ID
	Status     PlanStatus `json:"status"`       // Status is the current task status
	AppGroupID string     `json:"app_group_id"` // AppGroupID is the application group ID
	Prompt     string     `json:"prompt"`       // Prompt is the user's question content
	SSEURL     string     `json:"sse_url"`      // SSEURL is the URL for streaming task logs
}

// AttachmentPresignRequest represents data for querying attachment presigned URL.
type AttachmentPresignRequest struct {
	AppGroupID int    `json:"app_group_id"`
	FileName   string `json:"file_name"`
	FileSize   int64  `json:"file_size"`
}

// AttachmentPresignRequest represents data for querying attachment presigned URL.
type AttachmentPresignResponse struct {
	FileID    string `json:"file_id"`
	UploadURL string `json:"upload_url"`
}

// AttachmentUpdateRequest represents data for updating attachment.
type AttachmentUpdateRequest struct {
	FileID       string `json:"file_id"`
	UploadStatus int    `json:"is_uploaded"`
}

// AttachmentUpdateResponse represents data for updating attachment.
// No need to care about the return parameters
// refe: https://iwiki.woa.com/p/4014325318
type AttachmentUpdateResponse struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	AppGroupID  int    `json:"app_group_id"`
	FileID      string `json:"file_id"`
	DownloadURL string `json:"download_url"`
	FileName    string `json:"file_name"`
	FileCosPath string `json:"file_cos_path"`
}

// DownloadFileRequest represents data for downloading file.
type DownloadFileRequest struct {
	PlanID string `json:"plan_id"`
	FileID string `json:"file_id"`
}

// FileCategory represents file category.
type FileCategory string

const (
	ResultFile  FileCategory = "result"  // final result file.
	ProcessFile FileCategory = "process" // process file.
)

// FileListRequest represents data for listing files.
type FileListRequest struct {
	PlanID   string       `json:"plan_id"`
	Category FileCategory `json:"category"`
}

// FileInfo represents file info.
type FileInfo struct {
	FileID      string `json:"file_id"`
	FileName    string `json:"file_name"`
	FilePath    string `json:"file_path"`
	Category    string `json:"category"`
	DownloadURL string `json:"download_url,omitempty"`
}

// FileListResponse represents data for listing files.
type FileListResponse []FileInfo
