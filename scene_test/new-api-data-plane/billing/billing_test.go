// Package billing contains integration tests for billing correctness.
//
// Test Focus:
// ===========
// This package validates the billing system's adherence to the core principle:
// "计费看自己" (Billing depends on the consumer's own group, not the channel provider's).
//
// Key Test Scenarios:
// - B-01: High-rate user uses low-rate channel (bills at user's high rate)
// - B-02: Low-rate user uses high-rate channel (bills at user's low rate)
// - B-03: Token forces billing group override
// - B-04: Token billing group list failover
// - B-05: Anti-downgrade protection
// - B-06: P2P sharing revenue split
package billing

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// TestSuite holds shared test resources for billing tests.
type TestSuite struct {
	Server   *testutil.TestServer
	Client   *testutil.APIClient
	Upstream *testutil.MockUpstreamServer
	Fixtures *testutil.TestFixtures
}

// SetupSuite initializes the test suite with a running server.
func SetupSuite(t *testing.T) (*TestSuite, func()) {
	t.Helper()

	// Start mock upstream first so we can wire channels to it
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

	// Initialize system and login as root (admin)
	t.Log("Initializing system...")
	rootUser, rootPass, err := client.InitializeSystem()
	if err != nil {
		upstream.Close()
		_ = server.Stop()
		t.Fatalf("Failed to initialize system: %v", err)
	}
	t.Logf("System initialized with root user: %s", rootUser)

	if _, err := client.Login(rootUser, rootPass); err != nil {
		upstream.Close()
		_ = server.Stop()
		t.Fatalf("Failed to login as root: %v", err)
	}
	t.Log("Logged in as root user")

	// Create fixtures bound to this server and upstream
	fixtures := testutil.NewTestFixtures(t, client)
	fixtures.SetUpstream(upstream)

	// Configure billing-related options (group ratios, usable groups, share ratio)
	if err := configureBillingEnvironment(t, client); err != nil {
		upstream.Close()
		_ = server.Stop()
		t.Fatalf("Failed to configure billing environment: %v", err)
	}

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

// configureBillingEnvironment sets up group ratios, usable groups and share ratio
// according to the design docs used by the billing tests.
func configureBillingEnvironment(t *testing.T, client *testutil.APIClient) error {
	t.Helper()

	// 1. Configure basic group ratios:
	//    default: 1.0, vip: 2.0, svip: 0.8
	groupRatio := map[string]float64{
		"default": 1.0,
		"vip":     2.0,
		"svip":    0.8,
	}
	grBytes, err := json.Marshal(groupRatio)
	if err != nil {
		return fmt.Errorf("failed to marshal GroupRatio: %w", err)
	}
	if err := updateOption(client, "GroupRatio", string(grBytes)); err != nil {
		return fmt.Errorf("failed to update GroupRatio: %w", err)
	}

	// 2. Configure special group-group ratio:
	//    svip user using default group: 0.5
	groupGroupRatio := map[string]map[string]float64{
		"svip": {
			"default": 0.5,
		},
	}
	ggrBytes, err := json.Marshal(groupGroupRatio)
	if err != nil {
		return fmt.Errorf("failed to marshal GroupGroupRatio: %w", err)
	}
	if err := updateOption(client, "GroupGroupRatio", string(ggrBytes)); err != nil {
		return fmt.Errorf("failed to update GroupGroupRatio: %w", err)
	}

	// 3. Configure user-usable groups to include default, vip, svip, auto.
	userUsableGroups := map[string]string{
		"default": "Default group",
		"vip":     "VIP group",
		"svip":    "SVIP group",
		"auto":    "Auto group",
	}
	uugBytes, err := json.Marshal(userUsableGroups)
	if err != nil {
		return fmt.Errorf("failed to marshal UserUsableGroups: %w", err)
	}
	if err := updateOption(client, "UserUsableGroups", string(uugBytes)); err != nil {
		return fmt.Errorf("failed to update UserUsableGroups: %w", err)
	}

	// 4. Configure P2P share ratio used by B-06 (default tests may override).
	if err := updateOption(client, "p2p_setting.share_ratio", "0.5"); err != nil {
		return fmt.Errorf("failed to update p2p_setting.share_ratio: %w", err)
	}

	// 5. Ensure downgrade protection is enabled by default (can_downgrade=true).
	if err := updateOption(client, "group_ratio_setting.can_downgrade", "true"); err != nil {
		return fmt.Errorf("failed to set default can_downgrade=true: %w", err)
	}

	return nil
}

// updateOption is a small helper to call /api/option.
func updateOption(client *testutil.APIClient, key, value string) error {
	var resp testutil.APIResponse
	body := map[string]interface{}{
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

// getUserQuota retrieves the quota for a given user ID.
func getUserQuota(t *testing.T, client *testutil.APIClient, userID int) int {
	t.Helper()
	user, err := client.GetUser(userID)
	if err != nil {
		t.Fatalf("failed to get user %d: %v", userID, err)
	}
	return int(user.Quota)
}

// getUserShareQuota retrieves the share_quota for a given user ID.
func getUserShareQuota(t *testing.T, client *testutil.APIClient, userID int) int {
	t.Helper()
	user, err := client.GetUser(userID)
	if err != nil {
		t.Fatalf("failed to get user %d: %v", userID, err)
	}
	return int(user.ShareQuota)
}

// approxEqualRatio checks whether actual ~= expected within a relative tolerance.
func approxEqualRatio(t *testing.T, actual, expected float64, tolerance float64, msg string) {
	t.Helper()
	if expected == 0 {
		t.Fatalf("expected ratio is zero in approxEqualRatio: %s", msg)
	}
	diff := math.Abs(actual-expected) / expected
	if diff > tolerance {
		t.Fatalf("%s: actual ratio=%.4f, expected=%.4f, diff=%.4f > tolerance=%.4f",
			msg, actual, expected, diff, tolerance)
	}
}

// measureBaselineQuotaForGroup performs a single chat completion for a fresh user
// in the given system group and returns the consumed quota. It is used to
// compare effective billing ratios across different scenarios.
func measureBaselineQuotaForGroup(t *testing.T, suite *TestSuite, group string) int {
	t.Helper()

	fixtures := suite.Fixtures

	username := fmt.Sprintf("orth_base_%s_user", group)
	password := "password123"

	user, err := fixtures.CreateTestUser(username, password, group)
	if err != nil {
		t.Fatalf("failed to create baseline user for group %s: %v", group, err)
	}

	userClient := suite.Client.Clone()
	if _, err := userClient.Login(username, password); err != nil {
		t.Fatalf("failed to login baseline user for group %s: %v", group, err)
	}

	channelName := fmt.Sprintf("orth-base-%s-channel", group)
	channel, err := fixtures.CreateTestChannel(
		channelName,
		"gpt-4",
		group,
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create baseline channel for group %s: %v", group, err)
	}
	t.Logf("Baseline channel for group %s created with ID=%d", group, channel.ID)

	tokenName := fmt.Sprintf("orth-base-%s-token", group)
	tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:           tokenName,
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create baseline token for group %s: %v", group, err)
	}

	quotaBefore := getUserQuota(t, suite.Client, user.ID)
	apiClient := suite.Client.WithToken(tokenKey)
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", fmt.Sprintf("baseline request for group %s", group))
	if !success {
		t.Fatalf("baseline request for group %s failed: status=%d, err=%s", group, statusCode, errMsg)
	}
	quotaAfter := getUserQuota(t, suite.Client, user.ID)
	used := quotaBefore - quotaAfter
	if used <= 0 {
		t.Fatalf("expected baseline used quota for group %s > 0, got %d", group, used)
	}

	t.Logf("Baseline used quota for group %s: %d", group, used)
	return used
}

// TestBilling_B01_HighRateUserLowRateChannel tests billing when a high-rate user uses a low-rate channel.
// Scenario: User A (vip, rate=2.0) uses User B's channel (default, rate=1.0) via P2P sharing.
// Expected: Billing should use User A's vip rate (2.0).
func TestBilling_B01_HighRateUserLowRateChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Create User A (vip, high rate) and User B (default, low rate)
	userA, err := fixtures.CreateTestUser("b01_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}
	userB, err := fixtures.CreateTestUser("b01_userB", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	// Create per-user clients and login
	userAClient := suite.Client.Clone()
	if _, err := userAClient.Login("b01_userA", "password123"); err != nil {
		t.Fatalf("failed to login user A: %v", err)
	}
	userBClient := suite.Client.Clone()
	if _, err := userBClient.Login("b01_userB", "password123"); err != nil {
		t.Fatalf("failed to login user B: %v", err)
	}

	// Baseline channel for default group (no P2P restriction, platform-owned)
	baselineChannel, err := fixtures.CreateTestChannel(
		"b01-baseline-default",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		0, // platform channel, no owner
		"",
	)
	if err != nil {
		t.Fatalf("failed to create baseline default channel: %v", err)
	}
	t.Logf("Created baseline default channel ID=%d", baselineChannel.ID)

	// Create channel owned by User B in "vip" group. This represents
	// "User B's channel"; routing still honours the consumer's BillingGroup
	// (vip for user A).
	channel, err := fixtures.CreateTestChannel(
		"b01-channel-B-vip",
		"gpt-4",
		"vip",
		fixtures.GetUpstreamURL(),
		false,    // not private
		userB.ID, // owner is B
		"",       // no P2P restriction needed for billing test
	)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}
	t.Logf("Created channel (owned by B) with ID=%d", channel.ID)

	// Create API tokens for both users
	tokenB, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b01-token-B",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token for user B: %v", err)
	}
	tokenA, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b01-token-A",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token for user A: %v", err)
	}

	// Baseline: default-rate user B uses the baseline default-group channel
	baseQuotaBefore := getUserQuota(t, suite.Client, userB.ID)
	apiClientB := suite.Client.WithToken(tokenB)
	success, statusCode, errMsg := apiClientB.TryChatCompletion("gpt-4", "B-01 baseline request from B")
	if !success {
		t.Fatalf("baseline request failed: status=%d, err=%s", statusCode, errMsg)
	}
	baseQuotaAfter := getUserQuota(t, suite.Client, userB.ID)
	baseUsed := baseQuotaBefore - baseQuotaAfter
	if baseUsed <= 0 {
		t.Fatalf("expected baseline used quota > 0, got %d", baseUsed)
	}
	t.Logf("Baseline used quota by user B (default group): %d", baseUsed)

	// Test: high-rate user A uses User B's channel via P2P
	quotaBeforeA := getUserQuota(t, suite.Client, userA.ID)
	apiClientA := suite.Client.WithToken(tokenA)
	success, statusCode, errMsg = apiClientA.TryChatCompletion("gpt-4", "B-01 request from A via B's channel")
	if !success {
		t.Fatalf("request from A via B's channel failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterA := getUserQuota(t, suite.Client, userA.ID)
	usedA := quotaBeforeA - quotaAfterA
	if usedA <= 0 {
		t.Fatalf("expected used quota for user A > 0, got %d", usedA)
	}
	t.Logf("Used quota by user A (vip) via B's channel: %d", usedA)

	// Expect A's usage to be approximately 2x B's usage (vip rate=2.0 vs default=1.0)
	actualRatio := float64(usedA) / float64(baseUsed)
	approxEqualRatio(t, actualRatio, 2.0, 0.05, "B-01: vip vs default billing ratio mismatch")
}

// TestBilling_B02_LowRateUserHighRateChannel tests billing when a low-rate user uses a high-rate channel.
// Scenario: User B (default, rate=1.0) uses User A's channel (vip, rate=2.0) via P2P sharing.
// Expected: Billing should use User B's default rate (1.0).
func TestBilling_B02_LowRateUserHighRateChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Create User A (vip, channel owner) and User B (default, consumer)
	userA, err := fixtures.CreateTestUser("b02_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}
	userB, err := fixtures.CreateTestUser("b02_userB", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	userAClient := suite.Client.Clone()
	if _, err := userAClient.Login("b02_userA", "password123"); err != nil {
		t.Fatalf("failed to login user A: %v", err)
	}
	userBClient := suite.Client.Clone()
	if _, err := userBClient.Login("b02_userB", "password123"); err != nil {
		t.Fatalf("failed to login user B: %v", err)
	}

	// Baseline: default user B uses a default-group channel
	defaultChannel, err := fixtures.CreateTestChannel(
		"b02-default-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create default channel: %v", err)
	}
	t.Logf("Created baseline default channel ID=%d", defaultChannel.ID)

	tokenBDefault, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b02-token-B-default",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create baseline token for user B: %v", err)
	}

	quotaBeforeBase := getUserQuota(t, suite.Client, userB.ID)
	apiClientB := suite.Client.WithToken(tokenBDefault)
	success, statusCode, errMsg := apiClientB.TryChatCompletion("gpt-4", "B-02 baseline request from B")
	if !success {
		t.Fatalf("baseline request from B failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterBase := getUserQuota(t, suite.Client, userB.ID)
	baseUsed := quotaBeforeBase - quotaAfterBase
	if baseUsed <= 0 {
		t.Fatalf("expected baseline used quota > 0, got %d", baseUsed)
	}
	t.Logf("Baseline used quota by user B (default group): %d", baseUsed)

	// Create P2P group owned by A and channel in vip group authorized to that group
	p2pGroupID, err := userAClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:       "b02-shared-group",
		OwnerId:    userA.ID,
		Type:       testutil.P2PGroupTypeShared,
		JoinMethod: testutil.P2PJoinMethodPassword,
		JoinKey:    "b02pass",
	})
	if err != nil {
		t.Fatalf("failed to create P2P group: %v", err)
	}
	if err := userBClient.ApplyToP2PGroup(p2pGroupID, "b02pass"); err != nil {
		t.Fatalf("failed to add user B to P2P group: %v", err)
	}

	channelVip, err := fixtures.CreateTestChannel(
		"b02-channel-A-vip",
		"gpt-4",
		"vip",
		fixtures.GetUpstreamURL(),
		false,
		userA.ID,
		fmt.Sprintf("%d", p2pGroupID),
	)
	if err != nil {
		t.Fatalf("failed to create vip channel for A: %v", err)
	}
	t.Logf("Created vip channel owned by A with ID=%d", channelVip.ID)

	// Token for B to access A's channel via P2P (no special billing group override)
	tokenBP2P, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b02-token-B-p2p",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create P2P token for B: %v", err)
	}

	quotaBeforeP2P := getUserQuota(t, suite.Client, userB.ID)
	apiClientBP2P := suite.Client.WithToken(tokenBP2P)
	success, statusCode, errMsg = apiClientBP2P.TryChatCompletion("gpt-4", "B-02 request from B via A's vip channel")
	if !success {
		t.Fatalf("request from B via A's channel failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterP2P := getUserQuota(t, suite.Client, userB.ID)
	usedP2P := quotaBeforeP2P - quotaAfterP2P
	if usedP2P <= 0 {
		t.Fatalf("expected used quota for B via A's channel > 0, got %d", usedP2P)
	}
	t.Logf("Used quota by user B via A's vip channel: %d", usedP2P)

	// Expect B's usage via A's vip channel ~= baseline usage (rate=1.0)
	actualRatio := float64(usedP2P) / float64(baseUsed)
	approxEqualRatio(t, actualRatio, 1.0, 0.05, "B-02: default user should be billed at default rate even on vip channel")
}

// TestBilling_B03_TokenForceBillingGroup tests Token-level billing group override.
// Scenario: User A (vip, rate=2.0) uses a Token with group="default" (rate=1.0).
// Expected: Billing should use Token's specified rate (1.0).
func TestBilling_B03_TokenForceBillingGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Create User A (vip)
	userA, err := fixtures.CreateTestUser("b03_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}

	userAClient := suite.Client.Clone()
	if _, err := userAClient.Login("b03_userA", "password123"); err != nil {
		t.Fatalf("failed to login user A: %v", err)
	}

	// Create vip-group channel and default-group channel
	channelVip, err := fixtures.CreateTestChannel(
		"b03-vip-channel",
		"gpt-4",
		"vip",
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create vip channel: %v", err)
	}
	t.Logf("Created vip channel ID=%d", channelVip.ID)

	channelDefault, err := fixtures.CreateTestChannel(
		"b03-default-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create default channel: %v", err)
	}
	t.Logf("Created default channel ID=%d", channelDefault.ID)

	// Baseline: User A uses vip group (no token group override)
	tokenVip, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b03-token-vip",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create baseline token: %v", err)
	}

	quotaBeforeVip := getUserQuota(t, suite.Client, userA.ID)
	apiClientVip := suite.Client.WithToken(tokenVip)
	success, statusCode, errMsg := apiClientVip.TryChatCompletion("gpt-4", "B-03 baseline vip request")
	if !success {
		t.Fatalf("baseline vip request failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterVip := getUserQuota(t, suite.Client, userA.ID)
	usedVip := quotaBeforeVip - quotaAfterVip
	if usedVip <= 0 {
		t.Fatalf("expected used quota for vip baseline > 0, got %d", usedVip)
	}
	t.Logf("Baseline used quota in vip group: %d", usedVip)

	// Token with group=["default"] should force billing to use default rate (1.0)
	tokenDefault, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b03-token-default",
		Status:         1,
		UnlimitedQuota: true,
		Group:          `["default"]`,
	})
	if err != nil {
		t.Fatalf("failed to create token with default billing group: %v", err)
	}

	quotaBeforeDefault := getUserQuota(t, suite.Client, userA.ID)
	apiClientDefault := suite.Client.WithToken(tokenDefault)
	success, statusCode, errMsg = apiClientDefault.TryChatCompletion("gpt-4", "B-03 request with token forcing default group")
	if !success {
		t.Fatalf("request with forced default group failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterDefault := getUserQuota(t, suite.Client, userA.ID)
	usedDefault := quotaBeforeDefault - quotaAfterDefault
	if usedDefault <= 0 {
		t.Fatalf("expected used quota with default group > 0, got %d", usedDefault)
	}
	t.Logf("Used quota with token forcing default group: %d", usedDefault)

	// Since vip rate=2.0 and default rate=1.0, we expect usedVip ~= 2 * usedDefault
	actualRatio := float64(usedVip) / float64(usedDefault)
	approxEqualRatio(t, actualRatio, 2.0, 0.05, "B-03: token billing group override did not use default rate")
}

// TestBilling_B04_TokenBillingGroupFailover tests billing when Token's first billing group has no channels.
// Scenario: Token has group=["svip", "default"], but no svip channels exist.
// Expected: System falls back to "default" and bills at default rate.
func TestBilling_B04_TokenBillingGroupFailover(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Create User A (vip)
	userA, err := fixtures.CreateTestUser("b04_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}

	userAClient := suite.Client.Clone()
	if _, err := userAClient.Login("b04_userA", "password123"); err != nil {
		t.Fatalf("failed to login user A: %v", err)
	}

	// Baseline tokens and channels:
	// - default group channel
	// - vip group channel
	channelDefault, err := fixtures.CreateTestChannel(
		"b04-default-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create default channel: %v", err)
	}
	t.Logf("Created default channel ID=%d", channelDefault.ID)

	channelVip, err := fixtures.CreateTestChannel(
		"b04-vip-channel",
		"gpt-4",
		"vip",
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create vip channel: %v", err)
	}
	t.Logf("Created vip channel ID=%d", channelVip.ID)

	// Baseline: billing at default rate
	tokenDefault, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b04-token-default",
		Status:         1,
		UnlimitedQuota: true,
		Group:          `["default"]`,
	})
	if err != nil {
		t.Fatalf("failed to create default billing token: %v", err)
	}

	quotaBeforeDefault := getUserQuota(t, suite.Client, userA.ID)
	apiClientDefault := suite.Client.WithToken(tokenDefault)
	success, statusCode, errMsg := apiClientDefault.TryChatCompletion("gpt-4", "B-04 baseline default billing request")
	if !success {
		t.Fatalf("baseline default billing request failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterDefault := getUserQuota(t, suite.Client, userA.ID)
	usedDefault := quotaBeforeDefault - quotaAfterDefault
	if usedDefault <= 0 {
		t.Fatalf("expected used quota for default baseline > 0, got %d", usedDefault)
	}
	t.Logf("Baseline used quota with default billing: %d", usedDefault)

	// Baseline: billing at vip rate
	tokenVip, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b04-token-vip",
		Status:         1,
		UnlimitedQuota: true,
		Group:          `["vip"]`,
	})
	if err != nil {
		t.Fatalf("failed to create vip billing token: %v", err)
	}

	quotaBeforeVip := getUserQuota(t, suite.Client, userA.ID)
	apiClientVip := suite.Client.WithToken(tokenVip)
	success, statusCode, errMsg = apiClientVip.TryChatCompletion("gpt-4", "B-04 baseline vip billing request")
	if !success {
		t.Fatalf("baseline vip billing request failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterVip := getUserQuota(t, suite.Client, userA.ID)
	usedVip := quotaBeforeVip - quotaAfterVip
	if usedVip <= 0 {
		t.Fatalf("expected used quota for vip baseline > 0, got %d", usedVip)
	}
	t.Logf("Baseline used quota with vip billing: %d", usedVip)

	// Now, Token with group=["svip", "default"], but no svip channels exist.
	// The system should failover to "default" and bill as default rate.
	tokenFailover, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b04-token-svip-default",
		Status:         1,
		UnlimitedQuota: true,
		Group:          `["svip","default"]`,
	})
	if err != nil {
		t.Fatalf("failed to create failover token: %v", err)
	}

	quotaBeforeFailover := getUserQuota(t, suite.Client, userA.ID)
	apiClientFailover := suite.Client.WithToken(tokenFailover)
	success, statusCode, errMsg = apiClientFailover.TryChatCompletion("gpt-4", "B-04 failover billing request")
	if !success {
		t.Fatalf("failover request failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterFailover := getUserQuota(t, suite.Client, userA.ID)
	usedFailover := quotaBeforeFailover - quotaAfterFailover
	if usedFailover <= 0 {
		t.Fatalf("expected used quota for failover > 0, got %d", usedFailover)
	}
	t.Logf("Used quota with token group [\"svip\",\"default\"]: %d", usedFailover)

	// Failover billing should match default-rate billing, not vip-rate billing
	ratioToDefault := float64(usedFailover) / float64(usedDefault)
	ratioToVip := float64(usedFailover) / float64(usedVip)
	approxEqualRatio(t, ratioToDefault, 1.0, 0.05, "B-04: failover billing should match default rate")

	// And clearly should NOT match vip rate
	if math.Abs(ratioToVip-1.0) < 0.2 {
		t.Fatalf("B-04: failover billing unexpectedly close to vip rate: ratioToVip=%.4f", ratioToVip)
	}
}

// TestBilling_B05_AntiDowngradeProtection tests anti-downgrade billing protection.
// Scenario: User A (vip, rate=2.0) uses Token with group="default" (rate=1.0),
// but system has can_downgrade=false.
// Expected: Billing should use User A's higher rate (2.0).
func TestBilling_B05_AntiDowngradeProtection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// First ensure downgrade is allowed (can_downgrade=true) and measure baseline.
	if err := updateOption(suite.Client, "group_ratio_setting.can_downgrade", "true"); err != nil {
		t.Fatalf("failed to set can_downgrade=true: %v", err)
	}

	userA, err := fixtures.CreateTestUser("b05_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}

	userAClient := suite.Client.Clone()
	if _, err := userAClient.Login("b05_userA", "password123"); err != nil {
		t.Fatalf("failed to login user A: %v", err)
	}

	// Create default-group channel
	channelDefault, err := fixtures.CreateTestChannel(
		"b05-default-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create default channel: %v", err)
	}
	t.Logf("Created default channel ID=%d", channelDefault.ID)

	// Baseline: with can_downgrade=true, token group=["default"] should actually downgrade billing.
	tokenDefault, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b05-token-default-downgrade",
		Status:         1,
		UnlimitedQuota: true,
		Group:          `["default"]`,
	})
	if err != nil {
		t.Fatalf("failed to create token for downgrade baseline: %v", err)
	}

	quotaBeforeAllow := getUserQuota(t, suite.Client, userA.ID)
	apiClientAllow := suite.Client.WithToken(tokenDefault)
	success, statusCode, errMsg := apiClientAllow.TryChatCompletion("gpt-4", "B-05 baseline downgrade request")
	if !success {
		t.Fatalf("baseline downgrade request failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterAllow := getUserQuota(t, suite.Client, userA.ID)
	usedAllow := quotaBeforeAllow - quotaAfterAllow
	if usedAllow <= 0 {
		t.Fatalf("expected used quota with can_downgrade=true > 0, got %d", usedAllow)
	}
	t.Logf("Used quota with can_downgrade=true (billing at default rate): %d", usedAllow)

	// Now enable anti-downgrade protection
	if err := updateOption(suite.Client, "group_ratio_setting.can_downgrade", "false"); err != nil {
		t.Fatalf("failed to set can_downgrade=false: %v", err)
	}

	// Create another token with the same billing group override (["default"])
	tokenNoDowngrade, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b05-token-default-no-downgrade",
		Status:         1,
		UnlimitedQuota: true,
		Group:          `["default"]`,
	})
	if err != nil {
		t.Fatalf("failed to create token for no-downgrade scenario: %v", err)
	}

	quotaBeforeNoDowngrade := getUserQuota(t, suite.Client, userA.ID)
	apiClientNoDowngrade := suite.Client.WithToken(tokenNoDowngrade)
	success, statusCode, errMsg = apiClientNoDowngrade.TryChatCompletion("gpt-4", "B-05 anti-downgrade request")
	if !success {
		t.Fatalf("anti-downgrade request failed: status=%d, err=%s", statusCode, errMsg)
	}
	quotaAfterNoDowngrade := getUserQuota(t, suite.Client, userA.ID)
	usedNoDowngrade := quotaBeforeNoDowngrade - quotaAfterNoDowngrade
	if usedNoDowngrade <= 0 {
		t.Fatalf("expected used quota with can_downgrade=false > 0, got %d", usedNoDowngrade)
	}
	t.Logf("Used quota with can_downgrade=false (should bill at vip rate): %d", usedNoDowngrade)

	// With anti-downgrade protection, billing should be at vip rate (2x default).
	actualRatio := float64(usedNoDowngrade) / float64(usedAllow)
	approxEqualRatio(t, actualRatio, 2.0, 0.05, "B-05: anti-downgrade protection did not restore vip rate")
}

