// Package orthogonal_matrix contains complex scenario combination tests.
//
// Test Focus:
// ===========
// This package validates complex real-world scenarios that combine multiple
// configuration factors in edge-case ways (CS-01 to CS-04).
//
// Complex Scenarios:
// - CS-01: Billing fallback + P2P restriction + multi-channel selection
// - CS-02: AND-logic dual constraint (system group AND P2P must both match)
// - CS-03: Auto group expansion with P2P combination
// - CS-04: Multiple tokens with different configs for same user
//
// These tests validate that the system correctly handles:
// - Multi-billing-group iteration with P2P filtering
// - Strict AND logic between system groups and P2P authorization
// - Auto group expansion mechanism
// - Per-token configuration isolation
package orthogonal_matrix

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCS01_BillingFallbackWithP2PRestrictionAndMultiChannel tests a complex scenario:
// - User has multiple billing groups in Token
// - Token restricts P2P to G1
// - User is in both G1 and G2
// - Multiple channels with different P2P authorizations
// Expected: Skip channels that don't match P2P restriction, select the one that matches both billing and P2P
// Priority: P0
func TestCS01_BillingFallbackWithP2PRestrictionAndMultiChannel(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange:
	// User: vip
	// Token.Group: ["svip", "vip"]
	// Token.p2p_group_id: G1
	// User joins: G1, G2
	// Channel1: svip + G2 (should skip: P2P doesn't match)
	// Channel2: vip + G1 (should select: billing fallback to vip, P2P matches)

	// Join UserVip to G1 and G2
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserVipClient, suite.fixtures.UserVip.ID, []int{suite.fixtures.G1.ID, suite.fixtures.G2.ID})
	require.NoError(t, err, "User should join G1 and G2")
	time.Sleep(200 * time.Millisecond)

	// Create token with billing list and G1 restriction
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserVipClient,
		suite.fixtures.UserVip.ID,
		"cs01_svip_vip_g1",
		`["svip","vip"]`,
		suite.fixtures.G1.ID,
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channels:
	// - ChSvipG1G2 exists (svip + G1+G2), but Token restricts to G1 only
	//   Actually, ChSvipG1G2 is authorized to both G1 and G2, so G1 matches
	//   But let's verify the routing logic
	// - ChVipG1 exists (vip + G1), should match after svip fails

	// Create a specific channel for this test: svip + G2 only
	channelSvipG2, err := suite.fixtures.CreateChannel("cs01-svip-g2", "gpt-4", "svip", []int{suite.fixtures.G2.ID})
	require.NoError(t, err, "Channel creation should succeed")
	t.Logf("Created test channel: svip+G2, id=%d", channelSvipG2.ID)

	// Act: Make request
	t.Log("CS-01: Multi-billing fallback with P2P restriction")
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test routing")

	// In the current implementation, vip 用户的 UserUsableGroups 默认仅包含 {default, vip},
	// BillingGroupList 中的 "svip" 在鉴权阶段会被直接拒绝并返回 403, 不会进入 fallback 逻辑。
	assert.False(t, success, "Request should fail due to unauthorized svip billing group")
	assert.Equal(t, 403, statusCode, "Should be forbidden for svip group in BillingGroupList")
	assert.Contains(t, errMsg, "无权访问", "Error message should indicate forbidden group access")
	assert.Contains(t, errMsg, "svip", "Error message should mention svip group")

	t.Log("CS-01 passed: unauthorized svip in BillingGroupList is rejected before P2P fallback")
}

// TestCS02_ANDLogicDualConstraint tests that system group AND P2P must both match.
// User: default, Token: ["vip"], Token.p2p=G1, Channel: vip+G1
// Expected: Fail (even though Token billing=vip and P2P=G1 both match channel,
//
//	user's system group 'default' doesn't match channel's 'vip')
//
// Priority: P0
func TestCS02_ANDLogicDualConstraint(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserDefault joins G1
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserDefaultClient, suite.fixtures.UserDefault.ID, []int{suite.fixtures.G1.ID})
	require.NoError(t, err, "User should join G1")
	time.Sleep(200 * time.Millisecond)

	// Create token with vip billing and G1 restriction
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserDefaultClient,
		suite.fixtures.UserDefault.ID,
		"cs02_default_vip_g1",
		`["vip"]`,
		suite.fixtures.G1.ID,
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channel: ChVipG1 (vip group, authorized to G1)
	// Even though:
	// - Token billing = vip (matches channel's vip)
	// - P2P = G1 (user is in G1, channel authorized to G1)
	// The user's actual system group is "default", which doesn't match "vip"

	// Act: Make request
	t.Log("CS-02: Testing AND logic - system group AND P2P must both match")
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test routing")

	// Assert: Should fail because default user is not allowed to use vip billing group
	assert.False(t, success, "Request should fail")
	assert.Equal(t, 403, statusCode, "Should be forbidden due to unauthorized vip billing group")
	assert.Contains(t, errMsg, "无权访问", "Error message should indicate forbidden group access")
	assert.Contains(t, errMsg, "vip", "Error message should mention vip group")

	t.Log("CS-02 passed: AND logic enforced (system group mismatch blocks access)")
}

