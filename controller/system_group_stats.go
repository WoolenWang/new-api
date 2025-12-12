package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetSystemGroupsStats 获取系统分组统计数据
// GET /api/groups/system/stats?period=1h
// 权限：任何已登录用户
func GetSystemGroupsStats(c *gin.Context) {
	// 1. 解析查询参数
	period := c.DefaultQuery("period", "1h")

	// 2. 计算时间范围
	endTime := time.Now().Unix()
	startTime, err := calculateStartTime(endTime, period)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	logger.LogInfo(
		c.Request.Context(),
		"system_group_stats_query",
	)

	// 3. 定义系统分组列表
	systemGroups := []string{"default", "vip", "svip"}

	// 4. 为每个系统分组查询统计数据
	var results []map[string]interface{}

	for _, groupName := range systemGroups {
		// 查询该系统分组的聚合统计
		stats, err := model.AggregateChannelStatsByUserGroup(groupName, startTime, endTime)
		if err != nil {
			logger.LogWarn(
				c.Request.Context(),
				"system_group_stats_aggregation_failed",
			)
			// 聚合失败时返回空数据，但不中断整个查询
			stats = &model.AggregatedStats{}
		}

		result := map[string]interface{}{
			"group_name": groupName,
			"stats": map[string]interface{}{
				"tpm":                 stats.TPM,
				"rpm":                 stats.RPM,
				"quota_pm":            stats.QuotaPM,
				"avg_response_time":   stats.AvgResponseTimeMs,
				"fail_rate":           stats.FailRate,
				"total_sessions":      stats.TotalSessions,
				"unique_users":        stats.UniqueUsers,
				"avg_cache_hit_rate":  stats.CacheHitRate,
				"stream_req_ratio":    stats.StreamReqRatio,
				"downtime_percentage": stats.DowntimePercentage,
			},
		}

		results = append(results, result)
	}

	logger.LogInfo(
		c.Request.Context(),
		"system_group_stats_success",
	)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    results,
	})
}
