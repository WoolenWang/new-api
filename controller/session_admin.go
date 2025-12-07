package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetSessionsSummary returns the session monitoring summary
// GET /api/admin/sessions/summary
func GetSessionsSummary(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Parse optional limit parameters
	topUsersLimit := common.String2Int(c.DefaultQuery("top_users_limit", "10"))
	if topUsersLimit <= 0 || topUsersLimit > 100 {
		topUsersLimit = 10
	}

	recentSessionsLimit := common.String2Int(c.DefaultQuery("recent_sessions_limit", "20"))
	if recentSessionsLimit <= 0 || recentSessionsLimit > 100 {
		recentSessionsLimit = 20
	}

	summary, err := service.GetSessionSummary(ctx, topUsersLimit, recentSessionsLimit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "获取会话摘要失败: " + err.Error(),
		})
		return
	}

	common.ApiSuccess(c, summary)
}

// GetUserSessionCount returns the current session count for a specific user
// GET /api/admin/sessions/user/:id
func GetUserSessionCount(c *gin.Context) {
	userId := common.String2Int(c.Param("id"))
	if userId <= 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的用户ID",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := service.GetUserSessionCount(ctx, userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "获取用户会话数失败: " + err.Error(),
		})
		return
	}

	common.ApiSuccess(c, gin.H{
		"user_id":       userId,
		"session_count": count,
	})
}

// CleanupChannelSessions manually cleans up all session bindings for a channel
// POST /api/admin/sessions/cleanup/:channel_id
func CleanupChannelSessions(c *gin.Context) {
	channelId := common.String2Int(c.Param("channel_id"))
	if channelId <= 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的渠道ID",
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := service.CleanupChannelSessions(ctx, channelId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "清理渠道会话失败: " + err.Error(),
		})
		return
	}

	common.ApiSuccess(c, gin.H{
		"channel_id": channelId,
		"message":    "渠道会话清理完成",
	})
}
