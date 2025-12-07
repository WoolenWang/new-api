package model

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/go-redis/redis/v8"
)

// ChannelUsageStats tracks real-time usage statistics for channels
type ChannelUsageStats struct {
	ChannelId          int
	UsedQuota          int64     // Total quota used (累计使用额度)
	CurrentConcurrency int       // Current concurrent requests (当前并发数)
	HourlyRequests     int       // Requests in current hour (当前小时请求数) - Legacy, for backward compatibility
	DailyRequests      int       // Requests in current day (当前天请求数) - Legacy, for backward compatibility
	HourStartTime      time.Time // Start of current hour window
	DayStartTime       time.Time // Start of current day window

	// In-memory quota counters used as a fallback when Redis is unavailable.
	// These are per-time-window, quota-based (单位与 UsedQuota 一致), and keyed by time bucket.
	HourlyQuotaUsed    int64
	HourlyQuotaBucket  string
	DailyQuotaUsed     int64
	DailyQuotaBucket   string
	WeeklyQuotaUsed    int64
	WeeklyQuotaBucket  string
	MonthlyQuotaUsed   int64
	MonthlyQuotaBucket string

	mu sync.RWMutex
}

var (
	channelUsageStats = make(map[int]*ChannelUsageStats)
	usageStatsMutex   sync.RWMutex
)

// TimeWindow represents different time periods for quota tracking
type TimeWindow string

const (
	TimeWindowHourly  TimeWindow = "hourly"
	TimeWindowDaily   TimeWindow = "daily"
	TimeWindowWeekly  TimeWindow = "weekly"
	TimeWindowMonthly TimeWindow = "monthly"
)

// getCurrentTimeBucket returns the logical time bucket string for a given window
// (e.g. hour "2025120708", day "20251207", week "2025_W49", month "202512").
func getCurrentTimeBucket(window TimeWindow) string {
	now := time.Now()
	switch window {
	case TimeWindowHourly:
		return now.Format("2006010215")
	case TimeWindowDaily:
		return now.Format("20060102")
	case TimeWindowWeekly:
		year, week := now.ISOWeek()
		return fmt.Sprintf("%d_W%02d", year, week)
	case TimeWindowMonthly:
		return now.Format("200601")
	default:
		return "unknown"
	}
}

// getTimeBucketKey generates Redis key for time-window quota tracking
// Format: channel_quota:{channel_id}:{period}:{timestamp_bucket}
func getTimeBucketKey(channelId int, window TimeWindow) string {
	bucket := getCurrentTimeBucket(window)
	return fmt.Sprintf("channel_quota:%d:%s:%s", channelId, window, bucket)
}

// getTTLForWindow returns appropriate TTL for each time window
func getTTLForWindow(window TimeWindow) time.Duration {
	switch window {
	case TimeWindowHourly:
		return 2 * time.Hour // Keep for 2 hours to handle edge cases
	case TimeWindowDaily:
		return 26 * time.Hour // Keep for 26 hours
	case TimeWindowWeekly:
		return 8 * 24 * time.Hour // Keep for 8 days
	case TimeWindowMonthly:
		return 32 * 24 * time.Hour // Keep for 32 days
	default:
		return 1 * time.Hour
	}
}

// getQuotaUsedInWindow retrieves the quota consumed in a specific time window from Redis.
// Falls back to an in-memory, quota-based counter if Redis is unavailable.
func getQuotaUsedInWindow(channelId int, window TimeWindow) (int64, error) {
	// Primary: Try Redis
	if common.RedisEnabled && common.RDB != nil {
		key := getTimeBucketKey(channelId, window)
		ctx := context.Background()

		val, err := common.RDB.Get(ctx, key).Result()
		if err == redis.Nil {
			// Key doesn't exist, meaning no quota used yet in this window
			return 0, nil
		}
		if err != nil {
			common.SysLog(fmt.Sprintf("Redis GET failed for key %s: %v, falling back to memory", key, err))
			// Fall through to memory-based tracking
		} else {
			// Successfully retrieved from Redis
			quotaUsed, parseErr := strconv.ParseInt(val, 10, 64)
			if parseErr != nil {
				common.SysLog(fmt.Sprintf("Failed to parse Redis quota value for key %s: %v", key, parseErr))
				return 0, parseErr
			}
			return quotaUsed, nil
		}
	}

	// Fallback: Use in-memory quota tracking (less accurate in multi-node deployments)
	stats := GetChannelUsageStats(channelId)
	stats.mu.Lock()
	defer stats.mu.Unlock()

	bucket := getCurrentTimeBucket(window)
	var used int64

	switch window {
	case TimeWindowHourly:
		if stats.HourlyQuotaBucket != bucket {
			stats.HourlyQuotaBucket = bucket
			stats.HourlyQuotaUsed = 0
		}
		used = stats.HourlyQuotaUsed
	case TimeWindowDaily:
		if stats.DailyQuotaBucket != bucket {
			stats.DailyQuotaBucket = bucket
			stats.DailyQuotaUsed = 0
		}
		used = stats.DailyQuotaUsed
	case TimeWindowWeekly:
		if stats.WeeklyQuotaBucket != bucket {
			stats.WeeklyQuotaBucket = bucket
			stats.WeeklyQuotaUsed = 0
		}
		used = stats.WeeklyQuotaUsed
	case TimeWindowMonthly:
		if stats.MonthlyQuotaBucket != bucket {
			stats.MonthlyQuotaBucket = bucket
			stats.MonthlyQuotaUsed = 0
		}
		used = stats.MonthlyQuotaUsed
	default:
		return 0, fmt.Errorf("unknown time window: %s", window)
	}

	common.SysLog(fmt.Sprintf("Warning: Redis disabled/unavailable for channel %d window %s, using in-memory quota fallback (used=%d)", channelId, window, used))
	return used, nil
}

