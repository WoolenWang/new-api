// Package testutil provides specialized helper functions for orthogonal package tests.
package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
)

// OrthogonalPackageHelper 正交套餐测试辅助工具
type OrthogonalPackageHelper struct {
	t          *testing.T
	redisMock  *RedisMock
	apiBaseURL string
}

// NewOrthogonalPackageHelper 创建正交套餐测试辅助工具
func NewOrthogonalPackageHelper(t *testing.T, redisMock *RedisMock, apiBaseURL string) *OrthogonalPackageHelper {
	return &OrthogonalPackageHelper{
		t:          t,
		redisMock:  redisMock,
		apiBaseURL: apiBaseURL,
	}
}

// CreatePackageForOrthogonal 为正交测试创建套餐（简化版）
func (h *OrthogonalPackageHelper) CreatePackageForOrthogonal(
	name string,
	priority int,
	p2pGroupID int,
	hourlyLimit int64,
	fallback bool,
) *model.Package {
	h.t.Helper()

	pkg := CreateTestPackage(h.t, PackageTestData{
		Name:              name,
		Priority:          priority,
		P2PGroupId:        p2pGroupID,
		Quota:             500000000, // 默认500M总额度
		HourlyLimit:       hourlyLimit,
		DailyLimit:        0, // 简化：不设置日限额
		WeeklyLimit:       0, // 简化：不设置周限额
		FourHourlyLimit:   0, // 简化：不设置4小时限额
		RpmLimit:          0, // 简化：不设置RPM限额
		DurationType:      "month",
		Duration:          1,
		FallbackToBalance: fallback,
		Status:            1,
		CreatorId:         0,
	})

	h.t.Logf("    [Helper] Created package: ID=%d, Name=%s, Priority=%d, HourlyLimit=%dM, Fallback=%v",
		pkg.Id, name, priority, hourlyLimit/1000000, fallback)
	return pkg
}

// CreateUserForOrthogonal 为正交测试创建用户
func (h *OrthogonalPackageHelper) CreateUserForOrthogonal(group string, quota int) *model.User {
	h.t.Helper()

	user := CreateTestUser(h.t, UserTestData{
		Username: fmt.Sprintf("ox_user_%s_%d", group, time.Now().UnixNano()),
		Group:    group,
		Quota:    quota,
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	})

	h.t.Logf("    [Helper] Created user: ID=%d, Group=%s, Quota=%dM",
		user.Id, group, quota/1000000)
	return user
}

// CreateChannelForOrthogonal 为正交测试创建渠道
func (h *OrthogonalPackageHelper) CreateChannelForOrthogonal(
	channelType string,
	systemGroup string,
	p2pGroupID int,
	ownerID int,
) *model.Channel {
	h.t.Helper()

	name := fmt.Sprintf("ox_ch_%s_%s_%d", channelType, systemGroup, time.Now().UnixNano())
	baseURL := "http://localhost:8080" // Mock URL

	var allowedGroupsJSON *string
	if p2pGroupID > 0 {
		allowedGroupsStr := fmt.Sprintf("[%d]", p2pGroupID)
		allowedGroupsJSON = &allowedGroupsStr
	}

	isPrivate := channelType == "private"

	channel := &model.Channel{
		Name:          name,
		Type:          1, // OpenAI type
		Group:         systemGroup,
		Models:        "gpt-4",
		BaseURL:       &baseURL,
		Key:           "sk-test-" + name,
		Status:        common.ChannelStatusEnabled,
		IsPrivate:     isPrivate,
		OwnerUserId:   ownerID,
		AllowedGroups: allowedGroupsJSON,
	}

	err := model.DB.Create(channel).Error
	assert.Nil(h.t, err, "Failed to create channel for orthogonal test")

	h.t.Logf("    [Helper] Created channel: ID=%d, Type=%s, Group=%s, IsPrivate=%v, Owner=%d, P2PGroup=%d",
		channel.Id, channelType, systemGroup, isPrivate, ownerID, p2pGroupID)
	return channel
}

