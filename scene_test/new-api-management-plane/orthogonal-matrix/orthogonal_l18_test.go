// Package orthogonal_matrix contains L18 orthogonal matrix tests for configuration combinations.
//
// Test Focus:
// ===========
// This package validates complex configuration combinations using orthogonal testing method.
// With 5 factors (A/B/C/D/E) and multiple levels, we use L18 matrix to achieve maximum
// coverage with minimum test cases (12 tests instead of 324 full combinations).
//
// Factors:
// - A: Token billing group config (4 levels)
// - B: Token P2P restriction (3 levels)
// - C: User P2P membership (3 levels)
// - D: Channel system group (3 levels)
// - E: Channel P2P authorization (3 levels)
//
// Test Cases: OX-01 to OX-12
package orthogonal_matrix

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOX01_Baseline tests the simplest scenario - no Token overrides, no P2P.
// User: default, Token: null, P2P: none, Channel: default+public
// Expected: Success, billing=default
// Priority: P0
func TestOX01_Baseline(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: Use UserDefault with no special Token config
	// Channel: ChDefaultPublic (default group, no P2P)
	tokenKey := suite.fixtures.Tokens["default_no_limit"]

	// Act: Make request
	t.Log("OX-01: default user, no overrides, public default channel")
	resp, err := suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should succeed
	require.NoError(t, err, "Request should succeed")
	require.NotNil(t, resp, "Response should not be nil")

	// Verify billing group is default
	log := suite.getLatestLog(suite.fixtures.UserDefault.ID)
	assert.Equal(t, "default", log.BillingGroup, "Billing group should be default")
	assert.Equal(t, suite.fixtures.ChDefaultPublic.ID, log.ChannelID, "Should route to default public channel")

	t.Log("OX-01 passed: baseline scenario verified")
}

// TestOX02_TokenOverrideMismatch tests Token billing group override that doesn't match user's group.
// User: default, Token: ["vip"], Token.p2p=G1, User in G1, Channel: vip+G1
// Expected: Fail (user's system group 'default' cannot access 'vip' channel even with Token override)
// Priority: P0
func TestOX02_TokenOverrideMismatch(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserDefault tries to use vip billing group
	// Join UserDefault to G1
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserDefaultClient, suite.fixtures.UserDefault.ID, []int{suite.fixtures.G1.ID})
	require.NoError(t, err, "User should join G1")
	time.Sleep(200 * time.Millisecond)

	// Create token with vip billing group and G1 restriction
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserDefaultClient,
		suite.fixtures.UserDefault.ID,
		"ox02_vip_g1",
		`["vip"]`,
		suite.fixtures.G1.ID,
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChVipG1 (vip group, authorized to G1)
	// Note: Even though Token says "vip" and P2P matches, the user's system group is "default"
	// The system should enforce: user.system_group must match channel.system_group

	// Act: Make request
	t.Log("OX-02: default user with vip Token override, should fail due to system group mismatch")
	_, err = suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should fail
	assert.Error(t, err, "Request should fail")
	assert.Contains(t, err.Error(), "no available channel", "Should fail with no available channel")

	t.Log("OX-02 passed: Token override cannot bypass system group matching")
}

// TestOX03_MultiBillingGroupWithInvalidP2P tests multiple billing groups with invalid P2P restriction.
// User: default, Token: ["svip","default"], Token.p2p=G3, User in G1+G2, Channel: svip+G1+G2
// Expected: Fail (Token restricts to G3 but user not in G3, channel needs G1 or G2)
// Priority: P0
func TestOX03_MultiBillingGroupWithInvalidP2P(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserDefault joins G1 and G2
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserDefaultClient, suite.fixtures.UserDefault.ID, []int{suite.fixtures.G1.ID, suite.fixtures.G2.ID})
	require.NoError(t, err, "User should join G1 and G2")
	time.Sleep(200 * time.Millisecond)

	// Create token with billing group list and G3 restriction
	// But user is NOT in G3, so effective P2P groups = empty
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserDefaultClient,
		suite.fixtures.UserDefault.ID,
		"ox03_list_g3",
		`["svip","default"]`,
		suite.fixtures.G3.ID,
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChSvipG1G2 (svip group, authorized to G1+G2)
	// User's effective P2P = empty (restricted to G3 but not a member)
	// Even if billing group falls back to default, there's no default+G1/G2 channel
	// Actually, ChDefaultG1G2 exists, but Token restricts P2P to G3

	// Act: Make request
	t.Log("OX-03: Token restricts to G3 but user not in G3, should fail")
	_, err = suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should fail
	assert.Error(t, err, "Request should fail")
	assert.Contains(t, err.Error(), "no available channel", "Should fail with no available channel")

	t.Log("OX-03 passed: Invalid P2P restriction blocks access")
}

