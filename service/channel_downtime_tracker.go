package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ChannelDowntimeTracker 渠道停服时间追踪器
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: CS4-2 渠道状态相关控制器与服务
//
// 功能:
// - 记录渠道禁用/启用的时间戳
// - 计算任意时间窗口内的停服时长
// - 支持Redis持久化和内存回退
//
// Redis数据结构:
// - channel_status:{channel_id} -> HASH
//   - last_disable_time: 最近禁用时间戳
//   - last_enable_time: 最近启用时间戳
//   - total_downtime_seconds: 累计停服时长(秒)
//   - status: 当前状态 (1=enabled, 2=disabled_manual, 3=disabled_auto)

// ChannelDowntimeTrackerService 停服时间追踪服务
type ChannelDowntimeTrackerService struct {
	// 内存缓存（Redis不可用时的回退方案）
	memory     map[int]*ChannelDowntimeState
	memoryLock sync.RWMutex
}

// ChannelDowntimeState 渠道停服状态
type ChannelDowntimeState struct {
	ChannelID            int
	LastDisableTime      int64 // Unix timestamp
	LastEnableTime       int64 // Unix timestamp
	TotalDowntimeSeconds int64 // 累计停服时长
	CurrentStatus        int   // 1=enabled, 2/3=disabled
}

var (
	channelDowntimeTracker     *ChannelDowntimeTrackerService
	channelDowntimeTrackerOnce sync.Once
)

// GetChannelDowntimeTracker 获取停服追踪器单例
func GetChannelDowntimeTracker() *ChannelDowntimeTrackerService {
	channelDowntimeTrackerOnce.Do(func() {
		channelDowntimeTracker = &ChannelDowntimeTrackerService{
			memory: make(map[int]*ChannelDowntimeState),
		}
		common.SysLog("ChannelDowntimeTracker initialized")
	})
	return channelDowntimeTracker
}

// getStatusKey 生成Redis HASH键
func (s *ChannelDowntimeTrackerService) getStatusKey(channelID int) string {
	return fmt.Sprintf("channel_status:%d", channelID)
}

// RecordDisable 记录渠道禁用事件
func (s *ChannelDowntimeTrackerService) RecordDisable(channelID int, status int, timestamp int64) error {
	if timestamp == 0 {
		timestamp = time.Now().Unix()
	}

	if common.RedisEnabled {
		return s.recordDisableRedis(channelID, status, timestamp)
	}
	return s.recordDisableMemory(channelID, status, timestamp)
}

// RecordEnable 记录渠道启用事件
func (s *ChannelDowntimeTrackerService) RecordEnable(channelID int, timestamp int64) error {
	if timestamp == 0 {
		timestamp = time.Now().Unix()
	}

	if common.RedisEnabled {
		return s.recordEnableRedis(channelID, timestamp)
	}
	return s.recordEnableMemory(channelID, timestamp)
}

// recordDisableRedis Redis模式：记录禁用
func (s *ChannelDowntimeTrackerService) recordDisableRedis(channelID int, status int, timestamp int64) error {
	ctx := context.Background()
	key := s.getStatusKey(channelID)

	pipe := common.RDB.Pipeline()
	pipe.HSet(ctx, key, "last_disable_time", timestamp)
	pipe.HSet(ctx, key, "status", status)
	pipe.Expire(ctx, key, 90*24*time.Hour) // 90天过期

	_, err := pipe.Exec(ctx)
	if err != nil {
		common.SysLog(fmt.Sprintf("Failed to record channel %d disable to Redis: %v", channelID, err))
		// 回退到内存
		return s.recordDisableMemory(channelID, status, timestamp)
	}

	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf("Recorded channel %d disable at %d (status=%d)", channelID, timestamp, status))
	}
	return nil
}

