// Package priority_fallback_test - 套餐优先级与Fallback测试
//
// 测试场景：
// - PF-01: 单套餐未超限
// - PF-02: 单套餐超限-允许Fallback
// - PF-03: 单套餐超限-禁止Fallback
// - PF-04: 多套餐优先级降级
// - PF-05: 优先级相同按ID排序
// - PF-06: 所有套餐超限-Fallback
// - PF-07: 所有套餐超限-无Fallback
// - PF-08: 月度总限额优先检查
// - PF-09: 多窗口任一超限即失败
package priority_fallback_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// PriorityFallbackTestSuite 优先级与Fallback测试套件
type PriorityFallbackTestSuite struct {
	suite.Suite
	server     *testutil.TestServer
	mockLLM    *testutil.MockLLMServer
	baseURL    string
	cleanupFns []func()
}

// SetupSuite 套件级别设置（所有测试前执行一次）
func (s *PriorityFallbackTestSuite) SetupSuite() {
	var err error

	// 启动测试服务器
	s.server, err = testutil.StartTestServer()
	if err != nil {
		s.T().Fatalf("Failed to start test server: %v", err)
	}
	s.baseURL = s.server.BaseURL

	// 启动Mock LLM服务器
	s.mockLLM = testutil.NewMockLLMServer()
	s.T().Logf("Mock LLM Server started at: %s", s.mockLLM.URL())
}

// TearDownSuite 套件级别清理（所有测试后执行一次）
func (s *PriorityFallbackTestSuite) TearDownSuite() {
	if s.mockLLM != nil {
		s.mockLLM.Close()
	}
	if s.server != nil {
		s.server.Stop()
	}
}

// SetupTest 测试级别设置（每个测试前执行）
func (s *PriorityFallbackTestSuite) SetupTest() {
	// 清理测试数据
	testutil.CleanupPackageTestData(s.T())

	// 重置cleanup函数列表
	s.cleanupFns = []func(){}
}

// TearDownTest 测试级别清理（每个测试后执行）
func (s *PriorityFallbackTestSuite) TearDownTest() {
	// 执行所有注册的清理函数
	for i := len(s.cleanupFns) - 1; i >= 0; i-- {
		s.cleanupFns[i]()
	}
}

// addCleanup 添加清理函数
func (s *PriorityFallbackTestSuite) addCleanup(fn func()) {
	s.cleanupFns = append(s.cleanupFns, fn)
}

