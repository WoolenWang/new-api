package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// Checkin handles user daily check-in
// POST /api/user/checkin
func Checkin(c *gin.Context) {
	userId := c.GetInt("id")

	result, err := model.UserCheckin(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	if !result.Success {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": result.Message,
			"data": gin.H{
				"current_streak": result.CurrentStreak,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": result.Message,
		"data": gin.H{
			"quota":           result.Quota,
			"bonus_quota":     result.BonusQuota,
			"total_quota":     result.TotalQuota,
			"current_streak":  result.CurrentStreak,
			"is_streak_bonus": result.IsStreakBonus,
		},
	})
}

// GetCheckinStatus returns user's checkin status
// GET /api/user/checkin/status
func GetCheckinStatus(c *gin.Context) {
	userId := c.GetInt("id")

	// Check if checkin is enabled
	if !operation_setting.IsCheckinEnabled() {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"enabled": false,
			},
		})
		return
	}

	status, err := model.GetUserCheckinStatus(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"enabled":              true,
			"has_checked_in_today": status.HasCheckedInToday,
			"current_streak":       status.CurrentStreak,
			"last_checkin_time":    status.LastCheckinTime,
			"streak_days":          status.StreakDays,
			"days_until_bonus":     status.DaysUntilBonus,
			"daily_quota":          status.DailyQuota,
			"streak_bonus_quota":   status.StreakBonusQuota,
		},
	})
}
