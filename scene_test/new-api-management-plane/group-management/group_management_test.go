// Package p2p_management contains integration tests for P2P group management APIs.
//
// Test Focus:
// ===========
// This package validates the P2P group management functionality including:
// - Group creation, update, deletion
// - Membership application and approval workflow
// - Password-based joining
// - Group discovery (public groups)
//
// Key Test Scenarios:
// - G-01: Create private/shared groups
// - G-02: Password-based joining
// - G-03: Application and approval workflow
// - G-04: Member leave and kick
// - G-05: Group deletion with cascade
package p2p_management

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// TestSuite holds shared test resources for group management tests.
type TestSuite struct {
	Server *testutil.TestServer
	Client *testutil.APIClient
}

// SetupSuite initializes the test suite with a running server.
func SetupSuite(t *testing.T) (*TestSuite, func()) {
	t.Helper()

	projectRoot, err := findProjectRoot()
	if err != nil {
		t.Fatalf("failed to find project root: %v", err)
	}

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()

	server, err := testutil.StartServer(cfg)
	if err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}

	client := testutil.NewAPIClient(server)

	// Initialize system and login as root (admin user).
	rootUser, rootPass, err := client.InitializeSystem()
	if err != nil {
		_ = server.Stop()
		t.Fatalf("failed to initialize system: %v", err)
	}
	if _, err := client.Login(rootUser, rootPass); err != nil {
		_ = server.Stop()
		t.Fatalf("failed to login as root: %v", err)
	}

	suite := &TestSuite{
		Server: server,
		Client: client,
	}

	cleanup := func() {
		if err := server.Stop(); err != nil {
			t.Errorf("Failed to stop server: %v", err)
		}
	}

	return suite, cleanup
}

func findProjectRoot() (string, error) {
	return testutil.FindProjectRoot()
}

// createTestUser creates a user with a unique external_id to avoid UNIQUE
// constraint conflicts on users.external_id. It returns the created user model.
func createTestUser(t *testing.T, admin *testutil.APIClient, username, password, group string) *testutil.UserModel {
	t.Helper()

	user := &testutil.UserModel{
		Username:   username,
		Password:   password,
		Group:      group,
		Status:     1,
		ExternalId: fmt.Sprintf("gm_%s_%d", username, time.Now().UnixNano()),
	}

	id, err := admin.CreateUserFull(user)
	if err != nil {
		t.Fatalf("failed to create user %s: %v", username, err)
	}
	user.ID = id
	return user
}

// TestGroup_G01_CreateGroups tests creating private and shared P2P groups.
func TestGroup_G01_CreateGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create an owner user.
	ownerUsername := "g01_owner"
	ownerPassword := "password123"
	owner := createTestUser(t, admin, ownerUsername, ownerPassword, "default")
	ownerID := owner.ID

	ownerClient := admin.Clone()
	if _, err := ownerClient.Login(ownerUsername, ownerPassword); err != nil {
		t.Fatalf("failed to login owner user: %v", err)
	}

	// Create a private group (type=1).
	privateGroupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "g01_private_group",
		DisplayName: "G01 Private",
		Type:        model.GroupTypePrivate,
		JoinMethod:  model.JoinMethodInvite,
		Description: "G01 private group",
	})
	if err != nil {
		t.Fatalf("failed to create private group: %v", err)
	}
	if privateGroupID <= 0 {
		t.Fatalf("expected private group id > 0, got %d", privateGroupID)
	}

	// Create a shared group (type=2).
	sharedGroupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "g01_shared_group",
		DisplayName: "G01 Shared",
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodApproval,
		Description: "G01 shared group",
	})
	if err != nil {
		t.Fatalf("failed to create shared group: %v", err)
	}
	if sharedGroupID <= 0 {
		t.Fatalf("expected shared group id > 0, got %d", sharedGroupID)
	}

	// Verify both groups appear in the owner's self-owned group list with correct owner_id and type.
	ownedGroups, err := ownerClient.GetSelfOwnedGroups()
	if err != nil {
		t.Fatalf("failed to get self owned groups: %v", err)
	}

	var privateFound, sharedFound bool
	for _, g := range ownedGroups {
		switch g.ID {
		case privateGroupID:
			privateFound = true
			if g.OwnerId != ownerID {
				t.Fatalf("private group owner_id mismatch: expected %d, got %d", ownerID, g.OwnerId)
			}
			if g.Type != model.GroupTypePrivate {
				t.Fatalf("private group type mismatch: expected %d, got %d", model.GroupTypePrivate, g.Type)
			}
		case sharedGroupID:
			sharedFound = true
			if g.OwnerId != ownerID {
				t.Fatalf("shared group owner_id mismatch: expected %d, got %d", ownerID, g.OwnerId)
			}
			if g.Type != model.GroupTypeShared {
				t.Fatalf("shared group type mismatch: expected %d, got %d", model.GroupTypeShared, g.Type)
			}
		}
	}

	if !privateFound || !sharedFound {
		t.Fatalf("expected both private and shared groups to be present, privateFound=%v, sharedFound=%v", privateFound, sharedFound)
	}
}

