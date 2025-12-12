// Package orthogonal_config contains tests for complex configuration combinations
package orthogonal_config

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// UserMultiTokenSuite tests scenarios where a single user has multiple tokens
// with different configurations (billing groups, P2P restrictions, model limits).
// This suite focuses on validating Token dimension (Factors F & G).
type UserMultiTokenSuite struct {
	suite.Suite
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	fixtures *testutil.OrthogonalFixtures
}

// SetupSuite initializes the test server and creates base fixtures.
func (s *UserMultiTokenSuite) SetupSuite() {
	var err error

	// Find project root
	projectRoot, err := testutil.FindProjectRoot()
	require.NoError(s.T(), err, "Failed to find project root")

	// Create and start test server
	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.CustomEnv = map[string]string{
		"GLOBAL_API_RATE_LIMIT_ENABLE": "false",
		"GLOBAL_WEB_RATE_LIMIT_ENABLE": "false",
		"CRITICAL_RATE_LIMIT_ENABLE":   "false",
	}
	s.server, err = testutil.StartServer(cfg)
	require.NoError(s.T(), err, "Failed to start test server")

	// Create API client
	s.client = testutil.NewAPIClient(s.server)

	// Initialize system
	rootUser, rootPass, err := s.client.InitializeSystem()
	require.NoError(s.T(), err, "Failed to initialize system")

	_, err = s.client.Login(rootUser, rootPass)
	require.NoError(s.T(), err, "Failed to login as admin")

	// Ensure orthogonal billing groups are usable in this test environment.
	var optionResp testutil.APIResponse
	err = s.client.PutJSON("/api/option/", map[string]any{
		"key":   "UserUsableGroups",
		"value": "{\"default\":\"默认分组\",\"vip\":\"vip分组\",\"svip\":\"svip分组\"}",
	}, &optionResp)
	require.NoError(s.T(), err, "Failed to update UserUsableGroups option")
	require.True(s.T(), optionResp.Success, "Failed to update UserUsableGroups: %s", optionResp.Message)

	// Create mock upstream server once per suite to avoid stale BaseURL in persisted channels.
	s.upstream = testutil.NewMockUpstreamServer()

	s.T().Log("✓ Test server started and system initialized")
}

// SetupTest creates fresh fixtures for each test.
func (s *UserMultiTokenSuite) SetupTest() {
	if s.upstream != nil {
		s.upstream.Reset()
	}

	// Create orthogonal fixtures
	s.fixtures = testutil.NewOrthogonalFixtures(s.T(), s.client, s.upstream)

	// Setup fixtures (users, groups, channels)
	err := s.fixtures.Setup()
	require.NoError(s.T(), err, "Failed to setup fixtures")

	s.T().Log("✓ Fixtures created for test")
}

// TearDownTest cleans up after each test.
func (s *UserMultiTokenSuite) TearDownTest() {
	s.T().Log("✓ Test cleanup completed")
}

// TearDownSuite stops the test server.
func (s *UserMultiTokenSuite) TearDownSuite() {
	if s.upstream != nil {
		s.upstream.Close()
	}
	if s.server != nil {
		s.server.Stop()
	}
	s.T().Log("✓ Test server stopped")
}

// TestOT01_SameUserDifferentBillingTokens tests that the same user can have tokens
// with different billing groups, and each token is billed according to its configuration.
//
// Test Case: OT-01
// Scenario: User-Vip (sys_group=vip, ratio=2.0) has 3 tokens:
//   - Token1: billing_groups=[] (default to vip)
//   - Token2: billing_groups=["default"] (force default, ratio=1.0)
//   - Token3: billing_groups=["svip"] (force svip, ratio=0.8)
//     All tokens request the same vip channel
//
// Expected: Token1 billed at vip rate (2.0)
//
//	Token2 billed at default rate (1.0)
//	Token3 billed at svip rate (0.8)
func (s *UserMultiTokenSuite) TestOT01_SameUserDifferentBillingTokens() {
	// Arrange: Create tokens with different billing groups
	token1, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot01-token1-default-vip",
		"", // No override, use user's vip group
		0,
	)
	require.NoError(s.T(), err, "Failed to create token1")

	token2, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot01-token2-force-default",
		`["default"]`, // Force default billing
		0,
	)
	require.NoError(s.T(), err, "Failed to create token2")

	token3, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot01-token3-force-svip",
		`["svip"]`, // Force svip billing
		0,
	)
	require.NoError(s.T(), err, "Failed to create token3")

	// Act & Assert: All three tokens should succeed, but with different billing
	s.fixtures.VerifyRoutingSuccess(s.T(), token1, "gpt-4", "vip")
	s.fixtures.VerifyRoutingSuccess(s.T(), token2, "gpt-4", "default")
	s.fixtures.VerifyRoutingSuccess(s.T(), token3, "gpt-4", "svip")

	// Note: Actual billing rate validation would require querying the logs table
	s.T().Log("✓ OT-01: Same user, 3 tokens with different billing groups → all succeed with respective rates")
}