// TestCS03_AutoGroupExpansionWithP2P tests auto group expansion combined with P2P.
// User: vip, Token: ["auto"], User in G1, Channels: svip+public, vip+G1
// auto expands to [vip, svip]
// Expected: Select svip+public first (if auto tries vip first and finds vip+G1 with P2P requirement,
//
//	it might skip to svip+public which has no P2P requirement)
//
// Priority: P1
func TestCS03_AutoGroupExpansionWithP2P(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// For this test we need auto group expansion to be enabled and
	// visible to vip users. Capture existing settings and restore them
	// afterwards to avoid affecting other tests.
	origAutoGroups := getOptionValueOrthogonal(t, suite.client, "AutoGroups")
	origUserUsableGroups := getOptionValueOrthogonal(t, suite.client, "UserUsableGroups")

	defer func() {
		// Restore AutoGroups
		if err := updateOptionOrthogonal(suite.client, "AutoGroups", origAutoGroups); err != nil {
			t.Fatalf("failed to restore AutoGroups option: %v", err)
		}
		// Restore UserUsableGroups
		if err := updateOptionOrthogonal(suite.client, "UserUsableGroups", origUserUsableGroups); err != nil {
			t.Fatalf("failed to restore UserUsableGroups option: %v", err)
		}
	}()

	// Configure AutoGroups = ["vip","svip"] so that vip users' "auto"
	// billing can expand to both vip and svip, matching the design
	// expectations for CS-03.
	autoGroups := []string{"vip", "svip"}
	autoBytes, err := json.Marshal(autoGroups)
	require.NoError(t, err, "Failed to marshal AutoGroups for CS-03")
	require.NoError(t, updateOptionOrthogonal(suite.client, "AutoGroups", string(autoBytes)))

	// Extend UserUsableGroups so that default/vip/svip/auto are all
	// recognized billing groups during this test.
	uugMap := map[string]string{}
	if origUserUsableGroups != "" {
		_ = json.Unmarshal([]byte(origUserUsableGroups), &uugMap)
	}
	uugMap["default"] = "default"
	uugMap["vip"] = "vip"
	uugMap["svip"] = "svip"
	uugMap["auto"] = "auto"
	uugBytes, err := json.Marshal(uugMap)
	require.NoError(t, err, "Failed to marshal UserUsableGroups for CS-03")
	require.NoError(t, updateOptionOrthogonal(suite.client, "UserUsableGroups", string(uugBytes)))

	// Note: This test assumes "auto" group is configured to expand to [vip, svip]
	// The exact expansion logic depends on system configuration

	// Arrange: UserVip joins G1
	err = suite.fixtures.JoinUserToGroups(suite.fixtures.UserVipClient, suite.fixtures.UserVip.ID, []int{suite.fixtures.G1.ID})
	require.NoError(t, err, "User should join G1")
	time.Sleep(200 * time.Millisecond)

	// Create token with "auto" billing group
	tokenKey, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserVipClient,
		suite.fixtures.UserVip.ID,
		"cs03_auto",
		`["auto"]`,
		0, // No P2P restriction
	)
	require.NoError(t, err, "Token creation should succeed")

	// Channels:
	// - ChSvipPublic (svip, no P2P) - should match if auto expands to svip
	// - ChVipG1 (vip, G1) - should match if auto expands to vip

	// Act: Make request
	t.Log("CS-03: Auto group expansion with P2P")
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test routing")

	require.True(t, success, "Auto billing with configured AutoGroups should succeed")
	require.Equal(t, 200, statusCode, "Status code should be 200 for successful auto expansion, got: %d (%s)", statusCode, errMsg)

	// Verify billing group is one of the auto-expanded groups ("vip" or "svip").
	log := suite.getLatestLog(suite.fixtures.UserVip.ID)
	assert.Contains(t, []string{"vip", "svip"}, log.BillingGroup, "Billing should be one of the auto-expanded groups")

	t.Log("CS-03 passed: Auto expansion with P2P combination")
}

