package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

// ChannelStatsL2 L2层Redis统计缓存服务
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: 3.2.2 L2: Redis 缓存 (Cache & Buffer)
//
// 架构特点:
// - 使用Redis HASH存储统计计数
// - 使用Redis HyperLogLog进行用户去重
// - 使用Redis ZSET标记脏数据
// - 每分钟从L1批量刷新到Redis

// ChannelStatsL2Service L2层Redis统计服务
type ChannelStatsL2Service struct {
	l1Service *ChannelStatsL1Service

	// 配置
	flushInterval time.Duration // L1->L2刷新间隔
	ttl           time.Duration // Redis键TTL

	// 后台任务控制
	stopChan chan struct{}
	wg       sync.WaitGroup
}

var (
	channelStatsL2Service *ChannelStatsL2Service
	channelStatsL2Once    sync.Once
)

// GetChannelStatsL2Service 获取L2统计服务单例
func GetChannelStatsL2Service() *ChannelStatsL2Service {
	channelStatsL2Once.Do(func() {
		flushInterval := getChannelStatsFlushInterval()

		channelStatsL2Service = &ChannelStatsL2Service{
			l1Service:     GetChannelStatsL1Service(),
			flushInterval: flushInterval,  // 默认1分钟，可通过ENV缩短
			ttl:           24 * time.Hour, // Redis键24小时过期
			stopChan:      make(chan struct{}),
		}

		// 仅在Redis启用时启动L2服务
		if common.RedisEnabled {
			channelStatsL2Service.wg.Add(1)
			go channelStatsL2Service.flushLoop()
			common.SysLog("ChannelStatsL2Service initialized (Redis enabled)")
		} else {
			common.SysLog("ChannelStatsL2Service skipped (Redis disabled)")
		}
	})
	return channelStatsL2Service
}

// Redis键名生成
const (
	// HASH键: channel_stats:{channel_id}:{model_name}:{window}
	// Phase 8.x: 添加窗口时间戳避免累积重复计数
	redisKeyStatsHashPrefix = "channel_stats:"

	// HyperLogLog键: user_hll:{channel_id}:{model_name}:{window}
	redisKeyUserHLLPrefix = "user_hll:"

	// ZSET键: dirty_channels (member=channel:model:window, score=timestamp)
	redisKeyDirtyChannels = "dirty_channels"
)

// 统计窗口大小（秒），默认15分钟。
// 可通过 CHANNEL_STATS_WINDOW_SECONDS 在测试环境中缩短到秒级。
var statsWindowSeconds int64 = getChannelStatsWindowSeconds()

// alignToWindow 将时间戳对齐到统计窗口边界
// Phase 8.x Task 2.2: Window alignment to prevent drift
func AlignToWindow(timestamp int64) int64 {
	return (timestamp / statsWindowSeconds) * statsWindowSeconds
}

// getStatsHashKey 生成统计HASH键（带窗口时间戳）
// Phase 8.x Task 2.1: Add window context to Redis keys
func getStatsHashKey(channelID int, modelName string, windowStart int64) string {
	return fmt.Sprintf("%s%d:%s:%d", redisKeyStatsHashPrefix, channelID, modelName, windowStart)
}

// getUserHLLKey 生成用户HLL键
func getUserHLLKey(channelID int, modelName string, timeWindow int64) string {
	return fmt.Sprintf("%s%d:%s:%d", redisKeyUserHLLPrefix, channelID, modelName, timeWindow)
}

// getDirtyChannelMember 生成脏数据ZSet的member（带窗口）
func GetDirtyChannelMember(channelID int, modelName string, windowStart int64) string {
	return fmt.Sprintf("%d:%s:%d", channelID, modelName, windowStart)
}

// flushLoop L1->L2定时刷新循环
func (s *ChannelStatsL2Service) flushLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.flushL1ToRedis(); err != nil {
				common.SysLog(fmt.Sprintf("L1->L2 flush error: %v", err))
			}
		case <-s.stopChan:
			common.SysLog("ChannelStatsL2Service flush loop stopped")
			return
		}
	}
}

