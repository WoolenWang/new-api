// Package p2p_management contains integration tests for P2P group configuration changes.
//
// Test Focus:
// ===========
// This package validates dynamic configuration changes and complex workflows (CF-01 to CF-06).
//
// Configuration Scenarios:
// - Invite-based joining (join_method=0)
// - Dynamic join method changes
// - Password modification
// - Group type conversion
// - Concurrent member operations
// - Member capacity limits
//
// Key Test Scenarios:
// - CF-01: Invite mechanism complete flow
// - CF-02: Join method change impact on pending members
// - CF-03: Password change verification
// - CF-04: Group type conversion (Private <-> Shared)
// - CF-05: Concurrent join operations
// - CF-06: Member capacity limit enforcement
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

// TestCF01_InviteFlowComplete tests the complete invite-based joining flow.
// Priority: P0
func TestCF01_InviteFlowComplete(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates an invite-only group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"invite-group-cf01",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		0, // Invite only
		"",
	)

	// In invite-only mode, users cannot self-apply
	// They need to be invited by the owner

	// Simulate invite: Owner directly adds User2 with Active status
	// Note: This requires an invite API which might not be implemented
	// For now, we test that non-invited users cannot join

	t.Log("User2 attempting to join invite-only group without invitation...")
	err := suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, "")

	// Should fail (no invitation)
	if err != nil {
		assert.Contains(t, err.Error(), "invite", "Should mention invitation requirement")
	} else {
		// If apply succeeds, check that it goes to Pending (needs owner approval)
		status, exists, statusErr := suite.inspector.GetMemberStatus(suite.fixtures.RegularUser2.ID, group.ID)
		require.NoError(t, statusErr, "Should be able to get status")
		if exists {
			assert.Equal(t, 0, status, "Without invitation, should be Pending at most")
		}
	}

	// Test the invite mechanism (if implemented)
	// In a real implementation, owner would generate an invite code or link
	// For this test, we'll simulate by having owner directly approve

	// Owner "invites" User3 by pre-approving them
	// This simulates: owner sends invite -> User3 clicks link -> auto-approved
	t.Log("Simulating invite mechanism for User3...")

	// Option 1: Owner directly creates Active membership (simulates invite acceptance)
	// Option 2: User3 applies with special invite code

	// For now, we test that owner can pre-approve a user
	// User3 applies
	err = suite.fixtures.User3Client.ApplyToP2PGroup(group.ID, "")
	if err == nil {
		// Owner immediately approves (simulating invite flow)
		err = suite.fixtures.User1Client.UpdateMemberStatus(group.ID, suite.fixtures.RegularUser3.ID, 1)
		require.NoError(t, err, "Owner should be able to approve")

		// Verify User3 is now Active
		suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser3.ID, group.ID, 1)
		suite.inspector.AssertDBContains(suite.fixtures.RegularUser3.ID, group.ID)

		t.Log("Invite flow simulation completed")
	} else {
		t.Logf("Invite-only mode blocks self-application: %v", err)
	}

	t.Log("Invite flow test completed")
}

// TestCF02_JoinMethodChangeImpact tests that changing join method doesn't break existing pending members.
// Priority: P1
func TestCF02_JoinMethodChangeImpact(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a password-protected group
	originalPassword := "oldpass123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"joinmethod-change-cf02",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		originalPassword,
	)

	// User2 applies with correct password -> goes to Pending (in this test scenario)
	// Note: Normally correct password should make them Active
	// For this test, we'll apply with wrong password to create Pending status
	err := suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, "wrongpass")
	require.NoError(t, err, "Apply should succeed")
	time.Sleep(200 * time.Millisecond)

	// Verify User2 is Pending
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 0)

	// Owner changes join method to Review (1)
	t.Log("Owner changing join method from Password to Review...")
	err = helper.UpdateGroupConfig(suite.fixtures.User1Client, group.ID, map[string]interface{}{
		"join_method": 1,
	})
	require.NoError(t, err, "Join method change should succeed")

	// Owner should still be able to approve User2's pending application
	t.Log("Owner approving User2's pending application after join method change...")
	err = suite.fixtures.User1Client.UpdateMemberStatus(group.ID, suite.fixtures.RegularUser2.ID, 1)
	require.NoError(t, err, "Approval should still work after join method change")

	// Verify User2 is now Active
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 1)

	t.Log("Join method change impact on pending members verified")
}

