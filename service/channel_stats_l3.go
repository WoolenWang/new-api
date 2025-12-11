package service

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ChannelStatsL3 L3层数据库同步服务
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: 3.2.3 L3: 数据库持久化 (Database)
//
// 架构特点:
// - 错峰同步：每个渠道独立的15分钟间隔+随机抖动
// - 原子操作：使用Lua脚本检查并提取数据
// - 增量同步：只同步已达到同步时间的渠道
// - 停服追踪：记录渠道禁用时间段

// ChannelStatsL3Service L3层数据库同步服务
type ChannelStatsL3Service struct {
	l2Service *ChannelStatsL2Service

	// 配置
	syncInterval    time.Duration // L2->L3同步检查间隔
	windowSize      time.Duration // 统计窗口大小（15分钟）
	maxConcurrency  int           // 最大并发同步数
	syncJitterRange time.Duration // 随机抖动范围（0-60秒）

	// 并发控制
	semaphore chan struct{} // 信号量限制并发数

	// 后台任务控制
	stopChan chan struct{}
	wg       sync.WaitGroup
}

var (
	channelStatsL3Service *ChannelStatsL3Service
	channelStatsL3Once    sync.Once
)

// GetChannelStatsL3Service 获取L3同步服务单例
func GetChannelStatsL3Service() *ChannelStatsL3Service {
	channelStatsL3Once.Do(func() {
		// 使用与L2相同的窗口配置，支持通过 CHANNEL_STATS_WINDOW_SECONDS
		// 在测试环境中缩短窗口大小。
		windowSeconds := getChannelStatsWindowSeconds()
		windowSize := time.Duration(windowSeconds) * time.Second

		// L2->L3 同步检查间隔，默认60秒，可通过
		// CHANNEL_STATS_SYNC_INTERVAL_SECONDS 在测试环境缩短。
		syncInterval := getChannelStatsSyncInterval()

		channelStatsL3Service = &ChannelStatsL3Service{
			l2Service:       GetChannelStatsL2Service(),
			syncInterval:    syncInterval,     // 默认1分钟，可通过ENV缩短
			windowSize:      windowSize,       // 默认15分钟，可通过ENV缩短
			maxConcurrency:  5,                // 最多5个并发同步
			syncJitterRange: 60 * time.Second, // 0-60秒抖动
			semaphore:       make(chan struct{}, 5),
			stopChan:        make(chan struct{}),
		}

		// 启动后台同步任务
		channelStatsL3Service.wg.Add(1)
		go channelStatsL3Service.syncLoop()

		common.SysLog("ChannelStatsL3Service initialized")
	})
	return channelStatsL3Service
}

// syncLoop L2->L3定时同步循环
func (s *ChannelStatsL3Service) syncLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.syncRedisToDatabase(); err != nil {
				common.SysLog(fmt.Sprintf("L2->L3 sync error: %v", err))
			}
		case <-s.stopChan:
			common.SysLog("ChannelStatsL3Service sync loop stopped")
			return
		}
	}
}

// syncRedisToDatabase 从Redis同步到数据库
func (s *ChannelStatsL3Service) syncRedisToDatabase() error {
	if !common.RedisEnabled {
		// Redis未启用，直接从L1同步到DB
		return s.syncL1ToDatabase()
	}

	// 1. 获取脏渠道列表
	dirtyChannels, err := s.l2Service.GetDirtyChannels(100)
	if err != nil {
		return fmt.Errorf("failed to get dirty channels: %w", err)
	}

	if len(dirtyChannels) == 0 {
		// 没有需要同步的数据
		return nil
	}

	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf("Found %d dirty channels to sync", len(dirtyChannels)))
	}

	// 2. 检查每个渠道的next_db_sync_time
	now := time.Now().Unix()
	syncedCount := 0

	for _, channelKey := range dirtyChannels {
		// Phase 8.x Task 2.1: 解析带窗口的key格式 "channel_id:model_name:window"
		parts := strings.Split(channelKey, ":")
		if len(parts) != 3 {
			common.SysLog(fmt.Sprintf("Failed to parse dirty channel key: %s", channelKey))
			continue
		}

		channelID, err := strconv.Atoi(parts[0])
		if err != nil {
			common.SysLog(fmt.Sprintf("Failed to parse channel_id from dirty channel key %q: %v", channelKey, err))
			continue
		}
		modelName := parts[1]
		windowStart, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			common.SysLog(fmt.Sprintf("Failed to parse windowStart from dirty channel key %q: %v", channelKey, err))
			continue
		}

		// 检查是否到达同步时间
		if !s.shouldSync(channelID, modelName, now) {
			continue
		}

		// 获取信号量（限制并发）
		s.semaphore <- struct{}{}

		// 异步同步
		go func(cid int, model string, window int64, key string) {
			defer func() { <-s.semaphore }()

			if err := s.syncChannel(cid, model, window, key); err != nil {
				common.SysLog(fmt.Sprintf("Failed to sync channel %d model %s window %d: %v", cid, model, window, err))
			} else {
				syncedCount++
			}
		}(channelID, modelName, windowStart, channelKey)
	}

	if common.DebugEnabled && syncedCount > 0 {
		common.SysLog(fmt.Sprintf("Synced %d channels to database", syncedCount))
	}

	return nil
}

