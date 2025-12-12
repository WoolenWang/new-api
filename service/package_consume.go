package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// TryConsumeFromPackage 尝试从套餐中消耗额度
// 参数:
//   - userId: 用户ID
//   - p2pGroupId: P2P分组ID (可选)，用于过滤套餐权限
//   - estimatedQuota: 预估消耗额度
//
// 返回值:
//   - subscriptionId: 使用的套餐订阅ID (0表示未使用套餐)
//   - preConsumedQuota: 预扣的额度
//   - error: 错误信息
//
// 返回场景:
//   - (id > 0, quota, nil): 成功找到可用套餐并预扣费
//   - (0, 0, nil): 无可用套餐或允许fallback到用户余额
//   - (0, 0, error): 套餐超限且不允许fallback
func TryConsumeFromPackage(userId int, p2pGroupId *int, estimatedQuota int64) (int, int64, error) {
	// 1. 查询用户的活跃套餐（已按优先级排序：priority DESC）
	subscriptions, err := model.GetUserActiveSubscriptions(userId, p2pGroupId)
	if err != nil {
		// 数据库查询失败，降级到用户余额流程
		common.SysError(fmt.Sprintf("failed to query user subscriptions: %v", err))
		return 0, 0, nil
	}

	if len(subscriptions) == 0 {
		// 用户没有活跃的套餐，降级到用户余额流程
		return 0, 0, nil
	}

	// 2. 按优先级遍历套餐，尝试扣减
	subscription, pkg, err := SelectAvailablePackage(subscriptions, estimatedQuota)

	if subscription != nil {
		// 成功找到可用套餐
		// 【监控】记录使用套餐的请求
		IncrementPackageRequest()

		if common.DataPlaneLogEnabled {
			common.SysLog(fmt.Sprintf(
				"[Package] User %d using subscription %d (package %d, priority %d), pre-consumed %d quota",
				userId, subscription.Id, pkg.Id, pkg.Priority, estimatedQuota,
			))
		}
		return subscription.Id, estimatedQuota, nil
	}

	// 3. 所有套餐都超限，检查是否允许 fallback
	if err != nil {
		// 有错误（说明尝试过至少一个套餐但都失败了）
		if common.DataPlaneLogEnabled {
			lastPkgID := 0
			lastPkgPriority := 0
			lastPkgFallback := false
			if pkg != nil {
				lastPkgID = pkg.Id
				lastPkgPriority = pkg.Priority
				lastPkgFallback = pkg.FallbackToBalance
			}
			common.SysLog(fmt.Sprintf(
				"[Package] All subscriptions exceeded for user %d, last package id=%d priority=%d fallback_to_balance=%v, err=%v",
				userId, lastPkgID, lastPkgPriority, lastPkgFallback, err,
			))
		}

		if pkg != nil && pkg.FallbackToBalance {
			// 最后尝试的套餐允许 fallback 到用户余额
			// 【监控】记录 Fallback 到余额
			IncrementFallbackRequest()
			IncrementBalanceRequest()

			if common.DataPlaneLogEnabled {
				common.SysLog(fmt.Sprintf(
					"[Package] All subscriptions exceeded for user %d, fallback to user balance (last package: %d)",
					userId, pkg.Id,
				))
			}
			return 0, 0, nil
		}

		// 不允许 fallback，返回错误（请求将被拒绝）
		return 0, 0, fmt.Errorf("all available packages exceeded limit and fallback is disabled: %w", err)
	}

	// 理论上不应该走到这里（subscriptions非空但selection无结果且无error）
	// 作为兜底，降级到用户余额
	// 【监控】记录使用余额
	IncrementBalanceRequest()

	if common.DataPlaneLogEnabled {
		common.SysLog(fmt.Sprintf(
			"[Package] No subscription selected for user %d, no error returned from selector; falling back to user balance",
			userId,
		))
	}

	return 0, 0, nil
}

