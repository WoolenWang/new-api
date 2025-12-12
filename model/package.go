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
	var pkg Package
	err := DB.First(&pkg, id).Error
	return &pkg, err
}

func CreatePackage(pkg *Package) error {
	return DB.Create(pkg).Error
}

func (p *Package) Update() error {
	return DB.Save(p).Error
}

func DeletePackage(id int) error {
	return DB.Delete(&Package{}, id).Error
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
