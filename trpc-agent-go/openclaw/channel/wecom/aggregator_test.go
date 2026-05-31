package wecom

import (
	"sync"
	"testing"
	"time"
)

func TestAggregationKey(t *testing.T) {
	tests := []struct {
		name string
		msg  WebhookMessage
		want string
	}{
		{
			name: "group chat",
			msg:  WebhookMessage{ChatID: "chat123", From: FromInfo{UserID: "user1"}},
			want: "chat123:user1",
		},
		{
			name: "direct message",
			msg:  WebhookMessage{From: FromInfo{UserID: "user1"}},
			want: "dm:user1",
		},
		{
			name: "alias fallback",
			msg:  WebhookMessage{ChatID: "chat1", From: FromInfo{Alias: "alias1"}},
			want: "chat1:alias1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := aggregationKey(tt.msg)
			if got != tt.want {
				t.Errorf("aggregationKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMessageAggregator_SingleMessage(t *testing.T) {
	agg := newMessageAggregator(100 * time.Millisecond)

	var mu sync.Mutex
	var result []WebhookMessage

	msg := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "hello"},
	}

	agg.Add(msg, func(msgs []WebhookMessage) {
		mu.Lock()
		result = msgs
		mu.Unlock()
	})

	// Wait for aggregation window to expire.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Text.Content != "hello" {
		t.Errorf("expected 'hello', got %q", result[0].Text.Content)
	}
}

func TestMessageAggregator_MergesMultipleMessages(t *testing.T) {
	agg := newMessageAggregator(200 * time.Millisecond)

	var mu sync.Mutex
	var result []WebhookMessage

	textMsg := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "请分析这个文件"},
	}
	fileMsg := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeFile,
		File:    FileContent{URL: "https://example.com/file.pdf"},
	}

	flush := func(msgs []WebhookMessage) {
		mu.Lock()
		result = msgs
		mu.Unlock()
	}

	agg.Add(textMsg, flush)
	time.Sleep(50 * time.Millisecond) // Simulate ~50ms delay between callbacks
	agg.Add(fileMsg, flush)

	// Wait for aggregation window to expire.
	time.Sleep(400 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(result) != 2 {
		t.Fatalf("expected 2 messages in batch, got %d", len(result))
	}
	if result[0].MsgType != MsgTypeText {
		t.Errorf("first message type = %q, want %q", result[0].MsgType, MsgTypeText)
	}
	if result[1].MsgType != MsgTypeFile {
		t.Errorf("second message type = %q, want %q", result[1].MsgType, MsgTypeFile)
	}
}

func TestMessageAggregator_DifferentUsersSeparate(t *testing.T) {
	agg := newMessageAggregator(100 * time.Millisecond)

	var mu sync.Mutex
	batches := make(map[string]int)

	msg1 := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "from user1"},
	}
	msg2 := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user2"},
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "from user2"},
	}

	agg.Add(msg1, func(msgs []WebhookMessage) {
		mu.Lock()
		batches["user1"] = len(msgs)
		mu.Unlock()
	})
	agg.Add(msg2, func(msgs []WebhookMessage) {
		mu.Lock()
		batches["user2"] = len(msgs)
		mu.Unlock()
	})

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if batches["user1"] != 1 {
		t.Errorf("user1 batch size = %d, want 1", batches["user1"])
	}
	if batches["user2"] != 1 {
		t.Errorf("user2 batch size = %d, want 1", batches["user2"])
	}
}

func TestMessageAggregator_EarlySettleTextAndFile(t *testing.T) {
	agg := newMessageAggregator(2 * time.Second)

	flushCh := make(chan []WebhookMessage, 1)
	textMsg := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "请分析这个文件"},
	}
	fileMsg := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeFile,
		File:    FileContent{URL: "https://example.com/file.pdf"},
	}

	agg.Add(textMsg, func(msgs []WebhookMessage) {
		flushCh <- msgs
	})
	time.Sleep(20 * time.Millisecond)
	startedAt := time.Now()
	agg.Add(fileMsg, func(msgs []WebhookMessage) {
		flushCh <- msgs
	})

	select {
	case msgs := <-flushCh:
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		if time.Since(startedAt) > 1200*time.Millisecond {
			t.Fatalf("expected early settle within 1.2s, got %v",
				time.Since(startedAt))
		}
	case <-time.After(time.Second):
		t.Fatal("expected early settle flush")
	}
}

