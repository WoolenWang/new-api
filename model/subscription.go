package model

import (
	"errors"
	"strings"
	"sync"
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
	err := DB.Create(sub).Error
	if err != nil {
		return err
	}

	// 【缓存一致性】创建成功后，预填充缓存
	cache := GetPackageCache()
	cache.setSubscriptionToL1(sub.Id, sub)
	if common.RedisEnabled {
		cache.setSubscriptionToL2(sub.Id, sub)
	}

	return nil
}

// GetSubscriptionById retrieves a subscription by its ID (使用三级缓存)
func GetSubscriptionById(id int) (*Subscription, error) {
	// 使用三级缓存（L1 内存 + L2 Redis + L3 DB）
	return GetPackageCache().GetSubscriptionByIDCached(id, false)
}

// GetSubscriptionByIdFromDB 强制从 DB 读取并刷新缓存
func GetSubscriptionByIdFromDB(id int) (*Subscription, error) {
	return GetPackageCache().GetSubscriptionByIDCached(id, true)
}

// UpdateSubscriptionStatus updates the status of a subscription
func UpdateSubscriptionStatus(id int, status string) error {
	err := DB.Model(&Subscription{}).Where("id = ?", id).Update("status", status).Error
	if err != nil {
		return err
	}

	// 【缓存一致性】更新成功后，使缓存失效
	GetPackageCache().InvalidateSubscription(id)
	return nil
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
	const maxRetries = 5

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := DB.Model(&Subscription{}).
			Where("id = ?", id).
			Update("total_consumed", gorm.Expr("total_consumed + ?", quota)).Error
		if err == nil {
			// 【缓存一致性】更新消耗量后，使缓存失效
			// 注意：由于 total_consumed 会频繁更新，缓存失效可能导致缓存频繁穿透
			// 但为保证数据一致性，仍需失效缓存
			GetPackageCache().InvalidateSubscription(id)
			return nil
		}

		// 对于 SQLite，高并发下可能出现 "database is locked"（SQLITE_BUSY）。
		// 在这种情况下进行短暂重试，可以提高并发更新成功率，避免误报失败。
		if !(common.UsingSQLite && strings.Contains(err.Error(), "database is locked")) {
			return err
		}

		// 退避等待一小段时间后重试
		time.Sleep(time.Duration(10*(attempt+1)) * time.Millisecond)
	}

	return errors.New("failed to increment subscription total_consumed after retries")
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

	// 兼容旧版本数据库：仅在 packages 表包含 p2p_group_id 列时才追加相关过滤条件，
	// 防止在未完成迁移的环境中出现 "no such column: packages.p2p_group_id" 错误。
	if hasPackagesP2PGroupColumn() {
		if p2pGroupId != nil {
			query = query.Where("packages.p2p_group_id = 0 OR packages.p2p_group_id = ?", *p2pGroupId)
		} else {
			query = query.Where("packages.p2p_group_id = 0")
		}
	}

	err := query.Order("packages.priority DESC, subscriptions.id ASC").Find(&subs).Error
	return subs, err
}

// ActivateSubscription performs an atomic state transition from "inventory" to "active"
// for the given subscription ID. It calculates start_time/end_time based on the
// associated package duration and uses a conditional UPDATE to avoid race conditions.
//
// This function is designed to be safe under concurrent activation attempts:
// only one caller will successfully update the row (RowsAffected == 1),
// others will receive an "invalid status" error.
func ActivateSubscription(id int, now int64) error {
	// Load subscription and its package to compute end_time.
	var sub Subscription
	if err := DB.First(&sub, id).Error; err != nil {
		return err
	}

	pkg, err := GetPackageByID(sub.PackageId)
	if err != nil {
		return err
	}

	endTime, err := CalculateEndTime(now, pkg)
	if err != nil {
		return err
	}

	// Atomic state transition: only update when current status is "inventory".
	result := DB.Model(&Subscription{}).
		Where("id = ? AND status = ?", id, SubscriptionStatusInventory).
		Updates(map[string]interface{}{
			"status":     SubscriptionStatusActive,
			"start_time": now,
			"end_time":   endTime,
		})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		// Status was not "inventory" at the time of update (可能是重复激活或并发冲突)。
		return errors.New("invalid status")
	}

	// Invalidate subscription cache to ensure subsequent reads see the new state.
	GetPackageCache().InvalidateSubscription(id)
	return nil
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

	err = DB.Save(s).Error
	if err != nil {
		return err
	}

	// 【缓存一致性】激活后使缓存失效
	GetPackageCache().InvalidateSubscription(s.Id)
	return nil
}

// UpdateConsumedQuota atomically increments the total_consumed field
func (s *Subscription) UpdateConsumedQuota(amount int64) error {
	err := DB.Model(s).Update("total_consumed", gorm.Expr("total_consumed + ?", amount)).Error
	if err != nil {
		return err
	}

	// 【缓存一致性】更新消耗量后，使缓存失效
	GetPackageCache().InvalidateSubscription(s.Id)
	return nil
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

// hasPackagesP2PGroupColumn checks once whether the packages table contains
// the p2p_group_id column. This is used as a guard before adding SQL that
// references the column so we do not trigger runtime "no such column" errors
// on databases that were created before the P2P package field existed.
var (
	packagesP2PGroupColumnOnce   sync.Once
	packagesP2PGroupColumnExists bool
)

func hasPackagesP2PGroupColumn() bool {
	packagesP2PGroupColumnOnce.Do(func() {
		if DB == nil {
			// DB 尚未初始化时，不做任何判断，保持默认 false。
			return
		}

		// 对于非 SQLite 数据库，直接委托给 GORM 的 HasColumn 即可。
		if !common.UsingSQLite {
			packagesP2PGroupColumnExists = DB.Migrator().HasColumn(&Package{}, "P2PGroupId")
			if !packagesP2PGroupColumnExists {
				common.SysLog("warning: packages.p2p_group_id column not found on non-SQLite DB; P2P package filtering disabled until migration runs")
			}
			return
		}

		// SQLite 环境下，为了避免 HasColumn 在某些驱动版本上的兼容性问题，
		// 直接使用 PRAGMA table_info 读取实际表结构进行判断。
		type columnInfo struct {
			Name string `gorm:"column:name"`
		}
		var cols []columnInfo
		if err := DB.Raw("PRAGMA table_info(packages)").Scan(&cols).Error; err != nil {
			common.SysError("failed to inspect packages table schema for p2p_group_id: " + err.Error())
			return
		}
		for _, c := range cols {
			if c.Name == "p2p_group_id" {
				packagesP2PGroupColumnExists = true
				break
			}
		}
		if !packagesP2PGroupColumnExists {
			common.SysLog("warning: packages.p2p_group_id column not found in SQLite schema; P2P package filtering disabled until migration runs")
		}
	})
	return packagesP2PGroupColumnExists
}
