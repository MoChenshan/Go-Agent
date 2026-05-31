// Package magictool 提供魔方平台配置管理工具
package magictool

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"git.code.oa.com/trpc-go/trpc-go/log"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

const (
	defaultMagicActInfoDesc = `get_magic_act_info可以获取魔方活动配置
`
	magicActInfoName = "get_magic_act_info"
)

// MagicToolImpl 魔方工具实现
type MagicToolImpl struct {
	dep Dep
}

// NewMagicToolImpl 创建魔方工具实现（供services层直接调用工具方法时使用）
func NewMagicToolImpl(dep Dep) *MagicToolImpl {
	return &MagicToolImpl{dep: dep}
}

// NewMagicActInfoTool 新建获取魔方活动信息工具
func NewMagicActInfoTool(dep Dep) tool.Tool {
	desc := defaultMagicActInfoDesc
	if dep.WujiCli != nil {
		config := dep.WujiCli.GetLocalToolConfig(magicActInfoName)
		if config != nil && config.Description != "" {
			desc = config.Description
		}
	}
	impl := &MagicToolImpl{dep: dep}
	return function.NewFunctionTool(
		impl.GetMagicActInfo,
		function.WithName(magicActInfoName),
		function.WithDescription(desc),
	)
}

// GetMagicActInfo 获取魔方活动配置信息
func (m *MagicToolImpl) GetMagicActInfo(ctx context.Context, req GetMagicActInfoReq) (GetMagicActInfoRsp, error) {
	// 获取活动信息
	actInfo, err := m.dep.MagicConfigCli.GetActInfoByID(ctx, int(req.ActID), req.IsFormal)
	if err != nil {
		log.ErrorContextf(ctx, "GetActInfoByID err: %+v", err)
		return GetMagicActInfoRsp{}, err
	}
	if actInfo == nil {
		log.ErrorContextf(ctx, "GetActInfoByID: act not found, actID: %d", req.ActID)
		return GetMagicActInfoRsp{}, fmt.Errorf("activity not found for actID: %d", req.ActID)
	}
	// 获取活动下的模块信息
	modInfos, err := m.dep.MagicConfigCli.GetModuleInfoByActID(ctx, int(req.ActID), req.IsFormal)
	if err != nil {
		log.ErrorContextf(ctx, "GetModuleInfoByActID err: %+v", err)
		return GetMagicActInfoRsp{}, err
	}
	// 获取模块类型信息
	uniqModTypeList := collectUniqueModTypes(modInfos)
	modTypeRsp, err := m.GetMagicModTypeInfo(ctx, GetMagicModTypeInfoReq{
		ModTypeIDs:         uniqModTypeList,
		RequiresConfigInfo: false,
		IsFormal:           req.IsFormal,
	})
	if err != nil {
		log.ErrorContextf(ctx, "GetMagicModTypeInfo err: %+v", err)
		return GetMagicActInfoRsp{}, err
	}

	moduleConditions := m.getModuleConditions(ctx, modInfos, req.IsFormal)

	return GetMagicActInfoRsp{
		ActInfo: ActInfo{
			Name:            actInfo.ActName,
			Desc:            actInfo.Desc,
			Developer:       actInfo.Developer,
			LastPublishTime: actInfo.LastPublishTime,
		},
		ModInfos:         modInfos,
		ModTypeInfo:      modTypeRsp.ModTypeInfos,
		ModuleConditions: moduleConditions,
	}, nil
}

// collectUniqueModTypes 从模块列表中提取去重的模块类型ID
func collectUniqueModTypes(modInfos []*domainmodel.ModuleInfo) []int32 {
	uniqModType := make(map[int32]struct{})
	uniqModTypeList := make([]int32, 0)
	for _, modInfo := range modInfos {
		if _, ok := uniqModType[modInfo.Type]; !ok {
			uniqModType[modInfo.Type] = struct{}{}
			uniqModTypeList = append(uniqModTypeList, modInfo.Type)
		}
	}
	return uniqModTypeList
}

