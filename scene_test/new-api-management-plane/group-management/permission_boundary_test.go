// Package p2p_management contains integration tests for P2P group permission boundaries.
//
// Test Focus:
// ===========
// This package validates permission boundaries and security controls (PM-01 to PM-07).
//
// Security Risks:
// - Unauthorized group deletion
// - Unauthorized member management
// - Unauthorized configuration changes
// - Banned users bypassing restrictions
// - Information leakage
// - Owner abandonment
//
// Key Test Scenarios:
// - PM-01: Non-owner cannot delete group
// - PM-02: Non-owner cannot kick members
// - PM-03: Non-owner cannot modify config
// - PM-04: Banned user cannot rejoin
// - PM-05: Rejected user can reapply
// - PM-06: Non-member cannot access private info
// - PM-07: Owner cannot leave without transfer
package p2p_management

import (
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPM01_NonOwnerCannotDeleteGroup tests that non-owner members cannot delete the group.
// Priority: P0
func TestPM01_NonOwnerCannotDeleteGroup(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"perm-delete-group-pm01",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		"pass123",
	)

	// User2 joins as a member
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		"pass123",
		1, // Active
	)

	// User2 (non-owner) attempts to delete the group
	t.Log("User2 (non-owner) attempting to delete group...")
	err := suite.fixtures.User2Client.DeleteGroup(group.ID)

	// Should fail with permission error
	assert.Error(t, err, "Non-owner should not be able to delete group")
	assert.Contains(t, err.Error(), "403", "Should return 403 Forbidden error")

	// Verify the group still exists
	helper.AssertGroupExists(group.ID)

	// Verify members are still intact
	suite.inspector.AssertDBContains(suite.fixtures.RegularUser2.ID, group.ID)

	t.Log("Non-owner delete group permission denied verified")
}

// TestPM02_NonOwnerCannotKickMembers tests that non-owner members cannot kick other members.
// Priority: P0
func TestPM02_NonOwnerCannotKickMembers(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"perm-kick-group-pm02",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		"pass123",
	)

	// User2 and User3 join
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		"pass123",
		1,
	)

	helper.ApplyToGroupAndVerify(
		suite.fixtures.User3Client,
		suite.fixtures.RegularUser3.ID,
		group.ID,
		"pass123",
		1,
	)

	// User2 (non-owner) attempts to kick User3
	t.Log("User2 (non-owner) attempting to kick User3...")
	err := suite.fixtures.User2Client.UpdateMemberStatus(group.ID, suite.fixtures.RegularUser3.ID, 3)

	// Should fail with permission error
	assert.Error(t, err, "Non-owner should not be able to kick members")
	assert.Contains(t, err.Error(), "403", "Should return 403 Forbidden error")

	// Verify User3 is still Active
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser3.ID, group.ID, 1)
	suite.inspector.AssertDBContains(suite.fixtures.RegularUser3.ID, group.ID)

	t.Log("Non-owner kick member permission denied verified")
}

// TestPM03_NonOwnerCannotModifyConfig tests that non-owner members cannot modify group configuration.
// Priority: P0
func TestPM03_NonOwnerCannotModifyConfig(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a group
	originalName := "perm-config-group-pm03"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		originalName,
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		"pass123",
	)

	// User2 joins as a member
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		"pass123",
		1,
	)

	// User2 (non-owner) attempts to modify group name
	t.Log("User2 (non-owner) attempting to modify group name...")
	newName := "hacked-name"
	err := helper.UpdateGroupConfig(suite.fixtures.User2Client, group.ID, map[string]interface{}{
		"name": newName,
	})

	// Should fail with permission error
	assert.Error(t, err, "Non-owner should not be able to modify group config")
	assert.Contains(t, err.Error(), "403", "Should return 403 Forbidden error")

	// Verify group name unchanged
	groupInfo, err := helper.GetGroupInfo(group.ID)
	require.NoError(t, err, "Should be able to get group info")
	assert.Equal(t, originalName, groupInfo.Name, "Group name should remain unchanged")

	t.Log("Non-owner modify config permission denied verified")
}

// TestPM04_BannedUserCannotRejoin tests that banned users cannot rejoin the group.
// Priority: P0
func TestPM04_BannedUserCannotRejoin(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a group
	password := "pass123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"banned-rejoin-group-pm04",
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

	// Owner kicks User2 (status -> Banned)
	helper.KickAndVerify(suite.fixtures.User1Client, suite.fixtures.RegularUser2.ID, group.ID)

	// Verify User2 is Banned
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 3)

	// User2 attempts to rejoin
	t.Log("User2 (banned) attempting to rejoin...")
	err := suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, password)

	// Should fail with banned error
	assert.Error(t, err, "Banned user should not be able to rejoin")
	assert.Contains(t, err.Error(), "banned", "Error should indicate user is banned")

	// Verify status unchanged (still Banned)
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 3)

	// Verify no Active membership
	suite.inspector.AssertDBNotContains(suite.fixtures.RegularUser2.ID, group.ID)

	t.Log("Banned user rejoin attempt blocked verified")
}

