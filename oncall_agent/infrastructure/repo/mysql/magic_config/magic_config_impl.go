package magicconfig

import (
	"context"
	"errors"

	"gorm.io/gorm"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/errs"
	"git.code.oa.com/trpc-go/trpc-go/log"

	"git.woa.com/video_pay_oss/magic_group/oncall_agent/consts"
	domainext "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/interfaces/external"
	domainmodel "git.woa.com/video_pay_oss/magic_group/oncall_agent/domain/model"
)

type magicConfigImpl struct {
	dbTest   *gorm.DB
	dbFormal *gorm.DB
}

// New 创建一个 magicConfigImpl 实例
func New(dbTest *gorm.DB, dbFormal *gorm.DB) domainext.MagicConfigAPI {
	return &magicConfigImpl{
		dbTest:   dbTest,
		dbFormal: dbFormal,
	}
}

// GetDB 根据 isFormal 参数选择数据库连接
func (m *magicConfigImpl) GetDB(isFormal bool) *gorm.DB {
	if isFormal {
		return m.dbFormal
	}
	return m.dbTest
}

// ==================== domainmodel.ActInfo 相关操作 ====================

// GetActInfoByID 根据ID获取活动信息
func (m *magicConfigImpl) GetActInfoByID(ctx context.Context, id int, isFormal bool) (*domainmodel.ActInfo, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.ActInfo
	err := db.WithContext(ctx).Where("c_id = ?", id).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询活动信息失败, id: %d, err: %v", id, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询活动信息失败")
	}
	return &result, nil
}

// ==================== domainmodel.ModuleInfo 相关操作 ====================

// GetModuleInfoByID 根据ID获取模块信息
func (m *magicConfigImpl) GetModuleInfoByID(ctx context.Context, id int, isFormal bool) (*domainmodel.ModuleInfo, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.ModuleInfo
	err := db.WithContext(ctx).Where("c_id = ?", id).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询模块信息失败, id: %d, err: %v", id, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询模块信息失败")
	}
	return &result, nil
}

// GetModuleInfoByActID 根据活动ID获取模块信息列表, 仅返回不在回收站中的模块
func (m *magicConfigImpl) GetModuleInfoByActID(ctx context.Context, actID int, isFormal bool) ([]*domainmodel.ModuleInfo, error) {
	db := m.GetDB(isFormal)
	var results []*domainmodel.ModuleInfo
	err := db.WithContext(ctx).Where("c_act_id = ? AND c_enable = 1", actID).Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "根据活动ID查询模块信息失败, actID: %d, err: %v", actID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据活动ID查询模块信息失败")
	}
	return results, nil
}

// ==================== domainmodel.ModuleType 相关操作 ====================

// GetModuleTypeByID 根据ID获取模块类型
func (m *magicConfigImpl) GetModuleTypeByID(ctx context.Context, id int, isFormal bool) (*domainmodel.ModuleType, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.ModuleType
	err := db.WithContext(ctx).Where("c_id = ?", id).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询模块类型失败, id: %d, err: %v", id, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询模块类型失败")
	}
	return &result, nil
}

// GetModuleTypesByFuzzyNames 根据模块名称模糊查询模块类型
func (m *magicConfigImpl) GetModuleTypesByFuzzyNames(ctx context.Context, names []string, isFormal bool) ([]*domainmodel.ModuleType, error) {
	if len(names) == 0 {
		return []*domainmodel.ModuleType{}, nil
	}

	db := m.GetDB(isFormal)
	var results []*domainmodel.ModuleType
	query := db.WithContext(ctx)

	// 构建 OR 条件进行模糊匹配
	for i, name := range names {
		if i == 0 {
			query = query.Where("c_name LIKE ?", "%"+name+"%")
		} else {
			query = query.Or("c_name LIKE ?", "%"+name+"%")
		}
	}

	err := query.Order("c_index ASC").Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "根据名称模糊查询模块类型失败, names: %v, err: %v", names, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据名称模糊查询模块类型失败")
	}
	return results, nil
}

// GetLatestSampleConfigByModuleTypeID 获取指定模块类型的最新示例配置
func (m *magicConfigImpl) GetLatestSampleConfigByModuleTypeID(ctx context.Context, moduleTypeID int, isFormal bool) (string, error) {
	db := m.GetDB(isFormal)
	var moduleInfo domainmodel.ModuleInfo
	err := db.WithContext(ctx).
		Where("c_type = ?", moduleTypeID).
		Where("c_enable = ?", 1).
		Where("c_m_time >= ?", "2024-01-01 00:00:00").
		Where("c_ext_conf != ?", "").
		Order("c_m_time DESC").
		First(&moduleInfo).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		log.ErrorContextf(ctx, "查询模块示例配置失败, moduleTypeID: %d, err: %v", moduleTypeID, err)
		return "", errs.New(consts.ErrCodeDBQuery, "查询模块示例配置失败")
	}

	return moduleInfo.ExtConf, nil
}

