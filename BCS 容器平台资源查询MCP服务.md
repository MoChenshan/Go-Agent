BCS 容器平台资源查询MCP服务

负责人evanxinli(李鑫)，lemonlluo(罗小其)最后修改于03-06 16:38
346
61
0
0
内部敏感数据，请谨慎使用外部模型！注意数据安全！

BCS容器平台 MCP 服务器
当前仅提供资源查询功能，有其他需求可找evanxinli，lemonlluo 沟通

服务器	功能	典型工具（非全部工具）
bcs-project	项目管理	ListAuthorizedProjects, GetProject
bcs-cluster	集群管理	ListProjectCluster, GetCluster, GetCloud
bcs-helm	Helm资源管理	ListRepository, ListChartV1, ListReleaseV1
bcs-resource	K8s资源管理	ListPo, GetPo, ListDeploy, ListSTS, ListSVC, ListCM
需要查看各MCP详细工具列表以及详情，可自行访问蓝鲸网关MCP市场： https://bkapigw.woa.com/mcp-market 查询相关MCP（sg环境：https://apigw.sg.crosgame.com/mcp-market）

image.png

Woa环境 MCP配置参考如下
年后会提供免配置方法
根据以下方式配置的MCP和本人在蓝鲸容器平台权限一致，对应蓝鲸容器平台权限以及申请可以参考： https://iwiki.woa.com/p/1564453640

获取app_code 和app_secret
如果当前没有应用，则到蓝鲸开发者平台（ https://v3.open.woa.com/developer-center ） 创建蓝鲸开发者应用

image.2.png

蓝鲸开发者中心申请密钥，用于后续mcp配置填入
image.3.png

应用权限申请
应用需要申请对应MCP权限（申请权限后，请自行找evanxinli / lemonlluo审批）
image.5.png

获取ticket
访问 https://bkapigw.woa.com/ 界面

F12 获取ticket
image.4.png

BCS项目相关管理 MCP 配置参考
"bcs-project": {
"type": "streamableHttp",
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-project/application/mcp/",
"headers": {
"X-Bkapi-Authorization": "{\"bk_app_code\": \"<应用code>\", \"bk_app_secret\": \"<应用secret>\", \"bk_ticket\": \"<对应ticket>\"}"
},
"description": "包含BCS项目,命名空间相关资源查询的MCP"
}
BCS集群管理 MCP 配置参考
"bcs-cluster": {
"type": "streamableHttp",
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-cluster/application/mcp/",
"headers": {
"X-Bkapi-Authorization": "{\"bk_app_code\": \"<应用code>\", \"bk_app_secret\": \"<应用secret>\", \"bk_ticket\": \"<对应ticket>\"}"
},
"description": "BCS集群管理MCP，包含集群相关，区域相关资源的查询"
}
BCS资源管理 MCP 配置参考
"bcs-resource": {
"type": "streamableHttp",
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-resource/application/mcp/",
"headers": {
"X-Bkapi-Authorization": "{\"bk_app_code\": \"<应用code>\", \"bk_app_secret\": \"<应用secret>\", \"bk_ticket\": \"<对应ticket>\"}"
},
"description": "BCS资源管理MCP，包含工作负载，ingress，自定义crd等资源的查询"
}
BCS的Helm管理 MCP 配置参考
"bcs-helm": {
"type": "streamableHttp",
"url": "https://bk-apigateway.apigw.o.woa.com/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-helm/application/mcp/",
"headers": {
"X-Bkapi-Authorization": "{\"bk_app_code\": \"<应用code>\", \"bk_app_secret\": \"<应用secret>\", \"bk_ticket\": \"<对应ticket>\"}"
},
"description": "BCS的Helm管理MCP，包含helm，Release资源管理"
}
SG 环境 MCP配置参考如下
根据以下方式配置的MCP和本人在蓝鲸容器平台权限一致，对应蓝鲸容器平台权限以及申请可以参考： https://iwiki.woa.com/p/1564453640

获取app_code 和app_secret
如果当前没有应用，则到蓝鲸开发者平台（sg环境：https://bkpaas.sg.crosgame.com/developer-center） 创建蓝鲸开发者应用

