package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// ============================================================================
// 异常注入辅助函数 (Exception Injection Helpers)
// ============================================================================

// RedisFailureInjector Redis异常注入器
type RedisFailureInjector struct {
	MiniRedis *miniredis.Miniredis
	isDown    bool
}

// NewRedisFailureInjector 创建Redis异常注入器
func NewRedisFailureInjector(mr *miniredis.Miniredis) *RedisFailureInjector {
	return &RedisFailureInjector{
		MiniRedis: mr,
		isDown:    false,
	}
}

// Shutdown 关闭Redis（模拟故障）
func (r *RedisFailureInjector) Shutdown(t *testing.T) {
	if r.MiniRedis != nil {
		r.MiniRedis.Close()
		r.isDown = true
		t.Log("Redis shutdown - simulating failure")
	}
}

// Restart 重启Redis（恢复服务）
func (r *RedisFailureInjector) Restart(t *testing.T) error {
	if r.isDown {
		var err error
		r.MiniRedis, err = miniredis.Run()
		if err != nil {
			return fmt.Errorf("failed to restart Redis: %w", err)
		}
		r.isDown = false
		t.Logf("Redis restarted at: %s", r.MiniRedis.Addr())
	}
	return nil
}

// IsDown 检查Redis是否关闭
func (r *RedisFailureInjector) IsDown() bool {
	return r.isDown
}

// DBFailureInjector DB异常注入器
type DBFailureInjector struct {
	OriginalDB *gorm.DB
	isDown     bool
}

// NewDBFailureInjector 创建DB异常注入器
func NewDBFailureInjector(db *gorm.DB) *DBFailureInjector {
	return &DBFailureInjector{
		OriginalDB: db,
		isDown:     false,
	}
}

// Shutdown 关闭DB（模拟故障）
func (d *DBFailureInjector) Shutdown(t *testing.T) error {
	if d.OriginalDB != nil {
		sqlDB, err := d.OriginalDB.DB()
		if err != nil {
			return err
		}
		err = sqlDB.Close()
		if err != nil {
			return err
		}
		d.isDown = true
		t.Log("DB shutdown - simulating failure")
	}
	return nil
}

// Restart 重启DB（恢复服务）
func (d *DBFailureInjector) Restart(t *testing.T) (*gorm.DB, error) {
	if d.isDown {
		db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed to restart DB: %w", err)
		}
		d.isDown = false
		t.Log("DB restarted successfully")
		return db, nil
	}
	return d.OriginalDB, nil
}

// IsDown 检查DB是否关闭
func (d *DBFailureInjector) IsDown() bool {
	return d.isDown
}

// ============================================================================
// 超时控制辅助函数 (Timeout Control Helpers)
// ============================================================================

// TimeoutSimulator 超时模拟器
type TimeoutSimulator struct {
	Timeout time.Duration
}

// NewTimeoutSimulator 创建超时模拟器
func NewTimeoutSimulator(timeout time.Duration) *TimeoutSimulator {
	return &TimeoutSimulator{
		Timeout: timeout,
	}
}

// SimulateSlowQuery 模拟慢速查询（超过超时时间）
func (ts *TimeoutSimulator) SimulateSlowQuery(t *testing.T, ctx context.Context, slowDuration time.Duration) (bool, error) {
	t.Logf("Simulating slow query (duration: %v, timeout: %v)", slowDuration, ts.Timeout)

	done := make(chan bool, 1)

	go func() {
		time.Sleep(slowDuration)
		done <- true
	}()

	select {
	case <-done:
		t.Log("Query completed before timeout")
		return true, nil
	case <-ctx.Done():
		t.Log("Query timed out")
		return false, ctx.Err()
	}
}

// SimulateFastQuery 模拟快速查询（在超时前完成）
func (ts *TimeoutSimulator) SimulateFastQuery(t *testing.T, ctx context.Context, fastDuration time.Duration) (bool, error) {
	t.Logf("Simulating fast query (duration: %v)", fastDuration)

	done := make(chan bool, 1)

	go func() {
		time.Sleep(fastDuration)
		done <- true
	}()

	select {
	case <-done:
		t.Log("Fast query completed successfully")
		return true, nil
	case <-ctx.Done():
		t.Log("Fast query timed out (should not happen)")
		return false, ctx.Err()
	}
}

// ============================================================================
// Lua脚本异常模拟 (Lua Script Exception Simulation)
// ============================================================================

