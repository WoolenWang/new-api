// Package p2p_management contains integration tests for P2P group role management.
//
// Test Focus:
// ===========
// This package validates member role management and batch operations (RM-01 to RM-06).
//
// Role Hierarchy:
// - Owner (implicit, owner_id field): Full control
// - Admin (role=1): Can manage members, cannot delete group or transfer ownership
// - Member (role=0): Regular member, no special privileges
//
// Key Test Scenarios:
// - RM-01: Promote member to admin
// - RM-02: Admin kicks regular member
// - RM-03: Admin cannot kick owner
// - RM-04: Owner transfers ownership
// - RM-05: Batch approval operations
// - RM-06: Batch kick operations
package p2p_management

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRM01_PromoteMemberToAdmin tests promoting a regular member to admin role.
// Priority: P1
func TestRM01_PromoteMemberToAdmin(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a group
	password := "admin123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"admin-promote-rm01",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		password,
	)

	// User2 joins as regular member
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		password,
		1, // Active
	)

	// Verify User2 is a regular member (role=0)
	memberInfo, err := suite.fixtures.User1Client.GetGroupMemberInfo(group.ID, suite.fixtures.RegularUser2.ID)
	require.NoError(t, err, "Should be able to get member info")
	require.Equal(t, 0, memberInfo.Role, "Initial role should be Member (0)")

	// Owner promotes User2 to Admin (role=1)
	t.Log("Owner promoting User2 to Admin...")
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	err = suite.fixtures.User1Client.PutJSON("/api/groups/members", map[string]interface{}{
		"group_id": group.ID,
		"user_id":  suite.fixtures.RegularUser2.ID,
		"role":     1, // Admin
	}, &resp)
	require.NoError(t, err, "Promotion should succeed")
	require.True(t, resp.Success, "Promotion should be successful")

	time.Sleep(200 * time.Millisecond)

	// Verify User2 is now Admin (role=1)
	memberInfo, err = suite.fixtures.User1Client.GetGroupMemberInfo(group.ID, suite.fixtures.RegularUser2.ID)
	require.NoError(t, err, "Should be able to get member info")
	assert.Equal(t, 1, memberInfo.Role, "Role should be Admin (1) after promotion")

	// User3 joins
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User3Client,
		suite.fixtures.RegularUser3.ID,
		group.ID,
		password,
		1,
	)

	// Verify User2 (now Admin) can manage User3
	t.Log("Admin User2 attempting to kick User3...")
	err = suite.fixtures.User2Client.UpdateMemberStatus(group.ID, suite.fixtures.RegularUser3.ID, 3)
	assert.NoError(t, err, "Admin should be able to kick members")

	// Verify User3 was kicked
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser3.ID, group.ID, 3)

	t.Log("Member promotion to admin verified")
}

// TestRM02_AdminKicksRegularMember tests that admin can kick regular members.
// Priority: P1
func TestRM02_AdminKicksRegularMember(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a group
	password := "admin123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"admin-kick-rm02",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		password,
	)

	// User2 and User3 join
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		password,
		1,
	)

	helper.ApplyToGroupAndVerify(
		suite.fixtures.User3Client,
		suite.fixtures.RegularUser3.ID,
		group.ID,
		password,
		1,
	)

	// Owner promotes User2 to Admin
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	err := suite.fixtures.User1Client.PutJSON("/api/groups/members", map[string]interface{}{
		"group_id": group.ID,
		"user_id":  suite.fixtures.RegularUser2.ID,
		"role":     1, // Admin
	}, &resp)
	require.NoError(t, err, "Promotion should succeed")
	time.Sleep(200 * time.Millisecond)

	// Admin User2 kicks regular member User3
	t.Log("Admin User2 kicking regular member User3...")
	err = suite.fixtures.User2Client.UpdateMemberStatus(group.ID, suite.fixtures.RegularUser3.ID, 3)
	require.NoError(t, err, "Admin should be able to kick regular members")

	// Verify User3 was kicked
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser3.ID, group.ID, 3)
	suite.inspector.AssertDBNotContains(suite.fixtures.RegularUser3.ID, group.ID)

	t.Log("Admin kicks regular member verified")
}