// shouldSync 检查渠道是否应该同步（通过Redis检查next_db_sync_time）
func (s *ChannelStatsL3Service) shouldSync(channelID int, modelName string, now int64) bool {
	if !common.RedisEnabled {
		return true // Redis未启用时总是同步
	}

	ctx := context.Background()
	key := fmt.Sprintf("channel_sync_time:%d:%s", channelID, modelName)

	// 获取next_db_sync_time
	nextSyncTime, err := common.RDB.Get(ctx, key).Int64()
	if err != nil {
		// 键不存在，表示首次同步
		return true
	}

	return now >= nextSyncTime
}

// setNextSyncTime 设置下次同步时间（15分钟+随机抖动）
func (s *ChannelStatsL3Service) setNextSyncTime(channelID int, modelName string) error {
	if !common.RedisEnabled {
		return nil
	}

	ctx := context.Background()
	key := fmt.Sprintf("channel_sync_time:%d:%s", channelID, modelName)

	// 计算下次同步时间：15分钟 + 0-60秒随机抖动
	jitter := time.Duration(rand.Int63n(int64(s.syncJitterRange)))
	nextSync := time.Now().Add(s.windowSize).Add(jitter).Unix()

	return common.RDB.Set(ctx, key, nextSync, s.windowSize*2).Err()
}