func TestMessageAggregator_WaitsForTrailingAttachmentBurst(
	t *testing.T,
) {
	agg := newMessageAggregator(2 * time.Second)

	flushCh := make(chan []WebhookMessage, 1)
	fileMsg1 := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeFile,
		File:    FileContent{URL: "https://example.com/1.pdf"},
	}
	textMsg := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "合并这两个 pdf"},
	}
	fileMsg2 := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeFile,
		File:    FileContent{URL: "https://example.com/2.pdf"},
	}

	flush := func(msgs []WebhookMessage) {
		flushCh <- msgs
	}

	agg.Add(fileMsg1, flush)
	time.Sleep(20 * time.Millisecond)
	agg.Add(textMsg, flush)

	select {
	case <-flushCh:
		t.Fatal("batch flushed before trailing attachment arrived")
	case <-time.After(450 * time.Millisecond):
	}

	agg.Add(fileMsg2, flush)

	select {
	case msgs := <-flushCh:
		if len(msgs) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(msgs))
		}
		if msgs[0].MsgType != MsgTypeFile {
			t.Fatalf("first message type = %q", msgs[0].MsgType)
		}
		if msgs[1].MsgType != MsgTypeText {
			t.Fatalf("second message type = %q", msgs[1].MsgType)
		}
		if msgs[2].MsgType != MsgTypeFile {
			t.Fatalf("third message type = %q", msgs[2].MsgType)
		}
	case <-time.After(time.Second):
		t.Fatal("expected burst batch to flush")
	}
}

func TestMessageAggregator_TextOnlyUsesFastPath(t *testing.T) {
	agg := newMessageAggregator(2 * time.Second)

	flushCh := make(chan []WebhookMessage, 1)
	textMsg1 := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "第一条"},
	}
	textMsg2 := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "第二条"},
	}

	agg.Add(textMsg1, func(msgs []WebhookMessage) {
		flushCh <- msgs
	})
	time.Sleep(20 * time.Millisecond)
	startedAt := time.Now()
	agg.Add(textMsg2, func(msgs []WebhookMessage) {
		flushCh <- msgs
	})

	select {
	case <-flushCh:
		t.Fatal("text-only batch flushed too early")
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case msgs := <-flushCh:
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		if time.Since(startedAt) >= time.Second {
			t.Fatalf("expected text fast path, got %v",
				time.Since(startedAt))
		}
	case <-time.After(time.Second):
		t.Fatal("expected text-only batch to flush via fast path")
	}
}

func TestMessageAggregator_CanDisableTextFastPath(t *testing.T) {
	agg := newMessageAggregatorWithTextFastPath(
		2*time.Second,
		false,
	)

	flushCh := make(chan []WebhookMessage, 1)
	textMsg := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "第一条"},
	}
	fileMsg := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeFile,
		File:    FileContent{URL: "https://example.com/a.pdf"},
	}

	agg.Add(textMsg, func(msgs []WebhookMessage) {
		flushCh <- msgs
	})

	select {
	case <-flushCh:
		t.Fatal("text batch flushed while fast-path disabled")
	case <-time.After(650 * time.Millisecond):
	}

	agg.Add(fileMsg, func(msgs []WebhookMessage) {
		flushCh <- msgs
	})

	select {
	case msgs := <-flushCh:
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("expected text and file to merge")
	}
}

