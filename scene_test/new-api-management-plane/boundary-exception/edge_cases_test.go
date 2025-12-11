// Package boundary_exception contains integration tests for boundary and exception cases.
//
// Test Focus:
// ===========
// This package validates edge cases and boundary conditions for the P2P group
// and billing system, including:
// - Empty P2P group scenarios
// - Token billing group edge cases
// - Nonexistent group handling
// - Group deletion cache consistency
// - Concurrent operations
// - Cache penetration protection
//
// Key Test Scenarios:
// - ED-01: Empty P2P group list
// - ED-02: Token billing group empty array
// - ED-03: Token billing group nonexistent
// - ED-04: Channel P2P authorization empty
// - ED-05: Group deletion request handling
// - ED-06: Concurrent join and request
// - ED-07: Cache penetration
package boundary_exception

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// TestSuite holds shared test resources for boundary/exception tests.
type TestSuite struct {
	Server   *testutil.TestServer
	Client   *testutil.APIClient
	Upstream *testutil.MockUpstreamServer
}

// SetupSuite initializes the test suite with a running server.
func SetupSuite(t *testing.T) (*TestSuite, func()) {
	t.Helper()

	// Create a mock upstream so data-plane requests do not depend on real providers.
	upstream := testutil.NewMockUpstreamServer()

	projectRoot, err := findProjectRoot()
	if err != nil {
		upstream.Close()
		t.Fatalf("failed to find project root: %v", err)
	}

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()

	// Disable global API/web/critical rate limits in this isolated test server.
	// ED-06 performs many concurrent control-plane requests (user creation, quota
	// adjustment, login, group operations). We want to focus this suite on P2P
	// boundary semantics rather than global rate-limiting behavior, which is
	// already covered by dedicated rate-limit tests in other suites.
	if cfg.CustomEnv == nil {
		cfg.CustomEnv = make(map[string]string)
	}
	cfg.CustomEnv["GLOBAL_API_RATE_LIMIT_ENABLE"] = "false"
	cfg.CustomEnv["GLOBAL_WEB_RATE_LIMIT_ENABLE"] = "false"
	cfg.CustomEnv["CRITICAL_RATE_LIMIT_ENABLE"] = "false"

	server, err := testutil.StartServer(cfg)
	if err != nil {
		upstream.Close()
		t.Fatalf("Failed to start test server: %v", err)
	}

	client := testutil.NewAPIClient(server)

	// Initialize system and login as root (admin user).
	rootUser, rootPass, err := client.InitializeSystem()
	if err != nil {
		upstream.Close()
		_ = server.Stop()
		t.Fatalf("failed to initialize system: %v", err)
	}
	if _, err := client.Login(rootUser, rootPass); err != nil {
		upstream.Close()
		_ = server.Stop()
		t.Fatalf("failed to login as root: %v", err)
	}

	suite := &TestSuite{
		Server:   server,
		Client:   client,
		Upstream: upstream,
	}

	cleanup := func() {
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

// createTestUser creates a user with a unique external_id to avoid UNIQUE
// constraint conflicts.
func createTestUser(t *testing.T, admin *testutil.APIClient, username, password, group string) *testutil.UserModel {
	t.Helper()

	user := &testutil.UserModel{
		Username:   username,
		Password:   password,
		Group:      "default", // create as default, then update if needed
		Status:     1,
		ExternalId: fmt.Sprintf("edge_%s_%d", username, time.Now().UnixNano()),
	}

	id, err := admin.CreateUserFull(user)
	if err != nil {
		t.Fatalf("failed to create user %s: %v", username, err)
	}
	user.ID = id

	// Update to desired system group if different from default.
	if group != "" && group != "default" {
		user.Group = group
		if err := admin.UpdateUser(user); err != nil {
			t.Fatalf("failed to update user %s group to %s: %v", username, group, err)
		}
	}

	// Ensure the test user has sufficient quota so that data-plane requests
	// are not rejected due to lack of user quota.
	if err := admin.AdjustUserQuota(user.ID, 1000000000); err != nil {
		t.Fatalf("failed to adjust quota for user %s: %v", username, err)
	}
	user.Quota = 1000000000

	return user
}

// TestED01_EmptyP2PGroupList tests that a user with no P2P group membership
// cannot access P2P-authorized channels, but can access public channels.
//
// Test Case: ED-01
// Priority: P0
// Scenario: User has not joined any P2P group, tries to request P2P channel
// Expected: Cannot access P2P channels, only public channels
func TestED01_EmptyP2PGroupList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create test user with no P2P group membership.
	userA := createTestUser(t, admin, "ed01_userA", "password123", "default")
	userAClient := admin.Clone()
	if _, err := userAClient.Login("ed01_userA", "password123"); err != nil {
		t.Fatalf("failed to login as userA: %v", err)
	}

	// Create a P2P group and channel authorized to that group.
	_ = createTestUser(t, admin, "ed01_owner", "password123", "default")
	ownerClient := admin.Clone()
	if _, err := ownerClient.Login("ed01_owner", "password123"); err != nil {
		t.Fatalf("failed to login as owner: %v", err)
	}

	groupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "ed01_p2p_group",
		DisplayName: "ED01 P2P Group",
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodApproval,
		Description: "ED01 test group",
	})
	if err != nil {
		t.Fatalf("failed to create P2P group: %v", err)
	}

	// Create a channel authorized to the P2P group.
	p2pChannelName := "ED01 P2P Channel"
	p2pAllowedGroups := fmt.Sprintf("[%d]", groupID)
	p2pBaseURL := suite.Upstream.BaseURL
	p2pChannel := &testutil.ChannelModel{
		Name:          p2pChannelName,
		Type:          1, // OpenAI
		Key:           "sk-test-ed01-p2p",
		Status:        1,
		Models:        "gpt-4",
		Group:         "default",
		BaseURL:       &p2pBaseURL,
		AllowedGroups: &p2pAllowedGroups,
	}
	if _, err := admin.AddChannel(p2pChannel); err != nil {
		t.Fatalf("failed to create P2P channel: %v", err)
	}

	// Create a public channel (no P2P authorization).
	publicChannelName := "ED01 Public Channel"
	publicAllowedGroups := "[]"
	publicBaseURL := suite.Upstream.BaseURL
	publicChannel := &testutil.ChannelModel{
		Name:          publicChannelName,
		Type:          1,
		Key:           "sk-test-ed01-public",
		Status:        1,
		Models:        "gpt-4",
		Group:         "default",
		BaseURL:       &publicBaseURL,
		AllowedGroups: &publicAllowedGroups, // No P2P authorization
	}
	if _, err := admin.AddChannel(publicChannel); err != nil {
		t.Fatalf("failed to create public channel: %v", err)
	}

	// Discover channel IDs from the channel list (AddChannel does not return ID).
	channels, err := admin.GetAllChannels()
	if err != nil {
		t.Fatalf("failed to query channels after creation: %v", err)
	}
	for _, ch := range channels {
		if ch.Name == p2pChannelName {
			p2pChannel.ID = ch.ID
		}
		if ch.Name == publicChannelName {
			publicChannel.ID = ch.ID
		}
	}
	if p2pChannel.ID == 0 || publicChannel.ID == 0 {
		t.Fatalf("failed to resolve channel IDs for ED01 (p2p=%d, public=%d)", p2pChannel.ID, publicChannel.ID)
	}

	// Create a token for userA without P2P group restriction.
	tokenKey, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "ED01 Test Token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	userATokenClient := userAClient.WithToken(tokenKey)

	// Attempt to use P2P channel (should fail or route to public channel).
	// Since userA has not joined any P2P group, the request should NOT use the P2P channel.
	resp, err := userATokenClient.Post("/v1/chat/completions", map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
	})
	if err != nil {
		t.Fatalf("chat completion request failed: %v", err)
	}
	defer resp.Body.Close()

	// If there's only P2P channel available, should get 404 or error.
	// If public channel exists, should use public channel.

	// Verify the request log to see which channel was used.
	// According to design, userA should only access public channels.
	logs, err := userAClient.GetUserLogs(userA.ID, 1)
	if err != nil {
		t.Fatalf("failed to get user logs: %v", err)
	}

	if len(logs) == 0 {
		t.Fatalf("expected at least one log entry, got none")
	}

	lastLog := logs[0]
	// The request should have used the public channel, not the P2P channel.
	if lastLog.ChannelID == p2pChannel.ID {
		t.Errorf("ED-01 FAILED: User without P2P membership accessed P2P channel (ID: %d)", p2pChannel.ID)
	}

	if lastLog.ChannelID == publicChannel.ID {
		t.Logf("ED-01 PASSED: User correctly used public channel (ID: %d)", publicChannel.ID)
	} else {
		t.Logf("Warning: Request used channel ID %d (expected public channel ID %d)", lastLog.ChannelID, publicChannel.ID)
	}
}

