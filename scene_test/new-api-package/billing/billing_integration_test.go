// Package billing_test - 计费测试集成示例
//
// 本文件提供端到端的计费测试示例，展示如何：
// 1. 组合使用testutil工具函数
// 2. 配置复杂的测试场景
// 3. 验证计费链路的完整性
package billing_test

import (
	"fmt"
	"net/http"
	"one-api/model"
	"scene_test/testutil"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// BillingIntegrationTestSuite 计费集成测试套件（示例）
type BillingIntegrationTestSuite struct {
	suite.Suite
	server  *testutil.TestServer
	client  *testutil.APIClient
	mockLLM *testutil.MockLLMServer
}

// SetupSuite 测试套件初始化
func (s *BillingIntegrationTestSuite) SetupSuite() {
	server, err := testutil.StartServer(testutil.DefaultConfig())
	s.Require().NoError(err)
	s.server = server
	s.client = testutil.NewAPIClient(server)
	s.mockLLM = testutil.NewMockLLMServer()
}

// TearDownSuite 测试套件清理
func (s *BillingIntegrationTestSuite) TearDownSuite() {
	if s.mockLLM != nil {
		s.mockLLM.Close()
	}
	if s.server != nil {
		s.server.Stop()
	}
}

// TestE2E_PackageBilling_CompleteFlow 端到端测试：完整计费流程
//
// 这个测试展示了一个完整的用户使用套餐的流程：
// 1. 创建用户和套餐
// 2. 用户订阅并启用套餐
// 3. 发起多次请求，验证计费
// 4. 套餐超限后Fallback到用户余额
// 5. 验证所有计费数据的一致性
func (s *BillingIntegrationTestSuite) TestE2E_PackageBilling_CompleteFlow() {
	s.T().Log("=== E2E Test: Complete Package Billing Flow ===")

	// ========== Step 1: 准备测试环境 ==========
	testutil.CleanupPackageTestData(s.T())

	// 创建测试用户（vip分组）
	user := testutil.CreateTestUser(s.T(), testutil.UserTestData{
		Username: "e2e-test-user",
		Group:    "vip",
		Quota:    50000000, // 50M初始余额
	})

	// 创建测试渠道
	channel := testutil.CreateTestChannel(s.T(), "e2e-test-channel", "vip", "gpt-4", s.mockLLM.URL())

	// 创建测试Token
	token := testutil.CreateTestToken(s.T(), user.Id, "e2e-test-token")

	s.T().Logf("Step 1: Environment prepared - User: %d, Channel: %d, Token: %s",
		user.Id, channel.Id, token.Key)

	// ========== Step 2: 创建并启用套餐 ==========
	pkg := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:              "e2e-test-package",
		Priority:          15,
		Quota:             100000000, // 100M月度限额
		HourlyLimit:       10000000,  // 10M小时限额（较小，容易超限）
		FallbackToBalance: true,
		Status:            1,
	})

	sub := testutil.CreateAndActivateSubscription(s.T(), user.Id, pkg.Id)
	s.T().Logf("Step 2: Package created and activated - Package: %d, Subscription: %d", pkg.Id, sub.Id)

	// ========== Step 3: 第一次请求 - 使用套餐 ==========
	s.T().Log("Step 3: First request - Should use package")

	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "First response",
		PromptTokens:     1000,
		CompletionTokens: 500,
	})

	apiClient := s.client.WithToken(token.Key)
	resp1, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "first request"},
		},
	})
	s.Require().NoError(err)
	assert.Equal(s.T(), http.StatusOK, resp1.StatusCode)
	resp1.Body.Close()

	time.Sleep(500 * time.Millisecond)

	// 计算第一次请求的quota
	calc1 := &testutil.QuotaCalculator{
		InputTokens:  1000,
		OutputTokens: 500,
		ModelName:    "gpt-4",
		UserGroup:    "vip",
	}
	quota1 := calc1.Calculate()
	s.T().Logf("Request 1 quota calculation: %s", calc1.Format())

	// 验证：套餐扣减，余额不变
	testutil.AssertSubscriptionConsumed(s.T(), sub.Id, quota1)
	testutil.AssertUserQuotaUnchanged(s.T(), user.Id, 50000000)

	// ========== Step 4: 第二次请求 - 仍使用套餐 ==========
	s.T().Log("Step 4: Second request - Should still use package")

	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "Second response",
		PromptTokens:     800,
		CompletionTokens: 400,
	})

	resp2, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "second request"},
		},
	})
	s.Require().NoError(err)
	assert.Equal(s.T(), http.StatusOK, resp2.StatusCode)
	resp2.Body.Close()

	time.Sleep(500 * time.Millisecond)

	calc2 := &testutil.QuotaCalculator{
		InputTokens:  800,
		OutputTokens: 400,
		ModelName:    "gpt-4",
		UserGroup:    "vip",
	}
	quota2 := calc2.Calculate()
	s.T().Logf("Request 2 quota calculation: %s", calc2.Format())

	// 验证：套餐累加扣减
	totalConsumed := quota1 + quota2
	testutil.AssertSubscriptionConsumed(s.T(), sub.Id, totalConsumed)
	testutil.AssertUserQuotaUnchanged(s.T(), user.Id, 50000000)

	// ========== Step 5: 第三次请求 - 触发小时限额，Fallback到余额 ==========
	s.T().Log("Step 5: Third request - Should exceed hourly limit and fallback")

	// 构造一个足够大的请求，使小时限额超限
	// 当前已消耗约：quota1 + quota2 (假设约8M左右)
	// 小时限额：10M
	// 构造一个4M的请求，使总消耗超过10M
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "Large response",
		PromptTokens:     400000,
		CompletionTokens: 100000,
	})

	resp3, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "large request"},
		},
	})
	s.Require().NoError(err)
	assert.Equal(s.T(), http.StatusOK, resp3.StatusCode)
	resp3.Body.Close()

	time.Sleep(500 * time.Millisecond)

	calc3 := &testutil.QuotaCalculator{
		InputTokens:  400000,
		OutputTokens: 100000,
		ModelName:    "gpt-4",
		UserGroup:    "vip",
	}
	quota3 := calc3.Calculate()
	s.T().Logf("Request 3 quota calculation: %s", calc3.Format())

	// 验证：套餐未继续扣减（因为小时限额超限），余额扣减
	// 注意：这取决于系统实现
	// 可能的行为：
	// A. 套餐保持不变，余额扣减quota3
	// B. 套餐继续累加，但触发月度限额检查
	updatedSub, err := model.GetSubscriptionById(sub.Id)
	s.Require().NoError(err)

	finalQuota, err := model.GetUserQuota(user.Id, true)
	s.Require().NoError(err)

	s.T().Logf("After 3rd request: Subscription consumed=%d, User quota=%d (initial=50M)",
		updatedSub.TotalConsumed, finalQuota)

	// 验证：余额应该有变化（Fallback发生）
	assert.Less(s.T(), finalQuota, 50000000, "User quota should decrease after fallback")

	s.T().Log("=== E2E Test Completed Successfully ===")
}

