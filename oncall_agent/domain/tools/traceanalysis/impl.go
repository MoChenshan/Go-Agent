// Package traceanalysis 提供分布式链路追踪分析功能
package traceanalysis

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/log"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

const (
	tagKeyStartTime     = "start.time"
	tagKeyTrpcErrorCode = "trpc.status_code"
	logLevelError       = "ERROR"
	logLevelFatal       = "FATAL"
	defaultDesc         = `trace_analysis 是一个用于分析分布式系统调用链的智能化诊断工具，专门用于解析和可视化tRPC框架的链路追踪数据。
# 核心功能
- 查询由Target服务开始的指定TraceID的调用链详情，包括调用关系、请求和回报详情、耗时、错误码等信息。
- 注意: 为了节省空间，只有主调服务属于会员技术中心-应用开发组的span才会被展开分析，其余的span会被省略，以Call tree truncated for other team's service表示
- 调用链路深度过大时会被截断，若想查询更深的调用链，请传入root_span_id参数，传入后会以该span_id为根节点继续查询调用链
- 本工具还会获取条件日志：魔方主控会根据活动配置来决定请求业务接口前是否需要计算条件，如有会先计算条件结果，若条件通过才会再调业务接口，否则报错。魔方的条件由多个条件组组成，每个条件组包含多个条件。
条件组之间是"或"的关系，条件组内是"与"的关系，每一个条件都关联了一个模块的条件接口。若本次请求计算了魔方条件，则工具会返回条件计算结果。
`
	toolName = "trace_analysis"
)

var (
	spanKindMap = map[string]string{
		"client": "主调",
		"server": "被调",
	}
)

// New 新建traceanalysis工具
func New(dep Dep) tool.Tool {
	desc := defaultDesc
	if dep.WujiCli != nil {
		config := dep.WujiCli.GetLocalToolConfig(toolName)
		if config != nil && config.Description != "" {
			desc = config.Description
		}
	}
	impl := &traceAnalysisImpl{dep: dep}
	return function.NewFunctionTool(
		impl.AnalysisTrace,
		function.WithName(toolName),
		function.WithDescription(desc),
	)
}

type traceAnalysisImpl struct {
	dep Dep
}

// AnalysisTrace 返回给定traceID的整个调用链路，并补充链路中每个服务的概述信息
func (t *traceAnalysisImpl) AnalysisTrace(ctx context.Context, req AnalysisTraceReq) (AnalysisTraceRsp, error) {
	var (
		traceSummary     string
		conditionSummary string
	)
	if err := trpc.GoAndWait(func() error {
		queryTraceRsp, err := t.dep.GalileoCli.QueryTrace(ctx, &domainmodel.QueryTraceReq{
			Target:  "PCG-123." + req.Target,
			TraceID: req.TraceID,
		})
		if err != nil {
			log.ErrorContextf(ctx, "TraceLog error req = %v", queryTraceRsp)
			return err
		}
		serviceNameList := make([]string, 0, len(queryTraceRsp.Trace.Processes))
		for _, process := range queryTraceRsp.Trace.Processes {
			serviceNameList = append(serviceNameList, process.ServiceName)
		}
		lingshanRsp, err := t.dep.LingshanCli.GetSrvDetailByNames(ctx, domainmodel.GetSrvDetailByNamesReq{
			Names: serviceNameList,
		})
		if err != nil {
			log.ErrorContextf(ctx, "GetServiceInfoMap error req = %v", serviceNameList)
			return err
		}
		traceSummary = t.BuildCallTreeString(queryTraceRsp, lingshanRsp.SrvInfoMap, req.RootSpanID)
		return nil
	}, func() error {
		var err error
		conditionSummary, err = t.dep.ConditionLogCli.GetConditionLog(ctx, req.Start, req.End, req.TraceID)
		log.ErrorContextf(ctx, "GetConditionLog err: %+v", err)
		return nil
	}); err != nil {
		return AnalysisTraceRsp{}, err
	}
	return AnalysisTraceRsp{
		TraceSummary:      traceSummary,
		ConditionGroupMap: conditionSummary,
	}, nil
}

// getStartTime 从span的tags中提取start.time值，用于子节点排序
func getStartTime(node *SpanNode) string {
	for _, tag := range node.Span.Tags {
		if tag.Key == tagKeyStartTime {
			return tag.Value
		}
	}
	return ""
}

