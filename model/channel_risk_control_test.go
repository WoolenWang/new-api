package model

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// TestTimeWindowKeyGeneration tests the Redis key generation for different time windows
func TestTimeWindowKeyGeneration(t *testing.T) {
	channelId := 123

	// Test hourly key
	hourlyKey := getTimeBucketKey(channelId, TimeWindowHourly)
	if len(hourlyKey) == 0 {
		t.Error("Hourly key should not be empty")
	}
	t.Logf("Hourly key: %s", hourlyKey)

	// Test daily key
	dailyKey := getTimeBucketKey(channelId, TimeWindowDaily)
	if len(dailyKey) == 0 {
		t.Error("Daily key should not be empty")
	}
	t.Logf("Daily key: %s", dailyKey)

	// Test weekly key
	weeklyKey := getTimeBucketKey(channelId, TimeWindowWeekly)
	if len(weeklyKey) == 0 {
		t.Error("Weekly key should not be empty")
	}
	t.Logf("Weekly key: %s", weeklyKey)

	// Test monthly key
	monthlyKey := getTimeBucketKey(channelId, TimeWindowMonthly)
	if len(monthlyKey) == 0 {
		t.Error("Monthly key should not be empty")
	}
	t.Logf("Monthly key: %s", monthlyKey)

	// Verify keys are different
	keys := []string{hourlyKey, dailyKey, weeklyKey, monthlyKey}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] == keys[j] {
				t.Errorf("Keys should be unique: %s == %s", keys[i], keys[j])
			}
		}
	}
}

// TestTTLForWindow tests TTL values for different time windows
func TestTTLForWindow(t *testing.T) {
	tests := []struct {
		window      TimeWindow
		expectedMin time.Duration
		expectedMax time.Duration
	}{
		{TimeWindowHourly, 1 * time.Hour, 3 * time.Hour},
		{TimeWindowDaily, 20 * time.Hour, 30 * time.Hour},
		{TimeWindowWeekly, 7 * 24 * time.Hour, 9 * 24 * time.Hour},
		{TimeWindowMonthly, 30 * 24 * time.Hour, 35 * 24 * time.Hour},
	}

	for _, tt := range tests {
		ttl := getTTLForWindow(tt.window)
		if ttl < tt.expectedMin || ttl > tt.expectedMax {
			t.Errorf("TTL for %s out of expected range: got %v, expected between %v and %v",
				tt.window, ttl, tt.expectedMin, tt.expectedMax)
		}
		t.Logf("TTL for %s: %v", tt.window, ttl)
	}
}

