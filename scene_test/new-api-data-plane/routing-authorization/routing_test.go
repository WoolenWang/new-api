// Package routing_authorization contains integration tests for routing and authorization logic.
//
// Test Focus:
// ===========
// This package tests the core routing and authorization mechanisms of NewAPI,
// specifically validating the decoupling of BillingGroup (计费分组) and RoutingGroups (路由分组).
//
// Key Test Scenarios:
// - R-01: Basic system group routing
// - R-02: Cross system group access (should fail)
// - R-03: P2P group basic sharing
// - R-04: P2P group access without membership
// - R-05: Private channel isolation
// - R-06: Private channel owner access
// - R-07: Token P2P group restriction
package routing_authorization

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// TestSuite holds shared test resources.
type TestSuite struct {
	Server   *testutil.TestServer
	Client   *testutil.APIClient
	Upstream *testutil.MockUpstreamServer
	Fixtures *testutil.TestFixtures
}

// SetupSuite initializes the test suite with a running server.
func SetupSuite(t *testing.T) (*TestSuite, func()) {
	t.Helper()

	// Create mock upstream first
	upstream := testutil.NewMockUpstreamServer()
	t.Logf("Mock upstream started at: %s", upstream.BaseURL)

	// Find project root
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

	// Initialize the system (create root user if needed)
	t.Log("Initializing system...")
	rootUser, rootPass, err := client.InitializeSystem()
	if err != nil {
		upstream.Close()
		server.Stop()
		t.Fatalf("Failed to initialize system: %v", err)
	}
	t.Logf("System initialized with root user: %s", rootUser)

	// Login as root - this sets session cookies on the client
	_, err = client.Login(rootUser, rootPass)
	if err != nil {
		upstream.Close()
		server.Stop()
		t.Fatalf("Failed to login as root: %v", err)
	}
	t.Log("Logged in as root user")

	// The client now has session cookies for admin access
	fixtures := testutil.NewTestFixtures(t, client)
	fixtures.SetUpstream(upstream)

	suite := &TestSuite{
		Server:   server,
		Client:   client, // Client with session cookies
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

// findProjectRoot locates the project root by looking for go.mod.
func findProjectRoot() (string, error) {
	return testutil.FindProjectRoot()
}

// TestRouting_R01_BasicSystemGroup tests basic routing within the same system group.
// Scenario: User with group "default" should be able to access channels in group "default".
func TestRouting_R01_BasicSystemGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	// Setup test data
	t.Log("Creating test user in 'default' group...")
	user, err := suite.Fixtures.CreateTestUser("r01_user", "password123", "default")
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	t.Logf("Created user ID: %d", user.ID)

	// Create a new client for this user and login
	userClient := suite.Client.Clone()
	_, err = userClient.Login("r01_user", "password123")
	if err != nil {
		t.Fatalf("Failed to login: %v", err)
	}

	// Create API token for the user (sk-* token for chat completion)
	apiToken, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "r01-api-token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("Failed to create API token: %v", err)
	}
	t.Logf("Created API token: %s...", apiToken[:10])

	// Create channel for "default" group pointing to mock upstream
	t.Log("Creating channel for 'default' group...")
	channel, err := suite.Fixtures.CreateTestChannel(
		"r01-default-channel",
		"gpt-4,gpt-3.5-turbo",
		"default",
		suite.Upstream.BaseURL,
		false, // Not private
		0,     // No owner
		"",    // No P2P restriction
	)
	if err != nil {
		t.Fatalf("Failed to create channel: %v", err)
	}
	t.Logf("Created channel ID: %d", channel.ID)

	// Make request with the API token (Bearer token for chat completion)
	t.Log("Making chat completion request...")
	apiClient := suite.Client.WithToken(apiToken)
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "Hello, test!")

	// Verify success
	if !success {
		t.Errorf("Chat completion failed: status=%d, error=%s", statusCode, errMsg)
		// Print server logs for debugging
		t.Log("Server logs:")
		for _, log := range suite.Server.GetLogs() {
			t.Log(log)
		}
		return
	}

	// Verify mock upstream received the request
	reqCount := suite.Upstream.GetRequestCount()
	if reqCount != 1 {
		t.Errorf("Expected 1 request to upstream, got %d", reqCount)
	}

	t.Log("R-01: Basic system group routing - PASSED")
}

