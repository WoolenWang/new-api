package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetUserSubscriptions 获取当前用户的所有订阅
// Query params:
//   - status: 状态过滤 (可选, inventory/active/expired)
func GetUserSubscriptions(c *gin.Context) {
	userId := c.GetInt("id")
	status := c.Query("status")

	subs, err := model.GetUserSubscriptions(userId, status)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 批量查询Package信息
	if len(subs) == 0 {
		common.ApiSuccess(c, []dto.SubscriptionResponse{})
		return
	}

	packageIds := make([]int, len(subs))
	for i, sub := range subs {
		packageIds[i] = sub.PackageId
	}
	packages, err := model.GetPackagesByIds(packageIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 构建Package ID到Package的映射
	packageMap := make(map[int]*model.Package)
	for _, pkg := range packages {
		packageMap[pkg.Id] = pkg
	}

	// 组装响应
	response := make([]dto.SubscriptionResponse, len(subs))
	for i, sub := range subs {
		pkg := packageMap[sub.PackageId]
		if pkg == nil {
			continue
		}

		remaining := pkg.Quota - sub.TotalConsumed
		if remaining < 0 {
			remaining = 0
		}

		response[i] = dto.SubscriptionResponse{
			SubscriptionId: sub.Id,
			UserId:         sub.UserId,
			PackageId:      sub.PackageId,
			PackageName:    pkg.Name,
			Priority:       pkg.Priority,
			Status:         sub.Status,
			TotalConsumed:  sub.TotalConsumed,
			TotalQuota:     pkg.Quota,
			RemainingQuota: remaining,
			StartTime:      sub.StartTime,
			EndTime:        sub.EndTime,
			SubscribedAt:   sub.SubscribedAt,
		}
	}

	common.ApiSuccess(c, response)
}

// SubscribePackage 订阅套餐（添加到库存）
// URL param: :id - package ID
func SubscribePackage(c *gin.Context) {
	pkgId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的套餐ID")
		return
	}

	userId := c.GetInt("id")

	// 验证订阅权限（全局套餐 vs P2P套餐）
	if err := service.ValidatePackageSubscription(userId, pkgId); err != nil {
		common.ApiError(c, err)
		return
	}

	// 检查用户活跃订阅数限制
	activeCount, err := model.CountUserActiveSubscriptions(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// Maximum 10 active subscriptions per user
	const maxActiveSubscriptions = 10
	if activeCount >= int64(maxActiveSubscriptions) {
		common.ApiErrorMsg(c, fmt.Sprintf("您已达到活跃订阅数量上限：%d", maxActiveSubscriptions))
		return
	}

	// 创建订阅记录（状态为 inventory）
	sub := &model.Subscription{
		UserId:    userId,
		PackageId: pkgId,
		Status:    model.SubscriptionStatusInventory,
	}

	if err := model.CreateSubscription(sub); err != nil {
		common.ApiError(c, err)
		return
	}

	// 查询套餐名称用于响应
	pkg, _ := model.GetPackageByID(pkgId)
	packageName := ""
	if pkg != nil {
		packageName = pkg.Name
	}

	common.ApiSuccess(c, gin.H{
		"subscription_id": sub.Id,
		"package_id":      pkgId,
		"package_name":    packageName,
		"status":          sub.Status,
		"subscribed_at":   sub.SubscribedAt,
		"message":         "套餐已添加到库存，请调用启用接口激活",
	})
}

// ActivateSubscription 启用套餐
// URL param: :id - subscription ID
func ActivateSubscription(c *gin.Context) {
	subId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的订阅ID")
		return
	}

	userId := c.GetInt("id")

	// 调用业务逻辑（包含权限验证）
	if err := service.ActivateSubscription(subId, userId); err != nil {
		common.ApiError(c, err)
		return
	}

	// 查询更新后的订阅
	sub, err := model.GetSubscriptionById(subId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pkg, _ := model.GetPackageByID(sub.PackageId)
	packageName := ""
	if pkg != nil {
		packageName = pkg.Name
	}

	common.ApiSuccess(c, gin.H{
		"subscription_id": sub.Id,
		"package_name":    packageName,
		"status":          sub.Status,
		"start_time":      sub.StartTime,
		"end_time":        sub.EndTime,
		"message":         fmt.Sprintf("套餐已启用，有效期至 %s", formatTimestampPtr(sub.EndTime)),
	})
}

// GetSubscriptionStatus 查询订阅详细状态（含滑动窗口）
// URL param: :id - subscription ID
func GetSubscriptionStatus(c *gin.Context) {
	subId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的订阅ID")
		return
	}

	userId := c.GetInt("id")

	// 查询订阅
	sub, err := model.GetSubscriptionById(subId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 验证所有权
	if sub.UserId != userId {
		common.ApiErrorMsg(c, "无权查询此订阅")
		return
	}

	// 查询套餐配置
	pkg, err := model.GetPackageByID(sub.PackageId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// 查询滑动窗口状态（使用任务集2的服务）
	ctx := context.Background()
	windows, err := service.GetAllSlidingWindowsStatus(ctx, sub.Id, pkg)
	if err != nil {
		common.SysError(fmt.Sprintf("Failed to get sliding window status: %v", err))
		windows = []service.WindowStatus{} // 降级：返回空
	}

	// 转换为 DTO
	windowMap := make(map[string]*dto.SlidingWindowStatusDTO)
	for _, w := range windows {
		windowMap[w.Period] = &dto.SlidingWindowStatusDTO{
			Period:       w.Period,
			IsActive:     w.IsActive,
			Consumed:     w.Consumed,
			Limit:        w.Limit,
			Remaining:    w.Remaining,
			StartTime:    w.StartTime,
			EndTime:      w.EndTime,
			TimeLeft:     w.TimeLeft,
			StartTimeStr: formatTimestamp(w.StartTime),
			EndTimeStr:   formatTimestamp(w.EndTime),
		}
	}

	// 组装响应
	remaining := pkg.Quota - sub.TotalConsumed
	if remaining < 0 {
		remaining = 0
	}

	response := dto.SubscriptionStatusResponse{
		SubscriptionId: sub.Id,
		PackageName:    pkg.Name,
		PackageId:      pkg.Id,
		Status:         sub.Status,
		Priority:       pkg.Priority,
		TotalQuota:     pkg.Quota,
		TotalConsumed:  sub.TotalConsumed,
		RemainingQuota: remaining,
		StartTime:      sub.StartTime,
		EndTime:        sub.EndTime,
		SlidingWindows: windowMap,
	}

	// 计算剩余天数
	if sub.EndTime != nil && *sub.EndTime > 0 {
		now := common.GetTimestamp()
		daysRemaining := (*sub.EndTime - now) / 86400
		response.DaysRemaining = &daysRemaining
	}

	common.ApiSuccess(c, response)
}

// formatTimestamp 格式化Unix时间戳为可读字符串
func formatTimestamp(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05")
}

// formatTimestampPtr 格式化Unix时间戳指针为可读字符串
func formatTimestampPtr(ts *int64) string {
	if ts == nil || *ts == 0 {
		return ""
	}
	return time.Unix(*ts, 0).Format("2006-01-02 15:04:05")
}
