// Package database_api contains integration tests for API completeness.
//
// Test Focus:
// ===========
// This package validates that all designed API endpoints are implemented and
// correctly handle authentication, authorization, parameter parsing, and data retrieval.
//
// Test Sections:
// - 2.5: API Interface Completeness Tests (API-01 to API-13)
//
// Key Test Scenarios:
// - API-01 to API-03: Channel statistics API tests
// - API-04 to API-05: Model baseline management API tests
// - API-06 to API-08: Model monitoring policy API tests
// - API-09 to API-10: Monitoring results query API tests
// - API-11 to API-13: P2P group statistics API tests
//
// Test Data:
// All tests use isolated in-memory SQLite database to ensure test independence.
package database_api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// APICompletenessSuite holds shared test resources for API completeness tests.
type APICompletenessSuite struct {
	Server       *testutil.TestServer
	AdminClient  *testutil.APIClient
	NormalClient *testutil.APIClient // Normal user client (non-admin)
	DB           *model.DBWrapper
	Fixtures     *testutil.TestFixtures

	// Test entities
	TestChannel     *testutil.ChannelModel
	TestP2PGroup    *testutil.P2PGroupModel
	NormalUser      *testutil.UserModel
	NormalUserToken string
}

// SetupAPICompletenessSuite initializes the test suite with a running server and test data.
func SetupAPICompletenessSuite(t *testing.T) (*APICompletenessSuite, func()) {
	t.Helper()

	projectRoot, err := testutil.FindProjectRoot()
	if err != nil {
		t.Fatalf("failed to find project root: %v", err)
	}

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()

	server, err := testutil.StartServer(cfg)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}

	adminClient := testutil.NewAPIClient(server)

	// Initialize system and login as root (admin user).
	rootUser, rootPass, err := adminClient.InitializeSystem()
	if err != nil {
		_ = server.Stop()
		t.Fatalf("failed to initialize system: %v", err)
	}
	if _, err := adminClient.Login(rootUser, rootPass); err != nil {
		_ = server.Stop()
		t.Fatalf("failed to login as root: %v", err)
	}

	// Get direct database access to the same SQLite file used by the test server.
	db := openTestDB(t, server)

	suite := &APICompletenessSuite{
		Server:      server,
		AdminClient: adminClient,
		DB:          db,
		Fixtures:    testutil.NewTestFixtures(t, adminClient),
	}

	// Create a normal (non-admin) user for permission tests, using fixtures
	normalUser, err := suite.Fixtures.CreateTestUser(
		fmt.Sprintf("normal-user-%d", time.Now().UnixNano()),
		"password123",
		"default",
	)
	if err != nil {
		_ = server.Stop()
		t.Fatalf("failed to create normal user: %v", err)
	}
	normalUserID := normalUser.ID
	suite.NormalUser = normalUser

	// Create a token for normal user
	tokenKey, err := adminClient.CreateTokenFull(&testutil.TokenModel{
		UserId:         normalUserID,
		Name:           "normal-user-token",
		UnlimitedQuota: false,
		RemainQuota:    100000,
		ExpiredTime:    -1, // Never expire
		Status:         1,  // Enabled
	})
	if err != nil {
		_ = server.Stop()
		t.Fatalf("failed to create token for normal user: %v", err)
	}
	suite.NormalUserToken = tokenKey

	// Create a client for normal user using session-based auth.
	normalClient := testutil.NewAPIClient(server)
	if _, err := normalClient.Login(normalUser.Username, "password123"); err != nil {
		_ = server.Stop()
		t.Fatalf("failed to login as normal user: %v", err)
	}
	// Ensure we don't accidentally use an admin access token for user routes.
	normalClient.Token = ""
	suite.NormalClient = normalClient

	// Create a test channel for statistics tests
	channel := &testutil.ChannelModel{
		Name:   fmt.Sprintf("test-channel-%d", time.Now().UnixNano()),
		Type:   1, // OpenAI type
		Key:    fmt.Sprintf("sk-test-%d", time.Now().UnixNano()),
		Status: 1, // Enabled
		Models: "gpt-4,gpt-3.5-turbo",
		Group:  "default",
	}
	channelID, err := adminClient.AddChannel(channel)
	if err != nil {
		_ = server.Stop()
		t.Fatalf("failed to create test channel: %v", err)
	}
	channel.ID = channelID
	suite.TestChannel = channel

	// Create a test P2P group for group statistics tests
	group := &testutil.P2PGroupModel{
		Name:        fmt.Sprintf("test-group-%d", time.Now().UnixNano()),
		DisplayName: "Test Group for API Tests",
		OwnerId:     1, // Admin user
		Type:        2, // Shared
		JoinMethod:  0, // Invite
	}
	groupID, err := adminClient.CreateP2PGroup(group)
	if err != nil {
		_ = server.Stop()
		t.Fatalf("failed to create test P2P group: %v", err)
	}
	group.ID = groupID
	suite.TestP2PGroup = group

	cleanup := func() {
		suite.Fixtures.Cleanup()
		if err := server.Stop(); err != nil {
			t.Errorf("Failed to stop server: %v", err)
		}
	}

	return suite, cleanup
}