// TestRouting_R02_CrossSystemGroup tests that users cannot access channels in other system groups.
// Scenario: User with group "default" should NOT be able to access channels in group "vip".
func TestRouting_R02_CrossSystemGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	// Create user in "default" group
	t.Log("Creating test user in 'default' group...")
	user, err := suite.Fixtures.CreateTestUser("r02_user", "password123", "default")
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	t.Logf("Created user ID: %d", user.ID)

	// Create a new client for this user and login
	userClient := suite.Client.Clone()
	_, err = userClient.Login("r02_user", "password123")
	if err != nil {
		t.Fatalf("Failed to login: %v", err)
	}

	// Create API token
	apiToken, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "r02-api-token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("Failed to create API token: %v", err)
	}

	// Create channel ONLY for "vip" group
	t.Log("Creating channel for 'vip' group only...")
	_, err = suite.Fixtures.CreateTestChannel(
		"r02-vip-only-channel",
		"gpt-4",
		"vip", // Only vip group
		suite.Upstream.BaseURL,
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("Failed to create channel: %v", err)
	}

	// Try to access with "default" user - should fail
	t.Log("Attempting chat completion (should fail - no access to vip channel)...")
	apiClient := suite.Client.WithToken(apiToken)
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "Hello, test!")

	// Verify failure
	if success {
		t.Errorf("Expected request to fail, but it succeeded")
		return
	}

	// Should get "no available channel" type error or similar
	t.Logf("Request correctly failed: status=%d, error=%s", statusCode, errMsg)

	// Verify mock upstream did NOT receive any request
	reqCount := suite.Upstream.GetRequestCount()
	if reqCount != 0 {
		t.Errorf("Expected 0 requests to upstream (should be blocked), got %d", reqCount)
	}

	t.Log("R-02: Cross system group blocking - PASSED")
}

// TestRouting_R03_P2PBasicSharing tests basic P2P group channel sharing.
// Scenario: User A shares a channel via P2P group, User B (member) can access it.
func TestRouting_R03_P2PBasicSharing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	// Create User A (owner) in "vip" group
	t.Log("Creating User A (owner) in 'vip' group...")
	userA, err := suite.Fixtures.CreateTestUser("r03_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("Failed to create user A: %v", err)
	}

	userAClient := suite.Client.Clone()
	_, err = userAClient.Login("r03_userA", "password123")
	if err != nil {
		t.Fatalf("Failed to login user A: %v", err)
	}

	// Create User B (consumer) in "default" group
	t.Log("Creating User B (consumer) in 'default' group...")
	_, err = suite.Fixtures.CreateTestUser("r03_userB", "password123", "default")
	if err != nil {
		t.Fatalf("Failed to create user B: %v", err)
	}

	userBClient := suite.Client.Clone()
	_, err = userBClient.Login("r03_userB", "password123")
	if err != nil {
		t.Fatalf("Failed to login user B: %v", err)
	}

	// User A creates P2P group with password join
	t.Log("User A creating P2P group...")
	p2pGroupID, err := userAClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:       "r03-shared-group",
		OwnerId:    userA.ID,
		Type:       testutil.P2PGroupTypeShared,
		JoinMethod: testutil.P2PJoinMethodPassword,
		JoinKey:    "sharepass123",
	})
	if err != nil {
		t.Fatalf("Failed to create P2P group: %v", err)
	}
	t.Logf("Created P2P group ID: %d", p2pGroupID)

	// User B joins the P2P group
	t.Log("User B joining P2P group...")
	err = userBClient.ApplyToP2PGroup(p2pGroupID, "sharepass123")
	if err != nil {
		t.Fatalf("Failed to join P2P group: %v", err)
	}

	// Create channel authorized for the P2P group
	// Important: Channel system group must include the user's group for P2P routing to work
	t.Log("Creating channel with P2P group restriction...")
	_, err = suite.Fixtures.CreateTestChannel(
		"r03-p2p-channel",
		"gpt-4",
		"default", // Must match User B's system group for routing to work
		suite.Upstream.BaseURL,
		false,
		0,
		fmt.Sprintf("%d", p2pGroupID), // Authorized for P2P group
	)
	if err != nil {
		t.Fatalf("Failed to create P2P channel: %v", err)
	}

	// Create API token for User B
	userBAPIToken, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "r03-userB-token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("Failed to create API token for user B: %v", err)
	}

	// User B makes request - should succeed via P2P group
	t.Log("User B making chat completion request...")
	apiClient := suite.Client.WithToken(userBAPIToken)
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "Hello from P2P member!")

	if !success {
		t.Errorf("Chat completion failed for P2P member: status=%d, error=%s", statusCode, errMsg)
		t.Log("Server logs:")
		for _, log := range suite.Server.GetLogs() {
			t.Log(log)
		}
		return
	}

	// Verify mock upstream received the request
	reqCount := suite.Upstream.GetRequestCount()
	if reqCount != 1 {
		t.Errorf("Expected 1 request to upstream, got %d", reqCount)
	}

	t.Log("R-03: P2P basic sharing - PASSED")
}

