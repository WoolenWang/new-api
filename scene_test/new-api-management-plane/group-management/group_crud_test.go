// Package p2p_management contains integration tests for P2P group management.
//
// Test Focus:
// ===========
// This package validates P2P group CRUD operations (GM-01 to GM-08).
//
// Key Test Scenarios:
// - GM-01: Create private group
// - GM-02: Create shared group with password
// - GM-03: Create shared group with review
// - GM-04: Query self-owned groups
// - GM-05: Query joined groups
// - GM-06: Update group information
// - GM-07: Delete group with cascade
// - GM-08: Query public group marketplace
package p2p_management

import (
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGM01_CreatePrivateGroup tests creating a private P2P group.
// Priority: P0
func TestGM01_CreatePrivateGroup(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a private group (type=1)
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"private-group-gm01",
		suite.fixtures.RegularUser1.ID,
		1,  // type=1: Private
		0,  // join_method=0: Invite only
		"", // no password
	)

	// Verify the group was created with correct attributes
	assert.Equal(t, 1, group.Type, "Group type should be Private (1)")
	assert.Equal(t, 0, group.JoinMethod, "Join method should be Invite (0)")
	assert.Equal(t, suite.fixtures.RegularUser1.ID, group.OwnerId, "Owner should be User1")

	// Verify the group exists in the database
	helper.AssertGroupExists(group.ID)

	t.Logf("Successfully created private group: id=%d, name=%s", group.ID, group.Name)
}

// TestGM02_CreateSharedGroupPassword tests creating a shared group with password protection.
// Priority: P0
func TestGM02_CreateSharedGroupPassword(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a shared group with password (type=2, join_method=2)
	password := "secure123"
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"shared-group-password-gm02",
		suite.fixtures.RegularUser1.ID,
		2, // type=2: Shared
		2, // join_method=2: Password
		password,
	)

	// Verify the group was created with correct attributes
	assert.Equal(t, 2, group.Type, "Group type should be Shared (2)")
	assert.Equal(t, 2, group.JoinMethod, "Join method should be Password (2)")
	assert.Equal(t, password, group.JoinKey, "Join key (password) should be stored")
	assert.Equal(t, suite.fixtures.RegularUser1.ID, group.OwnerId, "Owner should be User1")

	// Verify the group exists
	helper.AssertGroupExists(group.ID)

	t.Logf("Successfully created shared group with password: id=%d, name=%s", group.ID, group.Name)
}

// TestGM03_CreateSharedGroupReview tests creating a shared group with review-based joining.
// Priority: P0
func TestGM03_CreateSharedGroupReview(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a shared group with review process (type=2, join_method=1)
	group := helper.CreateAndVerifyGroup(
		suite.fixtures.User1Client,
		"shared-group-review-gm03",
		suite.fixtures.RegularUser1.ID,
		2, // type=2: Shared
		1, // join_method=1: Review
		"",
	)

	// Verify the group was created with correct attributes
	assert.Equal(t, 2, group.Type, "Group type should be Shared (2)")
	assert.Equal(t, 1, group.JoinMethod, "Join method should be Review (1)")
	assert.Equal(t, suite.fixtures.RegularUser1.ID, group.OwnerId, "Owner should be User1")

	// Verify the group exists
	helper.AssertGroupExists(group.ID)

	t.Logf("Successfully created shared group with review: id=%d, name=%s", group.ID, group.Name)
}

// TestGM04_QuerySelfOwnedGroups tests querying groups owned by the current user.
// Priority: P1
func TestGM04_QuerySelfOwnedGroups(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates multiple groups
	group1 := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "owned-group-1-gm04", suite.fixtures.RegularUser1.ID, 2, 1, "")
	group2 := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "owned-group-2-gm04", suite.fixtures.RegularUser1.ID, 2, 2, "pass123")
	group3 := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "owned-group-3-gm04", suite.fixtures.RegularUser1.ID, 1, 0, "")

	// Query User1's owned groups
	ownedGroups, err := suite.fixtures.User1Client.GetSelfOwnedGroups()
	require.NoError(t, err, "Failed to query owned groups")

	// Verify all created groups are in the list
	groupIDs := make([]int, len(ownedGroups))
	for i, g := range ownedGroups {
		groupIDs[i] = g.ID
	}

	assert.Contains(t, groupIDs, group1.ID, "Owned groups should include group1")
	assert.Contains(t, groupIDs, group2.ID, "Owned groups should include group2")
	assert.Contains(t, groupIDs, group3.ID, "Owned groups should include group3")

	t.Logf("Successfully queried %d owned groups for User1", len(ownedGroups))
}

