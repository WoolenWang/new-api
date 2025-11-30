package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	// UserGroupsCacheKeyPrefix Redis缓存Key前缀
	UserGroupsCacheKeyPrefix = "user_groups:"
	// UserGroupsCacheTTL Redis缓存过期时间 (30分钟)
	UserGroupsCacheTTL = 30 * time.Minute
)

// GetUserActiveGroups 获取用户的所有Active P2P分组ID (带缓存)
// 该函数实现三级缓存策略:
// 1. L1 (内存): 检查 UserCache 中的 ExtendedGroups 字段
// 2. L2 (Redis): 查询 Redis Key: user_groups:{user_id}
// 3. L3 (DB): 查询 user_groups 表
func GetUserActiveGroups(userId int, fromDB bool) ([]int, error) {
	var groupIds []int
	var err error

	// L1: 尝试从内存缓存读取
	if !fromDB && common.MemoryCacheEnabled {
		userCache, cacheErr := getUserCache(userId)
		if cacheErr == nil && userCache != nil && len(userCache.ExtendedGroups) >= 0 {
			// 命中内存缓存
			return userCache.ExtendedGroups, nil
		}
	}

	// L2: 尝试从Redis缓存读取
	if !fromDB && common.RedisEnabled {
		groupIds, err = getUserGroupsCache(userId)
		if err == nil {
			// 命中Redis缓存,异步回填内存缓存
			if common.MemoryCacheEnabled {
				gopool.Go(func() {
					_ = updateUserGroupsInMemoryCache(userId, groupIds)
				})
			}
			return groupIds, nil
		}
		// Redis未命中,继续查数据库
	}

	// L3: 从数据库查询
	groupIds, err = GetUserActiveGroupIds(userId)
	if err != nil {
		return nil, err
	}

	// 异步回填缓存
	if common.RedisEnabled {
		gopool.Go(func() {
			if writeErr := setUserGroupsCache(userId, groupIds); writeErr != nil {
				common.SysLog(fmt.Sprintf("failed to update user groups cache: user_id=%d, error=%v", userId, writeErr))
			}
		})
	}

	if common.MemoryCacheEnabled {
		gopool.Go(func() {
			_ = updateUserGroupsInMemoryCache(userId, groupIds)
		})
	}

	return groupIds, nil
}

// InvalidateUserGroupCache 使用户分组缓存失效
// 应在用户加入/退出分组时调用
func InvalidateUserGroupCache(userId int) error {
	var lastErr error

	// 清除Redis缓存
	if common.RedisEnabled {
		if err := deleteUserGroupsCache(userId); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate Redis user groups cache: user_id=%d, error=%v", userId, err))
			lastErr = err
		}
	}

	// 清除内存缓存
	if common.MemoryCacheEnabled {
		if err := invalidateUserGroupsInMemoryCache(userId); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate memory user groups cache: user_id=%d, error=%v", userId, err))
			lastErr = err
		}
	}

	return lastErr
}

// ========== Redis Cache Operations ==========

// getUserGroupsCache 从Redis获取用户分组列表
func getUserGroupsCache(userId int) ([]int, error) {
	if !common.RedisEnabled {
		return nil, errors.New("Redis is not enabled")
	}

	key := fmt.Sprintf("%s%d", UserGroupsCacheKeyPrefix, userId)
	val, err := common.RedisGet(key)
	if err != nil {
		return nil, err
	}

	if val == "" {
		return nil, errors.New("cache miss")
	}

	var groupIds []int
	if err := json.Unmarshal([]byte(val), &groupIds); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user groups: %w", err)
	}

	return groupIds, nil
}

// setUserGroupsCache 设置用户分组列表到Redis
func setUserGroupsCache(userId int, groupIds []int) error {
	if !common.RedisEnabled {
		return errors.New("Redis is not enabled")
	}

	key := fmt.Sprintf("%s%d", UserGroupsCacheKeyPrefix, userId)
	data, err := json.Marshal(groupIds)
	if err != nil {
		return fmt.Errorf("failed to marshal user groups: %w", err)
	}

	return common.RedisSet(key, string(data), UserGroupsCacheTTL)
}

// deleteUserGroupsCache 删除用户分组缓存
func deleteUserGroupsCache(userId int) error {
	if !common.RedisEnabled {
		return errors.New("Redis is not enabled")
	}

	key := fmt.Sprintf("%s%d", UserGroupsCacheKeyPrefix, userId)
	return common.RedisDel(key)
}

// ========== Memory Cache Operations ==========

// updateUserGroupsInMemoryCache 更新内存缓存中的用户分组
func updateUserGroupsInMemoryCache(userId int, groupIds []int) error {
	if !common.MemoryCacheEnabled {
		return errors.New("memory cache is not enabled")
	}

	userCache, err := getUserCache(userId)
	if err != nil {
		// 如果缓存不存在,创建新的缓存条目
		user, dbErr := GetUserById(userId, false)
		if dbErr != nil {
			return dbErr
		}
		userCache = user.ToBaseUser()
	}

	userCache.ExtendedGroups = groupIds
	return setUserCache(userId, userCache)
}

// invalidateUserGroupsInMemoryCache 清除内存缓存中的用户分组
func invalidateUserGroupsInMemoryCache(userId int) error {
	if !common.MemoryCacheEnabled {
		return errors.New("memory cache is not enabled")
	}

	// 方式1: 直接删除整个用户缓存
	return invalidateUserCache(userId)

	// 方式2: 仅清空ExtendedGroups字段 (保留其他缓存数据)
	// userCache, err := getUserCache(userId)
	// if err != nil {
	// 	return err
	// }
	// userCache.ExtendedGroups = nil
	// return setUserCache(userId, userCache)
}

// BatchInvalidateUserGroupCache 批量使用户分组缓存失效
// 用于分组被删除时,使所有成员的缓存失效
func BatchInvalidateUserGroupCache(userIds []int) error {
	var lastErr error
	for _, userId := range userIds {
		if err := InvalidateUserGroupCache(userId); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate user group cache in batch: user_id=%d, error=%v", userId, err))
			lastErr = err
		}
	}
	return lastErr
}

// InvalidateGroupMembersCache 使分组所有成员的缓存失效
// 用于分组被删除或分组权限发生变更时
func InvalidateGroupMembersCache(groupId int) error {
	// 获取分组的所有成员
	members, err := GetGroupMembers(groupId)
	if err != nil {
		return err
	}

	// 提取用户ID列表
	userIds := make([]int, 0, len(members))
	for _, member := range members {
		userIds = append(userIds, member.UserId)
	}

	// 批量使缓存失效
	return BatchInvalidateUserGroupCache(userIds)
}