// getModuleConditions 批量获取所有模块的条件信息
func (m *MagicToolImpl) getModuleConditions(
	ctx context.Context, modInfos []*domainmodel.ModuleInfo, isFormal bool,
) map[int32]*ModuleConditionInfo {
	moduleConditions := make(map[int32]*ModuleConditionInfo)
	if len(modInfos) == 0 {
		return moduleConditions
	}
	// 收集所有模块ID
	moduleIDs := make([]int, 0, len(modInfos))
	for _, modInfo := range modInfos {
		moduleIDs = append(moduleIDs, modInfo.ID)
	}
	// 批量获取所有模块的条件组
	allConditions, err := m.dep.MagicConfigCli.GetConditionsByModuleIDs(ctx, moduleIDs, isFormal)
	if err != nil {
		log.ErrorContextf(ctx, "GetConditionsByModuleIDs err: %+v", err)
		return moduleConditions // 非关键逻辑，记录日志后继续
	}
	// 收集所有条件ID
	conditionIDs := make([]int, 0)
	for _, conditions := range allConditions {
		for _, cond := range conditions {
			conditionIDs = append(conditionIDs, cond.ID)
		}
	}
	// 批量获取所有条件项
	allConditionItems := m.getConditionItems(ctx, conditionIDs, isFormal)
	// 按模块整理条件数据
	for _, modInfo := range modInfos {
		moduleID := int32(modInfo.ID)
		conditions := allConditions[modInfo.ID]
		conditionGroups := m.buildConditionGroups(conditions, allConditionItems)
		moduleConditions[moduleID] = &ModuleConditionInfo{
			ModuleID:        moduleID,
			ModuleName:      modInfo.Name,
			ModuleType:      modInfo.Type,
			ConditionGroups: conditionGroups,
		}
	}
	return moduleConditions
}

// getConditionItems 批量获取条件项
func (m *MagicToolImpl) getConditionItems(
	ctx context.Context, conditionIDs []int, isFormal bool,
) map[int][]*domainmodel.ConditionItem {
	if len(conditionIDs) == 0 {
		return make(map[int][]*domainmodel.ConditionItem)
	}
	allConditionItems, err := m.dep.MagicConfigCli.GetConditionItemsByConditionIDs(ctx, conditionIDs, isFormal)
	if err != nil {
		log.ErrorContextf(ctx, "GetConditionItemsByConditionIDs err: %+v", err)
		return make(map[int][]*domainmodel.ConditionItem)
	}
	return allConditionItems
}

// buildConditionGroups 根据条件和条件项构建条件组列表
func (m *MagicToolImpl) buildConditionGroups(
	conditions []*domainmodel.Condition,
	allConditionItems map[int][]*domainmodel.ConditionItem,
) []*ConditionGroupConfig {
	conditionGroups := make([]*ConditionGroupConfig, 0, len(conditions))
	for _, cond := range conditions {
		items := allConditionItems[cond.ID]
		conditionItems := make([]ConditionItemConfig, 0, len(items))
		for _, item := range items {
			conditionItems = append(conditionItems, ConditionItemConfig{
				ItemID:            int32(item.ID),
				OutputType:        item.OutputType,
				Value:             item.Value,
				Op:                ConditionOp(item.Op),
				RelateModuleID:    item.RelateModuleID,
				IsRelateOutput:    item.IsRelateOutput,
				FailedMsg:         item.FailedMsg,
				Sort:              item.Sort,
				ConsumeNumPerTime: item.ConsumeNumPerTime,
				Operator:          item.Operator,
			})
		}
		conditionGroups = append(conditionGroups, &ConditionGroupConfig{
			ConditionID: int32(cond.ID),
			Result:      cond.Result,
			Name:        cond.Name,
			SourceType:  ConditionSourceType(cond.SourceType),
			ScoreResult: cond.ScoreResult,
			Priority:    cond.Priority,
			CondName:    cond.CondName,
			Operator:    cond.Operator,
			Items:       conditionItems,
		})
	}
	return conditionGroups
}