// syncChannel 同步单个渠道的统计数据
// Phase 8.x Task 2.2 & 2.3: 使用窗口参数并实现同步后清理
func (s *ChannelStatsL3Service) syncChannel(channelID int, modelName string, windowStart int64, redisKey string) error {
	// 1. 从Redis读取统计数据（使用传入的窗口时间戳）
	var snapshot *ChannelStatsSnapshot
	var err error

	if common.RedisEnabled {
		snapshot, err = s.l2Service.GetStatsFromRedis(channelID, modelName, windowStart)
		if err != nil {
			return fmt.Errorf("failed to get stats from Redis: %w", err)
		}
	} else {
		// 回退到L1
		l1Stats := s.l2Service.l1Service.GetCurrentStats()
		key := fmt.Sprintf("%d:%s", channelID, modelName)
		if snap, ok := l1Stats[key]; ok {
			snapshot = snap
		} else {
			return fmt.Errorf("no stats found in L1")
		}
	}

	// 2. Phase 8.x Task 2.2: 使用传入的windowStart而不是重新计算
	// 这确保了与L2写入时使用相同的窗口边界

	// Phase 8.4: Calculate downtime within this window (CS4-2)
	// 从停服追踪器获取本窗口内的停服时长
	tracker := GetChannelDowntimeTracker()
	windowEnd := windowStart + int64(s.windowSize.Seconds())
	downtimeSeconds, err := tracker.GetDowntimeInWindow(channelID, windowStart, windowEnd)
	if err != nil {
		common.SysLog(fmt.Sprintf("Failed to get downtime for channel %d: %v", channelID, err))
		downtimeSeconds = 0 // 失败时使用0，不阻塞同步流程
	}

	// 3. 计算派生指标
	avgLatency := int64(0)
	if snapshot.RequestCount > 0 {
		avgLatency = snapshot.TotalLatencyMs / snapshot.RequestCount
	}

	// 4. 构建数据库记录
	stat := &model.ChannelStatistics{
		ChannelId:       channelID,
		ModelName:       modelName,
		TimeWindowStart: windowStart, // Phase 8.x Task 2.2: 使用对齐的窗口时间
		RequestCount:    int(snapshot.RequestCount),
		FailCount:       int(snapshot.FailCount),
		TotalTokens:     snapshot.TotalTokens,
		TotalQuota:      snapshot.TotalQuota,
		TotalLatencyMs:  snapshot.TotalLatencyMs,
		StreamReqCount:  int(snapshot.StreamReqCount),
		CacheHitCount:   int(snapshot.CacheHitCount),
		DowntimeSeconds: int(downtimeSeconds),      // Phase 8.4: Use actual downtime (CS4-2)
		UniqueUsers:     int(snapshot.UniqueUsers), // Phase 10.4: GS4-1 去重用户统计
	}

	// 5. 写入数据库（UPSERT）
	if err := model.UpsertChannelStatistics(stat); err != nil {
		return fmt.Errorf("failed to upsert channel statistics: %w", err)
	}

	// 6. 更新channels表的统计字段（最新值）
	if err := s.updateChannelAggregateStats(channelID, modelName, snapshot, avgLatency, downtimeSeconds); err != nil {
		common.SysLog(fmt.Sprintf("Failed to update channel aggregate stats: %v", err))
		// 不返回错误，继续处理
	}

	// 7. 发布渠道统计更新事件（Phase 10.2: GS2-1 触发P2P分组聚合）
	// 事件发布失败不影响主流程，仅记录日志
	PublishChannelStatsUpdatedEvent(channelID, modelName, windowStart)

	// 8. Phase 8.x Task 2.3: 同步后清除Redis中的该窗口数据
	// 这防止了重复计数，因为数据已持久化到数据库
	if common.RedisEnabled {
		if err := s.l2Service.ClearStatsInRedis(channelID, modelName, windowStart); err != nil {
			common.SysLog(fmt.Sprintf("Failed to clear Redis stats after sync: %v", err))
		}
	}

	// 9. 从脏集合中移除
	if common.RedisEnabled {
		if err := s.l2Service.RemoveDirtyChannel(redisKey); err != nil {
			common.SysLog(fmt.Sprintf("Failed to remove dirty channel: %v", err))
		}
	}

	// 10. 设置下次同步时间
	if err := s.setNextSyncTime(channelID, modelName); err != nil {
		common.SysLog(fmt.Sprintf("Failed to set next sync time: %v", err))
	}

	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf("Successfully synced channel %d model %s window %d (%d requests)",
			channelID, modelName, windowStart, snapshot.RequestCount))
	}

	return nil
}