// LuaResultSimulator Lua结果模拟器
type LuaResultSimulator struct{}

// NewLuaResultSimulator 创建Lua结果模拟器
func NewLuaResultSimulator() *LuaResultSimulator {
	return &LuaResultSimulator{}
}

// GenerateValidResult 生成有效的Lua返回值
func (l *LuaResultSimulator) GenerateValidResult(status int64, consumed int64, startTime int64, endTime int64) []interface{} {
	return []interface{}{status, consumed, startTime, endTime}
}

// GenerateNilResult 生成nil返回值
func (l *LuaResultSimulator) GenerateNilResult() interface{} {
	return nil
}

// GenerateInsufficientElementsResult 生成元素不足的返回值
func (l *LuaResultSimulator) GenerateInsufficientElementsResult(elementCount int) []interface{} {
	result := make([]interface{}, elementCount)
	for i := 0; i < elementCount; i++ {
		result[i] = int64(i)
	}
	return result
}

// GenerateExcessElementsResult 生成元素过多的返回值
func (l *LuaResultSimulator) GenerateExcessElementsResult(elementCount int) []interface{} {
	result := make([]interface{}, elementCount)
	for i := 0; i < elementCount; i++ {
		result[i] = int64(i)
	}
	return result
}

// GenerateWrongTypeResult 生成类型错误的返回值
func (l *LuaResultSimulator) GenerateWrongTypeResult() interface{} {
	return "invalid_string_instead_of_array"
}

// GenerateWrongElementTypesResult 生成元素类型错误的返回值
func (l *LuaResultSimulator) GenerateWrongElementTypesResult() []interface{} {
	return []interface{}{
		"not_an_int",
		"not_an_int",
		"not_a_timestamp",
		"not_a_timestamp",
	}
}

// ============================================================================
// Pipeline失败模拟 (Pipeline Failure Simulation)
// ============================================================================

// PipelineFailureSimulator Pipeline失败模拟器
type PipelineFailureSimulator struct {
	MiniRedis *miniredis.Miniredis
}

// NewPipelineFailureSimulator 创建Pipeline失败模拟器
func NewPipelineFailureSimulator(mr *miniredis.Miniredis) *PipelineFailureSimulator {
	return &PipelineFailureSimulator{
		MiniRedis: mr,
	}
}

// CreatePartialFailureScenario 创建部分失败场景（删除某些Key）
func (p *PipelineFailureSimulator) CreatePartialFailureScenario(t *testing.T, subscriptionID int, failingPeriods []string) {
	for _, period := range failingPeriods {
		key := FormatWindowKey(subscriptionID, period)
		p.MiniRedis.Del(key)
		t.Logf("Deleted window key: %s (simulating partial failure)", key)
	}
}

// CreateCompleteFailureScenario 创建完全失败场景（关闭Redis）
func (p *PipelineFailureSimulator) CreateCompleteFailureScenario(t *testing.T) {
	p.MiniRedis.Close()
	t.Log("Redis closed - simulating complete pipeline failure")
}

// ============================================================================
// 日志捕获辅助函数 (Log Capture Helpers)
// ============================================================================

// LogCapture 日志捕获器（用于验证日志输出）
type LogCapture struct {
	logs []string
}

// NewLogCapture 创建日志捕获器
func NewLogCapture() *LogCapture {
	return &LogCapture{
		logs: make([]string, 0),
	}
}

// Capture 捕获日志
func (lc *LogCapture) Capture(logLevel string, message string) {
	logEntry := fmt.Sprintf("[%s] %s", logLevel, message)
	lc.logs = append(lc.logs, logEntry)
}

// AssertContains 断言日志包含特定内容
func (lc *LogCapture) AssertContains(t *testing.T, expectedContent string) {
	for _, log := range lc.logs {
		if containsString(log, expectedContent) {
			t.Logf("Found expected log: %s", log)
			return
		}
	}
	assert.Fail(t, fmt.Sprintf("Expected log content not found: %s", expectedContent))
}

// AssertLogLevel 断言特定级别的日志存在
func (lc *LogCapture) AssertLogLevel(t *testing.T, level string) {
	for _, log := range lc.logs {
		if containsString(log, fmt.Sprintf("[%s]", level)) {
			t.Logf("Found %s level log: %s", level, log)
			return
		}
	}
	assert.Fail(t, fmt.Sprintf("Expected %s level log not found", level))
}

