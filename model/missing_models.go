package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// GetMissingModels returns model names that are referenced in the system
func GetMissingModels() ([]string, error) {
	// 1. 获取所有已启用模型（去重）
	models := GetEnabledModels()
	common.SysLog(fmt.Sprintf("GetMissingModels - 从abilities表获取到 %d 个已启用模型", len(models)))
	if len(models) == 0 {
		common.SysLog("GetMissingModels - abilities表中没有已启用的模型，可能是新系统或没有配置渠道")
		return []string{}, nil
	}

	// 2. 查询已有的元数据模型名
	var existing []string
	if err := DB.Model(&Model{}).Where("model_name IN ?", models).Pluck("model_name", &existing).Error; err != nil {
		return nil, err
	}
	common.SysLog(fmt.Sprintf("GetMissingModels - models表中已有 %d 个模型的元数据", len(existing)))

	existingSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingSet[e] = struct{}{}
	}

	// 3. 收集缺失模型
	var missing []string
	for _, name := range models {
		if _, ok := existingSet[name]; !ok {
			missing = append(missing, name)
		}
	}
	common.SysLog(fmt.Sprintf("GetMissingModels - 发现 %d 个缺失元数据的模型", len(missing)))
	return missing, nil
}

// GetMissingModelsFromUpstream 根据上游模型列表计算缺失的模型
// 逻辑：
// 1. 如果 abilities 表为空（新系统），返回所有上游模型中本地不存在的
// 2. 如果 abilities 表有数据，返回已启用但本地不存在的模型
func GetMissingModelsFromUpstream(upstreamModels []string) ([]string, error) {
	// 1. 获取已启用的模型列表
	enabledModels := GetEnabledModels()
	common.SysLog(fmt.Sprintf("GetMissingModelsFromUpstream - 从abilities表获取到 %d 个已启用模型", len(enabledModels)))

	// 2. 查询本地已有的模型（从上游列表中）
	var existing []string
	if len(upstreamModels) > 0 {
		if err := DB.Model(&Model{}).
			Where("model_name IN ?", upstreamModels).
			Pluck("model_name", &existing).Error; err != nil {
			return nil, err
		}
	}
	common.SysLog(fmt.Sprintf("GetMissingModelsFromUpstream - 本地已存在 %d 个上游模型", len(existing)))

	existingSet := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		existingSet[e] = struct{}{}
	}

	// 3. 计算缺失模型
	var missing []string
	if len(enabledModels) == 0 {
		// 新系统：返回所有本地不存在的上游模型
		common.SysLog("GetMissingModelsFromUpstream - abilities表为空，将导入所有不存在的上游模型")
		for _, name := range upstreamModels {
			if _, ok := existingSet[name]; !ok {
				missing = append(missing, name)
			}
		}
	} else {
		// 已有系统：只返回已启用但本地不存在的模型
		common.SysLog("GetMissingModelsFromUpstream - 只同步已启用的模型")
		enabledSet := make(map[string]struct{}, len(enabledModels))
		for _, e := range enabledModels {
			enabledSet[e] = struct{}{}
		}
		for _, name := range upstreamModels {
			// 只添加：已启用 且 本地不存在的
			if _, enabled := enabledSet[name]; enabled {
				if _, exists := existingSet[name]; !exists {
					missing = append(missing, name)
				}
			}
		}
	}

	common.SysLog(fmt.Sprintf("GetMissingModelsFromUpstream - 计算出 %d 个需要同步的模型", len(missing)))
	return missing, nil
}
