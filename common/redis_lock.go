package common

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis distributed lock implementation
// Phase 10.3: GS3-1 分布式锁支持多节点部署下的并发控制

// AcquireLock 获取Redis分布式锁
// 使用 SET key value NX EX expiration 原子命令确保锁的唯一性和过期清理
// 参数:
//   - key: 锁的键名（建议格式：lock:resource_type:resource_id）
//   - value: 锁的值（建议使用唯一标识符，如节点ID+goroutine ID）
//   - expiration: 锁的过期时间，防止持锁进程崩溃导致死锁
//
// 返回:
//   - success: true表示成功获取锁，false表示锁已被其他进程持有
//   - err: Redis操作错误（nil表示无错误）
func AcquireLock(key string, value string, expiration time.Duration) (success bool, err error) {
	if !RedisEnabled {
		// Redis未启用时降级：返回成功，依赖单节点并发控制
		SysLog(fmt.Sprintf("Redis not enabled, skipping distributed lock for key: %s", key))
		return true, nil
	}

	ctx := context.Background()

	// 使用 SET NX (Not eXists) EX (EXpiration) 原子命令
	// NX: 只有键不存在时才设置
	// EX: 设置过期时间（秒）
	result, err := RDB.SetNX(ctx, key, value, expiration).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock %s: %w", key, err)
	}

	if result {
		// 成功获取锁
		SysLog(fmt.Sprintf("Lock acquired: %s (value: %s, expiration: %v)", key, value, expiration))
	}

	return result, nil
}

// ReleaseLock 释放Redis分布式锁
// 使用Lua脚本确保只有锁的持有者才能释放锁，防止误删其他进程的锁
// 参数:
//   - key: 锁的键名
//   - value: 锁的值（必须与AcquireLock时使用的值一致）
//
// 返回:
//   - released: true表示成功释放锁，false表示锁不存在或值不匹配
//   - err: Redis操作错误（nil表示无错误）
func ReleaseLock(key string, value string) (released bool, err error) {
	if !RedisEnabled {
		// Redis未启用时降级：返回成功
		return true, nil
	}

	ctx := context.Background()

	// Lua脚本：原子性地检查value并删除key
	// 只有当key存在且其值等于传入的value时才删除
	// 返回值：1表示删除成功，0表示key不存在或值不匹配
	luaScript := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`

	result, err := RDB.Eval(ctx, luaScript, []string{key}, value).Result()
	if err != nil {
		// 处理NOSCRIPT错误（Redis重启后脚本缓存丢失）
		if errors.Is(err, redis.Nil) || err.Error() == "NOSCRIPT No matching script. Please use EVAL." {
			// 重试一次
			result, err = RDB.Eval(ctx, luaScript, []string{key}, value).Result()
			if err != nil {
				return false, fmt.Errorf("failed to release lock %s (retry): %w", key, err)
			}
		} else {
			return false, fmt.Errorf("failed to release lock %s: %w", key, err)
		}
	}

	// result为int64类型，1表示成功删除，0表示未删除
	deleted := result.(int64) == 1

	if deleted {
		SysLog(fmt.Sprintf("Lock released: %s (value: %s)", key, value))
	} else {
		SysLog(fmt.Sprintf("Lock release failed: %s (value mismatch or key not found)", key))
	}

	return deleted, nil
}

// ExtendLock 延长锁的过期时间
// 用于长时间运行的任务，避免任务未完成时锁自动过期
// 参数:
//   - key: 锁的键名
//   - value: 锁的值（必须与AcquireLock时使用的值一致）
//   - extension: 延长的时间
//
// 返回:
//   - extended: true表示成功延长，false表示锁不存在或值不匹配
//   - err: Redis操作错误（nil表示无错误）
func ExtendLock(key string, value string, extension time.Duration) (extended bool, err error) {
	if !RedisEnabled {
		return true, nil
	}

	ctx := context.Background()

	// Lua脚本：原子性地检查value并延长过期时间
	luaScript := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("EXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	expirationSeconds := int(extension.Seconds())
	result, err := RDB.Eval(ctx, luaScript, []string{key}, value, expirationSeconds).Result()
	if err != nil {
		return false, fmt.Errorf("failed to extend lock %s: %w", key, err)
	}

	success := result.(int64) == 1
	if success {
		SysLog(fmt.Sprintf("Lock extended: %s (extension: %v)", key, extension))
	}

	return success, nil
}

// TryAcquireLockWithRetry 尝试获取锁，失败时自动重试
// 参数:
//   - key: 锁的键名
//   - value: 锁的值
//   - expiration: 锁的过期时间
//   - maxRetries: 最大重试次数
//   - retryInterval: 重试间隔
//
// 返回:
//   - success: true表示成功获取锁
//   - err: Redis操作错误或达到最大重试次数
func TryAcquireLockWithRetry(key string, value string, expiration time.Duration, maxRetries int, retryInterval time.Duration) (success bool, err error) {
	for i := 0; i <= maxRetries; i++ {
		acquired, err := AcquireLock(key, value, expiration)
		if err != nil {
			return false, err
		}

		if acquired {
			return true, nil
		}

		// 未获取到锁，等待后重试
		if i < maxRetries {
			time.Sleep(retryInterval)
		}
	}

	return false, fmt.Errorf("failed to acquire lock %s after %d retries", key, maxRetries)
}
