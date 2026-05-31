# DDD Compliance Refactor — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring `oncall_agent` into full compliance with the `trpc-ddd-codegen` skill — eliminating domain→infra imports, establishing `domain/interfaces/external/`, moving all shared types to `domain/model`, and introducing Wire DI.

**Architecture:** Domain layer owns all shared types (`domain/model`) and interface definitions (`domain/interfaces/external`). Infrastructure implements those interfaces and imports domain types. Wire replaces manual `initServers()` wiring. `main.go` shrinks to ~50 lines.

**Tech Stack:** Go, `github.com/google/wire`, tRPC-Go, GoConvey + GoMonkey for tests, golangci-lint.

**Spec:** `docs/superpowers/specs/2026-03-10-ddd-compliance-design.md`

---

## Chunk 1: Move shared types to `domain/model`

Move all request/response types and DB entity types from infra packages into `domain/model`. No behaviour changes — pure type relocation.

### Task 1: Add galileo types to `domain/model`

**Files:**
- Create: `domain/model/galileo.go`
- Modify: `infrastructure/external/http/galileo/types.go` → delete after migration
- Modify: `infrastructure/external/http/galileo/galileo_impl.go` — import `domain/model`

- [ ] **Step 1: Create `domain/model/galileo.go`**

Move all types from `infrastructure/external/http/galileo/types.go` into a new file. The package `gjson`/`utils` import for `RemoveRedundantInfo` stays — move that method to `domain/model/galileo.go` as well.

```go
// Package model 包含跨领域共享的领域对象和原语
package model

import (
    "github.com/tidwall/gjson"
    "git.woa.com/video_pay_middle_platform/pay-go-comm/utils"
)

// QueryTraceReq 伽利略trace查询请求
type QueryTraceReq struct {
    Target  string `protobuf:"bytes,1,opt,name=target,proto3" json:"target,omitempty"`
    TraceID string `protobuf:"bytes,2,opt,name=trace_id,json=traceId,proto3" json:"trace_id,omitempty"`
}

// QueryLogReq 伽利略日志查询请求
type QueryLogReq struct {
    Target         string         `json:"target" jsonschema:"required,description=需要查询日志的对象 格式为{{app}}.{{server}} 比如魔方接入层为magic.magic_access" validate:"required,pattern=^.+$"`
    Namespace      string         `json:"namespace" jsonschema:"required,description=命名空间 正式环境/预发布环境: Production 测试环境: Development" validate:"required,oneof=Production Development"`
    Start          int64          `json:"start" jsonschema:"required,description=查询的开始时间 **单位毫秒**"`
    End            int64          `json:"end" jsonschema:"required,description=查询的结束时间 **单位毫秒** 注意 查询开始和结束时间间隔不能超过24小时"`
    Limit          int32          `json:"limit" jsonschema:"required,description=最大查询条数 范围1-30"`
    TagWhere       *TagSearch     `json:"tag_where,omitempty" jsonschema:"description=日志需要包含的标签列表 不同服务日志上报的标签不一样"`
    MessageKeyword []string       `json:"message_keyword,omitempty" jsonschema:"description=message关键字搜索 多关键字之间关系为且（已废弃）"`
    Cursor         string         `json:"cursor,omitempty" jsonschema:"description=游标翻页查询 用于分页获取数据" validate:"tsecstr"`
    Include        *MessageSearch `json:"include,omitempty" jsonschema:"description=需要包含的日志正文关键字列表"`
    Exclude        *MessageSearch `json:"exclude,omitempty" jsonschema:"description=需要排除的日志正文关键字列表"`
    SortType       LogSortType    `json:"sort_type,omitempty" jsonschema:"description=排序方式：0-默认倒序 1-正序 2-倒序"`
}

// QueryLogRsp 伽利略日志查询响应
type QueryLogRsp struct {
    Code        int32        `json:"code,omitempty" jsonschema:"description=返回码"`
    Msg         string       `json:"msg,omitempty" jsonschema:"description=返回消息"`
    Total       int32        `json:"total,omitempty" jsonschema:"description=统计当前实际查询所得条数"`
    Logs        []*LogRecord `json:"logs,omitempty" jsonschema:"description=日志记录列表"`
    Cursor      string       `json:"cursor,omitempty" jsonschema:"description=游标翻页查询，用于获取下一页数据"`
    HasNextPage bool         `json:"has_next_page,omitempty" jsonschema:"description=是否有下一页数据"`
}

// LogRecord 伽利略日志记录
type LogRecord struct {
    Timestamp string            `json:"timestamp,omitempty" jsonschema:"description=时间戳，单位毫秒"`
    TraceId   string            `json:"trace_id,omitempty" jsonschema:"description=traceId，链路追踪标识"`
    SpanId    string            `json:"span_id,omitempty" jsonschema:"description=spanId，链路追踪中的span标识"`
    Message   string            `json:"message,omitempty" jsonschema:"description=日志消息内容"`
    Level     string            `json:"level,omitempty" jsonschema:"description=日志级别"`
    Tags      map[string]string `json:"tags,omitempty" jsonschema:"description=日志标签，键值对形式"`
}

// RemoveRedundantInfo 删除请求日志中多余的魔方登陆态信息
func (l *LogRecord) RemoveRedundantInfo() {
    if req := l.Tags["req"]; req != "" {
        if query := gjson.Get(req, "context.cgi_req_data.query"); query.Exists() {
            reqMap := utils.MustJSONToMap(req)
            delete(reqMap, "context")
            reqMap["query"] = query.String()
            l.Tags["req"] = utils.MustToJSON(reqMap)
        }
    }
}

// LogSortType 日志排序方式
type LogSortType int32

const (
    LogSortType_SORT_TYPE_DEFAULT LogSortType = 0
    LogSortType_SORT_TYPE_ASC     LogSortType = 1
    LogSortType_SORT_TYPE_DESC    LogSortType = 2
)

// MessageSearch_SearchType 日志搜索类型
type MessageSearch_SearchType int32

const (
    MessageSearch_MESSAGE_SEARCH_TYPE_DEFAULT MessageSearch_SearchType = 0
    MessageSearch_MESSAGE_SEARCH_TYPE_CASEINS MessageSearch_SearchType = 1
    MessageSearch_MESSAGE_SEARCH_TYPE_TOKEN   MessageSearch_SearchType = 2
    MessageSearch_MESSAGE_SEARCH_TYPE_SUBSTR  MessageSearch_SearchType = 3
    MessageSearch_MESSAGE_SEARCH_TYPE_REGULAR MessageSearch_SearchType = 4
)

// MessageSearch_FilterType 日志搜索过滤类型
type MessageSearch_FilterType int32

const (
    MessageSearch_EMTPTY MessageSearch_FilterType = 0
    MessageSearch_OR     MessageSearch_FilterType = 1
    MessageSearch_AND    MessageSearch_FilterType = 2
)

// MessageSearch 日志搜索
type MessageSearch struct {
    Keyword []string                 `json:"keyword,omitempty" jsonschema:"description=关键字列表"`
    Filter  MessageSearch_FilterType `json:"filter,omitempty" jsonschema:"description=关键词之间逻辑关系：0-默认为或，1-或，2-且"`
    Search  MessageSearch_SearchType `json:"search,omitempty" jsonschema:"description=搜索逻辑：0-默认搜索，1-忽略大小写，2-单词搜索，3-子串搜索，4-正则搜索"`
}

// LogTagsFields 日志标签字段
type LogTagsFields struct {
    Name   string   `json:"name,omitempty" jsonschema:"description=标签名称"`
    Values []string `json:"values,omitempty" jsonschema:"description=标签值列表，支持多个值匹配"`
}

// TagSearch 日志标签搜索
type TagSearch struct {
    TraceId   string           `json:"trace_id,omitempty" jsonschema:"description=traceId查询条件"`
    Level     []string         `json:"level,omitempty" jsonschema:"description=日志级别列表，支持多个级别筛选"`
    OtherTags []*LogTagsFields `json:"other_tags,omitempty" jsonschema:"description=其他标签查询条件"`
}

// QueryTraceRsp represents the complete trace structure
type QueryTraceRsp struct {
    Code  int    `json:"code,omitempty"`
    Msg   string `json:"msg,omitempty"`
    Trace Trace  `json:"trace,omitempty"`
}

// Trace contains the processes and spans
type Trace struct {
    Processes map[string]Process `json:"processes,omitempty"`
    Spans     []Span             `json:"spans,omitempty"`
}

// Process represents a service in the trace
type Process struct {
    ServiceName string `json:"service_name,omitempty"`
    Tags        []Tag  `json:"tags,omitempty"`
}

// Span represents a single operation in the trace
type Span struct {
    TraceID       string      `json:"trace_id,omitempty"`
    SpanID        string      `json:"span_id,omitempty"`
    Duration      string      `json:"duration,omitempty"`
    OperationName string      `json:"operation_name,omitempty"`
    ProcessID     string      `json:"process_id,omitempty"`
    Tags          []Tag       `json:"tags,omitempty"`
    Logs          []LogEntry  `json:"logs,omitempty"`
    References    []Reference `json:"references,omitempty"`
}

// Tag represents key-value pairs in spans and processes
type Tag struct {
    Key   string `json:"key,omitempty"`
    Value string `json:"value,omitempty"`
}

// LogEntry contains event logs with fields
type LogEntry struct {
    Fields []Field `json:"fields,omitempty"`
}

// Field represents a log field
type Field struct {
    Key   string `json:"key,omitempty"`
    Value string `json:"value,omitempty"`
}

// Reference represents parent-child relationships
type Reference struct {
    RefType string `json:"ref_type,omitempty"`
    SpanID  string `json:"span_id,omitempty"`
}
```