// TestGroup_G02_PasswordJoin tests password-based group joining.
func TestGroup_G02_PasswordJoin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create group owner and member users.
	ownerUsername := "g02_owner"
	ownerPassword := "password123"
	memberUsername := "g02_member"
	memberPassword := "password123"

	createTestUser(t, admin, ownerUsername, ownerPassword, "default")

	member := createTestUser(t, admin, memberUsername, memberPassword, "default")
	memberID := member.ID

	ownerClient := admin.Clone()
	if _, err := ownerClient.Login(ownerUsername, ownerPassword); err != nil {
		t.Fatalf("failed to login owner user: %v", err)
	}

	memberClient := admin.Clone()
	if _, err := memberClient.Login(memberUsername, memberPassword); err != nil {
		t.Fatalf("failed to login member user: %v", err)
	}

	// Owner creates a shared group with password join.
	const joinPassword = "123456"
	groupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "g02_password_group",
		DisplayName: "G02 Password Group",
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodPassword,
		JoinKey:     joinPassword,
		Description: "G02 password join group",
	})
	if err != nil {
		t.Fatalf("failed to create password group: %v", err)
	}

	// Member tries to join with wrong password:
	// New design: request succeeds but creates a Pending membership,
	// and the member should not see the group in joined list yet.
	if err := memberClient.ApplyToP2PGroup(groupID, "wrong-password"); err != nil {
		t.Fatalf("expected apply with wrong password to succeed (Pending), but got error: %v", err)
	}

	memberInfoAfterWrong, err := admin.GetGroupMemberInfo(groupID, memberID)
	if err != nil {
		t.Fatalf("failed to get member info after wrong password join: %v", err)
	}
	if memberInfoAfterWrong.Status != model.MemberStatusPending {
		t.Fatalf("expected member status Pending(%d) after wrong password, got %d",
			model.MemberStatusPending, memberInfoAfterWrong.Status)
	}

	joinedGroups, err := memberClient.GetSelfJoinedGroups()
	if err != nil {
		t.Fatalf("failed to get joined groups after wrong password: %v", err)
	}
	for _, g := range joinedGroups {
		if g.ID == groupID {
			t.Fatalf("group should not be in joined list after wrong password")
		}
	}

	// Owner approves the pending application -> status should become Active.
	if err := ownerClient.UpdateMemberStatus(groupID, memberID, model.MemberStatusActive); err != nil {
		t.Fatalf("failed to approve member after wrong password: %v", err)
	}

	memberInfo, err := admin.GetGroupMemberInfo(groupID, memberID)
	if err != nil {
		t.Fatalf("failed to get member info after password join: %v", err)
	}
	if memberInfo.Status != model.MemberStatusActive {
		t.Fatalf("expected member status Active(%d) after password join, got %d", model.MemberStatusActive, memberInfo.Status)
	}

	joinedGroups, err = memberClient.GetSelfJoinedGroups()
	if err != nil {
		t.Fatalf("failed to get joined groups after successful join: %v", err)
	}
	found := false
	for _, g := range joinedGroups {
		if g.ID == groupID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected password group to appear in member's joined groups")
	}
}

