package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ChannelStatistics 渠道统计时序表
// 用于持久化渠道在每个统计周期的性能快照，作为长期趋势分析的数据源
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: 6.1 channel_statistics (渠道统计时序表)
type ChannelStatistics struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId       int    `json:"channel_id" gorm:"not null;index:idx_channel_model_time"`
	ModelName       string `json:"model_name" gorm:"type:varchar(255);not null;index:idx_channel_model_time"`
	TimeWindowStart int64  `json:"time_window_start" gorm:"not null;index:idx_channel_model_time;comment:统计窗口起始时间戳"`
	RequestCount    int    `json:"request_count" gorm:"default:0;comment:总请求数"`
	FailCount       int    `json:"fail_count" gorm:"default:0;comment:失败请求数"`
	TotalTokens     int64  `json:"total_tokens" gorm:"default:0;comment:总Token数"`
	TotalQuota      int64  `json:"total_quota" gorm:"default:0;comment:总额度消耗"`
	TotalLatencyMs  int64  `json:"total_latency_ms" gorm:"default:0;comment:总首字延迟(ms)"`
	StreamReqCount  int    `json:"stream_req_count" gorm:"default:0;comment:流式请求数"`
	CacheHitCount   int    `json:"cache_hit_count" gorm:"default:0;comment:缓存命中数"`
	DowntimeSeconds int    `json:"downtime_seconds" gorm:"default:0;comment:禁用时长(秒)"`
	UniqueUsers     int    `json:"unique_users" gorm:"default:0;comment:区间服务用户数(去重)"` // Phase 10.4: GS4-1 去重用户统计
	CreatedAt       int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt       int64  `json:"updated_at" gorm:"bigint"`
}

// TableName specifies the table name for GORM
func (ChannelStatistics) TableName() string {
	return "channel_statistics"
}

// BeforeCreate GORM hook: set CreatedAt and UpdatedAt timestamps
func (cs *ChannelStatistics) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if cs.CreatedAt == 0 {
		cs.CreatedAt = now
	}
	if cs.UpdatedAt == 0 {
		cs.UpdatedAt = now
	}
	return nil
}

// BeforeUpdate GORM hook: update UpdatedAt timestamp
func (cs *ChannelStatistics) BeforeUpdate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	// Ensure UpdatedAt is strictly increasing even under sub-second updates.
	if now <= cs.UpdatedAt {
		now = cs.UpdatedAt + 1
	}
	cs.UpdatedAt = now
	return nil
}

// GetChannelStatistics 根据渠道ID、模型和时间范围查询统计数据
// startTime, endTime: Unix timestamp
func GetChannelStatistics(channelId int, modelName string, startTime, endTime int64) ([]*ChannelStatistics, error) {
	var stats []*ChannelStatistics
	query := DB.Where("channel_id = ?", channelId)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	if startTime > 0 {
		query = query.Where("time_window_start >= ?", startTime)
	}

	if endTime > 0 {
		query = query.Where("time_window_start <= ?", endTime)
	}

	err := query.Order("time_window_start DESC").Find(&stats).Error
	return stats, err
}

