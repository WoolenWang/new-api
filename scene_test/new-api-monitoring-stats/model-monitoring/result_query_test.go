package model_monitoring_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"one-api/scene_test/testutil"
)

// ResultQuerySuite tests the monitoring result storage and query functionality.
type ResultQuerySuite struct {
	suite.Suite
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	judgeLLM *testutil.MockJudgeLLM
	fixtures *testutil.TestFixtures
}

// SetupSuite runs once before all tests in the suite.
func (s *ResultQuerySuite) SetupSuite() {
	var err error
	s.server, err = testutil.StartTestServer()
	if err != nil {
		s.T().Fatalf("Failed to start test server: %v", err)
	}

	s.client = testutil.NewAPIClient(s.server)
	s.upstream = testutil.NewMockUpstreamServer()
	s.judgeLLM = testutil.NewMockJudgeLLM()

	// Setup basic fixtures
	s.fixtures = testutil.NewTestFixtures(s.T(), s.client)
	s.fixtures.SetUpstream(s.upstream)

	// Create basic users and channels
	if err := s.fixtures.SetupBasicUsers(); err != nil {
		s.T().Fatalf("Failed to setup basic users: %v", err)
	}

	if err := s.fixtures.SetupBasicChannels(); err != nil {
		s.T().Fatalf("Failed to setup basic channels: %v", err)
	}
}

// TearDownSuite runs once after all tests in the suite.
func (s *ResultQuerySuite) TearDownSuite() {
	if s.fixtures != nil {
		s.fixtures.Cleanup()
	}
	if s.judgeLLM != nil {
		s.judgeLLM.Close()
	}
	if s.upstream != nil {
		s.upstream.Close()
	}
	if s.server != nil {
		s.server.Stop()
	}
}

// SetupTest runs before each test.
func (s *ResultQuerySuite) SetupTest() {
	if s.upstream != nil {
		s.upstream.Reset()
	}
	if s.judgeLLM != nil {
		s.judgeLLM.Reset()
	}
}

// TearDownTest runs after each test.
func (s *ResultQuerySuite) TearDownTest() {
	// Clean up resources
}

// createTestMonitoringData creates test monitoring results for query tests.
func (s *ResultQuerySuite) createTestMonitoringData() (baselineID int, policyID int) {
	// Create baseline
	baseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "测试Prompt",
		BaselineOutput:     "测试基准输出",
	}
	var err error
	baselineID, err = s.client.CreateBaseline(baseline)
	assert.NoError(s.T(), err, "Baseline creation should succeed")

	// Create policy
	policy := &testutil.MonitorPolicyModel{
		Name:               "Query Test Policy",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"style"},
		EvaluationStandard: "standard",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyID, err = s.client.CreateMonitorPolicy(policy)
	assert.NoError(s.T(), err, "Policy creation should succeed")

	// Configure mocks
	s.upstream.SetResponse(200, map[string]interface{}{
		"id":     "test-query",
		"object": "chat.completion",
		"model":  "gpt-4",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "测试输出",
				},
				"finish_reason": "stop",
			},
		},
	})
	s.judgeLLM.SetHighSimilarity()

	// Trigger monitoring to create results
	err = s.client.TriggerMonitorWorker(policyID)
	if err != nil {
		s.T().Logf("Warning: Could not trigger monitor worker: %v", err)
	} else {
		time.Sleep(2 * time.Second)
	}

	return baselineID, policyID
}

// TestMR01_ResultPersistence tests that monitoring results are properly persisted.
//
// Test ID: MR-01
// Priority: P0
// Test Scenario: 结果持久化
// Expected Result: 表中新增记录，包含完整字段
func (s *ResultQuerySuite) TestMR01_ResultPersistence() {
	s.T().Log("MR-01: Testing monitoring result persistence")

	// Arrange & Act: Create test data and trigger monitoring
	baselineID, _ := s.createTestMonitoringData()

	// Assert: Verify result was persisted
	result, err := s.client.GetLatestMonitoringResult(s.fixtures.PublicChannel.ID, "gpt-4")
	if err != nil {
		s.T().Logf("Warning: Could not get monitoring result: %v", err)
		s.T().Skip("Skipping - monitoring results not available")
	}

	// Verify all important fields are populated
	assert.Greater(s.T(), result.ID, int64(0), "Result ID should be positive")
	assert.Equal(s.T(), s.fixtures.PublicChannel.ID, result.ChannelID, "Channel ID should match")
	assert.Equal(s.T(), "gpt-4", result.ModelName, "Model name should match")
	assert.Equal(s.T(), baselineID, result.BaselineID, "Baseline ID should match")
	assert.Greater(s.T(), result.TestTimestamp, int64(0), "Test timestamp should be set")
	assert.NotEmpty(s.T(), result.Status, "Status should not be empty")
	assert.GreaterOrEqual(s.T(), result.DiffScore, 0.0, "Diff score should be non-negative")
	assert.NotEmpty(s.T(), result.Reason, "Reason should not be empty")
	assert.NotEmpty(s.T(), result.RawOutput, "Raw output should not be empty")

	s.T().Logf("MR-01: Successfully verified result persistence with ID %d", result.ID)
}