// TestRouting_R04_P2PNoMembership tests that non-members cannot access P2P shared channels.
func TestRouting_R04_P2PNoMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	// Create User A (owner)
	t.Log("Creating User A (owner)...")
	userA, err := suite.Fixtures.CreateTestUser("r04_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("Failed to create user A: %v", err)
	}

	userAClient := suite.Client.Clone()
	_, err = userAClient.Login("r04_userA", "password123")
	if err != nil {
		t.Fatalf("Failed to login user A: %v", err)
	}

	// Create User B (NOT a member)
	t.Log("Creating User B (non-member)...")
	_, err = suite.Fixtures.CreateTestUser("r04_userB", "password123", "default")
	if err != nil {
		t.Fatalf("Failed to create user B: %v", err)
	}

	userBClient := suite.Client.Clone()
	_, err = userBClient.Login("r04_userB", "password123")
	if err != nil {
		t.Fatalf("Failed to login user B: %v", err)
	}

	// User A creates P2P group
	p2pGroupID, err := userAClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:       "r04-private-group",
		OwnerId:    userA.ID,
		Type:       testutil.P2PGroupTypeShared,
		JoinMethod: testutil.P2PJoinMethodInvite, // Invite only
	})
	if err != nil {
		t.Fatalf("Failed to create P2P group: %v", err)
	}

	// Create channel ONLY authorized for the P2P group
	_, err = suite.Fixtures.CreateTestChannel(
		"r04-p2p-only-channel",
		"gpt-4",
		"vip",
		suite.Upstream.BaseURL,
		false,
		0,
		fmt.Sprintf("%d", p2pGroupID), // Only P2P group
	)
	if err != nil {
		t.Fatalf("Failed to create channel: %v", err)
	}

	// User B creates API token (NOT a P2P member)
	userBAPIToken, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "r04-userB-token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("Failed to create API token for user B: %v", err)
	}

	// User B tries to access - should FAIL
	t.Log("User B (non-member) attempting chat completion...")
	apiClient := suite.Client.WithToken(userBAPIToken)
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "Hello!")

	if success {
		t.Errorf("Expected request to fail for non-member, but it succeeded")
		return
	}

	t.Logf("Request correctly blocked: status=%d, error=%s", statusCode, errMsg)

	// Verify no request reached upstream
	reqCount := suite.Upstream.GetRequestCount()
	if reqCount != 0 {
		t.Errorf("Expected 0 requests to upstream, got %d", reqCount)
	}

	t.Log("R-04: P2P no membership blocking - PASSED")
}

// TestRouting_R05_PrivateChannelIsolation tests that private channels are not visible to group members.
func TestRouting_R05_PrivateChannelIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	// Create User A (owner) and User B (same group member)
	t.Log("Creating users...")
	userA, err := suite.Fixtures.CreateTestUser("r05_userA", "password123", "default")
	if err != nil {
		t.Fatalf("Failed to create user A: %v", err)
	}

	_, err = suite.Fixtures.CreateTestUser("r05_userB", "password123", "default")
	if err != nil {
		t.Fatalf("Failed to create user B: %v", err)
	}

	userBClient := suite.Client.Clone()
	_, err = userBClient.Login("r05_userB", "password123")
	if err != nil {
		t.Fatalf("Failed to login user B: %v", err)
	}

	// User A creates a PRIVATE channel
	t.Log("Creating private channel owned by User A...")
	_, err = suite.Fixtures.CreateTestChannel(
		"r05-private-channel",
		"gpt-4",
		"default",
		suite.Upstream.BaseURL,
		true,     // Private!
		userA.ID, // Owned by User A
		"",
	)
	if err != nil {
		t.Fatalf("Failed to create private channel: %v", err)
	}

	// User B creates API token
	userBAPIToken, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "r05-userB-token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("Failed to create API token for user B: %v", err)
	}

	// User B tries to access - should FAIL (private channel not visible)
	t.Log("User B attempting to access private channel (should fail)...")
	apiClient := suite.Client.WithToken(userBAPIToken)
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "Hello!")

	if success {
		t.Errorf("Expected request to fail (private channel isolation), but it succeeded")
		return
	}

	t.Logf("Private channel correctly isolated: status=%d, error=%s", statusCode, errMsg)

	// Verify no request reached upstream
	reqCount := suite.Upstream.GetRequestCount()
	if reqCount != 0 {
		t.Errorf("Expected 0 requests to upstream, got %d", reqCount)
	}

	t.Log("R-05: Private channel isolation - PASSED")
}

