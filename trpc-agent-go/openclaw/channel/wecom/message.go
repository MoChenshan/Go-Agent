package wecom

import "encoding/json"

// Message type constants.
const (
	MsgTypeText     = "text"
	MsgTypeImage    = "image"
	MsgTypeVoice    = "voice"
	MsgTypeVideo    = "video"
	MsgTypeFile     = "file"
	MsgTypeLocation = "location"
	MsgTypeLink     = "link"
	MsgTypeEvent    = "event"
	MsgTypeMixed    = "mixed"
	MsgTypeStream   = "stream"
)

// WebhookMessage represents a decrypted enterprise WeChat webhook callback message.
// Supports: text, image, voice, video, file, location, link, event, mixed, stream.
//
// For AI Bot mode, additional fields (ResponseURL, AIBotID) will be present.
type WebhookMessage struct {
	WebhookURL   string              `json:"webhook_url,omitempty"`
	MsgID        string              `json:"msgid,omitempty"`
	ChatID       string              `json:"chatid,omitempty"`
	ChatType     string              `json:"chattype,omitempty"` // "group" or "single"
	From         FromInfo            `json:"from,omitempty"`
	MsgType      string              `json:"msgtype,omitempty"` // "text", "image", "voice", "video", "file", "location", "link", "event", "mixed", "stream"
	Text         TextContent         `json:"text,omitempty"`
	Image        ImageContent        `json:"image,omitempty"`
	Voice        VoiceContent        `json:"voice,omitempty"`
	Video        VideoContent        `json:"video,omitempty"`
	File         FileContent         `json:"file,omitempty"`
	Location     LocationContent     `json:"location,omitempty"`
	Link         LinkContent         `json:"link,omitempty"`
	Event        EventContent        `json:"event,omitempty"`
	MixedMessage MixedMessageContent `json:"mixed,omitempty"` // AI Bot uses "mixed", not "mixed_message"
	Stream       StreamContent       `json:"stream,omitempty"`
	Quote        *QuoteContent       `json:"quote,omitempty"` // Referenced message (AI Bot)

	// AI Bot specific fields (only present in ai mode)
	ResponseURL string `json:"response_url,omitempty"` // Dynamic URL for replying (AI Bot only)
	AIBotID     string `json:"aibotid,omitempty"`      // AI Bot ID (AI Bot only)

	CallbackReqID string          `json:"-"`
	ReplyWriter   wsReplyWriter   `json:"-"`
	RawBody       json.RawMessage `json:"-"`
}

// FromInfo contains sender information.
type FromInfo struct {
	UserID string `json:"userid,omitempty"`
	Name   string `json:"name,omitempty"`
	Alias  string `json:"alias,omitempty"`
}

// TextContent contains text message payload.
type TextContent struct {
	Content       string   `json:"content,omitempty"`
	MentionedList []string `json:"mentioned_list,omitempty"`
}

// ImageContent contains image message payload.
type ImageContent struct {
	URL    string `json:"url,omitempty"`
	AESKey string `json:"aeskey,omitempty"`
}

// VoiceContent contains voice message payload.
// Note: For AI Bot, voice messages return transcribed text in Content field.
// For Notification Bot, voice messages return media_id for download.
type VoiceContent struct {
	MediaID string `json:"media_id,omitempty"` // Voice file media ID (Notification Bot)
	Content string `json:"content,omitempty"`  // Transcribed text content (AI Bot)
}

// VideoContent contains video message payload.
type VideoContent struct {
	MediaID      string `json:"media_id,omitempty"`       // Video file media ID
	ThumbMediaID string `json:"thumb_media_id,omitempty"` // Thumbnail media ID
}

// LocationContent contains location message payload.
type LocationContent struct {
	Latitude  float64 `json:"latitude,omitempty"`  // Latitude
	Longitude float64 `json:"longitude,omitempty"` // Longitude
	Name      string  `json:"name,omitempty"`      // Location name
	Address   string  `json:"address,omitempty"`   // Detailed address
	Scale     int     `json:"scale,omitempty"`     // Map zoom level
}

// LinkContent contains link message payload.
type LinkContent struct {
	Title       string `json:"title,omitempty"`       // Link title
	Description string `json:"description,omitempty"` // Link description
	URL         string `json:"url,omitempty"`         // Link URL
	PicURL      string `json:"picurl,omitempty"`      // Preview image URL
}

// EventContent contains event message payload.
type EventContent struct {
	EventType         string             `json:"eventtype,omitempty"` // AI Bot uses "eventtype", Notification Bot uses "event_type"
	TemplateCardEvent *TemplateCardEvent `json:"template_card_event,omitempty"`
	FeedbackEvent     *FeedbackEvent     `json:"feedback_event,omitempty"`
}

// TemplateCardEvent contains interactive template card callbacks.
type TemplateCardEvent struct {
	CardType      string                    `json:"card_type,omitempty"`
	EventKey      string                    `json:"event_key,omitempty"`
	TaskID        string                    `json:"task_id,omitempty"`
	SelectedItems TemplateCardSelectedItems `json:"selected_items,omitempty"`
}

// TemplateCardSelectedItems wraps template card selections.
type TemplateCardSelectedItems struct {
	SelectedItem []TemplateCardSelectedItem `json:"selected_item,omitempty"`
}

// TemplateCardSelectedItem is one submitted card selector value.
type TemplateCardSelectedItem struct {
	QuestionKey string                `json:"question_key,omitempty"`
	OptionIDs   TemplateCardOptionIDs `json:"option_ids,omitempty"`
}

// TemplateCardOptionIDs wraps option IDs for one selector.
type TemplateCardOptionIDs struct {
	OptionID []string `json:"option_id,omitempty"`
}

// FeedbackEvent contains user feedback on a reply.
type FeedbackEvent struct {
	ID                   string `json:"id,omitempty"`
	Type                 int    `json:"type,omitempty"`
	Content              string `json:"content,omitempty"`
	InaccurateReasonList []int  `json:"inaccurate_reason_list,omitempty"`
}

// FileContent contains file message payload (AI Bot only, single chat).
// Note: The URL is encrypted and valid for 5 minutes.
type FileContent struct {
	URL    string `json:"url,omitempty"` // Encrypted file download URL
	AESKey string `json:"aeskey,omitempty"`
}

// StreamContent contains stream message payload for streaming replies.
type StreamContent struct {
	ID string `json:"id,omitempty"` // Stream ID for continuing the stream
}

// QuoteContent contains referenced/quoted message (AI Bot).
type QuoteContent struct {
	MsgType string              `json:"msgtype,omitempty"`
	Text    TextContent         `json:"text,omitempty"`
	Image   ImageContent        `json:"image,omitempty"`
	Voice   VoiceContent        `json:"voice,omitempty"`
	File    FileContent         `json:"file,omitempty"`
	Mixed   MixedMessageContent `json:"mixed,omitempty"`
}

// MixedMessageContent contains mixed (text+image) message payload.
type MixedMessageContent struct {
	MsgItem []MixedMsgItem `json:"msg_item,omitempty"`
}

// MixedMsgItem is one item inside a mixed message.
type MixedMsgItem struct {
	MsgType string       `json:"msgtype,omitempty"`
	Text    TextContent  `json:"text,omitempty"`
	Image   ImageContent `json:"image,omitempty"`
	File    FileContent  `json:"file,omitempty"`
}

// EncryptedBody is the outer JSON envelope sent by enterprise WeChat.
type EncryptedBody struct {
	Encrypt string `json:"encrypt"`
}