// UpsertChannelStatistics 插入或更新统计数据（基于channel_id, model_name, time_window_start唯一性）
func UpsertChannelStatistics(stat *ChannelStatistics) error {
	// 使用 GORM 的 Clauses 来执行 UPSERT
	// 对于 MySQL: INSERT ... ON DUPLICATE KEY UPDATE
	// 对于 PostgreSQL: INSERT ... ON CONFLICT ... DO UPDATE
	// 对于 SQLite: INSERT OR REPLACE

	if common.UsingMySQL || common.UsingPostgreSQL {
		// 先尝试查找现有记录
		var existing ChannelStatistics
		err := DB.Where("channel_id = ? AND model_name = ? AND time_window_start = ?",
			stat.ChannelId, stat.ModelName, stat.TimeWindowStart).
			First(&existing).Error

		if err == gorm.ErrRecordNotFound {
			// 记录不存在，执行插入
			return DB.Create(stat).Error
		} else if err != nil {
			return err
		}

		// 记录存在，执行更新（保持 CreatedAt 单调不变、UpdatedAt 单调递增）
		stat.Id = existing.Id               // 保留原有ID
		stat.CreatedAt = existing.CreatedAt // 保留创建时间
		now := common.GetTimestamp()
		if now <= existing.UpdatedAt {
			now = existing.UpdatedAt + 1
		}
		stat.UpdatedAt = now

		if err := DB.Save(stat).Error; err != nil {
			return err
		}

		// 回读一次，确保调用方拿到最终的 UpdatedAt（包含 BeforeUpdate 钩子可能的再次调整）。
		var latest ChannelStatistics
		if err := DB.Where("channel_id = ? AND model_name = ? AND time_window_start = ?",
			stat.ChannelId, stat.ModelName, stat.TimeWindowStart).
			First(&latest).Error; err == nil {
			stat.UpdatedAt = latest.UpdatedAt
		}
		return nil
	}

	// SQLite: 无唯一约束时 Save 会插入重复行，需要显式按唯一键查找再更新，
	// 并确保 UpdatedAt 在快速连续更新时仍然单调递增。
	var existing ChannelStatistics
	err := DB.Where("channel_id = ? AND model_name = ? AND time_window_start = ?",
		stat.ChannelId, stat.ModelName, stat.TimeWindowStart).
		First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		return DB.Create(stat).Error
	} else if err != nil {
		return err
	}

	stat.Id = existing.Id
	stat.CreatedAt = existing.CreatedAt
	now := common.GetTimestamp()
	if now <= existing.UpdatedAt {
		now = existing.UpdatedAt + 1
	}
	stat.UpdatedAt = now

	// 使用显式 Updates 保证 updated_at 被正确写入，即使 Hook 行为因环境差异有所不同。
	updates := map[string]interface{}{
		"request_count":    stat.RequestCount,
		"fail_count":       stat.FailCount,
		"total_tokens":     stat.TotalTokens,
		"total_quota":      stat.TotalQuota,
		"total_latency_ms": stat.TotalLatencyMs,
		"stream_req_count": stat.StreamReqCount,
		"cache_hit_count":  stat.CacheHitCount,
		"downtime_seconds": stat.DowntimeSeconds,
		"unique_users":     stat.UniqueUsers,
		"updated_at":       now,
	}

	if err := DB.Model(&ChannelStatistics{}).
		Where("channel_id = ? AND model_name = ? AND time_window_start = ?",
			stat.ChannelId, stat.ModelName, stat.TimeWindowStart).
		Updates(updates).Error; err != nil {
		return err
	}

	return nil
}

// BatchUpsertChannelStatistics 批量插入或更新统计数据
func BatchUpsertChannelStatistics(stats []*ChannelStatistics) error {
	if len(stats) == 0 {
		return nil
	}

	// 使用事务批量处理
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, stat := range stats {
		if err := UpsertChannelStatistics(stat); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to upsert channel statistics: %w", err)
		}
	}

	return tx.Commit().Error
}

// DeleteChannelStatisticsBefore 删除指定时间之前的统计数据（用于数据清理）
func DeleteChannelStatisticsBefore(beforeTime int64) (int64, error) {
	result := DB.Where("time_window_start < ?", beforeTime).Delete(&ChannelStatistics{})
	return result.RowsAffected, result.Error
}

// AggregateChannelStatistics 聚合查询统计数据（用于生成汇总报告以及系统分组统计）
type AggregatedStats struct {
	ChannelId         int     `json:"channel_id"`
	ModelName         string  `json:"model_name,omitempty"`
	RequestCount      int64   `json:"request_count"`        // 聚合请求数
	FailCount         int64   `json:"fail_count"`           // 聚合失败数
	FailRate          float64 `json:"fail_rate"`            // 失败率 (%)
	TotalTokens       int64   `json:"total_tokens"`         // 总 tokens
	TotalQuota        int64   `json:"total_quota"`          // 总额度
	AvgResponseTimeMs float64 `json:"avg_response_time_ms"` // 平均首字延迟(ms)
	CacheHitRate      float64 `json:"cache_hit_rate"`       // 缓存命中率(%)
	StreamReqRatio    float64 `json:"stream_req_ratio"`     // 流式请求占比(%)

	// 扩展字段：供系统分组统计与看板使用
	DowntimeSeconds    int64   `json:"downtime_seconds"`    // 统计区间内停服总时长(秒)
	TPM                int     `json:"tpm"`                 // Tokens per minute
	RPM                int     `json:"rpm"`                 // Requests per minute
	QuotaPM            int64   `json:"quota_pm"`            // Quota per minute
	DowntimePercentage float64 `json:"downtime_percentage"` // 停服时间占比(%)
	TotalSessions      int64   `json:"total_sessions"`      // 会话数 (预留, 当前未在查询中赋值)
	UniqueUsers        int     `json:"unique_users"`        // 去重用户数 (仅在部分查询中赋值)
}

