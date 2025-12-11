// Package testutil provides utilities for integration testing of the NewAPI service.
package testutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// CacheInspector provides methods to inspect cache states across L1/L2/L3 layers.
// L1: In-memory cache (User.ExtendedGroups)
// L2: Redis cache (user_groups:{user_id})
// L3: Database (user_groups table)
type CacheInspector struct {
	t      *testing.T
	client *APIClient
	redis  *redis.Client
}

// NewCacheInspector creates a new cache inspector.
func NewCacheInspector(t *testing.T, client *APIClient) *CacheInspector {
	t.Helper()

	var redisClient *redis.Client

	conn := os.Getenv("REDIS_CONN_STRING")
	if conn == "" {
		t.Log("CacheInspector: REDIS_CONN_STRING not set; L2 cache inspection will be disabled")
	} else {
		opt, err := redis.ParseURL(conn)
		if err != nil {
			t.Fatalf("CacheInspector: failed to parse REDIS_CONN_STRING: %v", err)
		}

		redisClient = redis.NewClient(opt)

		// Optional quick ping to fail fast if Redis is unreachable.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := redisClient.Ping(ctx).Err(); err != nil {
			t.Fatalf("CacheInspector: failed to connect to Redis at %s: %v", opt.Addr, err)
		}
	}

	return &CacheInspector{
		t:      t,
		client: client,
		redis:  redisClient,
	}
}

// openDB opens a read/write connection to the same SQLite database file
// used by the test server. It relies on the TestServer.DataDir exposed
// via APIClient.Server.
func (ci *CacheInspector) openDB() *gorm.DB {
	ci.t.Helper()
	if ci.client == nil || ci.client.Server == nil {
		ci.t.Fatalf("cache inspector: APIClient.Server is nil, cannot locate SQLite DB")
	}

	dbFile := filepath.Join(ci.client.Server.DataDir, "one-api.db")
	db, err := gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	if err != nil {
		ci.t.Fatalf("cache inspector: failed to open sqlite db at %s: %v", dbFile, err)
	}
	return db
}

// InspectL2Cache checks the Redis cache for user's P2P groups.
// Returns: (groupIDs, exists, error)
func (ci *CacheInspector) InspectL2Cache(userID int) ([]int, bool, error) {
	ci.t.Helper()

	if ci.redis == nil {
		return nil, false, fmt.Errorf("redis client not configured for CacheInspector (REDIS_CONN_STRING not set)")
	}

	key := fmt.Sprintf("user_groups:%d", userID)
	ctx := context.Background()

	val, err := ci.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, false, nil // Key does not exist
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to get Redis key %s: %w", key, err)
	}

	var groups []int
	if err := json.Unmarshal([]byte(val), &groups); err != nil {
		return nil, true, fmt.Errorf("failed to unmarshal groups from Redis: %w", err)
	}

	return groups, true, nil
}

// InspectL3DB checks the database for user's active P2P group memberships.
// Returns: groupIDs of all active (status=1) memberships
func (ci *CacheInspector) InspectL3DB(userID int) ([]int, error) {
	ci.t.Helper()

	db := ci.openDB()

	// Query user_groups table directly
	var memberships []struct {
		GroupID int `gorm:"column:group_id"`
	}

	err := db.Table("user_groups").
		Select("group_id").
		Where("user_id = ? AND status = ?", userID, 1). // status=1 means Active
		Find(&memberships).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query user_groups table: %w", err)
	}

	groupIDs := make([]int, len(memberships))
	for i, m := range memberships {
		groupIDs[i] = m.GroupID
	}

	return groupIDs, nil
}

// VerifyL2L3Consistency checks if L2 Redis cache matches L3 DB.
func (ci *CacheInspector) VerifyL2L3Consistency(userID int) error {
	ci.t.Helper()

	l2Groups, l2Exists, err := ci.InspectL2Cache(userID)
	if err != nil {
		return err
	}

	l3Groups, err := ci.InspectL3DB(userID)
	if err != nil {
		return err
	}

	if !l2Exists {
		// If L2 doesn't exist, that's okay (cache miss)
		ci.t.Logf("L2 cache miss for user %d (expected for cold start)", userID)
		return nil
	}

	// Compare L2 and L3
	if !slicesEqualUnordered(l2Groups, l3Groups) {
		return fmt.Errorf("L2 Redis cache mismatch with L3 DB for user %d: L2=%v, L3=%v",
			userID, l2Groups, l3Groups)
	}

	return nil
}

// InvalidateL2Cache deletes the Redis cache entry for a user.
func (ci *CacheInspector) InvalidateL2Cache(userID int) error {
	ci.t.Helper()

	if ci.redis == nil {
		// In test environments without Redis configured, gracefully skip L2
		// invalidation so that tests can still verify L3/logic behavior.
		ci.t.Logf("CacheInspector: Redis not configured, skipping L2 invalidation for user %d", userID)
		return nil
	}

	key := fmt.Sprintf("user_groups:%d", userID)
	ctx := context.Background()

	err := ci.redis.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete Redis key %s: %w", key, err)
	}

	ci.t.Logf("Invalidated L2 cache for user %d", userID)
	return nil
}

