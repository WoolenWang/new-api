package testutil

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
)

// ConcurrentExecutor 并发执行器，用于运行并发测试
type ConcurrentExecutor struct {
	Count         int           // 并发数量
	StaggerDelay  time.Duration // 交错延迟（可选，用于模拟更真实的并发场景）
	CollectErrors bool          // 是否收集错误
}

// ConcurrentResult 并发执行结果
type ConcurrentResult struct {
	Errors        []error       // 每个goroutine的错误
	SuccessCount  int           // 成功数量
	FailureCount  int           // 失败数量
	TotalDuration time.Duration // 总执行时间
}

// Run 执行并发任务
func (ce *ConcurrentExecutor) Run(fn func(i int) error) *ConcurrentResult {
	var wg sync.WaitGroup
	errors := make([]error, ce.Count)
	startTime := time.Now()

	wg.Add(ce.Count)
	for i := 0; i < ce.Count; i++ {
		go func(index int) {
			defer wg.Done()

			// 可选的交错启动
			if ce.StaggerDelay > 0 {
				time.Sleep(time.Duration(index) * ce.StaggerDelay)
			}

			errors[index] = fn(index)
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	// 统计成功/失败
	successCount := 0
	failureCount := 0
	for _, err := range errors {
		if err == nil {
			successCount++
		} else {
			failureCount++
		}
	}

	return &ConcurrentResult{
		Errors:        errors,
		SuccessCount:  successCount,
		FailureCount:  failureCount,
		TotalDuration: duration,
	}
}

// AtomicCounter 原子计数器，用于并发累加验证
type AtomicCounter struct {
	value int64
}

// Add 原子增加
func (ac *AtomicCounter) Add(delta int64) int64 {
	return atomic.AddInt64(&ac.value, delta)
}

// Get 获取当前值
func (ac *AtomicCounter) Get() int64 {
	return atomic.LoadInt64(&ac.value)
}

// Reset 重置为0
func (ac *AtomicCounter) Reset() {
	atomic.StoreInt64(&ac.value, 0)
}

// RaceDetector 竞态检测器
type RaceDetector struct {
	expectedSum int64
	actualSum   int64
	tolerance   int64
	mu          sync.Mutex
	results     []int64
}

// NewRaceDetector 创建竞态检测器
func NewRaceDetector(expectedSum, tolerance int64) *RaceDetector {
	return &RaceDetector{
		expectedSum: expectedSum,
		tolerance:   tolerance,
		results:     make([]int64, 0),
	}
}

// Record 记录一个结果
func (rd *RaceDetector) Record(value int64) {
	rd.mu.Lock()
	defer rd.mu.Unlock()

	rd.results = append(rd.results, value)
	rd.actualSum += value
}

// Verify 验证是否存在竞态
func (rd *RaceDetector) Verify(t *testing.T, message string) {
	diff := rd.actualSum - rd.expectedSum
	if diff < 0 {
		diff = -diff
	}

	assert.LessOrEqual(t, diff, rd.tolerance,
		"%s: expected sum=%d (±%d), actual sum=%d, diff=%d, recorded %d values",
		message, rd.expectedSum, rd.tolerance, rd.actualSum, diff, len(rd.results))
}

// GetResults 获取所有记录的结果
func (rd *RaceDetector) GetResults() []int64 {
	rd.mu.Lock()
	defer rd.mu.Unlock()
	return append([]int64{}, rd.results...)
}

// RedisWindowHelper Redis滑动窗口辅助工具
type RedisWindowHelper struct {
	mr *miniredis.Miniredis
}

// NewRedisWindowHelper 创建Redis窗口辅助工具
func NewRedisWindowHelper(mr *miniredis.Miniredis) *RedisWindowHelper {
	return &RedisWindowHelper{mr: mr}
}

// GetWindowConsumed 获取窗口消耗值
func (rwh *RedisWindowHelper) GetWindowConsumed(subscriptionID int, period string) (int64, error) {
	key := fmt.Sprintf("subscription:%d:%s:window", subscriptionID, period)
	consumed := rwh.mr.HGet(key, "consumed")
	if consumed == "" {
		return 0, fmt.Errorf("window %s missing consumed field", key)
	}

	var value int64
	_, err := fmt.Sscanf(consumed, "%d", &value)
	return value, err
}

// GetWindowStartTime 获取窗口开始时间
func (rwh *RedisWindowHelper) GetWindowStartTime(subscriptionID int, period string) (int64, error) {
	key := fmt.Sprintf("subscription:%d:%s:window", subscriptionID, period)
	startTime := rwh.mr.HGet(key, "start_time")
	if startTime == "" {
		return 0, fmt.Errorf("window %s missing start_time field", key)
	}

	var value int64
	_, err := fmt.Sscanf(startTime, "%d", &value)
	return value, err
}

// GetWindowEndTime 获取窗口结束时间
func (rwh *RedisWindowHelper) GetWindowEndTime(subscriptionID int, period string) (int64, error) {
	key := fmt.Sprintf("subscription:%d:%s:window", subscriptionID, period)
	endTime := rwh.mr.HGet(key, "end_time")
	if endTime == "" {
		return 0, fmt.Errorf("window %s missing end_time field", key)
	}

	var value int64
	_, err := fmt.Sscanf(endTime, "%d", &value)
	return value, err
}

// WindowExists 检查窗口是否存在
func (rwh *RedisWindowHelper) WindowExists(subscriptionID int, period string) bool {
	key := fmt.Sprintf("subscription:%d:%s:window", subscriptionID, period)
	return rwh.mr.Exists(key)
}

// CountWindowKeys 统计匹配模式的窗口Key数量
func (rwh *RedisWindowHelper) CountWindowKeys(pattern string) int {
	keys := rwh.mr.Keys()
	count := 0
	for _, key := range keys {
		// 简单的模式匹配
		if matchPattern(key, pattern) {
			count++
		}
	}
	return count
}

// DeleteWindow 删除窗口
func (rwh *RedisWindowHelper) DeleteWindow(subscriptionID int, period string) error {
	key := fmt.Sprintf("subscription:%d:%s:window", subscriptionID, period)
	rwh.mr.Del(key)
	return nil
}

// CreateExpiredWindow 创建一个已过期的窗口（用于测试过期重建）
func (rwh *RedisWindowHelper) CreateExpiredWindow(
	subscriptionID int,
	period string,
	consumed int64,
	limit int64,
) error {
	key := fmt.Sprintf("subscription:%d:%s:window", subscriptionID, period)

	now := time.Now().Unix()
	expiredEndTime := now - 10 // 10秒前过期

	rwh.mr.HSet(key, "start_time", fmt.Sprintf("%d", expiredEndTime-3600))
	rwh.mr.HSet(key, "end_time", fmt.Sprintf("%d", expiredEndTime))
	rwh.mr.HSet(key, "consumed", fmt.Sprintf("%d", consumed))
	rwh.mr.HSet(key, "limit", fmt.Sprintf("%d", limit))

	return nil
}

// GetAllWindowFields 获取窗口的所有字段
func (rwh *RedisWindowHelper) GetAllWindowFields(subscriptionID int, period string) (map[string]string, error) {
	key := fmt.Sprintf("subscription:%d:%s:window", subscriptionID, period)
	fields, err := rwh.mr.HKeys(key)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(fields))
	for _, f := range fields {
		result[f] = rwh.mr.HGet(key, f)
	}
	return result, nil
}

// matchPattern 简单的通配符模式匹配
func matchPattern(text, pattern string) bool {
	// 简化版实现，仅支持 * 通配符
	if pattern == "*" {
		return true
	}

	// 检查前缀匹配
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(text) >= len(prefix) && text[:len(prefix)] == prefix
	}

	// 精确匹配
	return text == pattern
}

