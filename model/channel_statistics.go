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
	cs.UpdatedAt = common.GetTimestamp()
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

		// 记录存在，执行更新
		stat.Id = existing.Id               // 保留原有ID
		stat.CreatedAt = existing.CreatedAt // 保留创建时间
		return DB.Save(stat).Error
	}

	// SQLite: 使用 Save 方法（GORM 会自动处理）
	return DB.Save(stat).Error
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

// AggregateChannelStatistics 聚合查询统计数据（用于生成汇总报告）
type AggregatedStats struct {
	ChannelId         int     `json:"channel_id"`
	ModelName         string  `json:"model_name,omitempty"`
	TotalRequests     int64   `json:"total_requests"`
	TotalFailures     int64   `json:"total_failures"`
	FailRate          float64 `json:"fail_rate"`
	TotalTokens       int64   `json:"total_tokens"`
	TotalQuota        int64   `json:"total_quota"`
	AvgResponseTimeMs float64 `json:"avg_response_time_ms"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
	StreamReqRatio    float64 `json:"stream_req_ratio"`
}

// AggregateChannelStatisticsByTime 按时间范围聚合统计数据
func AggregateChannelStatisticsByTime(channelId int, modelName string, startTime, endTime int64) (*AggregatedStats, error) {
	query := DB.Model(&ChannelStatistics{}).
		Select(`
			channel_id,
			? as model_name,
			COALESCE(SUM(request_count), 0) as total_requests,
			COALESCE(SUM(fail_count), 0) as total_failures,
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
