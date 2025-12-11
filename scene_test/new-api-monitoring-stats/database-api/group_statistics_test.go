// Package database_api contains integration tests for database schema completeness.
// This file focuses on group_statistics table tests (DB-06 to DB-08).
package database_api

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDB06_GroupStatistics_Create tests the creation and persistence of group statistics.
//
// Test ID: DB-06
// Table: group_statistics
// Test Scenario: 分组聚合结果持久化
// Operation Type: CREATE
// Acceptance Criteria: 按 (group_id, model_name, time_window_start) 生成记录
// Priority: P0
//
// Test Steps:
// 1. Create a test user and P2P group
// 2. Insert multiple group statistics records with different models and time windows
// 3. Verify each record is created successfully with correct composite key
// 4. Verify UpdatedAt timestamp is set automatically
// 5. Verify we can query records by composite key
func TestDB06_GroupStatistics_Create(t *testing.T) {
	suite, cleanup := SetupDatabaseSchemaSuite(t)
	defer cleanup()

	// Step 1: Create a test user and P2P group
	testUser, err := suite.Fixtures.CreateTestUser("groupstats-owner", "defaultpass", "default")
	require.NoError(t, err, "Failed to create test user")

	group := suite.createTestP2PGroup(t, "test-group-db06", testUser.ID)

	// Step 2: Insert multiple group statistics records with different models and time windows
	now := time.Now()
	baseTime := roundToTimeWindow(now)

	testCases := []struct {
		modelName       string
		timeOffset      int64 // offset in seconds from base time
		tpm             int
		rpm             int
		failRate        float64
		avgResponseTime int
	}{
		{"gpt-4", 0, 50000, 100, 5.5, 250},                // Current window, gpt-4
		{"gpt-4", -15 * 60, 60000, 120, 4.8, 220},         // 15 min ago, gpt-4
		{"gpt-4", -30 * 60, 45000, 90, 6.2, 280},          // 30 min ago, gpt-4
		{"gpt-3.5-turbo", 0, 80000, 150, 3.2, 180},        // Current window, gpt-3.5
		{"gpt-3.5-turbo", -15 * 60, 75000, 140, 3.8, 190}, // 15 min ago, gpt-3.5
		{"claude-3-opus", 0, 40000, 80, 2.5, 200},         // Current window, claude
	}

	var createdStats []*model.GroupStatistics
	for i, tc := range testCases {
		stat := &model.GroupStatistics{
			GroupId:            group.ID,
			ModelName:          tc.modelName,
			TimeWindowStart:    baseTime + tc.timeOffset,
			TPM:                tc.tpm,
			RPM:                tc.rpm,
			FailRate:           tc.failRate,
			AvgResponseTimeMs:  tc.avgResponseTime,
			AvgCacheHitRate:    30.0,
			StreamReqRatio:     65.0,
			QuotaPM:            int64(tc.tpm / 10),
			TotalTokens:        int64(tc.tpm * 15), // 15 minutes of data
			TotalQuota:         int64(tc.tpm * 15 / 10),
			AvgConcurrency:     5.0,
			TotalSessions:      1000 + int64(i)*100,
			DowntimePercentage: 1.5,
			UniqueUsers:        50 + i*10,
		}

		// Step 3: Verify each record is created successfully
		err := model.UpsertGroupStatistics(stat)
		require.NoError(t, err, "Failed to create group statistics record %d", i)

		// Step 4: Verify UpdatedAt timestamp is set automatically
		assert.NotZero(t, stat.UpdatedAt, "UpdatedAt should be set automatically")

		createdStats = append(createdStats, stat)
	}

	// Verify all records were created
	count, err := model.CountGroupStatistics()
	require.NoError(t, err, "Failed to count group statistics")
	assert.GreaterOrEqual(t, int(count), len(testCases), "All records should be created")

	// Step 5: Verify we can query records by composite key
	for i, stat := range createdStats {
		var retrieved model.GroupStatistics
		err := suite.DB.Where("group_id = ? AND model_name = ? AND time_window_start = ?",
			stat.GroupId, stat.ModelName, stat.TimeWindowStart).
			First(&retrieved).Error
		require.NoError(t, err, "Failed to retrieve group statistics record %d", i)

		assert.Equal(t, stat.GroupId, retrieved.GroupId, "GroupId should match")
		assert.Equal(t, stat.ModelName, retrieved.ModelName, "ModelName should match")
		assert.Equal(t, stat.TimeWindowStart, retrieved.TimeWindowStart, "TimeWindowStart should match")
		assert.Equal(t, stat.TPM, retrieved.TPM, "TPM should match")
		assert.Equal(t, stat.RPM, retrieved.RPM, "RPM should match")
		assert.InDelta(t, stat.FailRate, retrieved.FailRate, 0.01, "FailRate should match")
		assert.Equal(t, stat.AvgResponseTimeMs, retrieved.AvgResponseTimeMs, "AvgResponseTimeMs should match")
	}

	// Verify composite key uniqueness - try to query all records for the group
	allStats, err := model.GetGroupStatisticsByGroupId(group.ID)
	require.NoError(t, err, "Failed to query all group statistics")
	assert.Len(t, allStats, len(testCases), "Should have exactly %d records", len(testCases))

	t.Logf("✓ DB-06 passed: Created and verified %d group statistics records with composite keys", len(testCases))
}

