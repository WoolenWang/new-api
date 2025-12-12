package model

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	// use common.SysLog / common.SysError for logging to keep
	// package cache aligned with the rest of the system logging.
)

// ============================================
// 套餐信息三级缓存实现
// L1 内存 + L2 Redis + L3 DB
// 相关设计：docs/NewAPI-支持多种包月套餐-优化版.md 第 8.3 节
// ============================================

// PackageCacheConfig 套餐缓存配置
type PackageCacheConfig struct {
	L1TTL time.Duration // L1 内存缓存 TTL（默认 1 分钟）
	L2TTL time.Duration // L2 Redis 缓存 TTL（默认 10 分钟）
}

// 默认缓存配置
var DefaultPackageCacheConfig = PackageCacheConfig{
	L1TTL: 1 * time.Minute,
	L2TTL: 10 * time.Minute,
}

// packageCacheEntry L1 内存缓存条目
type packageCacheEntry struct {
	Data      *Package
	ExpiredAt time.Time
}

// subscriptionCacheEntry L1 内存缓存条目
type subscriptionCacheEntry struct {
	Data      *Subscription
	ExpiredAt time.Time
}

// PackageCache 套餐缓存管理器（单例模式）
type PackageCache struct {
	config PackageCacheConfig

	// L1 内存缓存
	l1Packages      sync.Map // map[int]*packageCacheEntry
	l1Subscriptions sync.Map // map[int]*subscriptionCacheEntry

	// 统计信息
	stats struct {
		L1Hits   uint64
		L1Misses uint64
		L2Hits   uint64
		L2Misses uint64
		DBHits   uint64
	}
	statsMu sync.RWMutex
}

// 全局缓存实例
var packageCache *PackageCache
var packageCacheOnce sync.Once

// GetPackageCache 获取套餐缓存管理器（单例）
func GetPackageCache() *PackageCache {
	packageCacheOnce.Do(func() {
		packageCache = &PackageCache{
			config: DefaultPackageCacheConfig,
		}
		go packageCache.startEvictionLoop()
	})
	return packageCache
}

// ResetPackageCacheForTests 清空套餐与订阅的 L1 内存缓存。
// 仅用于集成测试场景，在测试代码直接操作数据库（例如 DELETE FROM packages）
// 后，避免缓存中的旧值影响后续用例。
func ResetPackageCacheForTests() {
	GetPackageCache().ResetForTest()
}

// ============================================
// Package 缓存实现
// ============================================

// GetPackageByIDCached 通过三级缓存获取 Package
// forceDB=true 时，强制从 DB 读取（用于写操作后的刷新）
func (pc *PackageCache) GetPackageByIDCached(id int, forceDB bool) (*Package, error) {
	if forceDB {
		// 强制从 DB 读取，并更新缓存
		pkg, err := pc.loadPackageFromDB(id)
		if err != nil {
			return nil, err
		}
		pc.setPackageToL1(id, pkg)
		pc.setPackageToL2(id, pkg)
		return pkg, nil
	}

	// L1 内存缓存查询
	if pkg := pc.getPackageFromL1(id); pkg != nil {
		pc.recordL1Hit()
		return pkg, nil
	}
	pc.recordL1Miss()

	// L2 Redis 缓存查询（仅在 Redis 已正确初始化时启用）
	if common.RedisEnabled && common.RDB != nil {
		if pkg, err := pc.getPackageFromL2(id); err == nil && pkg != nil {
			pc.recordL2Hit()
			pc.setPackageToL1(id, pkg) // 回填 L1
			return pkg, nil
		}
		pc.recordL2Miss()
	}

	// L3 DB 查询
	pkg, err := pc.loadPackageFromDB(id)
	if err != nil {
		return nil, err
	}

	pc.recordDBHit()

	// 回填缓存
	pc.setPackageToL1(id, pkg)
	if common.RedisEnabled && common.RDB != nil {
		pc.setPackageToL2(id, pkg)
	}

	return pkg, nil
}

