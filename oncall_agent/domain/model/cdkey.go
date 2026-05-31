package model

// BatchQueryCdkeyReq 批量查询cdkey的请求
type BatchQueryCdkeyReq struct {
	Queries []QueryCdkeyReq `json:"queries" jsonschema:"required,description=查询的cdkey列表"`
}

// QueryCdkeyReq 查询cdkey的请求
type QueryCdkeyReq struct {
	Cdkey       string `json:"cdkey" jsonschema:"description=选填，若填写代表只查询该cdkey发放记录"`
	Vuid        int64  `json:"vuid" jsonschema:"description=选填，若填写代表只查询该用户vuid的兑换记录"`
	SuccessOnly bool   `json:"success_only" jsonschema:"required,description=是否只查询成功发放记录"`
}

// BatchQueryCdkeyRsp 批量查询cdkey的返回
type BatchQueryCdkeyRsp struct {
	Results []QueryResult `json:"results" jsonschema:"required,description=每个查询的结果列表，顺序与请求中的queries一致"`
}

// QueryResult 单个查询的结果
type QueryResult struct {
	Query   QueryCdkeyReq `json:"query" jsonschema:"required,description=原始查询条件"`
	Records []CdkeyRecord `json:"records" jsonschema:"required,description=查询到的记录列表"`
	Error   string        `json:"error,omitempty" jsonschema:"description=查询错误信息（如有）"`
}

// CdkeyRecord cdkey的发放结果
type CdkeyRecord struct {
	SMa             string `json:"sMa"`
	SShopid         string `json:"sShopid"`
	SInnerMsg       string `json:"sInnerMsg"  jsonschema:"description=系统报错"`
	SUserMsg        string `json:"sUserMsg"`
	SCdkey          string `json:"sCdkey"`
	SAccountID      string `json:"sAccountId"`
	SAppid          string `json:"sAppid"`
	SUserIP         string `json:"sUserIp"`
	IErrCode        int    `json:"iErrCode"`
	STime           string `json:"sTime"`
	DwSourceType    int    `json:"dwSourceType"`
	DwExchangeType  int    `json:"dwExchangeType"`
	DwBid           int    `json:"dwBid"`
	DwAccountType   int    `json:"dwAccountType"`
	DwFailCheckType int    `json:"dwFailCheckType"`
	DwEvilLevel     int    `json:"dwEvilLevel"`
	DdwVuid         int64  `json:"ddwVuid" jsonschema:"description=兑换的用户vuid"`
}
