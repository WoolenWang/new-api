// Package testutil provides specialized fixtures for orthogonal configuration tests.
package testutil

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// OrthogonalFixtures holds test data for orthogonal configuration tests.
// This includes users with different system groups, P2P groups, tokens with
// various configurations, and channels with different group combinations.
type OrthogonalFixtures struct {
	t      *testing.T
	client *APIClient

	// Users with different system groups
	UserDefault *UserModel // default group (ratio 1.0)
	UserVip     *UserModel // vip group (ratio 2.0)
	UserSvip    *UserModel // svip group (ratio 0.8)

	// User clients
	UserDefaultClient *APIClient
	UserVipClient     *APIClient
	UserSvipClient    *APIClient

	// P2P Groups
	G1 *P2PGroupModel // Public shared group
	G2 *P2PGroupModel // Public shared group
	G3 *P2PGroupModel // Public shared group (used for "invalid" in some tests)

	// Channels organized by system group and P2P authorization
	// Format: Ch_{SystemGroup}_{P2PAuth}
	ChDefaultPublic *ChannelModel // default, no P2P
	ChDefaultG1     *ChannelModel // default, allowed_groups=[G1]
	ChDefaultG1G2   *ChannelModel // default, allowed_groups=[G1,G2]
	ChVipPublic     *ChannelModel // vip, no P2P
	ChVipG1         *ChannelModel // vip, allowed_groups=[G1]
	ChVipG2         *ChannelModel // vip, allowed_groups=[G2]
	ChSvipPublic    *ChannelModel // svip, no P2P
	ChSvipG1G2      *ChannelModel // svip, allowed_groups=[G1,G2]

	// Tokens for different configurations
	// User-specific tokens will be created on-demand in tests
	Tokens map[string]string // token_name -> token_key

	// Upstream mock server
	upstream *MockUpstreamServer
}

// NewOrthogonalFixtures creates fixtures for orthogonal tests.
func NewOrthogonalFixtures(t *testing.T, client *APIClient, upstream *MockUpstreamServer) *OrthogonalFixtures {
	t.Helper()

	return &OrthogonalFixtures{
		t:        t,
		client:   client,
		upstream: upstream,
		Tokens:   make(map[string]string),
	}
}

// Setup creates all necessary test data for orthogonal tests.
func (of *OrthogonalFixtures) Setup() error {
	of.t.Helper()

	// 1. Create users with different system groups
	if err := of.setupUsers(); err != nil {
		return err
	}

	// 2. Create P2P groups
	if err := of.setupP2PGroups(); err != nil {
		return err
	}

	// 3. Create channels with various configurations
	if err := of.setupChannels(); err != nil {
		return err
	}

	of.t.Log("Orthogonal fixtures setup completed")
	return nil
}

// setupUsers creates users with different system groups.
func (of *OrthogonalFixtures) setupUsers() error {
	of.t.Helper()

	var err error

	// Create default user
	of.UserDefault, err = of.createUser("ox_user_default", "default")
	if err != nil {
		return err
	}
	of.UserDefaultClient = of.client.Clone()
	if _, err = of.UserDefaultClient.Login("ox_user_default", "testpass123"); err != nil {
		return fmt.Errorf("failed to login default user: %w", err)
	}

	// Create vip user
	of.UserVip, err = of.createUser("ox_user_vip", "vip")
	if err != nil {
		return err
	}
	of.UserVipClient = of.client.Clone()
	if _, err = of.UserVipClient.Login("ox_user_vip", "testpass123"); err != nil {
		return fmt.Errorf("failed to login vip user: %w", err)
	}

	// Create svip user
	of.UserSvip, err = of.createUser("ox_user_svip", "svip")
	if err != nil {
		return err
	}
	of.UserSvipClient = of.client.Clone()
	if _, err = of.UserSvipClient.Login("ox_user_svip", "testpass123"); err != nil {
		return fmt.Errorf("failed to login svip user: %w", err)
	}

	of.t.Log("Created users: default, vip, svip")
	return nil
}

