// Package magictool 提供魔方平台配置管理工具
package magictool

import (
	"time"

	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=magic_tool_api.go --destination=magic_tool_mock.go --package=magictool

// Dep magictool 依赖的外部接口
type Dep struct {
	// WujiCli 无极配置客户端
	WujiCli domainmodel.WujiAPI
	// MagicConfigCli 魔方配置数据库客户端
	MagicConfigCli domainext.MagicConfigAPI
	// MagicCliAPI 魔方加密接口客户端
	MagicCliAPI domainext.MagicCliAPI
}

// ==================== Enums ====================

// ConditionSourceType 条件来源类型/输出计算类型
type ConditionSourceType int

const (
	// ConditionSourceTypeFixed 按固定值输出 (ConditionCalFixed)
	ConditionSourceTypeFixed ConditionSourceType = 1
	// ConditionSourceTypeRelated 按比例输出 (ConditionCalRelated)
	ConditionSourceTypeRelated ConditionSourceType = 2
	// ConditionSourceTypeConsumed 按消耗输出 (ConditionCalConsumed)
	ConditionSourceTypeConsumed ConditionSourceType = 3
)

// ConditionOp 条件比较操作符
type ConditionOp int

const (
	ConditionOpEqual          ConditionOp = 1  // 等于 (=)
	ConditionOpNotEqual       ConditionOp = 2  // 不等于 (!=)
	ConditionOpGreater        ConditionOp = 3  // 大于 (>)
	ConditionOpGreaterOrEqual ConditionOp = 4  // 大于等于 (>=)
	ConditionOpLess           ConditionOp = 5  // 小于 (<)
	ConditionOpLessOrEqual    ConditionOp = 6  // 小于等于 (<=)
	ConditionOpInRange        ConditionOp = 7  // 在范围内 (range)
	ConditionOpNotInRange     ConditionOp = 8  // 不在范围内 (not range)
	ConditionOpIn             ConditionOp = 9  // 包含于 (in)
	ConditionOpNotIn          ConditionOp = 10 // 不包含于 (not in)
)

// GetMagicActInfoReq 获取魔方活动信息请求
type GetMagicActInfoReq struct {
	ActID     int32 `json:"act_id" jsonschema:"required,description=需要分析的act id"`
	IsFormal  bool  `json:"is_formal" jsonschema:"required,description=是否查询正式库，true为正式库，false为测试库"`
}

// ActInfo 活动信息
type ActInfo struct {
	Name            string    `json:"name" jsonschema:"required,description=活动名称"`
	Desc            string    `json:"desc" jsonschema:"required,description=活动描述"`
	Developer       string    `json:"developer" jsonschema:"required,description=活动开发者"`
	LastPublishTime time.Time `json:"last_publish_time" jsonschema:"required,description=最后发布时间"`
}

// GetMagicActInfoRsp 获取魔方活动信息响应
type GetMagicActInfoRsp struct {
	ActInfo          ActInfo                        `json:"act_info" jsonschema:"required,description=活动信息"`
	ModInfos         []*domainmodel.ModuleInfo      `json:"mod_infos" jsonschema:"required,description=模块信息"`
	ModTypeInfo      string                         `json:"mod_type_infos" jsonschema:"required,description=模块类型=>类型详细信息"`
	ModuleConditions map[int32]*ModuleConditionInfo `json:"module_conditions" jsonschema:"required,description=模块条件信息，key为模块ID"`
}

// GetMagicModTypeInfoReq 获取魔方模块类型信息请求
type GetMagicModTypeInfoReq struct {
	ModTypeIDs         []int32  `json:"mod_type_id" jsonschema:"description=需要分析的mod type id列表。若不为空，任何匹配的模块类型ID都会包含在响应中"`
	ModNames           []string `json:"mod_name" jsonschema:"description=需要分析的模块名称列表。使用子串匹配检索，任何匹配的模块名称都会包含在响应中。两个过滤条件是OR关系"`
	RequiresConfigInfo bool     `json:"requires_config_info" jsonschema:"required,description=是否需要配置信息"`
	IsFormal           bool     `json:"is_formal" jsonschema:"required,description=是否查询正式库，true为正式库，false为测试库"`
}

// GetMagicModTypeInfoRsp 获取魔方模块类型信息响应
type GetMagicModTypeInfoRsp struct {
	ModTypeInfos string `json:"mod_type_infos" jsonschema:"required,description=模块类型=>类型详细信息"`
}

// ModTypeInfo 模块类型信息
type ModTypeInfo struct {
	ModType                  int32             `json:"mod_type" jsonschema:"required,description=模块类型"`
	ModName                  string            `json:"mod_name" jsonschema:"required,description=模块名称"`
	OutputTypes              []ModOutputType   `json:"output_types" jsonschema:"required,description=模块输出的条件(输出条件)"`
	OpTypes                  []ModOpType       `json:"op_types" jsonschema:"required,description=模块操作类型(接口配置)"`
	UISchema                 string            `json:"ui_schema,omitempty" jsonschema:"description=模块配置的UI schema"`
	SampleConfig             string            `json:"sample_config,omitempty" jsonschema:"description=模块示例配置。仅当RequiresConfigInfo为true时返回"`
	Target                   string            `json:"target,omitempty" jsonschema:"description=模块实现的服务名"`
	Desc                     string            `json:"desc" jsonschema:"required,description=模块描述"`
	AvailableSecurityOptions []*SecurityOption `json:"available_security_options" jsonschema:"required,description=可用的安全选项"`
}

// SecurityOption 安全选项
type SecurityOption struct {
	ID   string `json:"id" jsonschema:"required,description=安全选项id"`
	Name string `json:"name" jsonschema:"required,description=安全选项名称"`
}

// ModOutputType 模块输出类型
type ModOutputType struct {
	ID             int32                      `json:"id" jsonschema:"required,description=输出类型id"`
	CnName         string                     `json:"cn_name" jsonschema:"required,description=中文名称"`
	Desc           string                     `json:"desc" jsonschema:"required,description=描述"`
	OutputDataType domainmodel.OutputDataType `json:"type" jsonschema:"required,description=输出数据类型"`
}

// ModOpType 模块操作类型(接口配置)
type ModOpType struct {
	Option int32  `json:"id" jsonschema:"required,description=操作类型id"`
	CnName string `json:"cn_name" jsonschema:"required,description=中文名称"`
	Desc   string `json:"desc" jsonschema:"required,description=描述"`
}

// ModuleConditionInfo 模块条件信息
type ModuleConditionInfo struct {
	ModuleID        int32                   `json:"module_id" jsonschema:"required,description=模块ID"`
	ModuleName      string                  `json:"module_name" jsonschema:"required,description=模块名称"`
	ModuleType      int32                   `json:"module_type" jsonschema:"required,description=模块类型"`
	ConditionGroups []*ConditionGroupConfig `json:"condition_groups" jsonschema:"required,description=条件组列表"`
}

// ConditionGroupConfig 条件组配置
type ConditionGroupConfig struct {
	ConditionID int32                 `json:"condition_id" jsonschema:"description=条件组ID，如果为0则创建新条件组，否则更新现有条件组"`
	Result      int                   `json:"result" jsonschema:"required,description=条件结果配置值"`
	Name        string                `json:"name" jsonschema:"description=条件组名称"`
	SourceType  ConditionSourceType   `json:"source_type" jsonschema:"required,description=条件输出类型"`
	ScoreResult string                `json:"score_result" jsonschema:"required,description=资格配置值"`
	Priority    int                   `json:"priority" jsonschema:"required,description=条件组优先级"`
	CondName    string                `json:"cond_name" jsonschema:"description=条件组名称"`
	Operator    string                `json:"operator" jsonschema:"required,description=操作人"`
	Items       []ConditionItemConfig `json:"items" jsonschema:"required,description=条件项配置列表"`
}

// ConditionItemConfig 条件项配置
type ConditionItemConfig struct {
	ItemID            int32       `json:"item_id" jsonschema:"description=条件项ID，如果为0则创建新条件项，否则更新现有条件项"`
	OutputType        int         `json:"output_type" jsonschema:"required,description=输出类型ID"`
	Value             int         `json:"value" jsonschema:"required,description=期望值"`
	Op                ConditionOp `json:"op" jsonschema:"required,description=比较操作符"`
	RelateModuleID    int         `json:"relate_module_id" jsonschema:"required,description=关联模块ID"`
	ModuleID          int         `json:"module_id" jsonschema:"required,description=被配置条件的模块ID"`
	IsRelateOutput    int         `json:"is_relate_output" jsonschema:"description=是否关联输出"`
	FailedMsg         string      `json:"failed_msg" jsonschema:"description=若条件不通过时应当给用户展示的自定义报错消息"`
	Sort              int         `json:"sort" jsonschema:"description=条件项排序优先级"`
	ConsumeNumPerTime int         `json:"consume_num_per_time" jsonschema:"description=每次消耗数量"`
	Operator          string      `json:"operator" jsonschema:"required,description=操作人，固定为magic_agent"`
}


