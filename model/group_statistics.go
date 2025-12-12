package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// GroupStatistics P2P分组聚合统计表
// 用于存储P2P分组在每个统计周期的聚合性能数据
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: 6.2 group_statistics (分组聚合统计表)
type GroupStatistics struct {
	GroupId           int     `json:"group_id" gorm:"primaryKey;not null"`
	ModelName         string  `json:"model_name" gorm:"type:varchar(255);primaryKey;not null"`
	TimeWindowStart   int64   `json:"time_window_start" gorm:"primaryKey;not null;comment:统计窗口起始时间戳"`
	TPM               int     `json:"tpm" gorm:"default:0;comment:每分钟Token数"`
	RPM               int     `json:"rpm" gorm:"default:0;comment:每分钟请求数"`
	FailRate          float64 `json:"fail_rate" gorm:"type:double precision;default:0.0;comment:失败率(%)"`
	AvgResponseTimeMs int     `json:"avg_response_time_ms" gorm:"default:0;comment:平均响应时间(ms)"`
	// AvgResponseTime 为测试和API兼容提供的别名字段，不直接映射到数据库列。
	// 数据库仍使用 avg_response_time_ms 列存储毫秒级平均响应时间。
	AvgResponseTime    int     `json:"avg_response_time,omitempty" gorm:"-"`
	AvgCacheHitRate    float64 `json:"avg_cache_hit_rate" gorm:"type:double precision;default:0.0;comment:缓存命中率(%)"`
	StreamReqRatio     float64 `json:"stream_req_ratio" gorm:"type:double precision;default:0.0;comment:流式请求占比(%)"`
	QuotaPM            int64   `json:"quota_pm" gorm:"default:0;comment:每分钟消耗额度"`
	TotalTokens        int64   `json:"total_tokens" gorm:"default:0;comment:区间总Token数"`
	TotalQuota         int64   `json:"total_quota" gorm:"default:0;comment:区间总额度消耗"`
	AvgConcurrency     float64 `json:"avg_concurrency" gorm:"type:double precision;default:0.0;comment:平均并发数"`
	TotalSessions      int64   `json:"total_sessions" gorm:"default:0;comment:区间总会话数"`
	DowntimePercentage float64 `json:"downtime_percentage" gorm:"type:double precision;default:0.0;comment:停服时间占比(%)"`
	UniqueUsers        int     `json:"unique_users" gorm:"default:0;comment:区间服务用户数(去重)"`
	UpdatedAt          int64   `json:"updated_at" gorm:"bigint;not null"`
}

// TableName specifies the table name for GORM
func (GroupStatistics) TableName() string {
	return "group_statistics"
}

// BeforeCreate GORM hook: set UpdatedAt timestamp
func (gs *GroupStatistics) BeforeCreate(tx *gorm.DB) error {
	if gs.UpdatedAt == 0 {
		gs.UpdatedAt = common.GetTimestamp()
	}
	return nil
}

// BeforeUpdate GORM hook: update UpdatedAt timestamp
func (gs *GroupStatistics) BeforeUpdate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	// Ensure UpdatedAt is strictly increasing for rapid successive updates.
	if now <= gs.UpdatedAt {
		now = gs.UpdatedAt + 1
	}
	gs.UpdatedAt = now
	return nil
}

// ========== CRUD Operations ==========

// UpsertGroupStatistics 插入或更新分组统计数据
// 基于 (group_id, model_name, time_window_start) 唯一性
func UpsertGroupStatistics(stat *GroupStatistics) error {
	if common.UsingMySQL || common.UsingPostgreSQL {
		// 先尝试查找现有记录
		var existing GroupStatistics
		err := DB.Where("group_id = ? AND model_name = ? AND time_window_start = ?",
			stat.GroupId, stat.ModelName, stat.TimeWindowStart).
			First(&existing).Error

		if err == gorm.ErrRecordNotFound {
			// 记录不存在，执行插入
			return DB.Create(stat).Error
		} else if err != nil {
			return err
		}

		// 记录存在，执行更新
		now := common.GetTimestamp()
		if now <= existing.UpdatedAt {
			now = existing.UpdatedAt + 1
		}
		stat.UpdatedAt = now

		if err := DB.Model(&GroupStatistics{}).
			Where("group_id = ? AND model_name = ? AND time_window_start = ?",
				stat.GroupId, stat.ModelName, stat.TimeWindowStart).
			Updates(stat).Error; err != nil {
			return err
		}

		// Ensure the caller's struct observes the final UpdatedAt value after
		// GORM hooks, even when updates happen within the same second.
		var latest GroupStatistics
		if err := DB.Where("group_id = ? AND model_name = ? AND time_window_start = ?",
			stat.GroupId, stat.ModelName, stat.TimeWindowStart).
			First(&latest).Error; err == nil {
			stat.UpdatedAt = latest.UpdatedAt
		}
		return nil
	}

	// SQLite: 同样按唯一键查找以保证 UpdatedAt 单调递增。
	var existing GroupStatistics
	err := DB.Where("group_id = ? AND model_name = ? AND time_window_start = ?",
		stat.GroupId, stat.ModelName, stat.TimeWindowStart).
		First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		return DB.Create(stat).Error
	} else if err != nil {
		return err
	}

	stat.UpdatedAt = existing.UpdatedAt
	now := common.GetTimestamp()
	if now <= existing.UpdatedAt {
		now = existing.UpdatedAt + 1
	}
	stat.UpdatedAt = now

	updates := map[string]interface{}{
		"tpm":                  stat.TPM,
		"rpm":                  stat.RPM,
		"fail_rate":            stat.FailRate,
		"avg_response_time_ms": stat.AvgResponseTimeMs,
		"avg_cache_hit_rate":   stat.AvgCacheHitRate,
		"stream_req_ratio":     stat.StreamReqRatio,
		"quota_pm":             stat.QuotaPM,
		"total_tokens":         stat.TotalTokens,
		"total_quota":          stat.TotalQuota,
		"avg_concurrency":      stat.AvgConcurrency,
		"total_sessions":       stat.TotalSessions,
		"downtime_percentage":  stat.DowntimePercentage,
		"unique_users":         stat.UniqueUsers,
		"updated_at":           now,
	}

	if err := DB.Model(&GroupStatistics{}).
		Where("group_id = ? AND model_name = ? AND time_window_start = ?",
			stat.GroupId, stat.ModelName, stat.TimeWindowStart).
		Updates(updates).Error; err != nil {
		return err
	}

	return nil
}

