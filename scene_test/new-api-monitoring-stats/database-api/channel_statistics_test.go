// Package database_api contains integration tests for database schema completeness.
// This file focuses on channel_statistics table tests (DB-01 to DB-03).
package database_api

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDB01_ChannelStatistics_Create tests the creation and persistence of channel statistics.
//
// Test ID: DB-01
// Table: channel_statistics
// Test Scenario: 渠道统计持久化
// Operation Type: CREATE
// Acceptance Criteria: 每15分钟窗口生成新记录
// Priority: P0
//
// Test Steps:
// 1. Create a test channel
// 2. Insert multiple channel statistics records with different time windows
// 3. Verify each record is created successfully with correct data
// 4. Verify timestamps are set automatically
func TestDB01_ChannelStatistics_Create(t *testing.T) {
	suite, cleanup := SetupDatabaseSchemaSuite(t)
	defer cleanup()

	// Step 1: Create a test channel
	channel := suite.createTestChannel(t, "test-channel-db01", "gpt-4", "default")

	// Step 2: Insert multiple channel statistics records with different time windows
	now := time.Now()
	baseTime := roundToTimeWindow(now)

	testCases := []struct {
		timeWindowOffset int64 // offset in seconds from base time
		requestCount     int
		failCount        int
		totalTokens      int64
		totalQuota       int64
	}{
		{0, 100, 5, 10000, 1000},         // Current window
		{-15 * 60, 200, 10, 20000, 2000}, // 15 minutes ago
		{-30 * 60, 150, 8, 15000, 1500},  // 30 minutes ago
		{-45 * 60, 180, 12, 18000, 1800}, // 45 minutes ago
	}

	var createdStats []*model.ChannelStatistics
	for i, tc := range testCases {
		stat := &model.ChannelStatistics{
			ChannelId:       channel.Id,
			ModelName:       "gpt-4",
			TimeWindowStart: baseTime + tc.timeWindowOffset,
			RequestCount:    tc.requestCount,
			FailCount:       tc.failCount,
			TotalTokens:     tc.totalTokens,
			TotalQuota:      tc.totalQuota,
			TotalLatencyMs:  int64(tc.requestCount * 200), // Avg 200ms per request
			StreamReqCount:  tc.requestCount / 2,          // 50% stream requests
			CacheHitCount:   tc.requestCount / 4,          // 25% cache hit
			DowntimeSeconds: 0,
			UniqueUsers:     10 + i,
		}

		// Step 3: Verify each record is created successfully
		err := model.UpsertChannelStatistics(stat)
		require.NoError(t, err, "Failed to create channel statistics record %d", i)
		require.NotZero(t, stat.Id, "Channel statistics ID should be set after creation")

		// Step 4: Verify timestamps are set automatically
		assert.NotZero(t, stat.CreatedAt, "CreatedAt should be set automatically")
		assert.NotZero(t, stat.UpdatedAt, "UpdatedAt should be set automatically")
		assert.Equal(t, stat.CreatedAt, stat.UpdatedAt, "CreatedAt and UpdatedAt should be equal on creation")

		createdStats = append(createdStats, stat)
	}

	// Verify all records were created
	count, err := model.CountChannelStatistics()
	require.NoError(t, err, "Failed to count channel statistics")
	assert.GreaterOrEqual(t, int(count), len(testCases), "All records should be created")

	// Verify we can retrieve each record by ID
	for i, stat := range createdStats {
		var retrieved model.ChannelStatistics
		err := suite.DB.First(&retrieved, stat.Id).Error
		require.NoError(t, err, "Failed to retrieve channel statistics record %d", i)

		assert.Equal(t, stat.ChannelId, retrieved.ChannelId)
		assert.Equal(t, stat.ModelName, retrieved.ModelName)
		assert.Equal(t, stat.TimeWindowStart, retrieved.TimeWindowStart)
		assert.Equal(t, stat.RequestCount, retrieved.RequestCount)
		assert.Equal(t, stat.FailCount, retrieved.FailCount)
		assert.Equal(t, stat.TotalTokens, retrieved.TotalTokens)
		assert.Equal(t, stat.TotalQuota, retrieved.TotalQuota)
	}

	t.Logf("✓ DB-01 passed: Created and verified %d channel statistics records", len(testCases))
}