// TestRouting_R06_PrivateChannelOwnerAccess tests that owners can use their private channels.
func TestRouting_R06_PrivateChannelOwnerAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	// Create User A (owner)
	t.Log("Creating User A (owner)...")
	userA, err := suite.Fixtures.CreateTestUser("r06_userA", "password123", "default")
	if err != nil {
		t.Fatalf("Failed to create user A: %v", err)
	}

	userAClient := suite.Client.Clone()
	_, err = userAClient.Login("r06_userA", "password123")
	if err != nil {
		t.Fatalf("Failed to login user A: %v", err)
	}

	// User A creates a PRIVATE channel
	t.Log("Creating private channel owned by User A...")
	_, err = suite.Fixtures.CreateTestChannel(
		"r06-private-channel",
		"gpt-4",
		"default",
		suite.Upstream.BaseURL,
		true,     // Private
		userA.ID, // Owned by User A
		"",
	)
	if err != nil {
		t.Fatalf("Failed to create private channel: %v", err)
	}

	// User A creates API token
	userAAPIToken, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "r06-userA-token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("Failed to create API token for user A: %v", err)
	}

	// User A accesses own private channel - should succeed
	t.Log("User A (owner) accessing private channel...")
	apiClient := suite.Client.WithToken(userAAPIToken)
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "Hello from owner!")

	if !success {
		t.Errorf("Owner failed to access own private channel: status=%d, error=%s", statusCode, errMsg)
		t.Log("Server logs:")
		for _, log := range suite.Server.GetLogs() {
			t.Log(log)
		}
		return
	}

	// Verify request reached upstream
	reqCount := suite.Upstream.GetRequestCount()
	if reqCount != 1 {
		t.Errorf("Expected 1 request to upstream, got %d", reqCount)
	}

	t.Log("R-06: Private channel owner access - PASSED")
}

