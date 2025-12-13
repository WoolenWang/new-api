package controller

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetPublicGroupRankings 处理 GET /api/groups/public/rankings
//
// 用途：为"分组广场"提供公开共享分组的排行榜数据
// 权限：任何已登录用户（middleware.UserAuth()）
//
// 查询参数：
//   - metric (必填): 排名指标，支持 tokens_7d, tokens_30d, tpm, rpm, latency, fail_rate, downtime
//   - period (可选): 时间窗口，默认 "7d"，支持 1h/6h/24h/7d/30d
//   - order (可选): 排序方向，"asc" 或 "desc"，默认根据 metric 类型自动判断
//   - limit (可选): 每页数量，默认 20，最大 100
//   - offset (可选): 偏移量，默认 0
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 5.4
func GetPublicGroupRankings(c *gin.Context) {
	// 1. 解析必填参数：metric
	metric := c.Query("metric")
	if metric == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "metric parameter is required",
		})
		return
	}

	// 2. 验证 metric 参数
	validMetrics := []string{"tokens_7d", "tokens_30d", "tpm", "rpm", "latency", "fail_rate", "downtime"}
	if !contains(validMetrics, metric) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid metric: must be one of " + strings.Join(validMetrics, ", "),
		})
		return
	}

	// 3. 解析可选参数：period
	period := c.DefaultQuery("period", "7d")
	validPeriods := []string{"1h", "6h", "24h", "1d", "7d", "30d"}
	if !contains(validPeriods, period) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid period: must be one of " + strings.Join(validPeriods, ", "),
		})
		return
	}

	// 4. 解析可选参数：order
	order := c.Query("order")
	if order == "" {
		// 根据 metric 类型自动判断默认排序方向
		switch metric {
		case "tokens_7d", "tokens_30d", "tpm", "rpm":
			// 容量类指标：降序（越大越好）
			order = "desc"
		case "latency", "fail_rate", "downtime":
			// 稳定性类指标：升序（越小越好）
			order = "asc"
		default:
			order = "desc" // 默认降序
		}
	} else {
		// 验证 order 参数
		if order != "asc" && order != "desc" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "invalid order: must be 'asc' or 'desc'",
			})
			return
		}
	}

	// 5. 解析可选参数：limit
	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid limit: must be between 1 and 100",
		})
		return
	}

	// 6. 解析可选参数：offset
	offsetStr := c.DefaultQuery("offset", "0")
	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid offset: must be >= 0",
		})
		return
	}

	// 7. 调用 Model 层获取聚合数据（未排序）
	results, err := model.RankPublicGroups(metric, period)
	if err != nil {
		common.SysError("failed to rank public groups: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to fetch group rankings: " + err.Error(),
		})
		return
	}

	// 8. 在 Controller 层进行排序
	// 根据 metric 和 order 对结果进行排序
	sortGroupRankings(results, metric, order)

	// 9. 分页处理
	totalCount := len(results)
	start := offset
	end := offset + limit

	// 边界检查
	if start > totalCount {
		start = totalCount
	}
	if end > totalCount {
		end = totalCount
	}

	// 提取分页后的结果
	var pagedResults []model.GroupRankingRow
	if start < end {
		pagedResults = results[start:end]
	} else {
		pagedResults = []model.GroupRankingRow{}
	}

	// 10. 返回响应
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"metric":      metric,
			"period":      period,
			"order":       order,
			"total_count": totalCount,
			"offset":      offset,
			"limit":       limit,
			"items":       pagedResults,
		},
	})
}

// sortGroupRankings 对分组排名数据进行排序
//
// 参数：
//   - results: 待排序的数据切片（会被原地修改）
//   - metric: 排名指标
//   - order: 排序方向（"asc" 或 "desc"）
func sortGroupRankings(results []model.GroupRankingRow, metric string, order string) {
	sort.Slice(results, func(i, j int) bool {
		var less bool

		// 根据 metric 确定比较逻辑
		switch metric {
		case "tokens_7d":
			less = results[i].Tokens7d < results[j].Tokens7d
		case "tokens_30d":
			less = results[i].Tokens30d < results[j].Tokens30d
		case "tpm":
			less = results[i].AvgTPM < results[j].AvgTPM
		case "rpm":
			less = results[i].AvgRPM < results[j].AvgRPM
		case "latency":
			less = results[i].AvgLatencyMs < results[j].AvgLatencyMs
		case "fail_rate":
			less = results[i].AvgFailRate < results[j].AvgFailRate
		case "downtime":
			less = results[i].AvgDowntimePercent < results[j].AvgDowntimePercent
		default:
			// 默认按 group_id 排序
			less = results[i].GroupId < results[j].GroupId
		}

		// 根据 order 决定是否反转比较结果
		if order == "desc" {
			return !less
		}
		return less
	})
}

// contains 检查字符串切片是否包含指定元素
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
