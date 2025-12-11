package service

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ChannelStatsL1 L1层内存统计收集器
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: 3.2.1 L1: 进程内存缓存 (In-Memory)
//
// 架构特点:
// - 使用sync.Map实现无锁并发安全
// - 使用atomic.Int64进行原子计数
// - 每个渠道+模型维度独立统计
// - 轻量级协程异步上报，零阻塞主流程

// ChannelModelKey 渠道模型统计键
type ChannelModelKey struct {
	ChannelID int
	ModelName string
}

// String 实现Stringer接口，用于Map的键
func (k ChannelModelKey) String() string {
	return fmt.Sprintf("%d:%s", k.ChannelID, k.ModelName)
}

// ChannelStatsCounter L1内存计数器（原子操作）
type ChannelStatsCounter struct {
	// 请求统计
	RequestCount atomic.Int64 // 总请求数
	FailCount    atomic.Int64 // 失败请求数

	// Token统计
	TotalTokens      atomic.Int64 // 总Token数
	PromptTokens     atomic.Int64 // 输入Token数
	CompletionTokens atomic.Int64 // 输出Token数

	// 额度统计
	TotalQuota atomic.Int64 // 总额度消耗

	// 延迟统计
	TotalLatencyMs atomic.Int64 // 总首字延迟(ms)

	// 流式请求统计
	StreamReqCount atomic.Int64 // 流式请求数

	// 缓存统计
	CacheHitCount atomic.Int64 // 缓存命中数

	// 会话统计
	SessionCount atomic.Int64 // 会话数

	// 用户统计（需要HyperLogLog去重，暂存UserID集合）
	UserIDSet sync.Map // map[int]bool 用户ID集合（简化实现，生产应用HyperLogLog）

	// 元数据
	LastUpdateTime atomic.Int64 // 最后更新时间（Unix时间戳）
	ChannelID      int          // 渠道ID
	ModelName      string       // 模型名称
}

// NewChannelStatsCounter 创建新的计数器
func NewChannelStatsCounter(channelID int, modelName string) *ChannelStatsCounter {
	counter := &ChannelStatsCounter{
		ChannelID: channelID,
		ModelName: modelName,
	}
	counter.LastUpdateTime.Store(time.Now().Unix())
	return counter
}

// RecordRequest 记录请求统计
type RequestStats struct {
	ChannelID        int
	ModelName        string
	Success          bool   // 是否成功
	TotalTokens      int64  // 总Token数
	PromptTokens     int64  // 输入Token数
	CompletionTokens int64  // 输出Token数
	QuotaUsed        int64  // 额度消耗
	FirstByteLatency int64  // 首字延迟(ms)
	IsStream         bool   // 是否流式请求
	IsCacheHit       bool   // 是否缓存命中
	UserID           int    // 用户ID
	SessionID        string // 会话ID（用于去重）
}

// ChannelStatsL1Service L1层统计服务
type ChannelStatsL1Service struct {
	// sync.Map: key为ChannelModelKey字符串, value为*ChannelStatsCounter
	counters sync.Map

	// 清理配置
	cleanupInterval time.Duration // 清理间隔
	maxIdleTime     time.Duration // 最大空闲时间

	// 后台任务控制
	stopChan chan struct{}
	wg       sync.WaitGroup
}

var (
	// 全局L1统计服务实例
	channelStatsL1Service *ChannelStatsL1Service
	channelStatsL1Once    sync.Once
)

// GetChannelStatsL1Service 获取L1统计服务单例
func GetChannelStatsL1Service() *ChannelStatsL1Service {
	channelStatsL1Once.Do(func() {
		channelStatsL1Service = &ChannelStatsL1Service{
			cleanupInterval: 1 * time.Minute, // 每分钟清理一次
			maxIdleTime:     5 * time.Minute, // 5分钟无更新视为冷数据
			stopChan:        make(chan struct{}),
		}

		// 启动后台清理任务
		channelStatsL1Service.wg.Add(1)
		go channelStatsL1Service.cleanupLoop()

		common.SysLog("ChannelStatsL1Service initialized")
	})
	return channelStatsL1Service
}

