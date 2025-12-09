package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"

	"github.com/go-redis/redis/v8"
)

const (
	sessionBindingKeyPattern = "session:%d:%s:%s"
	sessionUserKeyPrefix     = "session:user:"
	sessionChannelStatsKey   = "session:channel:stats"
	sessionGlobalCountKey    = "session:global:count"
	sessionIndexKey          = "session:index"
	// defaultSessionBindingTTL defines the default session binding lifetime.
	// The actual TTL can be overridden at process start via the
	// SESSION_BINDING_TTL_SECONDS environment variable, which is useful for
	// integration tests that need short-lived sessions.
	defaultSessionBindingTTL = 30 * time.Minute
	indexScanCount           = 100
)

// sessionBindingTTL holds the effective TTL used for session bindings.
// It is initialized from defaultSessionBindingTTL and can be overridden
// by the SESSION_BINDING_TTL_SECONDS environment variable.
var sessionBindingTTL = defaultSessionBindingTTL

func init() {
	if v := strings.TrimSpace(os.Getenv("SESSION_BINDING_TTL_SECONDS")); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			sessionBindingTTL = time.Duration(sec) * time.Second
		}
	}
}

type SessionIndexEntry struct {
	SessionKey string `json:"session_key"`
	SessionID  string `json:"session_id"`
	UserID     int    `json:"user_id"`
	Model      string `json:"model"`
	ChannelID  int    `json:"channel_id"`
	KeyID      int    `json:"key_id"`
	KeyHash    string `json:"key_hash"`
	Group      string `json:"group"`
	CreatedAt  int64  `json:"created_at"`
}

type memorySessionBinding struct {
	entry     *SessionIndexEntry
	expiresAt time.Time
}

type memorySessionStore struct {
	mu            sync.RWMutex
	bindings      map[string]*memorySessionBinding
	userSessions  map[int]map[string]struct{}
	channelCounts map[int]int
	globalCount   int
	ttl           time.Duration
}

var (
	memorySession *memorySessionStore
	sessionOnce   sync.Once
)

func InitSessionManager() {
	sessionOnce.Do(func() {
		if !common.RedisEnabled {
			memorySession = &memorySessionStore{
				bindings:      make(map[string]*memorySessionBinding),
				userSessions:  make(map[int]map[string]struct{}),
				channelCounts: make(map[int]int),
				ttl:           sessionBindingTTL,
			}
			go memorySession.cleanupLoop()
			return
		}

		go startSessionExpirationListener()
		go startSessionIndexReaper()
	})
}

func BuildSessionBindingKey(userID int, model, sessionID string) string {
	return fmt.Sprintf(sessionBindingKeyPattern, userID, model, sessionID)
}

func GetSessionBinding(ctx context.Context, userID int, model, sessionID string) (*SessionIndexEntry, error) {
	if sessionID == "" || userID == 0 || model == "" {
		return nil, nil
	}
	key := BuildSessionBindingKey(userID, model, sessionID)

	if !common.RedisEnabled {
		return memorySession.get(key)
	}

	entry, err := loadSessionEntry(ctx, key, userID, model, sessionID)
	if err != nil || entry == nil {
		return entry, err
	}
	_ = refreshSessionTTL(ctx, key)
	return entry, nil
}