- [ ] **Step 2: Delete `infrastructure/external/http/galileo/types.go`**

Remove the file entirely.

- [ ] **Step 3: Update `infrastructure/external/http/galileo/galileo_impl.go`**

Add import `domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"` and replace all `galileo.QueryLogReq` etc. references with `domainmodel.QueryLogReq` etc.

- [ ] **Step 4: Update `infrastructure/external/http/galileo/galileo_api.go`**

The `API` interface method signatures now use `*domainmodel.QueryLogReq` etc. Add `domainmodel` import. Remove the old type imports.

- [ ] **Step 5: Build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add domain/model/galileo.go infrastructure/external/http/galileo/
git commit -m "refactor: move galileo types to domain/model"
```

---

### Task 2: Add lingshan types to `domain/model`

**Files:**
- Create: `domain/model/lingshan.go`
- Delete: `infrastructure/external/http/lingshan/types.go`
- Modify: `infrastructure/external/http/lingshan/lingshan_impl.go`
- Modify: `infrastructure/external/http/lingshan/lingshan_api.go`

- [ ] **Step 1: Create `domain/model/lingshan.go`**

```go
package model

import "time"

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

// MemberInfo represents member information
type MemberInfo struct{}

// ListComponentsResponse represents the API response structure
type ListComponentsResponse struct {
    Components []*Component `json:"components"`
    Count      int          `json:"count"`
}
```

- [ ] **Step 2: Delete `infrastructure/external/http/lingshan/types.go`**

- [ ] **Step 3: Update `lingshan_impl.go` and `lingshan_api.go`** — replace `lingshan.*` type refs with `domainmodel.*`.

- [ ] **Step 4: Build and commit**

```bash
go build ./...
git add domain/model/lingshan.go infrastructure/external/http/lingshan/
git commit -m "refactor: move lingshan types to domain/model"
```

---

### Task 3: Add cdkey types to `domain/model`

**Files:**
- Create: `domain/model/cdkey.go`
- Delete: `infrastructure/external/http/cdkey/types.go`
- Modify: `infrastructure/external/http/cdkey/cdkey_impl.go`
- Modify: `infrastructure/external/http/cdkey/cdkey_api.go`

- [ ] **Step 1: Create `domain/model/cdkey.go`**

```go
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
    SInnerMsg       string `json:"sInnerMsg" jsonschema:"description=系统报错"`
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
```

Note: `Record` is renamed to `CdkeyRecord` to avoid collision with other domain types. Update all usages in `cdkey_impl.go` accordingly.

- [ ] **Step 2: Delete `infrastructure/external/http/cdkey/types.go`**, update impl and api files.

- [ ] **Step 3: Build and commit**

```bash
go build ./...
git add domain/model/cdkey.go infrastructure/external/http/cdkey/
git commit -m "refactor: move cdkey types to domain/model"
```

---

### Task 4: Add magic_config types to `domain/model`

**Files:**
- Create: `domain/model/magic_config.go`
- Modify: `infrastructure/repo/mysql/magic_config/magic_config.go` — keep `API` interface + `New()`, remove type defs
- Modify: `infrastructure/repo/mysql/magic_config/magic_config_impl.go` — import `domain/model`
- Modify: `domain/tools/magic_tool/magic_tool_api.go` — replace `magicconfig.*` with `model.*`
- Modify: `domain/tools/magic_tool/magic_act_info.go`, `magic_mod_type_info.go`, `propose_config_change.go`

- [ ] **Step 1: Create `domain/model/magic_config.go`**

Copy all type definitions (not the `API` interface) from `infrastructure/repo/mysql/magic_config/magic_config.go` into `domain/model/magic_config.go`. Keep `gorm` struct tags intact — they are needed by the impl.

```go
// Package model 包含跨领域共享的领域对象和原语
package model

