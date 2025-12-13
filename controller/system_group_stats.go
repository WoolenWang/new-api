package controller

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
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
	// 设计目标：按“全部计费分组”进行统计，而不是硬编码 default/vip/svip。
	// 当前实现：从 GroupRatio 配置中动态枚举分组名称，并过滤掉 "auto" 等非计费组。
	systemGroups := getAllBillingGroups()

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
				"total_tokens":        stats.TotalTokens,
				"total_quota":         stats.TotalQuota,
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

// GetSystemGroupModelStats 获取系统分组按模型聚合的统计数据
// GET /api/groups/system/model_stats?group=default&period=7d&model_name=gpt-4
// 权限：仅管理员（通过 Admin 调用或后端服务透传）
func GetSystemGroupModelStats(c *gin.Context) {
	groupName := c.DefaultQuery("group", "default")
	period := c.DefaultQuery("period", "7d")
	modelName := c.Query("model_name")

	endTime := time.Now().Unix()
	startTime, err := calculateStartTime(endTime, period)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	stats, err := model.AggregateBillingGroupModelStats(groupName, startTime, endTime, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, stats)
}

// GetSystemGroupModelDailyTokens 获取系统分组按模型的每日 Token/Quota 消耗曲线
// GET /api/groups/system/model_daily_tokens?group=default&days=30&model_name=gpt-4
// 权限：仅管理员
func GetSystemGroupModelDailyTokens(c *gin.Context) {
	groupName := c.DefaultQuery("group", "default")

	daysStr := c.DefaultQuery("days", "30")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 1 {
		common.ApiError(c, errors.New("invalid days parameter"))
		return
	}

	modelName := c.Query("model_name")

	usage, err := model.GetBillingGroupModelDailyUsage(groupName, days, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, usage)
}

// getAllBillingGroups 从分组倍率配置中动态枚举“计费分组”列表。
// 说明：
//   - 使用 ratio_setting.GetGroupRatioCopy() 作为计费分组来源；
//   - 过滤掉 "auto" 等非计费分组；
//   - 返回按名称排序后的列表，保证响应顺序稳定。
func getAllBillingGroups() []string {
	ratioMap := ratio_setting.GetGroupRatioCopy()
	groups := make([]string, 0, len(ratioMap))
	for name := range ratioMap {
		if name == "" || name == "auto" {
			continue
		}
		groups = append(groups, name)
	}
	sort.Strings(groups)
	return groups
}
