// Package cache_consistency contains integration tests for cache consistency.
//
// Test Focus:
// ===========
// This package validates cache behavior during concurrent operations,
// ensuring that permission changes are properly propagated and that
// in-flight requests are handled gracefully.
//
// Key Test Scenarios:
// - CON-01: Request during group membership revocation
// - CON-02: Request during channel disable
// - CON-03: Request during channel deletion
package cache_consistency

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// TestSuite holds shared test resources for cache consistency tests.
type TestSuite struct {
	Server   *testutil.TestServer
	Client   *testutil.APIClient
	Upstream *testutil.MockUpstreamServer
	Fixtures *testutil.TestFixtures
}

// conUpstreamDelay simulates a "long running" upstream request in CON-xx tests.
// We intentionally keep this delay relatively small so that the tests can still
// reliably exercise in-flight permission changes without risking hitting the
// global go test timeout when the entire cache-consistency suite runs together.
const conUpstreamDelay = 500 * time.Millisecond

// SetupSuite initializes the test suite with a running server.
func SetupSuite(t *testing.T) (*TestSuite, func()) {
	t.Helper()

	// Start mock upstream so we can control latency.
	upstream := testutil.NewMockUpstreamServer()
	t.Logf("Mock upstream started at: %s", upstream.BaseURL)

	projectRoot, err := findProjectRoot()
	if err != nil {
		upstream.Close()
		t.Fatalf("Failed to find project root: %v", err)
	}

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()

	server, err := testutil.StartServer(cfg)
	if err != nil {
		upstream.Close()
		t.Fatalf("Failed to start test server: %v", err)
	}

	client := testutil.NewAPIClient(server)

	// Initialize system and login as root.
	rootUser, rootPass, err := client.InitializeSystem()
	if err != nil {
		upstream.Close()
		_ = server.Stop()
		t.Fatalf("Failed to initialize system: %v", err)
	}
	if _, err := client.Login(rootUser, rootPass); err != nil {
		upstream.Close()
		_ = server.Stop()
		t.Fatalf("Failed to login as root: %v", err)
	}

	fixtures := testutil.NewTestFixtures(t, client)
	fixtures.SetUpstream(upstream)

	suite := &TestSuite{
		Server:   server,
		Client:   client,
		Upstream: upstream,
		Fixtures: fixtures,
	}

	cleanup := func() {
		fixtures.Cleanup()
		upstream.Close()
		if err := server.Stop(); err != nil {
			t.Errorf("Failed to stop server: %v", err)
		}
	}

	return suite, cleanup
}

func findProjectRoot() (string, error) {
	return testutil.FindProjectRoot()
}