import "time"

// OutputDataType 模块输出数据类型
type OutputDataType int

// ... (copy all types: ActInfo, ModuleInfo, ModuleType, ModuleInputType,
//      ModuleOutputType, ModuleOpType, AdminConfigV3, OutputType,
//      Condition, ConditionItem, ModuleSecurity, CompleteModuleTypeInfo)
```

- [ ] **Step 2: Remove type definitions from `infrastructure/repo/mysql/magic_config/magic_config.go`**

Keep only `package`, imports, `API` interface, and `New()`/`NewWithClientName()` constructors. Replace the `API` interface method signatures to use `*domainmodel.ActInfo`, etc.

- [ ] **Step 3: Update `magic_config_impl.go`** — add `domainmodel` import, replace `ActInfo` etc. with `domainmodel.ActInfo`.

- [ ] **Step 4: Update `domain/tools/magic_tool/magic_tool_api.go`** — replace `magicconfig.*` with `domainmodel.*` throughout. Pay attention to:
  - Any struct fields typed `magicconfig.ModuleInfo`, `magicconfig.ActInfo`, etc. in request/response structs (e.g. `GetMagicActInfoRsp.ModInfos []*domainmodel.ModuleInfo`)
  - `ModOutputType.OutputDataType` field — its type is `OutputDataType` (also moved to `domainmodel.OutputDataType`), so update to `domainmodel.OutputDataType`

- [ ] **Step 5: Update `magic_act_info.go`, `magic_mod_type_info.go`, `propose_config_change.go`** — same replacement.

- [ ] **Step 6: Build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add domain/model/magic_config.go infrastructure/repo/mysql/magic_config/ domain/tools/magic_tool/
git commit -m "refactor: move magic_config types to domain/model"
```

---

## Chunk 2: Create `domain/interfaces/external/`

Define all shared external service interfaces in `domain/interfaces/external/`, using only `domain/model` types. Update domain tool packages to reference these instead of defining their own.

### Task 5: Create interface files

**Files:**
- Create: `domain/interfaces/external/galileo_api.go`
- Create: `domain/interfaces/external/lingshan_api.go`
- Create: `domain/interfaces/external/cdkey_api.go`
- Create: `domain/interfaces/external/conditionlog_api.go`
- Create: `domain/interfaces/external/magiccli_api.go`
- Create: `domain/interfaces/external/magic_config_api.go`

- [ ] **Step 1: Create `domain/interfaces/external/galileo_api.go`**

```go
// Package external 定义外部服务接口，由 infrastructure/external 实现
package external

import (
    "context"

    domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=galileo_api.go --destination=galileo_mock.go --package=external

// GalileoAPI 伽利略日志与链路查询接口
type GalileoAPI interface {
    // QueryLog 日志查询接口
    QueryLog(ctx context.Context, req *domainmodel.QueryLogReq) (*domainmodel.QueryLogRsp, error)
    // QueryTrace traceId查询接口
    QueryTrace(ctx context.Context, req *domainmodel.QueryTraceReq) (*domainmodel.QueryTraceRsp, error)
}
```

- [ ] **Step 2: Create `domain/interfaces/external/lingshan_api.go`**

```go
package external

import (
    "context"

    domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=lingshan_api.go --destination=lingshan_mock.go --package=external

// LingshanAPI 灵杉服务注册中心查询接口
type LingshanAPI interface {
    // GetSrvDetailByNames 获取服务详情，包括所属团队、概况、负责人等
    GetSrvDetailByNames(ctx context.Context, req domainmodel.GetSrvDetailByNamesReq) (domainmodel.GetSrvDetailByNamesRsp, error)
}
```

- [ ] **Step 3: Create `domain/interfaces/external/cdkey_api.go`**

```go
package external

import (
    "context"

    domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=cdkey_api.go --destination=cdkey_mock.go --package=external

// CdkeyAPI cdkey查询接口
type CdkeyAPI interface {
    // BatchQueryCdkey 批量查询cdkey
    BatchQueryCdkey(ctx context.Context, req domainmodel.BatchQueryCdkeyReq) (domainmodel.BatchQueryCdkeyRsp, error)
}
```

- [ ] **Step 4: Create `domain/interfaces/external/conditionlog_api.go`**

```go
package external

import "context"

//go:generate mockgen --source=conditionlog_api.go --destination=conditionlog_mock.go --package=external

// ConditionLogAPI 条件日志查询接口
type ConditionLogAPI interface {
    // GetConditionLog 获取条件日志
    GetConditionLog(ctx context.Context, start, end int64, traceID string) (string, error)
}
```

- [ ] **Step 5: Create `domain/interfaces/external/magiccli_api.go`**

```go
package external

import "context"

//go:generate mockgen --source=magiccli_api.go --destination=magiccli_mock.go --package=external

// MagicCliAPI 魔方加密接口
type MagicCliAPI interface {
    // EncryptMagicID 加密魔方ID
    EncryptMagicID(ctx context.Context, plain string) (string, error)
}
```

- [ ] **Step 6: Create `domain/interfaces/external/magic_config_api.go`**