// CheckChannelRiskControl checks if channel passes risk control limits
// This function now supports ALL channel types (platform + P2P) and uses unified quota-based limits
// Returns error if channel exceeds any limits
func CheckChannelRiskControl(channel *Channel, estimatedQuota int64) error {
	stats := GetChannelUsageStats(channel.Id)

	// Check 1: Total quota limit (applies to all channels)
	if channel.TotalQuota > 0 {
		stats.mu.RLock()
		currentUsed := stats.UsedQuota
		stats.mu.RUnlock()

		if currentUsed >= channel.TotalQuota {
			return types.NewError(
				fmt.Errorf("渠道已达到总额度限制: %d/%d", currentUsed, channel.TotalQuota),
				types.ErrorCodeChannelTotalQuotaExceeded,
			)
		}

		// Check if adding estimated quota would exceed limit
		if currentUsed+estimatedQuota > channel.TotalQuota {
			return types.NewError(
				fmt.Errorf("渠道额度不足，预计消耗: %d, 剩余: %d", estimatedQuota, channel.TotalQuota-currentUsed),
				types.ErrorCodeChannelTotalQuotaExceeded,
			)
		}
	}

	// Check 2: Concurrency limit (applies to all channels)
	if channel.Concurrency > 0 {
		stats.mu.RLock()
		currentConcurrency := stats.CurrentConcurrency
		stats.mu.RUnlock()

		if currentConcurrency >= channel.Concurrency {
			return types.NewError(
				fmt.Errorf("渠道已达到并发数限制: %d/%d", currentConcurrency, channel.Concurrency),
				types.ErrorCodeChannelConcurrencyExceeded,
			)
		}
	}

	// Check 3: Time-based quota limits (NEW - applies to all channels)
	// Check hourly quota limit
	if channel.HourlyQuotaLimit > 0 {
		hourlyUsed, err := getQuotaUsedInWindow(channel.Id, TimeWindowHourly)
		if err != nil {
			common.SysLog(fmt.Sprintf("Failed to get hourly quota for channel %d: %v", channel.Id, err))
			// Continue checking other limits even if this one fails
		} else {
			if hourlyUsed >= channel.HourlyQuotaLimit {
				return types.NewError(
					fmt.Errorf("渠道已达到每小时额度限制: %d/%d", hourlyUsed, channel.HourlyQuotaLimit),
					types.ErrorCodeChannelHourlyLimitExceeded,
				)
			}
			// Pre-check: would this request exceed the limit?
			if hourlyUsed+estimatedQuota > channel.HourlyQuotaLimit {
				return types.NewError(
					fmt.Errorf("请求将超出每小时额度限制，预计消耗: %d, 剩余: %d", estimatedQuota, channel.HourlyQuotaLimit-hourlyUsed),
					types.ErrorCodeChannelHourlyLimitExceeded,
				)
			}
		}
	}

	// Check daily quota limit
	if channel.DailyQuotaLimit > 0 {
		dailyUsed, err := getQuotaUsedInWindow(channel.Id, TimeWindowDaily)
		if err != nil {
			common.SysLog(fmt.Sprintf("Failed to get daily quota for channel %d: %v", channel.Id, err))
		} else {
			if dailyUsed >= channel.DailyQuotaLimit {
				return types.NewError(
					fmt.Errorf("渠道已达到每日额度限制: %d/%d", dailyUsed, channel.DailyQuotaLimit),
					types.ErrorCodeChannelDailyLimitExceeded,
				)
			}
			if dailyUsed+estimatedQuota > channel.DailyQuotaLimit {
				return types.NewError(
					fmt.Errorf("请求将超出每日额度限制，预计消耗: %d, 剩余: %d", estimatedQuota, channel.DailyQuotaLimit-dailyUsed),
					types.ErrorCodeChannelDailyLimitExceeded,
				)
			}
		}
	}

	// Check weekly quota limit
	if channel.WeeklyQuotaLimit > 0 {
		weeklyUsed, err := getQuotaUsedInWindow(channel.Id, TimeWindowWeekly)
		if err != nil {
			common.SysLog(fmt.Sprintf("Failed to get weekly quota for channel %d: %v", channel.Id, err))
		} else {
			if weeklyUsed >= channel.WeeklyQuotaLimit {
				return types.NewError(
					fmt.Errorf("渠道已达到每周额度限制: %d/%d", weeklyUsed, channel.WeeklyQuotaLimit),
					types.ErrorCodeChannelDailyLimitExceeded, // Reuse daily limit error code
				)
			}
			if weeklyUsed+estimatedQuota > channel.WeeklyQuotaLimit {
				return types.NewError(
					fmt.Errorf("请求将超出每周额度限制，预计消耗: %d, 剩余: %d", estimatedQuota, channel.WeeklyQuotaLimit-weeklyUsed),
					types.ErrorCodeChannelDailyLimitExceeded,
				)
			}
		}
	}

	// Check monthly quota limit
	if channel.MonthlyQuotaLimit > 0 {
		monthlyUsed, err := getQuotaUsedInWindow(channel.Id, TimeWindowMonthly)
		if err != nil {
			common.SysLog(fmt.Sprintf("Failed to get monthly quota for channel %d: %v", channel.Id, err))
		} else {
			if monthlyUsed >= channel.MonthlyQuotaLimit {
				return types.NewError(
					fmt.Errorf("渠道已达到每月额度限制: %d/%d", monthlyUsed, channel.MonthlyQuotaLimit),
					types.ErrorCodeChannelDailyLimitExceeded,
				)
			}
			if monthlyUsed+estimatedQuota > channel.MonthlyQuotaLimit {
				return types.NewError(
					fmt.Errorf("请求将超出每月额度限制，预计消耗: %d, 剩余: %d", estimatedQuota, channel.MonthlyQuotaLimit-monthlyUsed),
					types.ErrorCodeChannelDailyLimitExceeded,
				)
			}
		}
	}

	// Legacy: Check request-count-based limits (for backward compatibility with P2P channels)
	// These checks only apply if the new quota-based limits are not set
	if channel.HourlyQuotaLimit == 0 && channel.HourlyLimit > 0 {
		stats.mu.RLock()
		now := time.Now()
		hourlyReqs := stats.HourlyRequests
		if now.Sub(stats.HourStartTime) >= time.Hour {
			hourlyReqs = 0 // Window expired
		}
		stats.mu.RUnlock()

		if hourlyReqs >= channel.HourlyLimit {
			return types.NewError(
				fmt.Errorf("渠道已达到每小时请求数限制: %d/%d", hourlyReqs, channel.HourlyLimit),
				types.ErrorCodeChannelHourlyLimitExceeded,
			)
		}
	}

	if channel.DailyQuotaLimit == 0 && channel.DailyLimit > 0 {
		stats.mu.RLock()
		now := time.Now()
		dailyReqs := stats.DailyRequests
		if now.Truncate(24 * time.Hour).After(stats.DayStartTime) {
			dailyReqs = 0
		}
		stats.mu.RUnlock()

		if dailyReqs >= channel.DailyLimit {
			return types.NewError(
				fmt.Errorf("渠道已达到每日请求数限制: %d/%d", dailyReqs, channel.DailyLimit),
				types.ErrorCodeChannelDailyLimitExceeded,
			)
		}
	}

	return nil
}