// flushL1ToRedis 将L1快照刷新到Redis
func (s *ChannelStatsL2Service) flushL1ToRedis() error {
	if !common.RedisEnabled {
		return nil
	}

	// 1. 获取L1快照（原子重置）
	snapshot := s.l1Service.GetSnapshot()
	if len(snapshot) == 0 {
		// 没有数据需要刷新
		return nil
	}

	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf("Flushing %d channel stats from L1 to Redis", len(snapshot)))
	}

	// 2. 使用Pipeline批量写入Redis
	ctx := context.Background()
	pipe := common.RDB.Pipeline()

	now := time.Now().Unix()
	currentWindow := AlignToWindow(now) // Phase 8.x Task 2.2: 对齐到窗口边界

	for _, snap := range snapshot {
		if snap.RequestCount == 0 {
			continue // 跳过空快照
		}

		// Phase 8.x Task 2.1: Redis键包含窗口时间戳
		hashKey := getStatsHashKey(snap.ChannelID, snap.ModelName, currentWindow)

		// 3. 使用HINCRBY累加计数到Redis HASH
		pipe.HIncrBy(ctx, hashKey, "request_count", snap.RequestCount)
		pipe.HIncrBy(ctx, hashKey, "fail_count", snap.FailCount)
		pipe.HIncrBy(ctx, hashKey, "total_tokens", snap.TotalTokens)
		pipe.HIncrBy(ctx, hashKey, "prompt_tokens", snap.PromptTokens)
		pipe.HIncrBy(ctx, hashKey, "completion_tokens", snap.CompletionTokens)
		pipe.HIncrBy(ctx, hashKey, "total_quota", snap.TotalQuota)
		pipe.HIncrBy(ctx, hashKey, "total_latency_ms", snap.TotalLatencyMs)
		pipe.HIncrBy(ctx, hashKey, "stream_req_count", snap.StreamReqCount)
		pipe.HIncrBy(ctx, hashKey, "cache_hit_count", snap.CacheHitCount)
		pipe.HIncrBy(ctx, hashKey, "session_count", snap.SessionCount)

		// 设置TTL（窗口结束后24小时）
		windowTTL := time.Duration(statsWindowSeconds+86400) * time.Second
		pipe.Expire(ctx, hashKey, windowTTL)

		// 4. Phase 8.x Task 3.1: 使用HyperLogLog进行用户去重
		// 为当前窗口的HLL键添加用户ID
		if len(snap.UserIDs) > 0 {
			hllKey := getUserHLLKey(snap.ChannelID, snap.ModelName, currentWindow)
			userIDStrings := make([]interface{}, len(snap.UserIDs))
			for i, uid := range snap.UserIDs {
				userIDStrings[i] = uid
			}
			pipe.PFAdd(ctx, hllKey, userIDStrings...)
			pipe.Expire(ctx, hllKey, 30*24*time.Hour) // HLL保留30天
		}

		// 5. 标记为脏数据（用于L3同步）
		dirtyMember := GetDirtyChannelMember(snap.ChannelID, snap.ModelName, currentWindow)
		pipe.ZAdd(ctx, redisKeyDirtyChannels, &redis.Z{
			Score:  float64(now),
			Member: dirtyMember,
		})
	}

	// 6. 执行Pipeline
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to flush L1 to Redis: %w", err)
	}

	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf("Successfully flushed %d snapshots to Redis (window=%d)", len(snapshot), currentWindow))
	}

	return nil
}

