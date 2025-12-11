package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	// GroupStatsThrottleDuration 分组统计更新节流时间（30分钟）
	GroupStatsThrottleDuration = 30 * time.Minute
	// MaxConcurrentGroupAggregations 全局最大并发聚合任务数
	MaxConcurrentGroupAggregations = 5
)

// GroupStatsUpdateTask 分组统计更新任务
type GroupStatsUpdateTask struct {
	GroupId         int   `json:"group_id"`
	TriggerTime     int64 `json:"trigger_time"`
	ChannelId       int   `json:"channel_id"`        // 触发此任务的渠道ID
	TimeWindowStart int64 `json:"time_window_start"` // 统计窗口起始时间
}

// GroupStatsScheduler 分组统计调度器
// 负责监听渠道统计更新事件，并按节流策略调度分组聚合任务
type GroupStatsScheduler struct {
	taskQueue            chan GroupStatsUpdateTask
	lastUpdateTime       map[int]int64 // groupId -> last update timestamp
	lastUpdateTimeMu     sync.RWMutex
	ctx                  context.Context
	cancel               context.CancelFunc
	enabled              bool
	concurrencySemaphore chan struct{} // 用于全局并发控制
}

var (
	globalGroupStatsScheduler *GroupStatsScheduler
	groupStatsSchedulerOnce   sync.Once
	taskQueueBufferSize       = 1000 // 任务队列缓冲区大小
)

// GetGlobalScheduler 获取全局调度器实例（单例模式）
func GetGlobalScheduler() *GroupStatsScheduler {
	groupStatsSchedulerOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		globalGroupStatsScheduler = &GroupStatsScheduler{
			taskQueue:            make(chan GroupStatsUpdateTask, taskQueueBufferSize),
			lastUpdateTime:       make(map[int]int64),
			ctx:                  ctx,
			cancel:               cancel,
			enabled:              true,
			concurrencySemaphore: make(chan struct{}, MaxConcurrentGroupAggregations),
		}
		common.SysLog("GroupStatsScheduler initialized")
	})
	return globalGroupStatsScheduler
}

// Start 启动调度器
// 开始监听渠道统计更新事件，并调度分组聚合任务
func (s *GroupStatsScheduler) Start() {
	if !s.enabled {
		common.SysLog("GroupStatsScheduler is disabled, not starting")
		return
	}

	common.SysLog("GroupStatsScheduler starting...")

	// 订阅渠道统计更新事件
	eventChan := SubscribeGroupStatsEvents()

	// 启动事件监听goroutine
	go s.listenEvents(eventChan)

	// 启动任务处理goroutine
	go s.processTasks()

	common.SysLog("GroupStatsScheduler started successfully")
}

// Stop 停止调度器
func (s *GroupStatsScheduler) Stop() {
	common.SysLog("GroupStatsScheduler stopping...")
	s.cancel()
	close(s.taskQueue)
	common.SysLog("GroupStatsScheduler stopped")
}

// listenEvents 监听渠道统计更新事件
func (s *GroupStatsScheduler) listenEvents(eventChan <-chan ChannelStatsUpdatedEvent) {
	for {
		select {
		case <-s.ctx.Done():
			common.SysLog("GroupStatsScheduler event listener stopped")
			return
		case event, ok := <-eventChan:
			if !ok {
				common.SysLog("Event channel closed, stopping listener")
				return
			}
			// 处理事件
			s.handleChannelStatsUpdated(event)
		}
	}
}

// handleChannelStatsUpdated 处理渠道统计更新事件
func (s *GroupStatsScheduler) handleChannelStatsUpdated(event ChannelStatsUpdatedEvent) {
	// 1. 查询该渠道所属的P2P分组
	groupIds, err := s.findAffectedP2PGroups(event.ChannelId)
	if err != nil {
		common.SysLog("Error finding affected P2P groups for channel %d: %v", event.ChannelId, err)
		return
	}

	if len(groupIds) == 0 {
		// 该渠道不属于任何P2P分组，无需处理
		return
	}

	currentTime := time.Now().Unix()

	// 2. 对每个受影响的分组进行节流检查
	for _, groupId := range groupIds {
		if s.shouldTriggerUpdate(groupId, currentTime) {
			// 通过节流检查，推送任务到队列
			task := GroupStatsUpdateTask{
				GroupId:         groupId,
				TriggerTime:     currentTime,
				ChannelId:       event.ChannelId,
				TimeWindowStart: event.TimeWindowStart,
			}
			s.enqueueTask(task)
		}
	}
}

// findAffectedP2PGroups 查找受影响的P2P分组
// Phase 10.2: GS2-2 解析渠道所属的P2P分组
// 通过channels表的AllowedGroups字段（JSON数组格式）查找P2P分组ID
func (s *GroupStatsScheduler) findAffectedP2PGroups(channelId int) ([]int, error) {
	// 查询渠道信息
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		return nil, err
	}

	if channel == nil {
		return []int{}, nil
	}

	// 解析渠道的P2P分组配置
	// AllowedGroups字段存储的是P2P分组ID的JSON数组，如 [101, 102, 103]
	// GetAllowedGroupIDs()方法会解析该字段并返回整数ID列表
	groupIds := channel.GetAllowedGroupIDs()

	// 如果渠道未配置P2P分组（AllowedGroups为空或nil），返回空列表
	// 这表示该渠道不属于任何P2P分组，无需触发分组统计聚合
	if len(groupIds) == 0 {
		common.SysLog(fmt.Sprintf("Channel %d has no P2P group associations", channelId))
		return []int{}, nil
	}

	common.SysLog(fmt.Sprintf("Channel %d affects P2P groups: %v", channelId, groupIds))
	return groupIds, nil
}

