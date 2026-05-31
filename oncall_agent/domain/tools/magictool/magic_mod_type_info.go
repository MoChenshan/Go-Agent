// Package magictool 提供魔方平台配置管理工具
package magictool

import (
	"context"
	"strings"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.woa.com/video_pay_middle_platform/pay-go-comm/utils"

	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

const (
	defaultMagicModTypeInfoDesc = `get_magic_mod_type_info可以获取魔方模块类型的准确详细描述, 由于魔方知识库中的信息可能不准确, 必须使用此工具获取准确的模块类型信息
`
	magicModTypeInfoName = "get_magic_mod_type_info"
)

// NewMagicModTypeInfoTool 新建获取魔方模块类型信息工具
func NewMagicModTypeInfoTool(dep Dep) tool.Tool {
	desc := defaultMagicModTypeInfoDesc
	if dep.WujiCli != nil {
		config := dep.WujiCli.GetLocalToolConfig(magicModTypeInfoName)
		if config != nil && config.Description != "" {
			desc = config.Description
		}
	}
	impl := &MagicToolImpl{dep: dep}
	return function.NewFunctionTool(
		impl.GetMagicModTypeInfo,
		function.WithName(magicModTypeInfoName),
		function.WithDescription(desc),
	)
}

// GetMagicModTypeInfo 获取魔方模块类型信息
func (m *MagicToolImpl) GetMagicModTypeInfo(ctx context.Context, req GetMagicModTypeInfoReq) (GetMagicModTypeInfoRsp, error) {
	// 收集所有模块类型ID
	moduleTypeIDs, err := m.collectModuleTypeIDs(ctx, req)
	if err != nil {
		return GetMagicModTypeInfoRsp{}, err
	}
	log.DebugContextf(ctx, "moduleTypeIDs: %+v", moduleTypeIDs)

	// 并行处理所有模块类型
	modTypeInfos, err := m.processModuleTypesInParallel(ctx, moduleTypeIDs, req.RequiresConfigInfo, req.IsFormal)
	if err != nil {
		return GetMagicModTypeInfoRsp{}, err
	}

	return GetMagicModTypeInfoRsp{
		ModTypeInfos: utils.MustToJSON(modTypeInfos),
	}, nil
}

// collectModuleTypeIDs 从请求参数中收集所有模块类型ID
func (m *MagicToolImpl) collectModuleTypeIDs(ctx context.Context, req GetMagicModTypeInfoReq) (map[int32]bool, error) {
	moduleTypeIDs := make(map[int32]bool)

	for _, modTypeID := range req.ModTypeIDs {
		moduleTypeIDs[modTypeID] = true
	}

	if len(req.ModNames) > 0 {
		moduleTypes, err := m.dep.MagicConfigCli.GetModuleTypesByFuzzyNames(ctx, req.ModNames, req.IsFormal)
		if err != nil {
			log.ErrorContextf(ctx, "根据名称查询模块类型失败, modNames: %v, err: %v", req.ModNames, err)
			return nil, err
		}
		for _, moduleType := range moduleTypes {
			moduleTypeIDs[int32(moduleType.ID)] = true
		}
	}

	return moduleTypeIDs, nil
}

// processModuleTypesInParallel 并行处理所有模块类型
func (m *MagicToolImpl) processModuleTypesInParallel(ctx context.Context, moduleTypeIDs map[int32]bool, requiresConfigInfo bool, isFormal bool) (map[int32]*ModTypeInfo, error) {
	var (
		modTypeInfos = make(map[int32]*ModTypeInfo)
		handlers     = make([]func() error, 0, len(moduleTypeIDs))
		mu           sync.Mutex
	)

	for modTypeID := range moduleTypeIDs {
		currentModTypeID := modTypeID
		handlers = append(handlers, func() error {
			modTypeInfo, err := m.processModuleType(ctx, currentModTypeID, requiresConfigInfo, isFormal)
			if modTypeInfo == nil || err != nil {
				log.ErrorContextf(ctx, "获取模块类型信息失败, modTypeID: %d, err: %v", currentModTypeID, err)
				// 非关键逻辑，忽略
				return nil
			}

			mu.Lock()
			modTypeInfos[currentModTypeID] = modTypeInfo
			mu.Unlock()
			return nil
		})
	}

	if err := trpc.GoAndWait(handlers...); err != nil {
		log.ErrorContextf(ctx, "获取模块类型信息失败, err: %v", err)
		return nil, err
	}

	return modTypeInfos, nil
}

// processModuleType 处理单个模块类型，并行获取所有相关数据
func (m *MagicToolImpl) processModuleType(ctx context.Context, modTypeID int32, requiresConfigInfo bool, isFormal bool) (*ModTypeInfo, error) {
	var (
		completeInfo   *domainmodel.CompleteModuleTypeInfo
		sampleConfig   string
		moduleSecurity *domainmodel.ModuleSecurity
	)

	handlers := []func() error{
		func() error {
			info, err := m.dep.MagicConfigCli.GetCompleteModuleTypeInfo(ctx, int(modTypeID), isFormal)
			if err != nil {
				log.ErrorContextf(ctx, "获取模块类型信息失败, modTypeID: %d, err: %v", modTypeID, err)
				return err
			}
			completeInfo = info
			return nil
		},
		func() error {
			security, err := m.dep.MagicConfigCli.GetModuleSecurityByModuleTypeID(ctx, int(modTypeID), isFormal)
			if err != nil {
				log.ErrorContextf(ctx, "获取模块安全策略失败, modTypeID: %d, err: %v", modTypeID, err)
				return err
			}
			moduleSecurity = security
			return nil
		},
	}
	if requiresConfigInfo {
		handlers = append(handlers,
			func() error {
				config, err := m.dep.MagicConfigCli.GetLatestSampleConfigByModuleTypeID(ctx, int(modTypeID), isFormal)
				if err != nil {
					log.ErrorContextf(ctx, "获取模块示例配置失败, modTypeID: %d, err: %v", modTypeID, err)
					return err
				}
				sampleConfig = config
				return nil
			},
		)
	}

	if err := trpc.GoAndWait(handlers...); err != nil {
		return nil, err
	}

	// 忽略废弃的模块
	if completeInfo == nil || completeInfo.ModuleType == nil {
		return nil, nil
	}
	if completeInfo.ModuleType.Type != 1 || strings.Contains(completeInfo.ModuleType.Name, "废弃") {
		return nil, nil
	}

	return m.buildModTypeInfo(modTypeID, completeInfo, sampleConfig, moduleSecurity), nil
}

// buildModTypeInfo 从获取的数据构建ModTypeInfo
func (m *MagicToolImpl) buildModTypeInfo(
	modTypeID int32,
	completeInfo *domainmodel.CompleteModuleTypeInfo,
	sampleConfig string,
	moduleSecurity *domainmodel.ModuleSecurity,
) *ModTypeInfo {
	modTypeInfo := &ModTypeInfo{
		ModType: modTypeID,
		ModName: completeInfo.ModuleType.Name,
		Desc:    completeInfo.ModuleType.Desc,
	}

	modTypeInfo.OutputTypes = convertOutputTypes(completeInfo.OutputTypes)
	modTypeInfo.OpTypes = convertOpTypes(completeInfo.ModuleOpTypes)

	if completeInfo.UISchema != nil {
		modTypeInfo.UISchema = completeInfo.UISchema.Config
	}

	if sampleConfig != "" {
		modTypeInfo.SampleConfig = sampleConfig
	}

	if inputType := completeInfo.InputTypes; inputType != nil {
		// 首先尝试解析业务接口寻址target 若为空则尝试解析条件接口target
		var polarisTarget = inputType.AccessData
		if polarisTarget == "" {
			polarisTarget = inputType.AccessResultData
		}
		modTypeInfo.Target = parseRelatedService(polarisTarget)
	}

	if moduleSecurity != nil {
		modTypeInfo.AvailableSecurityOptions = parseSecurityOptions(moduleSecurity.SecurityID, moduleSecurity.SecurityDesc)
	}

	return modTypeInfo
}

// convertOutputTypes 将数据库输出类型转换为响应格式
func convertOutputTypes(outputTypes []*domainmodel.OutputType) []ModOutputType {
	result := make([]ModOutputType, 0, len(outputTypes))
	for _, outputType := range outputTypes {
		result = append(result, ModOutputType{
			ID:             int32(outputType.ID),
			CnName:         outputType.Name,
			Desc:           outputType.Desc,
			OutputDataType: outputType.DataType,
		})
	}
	return result
}

// convertOpTypes 将数据库操作类型转换为响应格式
func convertOpTypes(opTypes []*domainmodel.ModuleOpType) []ModOpType {
	result := make([]ModOpType, 0, len(opTypes))
	for _, opType := range opTypes {
		result = append(result, ModOpType{
			Option: int32(opType.OpTypeID),
			CnName: opType.Name,
			Desc:   opType.Desc,
		})
	}
	return result
}

// parseRelatedService 解析模块类型的业务接口寻址target，获得相关服务名
// 样例："trpc.magic.condition_match_server.ConditionMatch" => "magic.condition_match_server"
func parseRelatedService(accessProData string) string {
	parts := strings.Split(accessProData, ".")
	if len(parts) < 3 {
		return ""
	}
	middleParts := parts[1 : len(parts)-1]
	return strings.Join(middleParts, ".")
}

// parseSecurityOptions 解析逗号分隔的安全选项ID和描述为SecurityOption列表
func parseSecurityOptions(securityIDs, securityDescs string) []*SecurityOption {
	if securityIDs == "" {
		return nil
	}

	idParts := strings.Split(securityIDs, ",")
	descParts := strings.Split(securityDescs, ",")

	var options []*SecurityOption
	for i, id := range idParts {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}

		desc := ""
		if i < len(descParts) {
			desc = strings.TrimSpace(descParts[i])
		}

		options = append(options, &SecurityOption{
			ID:   id,
			Name: desc,
		})
	}

	return options
}