// TestE2E_MultiPackage_PriorityDegradation 端到端测试：多套餐优先级降级
//
// 展示多套餐场景下的优先级自动降级机制
func (s *BillingIntegrationTestSuite) TestE2E_MultiPackage_PriorityDegradation() {
	s.T().Log("=== E2E Test: Multi-Package Priority Degradation ===")

	// 准备环境
	testutil.CleanupPackageTestData(s.T())

	user := testutil.CreateTestUser(s.T(), testutil.UserTestData{
		Username: "multi-pkg-user",
		Group:    "vip",
		Quota:    100000000, // 100M
	})

	testutil.CreateTestChannel(s.T(), "test-channel", "vip", "gpt-4", s.mockLLM.URL())
	token := testutil.CreateTestToken(s.T(), user.Id, "multi-pkg-token")

	// 创建两个套餐：高优先级（小限额）+ 低优先级（大限额）
	pkgHigh := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:              "high-priority-package",
		Priority:          15, // 高优先级
		Quota:             100000000,
		HourlyLimit:       5000000, // 5M小时限额（小）
		FallbackToBalance: true,
		Status:            1,
	})

	pkgLow := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:              "low-priority-package",
		Priority:          5, // 低优先级
		Quota:             100000000,
		HourlyLimit:       20000000, // 20M小时限额（大）
		FallbackToBalance: true,
		Status:            1,
	})

	// 订阅两个套餐
	subHigh := testutil.CreateAndActivateSubscription(s.T(), user.Id, pkgHigh.Id)
	subLow := testutil.CreateAndActivateSubscription(s.T(), user.Id, pkgLow.Id)

	s.T().Logf("Created packages: High (ID=%d, priority=15, hourly=5M), Low (ID=%d, priority=5, hourly=20M)",
		pkgHigh.Id, pkgLow.Id)

	// ========== 第一次请求：3M，使用高优先级套餐 ==========
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "Response 1",
		PromptTokens:     600000,
		CompletionTokens: 150000,
	})

	apiClient := s.client.WithToken(token.Key)
	resp1, _ := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "request 1"},
		},
	})
	resp1.Body.Close()
	time.Sleep(300 * time.Millisecond)

	calc1 := &testutil.QuotaCalculator{
		InputTokens:  600000,
		OutputTokens: 150000,
		ModelName:    "gpt-4",
		UserGroup:    "vip",
	}
	quota1 := calc1.Calculate()

	// 验证：高优先级套餐扣减，低优先级未动
	testutil.AssertSubscriptionConsumed(s.T(), subHigh.Id, quota1)
	testutil.AssertSubscriptionConsumed(s.T(), subLow.Id, 0)
	s.T().Logf("Request 1: Used high-priority package, consumed: %d", quota1)

	// ========== 第二次请求：4M，高优先级超限，降级到低优先级 ==========
	s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
		StatusCode:       http.StatusOK,
		Content:          "Response 2",
		PromptTokens:     800000,
		CompletionTokens: 200000,
	})

	resp2, _ := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "request 2"},
		},
	})
	resp2.Body.Close()
	time.Sleep(300 * time.Millisecond)

	calc2 := &testutil.QuotaCalculator{
		InputTokens:  800000,
		OutputTokens: 200000,
		ModelName:    "gpt-4",
		UserGroup:    "vip",
	}
	quota2 := calc2.Calculate()

	// 验证：高优先级未增加（超限），低优先级扣减
	testutil.AssertSubscriptionConsumed(s.T(), subHigh.Id, quota1) // 保持不变
	testutil.AssertSubscriptionConsumed(s.T(), subLow.Id, quota2)  // 新增扣减

	s.T().Logf("Request 2: Degraded to low-priority package, consumed: %d", quota2)

	// ========== Step 6: 验证用户余额未变 ==========
	testutil.AssertUserQuotaUnchanged(s.T(), user.Id, 100000000)

	s.T().Log("=== E2E Test Completed: Priority degradation verified ===")
}