// recordEnableRedis Redis模式：记录启用并累加停服时长
func (s *ChannelDowntimeTrackerService) recordEnableRedis(channelID int, timestamp int64) error {
	ctx := context.Background()
	key := s.getStatusKey(channelID)

	// 1. 读取上次禁用时间
	lastDisableTime, err := common.RDB.HGet(ctx, key, "last_disable_time").Int64()
	if err != nil {
		// 键不存在或从未禁用过，无需累加
		pipe := common.RDB.Pipeline()
		pipe.HSet(ctx, key, "last_enable_time", timestamp)
		pipe.HSet(ctx, key, "status", common.ChannelStatusEnabled)
		pipe.Expire(ctx, key, 90*24*time.Hour)
		_, _ = pipe.Exec(ctx)
		return nil
	}

	// 2. 计算本次停服时长
	downtime := timestamp - lastDisableTime
	if downtime < 0 {
		downtime = 0
	}

	// 3. 累加到total_downtime_seconds并更新状态
	pipe := common.RDB.Pipeline()
	pipe.HIncrBy(ctx, key, "total_downtime_seconds", downtime)
	pipe.HSet(ctx, key, "last_enable_time", timestamp)
	pipe.HSet(ctx, key, "status", common.ChannelStatusEnabled)
	pipe.Expire(ctx, key, 90*24*time.Hour)

	_, err = pipe.Exec(ctx)
	if err != nil {
		common.SysLog(fmt.Sprintf("Failed to record channel %d enable to Redis: %v", channelID, err))
		return s.recordEnableMemory(channelID, timestamp)
	}

	if common.DebugEnabled {
		common.SysLog(fmt.Sprintf("Recorded channel %d enable at %d, downtime=%ds", channelID, timestamp, downtime))
	}
	return nil
}

// recordDisableMemory 内存模式：记录禁用
func (s *ChannelDowntimeTrackerService) recordDisableMemory(channelID int, status int, timestamp int64) error {
	s.memoryLock.Lock()
	defer s.memoryLock.Unlock()

	state, exists := s.memory[channelID]
	if !exists {
		state = &ChannelDowntimeState{
			ChannelID: channelID,
		}
		s.memory[channelID] = state
	}

	state.LastDisableTime = timestamp
	state.CurrentStatus = status

	return nil
}

// recordEnableMemory 内存模式：记录启用并累加停服时长
func (s *ChannelDowntimeTrackerService) recordEnableMemory(channelID int, timestamp int64) error {
	s.memoryLock.Lock()
	defer s.memoryLock.Unlock()

	state, exists := s.memory[channelID]
	if !exists || state.LastDisableTime == 0 {
		// 从未禁用过，直接记录启用
		if !exists {
			state = &ChannelDowntimeState{ChannelID: channelID}
			s.memory[channelID] = state
		}
		state.LastEnableTime = timestamp
		state.CurrentStatus = common.ChannelStatusEnabled
		return nil
	}

	// 计算本次停服时长
	downtime := timestamp - state.LastDisableTime
	if downtime < 0 {
		downtime = 0
	}

	state.TotalDowntimeSeconds += downtime
	state.LastEnableTime = timestamp
	state.CurrentStatus = common.ChannelStatusEnabled

	return nil
}

// GetDowntimeInWindow 获取指定时间窗口内的停服时长（秒）
// windowStart, windowEnd: Unix timestamps
func (s *ChannelDowntimeTrackerService) GetDowntimeInWindow(channelID int, windowStart, windowEnd int64) (int64, error) {
	if common.RedisEnabled {
		return s.getDowntimeRedis(channelID, windowStart, windowEnd)
	}
	return s.getDowntimeMemory(channelID, windowStart, windowEnd)
}

