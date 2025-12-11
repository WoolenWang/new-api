// Package p2p_management contains integration tests for P2P group member lifecycle.
//
// Test Focus:
// ===========
// This package validates member lifecycle state machine transitions (MS-01 to MS-09).
//
// Member Status:
// - 0: Pending (waiting for approval)
// - 1: Active (joined successfully)
// - 2: Rejected (approval denied)
// - 3: Banned (kicked out by owner)
// - 4: Left (member left voluntarily)
//
// Key Test Scenarios:
// - MS-01: Password correct -> Active
// - MS-02: Password incorrect -> Pending
// - MS-03: Review mode -> Pending
// - MS-04: Approve Pending -> Active
// - MS-05: Reject Pending -> Rejected
// - MS-06: Owner kicks member -> Banned
// - MS-07: Member leaves -> record deleted
// - MS-08: Duplicate join attempt
// - MS-09: Query member list
package p2p_management

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMS01_PasswordCorrectDirectJoin tests that correct password leads to immediate Active status.
// Priority: P0
func TestMS01_PasswordCorrectDirectJoin(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a group with password protection
	password := "correct123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"password-group-ms01",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password method
		password,
	)

	// User2 applies with correct password
	t.Log("User2 applying with correct password...")
	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		password,
		1, // Expected status: Active
	)

	// Verify cache was invalidated
	time.Sleep(200 * time.Millisecond)
	suite.inspector.AssertDBContains(suite.fixtures.RegularUser2.ID, group.ID)

	// Verify User2 can see the group
	joinedGroups, err := suite.fixtures.User2Client.GetSelfJoinedGroups()
	require.NoError(t, err, "User2 should be able to query joined groups")

	groupFound := false
	for _, g := range joinedGroups {
		if g.ID == group.ID {
			groupFound = true
			break
		}
	}
	assert.True(t, groupFound, "User2 should see the joined group")

	t.Log("Password correct direct join verified")
}

// TestMS02_PasswordIncorrectPending tests that incorrect password leads to Pending status.
// Priority: P0
func TestMS02_PasswordIncorrectPending(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a group with password
	correctPassword := "correct123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"password-group-ms02",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password method
		correctPassword,
	)

	// User2 applies with incorrect password
	t.Log("User2 applying with incorrect password...")
	incorrectPassword := "wrong456"

	err := suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, incorrectPassword)
	require.NoError(t, err, "Apply should succeed (but go to Pending)")

	// Wait for operation to complete
	time.Sleep(200 * time.Millisecond)

	// Verify status is Pending (0)
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 0)

	// Verify cache was NOT invalidated (no Active membership)
	suite.inspector.AssertDBNotContains(suite.fixtures.RegularUser2.ID, group.ID)

	// Verify User2 does NOT see the group in joined list
	joinedGroups, err := suite.fixtures.User2Client.GetSelfJoinedGroups()
	require.NoError(t, err, "User2 should be able to query groups")

	for _, g := range joinedGroups {
		assert.NotEqual(t, group.ID, g.ID, "User2 should not see Pending group in joined list")
	}

	t.Log("Password incorrect leads to Pending status verified")
}

// TestMS03_ReviewModeEntersPending tests that review mode always enters Pending status.
// Priority: P0
func TestMS03_ReviewModeEntersPending(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a group with review mode (no password)
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"review-group-ms03",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		1, // Review method
		"",
	)

	// User2 applies (no password needed)
	t.Log("User2 applying to review-mode group...")
	err := suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, "")
	require.NoError(t, err, "Apply should succeed")

	// Wait for operation to complete
	time.Sleep(200 * time.Millisecond)

	// Verify status is Pending (0)
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 0)

	// Verify no Active membership
	suite.inspector.AssertDBNotContains(suite.fixtures.RegularUser2.ID, group.ID)

	t.Log("Review mode enters Pending status verified")
}

