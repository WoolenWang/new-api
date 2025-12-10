package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// AggregateGroupStatsForAllModels 为分组聚合所有模型的统计数据
// 该函数会查询分组内所有活跃渠道，按模型聚合统计数据并写入数据库
func AggregateGroupStatsForAllModels(groupId int, timeWindowStart int64) error {
	// 1. 查询分组成员（所有活跃渠道）
	channelIds, err := getGroupChannelIds(groupId)
	if err != nil {
		return fmt.Errorf("failed to get group channel ids: %w", err)
	}

	if len(channelIds) == 0 {
		common.SysLog("No active channels found for group %d, skipping aggregation", groupId)
		return nil
	}

	// 2. 查询这些渠道的统计数据（按模型分组）
	modelStats, err := getChannelStatsByModel(channelIds, timeWindowStart)
	if err != nil {
		return fmt.Errorf("failed to get channel stats by model: %w", err)
	}

	if len(modelStats) == 0 {
		common.SysLog("No channel statistics found for group %d at time window %d", groupId, timeWindowStart)
		return nil
	}

	// 3. 对每个模型执行聚合
	for modelName, channelStatsList := range modelStats {
		aggregated := aggregateChannelStats(channelStatsList)

		// 构造GroupStatistics对象
		groupStat := &model.GroupStatistics{
			GroupId:            groupId,
			ModelName:          modelName,
			TimeWindowStart:    timeWindowStart,
			TPM:                int(aggregated.TPM),
			RPM:                int(aggregated.RPM),
			FailRate:           aggregated.FailRate,
			AvgResponseTimeMs:  int(aggregated.AvgResponseTimeMs),
			AvgCacheHitRate:    aggregated.AvgCacheHitRate,
			StreamReqRatio:     aggregated.StreamReqRatio,
			QuotaPM:            aggregated.QuotaPM,
			TotalTokens:        aggregated.TotalTokens,
			TotalQuota:         aggregated.TotalQuota,
			AvgConcurrency:     aggregated.AvgConcurrency,
			TotalSessions:      aggregated.TotalSessions,
			DowntimePercentage: aggregated.DowntimePercentage,
			UniqueUsers:        int(aggregated.UniqueUsers),
		}

		// 4. 写入数据库
		err := model.UpsertGroupStatistics(groupStat)
		if err != nil {
			common.SysLog("Error upserting group statistics for group %d, model %s: %v", groupId, modelName, err)
			return fmt.Errorf("failed to upsert group statistics: %w", err)
		}

		common.SysLog("Successfully aggregated stats for group %d, model %s", groupId, modelName)
	}

	return nil
}

// getGroupChannelIds 获取分组内所有活跃渠道的ID列表
// 注意：这里需要根据实际的分组-渠道关联关系查询
// 目前简化实现，实际应该查询abilities表或channels表的group字段
func getGroupChannelIds(groupId int) ([]int, error) {
	// TODO: 实现实际的分组-渠道关联查询逻辑
	// 示例：查询所有group字段包含"p2p_{groupId}"的渠道

	// 临时实现：查询abilities表中group为"p2p_{groupId}"的所有渠道
	var channelIds []int
	groupName := fmt.Sprintf("p2p_%d", groupId)

	err := model.DB.Model(&model.Ability{}).
		Select("DISTINCT channel_id").
		Where("`group` = ?", groupName).
		Pluck("channel_id", &channelIds).Error

	if err != nil {
		return nil, err
	}

	// 过滤出启用的渠道
	var activeChannelIds []int
	for _, channelId := range channelIds {
		channel, err := model.GetChannelById(channelId, true)
		if err != nil || channel == nil {
			continue
		}
		if channel.Status == common.ChannelStatusEnabled {
			activeChannelIds = append(activeChannelIds, channelId)
		}
	}

	return activeChannelIds, nil
}

// getChannelStatsByModel 查询指定渠道列表的统计数据，按模型分组
// 返回 map[modelName][]ChannelStatistics
func getChannelStatsByModel(channelIds []int, timeWindowStart int64) (map[string][]*model.ChannelStatistics, error) {
	var allStats []*model.ChannelStatistics

	// 查询所有渠道在指定时间窗口的统计数据
	err := model.DB.Where("channel_id IN ? AND time_window_start = ?", channelIds, timeWindowStart).
		Find(&allStats).Error
	if err != nil {
		return nil, err
	}

	// 按模型分组
	modelStats := make(map[string][]*model.ChannelStatistics)
	for _, stat := range allStats {
		modelStats[stat.ModelName] = append(modelStats[stat.ModelName], stat)
	}

	return modelStats, nil
}

// AggregatedChannelStats 聚合后的渠道统计数据（中间结构）
type AggregatedChannelStats struct {
	TPM                int64
	RPM                int64
	FailRate           float64
	AvgResponseTimeMs  float64
	AvgCacheHitRate    float64
	StreamReqRatio     float64
	QuotaPM            int64
	TotalTokens        int64
	TotalQuota         int64
	AvgConcurrency     float64
	TotalSessions      int64
	DowntimePercentage float64
	UniqueUsers        int64
}

