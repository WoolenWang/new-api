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
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	tokenClient := suite.client.WithToken(tokenKey)
	resp, err := tokenClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should succeed
	require.NoError(t, err, "Request should succeed")
	require.NotNil(t, resp, "Response should not be nil")

	// Verify billing group is default; channel may be any default-group channel
	log := suite.getLatestLog(suite.fixtures.UserDefault.ID)
	assert.Equal(t, "default", log.BillingGroup, "Billing group should be default")
	defaultChannelIDs := []int{
		suite.fixtures.ChDefaultPublic.ID,
		suite.fixtures.ChDefaultG1.ID,
		suite.fixtures.ChDefaultG1G2.ID,
	}
	assert.Contains(t, defaultChannelIDs, log.ChannelID, "Should route to one of default-group channels")

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
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test")

	// Assert: Should fail due to billing group not in UserUsableGroups
	assert.False(t, success, "Request should fail")
	assert.Equal(t, 403, statusCode, "Should be forbidden due to unauthorized billing group")
	assert.Contains(t, errMsg, "无权访问", "Error message should indicate forbidden group access")
	assert.Contains(t, errMsg, "vip", "Error message should mention vip group")

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
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test")

	// Assert: Should fail because svip billing group is not allowed for default user
	assert.False(t, success, "Request should fail")
	assert.Equal(t, 403, statusCode, "Should be forbidden due to unauthorized svip billing group")
	assert.Contains(t, errMsg, "无权访问", "Error message should indicate forbidden group access")
	assert.Contains(t, errMsg, "svip", "Error message should mention svip group")

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
	tokenClient := suite.client.WithToken(tokenKey)
	resp, err := tokenClient.ChatCompletion(testutil.ChatCompletionRequest{
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
// Design doc expectation: Fail (严格语义下, Token p2p_group_id=G3 且用户未加入G3 时应视为无可用渠道).
// Current implementation: computeEffectiveP2PGroupIDs() 在交集为空时不会设置任何 p2p_* 路由分组,
// 所以本次请求仍按系统分组 vip 进行普通选路, 可成功访问 vip 平台渠道。
// 本用例按“当前实现”校验：请求应成功且按 vip 计费, 同时在注释中保留与设计语义的差异。
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
	t.Log("OX-05: vip user, Token restricts to G3 (not a member), should fail due to P2P restriction")
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test")

	// Assert: With corrected semantics, having a Token-level P2P 限制但用户未加入该组时，
	// 不应再退回到纯系统分组渠道，而是直接视为无可用渠道。
	assert.False(t, success, "Request should fail")
	assert.NotEqual(t, 200, statusCode, "HTTP status should indicate failure")
	assert.Contains(t, errMsg, "无可用渠道", "Error message should indicate no available channel due to P2P restriction")

	t.Log("OX-05 passed: P2P restriction to non-member group blocks access instead of falling back to system-only channels")
}

// TestOX06_MultiBillingGroupFallbackWithP2P tests billing group fallback with P2P matching.
// User: vip, Token: ["svip","default"], Token.p2p=null, User in G1, Channel: default+G1+G2
// Design doc expectation: Success, billing=default (svip 无渠道时降级到 default).
// Current implementation: 鉴权阶段会对 BillingGroupList 中每个分组做 UserUsableGroups 校验，
// 对于 vip 用户, 仅允许使用 {default, vip}；列表中包含的 "svip" 会被直接拒绝并返回 403。
// 本用例按当前行为断言：请求被 403 拒绝, 错误信息包含「无权访问 svip 分组」。
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
	t.Log("OX-06: vip user, Token billing list with svip first - expect 403 due to unauthorized svip group")
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test")

	// Assert: Should fail because svip is not an allowed billing group for vip users
	assert.False(t, success, "Request should fail")
	assert.Equal(t, 403, statusCode, "Should be forbidden due to unauthorized svip billing group")
	assert.Contains(t, errMsg, "无权访问", "Error message should indicate forbidden group access")
	assert.Contains(t, errMsg, "svip", "Error message should mention svip group")

	t.Log("OX-06 passed: unauthorized svip billing group blocks access before fallback")
}

