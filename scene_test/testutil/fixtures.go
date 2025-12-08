// Package testutil provides utilities for integration testing of the NewAPI service.
package testutil

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestFixtures holds all test data created during setup.
// It provides a convenient way to reference created entities in tests.
type TestFixtures struct {
	t          *testing.T
	client     *APIClient // Admin client with session
	upstream   *MockUpstreamServer
	cleanupFns []func()

	// Users
	AdminUser    *UserModel
	RegularUser1 *UserModel
	RegularUser2 *UserModel
	RegularUser3 *UserModel

	// User clients (each with their own session)
	User1Client *APIClient
	User2Client *APIClient
	User3Client *APIClient

	// API Tokens (sk-* tokens for chat completion)
	AdminAPIToken string
	User1APIToken string
	User2APIToken string
	User3APIToken string

	// P2P Groups
	SharedGroup1  *P2PGroupModel
	SharedGroup2  *P2PGroupModel
	PrivateGroup1 *P2PGroupModel

	// Channels
	PublicChannel    *ChannelModel
	GroupChannel1    *ChannelModel
	GroupChannel2    *ChannelModel
	PrivateChannel1  *ChannelModel
	P2PGroupChannel1 *ChannelModel
}

// NewTestFixtures creates a new fixtures instance.
func NewTestFixtures(t *testing.T, client *APIClient) *TestFixtures {
	return &TestFixtures{
		t:          t,
		client:     client,
		cleanupFns: make([]func(), 0),
	}
}

// SetUpstream sets the mock upstream server for the fixtures.
func (f *TestFixtures) SetUpstream(upstream *MockUpstreamServer) {
	f.upstream = upstream
}

// GetUpstreamURL returns the mock upstream server URL.
func (f *TestFixtures) GetUpstreamURL() string {
	if f.upstream != nil {
		return f.upstream.BaseURL
	}
	return ""
}

// addCleanup adds a cleanup function to be called during teardown.
func (f *TestFixtures) addCleanup(fn func()) {
	f.cleanupFns = append(f.cleanupFns, fn)
}

// Cleanup runs all cleanup functions in reverse order.
func (f *TestFixtures) Cleanup() {
	for i := len(f.cleanupFns) - 1; i >= 0; i-- {
		f.cleanupFns[i]()
	}
}

// --- User Creation Helpers ---

// CreateTestUser creates a test user with the given username and password.
func (f *TestFixtures) CreateTestUser(username, password string, group string) (*UserModel, error) {
	// Generate unique external_id to avoid UNIQUE constraint violation
	externalId := fmt.Sprintf("test_%s_%d", username, time.Now().UnixNano())
	user := &UserModel{
		Username:   username,
		Password:   password,
		Group:      "default", // initial group; will be updated below if needed
		Status:     1,         // Active
		ExternalId: externalId,
	}

	id, err := f.client.CreateUserFull(user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user %s: %w", username, err)
	}

	user.ID = id

	// If a non-empty group is requested and different from default,
	// update the user to set the desired billing group while keeping
	// the same password.
	if group != "" && group != "default" {
		update := &UserModel{
			ID:       id,
			Username: username,
			Password: password,
			Group:    group,
		}
		if err := f.client.UpdateUser(update); err != nil {
			return nil, fmt.Errorf("failed to update user %s group to %s: %w", username, group, err)
		}
		user.Group = group
	}

	// CreateUser API doesn't set quota, so we need to adjust it separately using admin API
	// Add 10B quota (enough for testing)
	err = f.client.AdjustUserQuota(id, 10000000000)
	if err != nil {
		return nil, fmt.Errorf("failed to adjust quota for user %s: %w", username, err)
	}
	user.Quota = 10000000000

	f.addCleanup(func() {
		f.client.DeleteUser(id)
	})

	return user, nil
}

