
# Runbook：Pod CrashLoopBackOff 处置手册

## 现象

Pod 反复重启，`kubectl get pod` 看到 STATUS=CrashLoopBackOff，RESTARTS 计数持续增长。

## 快速定位步骤

1. **拉取最近 1 小时容器退出事件**
   - 调用 `bcs_resource_query(resource=event, namespace=<ns>)`
   - 关注 reason=BackOff / FailedPostStartHook / OOMKilled

2. **查看容器最后日志**
   - 调用 `bk_log_query(service=<service>, keyword=error|panic|exit, last=1h)`
   - 重点看进程退出前 30 秒的日志

3. **检查资源配额**
   - 调用 `bcs_resource_query(resource=pod, name=<pod-name>)`
   - 对比 request/limit 与 node 可用容量

## 常见根因与对策

| 根因 | 现象特征 | 处置 |
|------|---------|------|
| OOMKilled | 最后一行 `container killed (reason: OOMKilled)` | 上调 memory limit，或优化代码内存 |
| 启动依赖未 Ready | 日志中重复 `connection refused` 某下游 | 调 `initialDelaySeconds`，或修依赖 |
| 镜像版本错误 | `ImagePullBackOff` 紧跟其后 | 检查镜像 tag，走 Helm 回滚到上个稳定版本 |
| 配置错误 | 启动即崩，日志 `parse config failed` | 检查 ConfigMap/Secret 挂载 |

## 回滚操作（需 HITL）

确认问题来自最近一次发布后：

1. 先查版本历史：`bcs_helm_manage(action=history, release_name=<r>, cluster_id=<c>, namespace=<n>)`
2. 向用户展示 history 并请求确认要回滚到的 revision
3. 得到确认后：`bcs_helm_manage(action=rollback, revision=<r>, confirmed=true)`