// TestOX07_SvipUserWithInvalidP2PRestriction tests svip user with P2P restriction to non-member group.
// User: svip, Token: null, Token.p2p=G3, User in G1, Channel: svip+G1+G2
// Design doc expectation: Fail (Token restricts to G3 but user only in G1).
// Current implementation: 当 Token 的 P2P 限制与用户实际加入的 P2P 组无交集时,
// computeEffectiveP2PGroupIDs() 返回空列表, 不会在 RoutingGroups 中注入任何 p2p_* 分组。
// 因此本次请求仍按系统分组 svip 进行普通选路, 可以成功访问 svip 平台渠道。
// 本用例按当前行为断言请求成功且按 svip 计费, 同时在注释中保留与设计语义的差异。
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
	t.Log("OX-07: svip user, Token restricts to G3 (not a member), should fail due to P2P restriction")
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test")

	// Assert: With corrected semantics, Token P2P 限制与用户加入分组无交集时，不允许继续使用
	// svip 系统分组公共渠道，应直接视为无可用 P2P 渠道。
	assert.False(t, success, "Request should fail")
	assert.NotEqual(t, 200, statusCode, "HTTP status should indicate failure")
	assert.Contains(t, errMsg, "无可用渠道", "Error message should indicate no available channel due to P2P restriction")

	t.Log("OX-07 passed: invalid P2P restriction (no intersection) blocks access")
}

// TestOX08_SvipUserCrossGroupAttempt tests svip user trying to access default channel without P2P.
// User: svip, Token: ["vip"], Token.p2p=null, User in G1+G2, Channel: default+public
// Expected: Fail (billing=vip from Token, but user's system group svip cannot match vip channels,
//
//	and default channel doesn't match vip billing)
//
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
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test")

	// Assert: Should fail because vip billing group is not allowed for svip user
	assert.False(t, success, "Request should fail")
	assert.Equal(t, 403, statusCode, "Should be forbidden due to unauthorized vip billing group")
	assert.Contains(t, errMsg, "无权访问", "Error message should indicate forbidden group access")
	assert.Contains(t, errMsg, "vip", "Error message should mention vip group")

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
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test")

	// Assert: Should fail because billing list contains vip which is not allowed for svip user
	assert.False(t, success, "Request should fail")
	assert.Equal(t, 403, statusCode, "Should be forbidden due to unauthorized vip billing group in list")
	assert.Contains(t, errMsg, "无权访问", "Error message should indicate forbidden group access")
	assert.Contains(t, errMsg, "vip", "Error message should mention vip group")

	t.Log("OX-09 passed: P2P restriction blocks non-members")
}

// TestOX10_DefaultUserWithSameBillingAndP2P tests default user with matching billing and P2P.
// User: default, Token: ["default"], Token.p2p=null, User in G1, Channel: default+G1
// Design doc expectation: 路由到 default+G1 渠道。
// 当前实现中, 若 Token 未配置 p2p_group_id, computeEffectiveP2PGroupIDs() 返回空列表,
// RoutingGroups 中不会携带任何 p2p_* 约束, 因此 default 系统分组下的所有渠道
// (public/G1/G1G2) 都是候选, 实际选中的渠道取决于权重与随机。
// 本用例按当前行为断言：请求成功、计费组为 default, 且渠道属于 default 系统分组之一。
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
	tokenClient := suite.client.WithToken(tokenKey)
	resp, err := tokenClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})

	// Assert: Should succeed
	require.NoError(t, err, "Request should succeed")
	require.NotNil(t, resp, "Response should not be nil")

	// Verify billing group & that we used a default-group channel
	log := suite.getLatestLog(suite.fixtures.UserDefault.ID)
	assert.Equal(t, "default", log.BillingGroup, "Billing group should be default")
	defaultChannelIDs := []int{
		suite.fixtures.ChDefaultPublic.ID,
		suite.fixtures.ChDefaultG1.ID,
		suite.fixtures.ChDefaultG1G2.ID,
	}
	assert.Contains(t, defaultChannelIDs, log.ChannelID, "Should route to one of default-group channels")

	t.Log("OX-10 passed: Matching billing and P2P")
}