image.2.png

蓝鲸开发者中心申请密钥，用于后续mcp配置填入
image.3.png

应用权限申请
应用需要申请对应MCP权限（申请权限后，请自行找evanxinli / lemonlluo审批）
image.5.png

获取bk_token
访问 https://apigw.sg.crosgame.com/界面

F12 获取bk_token
image.9.png

BCS项目相关管理 MCP 配置参考
"bcs-project": {
"type": "streamableHttp",
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-project/application/mcp/",
"headers": {
"X-Bkapi-Authorization": "{\"bk_app_code\": \"<应用code>\", \"bk_app_secret\": \"<应用secret>\", \"bk_token\": \"<对应bk_token>\"}"
},
"description": "包含BCS项目,命名空间相关资源查询的MCP"
}
BCS集群管理 MCP 配置参考
"bcs-cluster": {
"type": "streamableHttp",
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-cluster/application/mcp/",
"headers": {
"X-Bkapi-Authorization": "{\"bk_app_code\": \"<应用code>\", \"bk_app_secret\": \"<应用secret>\", \"bk_token\": \"<对应bk_token>\"}"
},
"description": "BCS集群管理MCP，包含集群相关，区域相关资源的查询"
}
BCS资源管理 MCP 配置参考
"bcs-resource": {
"type": "streamableHttp",
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-resource/application/mcp/",
"headers": {
"X-Bkapi-Authorization": "{\"bk_app_code\": \"<应用code>\", \"bk_app_secret\": \"<应用secret>\", \"bk_token\": \"<对应bk_token>\"}"
},
"description": "BCS资源管理MCP，包含工作负载，ingress，自定义crd等资源的查询"
}
BCS的Helm管理 MCP 配置参考
"bcs-helm": {
"type": "streamableHttp",
"url": "https://bkapi.sg.crosgame.com/api/bk-apigateway/prod/api/v2/mcp-servers/bcs-api-gateway-mcp-helm/application/mcp/",
"headers": {
"X-Bkapi-Authorization": "{\"bk_app_code\": \"<应用code>\", \"bk_app_secret\": \"<应用secret>\", \"bk_token\": \"<对应bk_token>\"}"
},
"description": "BCS的Helm管理MCP，包含helm，Release资源管理"
}
常见问题
MCP配置后一直处于loading 状态
image.6.png

解决方法
应用未申请MCP权限，请参考上面章节申请权限，并让审批人进行审批

MCP配置后报错405（no available）
image.10.png

解决方法
当前在knot 以及 Intellj ide上的 codebuddy 插件上 配置MCP时需要在MCP中新增以下配置

"transportType": "streamable-http"
实际应用场景参考
提问 1: 查询当前 BCS 的项目列表
用户请求:

帮我查询我当前BCS的项目列表
AI 执行内容:

尝试使用 ListProjects

CallMcpTool({
server: "user-bcs-project",
toolName: "BCSProject_ListProjects",
arguments: { query_param: { all: true } }
})


结果: ❌ 权限错误 (1405403)
切换到 ListAuthorizedProjects

读取工具定义：

/root/.cursor/projects/root-code-bk-bcs/mcps/user-bcs-project/tools/
BCSProject_ListAuthorizedProjects.json


调用：
CallMcpTool({
server: "user-bcs-project",
toolName: "BCSProject_ListAuthorizedProjects",
arguments: { query_param: { all: true } }
})


结果: ✅ 成功返回项目列表
返回的项目信息

{
"data": [
{
"projectID": "testprojectliu",
"projectCode": "testprojectliu",
"name": "测试项目",
"businessID": "100001",
"createTime": "...",
"permissions": {
"cluster_create": true,
"cluster_view": true,
"cluster_manage": true
}
}
// ... 其他项目
]
}
关键收获:

🔑 权限问题: 普通用户应使用 ListAuthorizedProjects 而非 ListProjects

🔑 参数命名: 注意 projectID 和 projectCode 的区别

提问 2: 查询项目 testprojectliu 的集群列表
用户请求:

查询项目 testprojectliu 的集群列表
AI 执行内容:

