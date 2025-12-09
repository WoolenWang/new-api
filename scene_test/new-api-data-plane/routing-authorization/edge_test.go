package routing_authorization

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// TestEdge_E01_UserInManyP2PGroups verifies routing when a user joins many P2P
// groups and only the last group has an available channel.
// Design ref: 2.6 E-01 用户加入大量分组.
func TestEdge_E01_UserInManyP2PGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Consumer user in default group.
	_, err := fixtures.CreateTestUser("edge_e01_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	userClient := suite.Client.Clone()
	if _, err := userClient.Login("edge_e01_user", "password123"); err != nil {
		t.Fatalf("failed to login user: %v", err)
	}

	// P2P owner user.
	owner, err := fixtures.CreateTestUser("edge_e01_owner", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
	ownerClient := suite.Client.Clone()
	if _, err := ownerClient.Login("edge_e01_owner", "password123"); err != nil {
		t.Fatalf("failed to login owner: %v", err)
	}

	// Use a reasonably large number of groups to exercise routing logic without
	// hitting global rate limits for group creation.
	const groupCount = 20
	var lastGroupID int

	// Create 100 P2P groups owned by owner; user joins each one.
	for i := 1; i <= groupCount; i++ {
		group, err := fixtures.CreateTestP2PGroup(
			fmt.Sprintf("edge-e01-g%d", i),
			ownerClient,
			owner.ID,
			testutil.P2PGroupTypeShared,
			testutil.P2PJoinMethodPassword,
			fmt.Sprintf("pass-%d", i),
		)
		if err != nil {
			t.Fatalf("failed to create group %d: %v", i, err)
		}

		if err := userClient.ApplyToP2PGroup(group.ID, fmt.Sprintf("pass-%d", i)); err != nil {
			t.Fatalf("user failed to join group %d: %v", i, err)
		}

		lastGroupID = group.ID
	}

	// Channel only authorized for the last P2P group.
	channel, err := fixtures.CreateTestChannel(
		"edge-e01-channel-g100",
		"gpt-4",
		"default",
		suite.Upstream.BaseURL,
		false,
		owner.ID,
		fmt.Sprintf("%d", lastGroupID),
	)
	if err != nil {
		t.Fatalf("failed to create channel for last group: %v", err)
	}
	t.Logf("E-01 channel ID=%d (authorized for group %d)", channel.ID, lastGroupID)

	// Token restricted to the last group only.
	tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "edge-e01-token",
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

	start := time.Now()
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "E-01 many P2P groups test")
	elapsed := time.Since(start)

	if !success {
		t.Fatalf("chat completion failed in E-01: status=%d err=%s", statusCode, errMsg)
	}

	if suite.Upstream.GetRequestCount() != 1 {
		t.Fatalf("expected 1 upstream request, got %d", suite.Upstream.GetRequestCount())
	}

	t.Logf("E-01 request latency: %v", elapsed)
	// Soft performance check: ensure routing completes in a reasonable time.
	if elapsed > 2*time.Second {
		t.Fatalf("E-01 routing took too long: %v", elapsed)
	}
}

