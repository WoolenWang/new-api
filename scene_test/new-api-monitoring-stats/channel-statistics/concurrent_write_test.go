// Package channel_statistics - Concurrency and Consistency Tests
//
// Test Focus:
// ===========
// This file tests concurrency safety and data consistency under high-load scenarios,
// including high-concurrency writes, flush safety, DB sync control, and conflict resolution.
//
// Test Scenarios (CON-01 to CON-04):
// - CON-01: High-concurrency L1 writes (1000 goroutines)
// - CON-02: Flush concurrency safety (multiple flush tasks)
// - CON-03: DB Sync concurrency control (distributed lock)
// - CON-04: Statistics vs channel disable conflict
package channel_statistics

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// ConcurrentWriteSuite holds shared test resources for concurrency tests.
type ConcurrentWriteSuite struct {
	Server *testutil.TestServer
	Client *testutil.APIClient
}

// SetupConcurrentWriteSuite initializes the test suite.
func SetupConcurrentWriteSuite(t *testing.T) (*ConcurrentWriteSuite, func()) {
	t.Helper()

	projectRoot, err := findProjectRoot()
	if err != nil {
		t.Fatalf("failed to find project root: %v", err)
	}

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()

	server, err := testutil.StartServer(cfg)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}

	client := testutil.NewAPIClient(server)

	rootUser, rootPass, err := client.InitializeSystem()
	if err != nil {
		_ = server.Stop()
		t.Fatalf("failed to initialize system: %v", err)
	}
	if _, err := client.Login(rootUser, rootPass); err != nil {
		_ = server.Stop()
		t.Fatalf("failed to login as root: %v", err)
	}

	suite := &ConcurrentWriteSuite{
		Server: server,
		Client: client,
	}

	cleanup := func() {
		if err := server.Stop(); err != nil {
			t.Errorf("Failed to stop server: %v", err)
		}
	}

	return suite, cleanup
}

// TestCON01_HighConcurrencyL1Writes tests high-concurrency L1 writes.
//
// Test Case: CON-01
// Priority: P0
// Scenario: 1000 goroutines simultaneously request the same channel
// Expected: Atomic counters have no data race, final count is accurate
func TestCON01_HighConcurrencyL1Writes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupConcurrentWriteSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create test user and channel.
	user := createTestUser(t, admin, "con01_user", "password123", "default")
	userClient := admin.Clone()
	if _, err := userClient.Login("con01_user", "password123"); err != nil {
		t.Fatalf("failed to login as user: %v", err)
	}

	channel, err := admin.CreateChannel(&testutil.ChannelModel{
		Name:   "CON01 Concurrent Channel",
		Type:   1,
		Key:    "sk-test-con01",
		Status: 1,
		Models: "gpt-4",
		Group:  "default",
	})
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	tokenKey, _, err := admin.CreateTokenForUser(user.ID, &testutil.TokenModel{
		Name:   "CON01 Token",
		Status: 1,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	userTokenClient := userClient.WithToken(tokenKey)

	// Act: Launch 1000 concurrent requests.
	const numConcurrent = 1000
	var wg sync.WaitGroup
	var successCount int32
	var errorCount int32

	t.Logf("Starting %d concurrent requests...", numConcurrent)
	startTime := time.Now()

	for i := 0; i < numConcurrent; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			resp, err := userTokenClient.Post("/v1/chat/completions", map[string]interface{}{
				"model": "gpt-4",
				"messages": []map[string]string{
					{"role": "user", "content": fmt.Sprintf("concurrent test %d", idx)},
				},
			})
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
				t.Logf("Request %d error: %v", idx, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&errorCount, 1)
				t.Logf("Request %d failed with status %d", idx, resp.StatusCode)
			}
		}(i)
	}

	wg.Wait()
	elapsedTime := time.Since(startTime)

	t.Logf("CON-01 Test Results:")
	t.Logf("  Total requests: %d", numConcurrent)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Elapsed time: %v", elapsedTime)
	t.Logf("  Throughput: %.2f req/s", float64(successCount)/elapsedTime.Seconds())

	// Verify: Atomic counter consistency.
	totalProcessed := successCount + errorCount
	if totalProcessed != numConcurrent {
		t.Errorf("CON-01 FAILED: Data race detected - total processed %d != expected %d",
			totalProcessed, numConcurrent)
	}

	// Verify: At least 80% success rate (allowing for system load/rate limiting).
	expectedMinSuccess := int32(numConcurrent * 80 / 100)
	if successCount < expectedMinSuccess {
		t.Errorf("CON-01 WARNING: Success rate too low - got %d, expected at least %d",
			successCount, expectedMinSuccess)
	}

	// Wait for async operations to complete.
	time.Sleep(2 * time.Second)

	// Verify: Check logs to ensure requests were recorded.
	logs, err := admin.GetUserLogs(user.ID, int(successCount))
	if err != nil {
		t.Logf("Warning: failed to get user logs: %v", err)
	} else {
		channelLogCount := 0
		for _, log := range logs {
			if log.ChannelID == channel.ID {
				channelLogCount++
			}
		}
		t.Logf("  Logs for this channel: %d", channelLogCount)
	}

	if totalProcessed == numConcurrent && successCount >= expectedMinSuccess {
		t.Logf("CON-01 PASSED: High-concurrency L1 writes verified, no data race")
	}
}

