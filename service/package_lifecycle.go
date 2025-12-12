package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ActivateSubscription activates a subscription, setting start_time and calculating end_time.
// This function validates ownership and status before activation.
//
// Workflow:
//  1. Retrieve subscription by ID
//  2. Validate ownership (sub.UserId == userId)
//  3. Validate current status is 'inventory'
//  4. Call model.Subscription.Activate() to set times and status
//
// Parameters:
//   - subscriptionId: The ID of the subscription to activate
//   - userId: The user ID requesting activation
//
// Returns:
//   - error: Permission error, status error, or database error
//
// Example:
//
//	err := ActivateSubscription(123, 456)
//	if err != nil {
//	    return fmt.Errorf("failed to activate subscription: %w", err)
//	}
func ActivateSubscription(subscriptionId int, userId int) error {
	// Step 1: Retrieve subscription
	sub, err := model.GetSubscriptionById(subscriptionId)
	if err != nil {
		return fmt.Errorf("failed to get subscription: %w", err)
	}

	// Step 2: Validate ownership
	if sub.UserId != userId {
		return errors.New("permission denied: you do not own this subscription")
	}

	// Step 3: Validate status
	if sub.Status != model.SubscriptionStatusInventory {
		return fmt.Errorf("invalid status: subscription is '%s', expected 'inventory'", sub.Status)
	}

	// Step 4: Activate subscription (model layer handles time calculation)
	if err := sub.Activate(); err != nil {
		return fmt.Errorf("failed to activate subscription: %w", err)
	}

	common.SysLog(fmt.Sprintf("Subscription %d activated by user %d", subscriptionId, userId))
	return nil
}

// CancelSubscription cancels an active subscription.
// This is an optional feature that allows users or admins to manually cancel subscriptions.
//
// Workflow:
//  1. Retrieve subscription by ID
//  2. Validate ownership (or admin override)
//  3. Validate current status is 'active'
//  4. Update status to 'cancelled'
//
// Parameters:
//   - subscriptionId: The ID of the subscription to cancel
//   - userId: The user ID requesting cancellation
//   - isAdmin: Whether the requester is an administrator (bypasses ownership check)
//
// Returns:
//   - error: Permission error, status error, or database error
func CancelSubscription(subscriptionId int, userId int, isAdmin bool) error {
	// Step 1: Retrieve subscription
	sub, err := model.GetSubscriptionById(subscriptionId)
	if err != nil {
		return fmt.Errorf("failed to get subscription: %w", err)
	}

	// Step 2: Validate ownership (admin can bypass)
	if !isAdmin && sub.UserId != userId {
		return errors.New("permission denied: you do not own this subscription")
	}

	// Step 3: Validate status
	if sub.Status != model.SubscriptionStatusActive {
		return fmt.Errorf("invalid status: subscription is '%s', can only cancel active subscriptions", sub.Status)
	}

	// Step 4: Update status to cancelled
	if err := model.UpdateSubscriptionStatus(subscriptionId, model.SubscriptionStatusCancelled); err != nil {
		return fmt.Errorf("failed to cancel subscription: %w", err)
	}

	common.SysLog(fmt.Sprintf("Subscription %d cancelled by user %d (admin=%v)", subscriptionId, userId, isAdmin))
	return nil
}

// MarkExpiredSubscriptions identifies and marks all expired subscriptions.
// This function should be called periodically (e.g., every hour) by a cron job.
//
// Workflow:
//  1. Find all active subscriptions where end_time < now
//  2. Batch update their status to 'expired'
//  3. Log the number of subscriptions marked as expired
//
// This function is idempotent and safe to call multiple times.
//
// Returns:
//   - int: Number of subscriptions marked as expired
//   - error: Database error if any
func MarkExpiredSubscriptions() (int, error) {
	now := common.GetTimestamp()

	// Step 1: 找出所有已过期且仍为 active 的订阅 ID
	var expiredIds []int
	if err := model.DB.Model(&model.Subscription{}).
		Where("status = ?", model.SubscriptionStatusActive).
		Where("end_time IS NOT NULL").
		Where("end_time < ?", now).
		Pluck("id", &expiredIds).Error; err != nil {
		common.SysError(fmt.Sprintf("Failed to query expired subscriptions: %v", err))
		return 0, err
	}

	if len(expiredIds) == 0 {
		// 没有需要更新的订阅，直接返回
		return 0, nil
	}

	// Step 2: 批量更新状态为 expired
	result := model.DB.Model(&model.Subscription{}).
		Where("id IN ?", expiredIds).
		Update("status", model.SubscriptionStatusExpired)
	if result.Error != nil {
		common.SysError(fmt.Sprintf("Failed to mark expired subscriptions: %v", result.Error))
		return 0, result.Error
	}

	affected := int(result.RowsAffected)

	// Step 3: 失效对应订阅的缓存（L1/L2），确保后续读取到最新状态
	cache := model.GetPackageCache()
	for _, id := range expiredIds {
		cache.InvalidateSubscription(id)
	}

	if affected > 0 {
		common.SysLog(fmt.Sprintf("Marked %d expired subscriptions", affected))
	}

	return affected, nil
}