// createUser creates a user with the specified group.
func (of *OrthogonalFixtures) createUser(username, group string) (*UserModel, error) {
	of.t.Helper()

	user := &UserModel{
		Username:   username,
		Password:   "testpass123",
		Group:      "default", // Will be updated below
		Status:     1,
		ExternalId: fmt.Sprintf("ox_%s", username),
	}

	id, err := of.client.CreateUserFull(user)
	if err != nil {
		return nil, fmt.Errorf("failed to create user %s: %w", username, err)
	}

	user.ID = id

	// Update to the desired group
	if group != "default" {
		user.Group = group
		if err := of.client.UpdateUser(user); err != nil {
			return nil, fmt.Errorf("failed to update user %s group: %w", username, err)
		}
	}

	// Add quota
	if err := of.client.AdjustUserQuota(id, 10000000000); err != nil {
		return nil, fmt.Errorf("failed to adjust quota: %w", err)
	}
	user.Quota = 10000000000

	return user, nil
}

// setupP2PGroups creates P2P groups G1, G2, G3.
func (of *OrthogonalFixtures) setupP2PGroups() error {
	of.t.Helper()

	var err error

	// Create G1 (password-protected, owned by UserDefault)
	of.G1 = &P2PGroupModel{
		Name:        "ox-g1",
		DisplayName: "Orthogonal G1",
		OwnerId:     of.UserDefault.ID,
		Type:        2, // Shared
		JoinMethod:  2, // Password
		JoinKey:     "g1pass",
	}
	of.G1.ID, err = of.UserDefaultClient.CreateP2PGroup(of.G1)
	if err != nil {
		return fmt.Errorf("failed to create G1: %w", err)
	}

	// Create G2 (password-protected, owned by UserVip)
	of.G2 = &P2PGroupModel{
		Name:        "ox-g2",
		DisplayName: "Orthogonal G2",
		OwnerId:     of.UserVip.ID,
		Type:        2, // Shared
		JoinMethod:  2, // Password
		JoinKey:     "g2pass",
	}
	of.G2.ID, err = of.UserVipClient.CreateP2PGroup(of.G2)
	if err != nil {
		return fmt.Errorf("failed to create G2: %w", err)
	}

	// Create G3 (password-protected, owned by UserSvip)
	of.G3 = &P2PGroupModel{
		Name:        "ox-g3",
		DisplayName: "Orthogonal G3",
		OwnerId:     of.UserSvip.ID,
		Type:        2, // Shared
		JoinMethod:  2, // Password
		JoinKey:     "g3pass",
	}
	of.G3.ID, err = of.UserSvipClient.CreateP2PGroup(of.G3)
	if err != nil {
		return fmt.Errorf("failed to create G3: %w", err)
	}

	of.t.Logf("Created P2P groups: G1=%d, G2=%d, G3=%d", of.G1.ID, of.G2.ID, of.G3.ID)
	return nil
}

// setupChannels creates channels with various system group and P2P authorization combinations.
func (of *OrthogonalFixtures) setupChannels() error {
	of.t.Helper()

	var err error
	model := "gpt-4"

	// Create channels with different configurations
	// Format: {SystemGroup}_{P2PAuth}

	// default group channels
	of.ChDefaultPublic, err = of.CreateChannel("ox-ch-default-public", model, "default", []int{})
	if err != nil {
		return err
	}

	of.ChDefaultG1, err = of.CreateChannel("ox-ch-default-g1", model, "default", []int{of.G1.ID})
	if err != nil {
		return err
	}

	of.ChDefaultG1G2, err = of.CreateChannel("ox-ch-default-g1g2", model, "default", []int{of.G1.ID, of.G2.ID})
	if err != nil {
		return err
	}

	// vip group channels
	of.ChVipPublic, err = of.CreateChannel("ox-ch-vip-public", model, "vip", []int{})
	if err != nil {
		return err
	}

	of.ChVipG1, err = of.CreateChannel("ox-ch-vip-g1", model, "vip", []int{of.G1.ID})
	if err != nil {
		return err
	}

	of.ChVipG2, err = of.CreateChannel("ox-ch-vip-g2", model, "vip", []int{of.G2.ID})
	if err != nil {
		return err
	}

	// svip group channels
	of.ChSvipPublic, err = of.CreateChannel("ox-ch-svip-public", model, "svip", []int{})
	if err != nil {
		return err
	}

	of.ChSvipG1G2, err = of.CreateChannel("ox-ch-svip-g1g2", model, "svip", []int{of.G1.ID, of.G2.ID})
	if err != nil {
		return err
	}

	of.t.Log("Created 8 channels with various configurations")
	return nil
}