// TestMS04_ApprovalProcess tests the approval workflow: Pending -> Active.
// Priority: P0
func TestMS04_ApprovalProcess(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a review-mode group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"approval-group-ms04",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		1, // Review method
		"",
	)

	// User2 applies
	err := suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, "")
	require.NoError(t, err, "Apply should succeed")
	time.Sleep(200 * time.Millisecond)

	// Verify Pending status
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 0)

	// Owner (User1) approves the application
	t.Log("Owner approving User2's application...")
	helper.ApproveAndVerify(suite.fixtures.User1Client, suite.fixtures.RegularUser2.ID, group.ID)

	// Verify status changed to Active
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 1)

	// Verify Active membership in DB
	suite.inspector.AssertDBContains(suite.fixtures.RegularUser2.ID, group.ID)

	// Verify User2 can now see the group
	joinedGroups, err := suite.fixtures.User2Client.GetSelfJoinedGroups()
	require.NoError(t, err, "User2 should be able to query groups")

	groupFound := false
	for _, g := range joinedGroups {
		if g.ID == group.ID {
			groupFound = true
			break
		}
	}
	assert.True(t, groupFound, "User2 should see the approved group")

	t.Log("Approval process: Pending -> Active verified")
}

// TestMS05_RejectionProcess tests the rejection workflow: Pending -> Rejected.
// Priority: P1
func TestMS05_RejectionProcess(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a review-mode group
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"rejection-group-ms05",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		1, // Review method
		"",
	)

	// User2 applies
	err := suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, "")
	require.NoError(t, err, "Apply should succeed")
	time.Sleep(200 * time.Millisecond)

	// Verify Pending status
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 0)

	// Owner (User1) rejects the application
	t.Log("Owner rejecting User2's application...")
	err = suite.fixtures.User1Client.UpdateMemberStatus(group.ID, suite.fixtures.RegularUser2.ID, 2)
	require.NoError(t, err, "Rejection should succeed")
	time.Sleep(200 * time.Millisecond)

	// Verify status changed to Rejected (2)
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 2)

	// Verify no Active membership
	suite.inspector.AssertDBNotContains(suite.fixtures.RegularUser2.ID, group.ID)

	// Verify User2 still cannot see the group
	joinedGroups, err := suite.fixtures.User2Client.GetSelfJoinedGroups()
	require.NoError(t, err, "User2 should be able to query groups")

	for _, g := range joinedGroups {
		assert.NotEqual(t, group.ID, g.ID, "User2 should not see rejected group")
	}

	t.Log("Rejection process: Pending -> Rejected verified")
}

// TestMS06_OwnerKicksMember tests the kick workflow: Active -> Banned.
// Priority: P0
func TestMS06_OwnerKicksMember(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a group and User2 joins
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"kick-group-ms06",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		"pass123",
	)

	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		"pass123",
		1, // Active
	)

	// Verify User2 is Active
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 1)
	suite.inspector.AssertDBContains(suite.fixtures.RegularUser2.ID, group.ID)

	// Owner (User1) kicks User2
	t.Log("Owner kicking User2 from group...")
	helper.KickAndVerify(suite.fixtures.User1Client, suite.fixtures.RegularUser2.ID, group.ID)

	// Verify status changed to Banned (3)
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 3)

	// Verify Active membership removed
	suite.inspector.AssertDBNotContains(suite.fixtures.RegularUser2.ID, group.ID)

	// Verify User2 can no longer see the group
	joinedGroups, err := suite.fixtures.User2Client.GetSelfJoinedGroups()
	require.NoError(t, err, "User2 should be able to query groups")

	for _, g := range joinedGroups {
		assert.NotEqual(t, group.ID, g.ID, "User2 should not see the group after being kicked")
	}

	t.Log("Owner kicks member: Active -> Banned verified")
}

