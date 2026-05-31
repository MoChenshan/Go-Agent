// Package integration 跨模块端到端集成测试。
//
// # 为什么独立成包
//
// D16~D19.2 九个阶段，每个阶段都在自己的包里做了单元测试充分覆盖。但跨包、
// 跨模块的"接缝处"——A 模块如何把一个值传给 B 模块、B 模块在什么假设下工作
// ——往往是单测捕捉不到的盲区。这个包就是专门盯这些缝隙。
//
// # 为什么不做真 E2E
//
// 真 E2E（起二进制、拉 OTLP Collector、Mock BCS/BK HTTP 服务端）代价极高：
// - CI 从秒级变分钟级；
// - 信号噪声比低（失败常是网络抖动，不是代码逻辑错）；
// - 定位成本高（一条链路几千行代码，出错很难快速锁定）。
//
// 本包的策略：**场景驱动集成测试**——装配真实模块（Report/Audit/Webhook/Async），
// 只把最外围（LLM、HTTP 外呼）换成 in-memory 替身，在一个进程内跑完整业务路径。
// 覆盖 90% 真实集成 bug，0 外部依赖，CI 10 秒跑完。
//
// # 五大场景
//
//   TestIntegration_WebhookDedupePersist         Deduper + FileStore + Audit
//   TestIntegration_AsyncSubmitWaitCancel        AsyncRunner 全生命周期
//   TestIntegration_AsyncQueueLimitRejects       限流边界
//   TestIntegration_SummarizerGeneratesOutcome   Webhook + Summarizer + Report
//   TestIntegration_AuditHMACChainAndVerify      HMAC 链式签名 + 逐条验证 + 篡改检测
package integration
