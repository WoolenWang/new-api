// Package orthogonal_config contains tests for complex configuration combinations
package orthogonal_config

import (
	"testing"

	"new-api/scene_test/testutil"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ChannelMultiGroupSuite tests channels authorized to multiple P2P groups.
// This suite focuses on validating the P2P authorization dimension (Factor B).
type ChannelMultiGroupSuite struct {
	suite.Suite
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	fixtures *testutil.OrthogonalFixtures
}

// SetupSuite initializes the test server and creates base fixtures.
func (s *ChannelMultiGroupSuite) SetupSuite() {
	var err error

	// Find project root
	projectRoot, err := testutil.FindProjectRoot()
	require.NoError(s.T(), err, "Failed to find project root")

	// Create and start test server
	config := testutil.DefaultConfig()
	s.server, err = testutil.NewTestServer(s.T(), projectRoot, config)
	require.NoError(s.T(), err, "Failed to create test server")

	err = s.server.Start()
	require.NoError(s.T(), err, "Failed to start test server")

	// Create API client
	s.client = testutil.NewAPIClient(s.server)

	// Initialize system
	rootUser, rootPass, err := s.client.InitializeSystem()
	require.NoError(s.T(), err, "Failed to initialize system")

	err = s.client.Login(rootUser, rootPass)
	require.NoError(s.T(), err, "Failed to login as admin")

	s.T().Log("✓ Test server started and system initialized")
}

// SetupTest creates fresh fixtures for each test.
func (s *ChannelMultiGroupSuite) SetupTest() {
	// Create mock upstream server
	s.upstream = testutil.NewMockUpstreamServer()

	// Create orthogonal fixtures
	s.fixtures = testutil.NewOrthogonalFixtures(s.T(), s.client, s.upstream)

	// Setup fixtures (users, groups, channels)
	err := s.fixtures.Setup()
	require.NoError(s.T(), err, "Failed to setup fixtures")

	s.T().Log("✓ Fixtures created for test")
}

// TearDownTest cleans up after each test.
func (s *ChannelMultiGroupSuite) TearDownTest() {
	if s.upstream != nil {
		s.upstream.Close()
	}
	s.T().Log("✓ Test cleanup completed")
}

// TearDownSuite stops the test server.
func (s *ChannelMultiGroupSuite) TearDownSuite() {
	if s.server != nil {
		s.server.Stop()
	}
	s.T().Log("✓ Test server stopped")
}

// TestOC01_ChannelMultiGroupAuthorization tests a channel authorized to multiple P2P groups.
//
// Test Case: OC-01
// Scenario: Ch-X (sys_group=vip) is authorized to [G1, G2]
//           User-A (sys_group=vip) joins G1
//           User-A uses a token with no restrictions
// Expected: Routing succeeds, billing under vip group
func (s *ChannelMultiGroupSuite) TestOC01_ChannelMultiGroupAuthorization() {
	// Arrange: Create a channel authorized to both G1 and G2
	chMulti, err := s.fixtures.CreateChannel(
		"oc01-ch-multi",
		"gpt-4",
		"vip",
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to create multi-group channel")

	// Join User-Vip to G1
	err = s.fixtures.JoinUserToGroups(s.fixtures.UserVipClient, s.fixtures.UserVip.ID, []int{s.fixtures.G1.ID})
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token with no restrictions
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"oc01-token",
		"", // No billing group override
		0,  // No P2P restriction
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Request should succeed and route to the channel
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "vip")
	s.T().Logf("✓ OC-01: Channel authorized to [G1, G2], user in G1 → success (channel_id=%d)", chMulti.ID)
}

// TestOC02_UserMatchingMultipleAuthGroups tests a user who matches multiple authorized groups.
//
// Test Case: OC-02
// Scenario: Ch-X (sys_group=vip) is authorized to [G1, G2, G3]
//           User-Vip joins G2 only
//           Request with no token P2P restriction
// Expected: Routing succeeds (matches G2)
func (s *ChannelMultiGroupSuite) TestOC02_UserMatchingMultipleAuthGroups() {
	// Arrange: Create a channel authorized to G1, G2, G3
	chMulti, err := s.fixtures.CreateChannel(
		"oc02-ch-multi",
		"gpt-4",
		"vip",
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID, s.fixtures.G3.ID},
	)
	require.NoError(s.T(), err, "Failed to create multi-group channel")

	// Join User-Vip to G2 only
	err = s.fixtures.JoinUserToGroups(s.fixtures.UserVipClient, s.fixtures.UserVip.ID, []int{s.fixtures.G2.ID})
	require.NoError(s.T(), err, "Failed to join G2")

	// Create token
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"oc02-token",
		"",
		0,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "vip")
	s.T().Logf("✓ OC-02: Channel authorized to [G1, G2, G3], user in G2 → success (channel_id=%d)", chMulti.ID)
}

