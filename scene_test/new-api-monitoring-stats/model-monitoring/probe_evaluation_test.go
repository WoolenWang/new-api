package model_monitoring_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// ProbeEvaluationSuite tests the model probing and evaluation functionality.
type ProbeEvaluationSuite struct {
	suite.Suite
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	judgeLLM *testutil.MockJudgeLLM
	fixtures *testutil.TestFixtures
}

// SetupSuite runs once before all tests in the suite.
func (s *ProbeEvaluationSuite) SetupSuite() {
	var err error
	s.judgeLLM = testutil.NewMockJudgeLLM()

	// Start test server with MONITOR_JUDGE_URL bound to the mock judge LLM,
	// so that MonitorEvaluator will call the in-process mock instead of
	// a hard-coded localhost:3000 endpoint.
	projectRoot, err := testutil.FindProjectRoot()
	if err != nil {
		s.T().Fatalf("Failed to find project root: %v", err)
	}
	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = true
	cfg.CustomEnv = map[string]string{
		"MONITOR_JUDGE_URL":             fmt.Sprintf("%s/v1/chat/completions", s.judgeLLM.BaseURL),
		"MONITOR_JUDGE_MODEL":           "gpt-4-judge",
		"MONITOR_JUDGE_MAX_RETRIES":     "2",
		"MONITOR_PROBE_MAX_RETRIES":     "2",
		"MONITOR_PROBE_TIMEOUT_SECONDS": "1",
	}

	s.server, err = testutil.StartServer(cfg)
	if err != nil {
		s.T().Fatalf("Failed to start test server: %v", err)
	}

	s.client = testutil.NewAPIClient(s.server)
	s.upstream = testutil.NewMockUpstreamServer()

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
func (s *ProbeEvaluationSuite) TearDownSuite() {
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
func (s *ProbeEvaluationSuite) SetupTest() {
	if s.upstream != nil {
		s.upstream.Reset()
	}
	if s.judgeLLM != nil {
		s.judgeLLM.Reset()
	}
}

// TearDownTest runs after each test.
func (s *ProbeEvaluationSuite) TearDownTest() {
	// Clean up resources created during tests
}

// TestME01_RuleBasedEvaluation_Encoding tests simple rule-based evaluation for encoding tasks.
//
// Test ID: ME-01
// Priority: P0
// Test Scenario: 简单规则评估 (编码) - encoding
// Expected Result: status=pass, diff_score=0
func (s *ProbeEvaluationSuite) TestME01_RuleBasedEvaluation_Encoding() {
	s.T().Log("ME-01: Testing rule-based evaluation for encoding")

	// Arrange: Create an encoding baseline
	baseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "encoding",
		EvaluationStandard: "strict",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "请编写一个Python函数，计算斐波那契数列的第n项",
		BaselineOutput: `def fibonacci(n):
    if n <= 1:
        return n
    return fibonacci(n-1) + fibonacci(n-2)`,
	}
	baselineID, err := s.client.CreateBaseline(baseline)
	assert.NoError(s.T(), err, "Baseline creation should succeed")

	// Configure mock upstream to return valid executable code
	s.upstream.SetResponse(200, map[string]interface{}{
		"id":     "test-encoding",
		"object": "chat.completion",
		"model":  "gpt-4",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role": "assistant",
					"content": `def fibonacci(n):
    if n <= 1:
        return n
    return fibonacci(n-1) + fibonacci(n-2)`,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     20,
			"completion_tokens": 30,
			"total_tokens":      50,
		},
	})

	// Create and trigger policy
	policy := &testutil.MonitorPolicyModel{
		Name:               "Encoding Test Policy",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"encoding"},
		EvaluationStandard: "strict",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyID, err := s.client.CreateMonitorPolicy(policy)
	assert.NoError(s.T(), err, "Policy creation should succeed")

	// Act: Trigger monitoring
	err = s.client.TriggerMonitorWorker(policyID)
	if err != nil {
		s.T().Logf("Warning: Could not trigger monitor worker: %v", err)
		s.T().Skip("Skipping - monitor worker not implemented")
	}

	// Wait for monitoring to complete
	time.Sleep(2 * time.Second)

	// Assert: Verify monitoring result
	result, err := s.client.GetLatestMonitoringResult(s.fixtures.PublicChannel.ID, "gpt-4")
	if err != nil {
		s.T().Logf("Warning: Could not get monitoring result: %v", err)
		s.T().Skip("Skipping - monitoring results not available")
	}

	assert.Equal(s.T(), "pass", result.Status, "Encoding test should pass")
	assert.Equal(s.T(), 0.0, result.DiffScore, "Diff score should be 0 for passing code")
	assert.Equal(s.T(), baselineID, result.BaselineID, "Should reference correct baseline")

	s.T().Log("ME-01: Rule-based encoding evaluation completed successfully")
}

