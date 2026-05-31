// Package lingshan 实现灵山服务 API 调用，提供组件信息查询能力。
package lingshan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"git.code.oa.com/trpc-go/trpc-go/log"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

const (
	maxPageSize = 100
	baseURL     = "http://api.apigw.oa.com/lingshan/ServiceCatalog/ListComponentsSummary"
	httpTimeOut = 30 * time.Second
)

// httpClient 复用的 HTTP 客户端，避免每次请求都创建新实例
var httpClient = &http.Client{
	Timeout: httpTimeOut,
}

// lingshanImpl 灵山客户端实现
type lingshanImpl struct {
	secretID  string
	secretKey string
}

// GetSrvDetailByNames 获取服务详情，包括所属团队、概况、负责人等
func (l *lingshanImpl) GetSrvDetailByNames(ctx context.Context, req domainmodel.GetSrvDetailByNamesReq) (domainmodel.GetSrvDetailByNamesRsp, error) {
	names := req.Names
	if len(names) == 0 {
		return domainmodel.GetSrvDetailByNamesRsp{
			SrvInfoMap: make(map[string]*domainmodel.Component),
		}, nil
	}

	var allComponents = make(map[string]*domainmodel.Component)
	pageNum := 1

	for {
		start := (pageNum - 1) * maxPageSize
		end := start + maxPageSize
		if end > len(names) {
			end = len(names)
		}

		currentNames := names[start:end]
		if len(currentNames) == 0 {
			break
		}

		components, err := l.getComponentsPage(ctx, currentNames)
		if err != nil {
			return domainmodel.GetSrvDetailByNamesRsp{}, fmt.Errorf("failed to get page %d: %w", pageNum, err)
		}

		for name, component := range components {
			allComponents[name] = component
		}

		if end >= len(names) {
			break
		}

		pageNum++
	}
	log.InfoContextf(ctx, "GetSrvDetailByNames req: %+v, rsp: %+v", req, allComponents)

	return domainmodel.GetSrvDetailByNamesRsp{
		SrvInfoMap: allComponents,
	}, nil
}

// getComponentsPage makes a single page request
func (l *lingshanImpl) getComponentsPage(ctx context.Context, names []string) (map[string]*domainmodel.Component, error) {
	body := map[string]interface{}{
		"componentNames": names,
		"page": map[string]interface{}{
			"num":  1,
			"size": maxPageSize,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gateway-Stage", "RELEASE")
	req.Header.Set("X-Gateway-SecretId", l.secretID)
	req.Header.Set("X-Gateway-SecretKey", l.secretKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	var response domainmodel.ListComponentsResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result := make(map[string]*domainmodel.Component)
	for _, component := range response.Components {
		if component != nil && component.BaseInfo != nil && component.BaseInfo.Name != "" {
			result[component.BaseInfo.Name] = component
		}
	}

	return result, nil
}
