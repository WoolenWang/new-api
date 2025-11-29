package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// ChannelUsageStats tracks real-time usage statistics for P2P channels
type ChannelUsageStats struct {
	ChannelId          int
	UsedQuota          int64     // Total quota used (累计使用额度)
	CurrentConcurrency int       // Current concurrent requests (当前并发数)
	HourlyRequests     int       // Requests in current hour (当前小时请求数)
	DailyRequests      int       // Requests in current day (当前天请求数)
	HourStartTime      time.Time // Start of current hour window
	DayStartTime       time.Time // Start of current day window
	mu                 sync.RWMutex
}

var (
	channelUsageStats = make(map[int]*ChannelUsageStats)
	usageStatsMutex   sync.RWMutex
)

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
	err := DB.Select("used_quota").Where("id = ?", channelId).First(&channel).Error
	if err == nil {
		persistedQuota = channel.UsedQuota
		common.SysLog(fmt.Sprintf("Loaded persisted used_quota for channel #%d: %d", channelId, persistedQuota))
	} else {
		common.SysLog(fmt.Sprintf("Warning: Failed to load used_quota for channel #%d, starting from 0: %v", channelId, err))
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

// CheckChannelRiskControl checks if channel passes risk control limits
// Returns error if channel exceeds any limits
func CheckChannelRiskControl(channel *Channel) error {
	// Only check P2P channels (channels with owner_user_id != 0)
	if channel.OwnerUserId == 0 {
		return nil // Skip risk control for public platform channels
	}

	stats := GetChannelUsageStats(channel.Id)
	stats.mu.RLock()
	defer stats.mu.RUnlock()

	// Check 1: Total quota limit
	if channel.TotalQuota > 0 {
		if stats.UsedQuota >= channel.TotalQuota {
			return fmt.Errorf("渠道已达到总额度限制: %d/%d", stats.UsedQuota, channel.TotalQuota)
		}
	}

	// Check 2: Concurrency limit
	if channel.Concurrency > 0 {
		if stats.CurrentConcurrency >= channel.Concurrency {
			return types.NewError(
				fmt.Errorf("渠道已达到并发数限制: %d/%d", stats.CurrentConcurrency, channel.Concurrency),
				types.ErrorCodeChannelConcurrencyExceeded,
			)
		}
	}

	// Check 3: Hourly rate limit
	if channel.HourlyLimit > 0 {
		now := time.Now()
		// Reset hourly counter if hour has changed
		if now.Sub(stats.HourStartTime) >= time.Hour {
			// Note: This is read-locked, actual reset happens in IncrementChannelRequest
			// We just check the old counter here
		}
		if stats.HourlyRequests >= channel.HourlyLimit {
			return fmt.Errorf("渠道已达到每小时请求数限制: %d/%d", stats.HourlyRequests, channel.HourlyLimit)
		}
	}

	// Check 4: Daily rate limit
	if channel.DailyLimit > 0 {
		now := time.Now()
		// Reset daily counter if day has changed
		if now.Truncate(24 * time.Hour).After(stats.DayStartTime) {
			// Note: This is read-locked, actual reset happens in IncrementChannelRequest
			// We just check the old counter here
		}
		if stats.DailyRequests >= channel.DailyLimit {
			return fmt.Errorf("渠道已达到每日请求数限制: %d/%d", stats.DailyRequests, channel.DailyLimit)
		}
	}

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