// TestGroup_G03_ApplicationApproval tests the application and approval workflow.
func TestGroup_G03_ApplicationApproval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create owner, applicant B and applicant C.
	ownerUsername := "g03_owner"
	ownerPassword := "password123"
	userBUsername := "g03_userB"
	userBPassword := "password123"
	userCUsername := "g03_userC"
	userCPassword := "password123"

	createTestUser(t, admin, ownerUsername, ownerPassword, "default")

	userB := createTestUser(t, admin, userBUsername, userBPassword, "default")
	userBID := userB.ID

	userC := createTestUser(t, admin, userCUsername, userCPassword, "default")
	userCID := userC.ID

	ownerClient := admin.Clone()
	if _, err := ownerClient.Login(ownerUsername, ownerPassword); err != nil {
		t.Fatalf("failed to login owner user: %v", err)
	}

	userBClient := admin.Clone()
	if _, err := userBClient.Login(userBUsername, userBPassword); err != nil {
		t.Fatalf("failed to login user B: %v", err)
	}

	userCClient := admin.Clone()
	if _, err := userCClient.Login(userCUsername, userCPassword); err != nil {
		t.Fatalf("failed to login user C: %v", err)
	}

	// Owner creates a review-based group.
	groupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "g03_review_group",
		DisplayName: "G03 Review Group",
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodApproval,
		Description: "G03 review join group",
	})
	if err != nil {
		t.Fatalf("failed to create review group: %v", err)
	}

	// User B applies to join -> status should be Pending.
	if err := userBClient.ApplyToP2PGroup(groupID, ""); err != nil {
		t.Fatalf("user B apply to group failed: %v", err)
	}

	memberInfoB, err := admin.GetGroupMemberInfo(groupID, userBID)
	if err != nil {
		t.Fatalf("failed to get member info for user B: %v", err)
	}
	if memberInfoB.Status != model.MemberStatusPending {
		t.Fatalf("expected user B status Pending(%d) after apply, got %d", model.MemberStatusPending, memberInfoB.Status)
	}

	// Owner approves user B -> status should become Active.
	if err := ownerClient.UpdateMemberStatus(groupID, userBID, model.MemberStatusActive); err != nil {
		t.Fatalf("failed to approve user B: %v", err)
	}

	memberInfoB, err = admin.GetGroupMemberInfo(groupID, userBID)
	if err != nil {
		t.Fatalf("failed to get member info for user B after approval: %v", err)
	}
	if memberInfoB.Status != model.MemberStatusActive {
		t.Fatalf("expected user B status Active(%d) after approval, got %d", model.MemberStatusActive, memberInfoB.Status)
	}

	joinedGroupsB, err := userBClient.GetSelfJoinedGroups()
	if err != nil {
		t.Fatalf("failed to get joined groups for user B: %v", err)
	}
	foundB := false
	for _, g := range joinedGroupsB {
		if g.ID == groupID {
			foundB = true
			break
		}
	}
	if !foundB {
		t.Fatalf("expected group to appear in user B's joined groups after approval")
	}

	// Rejection flow: user C applies then gets rejected.
	if err := userCClient.ApplyToP2PGroup(groupID, ""); err != nil {
		t.Fatalf("user C apply to group failed: %v", err)
	}

	memberInfoC, err := admin.GetGroupMemberInfo(groupID, userCID)
	if err != nil {
		t.Fatalf("failed to get member info for user C: %v", err)
	}
	if memberInfoC.Status != model.MemberStatusPending {
		t.Fatalf("expected user C status Pending(%d) after apply, got %d", model.MemberStatusPending, memberInfoC.Status)
	}

	if err := ownerClient.UpdateMemberStatus(groupID, userCID, model.MemberStatusRejected); err != nil {
		t.Fatalf("failed to reject user C: %v", err)
	}

	memberInfoC, err = admin.GetGroupMemberInfo(groupID, userCID)
	if err != nil {
		t.Fatalf("failed to get member info for user C after rejection: %v", err)
	}
	if memberInfoC.Status != model.MemberStatusRejected {
		t.Fatalf("expected user C status Rejected(%d) after rejection, got %d", model.MemberStatusRejected, memberInfoC.Status)
	}

	joinedGroupsC, err := userCClient.GetSelfJoinedGroups()
	if err != nil {
		t.Fatalf("failed to get joined groups for user C: %v", err)
	}
	for _, g := range joinedGroupsC {
		if g.ID == groupID {
			t.Fatalf("rejected user C should not see group in joined groups")
		}
	}
}

// TestGroup_G04_LeaveAndKick tests member leaving and being kicked.
func TestGroup_G04_LeaveAndKick(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create owner and two members.
	ownerUsername := "g04_owner"
	ownerPassword := "password123"
	memberBUsername := "g04_memberB"
	memberBPassword := "password123"
	memberCUsername := "g04_memberC"
	memberCPassword := "password123"

	createTestUser(t, admin, ownerUsername, ownerPassword, "default")

	memberB := createTestUser(t, admin, memberBUsername, memberBPassword, "default")
	memberBID := memberB.ID

	memberC := createTestUser(t, admin, memberCUsername, memberCPassword, "default")
	memberCID := memberC.ID

	ownerClient := admin.Clone()
	if _, err := ownerClient.Login(ownerUsername, ownerPassword); err != nil {
		t.Fatalf("failed to login owner user: %v", err)
	}

	memberBClient := admin.Clone()
	if _, err := memberBClient.Login(memberBUsername, memberBPassword); err != nil {
		t.Fatalf("failed to login member B: %v", err)
	}

	memberCClient := admin.Clone()
	if _, err := memberCClient.Login(memberCUsername, memberCPassword); err != nil {
		t.Fatalf("failed to login member C: %v", err)
	}

	// Owner creates an invite-only group.
	groupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "g04_invite_group",
		DisplayName: "G04 Invite Group",
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodInvite,
		Description: "G04 invite group",
	})
	if err != nil {
		t.Fatalf("failed to create invite group: %v", err)
	}

	// Member B applies and gets approved -> Active.
	if err := memberBClient.ApplyToP2PGroup(groupID, ""); err != nil {
		t.Fatalf("member B apply to group failed: %v", err)
	}
	if err := ownerClient.UpdateMemberStatus(groupID, memberBID, model.MemberStatusActive); err != nil {
		t.Fatalf("failed to approve member B: %v", err)
	}

	// Verify B is active and sees the group.
	memberInfoB, err := admin.GetGroupMemberInfo(groupID, memberBID)
	if err != nil {
		t.Fatalf("failed to get member B info: %v", err)
	}
	if memberInfoB.Status != model.MemberStatusActive {
		t.Fatalf("expected member B status Active(%d), got %d", model.MemberStatusActive, memberInfoB.Status)
	}

	joinedGroupsB, err := memberBClient.GetSelfJoinedGroups()
	if err != nil {
		t.Fatalf("failed to get joined groups for member B: %v", err)
	}
	foundB := false
	for _, g := range joinedGroupsB {
		if g.ID == groupID {
			foundB = true
			break
		}
	}
	if !foundB {
		t.Fatalf("expected group to appear in member B's joined groups before leaving")
	}

	// Member B voluntarily leaves the group.
	if err := memberBClient.LeaveGroup(groupID); err != nil {
		t.Fatalf("member B leave group failed: %v", err)
	}

	memberInfoB, err = admin.GetGroupMemberInfo(groupID, memberBID)
	if err != nil {
		t.Fatalf("failed to get member B info after leaving: %v", err)
	}
	if memberInfoB.Status != model.MemberStatusLeft {
		t.Fatalf("expected member B status Left(%d) after leaving, got %d", model.MemberStatusLeft, memberInfoB.Status)
	}

	joinedGroupsB, err = memberBClient.GetSelfJoinedGroups()
	if err != nil {
		t.Fatalf("failed to get joined groups for member B after leaving: %v", err)
	}
	for _, g := range joinedGroupsB {
		if g.ID == groupID {
			t.Fatalf("member B should not see group in joined groups after leaving")
		}
	}

	// Kick flow: member C joins and then gets banned.
	if err := memberCClient.ApplyToP2PGroup(groupID, ""); err != nil {
		t.Fatalf("member C apply to group failed: %v", err)
	}
	if err := ownerClient.UpdateMemberStatus(groupID, memberCID, model.MemberStatusActive); err != nil {
		t.Fatalf("failed to approve member C: %v", err)
	}

	memberInfoC, err := admin.GetGroupMemberInfo(groupID, memberCID)
	if err != nil {
		t.Fatalf("failed to get member C info after approval: %v", err)
	}
	if memberInfoC.Status != model.MemberStatusActive {
		t.Fatalf("expected member C status Active(%d) after approval, got %d", model.MemberStatusActive, memberInfoC.Status)
	}

	// Owner kicks member C (status=Banned).
	if err := ownerClient.UpdateMemberStatus(groupID, memberCID, model.MemberStatusBanned); err != nil {
		t.Fatalf("failed to ban member C: %v", err)
	}

	memberInfoC, err = admin.GetGroupMemberInfo(groupID, memberCID)
	if err != nil {
		t.Fatalf("failed to get member C info after ban: %v", err)
	}
	if memberInfoC.Status != model.MemberStatusBanned {
		t.Fatalf("expected member C status Banned(%d) after kick, got %d", model.MemberStatusBanned, memberInfoC.Status)
	}

	joinedGroupsC, err := memberCClient.GetSelfJoinedGroups()
	if err != nil {
		t.Fatalf("failed to get joined groups for member C after ban: %v", err)
	}
	for _, g := range joinedGroupsC {
		if g.ID == groupID {
			t.Fatalf("banned member C should not see group in joined groups")
		}
	}
}

