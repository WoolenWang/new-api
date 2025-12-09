package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// CheckinSetting defines the configuration for daily check-in feature
type CheckinSetting struct {
	Enabled          bool `json:"enabled"`            // Whether check-in feature is enabled
	DailyQuota       int  `json:"daily_quota"`        // Daily check-in reward quota
	StreakBonusQuota int  `json:"streak_bonus_quota"` // Bonus quota for 7-day streak
	StreakDays       int  `json:"streak_days"`        // Number of days for streak bonus (default 7)
}

// Default configuration
var checkinSetting = CheckinSetting{
	Enabled:          true,
	DailyQuota:       1000, // Default 1000 quota per day
	StreakBonusQuota: 5000, // Default 5000 bonus for 7-day streak
	StreakDays:       7,    // Default 7 days for streak
}

func init() {
	// Register to global config manager
	config.GlobalConfig.Register("checkin_setting", &checkinSetting)
}

func GetCheckinSetting() *CheckinSetting {
	return &checkinSetting
}

func IsCheckinEnabled() bool {
	return checkinSetting.Enabled
}

func GetCheckinDailyQuota() int {
	return checkinSetting.DailyQuota
}

func GetCheckinStreakBonusQuota() int {
	return checkinSetting.StreakBonusQuota
}

func GetCheckinStreakDays() int {
	if checkinSetting.StreakDays <= 0 {
		return 7 // Default to 7 days
	}
	return checkinSetting.StreakDays
}
