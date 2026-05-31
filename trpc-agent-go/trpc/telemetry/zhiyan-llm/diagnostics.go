package zhiyanllm

import (
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

type transformDiagnostics struct {
	spanName                  string
	attributeValueLengthLimit int
}

func logJSONUnmarshalFailure(diagnostics transformDiagnostics, attributeKey string, valueLength int, err error) {
	if err == nil {
		return
	}

	if likelyTruncatedJSON(err, valueLength, diagnostics.attributeValueLengthLimit) {
		log.Warnf(
			"zhiyan-llm: failed to parse span attribute as JSON; value may be truncated by AttributeValueLengthLimit, span=%q attribute=%q value_length=%d limit=%d err=%v; increase attribute_value_length_limit or WithAttributeValueLengthLimit if full LLM input/output is required",
			diagnostics.spanName,
			attributeKey,
			valueLength,
			diagnostics.attributeValueLengthLimit,
			err,
		)
		return
	}

	log.Warnf(
		"zhiyan-llm: failed to parse span attribute as JSON, span=%q attribute=%q value_length=%d limit=%d err=%v",
		diagnostics.spanName,
		attributeKey,
		valueLength,
		diagnostics.attributeValueLengthLimit,
		err,
	)
}

func logJSONMarshalFailure(diagnostics transformDiagnostics, attributeKey string, itemCount int, err error) {
	if err == nil {
		return
	}

	log.Warnf(
		"zhiyan-llm: failed to serialize derived span attribute, span=%q attribute=%q item_count=%d err=%v",
		diagnostics.spanName,
		attributeKey,
		itemCount,
		err,
	)
}

func likelyTruncatedJSON(err error, valueLength, attributeValueLengthLimit int) bool {
	if err == nil {
		return false
	}
	if attributeValueLengthLimit > 0 && valueLength >= attributeValueLengthLimit {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected EOF") || strings.Contains(msg, "unexpected end of JSON input")
}
