package controller

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetUserChannelStats 获取当前用户拥有的所有渠道的统计数据
// GET /api/channel/self/stats?period=1h&model=gpt-4
// 权限：用户自己
func GetUserChannelStats(c *gin.Context) {
	// 1. 获取当前用户ID
	userId := c.GetInt("id")

	// 2. 解析查询参数（当前实现仅使用 period 作为展示字段，预留按模型与时间窗口筛选的扩展点）
	period := c.DefaultQuery("period", "1h")

	// 3. 查询用户拥有的所有渠道
	var channels []model.Channel
	err := model.DB.Where("owner_user_id = ?", userId).Find(&channels).Error
	if err != nil {
		common.ApiError(c, errors.New("查询渠道列表失败"))
		return
	}

	if len(channels) == 0 {
		// 用户没有任何渠道
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    []map[string]interface{}{},
		})
		return
	}

	// 4. 为每个渠道返回统计数据
	var result []map[string]interface{}

	for _, channel := range channels {
		// 使用渠道表中已聚合的统计字段
		channelStats := map[string]interface{}{
			"channel_id":   channel.Id,
			"channel_name": channel.Name,
			"model_name":   extractPrimaryModel(channel.Models),
			"status":       channel.Status,
			"stats": map[string]interface{}{
				"period":              period,
				"tpm":                 channel.TPM,
				"rpm":                 channel.RPM,
				"quota_pm":            0, // channels 表中没有此字段，可以计算或留空
				"avg_response_time":   channel.AvgResponseTime,
				"fail_rate":           channel.FailRate,
				"avg_cache_hit_rate":  channel.AvgCacheHitRate,
				"stream_req_ratio":    channel.StreamReqRatio,
				"total_sessions":      channel.TotalSessions,
				"downtime_percentage": 0, // channels 表中没有此字段
				"unique_users":        0, // channels 表中没有此字段
			},
		}

		result = append(result, channelStats)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// GetUserChannelStatsHistory 获取指定渠道的历史统计趋势
// GET /api/channel/self/stats/history?channel_id=3&period=7d&model=gpt-4
// 权限：渠道所有者
func GetUserChannelStatsHistory(c *gin.Context) {
	// 1. 获取当前用户ID
	userId := c.GetInt("id")

	// 2. 解析查询参数
	channelIdStr := c.Query("channel_id")
	if channelIdStr == "" {
		common.ApiError(c, errors.New("channel_id is required"))
		return
	}

	// 支持 WQuant 侧传入的浮点形式 channel_id（如 "3.0"），向下兼容整型字符串。
	channelId, err := strconv.Atoi(channelIdStr)
	if err != nil {
		if f, ferr := strconv.ParseFloat(channelIdStr, 64); ferr == nil {
			rounded := int(math.Round(f))
			// 仅接受类似 3.0 这种“看起来是整数”的浮点值
			if rounded > 0 && math.Abs(f-float64(rounded)) < 1e-9 {
				channelId = rounded
				common.SysLog("[GetUserChannelStatsHistory] tolerate float channel_id param: raw=%s -> id=%d", channelIdStr, channelId)
			} else {
				common.ApiError(c, errors.New("invalid channel_id"))
				return
			}
		} else {
			common.ApiError(c, errors.New("invalid channel_id"))
			return
		}
	}

	period := c.DefaultQuery("period", "7d")
	modelName := c.Query("model") // 可选：按模型筛选

	// 3. 验证渠道归属权限
	channel, err := model.GetChannelById(channelId, false)
	if err != nil {
		common.ApiError(c, errors.New("渠道不存在"))
		return
	}

	if channel.OwnerUserId != userId {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"error":   "您不是该渠道的所有者，无权查看统计数据",
		})
		return
	}

	// 4. 计算时间范围
	endTime := time.Now().Unix()
	startTime, err := calculateStartTime(endTime, period)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 5. 查询该渠道的历史统计数据
	var stats []model.ChannelStatistics
	query := model.DB.Where("channel_id = ?", channelId).
		Where("time_window_start >= ?", startTime).
		Where("time_window_start <= ?", endTime)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	err = query.Order("time_window_start ASC").Find(&stats).Error
	if err != nil {
		common.ApiError(c, errors.New("查询历史统计失败"))
		return
	}

	// 6. 转换为时间序列格式
	timeSeries := make([]map[string]interface{}, 0, len(stats))
	for _, stat := range stats {
		// 计算平均值和比率
		var avgResponseTime int
		var failRate float64

		if stat.RequestCount > 0 {
			avgResponseTime = int(stat.TotalLatencyMs / int64(stat.RequestCount))
			failRate = float64(stat.FailCount) * 100.0 / float64(stat.RequestCount)
		}

		// 计算时间范围（分钟数）- 假设每个统计窗口是15分钟
		windowDurationMinutes := 15.0

		tpm := 0
		rpm := 0
		if windowDurationMinutes > 0 {
			tpm = int(float64(stat.TotalTokens) / windowDurationMinutes)
			rpm = int(float64(stat.RequestCount) / windowDurationMinutes)
		}

		timePoint := map[string]interface{}{
			"timestamp":         stat.TimeWindowStart,
			"tpm":               tpm,
			"rpm":               rpm,
			"avg_response_time": avgResponseTime,
			"fail_rate":         failRate,
		}

		timeSeries = append(timeSeries, timePoint)
	}

	// 7. 返回结果
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"channel_id":  channelId,
			"model_name":  modelName,
			"time_series": timeSeries,
		},
	})
}
