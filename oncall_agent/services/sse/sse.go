// Package sse 包含SSE服务的注册
package sse

import "net/http"

// API 定义了 SSE 服务的接口
type API interface {
	// HandleSSE 处理 SSE 请求
	HandleSSE(w http.ResponseWriter, r *http.Request) error
}
