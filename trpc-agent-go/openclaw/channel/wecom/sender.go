package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
	occhannel "trpc.group/trpc-go/trpc-agent-go/openclaw/channel"
)

const (
	msgTypeMarkdown              = "markdown"
	msgTypeMarkdownV2            = "markdown_v2"
	senderHeaderType             = "Content-Type"
	senderContentType            = "application/json"
	senderLogMaxContent          = 100
	commandReplyTruncationNotice = "\n\n（内容较长，" +
		"当前发送方式不支持分段回包，剩余内容已省略。）"
)

// httpClient is the minimal HTTP client interface for testing.
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type streamingSender interface {
	SendStream(
		ctx context.Context,
		chatID string,
		streamID string,
		content string,
		finish bool,
	) error
}

type feedbackStreamingSender interface {
	SendStreamWithFeedback(
		ctx context.Context,
		chatID string,
		streamID string,
		content string,
		finish bool,
		feedbackID string,
	) error
}

type wsReplyWriter interface {
	send(ctx context.Context, frame wsOutboundFrame) error
}

type wsRequestWriter interface {
	wsReplyWriter
	request(
		ctx context.Context,
		frame wsOutboundFrame,
	) (wsInboundFrame, error)
}

type localFileSender interface {
	SendLocalFile(
		ctx context.Context,
		chatID string,
		path string,
	) error
}

type multipartReplyCapability interface {
	supportsMultipartReplies() bool
}

func sendTextReply(
	ctx context.Context,
	sender messageSender,
	chatID string,
	content string,
) error {
	if sender == nil {
		return fmt.Errorf("wecom sender: nil sender")
	}

	parts := splitReplyText(content)
	if len(parts) == 1 {
		return sender.SendText(ctx, chatID, parts[0])
	}
	if !senderSupportsMultipartReplies(sender) {
		return sender.SendText(
			ctx,
			chatID,
			truncateTextReply(content),
		)
	}
	for _, part := range parts {
		if err := sender.SendText(ctx, chatID, part); err != nil {
			return err
		}
	}
	return nil
}

func senderSupportsMultipartReplies(
	sender messageSender,
) bool {
	capability, ok := sender.(multipartReplyCapability)
	if !ok {
		return true
	}
	return capability.supportsMultipartReplies()
}

func truncateTextReply(content string) string {
	limit := maxReplyRunes - len([]rune(commandReplyTruncationNotice))
	if limit <= 0 {
		return commandReplyTruncationNotice
	}
	parts := splitRunes(content, limit)
	return parts[0] + commandReplyTruncationNotice
}

// ---------------------------------------------------------------------------
// webhookSender: for group-bot webhook mode (chatid-based).
// ---------------------------------------------------------------------------

// webhookSender sends messages to enterprise WeChat via the Webhook API.
// It implements the messageSender interface.
type webhookSender struct {
	webhookURL string
	client     httpClient
}

// Compile-time check that webhookSender implements messageSender.
var _ messageSender = (*webhookSender)(nil)

func newWebhookSender(webhookURL string, client httpClient) *webhookSender {
	return &webhookSender{
		webhookURL: webhookURL,
		client:     client,
	}
}

// webhookPayload is the outer JSON envelope for group bot webhook messages.
type webhookPayload struct {
	MsgType    string             `json:"msgtype"`
	ChatID     string             `json:"chatid,omitempty"`
	Text       *webhookText       `json:"text,omitempty"`
	MarkdownV2 *webhookMarkdownV2 `json:"markdown_v2,omitempty"`
}

type webhookText struct {
	Content string `json:"content"`
}

type webhookMarkdownV2 struct {
	Content string `json:"content"`
}

// SendText sends a plain text message to a chat.
func (s *webhookSender) SendText(ctx context.Context, chatID, content string) error {
	payload := webhookPayload{
		MsgType: MsgTypeText,
		ChatID:  chatID,
		Text:    &webhookText{Content: content},
	}
	return s.doPost(ctx, payload)
}

// SendMarkdown sends a markdown_v2 message to a chat.
func (s *webhookSender) SendMarkdown(ctx context.Context, chatID, content string) error {
	payload := webhookPayload{
		MsgType:    msgTypeMarkdownV2,
		ChatID:     chatID,
		MarkdownV2: &webhookMarkdownV2{Content: content},
	}
	return s.doPost(ctx, payload)
}