```go
package external

import (
    "context"

    domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=magic_config_api.go --destination=magic_config_mock.go --package=external

// MagicConfigAPI 魔方配置数据库接口
type MagicConfigAPI interface {
    GetActInfoByID(ctx context.Context, actID int) (*domainmodel.ActInfo, error)
    GetModuleInfoByActID(ctx context.Context, actID int) ([]*domainmodel.ModuleInfo, error)
    GetModuleInfoByID(ctx context.Context, moduleID int) (*domainmodel.ModuleInfo, error)
    CreateModuleInfo(ctx context.Context, module *domainmodel.ModuleInfo) error
    UpdateModuleInfo(ctx context.Context, module *domainmodel.ModuleInfo) error
    GetModuleTypesByFuzzyNames(ctx context.Context, names []string) ([]*domainmodel.ModuleType, error)
    GetLatestSampleConfigByModuleTypeID(ctx context.Context, modTypeID int) (string, error)
    GetCompleteModuleTypeInfo(ctx context.Context, modTypeID int) (*domainmodel.CompleteModuleTypeInfo, error)
    GetModuleSecurityByModuleTypeID(ctx context.Context, modTypeID int) (*domainmodel.ModuleSecurity, error)
    GetConditionsByModuleIDs(ctx context.Context, moduleIDs []int) (map[int][]*domainmodel.Condition, error)
    GetConditionByID(ctx context.Context, conditionID int) (*domainmodel.Condition, error)
    CreateCondition(ctx context.Context, condition *domainmodel.Condition) error
    UpdateCondition(ctx context.Context, condition *domainmodel.Condition) error
    GetConditionItemsByConditionIDs(ctx context.Context, conditionIDs []int) (map[int][]*domainmodel.ConditionItem, error)
    GetConditionItemByID(ctx context.Context, itemID int) (*domainmodel.ConditionItem, error)
    CreateConditionItem(ctx context.Context, item *domainmodel.ConditionItem) error
    UpdateConditionItem(ctx context.Context, item *domainmodel.ConditionItem) error
}
```

- [ ] **Step 7: Build**

```bash
go build ./...
```

Expected: no errors (new files only add types, nothing uses them yet).

- [ ] **Step 8: Commit**

```bash
git add domain/interfaces/
git commit -m "feat: add domain/interfaces/external with shared interface definitions"
```

---

### Task 6: Update domain tool packages to use `domain/interfaces/external`

Replace all per-package local `GalileoAPI`, `LingshanAPI`, `CdkeyAPI`, `ConditionLogAPI`, `MagicCliAPI`, `MagicConfigAPI` type definitions with imports from `domain/interfaces/external`.

**Files:**
- Modify: `domain/tools/log_query/log_query_api.go`
- Modify: `domain/tools/trace_analysis/trace_analysis_api.go`
- Modify: `domain/tools/lingshan_query/lingshan_query_api.go`
- Modify: `domain/tools/cdkey_query/cdkey_query_api.go`
- Modify: `domain/tools/magic_tool/magic_tool_api.go`

- [ ] **Step 1: Update `domain/tools/log_query/log_query_api.go`**

Remove local `GalileoAPI` interface definition and `galileo` import. Change `Dep.GalileoCli` type to `domainext.GalileoAPI`:

```go
import (
    "context"

    domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
    domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

type Dep struct {
    GalileoCli domainext.GalileoAPI
    WujiCli    domainmodel.WujiAPI
}
```

Remove the now-unused `context` import if `context` is only used in the old `GalileoAPI` interface. Update `log_query_impl.go` — the `dep.GalileoCli` call signatures now use `*domainmodel.QueryLogReq` etc., which they already do (the impl was already using whatever type came through).

- [ ] **Step 2: Update `domain/tools/trace_analysis/trace_analysis_api.go`**

Remove local `GalileoAPI`, `LingshanAPI`, `ConditionLogAPI` definitions. Update `Dep`:

```go
import (
    domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
    domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

type Dep struct {
    GalileoCli      domainext.GalileoAPI
    LingshanCli     domainext.LingshanAPI
    ConditionLogCli domainext.ConditionLogAPI
    WujiCli         domainmodel.WujiAPI
    Cfg             TraceConfig
}
```

Also update `domain/tools/trace_analysis/types.go` — replace `galileo.LogEntry` and `galileo.Span` with `domainmodel.LogEntry` and `domainmodel.Span`.

- [ ] **Step 3: Update `domain/tools/lingshan_query/lingshan_query_api.go`**

Remove local `LingshanAPI` definition. Change `Dep.LingshanCli` to `domainext.LingshanAPI`.

- [ ] **Step 4: Update `domain/tools/cdkey_query/cdkey_query_api.go`**

Remove local `CdkeyAPI` definition. Change `Dep.CdkeyCli` to `domainext.CdkeyAPI`.

- [ ] **Step 5: Update `domain/tools/magic_tool/magic_tool_api.go`**

Remove local `MagicConfigAPI` and `MagicCliAPI` definitions. Change `Dep` fields:

```go
import (
    domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
    domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

type Dep struct {
    WujiCli        domainmodel.WujiAPI
    MagicConfigCli domainext.MagicConfigAPI
    MagicCliAPI    domainext.MagicCliAPI
}
```

- [ ] **Step 6: Build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add domain/tools/
git commit -m "refactor: domain tools now reference domain/interfaces/external"
```

---

### Task 7: Update infra packages to implement `domain/interfaces/external`

Each infra external package's `New()` now returns the domain interface type. The `API` type alias in each infra package is removed (it was the local interface def).

**Files:**
- Modify: `infrastructure/external/http/galileo/galileo_api.go`
- Modify: `infrastructure/external/http/lingshan/lingshan_api.go`
- Modify: `infrastructure/external/http/cdkey/cdkey_api.go`
- Modify: `infrastructure/external/http/magiccli/magiccli_api.go`
- Modify: `infrastructure/external/trpc/conditionlog/conditionlog_api.go`
- Modify: `infrastructure/repo/mysql/magic_config/magic_config.go`

- [ ] **Step 1: Update each infra `_api.go`** to remove the local `API` interface and return the domain interface from `New()`:

Example for `galileo_api.go`:

```go
package galileo

import (
    domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
)

