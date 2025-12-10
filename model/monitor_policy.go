package model

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// MonitorPolicy 监控策略表
// 定义对哪些模型、以何种频率、按何种标准进行监控
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: 6.3 monitor_policies (模型监控策略表)
type MonitorPolicy struct {
	Id                 int     `json:"id" gorm:"primaryKey;autoIncrement"`
	Name               string  `json:"name" gorm:"type:varchar(100);not null;comment:策略名称"`
	TargetModels       *string `json:"target_models" gorm:"type:text;comment:监控的模型列表(JSON Array)"`
	TestTypes          *string `json:"test_types" gorm:"type:text;comment:检测类型(JSON Array: encoding, reasoning, style, instruction_following, structure_consistency)"`
	EvaluationStandard string  `json:"evaluation_standard" gorm:"type:varchar(50);not null;comment:评估标准: strict/standard/lenient"`
	TargetChannels     *string `json:"target_channels" gorm:"type:text;comment:受此策略影响的渠道ID列表(JSON Array); 为空表示所有渠道"`
	ScheduleCron       string  `json:"schedule_cron" gorm:"type:varchar(50);not null;comment:Cron表达式"`
	IsEnabled          bool    `json:"is_enabled" gorm:"type:boolean;default:true;comment:是否启用"`
	CreatedAt          int64   `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64   `json:"updated_at" gorm:"bigint"`
	LastExecutedAt     int64   `json:"last_executed_at" gorm:"bigint;default:0;comment:上次执行时间"`
	NextExecutionAt    int64   `json:"next_execution_at" gorm:"bigint;default:0;comment:下次执行时间"`
}

// TableName specifies the table name for GORM
func (MonitorPolicy) TableName() string {
	return "monitor_policies"
}

// BeforeCreate GORM hook: set CreatedAt and UpdatedAt timestamps
func (mp *MonitorPolicy) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if mp.CreatedAt == 0 {
		mp.CreatedAt = now
	}
	if mp.UpdatedAt == 0 {
		mp.UpdatedAt = now
	}
	return nil
}

// BeforeUpdate GORM hook: update UpdatedAt timestamp
func (mp *MonitorPolicy) BeforeUpdate(tx *gorm.DB) error {
	mp.UpdatedAt = common.GetTimestamp()
	return nil
}

// GetTargetModels 解析 TargetModels JSON 数组
func (mp *MonitorPolicy) GetTargetModels() []string {
	if mp.TargetModels == nil || *mp.TargetModels == "" {
		return []string{}
	}
	var models []string
	if err := json.Unmarshal([]byte(*mp.TargetModels), &models); err != nil {
		common.SysLog(fmt.Sprintf("failed to unmarshal target_models for policy %d: %v", mp.Id, err))
		return []string{}
	}
	return models
}

// SetTargetModels 设置 TargetModels JSON 数组
func (mp *MonitorPolicy) SetTargetModels(models []string) error {
	data, err := json.Marshal(models)
	if err != nil {
		return fmt.Errorf("failed to marshal target_models: %w", err)
	}
	modelsStr := string(data)
	mp.TargetModels = &modelsStr
	return nil
}

// GetTestTypes 解析 TestTypes JSON 数组
func (mp *MonitorPolicy) GetTestTypes() []string {
	if mp.TestTypes == nil || *mp.TestTypes == "" {
		return []string{}
	}
	var types []string
	if err := json.Unmarshal([]byte(*mp.TestTypes), &types); err != nil {
		common.SysLog(fmt.Sprintf("failed to unmarshal test_types for policy %d: %v", mp.Id, err))
		return []string{}
	}
	return types
}

// SetTestTypes 设置 TestTypes JSON 数组
func (mp *MonitorPolicy) SetTestTypes(types []string) error {
	data, err := json.Marshal(types)
	if err != nil {
		return fmt.Errorf("failed to marshal test_types: %w", err)
	}
	typesStr := string(data)
	mp.TestTypes = &typesStr
	return nil
}

// GetTargetChannels 解析 TargetChannels JSON 数组
// 返回空数组表示所有渠道
func (mp *MonitorPolicy) GetTargetChannels() []int {
	if mp.TargetChannels == nil || *mp.TargetChannels == "" {
		return []int{}
	}
	var channels []int
	if err := json.Unmarshal([]byte(*mp.TargetChannels), &channels); err != nil {
		common.SysLog(fmt.Sprintf("failed to unmarshal target_channels for policy %d: %v", mp.Id, err))
		return []int{}
	}
	return channels
}

// SetTargetChannels 设置 TargetChannels JSON 数组
func (mp *MonitorPolicy) SetTargetChannels(channels []int) error {
	data, err := json.Marshal(channels)
	if err != nil {
		return fmt.Errorf("failed to marshal target_channels: %w", err)
	}
	channelsStr := string(data)
	mp.TargetChannels = &channelsStr
	return nil
}

// ==================== CRUD Operations ====================

// CreateMonitorPolicy 创建监控策略
func CreateMonitorPolicy(policy *MonitorPolicy) error {
	return DB.Create(policy).Error
}

// GetMonitorPolicyById 根据ID获取监控策略
func GetMonitorPolicyById(id int) (*MonitorPolicy, error) {
	var policy MonitorPolicy
	err := DB.Where("id = ?", id).First(&policy).Error
	if err != nil {
		return nil, err
	}
	return &policy, nil
}

// GetAllMonitorPolicies 获取所有监控策略
func GetAllMonitorPolicies(enabledOnly bool) ([]*MonitorPolicy, error) {
	var policies []*MonitorPolicy
	query := DB.Order("id desc")
	if enabledOnly {
		query = query.Where("is_enabled = ?", true)
	}
	err := query.Find(&policies).Error
	return policies, err
}

// GetEnabledMonitorPolicies 获取所有启用的监控策略
func GetEnabledMonitorPolicies() ([]*MonitorPolicy, error) {
	return GetAllMonitorPolicies(true)
}

// UpdateMonitorPolicy 更新监控策略
func (mp *MonitorPolicy) Update() error {
	return DB.Save(mp).Error
}

// DeleteMonitorPolicy 删除监控策略
func DeleteMonitorPolicy(id int) error {
	return DB.Where("id = ?", id).Delete(&MonitorPolicy{}).Error
}

// UpdateLastExecutedTime 更新策略的上次执行时间和下次执行时间
func (mp *MonitorPolicy) UpdateLastExecutedTime(lastExecutedAt, nextExecutionAt int64) error {
	return DB.Model(mp).Updates(map[string]interface{}{
		"last_executed_at":  lastExecutedAt,
		"next_execution_at": nextExecutionAt,
		"updated_at":        common.GetTimestamp(),
	}).Error
}

// SearchMonitorPolicies 搜索监控策略（按名称或模型）
func SearchMonitorPolicies(keyword string) ([]*MonitorPolicy, error) {
	var policies []*MonitorPolicy
	query := DB.Order("id desc")
	if keyword != "" {
		query = query.Where("name LIKE ? OR target_models LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	err := query.Find(&policies).Error
	return policies, err
}

// CountMonitorPolicies 统计监控策略总数
func CountMonitorPolicies() (int64, error) {
	var count int64
	err := DB.Model(&MonitorPolicy{}).Count(&count).Error
	return count, err
}

// ToggleMonitorPolicyStatus 切换监控策略启用状态
func ToggleMonitorPolicyStatus(id int) error {
	policy, err := GetMonitorPolicyById(id)
	if err != nil {
		return err
	}
	policy.IsEnabled = !policy.IsEnabled
	return policy.Update()
}
