package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetUserBillingGroupStats 处理 GET /api/billing_groups/self/stats
//
// 用途：返回当前登录用户在不同计费分组下的消耗统计
// 权限：任何已登录用户（middleware.UserAuth()）
//
// 查询参数：
//   - period (可选): 时间窗口，默认 "7d"，支持 1d/7d/30d
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 6.3
func GetUserBillingGroupStats(c *gin.Context) {
	// 1. 获取当前用户ID
	userId := c.GetInt("id")
	if userId == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized: user ID not found in context",
		})
		return
	}

	// 2. 解析查询参数
	period := c.DefaultQuery("period", "7d")

	// 3. 验证 period 参数
	validPeriods := []string{"1d", "7d", "30d"}
	if !contains(validPeriods, period) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid period: must be one of 1d, 7d, 30d",
		})
		return
	}

	// 4. 计算时间范围
	endTime := time.Now().Unix()
	startTime, err := calculateStartTime(endTime, period)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid period format: " + err.Error(),
		})
		return
	}

	// 5. 调用 Model 层聚合用户计费分组统计
	stats, err := model.AggregateUserBillingGroupStats(userId, startTime, endTime)
	if err != nil {
		common.SysError("failed to aggregate user billing group stats: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to fetch billing group statistics: " + err.Error(),
		})
		return
	}

	// 6. 返回响应
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    stats,
	})
}

// GetUserBillingGroupDailyTokens 处理 GET /api/billing_groups/self/daily_tokens
//
// 用途：返回当前登录用户按计费分组的每日 Token/Quota 消耗曲线
// 权限：任何已登录用户（middleware.UserAuth()）
//
// 查询参数：
//   - days (可选): 向前多少天，默认 30，最大 90
//   - billing_group (可选): 指定计费分组，为空则返回所有计费分组
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 6.3
func GetUserBillingGroupDailyTokens(c *gin.Context) {
	// 1. 获取当前用户ID
	userId := c.GetInt("id")
	if userId == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Unauthorized: user ID not found in context",
		})
		return
	}

	// 2. 解析查询参数：days
	daysStr := c.DefaultQuery("days", "30")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid days parameter: must be a positive integer",
		})
		return
	}

	// 限制最大天数
	if days > 90 {
		days = 90
	}

	// 3. 解析查询参数：billing_group（可选）
	billingGroup := c.Query("billing_group")

	// 4. 调用 Model 层获取日均曲线
	dailyUsage, err := model.GetUserBillingGroupDailyUsage(userId, days, billingGroup)
	if err != nil {
		common.SysError("failed to get user billing group daily usage: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to fetch daily token usage: " + err.Error(),
		})
		return
	}

	// 5. 返回响应
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    dailyUsage,
	})
}