// New 创建伽利略客户端
func New(bkAppCode, bkAppToken string) domainext.GalileoAPI {
    return &galileoImpl{bkAppCode: bkAppCode, bkAppToken: bkAppToken}
}
```

Repeat for `lingshan`, `cdkey`, `magiccli`, `conditionlog`.

- [ ] **Step 2: Update `magic_config/magic_config.go`**

Remove the `API` interface (now in `domain/interfaces/external`). Keep only the constructors returning `domainext.MagicConfigAPI`.

Move the mock file: the old `mock_magic_config.go` in `infrastructure/repo/mysql/magic_config/` will be regenerated in `domain/interfaces/external/` via `go:generate` in Task 8. Delete the old mock file.

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add infrastructure/
git commit -m "refactor: infra packages implement domain/interfaces/external"
```

---

### Task 8: Generate mocks

- [ ] **Step 1: Run mockgen for all interfaces**

```bash
cd domain/interfaces/external
go generate ./...
```

This generates `galileo_mock.go`, `lingshan_mock.go`, `cdkey_mock.go`, `conditionlog_mock.go`, `magiccli_mock.go`, `magic_config_mock.go` in `domain/interfaces/external/`.

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add domain/interfaces/external/
git commit -m "feat: generate mocks for domain/interfaces/external"
```

---

## Chunk 3: Wire Dependency Injection

Replace manual `initServers()` in `main.go` with Wire.

### Task 9: Add Wire dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add Wire to go.mod**

```bash
go get github.com/google/wire/cmd/wire@latest
go get github.com/google/wire@latest
go mod tidy
```

- [ ] **Step 2: Verify wire binary**

```bash
wire --version
```

Expected: prints a version string.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add google/wire dependency"
```

---

### Task 10: Create `wire.go`

**Files:**
- Create: `wire.go`

- [ ] **Step 1: Create `wire.go`**

**Important:** Wire cannot disambiguate multiple providers that all return `tool.Tool` — it has no way to know which `tool.Tool` to inject where. The strategy used here is to **not register individual `tool.Tool` instances as Wire providers at all**. Instead, each `provide*AgentDep` function receives the underlying domain clients (e.g. `domainext.GalileoAPI`) and constructs its own tool slice inline. This is unambiguous because each domain client type is unique.