// TestBilling_B06_P2PSharingRevenue tests P2P sharing revenue calculation.
// Scenario: User B consumes 1000 quota using User A's shared channel.
// Expected: B is charged 1000, A receives share_quota based on ShareRatio.
func TestBilling_B06_P2PSharingRevenue(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Ensure share ratio is 0.5 for this test
	if err := updateOption(suite.Client, "p2p_setting.share_ratio", "0.5"); err != nil {
		t.Fatalf("failed to set ShareRatio=0.5: %v", err)
	}

	// Create User A (channel provider) and User B (consumer)
	userA, err := fixtures.CreateTestUser("b06_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}
	userB, err := fixtures.CreateTestUser("b06_userB", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	userAClient := suite.Client.Clone()
	if _, err := userAClient.Login("b06_userA", "password123"); err != nil {
		t.Fatalf("failed to login user A: %v", err)
	}
	userBClient := suite.Client.Clone()
	if _, err := userBClient.Login("b06_userB", "password123"); err != nil {
		t.Fatalf("failed to login user B: %v", err)
	}

	// Create a channel owned by A, in "default" group (public, no explicit P2P restriction).
	// Sharing revenue only depends on OwnerUserId and consumer id, not on P2P membership.
	channel, err := fixtures.CreateTestChannel(
		"b06-shared-channel",
		"gpt-4",
		"default",
		fixtures.GetUpstreamURL(),
		false,
		userA.ID,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create shared channel: %v", err)
	}
	t.Logf("Created shared channel ID=%d owned by A", channel.ID)

	// Token for user B to consume quota via A's channel
	tokenB, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "b06-token-B",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token for user B: %v", err)
	}

	// Record pre-state
	quotaBeforeB := getUserQuota(t, suite.Client, userB.ID)
	shareBeforeA := getUserShareQuota(t, suite.Client, userA.ID)

	// B makes a single request
	apiClientB := suite.Client.WithToken(tokenB)
	success, statusCode, errMsg := apiClientB.TryChatCompletion("gpt-4", "B-06 sharing revenue test request")
	if !success {
		t.Fatalf("B's consumption request failed: status=%d, err=%s", statusCode, errMsg)
	}

	quotaAfterB := getUserQuota(t, suite.Client, userB.ID)
	shareAfterA := getUserShareQuota(t, suite.Client, userA.ID)

	usedByB := quotaBeforeB - quotaAfterB
	shareDeltaA := shareAfterA - shareBeforeA

	if usedByB <= 0 {
		t.Fatalf("expected user B used quota > 0, got %d", usedByB)
	}
	if shareDeltaA <= 0 {
		t.Fatalf("expected user A share_quota increase > 0, got %d", shareDeltaA)
	}

	t.Logf("User B used quota: %d, User A share_quota increased: %d", usedByB, shareDeltaA)

	// With ShareRatio=0.5, we expect shareDeltaA ~= usedByB * 0.5
	expectedShare := float64(usedByB) * 0.5
	actualShare := float64(shareDeltaA)
	approxEqualRatio(t, actualShare, expectedShare, 0.1, "B-06: share quota for provider does not match ShareRatio")
}

// TestBilling_OrthogonalMatrix tests the orthogonal combinations of system group and P2P group.
// This implements the test matrix from section 2.3 of the test design document.
func TestBilling_OrthogonalMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	type orthCase struct {
		name                 string
		consumerGroup        string
		channelGroup         string
		useP2PAuthorization  bool
		consumerJoinsG1      bool
		consumerJoinsG2      bool
		tokenBillingOverride string
		tokenLimitToG2       bool
		expectSuccess        bool
		expectBillingGroup   string
	}

	testCases := []orthCase{
		// default user, vip channel, no P2P -> routing fails
		{
			name:          "default_user_vip_channel_no_p2p",
			consumerGroup: "default",
			channelGroup:  "vip",
			expectSuccess: false,
		},
		// vip user, vip channel, no P2P -> success, billed as vip
		{
			name:               "vip_user_vip_channel_no_p2p",
			consumerGroup:      "vip",
			channelGroup:       "vip",
			expectSuccess:      true,
			expectBillingGroup: "vip",
		},
		// default user, default channel, channel authorized G1, user in G1 -> success, billed as default
		{
			name:                "default_user_default_channel_with_p2p_member",
			consumerGroup:       "default",
			channelGroup:        "default",
			useP2PAuthorization: true,
			consumerJoinsG1:     true,
			expectSuccess:       true,
			expectBillingGroup:  "default",
		},
		// default user, vip channel, channel authorized G1, user in G1 -> fail (system group mismatch)
		{
			name:                "default_user_vip_channel_with_p2p_member",
			consumerGroup:       "default",
			channelGroup:        "vip",
			useP2PAuthorization: true,
			consumerJoinsG1:     true,
			expectSuccess:       false,
		},
		// vip user, default channel, no P2P auth, user joined G1 -> fail (channel not in vip group)
		{
			name:            "vip_user_default_channel_no_p2p_with_membership",
			consumerGroup:   "vip",
			channelGroup:    "default",
			consumerJoinsG1: true,
			expectSuccess:   false,
		},
		// vip user, default channel, channel authorized G1, user in G1 -> fail (system group mismatch)
		{
			name:                "vip_user_default_channel_with_p2p_member",
			consumerGroup:       "vip",
			channelGroup:        "default",
			useP2PAuthorization: true,
			consumerJoinsG1:     true,
			expectSuccess:       false,
		},
		// vip user with token billing override ["default"], default channel authorized G1, user in G1 -> success, billed as default
		{
			name:                 "vip_user_token_default_billing_with_p2p",
			consumerGroup:        "vip",
			channelGroup:         "default",
			useP2PAuthorization:  true,
			consumerJoinsG1:      true,
			tokenBillingOverride: `["default"]`,
			expectSuccess:        true,
			expectBillingGroup:   "default",
		},
		// default user, default channel authorized G1, user in G1 and G2, token limits P2P to G2 -> fail
		{
			name:                "default_user_token_limit_p2p_to_g2",
			consumerGroup:       "default",
			channelGroup:        "default",
			useP2PAuthorization: true,
			consumerJoinsG1:     true,
			consumerJoinsG2:     true,
			tokenLimitToG2:      true,
			expectSuccess:       false,
		},
	}

	for index, tc := range testCases {
		tc := tc
		caseIndex := index + 1
		t.Run(tc.name, func(t *testing.T) {
			suite, cleanup := SetupSuite(t)
			defer cleanup()

			fixtures := suite.Fixtures

			consumerUsername := fmt.Sprintf("orth_c_%02d", caseIndex)
			ownerUsername := fmt.Sprintf("orth_o_%02d", caseIndex)
			password := "password123"

			consumer, err := fixtures.CreateTestUser(consumerUsername, password, tc.consumerGroup)
			if err != nil {
				t.Fatalf("failed to create consumer user: %v", err)
			}
			owner, err := fixtures.CreateTestUser(ownerUsername, password, tc.channelGroup)
			if err != nil {
				t.Fatalf("failed to create owner user: %v", err)
			}

			consumerClient := suite.Client.Clone()
			if _, err := consumerClient.Login(consumerUsername, password); err != nil {
				t.Fatalf("failed to login consumer: %v", err)
			}
			ownerClient := suite.Client.Clone()
			if _, err := ownerClient.Login(ownerUsername, password); err != nil {
				t.Fatalf("failed to login owner: %v", err)
			}

			var g1ID, g2ID int

			// Create P2P groups and memberships if required
			if tc.useP2PAuthorization || tc.consumerJoinsG1 || tc.consumerJoinsG2 || tc.tokenLimitToG2 {
				if tc.useP2PAuthorization || tc.consumerJoinsG1 {
					g1ID, err = ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
						Name:       fmt.Sprintf("%s_G1", tc.name),
						OwnerId:    owner.ID,
						Type:       testutil.P2PGroupTypeShared,
						JoinMethod: testutil.P2PJoinMethodPassword,
						JoinKey:    "g1pass",
					})
					if err != nil {
						t.Fatalf("failed to create P2P group G1: %v", err)
					}
					if tc.consumerJoinsG1 {
						if err := consumerClient.ApplyToP2PGroup(g1ID, "g1pass"); err != nil {
							t.Fatalf("failed to add consumer to G1: %v", err)
						}
					}
				}

				if tc.consumerJoinsG2 || tc.tokenLimitToG2 {
					g2ID, err = ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
						Name:       fmt.Sprintf("%s_G2", tc.name),
						OwnerId:    owner.ID,
						Type:       testutil.P2PGroupTypeShared,
						JoinMethod: testutil.P2PJoinMethodPassword,
						JoinKey:    "g2pass",
					})
					if err != nil {
						t.Fatalf("failed to create P2P group G2: %v", err)
					}
					if tc.consumerJoinsG2 {
						if err := consumerClient.ApplyToP2PGroup(g2ID, "g2pass"); err != nil {
							t.Fatalf("failed to add consumer to G2: %v", err)
						}
					}
				}
			}

			var allowedGroups string
			if tc.useP2PAuthorization {
				if g1ID == 0 {
					t.Fatal("G1 must be created when useP2PAuthorization is true")
				}
				allowedGroups = fmt.Sprintf("%d", g1ID)
			}

			ownerUserID := 0
			if tc.useP2PAuthorization || tc.consumerJoinsG1 || tc.consumerJoinsG2 || tc.tokenLimitToG2 {
				ownerUserID = owner.ID
			}

			channelName := fmt.Sprintf("orth-%s-channel", tc.name)
			channel, err := fixtures.CreateTestChannel(
				channelName,
				"gpt-4",
				tc.channelGroup,
				fixtures.GetUpstreamURL(),
				false,
				ownerUserID,
				allowedGroups,
			)
			if err != nil {
				t.Fatalf("failed to create test channel: %v", err)
			}
			t.Logf("Created test channel ID=%d for case %s", channel.ID, tc.name)

			// Baseline quota for default group to infer effective billing ratios
			var baselineDefault int
			if tc.expectSuccess && tc.expectBillingGroup != "" {
				baselineDefault = measureBaselineQuotaForGroup(t, suite, "default")
			}

			tokenModel := &testutil.TokenModel{
				Name:           fmt.Sprintf("orth_token_%02d", caseIndex),
				Status:         1,
				UnlimitedQuota: true,
			}
			if tc.tokenBillingOverride != "" {
				tokenModel.Group = tc.tokenBillingOverride
			}
			if tc.tokenLimitToG2 {
				if g2ID == 0 {
					t.Fatal("G2 must be created when tokenLimitToG2 is true")
				}
				tokenModel.P2PGroupID = &g2ID
			}

			tokenKey, err := consumerClient.CreateTokenFull(tokenModel)
			if err != nil {
				t.Fatalf("failed to create consumer token: %v", err)
			}

			quotaBefore := getUserQuota(t, suite.Client, consumer.ID)
			apiClient := suite.Client.WithToken(tokenKey)
			success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", fmt.Sprintf("Orthogonal case %s", tc.name))
			quotaAfter := getUserQuota(t, suite.Client, consumer.ID)
			used := quotaBefore - quotaAfter

			if tc.expectSuccess {
				if !success {
					t.Fatalf("expected success but request failed: status=%d, err=%s", statusCode, errMsg)
				}
				if used <= 0 {
					t.Fatalf("expected quota usage > 0 for success case, got %d", used)
				}

				if tc.expectBillingGroup != "" {
					expectedRatio := 1.0
					if tc.expectBillingGroup == "vip" {
						expectedRatio = 2.0
					}
					actualRatio := float64(used) / float64(baselineDefault)
					approxEqualRatio(t, actualRatio, expectedRatio, 0.05,
						fmt.Sprintf("orthogonal case %s: billing ratio mismatch", tc.name))
				}
			} else {
				if success {
					t.Fatalf("expected routing failure but request succeeded, used_quota=%d", used)
				}
				if used != 0 {
					t.Fatalf("expected no quota usage on failed routing, got %d", used)
				}
				if statusCode != http.StatusServiceUnavailable && statusCode != http.StatusForbidden {
					t.Fatalf("expected 4xx/503 status for failed routing, got %d (err=%s)", statusCode, errMsg)
				}
			}
		})
	}
}

// TestBillingSkeleton is a placeholder test to verify the test file compiles.
func TestBillingSkeleton(t *testing.T) {
	t.Log("Billing test skeleton loaded successfully")
}