读取工具定义

ClusterManager_ListProjectCluster.json

调用工具

CallMcpTool({
server: "user-bcs-cluster",
toolName: "ClusterManager_ListProjectCluster",
arguments: {
path_param: {
projectID: "testprojectliu"
}
}
})

返回的集群列表

{
"data": [
{
"clusterID": "BCS-K8S-15965",
"clusterName": "UAT主环境",
"region": "ap-shanghai",
"status": "RUNNING",
"clusterType": "single",
"environment": "stag",
"creator": "admin",
"createTime": "...",
"clusterBasicSettings": {
"version": "1.24.4"
}
},
{
"clusterID": "BCS-K8S-15967",
"clusterName": "测试环境",
"region": "ap-shanghai",
"status": "RUNNING",
"environment": "debug"
}
]
}

查询云平台详情

CallMcpTool({
server: "user-bcs-cluster",
toolName: "ClusterManager_GetCloud",
arguments: {
path_param: {
cloudID: "tencentCloud"
}
}
})


返回了腾讯云 TKE 的详细配置信息。
关键信息:

项目有 2 个集群：UAT主环境 (15965) 和测试环境 (15967)

集群版本: Kubernetes 1.24.4

云平台: 腾讯云 TKE

提问 3: 查询项目的 Helm 仓库列表
用户请求:

查询该项目的仓库列表
AI 执行内容:

读取工具定义

HelmManager_ListRepository.json


注意到参数名是 projectCode（不是 projectID）
调用工具

CallMcpTool({
server: "user-bcs-helm",
toolName: "HelmManager_ListRepository",
arguments: {
path_param: {
projectCode: "testprojectliu"
}
}
})

返回的仓库列表

{
"data": [
{
"name": "testprojectliu",
"type": "HELM",
"url": "https://helm.bkbcs.tencent.com/testprojectliu",
"createTime": "..."
}
]
}
关键发现:

🔑 参数差异: Helm 相关操作使用 projectCode，而不是 projectID

项目有一个同名的 Helm 仓库

提问 4: 查询 Helm Chart 包和 Release
用户请求:

查询 testprojectliu 仓库的chart包有什么，以及当前项目中的release
AI 执行内容:

查询 Chart 包列表

读取工具定义：

HelmManager_ListChartV1.json


调用：
CallMcpTool({
server: "user-bcs-helm",
toolName: "HelmManager_ListChartV1",
arguments: {
path_param: {
projectCode: "testprojectliu",
repoName: "testprojectliu"
}
}
})


结果：返回可用的 Chart 包列表
{
"data": [
{
"name": "nginx",
"latestVersion": "13.2.21",
"latestAppVersion": "1.23.2",
"description": "NGINX Open Source"
},
{
"name": "redis",
"latestVersion": "17.3.14",
"latestAppVersion": "7.0.5",
"description": "Redis key-value store"
}
]
}

查询已部署的 Release

读取工具定义：

HelmManager_ListReleaseV1.json


需要 projectCode 和 clusterID 两个参数。

调用（针对测试集群）：
CallMcpTool({
server: "user-bcs-helm",
toolName: "HelmManager_ListReleaseV1",
arguments: {
path_param: {
projectCode: "testprojectliu",
clusterID: "BCS-K8S-15967"
}
}
})


结果：返回已部署的 Release
{
"data": [
{
"name": "my-nginx",
"namespace": "default",
"revision": 1,
"status": "deployed",
"chart": "nginx-13.2.21",
"appVersion": "1.23.2"
}
]
}
关键信息:

仓库中有 nginx、redis 等 Chart 包

测试集群中部署了 my-nginx Release

提问 5: 查询 UAT 主集群的命名空间和 Pod
用户请求:

查询UAT主集群的命名空间，以及命名空间下的pod
AI 执行内容:

查询命名空间列表

读取工具定义：

Namespace_ListNamespaces.json


调用：
CallMcpTool({
server: "user-bcs-project",
toolName: "Namespace_ListNamespaces",
arguments: {
path_param: {
projectCode: "testprojectliu",
clusterID: "BCS-K8S-15965"
}
}
})