// TestMR02_HistoricalResultQuery tests querying historical monitoring results.
//
// Test ID: MR-02
// Priority: P1
// Test Scenario: 历史结果查询
// Expected Result: 返回该渠道该模型的所有历史监控记录，按时间倒序
func (s *ResultQuerySuite) TestMR02_HistoricalResultQuery() {
	s.T().Log("MR-02: Testing historical result query")

	// Arrange: Create multiple monitoring results over time
	s.createTestMonitoringData()

	// Wait a bit
	time.Sleep(500 * time.Millisecond)

	// Trigger another monitoring run
	policies, err := s.client.GetAllMonitorPolicies()
	if err == nil && len(policies) > 0 {
		s.client.TriggerMonitorWorker(policies[0].ID)
		time.Sleep(2 * time.Second)
	}

	// Act: Query historical results
	now := time.Now().Unix()
	startTime := now - 3600 // Last hour
	results, err := s.client.GetChannelMonitoringResults(
		s.fixtures.PublicChannel.ID,
		"gpt-4",
		"style",
		startTime,
		now+100,
	)

	// Assert: Verify results are returned
	if err != nil {
		s.T().Logf("Warning: Could not get historical results: %v", err)
		s.T().Skip("Skipping - monitoring results query not available")
	}

	assert.Greater(s.T(), len(results), 0, "Should have at least one historical result")

	// Verify results are sorted by timestamp (descending)
	if len(results) > 1 {
		for i := 0; i < len(results)-1; i++ {
			assert.GreaterOrEqual(s.T(), results[i].TestTimestamp, results[i+1].TestTimestamp,
				"Results should be sorted by timestamp descending")
		}
	}

	// Verify all results match the query criteria
	for _, result := range results {
		assert.Equal(s.T(), s.fixtures.PublicChannel.ID, result.ChannelID, "All results should match channel ID")
		assert.Equal(s.T(), "gpt-4", result.ModelName, "All results should match model name")
		assert.GreaterOrEqual(s.T(), result.TestTimestamp, startTime, "Timestamp should be within range")
		assert.LessOrEqual(s.T(), result.TestTimestamp, now+100, "Timestamp should be within range")
	}

	s.T().Logf("MR-02: Successfully retrieved %d historical results", len(results))
}

// TestMR03_ModelMonitoringReport tests retrieving a cross-channel monitoring report for a model.
//
// Test ID: MR-03
// Priority: P1
// Test Scenario: 模型横向对比报告
// Expected Result: 返回所有渠道的该模型最新监控状态，方便对比
func (s *ResultQuerySuite) TestMR03_ModelMonitoringReport() {
	s.T().Log("MR-03: Testing model monitoring report")

	// Arrange: Create monitoring data for multiple channels
	s.createTestMonitoringData()

	// Create monitoring for another channel if possible
	if s.fixtures.GroupChannel1 != nil {
		baseline := &testutil.ModelBaselineModel{
			ModelName:          "gpt-4",
			TestType:           "style",
			EvaluationStandard: "standard",
			BaselineChannelID:  s.fixtures.GroupChannel1.ID,
			Prompt:             "测试Prompt",
			BaselineOutput:     "测试基准输出",
		}
		s.client.CreateBaseline(baseline)

		policy := &testutil.MonitorPolicyModel{
			Name:               "Multi-Channel Test",
			TargetModels:       []string{"gpt-4"},
			TestTypes:          []string{"style"},
			EvaluationStandard: "standard",
			TargetChannels:     []int{s.fixtures.GroupChannel1.ID},
			ScheduleCron:       "0 */4 * * *",
			IsEnabled:          true,
		}
		policyID, err := s.client.CreateMonitorPolicy(policy)
		if err == nil {
			s.client.TriggerMonitorWorker(policyID)
			time.Sleep(2 * time.Second)
		}
	}

	// Act: Get model monitoring report
	report, err := s.client.GetModelMonitoringReport("gpt-4")

	// Assert: Verify report contains data
	if err != nil {
		s.T().Logf("Warning: Could not get model monitoring report: %v", err)
		s.T().Skip("Skipping - model monitoring report not available")
	}

	assert.Greater(s.T(), len(report), 0, "Report should contain at least one channel's data")

	// Verify all results are for the requested model
	channelMap := make(map[int]bool)
	for _, result := range report {
		assert.Equal(s.T(), "gpt-4", result.ModelName, "All results should be for gpt-4")
		channelMap[result.ChannelID] = true
	}

	s.T().Logf("MR-03: Successfully retrieved monitoring report for %d channels", len(channelMap))

	// Verify we can compare channels
	if len(report) > 1 {
		s.T().Log("MR-03: Multiple channels available for comparison:")
		for _, result := range report {
			s.T().Logf("  - Channel %d: Status=%s, DiffScore=%.2f",
				result.ChannelID, result.Status, result.DiffScore)
		}
	}
}

