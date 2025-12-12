// Package billing_test - 套餐计费准确性测试
//
// 本测试套件验证套餐消耗的计费准确性，包括：
// - BA-01: 套餐消耗基础计费公式验证
// - BA-02: Fallback时GroupRatio应用
// - BA-03: 流式请求预扣与补差
// - BA-04: 缓存Token特殊计费
// - BA-05: 多模型混合计费累加
package billing_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"one-api/model"
	"scene_test/testutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// BillingAccuracyTestSuite 计费准确性测试套件
type BillingAccuracyTestSuite struct {
	suite.Suite
	server     *testutil.TestServer
	client     *testutil.APIClient
	mockLLM    *testutil.MockLLMServer
	testUserID int
	testToken  string
}

// SetupSuite 在整个测试套件开始前执行一次
func (s *BillingAccuracyTestSuite) SetupSuite() {
	// 启动测试服务器
	server, err := testutil.StartServer(testutil.DefaultConfig())
	s.Require().NoError(err, "Failed to start test server")
	s.server = server
	s.client = testutil.NewAPIClient(server)

	// 启动Mock LLM服务器
	s.mockLLM = testutil.NewMockLLMServer()

	// 设置默认响应（成功响应）
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "This is a test response from mock LLM.",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})

	s.T().Logf("Test server started at: %s", server.BaseURL)
	s.T().Logf("Mock LLM server started at: %s", s.mockLLM.URL())
}

// TearDownSuite 在整个测试套件结束后执行一次
func (s *BillingAccuracyTestSuite) TearDownSuite() {
	if s.mockLLM != nil {
		s.mockLLM.Close()
	}
	if s.server != nil {
		s.server.Stop()
	}
}

// SetupTest 在每个测试用例开始前执行
func (s *BillingAccuracyTestSuite) SetupTest() {
	// 清理测试数据
	testutil.CleanupPackageTestData(s.T())

	// 创建测试用户（vip分组，GroupRatio=2.0）
	user := testutil.CreateTestUser(s.T(), testutil.UserTestData{
		Username: fmt.Sprintf("billing-test-user-%d", time.Now().UnixNano()),
		Group:    "vip",
		Quota:    100000000, // 100M初始余额
	})
	s.testUserID = user.Id

	// 创建测试渠道（指向Mock LLM）
	channel := testutil.CreateTestChannel(s.T(), "test-channel", "vip", "gpt-4", s.mockLLM.URL())

	// 创建测试Token
	tokenModel := testutil.CreateTestToken(s.T(), s.testUserID, "test-token")
	s.testToken = tokenModel.Key

	s.T().Logf("SetupTest: Created test user (ID=%d), channel (ID=%d), token (key=%s)",
		s.testUserID, channel.Id, s.testToken)
}

// TearDownTest 在每个测试用例结束后执行
func (s *BillingAccuracyTestSuite) TearDownTest() {
	// 测试用例级清理会在下一个SetupTest中执行
}