// ==================== domainmodel.ModuleInputType 相关操作（只读） ====================

// GetModuleInputTypeByID 根据模块类型ID和SetID获取模块输入类型
func (m *magicConfigImpl) GetModuleInputTypeByID(ctx context.Context, moduleTypeID, setID int, isFormal bool) (*domainmodel.ModuleInputType, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.ModuleInputType
	err := db.WithContext(ctx).Where("c_module_type_id = ? AND c_set_id = ?", moduleTypeID, setID).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询模块输入类型失败, moduleTypeID: %d, setID: %d, err: %v", moduleTypeID, setID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询模块输入类型失败")
	}
	return &result, nil
}

// GetModuleInputType 根据模块类型ID获取所有模块输入类型
func (m *magicConfigImpl) GetModuleInputType(ctx context.Context, moduleTypeID int, isFormal bool) (*domainmodel.ModuleInputType, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.ModuleInputType
	err := db.WithContext(ctx).Where("c_module_type_id = ?", moduleTypeID).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "根据模块类型ID查询模块输入类型失败, moduleTypeID: %d, err: %v", moduleTypeID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据模块类型ID查询模块输入类型失败")
	}
	return &result, nil
}

// ==================== domainmodel.ModuleOutputType 相关操作（只读） ====================

// GetModuleOutputTypeByID 根据ID获取模块输出类型
func (m *magicConfigImpl) GetModuleOutputTypeByID(ctx context.Context, id int, isFormal bool) (*domainmodel.ModuleOutputType, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.ModuleOutputType
	err := db.WithContext(ctx).Where("c_id = ?", id).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询模块输出类型失败, id: %d, err: %v", id, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询模块输出类型失败")
	}
	return &result, nil
}

// GetModuleOutputTypesByModuleTypeID 根据模块类型ID获取所有模块输出类型
func (m *magicConfigImpl) GetModuleOutputTypesByModuleTypeID(ctx context.Context, moduleTypeID int, isFormal bool) ([]*domainmodel.ModuleOutputType, error) {
	db := m.GetDB(isFormal)
	var results []*domainmodel.ModuleOutputType
	err := db.WithContext(ctx).Where("c_module_type_id = ?", moduleTypeID).Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "根据模块类型ID查询模块输出类型失败, moduleTypeID: %d, err: %v", moduleTypeID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据模块类型ID查询模块输出类型失败")
	}
	return results, nil
}

// ==================== domainmodel.ModuleOpType 相关操作（只读） ====================

// GetModuleOpTypeByID 根据ID获取模块操作类型
func (m *magicConfigImpl) GetModuleOpTypeByID(ctx context.Context, id int, isFormal bool) (*domainmodel.ModuleOpType, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.ModuleOpType
	err := db.WithContext(ctx).Where("c_id = ?", id).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询模块操作类型失败, id: %d, err: %v", id, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询模块操作类型失败")
	}
	return &result, nil
}

// GetModuleOpTypesByModuleTypeID 根据模块类型ID获取所有模块操作类型
func (m *magicConfigImpl) GetModuleOpTypesByModuleTypeID(ctx context.Context, moduleTypeID int, isFormal bool) ([]*domainmodel.ModuleOpType, error) {
	db := m.GetDB(isFormal)
	var results []*domainmodel.ModuleOpType
	err := db.WithContext(ctx).Where("c_module_type_id = ?", moduleTypeID).Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "根据模块类型ID查询模块操作类型失败, moduleTypeID: %d, err: %v", moduleTypeID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据模块类型ID查询模块操作类型失败")
	}
	return results, nil
}

// ==================== domainmodel.AdminConfigV3 相关操作（只读） ====================

// GetAdminConfigV3ByID 根据ID和配置类型获取管理配置
func (m *magicConfigImpl) GetAdminConfigV3ByID(ctx context.Context, id, configType string, isFormal bool) (*domainmodel.AdminConfigV3, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.AdminConfigV3
	err := db.WithContext(ctx).Where("c_id = ? AND c_type = ?", id, configType).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询管理配置失败, id: %s, configType: %s, err: %v", id, configType, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询管理配置失败")
	}
	return &result, nil
}

// GetAdminConfigV3sByType 根据配置类型获取所有管理配置
func (m *magicConfigImpl) GetAdminConfigV3sByType(ctx context.Context, configType string, isFormal bool) ([]*domainmodel.AdminConfigV3, error) {
	db := m.GetDB(isFormal)
	var results []*domainmodel.AdminConfigV3
	err := db.WithContext(ctx).Where("c_type = ?", configType).Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "根据配置类型查询管理配置失败, configType: %s, err: %v", configType, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据配置类型查询管理配置失败")
	}
	return results, nil
}

