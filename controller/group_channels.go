package controller

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetPublicGroupChannels 获取公开分组的渠道列表（部分脱敏）
// GET /api/groups/public/channels?group_id=4&period=1h
// 权限：所有已登录用户
func GetPublicGroupChannels(c *gin.Context) {
	// 1. 解析参数
	groupIdStr := c.Query("group_id")
	if groupIdStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   "group_id is required",
		})
		return
	}

	// 支持 WQuant 侧传入的浮点形式 group_id（如 "4.0"），向下兼容整型字符串。
	groupId, err := strconv.Atoi(groupIdStr)
	if err != nil {
		if f, ferr := strconv.ParseFloat(groupIdStr, 64); ferr == nil {
			rounded := int(math.Round(f))
			// 仅接受类似 4.0 这种“看起来是整数”的浮点值
			if rounded > 0 && math.Abs(f-float64(rounded)) < 1e-9 {
				groupId = rounded
				common.SysLog("[GetPublicGroupChannels] tolerate float group_id param: raw=%s -> id=%d", groupIdStr, groupId)
			} else {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"error":   "invalid group_id",
				})
				return
			}
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "invalid group_id",
			})
			return
		}
	}

	period := c.DefaultQuery("period", "1h")

	common.SysLog(fmt.Sprintf("[GetPublicGroupChannels] group_id=%d, period=%s, user_id=%d", groupId, period, c.GetInt("id")))

	// 2. 验证分组是否存在且为公开分组
	group, err := model.GetGroupById(groupId)
	if err != nil {
		common.SysLog(fmt.Sprintf("[GetPublicGroupChannels] 分组不存在: group_id=%d, error=%s", groupId, err.Error()))
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "分组不存在",
		})
		return
	}

	common.SysLog(fmt.Sprintf("[GetPublicGroupChannels] 分组信息: id=%d, type=%d, join_method=%d", group.Id, group.Type, group.JoinMethod))

	// 检查是否为公开分组 (Type=Shared 且 JoinMethod != 0)
	if group.Type != model.GroupTypeShared || group.JoinMethod == model.JoinMethodInvite {
		common.SysLog(fmt.Sprintf("[GetPublicGroupChannels] 不是公开分组: type=%d, join_method=%d", group.Type, group.JoinMethod))
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "该分组不是公开分组",
		})
		return
	}

	// 3. 获取分组内的渠道列表
	channels, err := getGroupChannelsWithStats(groupId, period, true) // true = 脱敏
	if err != nil {
		common.SysLog(fmt.Sprintf("[GetPublicGroupChannels] 查询渠道列表失败: %s", err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	common.SysLog(fmt.Sprintf("[GetPublicGroupChannels] 查询成功, 返回 %d 个渠道", len(channels)))

	// 4. 返回结果
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"group_id":   groupId,
			"group_name": group.DisplayName,
			"channels":   channels,
		},
	})
}

// GetJoinedGroupChannels 获取已加入分组的渠道列表（完整数据）
// GET /api/groups/joined/channels?group_id=4&period=1h
// 权限：分组成员
func GetJoinedGroupChannels(c *gin.Context) {
	// 1. 解析参数
	groupIdStr := c.Query("group_id")
	if groupIdStr == "" {
		common.ApiError(c, errors.New("group_id is required"))
		return
	}

	// 支持 WQuant 侧传入的浮点形式 group_id（如 "4.0"）
	groupId, err := strconv.Atoi(groupIdStr)
	if err != nil {
		if f, ferr := strconv.ParseFloat(groupIdStr, 64); ferr == nil {
			rounded := int(math.Round(f))
			if rounded > 0 && math.Abs(f-float64(rounded)) < 1e-9 {
				groupId = rounded
				common.SysLog("[GetJoinedGroupChannels] tolerate float group_id param: raw=%s -> id=%d", groupIdStr, groupId)
			} else {
				common.ApiError(c, errors.New("invalid group_id"))
				return
			}
		} else {
			common.ApiError(c, errors.New("invalid group_id"))
			return
		}
	}

	period := c.DefaultQuery("period", "1h")
	userId := c.GetInt("id")

	// 2. 验证分组是否存在
	group, err := model.GetGroupById(groupId)
	if err != nil {
		common.ApiError(c, errors.New("分组不存在"))
		return
	}

	// 3. 验证用户是否为分组成员
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
				"error":   "您不是该分组的成员，无权查看完整渠道信息",
			})
			return
		}
	}

	// 4. 获取分组内的渠道列表（完整数据，不脱敏）
	channels, err := getGroupChannelsWithStats(groupId, period, false) // false = 不脱敏
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 5. 返回结果
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"group_id":   groupId,
			"group_name": group.DisplayName,
			"channels":   channels,
		},
	})
}

// getGroupChannelsWithStats 获取分组内的渠道列表和统计信息
// desensitize: true = 脱敏（隐藏 owner_user_id），false = 完整数据
func getGroupChannelsWithStats(groupId int, period string, desensitize bool) ([]map[string]interface{}, error) {
	// 1. 查询所有启用的渠道
	var allChannels []model.Channel
	err := model.DB.Find(&allChannels).Error
	if err != nil {
		return nil, errors.New("查询渠道列表失败")
	}

	// 2. 筛选包含目标分组的渠道
	result := make([]map[string]interface{}, 0)

	for _, channel := range allChannels {
		// 获取渠道允许的分组ID列表
		allowedGroups := channel.GetAllowedGroupIDs()

		// 检查该渠道是否允许目标分组访问
		isAllowed := false
		for _, gid := range allowedGroups {
			if gid == groupId {
				isAllowed = true
				break
			}
		}

		if !isAllowed {
			continue
		}

		// 3. 构建渠道数据
		channelData := map[string]interface{}{
			"channel_id":   channel.Id,
			"channel_name": channel.Name,
			"model_name":   extractPrimaryModel(channel.Models),
			"status":       channel.Status,
			"stats": map[string]interface{}{
				"tpm":                channel.TPM,
				"rpm":                channel.RPM,
				"avg_response_time":  channel.AvgResponseTime,
				"fail_rate":          channel.FailRate,
				"total_sessions":     channel.TotalSessions,
				"avg_cache_hit_rate": channel.AvgCacheHitRate,
				"stream_req_ratio":   channel.StreamReqRatio,
			},
		}

		// 4. 根据是否脱敏决定是否包含敏感字段
		if !desensitize {
			channelData["owner_user_id"] = channel.OwnerUserId
		}

		// 5. 查询并添加 owner_username（总是可见）
		if channel.OwnerUserId > 0 {
			if user, err := model.GetUserById(channel.OwnerUserId, false); err == nil {
				channelData["owner_username"] = user.Username
			}
		}

		result = append(result, channelData)
	}

	return result, nil
}

// extractPrimaryModel 提取渠道的主要模型名称
// 从 models 字段（逗号分隔的模型列表）中提取第一个模型
func extractPrimaryModel(models string) string {
	if models == "" {
		return ""
	}

	// models 格式可能是 "gpt-4,gpt-3.5-turbo" 或 "gpt-4"
	parts := strings.Split(models, ",")
	if len(parts) > 0 {
		return parts[0]
	}

	return models
}