// TestGroup_G05_DeleteGroup tests group deletion with cascade.
func TestGroup_G05_DeleteGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create owner and two members.
	ownerUsername := "g05_owner"
	ownerPassword := "password123"
	memberBUsername := "g05_memberB"
	memberBPassword := "password123"
	memberCUsername := "g05_memberC"
	memberCPassword := "password123"

	createTestUser(t, admin, ownerUsername, ownerPassword, "default")

	memberB := createTestUser(t, admin, memberBUsername, memberBPassword, "default")
	memberBID := memberB.ID

	memberC := createTestUser(t, admin, memberCUsername, memberCPassword, "default")
	memberCID := memberC.ID

	ownerClient := admin.Clone()
	if _, err := ownerClient.Login(ownerUsername, ownerPassword); err != nil {
		t.Fatalf("failed to login owner user: %v", err)
	}

	memberBClient := admin.Clone()
	if _, err := memberBClient.Login(memberBUsername, memberBPassword); err != nil {
		t.Fatalf("failed to login member B: %v", err)
	}

	memberCClient := admin.Clone()
	if _, err := memberCClient.Login(memberCUsername, memberCPassword); err != nil {
		t.Fatalf("failed to login member C: %v", err)
	}

	// Owner creates a shared group and both members join and become active.
	groupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "g05_shared_group",
		DisplayName: "G05 Shared Group",
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodApproval,
		Description: "G05 deletion group",
	})
	if err != nil {
		t.Fatalf("failed to create shared group: %v", err)
	}

	if err := memberBClient.ApplyToP2PGroup(groupID, ""); err != nil {
		t.Fatalf("member B apply to group failed: %v", err)
	}
	if err := memberCClient.ApplyToP2PGroup(groupID, ""); err != nil {
		t.Fatalf("member C apply to group failed: %v", err)
	}

	if err := ownerClient.UpdateMemberStatus(groupID, memberBID, model.MemberStatusActive); err != nil {
		t.Fatalf("failed to approve member B: %v", err)
	}
	if err := ownerClient.UpdateMemberStatus(groupID, memberCID, model.MemberStatusActive); err != nil {
		t.Fatalf("failed to approve member C: %v", err)
	}

	// Sanity check: group has active members.
	members, err := admin.GetGroupMembers(groupID, model.MemberStatusActive)
	if err != nil {
		t.Fatalf("failed to get active members before deletion: %v", err)
	}
	if len(members) == 0 {
		t.Fatalf("expected at least one active member before deletion")
	}

	// Owner deletes the group (should cascade delete user_groups relations).
	if err := ownerClient.DeleteGroup(groupID); err != nil {
		t.Fatalf("failed to delete group: %v", err)
	}

	// Owner should no longer see the group in self-owned list.
	ownedGroups, err := ownerClient.GetSelfOwnedGroups()
	if err != nil {
		t.Fatalf("failed to get owned groups after deletion: %v", err)
	}
	for _, g := range ownedGroups {
		if g.ID == groupID {
			t.Fatalf("deleted group should not appear in owner's owned groups")
		}
	}

	// Members should no longer see the group in joined list.
	joinedB, err := memberBClient.GetSelfJoinedGroups()
	if err != nil {
		t.Fatalf("failed to get member B joined groups after deletion: %v", err)
	}
	for _, g := range joinedB {
		if g.ID == groupID {
			t.Fatalf("deleted group should not appear in member B's joined groups")
		}
	}

	joinedC, err := memberCClient.GetSelfJoinedGroups()
	if err != nil {
		t.Fatalf("failed to get member C joined groups after deletion: %v", err)
	}
	for _, g := range joinedC {
		if g.ID == groupID {
			t.Fatalf("deleted group should not appear in member C's joined groups")
		}
	}

	// Group members API should return no records for this group.
	members, err = admin.GetGroupMembers(groupID, -1)
	if err != nil {
		t.Fatalf("failed to get group members after deletion: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no members after group deletion, got %d", len(members))
	}
}

