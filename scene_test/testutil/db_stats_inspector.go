// Package testutil - Database Statistics Helper for Testing
//
// This file provides utilities to directly query and verify channel statistics
// in the database (L3 layer).
//
// Features:
// - Query channel_statistics table
// - Verify statistics records
// - Calculate aggregated metrics
// - Test data cleanup
package testutil

import (
	"database/sql"
	"fmt"
	"time"
)

// DBStatsInspector provides utilities to inspect database statistics.
type DBStatsInspector struct {
	db *sql.DB
}

// NewDBStatsInspector creates a new database inspector.
//
// Note: In actual test environment, this would receive a connection to the
// test database (in-memory SQLite).
func NewDBStatsInspector(dbPath string) (*DBStatsInspector, error) {
	// For SQLite in-memory database used in tests.
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &DBStatsInspector{
		db: db,
	}, nil
}

// Close closes the database connection.
func (d *DBStatsInspector) Close() error {
	return d.db.Close()
}

// ChannelStatisticsRecord represents a record in channel_statistics table.
type ChannelStatisticsRecord struct {
	ID              int64
	ChannelID       int
	ModelName       string
	TimeWindowStart int64
	RequestCount    int
	FailCount       int
	TotalTokens     int64
	TotalQuota      int64
	TotalLatencyMS  int64
	StreamReqCount  int
	CacheHitCount   int
	DowntimeSeconds int
}

// QueryChannelStatistics queries the channel_statistics table.
//
// Returns all records for a channel and model within a time range.
func (d *DBStatsInspector) QueryChannelStatistics(channelID int, modelName string, startTime, endTime int64) ([]ChannelStatisticsRecord, error) {
	query := `
		SELECT id, channel_id, model_name, time_window_start,
		       request_count, fail_count, total_tokens, total_quota,
		       total_latency_ms, stream_req_count, cache_hit_count, downtime_seconds
		FROM channel_statistics
		WHERE channel_id = ? AND model_name = ?
		  AND time_window_start >= ? AND time_window_start < ?
		ORDER BY time_window_start DESC
	`

	rows, err := d.db.Query(query, channelID, modelName, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query channel_statistics: %w", err)
	}
	defer rows.Close()

	var records []ChannelStatisticsRecord
	for rows.Next() {
		var r ChannelStatisticsRecord
		err := rows.Scan(
			&r.ID, &r.ChannelID, &r.ModelName, &r.TimeWindowStart,
			&r.RequestCount, &r.FailCount, &r.TotalTokens, &r.TotalQuota,
			&r.TotalLatencyMS, &r.StreamReqCount, &r.CacheHitCount, &r.DowntimeSeconds,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		records = append(records, r)
	}

	return records, rows.Err()
}

// GetLatestChannelStatistics retrieves the most recent statistics record.
func (d *DBStatsInspector) GetLatestChannelStatistics(channelID int, modelName string) (*ChannelStatisticsRecord, error) {
	now := time.Now().Unix()
	oneHourAgo := now - 3600

	records, err := d.QueryChannelStatistics(channelID, modelName, oneHourAgo, now+3600)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("no statistics record found for channel %d, model %s", channelID, modelName)
	}

	return &records[0], nil
}

// WaitForStatisticsRecord waits for a statistics record to appear in the database.
func (d *DBStatsInspector) WaitForStatisticsRecord(channelID int, modelName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		_, err := d.GetLatestChannelStatistics(channelID, modelName)
		if err == nil {
			return nil // Record found
		}

		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("timeout waiting for statistics record for channel %d, model %s", channelID, modelName)
}

