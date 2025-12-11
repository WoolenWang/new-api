// Package testutil - Redis Inspector for Channel Statistics Testing
//
// This file provides utilities to inspect Redis state for channel statistics testing,
// including Hash, HyperLogLog, and ZSet operations.
//
// Features:
// - Check channel statistics Hash keys
// - Verify HyperLogLog unique user counts
// - Inspect dirty_channels ZSet
// - Verify TTL settings
// - Monitor flush and sync operations
package testutil

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStatsInspector provides utilities to inspect Redis state for channel statistics.
type RedisStatsInspector struct {
	client *redis.Client
	ctx    context.Context
}

// NewRedisStatsInspector creates a new Redis inspector.
func NewRedisStatsInspector(redisAddr string) (*RedisStatsInspector, error) {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctx := context.Background()

	// Test connection.
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisStatsInspector{
		client: client,
		ctx:    ctx,
	}, nil
}

// Close closes the Redis connection.
func (r *RedisStatsInspector) Close() error {
	return r.client.Close()
}

// GetChannelStatsHash retrieves the statistics Hash for a channel and model.
//
// Redis key format: channel_stats:{channel_id}:{model_name}
// Returns: map of field -> value
func (r *RedisStatsInspector) GetChannelStatsHash(channelID int, modelName string) (map[string]string, error) {
	key := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	result, err := r.client.HGetAll(r.ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get Hash %s: %w", key, err)
	}
	return result, nil
}

// GetChannelStatsField retrieves a specific field from the statistics Hash.
func (r *RedisStatsInspector) GetChannelStatsField(channelID int, modelName, field string) (string, error) {
	key := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	result, err := r.client.HGet(r.ctx, key, field).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("field %s not found in %s", field, key)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get field %s from %s: %w", field, key, err)
	}
	return result, nil
}

// GetUniqueUsersCount retrieves the unique users count from HyperLogLog.
//
// Redis key format: user_hll:{channel_id}:{model_name}:{time_window}
// Returns: count of unique users
func (r *RedisStatsInspector) GetUniqueUsersCount(channelID int, modelName, timeWindow string) (int64, error) {
	key := fmt.Sprintf("user_hll:%d:%s:%s", channelID, modelName, timeWindow)
	count, err := r.client.PFCount(r.ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get HLL count for %s: %w", key, err)
	}
	return count, nil
}

// AddUserToHLL adds a user ID to the HyperLogLog.
//
// This is used for testing HLL functionality directly.
func (r *RedisStatsInspector) AddUserToHLL(channelID int, modelName, timeWindow string, userIDs ...int) error {
	key := fmt.Sprintf("user_hll:%d:%s:%s", channelID, modelName, timeWindow)

	// Convert user IDs to strings.
	values := make([]interface{}, len(userIDs))
	for i, id := range userIDs {
		values[i] = fmt.Sprintf("%d", id)
	}

	if err := r.client.PFAdd(r.ctx, key, values...).Err(); err != nil {
		return fmt.Errorf("failed to add to HLL %s: %w", key, err)
	}
	return nil
}

// GetDirtyChannels retrieves all dirty channels from the ZSet.
//
// Redis key: dirty_channels
// Returns: map of {channel_id}:{model} -> timestamp score
func (r *RedisStatsInspector) GetDirtyChannels() (map[string]float64, error) {
	result, err := r.client.ZRangeWithScores(r.ctx, "dirty_channels", 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get dirty_channels ZSet: %w", err)
	}

	dirtyMap := make(map[string]float64)
	for _, z := range result {
		member, _ := z.Member.(string)
		dirtyMap[member] = z.Score
	}

	return dirtyMap, nil
}

// GetDirtyChannelScore retrieves the score for a specific channel:model in dirty_channels.
func (r *RedisStatsInspector) GetDirtyChannelScore(channelID int, modelName string) (float64, bool, error) {
	member := fmt.Sprintf("%d:%s", channelID, modelName)
	score, err := r.client.ZScore(r.ctx, "dirty_channels", member).Result()
	if err == redis.Nil {
		return 0, false, nil // Not in set
	}
	if err != nil {
		return 0, false, fmt.Errorf("failed to get score for %s: %w", member, err)
	}
	return score, true, nil
}