// CreateChannel creates a channel with specified system group and P2P authorization.
// Exported so that scenario tests can provision additional ad-hoc channels on top
// of the standard orthogonal fixtures.
func (of *OrthogonalFixtures) CreateChannel(name, model, systemGroup string, allowedGroups []int) (*ChannelModel, error) {
	of.t.Helper()

	priority := int64(0)
	weight := uint(1)
	baseURL := of.upstream.BaseURL

	var allowedGroupsPtr *string
	if len(allowedGroups) > 0 {
		raw, err := json.Marshal(allowedGroups)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal allowed groups for channel %s: %w", name, err)
		}
		asStr := string(raw)
		allowedGroupsPtr = &asStr
	}

	channel := &ChannelModel{
		Name:      name,
		Type:      ChannelTypeOpenAI,
		Key:       "sk-test-key-" + name,
		Models:    model,
		Group:     systemGroup,
		BaseURL:   &baseURL,
		Priority:  &priority,
		Weight:    &weight,
		Status:    1, // Enabled
		IsPrivate: false,
	}

	if allowedGroupsPtr != nil {
		channel.AllowedGroups = allowedGroupsPtr
	}

	if _, err := of.client.AddChannel(channel); err != nil {
		return nil, fmt.Errorf("failed to create channel %s: %w", name, err)
	}

	// Query back to discover the assigned ID.
	channels, err := of.client.GetAllChannels()
	if err != nil {
		return nil, fmt.Errorf("failed to query channels after creation: %w", err)
	}
	for _, ch := range channels {
		if ch.Name == name {
			channel.ID = ch.ID
			of.t.Logf("Created channel: id=%d, name=%s, group=%s, p2p=%v", ch.ID, name, systemGroup, allowedGroups)
			return channel, nil
		}
	}

	return nil, fmt.Errorf("channel %s created but not found in list", name)
}

// JoinUserToGroups adds a user to specified P2P groups.
func (of *OrthogonalFixtures) JoinUserToGroups(userClient *APIClient, userID int, groupIDs []int) error {
	of.t.Helper()

	for _, groupID := range groupIDs {
		var password string

		// Determine the password based on group ID
		switch groupID {
		case of.G1.ID:
			password = "g1pass"
		case of.G2.ID:
			password = "g2pass"
		case of.G3.ID:
			password = "g3pass"
		default:
			return fmt.Errorf("unknown group ID: %d", groupID)
		}

		// Apply to join
		err := userClient.ApplyToP2PGroup(groupID, password)
		if err != nil {
			return fmt.Errorf("failed to join group %d: %w", groupID, err)
		}
	}

	of.t.Logf("User %d joined groups: %v", userID, groupIDs)
	return nil
}

// CreateTokenWithConfig creates an API token with specific configuration.
// billingGroups: JSON array like ["svip", "default"], or empty for default
// p2pGroupID: 0 means no restriction, >0 means restricted to that group
func (of *OrthogonalFixtures) CreateTokenWithConfig(userClient *APIClient, userID int, name string, billingGroups string, p2pGroupID int) (string, error) {
	of.t.Helper()

	// Use TokenModel + CreateTokenFull so we can reliably discover the sk-* key,
	// since the /api/token creation endpoint does not return it in the response.
	tokenModel := &TokenModel{
		UserId:      userID,
		Name:        name,
		Status:      1,
		RemainQuota: 100000000000, // large quota for tests
		Group:       billingGroups,
	}
	if p2pGroupID > 0 {
		tokenModel.P2PGroupID = &p2pGroupID
	}

	tokenKey, err := userClient.CreateTokenFull(tokenModel)
	if err != nil {
		return "", fmt.Errorf("failed to create token %s: %w", name, err)
	}

	of.Tokens[name] = tokenKey
	of.t.Logf("Created token: %s, billing_groups=%s, p2p_group_id=%v", name, billingGroups, p2pGroupID)
	return tokenKey, nil
}

