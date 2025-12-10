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
	group, err := model.GetGroupById(groupId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 3. 权限检查：验证用户是否为分组成员或管理员
	userId := c.GetInt("id")
	userRole := c.GetInt("role")

	if userRole != common.RoleRootUser && userRole != common.RoleAdminUser {
		// 非管理员，检查是否为分组成员
		isMember, err := isGroupMember(userId, groupId)
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

	// 5. 计算时间范围
	endTime := time.Now().Unix()
	startTime, err := calculateStartTime(endTime, period)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 6. 查询统计数据
	// 如果指定了model，查询特定模型的数据；否则查询所有模型
	if modelName != "" {
		// 查询特定模型的统计数据
		stats, err := model.GetGroupStatistics(groupId, modelName, startTime, endTime)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 聚合时间范围内的数据
		aggregated, err := model.AggregateGroupStatisticsByTime(groupId, modelName, startTime, endTime)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"group_id":    groupId,
				"group_name":  group.Name,
				"model_name":  modelName,
				"period":      period,
				"aggregated":  aggregated,
				"time_series": stats,
			},
		})
	} else {
		// 查询所有模型的统计数据（不聚合，返回原始列表）
		stats, err := model.GetGroupStatistics(groupId, "", startTime, endTime)
		if err != nil {
			common.ApiError(c, err)
			return
		}

		// 按模型分组聚合
		modelAggregates := make(map[string]*model.AggregatedGroupStats)

		// 获取所有唯一的模型名称
		uniqueModels := make(map[string]bool)
		for _, stat := range stats {
			uniqueModels[stat.ModelName] = true
		}

		// 对每个模型进行聚合
		for modelName := range uniqueModels {
			aggregated, err := model.AggregateGroupStatisticsByTime(groupId, modelName, startTime, endTime)
			if err != nil {
				common.SysLog("Error aggregating stats for model %s: %v", modelName, err)
				continue
			}
			modelAggregates[modelName] = aggregated
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"group_id":    groupId,
				"group_name":  group.Name,
				"period":      period,
				"models":      modelAggregates,
				"time_series": stats,
			},
		})
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
	group, err := model.GetGroupById(groupId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 3. 权限检查
	userId := c.GetInt("id")
	userRole := c.GetInt("role")

	if userRole != common.RoleRootUser && userRole != common.RoleAdminUser {
		isMember, err := isGroupMember(userId, groupId)
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

	// 5. 查询最新统计数据
	stat, err := model.GetLatestGroupStatistics(groupId, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"group_id":   groupId,
			"group_name": group.Name,
			"stat":       stat,
		},
	})
}

// ========== 辅助函数 ==========

// isGroupMember 检查用户是否为分组成员
func isGroupMember(userId, groupId int) (bool, error) {
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