// GetChannelUsageStats retrieves or creates usage stats for a channel
func GetChannelUsageStats(channelId int) *ChannelUsageStats {
	usageStatsMutex.RLock()
	stats, exists := channelUsageStats[channelId]
	usageStatsMutex.RUnlock()

	if exists {
		return stats
	}

	// Create new stats
	usageStatsMutex.Lock()
	defer usageStatsMutex.Unlock()

	// Double check after acquiring write lock
	if stats, exists := channelUsageStats[channelId]; exists {
		return stats
	}

	// Load persisted used_quota from database
	var persistedQuota int64 = 0
	var channel Channel
	if DB != nil {
		err := DB.Select("used_quota").Where("id = ?", channelId).First(&channel).Error
		if err == nil {
			persistedQuota = channel.UsedQuota
			common.SysLog(fmt.Sprintf("Loaded persisted used_quota for channel #%d: %d", channelId, persistedQuota))
		} else {
			common.SysLog(fmt.Sprintf("Warning: Failed to load used_quota for channel #%d, starting from 0: %v", channelId, err))
		}
	} else {
		common.SysLog(fmt.Sprintf("Warning: DB is nil when loading used_quota for channel #%d, starting from 0", channelId))
	}

	now := time.Now()
	stats = &ChannelUsageStats{
		ChannelId:          channelId,
		UsedQuota:          persistedQuota, // Initialize from database
		CurrentConcurrency: 0,
		HourlyRequests:     0,
		DailyRequests:      0,
		HourStartTime:      now,
		DayStartTime:       now.Truncate(24 * time.Hour),
	}
	channelUsageStats[channelId] = stats
	return stats
}