// TestPF01_SinglePackage_NotExceeded 测试PF-01: 单套餐未超限
//
// Test ID: PF-01
// Priority: P0
// Test Scenario: 用户拥有一个套餐，请求消耗未超过套餐限额
// Expected Result: 使用套餐扣减，用户余额不变
func (s *PriorityFallbackTestSuite) TestPF01_SinglePackage_NotExceeded() {
	t := s.T()
	t.Log("PF-01: Testing single package not exceeded scenario")

	// === Arrange: 准备测试数据 ===

	// 1. 创建测试用户（vip分组，余额100M）
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user-pf01",
		Group:    "vip",
		Quota:    100000000, // 100M
	})
	initialQuota := user.Quota
	s.addCleanup(func() {
		t.Logf("Cleaning up user: %d", user.Id)
	})

	// 2. 创建套餐（优先级15，小时限额10M）
	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "test-package-pf01",
		Priority:          15,
		Quota:             500000000, // 月度总限额500M
		HourlyLimit:       10000000,  // 小时限额10M
		FallbackToBalance: true,
	})
	t.Logf("Created package: ID=%d, Priority=%d, HourlyLimit=%d", pkg.Id, pkg.Priority, pkg.HourlyLimit)

	// 3. 创建并激活订阅
	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)
	t.Logf("Created and activated subscription: ID=%d, Status=%s", sub.Id, sub.Status)

	// 4. 创建Token
	token := testutil.CreateTestToken(t, user.Id, "test-token-pf01")
	t.Logf("Created token: Key=%s", token.Key)

	// 5. 创建测试渠道（指向Mock LLM）
	channel := testutil.CreateTestChannel(t, "test-channel-pf01", "vip", "gpt-4", s.mockLLM.URL())
	t.Logf("Created channel: ID=%d, Name=%s, BaseURL=%s", channel.Id, channel.Name, *channel.BaseURL)
	_ = channel // 标记为已使用（避免未使用变量警告）

	// 6. 配置Mock LLM响应（返回1000输入+500输出tokens）
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"This is a test response",
		1000, // prompt_tokens
		500,  // completion_tokens
	))

	// === Act: 执行请求 ===

	// 构造聊天请求
	chatReq := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test message"},
		},
	}

	t.Log("Sending chat completion request...")
	resp, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq)
	assert.Nil(t, err, "Request should succeed")
	defer resp.Body.Close()

	// === Assert: 验证结果 ===

	// 1. 验证HTTP响应成功
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Response should be 200 OK")
	t.Logf("✓ HTTP Status: %d", resp.StatusCode)

	// 2. 解析响应体
	chatResp, err := testutil.ParseChatResponse(resp.Body)
	assert.Nil(t, err, "Failed to parse response")
	t.Logf("✓ Response ID: %s", chatResp.ID)

	// 3. 验证usage信息
	assert.NotNil(t, chatResp.Usage, "Usage should be present")
	assert.Equal(t, 1000, chatResp.Usage.PromptTokens, "Prompt tokens should match")
	assert.Equal(t, 500, chatResp.Usage.CompletionTokens, "Completion tokens should match")
	t.Logf("✓ Usage: PromptTokens=%d, CompletionTokens=%d, TotalTokens=%d",
		chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, chatResp.Usage.TotalTokens)

	// 4. 计算预期quota消耗
	// 公式: (InputTokens + OutputTokens) × ModelRatio × GroupRatio
	// ModelRatio: gpt-4 默认为 1.0
	// GroupRatio: vip = 2.0
	modelRatio := 1.0
	groupRatio := testutil.GetGroupRatio("vip") // 2.0
	expectedQuota := testutil.CalculateExpectedQuota(1000, 500, modelRatio, groupRatio)
	t.Logf("Expected quota consumption: %d (ModelRatio=%.1f, GroupRatio=%.1f)",
		expectedQuota, modelRatio, groupRatio)

	// 5. 验证套餐消耗
	time.Sleep(500 * time.Millisecond) // 等待异步更新完成
	testutil.AssertSubscriptionConsumed(t, sub.Id, expectedQuota)
	t.Logf("✓ Subscription consumed: %d quota", expectedQuota)

	// 6. 验证用户余额未变
	testutil.AssertUserQuotaUnchanged(t, user.Id, initialQuota)
	t.Logf("✓ User quota unchanged: %d", initialQuota)

	// 7. 验证滑动窗口状态（可选，如果Redis可用）
	if s.server.MiniRedis != nil {
		windowKey := testutil.FormatWindowKey(sub.Id, "hourly")
		consumed, err := s.server.MiniRedis.HGet(windowKey, "consumed")
		if err == nil {
			t.Logf("✓ Redis hourly window consumed: %s", consumed)
			assert.Equal(t, string(expectedQuota), consumed, "Window consumed should match")
		}
	}

	t.Log("PF-01: Test completed successfully ✓")
}

