package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Package represents a subscription package template
type Package struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name        string `json:"name" gorm:"type:varchar(100);not null"`
	Description string `json:"description" gorm:"type:text"`
	Status      int    `json:"status" gorm:"type:tinyint;default:1;index:idx_p2p_group_status"`

	// Priority and ownership
	Priority   int `json:"priority" gorm:"type:tinyint;not null;default:10;index:idx_priority"`
	P2PGroupId int `json:"p2p_group_id" gorm:"type:int;default:0;index:idx_p2p_group_status"`
	CreatorId  int `json:"creator_id" gorm:"type:int;not null;default:0;index:idx_creator"`

	// Package quota and duration
	Quota        int64  `json:"quota" gorm:"type:bigint;not null"`
	DurationType string `json:"duration_type" gorm:"type:varchar(20);not null"` // week, month, quarter, year
	Duration     int    `json:"duration" gorm:"type:int;not null;default:1"`

	// Multi-dimensional rate limits
	RpmLimit        int   `json:"rpm_limit" gorm:"type:int;default:0"`
	HourlyLimit     int64 `json:"hourly_limit" gorm:"type:bigint;default:0"`
	FourHourlyLimit int64 `json:"four_hourly_limit" gorm:"type:bigint;default:0"`
	DailyLimit      int64 `json:"daily_limit" gorm:"type:bigint;default:0"`
	WeeklyLimit     int64 `json:"weekly_limit" gorm:"type:bigint;default:0"`

	// Configuration options
	FallbackToBalance bool `json:"fallback_to_balance" gorm:"type:boolean;default:true"`

	// Timestamps
	CreatedAt int64 `json:"created_at" gorm:"type:bigint;not null"`
	UpdatedAt int64 `json:"updated_at" gorm:"type:bigint;not null"`
}

// TableName specifies the table name for Package model
func (Package) TableName() string {
	return "packages"
}

func (p *Package) BeforeCreate(tx *gorm.DB) (err error) {
	p.CreatedAt = common.GetTimestamp()
	p.UpdatedAt = common.GetTimestamp()
	return
}

func (p *Package) BeforeUpdate(tx *gorm.DB) (err error) {
	p.UpdatedAt = common.GetTimestamp()
	return
}

func GetPackageByID(id int) (*Package, error) {
	// 使用三级缓存（L1 内存 + L2 Redis + L3 DB）
	// forceDB=false：优先从缓存读取
	return GetPackageCache().GetPackageByIDCached(id, false)
}

// GetPackageByIDFromDB 强制从 DB 读取并刷新缓存
// 用于需要最新数据的场景（如更新后的验证）
func GetPackageByIDFromDB(id int) (*Package, error) {
	return GetPackageCache().GetPackageByIDCached(id, true)
}

func CreatePackage(pkg *Package) error {
	err := DB.Create(pkg).Error
	if err != nil {
		return err
	}

	// 【缓存一致性】创建成功后，预填充缓存
	cache := GetPackageCache()
	cache.setPackageToL1(pkg.Id, pkg)
	if common.RedisEnabled {
		cache.setPackageToL2(pkg.Id, pkg)
	}

	return nil
}

func (p *Package) Update() error {
	err := DB.Save(p).Error
	if err != nil {
		return err
	}

	// 【缓存一致性】更新成功后，使缓存失效
	GetPackageCache().InvalidatePackage(p.Id)
	return nil
}

func DeletePackage(id int) error {
	err := DB.Delete(&Package{}, id).Error
	if err != nil {
		return err
	}

	// 【缓存一致性】删除成功后，使缓存失效
	GetPackageCache().InvalidatePackage(id)
	return nil
}