// UpdateChannelTimeWindowQuota atomically increments quota usage in all time windows
// This function is called after a request completes with exact quota consumption
func UpdateChannelTimeWindowQuota(channelId int, quota int64) error {
	if quota <= 0 {
		return nil // Nothing to update
	}

	// Primary: Update Redis counters
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		txn := common.RDB.TxPipeline()

		// Update all four time windows atomically
		windows := []TimeWindow{TimeWindowHourly, TimeWindowDaily, TimeWindowWeekly, TimeWindowMonthly}
		for _, window := range windows {
			key := getTimeBucketKey(channelId, window)
			ttl := getTTLForWindow(window)

			// INCRBY atomically increments the counter
			txn.IncrBy(ctx, key, quota)
			// Set TTL (will only apply if key was just created, otherwise keeps existing TTL)
			txn.Expire(ctx, key, ttl)
		}

		_, err := txn.Exec(ctx)
		if err != nil {
			common.SysLog(fmt.Sprintf("Failed to update Redis quota counters for channel %d: %v", channelId, err))
			// Fall through to memory update as fallback
		} else {
			if common.DebugEnabled {
				common.SysLog(fmt.Sprintf("Updated time-window quota for channel %d: +%d", channelId, quota))
			}
		}
	}

	// Always update in-memory statistics so that, when Redis is unavailable,
	// we still have a quota-based time-window view for risk control.
	stats := GetChannelUsageStats(channelId)
	stats.mu.Lock()

	// Update per-window quota buckets
	for _, window := range []TimeWindow{TimeWindowHourly, TimeWindowDaily, TimeWindowWeekly, TimeWindowMonthly} {
		bucket := getCurrentTimeBucket(window)
		switch window {
		case TimeWindowHourly:
			if stats.HourlyQuotaBucket != bucket {
				stats.HourlyQuotaBucket = bucket
				stats.HourlyQuotaUsed = 0
			}
			stats.HourlyQuotaUsed += quota
		case TimeWindowDaily:
			if stats.DailyQuotaBucket != bucket {
				stats.DailyQuotaBucket = bucket
				stats.DailyQuotaUsed = 0
			}
			stats.DailyQuotaUsed += quota
		case TimeWindowWeekly:
			if stats.WeeklyQuotaBucket != bucket {
				stats.WeeklyQuotaBucket = bucket
				stats.WeeklyQuotaUsed = 0
			}
			stats.WeeklyQuotaUsed += quota
		case TimeWindowMonthly:
			if stats.MonthlyQuotaBucket != bucket {
				stats.MonthlyQuotaBucket = bucket
				stats.MonthlyQuotaUsed = 0
			}
			stats.MonthlyQuotaUsed += quota
		}
	}
	stats.mu.Unlock()

	// Also update legacy request-count based counters for backward compatibility
	IncrementChannelRequest(channelId)

	return nil
}

// IncrementChannelConcurrency increments the concurrent request count
func IncrementChannelConcurrency(channelId int) {
	stats := GetChannelUsageStats(channelId)
	stats.mu.Lock()
	defer stats.mu.Unlock()
	stats.CurrentConcurrency++
}

// DecrementChannelConcurrency decrements the concurrent request count
func DecrementChannelConcurrency(channelId int) {
	stats := GetChannelUsageStats(channelId)
	stats.mu.Lock()
	defer stats.mu.Unlock()
	if stats.CurrentConcurrency > 0 {
		stats.CurrentConcurrency--
	}
}