// GetTTL retrieves the TTL for a key.
func (r *RedisStatsInspector) GetTTL(key string) (time.Duration, error) {
	ttl, err := r.client.TTL(r.ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get TTL for %s: %w", key, err)
	}
	return ttl, nil
}

// GetChannelStatsTTL retrieves the TTL for a channel stats key.
func (r *RedisStatsInspector) GetChannelStatsTTL(channelID int, modelName string) (time.Duration, error) {
	key := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	return r.GetTTL(key)
}

// KeyExists checks if a key exists in Redis.
func (r *RedisStatsInspector) KeyExists(key string) (bool, error) {
	count, err := r.client.Exists(r.ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check existence of %s: %w", key, err)
	}
	return count > 0, nil
}

// DeleteKey deletes a key from Redis (for cleanup or testing).
func (r *RedisStatsInspector) DeleteKey(key string) error {
	return r.client.Del(r.ctx, key).Err()
}

// DeleteChannelStatsKeys deletes all statistics keys for a channel.
func (r *RedisStatsInspector) DeleteChannelStatsKeys(channelID int, modelName string) error {
	// Delete Hash.
	hashKey := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	if err := r.client.Del(r.ctx, hashKey).Err(); err != nil {
		return err
	}

	// Delete from dirty_channels.
	member := fmt.Sprintf("%d:%s", channelID, modelName)
	if err := r.client.ZRem(r.ctx, "dirty_channels", member).Err(); err != nil {
		return err
	}

	// Delete HLL keys (may have multiple time windows).
	pattern := fmt.Sprintf("user_hll:%d:%s:*", channelID, modelName)
	keys, err := r.client.Keys(r.ctx, pattern).Result()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := r.client.Del(r.ctx, key).Err(); err != nil {
			return err
		}
	}

	return nil
}

// WaitForRedisKey waits for a Redis key to appear.
func (r *RedisStatsInspector) WaitForRedisKey(key string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		exists, err := r.KeyExists(key)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout waiting for Redis key %s after %v", key, timeout)
}

// WaitForChannelStats waits for channel statistics to appear in Redis.
func (r *RedisStatsInspector) WaitForChannelStats(channelID int, modelName string, timeout time.Duration) error {
	key := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	return r.WaitForRedisKey(key, timeout)
}

// WaitForDirtyChannel waits for a channel to be marked dirty.
func (r *RedisStatsInspector) WaitForDirtyChannel(channelID int, modelName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	member := fmt.Sprintf("%d:%s", channelID, modelName)

	for time.Now().Before(deadline) {
		score, err := r.client.ZScore(r.ctx, "dirty_channels", member).Result()
		if err == nil && score > 0 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("timeout waiting for dirty channel %d:%s after %v", channelID, modelName, timeout)
}

// IncrementChannelStats simulates incrementing channel statistics in Redis.
//
// This is used for testing purposes to manually populate Redis with test data.
func (r *RedisStatsInspector) IncrementChannelStats(channelID int, modelName string, fields map[string]int64) error {
	key := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)

	pipe := r.client.Pipeline()
	for field, value := range fields {
		pipe.HIncrBy(r.ctx, key, field, value)
	}

	// Set TTL to 24 hours.
	pipe.Expire(r.ctx, key, 24*time.Hour)

	_, err := pipe.Exec(r.ctx)
	return err
}

// GetNextDBSyncTime retrieves the next_db_sync_time for a channel.
func (r *RedisStatsInspector) GetNextDBSyncTime(channelID int, modelName string) (int64, error) {
	key := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	result, err := r.client.HGet(r.ctx, key, "next_db_sync_time").Result()
	if err == redis.Nil {
		return 0, nil // Not set
	}
	if err != nil {
		return 0, err
	}

	var timestamp int64
	fmt.Sscanf(result, "%d", &timestamp)
	return timestamp, nil
}

