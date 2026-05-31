// Package external 定义领域层对外部依赖的接口（端口）
package external

import (
	"context"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

//go:generate mockgen --source=magic_config_api.go --destination=magic_config_mock.go --package=external

// MagicConfigAPI 魔方配置数据库接口（只读）
// isFormal=true 时查询正式库，isFormal=false 时查询测试库
type MagicConfigAPI interface {
	GetActInfoByID(ctx context.Context, actID int, isFormal bool) (*domainmodel.ActInfo, error)
	GetModuleInfoByActID(ctx context.Context, actID int, isFormal bool) ([]*domainmodel.ModuleInfo, error)
	GetModuleInfoByID(ctx context.Context, moduleID int, isFormal bool) (*domainmodel.ModuleInfo, error)
	GetModuleTypesByFuzzyNames(ctx context.Context, names []string, isFormal bool) ([]*domainmodel.ModuleType, error)
	GetLatestSampleConfigByModuleTypeID(ctx context.Context, modTypeID int, isFormal bool) (string, error)
	GetCompleteModuleTypeInfo(ctx context.Context, modTypeID int, isFormal bool) (*domainmodel.CompleteModuleTypeInfo, error)
	GetModuleSecurityByModuleTypeID(ctx context.Context, modTypeID int, isFormal bool) (*domainmodel.ModuleSecurity, error)
	GetConditionsByModuleIDs(ctx context.Context, moduleIDs []int, isFormal bool) (map[int][]*domainmodel.Condition, error)
	GetConditionByID(ctx context.Context, conditionID int, isFormal bool) (*domainmodel.Condition, error)
	GetConditionItemsByConditionIDs(ctx context.Context, conditionIDs []int, isFormal bool) (map[int][]*domainmodel.ConditionItem, error)
	GetConditionItemByID(ctx context.Context, itemID int, isFormal bool) (*domainmodel.ConditionItem, error)
}