// AggregateChannelStatisticsByTime 按时间范围聚合统计数据
func AggregateChannelStatisticsByTime(channelId int, modelName string, startTime, endTime int64) (*AggregatedStats, error) {
	query := DB.Model(&ChannelStatistics{}).
		Select(`
			channel_id,
			? as model_name,
			COALESCE(SUM(request_count), 0) as request_count,
			COALESCE(SUM(fail_count), 0) as fail_count,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(fail_count) * 100.0 / SUM(request_count))
				ELSE 0
			END as fail_rate,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(SUM(total_quota), 0) as total_quota,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(total_latency_ms) * 1.0 / SUM(request_count))
				ELSE 0
			END as avg_response_time_ms,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(cache_hit_count) * 100.0 / SUM(request_count))
				ELSE 0
			END as cache_hit_rate,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(stream_req_count) * 100.0 / SUM(request_count))
				ELSE 0
			END as stream_req_ratio
		`, modelName).
		Where("channel_id = ?", channelId)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	if startTime > 0 {
		query = query.Where("time_window_start >= ?", startTime)
	}

	if endTime > 0 {
		query = query.Where("time_window_start <= ?", endTime)
	}

	var result AggregatedStats
	err := query.Group("channel_id").Scan(&result).Error
	if err != nil {
		return nil, err
	}

	result.ChannelId = channelId
	if modelName != "" {
		result.ModelName = modelName
	}

	return &result, nil
}

// CountChannelStatistics 统计记录总数（用于监控和调试）
func CountChannelStatistics() (int64, error) {
	var count int64
	err := DB.Model(&ChannelStatistics{}).Count(&count).Error
	return count, err
}

// AggregateChannelStatsByUserGroup 按用户系统分组聚合渠道统计
// 聚合所有属于指定系统分组（User.Group）的用户创建的渠道的统计数据
// 用于系统分组性能对比功能
func AggregateChannelStatsByUserGroup(userGroup string, startTime, endTime int64) (*AggregatedStats, error) {
	// 1. 查询属于该系统分组的所有渠道ID
	var channelIds []int
	userGroupCol := "users." + commonGroupCol
	err := DB.Table("channels").
		Select("channels.id").
		Joins("LEFT JOIN users ON channels.owner_user_id = users.id").
		Where(userGroupCol+" = ?", userGroup).
		Pluck("channels.id", &channelIds).Error

	if err != nil {
		return nil, fmt.Errorf("查询系统分组渠道失败: %w", err)
	}

	if len(channelIds) == 0 {
		// 该系统分组下没有渠道，返回空统计
		return &AggregatedStats{
			ModelName: "", // 不区分模型，返回总体统计
		}, nil
	}

	// 2. 聚合这些渠道的统计数据
	query := DB.Table("channel_statistics").
		Select(`
			SUM(request_count) as request_count,
			SUM(fail_count) as fail_count,
			SUM(total_tokens) as total_tokens,
			SUM(total_quota) as total_quota,
			SUM(total_latency_ms) as total_latency_ms,
			SUM(stream_req_count) as stream_req_count,
			SUM(cache_hit_count) as cache_hit_count,
			SUM(downtime_seconds) as downtime_seconds,
			SUM(unique_users) as unique_users,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(fail_count) * 100.0 / SUM(request_count))
				ELSE 0
			END as fail_rate,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(total_latency_ms) / SUM(request_count))
				ELSE 0
			END as avg_response_time_ms,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(cache_hit_count) * 100.0 / SUM(request_count))
				ELSE 0
			END as cache_hit_rate,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(stream_req_count) * 100.0 / SUM(request_count))
				ELSE 0
			END as stream_req_ratio
		`).
		Where("channel_id IN ?", channelIds)

	if startTime > 0 {
		query = query.Where("time_window_start >= ?", startTime)
	}

	if endTime > 0 {
		query = query.Where("time_window_start <= ?", endTime)
	}

	var result AggregatedStats
	err = query.Scan(&result).Error
	if err != nil {
		return nil, fmt.Errorf("聚合系统分组统计失败: %w", err)
	}

	// 3. 计算时间范围（分钟数）
	timeRangeMinutes := float64(endTime-startTime) / 60.0
	if timeRangeMinutes <= 0 {
		timeRangeMinutes = 1.0
	}

	// 4. 计算 TPM、RPM、QuotaPM
	result.TPM = int(float64(result.TotalTokens) / timeRangeMinutes)
	result.RPM = int(float64(result.RequestCount) / timeRangeMinutes)
	result.QuotaPM = int64(float64(result.TotalQuota) / timeRangeMinutes)

	// 5. 计算停服时间占比
	totalSeconds := endTime - startTime
	if totalSeconds > 0 {
		result.DowntimePercentage = float64(result.DowntimeSeconds) * 100.0 / float64(totalSeconds)
	}

	return &result, nil
}