// GetModuleTypeUISchema 根据模块类型ID获取UI schema配置
func (m *magicConfigImpl) GetModuleTypeUISchema(ctx context.Context, moduleTypeID int, isFormal bool) (*domainmodel.AdminConfigV3, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.AdminConfigV3
	err := db.WithContext(ctx).Where("c_id = ?", moduleTypeID).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询模块类型UI schema失败, moduleTypeID: %d, err: %v", moduleTypeID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询模块类型UI schema失败")
	}
	return &result, nil
}

// ==================== domainmodel.OutputType 相关操作（只读） ====================

// GetOutputTypeByID 根据ID获取输出类型
func (m *magicConfigImpl) GetOutputTypeByID(ctx context.Context, id int, isFormal bool) (*domainmodel.OutputType, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.OutputType
	err := db.WithContext(ctx).Where("c_id = ?", id).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询输出类型失败, id: %d, err: %v", id, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询输出类型失败")
	}
	return &result, nil
}

// GetOutputTypesByModuleTypeID 根据模块类型ID获取关联的输出类型列表
func (m *magicConfigImpl) GetOutputTypesByModuleTypeID(ctx context.Context, moduleTypeID int, isFormal bool) ([]*domainmodel.OutputType, error) {
	db := m.GetDB(isFormal)
	var results []*domainmodel.OutputType
	err := db.WithContext(ctx).
		Table("t_output_type").
		Select("t_output_type.*").
		Joins("INNER JOIN t_module_output_type ON t_output_type.c_id = t_module_output_type.c_output_type_id").
		Where("t_module_output_type.c_module_type_id = ?", moduleTypeID).
		Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "根据模块类型ID查询输出类型失败, moduleTypeID: %d, err: %v", moduleTypeID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据模块类型ID查询输出类型失败")
	}
	return results, nil
}

// GetCompleteModuleTypeInfo 获取完整的模块类型信息（包括操作类型、输出类型和UI schema）
func (m *magicConfigImpl) GetCompleteModuleTypeInfo(ctx context.Context, moduleTypeID int, isFormal bool) (*domainmodel.CompleteModuleTypeInfo, error) {
	var (
		moduleType    *domainmodel.ModuleType
		moduleOpTypes []*domainmodel.ModuleOpType
		outputTypes   []*domainmodel.OutputType
		inputType     *domainmodel.ModuleInputType
		uiSchema      *domainmodel.AdminConfigV3
	)
	// 并发获取模块类型基本信息、操作类型、输出类型、输入类型和UI schema
	if err := trpc.GoAndWait(func() error {
		var err error
		moduleType, err = m.GetModuleTypeByID(ctx, moduleTypeID, isFormal)
		if err != nil {
			return err
		}
		if moduleType == nil {
			return errs.New(consts.ErrCodeDBQuery, "模块类型不存在")
		}
		return nil
	}, func() error { // 获取模块业务操作类型
		var err error
		moduleOpTypes, err = m.GetModuleOpTypesByModuleTypeID(ctx, moduleTypeID, isFormal)
		return err
	}, func() error { // 获取模块输出类型
		var err error
		outputTypes, err = m.GetOutputTypesByModuleTypeID(ctx, moduleTypeID, isFormal)
		return err
	}, func() error { // 获取模块输入类型
		var err error
		inputType, err = m.GetModuleInputType(ctx, moduleTypeID, isFormal)
		return err
	}, func() error { // 获取模块配置UI schema
		var err error
		uiSchema, err = m.GetModuleTypeUISchema(ctx, moduleTypeID, isFormal)
		return err
	}); err != nil {
		log.ErrorContextf(ctx, "获取模块类型信息失败, moduleTypeID: %d, err: %v", moduleTypeID, err)
		return nil, err
	}

	return &domainmodel.CompleteModuleTypeInfo{
		ModuleType:    moduleType,
		ModuleOpTypes: moduleOpTypes,
		OutputTypes:   outputTypes,
		InputTypes:    inputType,
		UISchema:      uiSchema,
	}, nil
}

// ==================== domainmodel.ModuleSecurity 相关操作（只读） ====================

// GetModuleSecurityByModuleTypeID 根据模块类型ID获取模块安全策略
func (m *magicConfigImpl) GetModuleSecurityByModuleTypeID(ctx context.Context, moduleTypeID int, isFormal bool) (*domainmodel.ModuleSecurity, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.ModuleSecurity
	err := db.WithContext(ctx).Where("c_mod_type = ?", moduleTypeID).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询模块安全策略失败, moduleTypeID: %d, err: %v", moduleTypeID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询模块安全策略失败")
	}
	return &result, nil
}

// ==================== domainmodel.Condition 相关操作 ====================