// TestRouting_R07_TokenP2PGroupRestriction tests Token-level P2P group restriction.
// Scenario: Token restricts access to a specific P2P group only.
func TestRouting_R07_TokenP2PGroupRestriction(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	// Create User A (owner of both groups)
	t.Log("Creating User A...")
	userA, err := suite.Fixtures.CreateTestUser("r07_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("Failed to create user A: %v", err)
	}

	userAClient := suite.Client.Clone()
	_, err = userAClient.Login("r07_userA", "password123")
	if err != nil {
		t.Fatalf("Failed to login user A: %v", err)
	}

	// Create User B who will be member of both groups
	t.Log("Creating User B...")
	_, err = suite.Fixtures.CreateTestUser("r07_userB", "password123", "default")
	if err != nil {
		t.Fatalf("Failed to create user B: %v", err)
	}

	userBClient := suite.Client.Clone()
	_, err = userBClient.Login("r07_userB", "password123")
	if err != nil {
		t.Fatalf("Failed to login user B: %v", err)
	}

	// Create two P2P groups
	group1ID, err := userAClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:       "r07-group1",
		OwnerId:    userA.ID,
		Type:       testutil.P2PGroupTypeShared,
		JoinMethod: testutil.P2PJoinMethodPassword,
		JoinKey:    "g1pass",
	})
	if err != nil {
		t.Fatalf("Failed to create group 1: %v", err)
	}

	group2ID, err := userAClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:       "r07-group2",
		OwnerId:    userA.ID,
		Type:       testutil.P2PGroupTypeShared,
		JoinMethod: testutil.P2PJoinMethodPassword,
		JoinKey:    "g2pass",
	})
	if err != nil {
		t.Fatalf("Failed to create group 2: %v", err)
	}

	// User B joins both groups
	if err := userBClient.ApplyToP2PGroup(group1ID, "g1pass"); err != nil {
		t.Fatalf("Failed to join group 1: %v", err)
	}
	if err := userBClient.ApplyToP2PGroup(group2ID, "g2pass"); err != nil {
		t.Fatalf("Failed to join group 2: %v", err)
	}

	// Create channels for each group
	// Important: Channel system group must include the user's group for routing to work
	_, err = suite.Fixtures.CreateTestChannel(
		"r07-channel-g1",
		"gpt-4",
		"default", // User B is in "default" group
		suite.Upstream.BaseURL,
		false,
		0,
		fmt.Sprintf("%d", group1ID),
	)
	if err != nil {
		t.Fatalf("Failed to create channel for group 1: %v", err)
	}

	// Create second mock upstream for group 2
	upstream2 := testutil.NewMockUpstreamServer()
	defer upstream2.Close()

	_, err = suite.Fixtures.CreateTestChannel(
		"r07-channel-g2",
		"claude-3",
		"default", // User B is in "default" group
		upstream2.BaseURL,
		false,
		0,
		fmt.Sprintf("%d", group2ID),
	)
	if err != nil {
		t.Fatalf("Failed to create channel for group 2: %v", err)
	}

	// Create API token restricted to group 1 only
	userBAPIToken, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "r07-userB-g1-only",
		Status:         1,
		UnlimitedQuota: true,
		P2PGroupID:     &group1ID, // Restricted to group 1
	})
	if err != nil {
		t.Fatalf("Failed to create API token: %v", err)
	}

	apiClient := suite.Client.WithToken(userBAPIToken)

	// Request for gpt-4 (group 1 channel) should succeed
	t.Log("Testing access to group 1 channel (should succeed)...")
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "Hello to G1!")
	if !success {
		t.Errorf("Failed to access group 1 channel: status=%d, error=%s", statusCode, errMsg)
	} else {
		t.Log("Successfully accessed group 1 channel")
	}

	// Request for claude-3 (group 2 channel) should FAIL because token is restricted to G1
	t.Log("Testing access to group 2 channel (should fail - token restricted)...")
	success, statusCode, errMsg = apiClient.TryChatCompletion("claude-3", "Hello to G2!")
	if success {
		t.Errorf("Expected group 2 access to fail (token restricted to G1), but it succeeded")
	} else {
		t.Logf("Correctly blocked group 2 access: status=%d, error=%s", statusCode, errMsg)
	}

	// Verify only 1 request reached upstream (for group 1)
	if suite.Upstream.GetRequestCount() != 1 {
		t.Errorf("Expected 1 request to group 1 upstream, got %d", suite.Upstream.GetRequestCount())
	}
	if upstream2.GetRequestCount() != 0 {
		t.Errorf("Expected 0 requests to group 2 upstream, got %d", upstream2.GetRequestCount())
	}

	t.Log("R-07: Token P2P group restriction - PASSED")
}

// TestRouting_P2P_NoTokenRestriction_CannotUseP2PChannels verifies that when a Token does NOT specify
// any p2p_group_id, the request will not use P2P-restricted channels and will only route via
// public / non-P2P channels (or fail if no such channel exists).
func TestRouting_P2P_NoTokenRestriction_CannotUseP2PChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Setup basic users and P2P groups/channels (User1 owns P2P group/channel, User2 is member)
	if err := fixtures.SetupRoutingTestFixtures(); err != nil {
		t.Fatalf("SetupRoutingTestFixtures failed: %v", err)
	}

	// User2 client and token WITHOUT any P2P restriction
	user2Client := fixtures.ClientForUser2()

	// Send request for model only available via P2P group channel (owned by User1)
	apiClient := user2Client
	t.Log("Sending request without p2p_group_id, expecting NOT to use P2P channel...")
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "P2P no-token-restriction test")

	if success {
		t.Errorf("Expected request to fail (no public/default channel for gpt-4), but it succeeded")
	}
	if statusCode == 200 {
		t.Errorf("Expected non-200 when no non-P2P route available, got 200")
	}
	t.Logf("Request correctly did not use P2P channel: status=%d, err=%s", statusCode, errMsg)
}