func (s *webhookSender) doPost(ctx context.Context, payload any) error {
	return doHTTPPost(ctx, s.client, s.webhookURL, payload)
}

// ---------------------------------------------------------------------------
// aibotSender: for AI-bot mode (response_url-based, one-shot).
// See: https://developer.work.weixin.qq.com/document/path/101138
//
// Key constraints from the docs:
//   - response_url can only be called ONCE.
//   - Supported msgtype: "markdown" and "template_card".
//   - response_url expires after 1 hour.
// ---------------------------------------------------------------------------

// aibotSender sends a single reply to the AI bot response_url.
type aibotSender struct {
	responseURL string
	client      httpClient
	sent        bool // guards one-shot semantics
}

// Compile-time check that aibotSender implements messageSender.
var _ messageSender = (*aibotSender)(nil)
var _ templateCardSender = (*aibotSender)(nil)

func newAIBotSender(responseURL string, client httpClient) *aibotSender {
	return &aibotSender{
		responseURL: responseURL,
		client:      client,
	}
}

func (s *aibotSender) supportsMultipartReplies() bool {
	return false
}

// aibotPayload is the JSON envelope for AI bot active reply.
type aibotPayload struct {
	MsgType      string         `json:"msgtype"`
	Markdown     *aibotMarkdown `json:"markdown,omitempty"`
	Stream       *aibotStream   `json:"stream,omitempty"`
	TemplateCard *templateCard  `json:"template_card,omitempty"`
}

type aibotMarkdown struct {
	Content string `json:"content"`
}

type aibotStream struct {
	ID       string               `json:"id,omitempty"`
	Finish   bool                 `json:"finish"`
	Content  string               `json:"content,omitempty"`
	Feedback *aibotStreamFeedback `json:"feedback,omitempty"`
}

type aibotStreamFeedback struct {
	ID string `json:"id,omitempty"`
}

// SendText for AI bot mode: converts text to markdown and sends.
// Since response_url only supports markdown, we wrap text in a markdown payload.
func (s *aibotSender) SendText(ctx context.Context, _ string, content string) error {
	return s.sendMarkdown(ctx, content)
}

// SendMarkdown sends a markdown message to the response_url.
func (s *aibotSender) SendMarkdown(ctx context.Context, _ string, content string) error {
	return s.sendMarkdown(ctx, content)
}

func (s *aibotSender) SendTemplateCard(
	ctx context.Context,
	_ string,
	card *templateCard,
) error {
	if card == nil {
		return fmt.Errorf("wecom aibot sender: nil template card")
	}
	card = normalizeTemplateCard(card)
	if s.sent {
		log.WarnfContext(
			ctx,
			"wecom aibot sender: response_url already used, "+
				"skip template card",
		)
		return nil
	}
	s.sent = true

	payload := aibotPayload{
		MsgType:      msgTypeTemplateCard,
		TemplateCard: card,
	}
	return doHTTPPost(ctx, s.client, s.responseURL, payload)
}

func (s *aibotSender) sendMarkdown(ctx context.Context, content string) error {
	if s.sent {
		log.WarnfContext(
			ctx,
			"wecom aibot sender: response_url already used, "+
				"skip message: %s",
			truncate(content, senderLogMaxContent),
		)
		return nil
	}
	s.sent = true

	payload := aibotPayload{
		MsgType:  msgTypeMarkdown,
		Markdown: &aibotMarkdown{Content: content},
	}
	return doHTTPPost(ctx, s.client, s.responseURL, payload)
}

type aibotWebSocketSender struct {
	writer wsReplyWriter
	reqID  string
}

var _ messageSender = (*aibotWebSocketSender)(nil)
var _ streamingSender = (*aibotWebSocketSender)(nil)
var _ feedbackStreamingSender = (*aibotWebSocketSender)(nil)
var _ localFileSender = (*aibotWebSocketSender)(nil)
var _ templateCardSender = (*aibotWebSocketSender)(nil)
var _ interactiveTemplateCardSender = (*aibotWebSocketSender)(nil)