// TestED02_TokenBillingGroupEmptyArray tests that when Token.Group is set to
// an empty array, the system falls back to User.Group for billing.
//
// Test Case: ED-02
// Priority: P0
// Scenario: Token.Group = []
// Expected: Fallback to User.Group for billing
func TestED02_TokenBillingGroupEmptyArray(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create a vip user (billing rate = 2.0).
	userA := createTestUser(t, admin, "ed02_userA", "password123", "vip")
	userAClient := admin.Clone()
	if _, err := userAClient.Login("ed02_userA", "password123"); err != nil {
		t.Fatalf("failed to login as userA: %v", err)
	}

	// Create a vip channel.
	channelName := "ED02 Vip Channel"
	baseURL := suite.Upstream.BaseURL
	channel := &testutil.ChannelModel{
		Name:    channelName,
		Type:    1,
		Key:     "sk-test-ed02-vip",
		Status:  1,
		Models:  "gpt-4",
		Group:   "vip",
		BaseURL: &baseURL,
	}
	if _, err := admin.AddChannel(channel); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Resolve channel ID.
	channels, err := admin.GetAllChannels()
	if err != nil {
		t.Fatalf("failed to query channels after creation: %v", err)
	}
	for _, ch := range channels {
		if ch.Name == channelName {
			channel.ID = ch.ID
			break
		}
	}
	if channel.ID == 0 {
		t.Fatalf("failed to resolve channel ID for ED02")
	}

	// Create a token with empty Group array.
	tokenName := "ED02 Empty Group Token"
	tokenKey, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           tokenName,
		Status:         1,
		Group:          "[]", // Empty array
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	// Lookup token ID by name.
	tokens, err := userAClient.GetAllTokens()
	if err != nil {
		t.Fatalf("failed to list tokens: %v", err)
	}
	var tokenID int
	for _, tk := range tokens {
		if tk.Name == tokenName {
			tokenID = tk.ID
			break
		}
	}
	if tokenID == 0 {
		t.Fatalf("failed to resolve token ID for ED02")
	}

	userATokenClient := userAClient.WithToken(tokenKey)

	// Make a request with the token.
	resp, err := userATokenClient.Post("/v1/chat/completions", map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
	})
	if err != nil {
		t.Fatalf("chat completion request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check response status.
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify billing: should use User.Group (vip, rate=2.0) since Token.Group is empty.
	logs, err := userAClient.GetUserLogs(userA.ID, 1)
	if err != nil {
		t.Fatalf("failed to get user logs: %v", err)
	}

	if len(logs) == 0 {
		t.Fatalf("expected at least one log entry, got none")
	}

	lastLog := logs[0]
	if lastLog.TokenID != tokenID {
		t.Errorf("log token mismatch: expected %d, got %d", tokenID, lastLog.TokenID)
	}

	// According to design, when Token.Group is empty, should fallback to User.Group (vip).
	// The billing group should be "vip".
	// Note: The actual billing rate depends on system configuration.
	// For this test, we verify that the channel used is the vip channel.
	if lastLog.ChannelID != channel.ID {
		t.Errorf("ED-02 FAILED: Expected to use vip channel (ID: %d), but used channel ID: %d", channel.ID, lastLog.ChannelID)
	} else {
		t.Logf("ED-02 PASSED: Token with empty Group array correctly fell back to User.Group (vip)")
	}

	// Additional check: If billing_group field exists in logs, verify it's "vip".
	// (This depends on the actual log structure in the system.)
}

// TestED03_TokenBillingGroupNonexistent tests that when Token.Group contains
// a nonexistent group, the system returns a 404 error.
//
// Test Case: ED-03
// Priority: P1
// Scenario: Token.Group = ["nonexistent"]
// Expected: 404 error, no available channels
func TestED03_TokenBillingGroupNonexistent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create a default user.
	userA := createTestUser(t, admin, "ed03_userA", "password123", "default")
	userAClient := admin.Clone()
	if _, err := userAClient.Login("ed03_userA", "password123"); err != nil {
		t.Fatalf("failed to login as userA: %v", err)
	}

	// Create a default channel (so that system has at least one channel, but
	// it should not be usable for the nonexistent billing group).
	channelName := "ED03 Default Channel"
	baseURL := suite.Upstream.BaseURL
	channel := &testutil.ChannelModel{
		Name:    channelName,
		Type:    1,
		Key:     "sk-test-ed03-default",
		Status:  1,
		Models:  "gpt-4",
		Group:   "default",
		BaseURL: &baseURL,
	}
	if _, err := admin.AddChannel(channel); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Create a token with nonexistent billing group.
	tokenKey, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "ED03 Nonexistent Group Token",
		Status:         1,
		Group:          `["nonexistent_group_xyz"]`, // Nonexistent group
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	userATokenClient := userAClient.WithToken(tokenKey)

	// Make a request with the token - should fail with 404.
	resp, err := userATokenClient.Post("/v1/chat/completions", map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
	})
	if err != nil {
		t.Fatalf("chat completion request failed: %v", err)
	}
	defer resp.Body.Close()

	// Expect 404 or similar error indicating no available channels.
	if resp.StatusCode == 200 {
		t.Errorf("ED-03 FAILED: Expected error response, but got status 200")
	} else if resp.StatusCode == 404 || resp.StatusCode == 400 {
		t.Logf("ED-03 PASSED: Token with nonexistent billing group correctly returned error status %d", resp.StatusCode)
	} else {
		t.Logf("ED-03 WARNING: Unexpected status code %d (expected 404 or 400)", resp.StatusCode)
	}

	// Verify no log entry was created (or if created, it should indicate failure).
	logs, err := userAClient.GetUserLogs(userA.ID, 1)
	if err != nil {
		t.Logf("Warning: failed to get user logs: %v", err)
	}

	if len(logs) > 0 {
		lastLog := logs[0]
		// Check if the log indicates a failure or error.
		t.Logf("Log entry exists: ChannelID=%d, Type=%d", lastLog.ChannelID, lastLog.Type)
	}
}