// GetStatsFromRedis 从Redis读取统计数据（指定窗口）
// Phase 8.x Task 2.1: Add window parameter
func (s *ChannelStatsL2Service) GetStatsFromRedis(channelID int, modelName string, windowStart int64) (*ChannelStatsSnapshot, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("Redis is not enabled")
	}

	ctx := context.Background()
	hashKey := getStatsHashKey(channelID, modelName, windowStart)

	// 使用HGETALL获取所有字段
	result, err := common.RDB.HGetAll(ctx, hashKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats from Redis: %w", err)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no stats found in Redis for channel %d model %s window %d", channelID, modelName, windowStart)
	}

	// 解析结果
	snap := &ChannelStatsSnapshot{
		ChannelID:    channelID,
		ModelName:    modelName,
		SnapshotTime: windowStart,
	}

	// 辅助函数：安全地解析int64
	parseInt64 := func(s string) int64 {
		var v int64
		fmt.Sscanf(s, "%d", &v)
		return v
	}

	if v, ok := result["request_count"]; ok {
		snap.RequestCount = parseInt64(v)
	}
	if v, ok := result["fail_count"]; ok {
		snap.FailCount = parseInt64(v)
	}
	if v, ok := result["total_tokens"]; ok {
		snap.TotalTokens = parseInt64(v)
	}
	if v, ok := result["prompt_tokens"]; ok {
		snap.PromptTokens = parseInt64(v)
	}
	if v, ok := result["completion_tokens"]; ok {
		snap.CompletionTokens = parseInt64(v)
	}
	if v, ok := result["total_quota"]; ok {
		snap.TotalQuota = parseInt64(v)
	}
	if v, ok := result["total_latency_ms"]; ok {
		snap.TotalLatencyMs = parseInt64(v)
	}
	if v, ok := result["stream_req_count"]; ok {
		snap.StreamReqCount = parseInt64(v)
	}
	if v, ok := result["cache_hit_count"]; ok {
		snap.CacheHitCount = parseInt64(v)
	}
	if v, ok := result["session_count"]; ok {
		snap.SessionCount = parseInt64(v)
	}

	// Phase 8.x Task 3.1: 从HyperLogLog获取精确的去重用户数
	hllKey := getUserHLLKey(channelID, modelName, windowStart)
	uniqueUsers, err := common.RDB.PFCount(ctx, hllKey).Result()
	if err == nil {
		snap.UniqueUsers = int(uniqueUsers)
	} else {
		// HLL不存在时使用0
		snap.UniqueUsers = 0
	}

	return snap, nil
}

// GetDirtyChannels 获取脏渠道列表（用于L3同步）
func (s *ChannelStatsL2Service) GetDirtyChannels(limit int64) ([]string, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("Redis is not enabled")
	}

	ctx := context.Background()

	// 从ZSET中获取最老的N个脏渠道
	result, err := common.RDB.ZRangeWithScores(ctx, redisKeyDirtyChannels, 0, limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get dirty channels: %w", err)
	}

	channels := make([]string, len(result))
	for i, z := range result {
		channels[i] = z.Member.(string)
	}

	return channels, nil
}

// RemoveDirtyChannel 从脏集合中移除渠道（L3同步完成后调用）
func (s *ChannelStatsL2Service) RemoveDirtyChannel(channelKey string) error {
	if !common.RedisEnabled {
		return nil
	}

	ctx := context.Background()
	return common.RDB.ZRem(ctx, redisKeyDirtyChannels, channelKey).Err()
}

// ClearStatsInRedis 清空Redis中的统计数据（Phase 8.x Task 2.3: 用于窗口同步后清理）
func (s *ChannelStatsL2Service) ClearStatsInRedis(channelID int, modelName string, windowStart int64) error {
	if !common.RedisEnabled {
		return nil
	}

	ctx := context.Background()
	pipe := common.RDB.Pipeline()

	// 删除HASH键
	hashKey := getStatsHashKey(channelID, modelName, windowStart)
	pipe.Del(ctx, hashKey)

	// 删除HLL键
	hllKey := getUserHLLKey(channelID, modelName, windowStart)
	pipe.Del(ctx, hllKey)

	_, err := pipe.Exec(ctx)
	return err
}

// Stop 停止L2统计服务
func (s *ChannelStatsL2Service) Stop() {
	if common.RedisEnabled {
		close(s.stopChan)
		s.wg.Wait()
		common.SysLog("ChannelStatsL2Service stopped")
	}
}