// TestBA01_PackageConsumption_BasicFormula 测试BA-01：套餐消耗基础计费
//
// Test ID: BA-01
// Priority: P0
// Test Scenario: 验证套餐消耗的基础计费公式
// Input: InputTokens=1000, OutputTokens=500, ModelRatio=2.0, GroupRatio=1.5
// Expected Formula: (1000 + 500×1.2) × 2.0 × 1.5 = 4800 quota
// Expected Result:
//   - 套餐扣减4800 quota
//   - DB: total_consumed增加4800
//   - 用户余额不变
func (s *BillingAccuracyTestSuite) TestBA01_PackageConsumption_BasicFormula() {
	s.T().Log("BA-01: Testing package consumption with basic billing formula")

	// Arrange: 创建套餐和订阅
	pkg := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:              "ba01-test-package",
		Priority:          15,
		Quota:             500000000, // 500M月度限额
		HourlyLimit:       20000000,  // 20M小时限额
		FallbackToBalance: true,
		Status:            1,
	})

	sub := testutil.CreateAndActivateSubscription(s.T(), s.testUserID, pkg.Id)
	initialQuota := 100000000 // 用户初始余额

	// 配置Mock LLM响应：InputTokens=1000, OutputTokens=500
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "Test response for BA-01",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})

	// Act: 发起API请求
	apiClient := s.client.WithToken(s.testToken)
	resp, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	})
	s.Require().NoError(err, "ChatCompletion request should not fail")
	defer resp.Body.Close()

	// Assert: 请求成功
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode, "Request should succeed")

	// 等待异步更新完成
	time.Sleep(500 * time.Millisecond)

	// Assert: 套餐扣减正确
	// 计算预期扣减：(1000 + 500×1.2) × 2.0 × 1.5 = (1000 + 600) × 2.0 × 1.5 = 4800
	// 注意：这里的GroupRatio应该使用用户的分组倍率（vip=2.0），而不是1.5
	// 根据设计文档，套餐消耗应用完整的GroupRatio
	modelRatio := 2.0
	groupRatio := testutil.GetGroupRatio("vip") // vip = 2.0
	expectedQuota := testutil.CalculateExpectedQuota(1000, 500, modelRatio, groupRatio)

	updatedSub := testutil.AssertSubscriptionConsumed(s.T(), sub.Id, expectedQuota)
	s.T().Logf("Subscription consumed: %d (expected: %d)", updatedSub.TotalConsumed, expectedQuota)

	// Assert: 用户余额未变
	testutil.AssertUserQuotaUnchanged(s.T(), s.testUserID, initialQuota)

	s.T().Log("BA-01: Test completed - Package consumed correctly, user balance unchanged")
}

// TestBA02_Fallback_AppliesGroupRatio 测试BA-02：Fallback时应用GroupRatio
//
// Test ID: BA-02
// Priority: P0
// Test Scenario: 验证套餐超限后使用用户余额时，GroupRatio正确应用
// Input: 套餐小时限额5M，请求8M，fallback=true
// Expected Result:
//   - 套餐超限，不扣减套餐
//   - 用户余额扣减正确（应用GroupRatio）
//   - 计费公式与套餐一致
func (s *BillingAccuracyTestSuite) TestBA02_Fallback_AppliesGroupRatio() {
	s.T().Log("BA-02: Testing Fallback applies GroupRatio correctly")

	// Arrange: 创建小时限额5M的套餐
	pkg := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:              "ba02-test-package",
		Priority:          15,
		Quota:             500000000, // 500M月度限额
		HourlyLimit:       5000000,   // 5M小时限额（小，容易超限）
		FallbackToBalance: true,      // 允许Fallback
		Status:            1,
	})

	sub := testutil.CreateAndActivateSubscription(s.T(), s.testUserID, pkg.Id)
	initialQuota := 100000000 // 用户初始余额

	// 配置Mock LLM响应：构造一个消耗8M的请求
	// 假设ModelRatio=2.0, GroupRatio=2.0 (vip)
	// 需要的tokens: 8M / (2.0 * 2.0) = 2M tokens
	// InputTokens=1500000, OutputTokens=500000, Total=2000000
	// Quota = 2000000 * 2.0 * 2.0 = 8M
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "Large response for BA-02",
		PromptTokens:     1500000,
		CompletionTokens: 500000,
	})

	// Act: 发起API请求
	apiClient := s.client.WithToken(s.testToken)
	resp, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test large request"},
		},
	})
	s.Require().NoError(err, "ChatCompletion request should not fail")
	defer resp.Body.Close()

	// Assert: 请求成功（通过Fallback）
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode, "Request should succeed via fallback")

	// 等待异步更新完成
	time.Sleep(500 * time.Millisecond)

	// Assert: 套餐未扣减（因为超限）
	updatedSub, err := model.GetSubscriptionById(sub.Id)
	s.Require().NoError(err)
	assert.Equal(s.T(), int64(0), updatedSub.TotalConsumed, "Subscription should not be consumed (exceeded)")

	// Assert: 用户余额扣减正确
	modelRatio := 2.0
	groupRatio := testutil.GetGroupRatio("vip") // vip = 2.0
	expectedQuota := testutil.CalculateExpectedQuota(1500000, 500000, modelRatio, groupRatio)

	finalQuota, err := model.GetUserQuota(s.testUserID, true)
	s.Require().NoError(err)
	expectedFinalQuota := initialQuota - int(expectedQuota)
	assert.Equal(s.T(), expectedFinalQuota, finalQuota,
		fmt.Sprintf("User quota should be deducted by %d", expectedQuota))

	s.T().Logf("BA-02: Test completed - Fallback to user balance, quota deducted: %d", expectedQuota)
}