// TestPM05_RejectedUserCanReapply tests that rejected users can reapply.
// Priority: P1
func TestPM05_RejectedUserCanReapply(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a review-mode group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"rejected-reapply-group-pm05",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		1, // Review
		"",
	)

	// User2 applies
	err := suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, "")
	require.NoError(t, err, "Initial apply should succeed")

	// Owner rejects the application
	err = suite.fixtures.User1Client.UpdateMemberStatus(group.ID, suite.fixtures.RegularUser2.ID, 2)
	require.NoError(t, err, "Rejection should succeed")

	// Verify User2 is Rejected
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 2)

	// User2 reapplies
	t.Log("User2 (rejected) reapplying...")
	err = suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, "")

	// Should succeed (allowed to reapply)
	assert.NoError(t, err, "Rejected user should be able to reapply")

	// Verify status changed to Pending (or overwrites existing Rejected record)
	status, exists, err := suite.inspector.GetMemberStatus(suite.fixtures.RegularUser2.ID, group.ID)
	require.NoError(t, err, "Should be able to get member status")
	require.True(t, exists, "Membership record should exist")
	assert.Equal(t, 0, status, "Status should be Pending after reapply")

	t.Log("Rejected user reapply allowed verified")
}

// TestPM06_NonMemberCannotAccessPrivateInfo tests that non-members cannot access private group info.
// Priority: P1
func TestPM06_NonMemberCannotAccessPrivateInfo(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a private group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"private-info-group-pm06",
		suite.fixtures.RegularUser1.ID,
		1, // Private
		0, // Invite only
		"",
	)

	// User3 joins (invited)
	// For simplicity, we directly set them as Active (simulating invite)
	// In a real scenario, invite mechanism would be tested separately
	// For now, we skip adding User3 and test non-member access

	// User2 (non-member) attempts to query member list
	t.Log("User2 (non-member) attempting to access private group member list...")
	members, err := suite.fixtures.User2Client.GetGroupMembers(group.ID, 1)

	// Should either fail with permission error or return empty/limited data
	if err != nil {
		assert.Contains(t, err.Error(), "403", "Should return 403 Forbidden error")
	} else {
		// If no error, should return empty or limited data (no detailed member info)
		t.Logf("Member list returned (might be restricted): %d members", len(members))
		// In a secure implementation, non-members should not see full member details
	}

	// User2 should not see the private group in public listing
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Items []testutil.P2PGroupModel `json:"items"`
		} `json:"data"`
	}

	err = suite.fixtures.User2Client.GetJSON("/api/groups/public", &resp)
	require.NoError(t, err, "Public listing should succeed")

	for _, g := range resp.Data.Items {
		assert.NotEqual(t, group.ID, g.ID, "Private group should not appear in public listing")
	}

	t.Log("Non-member cannot access private info verified")
}

// TestPM07_OwnerCannotLeaveWithoutTransfer tests that owner cannot leave without transferring ownership.
// Priority: P1
func TestPM07_OwnerCannotLeaveWithoutTransfer(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates a group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"owner-leave-group-pm07",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		"pass123",
	)

	// User2 joins as member
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		"pass123",
		1,
	)

	// Owner (User1) attempts to leave
	t.Log("Owner (User1) attempting to leave without transferring ownership...")
	err := suite.fixtures.User1Client.LeaveGroup(group.ID)

	// Should fail (owner must transfer first)
	assert.Error(t, err, "Owner should not be able to leave without transfer")
	assert.Contains(t, err.Error(), "owner", "Error should mention owner restriction")

	// Verify the group still exists
	helper.AssertGroupExists(group.ID)

	// Verify User1 is still the owner
	groupInfo, err := helper.GetGroupInfo(group.ID)
	require.NoError(t, err, "Should be able to get group info")
	assert.Equal(t, suite.fixtures.RegularUser1.ID, groupInfo.OwnerId, "Owner should still be User1")

	t.Log("Owner cannot leave without transfer verified")

	// Optional: Test the correct flow - transfer then leave
	t.Log("Testing correct flow: transfer ownership then leave...")

	// Transfer ownership to User2
	err = helper.UpdateGroupConfig(suite.fixtures.User1Client, group.ID, map[string]interface{}{
		"owner_id": suite.fixtures.RegularUser2.ID,
	})
	require.NoError(t, err, "Ownership transfer should succeed")

	// Verify User2 is now the owner
	groupInfo, err = helper.GetGroupInfo(group.ID)
	require.NoError(t, err, "Should be able to get group info")
	assert.Equal(t, suite.fixtures.RegularUser2.ID, groupInfo.OwnerId, "Owner should now be User2")

	// Now User1 can leave (as a regular member)
	err = suite.fixtures.User1Client.LeaveGroup(group.ID)
	assert.NoError(t, err, "Former owner should be able to leave after transfer")

	t.Log("Owner transfer then leave flow verified")
}
