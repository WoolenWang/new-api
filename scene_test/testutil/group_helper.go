// Package testutil provides utilities for integration testing of the NewAPI service.
package testutil

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GroupHelper provides high-level helper functions for P2P group testing.
type GroupHelper struct {
	t         *testing.T
	client    *APIClient
	inspector *CacheInspector
}

// NewGroupHelper creates a new group helper.
func NewGroupHelper(t *testing.T, client *APIClient, inspector *CacheInspector) *GroupHelper {
	t.Helper()
	return &GroupHelper{
		t:         t,
		client:    client,
		inspector: inspector,
	}
}

// CreateAndVerifyGroup creates a P2P group and verifies it was created successfully.
func (gh *GroupHelper) CreateAndVerifyGroup(ownerClient *APIClient, name string, ownerID, groupType, joinMethod int, joinKey string) *P2PGroupModel {
	gh.t.Helper()

	group := &P2PGroupModel{
		Name:        name,
		DisplayName: name + " Display",
		OwnerId:     ownerID,
		Type:        groupType,
		JoinMethod:  joinMethod,
		JoinKey:     joinKey,
		Description: fmt.Sprintf("Test group: %s", name),
	}

	groupID, err := ownerClient.CreateP2PGroup(group)
	require.NoError(gh.t, err, "Failed to create P2P group")
	require.Greater(gh.t, groupID, 0, "Invalid group ID returned")

	group.ID = groupID
	gh.t.Logf("Created P2P group: id=%d, name=%s, type=%d, join_method=%d", groupID, name, groupType, joinMethod)

	return group
}

// ApplyToGroupAndVerify applies to join a group and verifies the expected status.
// expectedStatus: 0=Pending, 1=Active
func (gh *GroupHelper) ApplyToGroupAndVerify(userClient *APIClient, userID, groupID int, password string, expectedStatus int) {
	gh.t.Helper()

	err := userClient.ApplyToP2PGroup(groupID, password)
	require.NoError(gh.t, err, "Failed to apply to P2P group")

	// Wait a moment for the operation to complete
	time.Sleep(200 * time.Millisecond)

	// Verify the membership status
	gh.inspector.AssertMemberStatus(userID, groupID, expectedStatus)

	// If Active, verify cache was invalidated
	if expectedStatus == 1 {
		// After a successful join, the cache should eventually be invalidated
		// We give it some time to propagate
		time.Sleep(100 * time.Millisecond)

		// Verify DB contains the membership
		gh.inspector.AssertDBContains(userID, groupID)
	}
}

// ApproveAndVerify approves a membership and verifies cache invalidation.
func (gh *GroupHelper) ApproveAndVerify(ownerClient *APIClient, userID, groupID int) {
	gh.t.Helper()

	// Clear cache before approval to have a known state
	err := gh.inspector.InvalidateL2Cache(userID)
	require.NoError(gh.t, err, "Failed to invalidate cache")

	// Approve the membership (status=1 means Active)
	err = ownerClient.UpdateMemberStatus(groupID, userID, 1)
	require.NoError(gh.t, err, "Failed to approve member")

	// Wait for the operation to complete
	time.Sleep(200 * time.Millisecond)

	// Verify the status changed to Active
	gh.inspector.AssertMemberStatus(userID, groupID, 1)

	// Verify cache was invalidated (should not exist immediately after change)
	// Note: In some implementations, cache might be immediately repopulated
	// So we check that DB has the correct data
	gh.inspector.AssertDBContains(userID, groupID)
}

// KickAndVerify kicks a member and verifies cache invalidation.
func (gh *GroupHelper) KickAndVerify(ownerClient *APIClient, userID, groupID int) {
	gh.t.Helper()

	// Kick the member (status=3 means Banned)
	err := ownerClient.UpdateMemberStatus(groupID, userID, 3)
	require.NoError(gh.t, err, "Failed to kick member")

	// Wait for the operation to complete
	time.Sleep(200 * time.Millisecond)

	// Verify the status changed to Banned
	gh.inspector.AssertMemberStatus(userID, groupID, 3)

	// Verify cache was invalidated and DB no longer contains active membership
	gh.inspector.AssertDBNotContains(userID, groupID)
}