// getDowntimeRedis Redis模式：计算窗口内停服时长
func (s *ChannelDowntimeTrackerService) getDowntimeRedis(channelID int, windowStart, windowEnd int64) (int64, error) {
	ctx := context.Background()
	key := s.getStatusKey(channelID)

	// 读取状态
	result, err := common.RDB.HGetAll(ctx, key).Result()
	if err != nil || len(result) == 0 {
		// 键不存在，表示从未禁用过
		return 0, nil
	}

	state := &ChannelDowntimeState{ChannelID: channelID}

	if v, ok := result["last_disable_time"]; ok {
		state.LastDisableTime, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := result["last_enable_time"]; ok {
		state.LastEnableTime, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := result["total_downtime_seconds"]; ok {
		state.TotalDowntimeSeconds, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := result["status"]; ok {
		status, _ := strconv.Atoi(v)
		state.CurrentStatus = status
	}

	return s.calculateWindowDowntime(state, windowStart, windowEnd), nil
}

// getDowntimeMemory 内存模式：计算窗口内停服时长
func (s *ChannelDowntimeTrackerService) getDowntimeMemory(channelID int, windowStart, windowEnd int64) (int64, error) {
	s.memoryLock.RLock()
	state, exists := s.memory[channelID]
	s.memoryLock.RUnlock()

	if !exists {
		return 0, nil
	}

	return s.calculateWindowDowntime(state, windowStart, windowEnd), nil
}

// calculateWindowDowntime 计算窗口与停服时间段的交集
func (s *ChannelDowntimeTrackerService) calculateWindowDowntime(state *ChannelDowntimeState, windowStart, windowEnd int64) int64 {
	downtime := int64(0)

	// 场景1: 渠道当前处于禁用状态
	if state.CurrentStatus != common.ChannelStatusEnabled && state.LastDisableTime > 0 {
		disableStart := state.LastDisableTime
		disableEnd := time.Now().Unix() // 当前还在禁用中

		// 计算与窗口的交集
		intersection := s.calculateIntersection(windowStart, windowEnd, disableStart, disableEnd)
		downtime += intersection

		if common.DebugEnabled && intersection > 0 {
			common.SysLog(fmt.Sprintf("Channel %d: current disable period [%d, %d] intersects window [%d, %d] = %ds",
				state.ChannelID, disableStart, disableEnd, windowStart, windowEnd, intersection))
		}
	}

	// 场景2: 窗口内有历史的禁用->启用周期
	// 如果LastEnableTime在窗口内，且LastDisableTime也在窗口内或之前
	if state.LastEnableTime > 0 && state.LastDisableTime > 0 &&
		state.LastEnableTime >= windowStart && state.LastDisableTime < windowEnd {

		// 计算历史停服周期与窗口的交集
		disableStart := state.LastDisableTime
		disableEnd := state.LastEnableTime

		intersection := s.calculateIntersection(windowStart, windowEnd, disableStart, disableEnd)

		// 避免与"场景1"重复计数：只有当前是启用状态时才计入历史周期
		if state.CurrentStatus == common.ChannelStatusEnabled {
			downtime += intersection

			if common.DebugEnabled && intersection > 0 {
				common.SysLog(fmt.Sprintf("Channel %d: historical disable period [%d, %d] intersects window [%d, %d] = %ds",
					state.ChannelID, disableStart, disableEnd, windowStart, windowEnd, intersection))
			}
		}
	}

	return downtime
}

// calculateIntersection 计算两个时间段的交集长度（秒）
func (s *ChannelDowntimeTrackerService) calculateIntersection(start1, end1, start2, end2 int64) int64 {
	// [start1, end1] ∩ [start2, end2]
	intersectStart := max64(start1, start2)
	intersectEnd := min64(end1, end2)

	if intersectStart >= intersectEnd {
		return 0
	}
	return intersectEnd - intersectStart
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// ResetChannelDowntime 重置渠道停服统计（用于测试或管理）
func (s *ChannelDowntimeTrackerService) ResetChannelDowntime(channelID int) error {
	if common.RedisEnabled {
		ctx := context.Background()
		key := s.getStatusKey(channelID)
		return common.RDB.Del(ctx, key).Err()
	}

	s.memoryLock.Lock()
	delete(s.memory, channelID)
	s.memoryLock.Unlock()

	return nil
}
