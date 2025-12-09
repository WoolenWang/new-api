// Package dashboard contains integration tests for the management-plane
// dashboard / statistics APIs (section 2.9 of the Wquant integration test doc).
//
// Focus:
//   - DASH-01: 核心指标一致性
//   - Verify that aggregated consumption metrics from /api/log/stat
//     are consistent with user quota changes and underlying logs.
//   - Verify that P2P sharing revenue (share_quota) is recorded for
//     the channel owner according to ShareRatio, and that the
//     per-model log distribution reflects the new consumption.
package dashboard

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// TestSuite holds shared test resources for dashboard tests.
type TestSuite struct {
	Server   *testutil.TestServer
	Client   *testutil.APIClient
	Upstream *testutil.MockUpstreamServer
	Fixtures *testutil.TestFixtures
}

// SetupSuite starts a fresh NewAPI server instance configured with the
// billing / share settings used in the Wquant integration tests.
func SetupSuite(t *testing.T) (*TestSuite, func()) {
	t.Helper()

	// Start mock upstream first so we can wire channels to it.
	upstream := testutil.NewMockUpstreamServer()
	t.Logf("Mock upstream started at: %s", upstream.BaseURL)

	projectRoot, err := testutil.FindProjectRoot()
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

	fixtures := testutil.NewTestFixtures(t, client)
	fixtures.SetUpstream(upstream)

	// Configure billing-related options (group ratios, share ratio, etc.).
	if err := configureBillingEnvironmentForDashboard(t, client); err != nil {
		upstream.Close()
		_ = server.Stop()
		t.Fatalf("Failed to configure billing environment for dashboard tests: %v", err)
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

// configureBillingEnvironmentForDashboard mirrors the billing test setup:
//   - GroupRatio: default=1.0, vip=2.0, svip=0.8
//   - GroupGroupRatio: svip -> default = 0.5
//   - UserUsableGroups: default, vip, svip, auto
//   - p2p_setting.share_ratio: 0.5
//   - group_ratio_setting.can_downgrade: true
func configureBillingEnvironmentForDashboard(t *testing.T, client *testutil.APIClient) error {
	t.Helper()

	// 1. Basic group ratios.
	groupRatio := map[string]float64{
		"default": 1.0,
		"vip":     2.0,
		"svip":    0.8,
	}
	grBytes, err := json.Marshal(groupRatio)
	if err != nil {
		return fmt.Errorf("failed to marshal GroupRatio: %w", err)
	}
	if err := updateOptionDashboard(client, "GroupRatio", string(grBytes)); err != nil {
		return fmt.Errorf("failed to update GroupRatio: %w", err)
	}

	// 2. Special group-group ratio: svip using default -> 0.5.
	groupGroupRatio := map[string]map[string]float64{
		"svip": {
			"default": 0.5,
		},
	}
	ggrBytes, err := json.Marshal(groupGroupRatio)
	if err != nil {
		return fmt.Errorf("failed to marshal GroupGroupRatio: %w", err)
	}
	if err := updateOptionDashboard(client, "GroupGroupRatio", string(ggrBytes)); err != nil {
		return fmt.Errorf("failed to update GroupGroupRatio: %w", err)
	}

	// 3. User-usable groups.
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
	if err := updateOptionDashboard(client, "UserUsableGroups", string(uugBytes)); err != nil {
		return fmt.Errorf("failed to update UserUsableGroups: %w", err)
	}

	// 4. Global P2P share ratio.
	if err := updateOptionDashboard(client, "p2p_setting.share_ratio", "0.5"); err != nil {
		return fmt.Errorf("failed to update p2p_setting.share_ratio: %w", err)
	}

	// 5. Enable downgrade by default.
	if err := updateOptionDashboard(client, "group_ratio_setting.can_downgrade", "true"); err != nil {
		return fmt.Errorf("failed to set group_ratio_setting.can_downgrade=true: %w", err)
	}

	return nil
}

// updateOptionDashboard is a small helper to call /api/option.
func updateOptionDashboard(client *testutil.APIClient, key, value string) error {
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

// getUserQuotaDashboard retrieves the current quota for a given user ID.
func getUserQuotaDashboard(t *testing.T, client *testutil.APIClient, userID int) int {
	t.Helper()
	user, err := client.GetUser(userID)
	if err != nil {
		t.Fatalf("failed to get user %d: %v", userID, err)
	}
	return int(user.Quota)
}

// getUserShareQuotaDashboard retrieves the share_quota for a given user ID.
func getUserShareQuotaDashboard(t *testing.T, client *testutil.APIClient, userID int) int {
	t.Helper()
	user, err := client.GetUser(userID)
	if err != nil {
		t.Fatalf("failed to get user %d: %v", userID, err)
	}
	return int(user.ShareQuota)
}

// getConsumeStatForUser calls /api/log/stat and returns the aggregated quota
// for the given username over all time.
func getConsumeStatForUser(t *testing.T, client *testutil.APIClient, username string) int {
	t.Helper()

	now := time.Now().Unix()
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Quota int `json:"quota"`
			Rpm   int `json:"rpm"`
			Tpm   int `json:"tpm"`
		} `json:"data"`
	}

	// Note: SumUsedQuota internally filters type=LogTypeConsume; the "type"
	// query parameter is kept for forward-compatibility but not relied upon.
	path := fmt.Sprintf("/api/log/stat?type=2&username=%s&start_timestamp=0&end_timestamp=%d",
		username, now)
	if err := client.GetJSON(path, &resp); err != nil {
		t.Fatalf("failed to call /api/log/stat: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/log/stat failed: %s", resp.Message)
	}
	return resp.Data.Quota
}

// getModelQuotaFromLogs returns the total quota and log count for a given
// username and model, using /api/log/ (admin log list API).
func getModelQuotaFromLogs(t *testing.T, client *testutil.APIClient, username, modelName string) (totalQuota int, count int) {
	t.Helper()

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			Total    int `json:"total"`
			Items    []struct {
				Quota     int    `json:"quota"`
				ModelName string `json:"model_name"`
			} `json:"items"`
		} `json:"data"`
	}

	path := fmt.Sprintf("/api/log/?p=1&page_size=100&type=2&username=%s&model_name=%s",
		username, modelName)
	if err := client.GetJSON(path, &resp); err != nil {
		t.Fatalf("failed to call /api/log/: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/log/ failed: %s", resp.Message)
	}

	for _, item := range resp.Data.Items {
		totalQuota += item.Quota
	}
	return totalQuota, len(resp.Data.Items)
}