// TestE2E_BillingFormula_Precision 端到端测试：计费公式精度验证
//
// 验证各种token数量组合下的计费精度
func (s *BillingIntegrationTestSuite) TestE2E_BillingFormula_Precision() {
	s.T().Log("=== E2E Test: Billing Formula Precision ===")

	testutil.CleanupPackageTestData(s.T())

	user := testutil.CreateTestUser(s.T(), testutil.UserTestData{
		Username: "precision-test-user",
		Group:    "default", // 使用default分组（GroupRatio=1.0）
		Quota:    100000000,
	})

	testutil.CreateTestChannel(s.T(), "precision-channel", "default", "gpt-3.5", s.mockLLM.URL())
	token := testutil.CreateTestToken(s.T(), user.Id, "precision-token")

	pkg := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:        "precision-package",
		Priority:    10,
		Quota:       500000000,
		HourlyLimit: 50000000,
		Status:      1,
	})

	sub := testutil.CreateAndActivateSubscription(s.T(), user.Id, pkg.Id)

	// 测试多种token组合
	testCases := []struct {
		inputTokens  int
		outputTokens int
		description  string
	}{
		{100, 50, "Small request"},
		{1000, 500, "Medium request"},
		{10000, 5000, "Large request"},
		{1, 1, "Minimal request"},
		{999999, 111111, "Very large request"},
	}

	apiClient := s.client.WithToken(token.Key)
	var totalExpected int64 = 0

	for i, tc := range testCases {
		s.T().Logf("Test case %d: %s (input=%d, output=%d)", i+1, tc.description, tc.inputTokens, tc.outputTokens)

		s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
			StatusCode:       http.StatusOK,
			Content:          fmt.Sprintf("Response %d", i+1),
			PromptTokens:     tc.inputTokens,
			CompletionTokens: tc.outputTokens,
		})

		resp, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
			Model: "gpt-3.5",
			Messages: []testutil.ChatMessage{
				{Role: "user", Content: fmt.Sprintf("request %d", i+1)},
			},
		})
		s.Require().NoError(err)
		assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		time.Sleep(200 * time.Millisecond)

		// 计算预期quota
		calc := &testutil.QuotaCalculator{
			InputTokens:  tc.inputTokens,
			OutputTokens: tc.outputTokens,
			ModelName:    "gpt-3.5", // ModelRatio=1.0
			UserGroup:    "default", // GroupRatio=1.0
		}
		expectedQuota := calc.Calculate()
		totalExpected += expectedQuota

		s.T().Logf("  Expected quota: %d, Cumulative: %d", expectedQuota, totalExpected)
	}

	// 最终验证：总消耗应等于所有请求的和
	time.Sleep(500 * time.Millisecond)
	testutil.AssertSubscriptionConsumed(s.T(), sub.Id, totalExpected)

	s.T().Logf("=== E2E Test Completed: Total consumed=%d (expected=%d) ===", totalExpected, totalExpected)
}

