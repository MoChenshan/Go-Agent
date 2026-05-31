package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwclient"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwproto"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

const (
	adminWebChatPagePath     = "/chat"
	adminWebChatSessionsPath = "/api/admin-chat/sessions"
	adminWebChatHistoryPath  = "/api/admin-chat/history"
	adminWebChatSendPath     = "/api/admin-chat/send"
	adminWebChatStreamPath   = "/api/admin-chat/stream"
	adminWebChatCancelPath   = "/api/admin-chat/cancel"

	adminWebChatChannel       = "admin"
	adminWebChatDefaultUserID = "admin"
	adminWebChatSessionPrefix = "admin-chat-"
	adminWebChatMessagePrefix = "admin-msg-"

	adminWebChatContentText       = "text"
	adminWebChatContentToolCall   = "tool_call"
	adminWebChatContentToolResult = "tool_result"
	adminWebChatContentProgress   = "progress"

	adminWebChatRoleUser      = "user"
	adminWebChatRoleAssistant = "assistant"
	adminWebChatRoleSystem    = "system"
	adminWebChatRoleTool      = "tool"

	adminWebChatEventStarted = "run.started"
	adminWebChatEventError   = "run.error"

	adminWebChatEnvUser     = "USER"
	adminWebChatEnvUsername = "USERNAME"

	adminWebChatRequestBodyLimit = 1 << 20
	adminWebChatSessionListLimit = 30
	adminWebChatSessionTitleMax  = 12
	adminWebChatHistoryEventMax  = 120
	adminWebChatTextLimit        = 120000
	adminWebChatToolOutputLimit  = 40000
	adminWebChatPreviewLimit     = 80

	adminWebChatTruncatedMarker = "\n[truncated]"
)

var adminWebChatPageTemplate = template.Must(
	template.New("admin-web-chat").Parse(adminWebChatPageHTML),
)

type adminWebChatGateway interface {
	SendMessage(
		ctx context.Context,
		req gwclient.MessageRequest,
	) (gwclient.MessageResponse, error)

	Cancel(ctx context.Context, requestID string) (bool, error)
}

type adminWebChatStreamingGateway interface {
	adminWebChatGateway

	StreamMessage(
		ctx context.Context,
		req gwclient.MessageRequest,
	) (<-chan gwclient.StreamEvent, error)
}

type adminWebChatService struct {
	appName    string
	gateway    adminWebChatGateway
	sessionSvc session.Service
	defaultUID string
}

type adminWebChatPageData struct {
	DefaultUserID string

	ChatLink     string
	SessionsAPI  string
	HistoryAPI   string
	SendAPI      string
	StreamAPI    string
	CancelAPI    string
	ChannelsLink string
}

