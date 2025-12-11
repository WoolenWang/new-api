// Package testutil - Channel Statistics Test Helper Functions
//
// This file provides helper functions specifically for channel statistics testing,
// including utilities for:
// - Statistics data verification
// - Cache state inspection (L1/L2/L3)
// - Waiting for asynchronous operations
// - Mock data preparation
// - Performance measurement
package testutil

import (
	"fmt"
	"time"
)

// ChannelStatsModel represents channel statistics data structure.
type ChannelStatsModel struct {
	ChannelID       int     `json:"channel_id"`
	ModelName       string  `json:"model_name"`
	RequestCount    int     `json:"request_count"`
	FailCount       int     `json:"fail_count"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalQuota      int64   `json:"total_quota"`
	AvgResponseTime int     `json:"avg_response_time"`
	FailRate        float64 `json:"fail_rate"`
	CacheHitRate    float64 `json:"avg_cache_hit_rate"`
	StreamReqRatio  float64 `json:"stream_req_ratio"`
	TPM             int     `json:"tpm"`
	RPM             int     `json:"rpm"`
	QuotaPM         int64   `json:"quota_pm"`
	UniqueUsers     int     `json:"unique_users"`
	AvgConcurrency  float64 `json:"avg_concurrency"`
	DowntimePercent float64 `json:"downtime_percentage"`
	TotalSessions   int64   `json:"total_sessions"`
	TimeWindowStart int64   `json:"time_window_start"`
}

// GetChannelStats queries channel statistics via API.
//
// Parameters:
//   - channelID: Channel ID to query
//   - period: Time window (e.g., "1h", "6h", "7d", "30d")
//   - model: Model name filter (empty string for all models)
//
// Returns channel statistics or error.
func (c *APIClient) GetChannelStats(channelID int, period, model string) (*ChannelStatsModel, error) {
	path := fmt.Sprintf("/api/channels/%d/stats?period=%s", channelID, period)
	if model != "" {
		path += fmt.Sprintf("&model=%s", model)
	}

	var response struct {
		Success bool               `json:"success"`
		Message string             `json:"message"`
		Data    *ChannelStatsModel `json:"data"`
	}

	if err := c.GetJSON(path, &response); err != nil {
		return nil, fmt.Errorf("failed to get channel stats: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned error: %s", response.Message)
	}

	return response.Data, nil
}

// WaitForStatisticsAggregation waits for statistics to be aggregated.
//
// This function waits for the three-level cache flow:
// - L1 (memory) flush to L2 (Redis): ~1 minute
// - L2 (Redis) sync to L3 (DB): ~15 minutes
//
// Parameters:
//   - stage: "L1_L2" (wait 65s) or "L2_L3" (wait 16min) or "full" (wait both)
func WaitForStatisticsAggregation(stage string) time.Duration {
	switch stage {
	case "L1_L2":
		duration := 65 * time.Second
		time.Sleep(duration)
		return duration
	case "L2_L3":
		duration := 16 * time.Minute
		time.Sleep(duration)
		return duration
	case "full":
		l1l2 := 65 * time.Second
		time.Sleep(l1l2)
		l2l3 := 16 * time.Minute
		time.Sleep(l2l3)
		return l1l2 + l2l3
	default:
		// Default: wait for L1 to L2 only
		duration := 65 * time.Second
		time.Sleep(duration)
		return duration
	}
}

// VerifyStatisticsAccuracy verifies statistics accuracy against expected values.
//
// Parameters:
//   - actual: Actual statistics from API
//   - expected: Expected statistics
//   - tolerance: Tolerance for floating-point comparisons (e.g., 0.01 for 1%)
//
// Returns error if any metric is outside tolerance.
func VerifyStatisticsAccuracy(actual, expected *ChannelStatsModel, tolerance float64) error {
	if actual.RequestCount != expected.RequestCount {
		return fmt.Errorf("request_count mismatch: expected %d, got %d",
			expected.RequestCount, actual.RequestCount)
	}

	if actual.TotalTokens != expected.TotalTokens {
		return fmt.Errorf("total_tokens mismatch: expected %d, got %d",
			expected.TotalTokens, actual.TotalTokens)
	}

	if actual.TotalQuota != expected.TotalQuota {
		return fmt.Errorf("total_quota mismatch: expected %d, got %d",
			expected.TotalQuota, actual.TotalQuota)
	}

	// Floating-point comparisons with tolerance.
	if diff := abs(actual.FailRate - expected.FailRate); diff > tolerance {
		return fmt.Errorf("fail_rate mismatch: expected %.2f, got %.2f (diff %.2f > tolerance %.2f)",
			expected.FailRate, actual.FailRate, diff, tolerance)
	}

	if diff := abs(actual.CacheHitRate - expected.CacheHitRate); diff > tolerance {
		return fmt.Errorf("cache_hit_rate mismatch: expected %.2f, got %.2f",
			expected.CacheHitRate, actual.CacheHitRate)
	}

	if diff := abs(actual.StreamReqRatio - expected.StreamReqRatio); diff > tolerance {
		return fmt.Errorf("stream_req_ratio mismatch: expected %.2f, got %.2f",
			expected.StreamReqRatio, actual.StreamReqRatio)
	}

	return nil
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// LogEntry represents a simplified log entry for testing.
// Field tags are aligned with model.Log JSON tags so we can decode
// responses from /api/log/self correctly.
type LogEntry struct {
	ID               int    `json:"id"`
	UserID           int    `json:"user_id"`
	Type             int    `json:"type"`
	ModelName        string `json:"model_name"`
	TokenName        string `json:"token_name"`
	TokenID          int    `json:"token_id"`
	ChannelID        int    `json:"channel"`
	Quota            int    `json:"quota"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	CreatedAt        int64  `json:"created_at"`
}

