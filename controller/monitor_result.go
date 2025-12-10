package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetMonitoringResults 获取监控结果列表
// @Summary 获取监控结果列表
// @Description 根据渠道ID或模型名称获取监控结果
// @Tags 监控结果
// @Accept json
// @Produce json
// @Param channel_id query int false "渠道ID"
// @Param model_name query string false "模型名称"
// @Param status query string false "状态筛选 (pass/fail/monitor_failed)"
// @Param limit query int false "返回数量限制" default(100)
// @Success 200 {object} common.Response{data=[]model.ModelMonitoringResult}
// @Router /api/monitor/results [get]
func GetMonitoringResults(c *gin.Context) {
	channelIdStr := c.Query("channel_id")
	modelName := c.Query("model_name")
	status := c.Query("status")
	limitStr := c.DefaultQuery("limit", "100")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}

	var results []*model.ModelMonitoringResult

	// 根据不同的查询条件获取结果
	if channelIdStr != "" {
		channelId, err := strconv.Atoi(channelIdStr)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无效的渠道ID",
			})
			return
		}

		if modelName != "" {
			// 按渠道和模型查询
			results, err = model.GetMonitoringResultsByChannelAndModel(channelId, modelName, 0, 0, limit)
		} else {
			// 仅按渠道查询
			results, err = model.GetMonitoringResultsByChannel(channelId, limit)
		}
	} else if modelName != "" {
		// 仅按模型查询
		results, err = model.GetMonitoringResultsByModel(modelName, limit)
	} else if status != "" {
		// 按状态查询
		results, err = model.GetMonitoringResultsByStatus(status, limit)
	} else {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "请提供至少一个查询条件: channel_id, model_name 或 status",
		})
		return
	}

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取监控结果失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    results,
	})
}

// GetChannelMonitoringResults 获取渠道的监控结果列表
// @Summary 获取渠道监控结果
// @Description 获取指定渠道的历史监控结果
// @Tags 监控结果
// @Accept json
// @Produce json
// @Param id path int true "渠道ID"
// @Param model_name query string false "模型名称"
// @Param test_type query string false "检测类型"
// @Param start_time query int false "开始时间戳"
// @Param end_time query int false "结束时间戳"
// @Param limit query int false "返回数量限制" default(100)
// @Success 200 {object} common.Response{data=[]model.ModelMonitoringResult}
// @Router /api/channels/:id/monitoring_results [get]
func GetChannelMonitoringResults(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的渠道ID",
		})
		return
	}

	modelName := c.Query("model_name")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	limitStr := c.DefaultQuery("limit", "100")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}

	var startTime, endTime int64
	if startTimeStr != "" {
		startTime, _ = strconv.ParseInt(startTimeStr, 10, 64)
	}
	if endTimeStr != "" {
		endTime, _ = strconv.ParseInt(endTimeStr, 10, 64)
	}

	results, err := model.GetMonitoringResultsByChannelAndModel(channelId, modelName, startTime, endTime, limit)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取监控结果失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    results,
	})
}

// GetModelMonitoringReport 获取模型的监控报告
// @Summary 获取模型监控报告
// @Description 获取指定模型在所有渠道的监控统计报告
// @Tags 监控结果
// @Accept json
// @Produce json
// @Param model_name path string true "模型名称"
// @Param test_type query string false "检测类型"
// @Param start_time query int false "开始时间戳"
// @Param end_time query int false "结束时间戳"
// @Success 200 {object} common.Response{data=[]model.MonitoringStatistics}
// @Router /api/models/:model_name/monitoring_report [get]
func GetModelMonitoringReport(c *gin.Context) {
	modelName := c.Param("model_name")
	if modelName == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "缺少模型名称参数",
		})
		return
	}

	testType := c.Query("test_type")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime int64
	if startTimeStr != "" {
		startTime, _ = strconv.ParseInt(startTimeStr, 10, 64)
	}
	if endTimeStr != "" {
		endTime, _ = strconv.ParseInt(endTimeStr, 10, 64)
	}

	report, err := model.GetModelMonitoringReport(modelName, testType, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取监控报告失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    report,
	})
}

