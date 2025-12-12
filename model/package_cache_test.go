package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

// TestPackageCache_L1L2L3Flow 测试三级缓存的完整流程
func TestPackageCache_L1L2L3Flow(t *testing.T) {
	// Setup: 初始化测试环境
	setupTestDB(t)
	defer teardownTestDB(t)

	// 启用 Redis（如果可用）
	redisEnabled := common.RedisEnabled

	cache := GetPackageCache()

	// 创建测试套餐
	pkg := &Package{
		Name:         "测试套餐-缓存",
		Description:  "用于测试三级缓存",
		Priority:     15,
		P2PGroupId:   0,
		Quota:        100000000,
		DurationType: "month",
		Duration:     1,
		CreatorId:    1,
		Status:       1,
	}
	err := CreatePackage(pkg)
	assert.NoError(t, err)
	assert.NotZero(t, pkg.Id)

	pkgId := pkg.Id

	// 清空统计信息
	cache.stats.L1Hits = 0
	cache.stats.L1Misses = 0
	cache.stats.L2Hits = 0
	cache.stats.L2Misses = 0
	cache.stats.DBHits = 0

	// ========== 测试场景 1: L1 命中 ==========
	t.Run("L1_Cache_Hit", func(t *testing.T) {
		// CreatePackage 已经预填充了 L1
		pkg1, err := GetPackageByID(pkgId)
		assert.NoError(t, err)
		assert.Equal(t, "测试套餐-缓存", pkg1.Name)

		// 验证统计：L1 命中
		stats := cache.GetCacheStats()
		assert.Equal(t, uint64(1), stats["l1_hits"], "应该有 1 次 L1 命中")
		assert.Equal(t, uint64(0), stats["l1_misses"], "应该没有 L1 未命中")
	})

	// ========== 测试场景 2: L1 过期，L2 命中 ==========
	t.Run("L1_Expired_L2_Hit", func(t *testing.T) {
		if !redisEnabled {
			t.Skip("Redis 未启用，跳过 L2 测试")
		}

		// 清空 L1 缓存
		cache.l1Packages.Delete(pkgId)

		// 重置统计
		cache.stats.L1Hits = 0
		cache.stats.L1Misses = 0
		cache.stats.L2Hits = 0

		// 读取（应该从 L2 命中）
		pkg2, err := GetPackageByID(pkgId)
		assert.NoError(t, err)
		assert.Equal(t, "测试套餐-缓存", pkg2.Name)

		// 验证统计
		stats := cache.GetCacheStats()
		assert.Equal(t, uint64(1), stats["l1_misses"], "L1 应该未命中")
		assert.Equal(t, uint64(1), stats["l2_hits"], "L2 应该命中")

		// 验证 L1 已回填
		pkg3, err := GetPackageByID(pkgId)
		assert.NoError(t, err)
		assert.Equal(t, "测试套餐-缓存", pkg3.Name)

		stats2 := cache.GetCacheStats()
		assert.Equal(t, uint64(1), stats2["l1_hits"], "L1 应该命中（已回填）")
	})

	// ========== 测试场景 3: L1/L2 都过期，DB 查询 ==========
	t.Run("L1_L2_Miss_DB_Hit", func(t *testing.T) {
		// 清空所有缓存
		cache.l1Packages.Delete(pkgId)
		if redisEnabled {
			cache.InvalidatePackage(pkgId)
		}

		// 重置统计
		cache.stats.L1Hits = 0
		cache.stats.L1Misses = 0
		cache.stats.L2Hits = 0
		cache.stats.L2Misses = 0
		cache.stats.DBHits = 0

		// 读取（应该从 DB 查询）
		pkg4, err := GetPackageByID(pkgId)
		assert.NoError(t, err)
		assert.Equal(t, "测试套餐-缓存", pkg4.Name)

		// 验证统计
		stats := cache.GetCacheStats()
		assert.Equal(t, uint64(1), stats["l1_misses"], "L1 应该未命中")
		assert.Equal(t, uint64(1), stats["db_hits"], "DB 应该被查询")

		if redisEnabled {
			assert.Equal(t, uint64(1), stats["l2_misses"], "L2 应该未命中")
		}
	})

	// ========== 测试场景 4: 更新后缓存失效 ==========
	t.Run("Cache_Invalidation_On_Update", func(t *testing.T) {
		// 先读取一次，填充缓存
		pkg5, err := GetPackageByID(pkgId)
		assert.NoError(t, err)

		// 确认 L1 已缓存
		cachedPkg := cache.getPackageFromL1(pkgId)
		assert.NotNil(t, cachedPkg)

		// 更新套餐
		pkg5.Name = "更新后的套餐名称"
		err = pkg5.Update()
		assert.NoError(t, err)

		// 验证 L1 缓存已失效
		cachedPkg2 := cache.getPackageFromL1(pkgId)
		assert.Nil(t, cachedPkg2, "更新后 L1 缓存应该被清空")

		// 重新读取（从 DB）
		pkg6, err := GetPackageByID(pkgId)
		assert.NoError(t, err)
		assert.Equal(t, "更新后的套餐名称", pkg6.Name, "应该读取到最新数据")
	})

	// ========== 测试场景 5: 删除后缓存失效 ==========
	t.Run("Cache_Invalidation_On_Delete", func(t *testing.T) {
		// 创建新套餐用于删除测试
		pkgToDelete := &Package{
			Name:         "待删除套餐",
			Priority:     10,
			P2PGroupId:   0,
			Quota:        10000000,
			DurationType: "week",
			Duration:     1,
			CreatorId:    1,
			Status:       1,
		}
		err := CreatePackage(pkgToDelete)
		assert.NoError(t, err)

		deleteId := pkgToDelete.Id

		// 确认 L1 已缓存
		cachedPkg := cache.getPackageFromL1(deleteId)
		assert.NotNil(t, cachedPkg)

		// 删除
		err = DeletePackage(deleteId)
		assert.NoError(t, err)

		// 验证缓存已失效
		cachedPkg2 := cache.getPackageFromL1(deleteId)
		assert.Nil(t, cachedPkg2, "删除后 L1 缓存应该被清空")
	})
}