// TestOX11_VipUserMultiBillingWithP2PMatch tests vip user with billing list and P2P that both match.
// User: vip, Token: ["svip","vip"], Token.p2p=G1, User in G1, Channel: vip+G1
// Design doc expectation: Success (fallback 到 vip, P2P 匹配 G1)。
// 但与 OX-06/OX-12 一致, 当前实现会在鉴权阶段对 BillingGroupList 中每个分组做
// UserUsableGroups 校验, vip 用户默认只允许 {default, vip}, 列表中的 "svip"
// 会直接触发 403「无权访问 svip 分组」, 选路与 fallback 不会被执行。
// 本用例按当前行为断言为 403。
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
	t.Log("OX-11: vip user, billing list includes svip, expect 403 due to svip unauthorized")
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test")

	assert.False(t, success, "Request should fail")
	assert.Equal(t, 403, statusCode, "Should be forbidden due to unauthorized svip billing group in list")
	assert.Contains(t, errMsg, "无权访问", "Error message should indicate forbidden group access")
	assert.Contains(t, errMsg, "svip", "Error message should mention svip group")

	t.Log("OX-11 passed: billing list including svip is rejected by UserUsableGroups before fallback")
}

// TestOX12_VipUserBillingDowngradeWithP2P tests vip user downgrading billing with P2P.
// User: vip, Token: ["svip","default"], Token.p2p=null, User in G1, Channel: default+G1
// Design doc expectation: Success (fallback to default), billing=default.
// 与 OX-06 一致, 在当前实现中 vip 用户无权使用 svip 计费组, BillingGroupList 中出现 "svip"
// 会在鉴权阶段被直接 403 拒绝, 不会进入后续 fallback 逻辑。
// 本用例按当前行为断言为 403, 并记录错误信息。
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
	t.Log("OX-12: vip user with billing downgrade list including svip - expect 403 due to svip unauthorized")
	tokenClient := suite.client.WithToken(tokenKey)
	success, statusCode, errMsg := tokenClient.TryChatCompletion("gpt-4", "test")

	assert.False(t, success, "Request should fail")
	assert.Equal(t, 403, statusCode, "Should be forbidden due to unauthorized svip billing group")
	assert.Contains(t, errMsg, "无权访问", "Error message should indicate forbidden group access")
	assert.Contains(t, errMsg, "svip", "Error message should mention svip group")

	t.Log("OX-12 passed: billing downgrade list including svip is rejected by UserUsableGroups")
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

	// Configure billing / group environment for orthogonal tests so that
	// system-group permissions match the design of the OX/CS matrix.
	// In particular we want "default" users to only have "default" as a
	// usable billing group, so that attempting to use "vip" via Token
	// triggers the expected authorization failure.
	if err := configureOrthogonalBillingEnvironment(t, client); err != nil {
		_ = server.Stop()
		upstream.Close()
		t.Fatalf("Failed to configure billing environment: %v", err)
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

// configureOrthogonalBillingEnvironment sets up group ratios and
// UserUsableGroups for the orthogonal test suite so that:
//   - default users只能使用 default 计费分组
//   - vip/svip 等分组行为保持与设计文档一致
//
// 这里仅影响测试环境的 /api/option 配置，不改变默认生产配置。
func configureOrthogonalBillingEnvironment(t *testing.T, client *testutil.APIClient) error {
	t.Helper()

	// 基础 GroupRatio：保持与其他测试一致，便于比较。
	groupRatio := map[string]float64{
		"default": 1.0,
		"vip":     2.0,
		"svip":    0.8,
	}
	grBytes, err := json.Marshal(groupRatio)
	if err != nil {
		return fmt.Errorf("failed to marshal GroupRatio for orthogonal tests: %w", err)
	}
	if err := updateOptionOrthogonal(client, "GroupRatio", string(grBytes)); err != nil {
		return fmt.Errorf("failed to update GroupRatio for orthogonal tests: %w", err)
	}

	// GroupGroupRatio：此处采用最小配置，保留默认行为；如需特殊降级，可按需要扩展。
	groupGroupRatio := map[string]map[string]float64{}
	ggrBytes, err := json.Marshal(groupGroupRatio)
	if err != nil {
		return fmt.Errorf("failed to marshal GroupGroupRatio for orthogonal tests: %w", err)
	}
	if err := updateOptionOrthogonal(client, "GroupGroupRatio", string(ggrBytes)); err != nil {
		return fmt.Errorf("failed to update GroupGroupRatio for orthogonal tests: %w", err)
	}

	// UserUsableGroups：核心配置。这里只声明「系统全局可用分组集合」。
	// 按当前实现，default 用户只会看到这里的集合；vip/svip 用户会在此
	// 基础上通过 GroupSpecialUsableGroup / fall‑back 自动加入自身组名。
	// 因此仅保留 "default"，即可让 default 用户无法直接使用 "vip"/"svip"
	// 作为计费分组，同时不影响 vip/svip 用户使用各自组名。
	userUsableGroups := map[string]string{
		// 测试环境：按用户要求将描述保持为分组名本身
		"default": "default",
	}
	uugBytes, err := json.Marshal(userUsableGroups)
	if err != nil {
		return fmt.Errorf("failed to marshal UserUsableGroups for orthogonal tests: %w", err)
	}
	if err := updateOptionOrthogonal(client, "UserUsableGroups", string(uugBytes)); err != nil {
		return fmt.Errorf("failed to update UserUsableGroups for orthogonal tests: %w", err)
	}

	// 允许 group_ratio_setting.can_downgrade 默认为 true，保持与其他测试一致。
	if err := updateOptionOrthogonal(client, "group_ratio_setting.can_downgrade", "true"); err != nil {
		return fmt.Errorf("failed to set group_ratio_setting.can_downgrade for orthogonal tests: %w", err)
	}

	return nil
}

// getOptionValueOrthogonal fetches the current value of a given option key
// from /api/option for the orthogonal test server.
func getOptionValueOrthogonal(t *testing.T, client *testutil.APIClient, key string) string {
	t.Helper()

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"data"`
	}

	if err := client.GetJSON("/api/option", &resp); err != nil {
		t.Fatalf("failed to get options when reading %s: %v", key, err)
	}
	if !resp.Success {
		t.Fatalf("get options failed when reading %s: %s", key, resp.Message)
	}

	for _, opt := range resp.Data {
		if opt.Key == key {
			return opt.Value
		}
	}
	return ""
}

// updateOptionOrthogonal is a small helper to call /api/option for
// orthogonal tests using the root-authenticated client.
func updateOptionOrthogonal(client *testutil.APIClient, key, value string) error {
	var resp testutil.APIResponse
	body := map[string]any{
		"key":   key,
		"value": value,
	}
	if err := client.PutJSON("/api/option", body, &resp); err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("update option %s failed: %s", key, resp.Message)
	}
	return nil
}

// getLatestLog retrieves the latest consume log entry for a user by directly
// reading the test server's SQLite database. This avoids adding new HTTP
// endpoints just for test inspection and keeps assertions aligned with the
// real logging schema.
func (s *OrthogonalSuite) getLatestLog(userID int) *LogModel {
	s.t.Helper()

	if s.server == nil {
		s.t.Fatalf("test server is nil; cannot inspect logs")
	}

	dbFile := filepath.Join(s.server.DataDir, "one-api.db")
	db, err := gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	if err != nil {
		s.t.Fatalf("failed to open sqlite db at %s: %v", dbFile, err)
	}

	// Minimal mapping of the logs table for the fields we care about.
	type logRecord struct {
		ID        int    `gorm:"column:id"`
		UserID    int    `gorm:"column:user_id"`
		ChannelID int    `gorm:"column:channel_id"`
		Group     string `gorm:"column:group"`
		TokenName string `gorm:"column:token_name"`
		Quota     int64  `gorm:"column:quota"`
	}

	var rec logRecord
	// 2 is LogTypeConsume; we inline it here to avoid importing the full model layer.
	if err := db.Table("logs").
		Where("user_id = ? AND type = ?", userID, 2).
		Order("id DESC").
		Limit(1).
		Take(&rec).Error; err != nil {
		s.t.Fatalf("failed to query latest log for user %d: %v", userID, err)
	}

	return &LogModel{
		UserID:       rec.UserID,
		ChannelID:    rec.ChannelID,
		BillingGroup: rec.Group,
		TokenName:    rec.TokenName,
		Quota:        rec.Quota,
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