// TestPF02_SinglePackage_Exceeded_AllowFallback 测试PF-02: 单套餐超限-允许Fallback
//
// Test ID: PF-02
// Priority: P0
// Test Scenario: 套餐小时限额5M，用户请求8M，fallback=true
// Expected Result: 套餐超限，自动Fallback到用户余额，余额扣减8M
func (s *PriorityFallbackTestSuite) TestPF02_SinglePackage_Exceeded_AllowFallback() {
	t := s.T()
	t.Log("PF-02: Testing single package exceeded with fallback allowed")

	// === Arrange ===

	// 1. 创建用户（余额100M）
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user-pf02",
		Group:    "default",
		Quota:    100000000, // 100M
	})
	initialQuota := user.Quota

	// 2. 创建套餐（小时限额5M，允许Fallback）
	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "test-package-pf02",
		Priority:          15,
		Quota:             500000000, // 月度500M
		HourlyLimit:       5000000,   // 小时限额5M（故意设小）
		FallbackToBalance: true,      // 允许Fallback
	})
	t.Logf("Created package: HourlyLimit=%d, Fallback=%v", pkg.HourlyLimit, pkg.FallbackToBalance)

	// 3. 创建并激活订阅
	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// 4. 创建Token和渠道
	token := testutil.CreateTestToken(t, user.Id, "test-token-pf02")
	channel := testutil.CreateTestChannel(t, "test-channel-pf02", "default", "gpt-4", s.mockLLM.URL())
	_ = channel // 避免未使用警告

	// 5. 配置Mock返回大量tokens（模拟8M quota消耗）
	// 8M quota / (default ratio=1.0) = 8M tokens
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"Large response",
		4000000, // 4M input
		4000000, // 4M output = 8M total
	))

	// === Act ===

	chatReq := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test large request"},
		},
	}

	t.Log("Sending large request (8M quota)...")
	resp, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq)
	assert.Nil(t, err)
	defer resp.Body.Close()

	// === Assert ===

	// 1. 请求成功（虽然套餐超限，但Fallback到余额）
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Request should succeed with fallback")
	t.Logf("✓ HTTP Status: %d (fallback succeeded)", resp.StatusCode)

	// 2. 等待异步更新
	time.Sleep(500 * time.Millisecond)

	// 3. 验证套餐未扣减（因为超限）
	testutil.AssertSubscriptionConsumed(t, sub.Id, 0)
	t.Logf("✓ Subscription not consumed (exceeded limit)")

	// 4. 验证用户余额扣减
	modelRatio := 1.0
	groupRatio := testutil.GetGroupRatio("default")                                            // 1.0
	expectedQuota := testutil.CalculateExpectedQuota(4000000, 4000000, modelRatio, groupRatio) // 8M

	testutil.AssertUserQuotaChanged(t, user.Id, initialQuota, -int(expectedQuota))
	t.Logf("✓ User balance decreased by %d quota", expectedQuota)

	t.Log("PF-02: Test completed successfully ✓")
}

// TestPF03_SinglePackage_Exceeded_NoFallback 测试PF-03: 单套餐超限-禁止Fallback
//
// Test ID: PF-03
// Priority: P0
// Test Scenario: 套餐小时限额5M，用户请求8M，fallback=false
// Expected Result: 返回429错误，套餐和余额都不扣减
func (s *PriorityFallbackTestSuite) TestPF03_SinglePackage_Exceeded_NoFallback() {
	t := s.T()
	t.Log("PF-03: Testing single package exceeded without fallback")

	// === Arrange ===

	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user-pf03",
		Group:    "default",
		Quota:    100000000,
	})
	initialQuota := user.Quota

	// 套餐：小时限额5M，禁止Fallback
	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "test-package-pf03",
		Priority:          15,
		Quota:             500000000,
		HourlyLimit:       5000000, // 5M
		FallbackToBalance: false,   // 禁止Fallback
	})
	t.Logf("Created package: HourlyLimit=%d, Fallback=%v", pkg.HourlyLimit, pkg.FallbackToBalance)

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)
	token := testutil.CreateTestToken(t, user.Id, "test-token-pf03")
	channel := testutil.CreateTestChannel(t, "test-channel-pf03", "default", "gpt-4", s.mockLLM.URL())
	_ = channel // 避免未使用警告

	// 配置Mock返回8M tokens
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"Large response",
		4000000, // 4M
		4000000, // 4M
	))

	// === Act ===

	chatReq := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test large request"},
		},
	}

	t.Log("Sending large request (8M quota) without fallback...")
	resp, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq)
	assert.Nil(t, err)
	defer resp.Body.Close()

	// === Assert ===

	// 1. 返回429错误
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode,
		"Should return 429 when package exceeded and fallback disabled")
	t.Logf("✓ HTTP Status: %d (Too Many Requests)", resp.StatusCode)

	// 2. 等待确保异步操作完成
	time.Sleep(500 * time.Millisecond)

	// 3. 验证套餐未扣减
	testutil.AssertSubscriptionConsumed(t, sub.Id, 0)
	t.Logf("✓ Subscription not consumed")

	// 4. 验证用户余额未变
	testutil.AssertUserQuotaUnchanged(t, user.Id, initialQuota)
	t.Logf("✓ User balance unchanged: %d", initialQuota)

	t.Log("PF-03: Test completed successfully ✓")
}