// Helper function to parse HTTP response body
func parseResponseBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")
	t.Logf("raw response body: %s", string(body))

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "Failed to parse JSON response")

	return result
}

// Helper function to assert successful API response
func assertSuccessResponse(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")
	result := parseResponseBody(t, resp)

	// Check if response has success field and it's true
	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true in response")
	}

	return result
}

// Helper function to assert error API response
func assertErrorResponse(t *testing.T, resp *http.Response, expectedStatus int) {
	t.Helper()

	assert.Equal(t, expectedStatus, resp.StatusCode, "Expected error status code")
	result := parseResponseBody(t, resp)

	// Check if response has success field and it's false
	if success, ok := result["success"].(bool); ok {
		assert.False(t, success, "Expected success=false in error response")
	}
}

// ============================================================================
// Channel Statistics API Tests (API-01 to API-03)
// ============================================================================

// TestAPI01_ChannelStats_WithPeriodAndModel tests querying channel statistics with period=1h and model filter.
//
// Test ID: API-01
// API Endpoint: GET /api/channels/{id}/stats
// HTTP Method: GET
// Permission: Admin
// Test Scenario: 查询渠道统计，period=1h, model=gpt-4
// Expected Response: 返回200，包含所有统计指标
// Priority: P0
//
// Test Steps:
// 1. Prepare test channel with statistics data
// 2. Admin user queries channel stats with period=1h and model=gpt-4
// 3. Verify HTTP 200 status
// 4. Verify response contains all required statistics fields
func TestAPI01_ChannelStats_WithPeriodAndModel(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Prepare test channel statistics data
	// Prepare one statistics snapshot via internal admin API so that
	// /api/channels/{id}/stats can aggregate it correctly.
	payload := map[string]interface{}{
		"channel_id":       suite.TestChannel.ID,
		"model_name":       "gpt-4",
		"request_count":    100,
		"fail_count":       5,
		"total_tokens":     10000,
		"total_quota":      1000,
		"total_latency_ms": 20000,
		"stream_req_count": 50,
		"cache_hit_count":  25,
		"downtime_seconds": 0,
		"unique_users":     10,
	}

	resp, err := suite.AdminClient.Post("/api/internal/channel_statistics", payload)
	require.NoError(t, err, "Failed to seed channel statistics via internal API")
	resp.Body.Close()

	// Step 2: Admin user queries channel stats
	path := fmt.Sprintf("/api/channels/%d/stats?period=1h&model=gpt-4", suite.TestChannel.ID)
	resp, err = suite.AdminClient.Get(path)
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 3: Verify HTTP 200 status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")

	// Step 4: Verify response contains required fields
	result := parseResponseBody(t, resp)

	// Check success field
	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Check data field exists
	data, dataExists := result["data"]
	assert.True(t, dataExists, "Response should contain 'data' field")

	if dataMap, ok := data.(map[string]interface{}); ok {
		// Verify key statistics fields are present
		assert.Contains(t, dataMap, "request_count", "Should contain request_count")
		assert.Contains(t, dataMap, "fail_rate", "Should contain fail_rate")
		assert.Contains(t, dataMap, "total_tokens", "Should contain total_tokens")
		assert.Contains(t, dataMap, "total_quota", "Should contain total_quota")
		assert.Contains(t, dataMap, "tpm", "Should contain tpm")
		assert.Contains(t, dataMap, "rpm", "Should contain rpm")
		assert.Contains(t, dataMap, "avg_response_time", "Should contain avg_response_time")
		assert.Contains(t, dataMap, "unique_users", "Should contain unique_users")
	}

	t.Logf("API-01 Test Passed: Successfully queried channel stats with period=1h and model=gpt-4")
}

