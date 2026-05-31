package apigw

import (
	"fmt"
	"net/http"

	trpchttp "git.code.oa.com/trpc-go/trpc-go/http"
)

// NewGatewayErr create a gateway error
func NewGatewayErr(err error, rspHeader *trpchttp.ClientRspHeader, interfaceName string) error {
	gatewayCode, gatewayMsg, requestID := parseGatewayResp(rspHeader.Response)
	return fmt.Errorf("proxy error: %v, code: %s, msg: %s, interface: %s, requestID: %s",
		err, gatewayCode, gatewayMsg, interfaceName, requestID)
}

// NewBackendErr create a backend error
func NewBackendErr(code int32, rspHeader *trpchttp.ClientRspHeader, msg, interfaceName string) error {
	_, _, requestID := parseGatewayResp(rspHeader.Response)
	return fmt.Errorf("backend error, code: %d, msg: %s, interface: %s, requestID: %s",
		code, msg, interfaceName, requestID)
}

func parseGatewayResp(rsp *http.Response) (code, msg, requestID string) {
	if rsp != nil {
		gatewayCode := rsp.Header.Get(headerCode)
		gatewayMsg := rsp.Header.Get(headerMsg)
		requestID := rsp.Header.Get(headerRequestID)
		return gatewayCode, gatewayMsg, requestID
	}
	return "", "", ""
}
