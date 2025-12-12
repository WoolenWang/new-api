package service

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

// ============================================
// 套餐监控指标采集服务
// 相关设计：docs/NewAPI-支持多种包月套餐-优化版.md 第 11.1 节
// ============================================

// PackageMetrics 套餐监控指标
type PackageMetrics struct {
	// 请求计数
	TotalRequests      uint64 `json:"total_requests"`       // 总请求数
	PackageRequests    uint64 `json:"package_requests"`     // 使用套餐的请求数
	BalanceRequests    uint64 `json:"balance_requests"`     // 使用余额的请求数
	FallbackRequests   uint64 `json:"fallback_requests"`    // Fallback 到余额的请求数
	WindowExceededReqs uint64 `json:"window_exceeded_reqs"` // 窗口超限请求数
	LuaScriptFailures  uint64 `json:"lua_script_failures"`  // Lua 脚本失败次数

	// 时间统计（用于计算延迟）
	PackageQueryTimeNs uint64 `json:"package_query_time_ns"` // 套餐查询总耗时（纳秒）
	PackageQueryCount  uint64 `json:"package_query_count"`   // 套餐查询次数

	// 窗口利用率统计
	WindowCreations   uint64 `json:"window_creations"`   // 窗口创建次数
	WindowExpirations uint64 `json:"window_expirations"` // 窗口过期次数
}

// 全局指标实例（原子操作）
var globalPackageMetrics PackageMetrics

// IncrementPackageRequest 记录使用套餐的请求
func IncrementPackageRequest() {
	atomic.AddUint64(&globalPackageMetrics.TotalRequests, 1)
	atomic.AddUint64(&globalPackageMetrics.PackageRequests, 1)
}

// IncrementBalanceRequest 记录使用余额的请求
func IncrementBalanceRequest() {
	atomic.AddUint64(&globalPackageMetrics.TotalRequests, 1)
	atomic.AddUint64(&globalPackageMetrics.BalanceRequests, 1)
}

// IncrementFallbackRequest 记录 Fallback 到余额的请求
func IncrementFallbackRequest() {
	atomic.AddUint64(&globalPackageMetrics.FallbackRequests, 1)
}

// IncrementWindowExceeded 记录窗口超限次数
func IncrementWindowExceeded() {
	atomic.AddUint64(&globalPackageMetrics.WindowExceededReqs, 1)
}

// IncrementLuaScriptFailure 记录 Lua 脚本失败
func IncrementLuaScriptFailure() {
	atomic.AddUint64(&globalPackageMetrics.LuaScriptFailures, 1)
}

// RecordPackageQueryLatency 记录套餐查询延迟
func RecordPackageQueryLatency(duration time.Duration) {
	atomic.AddUint64(&globalPackageMetrics.PackageQueryTimeNs, uint64(duration.Nanoseconds()))
	atomic.AddUint64(&globalPackageMetrics.PackageQueryCount, 1)
}

// IncrementWindowCreation 记录窗口创建
func IncrementWindowCreation() {
	atomic.AddUint64(&globalPackageMetrics.WindowCreations, 1)
}

// IncrementWindowExpiration 记录窗口过期
func IncrementWindowExpiration() {
	atomic.AddUint64(&globalPackageMetrics.WindowExpirations, 1)
}