// approxEqual checks actual ~= expected within a relative tolerance.
func approxEqual(t *testing.T, actual, expected float64, tolerance float64, msg string) {
	t.Helper()
	if expected == 0 {
		t.Fatalf("expected value is zero in approxEqual: %s", msg)
	}
	diff := math.Abs(actual-expected) / expected
	if diff > tolerance {
		t.Fatalf("%s: actual=%.4f, expected=%.4f, diff=%.4f > tolerance=%.4f",
			msg, actual, expected, diff, tolerance)
	}
}

// TestDashboard_DASH01_CoreMetricsConsistency implements DASH-01:
//  1. Capture initial dashboard metrics D1 for consumption and sharing revenue.
//  2. Execute a single consumption operation via a shared channel.
//  3. Capture dashboard metrics D2 and assert that:
//     - D2.总消耗 = D1.总消耗 + usedByB
//     - D2.总收益 ~= D1.总收益 + usedByB * ShareRatio
//     - Per-model usage for the model increases by usedByB.
func TestDashboard_DASH01_CoreMetricsConsistency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration dashboard test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Create provider (User A) and consumer (User B).
	userA, err := fixtures.CreateTestUser("dash_userA", "password123", "vip")
	if err != nil {
		t.Fatalf("failed to create provider user A: %v", err)
	}
	userB, err := fixtures.CreateTestUser("dash_userB", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create consumer user B: %v", err)
	}

	userAClient := suite.Client.Clone()
	if _, err := userAClient.Login("dash_userA", "password123"); err != nil {
		t.Fatalf("failed to login user A: %v", err)
	}
	userBClient := suite.Client.Clone()
	if _, err := userBClient.Login("dash_userB", "password123"); err != nil {
		t.Fatalf("failed to login user B: %v", err)
	}

	// Shared channel owned by A (OwnerUserId != 0) in "default" group.
	channel, err := fixtures.CreateTestChannel(
		"dash-shared-channel",
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

	// Token for B to consume via A's channel.
	tokenB, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "dash-token-B",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token for user B: %v", err)
	}

	// D1: initial dashboard metrics.
	consumeBefore := getConsumeStatForUser(t, suite.Client, "dash_userB")
	shareBefore := getUserShareQuotaDashboard(t, suite.Client, userA.ID)
	modelQuotaBefore, _ := getModelQuotaFromLogs(t, suite.Client, "dash_userB", "gpt-4")

	quotaBeforeB := getUserQuotaDashboard(t, suite.Client, userB.ID)

	// Step 2: B makes a single consumption request.
	apiClientB := suite.Client.WithToken(tokenB)
	success, statusCode, errMsg := apiClientB.TryChatCompletion("gpt-4", "DASH-01 sharing revenue test request")
	if !success {
		t.Fatalf("consumer request failed: status=%d, err=%s", statusCode, errMsg)
	}

	// Compute actual consumption and sharing deltas from authoritative sources.
	quotaAfterB := getUserQuotaDashboard(t, suite.Client, userB.ID)
	usedByB := quotaBeforeB - quotaAfterB
	if usedByB <= 0 {
		t.Fatalf("expected user B used quota > 0, got %d", usedByB)
	}

	shareAfter := getUserShareQuotaDashboard(t, suite.Client, userA.ID)
	shareDelta := shareAfter - shareBefore
	if shareDelta <= 0 {
		t.Fatalf("expected user A share_quota increase > 0, got %d", shareDelta)
	}
	t.Logf("User B used quota: %d, User A share_quota increased: %d", usedByB, shareDelta)

	// D2: dashboard metrics after consumption.
	consumeAfter := getConsumeStatForUser(t, suite.Client, "dash_userB")
	modelQuotaAfter, modelCountAfter := getModelQuotaFromLogs(t, suite.Client, "dash_userB", "gpt-4")

	// Assertions:
	// a) Aggregated consumption equals initial + usedByB.
	if consumeAfter-consumeBefore != usedByB {
		t.Fatalf("dashboard consumption mismatch: before=%d after=%d used=%d (delta=%d)",
			consumeBefore, consumeAfter, usedByB, consumeAfter-consumeBefore)
	}

	// b) Sharing revenue follows configured ShareRatio (0.5).
	expectedShare := float64(usedByB) * 0.5
	approxEqual(t, float64(shareDelta), expectedShare, 0.1,
		"DASH-01: share_quota for provider does not match ShareRatio")

	// c) Per-model usage for gpt-4 increased by usedByB and at least one log exists.
	if modelCountAfter == 0 {
		t.Fatalf("expected at least one log entry for model gpt-4 after consumption")
	}
	if modelQuotaAfter-modelQuotaBefore != usedByB {
		t.Fatalf("model-level quota mismatch: before=%d after=%d used=%d (delta=%d)",
			modelQuotaBefore, modelQuotaAfter, usedByB, modelQuotaAfter-modelQuotaBefore)
	}

	t.Logf("DASH-01: dashboard metrics are consistent with logs and share_quota; used=%d share_delta=%d", usedByB, shareDelta)
}