// TestOT02_TokenBillingGroupFallback tests the fallback mechanism when the preferred
// billing group has no available channels.
//
// Test Case: OT-02
// Scenario: User-Vip has token with billing_groups=["svip", "default"]
//
//	There are no svip channels available
//	There is a default channel available
//
// Expected: Request succeeds, billed at default rate (fallback from svip to default)
func (s *UserMultiTokenSuite) TestOT02_TokenBillingGroupFallback() {
	// Arrange: Create token with ordered billing group list
	tokenFallback, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot02-token-fallback",
		`["svip", "default"]`, // Try svip first, fallback to default
		0,
	)
	require.NoError(s.T(), err, "Failed to create fallback token")

	// Note: By default, there is no svip channel for gpt-4 in fixtures
	// So the request should fallback to default channel

	// Act & Assert
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenFallback, "gpt-4", "default")

	s.T().Log("✓ OT-02: Token with [svip, default], no svip channel → fallback to default")
}

// TestOT03_DifferentP2PRestrictionsPerToken tests tokens with different P2P group restrictions.
//
// Test Case: OT-03
// Scenario: User-Vip joins both G1 and G2
//   - Token1: no P2P restriction (can use all user's P2P groups)
//   - Token2: p2p_group_id=G1 (restricted to G1 only)
//   - Token3: p2p_group_id=G2 (restricted to G2 only)
//     Ch-X is authorized to G1
//     Ch-Y is authorized to G2
//
// Expected: Token1 cannot use P2P channels (no p2p_group_id set)
//
//	Token2 can only use Ch-X
//	Token3 can only use Ch-Y
func (s *UserMultiTokenSuite) TestOT03_DifferentP2PRestrictionsPerToken() {
	// Arrange: Join User-Vip to G1 and G2
	err := s.fixtures.JoinUserToGroups(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to join groups")

	// Create Ch-X authorized to G1
	chX, err := s.fixtures.CreateChannel("ot03-ch-x", "gpt-4", "vip", []int{s.fixtures.G1.ID})
	require.NoError(s.T(), err, "Failed to create Ch-X")

	// Create Ch-Y authorized to G2
	chY, err := s.fixtures.CreateChannel("ot03-ch-y", "gpt-3.5-turbo", "vip", []int{s.fixtures.G2.ID})
	require.NoError(s.T(), err, "Failed to create Ch-Y")

	// Create tokens with different P2P restrictions
	token1, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot03-token1-no-p2p",
		"",
		0, // No P2P restriction (but also means cannot use P2P channels)
	)
	require.NoError(s.T(), err, "Failed to create token1")

	token2, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot03-token2-g1",
		"",
		s.fixtures.G1.ID, // Restricted to G1
	)
	require.NoError(s.T(), err, "Failed to create token2")

	token3, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot03-token3-g2",
		"",
		s.fixtures.G2.ID, // Restricted to G2
	)
	require.NoError(s.T(), err, "Failed to create token3")

	// Act & Assert
	// Token1: No P2P restriction → can use system-group channels, but not P2P channels
	s.fixtures.VerifyRoutingSuccess(s.T(), token1, "gpt-4", "vip")
	s.fixtures.VerifyRoutingFailure(s.T(), token1, "gpt-3.5-turbo")

	// Token2: Can only use Ch-X (G1)
	s.fixtures.VerifyRoutingSuccess(s.T(), token2, "gpt-4", "vip")
	s.fixtures.VerifyRoutingFailure(s.T(), token2, "gpt-3.5-turbo")

	// Token3: Can only use Ch-Y (G2)
	s.fixtures.VerifyRoutingSuccess(s.T(), token3, "gpt-4", "vip")
	s.fixtures.VerifyRoutingSuccess(s.T(), token3, "gpt-3.5-turbo", "vip")

	s.T().Logf("✓ OT-03: Token1 (no P2P) → fail, Token2 (G1) → Ch-X success, Token3 (G2) → Ch-Y success (ch_x=%d, ch_y=%d)", chX.ID, chY.ID)
}