// getPackageFromL1 从 L1 内存缓存读取
func (pc *PackageCache) getPackageFromL1(id int) *Package {
	if val, ok := pc.l1Packages.Load(id); ok {
		entry := val.(*packageCacheEntry)
		// 检查是否过期
		if time.Now().Before(entry.ExpiredAt) {
			return entry.Data
		}
		// 过期则删除
		pc.l1Packages.Delete(id)
	}
	return nil
}

// setPackageToL1 写入 L1 内存缓存
func (pc *PackageCache) setPackageToL1(id int, pkg *Package) {
	entry := &packageCacheEntry{
		Data:      pkg,
		ExpiredAt: time.Now().Add(pc.config.L1TTL),
	}
	pc.l1Packages.Store(id, entry)
}

// getPackageFromL2 从 L2 Redis 缓存读取
func (pc *PackageCache) getPackageFromL2(id int) (*Package, error) {
	ctx := context.Background()
	key := fmt.Sprintf("package:%d", id)

	data, err := common.RDB.Get(ctx, key).Result()
	if err != nil {
		return nil, err // Redis Miss 或错误
	}

	var pkg Package
	if err := json.Unmarshal([]byte(data), &pkg); err != nil {
		common.SysError(fmt.Sprintf("failed to unmarshal package from redis: %v", err))
		return nil, err
	}

	return &pkg, nil
}

// setPackageToL2 写入 L2 Redis 缓存
func (pc *PackageCache) setPackageToL2(id int, pkg *Package) {
	// 在未启用 Redis 或客户端未初始化时直接跳过写缓存，避免 nil 指针
	if !common.RedisEnabled || common.RDB == nil {
		return
	}

	ctx := context.Background()
	key := fmt.Sprintf("package:%d", id)

	data, err := json.Marshal(pkg)
	if err != nil {
		common.SysError(fmt.Sprintf("failed to marshal package for redis: %v", err))
		return
	}

	if err := common.RDB.Set(ctx, key, data, pc.config.L2TTL).Err(); err != nil {
		common.SysError(fmt.Sprintf("failed to set package to redis: %v", err))
	}
}

// loadPackageFromDB 从 L3 DB 加载
func (pc *PackageCache) loadPackageFromDB(id int) (*Package, error) {
	var pkg Package
	err := DB.First(&pkg, id).Error
	return &pkg, err
}

// InvalidatePackage 使某个 Package 缓存失效
// 用于更新/删除操作后，确保缓存一致性
func (pc *PackageCache) InvalidatePackage(id int) {
	// 删除 L1
	pc.l1Packages.Delete(id)

	// 删除 L2（仅在 Redis 已初始化时触发）
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		key := fmt.Sprintf("package:%d", id)
		common.RDB.Del(ctx, key)
	}
}

// ============================================
// Subscription 缓存实现
// ============================================

// GetSubscriptionByIDCached 通过三级缓存获取 Subscription
func (pc *PackageCache) GetSubscriptionByIDCached(id int, forceDB bool) (*Subscription, error) {
	if forceDB {
		sub, err := pc.loadSubscriptionFromDB(id)
		if err != nil {
			return nil, err
		}
		pc.setSubscriptionToL1(id, sub)
		pc.setSubscriptionToL2(id, sub)
		return sub, nil
	}

	// L1 内存缓存查询
	if sub := pc.getSubscriptionFromL1(id); sub != nil {
		pc.recordL1Hit()
		return sub, nil
	}
	pc.recordL1Miss()

	// L2 Redis 缓存查询（仅在 Redis 已正确初始化时启用）
	if common.RedisEnabled && common.RDB != nil {
		if sub, err := pc.getSubscriptionFromL2(id); err == nil && sub != nil {
			pc.recordL2Hit()
			pc.setSubscriptionToL1(id, sub)
			return sub, nil
		}
		pc.recordL2Miss()
	}

	// L3 DB 查询
	sub, err := pc.loadSubscriptionFromDB(id)
	if err != nil {
		return nil, err
	}

	pc.recordDBHit()

	// 回填缓存
	pc.setSubscriptionToL1(id, sub)
	if common.RedisEnabled && common.RDB != nil {
		pc.setSubscriptionToL2(id, sub)
	}

	return sub, nil
}