// SetupBasicUsers creates the standard set of test users.
// - User1: In "default" group
// - User2: In "default" group
// - User3: In "vip" group
func (f *TestFixtures) SetupBasicUsers() error {
	var err error

	// Create regular users (using admin client)
	f.RegularUser1, err = f.CreateTestUser("testuser1", "testpass123", "default")
	if err != nil {
		return err
	}

	f.RegularUser2, err = f.CreateTestUser("testuser2", "testpass123", "default")
	if err != nil {
		return err
	}

	f.RegularUser3, err = f.CreateTestUser("testuser3", "testpass123", "vip")
	if err != nil {
		return err
	}

	// Create separate clients and login each user
	f.User1Client = f.client.Clone()
	_, err = f.User1Client.Login("testuser1", "testpass123")
	if err != nil {
		return fmt.Errorf("failed to login user1: %w", err)
	}

	f.User2Client = f.client.Clone()
	_, err = f.User2Client.Login("testuser2", "testpass123")
	if err != nil {
		return fmt.Errorf("failed to login user2: %w", err)
	}

	f.User3Client = f.client.Clone()
	_, err = f.User3Client.Login("testuser3", "testpass123")
	if err != nil {
		return fmt.Errorf("failed to login user3: %w", err)
	}

	return nil
}

// --- API Token Creation Helpers ---

// CreateTestAPIToken creates an API token for a user using their client.
func (f *TestFixtures) CreateTestAPIToken(name string, userClient *APIClient, p2pGroupID *int) (string, error) {
	token := &TokenModel{
		Name:           name,
		Status:         1, // Active
		UnlimitedQuota: true,
		P2PGroupID:     p2pGroupID,
	}

	key, err := userClient.CreateTokenFull(token)
	if err != nil {
		return "", fmt.Errorf("failed to create API token %s: %w", name, err)
	}

	return key, nil
}

// SetupBasicAPITokens creates API tokens for all test users.
func (f *TestFixtures) SetupBasicAPITokens() error {
	var err error

	// Create API tokens for each user using their respective clients
	f.User1APIToken, err = f.CreateTestAPIToken("user1-token", f.User1Client, nil)
	if err != nil {
		return err
	}

	f.User2APIToken, err = f.CreateTestAPIToken("user2-token", f.User2Client, nil)
	if err != nil {
		return err
	}

	f.User3APIToken, err = f.CreateTestAPIToken("user3-token", f.User3Client, nil)
	if err != nil {
		return err
	}

	return nil
}

// --- Channel Creation Helpers ---

// ChannelType constants matching the backend
const (
	ChannelTypeOpenAI = 1
)

// CreateTestChannel creates a test channel.
func (f *TestFixtures) CreateTestChannel(name string, models string, group string, baseURL string, isPrivate bool, ownerUserID int, allowedGroups string) (*ChannelModel, error) {
	priority := int64(0)
	weight := uint(1)

	var allowedGroupsPtr *string
	if allowedGroups != "" {
		// New P2P routing expects allowed_groups to be a JSON array
		// of integers (group IDs). For convenience, tests often pass
		// a simple comma-separated list or a single numeric string.
		// If the value does not look like JSON, wrap it into an array.
		trimmed := strings.TrimSpace(allowedGroups)
		if !strings.HasPrefix(trimmed, "[") {
			jsonValue := fmt.Sprintf("[%s]", trimmed)
			allowedGroupsPtr = &jsonValue
		} else {
			allowedGroupsPtr = &allowedGroups
		}
	}

	channel := &ChannelModel{
		Name:        name,
		Type:        ChannelTypeOpenAI,
		Key:         "sk-test-key-" + name,
		Models:      models,
		Group:       group,
		BaseURL:     &baseURL,
		Priority:    &priority,
		Weight:      &weight,
		Status:      1, // Enabled
		IsPrivate:   isPrivate,
		OwnerUserId: ownerUserID,
	}

	if allowedGroupsPtr != nil {
		channel.AllowedGroups = allowedGroupsPtr
	}

	_, err := f.client.AddChannel(channel)
	if err != nil {
		return nil, fmt.Errorf("failed to create channel %s: %w", name, err)
	}

	// Query to get the actual ID
	channels, err := f.client.GetAllChannels()
	if err != nil {
		return nil, fmt.Errorf("failed to query channels: %w", err)
	}

	for _, ch := range channels {
		if ch.Name == name {
			channel.ID = ch.ID
			f.addCleanup(func() {
				f.client.DeleteChannel(ch.ID)
			})
			return channel, nil
		}
	}

	return nil, fmt.Errorf("channel %s created but not found in list", name)
}

