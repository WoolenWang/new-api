package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
)

// RedisMock 封装miniredis用于测试
type RedisMock struct {
	Server *miniredis.Miniredis
	Client *redis.Client
}

// NewRedisMockFromMiniRedis 使用已有的 miniredis 实例创建 RedisMock。
// 该方法不会修改 common.RDB / common.RedisEnabled，全局 Redis 配置由
// 调用方（例如独立的测试服务器进程）自行管理。
func NewRedisMockFromMiniRedis(t *testing.T, mr *miniredis.Miniredis) *RedisMock {
	if mr == nil {
		t.Fatalf("NewRedisMockFromMiniRedis: miniredis instance is nil")
	}

	// 仅为测试进程创建一个客户端，用于读取/断言窗口状态。
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	ctx := context.Background()
	if _, err := client.Ping(ctx).Result(); err != nil {
		t.Fatalf("NewRedisMockFromMiniRedis: failed to connect to miniredis: %v", err)
	}

	return &RedisMock{
		Server: mr,
		Client: client,
	}
}

// StartRedisMock 启动miniredis实例并初始化Redis客户端
func StartRedisMock(t *testing.T) *RedisMock {
	// 启动miniredis
	mr, err := miniredis.Run()
	assert.Nil(t, err, "Failed to start miniredis")

	// 创建Redis客户端
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	// 测试连接
	ctx := context.Background()
	_, err = client.Ping(ctx).Result()
	assert.Nil(t, err, "Failed to connect to miniredis")

	// 设置全局Redis客户端（供被测代码使用）
	common.RDB = client
	common.RedisEnabled = true

	// 重置滑动窗口 Lua 脚本缓存，避免在不同测试用例之间复用过期的 script SHA
	service.ResetSlidingWindowScriptCache()

	return &RedisMock{
		Server: mr,
		Client: client,
	}
}

// Close 关闭Redis mock
func (rm *RedisMock) Close() {
	if rm.Client != nil {
		rm.Client.Close()
	}
	if rm.Server != nil {
		rm.Server.Close()
	}
	common.RedisEnabled = false
	common.RDB = nil
}

// Reset 重置Redis数据（清空所有key）
func (rm *RedisMock) Reset() {
	rm.Server.FlushAll()
}

// FastForward 快进时间（用于测试TTL和窗口过期）
func (rm *RedisMock) FastForward(duration time.Duration) {
	rm.Server.FastForward(duration)
}

// CheckKeyExists 检查Redis Key是否存在
func (rm *RedisMock) CheckKeyExists(t *testing.T, key string) bool {
	return rm.Server.Exists(key)
}

// AssertKeyExists 断言Redis Key存在
func (rm *RedisMock) AssertKeyExists(t *testing.T, key string) {
	exists := rm.Server.Exists(key)
	assert.True(t, exists, fmt.Sprintf("Redis key '%s' should exist", key))
}

// AssertKeyNotExists 断言Redis Key不存在
func (rm *RedisMock) AssertKeyNotExists(t *testing.T, key string) {
	exists := rm.Server.Exists(key)
	assert.False(t, exists, fmt.Sprintf("Redis key '%s' should not exist", key))
}

// GetHashField 获取Hash字段值
func (rm *RedisMock) GetHashField(key string, field string) (string, error) {
	return rm.Server.HGet(key, field), nil
}

// GetHashFieldInt64 获取Hash字段的int64值
func (rm *RedisMock) GetHashFieldInt64(key string, field string) (int64, error) {
	val := rm.Server.HGet(key, field)
	if val == "" {
		return 0, fmt.Errorf("hash field %s:%s not found", key, field)
	}
	var result int64
	_, err := fmt.Sscanf(val, "%d", &result)
	return result, err
}

// AssertHashField 断言Hash字段值
func (rm *RedisMock) AssertHashField(t *testing.T, key string, field string, expectedValue string) {
	val := rm.Server.HGet(key, field)
	assert.Equal(t, expectedValue, val, fmt.Sprintf("Hash field %s:%s should be %s", key, field, expectedValue))
}

// AssertHashFieldInt64 断言Hash字段的int64值
func (rm *RedisMock) AssertHashFieldInt64(t *testing.T, key string, field string, expectedValue int64) {
	val, err := rm.GetHashFieldInt64(key, field)
	assert.Nil(t, err, fmt.Sprintf("Failed to get hash field %s:%s", key, field))
	assert.Equal(t, expectedValue, val, fmt.Sprintf("Hash field %s:%s should be %d", key, field, expectedValue))
}

// GetHashAllFields 获取Hash的所有字段
func (rm *RedisMock) GetHashAllFields(key string) (map[string]string, error) {
	fields, err := rm.Server.HKeys(key)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(fields))
	for _, f := range fields {
		result[f] = rm.Server.HGet(key, f)
	}
	return result, nil
}

// GetTTL 获取Key的TTL（秒）
func (rm *RedisMock) GetTTL(key string) time.Duration {
	return rm.Server.TTL(key)
}

// AssertTTL 断言Key的TTL（允许误差）
func (rm *RedisMock) AssertTTL(t *testing.T, key string, expectedTTL time.Duration, delta time.Duration) {
	actualTTL := rm.Server.TTL(key)
	diff := actualTTL - expectedTTL
	if diff < 0 {
		diff = -diff
	}
	assert.True(t, diff <= delta,
		fmt.Sprintf("TTL of key '%s' should be around %v (actual: %v, delta: %v)", key, expectedTTL, actualTTL, delta))
}

// DumpKey 打印Key的详细信息（用于调试）
func (rm *RedisMock) DumpKey(t *testing.T, key string) {
	if !rm.Server.Exists(key) {
		t.Logf("Redis key '%s' does not exist", key)
		return
	}

	fields, err := rm.GetHashAllFields(key)
	if err != nil {
		t.Logf("Failed to dump key '%s': %v", key, err)
		return
	}

	ttl := rm.Server.TTL(key)
	t.Logf("Redis key '%s' (TTL: %v):", key, ttl)
	for field, value := range fields {
		t.Logf("  %s = %s", field, value)
	}
}

// LoadLuaScript 加载Lua脚本到Redis（模拟service层的scriptSHA初始化）
// 返回脚本的SHA值
func (rm *RedisMock) LoadLuaScript(t *testing.T, script string) string {
	ctx := context.Background()
	sha, err := rm.Client.ScriptLoad(ctx, script).Result()
	assert.Nil(t, err, "Failed to load Lua script")
	return sha
}

// SetHashField sets a hash field value in miniredis.
func (rm *RedisMock) SetHashField(key, field, value string) {
	if rm.Server == nil {
		return
	}
	rm.Server.HSet(key, field, value)
}

// SetExpire sets key TTL in miniredis.
func (rm *RedisMock) SetExpire(key string, ttl time.Duration) {
	if rm.Server == nil {
		return
	}
	rm.Server.SetTTL(key, ttl)
}