// TestBA03_StreamRequest_PreConsumeAndAdjust 测试BA-03：流式请求预扣与补差
//
// Test ID: BA-03
// Priority: P0
// Test Scenario: 验证流式请求的预扣与补差机制
// Input: 预估2000 tokens, 实际返回2500 tokens
// Expected Result:
//   - 预扣2000×ratio
//   - 后补差500×ratio
//   - 套餐最终扣减2500×ratio
//   - Redis + DB一致
func (s *BillingAccuracyTestSuite) TestBA03_StreamRequest_PreConsumeAndAdjust() {
	s.T().Log("BA-03: Testing streaming request pre-consume and adjustment")

	// Arrange: 创建套餐和订阅
	pkg := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:              "ba03-test-package",
		Priority:          15,
		Quota:             500000000,
		HourlyLimit:       20000000,
		FallbackToBalance: true,
		Status:            1,
	})

	sub := testutil.CreateAndActivateSubscription(s.T(), s.testUserID, pkg.Id)
	initialQuota := 100000000

	// 配置Mock LLM流式响应
	// 实际返回2500 tokens (InputTokens=1500, OutputTokens=1000)
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		IsStream:         true,
		Content:          "Streaming response for BA-03",
		PromptTokens:     1500,
		CompletionTokens: 1000,
	})

	// Act: 发起流式请求
	apiClient := s.client.WithToken(s.testToken)
	resp, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model:  "gpt-4",
		Stream: true,
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test streaming"},
		},
	})
	s.Require().NoError(err, "ChatCompletion request should not fail")
	defer resp.Body.Close()

	// Assert: 请求成功
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode, "Streaming request should succeed")

	// 读取流式响应（模拟完整接收）
	_, _ = io.ReadAll(resp.Body)

	// 等待异步更新完成
	time.Sleep(500 * time.Millisecond)

	// Assert: 套餐最终扣减正确（2500 tokens）
	modelRatio := 2.0
	groupRatio := testutil.GetGroupRatio("vip")
	expectedQuota := testutil.CalculateExpectedQuota(1500, 1000, modelRatio, groupRatio)

	testutil.AssertSubscriptionConsumed(s.T(), sub.Id, expectedQuota)

	// Assert: 用户余额未变
	testutil.AssertUserQuotaUnchanged(s.T(), s.testUserID, initialQuota)

	s.T().Logf("BA-03: Test completed - Streaming request consumed: %d", expectedQuota)
}

// TestBA04_CachedTokenBilling 测试BA-04：缓存Token计费
//
// Test ID: BA-04
// Priority: P1
// Test Scenario: 验证缓存Token的特殊计费
// Input: cached_tokens=1000, normal_tokens=500
// Formula: cached×0.1 + normal×1.0
// Expected Result: 套餐扣减正确计算
func (s *BillingAccuracyTestSuite) TestBA04_CachedTokenBilling() {
	s.T().Log("BA-04: Testing cached token billing")

	// Arrange: 创建套餐和订阅
	pkg := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:              "ba04-test-package",
		Priority:          15,
		Quota:             500000000,
		HourlyLimit:       20000000,
		FallbackToBalance: true,
		Status:            1,
	})

	sub := testutil.CreateAndActivateSubscription(s.T(), s.testUserID, pkg.Id)
	initialQuota := 100000000

	// 配置Mock LLM响应（带缓存Token）
	// 注意：需要在响应中包含cache usage信息
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "Response with cached tokens for BA-04",
		PromptTokens:     500, // Normal tokens
		CompletionTokens: 500,
		// 缓存Token信息需要在response中体现（通过自定义字段）
		// 这里简化处理，实际需要Mock服务器支持cache usage字段
	})

	// Act: 发起API请求
	apiClient := s.client.WithToken(s.testToken)
	resp, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test cached tokens"},
		},
	})
	s.Require().NoError(err, "ChatCompletion request should not fail")
	defer resp.Body.Close()

	// Assert: 请求成功
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode, "Request should succeed")

	// 等待异步更新完成
	time.Sleep(500 * time.Millisecond)

	// TODO: 实现缓存Token计费逻辑后，验证扣减量
	// 当前仅验证请求成功，完整验证需要后端支持cache usage
	s.T().Log("BA-04: Test completed - Cached token billing (simplified)")
	s.T().Skip("Full cached token billing requires backend support for cache usage field")
}