// TestSubscriptionCache_L1L2L3Flow 测试 Subscription 的三级缓存流程
func TestSubscriptionCache_L1L2L3Flow(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	cache := GetPackageCache()

	// 创建测试套餐
	pkg := &Package{
		Name:         "测试套餐",
		Priority:     15,
		P2PGroupId:   0,
		Quota:        100000000,
		DurationType: "month",
		Duration:     1,
		CreatorId:    1,
		Status:       1,
	}
	err := CreatePackage(pkg)
	assert.NoError(t, err)

	// 创建测试订阅
	sub := &Subscription{
		UserId:    1,
		PackageId: pkg.Id,
		Status:    SubscriptionStatusInventory,
	}
	err = CreateSubscription(sub)
	assert.NoError(t, err)
	assert.NotZero(t, sub.Id)

	subId := sub.Id

	// 清空统计
	cache.stats.L1Hits = 0
	cache.stats.L1Misses = 0

	// ========== 测试 L1 命中 ==========
	sub1, err := GetSubscriptionById(subId)
	assert.NoError(t, err)
	assert.Equal(t, SubscriptionStatusInventory, sub1.Status)

	stats := cache.GetCacheStats()
	assert.Equal(t, uint64(1), stats["l1_hits"], "应该有 1 次 L1 命中")

	// ========== 测试更新后缓存失效 ==========
	err = UpdateSubscriptionStatus(subId, SubscriptionStatusExpired)
	assert.NoError(t, err)

	// 验证缓存已失效
	cachedSub := cache.getSubscriptionFromL1(subId)
	assert.Nil(t, cachedSub, "更新后 L1 缓存应该被清空")

	// 重新读取（从 DB）
	sub2, err := GetSubscriptionById(subId)
	assert.NoError(t, err)
	assert.Equal(t, SubscriptionStatusExpired, sub2.Status, "应该读取到最新状态")
}

// TestCacheStats 测试缓存统计信息
func TestCacheStats(t *testing.T) {
	cache := GetPackageCache()

	// 模拟一些缓存操作
	cache.recordL1Hit()
	cache.recordL1Hit()
	cache.recordL1Miss()
	cache.recordL2Hit()
	cache.recordL2Miss()
	cache.recordL2Miss()
	cache.recordDBHit()

	// 验证统计
	stats := cache.GetCacheStats()
	assert.Equal(t, uint64(2), stats["l1_hits"])
	assert.Equal(t, uint64(1), stats["l1_misses"])
	assert.Equal(t, uint64(1), stats["l2_hits"])
	assert.Equal(t, uint64(2), stats["l2_misses"])
	assert.Equal(t, uint64(1), stats["db_hits"])

	// 验证命中率计算
	l1HitRate := cache.GetL1HitRate()
	assert.InDelta(t, 0.6667, l1HitRate, 0.01, "L1 命中率应为 2/3")

	l2HitRate := cache.GetL2HitRate()
	assert.InDelta(t, 0.3333, l2HitRate, 0.01, "L2 命中率应为 1/3")
}