// TestCache_CON01_KickDuringRequest tests behavior when user is kicked during an active request.
// Scenario: User A is making a long streaming request via P2P channel when kicked from group.
// Expected: Current request completes normally; subsequent requests fail immediately.
func TestCache_CON01_KickDuringRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures
	// Make upstream slow enough to let us change permissions mid-request.
	suite.Upstream.SetDelay(conUpstreamDelay)

	// Owner and member users.
	owner, err := fixtures.CreateTestUser("con01_owner", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
	member, err := fixtures.CreateTestUser("con01_member", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create member user: %v", err)
	}

	ownerClient := suite.Client.Clone()
	if _, err := ownerClient.Login("con01_owner", "password123"); err != nil {
		t.Fatalf("failed to login owner: %v", err)
	}
	memberClient := suite.Client.Clone()
	if _, err := memberClient.Login("con01_member", "password123"); err != nil {
		t.Fatalf("failed to login member: %v", err)
	}

	// Owner creates P2P group and member joins.
	group, err := fixtures.CreateTestP2PGroup(
		"con01-group",
		ownerClient,
		owner.ID,
		testutil.P2PGroupTypeShared,
		testutil.P2PJoinMethodPassword,
		"con01pass",
	)
	if err != nil {
		t.Fatalf("failed to create P2P group: %v", err)
	}

	if err := memberClient.ApplyToP2PGroup(group.ID, "con01pass"); err != nil {
		t.Fatalf("member failed to join group: %v", err)
	}

	// Channel restricted to this P2P group.
	channel, err := fixtures.CreateTestChannel(
		"con01-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		owner.ID,
		fmt.Sprintf("%d", group.ID),
	)
	if err != nil {
		t.Fatalf("failed to create P2P channel: %v", err)
	}
	t.Logf("CON-01 channel ID=%d", channel.ID)

	// Token for member, restricted to this P2P group.
	tokenKey, err := memberClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "con01-member-token",
		Status:             1,
		UnlimitedQuota:     true,
		P2PGroupID:         &group.ID,
		ModelLimitsJson:    "",
		ModelLimitsEnabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create member token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)

	// Start first long request in background.
	type result struct {
		success    bool
		statusCode int
		errMsg     string
	}

	var wg sync.WaitGroup
	wg.Add(1)
	resultCh := make(chan result, 1)

	go func() {
		defer wg.Done()
		success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "CON-01 long running request")
		resultCh <- result{success: success, statusCode: statusCode, errMsg: errMsg}
	}()

	// Wait a bit to ensure request is in-flight, then kick member.
	time.Sleep(500 * time.Millisecond)

	if err := ownerClient.UpdateMemberStatus(group.ID, member.ID, model.MemberStatusBanned); err != nil {
		t.Fatalf("failed to ban member during request: %v", err)
	}

	wg.Wait()
	res := <-resultCh
	if !res.success {
		t.Fatalf("expected first request to succeed despite kick, got status=%d err=%s", res.statusCode, res.errMsg)
	}

	// Second request after kick should fail due to no available P2P channel.
	success2, statusCode2, errMsg2 := apiClient.TryChatCompletion("gpt-4", "CON-01 after kick")
	if success2 {
		t.Fatalf("expected second request to fail after kick, but succeeded with status=%d", statusCode2)
	}
	t.Logf("second request failed as expected: status=%d err=%s", statusCode2, errMsg2)

	// Upstream should only see the first successful request.
	if suite.Upstream.GetRequestCount() != 1 {
		t.Fatalf("expected 1 upstream request, got %d", suite.Upstream.GetRequestCount())
	}
}

// TestCache_CON02_DisableDuringRequest tests behavior when channel is disabled during active request.
// Scenario: User A is making a long request when the channel is disabled.
// Expected: Current request completes; subsequent requests fail.
func TestCache_CON02_DisableDuringRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures
	suite.Upstream.SetDelay(conUpstreamDelay)

	// Single user and channel.
	_, err := fixtures.CreateTestUser("con02_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	userClient := suite.Client.Clone()
	if _, err := userClient.Login("con02_user", "password123"); err != nil {
		t.Fatalf("failed to login user: %v", err)
	}

	channel, err := fixtures.CreateTestChannel(
		"con02-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}
	t.Logf("CON-02 channel ID=%d", channel.ID)

	tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "con02-token",
		Status:             1,
		UnlimitedQuota:     true,
		ModelLimitsJson:    "",
		ModelLimitsEnabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)

	// First request in background.
	type result struct {
		success    bool
		statusCode int
		errMsg     string
	}
	var wg sync.WaitGroup
	wg.Add(1)
	resultCh := make(chan result, 1)

	go func() {
		defer wg.Done()
		success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "CON-02 long running request")
		resultCh <- result{success: success, statusCode: statusCode, errMsg: errMsg}
	}()

	time.Sleep(500 * time.Millisecond)

	// Disable the channel via admin API.
	var resp testutil.APIResponse
	if err := suite.Client.PutJSON("/api/channel", map[string]interface{}{
		"id":     channel.ID,
		"status": common.ChannelStatusManuallyDisabled,
	}, &resp); err != nil {
		t.Fatalf("failed to disable channel: %v", err)
	}
	if !resp.Success {
		t.Fatalf("disable channel API failed: %s", resp.Message)
	}

	wg.Wait()
	res := <-resultCh
	if !res.success {
		t.Fatalf("expected first request to succeed despite disable, got status=%d err=%s", res.statusCode, res.errMsg)
	}

	// Second request should fail due to disabled channel.
	success2, statusCode2, errMsg2 := apiClient.TryChatCompletion("gpt-4", "CON-02 after disable")
	if success2 {
		t.Fatalf("expected second request to fail after disable, but succeeded with status=%d", statusCode2)
	}
	t.Logf("second request failed as expected after disable: status=%d err=%s", statusCode2, errMsg2)

	// Upstream should only see the first successful request.
	if suite.Upstream.GetRequestCount() != 1 {
		t.Fatalf("expected 1 upstream request, got %d", suite.Upstream.GetRequestCount())
	}
}

