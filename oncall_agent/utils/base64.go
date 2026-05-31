// Package utils 包含通用工具函数（无领域逻辑）
package utils

import (
	"context"
	"encoding/base64"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	base64DecodeName        = "base64_decode"
	base64DecodeDefaultDesc = "此工具用于解码base64字符串"
)

// Base64DecodeReq 解码base64字符串请求
type Base64DecodeReq struct {
	Input string `json:"input" jsonschema:"required,description=需要解码的base64字符串"`
}

// Base64Decode 解码base64字符串
func Base64Decode(_ context.Context, req Base64DecodeReq) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(req.Input)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// NewBase64Tool 新建base64解码工具，desc为空时使用默认描述
func NewBase64Tool(desc string) tool.Tool {
	if desc == "" {
		desc = base64DecodeDefaultDesc
	}
	return function.NewFunctionTool(
		Base64Decode,
		function.WithName(base64DecodeName),
		function.WithDescription(desc),
	)
}
