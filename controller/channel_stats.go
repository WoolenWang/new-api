package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetChannelStats 获取渠道统计数据
// Phase 8.5: Statistics Query API
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: 8.5 阶段五：统计查询 API 与读路径三级缓存
//
// GET /api/channels/:id/stats
// Query Parameters:
//   - period: 时间窗口 (1h, 6h, 7d, 30d), 默认1h
//   - model: 指定模型名称，为空则返回渠道总体统计
//
// Response: ChannelStatsSummaryResponse
func GetChannelStats(c *gin.Context) {
	// 1. 参数解析
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid channel ID",
		})
		return
	}

	period := c.DefaultQuery("period", "1h")
	modelName := c.Query("model")

	// 2. 权限检查（仅管理员）
	if !common.IsAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "Admin permission required",
		})
		return
	}

	// 3. 验证渠道存在
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	// 4. 解析时间窗口
	startTime, endTime, err := parsePeriod(period)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": fmt.Sprintf("Invalid period: %s", err.Error()),
		})
		return
	}

	// 5. 三级缓存查询：L1 -> L2 -> L3
	stats, err := getChannelStatsWithCache(channelID, modelName, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("Failed to get stats: %s", err.Error()),
		})
		return
	}

	// 6. 构建响应
	response := buildStatsResponse(channel, stats, period, modelName)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
	})
}

// parsePeriod 解析时间窗口参数
func parsePeriod(period string) (startTime, endTime int64, err error) {
	now := time.Now()
	endTime = now.Unix()

	switch period {
	case "1h":
		startTime = now.Add(-1 * time.Hour).Unix()
	case "6h":
		startTime = now.Add(-6 * time.Hour).Unix()
	case "7d":
		startTime = now.Add(-7 * 24 * time.Hour).Unix()
	case "30d":
		startTime = now.Add(-30 * 24 * time.Hour).Unix()
	default:
		return 0, 0, fmt.Errorf("unsupported period: %s (supported: 1h, 6h, 7d, 30d)", period)
	}

	return startTime, endTime, nil
}

// getChannelStatsWithCache 三级缓存查询
func getChannelStatsWithCache(channelID int, modelName string, startTime, endTime int64) (*ChannelStatsData, error) {
	// L1: 尝试从内存获取实时数据
	l1Service := service.GetChannelStatsL1Service()
	l1Stats := l1Service.GetCurrentStats()

	key := fmt.Sprintf("%d:%s", channelID, modelName)
	if modelName == "" {
		// 聚合所有模型
		key = fmt.Sprintf("%d:", channelID)
	}

	var l1Data *service.ChannelStatsSnapshot
	for k, v := range l1Stats {
		if modelName == "" {
			// 匹配channel_id前缀
			var cid int
			fmt.Sscanf(k, "%d:", &cid)
			if cid == channelID {
				if l1Data == nil {
					l1Data = v
				} else {
					// 聚合多个模型
					l1Data = aggregateSnapshots(l1Data, v)
				}
			}
		} else {
			// 精确匹配
			if k == key {
				l1Data = v
				break
			}
		}
	}

	// L2: 尝试从Redis获取缓存数据（当前窗口）
	var l2Data *service.ChannelStatsSnapshot
	if common.RedisEnabled {
		l2Service := service.GetChannelStatsL2Service()
		if modelName != "" {
			l2Data, _ = l2Service.GetCurrentWindowStats(channelID, modelName) // Phase 8.x: 使用当前窗口
		} else {
			// 聚合所有模型（需要查询该渠道支持的所有模型）
			channel, err := model.GetChannelById(channelID, false)
			if err == nil {
				models := channel.GetModels()
				for _, m := range models {
					snap, err := l2Service.GetCurrentWindowStats(channelID, m)
					if err == nil {
						if l2Data == nil {
							l2Data = snap
						} else {
							l2Data = aggregateSnapshots(l2Data, snap)
						}
					}
				}
			}
		}
	}

	// L3: 从数据库聚合历史数据
	dbStats, err := model.GetChannelStatistics(channelID, modelName, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query database: %w", err)
	}

	// 聚合所有数据源
	result := aggregateAllSources(l1Data, l2Data, dbStats)
	// 计算时间范围内的最大停服占比（窗口级），用于边界场景展示。
	result.DowntimePercent = computeMaxDowntimePercent(dbStats)

	// 在调试模式下输出一次聚合前后关键指标，便于排查三级缓存读路径
	// 与数据库窗口对齐问题（对应 CL-08 / CS-xx 场景）。
	if common.DebugEnabled {
		var l1Req, l2Req int64
		if l1Data != nil {
			l1Req = l1Data.RequestCount
		}
		if l2Data != nil {
			l2Req = l2Data.RequestCount
		}
		common.SysLog(fmt.Sprintf(
			"[GetChannelStatsDebug] channel_id=%d model=%s range=[%d,%d] db_windows=%d l1_req=%d l2_req=%d total_req=%d total_tokens=%d",
			channelID,
			modelName,
			startTime,
			endTime,
			len(dbStats),
			l1Req,
			l2Req,
			result.RequestCount,
			result.TotalTokens,
		))
	}

	return result, nil
}