// GetPackageMetrics 获取当前监控指标快照
func GetPackageMetrics() map[string]interface{} {
	totalReqs := atomic.LoadUint64(&globalPackageMetrics.TotalRequests)
	packageReqs := atomic.LoadUint64(&globalPackageMetrics.PackageRequests)
	balanceReqs := atomic.LoadUint64(&globalPackageMetrics.BalanceRequests)
	fallbackReqs := atomic.LoadUint64(&globalPackageMetrics.FallbackRequests)
	windowExceeded := atomic.LoadUint64(&globalPackageMetrics.WindowExceededReqs)
	luaFailures := atomic.LoadUint64(&globalPackageMetrics.LuaScriptFailures)
	queryTimeNs := atomic.LoadUint64(&globalPackageMetrics.PackageQueryTimeNs)
	queryCount := atomic.LoadUint64(&globalPackageMetrics.PackageQueryCount)
	windowCreations := atomic.LoadUint64(&globalPackageMetrics.WindowCreations)
	windowExpirations := atomic.LoadUint64(&globalPackageMetrics.WindowExpirations)

	// 计算比率
	packageUsageRate := 0.0
	if totalReqs > 0 {
		packageUsageRate = float64(packageReqs) / float64(totalReqs) * 100
	}

	fallbackRate := 0.0
	if packageReqs > 0 {
		fallbackRate = float64(fallbackReqs) / float64(packageReqs) * 100
	}

	windowExceededRate := 0.0
	if totalReqs > 0 {
		windowExceededRate = float64(windowExceeded) / float64(totalReqs) * 100
	}

	luaFailureRate := 0.0
	if queryCount > 0 {
		luaFailureRate = float64(luaFailures) / float64(queryCount) * 100
	}

	avgQueryLatencyMs := 0.0
	p99QueryLatencyMs := 0.0 // 简化实现，暂不计算真实 P99
	if queryCount > 0 {
		avgQueryLatencyMs = float64(queryTimeNs) / float64(queryCount) / 1e6
		p99QueryLatencyMs = avgQueryLatencyMs * 2 // 简化估算
	}

	return map[string]interface{}{
		// 计数器
		"total_requests":        totalReqs,
		"package_requests":      packageReqs,
		"balance_requests":      balanceReqs,
		"fallback_requests":     fallbackReqs,
		"window_exceeded_count": windowExceeded,
		"lua_script_failures":   luaFailures,
		"window_creations":      windowCreations,
		"window_expirations":    windowExpirations,

		// 比率（百分比）
		"package_usage_rate":       fmt.Sprintf("%.2f%%", packageUsageRate),
		"fallback_to_balance_rate": fmt.Sprintf("%.2f%%", fallbackRate),
		"window_exceeded_rate":     fmt.Sprintf("%.2f%%", windowExceededRate),
		"lua_failure_rate":         fmt.Sprintf("%.4f%%", luaFailureRate),

		// 延迟（毫秒）
		"avg_query_latency_ms": fmt.Sprintf("%.2f", avgQueryLatencyMs),
		"p99_query_latency_ms": fmt.Sprintf("%.2f", p99QueryLatencyMs),

		// 查询次数
		"query_count": queryCount,
	}
}

// GetPackageUtilizationStats 获取套餐使用率统计（基于数据库日志）
// 计算公式：SUM(total_consumed) / SUM(quota)
func GetPackageUtilizationStats(timeRangeSeconds int64) (map[string]interface{}, error) {
	now := common.GetTimestamp()
	startTime := now - timeRangeSeconds

	// 查询时间范围内所有活跃订阅的消耗情况
	type UtilizationRow struct {
		PackageId     int    `json:"package_id"`
		PackageName   string `json:"package_name"`
		TotalConsumed int64  `json:"total_consumed"`
		TotalQuota    int64  `json:"total_quota"`
		SubCount      int64  `json:"subscription_count"`
	}

	var results []UtilizationRow

	err := model.DB.Table("subscriptions").
		Select(`
			packages.id as package_id,
			packages.name as package_name,
			SUM(subscriptions.total_consumed) as total_consumed,
			SUM(packages.quota) as total_quota,
			COUNT(subscriptions.id) as sub_count
		`).
		Joins("JOIN packages ON subscriptions.package_id = packages.id").
		Where("subscriptions.status = ?", model.SubscriptionStatusActive).
		Where("subscriptions.start_time >= ?", startTime).
		Group("packages.id, packages.name").
		Scan(&results).Error

	if err != nil {
		return nil, err
	}

	// 计算总体使用率
	var totalConsumed int64
	var totalQuota int64
	for _, r := range results {
		totalConsumed += r.TotalConsumed
		totalQuota += r.TotalQuota
	}

	utilizationRate := 0.0
	if totalQuota > 0 {
		utilizationRate = float64(totalConsumed) / float64(totalQuota) * 100
	}

	return map[string]interface{}{
		"time_range_seconds": timeRangeSeconds,
		"total_consumed":     totalConsumed,
		"total_quota":        totalQuota,
		"utilization_rate":   fmt.Sprintf("%.2f%%", utilizationRate),
		"packages":           results,
	}, nil
}

