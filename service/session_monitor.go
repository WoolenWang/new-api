package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

// SessionInfo represents session information for monitoring
type SessionInfo struct {
	SessionId string `json:"session_id"`
	UserId    int    `json:"user_id"`
	Model     string `json:"model"`
	ChannelId int    `json:"channel_id"`
	CreatedAt int64  `json:"created_at"`
	ExpiresIn int64  `json:"expires_in_seconds"`
}

// UserSessionCount represents user session count info
type UserSessionCount struct {
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	SessionCount int64  `json:"session_count"`
}

// SessionSummary represents the summary of active sessions
type SessionSummary struct {
	TotalActiveSessions int64              `json:"total_active_sessions"`
	SessionsByChannel   map[string]int64   `json:"sessions_by_channel"`
	TopUsersBySession   []UserSessionCount `json:"top_users_by_session"`
	RecentSessions      []SessionInfo      `json:"recent_sessions"`
}

// GetSessionSummary retrieves the session monitoring summary
func GetSessionSummary(ctx context.Context, topUsersLimit int, recentSessionsLimit int) (*SessionSummary, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return getLocalSessionSummary()
	}

	summary := &SessionSummary{
		SessionsByChannel: make(map[string]int64),
		TopUsersBySession: make([]UserSessionCount, 0),
		RecentSessions:    make([]SessionInfo, 0),
	}

	// 1. Get global session count
	globalCount, err := common.RDB.Get(ctx, sessionGlobalCountKey).Int64()
	if err != nil && err != redis.Nil {
		common.SysLog(fmt.Sprintf("Failed to get global session count: %v", err))
	}
	summary.TotalActiveSessions = globalCount

	// 2. Get sessions by channel
	channelStats, err := common.RDB.HGetAll(ctx, sessionChannelStatsKey).Result()
	if err != nil && err != redis.Nil {
		common.SysLog(fmt.Sprintf("Failed to get channel session stats: %v", err))
	}
	for channelId, countStr := range channelStats {
		if count, err := strconv.ParseInt(countStr, 10, 64); err == nil && count > 0 {
			summary.SessionsByChannel[channelId] = count
		}
	}

	// 3. Get top users by session count
	summary.TopUsersBySession = getTopUsersBySessions(ctx, topUsersLimit)

	// 4. Get recent sessions
	summary.RecentSessions = getRecentSessions(ctx, recentSessionsLimit)

	return summary, nil
}

// getLocalSessionSummary returns a mock summary when Redis is not available
func getLocalSessionSummary() (*SessionSummary, error) {
	return &SessionSummary{
		TotalActiveSessions: 0,
		SessionsByChannel:   make(map[string]int64),
		TopUsersBySession:   make([]UserSessionCount, 0),
		RecentSessions:      make([]SessionInfo, 0),
	}, nil
}

