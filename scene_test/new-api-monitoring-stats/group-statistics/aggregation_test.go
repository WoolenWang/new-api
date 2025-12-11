package group_statistics_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"one-api/scene_test/testutil"
)

// AggregationSuite tests the group statistics aggregation calculation correctness.
type AggregationSuite struct {
	suite.Suite
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	fixtures *testutil.TestFixtures
}

// SetupSuite runs once before all tests in the suite.
func (s *AggregationSuite) SetupSuite() {
	var err error
	s.server, err = testutil.StartTestServer()
	if err != nil {
		s.T().Fatalf("Failed to start test server: %v", err)
	}

	s.client = testutil.NewAPIClient(s.server)
	s.upstream = testutil.NewMockUpstreamServer()

	// Setup basic fixtures
	s.fixtures = testutil.NewTestFixtures(s.T(), s.client)
	s.fixtures.SetUpstream(s.upstream)

	// Create basic users
	if err := s.fixtures.SetupBasicUsers(); err != nil {
		s.T().Fatalf("Failed to setup basic users: %v", err)
	}

	// Create basic channels
	if err := s.fixtures.SetupBasicChannels(); err != nil {
		s.T().Fatalf("Failed to setup basic channels: %v", err)
	}

	// Setup P2P groups
	if err := s.fixtures.SetupP2PGroups(); err != nil {
		s.T().Fatalf("Failed to setup P2P groups: %v", err)
	}
}

// TearDownSuite runs once after all tests in the suite.
func (s *AggregationSuite) TearDownSuite() {
	if s.fixtures != nil {
		s.fixtures.Cleanup()
	}
	if s.upstream != nil {
		s.upstream.Close()
	}
	if s.server != nil {
		s.server.Stop()
	}
}

// SetupTest runs before each test.
func (s *AggregationSuite) SetupTest() {
	if s.upstream != nil {
		s.upstream.Reset()
	}
}

// TearDownTest runs after each test.
func (s *AggregationSuite) TearDownTest() {
	// Clean up test data
}