// GetMonitoringStatistics 获取渠道的监控统计信息
// @Summary 获取监控统计信息
// @Description 获取指定渠道和模型的监控统计数据
// @Tags 监控结果
// @Accept json
// @Produce json
// @Param channel_id query int true "渠道ID"
// @Param model_name query string false "模型名称"
// @Param start_time query int false "开始时间戳"
// @Param end_time query int false "结束时间戳"
// @Success 200 {object} common.Response{data=model.MonitoringStatistics}
// @Router /api/monitor/statistics [get]
func GetMonitoringStatistics(c *gin.Context) {
	channelIdStr := c.Query("channel_id")
	if channelIdStr == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "缺少渠道ID参数",
		})
		return
	}

	channelId, err := strconv.Atoi(channelIdStr)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的渠道ID",
		})
		return
	}

	modelName := c.Query("model_name")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime int64
	if startTimeStr != "" {
		startTime, _ = strconv.ParseInt(startTimeStr, 10, 64)
	}
	if endTimeStr != "" {
		endTime, _ = strconv.ParseInt(endTimeStr, 10, 64)
	}

	stats, err := model.GetMonitoringStatistics(channelId, modelName, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取监控统计失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}

// GetLatestMonitoringResult 获取渠道和模型的最新监控结果
// @Summary 获取最新监控结果
// @Description 获取指定渠道和模型的最新一次监控结果
// @Tags 监控结果
// @Accept json
// @Produce json
// @Param channel_id query int true "渠道ID"
// @Param model_name query string true "模型名称"
// @Param test_type query string false "检测类型"
// @Success 200 {object} common.Response{data=model.ModelMonitoringResult}
// @Router /api/monitor/results/latest [get]
func GetLatestMonitoringResult(c *gin.Context) {
	channelIdStr := c.Query("channel_id")
	if channelIdStr == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "缺少渠道ID参数",
		})
		return
	}

	channelId, err := strconv.Atoi(channelIdStr)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的渠道ID",
		})
		return
	}

	modelName := c.Query("model_name")
	if modelName == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "缺少模型名称参数",
		})
		return
	}

	testType := c.Query("test_type")

	result, err := model.GetLatestMonitoringResult(channelId, modelName, testType)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取最新监控结果失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result,
	})
}

// DeleteMonitoringResult 删除监控结果
// @Summary 删除监控结果
// @Description 删除指定的监控结果记录
// @Tags 监控结果
// @Accept json
// @Produce json
// @Param id path int true "结果ID"
// @Success 200 {object} common.Response
// @Router /api/monitor/results/:id [delete]
func DeleteMonitoringResult(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的结果ID",
		})
		return
	}

	if err := model.DeleteMonitoringResult(id); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("删除监控结果失败: %v", err),
		})
		return
	}

	common.SysLog(fmt.Sprintf("管理员删除监控结果: id=%d", id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "删除监控结果成功",
	})
}

// CleanupOldMonitoringResults 清理旧的监控结果
// @Summary 清理旧监控结果
// @Description 删除指定时间之前的监控结果
// @Tags 监控结果
// @Accept json
// @Produce json
// @Param before_time query int true "截止时间戳（删除此时间之前的记录）"
// @Success 200 {object} common.Response
// @Router /api/monitor/results/cleanup [delete]
func CleanupOldMonitoringResults(c *gin.Context) {
	beforeTimeStr := c.Query("before_time")
	if beforeTimeStr == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "缺少时间参数 before_time",
		})
		return
	}

	beforeTime, err := strconv.ParseInt(beforeTimeStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的时间戳",
		})
		return
	}

	rowsAffected, err := model.DeleteMonitoringResultsBefore(beforeTime)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("清理监控结果失败: %v", err),
		})
		return
	}

	common.SysLog(fmt.Sprintf("管理员清理监控结果: deleted_count=%d, before_time=%d", rowsAffected, beforeTime))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("成功清理 %d 条记录", rowsAffected),
	})
}

// GetFailedChannels 获取失败率高的渠道列表
// @Summary 获取失败渠道列表
// @Description 获取在指定时间范围内失败率超过阈值的渠道列表
// @Tags 监控结果
// @Accept json
// @Produce json
// @Param start_time query int false "开始时间戳"
// @Param end_time query int false "结束时间戳"
// @Param failure_threshold query float64 false "失败率阈值(%)" default(50.0)
// @Success 200 {object} common.Response{data=[]int}
// @Router /api/monitor/failed_channels [get]
func GetFailedChannels(c *gin.Context) {
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	thresholdStr := c.DefaultQuery("failure_threshold", "50.0")

	var startTime, endTime int64
	if startTimeStr != "" {
		startTime, _ = strconv.ParseInt(startTimeStr, 10, 64)
	}
	if endTimeStr != "" {
		endTime, _ = strconv.ParseInt(endTimeStr, 10, 64)
	}

	threshold, err := strconv.ParseFloat(thresholdStr, 64)
	if err != nil {
		threshold = 50.0
	}

	channelIds, err := model.GetFailedChannels(startTime, endTime, threshold)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取失败渠道列表失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channelIds,
	})
}