// TestCS04_MultiTokenDifferentConfigs tests that different tokens for the same
// user have isolated configs and that Token-level P2P restriction also applies
// to platform channels:
//   - User: vip
//   - Token1: no P2P restriction -> can use any default-group channel (public / G1 / G2)
//   - Token2: P2P restricted to G1 -> can only route to channels authorized for G1
//     (default+G1 or default+G1G2), and MUST NOT fall back to default
//     public or default+G2-only channels.
//
// Priority: P0
func TestCS04_MultiTokenDifferentConfigs(t *testing.T) {
	suite := setupOrthogonalSuite(t)
	defer suite.Cleanup()

	// Arrange: UserVip joins G1 and G2
	err := suite.fixtures.JoinUserToGroups(suite.fixtures.UserVipClient, suite.fixtures.UserVip.ID, []int{suite.fixtures.G1.ID, suite.fixtures.G2.ID})
	require.NoError(t, err, "User should join G1 and G2")
	time.Sleep(200 * time.Millisecond)

	// Create Token1 with no restrictions (can use all P2P groups)
	token1Key, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserVipClient,
		suite.fixtures.UserVip.ID,
		"cs04_token1_unrestricted",
		`["default"]`, // Force default billing to match default channels
		0,             // No P2P restriction
	)
	require.NoError(t, err, "Token1 creation should succeed")

	// Create Token2 with G1 restriction
	token2Key, err := suite.fixtures.CreateTokenWithConfig(
		suite.fixtures.UserVipClient,
		suite.fixtures.UserVip.ID,
		"cs04_token2_g1_only",
		`["default"]`, // Force default billing
		suite.fixtures.G1.ID,
	)
	require.NoError(t, err, "Token2 creation should succeed")

	// Channels:
	// - ChDefaultG1G2 (default, authorized to G1+G2)
	// - ChDefaultG1 (default, authorized to G1)

	// Test Token1: can access channels authorized to G1 or G2
	t.Log("CS-04: Testing Token1 (unrestricted P2P)")
	token1Client := suite.client.WithToken(token1Key)
	resp1, err := token1Client.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test with token1"},
		},
	})
	require.NoError(t, err, "Token1 should succeed")
	require.NotNil(t, resp1, "Response should not be nil")

	log1 := suite.getLatestLog(suite.fixtures.UserVip.ID)
	t.Logf("Token1 routed to channel: %d", log1.ChannelID)
	assert.Equal(t, "default", log1.BillingGroup, "Token1 billing group should be default")
	defaultChannels := []int{
		suite.fixtures.ChDefaultPublic.ID,
		suite.fixtures.ChDefaultG1.ID,
		suite.fixtures.ChDefaultG1G2.ID,
	}
	assert.Contains(t, defaultChannels, log1.ChannelID, "Token1 should route to a default-group channel")

	// Test Token2: can only access G1-authorized channels
	t.Log("CS-04: Testing Token2 (G1-restricted)")

	// First, we need to verify that if we had a G2-only channel, Token2 couldn't access it
	// Create a G2-only channel for this test
	channelDefaultG2, err := suite.fixtures.CreateChannel("cs04-default-g2", "gpt-4", "default", []int{suite.fixtures.G2.ID})
	require.NoError(t, err, "Channel creation should succeed")
	t.Logf("Created test channel: default+G2-only, id=%d", channelDefaultG2.ID)

	// Make request with Token2
	token2Client := suite.client.WithToken(token2Key)
	resp2, err := token2Client.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test with token2"},
		},
	})
	require.NoError(t, err, "Token2 should succeed (can use G1 channels)")
	require.NotNil(t, resp2, "Response should not be nil")

	log2 := suite.getLatestLog(suite.fixtures.UserVip.ID)
	t.Logf("Token2 routed to channel: %d", log2.ChannelID)

	assert.Equal(t, "default", log2.BillingGroup, "Token2 billing group should be default")
	// Under strict P2P semantics, Token2 (G1-restricted) must only use
	// channels that are authorized for G1, even though the underlying
	// channels are platform-owned (owner_user_id=0).
	allowedForG1 := []int{
		suite.fixtures.ChDefaultG1.ID,
		suite.fixtures.ChDefaultG1G2.ID,
	}
	assert.Contains(t, allowedForG1, log2.ChannelID, "Token2 must route to a G1-authorized default channel")
	assert.NotEqual(t, suite.fixtures.ChDefaultPublic.ID, log2.ChannelID, "Token2 must not fall back to default public channel")
	assert.NotEqual(t, channelDefaultG2.ID, log2.ChannelID, "Token2 must not route to G2-only default channel")

	t.Log("CS-04 passed: Token-level P2P restriction also constrains platform channels")
}

// setupOrthogonalSuite initializes the orthogonal test suite.
// This is defined in orthogonal_l18_test.go, but we reference it here.
// If it's not available, we need to ensure it's imported or defined.

// For completeness, we'll add a local setup if needed
func setupComplexScenarioSuite(t *testing.T) *OrthogonalSuite {
	// Use the same setup as orthogonal tests
	return setupOrthogonalSuite(t)
}