// SetNextDBSyncTime sets the next_db_sync_time for testing.
func (r *RedisStatsInspector) SetNextDBSyncTime(channelID int, modelName string, timestamp int64) error {
	key := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	return r.client.HSet(r.ctx, key, "next_db_sync_time", timestamp).Err()
}

// FlushDB flushes all data from Redis (use with caution, only in tests).
func (r *RedisStatsInspector) FlushDB() error {
	return r.client.FlushDB(r.ctx).Err()
}

// GetAllKeys retrieves all keys matching a pattern.
func (r *RedisStatsInspector) GetAllKeys(pattern string) ([]string, error) {
	keys, err := r.client.Keys(r.ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get keys matching %s: %w", pattern, err)
	}
	return keys, nil
}

// CountChannelStatsKeys counts the number of channel_stats keys.
func (r *RedisStatsInspector) CountChannelStatsKeys() (int, error) {
	keys, err := r.GetAllKeys("channel_stats:*")
	if err != nil {
		return 0, err
	}
	return len(keys), nil
}

// VerifyRedisDataFlow verifies that data has flowed through Redis correctly.
//
// This checks:
// 1. Hash key exists with expected fields
// 2. Dirty channel is marked
// 3. TTL is set correctly
func (r *RedisStatsInspector) VerifyRedisDataFlow(channelID int, modelName string) error {
	// Check Hash exists.
	hashKey := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	exists, err := r.KeyExists(hashKey)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Hash key %s does not exist", hashKey)
	}

	// Check TTL is set.
	ttl, err := r.GetTTL(hashKey)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		return fmt.Errorf("TTL for %s is not set (ttl=%v)", hashKey, ttl)
	}

	// Check dirty channel marking.
	member := fmt.Sprintf("%d:%s", channelID, modelName)
	score, exists, err := r.GetDirtyChannelScore(channelID, modelName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Channel %s not in dirty_channels ZSet", member)
	}

	// Verify score is a recent timestamp (within last 2 minutes).
	now := float64(time.Now().Unix())
	if now-score > 120 {
		return fmt.Errorf("Dirty channel score %f is too old (now=%f)", score, now)
	}

	return nil
}

// GetChannelStatFieldsAsInt retrieves multiple Hash fields as integers.
func (r *RedisStatsInspector) GetChannelStatFieldsAsInt(channelID int, modelName string, fields []string) (map[string]int64, error) {
	key := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	result := make(map[string]int64)

	for _, field := range fields {
		val, err := r.client.HGet(r.ctx, key, field).Result()
		if err == redis.Nil {
			result[field] = 0
			continue
		}
		if err != nil {
			return nil, err
		}

		var intVal int64
		fmt.Sscanf(val, "%d", &intVal)
		result[field] = intVal
	}

	return result, nil
}

// SimulateL1Flush simulates the L1 to L2 flush operation.
//
// This manually writes data to Redis as the Flush Worker would.
// Used for testing L2/L3 operations without waiting for actual flush.
func (r *RedisStatsInspector) SimulateL1Flush(channelID int, modelName string, stats map[string]int64, userIDs []int) error {
	hashKey := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)
	dirtyMember := fmt.Sprintf("%d:%s", channelID, modelName)
	hllKey := fmt.Sprintf("user_hll:%d:%s:%s", channelID, modelName, getCurrentTimeWindow())

	pipe := r.client.Pipeline()

	// Write Hash fields.
	for field, value := range stats {
		pipe.HIncrBy(r.ctx, hashKey, field, value)
	}

	// Set TTL.
	pipe.Expire(r.ctx, hashKey, 24*time.Hour)

	// Add users to HLL.
	if len(userIDs) > 0 {
		hllValues := make([]interface{}, len(userIDs))
		for i, id := range userIDs {
			hllValues[i] = fmt.Sprintf("%d", id)
		}
		pipe.PFAdd(r.ctx, hllKey, hllValues...)
		pipe.Expire(r.ctx, hllKey, 30*24*time.Hour) // 30 days
	}

	// Mark as dirty.
	pipe.ZAdd(r.ctx, "dirty_channels", redis.Z{
		Score:  float64(time.Now().Unix()),
		Member: dirtyMember,
	})

	_, err := pipe.Exec(r.ctx)
	return err
}

