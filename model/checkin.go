package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CheckinResult represents the result of a checkin operation
type CheckinResult struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	Quota         int    `json:"quota"`           // Base checkin quota
	BonusQuota    int    `json:"bonus_quota"`     // Streak bonus quota (if any)
	TotalQuota    int    `json:"total_quota"`     // Total quota earned this checkin
	CurrentStreak int    `json:"current_streak"`  // Current streak after checkin
	IsStreakBonus bool   `json:"is_streak_bonus"` // Whether streak bonus was awarded
}

// CheckinStatus represents the user's checkin status
type CheckinStatus struct {
	HasCheckedInToday bool  `json:"has_checked_in_today"`
	CurrentStreak     int   `json:"current_streak"`
	LastCheckinTime   int64 `json:"last_checkin_time"`
	StreakDays        int   `json:"streak_days"`        // Days needed for streak bonus
	DaysUntilBonus    int   `json:"days_until_bonus"`   // Days until next streak bonus
	DailyQuota        int   `json:"daily_quota"`        // Daily checkin quota
	StreakBonusQuota  int   `json:"streak_bonus_quota"` // Streak bonus quota
}

// isSameDay checks if two timestamps are on the same day
func isSameDay(t1, t2 int64) bool {
	loc := time.Local
	time1 := time.Unix(t1, 0).In(loc)
	time2 := time.Unix(t2, 0).In(loc)
	return time1.Year() == time2.Year() &&
		time1.Month() == time2.Month() &&
		time1.Day() == time2.Day()
}

// isYesterday checks if t1 is the day before t2
func isYesterday(t1, t2 int64) bool {
	loc := time.Local
	time1 := time.Unix(t1, 0).In(loc)
	time2 := time.Unix(t2, 0).In(loc)

	// Get the start of yesterday (relative to t2)
	yesterday := time2.AddDate(0, 0, -1)

	return time1.Year() == yesterday.Year() &&
		time1.Month() == yesterday.Month() &&
		time1.Day() == yesterday.Day()
}

// UserCheckin performs daily check-in for a user
func UserCheckin(userId int) (*CheckinResult, error) {
	// Check if checkin is enabled
	if !operation_setting.IsCheckinEnabled() {
		return nil, errors.New("签到功能已关闭")
	}

	result := &CheckinResult{
		Success: false,
	}

	now := common.GetTimestamp()
	dailyQuota := operation_setting.GetCheckinDailyQuota()
	streakBonusQuota := operation_setting.GetCheckinStreakBonusQuota()
	streakDays := operation_setting.GetCheckinStreakDays()

	// Use transaction with row lock
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		// Lock the user row
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userId).
			First(&user).Error
		if err != nil {
			return err
		}

		// Check if user already checked in today
		if user.LastCheckinTime > 0 && isSameDay(user.LastCheckinTime, now) {
			result.Message = "今日已签到"
			result.CurrentStreak = user.CheckinStreak
			return errors.New("今日已签到")
		}

		// Calculate new streak
		var newStreak int
		if user.LastCheckinTime > 0 && isYesterday(user.LastCheckinTime, now) {
			// Consecutive day, increment streak
			newStreak = user.CheckinStreak + 1
		} else {
			// Not consecutive, reset to 1
			newStreak = 1
		}

		// Calculate quota reward
		totalQuota := dailyQuota
		bonusQuota := 0
		isStreakBonus := false

		// Check if streak bonus should be awarded
		if newStreak > 0 && newStreak%streakDays == 0 {
			bonusQuota = streakBonusQuota
			totalQuota += bonusQuota
			isStreakBonus = true
		}

		// Update user's checkin info and quota
		err = tx.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
			"last_checkin_time": now,
			"checkin_streak":    newStreak,
			"quota":             gorm.Expr("quota + ?", totalQuota),
		}).Error
		if err != nil {
			return err
		}

		result.Success = true
		result.Quota = dailyQuota
		result.BonusQuota = bonusQuota
		result.TotalQuota = totalQuota
		result.CurrentStreak = newStreak
		result.IsStreakBonus = isStreakBonus

		if isStreakBonus {
			result.Message = fmt.Sprintf("签到成功！连续签到 %d 天，获得额外奖励！", newStreak)
		} else {
			result.Message = fmt.Sprintf("签到成功！当前连续签到 %d 天", newStreak)
		}

		return nil
	})

	if err != nil {
		if result.Message == "" {
			return nil, err
		}
		// Return result with error message (e.g., already checked in)
		return result, nil
	}

	// Record checkin log
	username, _ := GetUsernameById(userId, false)
	logContent := fmt.Sprintf("每日签到奖励 %s", logger.LogQuota(result.TotalQuota))
	if result.IsStreakBonus {
		logContent = fmt.Sprintf("每日签到奖励 %s（含连续 %d 天额外奖励 %s）",
			logger.LogQuota(result.TotalQuota),
			result.CurrentStreak,
			logger.LogQuota(result.BonusQuota))
	}

	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: now,
		Type:      LogTypeCheckin,
		Content:   logContent,
		Quota:     result.TotalQuota,
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record checkin log: " + err.Error())
	}

	return result, nil
}

// GetUserCheckinStatus returns the checkin status for a user
func GetUserCheckinStatus(userId int) (*CheckinStatus, error) {
	var user User
	err := DB.Select("last_checkin_time", "checkin_streak").
		Where("id = ?", userId).
		First(&user).Error
	if err != nil {
		return nil, err
	}

	now := common.GetTimestamp()
	hasCheckedInToday := user.LastCheckinTime > 0 && isSameDay(user.LastCheckinTime, now)

	// Recalculate current streak based on last checkin time
	currentStreak := user.CheckinStreak
	if user.LastCheckinTime > 0 {
		// If not checked in yesterday and not today, streak should be 0
		if !isSameDay(user.LastCheckinTime, now) && !isYesterday(user.LastCheckinTime, now) {
			currentStreak = 0
		}
	}

	streakDays := operation_setting.GetCheckinStreakDays()
	daysUntilBonus := streakDays - (currentStreak % streakDays)
	if currentStreak == 0 {
		daysUntilBonus = streakDays
	}

	return &CheckinStatus{
		HasCheckedInToday: hasCheckedInToday,
		CurrentStreak:     currentStreak,
		LastCheckinTime:   user.LastCheckinTime,
		StreakDays:        streakDays,
		DaysUntilBonus:    daysUntilBonus,
		DailyQuota:        operation_setting.GetCheckinDailyQuota(),
		StreakBonusQuota:  operation_setting.GetCheckinStreakBonusQuota(),
	}, nil
}
