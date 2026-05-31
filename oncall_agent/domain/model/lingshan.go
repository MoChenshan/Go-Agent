package model

import "time"

// ListComponentsResponse represents the API response structure
type ListComponentsResponse struct {
	Components []*Component `json:"components"`
	Count      int          `json:"count"`
}

// GetSrvDetailByNamesReq 代表获取服务详情的请求结构
type GetSrvDetailByNamesReq struct {
	Names []string `json:"names" jsonschema:"required,description=需要查询的服务名列表，格式为 {app}.{server}"`
}

// GetSrvDetailByNamesRsp 代表获取服务详情的响应结构
type GetSrvDetailByNamesRsp struct {
	SrvInfoMap map[string]*Component `json:"srvInfoMap" jsonschema:"the map of service name to service info"`
}

// Component represents a single component in the response
type Component struct {
	BaseInfo   *BaseInfo   `json:"baseInfo"`
	MemberInfo *MemberInfo `json:"memberInfo"`
}

// BaseInfo contains basic information about a component
type BaseInfo struct {
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	Type               int       `json:"type"`
	ApplicationID      string    `json:"applicationId"`
	ApplicationName    string    `json:"applicationName"`
	ProductID          string    `json:"productId"`
	ProductName        string    `json:"productName"`
	CreatedAt          time.Time `json:"createdAt"`
	CreatedBy          string    `json:"createdBy"`
	UpdatedAt          time.Time `json:"updatedAt"`
	UpdatedBy          string    `json:"updatedBy"`
	Labels             []Label   `json:"labels"`
	ID                 string    `json:"id"`
	ProductDisplayName string    `json:"productDisplayName"`
	TeamID             string    `json:"teamId"`
	TeamName           string    `json:"teamName"`
	RepoPath           string    `json:"repoPath"`
	CodePath           string    `json:"codePath"`
	CodePaths          []string  `json:"codePaths"`
	Framework          int       `json:"framework"`
	Language           int       `json:"language"`
}

// Label represents a key-value label
type Label struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// MemberInfo represents member information (currently unused)
type MemberInfo struct {
	// Add fields as needed based on actual API response
}