// ========== Global System Statistics Operations ==========
// 全局系统统计操作
// 设计文档: docs/系统统计数据dashboard设计.md Section 7

// DailyTokenUsage 日均 Token 使用量结构
// 用于全局/分组/系统分组的日均消耗曲线
type DailyTokenUsage struct {
	Day    string `json:"day"`    // YYYY-MM-DD
	Tokens int64  `json:"tokens"` // 当天总 Token 数
	Quota  int64  `json:"quota"`  // 当天总额度消耗
}

// GlobalModelStats 系统级按模型聚合统计结构
// 用于 /api/system/stats/models 接口返回
type GlobalModelStats struct {
	ModelName         string  `json:"model_name"`
	TotalTokens       int64   `json:"total_tokens"`
	TotalQuota        int64   `json:"total_quota"`
	RequestCount      int64   `json:"request_count"`
	TPM               int     `json:"tpm"`
	RPM               int     `json:"rpm"`
	AvgResponseTimeMs float64 `json:"avg_response_time_ms"`
	FailRate          float64 `json:"fail_rate"`
}

// BillingGroupModelStats 计费分组（系统分组）按模型聚合统计结构
// 用于 /api/groups/system/model_stats 接口返回
type BillingGroupModelStats struct {
	UserGroup         string  `json:"user_group"`
	ModelName         string  `json:"model_name"`
	TotalTokens       int64   `json:"total_tokens"`
	TotalQuota        int64   `json:"total_quota"`
	TPM               int     `json:"tpm"`
	RPM               int     `json:"rpm"`
	AvgResponseTimeMs float64 `json:"avg_response_time_ms"`
	FailRate          float64 `json:"fail_rate"`
}

