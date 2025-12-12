package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// ============================================
// 套餐监控相关接口
// 相关设计：docs/NewAPI-支持多种包月套餐-优化版.md 第 11.1 节
// ============================================

// GetPackageMetrics 获取套餐监控指标（管理员接口）
// 返回实时指标快照
func GetPackageMetrics(c *gin.Context) {
	metrics := service.GetPackageMetrics()
	common.ApiSuccess(c, metrics)
}

// GetPackageUtilization 获取套餐使用率统计（管理员接口）
// Query params:
//   - time_range: 时间范围（秒），默认 86400（24 小时）
func GetPackageUtilization(c *gin.Context) {
	timeRange := 86400 // 默认 24 小时
	if tr := c.Query("time_range"); tr != "" {
		if val, err := strconv.Atoi(tr); err == nil && val > 0 {
			timeRange = val
		}
	}

	stats, err := service.GetPackageUtilizationStats(int64(timeRange))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, stats)
}

// GetBillingTypeDistribution 获取计费类型分布统计（管理员接口）
// Query params:
//   - time_range: 时间范围（秒），默认 86400（24 小时）
func GetBillingTypeDistribution(c *gin.Context) {
	timeRange := 86400
	if tr := c.Query("time_range"); tr != "" {
		if val, err := strconv.Atoi(tr); err == nil && val > 0 {
			timeRange = val
		}
	}

	stats, err := service.GetBillingTypeDistribution(int64(timeRange))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, stats)
}

// GetTopPackages 获取使用量 Top N 的套餐（管理员接口）
// Query params:
//   - limit: 返回数量，默认 10
//   - time_range: 时间范围（秒），默认 86400（24 小时）
func GetTopPackages(c *gin.Context) {
	limit := 10
	if l := c.Query("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 && val <= 100 {
			limit = val
		}
	}

	timeRange := 86400
	if tr := c.Query("time_range"); tr != "" {
		if val, err := strconv.Atoi(tr); err == nil && val > 0 {
			timeRange = val
		}
	}

	packages, err := service.GetTopPackagesByUsage(limit, int64(timeRange))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"time_range": timeRange,
		"limit":      limit,
		"packages":   packages,
	})
}