// CountStatisticsRecords counts the number of statistics records.
func (d *DBStatsInspector) CountStatisticsRecords(channelID int, modelName string, timeWindowStart int64) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM channel_statistics
		WHERE channel_id = ? AND model_name = ? AND time_window_start = ?
	`

	var count int
	err := d.db.QueryRow(query, channelID, modelName, timeWindowStart).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count records: %w", err)
	}

	return count, nil
}

// VerifyNoDuplicateRecords verifies there are no duplicate records for a time window.
func (d *DBStatsInspector) VerifyNoDuplicateRecords(channelID int, modelName string, timeWindowStart int64) error {
	count, err := d.CountStatisticsRecords(channelID, modelName, timeWindowStart)
	if err != nil {
		return err
	}

	if count > 1 {
		return fmt.Errorf("duplicate records detected: found %d records for channel %d, model %s, window %d",
			count, channelID, modelName, timeWindowStart)
	}

	return nil
}

// CalculateAggregatedMetrics calculates aggregated metrics from multiple records.
func (d *DBStatsInspector) CalculateAggregatedMetrics(records []ChannelStatisticsRecord) *ChannelStatsModel {
	if len(records) == 0 {
		return &ChannelStatsModel{}
	}

	stats := &ChannelStatsModel{
		ChannelID: records[0].ChannelID,
		ModelName: records[0].ModelName,
	}

	var totalRequests, totalFails, totalStreamReqs, totalCacheHits int
	var totalTokens, totalQuota, totalLatency int64
	var totalDowntime int64

	for _, r := range records {
		totalRequests += r.RequestCount
		totalFails += r.FailCount
		totalTokens += r.TotalTokens
		totalQuota += r.TotalQuota
		totalLatency += r.TotalLatencyMS
		totalStreamReqs += r.StreamReqCount
		totalCacheHits += r.CacheHitCount
		totalDowntime += int64(r.DowntimeSeconds)
	}

	stats.RequestCount = totalRequests
	stats.FailCount = totalFails
	stats.TotalTokens = totalTokens
	stats.TotalQuota = totalQuota

	if totalRequests > 0 {
		stats.FailRate = float64(totalFails) / float64(totalRequests) * 100
		stats.AvgResponseTime = int(totalLatency / int64(totalRequests))
		stats.StreamReqRatio = float64(totalStreamReqs) / float64(totalRequests) * 100
		stats.CacheHitRate = float64(totalCacheHits) / float64(totalRequests) * 100
	}

	// Calculate TPM, RPM based on time range.
	if len(records) > 0 {
		firstWindow := records[len(records)-1].TimeWindowStart
		lastWindow := records[0].TimeWindowStart
		durationMinutes := float64(lastWindow-firstWindow) / 60.0

		if durationMinutes > 0 {
			stats.TPM = int(float64(totalTokens) / durationMinutes)
			stats.RPM = int(float64(totalRequests) / durationMinutes)
			stats.QuotaPM = int64(float64(totalQuota) / durationMinutes)
		}
	}

	return stats
}

// DeleteChannelStatistics deletes all statistics records for a channel.
func (d *DBStatsInspector) DeleteChannelStatistics(channelID int, modelName string) error {
	query := `DELETE FROM channel_statistics WHERE channel_id = ? AND model_name = ?`
	_, err := d.db.Exec(query, channelID, modelName)
	return err
}

// InsertChannelStatistics inserts a test statistics record.
//
// This is used for testing query and aggregation logic.
func (d *DBStatsInspector) InsertChannelStatistics(record *ChannelStatisticsRecord) error {
	query := `
		INSERT INTO channel_statistics (
			channel_id, model_name, time_window_start,
			request_count, fail_count, total_tokens, total_quota,
			total_latency_ms, stream_req_count, cache_hit_count, downtime_seconds
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := d.db.Exec(query,
		record.ChannelID, record.ModelName, record.TimeWindowStart,
		record.RequestCount, record.FailCount, record.TotalTokens, record.TotalQuota,
		record.TotalLatencyMS, record.StreamReqCount, record.CacheHitCount, record.DowntimeSeconds,
	)

	return err
}

// VerifyStatisticsAggregation verifies that statistics were correctly aggregated.
//
// Compares database records with expected values.
func (d *DBStatsInspector) VerifyStatisticsAggregation(channelID int, modelName string, expected *ChannelStatisticsRecord) error {
	actual, err := d.GetLatestChannelStatistics(channelID, modelName)
	if err != nil {
		return fmt.Errorf("failed to get latest statistics: %w", err)
	}

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

	return nil
}

// GetTimeWindowRecords retrieves all records for a specific time window.
func (d *DBStatsInspector) GetTimeWindowRecords(timeWindowStart int64) ([]ChannelStatisticsRecord, error) {
	query := `
		SELECT id, channel_id, model_name, time_window_start,
		       request_count, fail_count, total_tokens, total_quota,
		       total_latency_ms, stream_req_count, cache_hit_count, downtime_seconds
		FROM channel_statistics
		WHERE time_window_start = ?
		ORDER BY channel_id, model_name
	`

	rows, err := d.db.Query(query, timeWindowStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ChannelStatisticsRecord
	for rows.Next() {
		var r ChannelStatisticsRecord
		err := rows.Scan(
			&r.ID, &r.ChannelID, &r.ModelName, &r.TimeWindowStart,
			&r.RequestCount, &r.FailCount, &r.TotalTokens, &r.TotalQuota,
			&r.TotalLatencyMS, &r.StreamReqCount, &r.CacheHitCount, &r.DowntimeSeconds,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}

	return records, rows.Err()
}

// WaitForWindowSync waits for a specific time window's data to be synced to DB.
func (d *DBStatsInspector) WaitForWindowSync(channelID int, modelName string, timeWindowStart int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		count, err := d.CountStatisticsRecords(channelID, modelName, timeWindowStart)
		if err != nil {
			return err
		}
		if count > 0 {
			return nil
		}

		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("timeout waiting for window %d to sync for channel %d, model %s",
		timeWindowStart, channelID, modelName)
}