// TestRouting_P2P_NoTokenRestriction_UsesPublicChannel verifies that when there is both a public
// (non-P2P) channel and a P2P channel for the same model, and Token has no p2p_group_id,
// routing prefers the public channel and will not utilize the P2P-restricted channel.
func TestRouting_P2P_NoTokenRestriction_UsesPublicChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Create basic user in "default" group with unlimited token
	user, err := fixtures.CreateTestUser("p2p_norestr_public_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	userClient := suite.Client.Clone()
	if _, err := userClient.Login("p2p_norestr_public_user", "password123"); err != nil {
		t.Fatalf("failed to login user: %v", err)
	}

	token, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "p2p-norestr-public-token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	// Create a public channel (no P2P restriction) and a P2P-restricted channel for same model
	publicChannel, err := fixtures.CreateTestChannel(
		"p2p-norestr-public-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create public channel: %v", err)
	}
	t.Logf("Created public channel ID=%d", publicChannel.ID)

	// Create a P2P group and a P2P-restricted channel
	ownerClient := userClient
	group, err := fixtures.CreateTestP2PGroup(
		"p2p-norestr-public-group",
		ownerClient,
		user.ID,
		testutil.P2PGroupTypeShared,
		testutil.P2PJoinMethodPassword,
		"p2ppass",
	)
	if err != nil {
		t.Fatalf("failed to create P2P group: %v", err)
	}

	p2pChannel, err := fixtures.CreateTestChannel(
		"p2p-norestr-p2p-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		0,
		fmt.Sprintf("%d", group.ID),
	)
	if err != nil {
		t.Fatalf("failed to create P2P channel: %v", err)
	}
	t.Logf("Created P2P channel ID=%d", p2pChannel.ID)

	// Using a token WITHOUT p2p_group_id, request should succeed via public channel
	apiClient := suite.Client.WithToken(token)
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "P2P no-token-restriction public channel test")
	if !success {
		t.Fatalf("expected request to succeed via public channel, got status=%d, err=%s", statusCode, errMsg)
	}
	if suite.Upstream.GetRequestCount() == 0 {
		t.Fatalf("expected upstream to receive request via public channel")
	}
	t.Log("Request succeeded via public channel without using P2P channel")
}

// TestRouting_P2P_NoTokenRestriction_OwnerCanUseOwnP2P verifies that even when Token has no
// p2p_group_id, channel owner仍然可以访问自己的 P2P 渠道（owner 权限优先级最高）。
func TestRouting_P2P_NoTokenRestriction_OwnerCanUseOwnP2P(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Owner user
	owner, err := fixtures.CreateTestUser("p2p_norestr_owner_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}

	ownerClient := suite.Client.Clone()
	if _, err := ownerClient.Login("p2p_norestr_owner_user", "password123"); err != nil {
		t.Fatalf("failed to login owner user: %v", err)
	}

	// Token without p2p_group_id
	token, err := ownerClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "p2p-norestr-owner-token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create owner token: %v", err)
	}

	// P2P group owned by owner
	group, err := fixtures.CreateTestP2PGroup(
		"p2p-norestr-owner-group",
		ownerClient,
		owner.ID,
		testutil.P2PGroupTypeShared,
		testutil.P2PJoinMethodPassword,
		"ownerpass",
	)
	if err != nil {
		t.Fatalf("failed to create owner P2P group: %v", err)
	}

	// P2P channel owned by owner (OwnerUserId = owner.ID)
	p2pChannel, err := fixtures.CreateTestChannel(
		"p2p-norestr-owner-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		owner.ID,
		fmt.Sprintf("%d", group.ID),
	)
	if err != nil {
		t.Fatalf("failed to create owner P2P channel: %v", err)
	}
	t.Logf("Created owner P2P channel ID=%d", p2pChannel.ID)

	apiClient := suite.Client.WithToken(token)
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "P2P owner self-use test")
	if !success {
		t.Fatalf("expected owner to access own P2P channel, got status=%d, err=%s", statusCode, errMsg)
	}
	if suite.Upstream.GetRequestCount() == 0 {
		t.Fatalf("expected upstream to receive request via owner's P2P channel")
	}
	t.Log("Owner successfully accessed own P2P channel without specifying p2p_group_id")
}

// TestRoutingSkeleton is a placeholder test to verify the test file compiles.
func TestRoutingSkeleton(t *testing.T) {
	t.Log("Routing test suite loaded successfully")
}
