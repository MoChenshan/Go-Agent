package wecom

import (
	"sync"
	"time"
)

const (
	// defaultAggregateWindow 是默认的消息聚合时间窗口。
	// 用户在企微中同时发送文件+文字时，企微会拆分为两条独立回调（间隔约 400ms），
	// 2s 窗口可以可靠地捕获这些消息。可通过插件配置覆盖：aggregate_window: "1s"
	defaultAggregateWindow = 2 * time.Second

	// defaultTextAggregateWindow keeps text-only chats responsive while
	// still leaving a short window for the common "caption then
	// attachment" split delivery pattern.
	defaultTextAggregateWindow = 500 * time.Millisecond

	// defaultAggregateSingleAttachmentSettleWindow keeps a little more
	// slack after the first attachment lands. WeCom can deliver
	// "file + text + file" bursts with sub-second gaps, and flushing too
	// early turns the trailing file into a second request.
	defaultAggregateSingleAttachmentSettleWindow = 800 *
		time.Millisecond

	// defaultAggregateMultiAttachmentSettleWindow shortens the wait once
	// we already have multiple attachments in the same batch.
	defaultAggregateMultiAttachmentSettleWindow = 350 *
		time.Millisecond
)

// messageAggregator 按时间窗口收集同一用户/会话的多条消息，合并后批量投递。
//
// 问题：用户在企微中发送文件+文字时，企微会拆分为两条独立回调（间隔约 1s）。
// 如果不做聚合，Agent 会将它们当作两个独立请求分别处理。
//
// 方案：按 user+chat 维度缓冲消息，在安静窗口（aggregateWindow）到期后，
// 将缓冲的消息批量交给 handler 处理。
type messageAggregator struct {
	mu                  sync.Mutex
	window              time.Duration
	textFastPathEnabled bool
	pending             map[string]*pendingBatch // 按聚合 key（chatID + userID）索引
}

// pendingBatch 保存某个聚合 key 下缓冲的消息。
type pendingBatch struct {
	messages []WebhookMessage
	timer    *time.Timer
	onFlush  func([]WebhookMessage) // 最新的 onFlush 回调（每次 Add 时更新）
}

// newMessageAggregator 创建一个新的聚合器。如果 window <= 0，则禁用聚合（消息立即投递）。
func newMessageAggregator(window time.Duration) *messageAggregator {
	return newMessageAggregatorWithTextFastPath(window, true)
}

func newMessageAggregatorWithTextFastPath(
	window time.Duration,
	enabled bool,
) *messageAggregator {
	return &messageAggregator{
		window:              window,
		textFastPathEnabled: enabled,
		pending:             make(map[string]*pendingBatch),
	}
}

// aggregationKey 返回用于消息分组的 key。同一会话中同一用户的消息会被分到同一组。
func aggregationKey(msg WebhookMessage) string {
	userID := msg.From.UserID
	if userID == "" {
		userID = msg.From.Alias
	}
	if msg.ChatID != "" {
		return msg.ChatID + ":" + userID
	}
	return "dm:" + userID
}

// Add 缓冲一条消息并调度批量投递。
// 当聚合窗口到期且没有新消息时，onFlush 会被调用并传入该 key 下所有收集的消息。
//
// onFlush 在新的 goroutine 中调用，调用方需自行处理并发控制。
func (a *messageAggregator) Add(msg WebhookMessage, onFlush func([]WebhookMessage)) {
	// 聚合已禁用：立即同步投递。
	if a.window <= 0 {
		onFlush([]WebhookMessage{msg})
		return
	}

	key := aggregationKey(msg)

	a.mu.Lock()
	defer a.mu.Unlock()

	batch, exists := a.pending[key]
	if exists {
		// 追加到已有批次，更新 onFlush（最新调用方拥有最新的 context/response_url），并重置定时器。
		batch.messages = append(batch.messages, msg)
		batch.onFlush = onFlush
		batch.timer.Reset(a.flushDelay(batch.messages))
		return
	}

	// 新批次：启动定时器。
	batch = &pendingBatch{
		messages: []WebhookMessage{msg},
		onFlush:  onFlush,
	}
	batch.timer = time.AfterFunc(a.flushDelay(batch.messages), func() {
		a.flush(key)
	})
	a.pending[key] = batch
}

func (a *messageAggregator) flushDelay(
	messages []WebhookMessage,
) time.Duration {
	if a.textFastPathEnabled &&
		shouldUseTextFastPath(messages) {
		return minDuration(
			a.window,
			defaultTextAggregateWindow,
		)
	}
	if shouldUseAggregateSettle(messages) {
		return minDuration(
			a.window,
			aggregateSettleWindow(messages),
		)
	}
	return a.window
}

func aggregateSettleWindow(messages []WebhookMessage) time.Duration {
	if countAttachments(messages) <= 1 {
		return defaultAggregateSingleAttachmentSettleWindow
	}
	return defaultAggregateMultiAttachmentSettleWindow
}

