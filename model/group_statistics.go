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

// GroupModelStats 聚合后的分组按模型统计结构（用于API响应）
type GroupModelStats struct {
	GroupId            int     `json:"group_id"`
	ModelName          string  `json:"model_name"`
	TPM                float64 `json:"tpm"`
	RPM                float64 `json:"rpm"`
	TotalTokens        int64   `json:"total_tokens"`
	TotalQuota         int64   `json:"total_quota"`
	AvgResponseTimeMs  float64 `json:"avg_response_time_ms"`
	FailRate           float64 `json:"fail_rate"`
	DowntimePercentage float64 `json:"downtime_percentage"`
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

// AggregateGroupModelStats 按模型维度聚合分组统计数据
//
// 用途：为 P2P 分组提供按模型维度的聚合视图（Token/Quota/TPM/RPM/延迟/失败率/停服占比）
//
// 参数：
//   - groupId: 分组ID
//   - startTime, endTime: 时间范围（Unix时间戳）
//   - modelName: 可选，指定模型名；为空则聚合该分组的所有模型
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 10.3.1 / 11.3.3
func AggregateGroupModelStats(groupId int, startTime, endTime int64, modelName string) ([]GroupModelStats, error) {
	query := DB.Table("group_statistics").
		Select(`
			group_id,
			model_name,
			COALESCE(SUM(total_tokens), 0)        AS total_tokens,
			COALESCE(SUM(total_quota), 0)         AS total_quota,
			COALESCE(AVG(tpm), 0)                 AS tpm,
			COALESCE(AVG(rpm), 0)                 AS rpm,
			COALESCE(AVG(avg_response_time_ms),0) AS avg_response_time_ms,
			COALESCE(AVG(fail_rate), 0)           AS fail_rate,
			COALESCE(AVG(downtime_percentage),0)  AS downtime_percentage
		`).
		Where("group_id = ?", groupId)

	if startTime > 0 {
		query = query.Where("time_window_start >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("time_window_start <= ?", endTime)
	}
	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	var results []GroupModelStats
	if err := query.
		Group("group_id, model_name").
		Order("model_name ASC").
		Scan(&results).Error; err != nil {
		return nil, err
	}

	return results, nil
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

// GroupModelDailyUsage 分组按模型每日使用量结构
// 用于 P2P 分组按模型每日消耗曲线
type GroupModelDailyUsage struct {
	GroupId   int    `json:"group_id"`
	Day       string `json:"day"`        // YYYY-MM-DD
	ModelName string `json:"model_name"` // 模型名称
	Tokens    int64  `json:"tokens"`     // 当日 Token 数
	Quota     int64  `json:"quota"`      // 当日额度消耗
}

// GetGroupModelDailyUsage 获取分组按模型的每日 Token/Quota 消耗曲线
//
// 参数：
//   - groupId: 分组ID
//   - days: 向前多少天（默认 30，最大 90）
//   - modelName: 可选，指定模型名
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 10.3.2 / 11.3.4
func GetGroupModelDailyUsage(groupId int, days int, modelName string) ([]GroupModelDailyUsage, error) {
	if days <= 0 {
		days = 30
	}
	if days > 90 {
		days = 90
	}

	now := common.GetTimestamp()
	startTime := now - int64(days*24*60*60)

	query := DB.Table("group_statistics").
		Select(`
			group_id,
			DATE(FROM_UNIXTIME(time_window_start)) AS day,
			model_name,
			SUM(total_tokens) AS tokens,
			SUM(total_quota)  AS quota
		`).
		Where("group_id = ?", groupId).
		Where("time_window_start >= ?", startTime)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}

	var results []GroupModelDailyUsage
	if err := query.
		Group("group_id, day, model_name").
		Order("day ASC, model_name ASC").
		Scan(&results).Error; err != nil {
		return nil, err
	}

	return results, nil
}

// ========== Public Group Ranking Operations ==========

// GroupRankingRow 公开分组排名数据行
// 用于分组广场的排行榜展示
// 设计文档: docs/系统统计数据dashboard设计.md Section 5
type GroupRankingRow struct {
	GroupId            int     `json:"group_id"`
	GroupName          string  `json:"group_name"`
	DisplayName        string  `json:"display_name"`
	MemberCount        int64   `json:"member_count"`
	ChannelCount       int64   `json:"channel_count"`
	Tokens7d           int64   `json:"tokens_7d"`
	Tokens30d          int64   `json:"tokens_30d"`
	AvgTPM             float64 `json:"tpm"`
	AvgRPM             float64 `json:"rpm"`
	AvgLatencyMs       float64 `json:"avg_response_time_ms"`
	AvgFailRate        float64 `json:"fail_rate"`
	AvgDowntimePercent float64 `json:"downtime_percentage"`
}

// RankPublicGroups 对所有公开共享分组进行聚合与排名
//
// 参数说明：
//   - metric: 排名指标 (tokens_7d, tokens_30d, tpm, rpm, latency, fail_rate, downtime)
//   - period: 用于计算 tpm/rpm/latency/fail_rate/downtime 的时间窗口（如 "7d"）
//   - order: 排序方向 ("asc" 或 "desc")，实际排序在 Controller 层完成，此函数只返回原始聚合数据
//
// 返回值：
//   - 未排序的分组排名数据列表，排序逻辑在 Controller 层实现以提供更灵活的多指标排序
//
// 设计文档: docs/系统统计数据dashboard设计.md Section 5.3
func RankPublicGroups(metric string, period string) ([]GroupRankingRow, error) {
	// 1. 计算时间范围
	now := common.GetTimestamp()
	start7d := now - 7*24*60*60
	start30d := now - 30*24*60*60

	// 根据 period 计算 startTime（用于 tpm/rpm 等指标）
	var startTimeForPeriod int64
	switch period {
	case "1h":
		startTimeForPeriod = now - 60*60
	case "6h":
		startTimeForPeriod = now - 6*60*60
	case "24h", "1d":
		startTimeForPeriod = now - 24*60*60
	case "7d":
		startTimeForPeriod = start7d
	case "30d":
		startTimeForPeriod = start30d
	default:
		startTimeForPeriod = start7d // 默认 7 天
	}

	// 2. 构建主查询：聚合所有公开共享分组的统计数据
	// 注意：这里不进行排序，排序在 Controller 层完成
	type AggResult struct {
		GroupId     int
		Tokens7d    int64
		Tokens30d   int64
		AvgTPM      float64
		AvgRPM      float64
		AvgLatency  float64
		AvgFailRate float64
		AvgDowntime float64
	}

	// 构建聚合查询
	// 使用 LEFT JOIN 确保即使没有统计数据的分组也能出现在结果中
	var aggResults []AggResult
	err := DB.Table("groups g").
		Select(`
			g.id AS group_id,
			COALESCE(SUM(CASE
				WHEN gs.time_window_start BETWEEN ? AND ?
				THEN gs.total_tokens ELSE 0
			END), 0) AS tokens_7d,
			COALESCE(SUM(CASE
				WHEN gs.time_window_start BETWEEN ? AND ?
				THEN gs.total_tokens ELSE 0
			END), 0) AS tokens_30d,
			COALESCE(AVG(CASE
				WHEN gs.time_window_start BETWEEN ? AND ?
				THEN gs.tpm ELSE NULL
			END), 0) AS avg_tpm,
			COALESCE(AVG(CASE
				WHEN gs.time_window_start BETWEEN ? AND ?
				THEN gs.rpm ELSE NULL
			END), 0) AS avg_rpm,
			COALESCE(AVG(CASE
				WHEN gs.time_window_start BETWEEN ? AND ?
				THEN gs.avg_response_time_ms ELSE NULL
			END), 0) AS avg_latency,
			COALESCE(AVG(CASE
				WHEN gs.time_window_start BETWEEN ? AND ?
				THEN gs.fail_rate ELSE NULL
			END), 0) AS avg_fail_rate,
			COALESCE(AVG(CASE
				WHEN gs.time_window_start BETWEEN ? AND ?
				THEN gs.downtime_percentage ELSE NULL
			END), 0) AS avg_downtime
		`, start7d, now, start30d, now,
			startTimeForPeriod, now,
			startTimeForPeriod, now,
			startTimeForPeriod, now,
			startTimeForPeriod, now,
			startTimeForPeriod, now).
		Joins("LEFT JOIN group_statistics gs ON gs.group_id = g.id").
		Where("g.type = ?", GroupTypeShared).
		Where("g.join_method != ?", JoinMethodInvite).
		Group("g.id").
		Scan(&aggResults).Error

	if err != nil {
		return nil, fmt.Errorf("failed to aggregate group statistics: %w", err)
	}

	// 3. 如果没有公开分组，直接返回空列表
	if len(aggResults) == 0 {
		return []GroupRankingRow{}, nil
	}

	// 4. 批量获取分组基础信息
	var groupIds []int
	for _, r := range aggResults {
		groupIds = append(groupIds, r.GroupId)
	}

	var groups []Group
	err = DB.Where("id IN ?", groupIds).Find(&groups).Error
	if err != nil {
		return nil, fmt.Errorf("failed to fetch group details: %w", err)
	}

	// 构建 groupId -> Group 映射
	groupMap := make(map[int]*Group)
	for i := range groups {
		groupMap[groups[i].Id] = &groups[i]
	}

	// 5. 批量获取成员数统计
	type MemberCount struct {
		GroupId int
		Count   int64
	}
	var memberCounts []MemberCount
	err = DB.Table("user_groups").
		Select("group_id, COUNT(*) as count").
		Where("group_id IN ?", groupIds).
		Where("status = ?", MemberStatusActive).
		Group("group_id").
		Scan(&memberCounts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to count group members: %w", err)
	}

	memberCountMap := make(map[int]int64)
	for _, mc := range memberCounts {
		memberCountMap[mc.GroupId] = mc.Count
	}

	// 6. 组装最终结果
	// 注意：channel_count 暂时设为 0，因为需要解析 channels.allowed_groups JSON 字段
	// 可以在后续迭代中优化
	var results []GroupRankingRow
	for _, aggResult := range aggResults {
		group := groupMap[aggResult.GroupId]
		if group == nil {
			continue // 跳过未找到的分组（理论上不应该发生）
		}

		row := GroupRankingRow{
			GroupId:            aggResult.GroupId,
			GroupName:          group.Name,
			DisplayName:        group.DisplayName,
			MemberCount:        memberCountMap[aggResult.GroupId],
			ChannelCount:       0, // TODO: 从 channels.allowed_groups 解析（后续优化）
			Tokens7d:           aggResult.Tokens7d,
			Tokens30d:          aggResult.Tokens30d,
			AvgTPM:             aggResult.AvgTPM,
			AvgRPM:             aggResult.AvgRPM,
			AvgLatencyMs:       aggResult.AvgLatency,
			AvgFailRate:        aggResult.AvgFailRate,
			AvgDowntimePercent: aggResult.AvgDowntime,
		}
		results = append(results, row)
	}

	return results, nil
}