// TestCache_CON03_DeleteDuringRequest tests behavior when channel is deleted during active request.
// Scenario: Similar to CON-02 but with channel deletion.
func TestCache_CON03_DeleteDuringRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures
	suite.Upstream.SetDelay(conUpstreamDelay)

	_, err := fixtures.CreateTestUser("con03_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	userClient := suite.Client.Clone()
	if _, err := userClient.Login("con03_user", "password123"); err != nil {
		t.Fatalf("failed to login user: %v", err)
	}

	channel, err := fixtures.CreateTestChannel(
		"con03-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}
	t.Logf("CON-03 channel ID=%d", channel.ID)

	tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "con03-token",
		Status:             1,
		UnlimitedQuota:     true,
		ModelLimitsJson:    "",
		ModelLimitsEnabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)

	type result struct {
		success    bool
		statusCode int
		errMsg     string
	}
	var wg sync.WaitGroup
	wg.Add(1)
	resultCh := make(chan result, 1)

	go func() {
		defer wg.Done()
		success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "CON-03 long running request")
		resultCh <- result{success: success, statusCode: statusCode, errMsg: errMsg}
	}()

	time.Sleep(500 * time.Millisecond)

	// Delete the channel via admin API.
	if err := suite.Client.DeleteChannel(channel.ID); err != nil {
		t.Fatalf("failed to delete channel: %v", err)
	}

	wg.Wait()
	res := <-resultCh
	if !res.success {
		t.Fatalf("expected first request to succeed despite delete, got status=%d err=%s", res.statusCode, res.errMsg)
	}

	// Second request should fail since channel is gone.
	success2, statusCode2, errMsg2 := apiClient.TryChatCompletion("gpt-4", "CON-03 after delete")
	if success2 {
		t.Fatalf("expected second request to fail after delete, but succeeded with status=%d", statusCode2)
	}
	t.Logf("second request failed as expected after delete: status=%d err=%s", statusCode2, errMsg2)

	if suite.Upstream.GetRequestCount() != 1 {
		t.Fatalf("expected 1 upstream request, got %d", suite.Upstream.GetRequestCount())
	}
}