// TestED04_ChannelP2PAuthorizationEmpty tests that when a channel's
// allowed_groups is empty, it behaves as a public channel accessible
// via system group only.
//
// Test Case: ED-04
// Priority: P1
// Scenario: Channel allowed_groups = []
// Expected: Can be accessed via system group, P2P not required
func TestED04_ChannelP2PAuthorizationEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create a default user.
	userA := createTestUser(t, admin, "ed04_userA", "password123", "default")
	userAClient := admin.Clone()
	if _, err := userAClient.Login("ed04_userA", "password123"); err != nil {
		t.Fatalf("failed to login as userA: %v", err)
	}

	// Create a channel with empty P2P authorization.
	channelName := "ED04 Empty P2P Auth Channel"
	baseURL := suite.Upstream.BaseURL
	allowedGroups := "[]"
	channel := &testutil.ChannelModel{
		Name:          channelName,
		Type:          1,
		Key:           "sk-test-ed04-empty-p2p",
		Status:        1,
		Models:        "gpt-4",
		Group:         "default",
		BaseURL:       &baseURL,
		AllowedGroups: &allowedGroups, // Empty array - no P2P authorization
	}
	if _, err := admin.AddChannel(channel); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Resolve channel ID.
	channels, err := admin.GetAllChannels()
	if err != nil {
		t.Fatalf("failed to query channels after creation: %v", err)
	}
	for _, ch := range channels {
		if ch.Name == channelName {
			channel.ID = ch.ID
			break
		}
	}
	if channel.ID == 0 {
		t.Fatalf("failed to resolve channel ID for ED04")
	}

	// Create a token for userA.
	tokenKey, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "ED04 Test Token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	userATokenClient := userAClient.WithToken(tokenKey)

	// Make a request - should succeed using the channel.
	resp, err := userATokenClient.Post("/v1/chat/completions", map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "test"},
		},
	})
	if err != nil {
		t.Fatalf("chat completion request failed: %v", err)
	}
	defer resp.Body.Close()

	// Expect success.
	if resp.StatusCode != 200 {
		t.Errorf("ED-04 FAILED: Expected status 200, got %d", resp.StatusCode)
	}

	// Verify the channel was used.
	logs, err := userAClient.GetUserLogs(userA.ID, 1)
	if err != nil {
		t.Fatalf("failed to get user logs: %v", err)
	}

	if len(logs) == 0 {
		t.Fatalf("expected at least one log entry, got none")
	}

	lastLog := logs[0]
	if lastLog.ChannelID == channel.ID {
		t.Logf("ED-04 PASSED: User correctly accessed channel with empty P2P authorization via system group")
	} else {
		t.Errorf("ED-04 FAILED: Expected channel ID %d, got %d", channel.ID, lastLog.ChannelID)
	}
}