// shouldTriggerUpdate 检查是否应该触发更新（节流检查）
func (s *GroupStatsScheduler) shouldTriggerUpdate(groupId int, currentTime int64) bool {
	s.lastUpdateTimeMu.RLock()
	lastTime, exists := s.lastUpdateTime[groupId]
	s.lastUpdateTimeMu.RUnlock()

	if !exists {
		// 第一次更新，允许
		s.updateLastTime(groupId, currentTime)
		return true
	}

	// 检查是否超过节流时间
	elapsedSeconds := currentTime - lastTime
	throttleSeconds := int64(GroupStatsThrottleDuration.Seconds())

	if elapsedSeconds >= throttleSeconds {
		// 超过节流时间，允许更新
		s.updateLastTime(groupId, currentTime)
		return true
	}

	// 未超过节流时间，跳过
	return false
}

// updateLastTime 更新分组的最后更新时间
func (s *GroupStatsScheduler) updateLastTime(groupId int, timestamp int64) {
	s.lastUpdateTimeMu.Lock()
	defer s.lastUpdateTimeMu.Unlock()
	s.lastUpdateTime[groupId] = timestamp
}

// enqueueTask 将任务加入队列
func (s *GroupStatsScheduler) enqueueTask(task GroupStatsUpdateTask) {
	select {
	case s.taskQueue <- task:
		// 成功入队
		common.SysLog("Group stats update task enqueued for group %d", task.GroupId)
	default:
		// 队列满，记录警告
		common.SysLog("Warning: task queue is full, dropping task for group %d", task.GroupId)
	}
}

// processTasks 处理任务队列
func (s *GroupStatsScheduler) processTasks() {
	for {
		select {
		case <-s.ctx.Done():
			common.SysLog("GroupStatsScheduler task processor stopped")
			return
		case task, ok := <-s.taskQueue:
			if !ok {
				common.SysLog("Task queue closed, stopping processor")
				return
			}
			// 获取并发信号量
			s.concurrencySemaphore <- struct{}{}
			// 启动goroutine处理任务
			go func(t GroupStatsUpdateTask) {
				defer func() {
					// 释放信号量
					<-s.concurrencySemaphore
				}()
				s.executeTask(t)
			}(task)
		}
	}
}

// executeTask 执行聚合任务
// Phase 10.3: GS3-1 使用分布式锁防止多节点并发聚合同一分组
func (s *GroupStatsScheduler) executeTask(task GroupStatsUpdateTask) {
	groupID := task.GroupId
	common.SysLog("Executing group stats aggregation for group %d", groupID)

	// 1. 尝试获取分布式锁（防止多实例并发聚合）
	lockKey := fmt.Sprintf("group_stats_lock:%d", groupID)
	lockValue := fmt.Sprintf("%d", time.Now().UnixNano()) // 使用纳秒时间戳作为唯一标识
	lockExpiration := 180 * time.Second                   // 锁超时时间3分钟，防止任务失败时死锁

	acquired, err := common.AcquireLock(lockKey, lockValue, lockExpiration)
	if err != nil {
		common.SysLog("Error acquiring lock for group %d: %v", groupID, err)
		return
	}

	if !acquired {
		// 锁已被其他节点持有，跳过本次聚合
		common.SysLog("Group %d aggregation skipped: lock held by another instance", groupID)
		return
	}

	// 2. 确保在函数退出时释放锁
	defer func() {
		released, err := common.ReleaseLock(lockKey, lockValue)
		if err != nil {
			common.SysLog("Error releasing lock for group %d: %v", groupID, err)
		} else if !released {
			common.SysLog("Warning: lock for group %d was not released (value mismatch)", groupID)
		}
	}()

	// 3. 执行实际的聚合计算
	common.SysLog("Lock acquired, starting aggregation for group %d", groupID)
	err = AggregateGroupStatsForAllModels(groupID, task.TimeWindowStart)
	if err != nil {
		common.SysLog("Error aggregating group stats for group %d: %v", groupID, err)
		return
	}

	common.SysLog("Successfully aggregated group stats for group %d", groupID)
}

// GetTaskQueueLength 获取任务队列长度（用于监控）
func (s *GroupStatsScheduler) GetTaskQueueLength() int {
	return len(s.taskQueue)
}

// GetLastUpdateTime 获取分组的最后更新时间（用于调试）
func (s *GroupStatsScheduler) GetLastUpdateTime(groupId int) (int64, bool) {
	s.lastUpdateTimeMu.RLock()
	defer s.lastUpdateTimeMu.RUnlock()
	t, exists := s.lastUpdateTime[groupId]
	return t, exists
}

// Enable 启用调度器
func (s *GroupStatsScheduler) Enable() {
	s.enabled = true
	common.SysLog("GroupStatsScheduler enabled")
}

// Disable 禁用调度器
func (s *GroupStatsScheduler) Disable() {
	s.enabled = false
	common.SysLog("GroupStatsScheduler disabled")
}