// AggregateGlobalChannelStatsByTime 聚合全局（所有渠道）在指定时间范围内的统计
//
// 用途：为系统级统计提供汇总指标，支持管理员查看整个 NewAPI 实例的全局性能
//
// 参数：
//   - startTime: 起始时间戳（Unix）
//   - endTime: 结束时间戳（Unix）
//
// 返回：
//   - AggregatedStats: 包含全局的 TPM/RPM/FailRate/AvgLatency 等指标
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 7.1
func AggregateGlobalChannelStatsByTime(startTime, endTime int64) (*AggregatedStats, error) {
	// 聚合所有渠道的统计数据（不加 channel_id 过滤）
	query := DB.Table("channel_statistics").
		Select(`
			SUM(request_count) as request_count,
			SUM(fail_count) as fail_count,
			SUM(total_tokens) as total_tokens,
			SUM(total_quota) as total_quota,
			SUM(total_latency_ms) as total_latency_ms,
			SUM(stream_req_count) as stream_req_count,
			SUM(cache_hit_count) as cache_hit_count,
			SUM(downtime_seconds) as downtime_seconds,
			SUM(unique_users) as unique_users,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(fail_count) * 100.0 / SUM(request_count))
				ELSE 0
			END as fail_rate,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(total_latency_ms) / SUM(request_count))
				ELSE 0
			END as avg_response_time_ms,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(cache_hit_count) * 100.0 / SUM(request_count))
				ELSE 0
			END as cache_hit_rate,
			CASE
				WHEN SUM(request_count) > 0 THEN (SUM(stream_req_count) * 100.0 / SUM(request_count))
				ELSE 0
			END as stream_req_ratio
		`)

	if startTime > 0 {
		query = query.Where("time_window_start >= ?", startTime)
	}

	if endTime > 0 {
		query = query.Where("time_window_start <= ?", endTime)
	}

	var result AggregatedStats
	err := query.Scan(&result).Error
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate global channel statistics: %w", err)
	}

	// 计算时间范围（分钟数）
	timeRangeMinutes := float64(endTime-startTime) / 60.0
	if timeRangeMinutes <= 0 {
		timeRangeMinutes = 1.0
	}

	// 计算 TPM、RPM、QuotaPM
	result.TPM = int(float64(result.TotalTokens) / timeRangeMinutes)
	result.RPM = int(float64(result.RequestCount) / timeRangeMinutes)
	result.QuotaPM = int64(float64(result.TotalQuota) / timeRangeMinutes)

	// 计算停服时间占比
	totalSeconds := endTime - startTime
	if totalSeconds > 0 {
		result.DowntimePercentage = float64(result.DowntimeSeconds) * 100.0 / float64(totalSeconds)
	}

	return &result, nil
}

// AggregateGlobalModelStats 按模型聚合全局统计数据
//
// 用途：为系统级按模型统计提供汇总指标（Token/Quota/TPM/RPM/延迟/失败率）
//
// 参数：
//   - startTime: 起始时间戳
//   - endTime: 结束时间戳
//   - modelName: 可选，指定模型名，空字符串表示聚合所有模型并按模型分组
//
// 返回：
//   - []GlobalModelStats: 每个模型一条记录
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 10.2.1 / 11.1.3
func AggregateGlobalModelStats(startTime, endTime int64, modelName string) ([]GlobalModelStats, error) {
	if endTime <= startTime {
		return []GlobalModelStats{}, nil
	}

	// 基础查询：按模型聚合 Token / Quota / 请求数 / 失败数 / 延迟和
	type rawModelAgg struct {
		ModelName      string
		TotalTokens    int64
		TotalQuota     int64
		RequestCount   int64
		FailCount      int64
		TotalLatencyMs int64
	}

	query := DB.Table("channel_statistics").
		Select(`
			model_name,
			SUM(total_tokens)       AS total_tokens,
			SUM(total_quota)        AS total_quota,
			SUM(request_count)      AS request_count,
			SUM(fail_count)         AS fail_count,
			SUM(total_latency_ms)   AS total_latency_ms
		`).
		Where("time_window_start BETWEEN ? AND ?", startTime, endTime)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	var rawResults []rawModelAgg
	if err := query.Group("model_name").Scan(&rawResults).Error; err != nil {
		return nil, fmt.Errorf("failed to aggregate global model stats: %w", err)
	}

	if len(rawResults) == 0 {
		return []GlobalModelStats{}, nil
	}

	minutes := float64(endTime-startTime) / 60.0
	if minutes <= 0 {
		minutes = 1.0
	}

	results := make([]GlobalModelStats, 0, len(rawResults))
	for _, r := range rawResults {
		stat := GlobalModelStats{
			ModelName:    r.ModelName,
			TotalTokens:  r.TotalTokens,
			TotalQuota:   r.TotalQuota,
			RequestCount: r.RequestCount,
		}

		// TPM/RPM
		stat.TPM = int(float64(r.TotalTokens) / minutes)
		stat.RPM = int(float64(r.RequestCount) / minutes)

		// 平均延迟与失败率
		if r.RequestCount > 0 {
			stat.AvgResponseTimeMs = float64(r.TotalLatencyMs) / float64(r.RequestCount)
			stat.FailRate = float64(r.FailCount) * 100.0 / float64(r.RequestCount)
		}

		results = append(results, stat)
	}

	return results, nil
}