// TestDB02_ChannelStatistics_Query tests querying channel historical data.
//
// Test ID: DB-02
// Table: channel_statistics
// Test Scenario: 查询渠道历史数据
// Operation Type: READ
// Acceptance Criteria: 按 channel_id, model_name, time_window_start 正确检索
// Priority: P0
//
// Test Steps:
// 1. Create a test channel
// 2. Insert multiple statistics records with different models and time windows
// 3. Query by channel_id only - should return all records for the channel
// 4. Query by channel_id and model_name - should return only matching model records
// 5. Query by channel_id and time range - should return records within time range
// 6. Query with all filters combined - should return precise results
func TestDB02_ChannelStatistics_Query(t *testing.T) {
	suite, cleanup := SetupDatabaseSchemaSuite(t)
	defer cleanup()

	// Step 1: Create a test channel
	channel := suite.createTestChannel(t, "test-channel-db02", "gpt-4,gpt-3.5-turbo", "default")

	// Step 2: Insert multiple statistics records with different models and time windows
	now := time.Now()
	baseTime := roundToTimeWindow(now)

	testData := []struct {
		modelName    string
		timeOffset   int64 // seconds from base time
		requestCount int
	}{
		{"gpt-4", 0, 100},                // Current window, gpt-4
		{"gpt-4", -15 * 60, 150},         // 15 min ago, gpt-4
		{"gpt-4", -30 * 60, 200},         // 30 min ago, gpt-4
		{"gpt-3.5-turbo", 0, 80},         // Current window, gpt-3.5
		{"gpt-3.5-turbo", -15 * 60, 120}, // 15 min ago, gpt-3.5
		{"gpt-3.5-turbo", -45 * 60, 90},  // 45 min ago, gpt-3.5
	}

	for _, td := range testData {
		stat := &model.ChannelStatistics{
			ChannelId:       channel.Id,
			ModelName:       td.modelName,
			TimeWindowStart: baseTime + td.timeOffset,
			RequestCount:    td.requestCount,
			FailCount:       td.requestCount / 10,
			TotalTokens:     int64(td.requestCount * 100),
			TotalQuota:      int64(td.requestCount * 10),
		}
		err := model.UpsertChannelStatistics(stat)
		require.NoError(t, err, "Failed to create test data")
	}

	// Step 3: Query by channel_id only
	allStats, err := model.GetChannelStatistics(channel.Id, "", 0, 0)
	require.NoError(t, err, "Failed to query all channel statistics")
	assert.Len(t, allStats, 6, "Should return all 6 records for the channel")

	// Step 4: Query by channel_id and model_name
	gpt4Stats, err := model.GetChannelStatistics(channel.Id, "gpt-4", 0, 0)
	require.NoError(t, err, "Failed to query gpt-4 statistics")
	assert.Len(t, gpt4Stats, 3, "Should return 3 gpt-4 records")
	for _, stat := range gpt4Stats {
		assert.Equal(t, "gpt-4", stat.ModelName, "All records should be for gpt-4")
	}

	gpt35Stats, err := model.GetChannelStatistics(channel.Id, "gpt-3.5-turbo", 0, 0)
	require.NoError(t, err, "Failed to query gpt-3.5-turbo statistics")
	assert.Len(t, gpt35Stats, 3, "Should return 3 gpt-3.5-turbo records")
	for _, stat := range gpt35Stats {
		assert.Equal(t, "gpt-3.5-turbo", stat.ModelName, "All records should be for gpt-3.5-turbo")
	}

	// Step 5: Query by channel_id and time range
	// Query for records from last 20 minutes (should include current and -15min windows)
	startTime := baseTime - 20*60
	endTime := baseTime + 60 // Include some buffer
	recentStats, err := model.GetChannelStatistics(channel.Id, "", startTime, endTime)
	require.NoError(t, err, "Failed to query recent statistics")
	assert.Len(t, recentStats, 4, "Should return 4 records within last 20 minutes")

	// Verify all returned records are within time range
	for _, stat := range recentStats {
		assert.GreaterOrEqual(t, stat.TimeWindowStart, startTime, "TimeWindowStart should be >= startTime")
		assert.LessOrEqual(t, stat.TimeWindowStart, endTime, "TimeWindowStart should be <= endTime")
	}

	// Step 6: Query with all filters combined
	// Query gpt-4 records from last 20 minutes
	filteredStats, err := model.GetChannelStatistics(channel.Id, "gpt-4", startTime, endTime)
	require.NoError(t, err, "Failed to query with all filters")
	assert.Len(t, filteredStats, 2, "Should return 2 gpt-4 records within last 20 minutes")

	// Verify precise filtering
	for _, stat := range filteredStats {
		assert.Equal(t, channel.Id, stat.ChannelId, "ChannelId should match")
		assert.Equal(t, "gpt-4", stat.ModelName, "ModelName should match")
		assert.GreaterOrEqual(t, stat.TimeWindowStart, startTime, "TimeWindowStart should be >= startTime")
		assert.LessOrEqual(t, stat.TimeWindowStart, endTime, "TimeWindowStart should be <= endTime")
	}

	// Verify results are ordered by time_window_start DESC
	if len(filteredStats) > 1 {
		for i := 0; i < len(filteredStats)-1; i++ {
			assert.GreaterOrEqual(t, filteredStats[i].TimeWindowStart, filteredStats[i+1].TimeWindowStart,
				"Results should be ordered by TimeWindowStart DESC")
		}
	}

	t.Logf("✓ DB-02 passed: Successfully queried channel statistics with various filters")
}