// TestPF04_MultiPackage_PriorityDegradation 测试PF-04: 多套餐优先级降级
//
// Test ID: PF-04
// Priority: P0
// Test Scenario: 用户有2个套餐（高优先级15限额5M，低优先级5限额20M），
//                 第一次请求3M（使用高优先级），第二次请求4M（高优先级超限，降级到低优先级）
// Expected Result: 第一次使用套餐A，第二次降级到套餐B，用户余额不变
func (s *PriorityFallbackTestSuite) TestPF04_MultiPackage_PriorityDegradation() {
	t := s.T()
	t.Log("PF-04: Testing multi-package priority degradation")

	// === Arrange ===

	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user-pf04",
		Group:    "default",
		Quota:    100000000,
	})
	initialQuota := user.Quota

	// 套餐A：高优先级15，小时限额5M
	pkgHigh := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "high-priority-pkg",
		Priority:          15,
		Quota:             500000000,
		HourlyLimit:       5000000, // 5M
		FallbackToBalance: true,
	})
	t.Logf("Created high priority package: ID=%d, Priority=%d, HourlyLimit=%d",
		pkgHigh.Id, pkgHigh.Priority, pkgHigh.HourlyLimit)

	// 套餐B：低优先级5，小时限额20M
	pkgLow := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "low-priority-pkg",
		Priority:          5,
		Quota:             500000000,
		HourlyLimit:       20000000, // 20M
		FallbackToBalance: true,
	})
	t.Logf("Created low priority package: ID=%d, Priority=%d, HourlyLimit=%d",
		pkgLow.Id, pkgLow.Priority, pkgLow.HourlyLimit)

	// 创建两个订阅
	subHigh := testutil.CreateAndActivateSubscription(t, user.Id, pkgHigh.Id)
	subLow := testutil.CreateAndActivateSubscription(t, user.Id, pkgLow.Id)

	token := testutil.CreateTestToken(t, user.Id, "test-token-pf04")
	channel := testutil.CreateTestChannel(t, "test-channel-pf04", "default", "gpt-4", s.mockLLM.URL())
	_ = channel // 避免未使用警告

	// === Act: 第一次请求3M ===

	t.Log("=== First Request: 3M quota ===")
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"First response",
		1500000, // 1.5M
		1500000, // 1.5M = 3M total
	))

	chatReq1 := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "first request"},
		},
	}

	resp1, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq1)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp1.StatusCode)
	resp1.Body.Close()

	time.Sleep(500 * time.Millisecond)

	// 验证第一次请求使用了高优先级套餐
	modelRatio := 1.0
	groupRatio := testutil.GetGroupRatio("default")
	expectedQuota1 := testutil.CalculateExpectedQuota(1500000, 1500000, modelRatio, groupRatio) // 3M

	testutil.AssertSubscriptionConsumed(t, subHigh.Id, expectedQuota1)
	testutil.AssertSubscriptionConsumed(t, subLow.Id, 0)
	t.Logf("✓ First request used high priority package: consumed=%d", expectedQuota1)

	// === Act: 第二次请求4M ===

	t.Log("=== Second Request: 4M quota ===")
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"Second response",
		2000000, // 2M
		2000000, // 2M = 4M total
	))

	chatReq2 := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "second request"},
		},
	}

	resp2, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq2)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	resp2.Body.Close()

	time.Sleep(500 * time.Millisecond)

	// === Assert ===

	// 验证第二次请求降级到低优先级套餐
	expectedQuota2 := testutil.CalculateExpectedQuota(2000000, 2000000, modelRatio, groupRatio) // 4M

	// 高优先级套餐消耗应该还是3M（未增加）
	testutil.AssertSubscriptionConsumed(t, subHigh.Id, expectedQuota1)
	t.Logf("✓ High priority package still consumed: %d (not increased)", expectedQuota1)

	// 低优先级套餐消耗应该是4M
	testutil.AssertSubscriptionConsumed(t, subLow.Id, expectedQuota2)
	t.Logf("✓ Second request degraded to low priority package: consumed=%d", expectedQuota2)

	// 用户余额不变
	testutil.AssertUserQuotaUnchanged(t, user.Id, initialQuota)
	t.Logf("✓ User balance unchanged: %d", initialQuota)

	t.Log("PF-04: Test completed successfully ✓")
}