// WaitForCacheSync waits for L2 cache to sync with L3 DB.
// This is useful after operations that should trigger cache invalidation.
// Timeout: maximum wait time (default 5 seconds)
func (ci *CacheInspector) WaitForCacheSync(userID int, timeout time.Duration) error {
	ci.t.Helper()

	if timeout == 0 {
		timeout = 5 * time.Second
	}

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		l3Groups, err := ci.InspectL3DB(userID)
		if err != nil {
			return err
		}

		l2Groups, l2Exists, err := ci.InspectL2Cache(userID)
		if err != nil {
			return err
		}

		// If L2 doesn't exist yet, wait for it to be populated
		if !l2Exists {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Check if L2 matches L3
		if slicesEqualUnordered(l2Groups, l3Groups) {
			ci.t.Logf("Cache sync verified for user %d: groups=%v", userID, l3Groups)
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("cache sync timeout for user %d after %v", userID, timeout)
}

// AssertCacheInvalidated asserts that the L2 Redis cache does not exist for a user.
func (ci *CacheInspector) AssertCacheInvalidated(userID int) {
	ci.t.Helper()

	_, exists, err := ci.InspectL2Cache(userID)
	assert.NoError(ci.t, err, "Failed to check L2 cache")
	assert.False(ci.t, exists, "Expected L2 cache to be invalidated for user %d", userID)
}

// AssertCacheContains asserts that the L2 Redis cache contains a specific group.
func (ci *CacheInspector) AssertCacheContains(userID, groupID int) {
	ci.t.Helper()

	groups, exists, err := ci.InspectL2Cache(userID)
	assert.NoError(ci.t, err, "Failed to check L2 cache")
	assert.True(ci.t, exists, "Expected L2 cache to exist for user %d", userID)
	assert.Contains(ci.t, groups, groupID, "Expected L2 cache to contain group %d for user %d", groupID, userID)
}

// AssertCacheNotContains asserts that the L2 Redis cache does not contain a specific group.
func (ci *CacheInspector) AssertCacheNotContains(userID, groupID int) {
	ci.t.Helper()

	groups, exists, err := ci.InspectL2Cache(userID)
	assert.NoError(ci.t, err, "Failed to check L2 cache")

	if !exists {
		// Cache doesn't exist, so it definitely doesn't contain the group
		return
	}

	assert.NotContains(ci.t, groups, groupID, "Expected L2 cache to not contain group %d for user %d", groupID, userID)
}

// AssertDBContains asserts that the database contains an active membership.
func (ci *CacheInspector) AssertDBContains(userID, groupID int) {
	ci.t.Helper()

	groups, err := ci.InspectL3DB(userID)
	assert.NoError(ci.t, err, "Failed to check L3 DB")
	assert.Contains(ci.t, groups, groupID, "Expected DB to contain active membership: user=%d, group=%d", userID, groupID)
}

// AssertDBNotContains asserts that the database does not contain an active membership.
func (ci *CacheInspector) AssertDBNotContains(userID, groupID int) {
	ci.t.Helper()

	groups, err := ci.InspectL3DB(userID)
	assert.NoError(ci.t, err, "Failed to check L3 DB")
	assert.NotContains(ci.t, groups, groupID, "Expected DB to not contain active membership: user=%d, group=%d", userID, groupID)
}

// GetMemberStatus returns the current status of a user in a P2P group.
// Status: 0=Pending, 1=Active, 2=Rejected, 3=Banned, 4=Left
// Returns: (status, exists, error)
func (ci *CacheInspector) GetMemberStatus(userID, groupID int) (int, bool, error) {
	ci.t.Helper()

	db := ci.openDB()

	var result struct {
		Status int `gorm:"column:status"`
	}

	err := db.Table("user_groups").
		Select("status").
		Where("user_id = ? AND group_id = ?", userID, groupID).
		First(&result).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to query user_groups status: %w", err)
	}

	return result.Status, true, nil
}

// AssertMemberStatus asserts that a user has a specific status in a group.
func (ci *CacheInspector) AssertMemberStatus(userID, groupID, expectedStatus int) {
	ci.t.Helper()

	status, exists, err := ci.GetMemberStatus(userID, groupID)
	assert.NoError(ci.t, err, "Failed to get member status")
	assert.True(ci.t, exists, "Expected membership record to exist: user=%d, group=%d", userID, groupID)
	assert.Equal(ci.t, expectedStatus, status, "Expected status=%d for user=%d in group=%d", expectedStatus, userID, groupID)
}

// Cleanup closes the Redis connection.
func (ci *CacheInspector) Cleanup() {
	if ci.redis != nil {
		ci.redis.Close()
	}
}

// slicesEqualUnordered checks if two int slices contain the same elements (order doesn't matter).
func slicesEqualUnordered(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}

	countMap := make(map[int]int)
	for _, v := range a {
		countMap[v]++
	}
	for _, v := range b {
		countMap[v]--
		if countMap[v] < 0 {
			return false
		}
	}

	for _, count := range countMap {
		if count != 0 {
			return false
		}
	}

	return true
}