// TestDB03_ChannelStatistics_Update tests updating existing window data with UPSERT logic.
//
// Test ID: DB-03
// Table: channel_statistics
// Test Scenario: 更新已有窗口数据
// Operation Type: UPDATE
// Acceptance Criteria: UPSERT逻辑正确，不重复插入
// Priority: P0
//
// Test Steps:
// 1. Create a test channel
// 2. Insert an initial statistics record
// 3. Upsert the same record with updated data (same channel_id, model_name, time_window_start)
// 4. Verify only one record exists (no duplicate insertion)
// 5. Verify the data was updated correctly
// 6. Verify UpdatedAt timestamp changed but CreatedAt did not
// 7. Insert a new record with different time window
// 8. Verify both records exist
func TestDB03_ChannelStatistics_Update(t *testing.T) {
	suite, cleanup := SetupDatabaseSchemaSuite(t)
	defer cleanup()

	// Step 1: Create a test channel
	channel := suite.createTestChannel(t, "test-channel-db03", "gpt-4", "default")

	// Step 2: Insert an initial statistics record
	now := time.Now()
	timeWindow := roundToTimeWindow(now)

	initialStat := &model.ChannelStatistics{
		ChannelId:       channel.Id,
		ModelName:       "gpt-4",
		TimeWindowStart: timeWindow,
		RequestCount:    100,
		FailCount:       5,
		TotalTokens:     10000,
		TotalQuota:      1000,
		TotalLatencyMs:  20000,
		StreamReqCount:  50,
		CacheHitCount:   25,
		DowntimeSeconds: 0,
		UniqueUsers:     10,
	}

	err := model.UpsertChannelStatistics(initialStat)
	require.NoError(t, err, "Failed to create initial statistics")
	initialId := initialStat.Id
	initialCreatedAt := initialStat.CreatedAt
	require.NotZero(t, initialId, "Initial record should have ID")
	require.NotZero(t, initialCreatedAt, "Initial record should have CreatedAt")

	// Small delay to ensure UpdatedAt will be different
	time.Sleep(time.Millisecond * 100)

	// Step 3: Upsert the same record with updated data
	updatedStat := &model.ChannelStatistics{
		ChannelId:       channel.Id,
		ModelName:       "gpt-4",
		TimeWindowStart: timeWindow, // Same as initial
		RequestCount:    200,        // Updated
		FailCount:       15,         // Updated
		TotalTokens:     25000,      // Updated
		TotalQuota:      2500,       // Updated
		TotalLatencyMs:  45000,      // Updated
		StreamReqCount:  100,        // Updated
		CacheHitCount:   50,         // Updated
		DowntimeSeconds: 10,         // Updated
		UniqueUsers:     15,         // Updated
	}

	err = model.UpsertChannelStatistics(updatedStat)
	require.NoError(t, err, "Failed to upsert statistics")

	// Step 4: Verify only one record exists (no duplicate insertion)
	stats, err := model.GetChannelStatistics(channel.Id, "gpt-4", timeWindow, timeWindow)
	require.NoError(t, err, "Failed to query statistics")
	assert.Len(t, stats, 1, "Should have exactly one record (no duplicate)")

	updatedRecord := stats[0]

	// Step 5: Verify the data was updated correctly
	assert.Equal(t, channel.Id, updatedRecord.ChannelId, "ChannelId should match")
	assert.Equal(t, "gpt-4", updatedRecord.ModelName, "ModelName should match")
	assert.Equal(t, timeWindow, updatedRecord.TimeWindowStart, "TimeWindowStart should match")
	assert.Equal(t, 200, updatedRecord.RequestCount, "RequestCount should be updated")
	assert.Equal(t, 15, updatedRecord.FailCount, "FailCount should be updated")
	assert.Equal(t, int64(25000), updatedRecord.TotalTokens, "TotalTokens should be updated")
	assert.Equal(t, int64(2500), updatedRecord.TotalQuota, "TotalQuota should be updated")
	assert.Equal(t, int64(45000), updatedRecord.TotalLatencyMs, "TotalLatencyMs should be updated")
	assert.Equal(t, 100, updatedRecord.StreamReqCount, "StreamReqCount should be updated")
	assert.Equal(t, 50, updatedRecord.CacheHitCount, "CacheHitCount should be updated")
	assert.Equal(t, 10, updatedRecord.DowntimeSeconds, "DowntimeSeconds should be updated")
	assert.Equal(t, 15, updatedRecord.UniqueUsers, "UniqueUsers should be updated")

	// Step 6: Verify UpdatedAt timestamp changed but CreatedAt did not
	assert.Equal(t, initialCreatedAt, updatedRecord.CreatedAt, "CreatedAt should not change on update")
	assert.Greater(t, updatedRecord.UpdatedAt, initialCreatedAt, "UpdatedAt should be greater than initial CreatedAt")

	// Step 7: Insert a new record with different time window
	newTimeWindow := timeWindow - 15*60 // 15 minutes earlier
	newStat := &model.ChannelStatistics{
		ChannelId:       channel.Id,
		ModelName:       "gpt-4",
		TimeWindowStart: newTimeWindow, // Different time window
		RequestCount:    150,
		FailCount:       8,
		TotalTokens:     15000,
		TotalQuota:      1500,
		TotalLatencyMs:  30000,
		StreamReqCount:  75,
		CacheHitCount:   38,
		DowntimeSeconds: 0,
		UniqueUsers:     12,
	}

	err = model.UpsertChannelStatistics(newStat)
	require.NoError(t, err, "Failed to insert new statistics")

	// Step 8: Verify both records exist
	allStats, err := model.GetChannelStatistics(channel.Id, "gpt-4", 0, 0)
	require.NoError(t, err, "Failed to query all statistics")
	assert.Len(t, allStats, 2, "Should have exactly 2 records")

	// Verify we can distinguish between the two records
	foundCurrent := false
	foundPrevious := false
	for _, stat := range allStats {
		if stat.TimeWindowStart == timeWindow {
			foundCurrent = true
			assert.Equal(t, 200, stat.RequestCount, "Current window should have updated data")
		} else if stat.TimeWindowStart == newTimeWindow {
			foundPrevious = true
			assert.Equal(t, 150, stat.RequestCount, "Previous window should have new data")
		}
	}
	assert.True(t, foundCurrent, "Should find current time window record")
	assert.True(t, foundPrevious, "Should find previous time window record")

	t.Logf("✓ DB-03 passed: UPSERT logic works correctly, no duplicate records created")
}