```go
//go:build wireinject
// +build wireinject

// Package main 启动oncall agent服务
package main

import (
    "context"
    "time"

    "github.com/google/wire"
    "trpc.group/trpc-go/trpc-agent-go/model/openai"
    "trpc.group/trpc-go/trpc-agent-go/session"
    "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
    "trpc.group/trpc-go/trpc-agent-go/session/summary"
    "trpc.group/trpc-go/trpc-agent-go/tool"
    "trpc.group/trpc-go/trpc-agent-go/tool/webfetch/httpfetch"
    a2aserver "trpc.group/trpc-go/trpc-a2a-go/server"
    sagui "trpc.group/trpc-go/trpc-agent-go/server/agui"

    "git.code.oa.com/trpc-go/trpc-database/gorm"
    "git.code.oa.com/trpc-go/trpc-database/mysql"
    wujisdk "git.code.oa.com/trpc-go/trpc-config-wuji"
    pb "git.woa.com/trpcprotocol/magic/oncall_agent_oncall_agent_debug"
    "git.woa.com/video_pay_middle_platform/pay-go-comm/utils"

    cdkagent "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/agents/cdk_agent"
    magicconfigagent "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/agents/magic_config_agent"
    magicagent "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/agents/magic_oncall_agent"
    repoagent "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/agents/repo_agent"
    ruleengine "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/agents/rule_engine_agent"
    spananalysis "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/agents/span_analysis_agent"
    domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
    domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
    magictool "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/magic_tool"
    mcptool "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/mcp_tool"
    traceanalysis "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/trace_analysis"
    cdkeyquery "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/cdkey_query"
    lingshanquery "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/lingshan_query"
    logquery "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/tools/log_query"
    "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/config/rainbow"
    magicwuji "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/config/wuji"
    cdkeycli "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/http/cdkey"
    galileocli "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/http/galileo"
    lingshancli "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/http/lingshan"
    magiccli "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/http/magiccli"
    conditionlog "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/external/trpc/conditionlog"
    magicconfig "git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/repo/mysql/magic_config"
    "git.woa.com/video_pay_oss/magic_group/oncall_agent/services/a2a"
    "git.woa.com/video_pay_oss/magic_group/oncall_agent/services/agui"
    "git.woa.com/video_pay_oss/magic_group/oncall_agent/services/debug"
    "git.woa.com/video_pay_oss/magic_group/oncall_agent/services/sse"
    oncallutils "git.woa.com/video_pay_oss/magic_group/oncall_agent/utils"
)

// App holds all registered tRPC services
type App struct {
    A2AServers  map[string]*a2aserver.A2AServer
    SSEServers  map[string]sse.API
    AguiServers map[string]*sagui.Server
    DebugSrv    pb.DebugService
}

// ---- infrastructure providers ----

func provideGenConfig(cfg rainbow.AppConfig) domainmodel.GenConfig {
    return domainmodel.GenConfig{
        Temperature: cfg.Temperature,
        MaxTokens:   cfg.MaxTokens,
        TopP:        cfg.TopP,
    }
}

func provideModelInstance(cfg rainbow.AppConfig) *openai.Model {
    return openai.New(cfg.OpenAIModelName,
        openai.WithBaseURL(cfg.OpenAIBaseURL),
        openai.WithAPIKey(cfg.OpenAIAPIKey),
        openai.WithMaxInputTokens(cfg.MaxTokens),
        openai.WithEnableTokenTailoring(true),
    )
}

func provideWujiCli() (domainmodel.WujiAPI, error) {
    wujiMCPCli, err := wujisdk.NewFilter("mcp_tool", []string{"valid"}, "valid=1", magicwuji.MCPTool{})
    if err != nil {
        return nil, err
    }
    agentConfigCli, err := wujisdk.NewFilter("agent_config", []string{"name"}, "is_valid=1", magicwuji.AgentConfig{})
    if err != nil {
        return nil, err
    }
    localToolCli, err := wujisdk.NewFilter("local_tool", []string{"name"}, "", magicwuji.LocalToolConfig{})
    if err != nil {
        return nil, err
    }
    return magicwuji.New(wujiMCPCli, agentConfigCli, localToolCli), nil
}

func provideMagicConfigAPI() (domainext.MagicConfigAPI, error) {
    db, err := gorm.NewClientProxy("trpc.magic.oncall_agent.magic_db")
    if err != nil {
        return nil, err
    }
    if !utils.IsFormalEnv() {
        db = db.Debug()
    }
    return magicconfig.New(db), nil
}

func provideGalileoCli(cfg rainbow.AppConfig) domainext.GalileoAPI {
    return galileocli.New(cfg.BkAppCode, cfg.BkAppToken)
}

func provideLingshanCli(cfg rainbow.AppConfig) domainext.LingshanAPI {
    return lingshancli.New(cfg.XGatewaySecretID, cfg.XGatewaySecretKey)
}

func provideCdkeyCli(cfg rainbow.AppConfig) domainext.CdkeyAPI {
    return cdkeycli.New(cfg.ESUsername, cfg.ESPassword, cfg.FlowPath)
}

func provideConditionLogCli() domainext.ConditionLogAPI {
    return conditionlog.New()
}

func provideMagicCliAPI() domainext.MagicCliAPI {
    return magiccli.New()
}

// ---- domain tool providers ----
// Note: individual tool.Tool instances are NOT registered as Wire providers because Wire
// cannot disambiguate multiple providers with the same return type. Instead, each
// provide*AgentDep function receives the domain-client interfaces directly and
// builds its own []tool.Tool inline.

func provideMCPTool(wujiCli domainmodel.WujiAPI, cfg rainbow.AppConfig) (mcptool.API, error) {
    return mcptool.NewMCPToolImpl(context.Background(), wujiCli, &cfg)
}

func provideTraceConfig(cfg rainbow.AppConfig) traceanalysis.TraceConfig {
    return traceanalysis.TraceConfig{
        MaxTraceDepth:          cfg.MaxTraceDepth,
        MaxSpanNum:             cfg.MaxSpanNum,
        MaxSpanLogLength:       cfg.MaxSpanLogLength,
        SelfTeamName:           cfg.SelfTeamName,
        OtherTeamTruncateDepth: cfg.OtherTeamTruncateDepth,
    }
}

func provideMagicToolDep(w domainmodel.WujiAPI, mc domainext.MagicConfigAPI, m domainext.MagicCliAPI) magictool.Dep {
    return magictool.Dep{WujiCli: w, MagicConfigCli: mc, MagicCliAPI: m}
}

// getLocalToolDesc is a helper (not a Wire provider) used inside provide*AgentDep functions.
func getLocalToolDescWire(wujiCli domainmodel.WujiAPI, name string) string {
    cfg := wujiCli.GetLocalToolConfig(name)
    if cfg != nil && cfg.Description != "" {
        return cfg.Description
    }
    return ""
}

// ---- agent dep providers ----
// Each function builds its own tool instances inline to avoid tool.Tool ambiguity in Wire.

func provideSpanAnalysisDep(
    m *openai.Model,
    w domainmodel.WujiAPI,
    mcp mcptool.API,
    gc domainmodel.GenConfig,
    galileo domainext.GalileoAPI,
    lingshan domainext.LingshanAPI,
) spananalysis.Dep {
    logQueryTool := logquery.New(logquery.Dep{GalileoCli: galileo, WujiCli: w})
    lingshanQueryTool := lingshanquery.New(lingshanquery.Dep{LingshanCli: lingshan, WujiCli: w})
    base64Tool := oncallutils.NewBase64Tool(getLocalToolDescWire(w, "base64_decode"))
    dt2tsTool := oncallutils.NewDateTimeToTimestampMSTool(getLocalToolDescWire(w, "date_time_to_timestamp_ms"))
    ts2dtTool := oncallutils.NewTimestampMSToDateTimeTool(getLocalToolDescWire(w, "timestamp_ms_to_date_time"))
    return spananalysis.Dep{
        ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
        LocalTools: []tool.Tool{logQueryTool, lingshanQueryTool, dt2tsTool, ts2dtTool, base64Tool},
    }
}

func provideRepoAgentDep(
    m *openai.Model,
    w domainmodel.WujiAPI,
    mcp mcptool.API,
    gc domainmodel.GenConfig,
) repoagent.Dep {
    return repoagent.Dep{ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc}
}

func provideMagicOncallDep(
    m *openai.Model,
    w domainmodel.WujiAPI,
    mcp mcptool.API,
    gc domainmodel.GenConfig,
    galileo domainext.GalileoAPI,
    lingshan domainext.LingshanAPI,
    condLog domainext.ConditionLogAPI,
    traceConfig traceanalysis.TraceConfig,
    magicToolDep magictool.Dep,
) (magicagent.Dep, error) {
    traceTool := traceanalysis.New(traceanalysis.Dep{
        GalileoCli: galileo, LingshanCli: lingshan, ConditionLogCli: condLog, WujiCli: w, Cfg: traceConfig,
    })
    logQueryTool := logquery.New(logquery.Dep{GalileoCli: galileo, WujiCli: w})
    lingshanQueryTool := lingshanquery.New(lingshanquery.Dep{LingshanCli: lingshan, WujiCli: w})
    base64Tool := oncallutils.NewBase64Tool(getLocalToolDescWire(w, "base64_decode"))
    dt2tsTool := oncallutils.NewDateTimeToTimestampMSTool(getLocalToolDescWire(w, "date_time_to_timestamp_ms"))
    ts2dtTool := oncallutils.NewTimestampMSToDateTimeTool(getLocalToolDescWire(w, "timestamp_ms_to_date_time"))
    modTypeTool := magictool.NewMagicModTypeInfoTool(magicToolDep)
    actInfoTool := magictool.NewMagicActInfoTool(magicToolDep)
    proposeTool := magictool.NewProposeConfigChangeTool(magicToolDep)
    spanAgentTool, err := spananalysis.NewSpanAnalysisAgentTool(spananalysis.Dep{
        ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
        LocalTools: []tool.Tool{logQueryTool, lingshanQueryTool, dt2tsTool, ts2dtTool, base64Tool},
    })
    if err != nil {
        return magicagent.Dep{}, err
    }
    repoAgentTool, err := repoagent.NewRepoAgentTool(repoagent.Dep{
        ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
    })
    if err != nil {
        return magicagent.Dep{}, err
    }
    return magicagent.Dep{
        ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
        LocalTools: []tool.Tool{
            traceTool, logQueryTool, spanAgentTool, repoAgentTool,
            base64Tool, lingshanQueryTool, dt2tsTool, ts2dtTool,
            modTypeTool, actInfoTool, proposeTool,
        },
    }, nil
}

func provideRuleEngineDep(
    m *openai.Model,
    w domainmodel.WujiAPI,
    mcp mcptool.API,
    gc domainmodel.GenConfig,
    lingshan domainext.LingshanAPI,
) ruleengine.Dep {
    lingshanQueryTool := lingshanquery.New(lingshanquery.Dep{LingshanCli: lingshan, WujiCli: w})
    base64Tool := oncallutils.NewBase64Tool(getLocalToolDescWire(w, "base64_decode"))
    dt2tsTool := oncallutils.NewDateTimeToTimestampMSTool(getLocalToolDescWire(w, "date_time_to_timestamp_ms"))
    ts2dtTool := oncallutils.NewTimestampMSToDateTimeTool(getLocalToolDescWire(w, "timestamp_ms_to_date_time"))
    return ruleengine.Dep{
        ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
        LocalTools: []tool.Tool{base64Tool, lingshanQueryTool, dt2tsTool, ts2dtTool},
    }
}

func provideCdkeyAgentDep(
    m *openai.Model,
    w domainmodel.WujiAPI,
    mcp mcptool.API,
    gc domainmodel.GenConfig,
    cdkey domainext.CdkeyAPI,
) cdkagent.Dep {
    cdkeyQueryTool := cdkeyquery.New(cdkeyquery.Dep{CdkeyCli: cdkey, WujiCli: w})
    dt2tsTool := oncallutils.NewDateTimeToTimestampMSTool(getLocalToolDescWire(w, "date_time_to_timestamp_ms"))
    ts2dtTool := oncallutils.NewTimestampMSToDateTimeTool(getLocalToolDescWire(w, "timestamp_ms_to_date_time"))
    return cdkagent.Dep{
        ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
        LocalTools: []tool.Tool{cdkeyQueryTool, dt2tsTool, ts2dtTool},
    }
}

func provideMagicConfigAgentDep(
    m *openai.Model,
    w domainmodel.WujiAPI,
    mcp mcptool.API,
    gc domainmodel.GenConfig,
    magicToolDep magictool.Dep,
) (magicconfigagent.Dep, error) {
    modTypeTool := magictool.NewMagicModTypeInfoTool(magicToolDep)
    actInfoTool := magictool.NewMagicActInfoTool(magicToolDep)
    proposeTool := magictool.NewProposeConfigChangeTool(magicToolDep)
    webFetchTool := httpfetch.NewTool(
        httpfetch.WithMaxContentLength(20000),
        httpfetch.WithMaxTotalContentLength(50000),
    )
    repoAgentTool, err := repoagent.NewRepoAgentTool(repoagent.Dep{
        ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
    })
    if err != nil {
        return magicconfigagent.Dep{}, err
    }
    return magicconfigagent.Dep{
        ModelInstance: m, WujiCli: w, MCPTool: mcp, GenConfig: gc,
        LocalTools: []tool.Tool{modTypeTool, actInfoTool, proposeTool, webFetchTool, repoAgentTool},
    }, nil
}

// ---- service providers ----

func provideSessionService(cfg rainbow.AppConfig, m *openai.Model) session.Service {
    summarizer := summary.NewSummarizer(m,
        summary.WithChecksAny(
            summary.CheckEventThreshold(cfg.SummarizeEventThreshold),
            summary.CheckTokenThreshold(cfg.SummarizeTokenThreshold),
            summary.CheckTimeThreshold(time.Duration(cfg.SummarizeTimeThreshold)*time.Minute),
        ),
    )
    return inmemory.NewSessionService(
        inmemory.WithSummarizer(summarizer),
        inmemory.WithSessionEventLimit(cfg.SummarizeEventThreshold*2),
    )
}

func provideSSEServers(
    cfg rainbow.AppConfig,
    sessionSvc session.Service,
    magicAgt, ruleAgt, cdkAgt, cfgAgt agent.Agent,
) map[string]sse.API {
    mysqlCli := mysql.NewClientProxy(mysqlFeedbackName)
    return map[string]sse.API{
        sseServiceName:            sse.NewSSEService(sessionSvc, magicAgt, mysqlCli, "magic_oncall_agent", cfg.Debug),
        ruleEngineSSEServiceName:  sse.NewSSEService(sessionSvc, ruleAgt, mysqlCli, "rule_engine_config_agent", cfg.Debug),
        cdkeySSEServiceName:       sse.NewSSEService(sessionSvc, cdkAgt, mysqlCli, "cdkey_oncall_agent", cfg.Debug),
        magicConfigSSEServiceName: sse.NewSSEService(sessionSvc, cfgAgt, mysqlCli, "magic_config_agent", cfg.Debug),
    }
}

func provideA2AServers(magicAgt agent.Agent, sessionSvc session.Service) (map[string]*a2aserver.A2AServer, error) {
    srv, err := a2a.NewA2AServer(a2aServiceName, magicAgt, sessionSvc)
    if err != nil {
        return nil, err
    }
    return map[string]*a2aserver.A2AServer{a2aServiceName: srv}, nil
}

func provideAguiServers(magicAgt agent.Agent, sessionSvc session.Service) (map[string]*sagui.Server, error) {
    srv, err := agui.New(magicAgt, sessionSvc)
    if err != nil {
        return nil, err
    }
    return map[string]*sagui.Server{aguiServiceName: srv}, nil
}

// InitApp wires all dependencies and returns the App.
// cfg is provided by main() after rainbow.Init().
func InitApp(cfg rainbow.AppConfig) (*App, error) {
    wire.Build(
        // config value
        wire.Value(cfg),

        // infra providers — each returns a unique interface type, no ambiguity
        provideGenConfig,
        provideModelInstance,
        provideWujiCli,
        provideMagicConfigAPI,
        provideGalileoCli,
        provideLingshanCli,
        provideCdkeyCli,
        provideConditionLogCli,
        provideMagicCliAPI,

        // shared domain tool deps (unique struct types, no ambiguity)
        provideMCPTool,
        provideTraceConfig,
        provideMagicToolDep,

        // agent dep providers — each builds its own tool slice inline
        provideSpanAnalysisDep,         spananalysis.NewSpanAnalysisAgentTool,
        provideRepoAgentDep,             repoagent.NewRepoAgentTool,
        provideMagicOncallDep,           magicagent.New,
        provideRuleEngineDep,            ruleengine.New,
        provideCdkeyAgentDep,            cdkagent.New,
        provideMagicConfigAgentDep,      magicconfigagent.New,

        // session + services
        provideSessionService,
        provideSSEServers,
        provideA2AServers,
        provideAguiServers,
        debug.New,

        wire.Struct(new(App), "*"),
    )
    return nil, nil
}
```