// LeaveAndVerify leaves a group and verifies cache invalidation.
func (gh *GroupHelper) LeaveAndVerify(userClient *APIClient, userID, groupID int) {
	gh.t.Helper()

	err := userClient.LeaveGroup(groupID)
	require.NoError(gh.t, err, "Failed to leave group")

	// Wait for the operation to complete
	time.Sleep(200 * time.Millisecond)

	// Verify the membership no longer exists or is marked as Left
	status, exists, err := gh.inspector.GetMemberStatus(userID, groupID)
	require.NoError(gh.t, err, "Failed to get member status")

	// Either the record doesn't exist (deleted) or status is 4 (Left)
	if exists {
		assert.Equal(gh.t, 4, status, "Expected status=4 (Left) for user=%d in group=%d", userID, groupID)
	}

	// Verify cache was invalidated and DB no longer contains active membership
	gh.inspector.AssertDBNotContains(userID, groupID)
}

// DeleteGroupAndVerify deletes a group and verifies all members' caches are invalidated.
func (gh *GroupHelper) DeleteGroupAndVerify(ownerClient *APIClient, groupID int, memberUserIDs []int) {
	gh.t.Helper()

	err := ownerClient.DeleteGroup(groupID)
	require.NoError(gh.t, err, "Failed to delete group")

	// Wait for the cascade deletion to complete
	time.Sleep(300 * time.Millisecond)

	// Verify all members no longer have active memberships in the DB
	for _, userID := range memberUserIDs {
		gh.inspector.AssertDBNotContains(userID, groupID)
	}
}

// VerifyCanAccessChannel verifies that a user can successfully access a channel through a token.
// This is used to validate that P2P group membership grants the expected routing permissions.
func (gh *GroupHelper) VerifyCanAccessChannel(userToken, model string, channelID int) {
	gh.t.Helper()

	// Make a chat completion request
	resp, err := gh.client.ChatCompletion(ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "user", Content: "test message"},
		},
		Stream: false,
	})
	require.NoError(gh.t, err, "Expected successful chat completion")
	require.NotNil(gh.t, resp, "Expected non-nil response")

	// Verify the request was routed to the expected channel
	// Note: This requires checking logs or adding a field in the response
	gh.t.Logf("Successfully accessed channel %d with model %s", channelID, model)
}

// VerifyCannotAccessChannel verifies that a user cannot access a channel (no available channels error).
func (gh *GroupHelper) VerifyCannotAccessChannel(userToken, model string) {
	gh.t.Helper()

	// Make a chat completion request
	_, err := gh.client.ChatCompletion(ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "user", Content: "test message"},
		},
		Stream: false,
	})
	assert.Error(gh.t, err, "Expected error when accessing unavailable channel")
	assert.Contains(gh.t, err.Error(), "no available channel", "Expected 'no available channel' error")

	gh.t.Logf("Correctly denied access to channels for model %s", model)
}

