// Package app 内的 shutdown 流程独立到本文件，避免与装配逻辑（app.go）耦合。
package app

import (
	"context"
	"log"
	"time"
)

// Shutdown 优雅停止 App 持有的所有有状态资源。
//
// 调用顺序按 "外层先停 → 内层后停" 设计：
//
//	1. AsyncRunner          停止接受新 job + 等 in-flight job 完成
//	2. MetricsPump          停止周期性采样
//	3. AuditRemote          flush in-flight batch（最后写）
//	4. GuardWatcher         停止 fsnotify
//	5. MCPTool              （框架级，无显式 Close 时跳过）
//
// 每一步都受 ctx 超时保护，单步失败不影响后续步骤。
//
// 注意：
//   - 本方法可重入（多次调用对已 Stop 的资源是 no-op）
//   - 调用者负责在调用本方法之前已停止接受新流量（http.Shutdown）
func (a *App) Shutdown(ctx context.Context) {
	if a == nil {
		return
	}

	// 1. AsyncRunner 排空（对 inflight 长任务最友好的步骤）
	if a.AsyncRunner != nil {
		stepCtx, cancel := timeoutFromCtx(ctx, 15*time.Second)
		if err := a.AsyncRunner.Shutdown(stepCtx); err != nil {
			log.Printf("[shutdown] async runner: %v", err)
		} else {
			log.Printf("[shutdown] async runner: drained")
		}
		cancel()
	}

	// 2. MetricsPump 停止采样
	if a.MetricsPump != nil {
		a.MetricsPump.Stop()
		log.Printf("[shutdown] metrics pump: stopped")
	}

	// 3. AuditRemote 写入剩余 batch（与 flush 时间预算紧密相关）
	if a.AuditRemote != nil {
		// RemoteSink.Close 接收 timeout 而非 ctx；从剩余 ctx 计算预算
		timeout := remainingTimeout(ctx, 10*time.Second)
		if err := a.AuditRemote.Close(timeout); err != nil {
			log.Printf("[shutdown] audit remote sink: %v", err)
		} else {
			log.Printf("[shutdown] audit remote sink: flushed")
		}
	}

	// 4. GuardWatcher 停止 fsnotify
	if a.GuardWatcher != nil {
		a.GuardWatcher.Stop()
		log.Printf("[shutdown] guard watcher: stopped")
	}

	// 5. MCPTool：当前接口未暴露 Close，由 GC 处理底层连接
	//    若后续 mcptools.API 增加 Close()，在这里调用即可
}

// timeoutFromCtx 从父 ctx 派生子 ctx，取 min(parent.剩余, fallback) 作为超时。
func timeoutFromCtx(parent context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	t := remainingTimeout(parent, fallback)
	return context.WithTimeout(parent, t)
}

// remainingTimeout 估算 parent 剩余时间，无 deadline 时返回 fallback。
func remainingTimeout(parent context.Context, fallback time.Duration) time.Duration {
	if dl, ok := parent.Deadline(); ok {
		left := time.Until(dl)
		if left > 0 && left < fallback {
			return left
		}
	}
	return fallback
}