// CreateTokenForOrthogonal 为正交测试创建Token
func (h *OrthogonalPackageHelper) CreateTokenForOrthogonal(
	userID int,
	tokenConfig string,
	billingGroups []string,
	p2pGroupID int,
) string {
	h.t.Helper()

	name := fmt.Sprintf("ox_token_%s_%d", tokenConfig, time.Now().UnixNano())
	tokenKey := fmt.Sprintf("sk-ox-%d", time.Now().UnixNano())

	var groupStr string
	if len(billingGroups) > 0 {
		// 将数组转换为JSON字符串
		groupStr = fmt.Sprintf("[\"%s\"]", billingGroups[0])
		for i := 1; i < len(billingGroups); i++ {
			groupStr = fmt.Sprintf("[\"%s\",\"%s\"]", billingGroups[0], billingGroups[i])
		}
	}

	token := &model.Token{
		UserId:         userID,
		Key:            tokenKey,
		Name:           name,
		Status:         common.TokenStatusEnabled,
		UnlimitedQuota: false,
		RemainQuota:    0, // 使用用户余额
		Group:          groupStr,
	}

	if p2pGroupID > 0 {
		token.P2PGroupID = &p2pGroupID
	}

	err := model.DB.Create(token).Error
	assert.Nil(h.t, err, "Failed to create token for orthogonal test")

	h.t.Logf("    [Helper] Created token: Key=%s, Config=%s, BillingGroups=%v, P2PGroup=%d",
		tokenKey[:12]+"...", tokenConfig, billingGroups, p2pGroupID)
	return tokenKey
}

// PreCreateValidWindow 预创建一个有效的滑动窗口（已消耗部分额度）
func (h *OrthogonalPackageHelper) PreCreateValidWindow(
	subscriptionID int,
	period string,
	consumed int64,
	limit int64,
) {
	h.t.Helper()

	if h.redisMock == nil {
		h.t.Logf("    [Helper] RedisMock not available, skipping window creation")
		return
	}

	key := GetWindowKey(subscriptionID, period)
	now := time.Now().Unix()
	duration := h.getDurationByPeriod(period)
	endTime := now + duration

	// 使用RedisMock设置窗口Hash
	h.redisMock.SetHashField(key, "start_time", fmt.Sprintf("%d", now))
	h.redisMock.SetHashField(key, "end_time", fmt.Sprintf("%d", endTime))
	h.redisMock.SetHashField(key, "consumed", fmt.Sprintf("%d", consumed))
	h.redisMock.SetHashField(key, "limit", fmt.Sprintf("%d", limit))

	// 设置TTL
	ttl := h.getTTLByPeriod(period)
	h.redisMock.SetExpire(key, ttl)

	h.t.Logf("    [Helper] Pre-created valid window: SubID=%d, Period=%s, Consumed=%dM/%dM",
		subscriptionID, period, consumed/1000000, limit/1000000)
}

// PreCreateExpiredWindow 预创建一个已过期的滑动窗口
func (h *OrthogonalPackageHelper) PreCreateExpiredWindow(
	subscriptionID int,
	period string,
) {
	h.t.Helper()

	if h.redisMock == nil {
		h.t.Logf("    [Helper] RedisMock not available, skipping window creation")
		return
	}

	key := GetWindowKey(subscriptionID, period)
	now := time.Now().Unix()
	duration := h.getDurationByPeriod(period)

	// 设置为1小时前的窗口（已过期）
	startTime := now - duration - 3600
	endTime := startTime + duration

	h.redisMock.SetHashField(key, "start_time", fmt.Sprintf("%d", startTime))
	h.redisMock.SetHashField(key, "end_time", fmt.Sprintf("%d", endTime))
	h.redisMock.SetHashField(key, "consumed", "5000000") // 已消耗5M
	h.redisMock.SetHashField(key, "limit", "20000000")   // 限额20M

	// 不设置TTL，让Lua脚本检测过期

	h.t.Logf("    [Helper] Pre-created expired window: SubID=%d, Period=%s, EndTime=%s (expired)",
		subscriptionID, period, time.Unix(endTime, 0).Format("15:04:05"))
}

