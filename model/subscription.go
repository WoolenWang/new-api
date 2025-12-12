package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Subscription represents a user's subscription instance of a package
type Subscription struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId    int    `json:"user_id" gorm:"type:int;not null;index:idx_user_status"`
	PackageId int    `json:"package_id" gorm:"type:int;not null;index:idx_package"`
	Status    string `json:"status" gorm:"type:varchar(20);not null;default:'inventory';index:idx_user_status,idx_active_time"`

	// Lifecycle timestamps
	SubscribedAt int64  `json:"subscribed_at" gorm:"type:bigint;not null"`                      // Purchase time
	StartTime    *int64 `json:"start_time" gorm:"type:bigint;default:null"`                     // Activation time (NULL if not activated)
	EndTime      *int64 `json:"end_time" gorm:"type:bigint;default:null;index:idx_active_time"` // Expiration time (NULL if not activated)

	// Consumption tracking
	TotalConsumed int64 `json:"total_consumed" gorm:"type:bigint;default:0"`
}

// Status constants for subscription lifecycle
const (
	SubscriptionStatusInventory = "inventory" // Purchased but not activated
	SubscriptionStatusActive    = "active"    // Currently active and usable
	SubscriptionStatusExpired   = "expired"   // Past end_time
	SubscriptionStatusCancelled = "cancelled" // Manually cancelled by user or admin
)

// TableName specifies the table name for Subscription model
func (Subscription) TableName() string {
	return "subscriptions"
}

func (s *Subscription) BeforeCreate(tx *gorm.DB) (err error) {
	s.SubscribedAt = common.GetTimestamp()
	return
}

func CreateSubscription(sub *Subscription) error {
	return DB.Create(sub).Error
}

// GetSubscriptionById retrieves a subscription by its ID
func GetSubscriptionById(id int) (*Subscription, error) {
	var sub Subscription
	err := DB.First(&sub, id).Error
	return &sub, err
}

// UpdateSubscriptionStatus updates the status of a subscription
func UpdateSubscriptionStatus(id int, status string) error {
	return DB.Model(&Subscription{}).Where("id = ?", id).Update("status", status).Error
}

// GetUserSubscriptions retrieves all subscriptions for a user, ordered by end_time DESC
func GetUserSubscriptions(userId int, status string) ([]*Subscription, error) {
	var subs []*Subscription
	query := DB.Where("user_id = ?", userId)

	// Filter by status if provided
	if status != "" {
		query = query.Where("status = ?", status)
	}

	err := query.Order("end_time DESC, id DESC").Find(&subs).Error
	return subs, err
}

// IncrementSubscriptionConsumed atomically increments the total_consumed field
// This is the same as UpdateConsumedQuota, provided for API consistency
func IncrementSubscriptionConsumed(id int, quota int64) error {
	return DB.Model(&Subscription{}).Where("id = ?", id).
		Update("total_consumed", gorm.Expr("total_consumed + ?", quota)).Error
}

// GetUserActiveSubscriptions retrieves active subscriptions for a user
// Filters by P2P group and validates time range
func GetUserActiveSubscriptions(userId int, p2pGroupId *int) ([]*Subscription, error) {
	var subs []*Subscription
	now := time.Now().Unix()
	query := DB.Joins("JOIN packages ON subscriptions.package_id = packages.id").
		Where("subscriptions.user_id = ?", userId).
		Where("subscriptions.status = ?", SubscriptionStatusActive).
		Where("subscriptions.start_time IS NOT NULL").
		Where("subscriptions.end_time IS NOT NULL").
		Where("subscriptions.start_time <= ?", now).
		Where("subscriptions.end_time > ?", now)

	if p2pGroupId != nil {
		query = query.Where("packages.p2p_group_id = 0 OR packages.p2p_group_id = ?", *p2pGroupId)
	} else {
		query = query.Where("packages.p2p_group_id = 0")
	}

	err := query.Order("packages.priority DESC, subscriptions.id ASC").Find(&subs).Error
	return subs, err
}

// Activate activates a subscription, setting start_time and calculating end_time
// Uses CalculateEndTime for accurate duration calculation (handles leap years and varying month lengths)
func (s *Subscription) Activate() error {
	pkg, err := GetPackageByID(s.PackageId)
	if err != nil {
		return err
	}

	now := common.GetTimestamp()
	endTime, err := CalculateEndTime(now, pkg)
	if err != nil {
		return err
	}

	s.StartTime = &now
	s.EndTime = &endTime
	s.Status = SubscriptionStatusActive

	return DB.Save(s).Error
}

// UpdateConsumedQuota atomically increments the total_consumed field
func (s *Subscription) UpdateConsumedQuota(amount int64) error {
	return DB.Model(s).Update("total_consumed", gorm.Expr("total_consumed + ?", amount)).Error
}

// CountActiveSubscriptions counts the number of active subscriptions for a specific package
// Used to prevent deleting packages that have active subscriptions
func CountActiveSubscriptions(packageId int) (int64, error) {
	var count int64
	err := DB.Model(&Subscription{}).
		Where("package_id = ?", packageId).
		Where("status = ?", SubscriptionStatusActive).
		Count(&count).Error
	return count, err
}

// CountUserActiveSubscriptions counts the number of active subscriptions for a specific user
// Used to enforce the maximum active subscriptions limit per user
func CountUserActiveSubscriptions(userId int) (int64, error) {
	var count int64
	now := time.Now().Unix()
	err := DB.Model(&Subscription{}).
		Where("user_id = ?", userId).
		Where("status = ?", SubscriptionStatusActive).
		Where("start_time IS NOT NULL").
		Where("end_time IS NOT NULL").
		Where("start_time <= ?", now).
		Where("end_time > ?", now).
		Count(&count).Error
	return count, err
}