// IncrementChannelRequest increments request counters with time window reset
func IncrementChannelRequest(channelId int) {
	stats := GetChannelUsageStats(channelId)
	stats.mu.Lock()
	defer stats.mu.Unlock()

	now := time.Now()

	// Reset hourly counter if hour has changed
	if now.Sub(stats.HourStartTime) >= time.Hour {
		stats.HourlyRequests = 0
		stats.HourStartTime = now
	}

	// Reset daily counter if day has changed
	if now.Truncate(24 * time.Hour).After(stats.DayStartTime) {
		stats.DailyRequests = 0
		stats.DayStartTime = now.Truncate(24 * time.Hour)
	}

	stats.HourlyRequests++
	stats.DailyRequests++
}

// AddChannelUsedQuota adds used quota to channel statistics and persists to DB
func AddChannelUsedQuota(channelId int, quota int64) {
	stats := GetChannelUsageStats(channelId)
	stats.mu.Lock()
	stats.UsedQuota += quota
	newUsedQuota := stats.UsedQuota
	stats.mu.Unlock()

	// Asynchronously update database to avoid blocking
	go func() {
		err := DB.Model(&Channel{}).Where("id = ?", channelId).Update("used_quota", newUsedQuota).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Error updating used_quota for channel #%d: %v", channelId, err))
		}
	}()
}

// GetChannelUsedQuota retrieves current used quota for a channel
func GetChannelUsedQuota(channelId int) int64 {
	stats := GetChannelUsageStats(channelId)
	stats.mu.RLock()
	defer stats.mu.RUnlock()
	return stats.UsedQuota
}

// ResetChannelUsageStats resets all usage statistics for a channel
func ResetChannelUsageStats(channelId int) {
	usageStatsMutex.Lock()
	defer usageStatsMutex.Unlock()
	delete(channelUsageStats, channelId)
	common.SysLog(fmt.Sprintf("Reset usage stats for channel #%d", channelId))
}

// GetAllChannelUsageStats returns a snapshot of all channel usage statistics
func GetAllChannelUsageStats() map[int]*ChannelUsageStats {
	usageStatsMutex.RLock()
	defer usageStatsMutex.RUnlock()

	snapshot := make(map[int]*ChannelUsageStats)
	for id, stats := range channelUsageStats {
		stats.mu.RLock()
		snapshot[id] = &ChannelUsageStats{
			ChannelId:          stats.ChannelId,
			UsedQuota:          stats.UsedQuota,
			CurrentConcurrency: stats.CurrentConcurrency,
			HourlyRequests:     stats.HourlyRequests,
			DailyRequests:      stats.DailyRequests,
			HourStartTime:      stats.HourStartTime,
			DayStartTime:       stats.DayStartTime,
		}
		stats.mu.RUnlock()
	}
	return snapshot
}

// SyncAllChannelUsedQuotaToDB synchronizes all in-memory used_quota to database
// This is a safety mechanism to ensure data persistence, called periodically or on shutdown
func SyncAllChannelUsedQuotaToDB() error {
	usageStatsMutex.RLock()
	defer usageStatsMutex.RUnlock()

	if len(channelUsageStats) == 0 {
		return nil
	}

	// Build batch update data
	type quotaUpdate struct {
		ChannelId int
		UsedQuota int64
	}
	updates := make([]quotaUpdate, 0, len(channelUsageStats))

	for id, stats := range channelUsageStats {
		stats.mu.RLock()
		updates = append(updates, quotaUpdate{
			ChannelId: id,
			UsedQuota: stats.UsedQuota,
		})
		stats.mu.RUnlock()
	}

	// Execute batch updates
	for _, u := range updates {
		err := DB.Model(&Channel{}).Where("id = ?", u.ChannelId).Update("used_quota", u.UsedQuota).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Error syncing used_quota for channel #%d: %v", u.ChannelId, err))
		}
	}

	common.SysLog(fmt.Sprintf("Synced used_quota for %d channels to database", len(updates)))
	return nil
}

// GetChannelConcurrencySnapshot returns a simplified snapshot of concurrency information for all channels
// This is specifically designed for management API consumption
func GetChannelConcurrencySnapshot() map[int]int {
	usageStatsMutex.RLock()
	defer usageStatsMutex.RUnlock()

	snapshot := make(map[int]int)
	for id, stats := range channelUsageStats {
		stats.mu.RLock()
		snapshot[id] = stats.CurrentConcurrency
		stats.mu.RUnlock()
	}
	return snapshot
}