// TestDB07_GroupStatistics_Query tests querying group statistics.
//
// Test ID: DB-07
// Table: group_statistics
// Test Scenario: 查询分组统计
// Operation Type: READ
// Acceptance Criteria: 能按主键正确检索最新记录
// Priority: P0
//
// Test Steps:
// 1. Create a test group
// 2. Insert multiple statistics records with different models and time windows
// 3. Query by group_id only - should return all records for the group
// 4. Query by group_id and model_name - should return only matching model records
// 5. Query by group_id and time range - should return records within time range
// 6. Query latest statistics - should return most recent record
// 7. Verify results are ordered by time_window_start DESC
func TestDB07_GroupStatistics_Query(t *testing.T) {
	suite, cleanup := SetupDatabaseSchemaSuite(t)
	defer cleanup()

	// Step 1: Create a test group
	testUser, err := suite.Fixtures.CreateTestUser("groupstats-query", "defaultpass", "default")
	require.NoError(t, err, "Failed to create test user")

	group := suite.createTestP2PGroup(t, "test-group-db07", testUser.ID)

	// Step 2: Insert multiple statistics records
	now := time.Now()
	baseTime := roundToTimeWindow(now)

	testData := []struct {
		modelName  string
		timeOffset int64
		tpm        int
	}{
		{"gpt-4", 0, 50000},                // Most recent, gpt-4
		{"gpt-4", -15 * 60, 48000},         // 15 min ago, gpt-4
		{"gpt-4", -30 * 60, 52000},         // 30 min ago, gpt-4
		{"gpt-4", -45 * 60, 47000},         // 45 min ago, gpt-4
		{"gpt-3.5-turbo", 0, 80000},        // Most recent, gpt-3.5
		{"gpt-3.5-turbo", -15 * 60, 78000}, // 15 min ago, gpt-3.5
		{"gpt-3.5-turbo", -45 * 60, 76000}, // 45 min ago, gpt-3.5
	}

	for _, td := range testData {
		stat := &model.GroupStatistics{
			GroupId:           group.ID,
			ModelName:         td.modelName,
			TimeWindowStart:   baseTime + td.timeOffset,
			TPM:               td.tpm,
			RPM:               100,
			FailRate:          5.0,
			AvgResponseTimeMs: 250,
		}
		err := model.UpsertGroupStatistics(stat)
		require.NoError(t, err, "Failed to create test data")
	}

	// Step 3: Query by group_id only
	allStats, err := model.GetGroupStatistics(group.ID, "", 0, 0)
	require.NoError(t, err, "Failed to query all group statistics")
	assert.Len(t, allStats, 7, "Should return all 7 records for the group")

	// Step 4: Query by group_id and model_name
	gpt4Stats, err := model.GetGroupStatistics(group.ID, "gpt-4", 0, 0)
	require.NoError(t, err, "Failed to query gpt-4 statistics")
	assert.Len(t, gpt4Stats, 4, "Should return 4 gpt-4 records")
	for _, stat := range gpt4Stats {
		assert.Equal(t, "gpt-4", stat.ModelName, "All records should be for gpt-4")
		assert.Equal(t, group.ID, stat.GroupId, "All records should be for the correct group")
	}

	gpt35Stats, err := model.GetGroupStatistics(group.ID, "gpt-3.5-turbo", 0, 0)
	require.NoError(t, err, "Failed to query gpt-3.5-turbo statistics")
	assert.Len(t, gpt35Stats, 3, "Should return 3 gpt-3.5-turbo records")

	// Step 5: Query by group_id and time range
	// Query for records from last 20 minutes (should include current and -15min windows)
	startTime := baseTime - 20*60
	endTime := baseTime + 60
	recentStats, err := model.GetGroupStatistics(group.ID, "", startTime, endTime)
	require.NoError(t, err, "Failed to query recent statistics")
	assert.Len(t, recentStats, 4, "Should return 4 records within last 20 minutes")

	// Verify all returned records are within time range
	for _, stat := range recentStats {
		assert.GreaterOrEqual(t, stat.TimeWindowStart, startTime, "TimeWindowStart should be >= startTime")
		assert.LessOrEqual(t, stat.TimeWindowStart, endTime, "TimeWindowStart should be <= endTime")
	}

	// Step 6: Query latest statistics
	latestGpt4, err := model.GetLatestGroupStatistics(group.ID, "gpt-4")
	require.NoError(t, err, "Failed to query latest gpt-4 statistics")
	assert.NotNil(t, latestGpt4, "Should return latest gpt-4 statistics")
	assert.Equal(t, "gpt-4", latestGpt4.ModelName, "Should be gpt-4")
	assert.Equal(t, baseTime, latestGpt4.TimeWindowStart, "Should be the most recent time window")
	assert.Equal(t, 50000, latestGpt4.TPM, "Should have correct TPM value")

	latestGpt35, err := model.GetLatestGroupStatistics(group.ID, "gpt-3.5-turbo")
	require.NoError(t, err, "Failed to query latest gpt-3.5-turbo statistics")
	assert.Equal(t, "gpt-3.5-turbo", latestGpt35.ModelName, "Should be gpt-3.5-turbo")
	assert.Equal(t, baseTime, latestGpt35.TimeWindowStart, "Should be the most recent time window")

	// Query latest without specifying model
	latestAny, err := model.GetLatestGroupStatistics(group.ID, "")
	require.NoError(t, err, "Failed to query latest statistics (any model)")
	assert.NotNil(t, latestAny, "Should return latest statistics")
	assert.Equal(t, baseTime, latestAny.TimeWindowStart, "Should be the most recent time window")

	// Step 7: Verify results are ordered by time_window_start DESC
	if len(allStats) > 1 {
		for i := 0; i < len(allStats)-1; i++ {
			assert.GreaterOrEqual(t, allStats[i].TimeWindowStart, allStats[i+1].TimeWindowStart,
				"Results should be ordered by TimeWindowStart DESC")
		}
	}

	t.Logf("✓ DB-07 passed: Successfully queried group statistics with various filters")
}