// SelectAvailablePackage 选择可用的套餐
// 按优先级遍历订阅列表，返回第一个未超限的套餐
// 参数:
//   - subscriptions: 已排序的订阅列表 (按 priority DESC)
//   - estimatedQuota: 预估消耗额度
//
// 返回值:
//   - subscription: 选中的订阅实例 (nil表示所有套餐都超限)
//   - pkg: 最后尝试的套餐配置 (用于判断 fallback 配置)
//   - error: 最后一次失败的错误信息 (用于日志记录)
func SelectAvailablePackage(subscriptions []*model.Subscription, estimatedQuota int64) (*model.Subscription, *model.Package, error) {
	var lastError error
	var lastPackage *model.Package

	for _, sub := range subscriptions {
		// 加载套餐配置（强制从 DB 读取，以确保在测试或后台任务直接修改 packages 表后，
		// 不会因为 PackageCache 中的旧值导致限额配置（如 HourlyLimit）失真）。
		pkg, err := model.GetPackageByIDFromDB(sub.PackageId)
		if err != nil {
			// 数据库查询失败，跳过此套餐
			common.SysError(fmt.Sprintf("failed to load package %d: %v", sub.PackageId, err))
			lastError = err
			continue
		}

		if common.DataPlaneLogEnabled {
			common.SysLog(fmt.Sprintf(
				"[Package] Evaluating subscription %d for user %d: package_id=%d priority=%d fallback_to_balance=%v hourly_limit=%d quota=%d total_consumed=%d estimated_quota=%d",
				sub.Id,
				sub.UserId,
				pkg.Id,
				pkg.Priority,
				pkg.FallbackToBalance,
				pkg.HourlyLimit,
				pkg.Quota,
				sub.TotalConsumed,
				estimatedQuota,
			))
		}

		// 检查并预留套餐额度
		err = CheckAndReservePackageQuota(sub, pkg, estimatedQuota)
		if err == nil {
			// 成功找到可用套餐
			return sub, pkg, nil
		}

		// 失败，记录错误并继续尝试下一个套餐
		if common.DataPlaneLogEnabled {
			common.SysLog(fmt.Sprintf(
				"[Package] Subscription %d (priority %d) check failed: %v",
				sub.Id, pkg.Priority, err,
			))
		}
		lastError = err
		lastPackage = pkg
	}

	// 所有套餐都不可用
	return nil, lastPackage, lastError
}

// CheckAndReservePackageQuota 检查并预留套餐额度
// 执行两层检查：
//  1. 数据库层：检查月度总限额 (total_consumed < quota)
//  2. Redis层：检查所有滑动窗口限额 (RPM, 小时, 4小时, 日, 周)
//
// 参数:
//   - sub: 订阅实例
//   - pkg: 套餐配置
//   - estimatedQuota: 预估消耗额度
//
// 返回值:
//   - error: 超限错误（包含详细的窗口信息）
func CheckAndReservePackageQuota(sub *model.Subscription, pkg *model.Package, estimatedQuota int64) error {
	// ========== 1. 检查月度总限额（数据库层） ==========
	if pkg.Quota > 0 {
		// 计算预扣后的消耗量
		projectedConsumed := sub.TotalConsumed + estimatedQuota

		// 在 DEBUG 模式下输出月度总限额检查的详细信息，便于排查「应当超限却未超限」等问题。
		if common.DebugEnabled && common.DataPlaneLogEnabled {
			common.SysLog(fmt.Sprintf(
				"[PackageMonthlyDebug] subscription_id=%d package_id=%d total_consumed=%d estimated_quota=%d projected=%d monthly_quota=%d",
				sub.Id, pkg.Id, sub.TotalConsumed, estimatedQuota, projectedConsumed, pkg.Quota,
			))
		}

		if projectedConsumed > pkg.Quota {
			if common.DataPlaneLogEnabled {
				common.SysLog(fmt.Sprintf(
					"[PackageMonthlyExceeded] subscription_id=%d package_id=%d total_consumed=%d estimated_quota=%d projected=%d monthly_quota=%d",
					sub.Id, pkg.Id, sub.TotalConsumed, estimatedQuota, projectedConsumed, pkg.Quota,
				))
			}
			return fmt.Errorf(
				"monthly quota exceeded: consumed=%d, estimated=%d, limit=%d",
				sub.TotalConsumed, estimatedQuota, pkg.Quota,
			)
		}
	}

	// ========== 2. 检查滑动窗口限额（Redis层） ==========
	if !common.RedisEnabled {
		// Redis 不可用，仅依赖 DB 的月度限额检查
		if common.DataPlaneLogEnabled {
			common.SysLog(fmt.Sprintf(
				"[Package] Redis unavailable, sliding window check skipped for subscription %d",
				sub.Id,
			))
		}
		return nil // 降级处理：允许通过
	}

	// 调用任务集2的滑动窗口检查函数
	ctx := context.Background()
	err := CheckAllSlidingWindows(ctx, sub, pkg, estimatedQuota)
	if err != nil {
		// 滑动窗口超限
		return err
	}

	// 所有检查通过
	return nil
}