// TestCache_Invalidation tests that cache is properly invalidated on membership changes.
func TestCache_Invalidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Owner and member users.
	owner, err := fixtures.CreateTestUser("cache_inv_owner", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
	member, err := fixtures.CreateTestUser("cache_inv_member", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create member user: %v", err)
	}

	ownerClient := suite.Client.Clone()
	if _, err := ownerClient.Login("cache_inv_owner", "password123"); err != nil {
		t.Fatalf("failed to login owner: %v", err)
	}
	memberClient := suite.Client.Clone()
	if _, err := memberClient.Login("cache_inv_member", "password123"); err != nil {
		t.Fatalf("failed to login member: %v", err)
	}

	// Owner creates shared P2P group with password join; member is NOT added yet.
	group, err := fixtures.CreateTestP2PGroup(
		"cache-inv-group",
		ownerClient,
		owner.ID,
		testutil.P2PGroupTypeShared,
		testutil.P2PJoinMethodPassword,
		"invpass",
	)
	if err != nil {
		t.Fatalf("failed to create P2P group: %v", err)
	}

	// Channel restricted to this P2P group only.
	channel, err := fixtures.CreateTestChannel(
		"cache-inv-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		owner.ID,
		fmt.Sprintf("%d", group.ID),
	)
	if err != nil {
		t.Fatalf("failed to create P2P channel: %v", err)
	}
	t.Logf("cache invalidation channel ID=%d", channel.ID)

	// Token for member, restricted to this P2P group.
	tokenKey, err := memberClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "cache-inv-member-token",
		Status:             1,
		UnlimitedQuota:     true,
		P2PGroupID:         &group.ID,
		ModelLimitsJson:    "",
		ModelLimitsEnabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create member token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)

	// 1) User is NOT a member of group yet; routing should fail and not hit upstream.
	success1, statusCode1, errMsg1 := apiClient.TryChatCompletion("gpt-4", "cache invalidation: before join")
	if success1 {
		t.Fatalf("expected request before membership to fail, got status=%d", statusCode1)
	}
	if got := suite.Upstream.GetRequestCount(); got != 0 {
		t.Fatalf("expected 0 upstream requests before membership, got %d", got)
	}
	t.Logf("before join request failed as expected: status=%d err=%s", statusCode1, errMsg1)

	// 2) Member joins group via password; membership caches should be invalidated by backend.
	if err := memberClient.ApplyToP2PGroup(group.ID, "invpass"); err != nil {
		t.Fatalf("member failed to join group: %v", err)
	}

	// 3) After joining, routing should succeed and hit upstream once.
	success2, statusCode2, errMsg2 := apiClient.TryChatCompletion("gpt-4", "cache invalidation: after join")
	if !success2 {
		t.Fatalf("expected request after join to succeed, got status=%d err=%s", statusCode2, errMsg2)
	}
	if got := suite.Upstream.GetRequestCount(); got != 1 {
		t.Fatalf("expected 1 upstream request after join, got %d", got)
	}

	// 4) Owner bans member; caches should be invalidated again.
	if err := ownerClient.UpdateMemberStatus(group.ID, member.ID, model.MemberStatusBanned); err != nil {
		t.Fatalf("failed to ban member: %v", err)
	}

	// 5) After ban, routing should fail and not send additional upstream requests.
	success3, statusCode3, errMsg3 := apiClient.TryChatCompletion("gpt-4", "cache invalidation: after ban")
	if success3 {
		t.Fatalf("expected request after ban to fail, but succeeded with status=%d", statusCode3)
	}
	if got := suite.Upstream.GetRequestCount(); got != 1 {
		t.Fatalf("expected upstream request count to remain 1 after ban, got %d", got)
	}
	t.Logf("after ban request failed as expected: status=%d err=%s", statusCode3, errMsg3)
}

// TestCache_UserGroupsLoad tests the user groups cache loading mechanism.
func TestCache_UserGroupsLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Owner and member.
	owner, err := fixtures.CreateTestUser("cache_load_owner", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
	if _, err := fixtures.CreateTestUser("cache_load_member", "password123", "default"); err != nil {
		t.Fatalf("failed to create member user: %v", err)
	}

	ownerClient := suite.Client.Clone()
	if _, err := ownerClient.Login("cache_load_owner", "password123"); err != nil {
		t.Fatalf("failed to login owner: %v", err)
	}
	memberClient := suite.Client.Clone()
	if _, err := memberClient.Login("cache_load_member", "password123"); err != nil {
		t.Fatalf("failed to login member: %v", err)
	}

	// Create several P2P groups and add member to each, exercising membership cache.
	const groupCount = 3
	var lastGroupID int
	for i := 1; i <= groupCount; i++ {
		group, err := fixtures.CreateTestP2PGroup(
			fmt.Sprintf("cache-load-g%d", i),
			ownerClient,
			owner.ID,
			testutil.P2PGroupTypeShared,
			testutil.P2PJoinMethodPassword,
			fmt.Sprintf("cl-pass-%d", i),
		)
		if err != nil {
			t.Fatalf("failed to create group %d: %v", i, err)
		}
		if err := memberClient.ApplyToP2PGroup(group.ID, fmt.Sprintf("cl-pass-%d", i)); err != nil {
			t.Fatalf("member failed to join group %d: %v", i, err)
		}
		lastGroupID = group.ID
	}

	// Channel authorized for the last group.
	channel, err := fixtures.CreateTestChannel(
		"cache-load-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		owner.ID,
		fmt.Sprintf("%d", lastGroupID),
	)
	if err != nil {
		t.Fatalf("failed to create P2P channel: %v", err)
	}
	t.Logf("cache load channel ID=%d, group=%d", channel.ID, lastGroupID)

	// Token restricting to the last group; routing should rely on user group cache.
	tokenKey, err := memberClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "cache-load-token",
		Status:             1,
		UnlimitedQuota:     true,
		P2PGroupID:         &lastGroupID,
		ModelLimitsJson:    "",
		ModelLimitsEnabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)

	// First request triggers cache load.
	start1 := time.Now()
	success1, statusCode1, errMsg1 := apiClient.TryChatCompletion("gpt-4", "cache user groups load - first")
	elapsed1 := time.Since(start1)
	if !success1 {
		t.Fatalf("expected first request to succeed, got status=%d err=%s", statusCode1, errMsg1)
	}

	// Second request should also succeed and reuse cached memberships internally.
	start2 := time.Now()
	success2, statusCode2, errMsg2 := apiClient.TryChatCompletion("gpt-4", "cache user groups load - second")
	elapsed2 := time.Since(start2)
	if !success2 {
		t.Fatalf("expected second request to succeed, got status=%d err=%s", statusCode2, errMsg2)
	}

	t.Logf("cache load first request latency=%v, second=%v", elapsed1, elapsed2)
	if suite.Upstream.GetRequestCount() != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", suite.Upstream.GetRequestCount())
	}
}