func shouldUseTextFastPath(messages []WebhookMessage) bool {
	if len(messages) == 0 {
		return false
	}

	hasText := false
	for _, msg := range messages {
		if messageHasAttachment(msg) {
			return false
		}
		if messageHasText(msg) {
			hasText = true
		}
	}
	return hasText
}

func shouldUseAggregateSettle(messages []WebhookMessage) bool {
	hasText := false
	hasAttachment := false
	for _, msg := range messages {
		hasText = hasText || messageHasText(msg)
		hasAttachment = hasAttachment || messageHasAttachment(msg)
		if hasText && hasAttachment {
			return true
		}
	}
	return false
}

func countAttachments(messages []WebhookMessage) int {
	total := 0
	for _, msg := range messages {
		total += attachmentCount(msg)
	}
	return total
}

func attachmentCount(msg WebhookMessage) int {
	switch msg.MsgType {
	case MsgTypeImage, MsgTypeFile, MsgTypeVideo,
		MsgTypeLocation, MsgTypeLink:
		return 1
	case MsgTypeMixed:
		total := 0
		for _, item := range msg.MixedMessage.MsgItem {
			if item.MsgType != MsgTypeText {
				total++
			}
		}
		return total
	default:
		return 0
	}
}

func messageHasText(msg WebhookMessage) bool {
	switch msg.MsgType {
	case MsgTypeText:
		return msg.Text.Content != ""
	case MsgTypeVoice:
		return msg.Voice.Content != ""
	case MsgTypeMixed:
		for _, item := range msg.MixedMessage.MsgItem {
			if item.MsgType == MsgTypeText &&
				item.Text.Content != "" {
				return true
			}
		}
	}
	return false
}

func messageHasAttachment(msg WebhookMessage) bool {
	switch msg.MsgType {
	case MsgTypeImage, MsgTypeFile, MsgTypeVideo,
		MsgTypeLocation, MsgTypeLink:
		return true
	case MsgTypeMixed:
		for _, item := range msg.MixedMessage.MsgItem {
			if item.MsgType != MsgTypeText {
				return true
			}
		}
	}
	return false
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= b {
		return a
	}
	return b
}

// flush 从 pending 中移除一个批次并通过 onFlush 投递。
func (a *messageAggregator) flush(key string) {
	a.mu.Lock()
	batch, exists := a.pending[key]
	if !exists {
		a.mu.Unlock()
		return
	}
	msgs := batch.messages
	onFlush := batch.onFlush
	delete(a.pending, key)
	a.mu.Unlock()

	onFlush(msgs)
}

// mergeMessages 将多条 WebhookMessage 合并为一条。
// 合并后的消息以第一条消息为基础：
// - 收集所有文本内容
// - 收集所有多媒体内容（图片、文件等）
// - 使用最后一条消息的 AI Bot response_url（最新的有效）
func mergeMessages(msgs []WebhookMessage) WebhookMessage {
	if len(msgs) == 0 {
		return WebhookMessage{}
	}
	if len(msgs) == 1 {
		return msgs[0]
	}

	// 以第一条消息为基础。
	merged := msgs[0]

	// 从所有消息中收集内容项，合并为 mixed 类型消息。
	var items []MixedMsgItem

	for _, msg := range msgs {
		switch msg.MsgType {
		case MsgTypeText:
			items = append(items, MixedMsgItem{
				MsgType: MsgTypeText,
				Text:    msg.Text,
			})
		case MsgTypeImage:
			items = append(items, MixedMsgItem{
				MsgType: MsgTypeImage,
				Image:   msg.Image,
			})
		case MsgTypeFile:
			if url := msg.File.URL; url != "" {
				items = append(items, MixedMsgItem{
					MsgType: MsgTypeFile,
					File:    msg.File,
				})
			}
		case MsgTypeVoice:
			if content := msg.Voice.Content; content != "" {
				items = append(items, MixedMsgItem{
					MsgType: MsgTypeText,
					Text:    TextContent{Content: content},
				})
			}
		case MsgTypeMixed:
			// 展平嵌套的 mixed 消息。
			items = append(items, msg.MixedMessage.MsgItem...)
		default:
			// 其他类型尽力转换为文本（兜底处理）。
		}

		// 使用最后一条消息的 response_url（AI Bot 模式下有效）。
		if msg.ResponseURL != "" {
			merged.ResponseURL = msg.ResponseURL
		}
	}

	// 设置为 mixed 类型并填入所有收集的内容项。
	merged.MsgType = MsgTypeMixed
	merged.MixedMessage = MixedMessageContent{MsgItem: items}

	// 清除单类型字段，因为现在是 mixed 消息。
	merged.Text = TextContent{}
	merged.Image = ImageContent{}
	merged.File = FileContent{}
	merged.Voice = VoiceContent{}

	return merged
}