- [ ] **Step 2: Build (wire.go excluded by build tag)**

```bash
go build ./...
```

Expected: no errors (wire.go is excluded from normal build).

- [ ] **Step 3: Commit**

```bash
git add wire.go
git commit -m "feat: add wire.go DI graph"
```

---

### Task 11: Run Wire and slim down `main.go`

**Files:**
- Create: `wire_gen.go` (auto-generated)
- Modify: `main.go`

- [ ] **Step 1: Run Wire**

```bash
wire gen .
```

Expected: `wire_gen.go` created with `func InitApp(cfg rainbow.AppConfig) (*App, error)`.

If Wire fails with "cannot distinguish" errors for `tool.Tool` (multiple providers returning the same type), introduce a `provide*` helper for each conflicting tool that returns a named wrapper, or use `wire.NewSet` with `wire.Value`. Fix the providers in `wire.go` as indicated by the error and re-run.

- [ ] **Step 2: Slim down `main.go`**

Replace the `initServers()` function and `getLocalToolDesc()` helper with a call to `InitApp(rainbow.GetCfg())`. The `main()` function becomes:

```go
func main() {
    s := trpc.NewServer(server.WithFilter(reqFilter))

    if err := rainbow.Init(); err != nil {
        log.Fatalf("rainbow.Init failed: %v", err)
    }

    app, err := InitApp(rainbow.GetCfg())
    if err != nil {
        log.Fatalf("InitApp failed: %v", err)
    }

    for name, srv := range app.A2AServers {
        if err := a2atrpc.RegisterA2AServer(s, name, srv); err != nil {
            log.Fatalf("RegisterA2AServer failed: %v", err)
        }
    }
    for name, srv := range app.SSEServers {
        thttp.HandleFunc(pathMap[name], srv.HandleSSE)
        thttp.RegisterNoProtocolService(s.Service(name))
    }
    for name, srv := range app.AguiServers {
        if err := tagui.RegisterAGUIServer(s, name, srv); err != nil {
            log.Fatalf("RegisterAGUIServer failed: %v", err)
        }
    }
    pb.RegisterDebugService(s, app.DebugSrv)

    if err := s.Serve(); err != nil {
        log.Fatalf("serve failed: %v", err)
    }
}
```