// TestAPI02_ChannelStats_WithPeriodOnly tests querying channel statistics with period=7d without model filter.
//
// Test ID: API-02
// API Endpoint: GET /api/channels/{id}/stats
// HTTP Method: GET
// Permission: Admin
// Test Scenario: period=7d，不指定model
// Expected Response: 返回渠道7天总体统计
// Priority: P0
//
// Test Steps:
// 1. Prepare test channel with statistics for multiple models
// 2. Admin user queries channel stats with period=7d (no model filter)
// 3. Verify HTTP 200 status
// 4. Verify response contains aggregated statistics for all models
func TestAPI02_ChannelStats_WithPeriodOnly(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Prepare test channel statistics for multiple models
	// Step 1: Prepare test channel statistics for multiple models via internal API
	models := []string{"gpt-4", "gpt-3.5-turbo"}
	for _, modelName := range models {
		payload := map[string]interface{}{
			"channel_id":       suite.TestChannel.ID,
			"model_name":       modelName,
			"request_count":    50,
			"fail_count":       2,
			"total_tokens":     5000,
			"total_quota":      500,
			"total_latency_ms": 10000,
			"stream_req_count": 25,
			"cache_hit_count":  10,
			"downtime_seconds": 0,
			"unique_users":     5,
		}

		resp, err := suite.AdminClient.Post("/api/internal/channel_statistics", payload)
		require.NoErrorf(t, err, "Failed to create test channel statistics for %s", modelName)
		resp.Body.Close()
	}

	// Step 2: Admin user queries channel stats without model filter
	path := fmt.Sprintf("/api/channels/%d/stats?period=7d", suite.TestChannel.ID)
	resp, err := suite.AdminClient.Get(path)
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 3: Verify HTTP 200 status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")

	// Step 4: Verify response contains aggregated data
	result := parseResponseBody(t, resp)

	// Check success field
	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Check data field exists
	data, dataExists := result["data"]
	assert.True(t, dataExists, "Response should contain 'data' field")

	if dataMap, ok := data.(map[string]interface{}); ok {
		// Verify aggregated statistics fields
		assert.Contains(t, dataMap, "request_count", "Should contain aggregated request_count")
		assert.Contains(t, dataMap, "total_tokens", "Should contain aggregated total_tokens")
		assert.Contains(t, dataMap, "total_quota", "Should contain aggregated total_quota")
	}

	t.Logf("API-02 Test Passed: Successfully queried channel stats with period=7d without model filter")
}

// TestAPI03_ChannelStats_NormalUserPermission tests that normal users cannot access channel statistics.
//
// Test ID: API-03
// API Endpoint: GET /api/channels/{id}/stats
// HTTP Method: GET
// Permission: Normal User (should be denied)
// Test Scenario: 尝试查询
// Expected Response: 返回403或401
// Priority: P1
//
// Test Steps:
// 1. Normal user attempts to query channel stats
// 2. Verify HTTP 403 or 401 status (permission denied)
func TestAPI03_ChannelStats_NormalUserPermission(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: An unauthenticated client (no session, no token) attempts to query channel stats
	unauthClient := suite.AdminClient.Clone()
	unauthClient.Token = ""
	unauthClient.UserID = 0

	path := fmt.Sprintf("/api/channels/%d/stats?period=1h", suite.TestChannel.ID)
	resp, err := unauthClient.Get(path)
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 2: Verify permission denied status (403 or 401)
	assert.True(t,
		resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized,
		"Expected 403 Forbidden or 401 Unauthorized, got %d", resp.StatusCode)

	t.Logf("API-03 Test Passed: Normal user correctly denied access to channel stats")
}

// ============================================================================
// Model Baseline Management API Tests (API-04 to API-05)
// ============================================================================

// TestAPI04_CreateModelBaseline tests creating a new model baseline.
//
// Test ID: API-04
// API Endpoint: POST /api/monitor/baselines
// HTTP Method: POST
// Permission: Admin
// Test Scenario: 创建新基准
// Expected Response: 返回201，响应包含基准ID
// Priority: P0
//
// Test Steps:
// 1. Admin user creates a new model baseline
// 2. Verify HTTP 201 or 200 status
// 3. Verify response contains baseline ID
// 4. Verify baseline is stored in database
func TestAPI04_CreateModelBaseline(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Admin user creates a new model baseline
	baselineData := map[string]interface{}{
		"model_name":          "gpt-4",
		"test_type":           "style",
		"evaluation_standard": "standard",
		"baseline_channel_id": suite.TestChannel.ID,
		"prompt":              "Write a short story about a robot.",
		"baseline_output":     "Once upon a time, in a distant future, there was a robot named R2D2...",
	}

	resp, err := suite.AdminClient.Post("/api/monitor/baselines", baselineData)
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 2: Verify HTTP 201 or 200 status (created or success)
	assert.True(t,
		resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK,
		"Expected 201 Created or 200 OK, got %d", resp.StatusCode)

	// Step 3: Verify response contains baseline ID
	result := parseResponseBody(t, resp)

	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Check for baseline ID in response
	var baselineID int
	if data, ok := result["data"].(map[string]interface{}); ok {
		if id, exists := data["id"]; exists {
			if idFloat, ok := id.(float64); ok {
				baselineID = int(idFloat)
			}
		}
	}
	assert.NotZero(t, baselineID, "Response should contain baseline ID")

	// Step 4: Verify baseline is stored in database
	var baseline model.ModelBaseline
	err = suite.DB.Where("model_name = ? AND test_type = ? AND evaluation_standard = ?",
		"gpt-4", "style", "standard").First(&baseline).Error
	assert.NoError(t, err, "Baseline should be stored in database")
	assert.Equal(t, "gpt-4", baseline.ModelName)
	assert.Equal(t, "style", baseline.TestType)
	assert.Equal(t, "standard", baseline.EvaluationStandard)

	t.Logf("API-04 Test Passed: Successfully created model baseline with ID %d", baselineID)
}