// isErrorSpan 判断span是否为错误span
func isErrorSpan(span domainmodel.Span) bool {
	for _, tag := range span.Tags {
		if tag.Key == tagKeyTrpcErrorCode && tag.Value != "" && tag.Value != "0" {
			return true
		}
	}
	for _, logEntry := range span.Logs {
		for _, field := range logEntry.Fields {
			if field.Key == "level" {
				level := strings.ToUpper(field.Value)
				if level == logLevelError || level == logLevelFatal {
					return true
				}
			}
		}
	}
	return false
}

// BuildCallTree 构建调用树，返回根节点列表并计算各个节点深度
func BuildCallTree(spans []domainmodel.Span, rootSpanID string) []*SpanNode {
	nodes := make(map[string]*SpanNode)
	for _, span := range spans {
		nodes[span.SpanID] = &SpanNode{
			Span:     span,
			Children: []*SpanNode{},
			Level:    0,
			SpanID:   span.SpanID,
		}
	}
	var rootNodes []*SpanNode

	for _, node := range nodes {
		isRoot := true
		for _, ref := range node.Span.References {
			if ref.RefType == "CHILD_OF" {
				if parent, exists := nodes[ref.SpanID]; exists {
					parent.Children = append(parent.Children, node)
					isRoot = false
				}
			}
		}

		if rootSpanID != "" {
			isRoot = (node.Span.SpanID == rootSpanID)
		}

		if isRoot {
			rootNodes = append(rootNodes, node)
		}
	}

	visited := make(map[string]bool)
	for _, root := range rootNodes {
		queue := []*SpanNode{root}
		visited[root.SpanID] = true
		root.Level = 0

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			sort.Slice(current.Children, func(i, j int) bool {
				return getStartTime(current.Children[i]) < getStartTime(current.Children[j])
			})

			for _, child := range current.Children {
				if !visited[child.SpanID] {
					child.Level = current.Level + 1
					visited[child.SpanID] = true
					queue = append(queue, child)
				}
			}
		}
	}

	return rootNodes
}

// ExtractCallInfoFromNode 从span node中提取调用信息
func ExtractCallInfoFromNode(traceData *domainmodel.QueryTraceRsp, node *SpanNode, serviceInfoMap map[string]*domainmodel.Component) CallInfo {
	span := node.Span
	call := CallInfo{
		OperationName: span.OperationName,
		SpanID:        span.SpanID,
		Logs:          span.Logs,
	}

	if span.Duration != "" {
		if durationMicro, err := strconv.ParseInt(span.Duration, 10, 64); err == nil {
			call.Duration = time.Duration(durationMicro) * time.Microsecond
		}
	}

	if process, exists := traceData.Trace.Processes[span.ProcessID]; exists {
		serviceInfo := serviceInfoMap[process.ServiceName]

		if serviceInfo != nil {
			var domain string
			for _, label := range serviceInfo.BaseInfo.Labels {
				if label.Key == "business_level3_name" {
					domain = label.Value
				}
			}
			call.ServiceName = serviceInfo.BaseInfo.Name
			call.ServiceTeamName = serviceInfo.BaseInfo.TeamName
			call.ServiceDescription = serviceInfo.BaseInfo.Description
			call.ServiceDomain = domain
		}
	}

	for _, tag := range span.Tags {
		switch tag.Key {
		case "trpc.caller_service":
			call.CallerService = tag.Value
		case "trpc.caller_method":
			call.CallerMethod = tag.Value
		case "trpc.callee_service":
			call.CalleeService = tag.Value
		case "trpc.callee_method":
			call.CalleeMethod = tag.Value
		case "trpc.status_code":
			call.StatusCode = tag.Value
		case "trpc.status_msg":
			call.StatusMessage = tag.Value
		case "span.kind":
			call.SpanKind = tag.Value
		case "start.time":
			call.StartTime = tag.Value
		}
	}

	return call
}

// writeNodeHeader writes the node header line to the builder
func writeNodeHeader(builder *strings.Builder, prefix, branch, number, spanKind, serviceName, spanID string, duration time.Duration, isError bool) {
	errorFlag := ""
	if isError {
		errorFlag = "[ERROR] "
	}
	builder.WriteString(fmt.Sprintf("%s%s %s %s%s %s [spanID: %s] (%s)\n",
		prefix, branch, number, errorFlag, spanKindMap[spanKind], serviceName, spanID, duration))
}