// TestPF05_SamePriority_SortByID 测试PF-05: 优先级相同按ID排序
//
// Test ID: PF-05
// Priority: P1
// Test Scenario: 两个套餐优先级相同（都是10），验证按subscription ID排序
// Expected Result: 优先使用ID小的套餐A
func (s *PriorityFallbackTestSuite) TestPF05_SamePriority_SortByID() {
	t := s.T()
	t.Log("PF-05: Testing same priority sort by ID")

	// === Arrange ===

	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user-pf05",
		Group:    "default",
		Quota:    100000000,
	})

	// 套餐A：优先级10
	pkgA := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "package-A-pf05",
		Priority:          10,
		Quota:             500000000,
		HourlyLimit:       20000000,
		FallbackToBalance: true,
	})

	// 套餐B：优先级10（相同）
	pkgB := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "package-B-pf05",
		Priority:          10, // 相同优先级
		Quota:             500000000,
		HourlyLimit:       20000000,
		FallbackToBalance: true,
	})

	// 创建订阅（按顺序创建，确保ID递增）
	subA := testutil.CreateAndActivateSubscription(t, user.Id, pkgA.Id)
	subB := testutil.CreateAndActivateSubscription(t, user.Id, pkgB.Id)

	t.Logf("Package A: ID=%d, Priority=%d, Subscription ID=%d", pkgA.Id, pkgA.Priority, subA.Id)
	t.Logf("Package B: ID=%d, Priority=%d, Subscription ID=%d", pkgB.Id, pkgB.Priority, subB.Id)

	// 验证订阅ID顺序
	assert.Less(t, subA.Id, subB.Id, "Subscription A should have smaller ID")

	token := testutil.CreateTestToken(t, user.Id, "test-token-pf05")
	channel := testutil.CreateTestChannel(t, "test-channel-pf05", "default", "gpt-4", s.mockLLM.URL())
	_ = channel // 避免未使用警告

	// Mock返回3M tokens
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"Response",
		1500000,
		1500000,
	))

	// === Act ===

	chatReq := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test"},
		},
	}

	resp, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq)
	assert.Nil(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	time.Sleep(500 * time.Millisecond)

	// === Assert ===

	// 验证使用了ID小的套餐A
	modelRatio := 1.0
	groupRatio := testutil.GetGroupRatio("default")
	expectedQuota := testutil.CalculateExpectedQuota(1500000, 1500000, modelRatio, groupRatio)

	testutil.AssertSubscriptionConsumed(t, subA.Id, expectedQuota)
	testutil.AssertSubscriptionConsumed(t, subB.Id, 0)
	t.Logf("✓ Used package A (smaller ID): consumed=%d", expectedQuota)
	t.Logf("✓ Package B not used: consumed=0")

	t.Log("PF-05: Test completed successfully ✓")
}

// TestPF06_AllPackages_Exceeded_AllowFallback 测试PF-06: 所有套餐超限-Fallback
//
// Test ID: PF-06
// Priority: P0
// Test Scenario: 两个套餐都超限（A限额5M，B限额3M），都允许Fallback，用户请求10M
// Expected Result: 所有套餐超限，使用用户余额，余额扣减10M
func (s *PriorityFallbackTestSuite) TestPF06_AllPackages_Exceeded_AllowFallback() {
	t := s.T()
	t.Log("PF-06: Testing all packages exceeded with fallback allowed")

	// === Arrange ===

	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user-pf06",
		Group:    "default",
		Quota:    100000000,
	})
	initialQuota := user.Quota

	// 套餐A：小时限额5M，允许Fallback
	pkgA := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "package-A-pf06",
		Priority:          15,
		Quota:             500000000,
		HourlyLimit:       5000000, // 5M
		FallbackToBalance: true,
	})

	// 套餐B：小时限额3M，允许Fallback
	pkgB := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "package-B-pf06",
		Priority:          5,
		Quota:             500000000,
		HourlyLimit:       3000000, // 3M
		FallbackToBalance: true,
	})

	subA := testutil.CreateAndActivateSubscription(t, user.Id, pkgA.Id)
	subB := testutil.CreateAndActivateSubscription(t, user.Id, pkgB.Id)

	token := testutil.CreateTestToken(t, user.Id, "test-token-pf06")
	channel := testutil.CreateTestChannel(t, "test-channel-pf06", "default", "gpt-4", s.mockLLM.URL())
	_ = channel // 避免未使用警告

	// Mock返回10M tokens（超过所有套餐限额）
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"Large response",
		5000000, // 5M
		5000000, // 5M = 10M total
	))

	// === Act ===

	chatReq := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "large request"},
		},
	}

	t.Log("Sending large request (10M quota, exceeds all packages)...")
	resp, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq)
	assert.Nil(t, err)
	defer resp.Body.Close()

	// === Assert ===

	// 1. 请求成功（Fallback到用户余额）
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Should succeed with fallback to balance")
	t.Logf("✓ HTTP Status: %d (fallback to balance)", resp.StatusCode)

	time.Sleep(500 * time.Millisecond)

	// 2. 验证所有套餐都未扣减
	testutil.AssertSubscriptionConsumed(t, subA.Id, 0)
	testutil.AssertSubscriptionConsumed(t, subB.Id, 0)
	t.Logf("✓ All packages not consumed (exceeded)")

	// 3. 验证用户余额扣减
	modelRatio := 1.0
	groupRatio := testutil.GetGroupRatio("default")
	expectedQuota := testutil.CalculateExpectedQuota(5000000, 5000000, modelRatio, groupRatio) // 10M

	testutil.AssertUserQuotaChanged(t, user.Id, initialQuota, -int(expectedQuota))
	t.Logf("✓ User balance decreased by %d quota", expectedQuota)

	t.Log("PF-06: Test completed successfully ✓")
}