// GetUserLogs queries recent logs for the current user via /api/log/self.
// The userID parameter is kept for backward compatibility but is not used;
// the endpoint always returns logs for the authenticated user.
func (c *APIClient) GetUserLogs(userID, limit int) ([]LogEntry, error) {
	path := fmt.Sprintf("/api/log/self?p=1&page_size=%d", limit)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Page     int        `json:"page"`
			PageSize int        `json:"page_size"`
			Total    int        `json:"total"`
			Items    []LogEntry `json:"items"`
		} `json:"data"`
	}

	if err := c.GetJSON(path, &response); err != nil {
		return nil, fmt.Errorf("failed to get user logs: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned error: %s", response.Message)
	}

	return response.Data.Items, nil
}

// ChannelStatsInspector provides utilities to inspect channel statistics cache
// state at each level (L1/L2/L3). It is intentionally separate from the
// P2P group CacheInspector defined in cache_inspector.go to avoid name
// collisions and to keep concerns isolated.
type ChannelStatsInspector struct {
	// Note: Actual implementation would need access to internal cache state
	// via test hooks, reflection, or debug endpoints.
}

// InspectL1Memory inspects L1 memory cache state.
//
// Returns counter values for a specific channel and model.
// Note: Requires test hooks in the relay package.
func (ci *ChannelStatsInspector) InspectL1Memory(channelID int, modelName string) (map[string]int64, error) {
	// Placeholder: In real implementation, would access internal sync.Map
	return nil, fmt.Errorf("not implemented - requires internal state access")
}

// InspectL2Redis inspects L2 Redis cache state.
//
// Returns Hash fields for channel statistics in Redis.
func (ci *ChannelStatsInspector) InspectL2Redis(channelID int, modelName string) (map[string]string, error) {
	// Placeholder: In real implementation, would query Redis directly
	return nil, fmt.Errorf("not implemented - requires Redis client access")
}

// InspectL3Database inspects L3 database statistics records.
//
// Returns the latest statistics record for a channel and model.
func (ci *ChannelStatsInspector) InspectL3Database(channelID int, modelName string) (*ChannelStatsModel, error) {
	// Placeholder: In real implementation, would query channel_statistics table
	return nil, fmt.Errorf("not implemented - requires DB access")
}

// PerformanceMetrics holds performance measurement results.
type PerformanceMetrics struct {
	TotalRequests  int
	SuccessfulReqs int
	FailedReqs     int
	ElapsedTime    time.Duration
	AvgLatency     time.Duration
	Throughput     float64 // requests per second
	P50Latency     time.Duration
	P95Latency     time.Duration
	P99Latency     time.Duration
}