// TestCheckChannelRiskControl tests the unified risk control function
func TestCheckChannelRiskControl(t *testing.T) {
	tests := []struct {
		name           string
		channel        *Channel
		estimatedQuota int64
		shouldPass     bool
		expectedError  string
	}{
		{
			name: "Channel with no limits should pass",
			channel: &Channel{
				Id:                1,
				TotalQuota:        0,
				Concurrency:       0,
				HourlyQuotaLimit:  0,
				DailyQuotaLimit:   0,
				WeeklyQuotaLimit:  0,
				MonthlyQuotaLimit: 0,
			},
			estimatedQuota: 2500,
			shouldPass:     true,
		},
		{
			name: "Channel exceeding total quota should fail",
			channel: &Channel{
				Id:         2,
				TotalQuota: 1000,
			},
			estimatedQuota: 2500,
			shouldPass:     false,
			expectedError:  "总额度限制",
		},
		{
			name: "Channel with hourly limit and current usage should be checked",
			channel: &Channel{
				Id:               3,
				HourlyQuotaLimit: 10000,
			},
			estimatedQuota: 2500,
			shouldPass:     true, // Should pass as we haven't set up actual usage
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize stats for the channel
			stats := GetChannelUsageStats(tt.channel.Id)
			stats.mu.Lock()
			// For test 2, set used quota to exceed limit
			if tt.channel.Id == 2 {
				stats.UsedQuota = 5000
			}
			stats.mu.Unlock()

			err := CheckChannelRiskControl(tt.channel, tt.estimatedQuota)

			if tt.shouldPass {
				if err != nil {
					t.Errorf("Expected check to pass, but got error: %v", err)
				}
			} else {
				if err == nil {
					t.Error("Expected check to fail, but it passed")
				} else {
					// Verify error contains expected text
					var newAPIErr *types.NewAPIError
					if !errors.As(err, &newAPIErr) {
						t.Errorf("Expected NewAPIError, got: %T", err)
					}
					if tt.expectedError != "" && !contains(err.Error(), tt.expectedError) {
						t.Errorf("Error message '%s' does not contain expected text '%s'",
							err.Error(), tt.expectedError)
					}
					t.Logf("Got expected error: %v", err)
				}
			}

			// Cleanup
			ResetChannelUsageStats(tt.channel.Id)
		})
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// TestUpdateChannelTimeWindowQuota tests quota update function
func TestUpdateChannelTimeWindowQuota(t *testing.T) {
	common.RedisEnabled = false
	common.RDB = nil
	// Note: This test will only work if Redis is enabled
	// If Redis is not available, the function should gracefully degrade to memory
	channelId := 999
	quota := int64(1000)

	err := UpdateChannelTimeWindowQuota(channelId, quota)
	if err != nil {
		t.Errorf("UpdateChannelTimeWindowQuota failed: %v", err)
	}

	// Verify that in-memory stats were updated (legacy counters)
	stats := GetChannelUsageStats(channelId)
	stats.mu.RLock()
	hourlyReqs := stats.HourlyRequests
	dailyReqs := stats.DailyRequests
	stats.mu.RUnlock()

	if hourlyReqs == 0 {
		t.Error("Hourly request counter should be incremented")
	}
	if dailyReqs == 0 {
		t.Error("Daily request counter should be incremented")
	}

	t.Logf("Stats after update - Hourly: %d, Daily: %d", hourlyReqs, dailyReqs)

	// Cleanup
	ResetChannelUsageStats(channelId)
}

// TestTimeBucketTransition tests time window boundary transitions
func TestTimeBucketTransition(t *testing.T) {
	channelId := 888
	common.RedisEnabled = false

	// Test hourly bucket transition
	stats := GetChannelUsageStats(channelId)
	oldBucket := getCurrentTimeBucket(TimeWindowHourly)

	stats.mu.Lock()
	stats.HourlyQuotaBucket = oldBucket
	stats.HourlyQuotaUsed = 5000
	stats.mu.Unlock()

	// Simulate checking quota in same bucket
	used1, _ := getQuotaUsedInWindow(channelId, TimeWindowHourly)
	if used1 != 5000 {
		t.Errorf("Expected quota 5000, got %d", used1)
	}

	// Simulate bucket change by setting a different bucket
	stats.mu.Lock()
	stats.HourlyQuotaBucket = "2025120700" // Old bucket
	stats.HourlyQuotaUsed = 5000
	stats.mu.Unlock()

	// Should reset to 0 when bucket changes
	used2, _ := getQuotaUsedInWindow(channelId, TimeWindowHourly)
	if used2 != 0 {
		t.Errorf("Expected quota to reset to 0 on bucket change, got %d", used2)
	}

	t.Logf("Bucket transition test - Old: %d, New (after reset): %d", used1, used2)
	ResetChannelUsageStats(channelId)
}

// TestQuotaAtExactThreshold tests behavior when quota exactly equals limit
func TestQuotaAtExactThreshold(t *testing.T) {
	tests := []struct {
		name           string
		channel        *Channel
		currentUsed    int64
		estimatedQuota int64
		shouldPass     bool
	}{
		{
			name: "Quota exactly at total limit should fail",
			channel: &Channel{
				Id:         100,
				TotalQuota: 10000,
			},
			currentUsed:    10000,
			estimatedQuota: 1,
			shouldPass:     false,
		},
		{
			name: "Quota just below limit should pass",
			channel: &Channel{
				Id:         101,
				TotalQuota: 10000,
			},
			currentUsed:    9999,
			estimatedQuota: 1,
			shouldPass:     true,
		},
		{
			name: "Estimated quota would exactly reach limit should fail",
			channel: &Channel{
				Id:         102,
				TotalQuota: 10000,
			},
			currentUsed:    9500,
			estimatedQuota: 501,
			shouldPass:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := GetChannelUsageStats(tt.channel.Id)
			stats.mu.Lock()
			stats.UsedQuota = tt.currentUsed
			stats.mu.Unlock()

			err := CheckChannelRiskControl(tt.channel, tt.estimatedQuota)

			if tt.shouldPass && err != nil {
				t.Errorf("Expected pass, but got error: %v", err)
			}
			if !tt.shouldPass && err == nil {
				t.Error("Expected failure at threshold, but check passed")
			}

			ResetChannelUsageStats(tt.channel.Id)
		})
	}
}