// SetupBasicChannels creates a standard set of test channels.
func (f *TestFixtures) SetupBasicChannels() error {
	var err error
	baseURL := f.GetUpstreamURL()

	// Public channel - available to all groups
	f.PublicChannel, err = f.CreateTestChannel(
		"public-channel",
		"gpt-4,gpt-3.5-turbo",
		"default,vip", // Available to both groups
		baseURL,
		false, // Not private
		0,     // No owner
		"",    // No P2P group restriction
	)
	if err != nil {
		return fmt.Errorf("failed to create public channel: %w", err)
	}

	// Group-specific channel for "default" group only
	f.GroupChannel1, err = f.CreateTestChannel(
		"default-group-channel",
		"gpt-4",
		"default", // Only default group
		baseURL,
		false,
		0,
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to create default group channel: %w", err)
	}

	// Group-specific channel for "vip" group only
	f.GroupChannel2, err = f.CreateTestChannel(
		"vip-group-channel",
		"gpt-4,claude-3",
		"vip", // Only vip group
		baseURL,
		false,
		0,
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to create vip group channel: %w", err)
	}

	return nil
}

// --- P2P Group Creation Helpers ---

// P2P Group type constants
const (
	P2PGroupTypePrivate = 1
	P2PGroupTypeShared  = 2
)

// P2P Group join method constants
const (
	P2PJoinMethodInvite   = 0
	P2PJoinMethodReview   = 1
	P2PJoinMethodPassword = 2
)

// P2P Member status constants
const (
	P2PMemberStatusPending  = 0
	P2PMemberStatusActive   = 1
	P2PMemberStatusRejected = 2
	P2PMemberStatusBanned   = 3
	P2PMemberStatusLeft     = 4
)

// CreateTestP2PGroup creates a P2P group using the owner's client.
func (f *TestFixtures) CreateTestP2PGroup(name string, ownerClient *APIClient, ownerID int, groupType int, joinMethod int, joinKey string) (*P2PGroupModel, error) {
	group := &P2PGroupModel{
		Name:       name,
		OwnerId:    ownerID,
		Type:       groupType,
		JoinMethod: joinMethod,
		JoinKey:    joinKey,
	}

	id, err := ownerClient.CreateP2PGroup(group)
	if err != nil {
		return nil, fmt.Errorf("failed to create P2P group %s: %w", name, err)
	}

	group.ID = id
	return group, nil
}

// AddUserToP2PGroup adds a user to a P2P group (handles application and approval).
func (f *TestFixtures) AddUserToP2PGroup(groupID int, userClient *APIClient, ownerClient *APIClient, password string) error {

	// User applies to join
	err := userClient.ApplyToP2PGroup(groupID, password)
	if err != nil {
		return fmt.Errorf("failed to apply to group: %w", err)
	}

	// For password-based join, the user should be auto-approved
	// For other join methods, owner needs to approve
	// We'll skip approval for password-based groups

	return nil
}

// ApproveP2PMember approves a pending member in a P2P group.
func (f *TestFixtures) ApproveP2PMember(groupID int, userID int, ownerClient *APIClient) error {
	return ownerClient.UpdateMemberStatus(groupID, userID, P2PMemberStatusActive)
}