// TestDB08_GroupStatistics_Update tests updating group statistics.
//
// Test ID: DB-08
// Table: group_statistics
// Test Scenario: 更新分组统计
// Operation Type: UPDATE
// Acceptance Criteria: 每次聚合更新 updated_at 字段
// Priority: P1
//
// Test Steps:
// 1. Create a test group
// 2. Insert an initial group statistics record
// 3. Wait a moment and update the statistics (same composite key)
// 4. Verify UpdatedAt timestamp changed
// 5. Verify data was updated correctly
// 6. Verify no duplicate records were created
// 7. Test multiple updates to the same record
func TestDB08_GroupStatistics_Update(t *testing.T) {
	suite, cleanup := SetupDatabaseSchemaSuite(t)
	defer cleanup()

	// Step 1: Create a test group
	testUser, err := suite.Fixtures.CreateTestUser("groupstats-update", "defaultpass", "default")
	require.NoError(t, err, "Failed to create test user")

	group := suite.createTestP2PGroup(t, "test-group-db08", testUser.ID)

	// Step 2: Insert an initial group statistics record
	now := time.Now()
	timeWindow := roundToTimeWindow(now)

	initialStat := &model.GroupStatistics{
		GroupId:            group.ID,
		ModelName:          "gpt-4",
		TimeWindowStart:    timeWindow,
		TPM:                50000,
		RPM:                100,
		FailRate:           5.5,
		AvgResponseTimeMs:  250,
		AvgCacheHitRate:    30.0,
		StreamReqRatio:     65.0,
		QuotaPM:            5000,
		TotalTokens:        750000,
		TotalQuota:         75000,
		AvgConcurrency:     5.0,
		TotalSessions:      1000,
		DowntimePercentage: 1.5,
		UniqueUsers:        50,
	}

	err = model.UpsertGroupStatistics(initialStat)
	require.NoError(t, err, "Failed to create initial statistics")
	initialUpdatedAt := initialStat.UpdatedAt
	require.NotZero(t, initialUpdatedAt, "Initial UpdatedAt should be set")

	// Step 3: Wait a moment and update the statistics
	time.Sleep(time.Millisecond * 100)

	updatedStat := &model.GroupStatistics{
		GroupId:            group.ID,
		ModelName:          "gpt-4",
		TimeWindowStart:    timeWindow, // Same composite key
		TPM:                60000,      // Updated
		RPM:                120,        // Updated
		FailRate:           4.2,        // Updated
		AvgResponseTimeMs:  230,        // Updated
		AvgCacheHitRate:    35.0,       // Updated
		StreamReqRatio:     70.0,       // Updated
		QuotaPM:            6000,       // Updated
		TotalTokens:        900000,     // Updated
		TotalQuota:         90000,      // Updated
		AvgConcurrency:     6.0,        // Updated
		TotalSessions:      1200,       // Updated
		DowntimePercentage: 1.0,        // Updated
		UniqueUsers:        60,         // Updated
	}

	err = model.UpsertGroupStatistics(updatedStat)
	require.NoError(t, err, "Failed to update statistics")

	// Step 4: Verify UpdatedAt timestamp changed
	assert.Greater(t, updatedStat.UpdatedAt, initialUpdatedAt, "UpdatedAt should be greater after update")

	// Step 5: Verify data was updated correctly
	retrievedStats, err := model.GetGroupStatistics(group.ID, "gpt-4", timeWindow, timeWindow)
	require.NoError(t, err, "Failed to query updated statistics")

	// Step 6: Verify no duplicate records were created
	assert.Len(t, retrievedStats, 1, "Should have exactly one record (no duplicate)")

	retrieved := retrievedStats[0]
	assert.Equal(t, 60000, retrieved.TPM, "TPM should be updated")
	assert.Equal(t, 120, retrieved.RPM, "RPM should be updated")
	assert.InDelta(t, 4.2, retrieved.FailRate, 0.01, "FailRate should be updated")
	assert.Equal(t, 230, retrieved.AvgResponseTimeMs, "AvgResponseTimeMs should be updated")
	assert.InDelta(t, 35.0, retrieved.AvgCacheHitRate, 0.01, "AvgCacheHitRate should be updated")
	assert.InDelta(t, 70.0, retrieved.StreamReqRatio, 0.01, "StreamReqRatio should be updated")
	assert.Equal(t, int64(6000), retrieved.QuotaPM, "QuotaPM should be updated")
	assert.Equal(t, int64(900000), retrieved.TotalTokens, "TotalTokens should be updated")
	assert.Equal(t, int64(90000), retrieved.TotalQuota, "TotalQuota should be updated")
	assert.InDelta(t, 6.0, retrieved.AvgConcurrency, 0.01, "AvgConcurrency should be updated")
	assert.Equal(t, int64(1200), retrieved.TotalSessions, "TotalSessions should be updated")
	assert.InDelta(t, 1.0, retrieved.DowntimePercentage, 0.01, "DowntimePercentage should be updated")
	assert.Equal(t, 60, retrieved.UniqueUsers, "UniqueUsers should be updated")
	assert.Equal(t, updatedStat.UpdatedAt, retrieved.UpdatedAt, "UpdatedAt should match")

	// Step 7: Test multiple updates to the same record
	secondUpdatedAt := updatedStat.UpdatedAt
	time.Sleep(time.Millisecond * 100)

	// Third update
	thirdUpdateStat := &model.GroupStatistics{
		GroupId:            group.ID,
		ModelName:          "gpt-4",
		TimeWindowStart:    timeWindow,
		TPM:                70000,
		RPM:                140,
		FailRate:           3.8,
		AvgResponseTimeMs:  220,
		AvgCacheHitRate:    40.0,
		StreamReqRatio:     75.0,
		QuotaPM:            7000,
		TotalTokens:        1050000,
		TotalQuota:         105000,
		AvgConcurrency:     7.0,
		TotalSessions:      1400,
		DowntimePercentage: 0.5,
		UniqueUsers:        70,
	}

	err = model.UpsertGroupStatistics(thirdUpdateStat)
	require.NoError(t, err, "Failed to perform third update")

	assert.Greater(t, thirdUpdateStat.UpdatedAt, secondUpdatedAt, "UpdatedAt should increase with each update")

	// Verify final state
	finalStats, err := model.GetGroupStatistics(group.ID, "gpt-4", timeWindow, timeWindow)
	require.NoError(t, err, "Failed to query final statistics")
	assert.Len(t, finalStats, 1, "Should still have exactly one record")

	final := finalStats[0]
	assert.Equal(t, 70000, final.TPM, "TPM should reflect final update")
	assert.Equal(t, 140, final.RPM, "RPM should reflect final update")
	assert.InDelta(t, 3.8, final.FailRate, 0.01, "FailRate should reflect final update")
	assert.Equal(t, thirdUpdateStat.UpdatedAt, final.UpdatedAt, "UpdatedAt should reflect final update")

	t.Logf("✓ DB-08 passed: Group statistics updated correctly, UpdatedAt timestamp updated with each change")
}