// TestCache_HighConcurrency tests cache behavior under high concurrency.
func TestCache_HighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Owner and member for a shared P2P group.
	owner, err := fixtures.CreateTestUser("cache_conc_owner", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
	if _, err := fixtures.CreateTestUser("cache_conc_member", "password123", "default"); err != nil {
		t.Fatalf("failed to create member user: %v", err)
	}

	ownerClient := suite.Client.Clone()
	if _, err := ownerClient.Login("cache_conc_owner", "password123"); err != nil {
		t.Fatalf("failed to login owner: %v", err)
	}
	memberClient := suite.Client.Clone()
	if _, err := memberClient.Login("cache_conc_member", "password123"); err != nil {
		t.Fatalf("failed to login member: %v", err)
	}

	group, err := fixtures.CreateTestP2PGroup(
		"cache-conc-group",
		ownerClient,
		owner.ID,
		testutil.P2PGroupTypeShared,
		testutil.P2PJoinMethodPassword,
		"concpass",
	)
	if err != nil {
		t.Fatalf("failed to create P2P group: %v", err)
	}
	if err := memberClient.ApplyToP2PGroup(group.ID, "concpass"); err != nil {
		t.Fatalf("member failed to join group: %v", err)
	}

	channel, err := fixtures.CreateTestChannel(
		"cache-conc-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		owner.ID,
		fmt.Sprintf("%d", group.ID),
	)
	if err != nil {
		t.Fatalf("failed to create P2P channel: %v", err)
	}
	t.Logf("cache concurrency channel ID=%d", channel.ID)

	tokenKey, err := memberClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "cache-conc-token",
		Status:             1,
		UnlimitedQuota:     true,
		P2PGroupID:         &group.ID,
		ModelLimitsJson:    "",
		ModelLimitsEnabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)

	const concurrency = 20
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ok, status, msg := apiClient.TryChatCompletion("gpt-4", fmt.Sprintf("cache concurrency request #%d", idx))
			if !ok {
				errCh <- fmt.Errorf("request #%d failed: status=%d err=%s", idx, status, msg)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("high concurrency request error: %v", err)
		}
	}

	if got := suite.Upstream.GetRequestCount(); got != concurrency {
		t.Fatalf("expected %d upstream requests under concurrency, got %d", concurrency, got)
	}
}

// TestCacheConsistencySkeleton is a placeholder test to verify the test file compiles.
func TestCacheConsistencySkeleton(t *testing.T) {
	t.Log("Cache consistency test skeleton loaded successfully")
}