// MeasurePerformance measures performance metrics for a series of requests.
func MeasurePerformance(requestFunc func() error, numRequests int) *PerformanceMetrics {
	metrics := &PerformanceMetrics{
		TotalRequests: numRequests,
	}

	latencies := make([]time.Duration, 0, numRequests)
	startTime := time.Now()

	for i := 0; i < numRequests; i++ {
		reqStart := time.Now()
		err := requestFunc()
		reqDuration := time.Since(reqStart)

		latencies = append(latencies, reqDuration)

		if err == nil {
			metrics.SuccessfulReqs++
		} else {
			metrics.FailedReqs++
		}
	}

	metrics.ElapsedTime = time.Since(startTime)
	metrics.Throughput = float64(metrics.SuccessfulReqs) / metrics.ElapsedTime.Seconds()

	// Calculate average latency.
	var totalLatency time.Duration
	for _, lat := range latencies {
		totalLatency += lat
	}
	if len(latencies) > 0 {
		metrics.AvgLatency = totalLatency / time.Duration(len(latencies))
	}

	// Calculate percentiles (simplified).
	// Note: A proper implementation would sort and calculate exact percentiles.
	if len(latencies) > 0 {
		metrics.P50Latency = latencies[len(latencies)/2]
		metrics.P95Latency = latencies[len(latencies)*95/100]
		metrics.P99Latency = latencies[len(latencies)*99/100]
	}

	return metrics
}

// WaitForCondition waits for a condition to become true, with timeout.
//
// Parameters:
//   - timeout: Maximum time to wait
//   - checkInterval: How often to check the condition
//   - condition: Function that returns true when condition is met
//
// Returns error if timeout expires before condition is met.
func WaitForCondition(timeout, checkInterval time.Duration, condition func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return nil
		}
		time.Sleep(checkInterval)
	}
	return fmt.Errorf("timeout waiting for condition after %v", timeout)
}

// WaitForDBSync waits for channel statistics to be synced to database.
//
// Parameters:
//   - channelID: Channel to wait for
//   - model: Model name
//   - maxWait: Maximum time to wait
//
// This function polls the database (via API or direct query) until statistics appear.
func WaitForDBSync(client *APIClient, channelID int, model string, maxWait time.Duration) error {
	return WaitForCondition(maxWait, 10*time.Second, func() bool {
		// Try to get statistics via API.
		stats, err := client.GetChannelStats(channelID, "1h", model)
		if err != nil {
			return false
		}
		// Check if statistics are present (not all zeros).
		return stats != nil && stats.RequestCount > 0
	})
}

// CalculateExpectedQuota calculates expected quota based on tokens and rate.
//
// Parameters:
//   - tokens: Total tokens (prompt + completion)
//   - rate: Billing rate multiplier
//
// Returns expected quota value.
func CalculateExpectedQuota(tokens int, rate float64) int {
	// Note: Actual calculation depends on system configuration.
	// This is a simplified version.
	return int(float64(tokens) * rate)
}