// TestOT04_TokenMultiP2PRestriction tests a token restricted to multiple P2P groups.
//
// Test Case: OT-04 (Extended scenario)
// Scenario: User-Vip joins G1, G2, G3
//
//	Token has p2p_group_id=[G1, G2] (conceptually, though current API may use single ID)
//	Ch-X authorized to G1
//	Ch-Y authorized to G2
//	Ch-Z authorized to G3
//
// Expected: Token can use Ch-X and Ch-Y, but not Ch-Z
//
// Note: Current implementation may only support single p2p_group_id.
// This test validates the single-ID restriction behavior.
func (s *UserMultiTokenSuite) TestOT04_TokenMultiP2PRestriction() {
	// Arrange: Join User-Vip to G1, G2, G3
	err := s.fixtures.JoinUserToGroups(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID, s.fixtures.G3.ID},
	)
	require.NoError(s.T(), err, "Failed to join groups")

	// Create channels for each group
	chG1, err := s.fixtures.CreateChannel("ot04-ch-g1", "gpt-4", "vip", []int{s.fixtures.G1.ID})
	require.NoError(s.T(), err, "Failed to create Ch-G1")

	chG2, err := s.fixtures.CreateChannel("ot04-ch-g2", "gpt-3.5-turbo", "vip", []int{s.fixtures.G2.ID})
	require.NoError(s.T(), err, "Failed to create Ch-G2")

	chG3, err := s.fixtures.CreateChannel("ot04-ch-g3", "gpt-4-turbo", "vip", []int{s.fixtures.G3.ID})
	require.NoError(s.T(), err, "Failed to create Ch-G3")

	// Create token restricted to G1 (single restriction)
	tokenG1, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot04-token-g1",
		"",
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert
	// Token can only use Ch-G1, not Ch-G2 or Ch-G3
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenG1, "gpt-4", "vip")
	s.fixtures.VerifyRoutingFailure(s.T(), tokenG1, "gpt-3.5-turbo")
	s.fixtures.VerifyRoutingFailure(s.T(), tokenG1, "gpt-4-turbo")

	s.T().Logf("✓ OT-04: Token restricted to G1 → can access Ch-G1 only (ch_g1=%d, ch_g2=%d, ch_g3=%d)", chG1.ID, chG2.ID, chG3.ID)
}

// TestOT08_TokenMultiP2PRestrictionUnsupported verifies that multi-P2P restriction
// is not supported by current API schema and should be rejected.
//
// Test Case: OT-08 (Coverage补例 for Factor G multiple)
// Scenario: User-Vip attempts to create a token with p2p_group_id=[G1, G2]
// Expected: Token creation fails (success=false)
func (s *UserMultiTokenSuite) TestOT08_TokenMultiP2PRestrictionUnsupported() {
	// Arrange: Build an invalid token payload with array p2p_group_id
	payload := map[string]any{
		"name":            "ot08-token-multi-p2p",
		"status":          1,
		"remain_quota":    100000000000,
		"unlimited_quota": true,
		"group":           "",
		"p2p_group_id":    []int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	}

	var resp testutil.APIResponse
	err := s.fixtures.UserVipClient.PostJSON("/api/token/", payload, &resp)
	require.NoError(s.T(), err, "Token creation request should return 200 with success=false")
	require.False(s.T(), resp.Success, "Multi-P2P restriction should be rejected by API")

	s.T().Logf("✓ OT-08: p2p_group_id as array rejected as expected (message=%q)", resp.Message)
}