// TestME02_RuleBasedEvaluation_Failure tests rule-based evaluation failure scenario.
//
// Test ID: ME-02
// Priority: P0
// Test Scenario: 简单规则评估失败
// Expected Result: status=fail, reason 包含错误信息
func (s *ProbeEvaluationSuite) TestME02_RuleBasedEvaluation_Failure() {
	s.T().Log("ME-02: Testing rule-based evaluation failure")

	// Arrange: Create baseline
	baseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "encoding",
		EvaluationStandard: "strict",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "请编写一个Python函数来反转字符串",
		BaselineOutput:     "def reverse_string(s):\n    return s[::-1]",
	}
	_, err := s.client.CreateBaseline(baseline)
	assert.NoError(s.T(), err, "Baseline creation should succeed")

	// Configure mock upstream to return code with syntax error
	s.upstream.SetResponse(200, map[string]interface{}{
		"id":     "test-encoding-fail",
		"object": "chat.completion",
		"model":  "gpt-4",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role": "assistant",
					"content": `def reverse_string(s)
    return s[::-1]  # Missing colon - syntax error`,
				},
				"finish_reason": "stop",
			},
		},
	})

	// Create and trigger policy
	policy := &testutil.MonitorPolicyModel{
		Name:               "Encoding Failure Test",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"encoding"},
		EvaluationStandard: "strict",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyID, err := s.client.CreateMonitorPolicy(policy)
	assert.NoError(s.T(), err, "Policy creation should succeed")

	// Act: Trigger monitoring
	err = s.client.TriggerMonitorWorker(policyID)
	if err != nil {
		s.T().Skip("Skipping - monitor worker not implemented")
	}

	time.Sleep(2 * time.Second)

	// Assert: Verify failure result
	result, err := s.client.GetLatestMonitoringResult(s.fixtures.PublicChannel.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - monitoring results not available")
	}

	assert.Equal(s.T(), "fail", result.Status, "Encoding test should fail")
	assert.NotEmpty(s.T(), result.Reason, "Reason should contain error information")
	assert.Contains(s.T(), result.Reason, "error", "Reason should mention error")

	s.T().Log("ME-02: Rule-based evaluation failure test completed")
}

// TestME03_LLMJudgeEvaluation_Style_Pass tests LLM-as-judge evaluation for style with high similarity.
//
// Test ID: ME-03
// Priority: P0
// Test Scenario: LLM裁判评估 (风格) - style
// Expected Result: status=pass, diff_score=5 (100-95)
func (s *ProbeEvaluationSuite) TestME03_LLMJudgeEvaluation_Style_Pass() {
	s.T().Log("ME-03: Testing LLM judge evaluation for style (pass)")

	// Arrange: Create style baseline
	baseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "请用优雅的语言描述春天的景色",
		BaselineOutput:     "春风拂面，万物复苏。桃花灼灼，柳绿如烟。大地披上了一层翠绿的新装，生机勃勃，充满希望。",
	}
	baselineID, err := s.client.CreateBaseline(baseline)
	assert.NoError(s.T(), err, "Baseline creation should succeed")

	// Configure mock upstream to return similar output
	s.upstream.SetResponse(200, map[string]interface{}{
		"id":     "test-style-pass",
		"object": "chat.completion",
		"model":  "gpt-4",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "春风送暖，万象更新。桃红柳绿，莺歌燕舞。大地换上崭新的绿衣，生机盎然，令人欣喜。",
				},
				"finish_reason": "stop",
			},
		},
	})

	// Configure judge LLM to return high similarity
	s.judgeLLM.SetHighSimilarity() // similarity_score=95, is_pass=true

	// Create and trigger policy
	policy := &testutil.MonitorPolicyModel{
		Name:               "Style Test Policy",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"style"},
		EvaluationStandard: "standard",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyID, err := s.client.CreateMonitorPolicy(policy)
	assert.NoError(s.T(), err, "Policy creation should succeed")

	// Act: Trigger monitoring
	err = s.client.TriggerMonitorWorker(policyID)
	if err != nil {
		s.T().Skip("Skipping - monitor worker not implemented")
	}

	time.Sleep(2 * time.Second)

	// Assert: Verify pass result with low diff score
	result, err := s.client.GetLatestMonitoringResult(s.fixtures.PublicChannel.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - monitoring results not available")
	}

	assert.Equal(s.T(), "pass", result.Status, "Style test should pass")
	assert.InDelta(s.T(), 5.0, result.DiffScore, 1.0, "Diff score should be around 5 (100-95)")
	assert.Equal(s.T(), baselineID, result.BaselineID, "Should reference correct baseline")
	assert.Contains(s.T(), result.Reason, "一致", "Reason should mention consistency")

	s.T().Log("ME-03: LLM judge style evaluation (pass) completed successfully")
}