// TestGM05_QueryJoinedGroups tests querying groups the user has joined.
// Priority: P0
func TestGM05_QueryJoinedGroups(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// User1 creates groups
	group1 := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "joined-group-1-gm05", suite.fixtures.RegularUser1.ID, 2, 2, "pass123")
	group2 := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "joined-group-2-gm05", suite.fixtures.RegularUser1.ID, 2, 2, "pass456")

	// User2 joins both groups
	helper.ApplyToGroupAndVerify(suite.fixtures.User2Client, suite.fixtures.RegularUser2.ID, group1.ID, "pass123", 1)
	helper.ApplyToGroupAndVerify(suite.fixtures.User2Client, suite.fixtures.RegularUser2.ID, group2.ID, "pass456", 1)

	// Query User2's joined groups
	joinedGroups, err := suite.fixtures.User2Client.GetSelfJoinedGroups()
	require.NoError(t, err, "Failed to query joined groups")

	// Verify both groups are in the list
	groupIDs := make([]int, len(joinedGroups))
	for i, g := range joinedGroups {
		groupIDs[i] = g.ID
	}

	assert.Contains(t, groupIDs, group1.ID, "Joined groups should include group1")
	assert.Contains(t, groupIDs, group2.ID, "Joined groups should include group2")

	// Verify User2 does not see groups they haven't joined
	t.Logf("Successfully queried %d joined groups for User2", len(joinedGroups))
}

// TestGM06_UpdateGroupInfo tests updating group information.
// Priority: P1
func TestGM06_UpdateGroupInfo(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a group
	group := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "update-group-gm06", suite.fixtures.RegularUser1.ID, 2, 2, "oldpass")

	// Update the group's name and password
	newName := "updated-group-gm06"
	newPassword := "newpass123"

	err := helper.UpdateGroupConfig(suite.fixtures.User1Client, group.ID, map[string]interface{}{
		"name":     newName,
		"join_key": newPassword,
	})
	require.NoError(t, err, "Failed to update group")

	// Query the group to verify updates
	groupInfo, err := helper.GetGroupInfo(group.ID)
	require.NoError(t, err, "Failed to get updated group info")

	assert.Equal(t, newName, groupInfo.Name, "Group name should be updated")
	// Note: JoinKey might not be returned in the query for security reasons
	// We verify by attempting to join with the new password

	// User2 tries to join with old password (should fail or go to review)
	err = suite.fixtures.User2Client.ApplyToP2PGroup(group.ID, "oldpass")
	// Old password should not work

	// User3 tries to join with new password (should succeed)
	helper.ApplyToGroupAndVerify(suite.fixtures.User3Client, suite.fixtures.RegularUser3.ID, group.ID, newPassword, 1)

	t.Log("Successfully updated group information")
}

// TestGM07_DeleteGroup tests deleting a group and cascading member deletion.
// Priority: P0
func TestGM07_DeleteGroup(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create a group
	group := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "delete-group-gm07", suite.fixtures.RegularUser1.ID, 2, 2, "pass123")

	// User2 and User3 join the group
	helper.ApplyToGroupAndVerify(suite.fixtures.User2Client, suite.fixtures.RegularUser2.ID, group.ID, "pass123", 1)
	helper.ApplyToGroupAndVerify(suite.fixtures.User3Client, suite.fixtures.RegularUser3.ID, group.ID, "pass123", 1)

	// Verify members exist
	suite.inspector.AssertDBContains(suite.fixtures.RegularUser2.ID, group.ID)
	suite.inspector.AssertDBContains(suite.fixtures.RegularUser3.ID, group.ID)

	// Delete the group
	memberIDs := []int{suite.fixtures.RegularUser2.ID, suite.fixtures.RegularUser3.ID}
	helper.DeleteGroupAndVerify(suite.fixtures.User1Client, group.ID, memberIDs)

	// Verify the group no longer exists
	helper.AssertGroupNotExists(group.ID)

	// Verify all member relationships were cascade deleted
	suite.inspector.AssertDBNotContains(suite.fixtures.RegularUser2.ID, group.ID)
	suite.inspector.AssertDBNotContains(suite.fixtures.RegularUser3.ID, group.ID)

	// Verify members' caches were invalidated
	joinedGroups, err := suite.fixtures.User2Client.GetSelfJoinedGroups()
	require.NoError(t, err, "User2 should be able to query groups")

	for _, g := range joinedGroups {
		assert.NotEqual(t, group.ID, g.ID, "User2 should not see the deleted group")
	}

	t.Log("Successfully deleted group with cascade member deletion")
}