// GetPackages retrieves packages filtered by p2pGroupId and status
// p2pGroupId: filter by P2P group (0 for global packages, -1 for all)
// status: filter by status (0 for all, 1 for available, 2 for unavailable)
func GetPackages(p2pGroupId int, status int) ([]*Package, error) {
	var packages []*Package
	query := DB.Model(&Package{})

	// Filter by status
	if status > 0 {
		query = query.Where("status = ?", status)
	}

	// Filter by P2P group
	if p2pGroupId > 0 {
		// Public packages OR packages for the specific p2p group
		query = query.Where("p2p_group_id = 0 OR p2p_group_id = ?", p2pGroupId)
	} else if p2pGroupId == 0 {
		// Only public packages
		query = query.Where("p2p_group_id = 0")
	}
	// If p2pGroupId < 0, no filter is applied (return all packages)

	err := query.Order("priority DESC, id ASC").Find(&packages).Error
	return packages, err
}

// GetPackagesByIds retrieves packages by a list of IDs
func GetPackagesByIds(ids []int) ([]*Package, error) {
	if len(ids) == 0 {
		return []*Package{}, nil
	}
	var packages []*Package
	err := DB.Where("id IN ?", ids).Order("priority DESC, id ASC").Find(&packages).Error
	return packages, err
}

// GetDurationSeconds calculates the duration in seconds for a package
// Supports: week, month, quarter, year
// Uses time.AddDate for accurate month/year calculations (handles leap years and varying month lengths)
func GetDurationSeconds(durationType string, duration int) (int64, error) {
	if duration <= 0 {
		return 0, errors.New("duration must be positive")
	}

	now := time.Now()
	var endTime time.Time

	switch durationType {
	case "week":
		endTime = now.AddDate(0, 0, 7*duration)
	case "month":
		endTime = now.AddDate(0, duration, 0)
	case "quarter":
		endTime = now.AddDate(0, 3*duration, 0)
	case "year":
		endTime = now.AddDate(duration, 0, 0)
	default:
		return 0, errors.New("invalid duration type: must be week, month, quarter, or year")
	}

	return int64(endTime.Sub(now).Seconds()), nil
}

// CalculateEndTime calculates the end time for a subscription based on start time and package duration
// Handles leap years and varying month lengths correctly using time.AddDate
func CalculateEndTime(startTime int64, pkg *Package) (int64, error) {
	if pkg == nil {
		return 0, errors.New("package cannot be nil")
	}
	if pkg.Duration <= 0 {
		return 0, errors.New("package duration must be positive")
	}

	start := time.Unix(startTime, 0)
	var endTime time.Time

	switch pkg.DurationType {
	case "week":
		endTime = start.AddDate(0, 0, 7*pkg.Duration)
	case "month":
		endTime = start.AddDate(0, pkg.Duration, 0)
	case "quarter":
		endTime = start.AddDate(0, 3*pkg.Duration, 0)
	case "year":
		endTime = start.AddDate(pkg.Duration, 0, 0)
	default:
		return 0, errors.New("invalid duration type: must be week, month, quarter, or year")
	}

	return endTime.Unix(), nil
}

// GetUserActiveP2PGroupIds 获取用户的活跃P2P分组ID列表
// 这是对 GetUserActiveGroupIds 的封装，用于套餐权限过滤
func GetUserActiveP2PGroupIds(userId int) ([]int, error) {
	return GetUserActiveGroupIds(userId)
}

// GetPackagesForUser 获取用户可访问的所有套餐
// 包括全局套餐（p2p_group_id=0）+ 用户加入的P2P分组套餐
// 用于普通用户查询可见套餐列表
func GetPackagesForUser(userId int, userP2PGroupIds []int, status int) ([]*Package, error) {
	var packages []*Package
	query := DB.Model(&Package{})

	// 过滤状态
	if status > 0 {
		query = query.Where("status = ?", status)
	}

	// 权限过滤：全局套餐 OR 用户的P2P分组套餐
	if len(userP2PGroupIds) > 0 {
		// 用户有P2P分组：返回全局套餐 + 这些分组的套餐
		query = query.Where("p2p_group_id = 0 OR p2p_group_id IN (?)", userP2PGroupIds)
	} else {
		// 用户没有P2P分组：只返回全局套餐
		query = query.Where("p2p_group_id = 0")
	}

	err := query.Order("priority DESC, id ASC").Find(&packages).Error
	return packages, err
}
