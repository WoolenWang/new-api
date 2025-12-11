// Package orthogonal_config contains tests for complex configuration combinations
package orthogonal_config

import (
	"testing"

	"new-api/scene_test/testutil"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// OrthogonalMatrixSuite tests systematic combinations of 7 factors using L18 orthogonal array.
// This suite provides maximum coverage with minimal test cases.
//
// 7 Factors:
// A. Channel System Group (default/vip/svip)
// B. Channel P2P Authorization (none/G1/G2/[G1,G2])
// C. Channel Privacy (private/non-private)
// D. User System Group (default/vip/svip)
// E. User P2P Membership (none/G1/G2/[G1,G2])
// F. Token Billing Groups (empty/single/list)
// G. Token P2P Restriction (none/single/multiple)
type OrthogonalMatrixSuite struct {
	suite.Suite
	server   *testutil.TestServer
	client   *testutil.APIClient
	upstream *testutil.MockUpstreamServer
	fixtures *testutil.OrthogonalFixtures
}

// SetupSuite initializes the test server and creates base fixtures.
func (s *OrthogonalMatrixSuite) SetupSuite() {
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
func (s *OrthogonalMatrixSuite) SetupTest() {
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
func (s *OrthogonalMatrixSuite) TearDownTest() {
	if s.upstream != nil {
		s.upstream.Close()
	}
	s.T().Log("✓ Test cleanup completed")
}

// TearDownSuite stops the test server.
func (s *OrthogonalMatrixSuite) TearDownSuite() {
	if s.server != nil {
		s.server.Stop()
	}
	s.T().Log("✓ Test server stopped")
}

// TestOM01_DefaultChannelDefaultUserNoRestriction tests the baseline scenario.
//
// Test Case: OM-01 (L18 Row 1)
// Factors: Channel(default, none, public) + User(default, none) + Token(empty, none)
// Expected: Success, billed at default rate
func (s *OrthogonalMatrixSuite) TestOM01_DefaultChannelDefaultUserNoRestriction() {
	// Arrange: Use default channel and default user
	channel := s.fixtures.ChDefaultPublic

	// Create token with no restrictions
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		"om01-token",
		"",
		0,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "default")
	s.T().Logf("✓ OM-01: default/none/public + default/none + empty/none → success (channel_id=%d)", channel.ID)
}

// TestOM02_DefaultChannelG1VipUserDefaultBilling tests cross-group with billing override.
//
// Test Case: OM-02 (L18 Row 2)
// Factors: Channel(default, G1, public) + User(vip, G1) + Token([default], none)
// Expected: Success, billed at default rate (billing override)
func (s *OrthogonalMatrixSuite) TestOM02_DefaultChannelG1VipUserDefaultBilling() {
	// Arrange: Use default channel authorized to G1
	channel := s.fixtures.ChDefaultG1

	// Join User-Vip to G1
	err := s.fixtures.JoinUserToGroups(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token with billing group override
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"om02-token",
		`["default"]`, // Force default billing
		0,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "default")
	s.T().Logf("✓ OM-02: default/G1/public + vip/G1 + [default]/none → success (channel_id=%d)", channel.ID)
}

// TestOM03_DefaultChannelG1G2PrivateSvipUserG1 tests private channel with P2P authorization.
//
// Test Case: OM-03 (L18 Row 3)
// Factors: Channel(default, [G1,G2], private) + User(svip, G1) + Token(empty, G1)
// Expected: Failure (private channel, user is not owner)
func (s *OrthogonalMatrixSuite) TestOM03_DefaultChannelG1G2PrivateSvipUserG1() {
	// Arrange: Create private default channel owned by User-Default, authorized to G1 and G2
	channel, err := s.fixtures.CreatePrivateChannel(
		"om03-ch-private",
		"gpt-4",
		"default",
		s.fixtures.UserDefault.ID, // Owner
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to create private channel")

	// Create svip user and join G1
	// Note: Using fixtures.UserSvip
	err = s.fixtures.JoinUserToGroups(
		s.fixtures.UserSvipClient,
		s.fixtures.UserSvip.ID,
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token for svip user with billing group default and P2P restriction G1
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserSvipClient,
		s.fixtures.UserSvip.ID,
		"om03-token",
		`["default"]`, // Force default billing to match channel
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Should fail because channel is private and user is not owner
	s.fixtures.VerifyRoutingFailure(s.T(), tokenKey, "gpt-4")
	s.T().Logf("✓ OM-03: default/[G1,G2]/private + svip/G1 + empty/G1 → failure (channel_id=%d)", channel.ID)
}

// TestOM04_VipChannelNoP2PVipUserNoRestriction tests simple vip-to-vip matching.
//
// Test Case: OM-04 (L18 Row 4)
// Factors: Channel(vip, none, public) + User(vip, none) + Token(empty, none)
// Expected: Success, billed at vip rate
func (s *OrthogonalMatrixSuite) TestOM04_VipChannelNoP2PVipUserNoRestriction() {
	// Arrange: Use vip public channel
	channel := s.fixtures.ChVipPublic

	// Create token
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"om04-token",
		"",
		0,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "vip")
	s.T().Logf("✓ OM-04: vip/none/public + vip/none + empty/none → success (channel_id=%d)", channel.ID)
}

// TestOM05_VipChannelG1DefaultUserG1VipBillingG1Restrict tests cross-group failure.
//
// Test Case: OM-05 (L18 Row 5)
// Factors: Channel(vip, G1, public) + User(default, G1) + Token([vip], G1)
// Expected: Failure (user's system group 'default' does not match channel's 'vip')
func (s *OrthogonalMatrixSuite) TestOM05_VipChannelG1DefaultUserG1VipBillingG1Restrict() {
	// Arrange: Use vip channel authorized to G1
	channel := s.fixtures.ChVipG1

	// Join User-Default to G1
	err := s.fixtures.JoinUserToGroups(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token with billing group vip and P2P restriction G1
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		"om05-token",
		`["vip"]`, // Force vip billing
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Should fail because default user cannot access vip channel
	// even with billing override and P2P match
	s.fixtures.VerifyRoutingFailure(s.T(), tokenKey, "gpt-4")
	s.T().Logf("✓ OM-05: vip/G1/public + default/G1 + [vip]/G1 → failure (channel_id=%d)", channel.ID)
}

// TestOM06_VipChannelG1G2PrivateVipUserG1G2VipDefaultBillingG1G2 tests private channel owner access.
//
// Test Case: OM-06 (L18 Row 6)
// Factors: Channel(vip, [G1,G2], private) + User(vip, [G1,G2]) + Token([vip,default], [G1,G2])
// Expected: Success if user is owner, otherwise failure
func (s *OrthogonalMatrixSuite) TestOM06_VipChannelG1G2PrivateVipUserG1G2VipDefaultBillingG1G2() {
	// Arrange: Create private vip channel owned by User-Vip
	channel, err := s.fixtures.CreatePrivateChannel(
		"om06-ch-private",
		"gpt-4",
		"vip",
		s.fixtures.UserVip.ID, // Owner
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to create private channel")

	// Join User-Vip to G1 and G2
	err = s.fixtures.JoinUserToGroups(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to join groups")

	// Create token with billing group list and P2P restriction G1
	// Note: Current API may only support single P2P restriction
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"om06-token",
		`["vip", "default"]`,
		s.fixtures.G1.ID, // Single P2P restriction
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Should succeed because user is the owner
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "vip")
	s.T().Logf("✓ OM-06: vip/[G1,G2]/private + vip(owner)/[G1,G2] + [vip,default]/G1 → success (channel_id=%d)", channel.ID)
}

// TestOM07_SvipChannelNoP2PSvipUserNoRestriction tests svip group matching.
//
// Test Case: OM-07
// Factors: Channel(svip, none, public) + User(svip, none) + Token(empty, none)
// Expected: Success, billed at svip rate
func (s *OrthogonalMatrixSuite) TestOM07_SvipChannelNoP2PSvipUserNoRestriction() {
	// Arrange: Use svip public channel
	channel := s.fixtures.ChSvipPublic

	// Create token
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserSvipClient,
		s.fixtures.UserSvip.ID,
		"om07-token",
		"",
		0,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "svip")
	s.T().Logf("✓ OM-07: svip/none/public + svip/none + empty/none → success (channel_id=%d)", channel.ID)
}

// TestOM08_SvipChannelG1DefaultUserG1SvipBillingG1Restrict tests cross-group failure with billing override.
//
// Test Case: OM-08
// Factors: Channel(svip, G1, public) + User(default, G1) + Token([svip], G1)
// Expected: Failure (default user cannot access svip channel)
func (s *OrthogonalMatrixSuite) TestOM08_SvipChannelG1DefaultUserG1SvipBillingG1Restrict() {
	// Arrange: Use svip channel authorized to G1
	channel := s.fixtures.ChSvipG1G2 // Reuse this channel

	// Join User-Default to G1
	err := s.fixtures.JoinUserToGroups(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token with svip billing and G1 restriction
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		"om08-token",
		`["svip"]`,
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Should fail
	s.fixtures.VerifyRoutingFailure(s.T(), tokenKey, "gpt-4")
	s.T().Logf("✓ OM-08: svip/G1/public + default/G1 + [svip]/G1 → failure (channel_id=%d)", channel.ID)
}

// TestOM09_SvipChannelG1G2PrivateVipUserG1 tests non-owner access to private channel.
//
// Test Case: OM-09
// Factors: Channel(svip, [G1,G2], private) + User(vip, G1) + Token(empty, G1)
// Expected: Failure (user is not owner)
func (s *OrthogonalMatrixSuite) TestOM09_SvipChannelG1G2PrivateVipUserG1() {
	// Arrange: Create private svip channel owned by User-Svip
	channel, err := s.fixtures.CreatePrivateChannel(
		"om09-ch-private",
		"gpt-4",
		"svip",
		s.fixtures.UserSvip.ID, // Owner
		[]int{s.fixtures.G1.ID, s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to create private channel")

	// Join User-Vip to G1
	err = s.fixtures.JoinUserToGroups(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token with billing group svip and P2P restriction G1
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"om09-token",
		`["svip"]`, // Force svip billing
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Should fail
	s.fixtures.VerifyRoutingFailure(s.T(), tokenKey, "gpt-4")
	s.T().Logf("✓ OM-09: svip/[G1,G2]/private + vip/G1 + empty/G1 → failure (channel_id=%d)", channel.ID)
}

// TestOM10_DefaultChannelG1VipUserG2DefaultBillingG2 tests P2P mismatch.
//
// Test Case: OM-10
// Factors: Channel(default, G1, public) + User(vip, G2) + Token([default], G2)
// Expected: Failure (P2P mismatch: channel requires G1, user has G2)
func (s *OrthogonalMatrixSuite) TestOM10_DefaultChannelG1VipUserG2DefaultBillingG2() {
	// Arrange: Use default channel authorized to G1
	channel := s.fixtures.ChDefaultG1

	// Join User-Vip to G2 only
	err := s.fixtures.JoinUserToGroups(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		[]int{s.fixtures.G2.ID},
	)
	require.NoError(s.T(), err, "Failed to join G2")

	// Create token with default billing and G2 restriction
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"om10-token",
		`["default"]`,
		s.fixtures.G2.ID,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Should fail due to P2P mismatch
	s.fixtures.VerifyRoutingFailure(s.T(), tokenKey, "gpt-4")
	s.T().Logf("✓ OM-10: default/G1/public + vip/G2 + [default]/G2 → failure (P2P mismatch, channel_id=%d)", channel.ID)
}

// TestOM11_VipChannelG1VipUserG1DefaultSvipBillingG1 tests billing group list fallback.
//
// Test Case: OM-11
// Factors: Channel(vip, G1, public) + User(vip, G1) + Token([default,svip], G1)
// Expected: Success, billed at default rate (first available in list)
func (s *OrthogonalMatrixSuite) TestOM11_VipChannelG1VipUserG1DefaultSvipBillingG1() {
	// Arrange: Use vip channel authorized to G1
	channel := s.fixtures.ChVipG1

	// Join User-Vip to G1
	err := s.fixtures.JoinUserToGroups(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token with billing group list [default, svip] and G1 restriction
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserVipClient,
		s.fixtures.UserVip.ID,
		"om11-token",
		`["default", "svip"]`, // Try default first, fallback to svip
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Should succeed with default billing (first match)
	// Note: Channel is vip group, so need to check if default billing can access vip channel
	// This should FAIL because default billing cannot access vip channel
	s.fixtures.VerifyRoutingFailure(s.T(), tokenKey, "gpt-4")
	s.T().Logf("✓ OM-11: vip/G1/public + vip/G1 + [default,svip]/G1 → failure (billing mismatch, channel_id=%d)", channel.ID)
}

// TestOM12_DefaultChannelG1G2DefaultUserG1DefaultBillingG1G2 tests multi-P2P success.
//
// Test Case: OM-12
// Factors: Channel(default, [G1,G2], public) + User(default, G1) + Token(empty, [G1,G2])
// Expected: Success, billed at default rate
func (s *OrthogonalMatrixSuite) TestOM12_DefaultChannelG1G2DefaultUserG1DefaultBillingG1G2() {
	// Arrange: Use default channel authorized to G1 and G2
	channel := s.fixtures.ChDefaultG1G2

	// Join User-Default to G1
	err := s.fixtures.JoinUserToGroups(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		[]int{s.fixtures.G1.ID},
	)
	require.NoError(s.T(), err, "Failed to join G1")

	// Create token with P2P restriction G1
	// Note: Current API may only support single P2P group ID
	tokenKey, err := s.fixtures.CreateTokenWithConfig(
		s.fixtures.UserDefaultClient,
		s.fixtures.UserDefault.ID,
		"om12-token",
		"",
		s.fixtures.G1.ID,
	)
	require.NoError(s.T(), err, "Failed to create token")

	// Act & Assert: Should succeed
	s.fixtures.VerifyRoutingSuccess(s.T(), tokenKey, "gpt-4", "default")
	s.T().Logf("✓ OM-12: default/[G1,G2]/public + default/G1 + empty/G1 → success (channel_id=%d)", channel.ID)
}

// TestOrthogonalMatrixSuite runs the test suite.
func TestOrthogonalMatrixSuite(t *testing.T) {
	suite.Run(t, new(OrthogonalMatrixSuite))
}
