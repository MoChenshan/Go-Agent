package wecom

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

const (
	gatewayErrorIDLabel       = "错误ID: "
	gatewayErrorIDLength      = 10
	unknownGatewayErrorID     = "unknown"
	gatewayErrorLogRawUnknown = "<empty>"
)

func sanitizeGatewayErrorMessage(
	message string,
	requestID string,
) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		trimmed = defaultGatewayErrorMessage
	}
	return appendGatewayErrorID(trimmed, requestID)
}

func appendGatewayErrorID(message, requestID string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		trimmed = defaultGatewayErrorMessage
	}
	if strings.Contains(trimmed, gatewayErrorIDLabel) {
		return trimmed
	}
	return trimmed + "\n" + gatewayErrorIDLabel +
		publicGatewayErrorID(requestID)
}

func publicGatewayErrorID(requestID string) string {
	trimmed := strings.TrimSpace(requestID)
	if trimmed == "" {
		return unknownGatewayErrorID
	}
	sum := sha1.Sum([]byte(trimmed))
	hexSum := hex.EncodeToString(sum[:])
	if len(hexSum) < gatewayErrorIDLength {
		return hexSum
	}
	return hexSum[:gatewayErrorIDLength]
}

func logGatewayFailure(
	ctx context.Context,
	source string,
	requestID string,
	rawText string,
	err error,
) {
	trimmed := strings.TrimSpace(rawText)
	if trimmed == "" {
		trimmed = gatewayErrorLogRawUnknown
	}
	if err != nil {
		log.WarnfContext(
			ctx,
			"wecom: %s failed: request_id=%s public_error_id=%s "+
				"err=%v raw=%q",
			source,
			requestID,
			publicGatewayErrorID(requestID),
			err,
			trimmed,
		)
		return
	}
	log.WarnfContext(
		ctx,
		"wecom: %s failed: request_id=%s public_error_id=%s "+
			"raw=%q",
		source,
		requestID,
		publicGatewayErrorID(requestID),
		trimmed,
	)
}