// PreCreateNearLimitWindow 预创建一个接近超限的滑动窗口
func (h *OrthogonalPackageHelper) PreCreateNearLimitWindow(
	subscriptionID int,
	period string,
	limit int64,
	remaining int64,
) {
	h.t.Helper()

	consumed := limit - remaining
	h.PreCreateValidWindow(subscriptionID, period, consumed, limit)

	h.t.Logf("    [Helper] Pre-created near-limit window: SubID=%d, Remaining=%dM/%dM",
		subscriptionID, remaining/1000000, limit/1000000)
}

// getDurationByPeriod 根据period获取窗口时长（秒）
func (h *OrthogonalPackageHelper) getDurationByPeriod(period string) int64 {
	switch period {
	case "rpm":
		return 60
	case "hourly":
		return 3600
	case "4hourly":
		return 14400
	case "daily":
		return 86400
	case "weekly":
		return 604800
	default:
		return 3600
	}
}

// getTTLByPeriod 根据period获取TTL（秒）
func (h *OrthogonalPackageHelper) getTTLByPeriod(period string) time.Duration {
	switch period {
	case "rpm":
		return 90 * time.Second
	case "hourly":
		return 4200 * time.Second
	case "4hourly":
		return 18000 * time.Second
	case "daily":
		return 93600 * time.Second
	case "weekly":
		return 691200 * time.Second
	default:
		return 4200 * time.Second
	}
}

