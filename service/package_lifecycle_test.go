package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB initializes an in-memory SQLite database for testing
func setupTestDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "Failed to connect to test database")

	// Auto-migrate required tables
	err = db.AutoMigrate(
		&model.Package{},
		&model.Subscription{},
		&model.User{},
		&model.Group{},
		&model.UserGroup{},
	)
	require.NoError(t, err, "Failed to migrate test database")

	model.DB = db
}

// teardownTestDB closes the test database
func teardownTestDB(t *testing.T) {
	sqlDB, err := model.DB.DB()
	if err == nil {
		sqlDB.Close()
	}
}

// Test Fixtures

func createTestPackage(t *testing.T, p2pGroupId int, priority int) *model.Package {
	pkg := &model.Package{
		Name:         "Test Package",
		Description:  "Test Description",
		Status:       1,
		Priority:     priority,
		P2PGroupId:   p2pGroupId,
		Quota:        1000000,
		DurationType: "month",
		Duration:     1,
		RpmLimit:     60,
		HourlyLimit:  100000,
	}
	err := model.CreatePackage(pkg)
	require.NoError(t, err, "Failed to create test package")
	return pkg
}

func createTestSubscription(t *testing.T, userId int, packageId int, status string) *model.Subscription {
	sub := &model.Subscription{
		UserId:    userId,
		PackageId: packageId,
		Status:    status,
	}
	err := model.CreateSubscription(sub)
	require.NoError(t, err, "Failed to create test subscription")
	return sub
}

func createTestUser(t *testing.T) *model.User {
	user := &model.User{
		Username: "testuser",
		Role:     common.RoleCommonUser,
		Status:   1,
	}
	err := model.DB.Create(user).Error
	require.NoError(t, err, "Failed to create test user")
	return user
}

func createTestAdminUser(t *testing.T) *model.User {
	user := &model.User{
		Username: "admin",
		Role:     common.RoleRootUser,
		Status:   1,
	}
	err := model.DB.Create(user).Error
	require.NoError(t, err, "Failed to create test admin user")
	return user
}

func createTestGroup(t *testing.T, ownerId int) *model.Group {
	group := &model.Group{
		Name:        "TestGroup",
		DisplayName: "Test Group",
		OwnerId:     ownerId,
		Type:        model.GroupTypeShared,
	}
	err := model.CreateGroup(group)
	require.NoError(t, err, "Failed to create test group")
	return group
}

func addUserToGroup(t *testing.T, userId int, groupId int, status int) {
	userGroup := &model.UserGroup{
		UserId:    userId,
		GroupId:   groupId,
		Role:      model.MemberRoleMember,
		Status:    status,
		CreatedAt: common.GetTimestamp(),
		UpdatedAt: common.GetTimestamp(),
	}
	err := model.DB.Create(userGroup).Error
	require.NoError(t, err, "Failed to add user to group")
}

// ========== Test Suite: Subscription Lifecycle ==========

func TestActivateSubscription_Success(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Setup: Create package and subscription
	pkg := createTestPackage(t, 0, 15)
	user := createTestUser(t)
	sub := createTestSubscription(t, user.Id, pkg.Id, model.SubscriptionStatusInventory)

	// Execute: Activate subscription
	err := ActivateSubscription(sub.Id, user.Id)

	// Assert: No error
	assert.NoError(t, err, "ActivateSubscription should succeed")

	// Verify: Status and timestamps are updated
	updated, _ := model.GetSubscriptionById(sub.Id)
	assert.Equal(t, model.SubscriptionStatusActive, updated.Status, "Status should be 'active'")
	assert.NotNil(t, updated.StartTime, "StartTime should be set")
	assert.NotNil(t, updated.EndTime, "EndTime should be set")

	// Verify: EndTime is correctly calculated (approximately 30 days for 1 month)
	expectedDuration := int64(30 * 24 * 3600) // 30 days in seconds
	actualDuration := *updated.EndTime - *updated.StartTime
	assert.InDelta(t, expectedDuration, actualDuration, 60, "EndTime should be ~30 days after StartTime")
}

func TestActivateSubscription_NotOwner(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Setup: Create package and subscription for user A
	pkg := createTestPackage(t, 0, 15)
	userA := createTestUser(t)
	userB := createTestUser(t)
	sub := createTestSubscription(t, userA.Id, pkg.Id, model.SubscriptionStatusInventory)

	// Execute: User B tries to activate user A's subscription
	err := ActivateSubscription(sub.Id, userB.Id)

	// Assert: Permission denied error
	assert.Error(t, err, "Should return permission denied error")
	assert.Contains(t, err.Error(), "permission denied", "Error message should mention permission denied")
}

