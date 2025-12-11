// Package database_api contains integration tests for database schema completeness.
// This file focuses on channels table extended fields tests (DB-04 to DB-05).
package database_api

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDB04_Channels_ExtendedFields tests reading extended statistics fields from channels table.
//
// Test ID: DB-04
// Table: channels
// Test Scenario: 读取渠道统计字段
// Operation Type: READ
// Acceptance Criteria: avg_response_time, fail_rate, tpm 等字段可正常读取
// Priority: P0
//
// Test Steps:
// 1. Create a test channel
// 2. Update channel with extended statistics fields
// 3. Read the channel back from database
// 4. Verify all extended fields are correctly retrieved
// 5. Test field default values on newly created channels
func TestDB04_Channels_ExtendedFields(t *testing.T) {
	suite, cleanup := SetupDatabaseSchemaSuite(t)
	defer cleanup()

	// Step 1: Create a test channel
	channel := suite.createTestChannel(t, "test-channel-db04", "gpt-4", "default")
	require.NotZero(t, channel.Id, "Channel should be created with ID")

	// Step 2: Update channel with extended statistics fields
	var updatedChannel model.Channel
	err := suite.DB.First(&updatedChannel, channel.Id).Error
	require.NoError(t, err, "Failed to retrieve channel")

	// Set extended statistics fields
	updatedChannel.AvgResponseTime = 250    // 250ms average response time
	updatedChannel.FailRate = 5.5           // 5.5% failure rate
	updatedChannel.AvgCacheHitRate = 30.2   // 30.2% cache hit rate
	updatedChannel.StreamReqRatio = 65.8    // 65.8% stream requests
	updatedChannel.TPM = 50000              // 50k tokens per minute
	updatedChannel.RPM = 100                // 100 requests per minute
	updatedChannel.QuotaPM = 5000           // 5000 quota per minute
	updatedChannel.TotalSessions = 10000    // 10k total sessions
	updatedChannel.DowntimePercentage = 2.3 // 2.3% downtime
	updatedChannel.UniqueUsers = 250        // 250 unique users

	err = suite.DB.Save(&updatedChannel).Error
	require.NoError(t, err, "Failed to update channel with extended fields")

	// Step 3: Read the channel back from database
	var retrievedChannel model.Channel
	err = suite.DB.First(&retrievedChannel, channel.Id).Error
	require.NoError(t, err, "Failed to retrieve updated channel")

	// Step 4: Verify all extended fields are correctly retrieved
	assert.Equal(t, 250, retrievedChannel.AvgResponseTime, "AvgResponseTime should match")
	assert.InDelta(t, 5.5, retrievedChannel.FailRate, 0.01, "FailRate should match")
	assert.InDelta(t, 30.2, retrievedChannel.AvgCacheHitRate, 0.01, "AvgCacheHitRate should match")
	assert.InDelta(t, 65.8, retrievedChannel.StreamReqRatio, 0.01, "StreamReqRatio should match")
	assert.Equal(t, 50000, retrievedChannel.TPM, "TPM should match")
	assert.Equal(t, 100, retrievedChannel.RPM, "RPM should match")
	assert.Equal(t, int64(5000), retrievedChannel.QuotaPM, "QuotaPM should match")
	assert.Equal(t, int64(10000), retrievedChannel.TotalSessions, "TotalSessions should match")
	assert.InDelta(t, 2.3, retrievedChannel.DowntimePercentage, 0.01, "DowntimePercentage should match")
	assert.Equal(t, 250, retrievedChannel.UniqueUsers, "UniqueUsers should match")

	// Step 5: Test field default values on newly created channels
	newChannel := suite.createTestChannel(t, "test-channel-db04-defaults", "gpt-3.5-turbo", "vip")

	var freshChannel model.Channel
	err = suite.DB.First(&freshChannel, newChannel.Id).Error
	require.NoError(t, err, "Failed to retrieve fresh channel")

	// Verify default values are zero/empty for statistics fields
	assert.Equal(t, 0, freshChannel.AvgResponseTime, "Default AvgResponseTime should be 0")
	assert.Equal(t, 0.0, freshChannel.FailRate, "Default FailRate should be 0")
	assert.Equal(t, 0.0, freshChannel.AvgCacheHitRate, "Default AvgCacheHitRate should be 0")
	assert.Equal(t, 0.0, freshChannel.StreamReqRatio, "Default StreamReqRatio should be 0")
	assert.Equal(t, 0, freshChannel.TPM, "Default TPM should be 0")
	assert.Equal(t, 0, freshChannel.RPM, "Default RPM should be 0")
	assert.Equal(t, int64(0), freshChannel.QuotaPM, "Default QuotaPM should be 0")
	assert.Equal(t, int64(0), freshChannel.TotalSessions, "Default TotalSessions should be 0")
	assert.Equal(t, 0.0, freshChannel.DowntimePercentage, "Default DowntimePercentage should be 0")
	assert.Equal(t, 0, freshChannel.UniqueUsers, "Default UniqueUsers should be 0")

	t.Logf("✓ DB-04 passed: All channel extended fields can be read and written correctly")
}