// GetGroupStatistics 查询分组统计数据
// modelName: 如果为空，返回该分组所有模型的统计数据
// startTime, endTime: Unix timestamp，用于时间范围过滤
func GetGroupStatistics(groupId int, modelName string, startTime, endTime int64) ([]*GroupStatistics, error) {
	var stats []*GroupStatistics
	query := DB.Where("group_id = ?", groupId)

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

// GetLatestGroupStatistics 获取分组的最新统计数据（单条记录）
func GetLatestGroupStatistics(groupId int, modelName string) (*GroupStatistics, error) {
	var stat GroupStatistics
	query := DB.Where("group_id = ?", groupId)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	err := query.Order("time_window_start DESC, updated_at DESC").First(&stat).Error
	if err != nil {
		return nil, err
	}
	return &stat, nil
}

// DeleteGroupStatisticsBefore 删除指定时间之前的分组统计数据（用于数据清理）
func DeleteGroupStatisticsBefore(beforeTime int64) (int64, error) {
	result := DB.Where("time_window_start < ?", beforeTime).Delete(&GroupStatistics{})
	return result.RowsAffected, result.Error
}

// ========== Aggregation Operations ==========

// AggregatedGroupStats 聚合后的分组统计结构（用于API响应）
type AggregatedGroupStats struct {
	GroupId            int     `json:"group_id"`
	ModelName          string  `json:"model_name,omitempty"`
	TPM                int64   `json:"tpm"`
	RPM                int64   `json:"rpm"`
	FailRate           float64 `json:"fail_rate"`
	AvgResponseTimeMs  float64 `json:"avg_response_time_ms"`
	AvgCacheHitRate    float64 `json:"avg_cache_hit_rate"`
	StreamReqRatio     float64 `json:"stream_req_ratio"`
	QuotaPM            int64   `json:"quota_pm"`
	TotalTokens        int64   `json:"total_tokens"`
	TotalQuota         int64   `json:"total_quota"`
	AvgConcurrency     float64 `json:"avg_concurrency"`
	TotalSessions      int64   `json:"total_sessions"`
	DowntimePercentage float64 `json:"downtime_percentage"`
	UniqueUsers        int64   `json:"unique_users"`
}

// AggregateGroupStatisticsByTime 按时间范围聚合分组统计数据
// 将多个时间窗口的数据聚合为一个总体视图
func AggregateGroupStatisticsByTime(groupId int, modelName string, startTime, endTime int64) (*AggregatedGroupStats, error) {
	// 构建SQL查询：聚合多个时间窗口的数据
	query := DB.Model(&GroupStatistics{}).
		Select(`
			group_id,
			? as model_name,
			COALESCE(AVG(tpm), 0) as tpm,
			COALESCE(AVG(rpm), 0) as rpm,
			COALESCE(AVG(fail_rate), 0) as fail_rate,
			COALESCE(AVG(avg_response_time_ms), 0) as avg_response_time_ms,
			COALESCE(AVG(avg_cache_hit_rate), 0) as avg_cache_hit_rate,
			COALESCE(AVG(stream_req_ratio), 0) as stream_req_ratio,
			COALESCE(AVG(quota_pm), 0) as quota_pm,
			COALESCE(SUM(total_tokens), 0) as total_tokens,
			COALESCE(SUM(total_quota), 0) as total_quota,
			COALESCE(AVG(avg_concurrency), 0) as avg_concurrency,
			COALESCE(SUM(total_sessions), 0) as total_sessions,
			COALESCE(AVG(downtime_percentage), 0) as downtime_percentage,
			COALESCE(MAX(unique_users), 0) as unique_users
		`, modelName).
		Where("group_id = ?", groupId)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	if startTime > 0 {
		query = query.Where("time_window_start >= ?", startTime)
	}

	if endTime > 0 {
		query = query.Where("time_window_start <= ?", endTime)
	}

	var result AggregatedGroupStats
	err := query.Group("group_id").Scan(&result).Error
	if err != nil {
		return nil, err
	}

	result.GroupId = groupId
	if modelName != "" {
		result.ModelName = modelName
	}

	return &result, nil
}

// BatchUpsertGroupStatistics 批量插入或更新分组统计数据
func BatchUpsertGroupStatistics(stats []*GroupStatistics) error {
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
		if err := UpsertGroupStatistics(stat); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to upsert group statistics: %w", err)
		}
	}

	return tx.Commit().Error
}

// CountGroupStatistics 统计记录总数（用于监控和调试）
func CountGroupStatistics() (int64, error) {
	var count int64
	err := DB.Model(&GroupStatistics{}).Count(&count).Error
	return count, err
}

// GetGroupStatisticsByGroupId 获取指定分组的所有统计数据（不限时间）
func GetGroupStatisticsByGroupId(groupId int) ([]*GroupStatistics, error) {
	var stats []*GroupStatistics
	err := DB.Where("group_id = ?", groupId).
		Order("time_window_start DESC, model_name ASC").
		Find(&stats).Error
	return stats, err
}
