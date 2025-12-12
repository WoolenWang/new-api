// Package testutil - 计费测试辅助函数
//
// 本文件提供计费测试专用的辅助函数，包括：
// - 精确的quota计算
// - 滑动窗口状态断言
// - 计费相关的自定义断言
package testutil

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

// BillingTestContext 计费测试上下文
type BillingTestContext struct {
	T              *testing.T
	SubscriptionID int
	UserID         int
	InitialQuota   int
	ModelRatio     float64
	GroupRatio     float64
}

// CalculateQuotaWithRatios 使用指定的倍率计算quota
func CalculateQuotaWithRatios(inputTokens, outputTokens int, modelRatio, groupRatio float64) int64 {
	completionRatio := 1.0 // 默认补全倍率
	baseTokens := float64(inputTokens) + float64(outputTokens)*completionRatio
	quota := baseTokens * modelRatio * groupRatio
	return int64(quota)
}

// CalculateQuotaWithCompletionRatio 使用自定义补全倍率计算quota
func CalculateQuotaWithCompletionRatio(inputTokens, outputTokens int, completionRatio, modelRatio, groupRatio float64) int64 {
	baseTokens := float64(inputTokens) + float64(outputTokens)*completionRatio
	quota := baseTokens * modelRatio * groupRatio
	return int64(quota)
}

// CalculateCachedTokenQuota 计算带缓存Token的quota
// Formula: (cached_tokens × 0.1 + normal_tokens) × ModelRatio × GroupRatio
func CalculateCachedTokenQuota(cachedTokens, normalInputTokens, normalOutputTokens int, modelRatio, groupRatio float64) int64 {
	completionRatio := 1.0
	// 缓存Token按0.1倍计费
	cachedCost := float64(cachedTokens) * 0.1
	// 普通Token正常计费
	normalCost := float64(normalInputTokens) + float64(normalOutputTokens)*completionRatio
	totalCost := (cachedCost + normalCost) * modelRatio * groupRatio
	return int64(totalCost)
}

// AssertSubscriptionConsumedInRange 断言订阅消耗在指定范围内
// 用于处理浮点数计算误差
func AssertSubscriptionConsumedInRange(t *testing.T, subscriptionID int, expectedMin, expectedMax int64) *model.Subscription {
	sub, err := model.GetSubscriptionById(subscriptionID)
	assert.Nil(t, err, "Failed to get subscription")
	assert.GreaterOrEqual(t, sub.TotalConsumed, expectedMin,
		fmt.Sprintf("Subscription consumed should be >= %d", expectedMin))
	assert.LessOrEqual(t, sub.TotalConsumed, expectedMax,
		fmt.Sprintf("Subscription consumed should be <= %d", expectedMax))
	return sub
}

// AssertSubscriptionNotConsumed 断言订阅未被消耗（失败场景）
func AssertSubscriptionNotConsumed(t *testing.T, subscriptionID int) {
	sub, err := model.GetSubscriptionById(subscriptionID)
	assert.Nil(t, err, "Failed to get subscription")
	assert.Equal(t, int64(0), sub.TotalConsumed,
		"Subscription should not be consumed in failure scenario")
}

// AssertUserQuotaDeducted 断言用户余额扣减正确
func AssertUserQuotaDeducted(t *testing.T, userID int, initialQuota int, expectedDeduction int64) int {
	finalQuota, err := model.GetUserQuota(userID, true)
	assert.Nil(t, err, "Failed to get user quota")
	expectedFinal := initialQuota - int(expectedDeduction)
	assert.Equal(t, expectedFinal, finalQuota,
		fmt.Sprintf("User quota should be deducted by %d (from %d to %d), got %d",
			expectedDeduction, initialQuota, expectedFinal, finalQuota))
	return finalQuota
}

// AssertUserQuotaInRange 断言用户余额在指定范围内
func AssertUserQuotaInRange(t *testing.T, userID int, minQuota, maxQuota int) {
	finalQuota, err := model.GetUserQuota(userID, true)
	assert.Nil(t, err, "Failed to get user quota")
	assert.GreaterOrEqual(t, finalQuota, minQuota,
		fmt.Sprintf("User quota should be >= %d", minQuota))
	assert.LessOrEqual(t, finalQuota, maxQuota,
		fmt.Sprintf("User quota should be <= %d", maxQuota))
}

// BillingScenario 计费场景配置
type BillingScenario struct {
	Description       string
	InputTokens       int
	OutputTokens      int
	ModelRatio        float64
	GroupRatio        float64
	CompletionRatio   float64
	ExpectedQuota     int64
	ShouldUsePackage  bool
	ShouldUseBalance  bool
	ShouldFail        bool
	ExpectedErrorCode int
}