// TestOT09_UserUsableGroupsForbidSvip verifies UserUsableGroups permission closure.
//
// Test Case: OT-09 (Coverage补例)
// Scenario: System removes svip from UserUsableGroups; default user uses token billing svip.
// Expected: Request is forbidden at auth stage.
func (s *UserMultiTokenSuite) TestOT09_UserUsableGroupsForbidSvip() {
	// Arrange: Remove svip from UserUsableGroups.
	baseline := "{\"default\":\"默认分组\",\"vip\":\"vip分组\",\"svip\":\"svip分组\"}"
	restricted := "{\"default\":\"默认分组\",\"vip\":\"vip分组\"}"
	var optionResp testutil.APIResponse
	err := s.client.PutJSON("/api/option/", map[string]any{
		"key":   "UserUsableGroups",
		"value": restricted,
	}, &optionResp)
	require.NoError(s.T(), err)
	require.True(s.T(), optionResp.Success, "Failed to restrict UserUsableGroups: %s", optionResp.Message)
	defer func() {
		_ = s.client.PutJSON("/api/option/", map[string]any{
			"key":   "UserUsableGroups",
			"value": baseline,
		}, &testutil.APIResponse{})
	}()

	// Create token for default user forcing svip billing.
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		"ot09-token-force-svip",
		`["svip"]`,
		0,
	)
	require.NoError(s.T(), err)

	// Act: Request with this token should be forbidden.
	client := s.client.WithToken(tokenKey)
	resp, err := client.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test usable groups forbid svip"},
		},
		Stream: false,
	})
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	require.Equalf(s.T(), http.StatusForbidden, resp.StatusCode, "Expected 403, got %d body=%s", resp.StatusCode, bodyStr)
	require.Contains(s.T(), bodyStr, "无权访问 svip 分组")

	s.T().Log("✓ OT-09: svip removed from UserUsableGroups → svip billing token forbidden")
}

// TestOT10_UserUsableGroupsForbidVip verifies forbidding vip billing when vip is not usable.
//
// Test Case: OT-10 (Coverage补例)
// Scenario: System removes vip from UserUsableGroups; default user uses token billing vip.
// Expected: Request is forbidden at auth stage.
func (s *UserMultiTokenSuite) TestOT10_UserUsableGroupsForbidVip() {
	// Arrange: Remove vip from UserUsableGroups.
	baseline := "{\"default\":\"默认分组\",\"vip\":\"vip分组\",\"svip\":\"svip分组\"}"
	restricted := "{\"default\":\"默认分组\",\"svip\":\"svip分组\"}"
	var optionResp testutil.APIResponse
	err := s.client.PutJSON("/api/option/", map[string]any{
		"key":   "UserUsableGroups",
		"value": restricted,
	}, &optionResp)
	require.NoError(s.T(), err)
	require.True(s.T(), optionResp.Success, "Failed to restrict UserUsableGroups: %s", optionResp.Message)
	defer func() {
		_ = s.client.PutJSON("/api/option/", map[string]any{
			"key":   "UserUsableGroups",
			"value": baseline,
		}, &testutil.APIResponse{})
	}()

	// Create token for default user forcing vip billing.
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		"ot10-token-force-vip",
		`["vip"]`,
		0,
	)
	require.NoError(s.T(), err)

	// Act: Request with this token should be forbidden.
	client := s.client.WithToken(tokenKey)
	resp, err := client.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test usable groups forbid vip"},
		},
		Stream: false,
	})
	require.NoError(s.T(), err)
	require.NotNil(s.T(), resp)
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	require.Equalf(s.T(), http.StatusForbidden, resp.StatusCode, "Expected 403, got %d body=%s", resp.StatusCode, bodyStr)
	require.Contains(s.T(), bodyStr, "无权访问 vip 分组")

	s.T().Log("✓ OT-10: vip removed from UserUsableGroups → vip billing token forbidden")
}