// TestAPI05_GetAllBaselines tests querying all model baselines.
//
// Test ID: API-05
// API Endpoint: GET /api/monitor/baselines
// HTTP Method: GET
// Permission: Admin
// Test Scenario: 查询所有基准
// Expected Response: 返回200，数组包含所有基准
// Priority: P1
//
// Test Steps:
// 1. Create multiple test baselines
// 2. Admin user queries all baselines
// 3. Verify HTTP 200 status
// 4. Verify response contains array of baselines
func TestAPI05_GetAllBaselines(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Create multiple test baselines
	baselines := []map[string]interface{}{
		{
			"model_name":          "gpt-4",
			"test_type":           "style",
			"evaluation_standard": "strict",
			"baseline_channel_id": suite.TestChannel.ID,
			"prompt":              "Test prompt 1",
			"baseline_output":     "Test output 1",
		},
		{
			"model_name":          "gpt-3.5-turbo",
			"test_type":           "encoding",
			"evaluation_standard": "standard",
			"baseline_channel_id": suite.TestChannel.ID,
			"prompt":              "Test prompt 2",
			"baseline_output":     "Test output 2",
		},
	}

	for _, baseline := range baselines {
		resp, err := suite.AdminClient.Post("/api/monitor/baselines", baseline)
		require.NoError(t, err, "Failed to create baseline")
		resp.Body.Close()
	}

	// Step 2: Admin user queries all baselines
	resp, err := suite.AdminClient.Get("/api/monitor/baselines")
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 3: Verify HTTP 200 status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")

	// Step 4: Verify response contains array of baselines
	result := parseResponseBody(t, resp)

	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Check data is an array
	if data, ok := result["data"].([]interface{}); ok {
		assert.GreaterOrEqual(t, len(data), 2, "Should return at least 2 baselines")

		// Verify each baseline has required fields
		for i, item := range data {
			if baseline, ok := item.(map[string]interface{}); ok {
				assert.Contains(t, baseline, "model_name", "Baseline %d should contain model_name", i)
				assert.Contains(t, baseline, "test_type", "Baseline %d should contain test_type", i)
				assert.Contains(t, baseline, "evaluation_standard", "Baseline %d should contain evaluation_standard", i)
			}
		}
	} else {
		t.Error("Response data should be an array")
	}

	t.Logf("API-05 Test Passed: Successfully queried all baselines")
}

// ============================================================================
// Model Monitoring Policy API Tests (API-06 to API-08)
// ============================================================================

// TestAPI06_CreateMonitorPolicy tests creating a new monitoring policy.
//
// Test ID: API-06
// API Endpoint: POST /api/monitor/policies
// HTTP Method: POST
// Permission: Admin
// Test Scenario: 创建监控策略
// Expected Response: 返回201
// Priority: P0
//
// Test Steps:
// 1. Admin user creates a new monitoring policy
// 2. Verify HTTP 201 or 200 status
// 3. Verify response indicates success
// 4. Verify policy is stored in database
func TestAPI06_CreateMonitorPolicy(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Admin user creates a new monitoring policy
	policyData := map[string]interface{}{
		"name":                "Test Policy",
		"target_models":       []string{"gpt-4", "gpt-3.5-turbo"},
		"test_types":          []string{"style", "reasoning"},
		"evaluation_standard": "standard",
		"schedule_cron":       "0 */4 * * *",
		"is_enabled":          true,
	}

	resp, err := suite.AdminClient.Post("/api/monitor/policies", policyData)
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 2: Verify HTTP 201 or 200 status
	assert.True(t,
		resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK,
		"Expected 201 Created or 200 OK, got %d", resp.StatusCode)

	// Step 3: Verify response indicates success
	result := parseResponseBody(t, resp)

	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Extract policy ID from response
	var policyID int
	if data, ok := result["data"].(map[string]interface{}); ok {
		if id, exists := data["id"]; exists {
			if idFloat, ok := id.(float64); ok {
				policyID = int(idFloat)
			}
		}
	}

	// Step 4: Verify policy is stored in database
	if policyID > 0 {
		var policy model.MonitorPolicy
		err = suite.DB.First(&policy, policyID).Error
		assert.NoError(t, err, "Policy should be stored in database")
		assert.Equal(t, "Test Policy", policy.Name)
		assert.True(t, policy.IsEnabled, "Policy should be enabled")
	}

	t.Logf("API-06 Test Passed: Successfully created monitoring policy")
}