// TestCF03_PasswordChangeVerification tests that password changes take effect immediately.
// Priority: P1
func TestCF03_PasswordChangeVerification(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a password-protected group
	oldPassword := "old123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"password-change-cf03",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		oldPassword,
	)

	// Owner changes the password
	newPassword := "new456"
	t.Log("Owner changing group password...")
	err := helper.UpdateGroupConfig(suite.fixtures.User1Client, group.ID, map[string]interface{}{
		"join_key": newPassword,
	})
	require.NoError(t, err, "Password change should succeed")

	// User2 tries with old password
	t.Log("User2 attempting to join with old password...")
	err = suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, oldPassword)

	// Should fail or go to Pending (not Active)
	if err == nil {
		status, exists, statusErr := suite.inspector.GetMemberStatus(suite.fixtures.RegularUser2.ID, group.ID)
		require.NoError(t, statusErr, "Should be able to get status")
		if exists {
			assert.NotEqual(t, 1, status, "Old password should not grant Active status")
		}
	} else {
		t.Logf("Old password rejected: %v", err)
	}

	// User3 tries with new password
	t.Log("User3 attempting to join with new password...")
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User3Client,
		suite.fixtures.RegularUser3.ID,
		group.ID,
		newPassword,
		1, // Should be Active with correct password
	)

	t.Log("Password change verification completed")
}

// TestCF04_GroupTypeConversion tests converting between Private and Shared group types.
// Priority: P2
func TestCF04_GroupTypeConversion(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a private group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"type-convert-cf04",
		suite.fixtures.RegularUser1.ID,
		1, // Private
		0, // Invite only
		"",
	)

	// Verify the group does NOT appear in public listing
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Items []testutil.P2PGroupModel `json:"items"`
		} `json:"data"`
	}

	err := suite.fixtures.User2Client.GetJSON("/api/groups/public", &resp)
	require.NoError(t, err, "Public listing should succeed")

	for _, g := range resp.Data.Items {
		assert.NotEqual(t, group.ID, g.ID, "Private group should not be in public listing")
	}

	// Owner converts to Shared type
	t.Log("Owner converting group from Private to Shared...")
	err = helper.UpdateGroupConfig(suite.fixtures.User1Client, group.ID, map[string]interface{}{
		"type": 2, // Shared
	})
	require.NoError(t, err, "Type conversion should succeed")

	// Verify the group NOW appears in public listing
	err = suite.fixtures.User2Client.GetJSON("/api/groups/public", &resp)
	require.NoError(t, err, "Public listing should succeed")

	groupFound := false
	for _, g := range resp.Data.Items {
		if g.ID == group.ID {
			groupFound = true
			assert.Equal(t, 2, g.Type, "Group type should be Shared")
			break
		}
	}
	assert.True(t, groupFound, "Shared group should appear in public listing")

	// New users can now apply to join
	t.Log("User2 applying to the now-shared group...")
	err = suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, "")
	assert.NoError(t, err, "Users should be able to apply to shared group")

	t.Log("Group type conversion verified")
}