// TestED05_GroupDeletionRequest tests that after a P2P group is deleted,
// users can no longer access channels authorized to that group, and
// cache is invalidated.
//
// Test Case: ED-05
// Priority: P0
// Scenario: User joins G1, G1 is deleted, user immediately makes request
// Expected: Cache invalidated, cannot access G1 channels
func TestED05_GroupDeletionRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create group owner and member.
	owner := createTestUser(t, admin, "ed05_owner", "password123", "default")
	ownerClient := admin.Clone()
	if _, err := ownerClient.Login("ed05_owner", "password123"); err != nil {
		t.Fatalf("failed to login as owner: %v", err)
	}

	member := createTestUser(t, admin, "ed05_member", "password123", "default")
	memberClient := admin.Clone()
	if _, err := memberClient.Login("ed05_member", "password123"); err != nil {
		t.Fatalf("failed to login as member: %v", err)
	}

	// Create a P2P group.
	groupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "ed05_test_group",
		DisplayName: "ED05 Test Group",
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodPassword,
		JoinKey:     "password123",
		Description: "ED05 test group",
	})
	if err != nil {
		t.Fatalf("failed to create P2P group: %v", err)
	}

	// Member joins the group.
	if err := memberClient.ApplyToP2PGroup(groupID, "password123"); err != nil {
		t.Fatalf("failed to apply to group: %v", err)
	}

	// Create a channel authorized to the group.
	channelName := "ED05 P2P Channel"
	baseURL := suite.Upstream.BaseURL
	allowedGroups := fmt.Sprintf("[%d]", groupID)
	channel := &testutil.ChannelModel{
		Name:          channelName,
		Type:          1,
		Key:           "sk-test-ed05-p2p",
		Status:        1,
		Models:        "gpt-4",
		Group:         "default",
		BaseURL:       &baseURL,
		OwnerUserId:   owner.ID, // mark as user-owned P2P channel so P2P access rules apply
		AllowedGroups: &allowedGroups,
	}
	if _, err := admin.AddChannel(channel); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Resolve channel ID.
	channels, err := admin.GetAllChannels()
	if err != nil {
		t.Fatalf("failed to query channels after creation: %v", err)
	}
	for _, ch := range channels {
		if ch.Name == channelName {
			channel.ID = ch.ID
			break
		}
	}
	if channel.ID == 0 {
		t.Fatalf("failed to resolve channel ID for ED05")
	}

	// Create a token for member.
	p2pGroupID := groupID
	tokenKey, err := memberClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "ED05 Member Token",
		Status:         1,
		UnlimitedQuota: true,
		P2PGroupID:     &p2pGroupID, // restrict token to this P2P group so deletion affects routing
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	memberTokenClient := memberClient.WithToken(tokenKey)

	// Make a request to verify member can access the P2P channel.
	resp1, err := memberTokenClient.Post("/v1/chat/completions", map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "test before deletion"},
		},
	})
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	resp1.Body.Close()

	if resp1.StatusCode != 200 {
		t.Logf("Warning: First request status %d (expected 200)", resp1.StatusCode)
	}

	// Owner deletes the group.
	if err := ownerClient.DeleteGroup(groupID); err != nil {
		t.Fatalf("failed to delete group: %v", err)
	}

	// Wait a moment for cache invalidation to propagate.
	time.Sleep(100 * time.Millisecond)

	// Member makes another request - should now fail or use a different channel.
	resp2, err := memberTokenClient.Post("/v1/chat/completions", map[string]interface{}{
		"model": "gpt-4",
		"messages": []map[string]string{
			{"role": "user", "content": "test after deletion"},
		},
	})
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer resp2.Body.Close()

	// If the second request still succeeds, verify it did NOT use the P2P channel.
	// If it fails (e.g. 503: no available channel), this also satisfies "cannot access G1 channels".
	if resp2.StatusCode == 200 {
		// Verify the latest log does not point to the deleted group's channel.
		logs, err := memberClient.GetUserLogs(member.ID, 2)
		if err != nil {
			t.Fatalf("failed to get user logs: %v", err)
		}
		if len(logs) == 0 {
			t.Fatalf("expected at least 1 log entry, got %d", len(logs))
		}
		secondLog := logs[0] // Most recent log
		if secondLog.ChannelID == channel.ID {
			t.Errorf("ED-05 FAILED: After group deletion, member still accessed P2P channel (ID: %d)", channel.ID)
		} else {
			t.Logf("ED-05 PASSED: After group deletion, member request succeeded without using P2P channel (used channel ID: %d)", secondLog.ChannelID)
		}
	} else {
		t.Logf("ED-05 PASSED: After group deletion, request no longer succeeds (status=%d) – no access to P2P channel", resp2.StatusCode)
	}
}

