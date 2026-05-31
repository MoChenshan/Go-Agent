// Package apigw provides API gateway integration functionality.
package apigw

import (
	"fmt"
	"net/http"
	"net/url"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"git.code.oa.com/trpc-go/trpc-go/codec"
	trpchttp "git.code.oa.com/trpc-go/trpc-go/http"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123/internal/filter"
)

// ReleaseStage is the release stage of the api gateway
const ReleaseStage = "RELEASE"

const (
	headerCode         = "X-Gateway-Code"
	headerMsg          = "X-Gateway-Msg"
	headerRequestID    = "X-Gateway-RequestID"
	headerGatewayStage = "X-Gateway-Stage"
	headerSecretID     = "X-Gateway-SecretId"
	headerSecretKey    = "X-Gateway-SecretKey"
)

// ProxyConfig is the config of the proxy
type ProxyConfig struct {
	SecretKey string
	SecretID  string
	Stage     string
}

// ProxyHelper use trpc client to call the api gateway interface
type ProxyHelper struct {
	stage     string
	secretID  string
	secretKey string
}

// NewProxyHelper init the proxy helper, use trpc client to call the api gateway interface
func NewProxyHelper(conf ProxyConfig) (*ProxyHelper, error) {
	helper := &ProxyHelper{
		stage:     conf.Stage,
		secretID:  conf.SecretID,
		secretKey: conf.SecretKey,
	}
	return helper, nil
}

// NewTRPCClientPostOpts generate the options and http response header for trpc client to call the api gateway interface
func (p *ProxyHelper) NewTRPCClientPostOpts(rpcName string) ([]client.Option, *trpchttp.ClientRspHeader) {
	reqHeader := &trpchttp.ClientReqHeader{
		Method: http.MethodPost,
	}
	rspHeader := &trpchttp.ClientRspHeader{}
	reqHeader.AddHeader(headerGatewayStage, p.stage)
	reqHeader.AddHeader(headerSecretID, p.secretID)
	reqHeader.AddHeader(headerSecretKey, p.secretKey)
	clientOpts := []client.Option{
		client.WithReqHead(reqHeader),
		client.WithRspHead(rspHeader),
		client.WithProtocol("http"),
		client.WithSerializationType(codec.SerializationTypeJSON),
		client.WithTarget("dns://api.apigw.oa.com"),
		filter.WithRPCNameOption(rpcName),
	}
	return clientOpts, rspHeader
}

// NewHTTPClientPostOpts generate the options and http response header for http client to call the api gateway interface
func (p *ProxyHelper) NewHTTPClientPostOpts(requestURL string) ([]client.Option, *trpchttp.ClientRspHeader, error) {
	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return nil, nil, err
	}
	reqHeader := &trpchttp.ClientReqHeader{
		Method: http.MethodPost,
	}
	rspHeader := &trpchttp.ClientRspHeader{}
	clientOpts := []client.Option{
		client.WithReqHead(reqHeader),
		client.WithRspHead(rspHeader),
		client.WithProtocol("http"),
		client.WithSerializationType(codec.SerializationTypeJSON),
		client.WithTarget(fmt.Sprintf("dns://%s", parsedURL.Host)),
		filter.WithRPCNameOption(parsedURL.Path),
	}
	return clientOpts, rspHeader, nil
}