// getSubscriptionFromL1 从 L1 内存缓存读取
func (pc *PackageCache) getSubscriptionFromL1(id int) *Subscription {
	if val, ok := pc.l1Subscriptions.Load(id); ok {
		entry := val.(*subscriptionCacheEntry)
		if time.Now().Before(entry.ExpiredAt) {
			return entry.Data
		}
		pc.l1Subscriptions.Delete(id)
	}
	return nil
}

// setSubscriptionToL1 写入 L1 内存缓存
func (pc *PackageCache) setSubscriptionToL1(id int, sub *Subscription) {
	entry := &subscriptionCacheEntry{
		Data:      sub,
		ExpiredAt: time.Now().Add(pc.config.L1TTL),
	}
	pc.l1Subscriptions.Store(id, entry)
}

// getSubscriptionFromL2 从 L2 Redis 缓存读取
func (pc *PackageCache) getSubscriptionFromL2(id int) (*Subscription, error) {
	ctx := context.Background()
	key := fmt.Sprintf("subscription:%d", id)

	data, err := common.RDB.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	var sub Subscription
	if err := json.Unmarshal([]byte(data), &sub); err != nil {
		common.SysError(fmt.Sprintf("failed to unmarshal subscription from redis: %v", err))
		return nil, err
	}

	return &sub, nil
}

// setSubscriptionToL2 写入 L2 Redis 缓存
func (pc *PackageCache) setSubscriptionToL2(id int, sub *Subscription) {
	// 在未启用 Redis 或客户端未初始化时直接跳过写缓存，避免 nil 指针
	if !common.RedisEnabled || common.RDB == nil {
		return
	}

	ctx := context.Background()
	key := fmt.Sprintf("subscription:%d", id)

	data, err := json.Marshal(sub)
	if err != nil {
		common.SysError(fmt.Sprintf("failed to marshal subscription for redis: %v", err))
		return
	}

	if err := common.RDB.Set(ctx, key, data, pc.config.L2TTL).Err(); err != nil {
		common.SysError(fmt.Sprintf("failed to set subscription to redis: %v", err))
	}
}

// loadSubscriptionFromDB 从 L3 DB 加载
func (pc *PackageCache) loadSubscriptionFromDB(id int) (*Subscription, error) {
	var sub Subscription
	err := DB.First(&sub, id).Error
	return &sub, err
}

// InvalidateSubscription 使某个 Subscription 缓存失效
func (pc *PackageCache) InvalidateSubscription(id int) {
	pc.l1Subscriptions.Delete(id)

	if common.RedisEnabled {
		ctx := context.Background()
		key := fmt.Sprintf("subscription:%d", id)
		common.RDB.Del(ctx, key)
	}
}

// ResetForTest 清空套餐与订阅的缓存，仅用于测试场景。
// 说明：
//   - 场景测试会在同一进程内多次直接清理 packages / subscriptions 表，
//     若不显式清理 PackageCache，可能导致缓存中的旧值与 DB 不一致（尤其是复用自增 ID 时）。
//   - 生产环境不应调用此函数。
func (pc *PackageCache) ResetForTest() {
	// 清空 L1 Package 缓存
	pc.l1Packages.Range(func(key, _ interface{}) bool {
		pc.l1Packages.Delete(key)
		return true
	})

	// 清空 L1 Subscription 缓存
	pc.l1Subscriptions.Range(func(key, _ interface{}) bool {
		pc.l1Subscriptions.Delete(key)
		return true
	})

	// 清空 L2 Redis 缓存（仅在 Redis 已初始化时执行）
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		patterns := []string{"package:*", "subscription:*"}
		for _, pat := range patterns {
			iter := common.RDB.Scan(ctx, 0, pat, 0).Iterator()
			for iter.Next(ctx) {
				if err := common.RDB.Del(ctx, iter.Val()).Err(); err != nil {
					common.SysError(fmt.Sprintf("failed to delete key %s when resetting package cache: %v", iter.Val(), err))
				}
			}
			if err := iter.Err(); err != nil {
				common.SysError(fmt.Sprintf("failed to scan keys with pattern %s when resetting package cache: %v", pat, err))
			}
		}
	}

	// 重置统计信息
	pc.statsMu.Lock()
	pc.stats = struct {
		L1Hits   uint64
		L1Misses uint64
		L2Hits   uint64
		L2Misses uint64
		DBHits   uint64
	}{}
	pc.statsMu.Unlock()
}