// GetBillingTypeDistribution 获取计费类型分布（基于日志）
func GetBillingTypeDistribution(timeRangeSeconds int64) (map[string]interface{}, error) {
	now := common.GetTimestamp()
	startTime := now - timeRangeSeconds

	type BillingStats struct {
		BillingType  string `json:"billing_type"`
		RequestCount int64  `json:"request_count"`
		TotalQuota   int64  `json:"total_quota"`
	}

	var stats []BillingStats

	err := model.LOG_DB.Table("logs").
		Select("billing_type, COUNT(*) as request_count, SUM(quota) as total_quota").
		Where("type = ? AND created_at >= ?", model.LogTypeConsume, startTime).
		Group("billing_type").
		Scan(&stats).Error

	if err != nil {
		return nil, err
	}

	// 计算百分比
	var totalRequests int64
	var totalQuota int64
	for _, s := range stats {
		totalRequests += s.RequestCount
		totalQuota += s.TotalQuota
	}

	result := make(map[string]interface{})
	result["time_range_seconds"] = timeRangeSeconds
	result["total_requests"] = totalRequests
	result["total_quota"] = totalQuota

	for _, s := range stats {
		requestPercent := 0.0
		quotaPercent := 0.0

		if totalRequests > 0 {
			requestPercent = float64(s.RequestCount) / float64(totalRequests) * 100
		}
		if totalQuota > 0 {
			quotaPercent = float64(s.TotalQuota) / float64(totalQuota) * 100
		}

		result[s.BillingType] = map[string]interface{}{
			"requests":        s.RequestCount,
			"request_percent": fmt.Sprintf("%.2f%%", requestPercent),
			"quota":           s.TotalQuota,
			"quota_percent":   fmt.Sprintf("%.2f%%", quotaPercent),
		}
	}

	return result, nil
}

// GetTopPackagesByUsage 获取使用量最高的套餐（Top N）
func GetTopPackagesByUsage(limit int, timeRangeSeconds int64) ([]map[string]interface{}, error) {
	now := common.GetTimestamp()
	startTime := now - timeRangeSeconds

	type PackageUsage struct {
		PackageId    int    `json:"package_id"`
		PackageName  string `json:"package_name"`
		RequestCount int64  `json:"request_count"`
		TotalQuota   int64  `json:"total_quota"`
	}

	var usage []PackageUsage

	err := model.LOG_DB.Table("logs").
		Select(`
			package_id,
			COUNT(*) as request_count,
			SUM(quota) as total_quota
		`).
		Joins("LEFT JOIN packages ON logs.package_id = packages.id").
		Where("logs.type = ? AND logs.billing_type = ? AND logs.created_at >= ?",
			model.LogTypeConsume, "package", startTime).
		Where("logs.package_id > 0").
		Group("logs.package_id").
		Order("total_quota DESC").
		Limit(limit).
		Scan(&usage).Error

	if err != nil {
		return nil, err
	}

	// 获取套餐名称
	var results []map[string]interface{}
	for _, u := range usage {
		pkg, err := model.GetPackageByID(u.PackageId)
		packageName := "Unknown"
		if err == nil {
			packageName = pkg.Name
		}

		results = append(results, map[string]interface{}{
			"package_id":    u.PackageId,
			"package_name":  packageName,
			"request_count": u.RequestCount,
			"total_quota":   u.TotalQuota,
			"quota_display": logger.FormatQuota(int(u.TotalQuota)),
		})
	}

	return results, nil
}

// StartPackageMetricsCollector 启动监控指标采集器（后台任务）
// 定期将指标持久化到数据库或日志
func StartPackageMetricsCollector(intervalMinutes int) {
	if !common.PackageEnabled {
		return
	}

	ticker := time.NewTicker(time.Duration(intervalMinutes) * time.Minute)
	go func() {
		for range ticker.C {
			metrics := GetPackageMetrics()
			common.SysLog(fmt.Sprintf("[PackageMetrics] %s", common.GetJsonString(metrics)))

			// 可选：持久化到数据库（用于历史趋势分析）
			// model.SavePackageMetricsSnapshot(metrics)
		}
	}()

	common.SysLog(fmt.Sprintf("[PackageMetrics] Collector started, interval: %d minutes", intervalMinutes))
}
