package controller

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetP2PGroupStats 获取P2P分组的统计数据
// GET /api/p2p_groups/:id/stats?model=gpt-4&period=24h
// 权限：分组成员或管理员
func GetP2PGroupStats(c *gin.Context) {
	// 1. 获取分组ID
	idStr := c.Param("id")
	if idStr == "" {
		common.ApiError(c, errors.New("group id is required"))
		return
	}

	groupId, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid group id"))
		return
	}

	// 2. 验证分组是否存在
	_, err = model.GetGroupById(groupId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 3. 权限检查：验证用户是否为分组成员或管理员
	userId := c.GetInt("id")
	userRole := c.GetInt("role")

	if userRole != common.RoleRootUser && userRole != common.RoleAdminUser {
		// 非管理员，检查是否为分组成员
		isMember, err := checkGroupMember(userId, groupId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if !isMember {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "您不是该分组的成员，无权查看统计数据",
			})
			return
		}
	}

	// 4. 解析查询参数
	modelName := c.Query("model")             // 可选：指定模型
	period := c.DefaultQuery("period", "24h") // 可选：时间窗口，默认24小时

	// 5. 计算时间范围（仅在按整体维度聚合时使用）
	endTime := time.Now().Unix()
	startTime, err := calculateStartTime(endTime, period)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 6. 查询统计数据
	// 如果指定了model，返回该模型的最新一个窗口的聚合快照；
	// 否则返回在指定时间范围内的总体聚合视图。
	if modelName != "" {
		// 返回指定模型的最新统计快照
		stat, err := model.GetLatestGroupStatistics(groupId, modelName)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		data := gin.H{
			"group_id":             stat.GroupId,
			"model_name":           stat.ModelName,
			"time_window_start":    stat.TimeWindowStart,
			"tpm":                  stat.TPM,
			"rpm":                  stat.RPM,
			"quota_pm":             stat.QuotaPM,
			"total_tokens":         stat.TotalTokens,
			"total_quota":          stat.TotalQuota,
			"fail_rate":            stat.FailRate,
			"avg_response_time":    stat.AvgResponseTimeMs,
			"avg_response_time_ms": stat.AvgResponseTimeMs,
			"avg_cache_hit_rate":   stat.AvgCacheHitRate,
			"stream_req_ratio":     stat.StreamReqRatio,
			"avg_concurrency":      stat.AvgConcurrency,
			"total_sessions":       stat.TotalSessions,
			"downtime_percentage":  stat.DowntimePercentage,
			"unique_users":         stat.UniqueUsers,
			"updated_at":           stat.UpdatedAt,
		}

		common.ApiSuccess(c, data)
	} else {
		// 总体聚合：不带模型过滤，按时间范围聚合所有模型
		aggregated, err := model.AggregateGroupStatisticsByTime(groupId, "", startTime, endTime)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 为了提供UpdatedAt信息，读取该分组的最新一条记录
		var latestUpdatedAt int64
		if latest, err := model.GetLatestGroupStatistics(groupId, ""); err == nil && latest != nil {
			latestUpdatedAt = latest.UpdatedAt
		}

		data := gin.H{
			"group_id":             aggregated.GroupId,
			"model_name":           aggregated.ModelName,
			"tpm":                  aggregated.TPM,
			"rpm":                  aggregated.RPM,
			"quota_pm":             aggregated.QuotaPM,
			"total_tokens":         aggregated.TotalTokens,
			"total_quota":          aggregated.TotalQuota,
			"fail_rate":            aggregated.FailRate,
			"avg_response_time":    int(aggregated.AvgResponseTimeMs),
			"avg_response_time_ms": aggregated.AvgResponseTimeMs,
			"avg_cache_hit_rate":   aggregated.AvgCacheHitRate,
			"stream_req_ratio":     aggregated.StreamReqRatio,
			"avg_concurrency":      aggregated.AvgConcurrency,
			"total_sessions":       aggregated.TotalSessions,
			"downtime_percentage":  aggregated.DowntimePercentage,
			"unique_users":         aggregated.UniqueUsers,
			"updated_at":           latestUpdatedAt,
			"period":               period,
		}

		common.ApiSuccess(c, data)
	}
}

