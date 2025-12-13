package service

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// StatsCleanupConfig 控制统计数据清理任务的行为。
type StatsCleanupConfig struct {
	Enabled              bool
	ChannelRetentionDays int
	GroupRetentionDays   int
	Interval             time.Duration
	MaxRunDuration       time.Duration
	BatchSize            int
}

const (
	defaultStatsCleanupIntervalHours = 24
	minStatsRetentionDays            = 30
	defaultStatsRetentionDays        = 90
	defaultStatsCleanupBatchSize     = 10_000
	defaultStatsCleanupMaxMinutes    = 10
	statsCleanupLockKey              = "lock:stats_cleanup"
)

// StartStatsCleanupWorker 启动统计数据清理后台任务。
//
// 行为：
//   - 默认开启（ENABLE_STATS_CLEANUP != "false"）；
//   - 间隔由 STATS_CLEANUP_INTERVAL_HOURS 控制（默认 24 小时）；
//   - 保留天数由 CHANNEL_STATS_RETENTION_DAYS / GROUP_STATS_RETENTION_DAYS 控制（默认 90，最小 30）。
func StartStatsCleanupWorker() {
	if os.Getenv("ENABLE_STATS_CLEANUP") == "false" {
		common.SysLog("StatsCleanupWorker disabled by ENABLE_STATS_CLEANUP=false")
		return
	}

	cfg := StatsCleanupConfig{
		Enabled:              true,
		ChannelRetentionDays: clampRetentionDays(common.GetEnvOrDefault("CHANNEL_STATS_RETENTION_DAYS", defaultStatsRetentionDays)),
		GroupRetentionDays:   clampRetentionDays(common.GetEnvOrDefault("GROUP_STATS_RETENTION_DAYS", defaultStatsRetentionDays)),
		Interval:             time.Duration(common.GetEnvOrDefault("STATS_CLEANUP_INTERVAL_HOURS", defaultStatsCleanupIntervalHours)) * time.Hour,
		MaxRunDuration:       defaultStatsCleanupMaxMinutes * time.Minute,
		BatchSize:            defaultStatsCleanupBatchSize,
	}

	if cfg.Interval <= 0 {
		cfg.Interval = defaultStatsCleanupIntervalHours * time.Hour
	}

	// 轻量级随机抖动，避免多实例在同一时间点同时触发。
	jitter := time.Duration(rand.Intn(300)) * time.Second

	go func() {
		time.Sleep(jitter)

		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()

		for {
			start := time.Now()
			if err := runStatsCleanupOnce(cfg); err != nil {
				common.SysLog(fmt.Sprintf("StatsCleanupWorker run error: %v", err))
			}
			// 避免超频执行：如果本次执行已经花费了超过 Interval，则下一轮会自然延后。
			elapsed := time.Since(start)
			if elapsed > cfg.Interval {
				common.SysLog(fmt.Sprintf("StatsCleanupWorker run took %s which exceeds interval %s", elapsed, cfg.Interval))
			}

			<-ticker.C
		}
	}()

	common.SysLog(fmt.Sprintf("StatsCleanupWorker started: channel_retention_days=%d, group_retention_days=%d, interval=%s",
		cfg.ChannelRetentionDays, cfg.GroupRetentionDays, cfg.Interval))
}

// runStatsCleanupOnce 执行一次统计数据清理。
func runStatsCleanupOnce(cfg StatsCleanupConfig) error {
	if !cfg.Enabled {
		return nil
	}

	now := common.GetTimestamp()
	channelCutoff := now - int64(cfg.ChannelRetentionDays*86400)
	groupCutoff := now - int64(cfg.GroupRetentionDays*86400)

	// 使用 Redis 分布式锁控制多实例并发；Redis 不可用时降级为单节点自管。
	lockValue := fmt.Sprintf("%d", time.Now().UnixNano())
	lockTTL := cfg.MaxRunDuration + 5*time.Minute

	acquired, err := common.AcquireLock(statsCleanupLockKey, lockValue, lockTTL)
	if err != nil {
		return fmt.Errorf("failed to acquire stats cleanup lock: %w", err)
	}
	if !acquired {
		// 另一实例正在执行清理，直接跳过本轮。
		if common.DebugEnabled {
			common.SysLog("StatsCleanupWorker: lock already held, skipping this run")
		}
		return nil
	}
	defer func() {
		_, _ = common.ReleaseLock(statsCleanupLockKey, lockValue)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.MaxRunDuration)
	defer cancel()

	start := time.Now()
	var (
		channelDeleted int64
		groupDeleted   int64
	)

	// 分批清理 channel_statistics
	for {
		if ctx.Err() != nil {
			break
		}
		rows, err := model.DeleteChannelStatisticsBeforeWithLimit(ctx, channelCutoff, cfg.BatchSize)
		if err != nil {
			common.SysLog(fmt.Sprintf("StatsCleanupWorker: failed to delete channel_statistics: %v", err))
			break
		}
		if rows == 0 {
			break
		}
		channelDeleted += rows
		if rows < int64(cfg.BatchSize) {
			break
		}
	}

	// 分批清理 group_statistics
	for {
		if ctx.Err() != nil {
			break
		}
		rows, err := model.DeleteGroupStatisticsBeforeWithLimit(ctx, groupCutoff, cfg.BatchSize)
		if err != nil {
			common.SysLog(fmt.Sprintf("StatsCleanupWorker: failed to delete group_statistics: %v", err))
			break
		}
		if rows == 0 {
			break
		}
		groupDeleted += rows
		if rows < int64(cfg.BatchSize) {
			break
		}
	}

	elapsed := time.Since(start)
	common.SysLog(fmt.Sprintf(
		"StatsCleanupWorker finished: channels_deleted=%d, groups_deleted=%d, channel_cutoff=%d, group_cutoff=%d, duration=%s",
		channelDeleted, groupDeleted, channelCutoff, groupCutoff, elapsed,
	))

	return nil
}

// clampRetentionDays 将保留天数限制在合理范围内（至少 minStatsRetentionDays）。
func clampRetentionDays(days int) int {
	if days < minStatsRetentionDays {
		common.SysLog(fmt.Sprintf("StatsCleanupWorker: retention days %d < %d, using minimum %d",
			days, minStatsRetentionDays, minStatsRetentionDays))
		return minStatsRetentionDays
	}
	return days
}