// TestMultipleTimeWindowLimits tests channel with limits in multiple dimensions
func TestMultipleTimeWindowLimits(t *testing.T) {
	common.RedisEnabled = false

	channel := &Channel{
		Id:                200,
		TotalQuota:        100000,
		HourlyQuotaLimit:  5000,
		DailyQuotaLimit:   50000,
		WeeklyQuotaLimit:  200000,
		MonthlyQuotaLimit: 500000,
	}

	// Should pass with no usage
	err := CheckChannelRiskControl(channel, 2500)
	if err != nil {
		t.Errorf("Should pass with no usage, got: %v", err)
	}

	// Simulate hourly limit reached
	stats := GetChannelUsageStats(channel.Id)
	bucket := getCurrentTimeBucket(TimeWindowHourly)
	stats.mu.Lock()
	stats.HourlyQuotaBucket = bucket
	stats.HourlyQuotaUsed = 5000
	stats.mu.Unlock()

	err = CheckChannelRiskControl(channel, 100)
	if err == nil {
		t.Error("Should fail when hourly limit exceeded")
	} else {
		var newAPIErr *types.NewAPIError
		if errors.As(err, &newAPIErr) {
			if newAPIErr.GetErrorCode() != types.ErrorCodeChannelHourlyLimitExceeded {
				t.Errorf("Expected hourly limit error, got: %v", newAPIErr.GetErrorCode())
			}
		}
		t.Logf("Correctly blocked by hourly limit: %v", err)
	}

	ResetChannelUsageStats(channel.Id)
}

// TestConcurrencyLimit tests concurrent request blocking
func TestConcurrencyLimit(t *testing.T) {
	channel := &Channel{
		Id:          300,
		Concurrency: 5,
	}

	stats := GetChannelUsageStats(channel.Id)

	// Simulate 5 concurrent requests
	for i := 0; i < 5; i++ {
		IncrementChannelConcurrency(channel.Id)
	}

	// 6th request should be blocked
	err := CheckChannelRiskControl(channel, 2500)
	if err == nil {
		t.Error("Should fail when concurrency limit reached")
	} else {
		var newAPIErr *types.NewAPIError
		if errors.As(err, &newAPIErr) {
			if newAPIErr.GetErrorCode() != types.ErrorCodeChannelConcurrencyExceeded {
				t.Errorf("Expected concurrency error, got: %v", newAPIErr.GetErrorCode())
			}
		}
		t.Logf("Correctly blocked by concurrency limit: %v", err)
	}

	// Cleanup - release all concurrent requests
	for i := 0; i < 5; i++ {
		DecrementChannelConcurrency(channel.Id)
	}

	stats.mu.RLock()
	finalConcurrency := stats.CurrentConcurrency
	stats.mu.RUnlock()

	if finalConcurrency != 0 {
		t.Errorf("Expected concurrency to be 0 after cleanup, got %d", finalConcurrency)
	}

	ResetChannelUsageStats(channel.Id)
}

// TestWeeklyAndMonthlyQuotaTracking tests longer time window tracking
func TestWeeklyAndMonthlyQuotaTracking(t *testing.T) {
	common.RedisEnabled = false
	channelId := 400

	// Test weekly quota update
	weeklyBucket := getCurrentTimeBucket(TimeWindowWeekly)
	t.Logf("Current weekly bucket: %s", weeklyBucket)

	stats := GetChannelUsageStats(channelId)
	stats.mu.Lock()
	stats.WeeklyQuotaBucket = weeklyBucket
	stats.WeeklyQuotaUsed = 15000
	stats.mu.Unlock()

	weeklyUsed, _ := getQuotaUsedInWindow(channelId, TimeWindowWeekly)
	if weeklyUsed != 15000 {
		t.Errorf("Expected weekly quota 15000, got %d", weeklyUsed)
	}

	// Test monthly quota update
	monthlyBucket := getCurrentTimeBucket(TimeWindowMonthly)
	t.Logf("Current monthly bucket: %s", monthlyBucket)

	stats.mu.Lock()
	stats.MonthlyQuotaBucket = monthlyBucket
	stats.MonthlyQuotaUsed = 80000
	stats.mu.Unlock()

	monthlyUsed, _ := getQuotaUsedInWindow(channelId, TimeWindowMonthly)
	if monthlyUsed != 80000 {
		t.Errorf("Expected monthly quota 80000, got %d", monthlyUsed)
	}

	t.Logf("Weekly/Monthly tracking - Weekly: %d, Monthly: %d", weeklyUsed, monthlyUsed)
	ResetChannelUsageStats(channelId)
}

// TestZeroAndNegativeQuota tests edge cases with zero and negative values
func TestZeroAndNegativeQuota(t *testing.T) {
	channelId := 500

	// Test zero quota update (should not error, but should not increment)
	err := UpdateChannelTimeWindowQuota(channelId, 0)
	if err != nil {
		t.Errorf("Zero quota update should not error: %v", err)
	}

	// Test negative quota (should not update)
	err = UpdateChannelTimeWindowQuota(channelId, -100)
	if err != nil {
		t.Errorf("Negative quota should not error: %v", err)
	}

	stats := GetChannelUsageStats(channelId)
	stats.mu.RLock()
	requests := stats.HourlyRequests
	stats.mu.RUnlock()

	// Negative/zero should not increment request counters
	if requests != 0 {
		t.Errorf("Expected no request increment for zero/negative quota, got %d", requests)
	}

	ResetChannelUsageStats(channelId)
}