func SaveSessionBinding(ctx context.Context, entry *SessionIndexEntry) error {
	if entry == nil || entry.ChannelID == 0 || entry.UserID == 0 || entry.SessionID == "" || entry.Model == "" {
		return errors.New("invalid session entry")
	}
	if entry.CreatedAt == 0 {
		entry.CreatedAt = time.Now().Unix()
	}
	if entry.SessionKey == "" {
		entry.SessionKey = BuildSessionBindingKey(entry.UserID, entry.Model, entry.SessionID)
	}

	if !common.RedisEnabled {
		return memorySession.set(entry)
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	pipe := common.RDB.TxPipeline()
	pipe.HSet(ctx, entry.SessionKey, map[string]interface{}{
		"channel_id": entry.ChannelID,
		"key_id":     entry.KeyID,
		"key_hash":   entry.KeyHash,
		"group":      entry.Group,
		"created_at": entry.CreatedAt,
		"user_id":    entry.UserID,
		"session_id": entry.SessionID,
		"model":      entry.Model,
	})
	pipe.Expire(ctx, entry.SessionKey, sessionBindingTTL)
	pipe.HSet(ctx, sessionIndexKey, entry.SessionKey, string(payload))
	pipe.SAdd(ctx, sessionUserKey(entry.UserID), entry.SessionID)
	pipe.HIncrBy(ctx, sessionChannelStatsKey, strconv.Itoa(entry.ChannelID), 1)
	pipe.Incr(ctx, sessionGlobalCountKey)

	_, err = pipe.Exec(ctx)
	return err
}

func RemoveSessionBinding(ctx context.Context, key string) (*SessionIndexEntry, error) {
	if key == "" {
		return nil, nil
	}

	if !common.RedisEnabled {
		return memorySession.delete(key), nil
	}

	entry, _ := getSessionIndexEntry(ctx, key)
	if entry == nil {
		entry, _ = loadSessionEntry(ctx, key, 0, "", "")
	}

	pipe := common.RDB.TxPipeline()
	pipe.Del(ctx, key)
	pipe.HDel(ctx, sessionIndexKey, key)
	if entry != nil {
		pipe.SRem(ctx, sessionUserKey(entry.UserID), entry.SessionID)
		pipe.HIncrBy(ctx, sessionChannelStatsKey, strconv.Itoa(entry.ChannelID), -1)
		pipe.Decr(ctx, sessionGlobalCountKey)
	}
	_, err := pipe.Exec(ctx)
	return entry, err
}

func GetUserSessionCount(ctx context.Context, userID int) (int64, error) {
	if userID == 0 {
		return 0, nil
	}
	if !common.RedisEnabled {
		return memorySession.userSessionCount(userID), nil
	}
	return common.RDB.SCard(ctx, sessionUserKey(userID)).Result()
}

func GetEffectiveSessionLimit(userLimit int, group string) int {
	if userLimit > 0 {
		return userLimit
	}
	if groupLimit := setting.GetGroupMaxConcurrentSessions(group); groupLimit > 0 {
		return groupLimit
	}
	if setting.SystemMaxConcurrentSessions > 0 {
		return setting.SystemMaxConcurrentSessions
	}
	return 0
}

func refreshSessionTTL(ctx context.Context, key string) error {
	if !common.RedisEnabled {
		return memorySession.refresh(key)
	}
	return common.RDB.Expire(ctx, key, sessionBindingTTL).Err()
}

func loadSessionEntry(ctx context.Context, key string, userID int, model string, sessionID string) (*SessionIndexEntry, error) {
	result, err := common.RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	entry := parseSessionEntry(result, key, userID, model, sessionID)
	return entry, nil
}

func parseSessionEntry(data map[string]string, key string, userID int, model string, sessionID string) *SessionIndexEntry {
	entry := &SessionIndexEntry{
		SessionKey: key,
		SessionID:  sessionID,
		UserID:     userID,
		Model:      model,
	}
	if v, ok := data["session_id"]; ok && v != "" {
		entry.SessionID = v
	}
	if v, ok := data["model"]; ok && v != "" {
		entry.Model = v
	}
	if v, ok := data["user_id"]; ok && v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			entry.UserID = parsed
		}
	}
	if v, ok := data["channel_id"]; ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			entry.ChannelID = parsed
		}
	}
	if v, ok := data["key_id"]; ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			entry.KeyID = parsed
		}
	}
	if v, ok := data["key_hash"]; ok {
		entry.KeyHash = v
	}
	if v, ok := data["group"]; ok {
		entry.Group = v
	}
	if v, ok := data["created_at"]; ok {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			entry.CreatedAt = parsed
		}
	}
	return entry
}

func sessionUserKey(userID int) string {
	return fmt.Sprintf("%s%d", sessionUserKeyPrefix, userID)
}