// TestRM03_AdminCannotKickOwner tests that admin cannot kick the owner.
// Priority: P1
func TestRM03_AdminCannotKickOwner(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a group
	password := "admin123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"admin-no-kick-owner-rm03",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		password,
	)

	// User2 joins
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		password,
		1,
	)

	// Owner promotes User2 to Admin
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	err := suite.fixtures.User1Client.PutJSON("/api/groups/members", map[string]interface{}{
		"group_id": group.ID,
		"user_id":  suite.fixtures.RegularUser2.ID,
		"role":     1, // Admin
	}, &resp)
	require.NoError(t, err, "Promotion should succeed")
	time.Sleep(200 * time.Millisecond)

	// Admin User2 attempts to kick Owner User1
	t.Log("Admin User2 attempting to kick Owner User1...")
	err = suite.fixtures.User2Client.UpdateMemberStatus(group.ID, suite.fixtures.RegularUser1.ID, 3)

	// Should fail (cannot kick owner)
	assert.Error(t, err, "Admin should not be able to kick owner")
	assert.Contains(t, err.Error(), "owner", "Error should mention owner protection")

	// Verify Owner is still intact
	groupInfo, err := helper.GetGroupInfo(group.ID)
	require.NoError(t, err, "Should be able to get group info")
	assert.Equal(t, suite.fixtures.RegularUser1.ID, groupInfo.OwnerId, "Owner should still be User1")

	t.Log("Admin cannot kick owner verified")
}

// TestRM04_OwnerTransferOwnership tests the ownership transfer process.
// Priority: P2
func TestRM04_OwnerTransferOwnership(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a group
	password := "transfer123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"ownership-transfer-rm04",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		password,
	)

	// User2 joins
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		password,
		1,
	)

	// Verify initial owner
	groupInfo, err := helper.GetGroupInfo(group.ID)
	require.NoError(t, err, "Should be able to get group info")
	assert.Equal(t, suite.fixtures.RegularUser1.ID, groupInfo.OwnerId, "Initial owner should be User1")

	// Owner User1 transfers ownership to User2
	t.Log("Owner User1 transferring ownership to User2...")
	err = helper.UpdateGroupConfig(suite.fixtures.User1Client, group.ID, map[string]interface{}{
		"owner_id": suite.fixtures.RegularUser2.ID,
	})
	require.NoError(t, err, "Ownership transfer should succeed")

	time.Sleep(300 * time.Millisecond)

	// Verify new owner
	groupInfo, err = helper.GetGroupInfo(group.ID)
	require.NoError(t, err, "Should be able to get group info")
	assert.Equal(t, suite.fixtures.RegularUser2.ID, groupInfo.OwnerId, "New owner should be User2")

	// Verify cache was invalidated for both users
	// User1 should no longer see it as owned
	ownedGroups, err := suite.fixtures.User1Client.GetSelfOwnedGroups()
	require.NoError(t, err, "User1 should be able to query owned groups")

	for _, g := range ownedGroups {
		assert.NotEqual(t, group.ID, g.ID, "User1 should not see the group as owned")
	}

	// User2 should now see it as owned
	ownedGroups, err = suite.fixtures.User2Client.GetSelfOwnedGroups()
	require.NoError(t, err, "User2 should be able to query owned groups")

	groupFound := false
	for _, g := range ownedGroups {
		if g.ID == group.ID {
			groupFound = true
			break
		}
	}
	assert.True(t, groupFound, "User2 should see the group as owned")

	// Verify User1 is now a regular member
	memberInfo, err := suite.fixtures.User2Client.GetGroupMemberInfo(group.ID, suite.fixtures.RegularUser1.ID)
	if err == nil && memberInfo != nil {
		assert.Equal(t, 0, memberInfo.Role, "Former owner should be demoted to regular member")
	}

	t.Log("Ownership transfer verified")
}