结果：返回命名空间列表
{
"data": [
{ "name": "default" },
{ "name": "kube-system" },
{ "name": "kube-public" },
{ "name": "bcs-system" },
{ "name": "istio-system" },
{ "name": "nginx" },
{ "name": "thanos" },
{ "name": "bkmonitor-operator" }
]
}

查询各命名空间的 Pod

读取工具定义：

Workload_ListPo.json


对几个关键命名空间查询 Pod：

default 命名空间:
CallMcpTool({
server: "user-bcs-resource",
toolName: "Workload_ListPo",
arguments: {
path_param: {
projectID: "testprojectliu",
clusterID: "BCS-K8S-15965",
namespace: "default"
}
}
})


kube-system 命名空间:
返回了系统组件 Pod（coredns、kube-proxy 等）

bcs-system 命名空间:

返回了 BCS 系统组件 Pod

Pod 状态统计

default: 5 个 Pod (全部 Running)
kube-system: 20+ 个 Pod (全部 Running)
bcs-system: 10+ 个 Pod (全部 Running)
nginx: 4 个 Pod (3 Running, 1 Pending)
发现:

⚠️ nginx 命名空间有异常 Pod，需要进一步诊断

提问 6: 查询 nginx 命名空间的资源列表
用户请求:

帮我查询nginx命名空间的资源列表
AI 执行内容:

查询 Pod 列表

CallMcpTool({
server: "user-bcs-resource",
toolName: "Workload_ListPo",
arguments: {
path_param: {
projectID: "testprojectliu",
clusterID: "BCS-K8S-15965",
namespace: "nginx"
}
}
})


结果：
{
"manifest": {
"items": [
{
"metadata": { "name": "nginx-68698c5898-299rh" },
"status": { "phase": "Pending" }
},
{
"metadata": { "name": "redis-master-0" },
"status": { "phase": "Pending" }
},
{
"metadata": { "name": "redis-replicas-0" },
"status": { "phase": "Pending" }
},
{
"metadata": { "name": "bk-log-collector-xxx" },
"status": { "phase": "Running" }
}
]
}
}

查询 Deployment 列表

读取工具定义：

Workload_ListDeploy.json


调用：
CallMcpTool({
server: "user-bcs-resource",
toolName: "Workload_ListDeploy",
arguments: {
path_param: {
projectID: "testprojectliu",
clusterID: "BCS-K8S-15965",
namespace: "nginx"
}
}
})


结果：
{
"manifest": {
"items": [
{
"metadata": { "name": "nginx" },
"spec": {
"replicas": 1,
"template": {
"spec": {
"containers": [{
"name": "nginx",
"image": "ocker.io/bitnami/nginx:1.23.2"
}]
}
}
},
"status": {
"replicas": 1,
"readyReplicas": 0
}
}
]
}
}

查询 StatefulSet 列表

读取工具定义：

Workload_ListSTS.json


调用后返回：
{
"manifest": {
"items": [
{
"metadata": { "name": "redis-master" },
"spec": {
"replicas": 1,
"volumeClaimTemplates": [
{
"metadata": { "name": "redis-data" },
"spec": {
"storageClassName": "cbs",
"accessModes": ["ReadWriteOnce"],
"resources": {
"requests": { "storage": "8Gi" }
}
}
}
]
}
},
{
"metadata": { "name": "redis-replicas" },
"spec": { "replicas": 1 }
}
]
}
}

查询 Service 列表

读取工具定义：

Network_ListSVC.json


返回了 4 个 Service：
nginx (ClusterIP)

redis-master-headless (Headless)

redis-master (ClusterIP)

redis-replicas-headless (Headless)

查询 ConfigMap 列表

读取工具定义：

Config_ListCM.json


返回了 3 个 ConfigMap：
nginx-config

redis-configuration

redis-health

资源统计:

Pods: 4 个 (1 Running, 3 Pending)
Deployments: 1 个 (nginx)
StatefulSets: 2 个 (redis-master, redis-replicas)
Services: 4 个
ConfigMaps: 3 个
发现的问题:

