package service

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// MonitoringConfig 渠道监控配置结构
// 对应 Channel.MonitoringConfig JSON 字段
type MonitoringConfig struct {
	Enabled             bool     `json:"enabled"`               // 是否启用监控
	TargetModel         string   `json:"target_model"`          // 监控的目标模型
	TestIntervalMinutes int      `json:"test_interval_minutes"` // 测试间隔(分钟)
	TestTypes           []string `json:"test_types"`            // 检测类型列表
	EvaluationStandard  string   `json:"evaluation_standard"`   // 评估标准
}

// MonitoringPlan 监控计划
// 表示对单个渠道和模型的监控配置
type MonitoringPlan struct {
	ChannelId          int      `json:"channel_id"`
	ModelName          string   `json:"model_name"`
	TestTypes          []string `json:"test_types"`
	EvaluationStandard string   `json:"evaluation_standard"`
	PolicyId           int      `json:"policy_id"` // 触发该计划的策略ID
}

// MonitorConfigResolver 监控配置解析器
// 负责合并全局策略和渠道级别配置
type MonitorConfigResolver struct{}

// NewMonitorConfigResolver 创建监控配置解析器实例
func NewMonitorConfigResolver() *MonitorConfigResolver {
	return &MonitorConfigResolver{}
}

// ResolveMonitoringPlans 解析监控计划
// 根据启用的策略，生成所有需要执行的监控计划列表
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: MS2-3 策略解析服务 (全局+渠道配置合并)
func (r *MonitorConfigResolver) ResolveMonitoringPlans() ([]*MonitoringPlan, error) {
	// 1. 获取所有启用的监控策略
	policies, err := model.GetEnabledMonitorPolicies()
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled monitor policies: %w", err)
	}

	if len(policies) == 0 {
		common.SysLog("No enabled monitor policies found")
		return []*MonitoringPlan{}, nil
	}

	var plans []*MonitoringPlan

	// 2. 遍历每个策略
	for _, policy := range policies {
		targetModels := policy.GetTargetModels()
		testTypes := policy.GetTestTypes()
		targetChannelIds := policy.GetTargetChannels()

		// 如果策略没有指定模型或检测类型，跳过
		if len(targetModels) == 0 || len(testTypes) == 0 {
			common.SysLog(fmt.Sprintf("Policy %d (%s) has no target models or test types, skipping",
				policy.Id, policy.Name))
			continue
		}

		// 3. 确定要监控的渠道列表
		var channelsToMonitor []*model.Channel
		if len(targetChannelIds) > 0 {
			// 策略指定了特定渠道
			channels, err := model.GetChannelsByIds(targetChannelIds)
			if err != nil {
				common.SysLog(fmt.Sprintf("Failed to get target channels for policy %d: %v", policy.Id, err))
				continue
			}
			channelsToMonitor = channels
		} else {
			// 策略未指定渠道，使用所有启用的渠道
			channels, err := model.GetAllChannels(0, 0, true, false)
			if err != nil {
				common.SysLog(fmt.Sprintf("Failed to get all channels for policy %d: %v", policy.Id, err))
				continue
			}
			// 过滤出启用的渠道
			for _, ch := range channels {
				if ch.Status == common.ChannelStatusEnabled {
					channelsToMonitor = append(channelsToMonitor, ch)
				}
			}
		}

		// 4. 为每个渠道和模型组合生成监控计划
		for _, channel := range channelsToMonitor {
			// 检查渠道是否支持目标模型
			channelModels := channel.GetModels()
			channelModelSet := make(map[string]bool)
			for _, m := range channelModels {
				channelModelSet[m] = true
			}

			// 检查渠道级别的监控配置（优先级更高）
			channelConfig := r.parseChannelMonitoringConfig(channel)

			for _, targetModel := range targetModels {
				// 检查渠道是否支持该模型
				if !channelModelSet[targetModel] {
					continue
				}

				// 合并策略配置和渠道配置
				finalTestTypes := testTypes
				finalStandard := policy.EvaluationStandard

				// 如果渠道有自定义配置且启用，则使用渠道配置覆盖
				if channelConfig != nil && channelConfig.Enabled {
					if channelConfig.TargetModel == targetModel {
						if len(channelConfig.TestTypes) > 0 {
							finalTestTypes = channelConfig.TestTypes
						}
						if channelConfig.EvaluationStandard != "" {
							finalStandard = channelConfig.EvaluationStandard
						}
					}
				}

				// 生成监控计划
				plan := &MonitoringPlan{
					ChannelId:          channel.Id,
					ModelName:          targetModel,
					TestTypes:          finalTestTypes,
					EvaluationStandard: finalStandard,
					PolicyId:           policy.Id,
				}
				plans = append(plans, plan)
			}
		}
	}

	common.SysLog(fmt.Sprintf("Resolved %d monitoring plans from %d policies", len(plans), len(policies)))
	return plans, nil
}