// writeNodeDetails writes the node details to the builder
func writeNodeDetails(builder *strings.Builder, detailPrefix string, call CallInfo) {
	builder.WriteString(fmt.Sprintf("%sDescription: %s\n", detailPrefix, call.ServiceDescription))
	builder.WriteString(fmt.Sprintf("%sDomain: %s\n", detailPrefix, call.ServiceDomain))
	if call.CallerService != "" || call.CalleeService != "" {
		builder.WriteString(fmt.Sprintf("%sService: %s → %s\n", detailPrefix, call.CallerService, call.CalleeService))
	}
	if call.CallerMethod != "" || call.CalleeMethod != "" {
		builder.WriteString(fmt.Sprintf("%sMethod: %s → %s\n", detailPrefix, call.CallerMethod, call.CalleeMethod))
	}
	if call.StatusCode != "" {
		builder.WriteString(fmt.Sprintf("%sStatus: %s (%s)\n", detailPrefix, call.StatusCode, call.StatusMessage))
	}
	if call.StartTime != "" {
		builder.WriteString(fmt.Sprintf("%sStart: %s\n", detailPrefix, call.StartTime))
	}
}

// writeNodeLogs writes the logs for a node to the builder
func writeNodeLogs(builder *strings.Builder, detailPrefix string, logs []domainmodel.LogEntry, maxLogLength int) {
	if len(logs) == 0 {
		return
	}
	builder.WriteString(fmt.Sprintf("%sLogs (%d):\n", detailPrefix, len(logs)))
	for i, log := range logs {
		logPrefix := detailPrefix + "  "
		logBranch := "├──"
		if i == len(logs)-1 {
			logBranch = "└──"
		}
		for j, field := range log.Fields {
			if field.Key == "message.uncompressed_size" {
				continue
			}
			if j == 0 {
				builder.WriteString(fmt.Sprintf("%s%s %s: %s\n", logPrefix, logBranch, field.Key,
					rewriteLog(field.Value, maxLogLength)))
			} else {
				builder.WriteString(fmt.Sprintf("%s    %s: %s\n",
					logPrefix, field.Key, rewriteLog(field.Value, maxLogLength)))
			}
		}
	}
}

// checkTruncation checks if the node should be truncated
func checkTruncation(builder *strings.Builder, detailPrefix string, node *SpanNode, call CallInfo, spanNum int, cfg TraceConfig) bool {
	selfTeamName := cfg.SelfTeamName
	if selfTeamName != "" && call.ServiceTeamName != selfTeamName && node.Level > cfg.OtherTeamTruncateDepth {
		builder.WriteString(fmt.Sprintf("%s span主调为其他团队服务，截断\n", detailPrefix))
		return true
	}
	if spanNum > cfg.MaxSpanNum {
		builder.WriteString(fmt.Sprintf("%s 调用树的span数量超过%d, 截断\n", detailPrefix, cfg.MaxSpanNum))
		return true
	}
	if node.Level >= cfg.MaxTraceDepth {
		builder.WriteString(fmt.Sprintf("%s 调用树深度超过%d, 截断\n", detailPrefix, cfg.MaxTraceDepth))
		return true
	}
	return false
}

// summaryEntry 调用链摘要中的单条记录
type summaryEntry struct {
	FromService string // 主调服务
	ToService   string // 被调服务
	SpanID      string
	IsError     bool
}

// collectSummary DFS 收集调用链摘要（只含 serviceName → serviceName + spanID）
func collectSummary(nodes []*SpanNode, traceData *domainmodel.QueryTraceRsp, serviceInfoMap map[string]*domainmodel.Component) []summaryEntry {
	var result []summaryEntry
	for _, node := range nodes {
		call := ExtractCallInfoFromNode(traceData, node, serviceInfoMap)
		from := call.CallerService
		to := call.CalleeService
		if from == "" {
			from = call.ServiceName
		}
		result = append(result, summaryEntry{
			FromService: from,
			ToService:   to,
			SpanID:      node.SpanID,
			IsError:     isErrorSpan(node.Span),
		})
		result = append(result, collectSummary(node.Children, traceData, serviceInfoMap)...)
	}
	return result
}

// errorSummaryEntry 错误span的摘要信息
type errorSummaryEntry struct {
	Number      string
	ServiceName string
	StatusCode  string
	StatusMsg   string
	SpanID      string
}

// collectErrorSpans DFS收集所有错误span
func collectErrorSpans(nodes []*SpanNode, traceData *domainmodel.QueryTraceRsp, serviceInfoMap map[string]*domainmodel.Component, parentNum string) []errorSummaryEntry {
	var result []errorSummaryEntry
	for i, node := range nodes {
		num := fmt.Sprintf("%d", i+1)
		if parentNum != "" {
			num = fmt.Sprintf("%s.%d", parentNum, i+1)
		}
		if isErrorSpan(node.Span) {
			call := ExtractCallInfoFromNode(traceData, node, serviceInfoMap)
			result = append(result, errorSummaryEntry{
				Number:      num,
				ServiceName: call.ServiceName,
				StatusCode:  call.StatusCode,
				StatusMsg:   call.StatusMessage,
				SpanID:      node.SpanID,
			})
		}
		result = append(result, collectErrorSpans(node.Children, traceData, serviceInfoMap, num)...)
	}
	return result
}