// ============================================
// 后台清理与统计
// ============================================

// startEvictionLoop 后台定期清理过期的 L1 缓存条目
func (pc *PackageCache) startEvictionLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		evicted := 0

		// 清理过期的 Package
		pc.l1Packages.Range(func(key, value interface{}) bool {
			entry := value.(*packageCacheEntry)
			if now.After(entry.ExpiredAt) {
				pc.l1Packages.Delete(key)
				evicted++
			}
			return true
		})

		// 清理过期的 Subscription
		pc.l1Subscriptions.Range(func(key, value interface{}) bool {
			entry := value.(*subscriptionCacheEntry)
			if now.After(entry.ExpiredAt) {
				pc.l1Subscriptions.Delete(key)
				evicted++
			}
			return true
		})

		if evicted > 0 {
			common.SysLog(fmt.Sprintf("[PackageCache] Evicted %d expired L1 entries", evicted))
		}
	}
}

// recordL1Hit 记录 L1 命中
func (pc *PackageCache) recordL1Hit() {
	pc.statsMu.Lock()
	pc.stats.L1Hits++
	pc.statsMu.Unlock()
}

// recordL1Miss 记录 L1 未命中
func (pc *PackageCache) recordL1Miss() {
	pc.statsMu.Lock()
	pc.stats.L1Misses++
	pc.statsMu.Unlock()
}

// recordL2Hit 记录 L2 命中
func (pc *PackageCache) recordL2Hit() {
	pc.statsMu.Lock()
	pc.stats.L2Hits++
	pc.statsMu.Unlock()
}

// recordL2Miss 记录 L2 未命中
func (pc *PackageCache) recordL2Miss() {
	pc.statsMu.Lock()
	pc.stats.L2Misses++
	pc.statsMu.Unlock()
}

// recordDBHit 记录 DB 查询
func (pc *PackageCache) recordDBHit() {
	pc.statsMu.Lock()
	pc.stats.DBHits++
	pc.statsMu.Unlock()
}

// GetCacheStats 获取缓存统计信息
func (pc *PackageCache) GetCacheStats() map[string]uint64 {
	pc.statsMu.RLock()
	defer pc.statsMu.RUnlock()

	return map[string]uint64{
		"l1_hits":   pc.stats.L1Hits,
		"l1_misses": pc.stats.L1Misses,
		"l2_hits":   pc.stats.L2Hits,
		"l2_misses": pc.stats.L2Misses,
		"db_hits":   pc.stats.DBHits,
	}
}

// GetL1HitRate 获取 L1 命中率
func (pc *PackageCache) GetL1HitRate() float64 {
	pc.statsMu.RLock()
	defer pc.statsMu.RUnlock()

	total := pc.stats.L1Hits + pc.stats.L1Misses
	if total == 0 {
		return 0
	}
	return float64(pc.stats.L1Hits) / float64(total)
}

// GetL2HitRate 获取 L2 命中率
func (pc *PackageCache) GetL2HitRate() float64 {
	pc.statsMu.RLock()
	defer pc.statsMu.RUnlock()

	total := pc.stats.L2Hits + pc.stats.L2Misses
	if total == 0 {
		return 0
	}
	return float64(pc.stats.L2Hits) / float64(total)
}