// GetGlobalDailyTokenUsage 获取全局按日聚合的 Token/Quota 消耗曲线
//
// 用途：为系统级统计提供日均消耗趋势图
//
// 参数：
//   - days: 向前多少天，默认 30，最大 90
//
// 返回：
//   - []DailyTokenUsage: 按日聚合的 Token/Quota 数据列表
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 7.2
func GetGlobalDailyTokenUsage(days int) ([]DailyTokenUsage, error) {
	if days <= 0 {
		days = 30
	}
	if days > 90 {
		days = 90
	}

	now := common.GetTimestamp()
	startTime := now - int64(days*24*60*60)

	// 按自然日聚合全局的 Token 和 Quota。
	// 为兼容 MySQL 与 SQLite，这里根据实际使用的数据库类型选择不同的日期表达式：
	//   - MySQL:   DATE(FROM_UNIXTIME(time_window_start))
	//   - SQLite:  DATE(datetime(time_window_start, 'unixepoch'))
	dateExpr := "DATE(FROM_UNIXTIME(time_window_start))"
	if common.UsingSQLite {
		dateExpr = "DATE(datetime(time_window_start, 'unixepoch'))"
	}

	selectExpr := fmt.Sprintf(`
		%s AS day,
		SUM(total_tokens) AS tokens,
		SUM(total_quota) AS quota
	`, dateExpr)

	var results []DailyTokenUsage
	err := DB.Table("channel_statistics").
		Select(selectExpr).
		Where("time_window_start >= ?", startTime).
		Group("day").
		Order("day ASC").
		Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get global daily token usage: %w", err)
	}

	return results, nil
}

// GlobalModelDailyUsage 系统级按模型每日使用量结构
// 用于 /api/system/stats/models/daily_tokens 接口返回
type GlobalModelDailyUsage struct {
	Day       string `json:"day"`        // YYYY-MM-DD
	ModelName string `json:"model_name"` // 模型名称
	Tokens    int64  `json:"tokens"`     // 当日 Token 数
	Quota     int64  `json:"quota"`      // 当日额度消耗
}

// GetGlobalModelDailyUsage 获取系统级按模型的每日 Token/Quota 消耗曲线
//
// 参数：
//   - days: 向前多少天（默认 30，最大 90）
//   - modelName: 可选，指定模型名，为空则返回所有模型
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 10.2.2 / 11.1.4
func GetGlobalModelDailyUsage(days int, modelName string) ([]GlobalModelDailyUsage, error) {
	if days <= 0 {
		days = 30
	}
	if days > 90 {
		days = 90
	}

	now := common.GetTimestamp()
	startTime := now - int64(days*24*60*60)

	dateExpr := "DATE(FROM_UNIXTIME(time_window_start))"
	if common.UsingSQLite {
		dateExpr = "DATE(datetime(time_window_start, 'unixepoch'))"
	}

	query := DB.Table("channel_statistics").
		Select(fmt.Sprintf(`
			%s AS day,
			model_name,
			SUM(total_tokens) AS tokens,
			SUM(total_quota)  AS quota
		`, dateExpr)).
		Where("time_window_start >= ?", startTime)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	var results []GlobalModelDailyUsage
	if err := query.
		Group("day, model_name").
		Order("day ASC, model_name ASC").
		Scan(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to get global model daily usage: %w", err)
	}

	return results, nil
}

// BillingGroupModelDailyUsage 系统分组按模型每日使用量结构
// 用于计费分组按模型每日消耗曲线
type BillingGroupModelDailyUsage struct {
	UserGroup string `json:"user_group"`
	Day       string `json:"day"`
	ModelName string `json:"model_name"`
	Tokens    int64  `json:"tokens"`
	Quota     int64  `json:"quota"`
}

