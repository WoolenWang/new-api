// Package database_api contains integration tests for database schema completeness.
//
// Test Focus:
// ===========
// This package validates that all new and extended database tables can correctly
// perform CRUD operations, verify constraints, and handle complex queries.
//
// Test Sections:
// - 2.4: Database Schema Completeness Tests (DB-01 to DB-18)
//
// Key Test Scenarios:
// - DB-01 to DB-03: channel_statistics table operations
// - DB-04 to DB-05: channels table extended fields
// - DB-06 to DB-08: group_statistics table operations
// - DB-09 to DB-12: monitor_policies table operations
// - DB-13 to DB-15: model_baselines table operations
// - DB-16 to DB-18: model_monitoring_results table operations
//
// Test Data:
// All tests use isolated in-memory SQLite database to ensure test independence.
package database_api

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DatabaseSchemaSuite holds shared test resources for database schema tests.
type DatabaseSchemaSuite struct {
	Server   *testutil.TestServer
	Client   *testutil.APIClient
	DB       *model.DBWrapper // Direct DB access for low-level operations
	Fixtures *testutil.TestFixtures
}

// SetupDatabaseSchemaSuite initializes the test suite with a running server and database access.
func SetupDatabaseSchemaSuite(t *testing.T) (*DatabaseSchemaSuite, func()) {
	t.Helper()

	projectRoot, err := testutil.FindProjectRoot()
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

	// Get direct database access
	db := model.DB

	suite := &DatabaseSchemaSuite{
		Server:   server,
		Client:   client,
		DB:       db,
		Fixtures: testutil.NewTestFixtures(t, client),
	}

	cleanup := func() {
		suite.Fixtures.Cleanup()
		if err := server.Stop(); err != nil {
			t.Errorf("Failed to stop server: %v", err)
		}
	}

	return suite, cleanup
}

// Helper function to create a test channel for database tests
func (s *DatabaseSchemaSuite) createTestChannel(t *testing.T, name, modelName, group string) *testutil.ChannelModel {
	t.Helper()

	channel := &testutil.ChannelModel{
		Name:   name,
		Type:   1, // OpenAI type
		Key:    fmt.Sprintf("sk-test-%d", time.Now().UnixNano()),
		Status: 1, // Enabled
		Models: modelName,
		Group:  group,
	}

	id, err := s.Client.CreateChannel(channel)
	require.NoError(t, err, "Failed to create test channel")
	channel.Id = id

	return channel
}

// Helper function to create a test P2P group for database tests
func (s *DatabaseSchemaSuite) createTestP2PGroup(t *testing.T, name string, ownerID int) *testutil.P2PGroupModel {
	t.Helper()

	group := &testutil.P2PGroupModel{
		Name:        name,
		DisplayName: fmt.Sprintf("Test Group %s", name),
		OwnerID:     ownerID,
		Type:        2, // Shared
		JoinMethod:  0, // Invite
		Status:      1, // Active
	}

	id, err := s.Client.CreateP2PGroup(group)
	require.NoError(t, err, "Failed to create test P2P group")
	group.Id = id

	return group
}

// Helper function to get current timestamp in seconds
func nowTimestamp() int64 {
	return time.Now().Unix()
}

// Helper function to round time window start to 15-minute intervals
func roundToTimeWindow(t time.Time) int64 {
	// Round down to nearest 15-minute interval
	minutes := t.Minute()
	roundedMinutes := (minutes / 15) * 15
	rounded := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), roundedMinutes, 0, 0, t.Location())
	return rounded.Unix()
}
