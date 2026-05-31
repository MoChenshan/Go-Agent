package wecom

import (
	"encoding/json"
	"strings"
)

const (
	rawMixedFieldMixed   = "mixed"
	rawMixedFieldMsgItem = "msg_item"
	rawMixedFieldMsgType = "msgtype"
	rawMixedFieldText    = "text"
	rawMixedFieldImage   = "image"
	rawMixedFieldFile    = "file"
	rawMixedFieldURL     = "url"
	rawMixedFieldAESKey  = "aeskey"
)

type rawMixedMediaRef struct {
	URL    string
	AESKey string
}

type rawMixedEnvelope struct {
	Mixed rawMixedMessage `json:"mixed,omitempty"`
}

type rawMixedMessage struct {
	MsgItem []map[string]json.RawMessage `json:"msg_item,omitempty"`
}

func unknownMixedMediaRefs(
	msg WebhookMessage,
) []rawMixedMediaRef {
	if msg.MsgType != MsgTypeMixed || len(msg.RawBody) == 0 {
		return nil
	}

	var envelope rawMixedEnvelope
	if err := json.Unmarshal(msg.RawBody, &envelope); err != nil {
		return nil
	}

	if len(envelope.Mixed.MsgItem) == 0 {
		return nil
	}

	refs := make([]rawMixedMediaRef, 0, len(envelope.Mixed.MsgItem))
	for _, item := range envelope.Mixed.MsgItem {
		ref, ok := decodeUnknownMixedMediaRef(item)
		if !ok {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

func decodeUnknownMixedMediaRef(
	item map[string]json.RawMessage,
) (rawMixedMediaRef, bool) {
	if len(item) == 0 {
		return rawMixedMediaRef{}, false
	}

	if knownMixedItemType(rawJSONString(
		item[rawMixedFieldMsgType],
	)) {
		return rawMixedMediaRef{}, false
	}

	if ref, ok := decodeEmbeddedMediaRefFields(item); ok {
		return ref, true
	}

	for key, value := range item {
		switch key {
		case rawMixedFieldMsgType,
			rawMixedFieldText,
			rawMixedFieldImage,
			rawMixedFieldFile:
			continue
		}
		if ref, ok := decodeEmbeddedMediaRef(value); ok {
			return ref, true
		}
	}
	return rawMixedMediaRef{}, false
}

func knownMixedItemType(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case MsgTypeText, MsgTypeImage, MsgTypeFile:
		return true
	default:
		return false
	}
}

func decodeEmbeddedMediaRef(
	raw json.RawMessage,
) (rawMixedMediaRef, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return rawMixedMediaRef{}, false
	}
	return decodeEmbeddedMediaRefFields(fields)
}

func decodeEmbeddedMediaRefFields(
	fields map[string]json.RawMessage,
) (rawMixedMediaRef, bool) {
	url := rawJSONString(fields[rawMixedFieldURL])
	if url == "" {
		return rawMixedMediaRef{}, false
	}
	return rawMixedMediaRef{
		URL:    url,
		AESKey: rawJSONString(fields[rawMixedFieldAESKey]),
	}, true
}

func rawJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}