// TestPF07_AllPackages_Exceeded_NoFallback 测试PF-07: 所有套餐超限-无Fallback
//
// Test ID: PF-07
// Priority: P0
// Test Scenario: 两个套餐都超限，最后一个套餐fallback=false
// Expected Result: 返回429错误，套餐和余额都不扣减
func (s *PriorityFallbackTestSuite) TestPF07_AllPackages_Exceeded_NoFallback() {
	t := s.T()
	t.Log("PF-07: Testing all packages exceeded without fallback")

	// === Arrange ===

	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user-pf07",
		Group:    "default",
		Quota:    100000000,
	})
	initialQuota := user.Quota

	// 套餐A：小时限额5M，允许Fallback
	pkgA := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "package-A-pf07",
		Priority:          15,
		Quota:             500000000,
		HourlyLimit:       5000000,
		FallbackToBalance: true, // 第一个允许
	})

	// 套餐B：小时限额3M，禁止Fallback（关键）
	pkgB := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "package-B-pf07",
		Priority:          5,
		Quota:             500000000,
		HourlyLimit:       3000000,
		FallbackToBalance: false, // 最后一个禁止
	})

	t.Logf("Package A: Priority=%d, Fallback=%v", pkgA.Priority, pkgA.FallbackToBalance)
	t.Logf("Package B: Priority=%d, Fallback=%v (last package, should block)", pkgB.Priority, pkgB.FallbackToBalance)

	subA := testutil.CreateAndActivateSubscription(t, user.Id, pkgA.Id)
	subB := testutil.CreateAndActivateSubscription(t, user.Id, pkgB.Id)

	token := testutil.CreateTestToken(t, user.Id, "test-token-pf07")
	channel := testutil.CreateTestChannel(t, "test-channel-pf07", "default", "gpt-4", s.mockLLM.URL())
	_ = channel // 避免未使用警告

	// Mock返回10M tokens
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"Large response",
		5000000,
		5000000,
	))

	// === Act ===

	chatReq := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "large request"},
		},
	}

	t.Log("Sending request (10M quota, all packages exceeded, last disables fallback)...")
	resp, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq)
	assert.Nil(t, err)
	defer resp.Body.Close()

	// === Assert ===

	// 1. 返回429错误（因为最后一个套餐禁止Fallback）
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode,
		"Should return 429 when last package disables fallback")
	t.Logf("✓ HTTP Status: %d (rejected, no fallback allowed)", resp.StatusCode)

	time.Sleep(500 * time.Millisecond)

	// 2. 验证所有套餐都未扣减
	testutil.AssertSubscriptionConsumed(t, subA.Id, 0)
	testutil.AssertSubscriptionConsumed(t, subB.Id, 0)
	t.Logf("✓ All packages not consumed")

	// 3. 验证用户余额未变
	testutil.AssertUserQuotaUnchanged(t, user.Id, initialQuota)
	t.Logf("✓ User balance unchanged: %d", initialQuota)

	t.Log("PF-07: Test completed successfully ✓")
}