// TestOC03_CrossSystemGroupWithP2P tests cross-system-group + P2P authorization.
//
// Test Case: OC-03
// Scenario: Ch-X (sys_group=default) is authorized to [G1]
//           User-Vip (sys_group=vip) joins G1
//           User-Vip uses token with billing_groups=["default"]
// Expected: Routing succeeds (system group downgrade + P2P match), billed under default
func (s *ChannelMultiGroupSuite) TestOC03_CrossSystemGroupWithP2P() {
	// Arrange: Create a default-group channel authorized to G1
	chCross, err := s.fixtures.CreateChannel(
		"oc03-ch-cross",
		"gpt-4",
		"default",
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to create cross-group channel")

	// Join User-Vip to G1
	err = s.fixtures.JoinUserToGroups(s.fixtures.UserVipClient, s.fixtures.UserVip.ID, []int{s.fixtures.G1.ID})
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token that forces billing group to "default"
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"oc03-token",
		`["default"]`, // Force billing group to default
		0,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "default")
	s.T().Logf("✓ OC-03: Vip user with default billing group + P2P → success (channel_id=%d)", chCross.ID)
}

// TestOC04_ChannelAuthorizationConflict tests channel authorization conflict scenarios.
//
// Test Case: OC-04
// Scenario: Ch-X (sys_group=vip) is authorized to [G1], is_private=false
//           User-Vip joins both G1 and G2
//           Token has no P2P restriction
// Expected: Routing succeeds (matches G1, ignores G2)
func (s *ChannelMultiGroupSuite) TestOC04_ChannelAuthorizationConflict() {
	// Arrange: Create a vip channel authorized to G1 only
	chSingle, err := s.fixtures.CreateChannel(
		"oc04-ch-single",
		"gpt-4",
		"vip",
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to create channel")

	// Join User-Vip to both G1 and G2
	err = s.fixtures.JoinUserToGroups(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to join groups")

	// Create token
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"oc04-token",
		"",
		0,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "vip")
	s.T().Logf("✓ OC-04: User in [G1, G2], channel authorized to [G1] → success (channel_id=%d)", chSingle.ID)
}

// TestOC05_PrivateChannelMultiGroupInvalid tests that private channels ignore P2P authorization.
//
// Test Case: OC-05
// Scenario: Ch-X (sys_group=vip, is_private=true) is authorized to [G1, G2]
//           User-B (sys_group=vip via token) joins G1
//           User-B is NOT the channel owner
// Expected: Routing fails (private channels only accessible by owner)
func (s *ChannelMultiGroupSuite) TestOC05_PrivateChannelMultiGroupInvalid() {
	// Arrange: Create a private channel owned by User-Vip, authorized to G1 and G2
	chPrivate, err := s.fixtures.CreatePrivateChannel(
		"oc05-ch-private",
		"gpt-4",
		"vip",
		s.fixtures.UserVip.ID, // Owner
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to create private channel")

	// Create User-Default as vip (via token) and join G1
	err = s.fixtures.JoinUserToGroups(s.fixtures.UserDefaultClient, s.fixtures.UserDefault.ID, []int{s.fixtures.G1.ID})
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token for User-Default with billing group "vip"
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		"oc05-token",
		`["vip"]`, // Force billing to vip
		0,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Should fail because User-Default is not the owner
	s.fixtures.VerifyRoutingFailure(s.T(), tokenKey, "gpt-4")
	s.T().Logf("✓ OC-05: Private channel authorized to [G1, G2], non-owner in G1 → failure (channel_id=%d)", chPrivate.ID)
}

// TestOC06_ChannelStatsAggregationByGroup tests channel statistics aggregation across groups.
//
// Test Case: OC-06
// Scenario: Ch-X is authorized to [G1, G2]
//           User-A (in G1) makes 10 requests
//           User-B (in G2) makes 20 requests
// Expected: Both G1 and G2 statistics should include Ch-X data
//
// Note: This test focuses on routing success; actual statistics validation
// would require waiting for stats aggregation (15+ minutes) or direct DB inspection.
func (s *ChannelMultiGroupSuite) TestOC06_ChannelStatsAggregationByGroup() {
	// Arrange: Create a channel authorized to G1 and G2
	chShared, err := s.fixtures.CreateChannel(
		"oc06-ch-shared",
		"gpt-4",
		"vip",
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to create shared channel")

	// Setup User-Vip in G1
	err = s.fixtures.JoinUserToGroups(s.fixtures.UserVipClient, s.fixtures.UserVip.ID, []int{s.fixtures.G1.ID})
	require.NoError(s.T(), err, "Failed to join G1")

	tokenVip, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"oc06-token-vip",
		"",
		s.fixtures.G1.ID, // Restrict to G1
	)
	require.NoError(s.T(), err, "Failed to create token for vip user")

	// Setup User-Default in G2 (with vip billing)
	err = s.fixtures.JoinUserToGroups(s.fixtures.UserDefaultClient, s.fixtures.UserDefault.ID, []int{s.fixtures.G2.ID})
	require.NoError(s.T(), err, "Failed to join G2")

	tokenDefault, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		"oc06-token-default",
		`["vip"]`,        // Force vip billing
		s.fixtures.G2.ID, // Restrict to G2
	)
	require.NoError(s.T(), err, "Failed to create token for default user")

	// Act: Make requests from both users
	for i := 0; i < 10; i++ {
		s.fixtures.VerifyRoutingSuccess(s.T(), tokenVip, "gpt-4", "vip")
	}
	for i := 0; i < 20; i++ {
		s.fixtures.VerifyRoutingSuccess(s.T(), tokenDefault, "gpt-4", "vip")
	}

	// Assert: Statistics aggregation would be validated in a separate OS series test
	s.T().Logf("✓ OC-06: 10 requests from G1, 20 from G2 → both should aggregate Ch-X stats (channel_id=%d)", chShared.ID)
}

// TestOC07_MultiGroupChannelDisable tests the impact of disabling a multi-group channel.
//
// Test Case: OC-07
// Scenario: Ch-X is authorized to [G1, G2]
//           User-A (in G1) and User-B (in G2) can both access Ch-X
//           Ch-X is disabled
// Expected: Both users can no longer access Ch-X
func (s *ChannelMultiGroupSuite) TestOC07_MultiGroupChannelDisable() {
	// Arrange: Create a multi-group channel
	chToDisable, err := s.fixtures.CreateChannel(
		"oc07-ch-to-disable",
		"gpt-4",
		"vip",
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to create channel")

	// Setup User-Vip in G1
	err = s.fixtures.JoinUserToGroups(s.fixtures.UserVipClient, s.fixtures.UserVip.ID, []int{s.fixtures.G1.ID})
	require.NoError(s.T(), err, "Failed to join G1")

	tokenG1, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"oc07-token-g1",
		"",
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token for G1")

	// Setup User-Default in G2 (with vip billing)
	err = s.fixtures.JoinUserToGroups(s.fixtures.UserDefaultClient, s.fixtures.UserDefault.ID, []int{s.fixtures.G2.ID})
	require.NoError(s.T(), err, "Failed to join G2")

	tokenG2, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		"oc07-token-g2",
		`["vip"]`,
		s.fixtures.G2.ID,
	)
	require.NoError(s.T(), err, "Failed to create token for G2")

	// Verify both users can access the channel before disabling
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenG1, "gpt-4", "vip")
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenG2, "gpt-4", "vip")

	// Act: Disable the channel
	chToDisable.Status = 2 // 2 = Disabled
	err = s.client.UpdateChannel(chToDisable)
	require.NoError(s.T(), err, "Failed to disable channel")

	// Assert: Both users should no longer be able to access the channel
	s.fixtures.VerifyRoutingFailure(s.T(), tokenG1, "gpt-4")
	s.fixtures.VerifyRoutingFailure(s.T(), tokenG2, "gpt-4")

	s.T().Logf("✓ OC-07: Multi-group channel disabled → both G1 and G2 users cannot access (channel_id=%d)", chToDisable.ID)
}

// TestChannelMultiGroupSuite runs the test suite.
func TestChannelMultiGroupSuite(t *testing.T) {
	suite.Run(t, new(ChannelMultiGroupSuite))
}
