package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ModelMonitoringResult 模型监控结果表
// 记录每一次自动化探测的结果
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: 6.5 model_monitoring_results (模型监控结果表)
type ModelMonitoringResult struct {
	Id                 int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId          int     `json:"channel_id" gorm:"not null;index:idx_monitoring_channel_model_time;comment:渠道ID"`
	ModelName          string  `json:"model_name" gorm:"type:varchar(255);not null;index:idx_monitoring_channel_model_time;comment:模型名称"`
	BaselineId         int     `json:"baseline_id" gorm:"not null;comment:基准ID"`
	TestType           string  `json:"test_type" gorm:"type:varchar(50);not null;comment:检测类型"`
	TestTimestamp      int64   `json:"test_timestamp" gorm:"not null;index:idx_monitoring_channel_model_time;comment:测试时间戳"`
	Status             string  `json:"status" gorm:"type:varchar(20);not null;comment:pass, fail, monitor_failed"`
	DiffScore          float64 `json:"diff_score" gorm:"type:double precision;comment:差异得分(0-100, 越大差异越大)"`
	SimilarityScore    float64 `json:"similarity_score" gorm:"type:double precision;comment:相似度得分(0-100)"`
	Reason             *string `json:"reason" gorm:"type:text;comment:失败原因或裁判LLM的评估理由"`
	RawOutput          *string `json:"raw_output" gorm:"type:text;comment:原始输出内容"`
	EvaluationStandard string  `json:"evaluation_standard" gorm:"type:varchar(50);comment:使用的评估标准"`
	PolicyId           int     `json:"policy_id" gorm:"default:0;comment:触发的策略ID,0表示手动触发"`
	CreatedAt          int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64   `json:"updated_at" gorm:"bigint"`
}

// TableName specifies the table name for GORM
func (ModelMonitoringResult) TableName() string {
	return "model_monitoring_results"
}

// BeforeCreate GORM hook: set CreatedAt and UpdatedAt timestamps
func (mmr *ModelMonitoringResult) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if mmr.CreatedAt == 0 {
		mmr.CreatedAt = now
	}
	if mmr.UpdatedAt == 0 {
		mmr.UpdatedAt = now
	}
	return nil
}

// BeforeUpdate GORM hook: update UpdatedAt timestamp
func (mmr *ModelMonitoringResult) BeforeUpdate(tx *gorm.DB) error {
	mmr.UpdatedAt = common.GetTimestamp()
	return nil
}

// ==================== CRUD Operations ====================

// CreateMonitoringResult 创建监控结果
func CreateMonitoringResult(result *ModelMonitoringResult) error {
	return DB.Create(result).Error
}