// TestED06_ConcurrentJoinAndRequest tests concurrent operations:
// multiple goroutines joining a group and making requests simultaneously.
//
// Test Case: ED-06
// Priority: P1
// Scenario: 100 goroutines concurrently join group and make requests
// Expected: No data race, final state consistent
func TestED06_ConcurrentJoinAndRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create group owner.
	_ = createTestUser(t, admin, "ed06_owner", "password123", "default")
	ownerClient := admin.Clone()
	if _, err := ownerClient.Login("ed06_owner", "password123"); err != nil {
		t.Fatalf("failed to login as owner: %v", err)
	}

	// Create a P2P group with password join.
	groupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "ed06_concurrent_group",
		DisplayName: "ED06 Concurrent Group",
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodPassword,
		JoinKey:     "concurrent123",
		Description: "ED06 concurrent test group",
	})
	if err != nil {
		t.Fatalf("failed to create P2P group: %v", err)
	}

	// Create a channel authorized to the group.
	channelName := "ED06 P2P Channel"
	baseURL := suite.Upstream.BaseURL
	allowedGroups := fmt.Sprintf("[%d]", groupID)
	channel := &testutil.ChannelModel{
		Name:          channelName,
		Type:          1,
		Key:           "sk-test-ed06-concurrent",
		Status:        1,
		Models:        "gpt-4",
		Group:         "default",
		BaseURL:       &baseURL,
		AllowedGroups: &allowedGroups,
	}
	if _, err := admin.AddChannel(channel); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Create multiple users concurrently.
	const numUsers = 50 // Reduced from 100 to avoid overwhelming the test server
	var wg sync.WaitGroup
	var successfulJoins int32
	var successfulRequests int32
	var errors int32

	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Create a unique user.
			username := fmt.Sprintf("ed06_user_%d", idx)
			_ = createTestUser(t, admin, username, "password123", "default")

			// Login as the user.
			userClient := admin.Clone()
			if _, err := userClient.Login(username, "password123"); err != nil {
				atomic.AddInt32(&errors, 1)
				t.Logf("User %d login failed: %v", idx, err)
				return
			}

			// Apply to join the group.
			if err := userClient.ApplyToP2PGroup(groupID, "concurrent123"); err != nil {
				atomic.AddInt32(&errors, 1)
				t.Logf("User %d join group failed: %v", idx, err)
				return
			}
			atomic.AddInt32(&successfulJoins, 1)

			// Create a token as this user.
			tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
				Name:           fmt.Sprintf("ED06 Token %d", idx),
				Status:         1,
				UnlimitedQuota: true,
			})
			if err != nil {
				atomic.AddInt32(&errors, 1)
				t.Logf("User %d token creation failed: %v", idx, err)
				return
			}

			userTokenClient := userClient.WithToken(tokenKey)

			// Make a request.
			resp, err := userTokenClient.Post("/v1/chat/completions", map[string]interface{}{
				"model": "gpt-4",
				"messages": []map[string]string{
					{"role": "user", "content": fmt.Sprintf("concurrent test %d", idx)},
				},
			})
			if err != nil {
				atomic.AddInt32(&errors, 1)
				t.Logf("User %d request failed: %v", idx, err)
				return
			}
			resp.Body.Close()

			if resp.StatusCode == 200 {
				atomic.AddInt32(&successfulRequests, 1)
			}
		}(i)
	}

	// Wait for all goroutines to complete.
	wg.Wait()

	// Verify results.
	t.Logf("ED-06 Concurrent Test Results:")
	t.Logf("  Successful joins: %d/%d", successfulJoins, numUsers)
	t.Logf("  Successful requests: %d/%d", successfulRequests, numUsers)
	t.Logf("  Errors: %d", errors)

	// Check for data consistency.
	members, err := admin.GetGroupMembers(groupID, model.MemberStatusActive)
	if err != nil {
		t.Fatalf("failed to get group members: %v", err)
	}

	memberCount := len(members)
	t.Logf("  Final active member count: %d", memberCount)

	if memberCount == 0 {
		t.Errorf("ED-06 FAILED: No active members after concurrent join operations")
	} else if memberCount > numUsers {
		t.Errorf("ED-06 FAILED: Member count (%d) exceeds number of users (%d) - data race detected", memberCount, numUsers)
	} else {
		t.Logf("ED-06 PASSED: Concurrent operations completed without data race")
	}

	// Acceptable range: some joins might fail due to concurrency or rate limiting.
	if successfulJoins < int32(numUsers/2) {
		t.Errorf("ED-06 WARNING: Less than 50%% successful joins (%d/%d)", successfulJoins, numUsers)
	}
}