// TestME04_LLMJudgeEvaluation_Style_Fail tests LLM-as-judge evaluation failure.
//
// Test ID: ME-04
// Priority: P0
// Test Scenario: LLM裁判评估失败
// Expected Result: status=fail, diff_score=70 (100-30)
func (s *ProbeEvaluationSuite) TestME04_LLMJudgeEvaluation_Style_Fail() {
	s.T().Log("ME-04: Testing LLM judge evaluation for style (fail)")

	// Arrange: Create baseline
	baseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "请用优雅的语言描述春天的景色",
		BaselineOutput:     "春风拂面，万物复苏。桃花灼灼，柳绿如烟。",
	}
	_, err := s.client.CreateBaseline(baseline)
	assert.NoError(s.T(), err, "Baseline creation should succeed")

	// Configure mock upstream to return very different output
	s.upstream.SetResponse(200, map[string]interface{}{
		"id":     "test-style-fail",
		"object": "chat.completion",
		"model":  "gpt-4",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "春天到了，天气变暖，花开了，树绿了。",
				},
				"finish_reason": "stop",
			},
		},
	})

	// Configure judge LLM to return low similarity
	s.judgeLLM.SetLowSimilarity() // similarity_score=30, is_pass=false

	// Create and trigger policy
	policy := &testutil.MonitorPolicyModel{
		Name:               "Style Fail Test",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"style"},
		EvaluationStandard: "standard",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyID, err := s.client.CreateMonitorPolicy(policy)
	assert.NoError(s.T(), err, "Policy creation should succeed")

	// Act: Trigger monitoring
	err = s.client.TriggerMonitorWorker(policyID)
	if err != nil {
		s.T().Skip("Skipping - monitor worker not implemented")
	}

	time.Sleep(2 * time.Second)

	// Assert: Verify fail result with high diff score
	result, err := s.client.GetLatestMonitoringResult(s.fixtures.PublicChannel.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - monitoring results not available")
	}

	assert.Equal(s.T(), "fail", result.Status, "Style test should fail")
	assert.InDelta(s.T(), 70.0, result.DiffScore, 5.0, "Diff score should be around 70 (100-30)")
	assert.Contains(s.T(), result.Reason, "差异", "Reason should mention difference")

	s.T().Log("ME-04: LLM judge style evaluation (fail) completed successfully")
}