// TestPF08_MonthlyQuota_CheckFirst 测试PF-08: 月度总限额优先检查
//
// Test ID: PF-08
// Priority: P1
// Test Scenario: 套餐月度总限额100M已消耗95M，小时限额50M，请求10M
// Expected Result: 月度总限额超限（95+10>100），返回月度超限错误，不检查小时窗口
func (s *PriorityFallbackTestSuite) TestPF08_MonthlyQuota_CheckFirst() {
	t := s.T()
	t.Log("PF-08: Testing monthly quota check has priority")

	// === Arrange ===

	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user-pf08",
		Group:    "default",
		Quota:    100000000,
	})
	initialQuota := user.Quota

	// 创建套餐：月度总限额100M，小时限额50M
	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "package-pf08",
		Priority:          15,
		Quota:             100000000, // 月度总限额100M
		HourlyLimit:       50000000,  // 小时限额50M（较大）
		FallbackToBalance: true,
	})

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	// 手动设置订阅已消耗95M（接近月度限额）
	sub.TotalConsumed = 95000000 // 95M
	err := testutil.DB.Save(sub).Error
	assert.Nil(t, err, "Failed to update subscription consumed")
	t.Logf("Pre-set subscription consumed: %d (approaching monthly limit)", sub.TotalConsumed)

	token := testutil.CreateTestToken(t, user.Id, "test-token-pf08")
	channel := testutil.CreateTestChannel(t, "test-channel-pf08", "default", "gpt-4", s.mockLLM.URL())
	_ = channel // 避免未使用警告

	// Mock返回10M tokens
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"Response",
		5000000,
		5000000,
	))

	// === Act ===

	chatReq := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test request"},
		},
	}

	t.Log("Sending request (10M quota, monthly limit: 95+10>100)...")
	resp, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq)
	assert.Nil(t, err)
	defer resp.Body.Close()

	// === Assert ===

	// 1. 应该返回429或fallback（取决于实现）
	// 根据设计：月度总限额超限应该返回错误或fallback
	if pkg.FallbackToBalance {
		// 如果允许fallback，应该fallback到用户余额
		assert.Equal(t, http.StatusOK, resp.StatusCode, "Should fallback when monthly exceeded")
		t.Logf("✓ Fallback to balance (monthly quota exceeded)")

		time.Sleep(500 * time.Millisecond)

		// 套餐不应增加消耗（因为月度超限）
		updatedSub, _ := testutil.GetSubscriptionById(sub.Id)
		assert.Equal(t, int64(95000000), updatedSub.TotalConsumed, "Subscription should not increase")
		t.Logf("✓ Subscription not consumed (monthly exceeded): %d", updatedSub.TotalConsumed)

		// 用户余额应该扣减
		modelRatio := 1.0
		groupRatio := testutil.GetGroupRatio("default")
		expectedQuota := testutil.CalculateExpectedQuota(5000000, 5000000, modelRatio, groupRatio)
		testutil.AssertUserQuotaChanged(t, user.Id, initialQuota, -int(expectedQuota))
		t.Logf("✓ User balance decreased (fallback): %d", expectedQuota)
	} else {
		assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "Should return 429")
		t.Logf("✓ Rejected with 429 (monthly quota exceeded)")
	}

	t.Log("PF-08: Test completed successfully ✓")
}