// GetP2PGroupStatsLatest 获取P2P分组的最新统计数据（单个数据点）
// GET /api/p2p_groups/:id/stats/latest?model=gpt-4
func GetP2PGroupStatsLatest(c *gin.Context) {
	// 1. 获取分组ID
	idStr := c.Param("id")
	if idStr == "" {
		common.ApiError(c, errors.New("group id is required"))
		return
	}

	groupId, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid group id"))
		return
	}

	// 2. 验证分组是否存在
	_, err = model.GetGroupById(groupId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 3. 权限检查
	userId := c.GetInt("id")
	userRole := c.GetInt("role")

	if userRole != common.RoleRootUser && userRole != common.RoleAdminUser {
		isMember, err := checkGroupMember(userId, groupId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if !isMember {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "您不是该分组的成员，无权查看统计数据",
			})
			return
		}
	}

	// 4. 解析查询参数
	modelName := c.Query("model") // 可选

	// 5. 查询最新统计数据（与 GetP2PGroupStats 在带 model 参数时保持一致的结构）
	stat, err := model.GetLatestGroupStatistics(groupId, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	data := gin.H{
		"group_id":             stat.GroupId,
		"model_name":           stat.ModelName,
		"time_window_start":    stat.TimeWindowStart,
		"tpm":                  stat.TPM,
		"rpm":                  stat.RPM,
		"quota_pm":             stat.QuotaPM,
		"total_tokens":         stat.TotalTokens,
		"total_quota":          stat.TotalQuota,
		"fail_rate":            stat.FailRate,
		"avg_response_time":    stat.AvgResponseTimeMs,
		"avg_response_time_ms": stat.AvgResponseTimeMs,
		"avg_cache_hit_rate":   stat.AvgCacheHitRate,
		"stream_req_ratio":     stat.StreamReqRatio,
		"avg_concurrency":      stat.AvgConcurrency,
		"total_sessions":       stat.TotalSessions,
		"downtime_percentage":  stat.DowntimePercentage,
		"unique_users":         stat.UniqueUsers,
		"updated_at":           stat.UpdatedAt,
	}

	common.ApiSuccess(c, data)
}

// GetP2PGroupStatsHistory 获取P2P分组的历史统计数据序列
// GET /api/p2p_groups/:id/stats/history?model=gpt-4&start_time=...&end_time=...
func GetP2PGroupStatsHistory(c *gin.Context) {
	// 1. 获取分组ID
	idStr := c.Param("id")
	if idStr == "" {
		common.ApiError(c, errors.New("group id is required"))
		return
	}

	groupId, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid group id"))
		return
	}

	// 2. 验证分组是否存在
	_, err = model.GetGroupById(groupId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 3. 权限检查
	userId := c.GetInt("id")
	userRole := c.GetInt("role")

	if userRole != common.RoleRootUser && userRole != common.RoleAdminUser {
		isMember, err := checkGroupMember(userId, groupId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if !isMember {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "您不是该分组的成员，无权查看统计数据",
			})
			return
		}
	}

	// 4. 解析查询参数
	modelName := c.Query("model")
	var startTime, endTime int64
	if v := c.Query("start_time"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			startTime = ts
		}
	}
	if v := c.Query("end_time"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			endTime = ts
		}
	}

	// 5. 查询历史统计序列（按 UpdatedAt 时间范围过滤）
	var stats []*model.GroupStatistics
	query := model.DB.Where("group_id = ?", groupId)
	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}
	if startTime > 0 {
		query = query.Where("updated_at >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("updated_at <= ?", endTime)
	}
	if err := query.Order("updated_at DESC").Find(&stats).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, stats)
}

// ========== 辅助函数 ==========