// BuildCallTreeString 将trace数据构建调用树，并将结果以字符串形式返回
func (t *traceAnalysisImpl) BuildCallTreeString(traceData *domainmodel.QueryTraceRsp, serviceInfoMap map[string]*domainmodel.Component, rootSpanID string) string {
	rootNodes := BuildCallTree(traceData.Trace.Spans, rootSpanID)

	var (
		builder   strings.Builder
		buildNode func(node *SpanNode, number string, prefix string, isLast bool)
		spanNum   int
	)

	builder.WriteString("=== Trace Call Tree with Logs ===\n")
	builder.WriteString(fmt.Sprintf("Total spans: %d\n\n", len(traceData.Trace.Spans)))

	errorSpans := collectErrorSpans(rootNodes, traceData, serviceInfoMap, "")
	if len(errorSpans) > 0 {
		builder.WriteString(fmt.Sprintf("=== 错误 Span 汇总（%d 个）===\n", len(errorSpans)))
		for _, e := range errorSpans {
			statusInfo := ""
			if e.StatusCode != "" {
				statusInfo = fmt.Sprintf("  status=%s", e.StatusCode)
				if e.StatusMsg != "" {
					statusInfo += fmt.Sprintf("(%s)", e.StatusMsg)
				}
			}
			builder.WriteString(fmt.Sprintf("  - [%s] %s%s  spanID: %s\n", e.Number, e.ServiceName, statusInfo, e.SpanID))
		}
		builder.WriteString("\n")
	}

	// 调用链摘要：只显示 serviceName → serviceName [spanID]
	summaryEntries := collectSummary(rootNodes, traceData, serviceInfoMap)
	if len(summaryEntries) > 0 {
		builder.WriteString("=== 调用链摘要 ===\n")
		for _, e := range summaryEntries {
			errFlag := ""
			if e.IsError {
				errFlag = " [ERROR]"
			}
			if e.ToService != "" {
				builder.WriteString(fmt.Sprintf("  %s → %s%s  [spanID: %s]\n", e.FromService, e.ToService, errFlag, e.SpanID))
			} else {
				builder.WriteString(fmt.Sprintf("  %s%s  [spanID: %s]\n", e.FromService, errFlag, e.SpanID))
			}
		}
		builder.WriteString("\n")
	}

	cfg := t.dep.Cfg
	buildNode = func(node *SpanNode, number string, prefix string, isLast bool) {
		spanNum++
		call := ExtractCallInfoFromNode(traceData, node, serviceInfoMap)
		branch := "├──"
		extend := "│   "
		if isLast {
			branch = "└──"
			extend = "    "
		}

		writeNodeHeader(&builder, prefix, branch, number,
			call.SpanKind, call.ServiceName, node.SpanID, call.Duration, isErrorSpan(node.Span))
		detailPrefix := prefix + extend + "    "
		writeNodeDetails(&builder, detailPrefix, call)
		writeNodeLogs(&builder, detailPrefix, call.Logs, cfg.MaxSpanLogLength)

		if checkTruncation(&builder, detailPrefix, node, call, spanNum, cfg) {
			return
		}
		for i, child := range node.Children {
			childNumber := fmt.Sprintf("%s.%d", number, i+1)
			if number == "" {
				childNumber = fmt.Sprintf("%d", i+1)
			}
			isLastChild := i == len(node.Children)-1
			buildNode(child, childNumber, prefix+extend, isLastChild)
		}
	}
	for i, root := range rootNodes {
		rootNumber := fmt.Sprintf("%d", i+1)
		isLastRoot := i == len(rootNodes)-1
		buildNode(root, rootNumber, "", isLastRoot)
	}
	return builder.String()
}

// rewriteLog 重写请求结构体，使得日志长度可控以及可读
func rewriteLog(s string, maxLen int) string {
	query := gjson.Get(s, "context.cgi_req_data.query").String()
	userInfo := gjson.Get(s, "context.user_info.main_login.user_id").String()
	conditionResult := gjson.Get(s, "condition_result").String()
	if query != "" && userInfo != "" {
		logMsg := fmt.Sprintf("改写后的魔方请求, 请求参数: %s, vuid: %s", query, userInfo)
		if conditionResult != "" {
			logMsg += fmt.Sprintf(", 条件计算结果: %s", conditionResult)
		}
		return logMsg
	}
	if len(s) <= maxLen {
		return s
	}
	return s[0:maxLen] + "...(日志过长截断)"
}
