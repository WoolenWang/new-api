package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// P2PChannelUsageInfo contains channel info plus runtime usage statistics
type P2PChannelUsageInfo struct {
	Id                 int     `json:"id"`
	Name               string  `json:"name"`
	Type               int     `json:"type"`
	Status             int     `json:"status"`
	OwnerUserId        int     `json:"owner_user_id"`
	TotalQuota         int64   `json:"total_quota"`
	UsedQuota          int64   `json:"used_quota"`         // From DB
	UsedQuotaRuntime   int64   `json:"used_quota_runtime"` // From memory stats
	CurrentConcurrency int     `json:"current_concurrency"`
	Concurrency        int     `json:"concurrency"`
	HourlyRequests     int     `json:"hourly_requests"`
	HourlyLimit        int     `json:"hourly_limit"`
	DailyRequests      int     `json:"daily_requests"`
	DailyLimit         int     `json:"daily_limit"`
	IsPrivate          bool    `json:"is_private"`
	AllowedUsers       *string `json:"allowed_users"`
	AllowedGroups      *string `json:"allowed_groups"`
	CreatedTime        int64   `json:"created_time"`
}

// GetP2PChannels returns all P2P channels with usage statistics
// GET /api/admin/p2p_channels
func GetP2PChannels(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	// Query only P2P channels (owner_user_id != 0)
	var channels []*model.Channel
	var total int64

	baseQuery := model.DB.Model(&model.Channel{}).Where("owner_user_id != ?", 0)
	baseQuery.Count(&total)

	err := baseQuery.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&channels).Error
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	// Build response with runtime stats
	result := make([]P2PChannelUsageInfo, 0, len(channels))
	for _, ch := range channels {
		stats := model.GetChannelUsageStats(ch.Id)
		stats.mu.RLock()
		info := P2PChannelUsageInfo{
			Id:                 ch.Id,
			Name:               ch.Name,
			Type:               ch.Type,
			Status:             ch.Status,
			OwnerUserId:        ch.OwnerUserId,
			TotalQuota:         ch.TotalQuota,
			UsedQuota:          ch.UsedQuota,    // From database
			UsedQuotaRuntime:   stats.UsedQuota, // From memory
			CurrentConcurrency: stats.CurrentConcurrency,
			Concurrency:        ch.Concurrency,
			HourlyRequests:     stats.HourlyRequests,
			HourlyLimit:        ch.HourlyLimit,
			DailyRequests:      stats.DailyRequests,
			DailyLimit:         ch.DailyLimit,
			IsPrivate:          ch.IsPrivate,
			AllowedUsers:       ch.AllowedUsers,
			AllowedGroups:      ch.AllowedGroups,
			CreatedTime:        ch.CreatedTime,
		}
		stats.mu.RUnlock()
		result = append(result, info)
	}

	common.ApiSuccess(c, gin.H{
		"items":     result,
		"total":     total,
		"page":      pageInfo.GetPage(),
		"page_size": pageInfo.GetPageSize(),
	})
}

// GetChannelUsage returns detailed usage statistics for a specific channel
// GET /api/admin/channel/:id/usage
func GetChannelUsage(c *gin.Context) {
	id := c.Param("id")

	// Query channel from database
	channel, err := model.GetChannelById(common.String2Int(id), false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "渠道不存在",
		})
		return
	}

	// Get runtime statistics
	stats := model.GetChannelUsageStats(channel.Id)
	stats.mu.RLock()
	defer stats.mu.RUnlock()

	common.ApiSuccess(c, gin.H{
		"channel_id":          channel.Id,
		"channel_name":        channel.Name,
		"owner_user_id":       channel.OwnerUserId,
		"total_quota":         channel.TotalQuota,
		"used_quota_db":       channel.UsedQuota, // Persisted value
		"used_quota_runtime":  stats.UsedQuota,   // Runtime value
		"current_concurrency": stats.CurrentConcurrency,
		"concurrency_limit":   channel.Concurrency,
		"hourly_requests":     stats.HourlyRequests,
		"hourly_limit":        channel.HourlyLimit,
		"daily_requests":      stats.DailyRequests,
		"daily_limit":         channel.DailyLimit,
		"hour_start_time":     stats.HourStartTime,
		"day_start_time":      stats.DayStartTime,
	})
}

// GetChannelConcurrencySnapshot returns a lightweight snapshot of current concurrency for all P2P channels
// This is optimized for quick polling and monitoring scenarios
// GET /api/admin/channel_concurrency
func GetChannelConcurrencySnapshot(c *gin.Context) {
	snapshot := model.GetChannelConcurrencySnapshot()

	common.ApiSuccess(c, gin.H{
		"concurrency_snapshot": snapshot,
		"total_channels":       len(snapshot),
	})
}