// TestCacheExpiration 测试 L1 缓存过期机制
func TestCacheExpiration(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	cache := GetPackageCache()
	// 临时修改 L1 TTL 为 100ms（便于测试）
	originalTTL := cache.config.L1TTL
	cache.config.L1TTL = 100 * time.Millisecond
	defer func() { cache.config.L1TTL = originalTTL }()

	// 创建测试套餐
	pkg := &Package{
		Name:         "过期测试套餐",
		Priority:     10,
		P2PGroupId:   0,
		Quota:        10000000,
		DurationType: "week",
		Duration:     1,
		CreatorId:    1,
		Status:       1,
	}
	err := CreatePackage(pkg)
	assert.NoError(t, err)

	pkgId := pkg.Id

	// 第一次读取（填充 L1）
	pkg1, err := GetPackageByID(pkgId)
	assert.NoError(t, err)
	assert.NotNil(t, pkg1)

	// 确认 L1 已缓存
	cachedPkg := cache.getPackageFromL1(pkgId)
	assert.NotNil(t, cachedPkg, "L1 应该已缓存")

	// 等待缓存过期
	time.Sleep(150 * time.Millisecond)

	// 验证过期后 L1 返回 nil
	cachedPkg2 := cache.getPackageFromL1(pkgId)
	assert.Nil(t, cachedPkg2, "L1 缓存应该已过期")

	// 重新读取（应该从 L2 或 DB 加载）
	pkg2, err := GetPackageByID(pkgId)
	assert.NoError(t, err)
	assert.Equal(t, "过期测试套餐", pkg2.Name)
}

// TestConcurrentCacheAccess 测试并发访问缓存的安全性
func TestConcurrentCacheAccess(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	cache := GetPackageCache()

	// 创建测试套餐
	pkg := &Package{
		Name:         "并发测试套餐",
		Priority:     10,
		P2PGroupId:   0,
		Quota:        10000000,
		DurationType: "month",
		Duration:     1,
		CreatorId:    1,
		Status:       1,
	}
	err := CreatePackage(pkg)
	assert.NoError(t, err)

	pkgId := pkg.Id

	// 清空 L1 缓存（模拟冷启动）
	cache.l1Packages.Delete(pkgId)

	// 并发读取 100 次
	concurrency := 100
	done := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			pkg, err := GetPackageByID(pkgId)
			assert.NoError(t, err)
			assert.Equal(t, "并发测试套餐", pkg.Name)
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < concurrency; i++ {
		<-done
	}

	// 验证：即使并发访问，数据也是一致的
	finalPkg, err := GetPackageByID(pkgId)
	assert.NoError(t, err)
	assert.Equal(t, "并发测试套餐", finalPkg.Name)

	// 验证 L1 只缓存了一个副本（sync.Map 并发安全）
	cachedPkg := cache.getPackageFromL1(pkgId)
	assert.NotNil(t, cachedPkg, "L1 应该已缓存")
}

// TestForceDBRefresh 测试强制从 DB 刷新缓存
func TestForceDBRefresh(t *testing.T) {
	setupTestDB(t)
	defer teardownTestDB(t)

	cache := GetPackageCache()

	// 创建测试套餐
	pkg := &Package{
		Name:         "强制刷新测试",
		Priority:     10,
		P2PGroupId:   0,
		Quota:        10000000,
		DurationType: "month",
		Duration:     1,
		CreatorId:    1,
		Status:       1,
	}
	err := CreatePackage(pkg)
	assert.NoError(t, err)

	pkgId := pkg.Id

	// 第一次读取（填充缓存）
	pkg1, err := GetPackageByID(pkgId)
	assert.NoError(t, err)
	assert.Equal(t, "强制刷新测试", pkg1.Name)

	// 直接在 DB 中修改（绕过缓存失效机制）
	err = DB.Model(&Package{}).Where("id = ?", pkgId).Update("name", "DB直接修改").Error
	assert.NoError(t, err)

	// 使用普通读取（应该返回旧缓存）
	pkg2, err := GetPackageByID(pkgId)
	assert.NoError(t, err)
	assert.Equal(t, "强制刷新测试", pkg2.Name, "L1 缓存应该还是旧值")

	// 使用 forceDB=true 强制刷新
	pkg3, err := GetPackageByIDFromDB(pkgId)
	assert.NoError(t, err)
	assert.Equal(t, "DB直接修改", pkg3.Name, "forceDB 应该读取到最新值")

	// 验证缓存已更新
	pkg4, err := GetPackageByID(pkgId)
	assert.NoError(t, err)
	assert.Equal(t, "DB直接修改", pkg4.Name, "缓存应该已刷新")
}

// Helper functions for test setup
func setupTestDB(t *testing.T) {
	// 使用内存数据库进行测试
	common.UsingPostgreSQLTest = true
	DB = common.InitDB("sqlite::memory:")
	assert.NotNil(t, DB)

	// 自动迁移
	err := DB.AutoMigrate(&Package{}, &Subscription{})
	assert.NoError(t, err)
}

func teardownTestDB(t *testing.T) {
	if DB != nil {
		sqlDB, _ := DB.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}
}