// SimulateChatRequest 模拟聊天请求（不实际调用API）
func (h *OrthogonalPackageHelper) SimulateChatRequest(
	ctx context.Context,
	subscriptionID int,
	packageID int,
	requestQuota int64,
) (*SimulatedResponse, error) {
	h.t.Helper()

	// 获取套餐配置
	pkg, err := model.GetPackageByID(packageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get package: %w", err)
	}

	// 获取订阅
	sub, err := model.GetSubscriptionById(subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	// 检查滑动窗口
	configs := h.getSlidingWindowConfigs(pkg)
	for _, config := range configs {
		result, err := service.CheckAndConsumeSlidingWindow(ctx, subscriptionID, config, requestQuota)
		if err != nil {
			return nil, fmt.Errorf("failed to check sliding window: %w", err)
		}

		if !result.Success {
			// 窗口超限
			return &SimulatedResponse{
				Success:        false,
				StatusCode:     429,
				UsingPackageID: 0,
				ErrorMessage:   fmt.Sprintf("%s window exceeded", config.Period),
			}, nil
		}
	}

	// 更新订阅总消耗
	sub.TotalConsumed += requestQuota
	err = model.DB.Save(sub).Error
	if err != nil {
		return nil, fmt.Errorf("failed to update subscription: %w", err)
	}

	return &SimulatedResponse{
		Success:        true,
		StatusCode:     200,
		UsingPackageID: packageID,
		ConsumedQuota:  requestQuota,
	}, nil
}

// getSlidingWindowConfigs 获取套餐的滑动窗口配置
func (h *OrthogonalPackageHelper) getSlidingWindowConfigs(pkg *model.Package) []service.SlidingWindowConfig {
	configs := []service.SlidingWindowConfig{}

	if pkg.RpmLimit > 0 {
		configs = append(configs, service.SlidingWindowConfig{
			Period:   "rpm",
			Duration: 60,
			Limit:    int64(pkg.RpmLimit),
			TTL:      90,
		})
	}

	if pkg.HourlyLimit > 0 {
		configs = append(configs, service.SlidingWindowConfig{
			Period:   "hourly",
			Duration: 3600,
			Limit:    pkg.HourlyLimit,
			TTL:      4200,
		})
	}

	if pkg.FourHourlyLimit > 0 {
		configs = append(configs, service.SlidingWindowConfig{
			Period:   "4hourly",
			Duration: 14400,
			Limit:    pkg.FourHourlyLimit,
			TTL:      18000,
		})
	}

	if pkg.DailyLimit > 0 {
		configs = append(configs, service.SlidingWindowConfig{
			Period:   "daily",
			Duration: 86400,
			Limit:    pkg.DailyLimit,
			TTL:      93600,
		})
	}

	if pkg.WeeklyLimit > 0 {
		configs = append(configs, service.SlidingWindowConfig{
			Period:   "weekly",
			Duration: 604800,
			Limit:    pkg.WeeklyLimit,
			TTL:      691200,
		})
	}

	return configs
}

// SimulatedResponse 模拟的响应结构
type SimulatedResponse struct {
	Success        bool
	StatusCode     int
	UsingPackageID int
	ConsumedQuota  int64
	ErrorMessage   string
}

// CleanupOrthogonalData 清理正交测试数据
func (h *OrthogonalPackageHelper) CleanupOrthogonalData(
	userID int,
	subscriptionIDs []int,
	packageIDs []int,
	channelIDs []int,
	tokenKeys []string,
	p2pGroupIDs []int,
) {
	h.t.Helper()

	// 删除订阅
	for _, subID := range subscriptionIDs {
		model.DB.Delete(&model.Subscription{}, "id = ?", subID)
	}

	// 删除套餐
	for _, pkgID := range packageIDs {
		model.DB.Delete(&model.Package{}, "id = ?", pkgID)
	}

	// 删除渠道
	for _, chID := range channelIDs {
		model.DB.Delete(&model.Channel{}, "id = ?", chID)
	}

	// 删除Token
	for _, tokenKey := range tokenKeys {
		model.DB.Delete(&model.Token{}, "key = ?", tokenKey)
	}

	// 删除P2P分组关系
	for _, groupID := range p2pGroupIDs {
		model.DB.Delete(&model.UserGroup{}, "group_id = ?", groupID)
	}

	// 删除P2P分组
	for _, groupID := range p2pGroupIDs {
		model.DB.Delete(&model.Group{}, "id = ?", groupID)
	}

	// 删除用户
	if userID > 0 {
		model.DB.Delete(&model.User{}, "id = ?", userID)
	}

	// 清理Redis滑动窗口
	if h.redisMock != nil {
		for _, subID := range subscriptionIDs {
			periods := []string{"rpm", "hourly", "4hourly", "daily", "weekly"}
			for _, period := range periods {
				key := GetWindowKey(subID, period)
				h.redisMock.Server.Del(key)
			}
		}
	}

	h.t.Logf("    [Helper] Cleaned up orthogonal test data")
}

// GetUserQuota 获取用户余额
func (h *OrthogonalPackageHelper) GetUserQuota(userID int) int64 {
	h.t.Helper()

	quota, err := model.GetUserQuota(userID, true)
	assert.Nil(h.t, err, "Failed to get user quota")
	return int64(quota)
}

// GetSubscription 获取订阅
func (h *OrthogonalPackageHelper) GetSubscription(subscriptionID int) *model.Subscription {
	h.t.Helper()

	sub, err := model.GetSubscriptionById(subscriptionID)
	assert.Nil(h.t, err, "Failed to get subscription")
	return sub
}

// AssertWindowExists 断言窗口存在
func (h *OrthogonalPackageHelper) AssertWindowExists(subscriptionID int, period string) {
	h.t.Helper()

	if h.redisMock == nil {
		h.t.Logf("    [Helper] RedisMock not available, skipping assertion")
		return
	}

	AssertWindowExists(h.t, h.redisMock, subscriptionID, period)
}

// AssertWindowRebuilt 断言窗口已重建（新的start_time）
func (h *OrthogonalPackageHelper) AssertWindowRebuilt(subscriptionID int, period string) {
	h.t.Helper()

	if h.redisMock == nil {
		h.t.Logf("    [Helper] RedisMock not available, skipping assertion")
		return
	}

	// 检查窗口存在
	AssertWindowExists(h.t, h.redisMock, subscriptionID, period)

	// 检查start_time接近当前时间（窗口被重建）
	startTime := GetWindowStartTime(h.t, h.redisMock, subscriptionID, period)
	now := time.Now().Unix()
	timeDiff := now - startTime

	// 允许10秒误差
	assert.LessOrEqual(h.t, timeDiff, int64(10),
		fmt.Sprintf("Window should be recently created (start_time=%d, now=%d, diff=%ds)",
			startTime, now, timeDiff))

	h.t.Logf("    [Helper] Window rebuilt: SubID=%d, Period=%s, StartTime=%s",
		subscriptionID, period, time.Unix(startTime, 0).Format("15:04:05"))
}
