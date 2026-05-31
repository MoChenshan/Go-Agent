package rainbow

import (
	"reflect"
	"strings"
)

// AppConfig 应用的配置结构，可以根据需要修改结构体
type AppConfig struct {
	Debug bool `toml:"debug"`
	// LLM 相关配置
	OpenAIAPIKey       string  `toml:"openai_api_key"`
	OpenAIBaseURL      string  `toml:"openai_base_url"`
	OpenAIModelName    string  `toml:"openai_model_name"`
	ModelContextWindow int     `toml:"model_context_window"`  // 模型上下文窗口大小 (tokens)，供 token tailoring 使用
	Temperature        float64 `json:"temperature,omitempty"` // 控制生成随机性（0.0 到 2.0）
	TopP               float64 `json:"top_p,omitempty"`       // 核采样参数（0.0 到 1.0）

	// 蓝鲸平台相关配置
	BkAppCode  string `toml:"bk_app_code"`
	BkAppToken string `toml:"bk_app_token"`

	// 太湖平台相关配置
	XGatewaySecretID  string `toml:"x_gateway_secret_id"`
	XGatewaySecretKey string `toml:"x_gateway_secret_key"`

	// traceanalysis 相关配置
	SummarizeTokenThreshold int    `toml:"summarize_token_threshold"`
	SummarizeEventThreshold int    `toml:"summarize_event_threshold"`
	SummarizeTimeThreshold  int    `toml:"summarize_time_threshold"`
	MaxTraceDepth           int    `toml:"max_trace_depth"`           // traceanalysis最大递归深度
	MaxSpanNum              int    `toml:"max_span_num"`              // traceanalysis最大返回的span数量
	MaxSpanLogLength        int    `toml:"max_span_log_length"`       // traceanalysis最大返回的span日志长度
	SelfTeamName            string `toml:"self_team_name"`            // 自己团队的名称
	OtherTeamTruncateDepth  int    `toml:"other_team_truncate_depth"` // 其他团队截断的trace深度
	KnotAPIToken            string `toml:"X-knot-api-token"`

	TaihuAPIKey string `toml:"taihu_api_key"` // 太湖平台API Key

	// cdkey agent请求配置
	ESUsername string `toml:"es_username"` // es账户名
	ESPassword string `toml:"es_password"` // es密码
	FlowPath   string `toml:"flow_path"`   // 兑换流水地址，取决于es index

	// 企微WeCom AI Bot配置
	WeComEnabled       bool   `toml:"wecom_enabled"`         // 是否启用企微bot
	WeComStreamBotID   string `toml:"wecom_stream_bot_id"`   // 企微AI Bot ID
	WeComStreamSecret  string `toml:"wecom_stream_secret"`   // 企微AI Bot Secret
	WeComBotName       string `toml:"wecom_bot_name"`        // 企微Bot名称
	WeComStreamWSURL   string `toml:"wecom_stream_ws_url"`   // 企微WebSocket URL (可选)
	WeComEnableStream  bool   `toml:"wecom_enable_stream"`   // 是否启用流式回复
	WeComShowToolCalls bool   `toml:"wecom_show_tool_calls"` // 是否在回复中显示工具调用
}

// ToMap 将AppConfig转换为map，使用toml标签名作为key
func (c *AppConfig) ToMap() map[string]interface{} {
	result := make(map[string]interface{})
	val := reflect.ValueOf(c).Elem()
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldValue := val.Field(i)

		// 获取toml标签
		tomlTag := field.Tag.Get("toml")
		if tomlTag == "" || tomlTag == "-" {
			continue
		}

		// 处理标签中的选项（如omitempty）
		tomlName := strings.Split(tomlTag, ",")[0]
		if tomlName == "" {
			continue
		}

		// 将字段值添加到map
		if fieldValue.CanInterface() {
			result[tomlName] = fieldValue.Interface()
		}
	}

	return result
}

/*
使用示例：

config := &AppConfig{
	Debug:                  true,
	OpenAIAPIKey:          "your-api-key",
	OpenAIBaseURL:         "https://api.openai.com",
	OpenAIModelName:       "gpt-4",
	BkAppCode:             "your-app-code",
	BkAppToken:            "your-app-token",
	XGatewaySecretID:      "your-secret-id",
	XGatewaySecretKey:     "your-secret-key",
	SummarizeTokenThreshold: 1000,
	MaxTraceDepth:         10,
	MaxSpanNum:            100,
	MaxSpanLogLength:      500,
	SelfTeamName:          "my-team",
	OtherTeamTruncateDepth: 3,
	KnotAPIToken:          "your-knot-token",
}

configMap := config.ToMap()
// configMap 将包含：
// {
//   "debug": true,
//   "openai_api_key": "your-api-key",
//   "openai_base_url": "https://api.openai.com",
//   "openai_model_name": "gpt-4",
//   "bk_app_code": "your-app-code",
//   "bk_app_token": "your-app-token",
//   "x_gateway_secret_id": "your-secret-id",
//   "x_gateway_secret_key": "your-secret-key",
//   "summarize_token_threshold": 1000,
//   "max_trace_depth": 10,
//   "max_span_num": 100,
//   "max_span_log_length": 500,
//   "self_team_name": "my-team",
//   "other_team_truncate_depth": 3,
//   "X-knot-api-token": "your-knot-token",
// }
*/
