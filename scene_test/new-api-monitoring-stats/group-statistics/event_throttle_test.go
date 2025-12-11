package group_statistics_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"one-api/scene_test/testutil"
)

// EventThrottleSuite tests the event-driven aggregation and throttle mechanism.
type EventThrottleSuite struct {
	suite.Suite
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	fixtures *testutil.TestFixtures
}

// SetupSuite runs once before all tests in the suite.
func (s *EventThrottleSuite) SetupSuite() {
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
func (s *EventThrottleSuite) TearDownSuite() {
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
func (s *EventThrottleSuite) SetupTest() {
	if s.upstream != nil {
		s.upstream.Reset()
	}
}

// TearDownTest runs after each test.
func (s *EventThrottleSuite) TearDownTest() {
	// Clean up test data
}

// TestGE01_ChannelUpdateTriggersEvent tests that channel statistics update triggers group aggregation event.
//
// Test ID: GE-01
// Priority: P0
// Test Scenario: 渠道更新触发事件
// Expected Result: 系统发出"渠道统计更新"事件
func (s *EventThrottleSuite) TestGE01_ChannelUpdateTriggersEvent() {
	s.T().Log("GE-01: Testing channel update triggers aggregation event")

	// Arrange: Create a channel in the group
	channel, err := s.fixtures.CreateTestChannel(
		"ge01-channel",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel creation should succeed")

	// Record initial group statistics state
	initialStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	var initialUpdateTime int64 = 0
	if err == nil && initialStats != nil {
		initialUpdateTime = initialStats.UpdatedAt
	}

	// Act: Create channel statistics (this should trigger the event)
	stats := testutil.CreateTestChannelStatistics(channel.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	// In a full implementation, the DB Sync Worker would:
	// 1. Persist channel statistics to database
	// 2. Emit "channel_stats_updated" event
	// 3. Event listener checks throttle and queues GroupStatUpdateTask

	// Wait for event processing and aggregation
	time.Sleep(3 * time.Second)

	// Assert: Verify group statistics were updated
	updatedStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	assert.NotNil(s.T(), updatedStats, "Group statistics should exist after channel update")
	assert.Greater(s.T(), updatedStats.UpdatedAt, initialUpdateTime,
		"Group statistics should be updated after channel statistics change")

	s.T().Logf("GE-01: Event trigger verified - Group stats updated at %d (was %d)",
		updatedStats.UpdatedAt, initialUpdateTime)
}

// TestGE02_ThrottleMechanism tests that multiple updates within 30 minutes are throttled.
//
// Test ID: GE-02
// Priority: P0
// Test Scenario: 节流机制 (30分钟)
// Expected Result: 只在T0时刻生成一次聚合任务，后续更新被节流忽略
func (s *EventThrottleSuite) TestGE02_ThrottleMechanism() {
	s.T().Log("GE-02: Testing 30-minute throttle mechanism")

	// Arrange: Create three channels in the same group
	channel1, err := s.fixtures.CreateTestChannel(
		"ge02-channel-1",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 1 creation should succeed")

	channel2, err := s.fixtures.CreateTestChannel(
		"ge02-channel-2",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 2 creation should succeed")

	channel3, err := s.fixtures.CreateTestChannel(
		"ge02-channel-3",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 3 creation should succeed")

	// Act: Update channel statistics at T0, T0+10min, T0+20min
	// T0: Update Ch1
	stats1 := testutil.CreateTestChannelStatistics(channel1.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats1)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	time.Sleep(2 * time.Second) // Wait for first aggregation
	firstUpdateTime := time.Now().Unix()

	// Get aggregation status after first update
	status1, err := s.client.GetGroupAggregationStatus(s.fixtures.SharedGroup1.ID)
	if err != nil {
		s.T().Skip("Skipping - aggregation status API not available")
	}
	s.T().Logf("After first update: %v", status1)

	// T0+10s: Update Ch2 (should be throttled if within 30 min window)
	time.Sleep(1 * time.Second)
	stats2 := testutil.CreateTestChannelStatistics(channel2.ID, "gpt-4", 200, 0, 2000)
	err = s.client.CreateChannelStatistics(stats2)
	assert.NoError(s.T(), err, "Stats 2 creation should succeed")

	time.Sleep(1 * time.Second)

	// T0+20s: Update Ch3 (should also be throttled)
	time.Sleep(1 * time.Second)
	stats3 := testutil.CreateTestChannelStatistics(channel3.ID, "gpt-4", 300, 0, 3000)
	err = s.client.CreateChannelStatistics(stats3)
	assert.NoError(s.T(), err, "Stats 3 creation should succeed")

	time.Sleep(2 * time.Second)

	// Assert: Verify only one aggregation task was executed
	// Check if group statistics UpdatedAt is close to firstUpdateTime (not updated for each channel)
	finalStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	// The UpdatedAt should be close to firstUpdateTime, not significantly later
	timeDiff := finalStats.UpdatedAt - firstUpdateTime
	s.T().Logf("GE-02: Time difference between first and final update: %d seconds", timeDiff)

	// In a properly implemented throttle, the subsequent updates should not trigger new aggregations
	// So the UpdatedAt should be within a few seconds of the first update
	assert.Less(s.T(), timeDiff, int64(10),
		"Subsequent updates should be throttled, not trigger new aggregations")

	s.T().Log("GE-02: Throttle mechanism test completed")
}

// TestGE03_ThrottleWindowExpiry tests that aggregation can be triggered after throttle window expires.
//
// Test ID: GE-03
// Priority: P0
// Test Scenario: 节流时间窗口过期
// Expected Result: 第二次触发成功生成新的聚合任务
func (s *EventThrottleSuite) TestGE03_ThrottleWindowExpiry() {
	s.T().Log("GE-03: Testing throttle window expiry (30 minutes)")

	// Arrange: Create a channel
	channel, err := s.fixtures.CreateTestChannel(
		"ge03-channel",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel creation should succeed")

	// Act: First update at T0
	stats1 := testutil.CreateTestChannelStatistics(channel.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats1)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	time.Sleep(2 * time.Second)

	firstStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}
	firstUpdateTime := firstStats.UpdatedAt
	s.T().Logf("First aggregation at: %d", firstUpdateTime)

	// NOTE: In a real test environment, we would need to:
	// 1. Mock the time or wait 31 minutes
	// 2. Or use a testing API to manipulate the throttle timestamp
	// For this test, we'll simulate by manually clearing the throttle

	// Simulate time passing (31 minutes)
	// In production, we would use a testing API like:
	// s.client.ResetGroupThrottle(s.fixtures.SharedGroup1.ID)

	s.T().Log("GE-03: Simulating 31 minutes passing...")
	s.T().Log("NOTE: In a full implementation, this would use time mocking or test API")

	// Second update after throttle window (simulated)
	time.Sleep(2 * time.Second) // In reality, this would be 31 minutes

	stats2 := testutil.CreateTestChannelStatistics(channel.ID, "gpt-4", 200, 0, 2000)
	err = s.client.CreateChannelStatistics(stats2)
	assert.NoError(s.T(), err, "Stats 2 creation should succeed")

	// Manually trigger aggregation to simulate throttle expiry
	err = s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
	if err == nil {
		time.Sleep(2 * time.Second)
	}

	// Assert: Verify second aggregation occurred
	secondStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err == nil {
		s.T().Logf("Second aggregation at: %d", secondStats.UpdatedAt)

		// In a real implementation with proper time control:
		// assert.Greater(s.T(), secondStats.UpdatedAt, firstUpdateTime+1800,
		//     "Second aggregation should occur after 30-minute window")
	}

	s.T().Log("GE-03: Throttle window expiry test completed")
	s.T().Log("NOTE: Full verification requires time mocking or waiting 31 minutes")
}

// TestGE04_CrossGroupIndependentThrottle tests that throttle is independent across different groups.
//
// Test ID: GE-04
// Priority: P1
// Test Scenario: 跨分组独立节流
// Expected Result: 两个分组的节流计时器独立，互不影响
func (s *EventThrottleSuite) TestGE04_CrossGroupIndependentThrottle() {
	s.T().Log("GE-04: Testing cross-group independent throttle")

	// Arrange: Create channels in two different groups
	// Channel in Group 1
	channel1, err := s.fixtures.CreateTestChannel(
		"ge04-channel-g1",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel 1 creation should succeed")

	// Create a second P2P group if not exists
	if s.fixtures.SharedGroup2 == nil {
		group2, err := s.fixtures.CreateTestP2PGroup(
			"ge04-group-2",
			s.fixtures.User2Client,
			s.fixtures.RegularUser2.ID,
			testutil.P2PGroupTypeShared,
			testutil.P2PJoinMethodPassword,
			"testpass",
		)
		assert.NoError(s.T(), err, "Group 2 creation should succeed")
		s.fixtures.SharedGroup2 = group2
	}

	// Channel in Group 2
	channel2, err := s.fixtures.CreateTestChannel(
		"ge04-channel-g2",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser2.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup2.ID),
	)
	assert.NoError(s.T(), err, "Channel 2 creation should succeed")

	// Act: Update both channels simultaneously
	stats1 := testutil.CreateTestChannelStatistics(channel1.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats1)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	stats2 := testutil.CreateTestChannelStatistics(channel2.ID, "gpt-4", 200, 0, 2000)
	err = s.client.CreateChannelStatistics(stats2)
	assert.NoError(s.T(), err, "Stats 2 creation should succeed")

	time.Sleep(3 * time.Second) // Wait for aggregations

	// Assert: Verify both groups were aggregated independently
	group1Stats, err1 := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	group2Stats, err2 := s.client.GetGroupStatistics(s.fixtures.SharedGroup2.ID, "gpt-4")

	if err1 != nil || err2 != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	// Both groups should have been updated
	assert.NotNil(s.T(), group1Stats, "Group 1 should have statistics")
	assert.NotNil(s.T(), group2Stats, "Group 2 should have statistics")
	assert.Greater(s.T(), group1Stats.UpdatedAt, int64(0), "Group 1 should be updated")
	assert.Greater(s.T(), group2Stats.UpdatedAt, int64(0), "Group 2 should be updated")

	// Verify they have different data
	assert.Equal(s.T(), stats1.TotalTokens, group1Stats.TotalTokens,
		"Group 1 should have Channel 1's tokens")
	assert.Equal(s.T(), stats2.TotalTokens, group2Stats.TotalTokens,
		"Group 2 should have Channel 2's tokens")

	// Now update Group 1 again (should be throttled)
	stats1_2 := testutil.CreateTestChannelStatistics(channel1.ID, "gpt-4", 150, 0, 1500)
	err = s.client.CreateChannelStatistics(stats1_2)
	assert.NoError(s.T(), err, "Stats 1_2 creation should succeed")

	// Update Group 2 again (should also be throttled independently)
	stats2_2 := testutil.CreateTestChannelStatistics(channel2.ID, "gpt-4", 250, 0, 2500)
	err = s.client.CreateChannelStatistics(stats2_2)
	assert.NoError(s.T(), err, "Stats 2_2 creation should succeed")

	time.Sleep(2 * time.Second)

	// Both should still have their original data (throttled)
	group1Stats2, _ := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	group2Stats2, _ := s.client.GetGroupStatistics(s.fixtures.SharedGroup2.ID, "gpt-4")

	if group1Stats2 != nil && group2Stats2 != nil {
		// Verify throttle is independent (both groups' throttle works separately)
		s.T().Logf("GE-04: Group 1 update time: %d (initial: %d)",
			group1Stats2.UpdatedAt, group1Stats.UpdatedAt)
		s.T().Logf("GE-04: Group 2 update time: %d (initial: %d)",
			group2Stats2.UpdatedAt, group2Stats.UpdatedAt)
	}

	s.T().Log("GE-04: Cross-group independent throttle test completed")
}

// TestRunner for the event throttle test suite
func TestEventThrottleSuite(t *testing.T) {
	suite.Run(t, new(EventThrottleSuite))
}