// VerifyBillingScenario 验证计费场景
func VerifyBillingScenario(t *testing.T, ctx *BillingTestContext, scenario BillingScenario) {
	t.Logf("Verifying scenario: %s", scenario.Description)

	// 获取初始状态
	initialSub, err := model.GetSubscriptionById(ctx.SubscriptionID)
	assert.Nil(t, err)
	initialConsumed := initialSub.TotalConsumed

	// 场景执行后的验证
	// （注意：这个函数假设场景已经执行，仅做验证）

	time.Sleep(500 * time.Millisecond) // 等待异步更新

	if scenario.ShouldFail {
		// 失败场景：套餐和余额都不变
		AssertSubscriptionConsumed(t, ctx.SubscriptionID, initialConsumed)
		AssertUserQuotaUnchanged(t, ctx.UserID, ctx.InitialQuota)
		t.Logf("✓ Scenario verified: Request failed, no charge")
		return
	}

	if scenario.ShouldUsePackage {
		// 使用套餐：套餐扣减，余额不变
		expectedTotal := initialConsumed + scenario.ExpectedQuota
		AssertSubscriptionConsumed(t, ctx.SubscriptionID, expectedTotal)
		AssertUserQuotaUnchanged(t, ctx.UserID, ctx.InitialQuota)
		t.Logf("✓ Scenario verified: Package consumed %d quota", scenario.ExpectedQuota)
		return
	}

	if scenario.ShouldUseBalance {
		// 使用余额：套餐不变，余额扣减
		AssertSubscriptionConsumed(t, ctx.SubscriptionID, initialConsumed)
		expectedFinal := ctx.InitialQuota - int(scenario.ExpectedQuota)
		AssertUserQuota(t, ctx.UserID, expectedFinal)
		t.Logf("✓ Scenario verified: User balance consumed %d quota", scenario.ExpectedQuota)
		return
	}

	t.Errorf("Invalid scenario configuration: no expected behavior specified")
}

// ModelRatioConfig 模型倍率配置
var ModelRatioConfig = map[string]float64{
	"gpt-4":           2.0,
	"gpt-4-turbo":     2.0,
	"gpt-3.5":         1.0,
	"gpt-3.5-turbo":   1.0,
	"claude-3-opus":   2.5,
	"claude-3-sonnet": 1.5,
	"gemini-pro":      1.0,
}

// GetModelRatio 获取模型倍率
func GetModelRatio(modelName string) float64 {
	if ratio, ok := ModelRatioConfig[modelName]; ok {
		return ratio
	}
	return 1.0 // 默认倍率
}

// GetEffectiveGroupRatio 获取有效的分组倍率（考虑反降级等逻辑）
// 这是一个简化版本，实际逻辑可能更复杂
func GetEffectiveGroupRatio(userGroup, billingGroup string) float64 {
	// 简化处理：直接返回billingGroup的倍率
	// 实际应该实现：
	// 1. 跨组倍率（如svip用户使用default分组渠道时的特殊费率）
	// 2. 反降级保护（can_downgrade=false）
	return GetGroupRatio(billingGroup)
}

// PackageBillingAssertion 套餐计费断言辅助结构
type PackageBillingAssertion struct {
	T                  *testing.T
	SubscriptionID     int
	UserID             int
	InitialQuota       int
	InitialConsumed    int64
	ExpectedDelta      int64
	ExpectQuotaChanged bool
}

// Verify 执行验证
func (a *PackageBillingAssertion) Verify() {
	// 验证订阅消耗
	expectedConsumed := a.InitialConsumed + a.ExpectedDelta
	AssertSubscriptionConsumed(a.T, a.SubscriptionID, expectedConsumed)

	// 验证用户余额
	if a.ExpectQuotaChanged {
		// Fallback场景：用户余额应该变化
		finalQuota, err := model.GetUserQuota(a.UserID, true)
		assert.Nil(a.T, err)
		a.T.Logf("User quota changed: %d -> %d", a.InitialQuota, finalQuota)
	} else {
		// 正常套餐消耗：用户余额不变
		AssertUserQuotaUnchanged(a.T, a.UserID, a.InitialQuota)
	}
}

// QuotaCalculator 计费计算器
type QuotaCalculator struct {
	InputTokens     int
	OutputTokens    int
	CachedTokens    int
	ModelName       string
	UserGroup       string
	BillingGroup    string
	CompletionRatio float64
}

// Calculate 计算最终quota
func (qc *QuotaCalculator) Calculate() int64 {
	// 设置默认值
	if qc.CompletionRatio == 0 {
		qc.CompletionRatio = 1.0
	}
	if qc.BillingGroup == "" {
		qc.BillingGroup = qc.UserGroup
	}

	modelRatio := GetModelRatio(qc.ModelName)
	groupRatio := GetEffectiveGroupRatio(qc.UserGroup, qc.BillingGroup)

	if qc.CachedTokens > 0 {
		// 有缓存Token
		return CalculateCachedTokenQuota(qc.CachedTokens, qc.InputTokens, qc.OutputTokens, modelRatio, groupRatio)
	}

	// 普通计费
	return CalculateQuotaWithCompletionRatio(qc.InputTokens, qc.OutputTokens, qc.CompletionRatio, modelRatio, groupRatio)
}

// Format 格式化输出计算过程
func (qc *QuotaCalculator) Format() string {
	quota := qc.Calculate()
	modelRatio := GetModelRatio(qc.ModelName)
	groupRatio := GetEffectiveGroupRatio(qc.UserGroup, qc.BillingGroup)

	if qc.CachedTokens > 0 {
		return fmt.Sprintf(
			"Quota = (cached:%d×0.1 + input:%d + output:%d×%.1f) × model:%.1f × group:%.1f = %d",
			qc.CachedTokens, qc.InputTokens, qc.OutputTokens, qc.CompletionRatio,
			modelRatio, groupRatio, quota)
	}

	return fmt.Sprintf(
		"Quota = (input:%d + output:%d×%.1f) × model:%.1f × group:%.1f = %d",
		qc.InputTokens, qc.OutputTokens, qc.CompletionRatio,
		modelRatio, groupRatio, quota)
}