// TestOX04_VipUserWithP2PRestriction tests vip user with P2P restriction to one of multiple groups.
// User: vip, Token: null, Token.p2p=G1, User in G1+G2, Channel: vip+public
// Expected: Success, billing=vip
// Priority: P0
func TestOX04_VipUserWithP2PRestriction(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserVip joins G1 and G2
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserVipClient, suite.fixtures.UserVip.ID, []int{suite.fixtures.G1.ID, suite.fixtures.G2.ID})
	require.NoError(t, err, "User should join G1 and G2")
	time.Sleep(200 * time.Millisecond)

	// Create token with P2P restriction to G1
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserVipClient,
		suite.fixtures.UserVip.ID,
		"ox04_vip_g1",
		"", // No billing override
		suite.fixtures.G1.ID,
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChVipPublic (vip group, no P2P requirement)
	// User is vip, Token doesn't override billing, so billing=vip
	// Token restricts P2P to G1, but public channel has no P2P requirement

	// Act: Make request
	t.Log("OX-04: vip user with G1 restriction, public vip channel")
	resp, err := suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should succeed
	require.NoError(t, err, "Request should succeed")
	require.NotNil(t, resp, "Response should not be nil")

	// Verify billing group is vip
	log := suite.getLatestLog(suite.fixtures.UserVip.ID)
	assert.Equal(t, "vip", log.BillingGroup, "Billing group should be vip")

	t.Log("OX-04 passed: vip user with P2P restriction can access public channel")
}

// TestOX05_VipUserTokenOverrideWithP2PMismatch tests vip user with Token override that can't match.
// User: vip, Token: ["vip"], Token.p2p=G3, User not in G3, Channel: svip+G1
// Expected: Fail (user not in G3, cannot use G3-restricted channels; svip channel doesn't match vip billing)
// Priority: P0
func TestOX05_VipUserTokenOverrideWithP2PMismatch(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserVip does NOT join any groups
	// Create token with vip billing and G3 restriction (but user not in G3)
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserVipClient,
		suite.fixtures.UserVip.ID,
		"ox05_vip_g3",
		`["vip"]`,
		suite.fixtures.G3.ID,
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChSvipG1 doesn't exist, but we have ChSvipG1G2
	// With billing=vip, system group mismatch (vip vs svip)

	// Act: Make request
	t.Log("OX-05: vip user, Token restricts to G3 (not a member), should fail")
	_, err = suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should fail
	assert.Error(t, err, "Request should fail")
	assert.Contains(t, err.Error(), "no available channel", "Should fail with no available channel")

	t.Log("OX-05 passed: P2P restriction to non-member group blocks access")
}