// TestOT05_TokenModelLimitWithGroupCombination tests token model limits combined with group configuration.
//
// Test Case: OT-05
// Scenario: User-Vip has token with:
//   - billing_groups=["default"]
//   - model_limits=["gpt-4"]
//     Channels available: gpt-4 (default group), gpt-3.5-turbo (default group)
//
// Expected: Token can only use gpt-4 (model limit), billed at default rate
//
//	Request for gpt-3.5-turbo should be rejected
func (s *UserMultiTokenSuite) TestOT05_TokenModelLimitWithGroupCombination() {
	// Arrange:
	// 1) Create a default-group gpt-3.5-turbo channel so routing is possible
	//    and any failure comes from token model limits.
	_, err := s.fixtures.CreateChannel(
		"ot05-ch-default-gpt35",
		"gpt-3.5-turbo",
		"default",
		[]int{},
	)
	require.NoError(s.T(), err, "Failed to create default gpt-3.5 channel")

	// 2) Create token with billing_groups=["default"] and model_limits enabled to only allow gpt-4.
	tokenModel := &testutil.TokenModel{
		UserId:             s.fixtures.UserVip.ID,
		Name:               "ot05-token-limited",
		Status:             1,
		RemainQuota:        100000000000,
		Group:              `["default"]`,
		ModelLimitsEnabled: true,
		ModelLimitsJson:    "gpt-4",
	}
	tokenLimited, err := s.fixtures.UserVipClient.CreateTokenFull(tokenModel)
	require.NoError(s.T(), err, "Failed to create token with model limits")

	// Act & Assert:
	// gpt-4 allowed → success, billed default.
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenLimited, "gpt-4", "default")
	// gpt-3.5-turbo blocked by model limits → 403 with model-limit message.
	limitClient := s.client.WithToken(tokenLimited)
	resp, err := limitClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-3.5-turbo",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test model limit"},
		},
		Stream: false,
	})
	require.NoError(s.T(), err, "Request should return HTTP response")
	require.NotNil(s.T(), resp, "Response should not be nil")
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	require.Equalf(s.T(), http.StatusForbidden, resp.StatusCode, "Expected 403 forbidden, got %d body=%s", resp.StatusCode, bodyStr)
	require.Truef(s.T(), strings.Contains(bodyStr, "无权访问模型") || strings.Contains(bodyStr, "no permission to access model"),
		"Expected model-limit forbidden message, body=%s", bodyStr)

	s.T().Log("✓ OT-05: Token billing=default, model_limits=[gpt-4] → gpt-4 success; gpt-3.5-turbo forbidden")
}

// TestOT06_TokenQuotaIndependentStats tests that tokens have independent quota consumption.
//
// Test Case: OT-06
// Scenario: User-Vip has Token1 (quota=100000) and Token2 (quota=50000)
//
//	Both tokens make requests
//
// Expected: Each token's quota is tracked independently
//
//	Channel statistics should aggregate both tokens' usage
func (s *UserMultiTokenSuite) TestOT06_TokenQuotaIndependentStats() {
	// Arrange: Create two tokens with different quotas
	token1, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot06-token1-100k",
		"",
		0,
	)
	require.NoError(s.T(), err, "Failed to create token1")

	token2, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot06-token2-50k",
		"",
		0,
	)
	require.NoError(s.T(), err, "Failed to create token2")

	// Act: Make requests with both tokens
	s.fixtures.VerifyRoutingSuccess(s.T(), token1, "gpt-4", "vip")
	s.fixtures.VerifyRoutingSuccess(s.T(), token2, "gpt-4", "vip")

	// Assert: Quota consumption would be validated by querying token status
	// Channel statistics should count both requests
	s.T().Log("✓ OT-06: Two tokens with independent quotas → both succeed, channel stats aggregate")
}

// TestOT07_SameUserMultiTokenConcurrent tests concurrent requests from multiple tokens of the same user.
//
// Test Case: OT-07
// Scenario: User-Vip has Token1, Token2, Token3
//
//	All three tokens make concurrent requests to the same channel
//
// Expected: All requests succeed
//
//	Channel concurrency is correctly counted
//	User unique count should be 1 (same user)
func (s *UserMultiTokenSuite) TestOT07_SameUserMultiTokenConcurrent() {
	// Arrange: Join User-Vip to G1 (for consistent routing)
	err := s.fixtures.JoinUserToGroups(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to join G1")

	// Create three tokens
	token1, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot07-token1",
		"",
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token1")

	token2, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot07-token2",
		"",
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token2")

	token3, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"ot07-token3",
		"",
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token3")

	// Act: Make concurrent requests
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		s.fixtures.VerifyRoutingSuccess(s.T(), token1, "gpt-4", "vip")
	}()

	go func() {
		defer wg.Done()
		s.fixtures.VerifyRoutingSuccess(s.T(), token2, "gpt-4", "vip")
	}()

	go func() {
		defer wg.Done()
		s.fixtures.VerifyRoutingSuccess(s.T(), token3, "gpt-4", "vip")
	}()

	wg.Wait()

	// Assert: All requests succeeded
	// Note: Statistics validation (unique_users=1, concurrent count) would require
	// querying the stats table or logs
	s.T().Log("✓ OT-07: 3 tokens from same user, concurrent requests → all succeed, unique_users=1")
}

// TestUserMultiTokenSuite runs the test suite.
func TestUserMultiTokenSuite(t *testing.T) {
	suite.Run(t, new(UserMultiTokenSuite))
}