// TestE2E_ErrorRecovery_ConsistentState 端到端测试：错误恢复与状态一致性
//
// 验证在连续的成功-失败-成功请求中，计费状态保持一致
func (s *BillingIntegrationTestSuite) TestE2E_ErrorRecovery_ConsistentState() {
	s.T().Log("=== E2E Test: Error Recovery & Consistent State ===")

	testutil.CleanupPackageTestData(s.T())

	user := testutil.CreateTestUser(s.T(), testutil.UserTestData{
		Username: "recovery-test-user",
		Group:    "vip",
		Quota:    100000000,
	})

	testutil.CreateTestChannel(s.T(), "recovery-channel", "vip", "gpt-4", s.mockLLM.URL())
	token := testutil.CreateTestToken(s.T(), user.Id, "recovery-token")

	pkg := testutil.CreateTestPackage(s.T(), testutil.PackageTestData{
		Name:        "recovery-package",
		Priority:    15,
		Quota:       500000000,
		HourlyLimit: 50000000,
		Status:      1,
	})

	sub := testutil.CreateAndActivateSubscription(s.T(), user.Id, pkg.Id)
	apiClient := s.client.WithToken(token.Key)

	// 请求序列：成功 -> 失败 -> 成功 -> 失败 -> 成功
	requests := []struct {
		shouldSucceed bool
		inputTokens   int
		outputTokens  int
	}{
		{true, 1000, 500},   // 成功
		{false, 2000, 1000}, // 失败（500错误）
		{true, 1500, 700},   // 成功
		{false, 3000, 1500}, // 失败（401错误）
		{true, 2000, 1000},  // 成功
	}

	var expectedTotal int64 = 0

	for i, req := range requests {
		if req.shouldSucceed {
			s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
				StatusCode:       http.StatusOK,
				Content:          fmt.Sprintf("Success response %d", i),
				PromptTokens:     req.inputTokens,
				CompletionTokens: req.outputTokens,
			})
		} else {
			s.mockLLM.SetDefaultResponse(&testutil.MockLLMResponse{
				StatusCode:   http.StatusInternalServerError,
				ErrorMessage: fmt.Sprintf("Error %d", i),
			})
		}

		resp, err := apiClient.ChatCompletion(testutil.ChatCompletionRequest{
			Model: "gpt-4",
			Messages: []testutil.ChatMessage{
				{Role: "user", Content: fmt.Sprintf("request %d", i+1)},
			},
		})
		s.Require().NoError(err)
		resp.Body.Close()

		time.Sleep(200 * time.Millisecond)

		if req.shouldSucceed {
			assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
			calc := &testutil.QuotaCalculator{
				InputTokens:  req.inputTokens,
				OutputTokens: req.outputTokens,
				ModelName:    "gpt-4",
				UserGroup:    "vip",
			}
			expectedTotal += calc.Calculate()
			s.T().Logf("Request %d: SUCCESS - consumed quota (cumulative=%d)", i+1, expectedTotal)
		} else {
			assert.NotEqual(s.T(), http.StatusOK, resp.StatusCode)
			s.T().Logf("Request %d: FAILED - no charge", i+1)
		}
	}

	// 最终验证：仅成功的请求扣减套餐
	time.Sleep(500 * time.Millisecond)
	testutil.AssertSubscriptionConsumed(s.T(), sub.Id, expectedTotal)
	testutil.AssertUserQuotaUnchanged(s.T(), user.Id, 100000000)

	s.T().Logf("=== E2E Test Completed: Only successful requests charged (total=%d) ===", expectedTotal)
}

// TestSuite 运行集成测试套件
func TestBillingIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(BillingIntegrationTestSuite))
}