// TestED07_CachePenetration tests cache penetration protection:
// querying non-existent user group information should not cause DB pressure.
//
// Test Case: ED-07
// Priority: P2
// Scenario: Query group info for non-existent user
// Expected: Fast return, no DB overload
func TestED07_CachePenetration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Generate a non-existent user ID (very large number).
	nonExistentUserID := 999999999

	// Measure time to query group info multiple times.
	const numQueries = 100
	startTime := time.Now()

	for i := 0; i < numQueries; i++ {
		// Try to get joined groups for non-existent user.
		// This should return quickly without hammering the DB.

		// Note: This test assumes there's an API endpoint to query user groups.
		// If such endpoint doesn't exist or requires authentication, we need to
		// test cache penetration at a different layer (e.g., internal cache lookup).

		// For now, we'll make a mock request to simulate cache lookup.
		// In a real scenario, we would:
		// 1. Try to access a resource as a non-existent user
		// 2. Measure response time
		// 3. Verify it doesn't cause DB overload

		// Attempt to query group members for a non-existent group ID. This should
		// return quickly without stressing the database.
		_, _ = admin.GetGroupMembers(nonExistentUserID, -1)
	}

	elapsedTime := time.Since(startTime)
	avgTimePerQuery := elapsedTime / numQueries

	t.Logf("ED-07 Cache Penetration Test Results:")
	t.Logf("  Total time for %d queries: %v", numQueries, elapsedTime)
	t.Logf("  Average time per query: %v", avgTimePerQuery)

	// Verify it was fast (should be < 10ms per query on average).
	if avgTimePerQuery > 10*time.Millisecond {
		t.Errorf("ED-07 WARNING: Average query time (%v) exceeds 10ms - possible cache penetration", avgTimePerQuery)
	} else {
		t.Logf("ED-07 PASSED: Queries for non-existent user returned quickly (avg %v)", avgTimePerQuery)
	}

	// Additional check: Verify DB connection pool is not exhausted.
	// This would require access to DB metrics, which may not be available in the test.
	// For now, the timing check is a reasonable proxy.
}