// getCurrentTimeWindow returns current time window identifier.
func getCurrentTimeWindow() string {
	// Use 15-minute windows.
	now := time.Now()
	windowStart := now.Truncate(15 * time.Minute)
	return fmt.Sprintf("%d", windowStart.Unix())
}

// WaitForFieldValue waits for a Hash field to reach a specific value.
func (r *RedisStatsInspector) WaitForFieldValue(channelID int, modelName, field string, expectedValue int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	key := fmt.Sprintf("channel_stats:%d:%s", channelID, modelName)

	for time.Now().Before(deadline) {
		val, err := r.client.HGet(r.ctx, key, field).Result()
		if err == redis.Nil {
			time.Sleep(1 * time.Second)
			continue
		}
		if err != nil {
			return err
		}

		var intVal int64
		fmt.Sscanf(val, "%d", &intVal)
		if intVal == expectedValue {
			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("timeout waiting for %s:%s to reach %d", key, field, expectedValue)
}

// VerifyHLLDeduplication verifies HyperLogLog deduplication works correctly.
//
// Adds duplicate user IDs and verifies the count matches unique users.
func (r *RedisStatsInspector) VerifyHLLDeduplication(channelID int, modelName string, userIDs []int, expectedUniqueCount int64) error {
	timeWindow := getCurrentTimeWindow()
	hllKey := fmt.Sprintf("user_hll:%d:%s:%s", channelID, modelName, timeWindow)

	// Add user IDs (including duplicates).
	for _, userID := range userIDs {
		if err := r.client.PFAdd(r.ctx, hllKey, fmt.Sprintf("%d", userID)).Err(); err != nil {
			return fmt.Errorf("failed to add user %d to HLL: %w", userID, err)
		}
	}

	// Get count.
	count, err := r.client.PFCount(r.ctx, hllKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get HLL count: %w", err)
	}

	if count != expectedUniqueCount {
		return fmt.Errorf("HLL count mismatch: expected %d unique users, got %d", expectedUniqueCount, count)
	}

	return nil
}

// GetChannelStatsSnapshot retrieves a snapshot of all channel statistics.
//
// Returns a map of channel:model -> statistics Hash.
func (r *RedisStatsInspector) GetChannelStatsSnapshot() (map[string]map[string]string, error) {
	keys, err := r.GetAllKeys("channel_stats:*")
	if err != nil {
		return nil, err
	}

	snapshot := make(map[string]map[string]string)
	for _, key := range keys {
		hash, err := r.client.HGetAll(r.ctx, key).Result()
		if err != nil {
			return nil, err
		}
		snapshot[key] = hash
	}

	return snapshot, nil
}

// ClearAllChannelStats clears all channel statistics from Redis.
func (r *RedisStatsInspector) ClearAllChannelStats() error {
	// Delete all Hash keys.
	keys, err := r.GetAllKeys("channel_stats:*")
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		if err := r.client.Del(r.ctx, keys...).Err(); err != nil {
			return err
		}
	}

	// Delete all HLL keys.
	hllKeys, err := r.GetAllKeys("user_hll:*")
	if err != nil {
		return err
	}
	if len(hllKeys) > 0 {
		if err := r.client.Del(r.ctx, hllKeys...).Err(); err != nil {
			return err
		}
	}

	// Clear dirty_channels ZSet.
	if err := r.client.Del(r.ctx, "dirty_channels").Err(); err != nil {
		return err
	}

	return nil
}