// TestBA05_MultiModelMixedBilling 测试BA-05：多模型混合计费
//
// Test ID: BA-05
// Priority: P1
// Test Scenario: 验证多模型混合计费的累加正确性
// Input:
//  1. 使用套餐A请求gpt-4（ratio=2.0）
//  2. 使用套餐A请求gpt-3.5（ratio=1.0）
//
// Expected Result: 套餐total_consumed = sum(所有请求)
func (s *BillingAccuracyTestSuite) TestBA05_MultiModelMixedBilling() {
	s.T().Log("BA-05: Testing multi-model mixed billing")

	// Arrange: 创建套餐和订阅
	pkg := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:              "ba05-test-package",
		Priority:          15,
		Quota:             500000000,
		HourlyLimit:       50000000, // 足够大，避免超限
		FallbackToBalance: true,
		Status:            1,
	})

	sub := testutil.CreateAndActivateSubscription(s.T(), s.testUserID, pkg.Id)
	initialQuota := 100000000

	// 创建两个渠道：gpt-4 和 gpt-3.5
	channelGPT4 := testutil.CreateTestChannel(s.T(), "gpt4-channel", "vip", "gpt-4", s.mockLLM.URL())
	channelGPT35 := testutil.CreateTestChannel(s.T(), "gpt35-channel", "vip", "gpt-3.5", s.mockLLM.URL())

	s.T().Logf("Created channels: GPT-4 (ID=%d), GPT-3.5 (ID=%d)", channelGPT4.Id, channelGPT35.Id)

	// Act: 第一次请求 gpt-4
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "GPT-4 response",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})

	apiClient := s.client.WithToken(s.testToken)
	resp1, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test gpt-4"},
		},
	})
	s.Require().NoError(err)
	assert.Equal(s.T(), http.StatusOK, resp1.StatusCode)
	resp1.Body.Close()

	time.Sleep(300 * time.Millisecond)

	// 计算第一次请求的quota
	modelRatio1 := 2.0 // gpt-4
	groupRatio := testutil.GetGroupRatio("vip")
	quota1 := testutil.CalculateExpectedQuota(1000, 500, modelRatio1, groupRatio)

	// Act: 第二次请求 gpt-3.5
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "GPT-3.5 response",
		PromptTokens:     2000,
		CompletionTokens: 1000,
	})

	resp2, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-3.5",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test gpt-3.5"},
		},
	})
	s.Require().NoError(err)
	assert.Equal(s.T(), http.StatusOK, resp2.StatusCode)
	resp2.Body.Close()

	time.Sleep(300 * time.Millisecond)

	// 计算第二次请求的quota
	modelRatio2 := 1.0 // gpt-3.5
	quota2 := testutil.CalculateExpectedQuota(2000, 1000, modelRatio2, groupRatio)

	// Assert: 套餐total_consumed = sum
	expectedTotal := quota1 + quota2
	testutil.AssertSubscriptionConsumed(s.T(), sub.Id, expectedTotal)

	// Assert: 用户余额未变
	testutil.AssertUserQuotaUnchanged(s.T(), s.testUserID, initialQuota)

	s.T().Logf("BA-05: Test completed - Total consumed: %d (gpt-4: %d + gpt-3.5: %d)",
		expectedTotal, quota1, quota2)
}

// TestSuite 运行测试套件
func TestBillingAccuracyTestSuite(t *testing.T) {
	suite.Run(t, new(BillingAccuracyTestSuite))
}