func TestActivateSubscription_InvalidStatus(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Setup: Create already active subscription
	pkg := createTestPackage(t, 0, 15)
	user := createTestUser(t)
	sub := createTestSubscription(t, user.Id, pkg.Id, model.SubscriptionStatusActive)
	now := common.GetTimestamp()
	sub.StartTime = &now
	endTime := now + 2592000 // 30 days
	sub.EndTime = &endTime
	model.DB.Save(sub)

	// Execute: Try to activate already active subscription
	err := ActivateSubscription(sub.Id, user.Id)

	// Assert: Invalid status error
	assert.Error(t, err, "Should return invalid status error")
	assert.Contains(t, err.Error(), "invalid status", "Error message should mention invalid status")
}

func TestCancelSubscription_Success(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Setup: Create active subscription
	pkg := createTestPackage(t, 0, 15)
	user := createTestUser(t)
	sub := createTestSubscription(t, user.Id, pkg.Id, model.SubscriptionStatusInventory)
	_ = ActivateSubscription(sub.Id, user.Id)

	// Execute: Cancel subscription
	err := CancelSubscription(sub.Id, user.Id, false)

	// Assert: No error
	assert.NoError(t, err, "CancelSubscription should succeed")

	// Verify: Status is cancelled
	updated, _ := model.GetSubscriptionById(sub.Id)
	assert.Equal(t, model.SubscriptionStatusCancelled, updated.Status, "Status should be 'cancelled'")
}

func TestMarkExpiredSubscriptions(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Setup: Create expired subscription
	pkg := createTestPackage(t, 0, 15)
	user := createTestUser(t)
	sub := createTestSubscription(t, user.Id, pkg.Id, model.SubscriptionStatusInventory)

	// Activate and manually set end_time to the past
	_ = ActivateSubscription(sub.Id, user.Id)
	pastTime := common.GetTimestamp() - 3600 // 1 hour ago
	sub.EndTime = &pastTime
	model.DB.Save(sub)

	// Execute: Mark expired subscriptions
	count, err := MarkExpiredSubscriptions()

	// Assert: No error and 1 subscription marked
	assert.NoError(t, err, "MarkExpiredSubscriptions should succeed")
	assert.Equal(t, 1, count, "Should mark 1 subscription as expired")

	// Verify: Subscription is expired
	updated, _ := model.GetSubscriptionById(sub.Id)
	assert.Equal(t, model.SubscriptionStatusExpired, updated.Status, "Status should be 'expired'")
}

// ========== Test Suite: Package Validation ==========

func TestValidatePackageCreation_AdminGlobal(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	admin := createTestAdminUser(t)

	pkg := &model.Package{
		P2PGroupId:   0,
		Priority:     15,
		DurationType: "month",
		Duration:     1,
	}

	err := ValidatePackageCreation(admin.Id, common.RoleRootUser, pkg)

	assert.NoError(t, err, "Admin should be able to create global package")
	assert.Equal(t, 15, pkg.Priority, "Priority should remain 15")
}

func TestValidatePackageCreation_NonAdminGlobal(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	user := createTestUser(t)

	pkg := &model.Package{
		P2PGroupId:   0,
		Priority:     15,
		DurationType: "month",
		Duration:     1,
	}

	err := ValidatePackageCreation(user.Id, common.RoleCommonUser, pkg)

	assert.Error(t, err, "Non-admin should not be able to create global package")
	assert.Contains(t, err.Error(), "only administrators", "Error should mention admin requirement")
}

func TestValidatePackageCreation_P2POwner(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	owner := createTestUser(t)
	group := createTestGroup(t, owner.Id)

	pkg := &model.Package{
		P2PGroupId:   group.Id,
		Priority:     20, // Will be overridden
		DurationType: "month",
		Duration:     1,
	}

	err := ValidatePackageCreation(owner.Id, common.RoleCommonUser, pkg)

	assert.NoError(t, err, "Group owner should be able to create P2P package")
	assert.Equal(t, PackagePriorityP2PFixed, pkg.Priority, "Priority should be forced to 11")
}

func TestValidatePackageCreation_NotOwner(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	owner := createTestUser(t)
	otherUser := createTestUser(t)
	group := createTestGroup(t, owner.Id)

	pkg := &model.Package{
		P2PGroupId:   group.Id,
		DurationType: "month",
		Duration:     1,
	}

	err := ValidatePackageCreation(otherUser.Id, common.RoleCommonUser, pkg)

	assert.Error(t, err, "Non-owner should not be able to create P2P package")
	assert.Contains(t, err.Error(), "only the group owner", "Error should mention ownership requirement")
}

func TestValidatePackageSubscription_GlobalPackage(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	user := createTestUser(t)
	pkg := createTestPackage(t, 0, 15) // Global package

	err := ValidatePackageSubscription(user.Id, pkg.Id)

	assert.NoError(t, err, "Any user should be able to subscribe to global package")
}