// MonitoringConfig represents the JSON structure stored in channels.monitoring_config
type MonitoringConfig struct {
	Enabled             bool     `json:"enabled"`
	TargetModel         string   `json:"target_model"`
	TestIntervalMinutes int      `json:"test_interval_minutes"`
	TestType            []string `json:"test_type"`
	EvaluationStandard  string   `json:"evaluation_standard"`
	BaselineChannelID   *int     `json:"baseline_channel_id,omitempty"`
}

// TestDB05_Channels_MonitoringConfig tests JSON field storage and parsing for monitoring_config.
//
// Test ID: DB-05
// Table: channels
// Test Scenario: 设置渠道监控配置
// Operation Type: CREATE/UPDATE
// Acceptance Criteria: JSON字段正确存储和解析
// Priority: P0
//
// Test Steps:
// 1. Create a test channel
// 2. Set monitoring_config with complex JSON structure
// 3. Save the channel
// 4. Retrieve the channel and parse monitoring_config JSON
// 5. Verify JSON data is correctly stored and retrieved
// 6. Update monitoring_config with different values
// 7. Verify updates are correctly persisted
// 8. Test with null/empty monitoring_config
func TestDB05_Channels_MonitoringConfig(t *testing.T) {
	suite, cleanup := SetupDatabaseSchemaSuite(t)
	defer cleanup()

	// Step 1: Create a test channel
	channel := suite.createTestChannel(t, "test-channel-db05", "gpt-4", "default")
	baselineChannelId := 999

	// Step 2: Set monitoring_config with complex JSON structure
	config := MonitoringConfig{
		Enabled:             true,
		TargetModel:         "gpt-4",
		TestIntervalMinutes: 60,
		TestType:            []string{"encoding", "style", "reasoning"},
		EvaluationStandard:  "standard",
		BaselineChannelID:   &baselineChannelId,
	}

	configJSON, err := json.Marshal(config)
	require.NoError(t, err, "Failed to marshal monitoring config")

	// Step 3: Save the channel with monitoring_config
	var dbChannel model.Channel
	err = suite.DB.First(&dbChannel, channel.Id).Error
	require.NoError(t, err, "Failed to retrieve channel")

	configStr := string(configJSON)
	dbChannel.MonitoringConfig = &configStr

	err = suite.DB.Save(&dbChannel).Error
	require.NoError(t, err, "Failed to save channel with monitoring_config")

	// Step 4: Retrieve the channel and parse monitoring_config JSON
	var retrievedChannel model.Channel
	err = suite.DB.First(&retrievedChannel, channel.Id).Error
	require.NoError(t, err, "Failed to retrieve channel with monitoring_config")

	// Step 5: Verify JSON data is correctly stored and retrieved
	require.NotNil(t, retrievedChannel.MonitoringConfig, "MonitoringConfig should not be nil")
	require.NotEmpty(t, *retrievedChannel.MonitoringConfig, "MonitoringConfig should not be empty")

	var parsedConfig MonitoringConfig
	err = json.Unmarshal([]byte(*retrievedChannel.MonitoringConfig), &parsedConfig)
	require.NoError(t, err, "Failed to unmarshal monitoring_config")

	assert.True(t, parsedConfig.Enabled, "Enabled should be true")
	assert.Equal(t, "gpt-4", parsedConfig.TargetModel, "TargetModel should match")
	assert.Equal(t, 60, parsedConfig.TestIntervalMinutes, "TestIntervalMinutes should match")
	assert.Len(t, parsedConfig.TestType, 3, "TestType should have 3 elements")
	assert.Contains(t, parsedConfig.TestType, "encoding", "TestType should contain 'encoding'")
	assert.Contains(t, parsedConfig.TestType, "style", "TestType should contain 'style'")
	assert.Contains(t, parsedConfig.TestType, "reasoning", "TestType should contain 'reasoning'")
	assert.Equal(t, "standard", parsedConfig.EvaluationStandard, "EvaluationStandard should match")
	require.NotNil(t, parsedConfig.BaselineChannelID, "BaselineChannelID should not be nil")
	assert.Equal(t, 999, *parsedConfig.BaselineChannelID, "BaselineChannelID should match")

	// Step 6: Update monitoring_config with different values
	updatedConfig := MonitoringConfig{
		Enabled:             false,
		TargetModel:         "gpt-3.5-turbo",
		TestIntervalMinutes: 120,
		TestType:            []string{"encoding"},
		EvaluationStandard:  "strict",
		BaselineChannelID:   nil,
	}

	updatedConfigJSON, err := json.Marshal(updatedConfig)
	require.NoError(t, err, "Failed to marshal updated config")

	updatedConfigStr := string(updatedConfigJSON)
	retrievedChannel.MonitoringConfig = &updatedConfigStr

	err = suite.DB.Save(&retrievedChannel).Error
	require.NoError(t, err, "Failed to update channel monitoring_config")

	// Step 7: Verify updates are correctly persisted
	var reRetrievedChannel model.Channel
	err = suite.DB.First(&reRetrievedChannel, channel.Id).Error
	require.NoError(t, err, "Failed to retrieve channel after update")

	require.NotNil(t, reRetrievedChannel.MonitoringConfig, "Updated MonitoringConfig should not be nil")

	var reparsedConfig MonitoringConfig
	err = json.Unmarshal([]byte(*reRetrievedChannel.MonitoringConfig), &reparsedConfig)
	require.NoError(t, err, "Failed to unmarshal updated monitoring_config")

	assert.False(t, reparsedConfig.Enabled, "Updated Enabled should be false")
	assert.Equal(t, "gpt-3.5-turbo", reparsedConfig.TargetModel, "Updated TargetModel should match")
	assert.Equal(t, 120, reparsedConfig.TestIntervalMinutes, "Updated TestIntervalMinutes should match")
	assert.Len(t, reparsedConfig.TestType, 1, "Updated TestType should have 1 element")
	assert.Contains(t, reparsedConfig.TestType, "encoding", "Updated TestType should contain 'encoding'")
	assert.Equal(t, "strict", reparsedConfig.EvaluationStandard, "Updated EvaluationStandard should match")
	assert.Nil(t, reparsedConfig.BaselineChannelID, "Updated BaselineChannelID should be nil")

	// Step 8: Test with null/empty monitoring_config
	nullChannel := suite.createTestChannel(t, "test-channel-db05-null", "claude-3", "default")

	var nullConfigChannel model.Channel
	err = suite.DB.First(&nullConfigChannel, nullChannel.Id).Error
	require.NoError(t, err, "Failed to retrieve null config channel")

	// By default, monitoring_config should be nil
	assert.Nil(t, nullConfigChannel.MonitoringConfig, "Default MonitoringConfig should be nil")

	// Test setting to empty string
	emptyStr := ""
	nullConfigChannel.MonitoringConfig = &emptyStr
	err = suite.DB.Save(&nullConfigChannel).Error
	require.NoError(t, err, "Failed to save channel with empty monitoring_config")

	var emptyConfigChannel model.Channel
	err = suite.DB.First(&emptyConfigChannel, nullChannel.Id).Error
	require.NoError(t, err, "Failed to retrieve empty config channel")

	require.NotNil(t, emptyConfigChannel.MonitoringConfig, "MonitoringConfig should not be nil")
	assert.Empty(t, *emptyConfigChannel.MonitoringConfig, "MonitoringConfig should be empty string")

	t.Logf("✓ DB-05 passed: MonitoringConfig JSON field stores and parses correctly")
}