func newAIBotWebSocketSender(
	writer wsReplyWriter,
	reqID string,
) *aibotWebSocketSender {
	return &aibotWebSocketSender{
		writer: writer,
		reqID:  strings.TrimSpace(reqID),
	}
}

func (s *aibotWebSocketSender) SendText(
	ctx context.Context,
	_ string,
	content string,
) error {
	return s.SendMarkdown(ctx, "", content)
}

func (s *aibotWebSocketSender) SendMarkdown(
	ctx context.Context,
	_ string,
	content string,
) error {
	if s == nil || s.writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}
	frame := wsOutboundFrame{
		Command: wsCommandRespond,
		Headers: wsFrameHeaders{ReqID: s.reqID},
		Body: wsReplyBody{
			MsgType:  msgTypeMarkdown,
			Markdown: &aibotMarkdown{Content: content},
		},
	}
	return s.writer.send(ctx, frame)
}

func (s *aibotWebSocketSender) SendStream(
	ctx context.Context,
	_ string,
	streamID string,
	content string,
	finish bool,
) error {
	return s.SendStreamWithFeedback(
		ctx,
		"",
		streamID,
		content,
		finish,
		"",
	)
}

func (s *aibotWebSocketSender) SendStreamWithFeedback(
	ctx context.Context,
	_ string,
	streamID string,
	content string,
	finish bool,
	feedbackID string,
) error {
	if s == nil || s.writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}
	stream := &aibotStream{
		ID:      streamID,
		Finish:  finish,
		Content: content,
	}
	if strings.TrimSpace(feedbackID) != "" {
		stream.Feedback = &aibotStreamFeedback{
			ID: strings.TrimSpace(feedbackID),
		}
	}
	frame := wsOutboundFrame{
		Command: wsCommandRespond,
		Headers: wsFrameHeaders{ReqID: s.reqID},
		Body: wsReplyBody{
			MsgType: MsgTypeStream,
			Stream:  stream,
		},
	}
	return s.writer.send(ctx, frame)
}

func (s *aibotWebSocketSender) SendTemplateCard(
	ctx context.Context,
	_ string,
	card *templateCard,
) error {
	if s == nil || s.writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}
	if card == nil {
		return fmt.Errorf("wecom websocket sender: nil template card")
	}
	card = normalizeTemplateCard(card)
	frame := wsOutboundFrame{
		Command: wsCommandRespond,
		Headers: wsFrameHeaders{ReqID: s.reqID},
		Body: wsReplyBody{
			MsgType:      msgTypeTemplateCard,
			TemplateCard: card,
		},
	}
	return s.writer.send(ctx, frame)
}

func (s *aibotWebSocketSender) UpdateTemplateCard(
	ctx context.Context,
	card *templateCard,
) error {
	if s == nil || s.writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}
	if card == nil {
		return fmt.Errorf("wecom websocket sender: nil template card")
	}
	card = normalizeTemplateCard(card)
	frame := wsOutboundFrame{
		Command: wsCommandRespondUpdate,
		Headers: wsFrameHeaders{ReqID: s.reqID},
		Body: wsTemplateCardUpdateBody{
			ResponseType: templateCardUpdateResponseType,
			TemplateCard: card,
		},
	}
	return s.writer.send(ctx, frame)
}

func sendWebSocketWelcome(
	ctx context.Context,
	writer wsReplyWriter,
	reqID string,
	reply callbackReplyBody,
) error {
	if writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}
	if reply.TemplateCard != nil {
		reply.TemplateCard = normalizeTemplateCard(
			reply.TemplateCard,
		)
	}
	frame := wsOutboundFrame{
		Command: wsCommandRespondWelcome,
		Headers: wsFrameHeaders{ReqID: strings.TrimSpace(reqID)},
		Body:    reply,
	}
	return writer.send(ctx, frame)
}

func (c *Channel) SendText(
	ctx context.Context,
	target string,
	text string,
) error {
	return c.sendOutboundMessage(
		ctx,
		target,
		occhannel.OutboundMessage{Text: text},
		"text",
	)
}

func (c *Channel) SendMessage(
	ctx context.Context,
	target string,
	msg occhannel.OutboundMessage,
) error {
	return c.sendOutboundMessage(ctx, target, msg, "message")
}