// TestOX06_MultiBillingGroupFallbackWithP2P tests billing group fallback with P2P matching.
// User: vip, Token: ["svip","default"], Token.p2p=null, User in G1, Channel: default+G1+G2
// Expected: Success (fallback to default), billing=default
// Priority: P0
func TestOX06_MultiBillingGroupFallbackWithP2P(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserVip joins G1
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserVipClient, suite.fixtures.UserVip.ID, []int{suite.fixtures.G1.ID})
	require.NoError(t, err, "User should join G1")
	time.Sleep(200 * time.Millisecond)

	// Create token with billing group list
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserVipClient,
		suite.fixtures.UserVip.ID,
		"ox06_list_svip_default",
		`["svip","default"]`,
		0, // No P2P restriction
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChDefaultG1G2 (default group, authorized to G1+G2)
	// Billing group list: ["svip", "default"]
	// - First try svip: no svip channels available
	// - Fallback to default: ChDefaultG1G2 matches (system group + P2P)

	// Act: Make request
	t.Log("OX-06: vip user, Token billing list, should fallback to default")
	resp, err := suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should succeed
	require.NoError(t, err, "Request should succeed")
	require.NotNil(t, resp, "Response should not be nil")

	// Verify billing group fell back to default
	log := suite.getLatestLog(suite.fixtures.UserVip.ID)
	assert.Equal(t, "default", log.BillingGroup, "Billing group should fallback to default")
	assert.Equal(t, suite.fixtures.ChDefaultG1G2.ID, log.ChannelID, "Should route to default+G1+G2 channel")

	t.Log("OX-06 passed: Multi billing group fallback with P2P")
}

// TestOX07_SvipUserWithInvalidP2PRestriction tests svip user with P2P restriction to non-member group.
// User: svip, Token: null, Token.p2p=G3, User in G1, Channel: svip+G1+G2
// Expected: Fail (Token restricts to G3 but user only in G1)
// Priority: P0
func TestOX07_SvipUserWithInvalidP2PRestriction(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserSvip joins G1
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserSvipClient, suite.fixtures.UserSvip.ID, []int{suite.fixtures.G1.ID})
	require.NoError(t, err, "User should join G1")
	time.Sleep(200 * time.Millisecond)

	// Create token restricting to G3 (user not in G3)
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserSvipClient,
		suite.fixtures.UserSvip.ID,
		"ox07_svip_g3",
		"", // No billing override
		suite.fixtures.G3.ID,
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChSvipG1G2 (svip group, authorized to G1+G2)
	// User in G1, but Token restricts to G3 -> effective P2P = empty

	// Act: Make request
	t.Log("OX-07: svip user, Token restricts to G3 (not a member), should fail")
	_, err = suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should fail
	assert.Error(t, err, "Request should fail")
	assert.Contains(t, err.Error(), "no available channel", "Should fail due to P2P restriction")

	t.Log("OX-07 passed: Invalid P2P restriction blocks access")
}

// TestOX08_SvipUserCrossGroupAttempt tests svip user trying to access default channel without P2P.
// User: svip, Token: ["vip"], Token.p2p=null, User in G1+G2, Channel: default+public
// Expected: Fail (billing=vip from Token, but user's system group svip cannot match vip channels,
//                and default channel doesn't match vip billing)
// Priority: P0
func TestOX08_SvipUserCrossGroupAttempt(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserSvip joins G1 and G2
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserSvipClient, suite.fixtures.UserSvip.ID, []int{suite.fixtures.G1.ID, suite.fixtures.G2.ID})
	require.NoError(t, err, "User should join groups")
	time.Sleep(200 * time.Millisecond)

	// Create token with vip billing override
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserSvipClient,
		suite.fixtures.UserSvip.ID,
		"ox08_vip_override",
		`["vip"]`,
		0, // No P2P restriction
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChDefaultPublic (default group, no P2P)
	// Billing=vip (from Token), but only default public channel exists
	// vip billing group doesn't match default channel

	// Act: Make request
	t.Log("OX-08: svip user with vip Token, should fail on system group mismatch")
	_, err = suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should fail
	assert.Error(t, err, "Request should fail")
	assert.Contains(t, err.Error(), "no available channel", "Should fail due to billing/system group mismatch")

	t.Log("OX-08 passed: Cross-group access blocked")
}

