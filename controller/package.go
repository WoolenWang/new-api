package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetPackages 查询套餐列表
// Query params:
//   - p2p_group_id: P2P分组ID (可选, 0=全局套餐, >0=指定分组, -1=所有)
//   - status: 状态过滤 (可选, 0=全部, 1=可用, 2=下架)
//
// 权限规则：
//   - 管理员：可以查看所有套餐（全局 + 所有P2P分组）
//   - 普通用户：只能查看全局套餐 + 自己有权访问的P2P分组套餐
func GetPackages(c *gin.Context) {
	userId := c.GetInt("id")
	userRole := c.GetInt("role")

	// 解析 p2p_group_id (默认 -1 表示不过滤)
	p2pGroupId := -1
	if groupIdStr := c.Query("p2p_group_id"); groupIdStr != "" {
		if gid, err := strconv.Atoi(groupIdStr); err == nil {
			p2pGroupId = gid
		}
	}

	// 解析 status (默认 0 表示不过滤)
	status := 0
	if statusStr := c.Query("status"); statusStr != "" {
		if st, err := strconv.Atoi(statusStr); err == nil {
			status = st
		}
	}

	// 【P1-3 改动】权限过滤：非管理员只能查看自己有权访问的套餐
	var packages []*model.Package
	var err error

	if userRole == common.RoleRootUser {
		// 管理员：查看所有套餐
		packages, err = model.GetPackages(p2pGroupId, status)
	} else {
		// 普通用户：只能查看全局套餐 + 自己的P2P分组套餐
		// 获取用户的所有活跃P2P分组
		userP2PGroupIds, _ := model.GetUserActiveP2PGroupIds(userId)

		if p2pGroupId == 0 {
			// 只查询全局套餐
			packages, err = model.GetPackages(0, status)
		} else if p2pGroupId > 0 {
			// 查询指定分组套餐 - 需要验证权限
			// 检查用户是否有权访问该分组
			hasAccess := false
			for _, gid := range userP2PGroupIds {
				if gid == p2pGroupId {
					hasAccess = true
					break
				}
			}

			if !hasAccess {
				common.ApiError(c, common.NewError("permission denied: you don't have access to this P2P group"))
				return
			}

			packages, err = model.GetPackages(p2pGroupId, status)
		} else {
			// p2pGroupId == -1：查询所有可访问的套餐
			// 包括全局套餐 + 用户的P2P分组套餐
			packages, err = model.GetPackagesForUser(userId, userP2PGroupIds, status)
		}
	}

	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, packages)
}

// GetPackageById 查询单个套餐详情
func GetPackageById(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的套餐ID")
		return
	}

	pkg, err := model.GetPackageByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, pkg)
}

// CreatePackage 创建套餐模板
// Request body: dto.PackageCreateRequest
// Permissions:
//   - Admin: 可以创建全局套餐（p2p_group_id=0），优先级 1-21
//   - P2P Owner: 可以创建分组套餐（p2p_group_id>0），优先级**强制为 11**
func CreatePackage(c *gin.Context) {
	var req dto.PackageCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	userId := c.GetInt("id")
	userRole := c.GetInt("role")

	// 构建Package对象
	pkg := &model.Package{
		Name:              req.Name,
		Description:       req.Description,
		Priority:          req.Priority,
		P2PGroupId:        req.P2PGroupId,
		Quota:             req.Quota,
		DurationType:      req.DurationType,
		Duration:          req.Duration,
		RpmLimit:          req.RpmLimit,
		HourlyLimit:       req.HourlyLimit,
		FourHourlyLimit:   req.FourHourlyLimit,
		DailyLimit:        req.DailyLimit,
		WeeklyLimit:       req.WeeklyLimit,
		FallbackToBalance: req.FallbackToBalance,
		CreatorId:         userId,
		Status:            1, // 默认可用
	}

	// 验证创建权限
	if err := service.ValidatePackageCreation(userId, userRole, pkg); err != nil {
		common.ApiError(c, err)
		return
	}

	// 创建套餐
	if err := model.CreatePackage(pkg); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, pkg)
}

// UpdatePackage 更新套餐
// Request body: dto.PackageUpdateRequest
// Permissions:
//   - Admin: 可以更新任意套餐
//   - Creator: 可以更新自己创建的套餐
func UpdatePackage(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的套餐ID")
		return
	}

	var req dto.PackageUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	// 查询现有套餐
	pkg, err := model.GetPackageByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 验证更新权限
	userId := c.GetInt("id")
	userRole := c.GetInt("role")

	// Admin can update any package, Creator can update their own package
	if userRole < common.RoleRootUser && pkg.CreatorId != userId {
		common.ApiErrorMsg(c, "您没有权限更新此套餐")
		return
	}

	// 应用更新（部分更新）
	if req.Name != nil {
		pkg.Name = *req.Name
	}
	if req.Description != nil {
		pkg.Description = *req.Description
	}
	if req.Status != nil {
		pkg.Status = *req.Status
	}
	if req.RpmLimit != nil {
		pkg.RpmLimit = *req.RpmLimit
	}
	if req.HourlyLimit != nil {
		pkg.HourlyLimit = *req.HourlyLimit
	}
	if req.FourHourlyLimit != nil {
		pkg.FourHourlyLimit = *req.FourHourlyLimit
	}
	if req.DailyLimit != nil {
		pkg.DailyLimit = *req.DailyLimit
	}
	if req.WeeklyLimit != nil {
		pkg.WeeklyLimit = *req.WeeklyLimit
	}
	if req.FallbackToBalance != nil {
		pkg.FallbackToBalance = *req.FallbackToBalance
	}

	if err := pkg.Update(); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, pkg)
}

// DeletePackage 删除套餐
// Permissions:
//   - Admin: 可以删除任意套餐（无活跃订阅时）
//   - Creator: 可以删除自己创建的套餐（无活跃订阅时）
func DeletePackage(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的套餐ID")
		return
	}

	// 查询现有套餐
	pkg, err := model.GetPackageByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 验证删除权限
	userId := c.GetInt("id")
	userRole := c.GetInt("role")

	// Admin can delete any package, Creator can delete their own package
	if userRole < common.RoleRootUser && pkg.CreatorId != userId {
		common.ApiErrorMsg(c, "您没有权限删除此套餐")
		return
	}

	// 检查是否有活跃订阅
	activeCount, err := model.CountActiveSubscriptions(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if activeCount > 0 {
		common.ApiErrorMsg(c, "该套餐有活跃订阅，无法删除")
		return
	}

	// 执行删除
	if err := model.DeletePackage(id); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{"message": "套餐已成功删除"})
}

// GetPackageCacheStats 获取套餐缓存统计信息（管理员接口）
// 用于监控三级缓存的性能
func GetPackageCacheStats(c *gin.Context) {
	cache := model.GetPackageCache()
	stats := cache.GetCacheStats()

	response := gin.H{
		"l1_hits":       stats["l1_hits"],
		"l1_misses":     stats["l1_misses"],
		"l1_hit_rate":   fmt.Sprintf("%.2f%%", cache.GetL1HitRate()*100),
		"l2_hits":       stats["l2_hits"],
		"l2_misses":     stats["l2_misses"],
		"l2_hit_rate":   fmt.Sprintf("%.2f%%", cache.GetL2HitRate()*100),
		"db_hits":       stats["db_hits"],
		"total_queries": stats["l1_hits"] + stats["l1_misses"],
		"cache_enabled": common.RedisEnabled,
	}

	common.ApiSuccess(c, response)
}