// parseChannelMonitoringConfig 解析渠道的监控配置
func (r *MonitorConfigResolver) parseChannelMonitoringConfig(channel *model.Channel) *MonitoringConfig {
	if channel.MonitoringConfig == nil || *channel.MonitoringConfig == "" {
		return nil
	}

	var config MonitoringConfig
	err := json.Unmarshal([]byte(*channel.MonitoringConfig), &config)
	if err != nil {
		common.SysLog(fmt.Sprintf("Failed to parse monitoring config for channel %d: %v", channel.Id, err))
		return nil
	}

	return &config
}

// GetMonitoringPlanForChannel 获取特定渠道的监控计划
// 用于手动触发监控或查询渠道监控配置
func (r *MonitorConfigResolver) GetMonitoringPlanForChannel(channelId int, modelName string) (*MonitoringPlan, error) {
	channel, err := model.GetChannelById(channelId, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel: %w", err)
	}

	// 检查渠道是否支持该模型
	channelModels := channel.GetModels()
	found := false
	for _, m := range channelModels {
		if m == modelName {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("channel %d does not support model %s", channelId, modelName)
	}

	// 获取相关的监控策略
	policies, err := model.GetEnabledMonitorPolicies()
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled policies: %w", err)
	}

	// 查找适用的策略
	var matchedPolicy *model.MonitorPolicy
	for _, policy := range policies {
		targetModels := policy.GetTargetModels()
		targetChannels := policy.GetTargetChannels()

		// 检查模型是否匹配
		modelMatched := false
		for _, m := range targetModels {
			if m == modelName {
				modelMatched = true
				break
			}
		}
		if !modelMatched {
			continue
		}

		// 检查渠道是否匹配
		if len(targetChannels) == 0 {
			// 策略适用于所有渠道
			matchedPolicy = policy
			break
		} else {
			for _, cid := range targetChannels {
				if cid == channelId {
					matchedPolicy = policy
					break
				}
			}
		}
		if matchedPolicy != nil {
			break
		}
	}

	if matchedPolicy == nil {
		return nil, fmt.Errorf("no matching policy found for channel %d and model %s", channelId, modelName)
	}

	// 构建监控计划
	plan := &MonitoringPlan{
		ChannelId:          channelId,
		ModelName:          modelName,
		TestTypes:          matchedPolicy.GetTestTypes(),
		EvaluationStandard: matchedPolicy.EvaluationStandard,
		PolicyId:           matchedPolicy.Id,
	}

	// 应用渠道级别配置覆盖
	channelConfig := r.parseChannelMonitoringConfig(channel)
	if channelConfig != nil && channelConfig.Enabled && channelConfig.TargetModel == modelName {
		if len(channelConfig.TestTypes) > 0 {
			plan.TestTypes = channelConfig.TestTypes
		}
		if channelConfig.EvaluationStandard != "" {
			plan.EvaluationStandard = channelConfig.EvaluationStandard
		}
	}

	return plan, nil
}

// GetMonitoringPlansForPolicy 获取特定策略的所有监控计划
// 用于策略预览和调试
func (r *MonitorConfigResolver) GetMonitoringPlansForPolicy(policyId int) ([]*MonitoringPlan, error) {
	policy, err := model.GetMonitorPolicyById(policyId)
	if err != nil {
		return nil, fmt.Errorf("failed to get policy: %w", err)
	}

	if !policy.IsEnabled {
		return []*MonitoringPlan{}, nil
	}

	targetModels := policy.GetTargetModels()
	testTypes := policy.GetTestTypes()
	targetChannelIds := policy.GetTargetChannels()

	if len(targetModels) == 0 || len(testTypes) == 0 {
		return []*MonitoringPlan{}, nil
	}

	// 确定要监控的渠道列表
	var channelsToMonitor []*model.Channel
	if len(targetChannelIds) > 0 {
		channels, err := model.GetChannelsByIds(targetChannelIds)
		if err != nil {
			return nil, fmt.Errorf("failed to get target channels: %w", err)
		}
		channelsToMonitor = channels
	} else {
		channels, err := model.GetAllChannels(0, 0, true, false)
		if err != nil {
			return nil, fmt.Errorf("failed to get all channels: %w", err)
		}
		for _, ch := range channels {
			if ch.Status == common.ChannelStatusEnabled {
				channelsToMonitor = append(channelsToMonitor, ch)
			}
		}
	}

	var plans []*MonitoringPlan
	for _, channel := range channelsToMonitor {
		channelModels := channel.GetModels()
		channelModelSet := make(map[string]bool)
		for _, m := range channelModels {
			channelModelSet[m] = true
		}

		for _, targetModel := range targetModels {
			if !channelModelSet[targetModel] {
				continue
			}

			plan := &MonitoringPlan{
				ChannelId:          channel.Id,
				ModelName:          targetModel,
				TestTypes:          testTypes,
				EvaluationStandard: policy.EvaluationStandard,
				PolicyId:           policy.Id,
			}
			plans = append(plans, plan)
		}
	}

	return plans, nil
}