// RecordRequestAsync 异步记录请求统计（无阻塞）
func (s *ChannelStatsL1Service) RecordRequestAsync(stats *RequestStats) {
	// 使用gopool或简单go routine异步上报
	go s.recordRequest(stats)
}

// recordRequest 实际的记录逻辑（在独立协程中执行）
func (s *ChannelStatsL1Service) recordRequest(stats *RequestStats) {
	if stats == nil {
		return
	}

	key := ChannelModelKey{
		ChannelID: stats.ChannelID,
		ModelName: stats.ModelName,
	}.String()

	// 获取或创建计数器
	counterInterface, _ := s.counters.LoadOrStore(key, NewChannelStatsCounter(stats.ChannelID, stats.ModelName))
	counter := counterInterface.(*ChannelStatsCounter)

	// 原子操作更新计数
	counter.RequestCount.Add(1)

	if !stats.Success {
		counter.FailCount.Add(1)
	}

	if stats.TotalTokens > 0 {
		counter.TotalTokens.Add(stats.TotalTokens)
		counter.PromptTokens.Add(stats.PromptTokens)
		counter.CompletionTokens.Add(stats.CompletionTokens)
	}

	if stats.QuotaUsed > 0 {
		counter.TotalQuota.Add(stats.QuotaUsed)
	}

	if stats.FirstByteLatency > 0 {
		counter.TotalLatencyMs.Add(stats.FirstByteLatency)
	}

	if stats.IsStream {
		counter.StreamReqCount.Add(1)
	}

	if stats.IsCacheHit {
		counter.CacheHitCount.Add(1)
	}

	if stats.SessionID != "" {
		counter.SessionCount.Add(1)
	}

	// 用户去重（简化实现，生产环境应使用HyperLogLog）
	if stats.UserID > 0 {
		counter.UserIDSet.Store(stats.UserID, true)
	}

	// 更新最后活跃时间
	counter.LastUpdateTime.Store(time.Now().Unix())
}

// GetSnapshot 获取当前快照并重置计数器（用于刷新到L2）
func (s *ChannelStatsL1Service) GetSnapshot() map[string]*ChannelStatsSnapshot {
	snapshot := make(map[string]*ChannelStatsSnapshot)

	s.counters.Range(func(key, value interface{}) bool {
		keyStr := key.(string)
		counter := value.(*ChannelStatsCounter)

		// 提取并重置计数（原子操作）
		// Phase 8.x Task 3.1: 提取UserIDs用于HyperLogLog
		uniqueUsers, userIDs := s.getUniqueUserCountAndIDs(&counter.UserIDSet)

		snap := &ChannelStatsSnapshot{
			ChannelID:        counter.ChannelID,
			ModelName:        counter.ModelName,
			RequestCount:     counter.RequestCount.Swap(0),
			FailCount:        counter.FailCount.Swap(0),
			TotalTokens:      counter.TotalTokens.Swap(0),
			PromptTokens:     counter.PromptTokens.Swap(0),
			CompletionTokens: counter.CompletionTokens.Swap(0),
			TotalQuota:       counter.TotalQuota.Swap(0),
			TotalLatencyMs:   counter.TotalLatencyMs.Swap(0),
			StreamReqCount:   counter.StreamReqCount.Swap(0),
			CacheHitCount:    counter.CacheHitCount.Swap(0),
			SessionCount:     counter.SessionCount.Swap(0),
			UniqueUsers:      uniqueUsers,
			UserIDs:          userIDs,
			SnapshotTime:     time.Now().Unix(),
		}

		// 重置用户ID集合
		counter.UserIDSet = sync.Map{}

		// 只返回有数据的快照
		if snap.RequestCount > 0 {
			snapshot[keyStr] = snap
		}

		return true
	})

	return snapshot
}