// updateChannelAggregateStats 更新channels表的聚合统计字段
func (s *ChannelStatsL3Service) updateChannelAggregateStats(channelID int, modelName string, snapshot *ChannelStatsSnapshot, avgLatency int64, downtimeSeconds int64) error {
	channel, err := model.GetChannelById(channelID, false)
	if err != nil {
		return err
	}

	// 计算派生指标
	failRate := 0.0
	if snapshot.RequestCount > 0 {
		failRate = float64(snapshot.FailCount) / float64(snapshot.RequestCount) * 100.0
	}

	cacheHitRate := 0.0
	if snapshot.RequestCount > 0 {
		cacheHitRate = float64(snapshot.CacheHitCount) / float64(snapshot.RequestCount) * 100.0
	}

	streamRatio := 0.0
	if snapshot.RequestCount > 0 {
		streamRatio = float64(snapshot.StreamReqCount) / float64(snapshot.RequestCount) * 100.0
	}

	// Phase 8.4: Calculate downtime percentage (CS4-2)
	downtimePercentage := 0.0
	windowTotalSeconds := int64(s.windowSize.Seconds())
	if windowTotalSeconds > 0 && downtimeSeconds > 0 {
		downtimePercentage = float64(downtimeSeconds) / float64(windowTotalSeconds) * 100.0
	}

	// 计算TPM/RPM（基于窗口长度换算为每分钟速率）
	// 注意：窗口大小在生产环境通常为15分钟，但在测试环境中可以通过
	// CHANNEL_STATS_WINDOW_SECONDS 缩短到秒级，因此不能直接使用
	// s.windowSize.Minutes() 向下取整（否则在 <60s 的窗口中会得到 0 分钟）。
	windowTotalSeconds = int64(s.windowSize.Seconds())
	if windowTotalSeconds <= 0 {
		// 防御性兜底，避免除以0；退回到1分钟窗口
		windowTotalSeconds = 60
	}
	// tokens / minute = tokens * 60 / windowSeconds
	tpm := int(snapshot.TotalTokens * 60 / windowTotalSeconds)
	rpm := int(snapshot.RequestCount * 60 / windowTotalSeconds)
	quotaPM := snapshot.TotalQuota * 60 / windowTotalSeconds

	// 更新统计字段
	avgLatencyInt := int(avgLatency)
	updates := &model.ChannelStatisticsUpdate{
		AvgResponseTime:    &avgLatencyInt,
		FailRate:           &failRate,
		AvgCacheHitRate:    &cacheHitRate,
		StreamReqRatio:     &streamRatio,
		DowntimePercentage: &downtimePercentage, // Phase 8.4: Add downtime percentage (CS4-2)
		TPM:                &tpm,
		RPM:                &rpm,
		QuotaPM:            &quotaPM,
		TotalSessions:      &snapshot.SessionCount,
		UniqueUsers:        &snapshot.UniqueUsers,
	}

	return channel.UpdateStatistics(updates)
}

// syncL1ToDatabase 直接从L1同步到数据库（Redis未启用时的回退方案）
func (s *ChannelStatsL3Service) syncL1ToDatabase() error {
	// 获取L1当前统计
	l1Stats := s.l2Service.l1Service.GetCurrentStats()

	if len(l1Stats) == 0 {
		return nil
	}

	// Phase 8.x Task 2.2: 使用对齐的窗口时间戳
	currentWindow := AlignToWindow(time.Now().Unix())

	for key := range l1Stats {
		var channelID int
		var modelName string
		fmt.Sscanf(key, "%d:%s", &channelID, &modelName)

		// 直接同步（使用对齐的窗口时间）
		if err := s.syncChannel(channelID, modelName, currentWindow, key); err != nil {
			common.SysLog(fmt.Sprintf("Failed to sync channel %d model %s: %v", channelID, modelName, err))
		}
	}

	return nil
}

// Stop 停止L3同步服务
func (s *ChannelStatsL3Service) Stop() {
	close(s.stopChan)
	s.wg.Wait()
	common.SysLog("ChannelStatsL3Service stopped")
}

// getChannelStatsSyncInterval 读取 L2->L3 同步检查间隔。
// 默认值为 60 秒，可通过环境变量 CHANNEL_STATS_SYNC_INTERVAL_SECONDS
// 在测试环境中缩短到秒级，以加速 channel_statistics 聚合测试。
func getChannelStatsSyncInterval() time.Duration {
	const envName = "CHANNEL_STATS_SYNC_INTERVAL_SECONDS"
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

// ForceSync 强制同步指定渠道（用于测试或手动触发）
func (s *ChannelStatsL3Service) ForceSync(channelID int, modelName string) error {
	currentWindow := AlignToWindow(time.Now().Unix())
	key := GetDirtyChannelMember(channelID, modelName, currentWindow)
	return s.syncChannel(channelID, modelName, currentWindow, key)
}

// GetSyncStats 获取同步统计信息（用于监控）
type SyncStats struct {
	PendingSyncCount int   // 待同步渠道数
	LastSyncTime     int64 // 最后同步时间
}

// GetSyncStats 获取同步统计
func (s *ChannelStatsL3Service) GetSyncStats() (*SyncStats, error) {
	if !common.RedisEnabled {
		return &SyncStats{
			PendingSyncCount: 0,
			LastSyncTime:     time.Now().Unix(),
		}, nil
	}

	ctx := context.Background()

	// 获取脏渠道数量
	count, err := common.RDB.ZCard(ctx, "dirty_channels").Result()
	if err != nil {
		return nil, err
	}

	return &SyncStats{
		PendingSyncCount: int(count),
		LastSyncTime:     time.Now().Unix(),
	}, nil
}