// SetupP2PGroups creates P2P groups for testing.
func (f *TestFixtures) SetupP2PGroups() error {
	var err error

	// Shared group owned by User1 with password join
	f.SharedGroup1, err = f.CreateTestP2PGroup(
		"shared-group-1",
		f.User1Client,
		f.RegularUser1.ID,
		P2PGroupTypeShared,
		P2PJoinMethodPassword,
		"group1pass",
	)
	if err != nil {
		return fmt.Errorf("failed to create shared group 1: %w", err)
	}

	// Add User2 to SharedGroup1
	err = f.User2Client.ApplyToP2PGroup(f.SharedGroup1.ID, "group1pass")
	if err != nil {
		return fmt.Errorf("failed to add user2 to shared group 1: %w", err)
	}

	// Private group owned by User3
	f.PrivateGroup1, err = f.CreateTestP2PGroup(
		"private-group-1",
		f.User3Client,
		f.RegularUser3.ID,
		P2PGroupTypePrivate,
		P2PJoinMethodInvite,
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to create private group 1: %w", err)
	}

	return nil
}

// SetupP2PChannels creates channels that are restricted to P2P groups.
func (f *TestFixtures) SetupP2PChannels() error {
	var err error
	baseURL := f.GetUpstreamURL()

	// Channel only accessible to SharedGroup1 members
	f.P2PGroupChannel1, err = f.CreateTestChannel(
		"p2p-shared-channel",
		"gpt-4,gpt-3.5-turbo",
		"default", // System group
		baseURL,
		false, // Not private
		0,
		fmt.Sprintf("%d", f.SharedGroup1.ID), // P2P group restriction
	)
	if err != nil {
		return fmt.Errorf("failed to create P2P group channel: %w", err)
	}

	// Private channel owned by User1
	f.PrivateChannel1, err = f.CreateTestChannel(
		"private-channel-1",
		"gpt-4",
		"default",
		baseURL,
		true,              // Is private
		f.RegularUser1.ID, // Owner
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to create private channel: %w", err)
	}

	return nil
}

// --- Complete Setup Functions ---

// SetupRoutingTestFixtures sets up all fixtures needed for routing tests.
func (f *TestFixtures) SetupRoutingTestFixtures() error {
	f.t.Log("Setting up basic users...")
	if err := f.SetupBasicUsers(); err != nil {
		return fmt.Errorf("SetupBasicUsers failed: %w", err)
	}

	f.t.Log("Setting up basic API tokens...")
	if err := f.SetupBasicAPITokens(); err != nil {
		return fmt.Errorf("SetupBasicAPITokens failed: %w", err)
	}

	f.t.Log("Setting up basic channels...")
	if err := f.SetupBasicChannels(); err != nil {
		return fmt.Errorf("SetupBasicChannels failed: %w", err)
	}

	f.t.Log("Setting up P2P groups...")
	if err := f.SetupP2PGroups(); err != nil {
		return fmt.Errorf("SetupP2PGroups failed: %w", err)
	}

	f.t.Log("Setting up P2P channels...")
	if err := f.SetupP2PChannels(); err != nil {
		return fmt.Errorf("SetupP2PChannels failed: %w", err)
	}

	return nil
}

// --- Client Helpers ---

// ClientForUser1 returns an API client authenticated as User1's API token.
func (f *TestFixtures) ClientForUser1() *APIClient {
	return f.client.WithToken(f.User1APIToken)
}

// ClientForUser2 returns an API client authenticated as User2's API token.
func (f *TestFixtures) ClientForUser2() *APIClient {
	return f.client.WithToken(f.User2APIToken)
}

// ClientForUser3 returns an API client authenticated as User3's API token.
func (f *TestFixtures) ClientForUser3() *APIClient {
	return f.client.WithToken(f.User3APIToken)
}

// ClientWithToken returns an API client with a specific token.
func (f *TestFixtures) ClientWithToken(token string) *APIClient {
	return f.client.WithToken(token)
}