type adminWebChatSendRequest struct {
	SessionID string `json:"session_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Message   string `json:"message,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

type adminWebChatCancelRequest struct {
	RequestID string `json:"request_id,omitempty"`
}

type adminWebChatSessionsResponse struct {
	UserID   string                    `json:"user_id"`
	Sessions []adminWebChatSessionView `json:"sessions"`
}

type adminWebChatSessionView struct {
	ID        string `json:"id"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type adminWebChatHistoryResponse struct {
	SessionID string                `json:"session_id"`
	UserID    string                `json:"user_id"`
	Messages  []adminWebChatMessage `json:"messages"`
}

type adminWebChatSendResponse struct {
	SessionID string              `json:"session_id"`
	RequestID string              `json:"request_id,omitempty"`
	Message   adminWebChatMessage `json:"message"`
	Usage     *gwclient.Usage     `json:"usage,omitempty"`
	Ignored   bool                `json:"ignored,omitempty"`
}

type adminWebChatStreamEvent struct {
	Type      string               `json:"type"`
	SessionID string               `json:"session_id,omitempty"`
	RequestID string               `json:"request_id,omitempty"`
	Delta     string               `json:"delta,omitempty"`
	Reply     string               `json:"reply,omitempty"`
	Stage     string               `json:"stage,omitempty"`
	Summary   string               `json:"summary,omitempty"`
	ElapsedMS int64                `json:"elapsed_ms,omitempty"`
	Message   *adminWebChatMessage `json:"message,omitempty"`
	Usage     *gwclient.Usage      `json:"usage,omitempty"`
	Ignored   bool                 `json:"ignored,omitempty"`
	Error     string               `json:"error,omitempty"`
}

type adminWebChatMessage struct {
	ID        string                `json:"id,omitempty"`
	Role      string                `json:"role"`
	Content   []adminWebChatContent `json:"content"`
	Timestamp string                `json:"timestamp,omitempty"`
	RequestID string                `json:"request_id,omitempty"`
}

type adminWebChatContent struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Arguments  string `json:"arguments,omitempty"`
	Output     string `json:"output,omitempty"`
	Stage      string `json:"stage,omitempty"`
	ElapsedMS  int64  `json:"elapsed_ms,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

func newAdminWebChatService(
	appName string,
	gateway adminWebChatGateway,
	sessionSvc session.Service,
) *adminWebChatService {
	if gateway == nil {
		return nil
	}
	return &adminWebChatService{
		appName:    strings.TrimSpace(appName),
		gateway:    gateway,
		sessionSvc: sessionSvc,
		defaultUID: defaultAdminWebChatUserID(),
	}
}

func wrapAdminWebChatHandler(
	base http.Handler,
	service *adminWebChatService,
) http.Handler {
	if service == nil {
		return base
	}
	mux := http.NewServeMux()
	mux.HandleFunc(adminWebChatPagePath, service.handlePage)
	mux.HandleFunc(adminWebChatSessionsPath, service.handleSessions)
	mux.HandleFunc(adminWebChatHistoryPath, service.handleHistory)
	mux.HandleFunc(adminWebChatSendPath, service.handleSend)
	mux.HandleFunc(adminWebChatStreamPath, service.handleStream)
	mux.HandleFunc(adminWebChatCancelPath, service.handleCancel)
	if base != nil {
		mux.Handle("/", base)
	}
	return mux
}

func (s *adminWebChatService) handlePage(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data := adminWebChatPageData{
		DefaultUserID: s.defaultUID,
	}
	data.applyRelativeAdminLinks(r.URL.Path)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminWebChatPageTemplate.Execute(w, data); err != nil {
		http.Error(
			w,
			http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError,
		)
	}
}

func (s *adminWebChatService) handleSessions(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := s.normalizeUserID(r.URL.Query().Get("user_id"))
	sessions, err := s.listSessions(r.Context(), userID)
	if err != nil {
		writeAdminWebChatError(w, http.StatusInternalServerError, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, adminWebChatSessionsResponse{
		UserID:   userID,
		Sessions: sessions,
	})
}

func (s *adminWebChatService) handleHistory(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	userID := s.normalizeUserID(r.URL.Query().Get("user_id"))
	if sessionID == "" {
		writeRuntimeJSON(w, http.StatusOK, adminWebChatHistoryResponse{
			UserID:   userID,
			Messages: []adminWebChatMessage{},
		})
		return
	}
	messages, err := s.loadHistory(r.Context(), userID, sessionID)
	if err != nil {
		writeAdminWebChatError(w, http.StatusInternalServerError, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, adminWebChatHistoryResponse{
		SessionID: sessionID,
		UserID:    userID,
		Messages:  messages,
	})
}

func (s *adminWebChatService) handleSend(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := decodeAdminWebChatSendRequest(w, r)
	if err != nil {
		writeAdminWebChatError(w, http.StatusBadRequest, err)
		return
	}
	gwReq, err := s.buildGatewayRequest(req)
	if err != nil {
		writeAdminWebChatError(w, http.StatusBadRequest, err)
		return
	}
	rsp, err := s.gateway.SendMessage(r.Context(), gwReq)
	if err != nil {
		writeAdminWebChatError(w, http.StatusBadGateway, err)
		return
	}
	sessionID := firstNonEmpty(rsp.SessionID, gwReq.SessionID)
	requestID := firstNonEmpty(rsp.RequestID, gwReq.RequestID)
	message := buildAdminWebChatTextMessage(
		adminWebChatRoleAssistant,
		rsp.Reply,
		requestID,
	)
	writeRuntimeJSON(w, http.StatusOK, adminWebChatSendResponse{
		SessionID: sessionID,
		RequestID: requestID,
		Message:   message,
		Usage:     rsp.Usage,
		Ignored:   rsp.Ignored,
	})
}

func (s *adminWebChatService) handleStream(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	streamGateway, ok := s.gateway.(adminWebChatStreamingGateway)
	if !ok {
		writeAdminWebChatError(
			w,
			http.StatusNotImplemented,
			fmt.Errorf("gateway streaming is not available"),
		)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAdminWebChatError(
			w,
			http.StatusInternalServerError,
			fmt.Errorf("streaming response is not supported"),
		)
		return
	}
	req, err := decodeAdminWebChatSendRequest(w, r)
	if err != nil {
		writeAdminWebChatError(w, http.StatusBadRequest, err)
		return
	}
	gwReq, err := s.buildGatewayRequest(req)
	if err != nil {
		writeAdminWebChatError(w, http.StatusBadRequest, err)
		return
	}
	events, err := streamGateway.StreamMessage(r.Context(), gwReq)
	if err != nil {
		writeAdminWebChatError(w, http.StatusBadGateway, err)
		return
	}

	w.Header().Set("Content-Type", gwproto.SSEContentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_ = writeAdminWebChatSSE(w, flusher, adminWebChatEventStarted,
		adminWebChatStreamEvent{
			Type:      adminWebChatEventStarted,
			SessionID: gwReq.SessionID,
			RequestID: gwReq.RequestID,
		},
	)

	var reply strings.Builder
	for evt := range events {
		if evt.Type == gwproto.StreamEventTypeMessageDelta {
			reply.WriteString(evt.Delta)
		}
		out := normalizeAdminWebChatStreamEvent(evt, reply.String())
		if out.SessionID == "" {
			out.SessionID = gwReq.SessionID
		}
		if out.RequestID == "" {
			out.RequestID = gwReq.RequestID
		}
		if err := writeAdminWebChatSSE(
			w,
			flusher,
			out.Type,
			out,
		); err != nil {
			return
		}
	}
}

func (s *adminWebChatService) handleCancel(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req adminWebChatCancelRequest
	if err := decodeAdminWebChatJSON(
		w,
		r,
		&req,
	); err != nil {
		writeAdminWebChatError(w, http.StatusBadRequest, err)
		return
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		writeAdminWebChatError(
			w,
			http.StatusBadRequest,
			fmt.Errorf("request_id is required"),
		)
		return
	}
	canceled, err := s.gateway.Cancel(r.Context(), requestID)
	if err != nil {
		writeAdminWebChatError(w, http.StatusBadGateway, err)
		return
	}
	writeRuntimeJSON(w, http.StatusOK, map[string]bool{
		"canceled": canceled,
	})
}

func (s *adminWebChatService) buildGatewayRequest(
	req adminWebChatSendRequest,
) (gwclient.MessageRequest, error) {
	text := strings.TrimSpace(req.Message)
	if text == "" {
		return gwclient.MessageRequest{}, fmt.Errorf("message is required")
	}
	userID := s.normalizeUserID(req.UserID)
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = adminWebChatSessionPrefix + uuid.NewString()
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = adminWebChatMessagePrefix + uuid.NewString()
	}
	return gwclient.MessageRequest{
		Channel:   adminWebChatChannel,
		From:      userID,
		To:        adminWebChatRoleAssistant,
		MessageID: requestID,
		Text:      text,
		UserID:    userID,
		SessionID: sessionID,
		RequestID: requestID,
	}, nil
}

func (s *adminWebChatService) listSessions(
	ctx context.Context,
	userID string,
) ([]adminWebChatSessionView, error) {
	if s == nil || s.sessionSvc == nil || s.appName == "" {
		return []adminWebChatSessionView{}, nil
	}
	sessions, err := s.sessionSvc.ListSessions(
		ctx,
		session.UserKey{
			AppName: s.appName,
			UserID:  userID,
		},
		session.WithEventNum(adminWebChatSessionTitleMax),
	)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	if len(sessions) > adminWebChatSessionListLimit {
		sessions = sessions[:adminWebChatSessionListLimit]
	}
	out := make([]adminWebChatSessionView, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		out = append(out, adminWebChatSessionView{
			ID:        strings.TrimSpace(sess.ID),
			Title:     adminWebChatSessionTitle(sess),
			UpdatedAt: formatAdminWebChatTime(sess.UpdatedAt),
		})
	}
	return out, nil
}

func (s *adminWebChatService) loadHistory(
	ctx context.Context,
	userID string,
	sessionID string,
) ([]adminWebChatMessage, error) {
	if s == nil || s.sessionSvc == nil || s.appName == "" {
		return []adminWebChatMessage{}, nil
	}
	sess, err := s.sessionSvc.GetSession(
		ctx,
		session.Key{
			AppName:   s.appName,
			UserID:    userID,
			SessionID: sessionID,
		},
		session.WithEventNum(adminWebChatHistoryEventMax),
	)
	if err != nil {
		return nil, err
	}
	return projectAdminWebChatMessages(sess), nil
}

func (s *adminWebChatService) normalizeUserID(value string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	if s != nil && strings.TrimSpace(s.defaultUID) != "" {
		return strings.TrimSpace(s.defaultUID)
	}
	return adminWebChatDefaultUserID
}

func decodeAdminWebChatSendRequest(
	w http.ResponseWriter,
	r *http.Request,
) (adminWebChatSendRequest, error) {
	var req adminWebChatSendRequest
	if err := decodeAdminWebChatJSON(w, r, &req); err != nil {
		return adminWebChatSendRequest{}, err
	}
	return req, nil
}

func decodeAdminWebChatJSON(
	w http.ResponseWriter,
	r *http.Request,
	dst any,
) error {
	r.Body = http.MaxBytesReader(
		w,
		r.Body,
		adminWebChatRequestBodyLimit,
	)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func normalizeAdminWebChatStreamEvent(
	evt gwclient.StreamEvent,
	fallbackReply string,
) adminWebChatStreamEvent {
	out := adminWebChatStreamEvent{
		Type:      string(evt.Type),
		SessionID: evt.SessionID,
		RequestID: evt.RequestID,
		Delta:     evt.Delta,
		Reply:     evt.Reply,
		Stage:     string(evt.Stage),
		Summary:   evt.Summary,
		ElapsedMS: evt.ElapsedMS,
		Usage:     evt.Usage,
		Ignored:   evt.Ignored,
	}
	if out.Type == "" {
		out.Type = string(gwproto.StreamEventTypeRunProgress)
	}
	if out.Reply == "" {
		out.Reply = fallbackReply
	}
	if evt.Error != nil {
		out.Error = strings.TrimSpace(evt.Error.Message)
	}
	if evt.Type == gwproto.StreamEventTypeMessageCompleted {
		msg := buildAdminWebChatTextMessage(
			adminWebChatRoleAssistant,
			out.Reply,
			out.RequestID,
		)
		out.Message = &msg
	}
	return out
}

func projectAdminWebChatMessages(
	sess *session.Session,
) []adminWebChatMessage {
	if sess == nil {
		return []adminWebChatMessage{}
	}
	events := sess.GetEvents()
	out := make([]adminWebChatMessage, 0, len(events))
	for i := range events {
		out = append(
			out,
			projectAdminWebChatEvent(events[i])...,
		)
	}
	return out
}

func projectAdminWebChatEvent(
	evt event.Event,
) []adminWebChatMessage {
	if evt.Response == nil || evt.IsPartial ||
		len(evt.Response.Choices) == 0 {
		return nil
	}
	out := make([]adminWebChatMessage, 0, len(evt.Response.Choices))
	for _, choice := range evt.Response.Choices {
		msg := choice.Message
		content := projectAdminWebChatMessageContent(msg)
		if len(content) == 0 {
			continue
		}
		out = append(out, adminWebChatMessage{
			ID:        strings.TrimSpace(evt.ID),
			Role:      adminWebChatRoleFromEvent(evt, msg),
			Content:   content,
			Timestamp: formatAdminWebChatTime(evt.Timestamp),
			RequestID: strings.TrimSpace(evt.RequestID),
		})
	}
	return out
}

func projectAdminWebChatMessageContent(
	msg model.Message,
) []adminWebChatContent {
	out := make([]adminWebChatContent, 0, 1+len(msg.ToolCalls))
	if text := adminWebChatMessageText(msg); text != "" {
		contentType := adminWebChatContentText
		if msg.Role == model.RoleTool || msg.ToolID != "" {
			contentType = adminWebChatContentToolResult
		}
		item := adminWebChatContent{
			Type: contentType,
			Text: truncateAdminWebChatText(
				text,
				adminWebChatTextLimit,
			),
		}
		if contentType == adminWebChatContentToolResult {
			item.ToolCallID = strings.TrimSpace(msg.ToolID)
			item.Name = strings.TrimSpace(msg.ToolName)
			item.Output, item.Truncated = truncateAdminWebChatOutput(text)
			item.Text = ""
		}
		out = append(out, item)
	}
	for _, call := range msg.ToolCalls {
		name := strings.TrimSpace(call.Function.Name)
		args := formatAdminWebChatToolArgs(call.Function.Arguments)
		out = append(out, adminWebChatContent{
			Type:       adminWebChatContentToolCall,
			ToolCallID: strings.TrimSpace(call.ID),
			Name:       name,
			Arguments:  args,
		})
	}
	return out
}

func adminWebChatRoleFromEvent(
	evt event.Event,
	msg model.Message,
) string {
	switch msg.Role {
	case model.RoleUser:
		return adminWebChatRoleUser
	case model.RoleAssistant:
		return adminWebChatRoleAssistant
	case model.RoleSystem:
		return adminWebChatRoleSystem
	case model.RoleTool:
		return adminWebChatRoleTool
	}
	switch strings.TrimSpace(evt.Author) {
	case adminWebChatRoleUser:
		return adminWebChatRoleUser
	case adminWebChatRoleSystem:
		return adminWebChatRoleSystem
	default:
		return adminWebChatRoleAssistant
	}
}

func adminWebChatMessageText(msg model.Message) string {
	if text := strings.TrimSpace(msg.Content); text != "" {
		return text
	}
	parts := make([]string, 0, len(msg.ContentParts))
	for _, part := range msg.ContentParts {
		if part.Type != model.ContentTypeText || part.Text == nil {
			continue
		}
		text := strings.TrimSpace(*part.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func buildAdminWebChatTextMessage(
	role string,
	text string,
	requestID string,
) adminWebChatMessage {
	text = strings.TrimSpace(text)
	content := []adminWebChatContent{}
	if text != "" {
		content = append(content, adminWebChatContent{
			Type: adminWebChatContentText,
			Text: truncateAdminWebChatText(
				text,
				adminWebChatTextLimit,
			),
		})
	}
	return adminWebChatMessage{
		Role:      role,
		Content:   content,
		Timestamp: formatAdminWebChatTime(time.Now()),
		RequestID: strings.TrimSpace(requestID),
	}
}

func formatAdminWebChatToolArgs(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err == nil {
		return buf.String()
	}
	return string(raw)
}

func truncateAdminWebChatOutput(value string) (string, bool) {
	out := truncateAdminWebChatText(value, adminWebChatToolOutputLimit)
	return out, out != value
}

func truncateAdminWebChatText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + adminWebChatTruncatedMarker
}

func adminWebChatSessionTitle(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	events := sess.GetEvents()
	for i := len(events) - 1; i >= 0; i-- {
		msgs := projectAdminWebChatEvent(events[i])
		for _, msg := range msgs {
			if msg.Role != adminWebChatRoleUser {
				continue
			}
			if text := firstAdminWebChatText(msg.Content); text != "" {
				return truncateAdminWebChatText(
					text,
					adminWebChatPreviewLimit,
				)
			}
		}
	}
	id := strings.TrimSpace(sess.ID)
	if id == "" {
		return "Untitled chat"
	}
	return id
}

func firstAdminWebChatText(
	content []adminWebChatContent,
) string {
	for _, item := range content {
		text := strings.TrimSpace(item.Text)
		if text != "" {
			return text
		}
	}
	return ""
}

func writeAdminWebChatSSE(
	w http.ResponseWriter,
	flusher http.Flusher,
	eventType string,
	value any,
) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		w,
		"%s %s\n",
		gwproto.SSEEventPrefix,
		eventType,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		w,
		"%s %s\n\n",
		gwproto.SSEDataPrefix,
		data,
	); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeAdminWebChatError(
	w http.ResponseWriter,
	status int,
	err error,
) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	writeRuntimeJSON(w, status, map[string]string{
		"error": message,
	})
}

func (d *adminWebChatPageData) applyRelativeAdminLinks(
	currentPath string,
) {
	if d == nil {
		return
	}
	d.ChatLink = adminRelativeReference(currentPath, adminWebChatPagePath)
	d.SessionsAPI = adminRelativeReference(
		currentPath,
		adminWebChatSessionsPath,
	)
	d.HistoryAPI = adminRelativeReference(
		currentPath,
		adminWebChatHistoryPath,
	)
	d.SendAPI = adminRelativeReference(currentPath, adminWebChatSendPath)
	d.StreamAPI = adminRelativeReference(
		currentPath,
		adminWebChatStreamPath,
	)
	d.CancelAPI = adminRelativeReference(
		currentPath,
		adminWebChatCancelPath,
	)
	d.ChannelsLink = adminRelativeReference(
		currentPath,
		channelsAdminPagePath,
	)
}

func defaultAdminWebChatUserID() string {
	for _, key := range []string{
		adminWebChatEnvUser,
		adminWebChatEnvUsername,
	} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value
		}
	}
	return adminWebChatDefaultUserID
}

func formatAdminWebChatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

const adminWebChatPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>TRPC-CLAW chat</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f3ed;
      --panel: #fffdf8;
      --line: #d9d1c4;
      --ink: #1d1a16;
      --muted: #5f574d;
      --accent: #0f6f61;
      --danger: #9a2f2f;
      --soft: rgba(15, 111, 97, 0.08);
      --shadow: 0 18px 40px rgba(35, 29, 22, 0.08);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      font-family: "Iowan Old Style", "Palatino Linotype", serif;
      color: var(--ink);
      background: linear-gradient(180deg, #efe7dc 0%, var(--bg) 100%);
    }
    button, input, textarea {
      font: inherit;
    }
    .app-shell {
      display: grid;
      grid-template-columns: 272px minmax(0, 1fr);
      min-height: 100vh;
    }
    .sidebar {
      position: sticky;
      top: 0;
      align-self: start;
      height: 100vh;
      overflow-y: auto;
` + adminSidebarScrollCSS + `
      padding: 24px 18px 22px;
      border-right: 1px solid rgba(215, 207, 194, 0.92);
      background: rgba(255, 250, 244, 0.86);
    }
    .sidebar-brand {
      display: flex;
      align-items: center;
      gap: 12px;
      margin-bottom: 28px;
    }
    .sidebar-mark {
      width: 42px;
      height: 42px;
      border-radius: 14px;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      background: var(--accent);
      color: #fff;
      font-weight: 700;
    }
    .sidebar-eyebrow {
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.12em;
    }
    .sidebar-title {
      margin-top: 2px;
      font-size: 26px;
      font-weight: 700;
      line-height: 1.1;
    }
    .sidebar-subtle {
      margin-top: 4px;
      color: var(--muted);
      font-size: 14px;
    }
    .sidebar-nav {
      display: grid;
      gap: 22px;
    }
    .sidebar-section-title {
      margin: 0 0 10px;
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.12em;
    }
    .sidebar-links {
      display: grid;
      gap: 8px;
    }
    .sidebar-link {
      display: flex;
      align-items: center;
      min-height: 42px;
      padding: 10px 14px;
      border-radius: 14px;
      border: 1px solid transparent;
      color: var(--ink);
      text-decoration: none;
      font-weight: 700;
    }
    .sidebar-link:hover {
      background: rgba(255, 253, 248, 0.88);
      border-color: rgba(215, 207, 194, 0.88);
    }
    .sidebar-link.active {
      background: var(--soft);
      border-color: rgba(15, 111, 97, 0.24);
      color: var(--accent);
      box-shadow: var(--shadow);
    }
    main {
      min-width: 0;
      padding: 28px;
    }
    .chat-shell {
      display: grid;
      grid-template-columns: 280px minmax(0, 1fr);
      gap: 18px;
      max-width: 1480px;
      min-height: calc(100vh - 56px);
    }
    .sessions,
    .thread {
      border: 1px solid var(--line);
      background: rgba(255, 253, 248, 0.94);
      box-shadow: var(--shadow);
    }
    .sessions {
      border-radius: 18px;
      padding: 16px;
      min-width: 0;
    }
    .thread {
      border-radius: 18px;
      display: grid;
      grid-template-rows: auto minmax(0, 1fr) auto;
      min-width: 0;
      overflow: hidden;
    }
    .panel-title {
      margin: 0;
      font-size: 18px;
    }
    .muted {
      color: var(--muted);
      font-size: 13px;
    }
    .session-toolbar,
    .chat-toolbar {
      display: flex;
      align-items: center;
      gap: 10px;
      flex-wrap: wrap;
    }
    .session-toolbar {
      justify-content: space-between;
      margin-bottom: 14px;
    }
    .chat-toolbar {
      justify-content: space-between;
      padding: 16px 18px;
      border-bottom: 1px solid var(--line);
      background: rgba(255, 250, 244, 0.92);
    }
    .session-list {
      display: grid;
      gap: 8px;
      margin-top: 14px;
    }
    .session-item {
      width: 100%;
      text-align: left;
      border: 1px solid var(--line);
      border-radius: 12px;
      padding: 10px;
      background: var(--panel);
      cursor: pointer;
    }
    .session-item.active {
      border-color: rgba(15, 111, 97, 0.42);
      background: var(--soft);
    }
    .session-title {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-weight: 700;
    }
    .messages {
      min-height: 0;
      overflow: auto;
      padding: 18px;
      display: grid;
      align-content: start;
      gap: 14px;
    }
    .message {
      display: grid;
      gap: 8px;
      max-width: 860px;
    }
    .message.user {
      justify-self: end;
    }
    .message.assistant,
    .message.tool,
    .message.system {
      justify-self: start;
    }
    .bubble {
      border: 1px solid var(--line);
      border-radius: 16px;
      padding: 12px 14px;
      background: var(--panel);
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      line-height: 1.5;
    }
    .message.user .bubble {
      border-color: rgba(15, 111, 97, 0.22);
      background: var(--soft);
    }
    .message-meta {
      color: var(--muted);
      font-size: 12px;
      font-weight: 700;
    }
    .tool-card {
      border: 1px solid var(--line);
      border-radius: 12px;
      background: #fbf8f1;
      overflow: hidden;
    }
    .tool-head {
      width: 100%;
      border: 0;
      background: transparent;
      padding: 10px 12px;
      display: flex;
      justify-content: space-between;
      gap: 12px;
      cursor: pointer;
      font-weight: 700;
      color: var(--ink);
    }
    .tool-body {
      display: none;
      border-top: 1px solid var(--line);
      padding: 12px;
    }
    .tool-card.open .tool-body {
      display: grid;
      gap: 10px;
    }
    pre {
      margin: 0;
      padding: 10px;
      max-height: 360px;
      overflow: auto;
      border-radius: 10px;
      background: #1d1a16;
      color: #f7f0e6;
      font-size: 13px;
      line-height: 1.45;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
    .composer {
      border-top: 1px solid var(--line);
      padding: 14px 18px 18px;
      background: rgba(255, 250, 244, 0.92);
    }
    .composer-grid {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 10px;
      align-items: end;
    }
    textarea {
      min-height: 88px;
      max-height: 220px;
      resize: vertical;
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 12px;
      background: var(--panel);
      color: var(--ink);
    }
    input {
      min-height: 38px;
      border: 1px solid var(--line);
      border-radius: 12px;
      padding: 8px 10px;
      background: var(--panel);
      color: var(--ink);
    }
    .btn {
      min-height: 38px;
      border: 1px solid var(--line);
      border-radius: 12px;
      padding: 8px 12px;
      background: var(--panel);
      color: var(--ink);
      font-weight: 700;
      cursor: pointer;
    }
    .btn.primary {
      background: var(--accent);
      border-color: var(--accent);
      color: #fff;
    }
    .btn.danger {
      border-color: rgba(154, 47, 47, 0.36);
      color: var(--danger);
    }
    .btn:disabled {
      cursor: not-allowed;
      opacity: 0.46;
    }
    .segmented {
      display: inline-grid;
      grid-template-columns: repeat(2, minmax(76px, 1fr));
      border: 1px solid var(--line);
      border-radius: 12px;
      overflow: hidden;
      background: var(--panel);
    }
    .segmented button {
      border: 0;
      min-height: 36px;
      padding: 6px 10px;
      background: transparent;
      color: var(--ink);
      cursor: pointer;
      font-weight: 700;
    }
    .segmented button.active {
      background: var(--accent);
      color: #fff;
    }
    .toggle {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      color: var(--muted);
      font-size: 14px;
      font-weight: 700;
    }
    .status {
      color: var(--muted);
      font-size: 13px;
      min-height: 20px;
    }
    @media (max-width: 980px) {
      .app-shell {
        grid-template-columns: 1fr;
      }
      .sidebar {
        position: relative;
        height: auto;
        overflow: visible;
      }
      .chat-shell {
        grid-template-columns: 1fr;
      }
    }
  </style>
</head>
<body
  data-default-user="{{.DefaultUserID}}"
  data-sessions-api="{{.SessionsAPI}}"
  data-history-api="{{.HistoryAPI}}"
  data-send-api="{{.SendAPI}}"
  data-stream-api="{{.StreamAPI}}"
  data-cancel-api="{{.CancelAPI}}"
>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="sidebar-brand">
        <div class="sidebar-mark">TC</div>
        <div>
          <div class="sidebar-eyebrow">control</div>
          <div class="sidebar-title">TRPC-CLAW</div>
          <div class="sidebar-subtle">trpc-claw</div>
        </div>
      </div>
      <nav class="sidebar-nav" aria-label="Admin sections">
        <section>
          <div class="sidebar-section-title">Control</div>
          <div class="sidebar-links">
            <a class="sidebar-link" href="overview">Overview</a>
            <a class="sidebar-link" href="config">Config</a>
            <a class="sidebar-link" href="skills">Skills</a>
            <a class="sidebar-link" href="prompts">Prompts</a>
            <a class="sidebar-link" href="identity">Identity</a>
            <a class="sidebar-link" href="personas">Personas</a>
            <a class="sidebar-link" href="chats">Chats</a>
            <a class="sidebar-link" href="memory">Memory</a>
            <a class="sidebar-link" href="automation">Automation</a>
          </div>
        </section>
        <section>
          <div class="sidebar-section-title">Diagnostics</div>
          <div class="sidebar-links">
            <a class="sidebar-link" href="runtime-control">
              Runtime Control
            </a>
            <a class="sidebar-link" href="sessions">Runtime Sessions</a>
            <a class="sidebar-link" href="debug">Debug</a>
            <a class="sidebar-link" href="browser">Browser</a>
          </div>
        </section>
        <section>
          <div class="sidebar-section-title">Admin</div>
          <div class="sidebar-links">
            <a class="sidebar-link active" href="{{.ChatLink}}">Chat</a>
            <a class="sidebar-link" href="{{.ChannelsLink}}">Channels</a>
          </div>
        </section>
      </nav>
    </aside>
    <main>
      <div class="chat-shell">
        <aside class="sessions">
          <div class="session-toolbar">
            <h1 class="panel-title">Chat</h1>
            <button class="btn" type="button" id="newChat">New</button>
          </div>
          <label class="muted" for="userID">User ID</label>
          <input id="userID" autocomplete="off">
          <div class="session-list" id="sessionList"></div>
        </aside>
        <section class="thread">
          <div class="chat-toolbar">
            <div>
              <h2 class="panel-title" id="threadTitle">New Chat</h2>
              <div class="muted" id="threadMeta"></div>
            </div>
            <div class="chat-toolbar">
              <div class="segmented" aria-label="Response mode">
                <button type="button" id="modeStream">Stream</button>
                <button type="button" id="modeFinal">Final</button>
              </div>
              <label class="toggle">
                <input id="showTools" type="checkbox" checked>
                Tools
              </label>
              <button class="btn danger" type="button" id="cancelRun">
                Cancel
              </button>
            </div>
          </div>
          <div class="messages" id="messages"></div>
          <div class="composer">
            <div class="composer-grid">
              <textarea
                id="composer"
                placeholder="Message the bot"
              ></textarea>
              <button class="btn primary" type="button" id="sendBtn">
                Send
              </button>
            </div>
            <div class="status" id="status"></div>
          </div>
        </section>
      </div>
    </main>
  </div>
  <script>
    const body = document.body.dataset;
    const state = {
      userID: localStorage.getItem("adminChatUserID") ||
        body.defaultUser || "admin",
      sessionID: localStorage.getItem("adminChatSessionID") || "",
      mode: localStorage.getItem("adminChatMode") || "stream",
      showTools: localStorage.getItem("adminChatShowTools") !== "false",
      requestID: "",
      running: false,
      messages: []
    };

    const el = {
      userID: document.getElementById("userID"),
      sessionList: document.getElementById("sessionList"),
      messages: document.getElementById("messages"),
      composer: document.getElementById("composer"),
      sendBtn: document.getElementById("sendBtn"),
      cancelRun: document.getElementById("cancelRun"),
      newChat: document.getElementById("newChat"),
      modeStream: document.getElementById("modeStream"),
      modeFinal: document.getElementById("modeFinal"),
      showTools: document.getElementById("showTools"),
      status: document.getElementById("status"),
      threadTitle: document.getElementById("threadTitle"),
      threadMeta: document.getElementById("threadMeta")
    };

    function setStatus(text) {
      el.status.textContent = text || "";
    }

    function escapeHTML(value) {
      return String(value || "").replace(/[&<>"']/g, function(ch) {
        return {
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;"
        }[ch];
      });
    }

    function savePrefs() {
      localStorage.setItem("adminChatUserID", state.userID);
      localStorage.setItem("adminChatSessionID", state.sessionID);
      localStorage.setItem("adminChatMode", state.mode);
      localStorage.setItem("adminChatShowTools", String(state.showTools));
    }

    function updateControls() {
      el.userID.value = state.userID;
      el.modeStream.classList.toggle("active", state.mode === "stream");
      el.modeFinal.classList.toggle("active", state.mode === "final");
      el.showTools.checked = state.showTools;
      el.sendBtn.disabled = state.running;
      el.cancelRun.disabled = !state.running || !state.requestID;
      el.threadTitle.textContent = state.sessionID ?
        "Admin Chat" : "New Chat";
      el.threadMeta.textContent = state.sessionID || "";
    }

    function textFromContent(content) {
      return (content || []).filter(function(item) {
        return item.type === "text" && item.text;
      }).map(function(item) {
        return item.text;
      }).join("\n");
    }

    function appendMessage(message) {
      state.messages.push(message);
      renderMessages();
    }

    function ensureAssistantMessage(requestID) {
      let current = state.messages[state.messages.length - 1];
      if (current && current.role === "assistant" &&
          current.request_id === requestID) {
        return current;
      }
      current = {
        role: "assistant",
        request_id: requestID,
        content: [{ type: "text", text: "" }]
      };
      state.messages.push(current);
      return current;
    }

    function renderMessages() {
      el.messages.innerHTML = state.messages.map(renderMessage).join("");
      el.messages.scrollTop = el.messages.scrollHeight;
    }

    function renderMessage(message) {
      const role = message.role || "assistant";
      const visible = renderContent(message.content || []);
      if (!visible) {
        return "";
      }
      const meta = escapeHTML(role);
      return [
        '<article class="message ', escapeHTML(role), '">',
        '<div class="message-meta">', meta, '</div>',
        visible,
        '</article>'
      ].join("");
    }

    function renderContent(content) {
      return content.map(function(item) {
        if (item.type === "text") {
          return '<div class="bubble">' + escapeHTML(item.text) + '</div>';
        }
        if (!state.showTools) {
          return "";
        }
        return renderToolCard(item);
      }).join("");
    }

    function renderToolCard(item) {
      const name = item.name || item.stage || "tool";
      const title = item.type === "progress" ? item.text || name : name;
      const detail = item.arguments || item.output || item.text || "";
      return [
        '<div class="tool-card">',
        '<button class="tool-head" type="button">',
        '<span>', escapeHTML(title), '</span>',
        '<span>', escapeHTML(item.type), '</span>',
        '</button>',
        '<div class="tool-body"><pre>',
        escapeHTML(detail || title),
        '</pre></div>',
        '</div>'
      ].join("");
    }

    function bindToolCards() {
      el.messages.querySelectorAll(".tool-head").forEach(function(btn) {
        btn.addEventListener("click", function() {
          btn.closest(".tool-card").classList.toggle("open");
        });
      });
    }

    const oldRenderMessages = renderMessages;
    renderMessages = function() {
      oldRenderMessages();
      bindToolCards();
    };

    async function loadSessions() {
      const url = body.sessionsApi + "?user_id=" +
        encodeURIComponent(state.userID);
      const rsp = await fetch(url);
      const data = await rsp.json();
      if (!rsp.ok) {
        throw new Error(data.error || "failed to load sessions");
      }
      el.sessionList.innerHTML = (data.sessions || []).map(function(item) {
        const active = item.id === state.sessionID ? " active" : "";
        return [
          '<button class="session-item', active, '" type="button"',
          ' data-session-id="', escapeHTML(item.id), '">',
          '<div class="session-title">',
          escapeHTML(item.title || item.id),
          '</div>',
          '<div class="muted">', escapeHTML(item.updated_at || ""), '</div>',
          '</button>'
        ].join("");
      }).join("");
      el.sessionList.querySelectorAll(".session-item").forEach(function(btn) {
        btn.addEventListener("click", function() {
          state.sessionID = btn.dataset.sessionId || "";
          savePrefs();
          loadHistory().catch(showError);
        });
      });
    }

    async function loadHistory() {
      updateControls();
      if (!state.sessionID) {
        state.messages = [];
        renderMessages();
        return;
      }
      const url = body.historyApi + "?user_id=" +
        encodeURIComponent(state.userID) + "&session_id=" +
        encodeURIComponent(state.sessionID);
      const rsp = await fetch(url);
      const data = await rsp.json();
      if (!rsp.ok) {
        throw new Error(data.error || "failed to load history");
      }
      state.messages = data.messages || [];
      renderMessages();
      await loadSessions();
    }

    function requestPayload(text) {
      return {
        user_id: state.userID,
        session_id: state.sessionID,
        request_id: "admin-msg-" + crypto.randomUUID(),
        message: text
      };
    }

    async function sendFinal(payload) {
      const rsp = await fetch(body.sendApi, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload)
      });
      const data = await rsp.json();
      if (!rsp.ok) {
        throw new Error(data.error || "send failed");
      }
      if (data.ignored) {
        throw new Error("Request ignored");
      }
      state.sessionID = data.session_id || state.sessionID;
      appendMessage(data.message);
      await loadHistory();
    }

    async function sendStream(payload) {
      const rsp = await fetch(body.streamApi, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload)
      });
      if (!rsp.ok || !rsp.body) {
        const data = await rsp.json().catch(function() { return {}; });
        throw new Error(data.error || "stream failed");
      }
      await readSSE(rsp.body.getReader(), handleStreamEvent);
      await loadHistory();
    }

    async function readSSE(reader, onEvent) {
      const decoder = new TextDecoder();
      let buffer = "";
      while (true) {
        const result = await reader.read();
        if (result.done) {
          break;
        }
        buffer += decoder.decode(result.value, { stream: true });
        const chunks = buffer.split("\n\n");
        buffer = chunks.pop() || "";
        chunks.forEach(function(chunk) {
          const data = chunk.split("\n").filter(function(line) {
            return line.startsWith("data:");
          }).map(function(line) {
            return line.slice(5).trim();
          }).join("\n");
          if (data) {
            onEvent(JSON.parse(data));
          }
        });
      }
    }

    function handleStreamEvent(evt) {
      if (evt.session_id) {
        state.sessionID = evt.session_id;
      }
      if (evt.request_id) {
        state.requestID = evt.request_id;
      }
      if (evt.ignored) {
        throw new Error("Request ignored");
      }
      if (evt.type === "message.delta") {
        const msg = ensureAssistantMessage(state.requestID);
        msg.content[0].text += evt.delta || "";
        renderMessages();
        return;
      }
      if (evt.type === "run.progress") {
        const msg = ensureAssistantMessage(state.requestID);
        msg.content.push({
          type: "progress",
          text: evt.summary || evt.stage || "Running",
          stage: evt.stage || "",
          elapsed_ms: evt.elapsed_ms || 0
        });
        renderMessages();
        return;
      }
      if (evt.message) {
        const msg = ensureAssistantMessage(state.requestID);
        msg.content = evt.message.content || msg.content;
        renderMessages();
      }
      if (evt.error) {
        throw new Error(evt.error);
      }
    }

    async function sendCurrent() {
      const text = el.composer.value.trim();
      if (!text || state.running) {
        return;
      }
      state.userID = el.userID.value.trim() || state.userID;
      const payload = requestPayload(text);
      state.requestID = payload.request_id;
      state.running = true;
      setStatus("Running");
      el.composer.value = "";
      appendMessage({
        role: "user",
        content: [{ type: "text", text: text }]
      });
      updateControls();
      try {
        if (state.mode === "stream") {
          await sendStream(payload);
        } else {
          await sendFinal(payload);
        }
        setStatus("");
      } catch (err) {
        showError(err);
      } finally {
        state.running = false;
        state.requestID = "";
        savePrefs();
        updateControls();
        await loadSessions().catch(function() {});
      }
    }

    async function cancelCurrent() {
      if (!state.requestID) {
        return;
      }
      setStatus("Cancel requested");
      try {
        const rsp = await fetch(body.cancelApi, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ request_id: state.requestID })
        });
        const data = await rsp.json().catch(function() {
          return {};
        });
        if (!rsp.ok) {
          throw new Error(data.error || "cancel failed");
        }
        if (!data.canceled) {
          setStatus("No active run matched this request");
        }
      } catch (err) {
        showError(err);
      }
    }

    function showError(err) {
      setStatus(err && err.message ? err.message : String(err));
    }

    el.sendBtn.addEventListener("click", sendCurrent);
    el.cancelRun.addEventListener("click", cancelCurrent);
    el.newChat.addEventListener("click", function() {
      state.sessionID = "";
      state.messages = [];
      savePrefs();
      updateControls();
      renderMessages();
    });
    el.modeStream.addEventListener("click", function() {
      state.mode = "stream";
      savePrefs();
      updateControls();
    });
    el.modeFinal.addEventListener("click", function() {
      state.mode = "final";
      savePrefs();
      updateControls();
    });
    el.showTools.addEventListener("change", function() {
      state.showTools = el.showTools.checked;
      savePrefs();
      renderMessages();
    });
    el.userID.addEventListener("change", function() {
      state.userID = el.userID.value.trim() || body.defaultUser || "admin";
      state.sessionID = "";
      state.messages = [];
      savePrefs();
      updateControls();
      renderMessages();
      loadSessions().catch(showError);
    });
    el.composer.addEventListener("keydown", function(evt) {
      if ((evt.metaKey || evt.ctrlKey) && evt.key === "Enter") {
        sendCurrent();
      }
    });

    updateControls();
    loadSessions().then(loadHistory).catch(showError);
  </script>
` + adminSidebarRevealScriptHTML + `
</body>
</html>
`