// GetAggregatedStats 从Redis聚合多个渠道的统计（用于分组统计）
// Phase 8.x: 需要指定窗口范围进行聚合
func (s *ChannelStatsL2Service) GetAggregatedStats(channelIDs []int, modelName string, startTime, endTime int64) (*ChannelStatsSnapshot, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("Redis is not enabled")
	}

	aggregated := &ChannelStatsSnapshot{
		ModelName:    modelName,
		SnapshotTime: time.Now().Unix(),
	}

	// 计算需要查询的窗口列表
	windows := s.getWindowsInRange(startTime, endTime)

	for _, channelID := range channelIDs {
		for _, window := range windows {
			snap, err := s.GetStatsFromRedis(channelID, modelName, window)
			if err != nil {
				// 跳过不存在的键
				continue
			}

			// 累加统计
			aggregated.RequestCount += snap.RequestCount
			aggregated.FailCount += snap.FailCount
			aggregated.TotalTokens += snap.TotalTokens
			aggregated.PromptTokens += snap.PromptTokens
			aggregated.CompletionTokens += snap.CompletionTokens
			aggregated.TotalQuota += snap.TotalQuota
			aggregated.TotalLatencyMs += snap.TotalLatencyMs
			aggregated.StreamReqCount += snap.StreamReqCount
			aggregated.CacheHitCount += snap.CacheHitCount
			aggregated.SessionCount += snap.SessionCount
			aggregated.UniqueUsers += snap.UniqueUsers // 注意：跨窗口会高估，应使用HLL合并
		}
	}

	return aggregated, nil
}

// getWindowsInRange 获取时间范围内的所有窗口边界
func (s *ChannelStatsL2Service) getWindowsInRange(startTime, endTime int64) []int64 {
	windows := []int64{}
	currentWindow := AlignToWindow(startTime)

	for currentWindow <= endTime {
		windows = append(windows, currentWindow)
		currentWindow += statsWindowSeconds
	}

	return windows
}

// GetCurrentWindowStats 获取当前窗口的统计（用于查询接口）
// Phase 8.x Task 2: 为读取查询提供便捷方法
func (s *ChannelStatsL2Service) GetCurrentWindowStats(channelID int, modelName string) (*ChannelStatsSnapshot, error) {
	currentWindow := AlignToWindow(time.Now().Unix())
	return s.GetStatsFromRedis(channelID, modelName, currentWindow)
}

// getChannelStatsFlushInterval 读取 L1->L2 刷新间隔。
// 默认值为 60 秒，可通过环境变量 CHANNEL_STATS_FLUSH_INTERVAL_SECONDS
// 在测试环境中将间隔缩短到秒级，以加速统计流转测试。
func getChannelStatsFlushInterval() time.Duration {
	const envName = "CHANNEL_STATS_FLUSH_INTERVAL_SECONDS"
	defaultInterval := time.Minute

	raw := os.Getenv(envName)
	if raw == "" {
		return defaultInterval
	}

	sec, err := strconv.Atoi(raw)
	if err != nil || sec <= 0 {
		common.SysLog(fmt.Sprintf("Invalid %s=%q, using default %s", envName, raw, defaultInterval))
		return defaultInterval
	}

	return time.Duration(sec) * time.Second
}

// getChannelStatsWindowSeconds 读取统计窗口大小（秒）。
// 默认值为 900 秒（15 分钟），可通过环境变量
// CHANNEL_STATS_WINDOW_SECONDS 在测试环境中缩短到 5-10 秒，
// 便于在 CI 中验证窗口对齐与 TTL 行为。
func getChannelStatsWindowSeconds() int64 {
	const envName = "CHANNEL_STATS_WINDOW_SECONDS"
	const defaultSeconds int64 = 900

	raw := os.Getenv(envName)
	if raw == "" {
		return defaultSeconds
	}

	sec, err := strconv.Atoi(raw)
	if err != nil || sec <= 0 {
		common.SysLog(fmt.Sprintf("Invalid %s=%q, using default %ds", envName, raw, defaultSeconds))
		return defaultSeconds
	}

	return int64(sec)
}