// TestMS07_MemberLeaves tests the leave workflow: Active -> record deleted.
// Priority: P0
func TestMS07_MemberLeaves(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a group and User2 joins
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"leave-group-ms07",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		"pass123",
	)

	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		"pass123",
		1, // Active
	)

	// Verify User2 is Active
	suite.inspector.AssertDBContains(suite.fixtures.RegularUser2.ID, group.ID)

	// User2 leaves the group
	t.Log("User2 leaving group voluntarily...")
	helper.LeaveAndVerify(suite.fixtures.User2Client, suite.fixtures.RegularUser2.ID, group.ID)

	// Verify Active membership removed
	suite.inspector.AssertDBNotContains(suite.fixtures.RegularUser2.ID, group.ID)

	// Verify the record is either deleted or marked as Left (status=4)
	status, exists, err := suite.inspector.GetMemberStatus(suite.fixtures.RegularUser2.ID, group.ID)
	require.NoError(t, err, "Should be able to check member status")

	if exists {
		assert.Equal(t, 4, status, "Status should be Left (4) if record still exists")
	} else {
		t.Log("Membership record was deleted (acceptable)")
	}

	// Verify User2 can no longer see the group
	joinedGroups, err := suite.fixtures.User2Client.GetSelfJoinedGroups()
	require.NoError(t, err, "User2 should be able to query groups")

	for _, g := range joinedGroups {
		assert.NotEqual(t, group.ID, g.ID, "User2 should not see the group after leaving")
	}

	t.Log("Member leaves: Active -> deleted verified")
}

// TestMS08_DuplicateJoinAttempt tests that duplicate join attempts are rejected.
// Priority: P1
func TestMS08_DuplicateJoinAttempt(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a group and User2 joins
	password := "pass123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"duplicate-group-ms08",
		suite.fixtures.RegularUser1.ID,
		2, // Shared
		2, // Password
		password,
	)

	helper.ApplyToGroupAndVerify(
		suite.fixtures.User2Client,
		suite.fixtures.RegularUser2.ID,
		group.ID,
		password,
		1, // Active
	)

	// Verify User2 is Active
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 1)

	// User2 tries to join again
	t.Log("User2 attempting to join again (duplicate)...")
	err := suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, password)

	// Should get an error (already a member)
	assert.Error(t, err, "Duplicate join should be rejected")
	assert.Contains(t, err.Error(), "already", "Error should indicate user is already a member")

	// Verify status unchanged
	suite.inspector.AssertMemberStatus(suite.fixtures.RegularUser2.ID, group.ID, 1)

	t.Log("Duplicate join attempt rejected verified")
}

// TestMS09_QueryMemberList tests querying the member list of a group.
// Priority: P1
func TestMS09_QueryMemberList(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a group
	password := "pass123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"member-list-group-ms09",
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
		1, // Active
	)

	helper.ApplyToGroupAndVerify(
		suite.fixtures.User3Client,
		suite.fixtures.RegularUser3.ID,
		group.ID,
		password,
		1, // Active
	)

	// Query member list (as Owner)
	t.Log("Querying member list...")
	members, err := suite.fixtures.User1Client.GetGroupMembers(group.ID, 1) // status=1 (Active)
	require.NoError(t, err, "Should be able to query member list")

	// Verify member count (Owner + 2 members = 3 total, but query might not include owner)
	// Check if User2 and User3 are in the list
	memberIDs := make([]int, len(members))
	for i, m := range members {
		memberIDs[i] = m.UserID
	}

	assert.Contains(t, memberIDs, suite.fixtures.RegularUser2.ID, "Member list should include User2")
	assert.Contains(t, memberIDs, suite.fixtures.RegularUser3.ID, "Member list should include User3")

	t.Logf("Member list query verified: %d active members", len(members))

	// Query Pending members (should be empty)
	pendingMembers, err := suite.fixtures.User1Client.GetGroupMembers(group.ID, 0) // status=0 (Pending)
	require.NoError(t, err, "Should be able to query pending members")
	assert.Empty(t, pendingMembers, "Should have no pending members")

	t.Log("Member list query verified")
}