// ConcurrencyTestConfig 并发测试配置
type ConcurrencyTestConfig struct {
	GoroutineCount int           // Goroutine数量
	RequestQuota   int64         // 每次请求的quota
	HourlyLimit    int64         // 小时限额
	StaggerDelay   time.Duration // 交错延迟
	Timeout        time.Duration // 超时时间
}

// DefaultConcurrencyConfig 默认并发测试配置
func DefaultConcurrencyConfig() *ConcurrencyTestConfig {
	return &ConcurrencyTestConfig{
		GoroutineCount: 100,
		RequestQuota:   150000,   // 0.15M
		HourlyLimit:    10000000, // 10M
		StaggerDelay:   0,
		Timeout:        30 * time.Second,
	}
}

// AssertNoRaceCondition 断言无竞态条件
func AssertNoRaceCondition(
	t *testing.T,
	expectedSum int64,
	actualSum int64,
	tolerance int64,
	message string,
) {
	diff := actualSum - expectedSum
	if diff < 0 {
		diff = -diff
	}

	assert.LessOrEqual(t, diff, tolerance,
		"%s: Race condition detected! Expected sum=%d (±%d), actual sum=%d, diff=%d",
		message, expectedSum, tolerance, actualSum, diff)
}

// AssertStrictLimit 断言严格限制（用于验证不超限）
func AssertStrictLimit(
	t *testing.T,
	consumed int64,
	limit int64,
	message string,
) {
	assert.LessOrEqual(t, consumed, limit,
		"%s: Limit exceeded! Consumed=%d, Limit=%d, Exceeded by=%d",
		message, consumed, limit, consumed-limit)
}
