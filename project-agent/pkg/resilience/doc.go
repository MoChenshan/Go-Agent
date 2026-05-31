// Package resilience 提供生产级韧性原语：
//
//   - Retry      指数退避 + jitter + 错误分类，区分"可重试/不可重试"
//   - Breaker    三态熔断器（Closed/Open/HalfOpen），按错误率/连续失败开断
//   - Bulkhead   信号量隔板，限制单依赖并发上限，防止资源耗尽
//   - RateLimit  令牌桶限流（按 key 维度，支持平滑爆发）
//
// 这些原语彼此独立、可任意组合。Agent 调用外部依赖（LLM/BCS/BK/iWiki）时，
// 推荐组合方式：RateLimit -> Bulkhead -> Breaker -> Retry -> 真实调用。
//
// 参考实现：sony/gobreaker、resilience4j、Polly。
// 故意不引入第三方库，以降低依赖足迹并保留对错误语义的完整控制。
package resilience