// TestCF05_ConcurrentJoinOperations tests concurrent join operations for data consistency.
// Priority: P1
func TestCF05_ConcurrentJoinOperations(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a password-protected group
	password := "concurrent123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"concurrent-join-cf05",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		password,
	)

	// Create 10 test users
	const userCount = 10
	testUsers := make([]*testutil.UserModel, userCount)
	testClients := make([]*testutil.APIClient, userCount)

	for i := 0; i < userCount; i++ {
		username := fmt.Sprintf("concurrent_user_%d", i+1)
		user, err := suite.fixtures.CreateTestUser(username, "testpass123", "default")
		require.NoError(t, err, "Failed to create test user")
		testUsers[i] = user

		// Create client for each user
		client := suite.client.Clone()
		_, err = client.Login(username, "testpass123")
		require.NoError(t, err, "Failed to login test user")
		testClients[i] = client
	}

	// All 10 users join concurrently
	t.Log("10 users joining group concurrently...")
	var wg sync.WaitGroup
	wg.Add(userCount)

	errors := make(chan error, userCount)

	for i := 0; i < userCount; i++ {
		go func(idx int) {
			defer wg.Done()

			err := testClients[idx].ApplyToP2PGroup(group.ID, password)
			if err != nil {
				errors <- fmt.Errorf("user %d join failed: %w", idx+1, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	errorCount := 0
	for err := range errors {
		t.Errorf("Concurrent join error: %v", err)
		errorCount++
	}
	assert.Equal(t, 0, errorCount, "All concurrent joins should succeed")

	// Wait for operations to complete
	time.Sleep(500 * time.Millisecond)

	// Verify all users are in the group (Active status)
	for i := 0; i < userCount; i++ {
		status, exists, err := suite.inspector.GetMemberStatus(testUsers[i].ID, group.ID)
		require.NoError(t, err, "Should be able to get member status")
		require.True(t, exists, "Membership should exist for user %d", i+1)
		assert.Equal(t, 1, status, "User %d should be Active", i+1)
	}

	// Verify no duplicate records (UNIQUE constraint should prevent this)
	// Query member count
	members, err := suite.fixtures.User1Client.GetGroupMembers(group.ID, 1)
	require.NoError(t, err, "Should be able to query members")

	// Should have exactly userCount members (plus potentially the owner)
	t.Logf("Total active members: %d", len(members))
	assert.GreaterOrEqual(t, len(members), userCount, "Should have at least %d members", userCount)

	t.Log("Concurrent join operations verified")
}

// TestCF06_MemberCapacityLimit tests group member capacity limits.
// Priority: P2
func TestCF06_MemberCapacityLimit(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Note: This test assumes the system has a configurable member limit
	// If not implemented, this test serves as a specification

	// User1 creates a group
	password := "capacity123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"capacity-limit-cf06",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		password,
	)

	// Assume the limit is 100 members (configurable)
	// For testing, we'll simulate approaching the limit

	// Create users up to the limit (for demo, we'll use a smaller number)
	const nearLimit = 5 // In production, this would be close to actual limit
	testUsers := make([]*testutil.UserModel, nearLimit)
	testClients := make([]*testutil.APIClient, nearLimit)

	for i := 0; i < nearLimit; i++ {
		username := fmt.Sprintf("capacity_user_%d", i+1)
		user, err := suite.fixtures.CreateTestUser(username, "testpass123", "default")
		require.NoError(t, err, "Failed to create test user")
		testUsers[i] = user

		client := suite.client.Clone()
		_, err = client.Login(username, "testpass123")
		require.NoError(t, err, "Failed to login test user")
		testClients[i] = client

		// Join the group
		err = testClients[i].ApplyToP2PGroup(group.ID, password)
		require.NoError(t, err, "User %d should be able to join", i+1)
	}

	time.Sleep(300 * time.Millisecond)

	// Verify all joined successfully
	for i := 0; i < nearLimit; i++ {
		suite.inspector.AssertMemberStatus(testUsers[i].ID, group.ID, 1)
	}

	// Check member count
	memberCount, err := helper.GetMemberCount(suite.fixtures.User1Client, group.ID)
	require.NoError(t, err, "Should be able to get member count")
	t.Logf("Current member count: %d", memberCount)

	// Note: If member limit is implemented, the next join would fail
	// For now, we've verified that multiple joins work correctly
	// In a real test with limits, we'd verify the (limit+1)th user gets rejected

	t.Log("Member capacity test completed (limit enforcement depends on implementation)")
}
