// Package testutil provides utilities for integration testing of the NewAPI service.
package testutil

import (
	"fmt"
	"time"
)

// --- Group Statistics Models ---

// GroupStatisticsModel represents aggregated statistics for a P2P group.
type GroupStatisticsModel struct {
	GroupID         int     `json:"group_id"`
	ModelName       string  `json:"model_name"`
	TimeWindowStart int64   `json:"time_window_start"`
	TPM             int     `json:"tpm"`
	RPM             int     `json:"rpm"`
	QuotaPM         int64   `json:"quota_pm"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalQuota      int64   `json:"total_quota"`
	FailRate        float64 `json:"fail_rate"`
	AvgResponseTime int     `json:"avg_response_time"`
	AvgConcurrency  float64 `json:"avg_concurrency"`
	TotalSessions   int64   `json:"total_sessions"`
	UniqueUsers     int     `json:"unique_users"`
	UpdatedAt       int64   `json:"updated_at"`
}

// ChannelStatisticsModel represents statistics for a channel in a time window.
type ChannelStatisticsModel struct {
	ID              int    `json:"id,omitempty"`
	ChannelID       int    `json:"channel_id"`
	ModelName       string `json:"model_name"`
	TimeWindowStart int64  `json:"time_window_start"`
	RequestCount    int    `json:"request_count"`
	FailCount       int    `json:"fail_count"`
	TotalTokens     int64  `json:"total_tokens"`
	TotalQuota      int64  `json:"total_quota"`
	TotalLatencyMs  int64  `json:"total_latency_ms"`
	StreamReqCount  int    `json:"stream_req_count"`
	CacheHitCount   int    `json:"cache_hit_count"`
	DowntimeSeconds int    `json:"downtime_seconds"`
	CreatedAt       int64  `json:"created_at,omitempty"`
	UpdatedAt       int64  `json:"updated_at,omitempty"`
}

// --- Channel Statistics API Methods ---

// CreateChannelStatistics creates a channel statistics record.
// This is used to simulate channel statistics data for testing group aggregation.
func (c *APIClient) CreateChannelStatistics(stats *ChannelStatisticsModel) error {
	var resp APIResponse

	err := c.PostJSON("/api/internal/channel_statistics", stats, &resp)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("create channel statistics failed: %s", resp.Message)
	}

	return nil
}

// GetChannelStatistics retrieves channel statistics for a specific channel and model.
func (c *APIClient) GetChannelStatistics(channelID int, modelName string, startTime, endTime int64) ([]*ChannelStatisticsModel, error) {
	var resp struct {
		Success bool                      `json:"success"`
		Message string                    `json:"message"`
		Data    []*ChannelStatisticsModel `json:"data"`
	}

	path := fmt.Sprintf("/api/internal/channel_statistics?channel_id=%d&model_name=%s&start_time=%d&end_time=%d",
		channelID, modelName, startTime, endTime)

	err := c.GetJSON(path, &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get channel statistics failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// --- Group Statistics API Methods ---

// GetGroupStatistics retrieves aggregated statistics for a P2P group.
func (c *APIClient) GetGroupStatistics(groupID int, modelName string) (*GroupStatisticsModel, error) {
	var resp struct {
		Success bool                  `json:"success"`
		Message string                `json:"message"`
		Data    *GroupStatisticsModel `json:"data"`
	}

	path := fmt.Sprintf("/api/p2p_groups/%d/stats", groupID)
	if modelName != "" {
		path += fmt.Sprintf("?model=%s", modelName)
	}

	err := c.GetJSON(path, &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get group statistics failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// GetGroupStatisticsHistory retrieves historical statistics for a P2P group.
func (c *APIClient) GetGroupStatisticsHistory(groupID int, modelName string, startTime, endTime int64) ([]*GroupStatisticsModel, error) {
	var resp struct {
		Success bool                    `json:"success"`
		Message string                  `json:"message"`
		Data    []*GroupStatisticsModel `json:"data"`
	}

	path := fmt.Sprintf("/api/p2p_groups/%d/stats/history?model=%s&start_time=%d&end_time=%d",
		groupID, modelName, startTime, endTime)

	err := c.GetJSON(path, &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get group statistics history failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// TriggerGroupAggregation manually triggers aggregation for a specific group.
// This is a testing/admin API to force aggregation without waiting for the event-driven trigger.
func (c *APIClient) TriggerGroupAggregation(groupID int) error {
	var resp APIResponse

	path := fmt.Sprintf("/api/internal/groups/%d/trigger_aggregation", groupID)
	err := c.PostJSON(path, nil, &resp)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("trigger group aggregation failed: %s", resp.Message)
	}

	return nil
}

// GetGroupAggregationStatus retrieves the aggregation status for a group.
// This includes last update time and whether an aggregation is in progress.
func (c *APIClient) GetGroupAggregationStatus(groupID int) (map[string]interface{}, error) {
	var resp struct {
		Success bool                   `json:"success"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}

	path := fmt.Sprintf("/api/internal/groups/%d/aggregation_status", groupID)
	err := c.GetJSON(path, &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get group aggregation status failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// --- Helper Functions for Testing ---

// CreateTestChannelStatistics creates channel statistics with reasonable defaults for testing.
func CreateTestChannelStatistics(channelID int, modelName string, requestCount, failCount int, totalTokens int64) *ChannelStatisticsModel {
	now := time.Now().Unix()
	windowStart := (now / 900) * 900 // Round down to 15-minute window

	avgLatency := 100 // Default 100ms per request
	if requestCount > 0 {
		avgLatency = 100 + (requestCount % 50) // Vary latency slightly
	}

	return &ChannelStatisticsModel{
		ChannelID:       channelID,
		ModelName:       modelName,
		TimeWindowStart: windowStart,
		RequestCount:    requestCount,
		FailCount:       failCount,
		TotalTokens:     totalTokens,
		TotalQuota:      totalTokens * 10, // Assume 1 token = 10 quota units
		TotalLatencyMs:  int64(avgLatency * requestCount),
		StreamReqCount:  requestCount / 2, // Assume half are streaming
		CacheHitCount:   requestCount / 4, // Assume 25% cache hit
		DowntimeSeconds: 0,
	}
}

// CalculateExpectedFailRate calculates the expected weighted average fail rate for group aggregation.
func CalculateExpectedFailRate(channelStats []*ChannelStatisticsModel) float64 {
	if len(channelStats) == 0 {
		return 0.0
	}

	totalWeightedFailRate := 0.0
	totalWeight := 0

	for _, stat := range channelStats {
		if stat.RequestCount > 0 {
			failRate := float64(stat.FailCount) / float64(stat.RequestCount) * 100.0
			totalWeightedFailRate += failRate * float64(stat.RequestCount)
			totalWeight += stat.RequestCount
		}
	}

	if totalWeight == 0 {
		return 0.0
	}

	return totalWeightedFailRate / float64(totalWeight)
}

// CalculateExpectedAvgResponseTime calculates the expected weighted average response time.
func CalculateExpectedAvgResponseTime(channelStats []*ChannelStatisticsModel) int {
	if len(channelStats) == 0 {
		return 0
	}

	totalWeightedLatency := int64(0)
	totalWeight := 0

	for _, stat := range channelStats {
		if stat.RequestCount > 0 {
			avgLatency := stat.TotalLatencyMs / int64(stat.RequestCount)
			totalWeightedLatency += avgLatency * int64(stat.RequestCount)
			totalWeight += stat.RequestCount
		}
	}

	if totalWeight == 0 {
		return 0
	}

	return int(totalWeightedLatency / int64(totalWeight))
}

// SumChannelTPM calculates the sum of TPM across channels.
func SumChannelTPM(channelStats []*ChannelStatisticsModel) int {
	total := int64(0)
	for _, stat := range channelStats {
		total += stat.TotalTokens
	}
	// TPM = total tokens in the window / window duration in minutes
	// Assuming 15-minute window
	return int(total / 15)
}

// SumChannelRPM calculates the sum of RPM across channels.
func SumChannelRPM(channelStats []*ChannelStatisticsModel) int {
	total := 0
	for _, stat := range channelStats {
		total += stat.RequestCount
	}
	// RPM = total requests in the window / window duration in minutes
	return total / 15
}

// WaitForGroupAggregation waits for group aggregation to complete.
// It polls the aggregation status and waits for the updated_at timestamp to be recent.
func (c *APIClient) WaitForGroupAggregation(groupID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		stats, err := c.GetGroupStatistics(groupID, "")
		if err == nil && stats != nil && stats.UpdatedAt > 0 {
			// Check if the update is recent (within last 5 seconds)
			if time.Now().Unix()-stats.UpdatedAt < 5 {
				return nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for group aggregation")
}