func (c *Channel) sendOutboundMessage(
	ctx context.Context,
	target string,
	msg occhannel.OutboundMessage,
	kind string,
) error {
	if c == nil {
		return fmt.Errorf("wecom channel: nil channel")
	}
	if c.botMode != botModeAI ||
		c.connectionMode != connectionModeWebSocket {
		return fmt.Errorf(
			"wecom channel: outbound %s requires ai "+
				"websocket mode",
			kind,
		)
	}

	pushTarget, err := parsePushTarget(target)
	if err != nil {
		return err
	}

	writer := c.webSocketPushWriter()
	if writer == nil {
		return fmt.Errorf(
			"wecom websocket: proactive send unavailable",
		)
	}

	return sendWebSocketPushMessage(
		ctx,
		writer,
		pushTarget,
		msg,
	)
}

func sendWebSocketPushMessage(
	ctx context.Context,
	writer wsRequestWriter,
	target pushTarget,
	msg occhannel.OutboundMessage,
) error {
	if writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}
	if strings.TrimSpace(msg.Text) != "" {
		if err := sendWebSocketPushText(
			ctx,
			writer,
			target,
			msg.Text,
		); err != nil {
			return err
		}
	}
	for _, file := range msg.Files {
		if err := sendWebSocketPushFile(
			ctx,
			writer,
			target,
			file,
		); err != nil {
			return err
		}
	}
	return nil
}

func sendWebSocketPushText(
	ctx context.Context,
	writer wsRequestWriter,
	target pushTarget,
	text string,
) error {
	if writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}

	for _, part := range splitReplyText(text) {
		_, err := writer.request(ctx, wsOutboundFrame{
			Command: wsCommandSend,
			Headers: wsFrameHeaders{
				ReqID: nextWSReqID(wsReqIDSend),
			},
			Body: wsSendBody{
				ChatID:  target.ChatID,
				MsgType: msgTypeMarkdown,
				Markdown: &aibotMarkdown{
					Content: part,
				},
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func sendWebSocketPushFile(
	ctx context.Context,
	writer wsRequestWriter,
	target pushTarget,
	file occhannel.OutboundFile,
) error {
	if writer == nil {
		return fmt.Errorf("wecom websocket sender: nil writer")
	}

	media, err := loadLocalReplyMediaFile(file)
	if err != nil {
		return err
	}
	log.InfofContext(
		ctx,
		"wecom websocket: upload outbound file msgtype=%s "+
			"filename=%q bytes=%d",
		media.MsgType,
		media.Filename,
		len(media.Data),
	)

	mediaID, err := uploadLocalReplyMedia(ctx, writer, media)
	if err != nil {
		return err
	}
	return sendUploadedPushMedia(ctx, writer, target, media, mediaID)
}

// ---------------------------------------------------------------------------
// Shared HTTP helper
// ---------------------------------------------------------------------------

// webhookResponse is the response from the enterprise WeChat API.
type webhookResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// doHTTPPost marshals payload as JSON and POSTs it to the given URL.
func doHTTPPost(ctx context.Context, client httpClient, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("wecom sender: marshal: %w", err)
	}

	log.InfofContext(ctx, "wecom sender: POST %s, body_len=%d, body=%s", url, len(body), string(body))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("wecom sender: new request: %w", err)
	}
	req.Header.Set(senderHeaderType, senderContentType)

	resp, err := client.Do(req)
	if err != nil {
		log.ErrorfContext(ctx, "wecom sender: do request failed: %v", err)
		return fmt.Errorf("wecom sender: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("wecom sender: read response: %w", err)
	}

	log.InfofContext(ctx, "wecom sender: response status=%d, body=%s", resp.StatusCode, string(respBody))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wecom sender: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result webhookResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		log.WarnfContext(ctx, "wecom sender: unmarshal response: %v", err)
		return nil
	}

	if result.ErrCode != 0 {
		log.ErrorfContext(ctx, "wecom sender: API error - errcode=%d errmsg=%s",
			result.ErrCode, result.ErrMsg)
		return fmt.Errorf("wecom sender: errcode=%d errmsg=%s",
			result.ErrCode, strings.TrimSpace(result.ErrMsg))
	}

	log.InfofContext(ctx, "wecom sender: message sent successfully")
	return nil
}

// truncate shortens a string to maxLen runes for logging.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
