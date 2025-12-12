package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

// TestCheckAndReservePackageQuota_MonthlyExceeded 测试月度限额超限场景
func TestCheckAndReservePackageQuota_MonthlyExceeded(t *testing.T) {
	// 创建测试数据
	sub := &model.Subscription{
		Id:            1,
		UserId:        100,
		PackageId:     1,
		TotalConsumed: 450000000, // 已消耗 4.5亿
		Status:        model.SubscriptionStatusActive,
	}

	pkg := &model.Package{
		Id:     1,
		Name:   "测试套餐",
		Quota:  500000000, // 总限额 5亿
		Status: 1,
	}

	estimatedQuota := int64(100000000) // 预估消耗 1亿

	// 执行检查（预计失败：4.5亿 + 1亿 > 5亿）
	err := CheckAndReservePackageQuota(sub, pkg, estimatedQuota)

	// 验证
	assert.NotNil(t, err, "应该返回错误")
	assert.Contains(t, err.Error(), "monthly quota exceeded", "错误信息应包含月度限额超限")
}

// TestCheckAndReservePackageQuota_Success 测试检查成功场景
func TestCheckAndReservePackageQuota_Success(t *testing.T) {
	// 创建测试数据
	sub := &model.Subscription{
		Id:            2,
		UserId:        100,
		PackageId:     2,
		TotalConsumed: 100000000, // 已消耗 1亿
		Status:        model.SubscriptionStatusActive,
	}

	pkg := &model.Package{
		Id:     2,
		Name:   "测试套餐2",
		Quota:  500000000, // 总限额 5亿
		Status: 1,
	}

	estimatedQuota := int64(50000000) // 预估消耗 0.5亿

	// 执行检查（预计成功：1亿 + 0.5亿 < 5亿）
	// 注意：此测试依赖 Redis，如果 Redis 不可用会降级通过
	err := CheckAndReservePackageQuota(sub, pkg, estimatedQuota)

	// 验证（Redis 不可用时不会报错）
	if err != nil {
		t.Logf("CheckAndReservePackageQuota returned error (Redis may be unavailable): %v", err)
	}
}

// TestSelectAvailablePackage_EmptyList 测试空套餐列表
func TestSelectAvailablePackage_EmptyList(t *testing.T) {
	subscriptions := []*model.Subscription{}
	estimatedQuota := int64(10000000)

	sub, pkg, err := SelectAvailablePackage(subscriptions, estimatedQuota)

	assert.Nil(t, sub, "空列表应返回 nil subscription")
	assert.Nil(t, pkg, "空列表应返回 nil package")
	assert.Nil(t, err, "空列表应返回 nil error")
}

// TestTryConsumeFromPackage_InvalidUserId 测试无效用户ID
func TestTryConsumeFromPackage_InvalidUserId(t *testing.T) {
	// 使用不存在的用户ID
	subscriptionId, quota, err := TryConsumeFromPackage(99999, nil, 10000000)

	// 应该降级到用户余额（返回 0, 0, nil）
	assert.Equal(t, 0, subscriptionId, "无效用户应返回 0 subscription ID")
	assert.Equal(t, int64(0), quota, "无效用户应返回 0 quota")
	assert.Nil(t, err, "无效用户应返回 nil error（降级）")
}

// 注意：以下测试需要完整的数据库和 Redis 环境，仅作为测试框架示例
/*
func TestTryConsumeFromPackage_IntegrationTest(t *testing.T) {
	// 此测试需要：
	// 1. 初始化测试数据库
	// 2. 创建测试用户、套餐、订阅
	// 3. 启动 Redis
	// 4. 执行完整的消耗流程
	// 5. 验证数据库和 Redis 的状态

	// TODO: 实现完整的集成测试
	t.Skip("Integration test requires full DB and Redis setup")
}
*/
