package cdkey

import (
	"context"
	"encoding/base64"
	"regexp"

	"github.com/spf13/cast"

	"git.code.oa.com/trpc-go/trpc-go/client"
	"git.code.oa.com/trpc-go/trpc-go/errs"
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.code.oa.com/trpc-go/trpc-go/metrics"
	"git.code.oa.com/video_pay_channel_coop/vcdkey/common/ers"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

const (
	esName = "trpc.tob.cdkey.es"
)

// cdkPattern cdk格式模版，支持16位或18位
var cdkPattern = regexp.MustCompile("^[a-zA-Z0-9]{16,18}$")

// cdkeyImpl cdkey客户端实现
type cdkeyImpl struct {
	esUsername string
	esPassword string
	flowPath   string
}

// ESQueryResult ES查询结果
type ESQueryResult struct {
	Took     int  `json:"took"`
	TimedOut bool `json:"timed_out"`
	Shards   struct {
		Total      int `json:"total"`
		Successful int `json:"successful"`
		Skipped    int `json:"skipped"`
		Failed     int `json:"failed"`
	} `json:"_shards"`
	Hits struct {
		Total    int         `json:"total"`
		MaxScore interface{} `json:"max_score"`
		Hits     []HitItem   `json:"hits"`
	} `json:"hits"`
}

// HitItem es流水查询结果元素
type HitItem struct {
	Index  string                  `json:"_index"`
	Type   string                  `json:"_type"`
	ID     string                  `json:"_id"`
	Score  interface{}             `json:"_score"`
	Source domainmodel.CdkeyRecord `json:"_source"`
	Sort   []int64                 `json:"sort"`
}

// BatchQueryCdkey 批量查询cdkey
func (c *cdkeyImpl) BatchQueryCdkey(ctx context.Context, req domainmodel.BatchQueryCdkeyReq) (domainmodel.BatchQueryCdkeyRsp, error) {
	if len(req.Queries) == 0 {
		metrics.Counter("批量查询参数为空").Incr()
		log.ErrorContextf(ctx, "batch query cdkey get empty queries")
		return domainmodel.BatchQueryCdkeyRsp{}, errs.New(errs.RetClientValidateFail, "查询参数为空")
	}

	if len(req.Queries) > 100 {
		metrics.Counter("批量查询超限").Incr()
		log.ErrorContextf(ctx, "batch query cdkey exceed limit: %d > 100", len(req.Queries))
		return domainmodel.BatchQueryCdkeyRsp{}, errs.New(errs.RetClientValidateFail, "批量查询数量超过限制，最多支持100个CDKey")
	}

	results := make([]domainmodel.QueryResult, len(req.Queries))
	for i, query := range req.Queries {
		results[i] = domainmodel.QueryResult{
			Query:   query,
			Records: []domainmodel.CdkeyRecord{},
		}
	}

	validQueries := make([]domainmodel.QueryCdkeyReq, 0, len(req.Queries))
	validIndices := make([]int, 0, len(req.Queries))

	for i, query := range req.Queries {
		if query.Cdkey != "" {
			if err := validateQueryCdkey(ctx, query.Cdkey); err != nil {
				results[i].Error = "CDKey格式错误，请检查是否为16位或18位腾讯视频CDK"
				results[i].Records = []domainmodel.CdkeyRecord{{
					SCdkey:    query.Cdkey,
					SInnerMsg: "CDKey格式错误，请检查是否为16位或18位腾讯视频CDK",
				}}
				continue
			}
		}
		validQueries = append(validQueries, query)
		validIndices = append(validIndices, i)
	}

	if len(validQueries) == 0 {
		return domainmodel.BatchQueryCdkeyRsp{Results: results}, nil
	}

	flows, err := c.batchQueryExchangeFlows(ctx, validQueries)
	if err != nil {
		log.ErrorContextf(ctx, "batchQueryExchangeFlows err: %+v", err)
		for _, idx := range validIndices {
			if results[idx].Error == "" {
				results[idx].Error = err.Error()
				results[idx].Records = []domainmodel.CdkeyRecord{{
					SCdkey:    req.Queries[idx].Cdkey,
					SInnerMsg: err.Error(),
				}}
			}
		}
		return domainmodel.BatchQueryCdkeyRsp{Results: results}, nil
	}

	for _, flow := range flows {
		for i, query := range req.Queries {
			if matchQueryWithFlow(query, flow) {
				results[i].Records = append(results[i].Records, flow)
			}
		}
	}

	return domainmodel.BatchQueryCdkeyRsp{Results: results}, nil
}

// matchQueryWithFlow 判断流水记录是否匹配查询条件
func matchQueryWithFlow(query domainmodel.QueryCdkeyReq, flow domainmodel.CdkeyRecord) bool {
	if query.Cdkey != "" && query.Cdkey != flow.SCdkey {
		return false
	}
	if query.Vuid != 0 {
		flowVuid := cast.ToInt64(flow.DdwVuid)
		if query.Vuid != flowVuid {
			return false
		}
	}
	if query.SuccessOnly && flow.IErrCode != 0 {
		return false
	}
	return true
}

func validateQueryCdkey(ctx context.Context, cdkey string) error {
	if len(cdkey) != 18 && len(cdkey) != 16 {
		metrics.Counter("cdk长度错误").Incr()
		log.ErrorContextf(ctx, "invalid cdkey length:%v, cdkey:%v", len(cdkey), cdkey)
		return ers.VerifyCdkeyFailErr.WithUserMsg("请确认位数是否正确,勿包含中文及空格")
	}
	if !cdkPattern.MatchString(cdkey) {
		metrics.Counter("cdk格式错误").Incr()
		log.ErrorContextf(ctx, "mismatch cdkey patten, cdkey:%v", cdkey)
		return ers.VerifyCdkeyFailErr.WithUserMsg("cdkey格式错误,请确认输入是否正确,勿包含中文及空格")
	}
	return nil
}

// buildBatchQuery 构造批量查询的ES DSL
func buildBatchQuery(queries []domainmodel.QueryCdkeyReq, limit int64) map[string]interface{} {
	shouldClauses := make([]map[string]interface{}, 0, len(queries))

	for _, q := range queries {
		mustClause := make([]map[string]interface{}, 0)

		if q.Cdkey != "" {
			mustClause = append(mustClause, map[string]interface{}{
				"match_phrase": map[string]string{
					"sCdkey": q.Cdkey,
				},
			})
		}

		if q.Vuid != 0 {
			mustClause = append(mustClause, map[string]interface{}{
				"match_phrase": map[string]string{
					"ddwVuid": cast.ToString(q.Vuid),
				},
			})
		}

		if q.SuccessOnly {
			mustClause = append(mustClause, map[string]interface{}{
				"match_phrase": map[string]string{
					"iErrCode": "0",
				},
			})
		}

		if len(mustClause) > 0 {
			shouldClauses = append(shouldClauses, map[string]interface{}{
				"bool": map[string]interface{}{
					"must": mustClause,
				},
			})
		}
	}

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"should":               shouldClauses,
				"minimum_should_match": 1,
			},
		},
		"sort": map[string]interface{}{
			"sTime": map[string]string{
				"order": "desc",
			},
		},
		"size": limit,
	}

	return query
}

