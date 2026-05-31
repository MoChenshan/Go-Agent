// Package sse 包含SSE服务的注册
package sse

import (
	"fmt"
	"time"

	"github.com/tidwall/gjson"

	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"
)

// DefaultUserID 默认用户ID，用于魔方管理台请求
const DefaultUserID = "default_user"

// Request SSE请求参数
type Request struct {
	User      string `json:"user"`       // 用户ID
	Content   string `json:"content"`    // 用户消息文本内容
	RawMsg    string `json:"raw_msg"`    // 企业微信解密后的消息回调
	SessionID string `json:"session_id"` // 会话ID
}

// Response SSE响应
type Response struct {
	Data Data `json:"data"`
}

// GetSessionID 获取会话ID
// 此函数需要兼容两种请求来源：魔方管理台和企业微信
// 魔方管理台请求没有传入用户ID，通过传入的SessionID构造会话ID
// 企业微信请求传入了用户ID, 需要自行通过UserID和当前时间构造会话ID
func (req Request) GetSessionID() string {
	if req.GetUserID() == DefaultUserID { // 若请求没有传入用户ID，则代表来源是魔方管理台，使用传入的sessionID
		return req.SessionID
	}
	// 若来源为企业微信，则自行构造sessionID
	return req.GetUserID() + "_" + time.Now().Format("20060102")
}

// String 打印SSE响应，返回样式参考
// https://iwiki.woa.com/p/4008300671
func (r Response) String() string {
	if r.Data.GlobalOutput.Docs == nil {
		r.Data.GlobalOutput.Docs = []Doc{}
	}
	return fmt.Sprintf("event:delta\ndata:%s\n", utils.MustToJSON(r.Data))
}

// GetUserID 获取用户ID, 优先使用群聊ID，如果没有则使用私聊用户ID
func (req Request) GetUserID() string {
	chatID := gjson.Get(req.RawMsg, "chatid")
	if chatID.Exists() {
		return chatID.String()
	}
	if req.User != "" {
		return req.User
	}
	return DefaultUserID
}

// Data SSE数据
type Data struct {
	Response     string       `json:"response"`      // 响应内容
	Finished     bool         `json:"finished"`      // 是否结束
	GlobalOutput GlobalOutput `json:"global_output"` // 全局输出
}

// GlobalOutput 全局输出
type GlobalOutput struct {
	Context       string `json:"context"`
	AnswerSuccess int    `json:"answer_success"`
	Docs          []Doc  `json:"docs"` // 文档列表
}

// Doc 文档
type Doc struct {
	DocID   string  `json:"doc_id"`
	SpaceID string  `json:"space_id"`
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Score   float64 `json:"score"`
}