- [ ] **Step 3: Build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Lint**

```bash
golangci-lint run
```

Fix any issues.

- [ ] **Step 5: Commit**

```bash
git add wire_gen.go main.go
git commit -m "refactor: replace initServers() with Wire-generated InitApp()"
```

---

## Chunk 4: consts, README, and final cleanup

### Task 12: Add `consts/` package

**Files:**
- Create: `consts/errors.go`

- [ ] **Step 1: Create `consts/errors.go`**

```go
// Package consts 定义错误码、枚举和全局常量
package consts

import "git.code.oa.com/trpc-go/trpc-go/errs"

// 业务错误码定义
// 使用方式: errs.New(consts.ErrCodeInvalidParam, "invalid param")
const (
    // ErrCodeInvalidParam 无效参数
    ErrCodeInvalidParam = 10001
)

// 预定义错误
var (
    // ErrInvalidParam 无效参数错误
    ErrInvalidParam = errs.New(ErrCodeInvalidParam, "invalid param")
)
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add consts/
git commit -m "feat: add consts package with error codes"
```

---

### Task 13: Add README troubleshooting guide

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add troubleshooting section to README.md**

Append the following section:

```markdown
## 问题排查指南

### 日志字段说明

每条请求日志由 `reqFilter` 注入以下固定字段（可在日志平台 Tag 搜索）：

| 字段 | 说明 |
|---|---|
| `service` | 被调服务名，如 `trpc.magic.oncall_agent.sse` |
| `method` | 被调方法名，如 `HandleSSE` |
| `env` | 环境标识，正式环境为 `production` |
| `req` | 请求体 JSON |
| `rsp` | 响应体 JSON |
| `ret` | tRPC 错误码，`0` 表示成功 |
| `cost_time` | 接口耗时（纳秒） |

业务逻辑中通过 `log.WithContextFields(ctx, "key", val)` 注入的字段也可在 Tag 搜索中查询，如 `session_id`、`user_id` 等。

### 日志检索方法

**Galileo 日志平台**（https://galileo.woa.com）：

- **Tag 搜索**（精确）：在 "标签搜索" 中输入 `key=value`，例如 `ret=10001` 查所有该错误码请求。
- **正文搜索**（模糊）：在 "日志正文" 中搜索关键字，如 `"InitApp failed"` 定位启动错误。
- **TraceId 追踪**：在 Tag 搜索中输入 `trace_id=<value>` 串联完整链路。

### 常见问题排查流程

**用户反馈 Agent 无响应：**
1. Galileo 搜索 `service=trpc.magic.oncall_agent.sse` + 时间范围
2. 过滤 `ret!=0` 找到异常请求
3. 取 `trace_id` 搜索完整链路
4. 查看 LLM 调用耗时和工具调用结果

**配置不生效：**
1. 检查 rainbow 日志中 `load rainbow cfg` 输出
2. 检查 wuji 表配置 `is_valid=1`
3. 查看 `method=HandleSSE` 请求的 `req` 字段确认入参

**性能问题：**
1. 过滤 `cost_time > 30000000000`（30s）的请求
2. 查看 trace 链路定位慢工具调用
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add troubleshooting guide to README"
```

---

### Task 14: Final verification

- [ ] **Step 1: Full build**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 2: Tests**

```bash
GOLANG_PROTOBUF_REGISTRATION_CONFLICT=ignore go test -race -gcflags=all=-l ./...
```

Expected: all pass.

- [ ] **Step 3: Lint**

```bash
golangci-lint run
```

Expected: no issues.

- [ ] **Step 4: Verify no domain→infra imports remain**

```bash
grep -rn '".*infrastructure' domain/
```

Expected: no output (domain packages must never import infrastructure paths).

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "refactor: complete DDD compliance — domain/interfaces, domain/model, Wire DI"
```