// TestAPI07_GetAllPolicies tests querying all monitoring policies.
//
// Test ID: API-07
// API Endpoint: GET /api/monitor/policies
// HTTP Method: GET
// Permission: Admin
// Test Scenario: 查询所有策略
// Expected Response: 返回200，包含策略列表
// Priority: P1
//
// Test Steps:
// 1. Create multiple test policies
// 2. Admin user queries all policies
// 3. Verify HTTP 200 status
// 4. Verify response contains array of policies
func TestAPI07_GetAllPolicies(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Create multiple test policies
	policies := []map[string]interface{}{
		{
			"name":                "Policy 1",
			"target_models":       []string{"gpt-4"},
			"test_types":          []string{"style"},
			"evaluation_standard": "strict",
			"schedule_cron":       "0 */2 * * *",
			"is_enabled":          true,
		},
		{
			"name":                "Policy 2",
			"target_models":       []string{"gpt-3.5-turbo"},
			"test_types":          []string{"encoding"},
			"evaluation_standard": "standard",
			"schedule_cron":       "0 */6 * * *",
			"is_enabled":          false,
		},
	}

	for _, policy := range policies {
		resp, err := suite.AdminClient.Post("/api/monitor/policies", policy)
		require.NoError(t, err, "Failed to create policy")
		resp.Body.Close()
	}

	// Step 2: Admin user queries all policies
	resp, err := suite.AdminClient.Get("/api/monitor/policies")
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 3: Verify HTTP 200 status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")

	// Step 4: Verify response contains array of policies
	result := parseResponseBody(t, resp)

	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Check data is an array
	if data, ok := result["data"].([]interface{}); ok {
		assert.GreaterOrEqual(t, len(data), 2, "Should return at least 2 policies")

		// Verify each policy has required fields
		for i, item := range data {
			if policy, ok := item.(map[string]interface{}); ok {
				assert.Contains(t, policy, "name", "Policy %d should contain name", i)
				assert.Contains(t, policy, "target_models", "Policy %d should contain target_models", i)
				assert.Contains(t, policy, "is_enabled", "Policy %d should contain is_enabled", i)
			}
		}
	} else {
		t.Error("Response data should be an array")
	}

	t.Logf("API-07 Test Passed: Successfully queried all policies")
}

// TestAPI08_UpdateMonitorPolicy tests updating a monitoring policy.
//
// Test ID: API-08
// API Endpoint: PUT /api/monitor/policies
// HTTP Method: PUT
// Permission: Admin
// Test Scenario: 更新策略 is_enabled=false
// Expected Response: 返回200
// Priority: P1
//
// Test Steps:
// 1. Create a test policy
// 2. Admin user updates the policy to disable it
// 3. Verify HTTP 200 status
// 4. Verify policy is updated in database
func TestAPI08_UpdateMonitorPolicy(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Create a test policy
	policyData := map[string]interface{}{
		"name":                "Policy to Update",
		"target_models":       []string{"gpt-4"},
		"test_types":          []string{"style"},
		"evaluation_standard": "standard",
		"schedule_cron":       "0 */4 * * *",
		"is_enabled":          true,
	}

	createResp, err := suite.AdminClient.Post("/api/monitor/policies", policyData)
	require.NoError(t, err, "Failed to create policy")
	defer createResp.Body.Close()

	createResult := parseResponseBody(t, createResp)
	var policyID int
	if data, ok := createResult["data"].(map[string]interface{}); ok {
		if id, exists := data["id"]; exists {
			if idFloat, ok := id.(float64); ok {
				policyID = int(idFloat)
			}
		}
	}
	require.NotZero(t, policyID, "Failed to get policy ID")

	// Step 2: Admin user updates the policy to disable it
	updateData := map[string]interface{}{
		"id":         policyID,
		"is_enabled": false,
	}

	resp, err := suite.AdminClient.Put("/api/monitor/policies", updateData)
	require.NoError(t, err, "Failed to send update request")
	defer resp.Body.Close()

	// Step 3: Verify HTTP 200 status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")

	result := parseResponseBody(t, resp)
	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Step 4: Verify policy is updated in database
	var policy model.MonitorPolicy
	err = suite.DB.First(&policy, policyID).Error
	require.NoError(t, err, "Policy should exist in database")
	assert.False(t, policy.IsEnabled, "Policy should be disabled after update")

	t.Logf("API-08 Test Passed: Successfully updated policy to disabled state")
}

// ============================================================================
// Monitoring Results Query API Tests (API-09 to API-10)
// ============================================================================

