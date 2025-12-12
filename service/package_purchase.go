package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// PurchasePackage creates a new subscription for a user.
// This is a simplified implementation that does not handle payment processing.
// The subscription is created in 'inventory' status and can be activated later.
//
// Workflow:
//  1. Validate package subscription permission (ValidatePackageSubscription)
//  2. Check if user has reached the maximum active subscriptions limit
//  3. Create a new subscription record (status = 'inventory')
//
// Future enhancements:
//  - Payment processing (deduct user quota)
//  - Transaction management
//  - Inventory/stock management
//
// Parameters:
//   - userId: The ID of the user purchasing the package
//   - packageId: The ID of the package to purchase
//
// Returns:
//   - *model.Subscription: The newly created subscription
//   - error: Permission error, validation error, or database error
func PurchasePackage(userId int, packageId int) (*model.Subscription, error) {
	// Step 1: Validate subscription permission
	if err := ValidatePackageSubscription(userId, packageId); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Step 2: Check active subscriptions limit
	// Note: This checks active subscriptions at the moment of purchase.
	// Users may have multiple subscriptions in 'inventory' status,
	// but can only activate up to MaxActiveSubscriptions at once.
	if common.MaxActiveSubscriptionsPerUser > 0 {
		activeCount, err := model.CountUserActiveSubscriptions(userId)
		if err != nil {
			return nil, fmt.Errorf("failed to count active subscriptions: %w", err)
		}

		if activeCount >= int64(common.MaxActiveSubscriptionsPerUser) {
			return nil, fmt.Errorf("maximum active subscriptions limit reached (%d/%d)", activeCount, common.MaxActiveSubscriptionsPerUser)
		}
	}

	// Step 3: Create subscription record
	sub := &model.Subscription{
		UserId:    userId,
		PackageId: packageId,
		Status:    model.SubscriptionStatusInventory,
		// SubscribedAt is set by BeforeCreate hook
	}

	if err := model.CreateSubscription(sub); err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	common.SysLog(fmt.Sprintf("User %d purchased package %d (subscription %d)", userId, packageId, sub.Id))
	return sub, nil
}

// GiftPackage allows an admin to gift a package to a user.
// This bypasses the active subscriptions limit check.
//
// Parameters:
//   - adminUserId: The ID of the admin user gifting the package
//   - targetUserId: The ID of the user receiving the gift
//   - packageId: The ID of the package to gift
//
// Returns:
//   - *model.Subscription: The newly created subscription
//   - error: Permission error, validation error, or database error
func GiftPackage(adminUserId int, targetUserId int, packageId int) (*model.Subscription, error) {
	// Step 1: Validate that the package exists and is available
	pkg, err := model.GetPackageByID(packageId)
	if err != nil {
		return nil, fmt.Errorf("package not found: %w", err)
	}

	if pkg.Status != 1 {
		return nil, errors.New("package is not available")
	}

	// Step 2: Create subscription record (no permission check, admin privilege)
	sub := &model.Subscription{
		UserId:    targetUserId,
		PackageId: packageId,
		Status:    model.SubscriptionStatusInventory,
	}

	if err := model.CreateSubscription(sub); err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	common.SysLog(fmt.Sprintf("Admin %d gifted package %d to user %d (subscription %d)", adminUserId, packageId, targetUserId, sub.Id))
	return sub, nil
}