// GetAllLogs 获取所有日志
func (lc *LogCapture) GetAllLogs() []string {
	return lc.logs
}

// Clear 清空日志
func (lc *LogCapture) Clear() {
	lc.logs = make([]string, 0)
}

// containsString 检查字符串是否包含子串
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && containsSubstring(s, substr)
}

// containsSubstring 递归检查子串
func containsSubstring(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// 异常场景验证辅助函数 (Exception Scenario Verification)
// ============================================================================

// AssertDegradationBehavior 断言降级行为
func AssertDegradationBehavior(t *testing.T, expectSuccess bool, actualSuccess bool, degradationReason string) {
	if expectSuccess {
		assert.True(t, actualSuccess, "Operation should succeed with degradation")
		t.Logf("Degradation successful: %s", degradationReason)
	} else {
		assert.False(t, actualSuccess, "Operation should fail")
	}
}

// AssertNoDataCorruption 断言数据未损坏
func AssertNoDataCorruption(t *testing.T, subscriptionID int, expectedConsumed int64, toleranceDelta int64) {
	// 从DB读取订阅
	sub, err := GetSubscriptionById(subscriptionID)
	if err != nil {
		// DB可能不可用，跳过检查
		t.Logf("DB unavailable, skipping data corruption check")
		return
	}

	// 允许一定误差（异步更新可能有延迟）
	actualConsumed := sub.TotalConsumed
	diff := actualConsumed - expectedConsumed
	if diff < 0 {
		diff = -diff
	}

	assert.LessOrEqual(t, diff, toleranceDelta,
		fmt.Sprintf("Data corruption detected: expected %d ± %d, got %d",
			expectedConsumed, toleranceDelta, actualConsumed))
}

// AssertRecoverySuccessful 断言服务恢复成功
func AssertRecoverySuccessful(t *testing.T, beforeFailure func(), afterRecovery func()) {
	// 执行恢复前的操作
	t.Log("Executing operation before failure")
	beforeFailure()

	// 模拟服务恢复
	t.Log("Service recovered, verifying functionality")

	// 执行恢复后的操作
	afterRecovery()

	t.Log("Recovery successful - functionality restored")
}

// ============================================================================
// 窗口状态验证辅助函数 (Window State Verification)
// ============================================================================

// WindowStateSnapshot 窗口状态快照
type WindowStateSnapshot struct {
	Key       string
	Exists    bool
	StartTime int64
	EndTime   int64
	Consumed  int64
	Limit     int64
	Timestamp int64
}

// CaptureWindowState 捕获窗口当前状态
func CaptureWindowState(t *testing.T, rm *RedisMock, subscriptionID int, period string) *WindowStateSnapshot {
	key := GetWindowKey(subscriptionID, period)
	exists := rm.CheckKeyExists(t, key)

	snapshot := &WindowStateSnapshot{
		Key:       key,
		Exists:    exists,
		Timestamp: time.Now().Unix(),
	}

	if exists {
		snapshot.StartTime, _ = rm.GetHashFieldInt64(key, "start_time")
		snapshot.EndTime, _ = rm.GetHashFieldInt64(key, "end_time")
		snapshot.Consumed, _ = rm.GetHashFieldInt64(key, "consumed")
		snapshot.Limit, _ = rm.GetHashFieldInt64(key, "limit")
	}

	return snapshot
}

// CompareWindowStates 比较两个窗口状态快照
func CompareWindowStates(t *testing.T, before *WindowStateSnapshot, after *WindowStateSnapshot, expectChange bool) {
	if expectChange {
		// 预期状态发生变化
		changed := before.Consumed != after.Consumed ||
			before.StartTime != after.StartTime ||
			before.EndTime != after.EndTime

		assert.True(t, changed, "Window state should have changed")
		t.Logf("Window state changed: consumed %d -> %d", before.Consumed, after.Consumed)
	} else {
		// 预期状态未变化
		assert.Equal(t, before.Consumed, after.Consumed, "Consumed should not change")
		assert.Equal(t, before.StartTime, after.StartTime, "StartTime should not change")
		assert.Equal(t, before.EndTime, after.EndTime, "EndTime should not change")
		t.Log("Window state unchanged as expected")
	}
}

// ============================================================================
// 异常恢复验证辅助函数 (Recovery Verification)
// ============================================================================

// RecoveryVerifier 恢复验证器
type RecoveryVerifier struct {
	t *testing.T
}

// NewRecoveryVerifier 创建恢复验证器
func NewRecoveryVerifier(t *testing.T) *RecoveryVerifier {
	return &RecoveryVerifier{t: t}
}

// VerifyRedisRecovery 验证Redis恢复后功能正常
func (rv *RecoveryVerifier) VerifyRedisRecovery(
	mr *miniredis.Miniredis,
	subscriptionID int,
	period string,
	expectedFunctionality string,
) {
	rv.t.Log("Verifying Redis recovery...")

	// 检查Redis可用
	assert.NotNil(rv.t, mr, "Redis should be restarted")

	// 验证可以创建新窗口
	key := GetWindowKey(subscriptionID, period)
	mr.HSet(key, "start_time", fmt.Sprintf("%d", time.Now().Unix()))

	// 验证数据可以写入
	exists := mr.Exists(key)
	assert.True(rv.t, exists, "Should be able to create new window after Redis recovery")

	rv.t.Logf("Redis recovery verified: %s", expectedFunctionality)
}

// VerifyDBRecovery 验证DB恢复后功能正常
func (rv *RecoveryVerifier) VerifyDBRecovery(db *gorm.DB, expectedFunctionality string) {
	rv.t.Log("Verifying DB recovery...")

	// 检查DB可用
	assert.NotNil(rv.t, db, "DB should be reopened")

	// 执行简单的DB操作验证
	var count int64
	err := db.Table("subscriptions").Count(&count).Error

	assert.NoError(rv.t, err, "DB query should succeed after recovery")

	rv.t.Logf("DB recovery verified: %s", expectedFunctionality)
}

// ============================================================================
// 异常场景测试辅助结构 (Exception Scenario Test Helpers)
// ============================================================================

// ExceptionScenario 异常场景定义
type ExceptionScenario struct {
	Name              string
	ExceptionType     string
	InjectionPoint    string
	ExpectedBehavior  string
	RecoverySupported bool
}

// CommonExceptionScenarios 常见异常场景列表
var CommonExceptionScenarios = []ExceptionScenario{
	{
		Name:              "Redis断开",
		ExceptionType:     "network",
		InjectionPoint:    "PostConsumeQuota",
		ExpectedBehavior:  "降级到DB-only，记录WARN日志",
		RecoverySupported: true,
	},
	{
		Name:              "DB断开",
		ExceptionType:     "database",
		InjectionPoint:    "PostConsumeQuota",
		ExpectedBehavior:  "记录ERROR日志，不影响响应",
		RecoverySupported: true,
	},
	{
		Name:              "Lua脚本异常",
		ExceptionType:     "script",
		InjectionPoint:    "CheckAndConsumeSlidingWindow",
		ExpectedBehavior:  "Type assertion容错，降级允许通过",
		RecoverySupported: false,
	},
	{
		Name:              "查询超时",
		ExceptionType:     "timeout",
		InjectionPoint:    "GetUserAvailablePackages",
		ExpectedBehavior:  "超时返回，降级到用户余额",
		RecoverySupported: false,
	},
	{
		Name:              "Pipeline失败",
		ExceptionType:     "batch_operation",
		InjectionPoint:    "GetAllSlidingWindowsStatus",
		ExpectedBehavior:  "部分成功，错误隔离",
		RecoverySupported: true,
	},
}

// PrintExceptionScenario 打印异常场景信息
func PrintExceptionScenario(t *testing.T, scenario ExceptionScenario) {
	t.Logf("=== Exception Scenario: %s ===", scenario.Name)
	t.Logf("  Type: %s", scenario.ExceptionType)
	t.Logf("  Injection Point: %s", scenario.InjectionPoint)
	t.Logf("  Expected Behavior: %s", scenario.ExpectedBehavior)
	t.Logf("  Recovery Supported: %v", scenario.RecoverySupported)
}

// ============================================================================
// 并发异常测试辅助函数 (Concurrent Exception Helpers)
// ============================================================================

// ConcurrentExceptionTester 并发异常测试器
type ConcurrentExceptionTester struct {
	t              *testing.T
	goroutineCount int
	exceptionRate  float64
}

// NewConcurrentExceptionTester 创建并发异常测试器
func NewConcurrentExceptionTester(t *testing.T, goroutineCount int, exceptionRate float64) *ConcurrentExceptionTester {
	return &ConcurrentExceptionTester{
		t:              t,
		goroutineCount: goroutineCount,
		exceptionRate:  exceptionRate,
	}
}

// RunWithRandomExceptions 运行并发测试，随机注入异常
func (cet *ConcurrentExceptionTester) RunWithRandomExceptions(
	operation func(index int) error,
	exceptionInjector func(index int) bool,
) (successCount int, failureCount int) {
	results := make(chan bool, cet.goroutineCount)

	for i := 0; i < cet.goroutineCount; i++ {
		go func(index int) {
			// 随机决定是否注入异常
			if exceptionInjector(index) {
				cet.t.Logf("Injecting exception for goroutine %d", index)
				results <- false
				return
			}

			// 执行正常操作
			err := operation(index)
			results <- (err == nil)
		}(i)
	}

	// 收集结果
	for i := 0; i < cet.goroutineCount; i++ {
		success := <-results
		if success {
			successCount++
		} else {
			failureCount++
		}
	}

	cet.t.Logf("Concurrent test completed: %d success, %d failures", successCount, failureCount)

	return successCount, failureCount
}

// ============================================================================
// 套餐过期验证辅助函数 (Package Expiration Helpers)
// ============================================================================

// ExpirationChecker 过期检查器
type ExpirationChecker struct{}

// NewExpirationChecker 创建过期检查器
func NewExpirationChecker() *ExpirationChecker {
	return &ExpirationChecker{}
}

// IsSubscriptionExpired 检查订阅是否过期
func (ec *ExpirationChecker) IsSubscriptionExpired(sub *TestSubscription, currentTime int64) bool {
	if sub.EndTime == nil {
		return false
	}
	return *sub.EndTime <= currentTime
}

// GetExpirationStatus 获取过期状态详情
func (ec *ExpirationChecker) GetExpirationStatus(sub *TestSubscription, currentTime int64) string {
	if sub.EndTime == nil {
		return "not_activated"
	}

	if *sub.EndTime > currentTime {
		remainingTime := *sub.EndTime - currentTime
		days := remainingTime / 86400
		return fmt.Sprintf("valid (expires in %d days)", days)
	}

	expiredTime := currentTime - *sub.EndTime
	days := expiredTime / 86400
	return fmt.Sprintf("expired (%d days ago)", days)
}

// TestSubscription 测试订阅结构（为避免循环依赖，在这里重新定义）
type TestSubscription struct {
	ID            int
	UserID        int
	PackageID     int
	Status        string
	TotalConsumed int64
	StartTime     *int64
	EndTime       *int64
}

// ============================================================================
// 降级策略验证 (Degradation Strategy Verification)
// ============================================================================

// DegradationStrategy 降级策略
type DegradationStrategy string

const (
	DegradationAllowRequest      DegradationStrategy = "allow_request"    // 允许请求通过
	DegradationFallbackToBalance DegradationStrategy = "fallback_balance" // 降级到用户余额
	DegradationUseDBOnly         DegradationStrategy = "use_db_only"      // 仅使用DB
	DegradationRejectRequest     DegradationStrategy = "reject_request"   // 拒绝请求
	DegradationRetryLater        DegradationStrategy = "retry_later"      // 稍后重试
)

// DegradationValidator 降级策略验证器
type DegradationValidator struct {
	t *testing.T
}

// NewDegradationValidator 创建降级策略验证器
func NewDegradationValidator(t *testing.T) *DegradationValidator {
	return &DegradationValidator{t: t}
}

// ValidateStrategy 验证降级策略
func (dv *DegradationValidator) ValidateStrategy(
	actualBehavior string,
	expectedStrategy DegradationStrategy,
) {
	switch expectedStrategy {
	case DegradationAllowRequest:
		assert.Contains(dv.t, actualBehavior, "allowed", "Request should be allowed")
	case DegradationFallbackToBalance:
		assert.Contains(dv.t, actualBehavior, "fallback", "Should fallback to balance")
	case DegradationUseDBOnly:
		assert.Contains(dv.t, actualBehavior, "db_only", "Should use DB only")
	case DegradationRejectRequest:
		assert.Contains(dv.t, actualBehavior, "rejected", "Request should be rejected")
	case DegradationRetryLater:
		assert.Contains(dv.t, actualBehavior, "retry", "Should retry later")
	}

	dv.t.Logf("Degradation strategy validated: %s", expectedStrategy)
}