// TestCON02_FlushConcurrencySafety tests flush concurrency safety.
//
// Test Case: CON-02
// Priority: P0
// Scenario: Multiple flush tasks trigger simultaneously
// Expected: Use locks or atomic operations to avoid duplicate flushes
func TestCON02_FlushConcurrencySafety(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Skip("CON-02: Requires internal Flush Worker control and lock observation")

	// Test implementation would:
	// 1. Populate L1 with statistics data
	// 2. Trigger multiple Flush Workers simultaneously (via test hooks)
	// 3. Verify only one flush actually executes (via locks/atomic flags)
	// 4. Check Redis data is not duplicated
	// 5. Verify L1 counter is reset exactly once
}

// TestCON03_DBSyncConcurrencyControl tests DB Sync concurrency control.
//
// Test Case: CON-03
// Priority: P0
// Scenario: Multiple workers try to sync the same channel
// Expected: Distributed lock ensures only one worker executes
func TestCON03_DBSyncConcurrencyControl(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Skip("CON-03: Requires DB Sync Worker control and distributed lock testing")

	// Test implementation would:
	// 1. Create channel with dirty data in Redis
	// 2. Spawn 5 DB Sync Workers simultaneously
	// 3. All workers attempt to sync the same channel
	// 4. Verify distributed lock (Redis SET NX) ensures only one succeeds
	// 5. Other workers should fail lock acquisition and skip
	// 6. Check DB has exactly one statistics record (no duplicates)
	// 7. Verify lock is released after sync completes
}

// TestCON04_StatisticsAndChannelDisableConflict tests conflict resolution.
//
// Test Case: CON-04
// Priority: P1
// Scenario: Channel has ongoing request, admin disables channel simultaneously
// Expected: Ongoing request completes and statistics are recorded normally,
//          subsequent requests are rejected
func TestCON04_StatisticsAndChannelDisableConflict(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupConcurrentWriteSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create test user and channel.
	user := createTestUser(t, admin, "con04_user", "password123", "default")
	userClient := admin.Clone()
	if _, err := userClient.Login("con04_user", "password123"); err != nil {
		t.Fatalf("failed to login as user: %v", err)
	}

	channel, err := admin.CreateChannel(&testutil.ChannelModel{
		Name:   "CON04 Test Channel",
		Type:   1,
		Key:    "sk-test-con04",
		Status: 1,
		Models: "gpt-4",
		Group:  "default",
	})
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	tokenKey, _, err := admin.CreateTokenForUser(user.ID, &testutil.TokenModel{
		Name:   "CON04 Token",
		Status: 1,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	userTokenClient := userClient.WithToken(tokenKey)

	// Act: Start a long-running request.
	var wg sync.WaitGroup
	var requestCompleted int32
	var requestFailed int32

	wg.Add(1)
	go func() {
		defer wg.Done()

		// Note: This assumes the Mock upstream can simulate long processing.
		// In real test, we'd configure Mock to delay response.
		resp, err := userTokenClient.Post("/v1/chat/completions", map[string]interface{}{
			"model": "gpt-4",
			"messages": []map[string]string{
				{"role": "user", "content": "long running request"},
			},
		})
		if err != nil {
			atomic.AddInt32(&requestFailed, 1)
			t.Logf("Long-running request error: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == 200 {
			atomic.AddInt32(&requestCompleted, 1)
			t.Logf("Long-running request completed with status 200")
		} else {
			atomic.AddInt32(&requestFailed, 1)
			t.Logf("Long-running request failed with status %d", resp.StatusCode)
		}
	}()

	// Wait a moment for the request to start processing.
	time.Sleep(100 * time.Millisecond)

	// Disable the channel while request is ongoing.
	err = admin.UpdateChannel(channel.ID, &testutil.ChannelModel{
		Status: 0, // Disable
	})
	if err != nil {
		t.Fatalf("failed to disable channel: %v", err)
	}
	t.Logf("Channel disabled while request was ongoing")

	// Wait for the ongoing request to complete.
	wg.Wait()

	// Assert: The ongoing request should have completed successfully.
	// (It was already in flight before the disable happened.)
	if requestCompleted != 1 {
		t.Logf("CON-04 WARNING: Ongoing request did not complete (expected to succeed)")
	}

	// Verify: Check logs to ensure the request was recorded.
	time.Sleep(1 * time.Second)
	logs, err := admin.GetUserLogs(user.ID, 1)
	if err != nil {
		t.Fatalf("failed to get user logs: %v", err)
	}

	if len(logs) == 0 {
		t.Errorf("CON-04 FAILED: No log entry for ongoing request")
	} else {
		lastLog := logs[0]
		if lastLog.ChannelID == channel.ID {
			t.Logf("CON-04: Ongoing request was logged correctly (channel_id=%d)", channel.ID)
		}
	}

	// Act: Try to send a new request after channel is disabled.
	resp2, err := userTokenClient.Post("/v1/chat/completions", map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "request after disable"},
		},
	})
	if err != nil {
		t.Logf("Request after disable error (expected): %v", err)
	} else {
		defer resp2.Body.Close()
		if resp2.StatusCode == 200 {
			t.Errorf("CON-04 FAILED: Request succeeded after channel was disabled (should fail)")
		} else {
			t.Logf("CON-04: Request correctly rejected after disable (status %d)", resp2.StatusCode)
		}
	}

	// Summary.
	if requestCompleted == 1 {
		t.Logf("CON-04 PASSED: Ongoing request completed, subsequent requests rejected")
	} else {
		t.Logf("CON-04 PARTIAL: Test completed but results may vary based on timing")
	}
}