⚠️ nginx Deployment 镜像名可能有误：ocker.io vs docker.io

⚠️ redis StatefulSet 的 Pod 处于 Pending 状态

提问 7: 确认 nginx Pod 的异常原因
用户请求:

帮我确认nginx的pod异常原因
AI 执行内容:

获取 nginx Pod 详细信息

读取工具定义：

Workload_GetPo.json


调用：
CallMcpTool({
server: "user-bcs-resource",
toolName: "Workload_GetPo",
arguments: {
path_param: {
projectID: "testprojectliu",
clusterID: "BCS-K8S-15965",
namespace: "nginx",
name: "nginx-68698c5898-299rh"
}
}
})


结果：
{
"manifest": {
"status": {
"phase": "Pending",
"conditions": [
{
"type": "PodScheduled",
"status": "True"
}
],
"containerStatuses": [
{
"name": "nginx",
"state": {
"waiting": {
"reason": "ImagePullBackOff",
"message": "Back-off pulling image \"ocker.io/bitnami/nginx:1.23.2-debian-11-r11\""
}
},
"ready": false,
"restartCount": 0,
"image": "ocker.io/bitnami/nginx:1.23.2-debian-11-r11"
}
]
}
}
}


诊断结果:
❌ 问题: ImagePullBackOff

❌ 根因: 镜像名拼写错误 - ocker.io 应为 docker.io

获取 redis-master-0 详细信息

调用：

CallMcpTool({
server: "user-bcs-resource",
toolName: "Workload_GetPo",
arguments: {
path_param: {
projectID: "testprojectliu",
clusterID: "BCS-K8S-15965",
namespace: "nginx",
name: "redis-master-0"
}
}
})


结果：
{
"manifest": {
"status": {
"phase": "Pending",
"conditions": [
{
"type": "PodScheduled",
"status": "False",
"reason": "Unschedulable",
"message": "0/3 nodes are available: 3 pod has unbound immediate PersistentVolumeClaims."
}
]
}
}
}


诊断结果:
❌ 问题: Pending (Unschedulable)

❌ 根因: PVC 未绑定 - StatefulSet 需要的 PersistentVolumeClaim 没有可用的 PV

获取 redis-replicas-0 详细信息

同样的问题：PVC 未绑定

问题汇总:

Pod	状态	问题类型	根因
nginx-68698c5898-299rh	Pending	ImagePullBackOff	镜像名拼写错误 (ocker.io)
redis-master-0	Pending	Unschedulable	PVC 未绑定
redis-replicas-0	Pending	Unschedulable	PVC 未绑定
修复建议:

问题 1: ImagePullBackOff - 镜像名错误

# 修正镜像名称
kubectl set image deployment/nginx \
nginx=docker.io/bitnami/nginx:1.23.2-debian-11-r11 \
-n nginx
问题 2: PVC 未绑定

方案 1: 配置动态供应

# 创建支持动态供应的 StorageClass
kubectl apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
name: cbs-dynamic
provisioner: com.tencent.cloud.csi.cbs
parameters:
diskType: CLOUD_PREMIUM
diskChargeType: POSTPAID_BY_HOUR
allowVolumeExpansion: true
volumeBindingMode: WaitForFirstConsumer
EOF

# 删除现有 StatefulSet 和 PVC
kubectl delete sts redis-master -n nginx
kubectl delete pvc redis-data-redis-master-0 -n nginx

# 修改 StatefulSet 使用新的 StorageClass 后重新创建
方案 2: 手动创建 PV

kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolume
metadata:
name: pv-redis-master-0
spec:
capacity:
storage: 8Gi
accessModes:
- ReadWriteOnce
persistentVolumeReclaimPolicy: Retain
storageClassName: cbs
csi:
driver: com.tencent.cloud.csi.cbs
volumeHandle: disk-xxxxx  # 需要实际的 CBS 盘 ID
fsType: ext4
EOF
方案 3: 使用 emptyDir（测试环境）

# 修改 StatefulSet 使用 emptyDir 而非 PVC
kubectl edit sts redis-master -n nginx
# 将 volumeClaimTemplates 改为 volumes.emptyDir