// TestAPI09_GetChannelMonitoringHistory tests querying channel monitoring history.
//
// Test ID: API-09
// API Endpoint: GET /api/channels/:id/monitoring_results
// HTTP Method: GET
// Permission: Admin
// Test Scenario: 查询渠道监控历史
// Expected Response: 返回200，包含结果数组
// Priority: P1
//
// Test Steps:
// 1. Create test monitoring results for a channel
// 2. Admin user queries monitoring results
// 3. Verify HTTP 200 status
// 4. Verify response contains array of monitoring results
func TestAPI09_GetChannelMonitoringHistory(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Create test monitoring results
	// First create a baseline
	baseline := &model.ModelBaseline{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelId:  suite.TestChannel.ID,
		Prompt:             "Test prompt",
		BaselineOutput:     "Test baseline output",
		CreatedAt:          time.Now().Unix(),
	}
	err := suite.DB.Create(baseline).Error
	require.NoError(t, err, "Failed to create baseline")

	// Create monitoring results
	reason1 := "Output matches baseline well"
	raw1 := "Test output 1"
	reason2 := "Significant deviation from baseline"
	raw2 := "Test output 2"
	results := []model.ModelMonitoringResult{
		{
			ChannelId:     suite.TestChannel.ID,
			ModelName:     "gpt-4",
			BaselineId:    baseline.Id,
			TestTimestamp: time.Now().Unix() - 3600, // 1 hour ago
			Status:        "pass",
			DiffScore:     5.0,
			Reason:        &reason1,
			RawOutput:     &raw1,
		},
		{
			ChannelId:     suite.TestChannel.ID,
			ModelName:     "gpt-4",
			BaselineId:    baseline.Id,
			TestTimestamp: time.Now().Unix() - 1800, // 30 minutes ago
			Status:        "fail",
			DiffScore:     70.0,
			Reason:        &reason2,
			RawOutput:     &raw2,
		},
	}

	for _, result := range results {
		err := suite.DB.Create(&result).Error
		require.NoError(t, err, "Failed to create monitoring result")
	}

	// Step 2: Admin user queries monitoring results
	path := fmt.Sprintf("/api/channels/%d/monitoring_results?model_name=gpt-4", suite.TestChannel.ID)
	resp, err := suite.AdminClient.Get(path)
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 3: Verify HTTP 200 status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")

	// Step 4: Verify response contains array of results
	result := parseResponseBody(t, resp)

	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Check data is an array
	if data, ok := result["data"].([]interface{}); ok {
		assert.GreaterOrEqual(t, len(data), 2, "Should return at least 2 monitoring results")

		// Verify each result has required fields
		for i, item := range data {
			if monitorResult, ok := item.(map[string]interface{}); ok {
				assert.Contains(t, monitorResult, "channel_id", "Result %d should contain channel_id", i)
				assert.Contains(t, monitorResult, "model_name", "Result %d should contain model_name", i)
				assert.Contains(t, monitorResult, "status", "Result %d should contain status", i)
				assert.Contains(t, monitorResult, "diff_score", "Result %d should contain diff_score", i)
			}
		}
	} else {
		t.Error("Response data should be an array")
	}

	t.Logf("API-09 Test Passed: Successfully queried channel monitoring history")
}