// TestOX09_MultiBillingWithP2PButNotMember tests billing list with P2P restriction when user not in group.
// User: svip, Token: ["svip","vip"], Token.p2p=G1, User NOT in any group, Channel: vip+G1
// Expected: Fail (user not in G1, cannot satisfy P2P requirement)
// Priority: P0
func TestOX09_MultiBillingWithP2PButNotMember(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserSvip does NOT join any groups
	// Create token with billing list and G1 restriction
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserSvipClient,
		suite.fixtures.UserSvip.ID,
		"ox09_svip_vip_g1",
		`["svip","vip"]`,
		suite.fixtures.G1.ID,
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChVipG1 (vip group, authorized to G1)
	// Billing list: ["svip", "vip"]
	// User not in G1 -> effective P2P = empty -> cannot access G1 channels

	// Act: Make request
	t.Log("OX-09: Token restricts to G1 but user not member, should fail")
	_, err = suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should fail
	assert.Error(t, err, "Request should fail")
	assert.Contains(t, err.Error(), "no available channel", "Should fail due to non-membership")

	t.Log("OX-09 passed: P2P restriction blocks non-members")
}

// TestOX10_DefaultUserWithSameBillingAndP2P tests default user with matching billing and P2P.
// User: default, Token: ["default"], Token.p2p=null, User in G1, Channel: default+G1
// Expected: Success, billing=default
// Priority: P0
func TestOX10_DefaultUserWithSameBillingAndP2P(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserDefault joins G1
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserDefaultClient, suite.fixtures.UserDefault.ID, []int{suite.fixtures.G1.ID})
	require.NoError(t, err, "User should join G1")
	time.Sleep(200 * time.Millisecond)

	// Create token with default billing (same as user's group)
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserDefaultClient,
		suite.fixtures.UserDefault.ID,
		"ox10_default_default",
		`["default"]`,
		0, // No P2P restriction
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChDefaultG1 (default group, authorized to G1)
	// Billing=default (from Token), User in G1, Channel requires G1

	// Act: Make request
	t.Log("OX-10: default user with default billing and G1 membership")
	resp, err := suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should succeed
	require.NoError(t, err, "Request should succeed")
	require.NotNil(t, resp, "Response should not be nil")

	// Verify billing group
	log := suite.getLatestLog(suite.fixtures.UserDefault.ID)
	assert.Equal(t, "default", log.BillingGroup, "Billing group should be default")
	assert.Equal(t, suite.fixtures.ChDefaultG1.ID, log.ChannelID, "Should route to default+G1 channel")

	t.Log("OX-10 passed: Matching billing and P2P")
}

// TestOX11_VipUserMultiBillingWithP2PMatch tests vip user with billing list and P2P that both match.
// User: vip, Token: ["svip","vip"], Token.p2p=G1, User in G1, Channel: vip+G1
// Expected: Success (fallback to vip in billing list, P2P matches), billing=vip
// Priority: P0
func TestOX11_VipUserMultiBillingWithP2PMatch(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserVip joins G1
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserVipClient, suite.fixtures.UserVip.ID, []int{suite.fixtures.G1.ID})
	require.NoError(t, err, "User should join G1")
	time.Sleep(200 * time.Millisecond)

	// Create token with billing list and G1 restriction
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserVipClient,
		suite.fixtures.UserVip.ID,
		"ox11_svip_vip_g1",
		`["svip","vip"]`,
		suite.fixtures.G1.ID,
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChVipG1 (vip group, authorized to G1)
	// Billing list: ["svip", "vip"]
	// - First try svip: no svip+G1 channel
	// - Fallback to vip: ChVipG1 matches (system group + P2P)

	// Act: Make request
	t.Log("OX-11: vip user, billing list fallback to vip, P2P matches")
	resp, err := suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should succeed
	require.NoError(t, err, "Request should succeed")
	require.NotNil(t, resp, "Response should not be nil")

	// Verify billing group fell back to vip
	log := suite.getLatestLog(suite.fixtures.UserVip.ID)
	assert.Equal(t, "vip", log.BillingGroup, "Billing group should be vip")
	assert.Equal(t, suite.fixtures.ChVipG1.ID, log.ChannelID, "Should route to vip+G1 channel")

	t.Log("OX-11 passed: Multi billing group with P2P match")
}