// checkGroupMember 检查用户是否为分组成员
func checkGroupMember(userId, groupId int) (bool, error) {
	// 将分组 Owner 视为天然成员，避免因为缺少 user_groups 记录导致权限误拒绝。
	if isOwner, err := model.IsGroupOwner(userId, groupId); err != nil {
		return false, err
	} else if isOwner {
		return true, nil
	}

	memberInfo, err := model.GetMemberInfo(groupId, userId)
	if err != nil {
		// 如果找不到记录，返回false
		return false, nil
	}

	// 检查成员状态是否为Active
	return memberInfo.Status == model.MemberStatusActive, nil
}

// calculateStartTime 根据period计算起始时间
// 支持的格式：1h, 6h, 24h, 7d, 30d
func calculateStartTime(endTime int64, period string) (int64, error) {
	now := time.Unix(endTime, 0)
	var duration time.Duration

	switch period {
	case "1h":
		duration = 1 * time.Hour
	case "6h":
		duration = 6 * time.Hour
	case "24h", "1d":
		duration = 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		// 尝试解析自定义格式（如"2h", "3d"）
		parsedDuration, err := time.ParseDuration(period)
		if err != nil {
			return 0, errors.New("invalid period format, supported: 1h, 6h, 24h, 7d, 30d")
		}
		duration = parsedDuration
	}

	startTime := now.Add(-duration).Unix()
	return startTime, nil
}

// GetP2PGroupModelStats 获取 P2P 分组按模型聚合的统计数据
// GET /api/p2p_groups/:id/stats/models?period=7d
// 权限：分组成员或管理员
func GetP2PGroupModelStats(c *gin.Context) {
	// 1. 获取分组ID
	idStr := c.Param("id")
	if idStr == "" {
		common.ApiError(c, errors.New("group id is required"))
		return
	}
	groupId, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid group id"))
		return
	}

	// 2. 验证分组是否存在
	if _, err := model.GetGroupById(groupId); err != nil {
		common.ApiError(c, err)
		return
	}

	// 3. 权限检查（成员或管理员）
	userId := c.GetInt("id")
	userRole := c.GetInt("role")
	if userRole != common.RoleRootUser && userRole != common.RoleAdminUser {
		isMember, err := checkGroupMember(userId, groupId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if !isMember {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "您不是该分组的成员，无权查看统计数据",
			})
			return
		}
	}

	// 4. 解析 period 参数
	period := c.DefaultQuery("period", "7d")
	endTime := time.Now().Unix()
	startTime, err := calculateStartTime(endTime, period)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 5. 可选模型过滤
	modelName := c.Query("model_name")

	// 6. 聚合按模型统计
	stats, err := model.AggregateGroupModelStats(groupId, startTime, endTime, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, stats)
}

// GetP2PGroupModelDailyTokens 获取 P2P 分组按模型的每日 Token/Quota 消耗曲线
// GET /api/p2p_groups/:id/stats/models/daily_tokens?days=30&model_name=gpt-4
// 权限：分组成员或管理员
func GetP2PGroupModelDailyTokens(c *gin.Context) {
	// 1. 获取分组ID
	idStr := c.Param("id")
	if idStr == "" {
		common.ApiError(c, errors.New("group id is required"))
		return
	}
	groupId, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, errors.New("invalid group id"))
		return
	}

	// 2. 验证分组是否存在
	if _, err := model.GetGroupById(groupId); err != nil {
		common.ApiError(c, err)
		return
	}

	// 3. 权限检查
	userId := c.GetInt("id")
	userRole := c.GetInt("role")
	if userRole != common.RoleRootUser && userRole != common.RoleAdminUser {
		isMember, err := checkGroupMember(userId, groupId)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if !isMember {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "您不是该分组的成员，无权查看统计数据",
			})
			return
		}
	}

	// 4. days 参数
	daysStr := c.DefaultQuery("days", "30")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 1 {
		common.ApiError(c, errors.New("invalid days parameter"))
		return
	}

	// 5. 模型过滤
	modelName := c.Query("model_name")

	// 6. 聚合每日曲线
	usage, err := model.GetGroupModelDailyUsage(groupId, days, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, usage)
}