// getTopUsersBySessions scans user session sets and returns top users
func getTopUsersBySessions(ctx context.Context, limit int) []UserSessionCount {
	result := make([]UserSessionCount, 0)
	if !common.RedisEnabled || common.RDB == nil {
		return result
	}

	// Scan for user session sets
	var cursor uint64
	userCounts := make(map[int]int64)

	for {
		keys, nextCursor, err := common.RDB.Scan(ctx, cursor, sessionUserKeyPrefix+"*", 100).Result()
		if err != nil {
			common.SysLog(fmt.Sprintf("Failed to scan user session keys: %v", err))
			break
		}

		for _, key := range keys {
			// Extract user_id from key: session:user:{user_id}
			parts := strings.Split(key, ":")
			if len(parts) >= 3 {
				userId, err := strconv.Atoi(parts[2])
				if err != nil {
					continue
				}
				count, err := common.RDB.SCard(ctx, key).Result()
				if err != nil {
					continue
				}
				if count > 0 {
					userCounts[userId] = count
				}
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	// Sort and get top users
	type userCount struct {
		userId int
		count  int64
	}
	var sorted []userCount
	for userId, count := range userCounts {
		sorted = append(sorted, userCount{userId, count})
	}
	// Simple bubble sort for small dataset
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[i].count {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Get user info and build result
	maxItems := limit
	if len(sorted) < maxItems {
		maxItems = len(sorted)
	}
	for i := 0; i < maxItems; i++ {
		user, err := model.GetUserById(sorted[i].userId, false)
		username := fmt.Sprintf("user_%d", sorted[i].userId)
		if err == nil && user != nil {
			username = user.Username
		}
		result = append(result, UserSessionCount{
			UserId:       sorted[i].userId,
			Username:     username,
			SessionCount: sorted[i].count,
		})
	}

	return result
}

// getRecentSessions scans session index and returns recent ones
func getRecentSessions(ctx context.Context, limit int) []SessionInfo {
	result := make([]SessionInfo, 0)
	if !common.RedisEnabled || common.RDB == nil {
		return result
	}

	// Get all sessions from index
	indexEntries, err := common.RDB.HGetAll(ctx, sessionIndexKey).Result()
	if err != nil {
		common.SysLog(fmt.Sprintf("Failed to get session index: %v", err))
		return result
	}

	var sessions []SessionInfo
	for key, entryJson := range indexEntries {
		// Parse the JSON entry
		var entry SessionIndexEntry
		if err := json.Unmarshal([]byte(entryJson), &entry); err != nil {
			continue
		}

		// Get TTL
		ttl, err := common.RDB.TTL(ctx, key).Result()
		if err != nil {
			ttl = 0
		}

		sessions = append(sessions, SessionInfo{
			SessionId: entry.SessionID,
			UserId:    entry.UserID,
			Model:     entry.Model,
			ChannelId: entry.ChannelID,
			CreatedAt: entry.CreatedAt,
			ExpiresIn: int64(ttl.Seconds()),
		})
	}

	// Sort by created_at desc
	for i := 0; i < len(sessions); i++ {
		for j := i + 1; j < len(sessions); j++ {
			if sessions[j].CreatedAt > sessions[i].CreatedAt {
				sessions[i], sessions[j] = sessions[j], sessions[i]
			}
		}
	}

	// Return top N
	if len(sessions) > limit {
		sessions = sessions[:limit]
	}
	result = sessions

	return result
}

// CleanupChannelSessions removes all session bindings for a specific channel
// This is called when a channel is disabled or deleted
func CleanupChannelSessions(ctx context.Context, channelId int) error {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}

	// Get all sessions from index
	indexEntries, err := common.RDB.HGetAll(ctx, sessionIndexKey).Result()
	if err != nil {
		common.SysLog(fmt.Sprintf("Failed to get session index for cleanup: %v", err))
		return err
	}

	deletedCount := 0
	for key, entryJson := range indexEntries {
		// Parse the JSON entry
		var entry SessionIndexEntry
		if err := json.Unmarshal([]byte(entryJson), &entry); err != nil {
			continue
		}

		// Check if this session is bound to the target channel
		if entry.ChannelID == channelId {
			// Delete session using existing function
			RemoveSessionBinding(ctx, key)
			deletedCount++
		}
	}

	// Reset channel's session count to 0
	common.RDB.HSet(ctx, sessionChannelStatsKey, strconv.Itoa(channelId), 0)

	common.SysLog(fmt.Sprintf("Cleaned up %d session bindings for channel %d", deletedCount, channelId))
	return nil
}

// InitSessionMonitor initializes the session monitoring service
// This should be called during application startup
func InitSessionMonitor() {
	// Register the session cleanup callback with the model package
	model.RegisterSessionCleanupCallback(func(channelId int) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := CleanupChannelSessions(ctx, channelId)
		if err != nil {
			common.SysLog(fmt.Sprintf("Failed to cleanup sessions for channel %d: %v", channelId, err))
		}
	})
	common.SysLog("Session monitor service initialized")
}