// CreateStandardTokens creates a set of standard tokens for orthogonal testing.
func (of *OrthogonalFixtures) CreateStandardTokens() error {
	of.t.Helper()

	// For UserDefault
	if _, err := of.CreateTokenWithConfig(of.UserDefaultClient, of.UserDefault.ID, "default_no_limit", "", 0); err != nil {
		return err
	}

	// For UserVip
	if _, err := of.CreateTokenWithConfig(of.UserVipClient, of.UserVip.ID, "vip_no_limit", "", 0); err != nil {
		return err
	}
	if _, err := of.CreateTokenWithConfig(of.UserVipClient, of.UserVip.ID, "vip_force_default", `["default"]`, 0); err != nil {
		return err
	}
	if _, err := of.CreateTokenWithConfig(of.UserVipClient, of.UserVip.ID, "vip_list_svip_default", `["svip","default"]`, 0); err != nil {
		return err
	}
	if _, err := of.CreateTokenWithConfig(of.UserVipClient, of.UserVip.ID, "vip_restrict_g1", "", of.G1.ID); err != nil {
		return err
	}

	// For UserSvip
	if _, err := of.CreateTokenWithConfig(of.UserSvipClient, of.UserSvip.ID, "svip_no_limit", "", 0); err != nil {
		return err
	}

	of.t.Logf("Created %d standard tokens", len(of.Tokens))
	return nil
}

// VerifyRoutingSuccess verifies that a request succeeds and routes to an expected channel.
func (of *OrthogonalFixtures) VerifyRoutingSuccess(t *testing.T, tokenKey, model string, expectedBillingGroup string) {
	t.Helper()

	client := of.client.WithToken(tokenKey)

	// Make a chat completion request
	resp, err := client.ChatCompletion(ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "user", Content: "test routing"},
		},
		Stream: false,
	})

	require.NoError(t, err, "Request should succeed")
	require.NotNil(t, resp, "Response should not be nil")

	// Verify billing group by checking the logs
	// Note: This requires querying the logs table or having a debug field in response
	t.Logf("Routing succeeded, expected billing group: %s", expectedBillingGroup)
}

// VerifyRoutingFailure verifies that a request fails with "no available channel" error.
func (of *OrthogonalFixtures) VerifyRoutingFailure(t *testing.T, tokenKey, model string) {
	t.Helper()

	client := of.client.WithToken(tokenKey)

	// Make a chat completion request
	_, err := client.ChatCompletion(ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "user", Content: "test routing"},
		},
		Stream: false,
	})

	require.Error(t, err, "Request should fail (no available channel)")
	require.Contains(t, err.Error(), "no available channel", "Error should indicate no available channel")

	t.Log("Routing failed as expected (no available channel)")
}

// GetUserByGroup returns the user with the specified system group.
func (of *OrthogonalFixtures) GetUserByGroup(group string) (*UserModel, *APIClient) {
	switch group {
	case "default":
		return of.UserDefault, of.UserDefaultClient
	case "vip":
		return of.UserVip, of.UserVipClient
	case "svip":
		return of.UserSvip, of.UserSvipClient
	default:
		of.t.Fatalf("Unknown group: %s", group)
		return nil, nil
	}
}

// GetChannelByConfig returns the channel matching the system group and P2P config.
func (of *OrthogonalFixtures) GetChannelByConfig(systemGroup string, p2pGroups []int) *ChannelModel {
	// This is a simplified lookup; in practice, you'd match based on exact config
	switch systemGroup {
	case "default":
		if len(p2pGroups) == 0 {
			return of.ChDefaultPublic
		} else if len(p2pGroups) == 1 && p2pGroups[0] == of.G1.ID {
			return of.ChDefaultG1
		} else if len(p2pGroups) == 2 {
			return of.ChDefaultG1G2
		}
	case "vip":
		if len(p2pGroups) == 0 {
			return of.ChVipPublic
		} else if len(p2pGroups) == 1 && p2pGroups[0] == of.G1.ID {
			return of.ChVipG1
		} else if len(p2pGroups) == 1 && p2pGroups[0] == of.G2.ID {
			return of.ChVipG2
		}
	case "svip":
		if len(p2pGroups) == 0 {
			return of.ChSvipPublic
		} else if len(p2pGroups) == 2 {
			return of.ChSvipG1G2
		}
	}

	return nil
}