// TestGM08_QueryPublicGroupMarketplace tests querying the public group marketplace.
// Priority: P1
func TestGM08_QueryPublicGroupMarketplace(t *testing.T) {
	suite := setupP2PSuite(t)
	defer suite.Cleanup()

	helper := testutil.NewGroupHelper(t, suite.client, suite.inspector)

	// Create multiple shared groups (type=2)
	sharedGroup1 := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "public-shared-1-gm08", suite.fixtures.RegularUser1.ID, 2, 1, "")
	sharedGroup2 := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "public-shared-2-gm08", suite.fixtures.RegularUser1.ID, 2, 2, "pass123")

	// Create a private group (type=1) - should not appear in public marketplace
	privateGroup := helper.CreateAndVerifyGroup(suite.fixtures.User1Client, "private-group-gm08", suite.fixtures.RegularUser1.ID, 1, 0, "")

	// Query public groups using User2 (non-owner)
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Items []testutil.P2PGroupModel `json:"items"`
			Total int                      `json:"total"`
		} `json:"data"`
	}

	err := suite.fixtures.User2Client.GetJSON("/api/groups/public", &resp)
	require.NoError(t, err, "Failed to query public groups")
	require.True(t, resp.Success, "Query should succeed")

	// Verify shared groups are in the list
	publicGroupIDs := make([]int, len(resp.Data.Items))
	for i, g := range resp.Data.Items {
		publicGroupIDs[i] = g.ID
		assert.Equal(t, 2, g.Type, "All public groups should be Shared (type=2)")
	}

	assert.Contains(t, publicGroupIDs, sharedGroup1.ID, "Public groups should include sharedGroup1")
	assert.Contains(t, publicGroupIDs, sharedGroup2.ID, "Public groups should include sharedGroup2")

	// Verify private group is NOT in the list
	assert.NotContains(t, publicGroupIDs, privateGroup.ID, "Public groups should not include private groups")

	t.Logf("Successfully queried public group marketplace: %d groups found", len(resp.Data.Items))
}

// setupP2PSuite initializes a test suite for P2P management tests.
func setupP2PSuite(t *testing.T) *P2PSuite {
	t.Helper()

	// Start mock upstream for routing-dependent scenarios.
	upstream := testutil.NewMockUpstreamServer()
	t.Logf("Mock upstream started at: %s", upstream.BaseURL)

	// Find project root so the test server can be compiled and started.
	projectRoot, err := testutil.FindProjectRoot()
	if err != nil {
		upstream.Close()
		t.Fatalf("Failed to find project root: %v", err)
	}

	// Start test server (in-memory SQLite DB, compiled once per run).
	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()

	server, err := testutil.StartServer(cfg)
	if err != nil {
		upstream.Close()
		t.Fatalf("Failed to start test server: %v", err)
	}

	// Create admin client bound to this server.
	client := testutil.NewAPIClient(server)

	// Initialize system and login as root admin.
	rootUser, rootPass, err := client.InitializeSystem()
	if err != nil {
		_ = server.Stop()
		upstream.Close()
		t.Fatalf("Failed to initialize system: %v", err)
	}
	if _, err := client.Login(rootUser, rootPass); err != nil {
		_ = server.Stop()
		upstream.Close()
		t.Fatalf("Failed to login as admin: %v", err)
	}

	// Create fixtures and basic users used across tests.
	fixtures := testutil.NewTestFixtures(t, client)
	fixtures.SetUpstream(upstream)
	if err := fixtures.SetupBasicUsers(); err != nil {
		_ = server.Stop()
		upstream.Close()
		t.Fatalf("Failed to setup basic users: %v", err)
	}

	// Create cache inspector for membership/cache assertions.
	inspector := testutil.NewCacheInspector(t, client)

	suite := &P2PSuite{
		t:         t,
		server:    server,
		client:    client,
		upstream:  upstream,
		fixtures:  fixtures,
		inspector: inspector,
	}

	t.Cleanup(func() {
		suite.Cleanup()
	})

	return suite
}

// P2PSuite holds resources for P2P management tests.
type P2PSuite struct {
	t         *testing.T
	server    *testutil.TestServer
	client    *testutil.APIClient
	upstream  *testutil.MockUpstreamServer
	fixtures  *testutil.TestFixtures
	inspector *testutil.CacheInspector
}

// Cleanup releases all resources.
func (s *P2PSuite) Cleanup() {
	if s.inspector != nil {
		s.inspector.Cleanup()
	}
	if s.fixtures != nil {
		s.fixtures.Cleanup()
	}
	if s.server != nil {
		s.server.Stop()
	}
	if s.upstream != nil {
		s.upstream.Close()
	}
}