func TestMessageAggregator_DisabledWindow(t *testing.T) {
	agg := newMessageAggregator(0) // disabled

	callCount := 0

	msg := WebhookMessage{
		ChatID:  "chat1",
		From:    FromInfo{UserID: "user1"},
		MsgType: MsgTypeText,
	}

	// With window=0, onFlush is called synchronously (no aggregation).
	agg.Add(msg, func(msgs []WebhookMessage) {
		callCount++
	})
	agg.Add(msg, func(msgs []WebhookMessage) {
		callCount++
	})

	if callCount != 2 {
		t.Errorf("expected 2 immediate flushes, got %d", callCount)
	}
}

func TestMergeMessages_Single(t *testing.T) {
	msg := WebhookMessage{
		MsgType: MsgTypeText,
		Text:    TextContent{Content: "hello"},
	}
	merged := mergeMessages([]WebhookMessage{msg})
	if merged.MsgType != MsgTypeText {
		t.Errorf("single message should not be modified, got type %q", merged.MsgType)
	}
}

func TestMergeMessages_TextAndFile(t *testing.T) {
	msgs := []WebhookMessage{
		{
			MsgID:   "msg1",
			ChatID:  "chat1",
			MsgType: MsgTypeText,
			Text:    TextContent{Content: "请分析这个文件"},
		},
		{
			MsgID:       "msg2",
			ChatID:      "chat1",
			MsgType:     MsgTypeFile,
			File:        FileContent{URL: "https://example.com/doc.pdf"},
			ResponseURL: "https://response.url/latest",
		},
	}

	merged := mergeMessages(msgs)

	if merged.MsgType != MsgTypeMixed {
		t.Errorf("merged type = %q, want %q", merged.MsgType, MsgTypeMixed)
	}
	if len(merged.MixedMessage.MsgItem) != 2 {
		t.Fatalf("expected 2 items, got %d", len(merged.MixedMessage.MsgItem))
	}
	if merged.MixedMessage.MsgItem[0].MsgType != MsgTypeText {
		t.Errorf("first item type = %q, want text", merged.MixedMessage.MsgItem[0].MsgType)
	}
	if merged.MixedMessage.MsgItem[0].Text.Content != "请分析这个文件" {
		t.Errorf("first item content = %q", merged.MixedMessage.MsgItem[0].Text.Content)
	}
	if merged.MixedMessage.MsgItem[1].MsgType != MsgTypeFile {
		t.Errorf("second item type = %q, want file", merged.MixedMessage.MsgItem[1].MsgType)
	}
	if merged.MixedMessage.MsgItem[1].File.URL !=
		"https://example.com/doc.pdf" {
		t.Errorf("second item url = %q",
			merged.MixedMessage.MsgItem[1].File.URL)
	}
	// Uses last response_url
	if merged.ResponseURL != "https://response.url/latest" {
		t.Errorf("response_url = %q, want last", merged.ResponseURL)
	}
	// Base fields from first message preserved
	if merged.MsgID != "msg1" {
		t.Errorf("base msg_id = %q, want msg1", merged.MsgID)
	}
}

func TestMergeMessages_Empty(t *testing.T) {
	merged := mergeMessages(nil)
	if merged.MsgType != "" {
		t.Errorf("empty merge should return zero value, got type %q", merged.MsgType)
	}
}

func TestMergeMessages_TextAndImage(t *testing.T) {
	msgs := []WebhookMessage{
		{
			MsgType: MsgTypeText,
			Text:    TextContent{Content: "看看这张图"},
		},
		{
			MsgType: MsgTypeImage,
			Image:   ImageContent{URL: "https://example.com/img.jpg"},
		},
	}

	merged := mergeMessages(msgs)

	if merged.MsgType != MsgTypeMixed {
		t.Errorf("merged type = %q, want mixed", merged.MsgType)
	}
	if len(merged.MixedMessage.MsgItem) != 2 {
		t.Fatalf("expected 2 items, got %d", len(merged.MixedMessage.MsgItem))
	}
	if merged.MixedMessage.MsgItem[1].MsgType != MsgTypeImage {
		t.Errorf("second item type = %q, want image", merged.MixedMessage.MsgItem[1].MsgType)
	}
}