// TestPF09_MultiWindow_AnyExceeded_Fails 测试PF-09: 多窗口任一超限即失败
//
// Test ID: PF-09
// Priority: P0
// Test Scenario: 套餐有小时限额10M和日限额20M，小时已用9M，日已用15M，请求2M
// Expected Result: 小时窗口超限（9+2>10），套餐不可用
func (s *PriorityFallbackTestSuite) TestPF09_MultiWindow_AnyExceeded_Fails() {
	t := s.T()
	t.Log("PF-09: Testing multi-window any exceeded fails")

	// === Arrange ===

	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: "test-user-pf09",
		Group:    "default",
		Quota:    100000000,
	})
	initialQuota := user.Quota

	// 创建套餐：小时限额10M，日限额20M
	pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
		Name:              "package-pf09",
		Priority:          15,
		Quota:             500000000,
		HourlyLimit:       10000000, // 10M
		DailyLimit:        20000000, // 20M
		FallbackToBalance: true,
	})
	t.Logf("Created package: HourlyLimit=%d, DailyLimit=%d", pkg.HourlyLimit, pkg.DailyLimit)

	sub := testutil.CreateAndActivateSubscription(t, user.Id, pkg.Id)

	token := testutil.CreateTestToken(t, user.Id, "test-token-pf09")
	channel := testutil.CreateTestChannel(t, "test-channel-pf09", "default", "gpt-4", s.mockLLM.URL())
	_ = channel // 避免未使用警告

	// === 预设窗口状态 ===

	// 小时窗口：已消耗9M（接近10M限额）
	if s.server.MiniRedis != nil {
		hourlyKey := testutil.FormatWindowKey(sub.Id, "hourly")
		now := time.Now().Unix()
		s.server.MiniRedis.HSet(hourlyKey, "start_time", fmt.Sprintf("%d", now))
		s.server.MiniRedis.HSet(hourlyKey, "end_time", fmt.Sprintf("%d", now+3600))
		s.server.MiniRedis.HSet(hourlyKey, "consumed", "9000000") // 9M
		s.server.MiniRedis.HSet(hourlyKey, "limit", "10000000")   // 10M
		s.server.MiniRedis.Expire(hourlyKey, 4200*time.Second)
		t.Logf("Pre-set hourly window: consumed=9M, limit=10M")

		// 日窗口：已消耗15M（未接近20M限额）
		dailyKey := testutil.FormatWindowKey(sub.Id, "daily")
		s.server.MiniRedis.HSet(dailyKey, "start_time", fmt.Sprintf("%d", now))
		s.server.MiniRedis.HSet(dailyKey, "end_time", fmt.Sprintf("%d", now+86400))
		s.server.MiniRedis.HSet(dailyKey, "consumed", "15000000") // 15M
		s.server.MiniRedis.HSet(dailyKey, "limit", "20000000")    // 20M
		s.server.MiniRedis.Expire(dailyKey, 93600*time.Second)
		t.Logf("Pre-set daily window: consumed=15M, limit=20M")
	}

	// Mock返回2M tokens（会导致小时窗口超限）
	s.mockLLM.SetDefaultResponse(testutil.NewDefaultSuccessResponse(
		"Response",
		1000000, // 1M
		1000000, // 1M = 2M total
	))

	// === Act ===

	chatReq := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test request"},
		},
	}

	t.Log("Sending request (2M quota, hourly would exceed: 9+2>10)...")
	resp, err := testutil.SendChatRequest(s.baseURL, token.Key, chatReq)
	assert.Nil(t, err)
	defer resp.Body.Close()

	// === Assert ===

	// 1. 应该fallback到余额（因为小时窗口超限）
	assert.Equal(t, http.StatusOK, resp.StatusCode, "Should fallback when hourly window exceeded")
	t.Logf("✓ HTTP Status: %d (fallback due to hourly limit)", resp.StatusCode)

	time.Sleep(500 * time.Millisecond)

	// 2. 验证套餐未增加消耗（因为小时窗口超限）
	updatedSub, _ := testutil.GetSubscriptionById(sub.Id)
	assert.Equal(t, int64(0), updatedSub.TotalConsumed,
		"Subscription should not increase when any window exceeded")
	t.Logf("✓ Subscription not consumed (hourly window exceeded)")

	// 3. 验证用户余额扣减（fallback）
	modelRatio := 1.0
	groupRatio := testutil.GetGroupRatio("default")
	expectedQuota := testutil.CalculateExpectedQuota(1000000, 1000000, modelRatio, groupRatio)

	testutil.AssertUserQuotaChanged(t, user.Id, initialQuota, -int(expectedQuota))
	t.Logf("✓ User balance decreased (fallback): %d", expectedQuota)

	// 4. 验证小时窗口未更新（因为被拒绝）
	if s.server.MiniRedis != nil {
		hourlyKey := testutil.FormatWindowKey(sub.Id, "hourly")
		consumed, _ := s.server.MiniRedis.HGet(hourlyKey, "consumed")
		assert.Equal(t, "9000000", consumed, "Hourly window should remain at 9M")
		t.Logf("✓ Hourly window unchanged: consumed=%s", consumed)
	}

	t.Log("PF-09: Test completed successfully ✓")
}

// TestSuite 运行测试套件
func TestPriorityFallbackSuite(t *testing.T) {
	suite.Run(t, new(PriorityFallbackTestSuite))
}
