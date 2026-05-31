package conditionlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"git.code.oa.com/trpcprotocol/magic_group/magic_condition_log"
)

// QueryData represents the query structure for condition log
type QueryData struct {
	Where   [][]string `json:"where"`
	SortDir string     `json:"sortDir"`
	Limit   string     `json:"limit"`
}

// conditionLogImpl 条件日志客户端实现
type conditionLogImpl struct{}

// GetConditionLog 获取条件日志
func (c *conditionLogImpl) GetConditionLog(ctx context.Context, start, end int64, traceID string) (string, error) {
	startTime := time.UnixMilli(start).Format("2006-01-02 15:04:05")
	endTime := time.UnixMilli(end).Format("2006-01-02 15:04:05")

	queryData := QueryData{
		Where: [][]string{
			{"order_id", "=", traceID},
			{"ftime", "between", startTime, endTime},
		},
		SortDir: "DESC",
		Limit:   "1",
	}

	dataJSON, err := json.Marshal(queryData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query data: %w", err)
	}

	cli := magic_condition_log.NewGetLogerClientProxy()
	rsp, err := cli.GetLog(ctx, &magic_condition_log.GetLogReq{
		Data: string(dataJSON),
	})
	if err != nil {
		return "", fmt.Errorf("failed to call GetLog: %w", err)
	}

	if rsp.Ret != 0 {
		return "", fmt.Errorf("GetLog returned error: ret=%d, msg=%s", rsp.Ret, rsp.Msg)
	}
	if !strings.Contains(rsp.OutData, traceID) {
		return "condition log not found", nil
	}

	return rsp.OutData, nil
}
