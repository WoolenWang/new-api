package model

import (
	"github.com/QuantumNous/new-api/common"
)

// SubscriptionHistory records each consumption event for a subscription
// This is an optional table for detailed consumption tracking
type SubscriptionHistory struct {
	Id             int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	SubscriptionId int    `json:"subscription_id" gorm:"type:int;not null;index:idx_subscription_time"`
	ModelName      string `json:"model_name" gorm:"type:varchar(100)"`
	ConsumedQuota  int    `json:"consumed_quota" gorm:"type:int;not null"`
	ConsumedAt     int64  `json:"consumed_at" gorm:"type:bigint;not null;index:idx_subscription_time"`
}

// TableName specifies the table name for SubscriptionHistory model
func (SubscriptionHistory) TableName() string {
	return "subscription_history"
}

// CreateHistory creates a new consumption history record
func CreateHistory(history *SubscriptionHistory) error {
	if history.ConsumedAt == 0 {
		history.ConsumedAt = common.GetTimestamp()
	}
	return DB.Create(history).Error
}

// GetHistoryBySubscription retrieves consumption history for a subscription
// Results are ordered by consumed_at DESC, with pagination support via limit
func GetHistoryBySubscription(subscriptionId int, limit int) ([]*SubscriptionHistory, error) {
	var histories []*SubscriptionHistory
	query := DB.Where("subscription_id = ?", subscriptionId).Order("consumed_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&histories).Error
	return histories, err
}

// GetHistoryBySubscriptionWithOffset retrieves consumption history with pagination
func GetHistoryBySubscriptionWithOffset(subscriptionId int, limit int, offset int) ([]*SubscriptionHistory, error) {
	var histories []*SubscriptionHistory
	err := DB.Where("subscription_id = ?", subscriptionId).
		Order("consumed_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&histories).Error
	return histories, err
}

// GetHistoryCount returns the total count of history records for a subscription
func GetHistoryCount(subscriptionId int) (int64, error) {
	var count int64
	err := DB.Model(&SubscriptionHistory{}).Where("subscription_id = ?", subscriptionId).Count(&count).Error
	return count, err
}