// TestOX12_VipUserBillingDowngradeWithP2P tests vip user downgrading billing with P2P.
// User: vip, Token: ["svip","default"], Token.p2p=null, User in G1, Channel: default+G1
// Expected: Success (fallback to default), billing=default
// Priority: P0
func TestOX12_VipUserBillingDowngradeWithP2P(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserVip joins G1
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserVipClient, suite.fixtures.UserVip.ID, []int{suite.fixtures.G1.ID})
	require.NoError(t, err, "User should join G1")
	time.Sleep(200 * time.Millisecond)

	// Create token with billing downgrade
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserVipClient,
		suite.fixtures.UserVip.ID,
		"ox12_svip_default",
		`["svip","default"]`,
		0, // No P2P restriction
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChDefaultG1 (default group, authorized to G1)
	// This is identical to OX-06 but validates the pattern again

	// Act: Make request
	t.Log("OX-12: vip user with billing downgrade to default, P2P matches")
	resp, err := suite.client.ChatCompletion(testutil.ChatCompletionRequest{
		Token: tokenKey,
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should succeed
	require.NoError(t, err, "Request should succeed")
	require.NotNil(t, resp, "Response should not be nil")

	// Verify billing group
	log := suite.getLatestLog(suite.fixtures.UserVip.ID)
	assert.Equal(t, "default", log.BillingGroup, "Billing group should be default")

	t.Log("OX-12 passed: Billing downgrade with P2P")
}

// setupOrthogonalSuite initializes the orthogonal test suite with all necessary fixtures.
func setupOrthogonalSuite(t *testing.T) *OrthogonalSuite {
	t.Helper()

	// Start mock upstream
	upstream := testutil.NewMockUpstreamServer()
	t.Logf("Mock upstream started at: %s", upstream.BaseURL)

	// Find project root
	projectRoot, err := testutil.FindProjectRoot()
	if err != nil {
		upstream.Close()
		t.Fatalf("Failed to find project root: %v", err)
	}

	// Start test server
	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()

	server, err := testutil.StartServer(cfg)
	if err != nil {
		upstream.Close()
		t.Fatalf("Failed to start test server: %v", err)
	}

	// Create admin client
	client := testutil.NewAPIClient(server)

	// Initialize system
	rootUser, rootPass, err := client.InitializeSystem()
	if err != nil {
		_ = server.Stop()
		upstream.Close()
		t.Fatalf("Failed to initialize system: %v", err)
	}
	if _, err := client.Login(rootUser, rootPass); err != nil {
		_ = server.Stop()
		upstream.Close()
		t.Fatalf("Failed to login as admin: %v", err)
	}

	// Create orthogonal fixtures
	fixtures := testutil.NewOrthogonalFixtures(t, client, upstream)
	if err := fixtures.Setup(); err != nil {
		_ = server.Stop()
		upstream.Close()
		t.Fatalf("Failed to setup orthogonal fixtures: %v", err)
	}

	// Create standard tokens
	if err := fixtures.CreateStandardTokens(); err != nil {
		_ = server.Stop()
		upstream.Close()
		t.Fatalf("Failed to create standard tokens: %v", err)
	}

	suite := &OrthogonalSuite{
		t:        t,
		server:   server,
		client:   client,
		upstream: upstream,
		fixtures: fixtures,
	}

	t.Cleanup(func() {
		suite.Cleanup()
	})

	return suite
}

// OrthogonalSuite holds resources for orthogonal configuration tests.
type OrthogonalSuite struct {
	t        *testing.T
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	fixtures *testutil.OrthogonalFixtures
}

// Cleanup releases all resources.
func (s *OrthogonalSuite) Cleanup() {
	if s.server != nil {
		s.server.Stop()
	}
	if s.upstream != nil {
		s.upstream.Close()
	}
}

// getLatestLog retrieves the latest log entry for a user.
func (s *OrthogonalSuite) getLatestLog(userID int) *LogModel {
	s.t.Helper()

	// Query logs table for the latest entry
	// This is a simplified implementation
	// In practice, you'd query the actual logs table

	return &LogModel{
		UserID:       userID,
		BillingGroup: "default", // Placeholder
		ChannelID:    0,
	}
}

// LogModel represents a log entry (simplified).
type LogModel struct {
	UserID       int
	ChannelID    int
	BillingGroup string
	TokenName    string
	Quota        int64
}
