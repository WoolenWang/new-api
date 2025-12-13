package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetSystemStatsSummary 处理 GET /api/system/stats/summary
//
// 用途：返回整个 NewAPI 系统在指定时间范围内的汇总统计指标
// 权限：仅管理员（middleware.AdminAuth()）
//
// 查询参数：
//   - period (可选): 时间窗口，默认 "7d"，支持 1d/7d/30d
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 7.1
func GetSystemStatsSummary(c *gin.Context) {
	// 1. 解析查询参数
	period := c.DefaultQuery("period", "7d")

	// 2. 验证 period 参数
	validPeriods := []string{"1d", "7d", "30d"}
	if !contains(validPeriods, period) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid period: must be one of 1d, 7d, 30d",
		})
		return
	}

	// 3. 计算时间范围
	endTime := time.Now().Unix()
	startTime, err := calculateStartTime(endTime, period)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid period format: " + err.Error(),
		})
		return
	}

	// 4. 调用 Model 层聚合全局统计数据
	stats, err := model.AggregateGlobalChannelStatsByTime(startTime, endTime)
	if err != nil {
		common.SysError("failed to aggregate global channel stats: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to fetch system statistics: " + err.Error(),
		})
		return
	}

	// 5. 返回响应
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"period":               period,
			"tpm":                  stats.TPM,
			"rpm":                  stats.RPM,
			"quota_pm":             stats.QuotaPM,
			"total_tokens":         stats.TotalTokens,
			"total_quota":          stats.TotalQuota,
			"avg_response_time_ms": stats.AvgResponseTimeMs,
			"fail_rate":            stats.FailRate,
			"cache_hit_rate":       stats.CacheHitRate,
			"stream_req_ratio":     stats.StreamReqRatio,
			"downtime_percentage":  stats.DowntimePercentage,
			"unique_users":         stats.UniqueUsers,
			"request_count":        stats.RequestCount,
		},
	})
}

// GetSystemDailyTokens 处理 GET /api/system/stats/daily_tokens
//
// 用途：返回整个 NewAPI 系统按日聚合的 Token/Quota 消耗曲线
// 权限：仅管理员（middleware.AdminAuth()）
//
// 查询参数：
//   - days (可选): 向前多少天，默认 30，最大 90
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 7.2
func GetSystemDailyTokens(c *gin.Context) {
	// 1. 解析查询参数
	daysStr := c.DefaultQuery("days", "30")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid days parameter: must be a positive integer",
		})
		return
	}

	// 2. 限制最大天数（避免查询过多数据）
	if days > 90 {
		days = 90
	}

	// 3. 调用 Model 层获取日均曲线
	dailyUsage, err := model.GetGlobalDailyTokenUsage(days)
	if err != nil {
		common.SysError("failed to get global daily token usage: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to fetch daily token usage: " + err.Error(),
		})
		return
	}

	// 4. 返回响应
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dailyUsage,
	})
}