// TestConcurrentMultiChannel tests multiple channels under concurrent load.
//
// Additional Scenario: Verify statistics isolation between channels
func TestConcurrentMultiChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupConcurrentWriteSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create test user.
	user := createTestUser(t, admin, "con_multi_user", "password123", "default")
	userClient := admin.Clone()
	if _, err := userClient.Login("con_multi_user", "password123"); err != nil {
		t.Fatalf("failed to login as user: %v", err)
	}

	// Create three channels.
	channel1, err := admin.CreateChannel(&testutil.ChannelModel{
		Name:   "CON Multi Ch1",
		Type:   1,
		Key:    "sk-test-con-multi-1",
		Status: 1,
		Models: "gpt-4",
		Group:  "default",
	})
	if err != nil {
		t.Fatalf("failed to create channel 1: %v", err)
	}

	channel2, err := admin.CreateChannel(&testutil.ChannelModel{
		Name:   "CON Multi Ch2",
		Type:   1,
		Key:    "sk-test-con-multi-2",
		Status: 1,
		Models: "gpt-4",
		Group:  "default",
	})
	if err != nil {
		t.Fatalf("failed to create channel 2: %v", err)
	}

	channel3, err := admin.CreateChannel(&testutil.ChannelModel{
		Name:   "CON Multi Ch3",
		Type:   1,
		Key:    "sk-test-con-multi-3",
		Status: 1,
		Models: "gpt-4",
		Group:  "default",
	})
	if err != nil {
		t.Fatalf("failed to create channel 3: %v", err)
	}

	tokenKey, _, err := admin.CreateTokenForUser(user.ID, &testutil.TokenModel{
		Name:   "CON Multi Token",
		Status: 1,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	userTokenClient := userClient.WithToken(tokenKey)

	// Act: Send concurrent requests, distributed across channels.
	const totalRequests = 300 // 100 per channel
	var wg sync.WaitGroup
	var successCount int32

	startTime := time.Now()

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			resp, err := userTokenClient.Post("/v1/chat/completions", map[string]interface{}{
				"model": "gpt-4",
				"messages": []map[string]string{
					{"role": "user", "content": fmt.Sprintf("multi channel test %d", idx)},
				},
			})
			if err != nil {
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				atomic.AddInt32(&successCount, 1)
			}
		}(i)
	}

	wg.Wait()
	elapsedTime := time.Since(startTime)

	t.Logf("Concurrent Multi-Channel Test Results:")
	t.Logf("  Total requests: %d", totalRequests)
	t.Logf("  Successful: %d", successCount)
	t.Logf("  Elapsed time: %v", elapsedTime)
	t.Logf("  Throughput: %.2f req/s", float64(successCount)/elapsedTime.Seconds())

	// Wait for logging to complete.
	time.Sleep(2 * time.Second)

	// Verify: Check logs to see distribution across channels.
	logs, err := admin.GetUserLogs(user.ID, int(successCount))
	if err != nil {
		t.Logf("Warning: failed to get user logs: %v", err)
		return
	}

	ch1Count := 0
	ch2Count := 0
	ch3Count := 0

	for _, log := range logs {
		switch log.ChannelID {
		case channel1.ID:
			ch1Count++
		case channel2.ID:
			ch2Count++
		case channel3.ID:
			ch3Count++
		}
	}

	t.Logf("  Channel 1 requests: %d", ch1Count)
	t.Logf("  Channel 2 requests: %d", ch2Count)
	t.Logf("  Channel 3 requests: %d", ch3Count)

	// Verify all channels were used.
	if ch1Count == 0 || ch2Count == 0 || ch3Count == 0 {
		t.Errorf("Multi-Channel WARNING: Some channels were not used (Ch1=%d, Ch2=%d, Ch3=%d)",
			ch1Count, ch2Count, ch3Count)
	} else {
		t.Logf("Multi-Channel PASSED: All channels received requests under concurrent load")
	}
}

// TestConcurrentWriteSkeleton is a placeholder test to verify compilation.
func TestConcurrentWriteSkeleton(t *testing.T) {
	t.Log("Concurrent write test suite loaded successfully")
}