// GetConditionByID 根据ID获取条件
func (m *magicConfigImpl) GetConditionByID(ctx context.Context, id int, isFormal bool) (*domainmodel.Condition, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.Condition
	err := db.WithContext(ctx).Where("c_id = ?", id).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询条件失败, id: %d, err: %v", id, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询条件失败")
	}
	return &result, nil
}

// GetConditionsByModuleID 根据模块ID获取条件列表
func (m *magicConfigImpl) GetConditionsByModuleID(ctx context.Context, moduleID int, isFormal bool) ([]*domainmodel.Condition, error) {
	db := m.GetDB(isFormal)
	var results []*domainmodel.Condition
	err := db.WithContext(ctx).Where("c_module_id = ?", moduleID).Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "根据模块ID查询条件失败, moduleID: %d, err: %v", moduleID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据模块ID查询条件失败")
	}
	return results, nil
}

// ==================== domainmodel.ConditionItem 相关操作 ====================

// GetConditionItemByID 根据ID获取条件项
func (m *magicConfigImpl) GetConditionItemByID(ctx context.Context, id int, isFormal bool) (*domainmodel.ConditionItem, error) {
	db := m.GetDB(isFormal)
	var result domainmodel.ConditionItem
	err := db.WithContext(ctx).Where("c_id = ?", id).First(&result).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		log.ErrorContextf(ctx, "查询条件项失败, id: %d, err: %v", id, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "查询条件项失败")
	}
	return &result, nil
}

// GetConditionItemsByConditionID 根据条件ID获取条件项列表
func (m *magicConfigImpl) GetConditionItemsByConditionID(ctx context.Context, conditionID int, isFormal bool) ([]*domainmodel.ConditionItem, error) {
	db := m.GetDB(isFormal)
	var results []*domainmodel.ConditionItem
	err := db.WithContext(ctx).Where("c_condition_id = ?", conditionID).Order("c_sort ASC").Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "根据条件ID查询条件项失败, conditionID: %d, err: %v", conditionID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据条件ID查询条件项失败")
	}
	return results, nil
}

// GetConditionItemsByModuleID 根据模块ID获取条件项列表
func (m *magicConfigImpl) GetConditionItemsByModuleID(ctx context.Context, moduleID int, isFormal bool) ([]*domainmodel.ConditionItem, error) {
	db := m.GetDB(isFormal)
	var results []*domainmodel.ConditionItem
	err := db.WithContext(ctx).Where("c_module_id = ?", moduleID).Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "根据模块ID查询条件项失败, moduleID: %d, err: %v", moduleID, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "根据模块ID查询条件项失败")
	}
	return results, nil
}

// ==================== 批量操作 ====================

// GetConditionsByModuleIDs 根据多个模块ID批量获取条件列表
// 该方法通过一次查询获取多个模块的条件，减少网络操作次数
func (m *magicConfigImpl) GetConditionsByModuleIDs(ctx context.Context, moduleIDs []int, isFormal bool) (map[int][]*domainmodel.Condition, error) {
	if len(moduleIDs) == 0 {
		return make(map[int][]*domainmodel.Condition), nil
	}

	db := m.GetDB(isFormal)
	var results []*domainmodel.Condition
	err := db.WithContext(ctx).Where("c_module_id IN ?", moduleIDs).Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "批量查询条件失败, moduleIDs: %v, err: %v", moduleIDs, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "批量查询条件失败")
	}

	// 按模块ID分组
	conditionsByModule := make(map[int][]*domainmodel.Condition)
	for _, cond := range results {
		conditionsByModule[cond.ModuleID] = append(conditionsByModule[cond.ModuleID], cond)
	}

	return conditionsByModule, nil
}

// GetConditionItemsByConditionIDs 根据多个条件ID批量获取条件项列表
// 该方法通过一次查询获取多个条件的条件项，减少网络操作次数
func (m *magicConfigImpl) GetConditionItemsByConditionIDs(ctx context.Context, conditionIDs []int, isFormal bool) (map[int][]*domainmodel.ConditionItem, error) {
	if len(conditionIDs) == 0 {
		return make(map[int][]*domainmodel.ConditionItem), nil
	}

	db := m.GetDB(isFormal)
	var results []*domainmodel.ConditionItem
	err := db.WithContext(ctx).Where("c_condition_id IN ?", conditionIDs).Order("c_sort ASC").Find(&results).Error
	if err != nil {
		log.ErrorContextf(ctx, "批量查询条件项失败, conditionIDs: %v, err: %v", conditionIDs, err)
		return nil, errs.New(consts.ErrCodeDBQuery, "批量查询条件项失败")
	}

	// 按条件ID分组
	itemsByCondition := make(map[int][]*domainmodel.ConditionItem)
	for _, item := range results {
		itemsByCondition[item.ConditionID] = append(itemsByCondition[item.ConditionID], item)
	}

	return itemsByCondition, nil
}