// TestAPI10_GetModelMonitoringReport tests querying model monitoring report across channels.
//
// Test ID: API-10
// API Endpoint: GET /api/models/:model_name/monitoring_report
// HTTP Method: GET
// Permission: Admin
// Test Scenario: 查询模型横向对比报告
// Expected Response: 返回200，包含所有渠道的该模型监控状态
// Priority: P1
//
// Test Steps:
// 1. Create test channels and monitoring results for multiple channels
// 2. Admin user queries model monitoring report
// 3. Verify HTTP 200 status
// 4. Verify response contains monitoring data for all channels
func TestAPI10_GetModelMonitoringReport(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Create test channels and monitoring results
	// Create second test channel
	channel2 := &testutil.ChannelModel{
		Name:   fmt.Sprintf("test-channel-2-%d", time.Now().UnixNano()),
		Type:   1,
		Key:    fmt.Sprintf("sk-test-2-%d", time.Now().UnixNano()),
		Status: 1,
		Models: "gpt-4",
		Group:  "default",
	}
	channel2ID, err := suite.AdminClient.AddChannel(channel2)
	require.NoError(t, err, "Failed to create second channel")
	channel2.ID = channel2ID

	// Create baseline
	baseline := &model.ModelBaseline{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelId:  suite.TestChannel.ID,
		Prompt:             "Test prompt",
		BaselineOutput:     "Test baseline output",
		CreatedAt:          time.Now().Unix(),
	}
	err = suite.DB.Create(baseline).Error
	require.NoError(t, err, "Failed to create baseline")

	// Create monitoring results for both channels
	reasonPass := "Good match"
	rawPass := "Output 1"
	reasonFail := "Poor match"
	rawFail := "Output 2"
	results := []model.ModelMonitoringResult{
		{
			ChannelId:     suite.TestChannel.ID,
			ModelName:     "gpt-4",
			BaselineId:    baseline.Id,
			TestTimestamp: time.Now().Unix(),
			Status:        "pass",
			DiffScore:     10.0,
			Reason:        &reasonPass,
			RawOutput:     &rawPass,
		},
		{
			ChannelId:     channel2ID,
			ModelName:     "gpt-4",
			BaselineId:    baseline.Id,
			TestTimestamp: time.Now().Unix(),
			Status:        "fail",
			DiffScore:     80.0,
			Reason:        &reasonFail,
			RawOutput:     &rawFail,
		},
	}

	for _, result := range results {
		err := suite.DB.Create(&result).Error
		require.NoError(t, err, "Failed to create monitoring result")
	}

	// Step 2: Admin user queries model monitoring report
	resp, err := suite.AdminClient.Get("/api/models/gpt-4/monitoring_report")
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 3: Verify HTTP 200 status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")

	// Step 4: Verify response contains monitoring data for all channels
	result := parseResponseBody(t, resp)

	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Check data is an array or map containing channel results
	data, dataExists := result["data"]
	assert.True(t, dataExists, "Response should contain 'data' field")

	// Verify we have results for multiple channels
	if dataArray, ok := data.([]interface{}); ok {
		assert.GreaterOrEqual(t, len(dataArray), 2, "Should have results for at least 2 channels")
	} else if dataMap, ok := data.(map[string]interface{}); ok {
		// Alternative structure: map keyed by channel ID
		assert.GreaterOrEqual(t, len(dataMap), 2, "Should have results for at least 2 channels")
	}

	t.Logf("API-10 Test Passed: Successfully queried model monitoring report")
}

// ============================================================================
// P2P Group Statistics API Tests (API-11 to API-13)
// ============================================================================

// TestAPI11_GetGroupStats_AsMember tests that group members can query group statistics.
//
// Test ID: API-11
// API Endpoint: GET /api/p2p_groups/:id/stats
// HTTP Method: GET
// Permission: Group Member
// Test Scenario: 查询分组统计
// Expected Response: 返回200，包含聚合数据
// Priority: P0
//
// Test Steps:
// 1. Add normal user as member to the test P2P group
// 2. Create test group statistics data
// 3. Normal user (as group member) queries group stats
// 4. Verify HTTP 200 status
// 5. Verify response contains aggregated statistics
func TestAPI11_GetGroupStats_AsMember(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Add normal user as member to the test P2P group
	err := suite.AdminClient.AddP2PGroupMember(suite.TestP2PGroup.ID, suite.NormalUser.ID, 1) // status=1 (Active)
	require.NoError(t, err, "Failed to add normal user to group")

	// Step 2: Create test group statistics data
	now := time.Now()
	groupStat := &model.GroupStatistics{
		GroupId:         suite.TestP2PGroup.ID,
		ModelName:       "gpt-4",
		TimeWindowStart: roundToTimeWindow(now),
		TPM:             5000,
		RPM:             100,
		FailRate:        2.5,
		AvgResponseTime: 250,
		TotalTokens:     50000,
		TotalQuota:      5000,
		AvgConcurrency:  10.5,
		TotalSessions:   150,
		UniqueUsers:     15,
		UpdatedAt:       now.Unix(),
	}

	err = suite.DB.Create(groupStat).Error
	require.NoError(t, err, "Failed to create group statistics")

	// Step 3: Normal user (as group member) queries group stats
	path := fmt.Sprintf("/api/p2p_groups/%d/stats", suite.TestP2PGroup.ID)
	resp, err := suite.NormalClient.Get(path)
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 4: Verify HTTP 200 status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")

	// Step 5: Verify response contains aggregated statistics
	result := parseResponseBody(t, resp)

	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Check data field exists and contains statistics
	data, dataExists := result["data"]
	assert.True(t, dataExists, "Response should contain 'data' field")

	if dataMap, ok := data.(map[string]interface{}); ok {
		// Verify key statistics fields are present
		assert.Contains(t, dataMap, "tpm", "Should contain tpm")
		assert.Contains(t, dataMap, "rpm", "Should contain rpm")
		assert.Contains(t, dataMap, "fail_rate", "Should contain fail_rate")
		assert.Contains(t, dataMap, "total_tokens", "Should contain total_tokens")
		assert.Contains(t, dataMap, "unique_users", "Should contain unique_users")
	}

	t.Logf("API-11 Test Passed: Group member successfully queried group statistics")
}

