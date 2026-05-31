// ready_waiter_adapter.go D19.8 —— FastPollReadyWaiter 的 observability 装配说明。
//
// # 重要的架构决策
//
// 本文件**故意不定义 FastPollMetricsAdapter 结构体**。
//
// 与 D19.4 的 async_adapter.go 不同，这里需要避免循环 import：
//
//	src/tools/bcs_tools → 已经正向 import src/observability（D19.5 起用于 IncPodReadyWait）
//
// 若 observability 再反向 import bcs_tools（为了获取 FastPollStats 类型），
// 就会形成**导入环**，Go 会拒绝编译。
//
// # 解决方案：让 app 装配层当胶水
//
// D19.4 async 的 adapter 之所以能独立在 observability 里，是因为 async 包
// **不依赖任何项目内的包**（它是"核心能力"包）。bcs_tools 不同，它正向依赖 audit/observability/
// infrastructure 等多个包，不能被反向 import。
//
// 所以 D19.8 的桥接分工如下：
//
//  1. observability 提供独立的打点函数：
//     - IncFastPollFinished(ctx, mode, reason)
//     - IncFastPollProbeBucket(ctx, mode, probeIdx)
//     - ObserveFastPollProbesPer(ctx, mode, reason, probes)
//     （见 metrics_more.go，零依赖 bcs_tools）
//
//  2. app 包实现 bcstools.FastPollMetricsHook 接口的胶水类型：
//     - 胶水类型位于 src/app（已经依赖双向的一切）
//     - 把 OnWaitFinished(mode, stats) 翻译为上述三个函数调用
//
// 这样 bcs_tools 和 observability 的单向依赖关系保持不变，
// 只有"本来就在做装配"的 app 层承担双向依赖，符合 SRP。
//
// 本文件只是个文档锚点，无运行时代码。真正的胶水见 src/app/ready_waiter_glue.go。
package observability

// FastPoll 指标的三个打点函数定义在 metrics_more.go：
//   - IncFastPollFinished
//   - IncFastPollProbeBucket
//   - ObserveFastPollProbesPer
//
// 本文件无需再声明 —— 保留文件作为"为什么不在此定义 adapter"的设计决策记录。