// ChannelStatsSnapshot 统计快照（用于刷新到L2）
type ChannelStatsSnapshot struct {
	ChannelID        int
	ModelName        string
	RequestCount     int64
	FailCount        int64
	TotalTokens      int64
	PromptTokens     int64
	CompletionTokens int64
	TotalQuota       int64
	TotalLatencyMs   int64
	StreamReqCount   int64
	CacheHitCount    int64
	SessionCount     int64
	UniqueUsers      int      // 去重后的用户数
	UserIDs          []string // Phase 8.x Task 3.1: 用户ID列表（用于HyperLogLog）
	SnapshotTime     int64
}

// getUniqueUserCount 计算去重用户数并提取UserID列表
// Phase 8.x Task 3.1: Extract user IDs for HyperLogLog
func (s *ChannelStatsL1Service) getUniqueUserCountAndIDs(userIDSet *sync.Map) (int, []string) {
	count := 0
	userIDs := make([]string, 0, 100) // 预分配容量
	userIDSet.Range(func(key, value interface{}) bool {
		count++
		// UserID 存储为 int，转换为字符串供 HyperLogLog 使用
		if userID, ok := key.(int); ok {
			userIDs = append(userIDs, fmt.Sprintf("%d", userID))
		}
		return true
	})
	return count, userIDs
}

// getUniqueUserCount 仅计算去重用户数量（用于只读快照）
func (s *ChannelStatsL1Service) getUniqueUserCount(userIDSet *sync.Map) int {
	count, _ := s.getUniqueUserCountAndIDs(userIDSet)
	return count
}

// cleanupLoop 后台清理循环（移除冷数据）
func (s *ChannelStatsL1Service) cleanupLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanupIdleCounters()
		case <-s.stopChan:
			common.SysLog("ChannelStatsL1Service cleanup loop stopped")
			return
		}
	}
}

// cleanupIdleCounters 清理空闲计数器
func (s *ChannelStatsL1Service) cleanupIdleCounters() {
	now := time.Now().Unix()
	deletedCount := 0

	s.counters.Range(func(key, value interface{}) bool {
		counter := value.(*ChannelStatsCounter)
		lastUpdate := counter.LastUpdateTime.Load()

		// 如果超过maxIdleTime未更新，则删除
		if now-lastUpdate > int64(s.maxIdleTime.Seconds()) {
			s.counters.Delete(key)
			deletedCount++
		}

		return true
	})

	if deletedCount > 0 && common.DebugEnabled {
		common.SysLog(fmt.Sprintf("ChannelStatsL1Service cleaned up %d idle counters", deletedCount))
	}
}

// Stop 停止L1统计服务
func (s *ChannelStatsL1Service) Stop() {
	close(s.stopChan)
	s.wg.Wait()
	common.SysLog("ChannelStatsL1Service stopped")
}

// GetCurrentStats 获取当前统计（不重置，用于监控/调试）
func (s *ChannelStatsL1Service) GetCurrentStats() map[string]*ChannelStatsSnapshot {
	snapshot := make(map[string]*ChannelStatsSnapshot)

	s.counters.Range(func(key, value interface{}) bool {
		keyStr := key.(string)
		counter := value.(*ChannelStatsCounter)

		// 读取但不重置
		snap := &ChannelStatsSnapshot{
			ChannelID:        counter.ChannelID,
			ModelName:        counter.ModelName,
			RequestCount:     counter.RequestCount.Load(),
			FailCount:        counter.FailCount.Load(),
			TotalTokens:      counter.TotalTokens.Load(),
			PromptTokens:     counter.PromptTokens.Load(),
			CompletionTokens: counter.CompletionTokens.Load(),
			TotalQuota:       counter.TotalQuota.Load(),
			TotalLatencyMs:   counter.TotalLatencyMs.Load(),
			StreamReqCount:   counter.StreamReqCount.Load(),
			CacheHitCount:    counter.CacheHitCount.Load(),
			SessionCount:     counter.SessionCount.Load(),
			UniqueUsers:      s.getUniqueUserCount(&counter.UserIDSet),
			SnapshotTime:     time.Now().Unix(),
		}

		snapshot[keyStr] = snap
		return true
	})

	return snapshot
}
