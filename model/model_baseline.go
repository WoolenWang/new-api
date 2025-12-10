package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ModelBaseline 模型基准表
// 存储由管理员设定的、作为"黄金标准"的模型输出
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: 6.4 model_baselines (模型基准表)
type ModelBaseline struct {
	Id                 int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ModelName          string `json:"model_name" gorm:"type:varchar(255);not null;index:idx_model_type_standard;comment:模型名称"`
	TestType           string `json:"test_type" gorm:"type:varchar(50);not null;index:idx_model_type_standard;comment:检测类型: encoding/reasoning/style/instruction_following/structure_consistency"`
	EvaluationStandard string `json:"evaluation_standard" gorm:"type:varchar(50);not null;index:idx_model_type_standard;comment:评估标准: strict/standard/lenient"`
	BaselineChannelId  int    `json:"baseline_channel_id" gorm:"not null;comment:基准渠道ID"`
	Prompt             string `json:"prompt" gorm:"type:text;not null;comment:测试用的Prompt"`
	BaselineOutput     string `json:"baseline_output" gorm:"type:text;not null;comment:基准输出内容"`
	CreatedAt          int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint"`
}

// TableName specifies the table name for GORM
func (ModelBaseline) TableName() string {
	return "model_baselines"
}

// BeforeCreate GORM hook: set CreatedAt and UpdatedAt timestamps
func (mb *ModelBaseline) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if mb.CreatedAt == 0 {
		mb.CreatedAt = now
	}
	if mb.UpdatedAt == 0 {
		mb.UpdatedAt = now
	}
	return nil
}

// BeforeUpdate GORM hook: update UpdatedAt timestamp
func (mb *ModelBaseline) BeforeUpdate(tx *gorm.DB) error {
	mb.UpdatedAt = common.GetTimestamp()
	return nil
}

// ==================== CRUD Operations ====================

// CreateModelBaseline 创建模型基准
func CreateModelBaseline(baseline *ModelBaseline) error {
	return DB.Create(baseline).Error
}

// GetModelBaselineById 根据ID获取模型基准
func GetModelBaselineById(id int) (*ModelBaseline, error) {
	var baseline ModelBaseline
	err := DB.Where("id = ?", id).First(&baseline).Error
	if err != nil {
		return nil, err
	}
	return &baseline, nil
}

// GetModelBaseline 根据模型名称、检测类型和评估标准获取基准
// 这是监控流程中最常用的查询方法
func GetModelBaseline(modelName, testType, evaluationStandard string) (*ModelBaseline, error) {
	var baseline ModelBaseline
	err := DB.Where("model_name = ? AND test_type = ? AND evaluation_standard = ?",
		modelName, testType, evaluationStandard).
		First(&baseline).Error
	if err != nil {
		return nil, err
	}
	return &baseline, nil
}

// GetAllModelBaselines 获取所有模型基准
func GetAllModelBaselines() ([]*ModelBaseline, error) {
	var baselines []*ModelBaseline
	err := DB.Order("id desc").Find(&baselines).Error
	return baselines, err
}

// GetModelBaselinesByModel 根据模型名称获取所有基准
func GetModelBaselinesByModel(modelName string) ([]*ModelBaseline, error) {
	var baselines []*ModelBaseline
	err := DB.Where("model_name = ?", modelName).Order("id desc").Find(&baselines).Error
	return baselines, err
}

// GetModelBaselinesByTestType 根据检测类型获取所有基准
func GetModelBaselinesByTestType(testType string) ([]*ModelBaseline, error) {
	var baselines []*ModelBaseline
	err := DB.Where("test_type = ?", testType).Order("id desc").Find(&baselines).Error
	return baselines, err
}

// UpsertModelBaseline 插入或更新模型基准
// 基于 (model_name, test_type, evaluation_standard) 的唯一性
func UpsertModelBaseline(baseline *ModelBaseline) error {
	// 先尝试查找现有记录
	var existing ModelBaseline
	err := DB.Where("model_name = ? AND test_type = ? AND evaluation_standard = ?",
		baseline.ModelName, baseline.TestType, baseline.EvaluationStandard).
		First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		// 记录不存在，执行插入
		return DB.Create(baseline).Error
	} else if err != nil {
		return err
	}

	// 记录存在，执行更新
	baseline.Id = existing.Id               // 保留原有ID
	baseline.CreatedAt = existing.CreatedAt // 保留创建时间
	return DB.Save(baseline).Error
}

// UpdateModelBaseline 更新模型基准
func (mb *ModelBaseline) Update() error {
	return DB.Save(mb).Error
}

// DeleteModelBaseline 删除模型基准
func DeleteModelBaseline(id int) error {
	return DB.Where("id = ?", id).Delete(&ModelBaseline{}).Error
}

// DeleteModelBaselinesByModel 删除指定模型的所有基准
func DeleteModelBaselinesByModel(modelName string) error {
	return DB.Where("model_name = ?", modelName).Delete(&ModelBaseline{}).Error
}

// SearchModelBaselines 搜索模型基准（按模型名称或测试类型）
func SearchModelBaselines(keyword string) ([]*ModelBaseline, error) {
	var baselines []*ModelBaseline
	query := DB.Order("id desc")
	if keyword != "" {
		query = query.Where("model_name LIKE ? OR test_type LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	err := query.Find(&baselines).Error
	return baselines, err
}

// CountModelBaselines 统计模型基准总数
func CountModelBaselines() (int64, error) {
	var count int64
	err := DB.Model(&ModelBaseline{}).Count(&count).Error
	return count, err
}

// GetDistinctModelNames 获取所有已设定基准的模型名称列表（去重）
func GetDistinctModelNamesFromBaselines() ([]string, error) {
	var modelNames []string
	err := DB.Model(&ModelBaseline{}).Distinct("model_name").Pluck("model_name", &modelNames).Error
	return modelNames, err
}

// GetDistinctTestTypes 获取所有已使用的检测类型列表（去重）
func GetDistinctTestTypesFromBaselines() ([]string, error) {
	var testTypes []string
	err := DB.Model(&ModelBaseline{}).Distinct("test_type").Pluck("test_type", &testTypes).Error
	return testTypes, err
}

// BaselineExistsForModel 检查指定模型是否已存在基准
func BaselineExistsForModel(modelName string) (bool, error) {
	var count int64
	err := DB.Model(&ModelBaseline{}).Where("model_name = ?", modelName).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