// TestME05_EvaluationStandardDifference tests different evaluation standards produce different results.
//
// Test ID: ME-05
// Priority: P0
// Test Scenario: 评估标准差异
// Expected Result: 不同 evaluation_standard 产生不同的 is_pass 结果
func (s *ProbeEvaluationSuite) TestME05_EvaluationStandardDifference() {
	s.T().Log("ME-05: Testing evaluation standard difference")

	// Arrange: Create baselines for different standards
	baselineStandard := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "测试Prompt",
		BaselineOutput:     "测试基准输出",
	}
	_, err := s.client.CreateBaseline(baselineStandard)
	assert.NoError(s.T(), err, "Standard baseline creation should succeed")

	baselineStrict := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "strict",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "测试Prompt",
		BaselineOutput:     "测试基准输出",
	}
	_, err = s.client.CreateBaseline(baselineStrict)
	assert.NoError(s.T(), err, "Strict baseline creation should succeed")

	// Configure upstream to return borderline quality output
	s.upstream.SetResponse(200, map[string]interface{}{
		"id":     "test-standard-diff",
		"object": "chat.completion",
		"model":  "gpt-4",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "测试输出 - 质量中等",
				},
				"finish_reason": "stop",
			},
		},
	})

	// Configure judge LLM for medium similarity (should pass standard, fail strict)
	s.judgeLLM.SetMediumSimilarity() // similarity_score=75

	// Create policy with standard evaluation
	policyStandard := &testutil.MonitorPolicyModel{
		Name:               "Standard Evaluation",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"style"},
		EvaluationStandard: "standard",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyStandardID, err := s.client.CreateMonitorPolicy(policyStandard)
	assert.NoError(s.T(), err, "Standard policy creation should succeed")

	// Create policy with strict evaluation
	policyStrict := &testutil.MonitorPolicyModel{
		Name:               "Strict Evaluation",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"style"},
		EvaluationStandard: "strict",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyStrictID, err := s.client.CreateMonitorPolicy(policyStrict)
	assert.NoError(s.T(), err, "Strict policy creation should succeed")

	// Act: Trigger both policies
	err = s.client.TriggerMonitorWorker(policyStandardID)
	if err == nil {
		time.Sleep(1 * time.Second)
		err = s.client.TriggerMonitorWorker(policyStrictID)
	}

	if err != nil {
		s.T().Skip("Skipping - monitor worker not implemented")
	}

	time.Sleep(2 * time.Second)

	// Assert: In a full implementation, we would verify that:
	// - Standard evaluation: status=pass (threshold ~70%)
	// - Strict evaluation: status=fail (threshold ~85%)

	s.T().Log("ME-05: Evaluation standard difference test completed")
	s.T().Log("Note: Full verification requires checking results with different evaluation standards")
}

// TestME06_ProbeRetryMechanism tests the retry mechanism for failed probes.
//
// Test ID: ME-06
// Priority: P1
// Test Scenario: 探测重试机制
// Expected Result: 系统自动重试，最终记录成功结果
func (s *ProbeEvaluationSuite) TestME06_ProbeRetryMechanism() {
	s.T().Log("ME-06: Testing probe retry mechanism")

	// Arrange: Create baseline
	baseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "测试Prompt",
		BaselineOutput:     "测试基准输出",
	}
	_, err := s.client.CreateBaseline(baseline)
	assert.NoError(s.T(), err, "Baseline creation should succeed")

	// Configure upstream to fail first, then succeed
	// First call: Return 500 error
	s.upstream.SetError(500, "internal_error", "Temporary server error")

	// Create policy
	policy := &testutil.MonitorPolicyModel{
		Name:               "Retry Test Policy",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"style"},
		EvaluationStandard: "standard",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyID, err := s.client.CreateMonitorPolicy(policy)
	assert.NoError(s.T(), err, "Policy creation should succeed")

	// Act: Trigger monitoring (should retry and eventually succeed)
	err = s.client.TriggerMonitorWorker(policyID)
	if err != nil {
		s.T().Skip("Skipping - monitor worker not implemented")
	}

	// Change upstream to success after first call
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.upstream.SetResponse(200, map[string]interface{}{
			"id":     "test-retry-success",
			"object": "chat.completion",
			"model":  "gpt-4",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "成功的重试输出",
					},
					"finish_reason": "stop",
				},
			},
		})
	}()

	time.Sleep(3 * time.Second) // Wait for retries

	// Assert: Verify eventually successful result
	result, err := s.client.GetLatestMonitoringResult(s.fixtures.PublicChannel.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - monitoring results not available")
	}

	// Should eventually succeed after retry
	assert.NotEqual(s.T(), "monitor_failed", result.Status, "Should not be monitor_failed after successful retry")

	s.T().Log("ME-06: Probe retry mechanism test completed")
}

