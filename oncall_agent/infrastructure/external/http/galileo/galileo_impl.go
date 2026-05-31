package galileo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

const (
	galileoTraceIDQueryPath = "/prod/trace/query"
	galileoLogQueryPath     = "/prod/log/query"
	baseURL                 = "https://galileo-api.apigw.o.woa.com"
	timeOut                 = 3 * time.Minute
)

var (
	httpClient = &http.Client{
		Timeout: timeOut,
	}
)

// galileoImpl 伽利略客户端实现
type galileoImpl struct {
	bkAppCode  string
	bkAppToken string
}

// QueryLog 日志查询接口
func (g *galileoImpl) QueryLog(ctx context.Context, req *domainmodel.QueryLogReq) (*domainmodel.QueryLogRsp, error) {
	rspData := &domainmodel.QueryLogRsp{}

	jsonData, err := json.Marshal(req)
	if err != nil {
		log.ErrorContextf(ctx, "QueryLog marshal failed, err = %v, req = %v", err, req)
		return nil, err
	}
	log.DebugContextf(ctx, "QueryLog req = %s", jsonData)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+galileoLogQueryPath, bytes.NewBuffer(jsonData))
	if err != nil {
		log.ErrorContextf(ctx, "QueryLog create request failed, err = %v", err)
		return nil, err
	}

	g.setHeaders(httpReq)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		log.ErrorContextf(ctx, "QueryLog failed, err = %v, req = %v", err, req)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.ErrorContextf(ctx, "QueryLog read response failed, body=%v, err = %v", body, err)
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		log.ErrorContextf(ctx, "QueryLog HTTP error, status = %d, body = %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	if err := json.Unmarshal(body, rspData); err != nil {
		log.ErrorContextf(ctx, "QueryLog unmarshal failed, err = %v, body = %s", err, string(body))
		return nil, err
	}

	log.InfoContextf(ctx, "QueryLog req: %+v, rsp: %+v", req, utils.MustToJSON(rspData))
	return rspData, nil
}

// QueryTrace traceId 查询接口
func (g *galileoImpl) QueryTrace(ctx context.Context, req *domainmodel.QueryTraceReq) (*domainmodel.QueryTraceRsp, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		log.ErrorContextf(ctx, "TraceLog marshal failed, err = %v, req = %v", err, req)
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL+galileoTraceIDQueryPath, bytes.NewBuffer(jsonData))
	if err != nil {
		log.ErrorContextf(ctx, "TraceLog create request failed, err = %v", err)
		return nil, err
	}

	g.setHeaders(httpReq)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		log.ErrorContextf(ctx, "TraceLog failed, err = %v, req = %v", err, req)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.ErrorContextf(ctx, "TraceLog read response failed, err = %v", err)
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		log.ErrorContextf(ctx, "TraceLog HTTP error, status = %d, body = %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	var traceData domainmodel.QueryTraceRsp
	err = json.Unmarshal(body, &traceData)
	if err != nil {
		log.ErrorContextf(ctx, "TraceLog unmarshal failed, err = %v, body = %s", err, string(body))
		return nil, err
	}

	log.InfoContextf(ctx, "QueryTrace: req: %+v, rsp: %+v", req, utils.MustToJSON(traceData))
	return &traceData, nil
}

// setHeaders 设置HTTP请求头
func (g *galileoImpl) setHeaders(req *http.Request) {
	authData := map[string]string{
		"bk_app_code":   g.bkAppCode,
		"bk_app_secret": g.bkAppToken,
	}

	authJSON, _ := json.Marshal(authData)

	req.Header.Set("X-Bkapi-Authorization", string(authJSON))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}
