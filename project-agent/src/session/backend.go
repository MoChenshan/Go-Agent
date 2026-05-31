// session 包的 Redis 后端切换。
//
// 设计说明：
//   - trpc-agent-go 框架的 session.Service 已经支持自定义 backend，
//     但生产部署需要一个稳定可用的 Redis 实现。
//   - 框架自带 trpc/storage/redis 适配（仅在内网构建启用），
//     这里通过包级开关让上层代码无需感知"目前是 inmem 还是 redis"。
//
// 用法：
//
//	cfg := session.DefaultConfig()
//	svc := session.NewWithBackend(cfg, model, session.BackendFromEnv())
//
// 默认 BackendFromEnv() 读取 SESSION_BACKEND 环境变量：
//   - ""        → inmem（默认，零依赖可跑）
//   - "inmem"   → inmem
//   - "redis"   → 调用 redis 适配器；当前外网构建下退化为 inmem，
//                 内网真实部署需要打 -tags 'redis' 并提供 SESSION_REDIS_ADDR。
//
// 这样做的好处：
//   - app.Init 不需要按 env 分叉两条装配路径
//   - 单元测试默认走 inmem，CI 零依赖
//   - 生产 K8s 通过 env + build tag 切到 redis，源码无需改动
package session

import (
	"log"
	"os"

	openaimodel "trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// Backend 会话存储后端枚举。
type Backend string

const (
	BackendInMem Backend = "inmem"
	BackendRedis Backend = "redis"
)

// BackendFromEnv 从 SESSION_BACKEND 读取后端。
func BackendFromEnv() Backend {
	switch os.Getenv("SESSION_BACKEND") {
	case "redis":
		return BackendRedis
	default:
		return BackendInMem
	}
}

// NewWithBackend 按 backend 选择具体实现。
//
// 当 backend=redis 但当前构建未启用 redis（外网构建），自动降级为 inmem
// 并打印 WARN 日志，避免线上启动失败。生产构建必须启用对应 build tag。
func NewWithBackend(cfg Config, model *openaimodel.Model, backend Backend) session.Service {
	switch backend {
	case BackendRedis:
		svc := newRedisSession(cfg, model)
		if svc != nil {
			log.Printf("[session] backend=redis addr=%s", os.Getenv("SESSION_REDIS_ADDR"))
			return svc
		}
		log.Printf("[session] WARN: redis backend requested but not compiled, falling back to inmem")
		return New(cfg, model)
	default:
		return New(cfg, model)
	}
}