func TestValidatePackageSubscription_P2PMember(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	owner := createTestUser(t)
	member := createTestUser(t)
	group := createTestGroup(t, owner.Id)
	addUserToGroup(t, member.Id, group.Id, model.MemberStatusActive)

	pkg := createTestPackage(t, group.Id, 11) // P2P package

	err := ValidatePackageSubscription(member.Id, pkg.Id)

	assert.NoError(t, err, "Group member should be able to subscribe to P2P package")
}

func TestValidatePackageSubscription_NotMember(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	owner := createTestUser(t)
	nonMember := createTestUser(t)
	group := createTestGroup(t, owner.Id)

	pkg := createTestPackage(t, group.Id, 11) // P2P package

	err := ValidatePackageSubscription(nonMember.Id, pkg.Id)

	assert.Error(t, err, "Non-member should not be able to subscribe to P2P package")
	assert.Contains(t, err.Error(), "not an active member", "Error should mention membership requirement")
}

// ========== Test Suite: Package Query ==========

func TestGetAvailablePackagesForUser(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Setup: Create global and P2P packages
	createTestPackage(t, 0, 15) // Global package
	owner := createTestUser(t)
	member := createTestUser(t)
	group := createTestGroup(t, owner.Id)
	addUserToGroup(t, member.Id, group.Id, model.MemberStatusActive)
	createTestPackage(t, group.Id, 11) // P2P package

	// Execute: Get available packages for member
	packages, err := GetAvailablePackagesForUser(member.Id)

	// Assert: Should get both global and P2P package
	assert.NoError(t, err, "GetAvailablePackagesForUser should succeed")
	assert.Len(t, packages, 2, "Member should see 2 packages (1 global + 1 P2P)")
}

func TestPurchasePackage_Success(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	user := createTestUser(t)
	pkg := createTestPackage(t, 0, 15)

	// Execute: Purchase package
	sub, err := PurchasePackage(user.Id, pkg.Id)

	// Assert: Success
	assert.NoError(t, err, "PurchasePackage should succeed")
	assert.NotNil(t, sub, "Subscription should be created")
	assert.Equal(t, model.SubscriptionStatusInventory, sub.Status, "Status should be 'inventory'")
}

func TestPurchasePackage_ActiveLimit(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Setup: Set limit and create active subscription
	common.MaxActiveSubscriptionsPerUser = 1
	defer func() { common.MaxActiveSubscriptionsPerUser = 0 }()

	user := createTestUser(t)
	pkg1 := createTestPackage(t, 0, 15)
	pkg2 := createTestPackage(t, 0, 14)

	// Purchase and activate first package
	sub1, _ := PurchasePackage(user.Id, pkg1.Id)
	_ = ActivateSubscription(sub1.Id, user.Id)

	// Execute: Try to purchase second package (should fail due to limit)
	_, err := PurchasePackage(user.Id, pkg2.Id)

	// Assert: Limit error
	assert.Error(t, err, "Should fail due to active subscription limit")
	assert.Contains(t, err.Error(), "maximum active subscriptions limit", "Error should mention limit")
}

// ========== Test Suite: Edge Cases ==========

func TestMarkExpiredSubscriptions_NoExpired(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	// Setup: Create active subscription that is not expired
	pkg := createTestPackage(t, 0, 15)
	user := createTestUser(t)
	sub := createTestSubscription(t, user.Id, pkg.Id, model.SubscriptionStatusInventory)
	_ = ActivateSubscription(sub.Id, user.Id)

	// Execute: Mark expired (should find none)
	count, err := MarkExpiredSubscriptions()

	// Assert: No error and 0 marked
	assert.NoError(t, err, "MarkExpiredSubscriptions should succeed")
	assert.Equal(t, 0, count, "Should mark 0 subscriptions (none expired)")
}

func TestGetDurationSeconds_AllTypes(t *testing.T) {
	tests := []struct {
		durationType     string
		duration         int
		expectedDuration int64 // Approximate expected duration in seconds
	}{
		{"week", 1, 7 * 24 * 3600},
		{"month", 1, 30 * 24 * 3600},
		{"quarter", 1, 90 * 24 * 3600},
		{"year", 1, 365 * 24 * 3600},
	}

	for _, tt := range tests {
		t.Run(tt.durationType, func(t *testing.T) {
			pkg := &model.Package{
				DurationType: tt.durationType,
				Duration:     tt.duration,
			}

			seconds, err := model.GetDurationSeconds(tt.durationType, tt.duration)

			assert.NoError(t, err, "GetDurationSeconds should succeed")
			// Allow 1 day tolerance for month/year calculations
			assert.InDelta(t, tt.expectedDuration, seconds, 24*3600, "Duration should match expected value")

			// Test CalculateEndTime as well
			now := time.Now().Unix()
			endTime, err := model.CalculateEndTime(now, pkg)
			assert.NoError(t, err, "CalculateEndTime should succeed")
			assert.Greater(t, endTime, now, "EndTime should be in the future")
		})
	}
}
