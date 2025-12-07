package model

import (
	"testing"
	"time"

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
					if !types.IsNewAPIError(err, &newAPIErr) {
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
	return len(s) >= len(substr) && (s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestUpdateChannelTimeWindowQuota tests quota update function
func TestUpdateChannelTimeWindowQuota(t *testing.T) {
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