// TestGroup_PublicDiscovery tests public group discovery API.
func TestGroup_PublicDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create an owner user.
	ownerUsername := "public_owner"
	ownerPassword := "password123"
	owner := createTestUser(t, admin, ownerUsername, ownerPassword, "default")

	ownerClient := admin.Clone()
	if _, err := ownerClient.Login(ownerUsername, ownerPassword); err != nil {
		t.Fatalf("failed to login owner user: %v", err)
	}

	// Create shared (public) groups that should appear in marketplace.
	sharedGroupID1, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "public-discovery-shared-1",
		DisplayName: "PublicDiscovery Shared 1",
		OwnerId:     owner.ID,
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodApproval,
		Description: "shared group 1 for public discovery",
	})
	if err != nil {
		t.Fatalf("failed to create shared group 1: %v", err)
	}

	sharedGroupID2, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "public-discovery-shared-2-keyword",
		DisplayName: "PublicDiscovery Shared 2",
		OwnerId:     owner.ID,
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodPassword,
		JoinKey:     "pass123",
		Description: "shared group 2 for public discovery with keyword",
	})
	if err != nil {
		t.Fatalf("failed to create shared group 2: %v", err)
	}

	// Create a private group that should never appear in public list.
	privateGroupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "public-discovery-private",
		DisplayName: "PublicDiscovery Private",
		OwnerId:     owner.ID,
		Type:        model.GroupTypePrivate,
		JoinMethod:  model.JoinMethodInvite,
		Description: "private group for public discovery negative case",
	})
	if err != nil {
		t.Fatalf("failed to create private group: %v", err)
	}

	// Helper response type for /api/groups/public
	type publicGroupsPage struct {
		Items []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			Type        int    `json:"type"`
			MemberCount int64  `json:"member_count"`
		} `json:"items"`
		Total int `json:"total"`
	}
	var resp struct {
		Success bool             `json:"success"`
		Message string           `json:"message"`
		Data    publicGroupsPage `json:"data"`
	}

	// 1) Query public groups without keyword filter: only shared groups should appear.
	if err := admin.GetJSON("/api/groups/public?p=1&page_size=100", &resp); err != nil {
		t.Fatalf("failed to query public groups: %v", err)
	}
	if !resp.Success {
		t.Fatalf("public groups query failed: %s", resp.Message)
	}

	var (
		foundShared1 bool
		foundShared2 bool
		foundPrivate bool
	)
	for _, g := range resp.Data.Items {
		if g.Type != model.GroupTypeShared {
			t.Fatalf("expected only shared groups (type=%d), got type=%d for group id=%d",
				model.GroupTypeShared, g.Type, g.ID)
		}
		switch g.ID {
		case sharedGroupID1:
			foundShared1 = true
		case sharedGroupID2:
			foundShared2 = true
		case privateGroupID:
			foundPrivate = true
		}
		// member_count should be non-negative for all groups
		if g.MemberCount < 0 {
			t.Fatalf("member_count should be non-negative, got %d for group id=%d", g.MemberCount, g.ID)
		}
	}

	if !foundShared1 || !foundShared2 {
		t.Fatalf("expected both shared groups to appear in public list, foundShared1=%v, foundShared2=%v", foundShared1, foundShared2)
	}
	if foundPrivate {
		t.Fatalf("private group should not appear in public group list")
	}

	// 2) Query public groups with keyword filter: only matching groups should be returned.
	var keywordResp struct {
		Success bool             `json:"success"`
		Message string           `json:"message"`
		Data    publicGroupsPage `json:"data"`
	}
	if err := admin.GetJSON("/api/groups/public?p=1&page_size=100&keyword=shared-2-keyword", &keywordResp); err != nil {
		t.Fatalf("failed to query public groups with keyword: %v", err)
	}
	if !keywordResp.Success {
		t.Fatalf("public groups keyword query failed: %s", keywordResp.Message)
	}

	if len(keywordResp.Data.Items) == 0 {
		t.Fatalf("expected at least one group for keyword search")
	}

	for _, g := range keywordResp.Data.Items {
		if g.ID != sharedGroupID2 {
			t.Fatalf("expected keyword search to return only sharedGroup2 (id=%d), got group id=%d", sharedGroupID2, g.ID)
		}
	}
}