// TestRM05_BatchApprovalOperations tests batch approval of multiple pending members.
// Priority: P1
func TestRM05_BatchApprovalOperations(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a review-mode group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"batch-approval-rm05",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		1, // Review
		"",
	)

	// Create 10 test users who all apply
	const userCount = 10
	testUsers := make([]*testutil.UserModel, userCount)
	testClients := make([]*testutil.APIClient, userCount)

	for i := 0; i < userCount; i++ {
		username := fmt.Sprintf("batch_approval_user_%d", i+1)
		user, err := suite.fixtures.CreateTestUser(username, "testpass123", "default")
		require.NoError(t, err, "Failed to create test user")
		testUsers[i] = user

		client := suite.client.Clone()
		_, err = client.Login(username, "testpass123")
		require.NoError(t, err, "Failed to login test user")
		testClients[i] = client

		// Apply to group
		err = testClients[i].ApplyToP2PGroup(group.ID, "")
		require.NoError(t, err, "Apply should succeed")
	}

	time.Sleep(300 * time.Millisecond)

	// Verify all are Pending
	for i := 0; i < userCount; i++ {
		suite.inspector.AssertMemberStatus(testUsers[i].ID, group.ID, 0)
	}

	// Owner batch-approves all pending members
	t.Log("Owner batch-approving 10 pending members...")

	// Note: This could be done via a batch API (if implemented) or loop
	// For now, we'll loop through individual approvals
	var wg sync.WaitGroup
	wg.Add(userCount)

	errors := make(chan error, userCount)

	for i := 0; i < userCount; i++ {
		go func(idx int) {
			defer wg.Done()

			err := suite.fixtures.User1Client.UpdateMemberStatus(group.ID, testUsers[idx].ID, 1)
			if err != nil {
				errors <- fmt.Errorf("approval %d failed: %w", idx+1, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Batch approval error: %v", err)
		errorCount++
	}
	assert.Equal(t, 0, errorCount, "All batch approvals should succeed")

	time.Sleep(500 * time.Millisecond)

	// Verify all are now Active
	for i := 0; i < userCount; i++ {
		suite.inspector.AssertMemberStatus(testUsers[i].ID, group.ID, 1)
		suite.inspector.AssertDBContains(testUsers[i].ID, group.ID)
	}

	// Verify member count
	helper.AssertMemberCount(suite.fixtures.User1Client, group.ID, userCount)

	t.Log("Batch approval operations verified")
}

// TestRM06_BatchKickOperations tests batch kicking of multiple active members.
// Priority: P1
func TestRM06_BatchKickOperations(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a password-protected group
	password := "batch123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"batch-kick-rm06",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		password,
	)

	// Create 10 test users who all join
	const userCount = 10
	testUsers := make([]*testutil.UserModel, userCount)
	testClients := make([]*testutil.APIClient, userCount)

	for i := 0; i < userCount; i++ {
		username := fmt.Sprintf("batch_kick_user_%d", i+1)
		user, err := suite.fixtures.CreateTestUser(username, "testpass123", "default")
		require.NoError(t, err, "Failed to create test user")
		testUsers[i] = user

		client := suite.client.Clone()
		_, err = client.Login(username, "testpass123")
		require.NoError(t, err, "Failed to login test user")
		testClients[i] = client

		// Join the group
		helper.ApplyToGroupAndVerify(testClients[i], testUsers[i].ID, group.ID, password, 1)
	}

	// Verify all are Active
	for i := 0; i < userCount; i++ {
		suite.inspector.AssertMemberStatus(testUsers[i].ID, group.ID, 1)
	}

	// Owner batch-kicks all members
	t.Log("Owner batch-kicking 10 members...")

	var wg sync.WaitGroup
	wg.Add(userCount)

	errors := make(chan error, userCount)

	for i := 0; i < userCount; i++ {
		go func(idx int) {
			defer wg.Done()

			err := suite.fixtures.User1Client.UpdateMemberStatus(group.ID, testUsers[idx].ID, 3)
			if err != nil {
				errors <- fmt.Errorf("kick %d failed: %w", idx+1, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Batch kick error: %v", err)
		errorCount++
	}
	assert.Equal(t, 0, errorCount, "All batch kicks should succeed")

	time.Sleep(500 * time.Millisecond)

	// Verify all are now Banned
	for i := 0; i < userCount; i++ {
		suite.inspector.AssertMemberStatus(testUsers[i].ID, group.ID, 3)
		suite.inspector.AssertDBNotContains(testUsers[i].ID, group.ID)
	}

	// Verify active member count is 0 (only owner remains)
	members, err := suite.fixtures.User1Client.GetGroupMembers(group.ID, 1)
	require.NoError(t, err, "Should be able to query members")
	assert.Equal(t, 0, len(members), "Should have no active members after batch kick")

	// Verify all kicked users cannot see the group
	for i := 0; i < userCount; i++ {
		joinedGroups, err := testClients[i].GetSelfJoinedGroups()
		require.NoError(t, err, "User should be able to query groups")

		for _, g := range joinedGroups {
			assert.NotEqual(t, group.ID, g.ID, "User %d should not see the group after being kicked", i+1)
		}
	}

	t.Log("Batch kick operations verified")
}
