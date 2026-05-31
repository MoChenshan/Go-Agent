//go:build !redis
// +build !redis

// 默认构建路径：不启用 redis 后端。返回 nil 让 backend.go 走降级。
package session

import (
	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// newRedisSession 在外网/普通构建下永远返回 nil，调用方会降级到 inmem。
//
// 真正的 Redis 实现放在 redis_session.go（build tag: redis）。
func newRedisSession(_ Config, _ *openaimodel.Model) session.Service {
	return nil
}