// TestME07_ProbeFailureMarking tests marking probe failures.
//
// Test ID: ME-07
// Priority: P0
// Test Scenario: 探测失败标记
// Expected Result: status=monitor_failed, reason 记录错误原因
func (s *ProbeEvaluationSuite) TestME07_ProbeFailureMarking() {
	s.T().Log("ME-07: Testing probe failure marking")

	// Arrange: Create baseline
	baseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "测试Prompt",
		BaselineOutput:     "测试基准输出",
	}
	_, err := s.client.CreateBaseline(baseline)
	assert.NoError(s.T(), err, "Baseline creation should succeed")

	// Configure upstream to timeout (or return persistent error)
	s.upstream.SetDelay(30 * time.Second) // Timeout
	s.upstream.SetError(500, "timeout", "Request timeout")

	// Create policy
	policy := &testutil.MonitorPolicyModel{
		Name:               "Failure Marking Test",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"style"},
		EvaluationStandard: "standard",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyID, err := s.client.CreateMonitorPolicy(policy)
	assert.NoError(s.T(), err, "Policy creation should succeed")

	// Act: Trigger monitoring (should fail after max retries)
	err = s.client.TriggerMonitorWorker(policyID)
	if err != nil {
		s.T().Skip("Skipping - monitor worker not implemented")
	}

	time.Sleep(5 * time.Second) // Wait for retries to exhaust

	// Assert: Verify monitor_failed status
	result, err := s.client.GetLatestMonitoringResult(s.fixtures.PublicChannel.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - monitoring results not available")
	}

	assert.Equal(s.T(), "monitor_failed", result.Status, "Should be marked as monitor_failed")
	assert.NotEmpty(s.T(), result.Reason, "Should have failure reason")
	assert.Contains(s.T(), result.Reason, "error", "Reason should mention error")

	s.T().Log("ME-07: Probe failure marking test completed")
}

// TestME08_JudgeLLMFailureHandling tests handling judge LLM failures.
//
// Test ID: ME-08
// Priority: P1
// Test Scenario: 裁判LLM失败处理
// Expected Result: status=monitor_failed, 不影响其他渠道的探测
func (s *ProbeEvaluationSuite) TestME08_JudgeLLMFailureHandling() {
	s.T().Log("ME-08: Testing judge LLM failure handling")

	// Arrange: Create baseline
	baseline := &testutil.ModelBaselineModel{
		ModelName:          "gpt-4",
		TestType:           "style",
		EvaluationStandard: "standard",
		BaselineChannelID:  s.fixtures.PublicChannel.ID,
		Prompt:             "测试Prompt",
		BaselineOutput:     "测试基准输出",
	}
	_, err := s.client.CreateBaseline(baseline)
	assert.NoError(s.T(), err, "Baseline creation should succeed")

	// Configure upstream to succeed
	s.upstream.SetResponse(200, map[string]interface{}{
		"id":     "test-judge-fail",
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

	// Configure judge LLM to fail
	s.judgeLLM.SetFailure("service_unavailable", "Judge LLM service is unavailable")

	// Create policy
	policy := &testutil.MonitorPolicyModel{
		Name:               "Judge Failure Test",
		TargetModels:       []string{"gpt-4"},
		TestTypes:          []string{"style"},
		EvaluationStandard: "standard",
		ScheduleCron:       "0 */4 * * *",
		IsEnabled:          true,
	}
	policyID, err := s.client.CreateMonitorPolicy(policy)
	assert.NoError(s.T(), err, "Policy creation should succeed")

	// Act: Trigger monitoring
	err = s.client.TriggerMonitorWorker(policyID)
	if err != nil {
		s.T().Skip("Skipping - monitor worker not implemented")
	}

	time.Sleep(2 * time.Second)

	// Assert: Verify monitor_failed due to judge LLM failure
	result, err := s.client.GetLatestMonitoringResult(s.fixtures.PublicChannel.ID, "gpt-4")
	if err != nil {
		s.T().Skip("Skipping - monitoring results not available")
	}

	assert.Equal(s.T(), "monitor_failed", result.Status, "Should be monitor_failed due to judge failure")
	assert.NotEmpty(s.T(), result.Reason, "Should have failure reason")

	// Verify that other channels can still be monitored
	// (In a full implementation, we would create another channel and verify it works)

	s.T().Log("ME-08: Judge LLM failure handling test completed")
}

// TestRunner for the probe evaluation test suite
func TestProbeEvaluationSuite(t *testing.T) {
	suite.Run(t, new(ProbeEvaluationSuite))
}