func getSessionIndexEntry(ctx context.Context, key string) (*SessionIndexEntry, error) {
	val, err := common.RDB.HGet(ctx, sessionIndexKey, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entry := &SessionIndexEntry{}
	if err := json.Unmarshal([]byte(val), entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func startSessionExpirationListener() {
	ctx := context.Background()
	config, err := common.RDB.ConfigGet(ctx, "notify-keyspace-events").Result()
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("failed to read redis notify-keyspace-events config: %v", err))
	} else if len(config) >= 2 {
		if v, ok := config[1].(string); ok && !strings.Contains(v, "E") {
			logger.LogWarn(ctx, "redis notify-keyspace-events missing Expired events, session counters may lag until reaper catches up")
		}
	}

	pattern := fmt.Sprintf("__keyevent@%d__:expired", common.RDB.Options().DB)
	pubsub := common.RDB.PSubscribe(ctx, pattern)

	go func() {
		for msg := range pubsub.Channel() {
			if !strings.HasPrefix(msg.Payload, "session:") {
				continue
			}
			if err := handleSessionExpiration(ctx, msg.Payload); err != nil && common.DebugEnabled {
				logger.LogWarn(ctx, fmt.Sprintf("failed to handle session expiration for %s: %v", msg.Payload, err))
			}
		}
	}()
}

func handleSessionExpiration(ctx context.Context, key string) error {
	entry, err := getSessionIndexEntry(ctx, key)
	if err != nil || entry == nil {
		return err
	}

	pipe := common.RDB.TxPipeline()
	pipe.HDel(ctx, sessionIndexKey, key)
	pipe.SRem(ctx, sessionUserKey(entry.UserID), entry.SessionID)
	pipe.HIncrBy(ctx, sessionChannelStatsKey, strconv.Itoa(entry.ChannelID), -1)
	pipe.Decr(ctx, sessionGlobalCountKey)
	_, err = pipe.Exec(ctx)
	return err
}

func startSessionIndexReaper() {
	ctx := context.Background()
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		cursor := uint64(0)
		for {
			results, nextCursor, err := common.RDB.HScan(ctx, sessionIndexKey, cursor, "", indexScanCount).Result()
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("session index reaper scan failed: %v", err))
				break
			}
			for i := 0; i+1 < len(results); i += 2 {
				field := results[i]
				val := results[i+1]
				entry := &SessionIndexEntry{}
				if err := json.Unmarshal([]byte(val), entry); err != nil {
					common.SysLog(fmt.Sprintf("failed to unmarshal session index entry for %s: %v", field, err))
					continue
				}
				exists, err := common.RDB.Exists(ctx, entry.SessionKey).Result()
				if err != nil {
					continue
				}
				if exists == 0 {
					_ = handleSessionExpiration(ctx, entry.SessionKey)
				}
			}
			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
	}
}

func (store *memorySessionStore) get(key string) (*SessionIndexEntry, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	binding, ok := store.bindings[key]
	if !ok {
		return nil, nil
	}
	if time.Now().After(binding.expiresAt) {
		return nil, nil
	}
	binding.expiresAt = time.Now().Add(store.ttl)
	return binding.entry, nil
}

func (store *memorySessionStore) set(entry *SessionIndexEntry) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if entry.SessionKey == "" {
		entry.SessionKey = BuildSessionBindingKey(entry.UserID, entry.Model, entry.SessionID)
	}
	store.bindings[entry.SessionKey] = &memorySessionBinding{
		entry:     entry,
		expiresAt: time.Now().Add(store.ttl),
	}
	if _, ok := store.userSessions[entry.UserID]; !ok {
		store.userSessions[entry.UserID] = make(map[string]struct{})
	}
	if _, exists := store.userSessions[entry.UserID][entry.SessionID]; !exists {
		store.userSessions[entry.UserID][entry.SessionID] = struct{}{}
		store.channelCounts[entry.ChannelID]++
		store.globalCount++
	}
	return nil
}

func (store *memorySessionStore) delete(key string) *SessionIndexEntry {
	store.mu.Lock()
	defer store.mu.Unlock()
	binding, ok := store.bindings[key]
	if !ok {
		return nil
	}
	delete(store.bindings, key)
	entry := binding.entry
	if sessions, ok := store.userSessions[entry.UserID]; ok {
		delete(sessions, entry.SessionID)
	}
	if count, ok := store.channelCounts[entry.ChannelID]; ok && count > 0 {
		store.channelCounts[entry.ChannelID] = count - 1
	}
	if store.globalCount > 0 {
		store.globalCount--
	}
	return entry
}

func (store *memorySessionStore) refresh(key string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if binding, ok := store.bindings[key]; ok {
		binding.expiresAt = time.Now().Add(store.ttl)
	}
	return nil
}

func (store *memorySessionStore) userSessionCount(userID int) int64 {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return int64(len(store.userSessions[userID]))
}

func (store *memorySessionStore) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		now := time.Now()
		store.mu.Lock()
		for key, binding := range store.bindings {
			if now.After(binding.expiresAt) {
				delete(store.bindings, key)
				entry := binding.entry
				if sessions, ok := store.userSessions[entry.UserID]; ok {
					delete(sessions, entry.SessionID)
				}
				if count, ok := store.channelCounts[entry.ChannelID]; ok && count > 0 {
					store.channelCounts[entry.ChannelID] = count - 1
				}
				if store.globalCount > 0 {
					store.globalCount--
				}
			}
		}
		store.mu.Unlock()
	}
}
