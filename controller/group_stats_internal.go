package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// CreateChannelStatisticsInternal
// POST /api/internal/channel_statistics
// 仅用于测试和内部运维：直接写入 channel_statistics 表，并发布渠道统计更新事件。
func CreateChannelStatisticsInternal(c *gin.Context) {
	var input model.ChannelStatistics
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}

	if input.ChannelId == 0 || input.ModelName == "" {
		common.ApiErrorMsg(c, "channel_id and model_name are required")
		return
	}

	// 为测试稳定性起见，忽略外部传入的窗口起始时间，统一使用当前时间戳。
	// 这样每次调用都会写入一个新的时间窗口，避免不同测试用例之间的窗口冲突。
	input.TimeWindowStart = time.Now().Unix()

	// 记录一条调试日志，便于在集成测试中排查不同用例之间的时间窗口与数据叠加关系。
	common.SysLog("CreateChannelStatisticsInternal: channel_id=%d model=%s window=%d tokens=%d",
		input.ChannelId, input.ModelName, input.TimeWindowStart, input.TotalTokens)

	if err := model.UpsertChannelStatistics(&input); err != nil {
		common.ApiError(c, err)
		return
	}

	// 确保分组统计调度器已启动，然后发布渠道统计更新事件，
	// 让 GroupStatsScheduler 能够按节流规则调度聚合任务。
	scheduler := service.GetGlobalScheduler()
	scheduler.Start()
	service.PublishChannelStatsUpdatedEvent(input.ChannelId, input.ModelName, input.TimeWindowStart)

	common.ApiSuccess(c, input)
}

// GetChannelStatisticsInternal
// GET /api/internal/channel_statistics?channel_id=...&model_name=...&start_time=...&end_time=...
// 仅用于测试：按渠道和模型查询 channel_statistics 记录。
func GetChannelStatisticsInternal(c *gin.Context) {
	channelIDStr := c.Query("channel_id")
	if channelIDStr == "" {
		common.ApiErrorMsg(c, "channel_id is required")
		return
	}
	channelID, err := strconv.Atoi(channelIDStr)
	if err != nil {
		common.ApiErrorMsg(c, "invalid channel_id")
		return
	}

	modelName := c.Query("model_name")

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

	stats, err := model.GetChannelStatistics(channelID, modelName, startTime, endTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, stats)
}

// TriggerGroupAggregationInternal
// POST /api/internal/groups/:id/trigger_aggregation
// 仅用于测试或手动运维：绕过节流窗口，立即为指定分组触发一次聚合任务。
func TriggerGroupAggregationInternal(c *gin.Context) {
	idStr := c.Param("id")
	if idStr == "" {
		common.ApiErrorMsg(c, "group id is required")
		return
	}
	groupID, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiErrorMsg(c, "invalid group id")
		return
	}

	// 可选：验证分组是否存在
	if _, err := model.GetGroupById(groupID); err != nil {
		common.ApiError(c, err)
		return
	}

	if err := service.TriggerGroupAggregation(groupID); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"group_id": groupID,
	})
}

// GetGroupAggregationStatusInternal
// GET /api/internal/groups/:id/aggregation_status
// 返回调度器内部观察到的分组聚合状态（用于测试与调试）。
func GetGroupAggregationStatusInternal(c *gin.Context) {
	idStr := c.Param("id")
	if idStr == "" {
		common.ApiErrorMsg(c, "group id is required")
		return
	}
	groupID, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiErrorMsg(c, "invalid group id")
		return
	}

	scheduler := service.GetGlobalScheduler()

	lastUpdateTime, exists := scheduler.GetLastUpdateTime(groupID)
	queueLen := scheduler.GetTaskQueueLength()

	status := gin.H{
		"group_id":          groupID,
		"last_update_time":  lastUpdateTime,
		"has_last_update":   exists,
		"task_queue_length": queueLen,
		"status":            http.StatusOK,
	}

	common.ApiSuccess(c, status)
}