// aggregateSnapshots 聚合两个快照
func aggregateSnapshots(a, b *service.ChannelStatsSnapshot) *service.ChannelStatsSnapshot {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}

	return &service.ChannelStatsSnapshot{
		ChannelID:        a.ChannelID,
		ModelName:        a.ModelName,
		RequestCount:     a.RequestCount + b.RequestCount,
		FailCount:        a.FailCount + b.FailCount,
		TotalTokens:      a.TotalTokens + b.TotalTokens,
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		TotalQuota:       a.TotalQuota + b.TotalQuota,
		TotalLatencyMs:   a.TotalLatencyMs + b.TotalLatencyMs,
		StreamReqCount:   a.StreamReqCount + b.StreamReqCount,
		CacheHitCount:    a.CacheHitCount + b.CacheHitCount,
		SessionCount:     a.SessionCount + b.SessionCount,
		UniqueUsers:      a.UniqueUsers + b.UniqueUsers, // 简化实现
		SnapshotTime:     time.Now().Unix(),
	}
}

// aggregateAllSources 聚合所有数据源
func aggregateAllSources(l1, l2 *service.ChannelStatsSnapshot, dbStats []*model.ChannelStatistics) *ChannelStatsData {
	result := &ChannelStatsData{}

	// 聚合数据库历史数据
	for _, stat := range dbStats {
		result.RequestCount += int64(stat.RequestCount)
		result.FailCount += int64(stat.FailCount)
		result.TotalTokens += stat.TotalTokens
		result.TotalQuota += stat.TotalQuota
		result.TotalLatencyMs += stat.TotalLatencyMs
		result.StreamReqCount += int64(stat.StreamReqCount)
		result.CacheHitCount += int64(stat.CacheHitCount)
		// UniqueUsers 为每个窗口内去重后的用户数。跨窗口直接累加会高估，
		// 但相比完全忽略该字段，能够更接近设计文档中的“区间服务用户数”需求。
		result.UniqueUsers += stat.UniqueUsers
	}

	// 加上L2数据
	if l2 != nil {
		result.RequestCount += l2.RequestCount
		result.FailCount += l2.FailCount
		result.TotalTokens += l2.TotalTokens
		result.TotalQuota += l2.TotalQuota
		result.TotalLatencyMs += l2.TotalLatencyMs
		result.StreamReqCount += l2.StreamReqCount
		result.CacheHitCount += l2.CacheHitCount
		result.UniqueUsers += l2.UniqueUsers
	}

	// 加上L1数据
	if l1 != nil {
		result.RequestCount += l1.RequestCount
		result.FailCount += l1.FailCount
		result.TotalTokens += l1.TotalTokens
		result.TotalQuota += l1.TotalQuota
		result.TotalLatencyMs += l1.TotalLatencyMs
		result.StreamReqCount += l1.StreamReqCount
		result.CacheHitCount += l1.CacheHitCount
		result.UniqueUsers += l1.UniqueUsers
	}

	return result
}

// computeMaxDowntimePercent computes the maximum downtime percentage among DB windows.
// This matches the window-level semantics in the design/tests.
func computeMaxDowntimePercent(dbStats []*model.ChannelStatistics) float64 {
	windowSeconds := common.GetEnvOrDefault("CHANNEL_STATS_WINDOW_SECONDS", 900)
	if windowSeconds <= 0 {
		windowSeconds = 900
	}
	maxPercent := 0.0
	for _, stat := range dbStats {
		if stat.DowntimeSeconds <= 0 {
			continue
		}
		percent := float64(stat.DowntimeSeconds) / float64(windowSeconds) * 100.0
		if percent > maxPercent {
			maxPercent = percent
		}
	}
	return maxPercent
}

// ChannelStatsData 聚合后的统计数据
type ChannelStatsData struct {
	RequestCount    int64
	FailCount       int64
	TotalTokens     int64
	TotalQuota      int64
	TotalLatencyMs  int64
	StreamReqCount  int64
	CacheHitCount   int64
	UniqueUsers     int
	DowntimePercent float64
}