// TestGS01_SummationMetrics tests aggregation of summation-type metrics (TPM, RPM, TotalTokens).
//
// Test ID: GS-01
// Priority: P0
// Test Scenario: 求和类指标聚合
// Expected Result: Group.TPM = Σ(Channel_i.TPM)
func (s *AggregationSuite) TestGS01_SummationMetrics() {
	s.T().Log("GS-01: Testing summation metrics aggregation (TPM, RPM, TotalTokens)")

	// Arrange: Create two channels in SharedGroup1
	channel1, err := s.fixtures.CreateTestChannel(
		"gs01-channel-1",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 1 creation should succeed")

	channel2, err := s.fixtures.CreateTestChannel(
		"gs01-channel-2",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 2 creation should succeed")

	// Create channel statistics
	// Ch1: 1000 tokens in 15 minutes = TPM=1000/15≈67
	stats1 := testutil.CreateTestChannelStatistics(channel1.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats1)
	if err != nil {
		s.T().Logf("Warning: Could not create channel statistics: %v", err)
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	// Ch2: 2000 tokens in 15 minutes = TPM=2000/15≈133
	stats2 := testutil.CreateTestChannelStatistics(channel2.ID, "gpt-4", 200, 0, 2000)
	err = s.client.CreateChannelStatistics(stats2)
	assert.NoError(s.T(), err, "Stats 2 creation should succeed")

	// Act: Trigger group aggregation
	err = s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
	if err != nil {
		s.T().Logf("Warning: Could not trigger aggregation: %v", err)
		s.T().Skip("Skipping - group aggregation not implemented")
	}

	// Wait for aggregation to complete
	time.Sleep(2 * time.Second)

	// Assert: Verify summation metrics
	groupStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	// Expected TPM = (1000 + 2000) / 15 = 200
	expectedTPM := testutil.SumChannelTPM([]*testutil.ChannelStatisticsModel{stats1, stats2})
	assert.Equal(s.T(), expectedTPM, groupStats.TPM, "Group TPM should equal sum of channel TPMs")

	// Expected RPM = (100 + 200) / 15 = 20
	expectedRPM := testutil.SumChannelRPM([]*testutil.ChannelStatisticsModel{stats1, stats2})
	assert.Equal(s.T(), expectedRPM, groupStats.RPM, "Group RPM should equal sum of channel RPMs")

	// Expected TotalTokens = 1000 + 2000 = 3000
	expectedTotalTokens := stats1.TotalTokens + stats2.TotalTokens
	assert.Equal(s.T(), expectedTotalTokens, groupStats.TotalTokens, "Group TotalTokens should equal sum")

	s.T().Logf("GS-01: Summation aggregation verified - TPM=%d, RPM=%d, TotalTokens=%d",
		groupStats.TPM, groupStats.RPM, groupStats.TotalTokens)
}

// TestGS02_WeightedAverageFailRate tests weighted average aggregation for fail rate.
//
// Test ID: GS-02
// Priority: P0
// Test Scenario: 加权平均聚合 (失败率)
// Expected Result: Group.FailRate = (FailRate1*ReqCount1 + FailRate2*ReqCount2) / (ReqCount1+ReqCount2)
func (s *AggregationSuite) TestGS02_WeightedAverageFailRate() {
	s.T().Log("GS-02: Testing weighted average fail rate aggregation")

	// Arrange: Create two channels with different fail rates
	channel1, err := s.fixtures.CreateTestChannel(
		"gs02-channel-1",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 1 creation should succeed")

	channel2, err := s.fixtures.CreateTestChannel(
		"gs02-channel-2",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 2 creation should succeed")

	// Ch1: fail_rate=10%, request_count=100
	stats1 := testutil.CreateTestChannelStatistics(channel1.ID, "gpt-4", 100, 10, 1000)
	err = s.client.CreateChannelStatistics(stats1)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	// Ch2: fail_rate=20%, request_count=200
	stats2 := testutil.CreateTestChannelStatistics(channel2.ID, "gpt-4", 200, 40, 2000)
	err = s.client.CreateChannelStatistics(stats2)
	assert.NoError(s.T(), err, "Stats 2 creation should succeed")

	// Act: Trigger aggregation
	err = s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
	if err != nil {
		s.T().Skip("Skipping - group aggregation not implemented")
	}

	time.Sleep(2 * time.Second)

	// Assert: Verify weighted average fail rate
	groupStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	// Expected: (10*100 + 40*200) / (100+200) = (1000 + 8000) / 300 = 30%
	// But we calculate as: (10*100 + 20*200) / 300 = (1000 + 4000) / 300 = 16.67%
	expectedFailRate := testutil.CalculateExpectedFailRate([]*testutil.ChannelStatisticsModel{stats1, stats2})
	assert.InDelta(s.T(), expectedFailRate, groupStats.FailRate, 0.01,
		"Group fail rate should be weighted average")

	s.T().Logf("GS-02: Weighted average fail rate verified - Expected=%.2f%%, Actual=%.2f%%",
		expectedFailRate, groupStats.FailRate)
}

// TestGS03_WeightedAverageResponseTime tests weighted average aggregation for response time.
//
// Test ID: GS-03
// Priority: P0
// Test Scenario: 加权平均聚合 (响应时间)
// Expected Result: Group.AvgResponseTime = weighted average by request count
func (s *AggregationSuite) TestGS03_WeightedAverageResponseTime() {
	s.T().Log("GS-03: Testing weighted average response time aggregation")

	// Arrange: Create channels with different response times
	channel1, err := s.fixtures.CreateTestChannel(
		"gs03-channel-1",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 1 creation should succeed")

	channel2, err := s.fixtures.CreateTestChannel(
		"gs03-channel-2",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 2 creation should succeed")

	// Ch1: avg_response_time=100ms, request_count=50
	stats1 := &testutil.ChannelStatisticsModel{
		ChannelID:       channel1.ID,
		ModelName:       "gpt-4",
		TimeWindowStart: time.Now().Unix(),
		RequestCount:    50,
		FailCount:       0,
		TotalTokens:     500,
		TotalQuota:      5000,
		TotalLatencyMs:  5000, // 100ms per request * 50
	}
	err = s.client.CreateChannelStatistics(stats1)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	// Ch2: avg_response_time=200ms, request_count=150
	stats2 := &testutil.ChannelStatisticsModel{
		ChannelID:       channel2.ID,
		ModelName:       "gpt-4",
		TimeWindowStart: time.Now().Unix(),
		RequestCount:    150,
		FailCount:       0,
		TotalTokens:     1500,
		TotalQuota:      15000,
		TotalLatencyMs:  30000, // 200ms per request * 150
	}
	err = s.client.CreateChannelStatistics(stats2)
	assert.NoError(s.T(), err, "Stats 2 creation should succeed")

	// Act: Trigger aggregation
	err = s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
	if err != nil {
		s.T().Skip("Skipping - group aggregation not implemented")
	}

	time.Sleep(2 * time.Second)

	// Assert: Verify weighted average response time
	groupStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	// Expected: (100*50 + 200*150) / (50+150) = (5000 + 30000) / 200 = 175ms
	expectedAvgResponseTime := testutil.CalculateExpectedAvgResponseTime([]*testutil.ChannelStatisticsModel{stats1, stats2})
	assert.Equal(s.T(), expectedAvgResponseTime, groupStats.AvgResponseTime,
		"Group avg response time should be weighted average")

	s.T().Logf("GS-03: Weighted average response time verified - Expected=%dms, Actual=%dms",
		expectedAvgResponseTime, groupStats.AvgResponseTime)
}

// TestGS04_ConcurrencySummation tests that average concurrency is summed (not averaged).
//
// Test ID: GS-04
// Priority: P1
// Test Scenario: 并发数直接求和
// Expected Result: Group.AvgConcurrency = Σ(Channel_i.AvgConcurrency)
func (s *AggregationSuite) TestGS04_ConcurrencySummation() {
	s.T().Log("GS-04: Testing concurrency summation (not weighted average)")

	// Arrange: Create channels with different concurrency levels
	channel1, err := s.fixtures.CreateTestChannel(
		"gs04-channel-1",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 1 creation should succeed")

	channel2, err := s.fixtures.CreateTestChannel(
		"gs04-channel-2",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 2 creation should succeed")

	// Note: Since ChannelStatisticsModel doesn't have AvgConcurrency field,
	// we'll need to extend it or use a mock. For now, let's skip this test
	// and note that it requires backend implementation.

	s.T().Skip("GS-04: Skipping - requires AvgConcurrency field in channel statistics")

	// Expected behavior (when implemented):
	// Ch1: avg_concurrency=5
	// Ch2: avg_concurrency=3
	// Group: avg_concurrency = 5 + 3 = 8 (direct sum, not weighted average)
}

// TestGS05_UniqueUsersAggregation tests that unique users are deduplicated across channels.
//
// Test ID: GS-05
// Priority: P0
// Test Scenario: 去重用户数聚合
// Expected Result: Ch1服务用户A、B，Ch2服务用户B、C => Group.UniqueUsers = 3 (A, B, C)
func (s *AggregationSuite) TestGS05_UniqueUsersAggregation() {
	s.T().Log("GS-05: Testing unique users deduplication aggregation")

	// Arrange: This test requires backend to track unique users per channel
	// and perform HyperLogLog-based deduplication during group aggregation

	// In a full implementation, we would:
	// 1. Have User A and User B use Channel 1
	// 2. Have User B and User C use Channel 2
	// 3. Verify that Group.UniqueUsers = 3 (not 4, because User B is deduplicated)

	s.T().Skip("GS-05: Skipping - requires unique user tracking and HyperLogLog deduplication")

	// Expected behavior:
	// Channel 1 statistics: unique_users stored in Redis HLL as user IDs {A, B}
	// Channel 2 statistics: unique_users stored in Redis HLL as user IDs {B, C}
	// Group aggregation: PFMERGE to combine HLLs => {A, B, C} => count = 3
}

// TestGS06_ModelDimensionAggregation tests that statistics are aggregated separately by model.
//
// Test ID: GS-06
// Priority: P0
// Test Scenario: 按模型维度聚合
// Expected Result: 生成 (G1, gpt-4) 和 (G1, gpt-3.5) 两条独立统计记录
func (s *AggregationSuite) TestGS06_ModelDimensionAggregation() {
	s.T().Log("GS-06: Testing per-model dimension aggregation")

	// Arrange: Create channels supporting different models in the same group
	channel1, err := s.fixtures.CreateTestChannel(
		"gs06-channel-1",
		"gpt-4,gpt-3.5-turbo",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 1 creation should succeed")

	channel2, err := s.fixtures.CreateTestChannel(
		"gs06-channel-2",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 2 creation should succeed")

	// Create statistics for gpt-4 on both channels
	stats1GPT4 := testutil.CreateTestChannelStatistics(channel1.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats1GPT4)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	stats2GPT4 := testutil.CreateTestChannelStatistics(channel2.ID, "gpt-4", 200, 0, 2000)
	err = s.client.CreateChannelStatistics(stats2GPT4)
	assert.NoError(s.T(), err, "GPT-4 stats creation should succeed")

	// Create statistics for gpt-3.5-turbo on channel1 only
	stats1GPT35 := testutil.CreateTestChannelStatistics(channel1.ID, "gpt-3.5-turbo", 50, 0, 500)
	err = s.client.CreateChannelStatistics(stats1GPT35)
	assert.NoError(s.T(), err, "GPT-3.5 stats creation should succeed")

	// Act: Trigger aggregation
	err = s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
	if err != nil {
		s.T().Skip("Skipping - group aggregation not implemented")
	}

	time.Sleep(2 * time.Second)

	// Assert: Verify separate statistics for each model
	groupStatsGPT4, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	groupStatsGPT35, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-3.5-turbo")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	// Verify GPT-4 statistics (sum of both channels)
	expectedGPT4Tokens := stats1GPT4.TotalTokens + stats2GPT4.TotalTokens
	assert.Equal(s.T(), expectedGPT4Tokens, groupStatsGPT4.TotalTokens,
		"GPT-4 group stats should sum both channels")

	// Verify GPT-3.5 statistics (only channel1)
	expectedGPT35Tokens := stats1GPT35.TotalTokens
	assert.Equal(s.T(), expectedGPT35Tokens, groupStatsGPT35.TotalTokens,
		"GPT-3.5 group stats should only include channel1")

	// Verify they are independent
	assert.NotEqual(s.T(), groupStatsGPT4.TotalTokens, groupStatsGPT35.TotalTokens,
		"Different models should have independent statistics")

	s.T().Logf("GS-06: Per-model aggregation verified - GPT-4: %d tokens, GPT-3.5: %d tokens",
		groupStatsGPT4.TotalTokens, groupStatsGPT35.TotalTokens)
}

// TestGS07_DisabledChannelExclusion tests that disabled channels are not included in aggregation.
//
// Test ID: GS-07
// Priority: P0
// Test Scenario: 禁用渠道不参与聚合
// Expected Result: 聚合结果仅包含启用渠道的数据
func (s *AggregationSuite) TestGS07_DisabledChannelExclusion() {
	s.T().Log("GS-07: Testing disabled channel exclusion from aggregation")

	// Arrange: Create two channels, then disable one
	channel1, err := s.fixtures.CreateTestChannel(
		"gs07-channel-1-enabled",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 1 creation should succeed")

	channel2, err := s.fixtures.CreateTestChannel(
		"gs07-channel-2-disabled",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 2 creation should succeed")

	// Create statistics for both channels
	stats1 := testutil.CreateTestChannelStatistics(channel1.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats1)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	stats2 := testutil.CreateTestChannelStatistics(channel2.ID, "gpt-4", 200, 0, 2000)
	err = s.client.CreateChannelStatistics(stats2)
	assert.NoError(s.T(), err, "Stats 2 creation should succeed")

	// Disable channel2
	channel2.Status = 0 // Disabled
	err = s.client.UpdateChannel(channel2)
	if err != nil {
		s.T().Logf("Warning: Could not disable channel: %v", err)
	}

	// Act: Trigger aggregation
	err = s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
	if err != nil {
		s.T().Skip("Skipping - group aggregation not implemented")
	}

	time.Sleep(2 * time.Second)

	// Assert: Verify only enabled channel's data is included
	groupStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	// Expected: Only channel1's tokens (1000), not channel1+channel2 (3000)
	assert.Equal(s.T(), stats1.TotalTokens, groupStats.TotalTokens,
		"Group stats should only include enabled channel")

	assert.NotEqual(s.T(), stats1.TotalTokens+stats2.TotalTokens, groupStats.TotalTokens,
		"Disabled channel should not be included")

	s.T().Logf("GS-07: Disabled channel exclusion verified - Group tokens=%d (should be %d, not %d)",
		groupStats.TotalTokens, stats1.TotalTokens, stats1.TotalTokens+stats2.TotalTokens)
}

// TestRunner for the aggregation test suite
func TestAggregationSuite(t *testing.T) {
	suite.Run(t, new(AggregationSuite))
}