// TestGroup_OwnerPermissions tests that only owners can manage groups.
func TestGroup_OwnerPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create owner and member users.
	ownerUsername := "perm_owner"
	ownerPassword := "password123"
	memberBUsername := "perm_member_b"
	memberBPassword := "password123"
	memberCUsername := "perm_member_c"
	memberCPassword := "password123"

	owner := createTestUser(t, admin, ownerUsername, ownerPassword, "default")
	memberB := createTestUser(t, admin, memberBUsername, memberBPassword, "default")
	memberC := createTestUser(t, admin, memberCUsername, memberCPassword, "default")

	ownerClient := admin.Clone()
	if _, err := ownerClient.Login(ownerUsername, ownerPassword); err != nil {
		t.Fatalf("failed to login owner user: %v", err)
	}

	memberBClient := admin.Clone()
	if _, err := memberBClient.Login(memberBUsername, memberBPassword); err != nil {
		t.Fatalf("failed to login member B user: %v", err)
	}

	memberCClient := admin.Clone()
	if _, err := memberCClient.Login(memberCUsername, memberCPassword); err != nil {
		t.Fatalf("failed to login member C user: %v", err)
	}

	// Owner creates a shared group with password join.
	const joinPassword = "perm-pass-123"
	groupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "owner-permissions-group",
		DisplayName: "Owner Permissions Group",
		OwnerId:     owner.ID,
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodPassword,
		JoinKey:     joinPassword,
		Description: "group for owner permission boundary test",
	})
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	// Member B and C join as Active members.
	if err := memberBClient.ApplyToP2PGroup(groupID, joinPassword); err != nil {
		t.Fatalf("member B failed to join group: %v", err)
	}
	if err := memberCClient.ApplyToP2PGroup(groupID, joinPassword); err != nil {
		t.Fatalf("member C failed to join group: %v", err)
	}

	// Sanity check: both members are active.
	memberInfoB, err := admin.GetGroupMemberInfo(groupID, memberB.ID)
	if err != nil {
		t.Fatalf("failed to get member B info: %v", err)
	}
	if memberInfoB.Status != model.MemberStatusActive {
		t.Fatalf("expected member B to be Active(%d), got %d", model.MemberStatusActive, memberInfoB.Status)
	}

	memberInfoC, err := admin.GetGroupMemberInfo(groupID, memberC.ID)
	if err != nil {
		t.Fatalf("failed to get member C info: %v", err)
	}
	if memberInfoC.Status != model.MemberStatusActive {
		t.Fatalf("expected member C to be Active(%d), got %d", model.MemberStatusActive, memberInfoC.Status)
	}

	// --- Non-owner (member B) permission checks (should be denied) ---

	// 1) Non-owner attempts to update group config.
	if err := ownerClient.UpdateMemberStatus(groupID, memberB.ID, model.MemberStatusActive); err != nil {
		t.Fatalf("unexpected failure when owner approving member B: %v", err)
	}

	newName := "owner-permissions-group-hacked"
	err = memberBClient.PutJSON("/api/groups", map[string]interface{}{
		"id":   groupID,
		"name": newName,
	}, &testutil.APIResponse{})
	if err == nil {
		t.Fatalf("expected non-owner config update to be forbidden, but got no error")
	}
	if !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected 403 error for non-owner config update, got: %v", err)
	}

	// 2) Non-owner attempts to manage members (kick member C).
	err = memberBClient.UpdateMemberStatus(groupID, memberC.ID, model.MemberStatusBanned)
	if err == nil {
		t.Fatalf("expected non-owner member management to be forbidden, but got no error")
	}
	if !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected 403 error for non-owner member management, got: %v", err)
	}

	// Verify member C is still active.
	memberInfoC, err = admin.GetGroupMemberInfo(groupID, memberC.ID)
	if err != nil {
		t.Fatalf("failed to get member C info after non-owner kick attempt: %v", err)
	}
	if memberInfoC.Status != model.MemberStatusActive {
		t.Fatalf("expected member C to remain Active(%d) after non-owner kick attempt, got %d",
			model.MemberStatusActive, memberInfoC.Status)
	}

	// 3) Non-owner attempts to delete the group.
	err = memberBClient.DeleteGroup(groupID)
	if err == nil {
		t.Fatalf("expected non-owner delete group to be forbidden, but got no error")
	}
	if !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected 403 error for non-owner delete group, got: %v", err)
	}

	// Verify group still exists by listing owner's self-owned groups.
	ownedGroups, err := ownerClient.GetSelfOwnedGroups()
	if err != nil {
		t.Fatalf("failed to get self owned groups: %v", err)
	}
	foundGroup := false
	for _, g := range ownedGroups {
		if g.ID == groupID {
			foundGroup = true
			break
		}
	}
	if !foundGroup {
		t.Fatalf("group should still exist after non-owner delete attempt")
	}

	// --- Owner permissions (should succeed) ---

	// 1) Owner updates group config successfully.
	updatedName := "owner-permissions-group-updated"
	if err := ownerClient.PutJSON("/api/groups", map[string]interface{}{
		"id":   groupID,
		"name": updatedName,
	}, &testutil.APIResponse{}); err != nil {
		t.Fatalf("owner failed to update group config: %v", err)
	}

	ownedGroups, err = ownerClient.GetSelfOwnedGroups()
	if err != nil {
		t.Fatalf("failed to get owned groups after owner update: %v", err)
	}
	nameMatched := false
	for _, g := range ownedGroups {
		if g.ID == groupID {
			if g.Name != updatedName {
				t.Fatalf("expected group name to be %q, got %q", updatedName, g.Name)
			}
			nameMatched = true
			break
		}
	}
	if !nameMatched {
		t.Fatalf("updated group not found in owner's owned groups")
	}

	// 2) Owner bans member C.
	if err := ownerClient.UpdateMemberStatus(groupID, memberC.ID, model.MemberStatusBanned); err != nil {
		t.Fatalf("owner failed to ban member C: %v", err)
	}

	memberInfoC, err = admin.GetGroupMemberInfo(groupID, memberC.ID)
	if err != nil {
		t.Fatalf("failed to get member C info after owner ban: %v", err)
	}
	if memberInfoC.Status != model.MemberStatusBanned {
		t.Fatalf("expected member C status Banned(%d) after owner ban, got %d", model.MemberStatusBanned, memberInfoC.Status)
	}

	// 3) Owner deletes the group successfully.
	if err := ownerClient.DeleteGroup(groupID); err != nil {
		t.Fatalf("owner failed to delete group: %v", err)
	}

	ownedGroups, err = ownerClient.GetSelfOwnedGroups()
	if err != nil {
		t.Fatalf("failed to get owned groups after owner deletion: %v", err)
	}
	for _, g := range ownedGroups {
		if g.ID == groupID {
			t.Fatalf("deleted group should not appear in owner's owned groups")
		}
	}
}