// aggregateChannelStats 聚合多个渠道的统计数据
// 实现设计文档5.1节定义的聚合规则
func aggregateChannelStats(stats []*model.ChannelStatistics) AggregatedChannelStats {
	result := AggregatedChannelStats{}

	if len(stats) == 0 {
		return result
	}

	// 用于加权平均计算的权重总和
	var totalRequests int64 = 0
	var weightedFailRate float64 = 0
	var weightedCacheHitRate float64 = 0
	var weightedStreamRatio float64 = 0
	var weightedDowntime float64 = 0
	var weightedResponseTime float64 = 0

	// 遍历所有渠道统计数据
	for _, stat := range stats {
		requestCount := int64(stat.RequestCount)

		// 1. 求和类指标（直接累加）
		result.TotalTokens += stat.TotalTokens
		result.TotalQuota += stat.TotalQuota
		result.TotalSessions += 1 // 每个渠道统计记录视为一个session

		// 2. 计算TPM和RPM（取平均值，因为它们已经是"每分钟"的速率）
		// 或者可以求和，这取决于业务语义。这里我们取平均值。
		// 根据设计文档，应该是求和（因为多个渠道的TPM可以叠加）
		// 修正：使用total_tokens和时间窗口计算实际的TPM
		// 简化实现：这里假设每个统计窗口是15分钟，计算平均TPM
		windowMinutes := int64(15) // 假设统计窗口为15分钟
		if windowMinutes > 0 {
			result.TPM += stat.TotalTokens / windowMinutes
			result.RPM += int64(stat.RequestCount) / windowMinutes
			result.QuotaPM += stat.TotalQuota / windowMinutes
		}

		// 3. 加权平均类指标（以请求数为权重）
		if requestCount > 0 {
			totalRequests += requestCount

			// 失败率
			failRate := float64(stat.FailCount) / float64(requestCount) * 100.0
			weightedFailRate += failRate * float64(requestCount)

			// 缓存命中率
			cacheHitRate := float64(stat.CacheHitCount) / float64(requestCount) * 100.0
			weightedCacheHitRate += cacheHitRate * float64(requestCount)

			// 流式请求占比
			streamRatio := float64(stat.StreamReqCount) / float64(requestCount) * 100.0
			weightedStreamRatio += streamRatio * float64(requestCount)

			// 平均响应时间
			avgLatency := float64(stat.TotalLatencyMs) / float64(requestCount)
			weightedResponseTime += avgLatency * float64(requestCount)
		}

		// 4. 停服时间占比（加权平均）
		// downtime_seconds是该渠道在统计窗口内的停服秒数
		// 假设统计窗口为15分钟 = 900秒
		windowSeconds := int64(900)
		if windowSeconds > 0 && requestCount > 0 {
			downtimePercent := float64(stat.DowntimeSeconds) / float64(windowSeconds) * 100.0
			weightedDowntime += downtimePercent * float64(requestCount)
		}

		// 5. 平均并发数（直接求和，因为并发能力是叠加的）
		// 这里简化计算：假设每个渠道的并发数为 total_sessions
		// 实际应该从更细粒度的数据计算
		// 简化实现：使用请求数作为并发的近似
		result.AvgConcurrency += float64(requestCount) / float64(windowMinutes)
	}

	// 计算加权平均值
	if totalRequests > 0 {
		result.FailRate = weightedFailRate / float64(totalRequests)
		result.AvgCacheHitRate = weightedCacheHitRate / float64(totalRequests)
		result.StreamReqRatio = weightedStreamRatio / float64(totalRequests)
		result.AvgResponseTimeMs = weightedResponseTime / float64(totalRequests)
		result.DowntimePercentage = weightedDowntime / float64(totalRequests)
	}

	// UniqueUsers: 聚合所有渠道的去重用户数
	// Phase 10.4: GS4-1 使用实际的unique_users字段而非请求数近似
	// 注意：跨渠道的用户去重仍然是近似的（用户可能在多个渠道都被计数）
	// 理想情况下应使用 HyperLogLog 合并，但当前实现使用求和作为上界估计
	for _, stat := range stats {
		// 从channel_statistics表的unique_users字段读取去重用户数
		result.UniqueUsers += int64(stat.UniqueUsers)
	}

	return result
}

// AggregateGroupStatsForModel 为分组聚合指定模型的统计数据
// 如果modelName为空，则聚合所有模型
func AggregateGroupStatsForModel(groupId int, modelName string, timeWindowStart int64) error {
	if modelName == "" {
		return AggregateGroupStatsForAllModels(groupId, timeWindowStart)
	}

	// 1. 查询分组成员渠道
	channelIds, err := getGroupChannelIds(groupId)
	if err != nil {
		return fmt.Errorf("failed to get group channel ids: %w", err)
	}

	if len(channelIds) == 0 {
		return nil
	}

	// 2. 查询这些渠道在指定模型的统计数据
	var stats []*model.ChannelStatistics
	err = model.DB.Where("channel_id IN ? AND model_name = ? AND time_window_start = ?",
		channelIds, modelName, timeWindowStart).
		Find(&stats).Error
	if err != nil {
		return fmt.Errorf("failed to get channel stats: %w", err)
	}

	if len(stats) == 0 {
		return nil
	}

	// 3. 聚合计算
	aggregated := aggregateChannelStats(stats)

	// 4. 构造并写入数据库
	groupStat := &model.GroupStatistics{
		GroupId:            groupId,
		ModelName:          modelName,
		TimeWindowStart:    timeWindowStart,
		TPM:                int(aggregated.TPM),
		RPM:                int(aggregated.RPM),
		FailRate:           aggregated.FailRate,
		AvgResponseTimeMs:  int(aggregated.AvgResponseTimeMs),
		AvgCacheHitRate:    aggregated.AvgCacheHitRate,
		StreamReqRatio:     aggregated.StreamReqRatio,
		QuotaPM:            aggregated.QuotaPM,
		TotalTokens:        aggregated.TotalTokens,
		TotalQuota:         aggregated.TotalQuota,
		AvgConcurrency:     aggregated.AvgConcurrency,
		TotalSessions:      aggregated.TotalSessions,
		DowntimePercentage: aggregated.DowntimePercentage,
		UniqueUsers:        int(aggregated.UniqueUsers),
	}

	return model.UpsertGroupStatistics(groupStat)
}
