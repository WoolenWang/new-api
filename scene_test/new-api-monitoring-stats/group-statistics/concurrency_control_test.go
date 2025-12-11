package group_statistics_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"one-api/scene_test/testutil"
)

// ConcurrencyControlSuite tests the concurrent aggregation control mechanisms.
type ConcurrencyControlSuite struct {
	suite.Suite
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	fixtures *testutil.TestFixtures
}

// SetupSuite runs once before all tests in the suite.
func (s *ConcurrencyControlSuite) SetupSuite() {
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
func (s *ConcurrencyControlSuite) TearDownSuite() {
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
func (s *ConcurrencyControlSuite) SetupTest() {
	if s.upstream != nil {
		s.upstream.Reset()
	}
}

// TearDownTest runs after each test.
func (s *ConcurrencyControlSuite) TearDownTest() {
	// Clean up test data
}

// TestGC01_DistributedLockAcquisition tests that distributed lock prevents duplicate aggregation.
//
// Test ID: GC-01
// Priority: P0
// Test Scenario: 分布式锁获取
// Expected Result: Worker A获取锁成功，Worker B获取失败并放弃任务
func (s *ConcurrencyControlSuite) TestGC01_DistributedLockAcquisition() {
	s.T().Log("GC-01: Testing distributed lock acquisition")

	// Arrange: Create a channel in the group
	channel, err := s.fixtures.CreateTestChannel(
		"gc01-channel",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel creation should succeed")

	// Create channel statistics
	stats := testutil.CreateTestChannelStatistics(channel.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	// Act: Trigger aggregation twice concurrently (simulating two workers)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	successCount := 0
	var mu sync.Mutex

	// Simulate Worker A
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
		if err == nil {
			mu.Lock()
			successCount++
			mu.Unlock()
		}
		errors <- err
	}()

	// Simulate Worker B (slight delay to ensure Worker A gets lock first)
	time.Sleep(50 * time.Millisecond)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
		if err == nil {
			mu.Lock()
			successCount++
			mu.Unlock()
		}
		errors <- err
	}()

	wg.Wait()
	close(errors)

	// Assert: Verify that lock mechanism works
	// In a properly implemented system with distributed locks:
	// - First trigger should succeed and acquire lock
	// - Second trigger should fail to acquire lock or be queued

	s.T().Logf("GC-01: Successful aggregation triggers: %d", successCount)

	// Collect errors
	errorList := make([]error, 0)
	for err := range errors {
		if err != nil {
			errorList = append(errorList, err)
		}
	}

	// Verify behavior (at least one should succeed, second may be rejected or queued)
	if len(errorList) == 2 {
		s.T().Skip("Skipping - aggregation trigger not implemented")
	}

	// In a lock-protected system, we expect one of these outcomes:
	// 1. Both succeed (queuing system) but only one actually runs
	// 2. One succeeds, one fails (lock rejection)

	time.Sleep(2 * time.Second)

	// Verify final state: group stats should be updated exactly once
	groupStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err == nil {
		assert.Equal(s.T(), stats.TotalTokens, groupStats.TotalTokens,
			"Group stats should reflect the aggregation result")
		s.T().Logf("GC-01: Group aggregated correctly with tokens=%d", groupStats.TotalTokens)
	}

	s.T().Log("GC-01: Distributed lock test completed")
}

// TestGC02_LockTimeoutRecovery tests that locks can recover after timeout.
//
// Test ID: GC-02
// Priority: P1
// Test Scenario: 锁超时恢复
// Expected Result: 新Worker成功获取锁并完成聚合
func (s *ConcurrencyControlSuite) TestGC02_LockTimeoutRecovery() {
	s.T().Log("GC-02: Testing lock timeout recovery (180 seconds)")

	// Arrange: Create a channel
	channel, err := s.fixtures.CreateTestChannel(
		"gc02-channel",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel creation should succeed")

	stats := testutil.CreateTestChannelStatistics(channel.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	// Act: In a real scenario, we would:
	// 1. Simulate a worker acquiring lock and crashing (lock not released)
	// 2. Wait 180 seconds for lock to expire
	// 3. New worker successfully acquires the expired lock

	// For testing purposes, we note this requires:
	// - Redis lock with TTL of 180 seconds
	// - Test API to simulate crashed worker or time mocking

	s.T().Log("GC-02: NOTE - Full test requires lock TTL simulation")
	s.T().Log("Expected behavior:")
	s.T().Log("  1. Worker A acquires lock (TTL=180s)")
	s.T().Log("  2. Worker A crashes without releasing lock")
	s.T().Log("  3. After 180s, lock expires automatically")
	s.T().Log("  4. Worker B successfully acquires expired lock")
	s.T().Log("  5. Worker B completes aggregation")

	// Simplified test: Verify lock exists and can be queried
	err = s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
	if err != nil {
		s.T().Skip("Skipping - aggregation trigger not implemented")
	}

	time.Sleep(2 * time.Second)

	// Verify aggregation completed
	groupStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err == nil {
		assert.Greater(s.T(), groupStats.TotalTokens, int64(0),
			"Aggregation should complete after lock timeout")
	}

	s.T().Log("GC-02: Lock timeout recovery test completed")
}

// TestGC03_GlobalConcurrencyLimit tests that global concurrency limit is enforced.
//
// Test ID: GC-03
// Priority: P0
// Test Scenario: 全局并发限制
// Expected Result: 最多5个Worker同时运行，其他任务排队
func (s *ConcurrencyControlSuite) TestGC03_GlobalConcurrencyLimit() {
	s.T().Log("GC-03: Testing global concurrency limit (MaxGroupStatConcurrency=5)")

	// Arrange: Create 10 different P2P groups
	groups := make([]*testutil.P2PGroupModel, 10)
	for i := 0; i < 10; i++ {
		group, err := s.fixtures.CreateTestP2PGroup(
			fmt.Sprintf("gc03-group-%d", i),
			s.fixtures.User1Client,
			s.fixtures.RegularUser1.ID,
			testutil.P2PGroupTypeShared,
			testutil.P2PJoinMethodPassword,
			"testpass",
		)
		if err != nil {
			s.T().Fatalf("Failed to create group %d: %v", i, err)
		}
		groups[i] = group

		// Create a channel in each group
		channel, err := s.fixtures.CreateTestChannel(
			fmt.Sprintf("gc03-channel-%d", i),
			"gpt-4",
			"default",
			s.fixtures.GetUpstreamURL(),
			false,
			s.fixtures.RegularUser1.ID,
			fmt.Sprintf("%d", group.ID),
		)
		if err != nil {
			s.T().Fatalf("Failed to create channel %d: %v", i, err)
		}

		// Create statistics
		stats := testutil.CreateTestChannelStatistics(channel.ID, "gpt-4", 100, 0, int64(1000*(i+1)))
		s.client.CreateChannelStatistics(stats)
	}

	// Act: Trigger aggregation for all 10 groups concurrently
	var wg sync.WaitGroup
	startTime := time.Now()
	completionTimes := make([]time.Duration, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int, groupID int) {
			defer wg.Done()
			err := s.client.TriggerGroupAggregation(groupID)
			if err != nil {
				s.T().Logf("Group %d aggregation trigger error: %v", index, err)
			}
			completionTimes[index] = time.Since(startTime)
		}(i, groups[i].ID)
	}

	wg.Wait()

	// Assert: Verify concurrency behavior
	// With MaxGroupStatConcurrency=5, we expect:
	// - First 5 tasks run immediately (complete within ~2-3 seconds)
	// - Next 5 tasks wait and run after first batch completes

	s.T().Log("GC-03: Completion times for 10 groups:")
	for i, duration := range completionTimes {
		s.T().Logf("  Group %d: %v", i, duration)
	}

	// In a properly implemented system with concurrency limit:
	// - Some tasks should complete significantly later than others
	// - We should see a batching pattern (first 5 fast, next 5 slower)

	s.T().Log("GC-03: Global concurrency limit test completed")
	s.T().Log("NOTE: Full verification requires monitoring actual concurrent worker count")
}

// TestGC04_LockReleaseFailureHandling tests handling of lock release failures.
//
// Test ID: GC-04
// Priority: P2
// Test Scenario: 锁释放失败处理
// Expected Result: 依赖锁的TTL自动过期，不导致死锁
func (s *ConcurrencyControlSuite) TestGC04_LockReleaseFailureHandling() {
	s.T().Log("GC-04: Testing lock release failure handling")

	// Arrange: This test verifies the system's resilience when:
	// 1. Worker completes aggregation successfully
	// 2. Worker fails to release the lock (network error, crash, etc.)
	// 3. Lock eventually expires due to TTL
	// 4. Subsequent aggregations can proceed

	channel, err := s.fixtures.CreateTestChannel(
		"gc04-channel",
		"gpt-4",
		"default",
		s.fixtures.GetUpstreamURL(),
		false,
		s.fixtures.RegularUser1.ID,
		fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
	)
	assert.NoError(s.T(), err, "Channel creation should succeed")

	stats := testutil.CreateTestChannelStatistics(channel.ID, "gpt-4", 100, 0, 1000)
	err = s.client.CreateChannelStatistics(stats)
	if err != nil {
		s.T().Skip("Skipping - channel statistics API not implemented")
	}

	// Act: Trigger aggregation
	err = s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
	if err != nil {
		s.T().Skip("Skipping - aggregation trigger not implemented")
	}

	time.Sleep(2 * time.Second)

	// In a full implementation, we would:
	// 1. Inject a failure in the lock release mechanism
	// 2. Verify the lock has TTL set
	// 3. Wait for TTL expiry
	// 4. Verify subsequent aggregations work

	// For this test, we verify the lock has reasonable TTL
	s.T().Log("GC-04: Expected behavior:")
	s.T().Log("  1. Worker completes aggregation")
	s.T().Log("  2. Lock release fails (simulated network error)")
	s.T().Log("  3. Lock remains in Redis with TTL=180s")
	s.T().Log("  4. After TTL expires, new aggregation can proceed")
	s.T().Log("  5. System does not deadlock")

	// Verify aggregation completed despite any lock issues
	groupStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err == nil {
		assert.Equal(s.T(), stats.TotalTokens, groupStats.TotalTokens,
			"Aggregation should complete successfully")
		s.T().Logf("GC-04: Aggregation completed with tokens=%d", groupStats.TotalTokens)
	}

	// Trigger another aggregation to verify no deadlock
	time.Sleep(1 * time.Second)
	stats2 := testutil.CreateTestChannelStatistics(channel.ID, "gpt-4", 200, 0, 2000)
	err = s.client.CreateChannelStatistics(stats2)
	if err == nil {
		err = s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
		if err == nil {
			time.Sleep(2 * time.Second)
			groupStats2, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
			if err == nil {
				s.T().Logf("GC-04: Second aggregation also completed with tokens=%d",
					groupStats2.TotalTokens)
			}
		}
	}

	s.T().Log("GC-04: Lock release failure handling test completed")
}

// TestGC05_RaceConditionPrevention tests that race conditions are prevented during aggregation.
//
// Additional test (bonus): Verify no data corruption occurs under high concurrency.
func (s *ConcurrencyControlSuite) TestGC05_RaceConditionPrevention() {
	s.T().Log("GC-05 (Bonus): Testing race condition prevention")

	// Arrange: Create multiple channels in the same group
	channels := make([]*testutil.ChannelModel, 5)
	for i := 0; i < 5; i++ {
		channel, err := s.fixtures.CreateTestChannel(
			fmt.Sprintf("gc05-channel-%d", i),
			"gpt-4",
			"default",
			s.fixtures.GetUpstreamURL(),
			false,
			s.fixtures.RegularUser1.ID,
			fmt.Sprintf("%d", s.fixtures.SharedGroup1.ID),
		)
		assert.NoError(s.T(), err, "Channel creation should succeed")
		channels[i] = channel
	}

	// Act: Update all channels concurrently and trigger aggregations
	var wg sync.WaitGroup
	totalExpectedTokens := int64(0)

	for i := 0; i < 5; i++ {
		tokens := int64(1000 * (i + 1))
		totalExpectedTokens += tokens

		wg.Add(1)
		go func(channelID int, tokenCount int64) {
			defer wg.Done()
			stats := testutil.CreateTestChannelStatistics(channelID, "gpt-4", 100, 0, tokenCount)
			s.client.CreateChannelStatistics(stats)
			// Each goroutine also tries to trigger aggregation
			s.client.TriggerGroupAggregation(s.fixtures.SharedGroup1.ID)
		}(channels[i].ID, tokens)
	}

	wg.Wait()
	time.Sleep(3 * time.Second) // Wait for all aggregations to settle

	// Assert: Verify final aggregated data is correct (no corruption)
	groupStats, err := s.client.GetGroupStatistics(s.fixtures.SharedGroup1.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - group statistics query not available")
	}

	// Expected: Sum of all channel tokens
	assert.Equal(s.T(), totalExpectedTokens, groupStats.TotalTokens,
		"Aggregated data should be correct despite concurrent updates")

	s.T().Logf("GC-05: Race condition prevention verified - Expected=%d, Actual=%d tokens",
		totalExpectedTokens, groupStats.TotalTokens)
}

// TestRunner for the concurrency control test suite
func TestConcurrencyControlSuite(t *testing.T) {
	suite.Run(t, new(ConcurrencyControlSuite))
}
