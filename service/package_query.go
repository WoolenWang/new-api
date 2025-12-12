package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/model"
)

// SubscriptionDetail contains detailed subscription information including package details.
type SubscriptionDetail struct {
	Subscription *model.Subscription `json:"subscription"`
	Package      *model.Package      `json:"package"`
}

// GetAvailablePackagesForUser retrieves all packages that a user can subscribe to.
// This includes:
//  - All global packages (p2p_group_id == 0, status == 1)
//  - P2P group packages from groups the user is an active member of
//
// Packages are ordered by priority DESC (highest priority first), then by ID ASC.
//
// Parameters:
//   - userId: The ID of the user
//
// Returns:
//   - []*model.Package: List of available packages
//   - error: Database error if any
func GetAvailablePackagesForUser(userId int) ([]*model.Package, error) {
	// Step 1: Get user's active P2P group IDs
	groupIds, err := model.GetUserActiveGroupIds(userId)
	if err != nil {
		return nil, fmt.Errorf("failed to get user's groups: %w", err)
	}

	// Step 2: Query packages
	// - Global packages (p2p_group_id = 0)
	// - P2P group packages from user's groups
	query := model.DB.Model(&model.Package{}).Where("status = ?", 1)

	if len(groupIds) > 0 {
		// User is in some P2P groups: include both global and group packages
		query = query.Where("p2p_group_id = 0 OR p2p_group_id IN ?", groupIds)
	} else {
		// User is not in any P2P group: only global packages
		query = query.Where("p2p_group_id = 0")
	}

	var packages []*model.Package
	err = query.Order("priority DESC, id ASC").Find(&packages).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query packages: %w", err)
	}

	return packages, nil
}

// GetUserActiveSubscriptions retrieves all active subscriptions for a user.
// Active subscriptions are those with:
//  - status == 'active'
//  - start_time <= now
//  - end_time > now
//
// The result includes subscriptions from:
//  - Global packages
//  - P2P group packages (if applicable)
//
// Subscriptions are ordered by package priority DESC (consume higher priority first).
//
// Parameters:
//   - userId: The ID of the user
//
// Returns:
//   - []*model.Subscription: List of active subscriptions
//   - error: Database error if any
func GetUserActiveSubscriptions(userId int) ([]*model.Subscription, error) {
	// Note: model.GetUserActiveSubscriptions handles P2P group filtering internally
	// It joins with the packages table and filters by p2p_group_id
	subs, err := model.GetUserActiveSubscriptions(userId, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get active subscriptions: %w", err)
	}

	return subs, nil
}

// GetUserAllSubscriptions retrieves all subscriptions for a user (all statuses).
// This is useful for displaying the user's subscription history.
//
// Parameters:
//   - userId: The ID of the user
//   - status: Filter by status (empty string for all statuses)
//
// Returns:
//   - []*model.Subscription: List of subscriptions
//   - error: Database error if any
func GetUserAllSubscriptions(userId int, status string) ([]*model.Subscription, error) {
	subs, err := model.GetUserSubscriptions(userId, status)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}

	return subs, nil
}

// GetSubscriptionDetail retrieves detailed information for a subscription,
// including both the subscription record and the associated package details.
//
// This function validates that the user owns the subscription.
//
// Parameters:
//   - subscriptionId: The ID of the subscription
//   - userId: The ID of the user (for ownership validation)
//
// Returns:
//   - *SubscriptionDetail: Detailed subscription information
//   - error: Not found error, permission error, or database error
func GetSubscriptionDetail(subscriptionId int, userId int) (*SubscriptionDetail, error) {
	// Step 1: Get subscription
	sub, err := model.GetSubscriptionById(subscriptionId)
	if err != nil {
		return nil, fmt.Errorf("subscription not found: %w", err)
	}

	// Step 2: Validate ownership
	if sub.UserId != userId {
		return nil, fmt.Errorf("permission denied: you do not own this subscription")
	}

	// Step 3: Get package details
	pkg, err := model.GetPackageByID(sub.PackageId)
	if err != nil {
		return nil, fmt.Errorf("package not found: %w", err)
	}

	// Step 4: Construct result
	detail := &SubscriptionDetail{
		Subscription: sub,
		Package:      pkg,
	}

	return detail, nil
}