// GetMonitoringResultById 根据ID获取监控结果
func GetMonitoringResultById(id int64) (*ModelMonitoringResult, error) {
	var result ModelMonitoringResult
	err := DB.Where("id = ?", id).First(&result).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// GetMonitoringResultsByChannel 根据渠道ID获取监控结果
func GetMonitoringResultsByChannel(channelId int, limit int) ([]*ModelMonitoringResult, error) {
	var results []*ModelMonitoringResult
	query := DB.Where("channel_id = ?", channelId).Order("test_timestamp desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&results).Error
	return results, err
}

// GetMonitoringResultsByModel 根据模型名称获取监控结果
func GetMonitoringResultsByModel(modelName string, limit int) ([]*ModelMonitoringResult, error) {
	var results []*ModelMonitoringResult
	query := DB.Where("model_name = ?", modelName).Order("test_timestamp desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&results).Error
	return results, err
}

// GetMonitoringResultsByChannelAndModel 根据渠道ID、模型名称和检测类型获取监控结果
func GetMonitoringResultsByChannelAndModel(channelId int, modelName, testType string, startTime, endTime int64, limit int) ([]*ModelMonitoringResult, error) {
	var results []*ModelMonitoringResult
	query := DB.Where("channel_id = ? AND model_name = ?", channelId, modelName)

	if testType != "" {
		query = query.Where("test_type = ?", testType)
	}

	if startTime > 0 {
		// startTime is treated as exclusive boundary to match test/design semantics.
		query = query.Where("test_timestamp > ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("test_timestamp <= ?", endTime)
	}

	query = query.Order("test_timestamp desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&results).Error
	return results, err
}

// GetMonitoringResultsByStatus 根据状态获取监控结果
func GetMonitoringResultsByStatus(status string, limit int) ([]*ModelMonitoringResult, error) {
	var results []*ModelMonitoringResult
	query := DB.Where("status = ?", status).Order("test_timestamp desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&results).Error
	return results, err
}

// GetLatestMonitoringResult 获取指定渠道和模型的最新监控结果
func GetLatestMonitoringResult(channelId int, modelName, testType string) (*ModelMonitoringResult, error) {
	var result ModelMonitoringResult
	query := DB.Where("channel_id = ? AND model_name = ?", channelId, modelName)
	if testType != "" {
		query = query.Where("test_type = ?", testType)
	}
	err := query.Order("test_timestamp desc").First(&result).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateMonitoringResult 更新监控结果
func (mmr *ModelMonitoringResult) Update() error {
	return DB.Save(mmr).Error
}

// DeleteMonitoringResult 删除监控结果
func DeleteMonitoringResult(id int64) error {
	return DB.Where("id = ?", id).Delete(&ModelMonitoringResult{}).Error
}

// DeleteMonitoringResultsBefore 删除指定时间之前的监控结果（用于数据清理）
func DeleteMonitoringResultsBefore(beforeTime int64) (int64, error) {
	result := DB.Where("test_timestamp < ?", beforeTime).Delete(&ModelMonitoringResult{})
	return result.RowsAffected, result.Error
}

// DeleteMonitoringResultsByChannel 删除指定渠道的所有监控结果
func DeleteMonitoringResultsByChannel(channelId int) error {
	return DB.Where("channel_id = ?", channelId).Delete(&ModelMonitoringResult{}).Error
}

// ==================== Aggregation & Statistics ====================

// MonitoringStatistics 监控统计信息
type MonitoringStatistics struct {
	ChannelId          int     `json:"channel_id"`
	ModelName          string  `json:"model_name,omitempty"`
	TotalTests         int64   `json:"total_tests"`
	PassCount          int64   `json:"pass_count"`
	FailCount          int64   `json:"fail_count"`
	MonitorFailCount   int64   `json:"monitor_fail_count"`
	PassRate           float64 `json:"pass_rate"`
	AvgDiffScore       float64 `json:"avg_diff_score"`
	AvgSimilarityScore float64 `json:"avg_similarity_score"`
}

// GetMonitoringStatistics 获取监控统计信息
func GetMonitoringStatistics(channelId int, modelName string, startTime, endTime int64) (*MonitoringStatistics, error) {
	query := DB.Model(&ModelMonitoringResult{}).
		Select(`
			channel_id,
			? as model_name,
			COUNT(*) as total_tests,
			SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) as pass_count,
			SUM(CASE WHEN status = 'fail' THEN 1 ELSE 0 END) as fail_count,
			SUM(CASE WHEN status = 'monitor_failed' THEN 1 ELSE 0 END) as monitor_fail_count,
			CASE
				WHEN COUNT(*) > 0 THEN (SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) * 100.0 / COUNT(*))
				ELSE 0
			END as pass_rate,
			AVG(CASE WHEN diff_score IS NOT NULL THEN diff_score ELSE 0 END) as avg_diff_score,
			AVG(CASE WHEN similarity_score IS NOT NULL THEN similarity_score ELSE 0 END) as avg_similarity_score
		`, modelName).
		Where("channel_id = ?", channelId)

	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}
	if startTime > 0 {
		query = query.Where("test_timestamp >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("test_timestamp <= ?", endTime)
	}

	var stats MonitoringStatistics
	err := query.Group("channel_id").Scan(&stats).Error
	if err != nil {
		return nil, err
	}

	stats.ChannelId = channelId
	if modelName != "" {
		stats.ModelName = modelName
	}

	return &stats, nil
}

// GetModelMonitoringReport 获取指定模型在所有渠道下的监控报告
func GetModelMonitoringReport(modelName string, testType string, startTime, endTime int64) ([]*MonitoringStatistics, error) {
	query := DB.Model(&ModelMonitoringResult{}).
		Select(`
			channel_id,
			model_name,
			COUNT(*) as total_tests,
			SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) as pass_count,
			SUM(CASE WHEN status = 'fail' THEN 1 ELSE 0 END) as fail_count,
			SUM(CASE WHEN status = 'monitor_failed' THEN 1 ELSE 0 END) as monitor_fail_count,
			CASE
				WHEN COUNT(*) > 0 THEN (SUM(CASE WHEN status = 'pass' THEN 1 ELSE 0 END) * 100.0 / COUNT(*))
				ELSE 0
			END as pass_rate,
			AVG(CASE WHEN diff_score IS NOT NULL THEN diff_score ELSE 0 END) as avg_diff_score,
			AVG(CASE WHEN similarity_score IS NOT NULL THEN similarity_score ELSE 0 END) as avg_similarity_score
		`).
		Where("model_name = ?", modelName)

	if testType != "" {
		query = query.Where("test_type = ?", testType)
	}
	if startTime > 0 {
		query = query.Where("test_timestamp >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("test_timestamp <= ?", endTime)
	}

	var stats []*MonitoringStatistics
	err := query.Group("channel_id, model_name").Scan(&stats).Error
	return stats, err
}

// CountMonitoringResults 统计监控结果总数
func CountMonitoringResults() (int64, error) {
	var count int64
	err := DB.Model(&ModelMonitoringResult{}).Count(&count).Error
	return count, err
}

// GetFailedChannels 获取在指定时间范围内失败的渠道列表
func GetFailedChannels(startTime, endTime int64, failureThreshold float64) ([]int, error) {
	type ChannelFailureRate struct {
		ChannelId int
		FailRate  float64
	}

	var results []ChannelFailureRate
	query := DB.Model(&ModelMonitoringResult{}).
		Select(`
			channel_id,
			CASE
				WHEN COUNT(*) > 0 THEN (SUM(CASE WHEN status = 'fail' THEN 1 ELSE 0 END) * 100.0 / COUNT(*))
				ELSE 0
			END as fail_rate
		`)

	if startTime > 0 {
		query = query.Where("test_timestamp >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("test_timestamp <= ?", endTime)
	}

	err := query.Group("channel_id").Having("fail_rate >= ?", failureThreshold).Scan(&results).Error
	if err != nil {
		return nil, err
	}

	channelIds := make([]int, len(results))
	for i, r := range results {
		channelIds[i] = r.ChannelId
	}
	return channelIds, nil
}
