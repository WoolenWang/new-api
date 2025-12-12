package testutil

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// WindowState 窗口状态数据结构（用于预设窗口状态）
type WindowState struct {
	StartTime int64
	EndTime   int64
	Consumed  int64
	Limit     int64
}

// PresetWindowState 预设窗口状态（手动设置Redis Hash，用于边界测试）
// 这个函数允许测试代码在测试开始前就设置好窗口的初始状态，
// 例如设置consumed接近limit来测试边界条件
func PresetWindowState(
	t *testing.T,
	rm *RedisMock,
	subscriptionId int,
	period string,
	state WindowState,
) {
	key := GetWindowKey(subscriptionId, period)

	// 设置Hash的所有字段
	rm.Server.HSet(key, "start_time", fmt.Sprintf("%d", state.StartTime))
	rm.Server.HSet(key, "end_time", fmt.Sprintf("%d", state.EndTime))
	rm.Server.HSet(key, "consumed", fmt.Sprintf("%d", state.Consumed))
	rm.Server.HSet(key, "limit", fmt.Sprintf("%d", state.Limit))

	// 根据period设置合适的TTL
	var ttl time.Duration
	switch period {
	case "rpm":
		ttl = 90 * time.Second
	case "hourly":
		ttl = 4200 * time.Second
	case "4hourly":
		ttl = 18000 * time.Second
	case "daily":
		ttl = 93600 * time.Second
	case "weekly":
		ttl = 691200 * time.Second
	default:
		ttl = 3600 * time.Second
	}

	rm.Server.SetTTL(key, ttl)

	t.Logf("Preset window state: key=%s, start=%d, end=%d, consumed=%d, limit=%d",
		key, state.StartTime, state.EndTime, state.Consumed, state.Limit)
}

// WindowExists 检查窗口是否存在（用于边界测试）
func WindowExists(rm *RedisMock, subscriptionId int, period string) bool {
	key := GetWindowKey(subscriptionId, period)
	return rm.Server.Exists(key)
}

// DeleteWindow 删除窗口（用于测试窗口重建逻辑）
func DeleteWindow(rm *RedisMock, subscriptionId int, period string) {
	key := GetWindowKey(subscriptionId, period)
	rm.Server.Del(key)
}

// GetWindowState 获取窗口的完整状态（用于验证）
func GetWindowState(t *testing.T, rm *RedisMock, subscriptionId int, period string) WindowState {
	key := GetWindowKey(subscriptionId, period)

	startTime, err := rm.GetHashFieldInt64(key, "start_time")
	if err != nil {
		t.Logf("Warning: failed to get start_time for key %s: %v", key, err)
		startTime = 0
	}

	endTime, err := rm.GetHashFieldInt64(key, "end_time")
	if err != nil {
		t.Logf("Warning: failed to get end_time for key %s: %v", key, err)
		endTime = 0
	}

	consumed, err := rm.GetHashFieldInt64(key, "consumed")
	if err != nil {
		t.Logf("Warning: failed to get consumed for key %s: %v", key, err)
		consumed = 0
	}

	limit, err := rm.GetHashFieldInt64(key, "limit")
	if err != nil {
		t.Logf("Warning: failed to get limit for key %s: %v", key, err)
		limit = 0
	}

	return WindowState{
		StartTime: startTime,
		EndTime:   endTime,
		Consumed:  consumed,
		Limit:     limit,
	}
}

// AssertWindowStateMatch 断言窗口状态匹配
func AssertWindowStateMatch(t *testing.T, rm *RedisMock, subscriptionId int, period string, expectedState WindowState) {
	actualState := GetWindowState(t, rm, subscriptionId, period)

	if expectedState.StartTime != 0 {
		assert.Equal(t, expectedState.StartTime, actualState.StartTime, "Start time should match")
	}

	if expectedState.EndTime != 0 {
		assert.Equal(t, expectedState.EndTime, actualState.EndTime, "End time should match")
	}

	if expectedState.Consumed != 0 || expectedState.Consumed == 0 {
		assert.Equal(t, expectedState.Consumed, actualState.Consumed, "Consumed should match")
	}

	if expectedState.Limit != 0 {
		assert.Equal(t, expectedState.Limit, actualState.Limit, "Limit should match")
	}
}

// CreateExpiredWindow 创建一个已过期的窗口（用于测试窗口过期重建）
func CreateExpiredWindow(
	t *testing.T,
	rm *RedisMock,
	subscriptionId int,
	period string,
	consumed int64,
	limit int64,
) {
	now := time.Now().Unix()

	// 创建一个end_time已经过去的窗口
	state := WindowState{
		StartTime: now - 3700, // 61分钟前开始
		EndTime:   now - 100,  // 100秒前结束（已过期）
		Consumed:  consumed,
		Limit:     limit,
	}

	PresetWindowState(t, rm, subscriptionId, period, state)

	t.Logf("Created expired window: end_time=%d, now=%d, expired=%d seconds ago",
		state.EndTime, now, now-state.EndTime)
}

// CreateAlmostExpiredWindow 创建一个即将过期的窗口（用于测试边界）
func CreateAlmostExpiredWindow(
	t *testing.T,
	rm *RedisMock,
	subscriptionId int,
	period string,
	secondsLeft int64,
	consumed int64,
	limit int64,
) {
	now := time.Now().Unix()

	// 创建一个还有secondsLeft秒才过期的窗口
	duration := int64(3600) // 默认1小时
	if period == "rpm" {
		duration = 60
	} else if period == "daily" {
		duration = 86400
	}

	state := WindowState{
		StartTime: now - duration + secondsLeft, // 设置start_time使得窗口还有secondsLeft秒
		EndTime:   now + secondsLeft,            // end_time = now + secondsLeft
		Consumed:  consumed,
		Limit:     limit,
	}

	PresetWindowState(t, rm, subscriptionId, period, state)

	t.Logf("Created almost expired window: end_time=%d, now=%d, %d seconds left",
		state.EndTime, now, secondsLeft)
}

// CreateAlmostFullWindow 创建一个几乎用尽的窗口（用于测试限额边界）
func CreateAlmostFullWindow(
	t *testing.T,
	rm *RedisMock,
	subscriptionId int,
	period string,
	limit int64,
	quotaLeft int64,
) {
	now := time.Now().Unix()

	state := WindowState{
		StartTime: now,
		EndTime:   now + 3600,
		Consumed:  limit - quotaLeft, // 已消耗 = 限额 - 剩余
		Limit:     limit,
	}

	PresetWindowState(t, rm, subscriptionId, period, state)

	t.Logf("Created almost full window: consumed=%d, limit=%d, quota_left=%d",
		state.Consumed, state.Limit, quotaLeft)
}