// AggregateBillingGroupModelStats 按系统分组 + 模型聚合统计数据
//
// 用途：为计费分组（系统分组）按模型维度提供汇总视图
//
// 参数：
//   - userGroup: 系统分组名（如 default/vip/svip）
//   - startTime, endTime: 时间范围
//   - modelName: 可选，指定模型名
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 10.4.1 / 11.2.2
func AggregateBillingGroupModelStats(userGroup string, startTime, endTime int64, modelName string) ([]BillingGroupModelStats, error) {
	if endTime <= startTime {
		return []BillingGroupModelStats{}, nil
	}

	// 1. 查询属于该系统分组的所有渠道 ID
	var channelIds []int
	userGroupCol := "users." + commonGroupCol
	err := DB.Table("channels").
		Select("channels.id").
		Joins("LEFT JOIN users ON channels.owner_user_id = users.id").
		Where(userGroupCol+" = ?", userGroup).
		Pluck("channels.id", &channelIds).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query channels for billing group %s: %w", userGroup, err)
	}
	if len(channelIds) == 0 {
		return []BillingGroupModelStats{}, nil
	}

	// 2. 按模型维度聚合 channel_statistics
	type rawAgg struct {
		ModelName      string
		TotalTokens    int64
		TotalQuota     int64
		RequestCount   int64
		FailCount      int64
		TotalLatencyMs int64
	}

	query := DB.Table("channel_statistics").
		Select(`
			model_name,
			SUM(total_tokens)       AS total_tokens,
			SUM(total_quota)        AS total_quota,
			SUM(request_count)      AS request_count,
			SUM(fail_count)         AS fail_count,
			SUM(total_latency_ms)   AS total_latency_ms
		`).
		Where("channel_id IN ?", channelIds).
		Where("time_window_start BETWEEN ? AND ?", startTime, endTime)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	var raws []rawAgg
	if err := query.Group("model_name").Scan(&raws).Error; err != nil {
		return nil, fmt.Errorf("failed to aggregate billing group model stats: %w", err)
	}
	if len(raws) == 0 {
		return []BillingGroupModelStats{}, nil
	}

	minutes := float64(endTime-startTime) / 60.0
	if minutes <= 0 {
		minutes = 1.0
	}

	results := make([]BillingGroupModelStats, 0, len(raws))
	for _, r := range raws {
		stat := BillingGroupModelStats{
			UserGroup:   userGroup,
			ModelName:   r.ModelName,
			TotalTokens: r.TotalTokens,
			TotalQuota:  r.TotalQuota,
		}
		stat.TPM = int(float64(r.TotalTokens) / minutes)
		stat.RPM = int(float64(r.RequestCount) / minutes)
		if r.RequestCount > 0 {
			stat.AvgResponseTimeMs = float64(r.TotalLatencyMs) / float64(r.RequestCount)
			stat.FailRate = float64(r.FailCount) * 100.0 / float64(r.RequestCount)
		}
		results = append(results, stat)
	}

	return results, nil
}

// GetBillingGroupModelDailyUsage 获取系统分组按模型的每日 Token/Quota 消耗曲线
//
// 参数：
//   - userGroup: 系统分组名
//   - days: 向前多少天（默认 30，最大 90）
//   - modelName: 可选，指定模型名
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 10.4.2 / 11.2.3
func GetBillingGroupModelDailyUsage(userGroup string, days int, modelName string) ([]BillingGroupModelDailyUsage, error) {
	if days <= 0 {
		days = 30
	}
	if days > 90 {
		days = 90
	}

	// 1. 获取该系统分组的渠道 ID
	var channelIds []int
	userGroupCol := "users." + commonGroupCol
	err := DB.Table("channels").
		Select("channels.id").
		Joins("LEFT JOIN users ON channels.owner_user_id = users.id").
		Where(userGroupCol+" = ?", userGroup).
		Pluck("channels.id", &channelIds).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query channels for billing group %s: %w", userGroup, err)
	}
	if len(channelIds) == 0 {
		return []BillingGroupModelDailyUsage{}, nil
	}

	now := common.GetTimestamp()
	startTime := now - int64(days*24*60*60)

	dateExpr := "DATE(FROM_UNIXTIME(time_window_start))"
	if common.UsingSQLite {
		dateExpr = "DATE(datetime(time_window_start, 'unixepoch'))"
	}

	query := DB.Table("channel_statistics").
		Select(fmt.Sprintf(`
			%s AS day,
			model_name,
			SUM(total_tokens) AS tokens,
			SUM(total_quota)  AS quota
		`, dateExpr)).
		Where("channel_id IN ?", channelIds).
		Where("time_window_start >= ?", startTime)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	var results []BillingGroupModelDailyUsage
	if err := query.
		Group("day, model_name").
		Order("day ASC, model_name ASC").
		Scan(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to get billing group model daily usage: %w", err)
	}

	for i := range results {
		results[i].UserGroup = userGroup
	}

	return results, nil
}
