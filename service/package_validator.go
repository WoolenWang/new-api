package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// PackagePriorityP2PFixed is the fixed priority for P2P group packages.
// P2P group owners can only create packages with this priority level.
const PackagePriorityP2PFixed = 11

// ValidatePackageCreation validates the permission and configuration for creating a package.
// It enforces the following rules:
//
// Global Packages (p2p_group_id == 0):
//   - Requires role == RoleRootUser (admin only)
//   - Priority can be set to any value in [1..21]
//
// P2P Group Packages (p2p_group_id > 0):
//   - Requires user to be the owner of the specified P2P group
//   - Priority is **forcibly set to 11** (cannot be customized)
//   - The group must exist
//
// Common Validations:
//   - duration_type must be one of: week, month, quarter, year
//   - duration must be positive
//
// Parameters:
//   - userId: The ID of the user creating the package
//   - userRole: The role of the user (e.g., common.RoleRootUser)
//   - pkg: The package to be validated (fields may be modified)
//
// Returns:
//   - error: Permission error, validation error, or nil if valid
//
// Side Effects:
//   - pkg.Priority will be forcibly set to 11 if it's a P2P group package
func ValidatePackageCreation(userId int, userRole int, pkg *model.Package) error {
	if pkg == nil {
		return errors.New("package cannot be nil")
	}

	// Validate duration type
	validDurationTypes := map[string]bool{
		"week":    true,
		"month":   true,
		"quarter": true,
		"year":    true,
	}
	if !validDurationTypes[pkg.DurationType] {
		return errors.New("invalid duration_type: must be week, month, quarter, or year")
	}

	// Validate duration value
	if pkg.Duration <= 0 {
		return errors.New("duration must be positive")
	}

	// Determine package type and apply corresponding rules
	if pkg.P2PGroupId == 0 {
		// ========== Global Package ==========
		// Rule: Only admins can create global packages
		if userRole != common.RoleRootUser {
			return errors.New("permission denied: only administrators can create global packages")
		}

		// Rule: Priority must be in valid range [1..21]
		if pkg.Priority < 1 || pkg.Priority > 21 {
			return errors.New("priority must be between 1 and 21")
		}
	} else {
		// ========== P2P Group Package ==========
		// Rule 1: Group must exist
		group, err := model.GetGroupById(pkg.P2PGroupId)
		if err != nil {
			return fmt.Errorf("group not found: %w", err)
		}

		// Rule 2: User must be the group owner
		if group.OwnerId != userId {
			return errors.New("permission denied: only the group owner can create packages for this group")
		}

		// Rule 3: Force priority to 11 (P2P packages have fixed priority)
		pkg.Priority = PackagePriorityP2PFixed
	}

	return nil
}

// ValidatePackageSubscription validates whether a user can subscribe to a package.
// It enforces the following rules:
//
// Global Packages (p2p_group_id == 0):
//   - Any user can subscribe
//
// P2P Group Packages (p2p_group_id > 0):
//   - User must be an active member of the P2P group (status == MemberStatusActive)
//
// Common Validations:
//   - Package must exist
//   - Package status must be 1 (available)
//
// Parameters:
//   - userId: The ID of the user subscribing to the package
//   - packageId: The ID of the package to subscribe to
//
// Returns:
//   - error: Permission error, validation error, or nil if valid
func ValidatePackageSubscription(userId int, packageId int) error {
	// Step 1: Retrieve package
	pkg, err := model.GetPackageByID(packageId)
	if err != nil {
		return fmt.Errorf("package not found: %w", err)
	}

	// Step 2: Validate package status
	if pkg.Status != 1 {
		return errors.New("package is not available for subscription")
	}

	// Step 3: Check P2P group membership if applicable
	if pkg.P2PGroupId > 0 {
		// This is a P2P group package - verify membership
		isMember, err := model.IsUserGroupMember(userId, pkg.P2PGroupId, model.MemberStatusActive)
		if err != nil {
			return fmt.Errorf("failed to check group membership: %w", err)
		}

		if !isMember {
			return errors.New("permission denied: you are not an active member of this group")
		}
	}
	// If p2p_group_id == 0, it's a global package - no additional checks needed

	return nil
}