// TestEdge_E02_ChannelAuthorizedManyP2PGroups verifies routing when a channel
// is authorized to many P2P groups but the user belongs to only one.
// Design ref: 2.6 E-02 渠道授权大量分组.
func TestEdge_E02_ChannelAuthorizedManyP2PGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Consumer user.
	_, err := fixtures.CreateTestUser("edge_e02_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	userClient := suite.Client.Clone()
	if _, err := userClient.Login("edge_e02_user", "password123"); err != nil {
		t.Fatalf("failed to login user: %v", err)
	}

	// P2P owner.
	owner, err := fixtures.CreateTestUser("edge_e02_owner", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
	ownerClient := suite.Client.Clone()
	if _, err := ownerClient.Login("edge_e02_owner", "password123"); err != nil {
		t.Fatalf("failed to login owner: %v", err)
	}

	// Use a reasonably large number of groups to exercise routing logic without
	// hitting global rate limits for group creation.
	const groupCount = 20
	groupIDs := make([]int, 0, groupCount)
	// Choose one group for membership; must be <= groupCount.
	targetIndex := 10
	var membershipGroupID int

	for i := 1; i <= groupCount; i++ {
		group, err := fixtures.CreateTestP2PGroup(
			fmt.Sprintf("edge-e02-g%d", i),
			ownerClient,
			owner.ID,
			testutil.P2PGroupTypeShared,
			testutil.P2PJoinMethodPassword,
			fmt.Sprintf("g2-pass-%d", i),
		)
		if err != nil {
			t.Fatalf("failed to create group %d: %v", i, err)
		}
		groupIDs = append(groupIDs, group.ID)

		if i == targetIndex {
			if err := userClient.ApplyToP2PGroup(group.ID, fmt.Sprintf("g2-pass-%d", i)); err != nil {
				t.Fatalf("user failed to join group %d: %v", i, err)
			}
			membershipGroupID = group.ID
		}
	}

	// Channel authorized to all groups; user is only member of one.
	allowedGroups := ""
	for idx, id := range groupIDs {
		if idx > 0 {
			allowedGroups += ","
		}
		allowedGroups += fmt.Sprintf("%d", id)
	}

	channel, err := fixtures.CreateTestChannel(
		"edge-e02-channel",
		"gpt-4",
		"default",
		suite.Upstream.BaseURL,
		false,
		owner.ID,
		allowedGroups,
	)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}
	t.Logf("E-02 channel ID=%d, membership group=%d", channel.ID, membershipGroupID)

	// Token restricted to the membership group.
	tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "edge-e02-token",
		Status:             1,
		UnlimitedQuota:     true,
		P2PGroupID:         &membershipGroupID,
		ModelLimitsJson:    "",
		ModelLimitsEnabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)
	start := time.Now()
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "E-02 many channel groups test")
	elapsed := time.Since(start)

	if !success {
		t.Fatalf("chat completion failed in E-02: status=%d err=%s", statusCode, errMsg)
	}

	if suite.Upstream.GetRequestCount() != 1 {
		t.Fatalf("expected 1 upstream request, got %d", suite.Upstream.GetRequestCount())
	}

	t.Logf("E-02 request latency: %v", elapsed)
	if elapsed > 2*time.Second {
		t.Fatalf("E-02 routing took too long: %v", elapsed)
	}
}

// TestEdge_E03_TokenWithoutP2PLimit verifies the new semantics for tokens
// without P2P group limits: even if the user has joined many P2P groups,
// P2P-restricted channels are not accessible without an explicit token limit.
// This aligns the test with the updated design in distributor.computeEffectiveP2PGroupIDs.
func TestEdge_E03_TokenWithoutP2PLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Consumer user in default group.
	_, err := fixtures.CreateTestUser("edge_e03_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	userClient := suite.Client.Clone()
	if _, err := userClient.Login("edge_e03_user", "password123"); err != nil {
		t.Fatalf("failed to login user: %v", err)
	}

	// P2P owner user.
	owner, err := fixtures.CreateTestUser("edge_e03_owner", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
	ownerClient := suite.Client.Clone()
	if _, err := ownerClient.Login("edge_e03_owner", "password123"); err != nil {
		t.Fatalf("failed to login owner: %v", err)
	}

	const groupCount = 20
	var someGroupID int

	// User joins many P2P groups.
	for i := 1; i <= groupCount; i++ {
		group, err := fixtures.CreateTestP2PGroup(
			fmt.Sprintf("edge-e03-g%d", i),
			ownerClient,
			owner.ID,
			testutil.P2PGroupTypeShared,
			testutil.P2PJoinMethodPassword,
			fmt.Sprintf("e3-pass-%d", i),
		)
		if err != nil {
			t.Fatalf("failed to create group %d: %v", i, err)
		}
		if err := userClient.ApplyToP2PGroup(group.ID, fmt.Sprintf("e3-pass-%d", i)); err != nil {
			t.Fatalf("user failed to join group %d: %v", i, err)
		}
		if i == groupCount {
			someGroupID = group.ID
		}
	}

	// P2P-restricted channel (non-platform).
	channel, err := fixtures.CreateTestChannel(
		"edge-e03-p2p-channel",
		"gpt-4",
		"default",
		suite.Upstream.BaseURL,
		false,
		owner.ID,
		fmt.Sprintf("%d", someGroupID),
	)
	if err != nil {
		t.Fatalf("failed to create P2P channel: %v", err)
	}
	t.Logf("E-03 p2p-only channel ID=%d, group=%d", channel.ID, someGroupID)

	// Token WITHOUT any P2P limit (p2p_group_id not set).
	tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "edge-e03-token-no-p2p",
		Status:             1,
		UnlimitedQuota:     true,
		ModelLimitsJson:    "",
		ModelLimitsEnabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)

	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "E-03 token without P2P limit")
	if success {
		t.Fatalf("expected request with token lacking P2P limit to fail for P2P-only channel, but succeeded")
	}
	t.Logf("E-03 correctly blocked access without P2P token limit: status=%d err=%s", statusCode, errMsg)
}