// buildStatsResponse 构建响应数据
func buildStatsResponse(channel *model.Channel, data *ChannelStatsData, period, modelName string) *ChannelStatsSummaryResponse {
	downtimePercent := data.DowntimePercent
	if downtimePercent == 0 {
		downtimePercent = channel.DowntimePercentage
	}
	// 计算派生指标
	avgLatency := 0.0
	if data.RequestCount > 0 {
		avgLatency = float64(data.TotalLatencyMs) / float64(data.RequestCount)
	}

	failRate := 0.0
	if data.RequestCount > 0 {
		failRate = float64(data.FailCount) / float64(data.RequestCount) * 100.0
	}

	cacheHitRate := 0.0
	if data.RequestCount > 0 {
		cacheHitRate = float64(data.CacheHitCount) / float64(data.RequestCount) * 100.0
	}

	streamRatio := 0.0
	if data.RequestCount > 0 {
		streamRatio = float64(data.StreamReqCount) / float64(data.RequestCount) * 100.0
	}

	// 计算每分钟请求数与Tokens数 (RPM / TPM)，仅用于统计展示。
	var minutes float64
	switch period {
	case "1h":
		minutes = 60
	case "6h":
		minutes = 6 * 60
	case "7d":
		minutes = 7 * 24 * 60
	case "30d":
		minutes = 30 * 24 * 60
	default:
		// 不支持的 period 使用0，保守返回0值
		minutes = 0
	}

	var rpm, tpm int64
	if minutes > 0 {
		rpm = int64(float64(data.RequestCount) / minutes)
		tpm = int64(float64(data.TotalTokens) / minutes)
	}

	response := &ChannelStatsSummaryResponse{
		ChannelID:       channel.Id,
		ChannelName:     channel.Name,
		ModelName:       modelName,
		Period:          period,
		RequestCount:    data.RequestCount,
		FailCount:       data.FailCount,
		FailRate:        failRate,
		TotalTokens:     data.TotalTokens,
		TotalQuota:      data.TotalQuota,
		AvgLatencyMs:    avgLatency,
		AvgResponseTime: avgLatency,
		CacheHitRate:    cacheHitRate,
		StreamReqRatio:  streamRatio,
		TPM:             tpm,
		RPM:             rpm,
		UniqueUsers:     data.UniqueUsers,
		DowntimePercent: downtimePercent,
		QueryTime:       time.Now().Unix(),
	}

	return response
}

// ChannelStatsSummaryResponse API响应结构
type ChannelStatsSummaryResponse struct {
	ChannelID       int     `json:"channel_id"`
	ChannelName     string  `json:"channel_name"`
	ModelName       string  `json:"model_name,omitempty"`
	Period          string  `json:"period"`
	RequestCount    int64   `json:"request_count"`
	FailCount       int64   `json:"fail_count"`
	FailRate        float64 `json:"fail_rate"`           // %
	TotalTokens     int64   `json:"total_tokens"`        // 总 tokens
	TotalQuota      int64   `json:"total_quota"`         // 总额度
	AvgLatencyMs    float64 `json:"avg_latency_ms"`      // ms
	AvgResponseTime float64 `json:"avg_response_time"`   // ms, 为兼容测试的别名
	CacheHitRate    float64 `json:"cache_hit_rate"`      // %
	StreamReqRatio  float64 `json:"stream_req_ratio"`    // %
	TPM             int64   `json:"tpm"`                 // Tokens per minute
	RPM             int64   `json:"rpm"`                 // Requests per minute
	UniqueUsers     int     `json:"unique_users"`        // 去重用户数
	DowntimePercent float64 `json:"downtime_percentage"` // %
	QueryTime       int64   `json:"query_time"`          // Unix timestamp
}

// GetChannelCurrentStats 获取渠道当前实时统计（从channels表）
// GET /api/channels/:id/current_stats
func GetChannelCurrentStats(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid channel ID",
		})
		return
	}

	// 权限检查
	if !common.IsAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "Admin permission required",
		})
		return
	}

	// 查询渠道
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	// 获取统计摘要
	summary := channel.GetStatisticsSummary()

	// 在调试/监控场景下输出一次关键统计，用于排查停服占比等字段是否已由 L3 聚合正确回填到 channels 表。
	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf(
			"[GetChannelCurrentStats] channel_id=%d tpm=%d rpm=%d downtime_percentage=%.2f fail_rate=%.2f cache_hit_rate=%.2f",
			summary.ChannelId,
			summary.TPM,
			summary.RPM,
			summary.DowntimePercentage,
			summary.FailRate,
			summary.AvgCacheHitRate,
		))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    summary,
	})
}

// ResetChannelStats 重置渠道统计数据
// POST /api/channels/:id/reset_stats
func ResetChannelStats(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid channel ID",
		})
		return
	}

	// 权限检查（仅管理员）
	if !common.IsAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "Admin permission required",
		})
		return
	}

	// 查询渠道
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "Channel not found",
		})
		return
	}

	// 重置统计
	if err := channel.ResetStatistics(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("Failed to reset statistics: %s", err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Statistics reset successfully",
	})
}