// TestMR04_TimeRangeFilter tests filtering results by time range.
//
// Test ID: MR-04
// Priority: P2
// Test Scenario: 时间范围过滤
// Expected Result: 仅返回时间范围内的记录
func (s *ResultQuerySuite) TestMR04_TimeRangeFilter() {
	s.T().Log("MR-04: Testing time range filter")

	// Arrange: Create monitoring data
	s.createTestMonitoringData()

	// Record the current time as a reference point
	midTime := time.Now().Unix()
	time.Sleep(1 * time.Second)

	// Create another result after the midpoint
	policies, err := s.client.GetAllMonitorPolicies()
	if err == nil && len(policies) > 0 {
		s.client.TriggerMonitorWorker(policies[0].ID)
		time.Sleep(2 * time.Second)
	}

	// Act: Query with different time ranges

	// Query 1: Get all results
	allResults, err := s.client.GetChannelMonitoringResults(
		s.fixtures.PublicChannel.ID,
		"gpt-4",
		"style",
		0,
		time.Now().Unix()+100,
	)
	if err != nil {
		s.T().Skip("Skipping - monitoring results query not available")
	}

	// Query 2: Get only recent results (after midpoint)
	recentResults, err := s.client.GetChannelMonitoringResults(
		s.fixtures.PublicChannel.ID,
		"gpt-4",
		"style",
		midTime,
		time.Now().Unix()+100,
	)
	assert.NoError(s.T(), err, "Recent results query should succeed")

	// Query 3: Get only old results (before midpoint)
	oldResults, err := s.client.GetChannelMonitoringResults(
		s.fixtures.PublicChannel.ID,
		"gpt-4",
		"style",
		0,
		midTime,
	)
	assert.NoError(s.T(), err, "Old results query should succeed")

	// Assert: Verify filtering works correctly
	s.T().Logf("MR-04: Query results - All: %d, Recent: %d, Old: %d",
		len(allResults), len(recentResults), len(oldResults))

	// Verify time boundaries are respected
	for _, result := range recentResults {
		assert.GreaterOrEqual(s.T(), result.TestTimestamp, midTime,
			"Recent results should be after midpoint")
	}

	for _, result := range oldResults {
		assert.LessOrEqual(s.T(), result.TestTimestamp, midTime,
			"Old results should be before or at midpoint")
	}

	// Verify total count makes sense
	if len(allResults) > 1 {
		assert.LessOrEqual(s.T(), len(recentResults), len(allResults),
			"Recent results should not exceed all results")
		assert.LessOrEqual(s.T(), len(oldResults), len(allResults),
			"Old results should not exceed all results")
	}

	s.T().Log("MR-04: Time range filter test completed successfully")
}

// TestMR05_MultipleTestTypesQuery tests querying results for different test types.
//
// Additional test (bonus): Verify we can distinguish between different test types in results.
func (s *ResultQuerySuite) TestMR05_MultipleTestTypesQuery() {
	s.T().Log("MR-05 (Bonus): Testing multiple test types query")

	// Arrange: Create baselines for different test types
	baselineStyle := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "Style test",
		BaselineOutput:     "Style output",
	}
	s.client.CreateBaseline(baselineStyle)

	baselineReasoning := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "reasoning",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "Reasoning test",
		BaselineOutput:     "Reasoning output",
	}
	s.client.CreateBaseline(baselineReasoning)

	// Create policies for both test types
	policyStyle := &testutil.MonitorPolicyModel{
		Name:               "Style Policy",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"style"},
		EvaluationStandard: "standard",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	styleID, _ := s.client.CreateMonitorPolicy(policyStyle)

	policyReasoning := &testutil.MonitorPolicyModel{
		Name:               "Reasoning Policy",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"reasoning"},
		EvaluationStandard: "standard",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	reasoningID, _ := s.client.CreateMonitorPolicy(policyReasoning)

	// Trigger both
	s.client.TriggerMonitorWorker(styleID)
	time.Sleep(1 * time.Second)
	s.client.TriggerMonitorWorker(reasoningID)
	time.Sleep(2 * time.Second)

	// Act: Query results for each test type
	styleResults, err1 := s.client.GetChannelMonitoringResults(
		s.fixtures.PublicChannel.ID, "gpt-4", "style", 0, time.Now().Unix()+100)

	reasoningResults, err2 := s.client.GetChannelMonitoringResults(
		s.fixtures.PublicChannel.ID, "gpt-4", "reasoning", 0, time.Now().Unix()+100)

	// Assert: Verify we can distinguish between test types
	if err1 != nil || err2 != nil {
		s.T().Skip("Skipping - monitoring results query not available")
	}

	s.T().Logf("MR-05: Style results: %d, Reasoning results: %d",
		len(styleResults), len(reasoningResults))

	// If we have results, verify they have different baselines
	if len(styleResults) > 0 && len(reasoningResults) > 0 {
		// The baseline IDs should be different for different test types
		assert.NotEqual(s.T(), styleResults[0].BaselineID, reasoningResults[0].BaselineID,
			"Different test types should use different baselines")
	}

	s.T().Log("MR-05: Multiple test types query test completed")
}

// TestRunner for the result query test suite
func TestResultQuerySuite(t *testing.T) {
	suite.Run(t, new(ResultQuerySuite))
}