// GroupStatsModel represents P2P group statistics.
type GroupStatsModel struct {
	GroupID         int     `json:"group_id"`
	ModelName       string  `json:"model_name"`
	TPM             int     `json:"tpm"`
	RPM             int     `json:"rpm"`
	QuotaPM         int64   `json:"quota_pm"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalQuota      int64   `json:"total_quota"`
	AvgConcurrency  float64 `json:"avg_concurrency"`
	TotalSessions   int64   `json:"total_sessions"`
	DowntimePercent float64 `json:"downtime_percentage"`
	UniqueUsers     int     `json:"unique_users"`
	UpdatedAt       int64   `json:"updated_at"`
}

// GetGroupStats queries P2P group statistics via API.
func (c *APIClient) GetGroupStats(groupID int, model string) (*GroupStatsModel, error) {
	path := fmt.Sprintf("/api/p2p_groups/%d/stats", groupID)
	if model != "" {
		path += fmt.Sprintf("?model=%s", model)
	}

	var response struct {
		Success bool             `json:"success"`
		Message string           `json:"message"`
		Data    *GroupStatsModel `json:"data"`
	}

	if err := c.GetJSON(path, &response); err != nil {
		return nil, fmt.Errorf("failed to get group stats: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned error: %s", response.Message)
	}

	return response.Data, nil
}

// StatsSummary provides a summary of statistics for logging/debugging.
func FormatStatsSummary(stats *ChannelStatsModel) string {
	summary := fmt.Sprintf(`Channel Statistics Summary:
  Channel ID: %d
  Model: %s
  Request Count: %d
  Fail Count: %d (%.2f%%)
  Total Tokens: %d
  Total Quota: %d
  Avg Response Time: %dms
  TPM: %d
  RPM: %d
  Unique Users: %d
  Stream Ratio: %.2f%%
  Cache Hit Rate: %.2f%%
  Avg Concurrency: %.2f
  Downtime: %.2f%%`,
		stats.ChannelID,
		stats.ModelName,
		stats.RequestCount,
		stats.FailCount,
		stats.FailRate,
		stats.TotalTokens,
		stats.TotalQuota,
		stats.AvgResponseTime,
		stats.TPM,
		stats.RPM,
		stats.UniqueUsers,
		stats.StreamReqRatio,
		stats.CacheHitRate,
		stats.AvgConcurrency,
		stats.DowntimePercent,
	)
	return summary
}

// VerifyChannelStatsInDB verifies channel statistics in database directly.
//
// Note: This requires direct database access, which may not be available in all test environments.
func VerifyChannelStatsInDB(channelID int, modelName string, timeWindow int64) (*ChannelStatsModel, error) {
	// Placeholder: Would query channel_statistics table directly
	return nil, fmt.Errorf("not implemented - requires direct DB access")
}

// CalculateStatisticsMetrics calculates statistics metrics from raw log data.
//
// This helper is useful for verifying expected values before querying the API.
func CalculateStatisticsMetrics(logs []LogEntry) *ChannelStatsModel {
	if len(logs) == 0 {
		return &ChannelStatsModel{}
	}

	stats := &ChannelStatsModel{
		ChannelID: logs[0].ChannelID,
		ModelName: logs[0].ModelName,
	}

	stats.RequestCount = len(logs)
	uniqueUsers := make(map[int]bool)
	var failCount int

	for _, log := range logs {
		stats.TotalTokens += int64(log.PromptTokens + log.CompletionTokens)
		stats.TotalQuota += int64(log.Quota)
		uniqueUsers[log.UserID] = true

		// Treat non-consume logs as failures in this helper.
		if log.Type != 2 {
			failCount++
		}
	}

	stats.FailCount = failCount
	stats.UniqueUsers = len(uniqueUsers)

	if stats.RequestCount > 0 {
		stats.FailRate = float64(stats.FailCount) / float64(stats.RequestCount) * 100
		// AvgResponseTime is not derivable from LogEntry; leave as zero.
	}

	return stats
}

// CompareStats compares two statistics objects and returns differences.
func CompareStats(expected, actual *ChannelStatsModel) []string {
	differences := []string{}

	if expected.RequestCount != actual.RequestCount {
		differences = append(differences, fmt.Sprintf("RequestCount: expected %d, got %d",
			expected.RequestCount, actual.RequestCount))
	}

	if expected.TotalTokens != actual.TotalTokens {
		differences = append(differences, fmt.Sprintf("TotalTokens: expected %d, got %d",
			expected.TotalTokens, actual.TotalTokens))
	}

	if expected.TotalQuota != actual.TotalQuota {
		differences = append(differences, fmt.Sprintf("TotalQuota: expected %d, got %d",
			expected.TotalQuota, actual.TotalQuota))
	}

	if abs(expected.FailRate-actual.FailRate) > 0.01 {
		differences = append(differences, fmt.Sprintf("FailRate: expected %.2f%%, got %.2f%%",
			expected.FailRate, actual.FailRate))
	}

	if expected.UniqueUsers != actual.UniqueUsers {
		differences = append(differences, fmt.Sprintf("UniqueUsers: expected %d, got %d",
			expected.UniqueUsers, actual.UniqueUsers))
	}

	return differences
}