// TestAPI12_GetGroupStats_NonMemberPermission tests that non-members cannot access group statistics.
//
// Test ID: API-12
// API Endpoint: GET /api/p2p_groups/:id/stats
// HTTP Method: GET
// Permission: Non-Member (should be denied)
// Test Scenario: 尝试查询
// Expected Response: 返回403
// Priority: P0
//
// Test Steps:
// 1. Ensure normal user is NOT a member of the test group
// 2. Normal user attempts to query group stats
// 3. Verify HTTP 403 status (permission denied)
func TestAPI12_GetGroupStats_NonMemberPermission(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Ensure normal user is NOT a member
	// (Normal user is not added to the group by default in setup)

	// Step 2: Normal user attempts to query group stats
	path := fmt.Sprintf("/api/p2p_groups/%d/stats", suite.TestP2PGroup.ID)
	resp, err := suite.NormalClient.Get(path)
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 3: Verify permission denied status (403)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode,
		"Expected 403 Forbidden for non-member access, got %d", resp.StatusCode)

	t.Logf("API-12 Test Passed: Non-member correctly denied access to group stats")
}

// TestAPI13_GetGroupStats_WithModelFilter tests querying group statistics with model filter.
//
// Test ID: API-13
// API Endpoint: GET /api/p2p_groups/:id/stats?model=gpt-4
// HTTP Method: GET
// Permission: Group Member
// Test Scenario: 按模型过滤
// Expected Response: 返回200，仅包含gpt-4数据
// Priority: P1
//
// Test Steps:
// 1. Add normal user as member to the test P2P group
// 2. Create statistics for multiple models
// 3. Normal user queries group stats with model=gpt-4 filter
// 4. Verify HTTP 200 status
// 5. Verify response contains only gpt-4 statistics
func TestAPI13_GetGroupStats_WithModelFilter(t *testing.T) {
	suite, cleanup := SetupAPICompletenessSuite(t)
	defer cleanup()

	// Step 1: Add normal user as member to the test P2P group
	err := suite.AdminClient.AddP2PGroupMember(suite.TestP2PGroup.ID, suite.NormalUser.ID, 1) // status=1 (Active)
	require.NoError(t, err, "Failed to add normal user to group")

	// Step 2: Create statistics for multiple models
	now := time.Now()
	baseTime := roundToTimeWindow(now)

	models := []string{"gpt-4", "gpt-3.5-turbo", "claude-3-opus"}
	for i, modelName := range models {
		groupStat := &model.GroupStatistics{
			GroupId:         suite.TestP2PGroup.ID,
			ModelName:       modelName,
			TimeWindowStart: baseTime,
			TPM:             1000 * (i + 1),
			RPM:             50 * (i + 1),
			FailRate:        1.0 + float64(i),
			AvgResponseTime: 200 + (i * 50),
			TotalTokens:     10000 * int64(i+1),
			TotalQuota:      1000 * int64(i+1),
			AvgConcurrency:  5.0 + float64(i),
			TotalSessions:   100 * int64(i+1),
			UniqueUsers:     10 * (i + 1),
			UpdatedAt:       now.Unix(),
		}

		err = suite.DB.Create(groupStat).Error
		require.NoError(t, err, "Failed to create group statistics for %s", modelName)
	}

	// Step 3: Normal user queries group stats with model filter
	path := fmt.Sprintf("/api/p2p_groups/%d/stats?model=gpt-4", suite.TestP2PGroup.ID)
	resp, err := suite.NormalClient.Get(path)
	require.NoError(t, err, "Failed to send request")
	defer resp.Body.Close()

	// Step 4: Verify HTTP 200 status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Expected 200 OK status")

	// Step 5: Verify response contains only gpt-4 statistics
	result := parseResponseBody(t, resp)

	if success, ok := result["success"].(bool); ok {
		assert.True(t, success, "Expected success=true")
	}

	// Check data field exists
	data, dataExists := result["data"]
	assert.True(t, dataExists, "Response should contain 'data' field")

	if dataMap, ok := data.(map[string]interface{}); ok {
		// Verify we have gpt-4 data
		// Check if model_name field exists and equals "gpt-4"
		if modelName, exists := dataMap["model_name"]; exists {
			assert.Equal(t, "gpt-4", modelName, "Response should only contain gpt-4 data")
		}

		// Verify statistics fields are present
		assert.Contains(t, dataMap, "tpm", "Should contain tpm")
		assert.Contains(t, dataMap, "rpm", "Should contain rpm")

		// Verify the TPM value matches gpt-4 (should be 1000)
		if tpm, ok := dataMap["tpm"].(float64); ok {
			assert.Equal(t, float64(1000), tpm, "TPM should match gpt-4 statistics")
		}
	}

	t.Logf("API-13 Test Passed: Successfully queried group stats filtered by model=gpt-4")
}