// batchQueryExchangeFlows 批量查询兑换ES流水
func (c *cdkeyImpl) batchQueryExchangeFlows(ctx context.Context, queries []domainmodel.QueryCdkeyReq) ([]domainmodel.CdkeyRecord, error) {
	metrics.Counter("批量查询兑换流水请求量").Incr()

	validQueries := make([]domainmodel.QueryCdkeyReq, 0, len(queries))
	for _, q := range queries {
		if q.Cdkey != "" {
			if err := validateQueryCdkey(ctx, q.Cdkey); err != nil {
				continue
			}
		}
		validQueries = append(validQueries, q)
	}

	if len(validQueries) == 0 {
		return []domainmodel.CdkeyRecord{}, nil
	}

	totalLimit := int64(len(validQueries) * 20)
	if totalLimit > 2000 {
		totalLimit = 2000
	}
	queryData := buildBatchQuery(validQueries, totalLimit)
	queryRes := &ESQueryResult{}

	reqHeader := basicAuthHeader(c.esUsername, c.esPassword)

	proxy := thttp.NewClientProxy(esName, client.WithReqHead(reqHeader))
	if err := proxy.Post(ctx, c.flowPath, queryData, queryRes); err != nil {
		metrics.Counter("批量查询兑换流水失败").Incr()
		log.ErrorContextf(ctx, "batch query cdk es flow err:%v", err)
		return nil, ers.CdkeyExchangeInnerErr.WithUserMsg("批量查询兑换流水失败")
	}

	log.InfoContextf(ctx, "batch query exchange flows with query: %s, rsp: %s",
		utils.MustToJSON(queryData), utils.MustToJSON(queryRes))

	if len(queryRes.Hits.Hits) == 0 {
		metrics.Counter("批量查询无兑换流水").Incr()
		log.DebugContextf(ctx, "batch query cdk es flow get empty res")
		return []domainmodel.CdkeyRecord{}, nil
	}

	flows := make([]domainmodel.CdkeyRecord, 0, len(queryRes.Hits.Hits))
	for _, hit := range queryRes.Hits.Hits {
		flows = append(flows, hit.Source)
	}

	return flows, nil
}

// basicAuthHeader 设置账号密码
func basicAuthHeader(username, password string) *thttp.ClientReqHeader {
	reqHeader := &thttp.ClientReqHeader{}
	reqHeader.AddHeader("Authorization", "Basic "+basicAuth(username, password))
	return reqHeader
}

// basicAuth https://www.ietf.org/rfc/rfc2617.txt
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