// TestGroup_MemberCount tests member count tracking.
func TestGroup_MemberCount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	admin := suite.Client

	// Create owner and three member users.
	ownerUsername := "mc_owner"
	ownerPassword := "password123"
	userBUsername := "mc_user_b"
	userBPassword := "password123"
	userCUsername := "mc_user_c"
	userCPassword := "password123"
	userDUsername := "mc_user_d"
	userDPassword := "password123"

	owner := createTestUser(t, admin, ownerUsername, ownerPassword, "default")
	userB := createTestUser(t, admin, userBUsername, userBPassword, "default")
	userC := createTestUser(t, admin, userCUsername, userCPassword, "default")
	userD := createTestUser(t, admin, userDUsername, userDPassword, "default")

	ownerClient := admin.Clone()
	if _, err := ownerClient.Login(ownerUsername, ownerPassword); err != nil {
		t.Fatalf("failed to login owner user: %v", err)
	}

	userBClient := admin.Clone()
	if _, err := userBClient.Login(userBUsername, userBPassword); err != nil {
		t.Fatalf("failed to login user B: %v", err)
	}

	userCClient := admin.Clone()
	if _, err := userCClient.Login(userCUsername, userCPassword); err != nil {
		t.Fatalf("failed to login user C: %v", err)
	}

	userDClient := admin.Clone()
	if _, err := userDClient.Login(userDUsername, userDPassword); err != nil {
		t.Fatalf("failed to login user D: %v", err)
	}

	// Owner creates a shared group.
	const joinPassword = "mc-pass-123"
	groupID, err := ownerClient.CreateP2PGroup(&testutil.P2PGroupModel{
		Name:        "member-count-group",
		DisplayName: "Member Count Group",
		OwnerId:     owner.ID,
		Type:        model.GroupTypeShared,
		JoinMethod:  model.JoinMethodPassword,
		JoinKey:     joinPassword,
		Description: "group for member count tests",
	})
	if err != nil {
		t.Fatalf("failed to create member count group: %v", err)
	}

	// User B and C join with correct password -> should become Active.
	if err := userBClient.ApplyToP2PGroup(groupID, joinPassword); err != nil {
		t.Fatalf("user B failed to join group: %v", err)
	}
	if err := userCClient.ApplyToP2PGroup(groupID, joinPassword); err != nil {
		t.Fatalf("user C failed to join group: %v", err)
	}

	// User D joins with wrong password -> should become Pending (not counted).
	if err := userDClient.ApplyToP2PGroup(groupID, "wrong-password"); err != nil {
		t.Fatalf("user D apply with wrong password should create Pending membership, got error: %v", err)
	}

	// Verify membership statuses.
	memberInfoB, err := admin.GetGroupMemberInfo(groupID, userB.ID)
	if err != nil {
		t.Fatalf("failed to get member info for user B: %v", err)
	}
	if memberInfoB.Status != model.MemberStatusActive {
		t.Fatalf("expected user B status Active(%d), got %d", model.MemberStatusActive, memberInfoB.Status)
	}

	memberInfoC, err := admin.GetGroupMemberInfo(groupID, userC.ID)
	if err != nil {
		t.Fatalf("failed to get member info for user C: %v", err)
	}
	if memberInfoC.Status != model.MemberStatusActive {
		t.Fatalf("expected user C status Active(%d), got %d", model.MemberStatusActive, memberInfoC.Status)
	}

	memberInfoD, err := admin.GetGroupMemberInfo(groupID, userD.ID)
	if err != nil {
		t.Fatalf("failed to get member info for user D: %v", err)
	}
	if memberInfoD.Status != model.MemberStatusPending {
		t.Fatalf("expected user D status Pending(%d), got %d", model.MemberStatusPending, memberInfoD.Status)
	}

	// DB truth: number of Active members in the group (should be 2: B and C).
	activeMembers, err := ownerClient.GetGroupMembers(groupID, model.MemberStatusActive)
	if err != nil {
		t.Fatalf("failed to get active members: %v", err)
	}
	if len(activeMembers) != 2 {
		t.Fatalf("expected 2 active members, got %d", len(activeMembers))
	}

	// Common response type for group list APIs that include member_count.
	type groupWithCount struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		MemberCount int64  `json:"member_count"`
	}
	type groupListPage struct {
		Items []groupWithCount `json:"items"`
		Total int              `json:"total"`
	}

	// 1) Verify member_count in /api/groups/public.
	var publicResp struct {
		Success bool          `json:"success"`
		Message string        `json:"message"`
		Data    groupListPage `json:"data"`
	}
	if err := admin.GetJSON("/api/groups/public?p=1&page_size=100", &publicResp); err != nil {
		t.Fatalf("failed to query public groups: %v", err)
	}
	if !publicResp.Success {
		t.Fatalf("public groups query failed: %s", publicResp.Message)
	}

	var foundInPublic bool
	for _, g := range publicResp.Data.Items {
		if g.ID == groupID {
			foundInPublic = true
			if g.MemberCount != 2 {
				t.Fatalf("expected member_count=2 in public list, got %d", g.MemberCount)
			}
			break
		}
	}
	if !foundInPublic {
		t.Fatalf("member count group not found in public groups list")
	}

	// 2) Verify member_count in /api/groups/self/owned.
	var ownedResp struct {
		Success bool          `json:"success"`
		Message string        `json:"message"`
		Data    groupListPage `json:"data"`
	}
	if err := ownerClient.GetJSON("/api/groups/self/owned?p=1&page_size=100", &ownedResp); err != nil {
		t.Fatalf("failed to query self owned groups: %v", err)
	}
	if !ownedResp.Success {
		t.Fatalf("self owned groups query failed: %s", ownedResp.Message)
	}

	var foundInOwned bool
	for _, g := range ownedResp.Data.Items {
		if g.ID == groupID {
			foundInOwned = true
			if g.MemberCount != 2 {
				t.Fatalf("expected member_count=2 in owned list, got %d", g.MemberCount)
			}
			break
		}
	}
	if !foundInOwned {
		t.Fatalf("member count group not found in self owned groups list")
	}

	// 3) Verify member_count in /api/groups/self/joined for a member (user B).
	var joinedResp struct {
		Success bool          `json:"success"`
		Message string        `json:"message"`
		Data    groupListPage `json:"data"`
	}
	if err := userBClient.GetJSON("/api/groups/self/joined?p=1&page_size=100", &joinedResp); err != nil {
		t.Fatalf("failed to query self joined groups: %v", err)
	}
	if !joinedResp.Success {
		t.Fatalf("self joined groups query failed: %s", joinedResp.Message)
	}

	var foundInJoined bool
	for _, g := range joinedResp.Data.Items {
		if g.ID == groupID {
			foundInJoined = true
			if g.MemberCount != 2 {
				t.Fatalf("expected member_count=2 in joined list, got %d", g.MemberCount)
			}
			break
		}
	}
	if !foundInJoined {
		t.Fatalf("member count group not found in self joined groups list")
	}

	// 4) Remove one active member and verify member_count is updated.
	if err := ownerClient.UpdateMemberStatus(groupID, userC.ID, model.MemberStatusBanned); err != nil {
		t.Fatalf("owner failed to ban user C: %v", err)
	}

	// Re-query owned list to verify member_count decremented to 1.
	if err := ownerClient.GetJSON("/api/groups/self/owned?p=1&page_size=100", &ownedResp); err != nil {
		t.Fatalf("failed to re-query self owned groups after ban: %v", err)
	}
	if !ownedResp.Success {
		t.Fatalf("self owned groups query failed after ban: %s", ownedResp.Message)
	}

	for _, g := range ownedResp.Data.Items {
		if g.ID == groupID {
			if g.MemberCount != 1 {
				t.Fatalf("expected member_count=1 in owned list after banning one member, got %d", g.MemberCount)
			}
			break
		}
	}
}

// TestGroupManagementSkeleton is a placeholder test to verify the test file compiles.
func TestGroupManagementSkeleton(t *testing.T) {
	t.Log("Group management test skeleton loaded successfully")
}