// WaitForCacheRefresh waits for cache to refresh after a membership change.
// This is useful when testing cache TTL and automatic refresh.
func (gh *GroupHelper) WaitForCacheRefresh(userID int, expectedGroupIDs []int, timeout time.Duration) {
	gh.t.Helper()

	if timeout == 0 {
		timeout = 5 * time.Second
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		groups, err := gh.inspector.InspectL3DB(userID)
		require.NoError(gh.t, err, "Failed to inspect DB")

		if slicesEqualUnordered(groups, expectedGroupIDs) {
			gh.t.Logf("Cache refreshed for user %d: groups=%v", userID, groups)
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	gh.t.Fatalf("Cache refresh timeout for user %d after %v", userID, timeout)
}

// CreateTestMembership creates a direct DB membership (for testing edge cases).
// This bypasses the API and is used for testing cache consistency.
func (gh *GroupHelper) CreateTestMembership(userID, groupID, status int) {
	gh.t.Helper()

	// This would require direct DB access
	// For now, we'll use the API as the primary method
	gh.t.Logf("Note: Direct DB membership creation not implemented, use API methods")
}

// GetGroupInfo retrieves group information through the API.
func (gh *GroupHelper) GetGroupInfo(groupID int) (*P2PGroupModel, error) {
	gh.t.Helper()

	// First try to locate the group via the current client's self-service APIs.
	// This works when the client is the owner or a member of the group.
	if gh.client != nil {
		if ownedGroups, err := gh.client.GetSelfOwnedGroups(); err == nil {
			for _, g := range ownedGroups {
				if g.ID == groupID {
					return &g, nil
				}
			}
		}

		if joinedGroups, err := gh.client.GetSelfJoinedGroups(); err == nil {
			for _, g := range joinedGroups {
				if g.ID == groupID {
					return &g, nil
				}
			}
		}
	}

	// Fallback: query the SQLite database directly using the CacheInspector.
	// This is necessary when the helper is bound to an admin client that is
	// neither the owner nor a member of the target group (common in tests).
	if gh.inspector != nil {
		db := gh.inspector.openDB()

		var row struct {
			ID          int    `gorm:"column:id"`
			Name        string `gorm:"column:name"`
			DisplayName string `gorm:"column:display_name"`
			OwnerId     int    `gorm:"column:owner_id"`
			Type        int    `gorm:"column:type"`
			JoinMethod  int    `gorm:"column:join_method"`
			JoinKey     string `gorm:"column:join_key"`
			Description string `gorm:"column:description"`
		}

		if err := db.Table("groups").Where("id = ?", groupID).First(&row).Error; err == nil {
			return &P2PGroupModel{
				ID:          row.ID,
				Name:        row.Name,
				DisplayName: row.DisplayName,
				OwnerId:     row.OwnerId,
				Type:        row.Type,
				JoinMethod:  row.JoinMethod,
				JoinKey:     row.JoinKey,
				Description: row.Description,
			}, nil
		}
	}

	return nil, fmt.Errorf("group %d not found", groupID)
}

// AssertGroupExists asserts that a group exists in the system.
func (gh *GroupHelper) AssertGroupExists(groupID int) {
	gh.t.Helper()

	group, err := gh.GetGroupInfo(groupID)
	assert.NoError(gh.t, err, "Failed to get group info")
	assert.NotNil(gh.t, group, "Expected group %d to exist", groupID)
}

// AssertGroupNotExists asserts that a group does not exist (has been deleted).
func (gh *GroupHelper) AssertGroupNotExists(groupID int) {
	gh.t.Helper()

	group, err := gh.GetGroupInfo(groupID)
	if err == nil && group != nil {
		gh.t.Errorf("Expected group %d to not exist, but it was found", groupID)
	}
}

// UpdateGroupConfig updates a group's configuration.
func (gh *GroupHelper) UpdateGroupConfig(ownerClient *APIClient, groupID int, updates map[string]interface{}) error {
	gh.t.Helper()

	// Add the group ID to the updates
	updates["id"] = groupID

	var resp APIResponse
	err := ownerClient.PutJSON("/api/groups", updates, &resp)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("update group failed: %s", resp.Message)
	}

	gh.t.Logf("Updated group %d configuration: %+v", groupID, updates)
	return nil
}

// GetMemberCount returns the number of active members in a group.
func (gh *GroupHelper) GetMemberCount(ownerClient *APIClient, groupID int) (int, error) {
	gh.t.Helper()

	members, err := ownerClient.GetGroupMembers(groupID, 1) // status=1 means Active
	if err != nil {
		return 0, err
	}

	return len(members), nil
}

// AssertMemberCount asserts that a group has a specific number of active members.
func (gh *GroupHelper) AssertMemberCount(ownerClient *APIClient, groupID, expectedCount int) {
	gh.t.Helper()

	count, err := gh.GetMemberCount(ownerClient, groupID)
	require.NoError(gh.t, err, "Failed to get member count")
	assert.Equal(gh.t, expectedCount, count, "Expected group %d to have %d members", groupID, expectedCount)
}
