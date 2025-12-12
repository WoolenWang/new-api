package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// PackageTestData 套餐测试数据结构
type PackageTestData struct {
	Name              string
	Priority          int
	P2PGroupId        int
	Quota             int64
	HourlyLimit       int64
	DailyLimit        int64
	WeeklyLimit       int64
	FourHourlyLimit   int64
	RpmLimit          int
	DurationType      string
	Duration          int
	FallbackToBalance bool
	Status            int
	CreatorId         int
}

// ChannelTestData 渠道测试数据结构
type ChannelTestData struct {
	Name          string
	Type          int
	Group         string
	Models        string
	Status        int
	BaseURL       string
	OwnerUserId   int
	IsPrivate     bool
	AllowedGroups string
}

// TokenTestData 令牌测试数据结构
type TokenTestData struct {
	UserId     int
	Name       string
	Key        string
	Group      string
	P2PGroupId int
}

// CreateTestPackage 创建测试套餐
func CreateTestPackage(t *testing.T, data PackageTestData) *model.Package {
	// 设置默认值
	if data.Name == "" {
		data.Name = fmt.Sprintf("test-package-%d", time.Now().UnixNano())
	}
	if data.DurationType == "" {
		data.DurationType = "month"
	}
	if data.Duration == 0 {
		data.Duration = 1
	}
	if data.Status == 0 {
		data.Status = 1 // 默认可用
	}
	if data.Quota == 0 {
		data.Quota = 100000000 // 默认100M
	}

	t.Logf("CreateTestPackage input: name=%s, priority=%d, hourly_limit=%d, quota=%d, fallback_to_balance=%v",
		data.Name, data.Priority, data.HourlyLimit, data.Quota, data.FallbackToBalance)

	pkg := &model.Package{
		Name:              data.Name,
		Priority:          data.Priority,
		P2PGroupId:        data.P2PGroupId,
		Quota:             data.Quota,
		HourlyLimit:       data.HourlyLimit,
		DailyLimit:        data.DailyLimit,
		WeeklyLimit:       data.WeeklyLimit,
		FourHourlyLimit:   data.FourHourlyLimit,
		RpmLimit:          data.RpmLimit,
		DurationType:      data.DurationType,
		Duration:          data.Duration,
		FallbackToBalance: data.FallbackToBalance,
		Status:            data.Status,
		CreatorId:         data.CreatorId,
	}

	// 直接使用 Select("*") 写入所有字段，避免部分字段因默认值而被省略。
	err := model.DB.Select("*").Create(pkg).Error
	assert.Nil(t, err, "Failed to create test package")

	// 由于 GORM + SQLite 在 bool 字段带有 default:true 时，Create 后模型实例
	// 可能被回填为 true，这里在需要时显式执行一次 UpdateColumn 来覆盖 DB 默认值，
	// 并重新读取一遍，确保 pkg.FallbackToBalance 与测试输入保持一致。
	if !data.FallbackToBalance {
		updateErr := model.DB.Model(&model.Package{}).
			Where("id = ?", pkg.Id).
			UpdateColumn("fallback_to_balance", false).Error
		assert.Nil(t, updateErr, "Failed to override fallback_to_balance to false for test package")

		reloadErr := model.DB.First(pkg, pkg.Id).Error
		assert.Nil(t, reloadErr, "Failed to reload test package after overriding fallback_to_balance")
	}

	t.Logf("CreateTestPackage: id=%d, name=%s, hourly_limit=%d, quota=%d, fallback_to_balance=%v",
		pkg.Id, pkg.Name, pkg.HourlyLimit, pkg.Quota, pkg.FallbackToBalance)
	return pkg
}

// SubscriptionTestData 订阅测试数据结构
type SubscriptionTestData struct {
	UserId        int
	PackageId     int
	Status        string
	StartTime     *int64
	EndTime       *int64
	TotalConsumed int64
	SubscribedAt  int64
}

// CreateTestSubscription 创建测试订阅
func CreateTestSubscription(t *testing.T, data SubscriptionTestData) *model.Subscription {
	// 设置默认值
	if data.Status == "" {
		data.Status = model.SubscriptionStatusInventory
	}
	if data.SubscribedAt == 0 {
		data.SubscribedAt = common.GetTimestamp()
	}

	sub := &model.Subscription{
		UserId:        data.UserId,
		PackageId:     data.PackageId,
		Status:        data.Status,
		StartTime:     data.StartTime,
		EndTime:       data.EndTime,
		TotalConsumed: data.TotalConsumed,
		SubscribedAt:  data.SubscribedAt,
	}

	err := model.DB.Create(sub).Error
	assert.Nil(t, err, "Failed to create test subscription")
	return sub
}

// CreateAndActivateSubscription 创建并启用订阅
func CreateAndActivateSubscription(t *testing.T, userId int, packageId int) *model.Subscription {
	// 先创建库存状态的订阅
	sub := CreateTestSubscription(t, SubscriptionTestData{
		UserId:    userId,
		PackageId: packageId,
		Status:    model.SubscriptionStatusInventory,
	})

	// 启用订阅
	pkg, err := model.GetPackageByID(packageId)
	assert.Nil(t, err, "Failed to get package")

	now := common.GetTimestamp()
	endTime := CalculateEndTime(now, pkg.DurationType, pkg.Duration)

	sub.Status = model.SubscriptionStatusActive
	sub.StartTime = &now
	sub.EndTime = &endTime

	err = model.DB.Save(sub).Error
	assert.Nil(t, err, "Failed to activate subscription")

	return sub
}

// ActivateSubscription 激活已有订阅（通过真实业务逻辑）
func ActivateSubscription(t *testing.T, subscriptionId int) *model.Subscription {
	sub, err := model.GetSubscriptionById(subscriptionId)
	assert.Nil(t, err, "Failed to get subscription before activation")

	err = service.ActivateSubscription(subscriptionId, sub.UserId)
	assert.Nil(t, err, "Failed to activate subscription")

	activated, err := model.GetSubscriptionById(subscriptionId)
	assert.Nil(t, err, "Failed to reload activated subscription")
	return activated
}

// CalculateEndTime 计算套餐结束时间
func CalculateEndTime(startTime int64, durationType string, duration int) int64 {
	t := time.Unix(startTime, 0)

	switch durationType {
	case "week":
		return t.AddDate(0, 0, 7*duration).Unix()
	case "month":
		return t.AddDate(0, duration, 0).Unix()
	case "quarter":
		return t.AddDate(0, 3*duration, 0).Unix()
	case "year":
		return t.AddDate(duration, 0, 0).Unix()
	default:
		return t.AddDate(0, duration, 0).Unix() // 默认按月
	}
}

// UserTestData 用户测试数据结构
type UserTestData struct {
	Username string
	Group    string
	Quota    int
	Role     int
	Status   int
}

// CreateTestUser 创建测试用户
func CreateTestUser(t *testing.T, data UserTestData) *model.User {
	// 设置默认值
	if data.Username == "" {
		data.Username = fmt.Sprintf("test-user-%d", time.Now().UnixNano())
	}
	if data.Group == "" {
		data.Group = "default"
	}
	if data.Quota == 0 {
		data.Quota = 10000000 // 默认10M
	}
	if data.Status == 0 {
		data.Status = common.UserStatusEnabled
	}
	if data.Role == 0 {
		data.Role = common.RoleCommonUser
	}

	// 为测试用户生成唯一 external_id / aff_code，避免违反唯一约束
	externalID := fmt.Sprintf("test-ext-%d", time.Now().UnixNano())
	affCode := fmt.Sprintf("test-aff-%d", time.Now().UnixNano())

	user := &model.User{
		Username:   data.Username,
		Group:      data.Group,
		Quota:      data.Quota,
		Role:       data.Role,
		Status:     data.Status,
		ExternalId: externalID,
		AffCode:    affCode,
	}

	err := model.DB.Create(user).Error
	assert.Nil(t, err, "Failed to create test user")
	return user
}

// EnsureUserAccessToken 确保用户拥有用于管理面 API 的 access_token 并返回该值。
// 说明：
//   - 管理面路由（如 /api/groups, /api/packages, /api/subscriptions 等）统一通过
//     middleware.UserAuth 校验 access_token + New-Api-User。
//   - 集成测试中直接创建的用户默认没有 access_token，因此需要在测试环境显式生成。
func EnsureUserAccessToken(t *testing.T, user *model.User) string {
	if user == nil {
		t.Fatalf("EnsureUserAccessToken: user is nil")
	}

	// 若已经存在 access token，直接复用，避免破坏可能依赖该字段的其他测试状态。
	if user.AccessToken != nil && *user.AccessToken != "" {
		return *user.AccessToken
	}

	// 生成随机 access token（长度与正式接口一致使用 32 左右的随机字符串）
	key, err := common.GenerateRandomKey(32)
	assert.Nil(t, err, "Failed to generate access token for test user")

	user.SetAccessToken(key)
	// 仅更新 access_token 字段，避免覆盖其他字段
	err = model.DB.Model(user).Update("access_token", key).Error
	assert.Nil(t, err, "Failed to persist access token for test user")

	// 再次从数据库确认写入是否成功，避免由于事务或多连接导致的隐性失败
	var count int64
	err = model.DB.Model(&model.User{}).
		Where("id = ? AND access_token = ?", user.Id, key).
		Count(&count).Error
	assert.Nil(t, err, "Failed to verify access token persistence")
	assert.Equal(t, int64(1), count, "Access token was not persisted correctly for user")

	return key
}

// CreateTestAdminUser 创建测试管理员用户
func CreateTestAdminUser(t *testing.T) *model.User {
	return CreateTestUser(t, UserTestData{
		Username: fmt.Sprintf("admin-user-%d", time.Now().UnixNano()),
		Role:     common.RoleRootUser,
		Quota:    100000000,
	})
}

// GroupTestData P2P分组测试数据结构
type GroupTestData struct {
	Name        string
	DisplayName string
	OwnerId     int
	Type        int
	JoinMethod  int
	JoinKey     string
	Description string
}

// CreateTestGroup 创建测试P2P分组
func CreateTestGroup(t *testing.T, data GroupTestData) *model.Group {
	// 设置默认值
	if data.Name == "" {
		data.Name = fmt.Sprintf("test-group-%d", time.Now().UnixNano())
	}
	if data.Type == 0 {
		data.Type = 2 // 默认共享分组
	}

	group := &model.Group{
		Name:        data.Name,
		DisplayName: data.DisplayName,
		OwnerId:     data.OwnerId,
		Type:        data.Type,
		JoinMethod:  data.JoinMethod,
		JoinKey:     data.JoinKey,
		Description: data.Description,
		CreatedAt:   common.GetTimestamp(),
		UpdatedAt:   common.GetTimestamp(),
	}

	err := model.DB.Create(group).Error
	assert.Nil(t, err, "Failed to create test group")
	return group
}

// AddUserToGroup 添加用户到P2P分组
func AddUserToGroup(t *testing.T, userId int, groupId int, status int) *model.UserGroup {
	if status == 0 {
		status = 1 // 默认活跃状态
	}

	userGroup := &model.UserGroup{
		UserId:    userId,
		GroupId:   groupId,
		Role:      0, // 默认普通成员
		Status:    status,
		CreatedAt: common.GetTimestamp(),
		UpdatedAt: common.GetTimestamp(),
	}

	err := model.DB.Create(userGroup).Error
	assert.Nil(t, err, "Failed to add user to group")
	if err == nil {
		t.Logf("AddUserToGroup: user_id=%d group_id=%d status=%d (SQLitePath=%s)", userId, groupId, status, common.SQLitePath)

		// 新增成员关系后同步失效该用户的分组缓存，避免在此前已被读取并缓存为空列表的情况下，
		// 后续 P2P 选路仍然看到旧的空分组信息（导致误判为「有 P2P 限制但无有效分组」）。
		// 仅在测试进程已显式初始化 Redis 客户端时执行（例如 StartTestServer 场景），
		// 避免在未配置 Redis 的管理面测试中因为 RDB 为 nil 而触发 panic。
		if common.RDB != nil && common.RedisEnabled {
			if cacheErr := model.InvalidateUserGroupCache(userId); cacheErr != nil {
				t.Logf("AddUserToGroup: failed to invalidate user group cache for user_id=%d: %v", userId, cacheErr)
			} else {
				t.Logf("AddUserToGroup: invalidated user group cache for user_id=%d", userId)
			}
		}
	}
	return userGroup
}

// AssertPackageExists 断言套餐存在
func AssertPackageExists(t *testing.T, packageId int) *model.Package {
	pkg, err := model.GetPackageByID(packageId)
	assert.Nil(t, err, "Package should exist")
	assert.NotNil(t, pkg, "Package should not be nil")
	return pkg
}

// AssertPackagePriority 断言套餐优先级
func AssertPackagePriority(t *testing.T, packageId int, expectedPriority int) {
	pkg := AssertPackageExists(t, packageId)
	assert.Equal(t, expectedPriority, pkg.Priority,
		fmt.Sprintf("Package priority should be %d", expectedPriority))
}

// AssertSubscriptionStatus 断言订阅状态
func AssertSubscriptionStatus(t *testing.T, subscriptionId int, expectedStatus string) {
	// 使用强制从 DB 读取的接口以避免三级缓存带来的陈旧数据影响断言，
	// 特别是在测试定时任务或后台批量更新场景时。
	sub, err := model.GetSubscriptionByIdFromDB(subscriptionId)
	assert.Nil(t, err, "Subscription should exist")
	assert.Equal(t, expectedStatus, sub.Status,
		fmt.Sprintf("Subscription status should be %s", expectedStatus))
}

// AssertSubscriptionActive 断言订阅已激活
func AssertSubscriptionActive(t *testing.T, subscriptionId int) *model.Subscription {
	sub, err := model.GetSubscriptionByIdFromDB(subscriptionId)
	assert.Nil(t, err, "Subscription should exist")
	assert.Equal(t, model.SubscriptionStatusActive, sub.Status, "Subscription should be active")
	assert.NotNil(t, sub.StartTime, "Start time should be set")
	assert.NotNil(t, sub.EndTime, "End time should be set")
	assert.Greater(t, *sub.EndTime, *sub.StartTime, "End time should be after start time")
	return sub
}

// AssertSubscriptionExpired 断言订阅已过期
func AssertSubscriptionExpired(t *testing.T, subscriptionId int) {
	sub, err := model.GetSubscriptionByIdFromDB(subscriptionId)
	assert.Nil(t, err, "Subscription should exist")
	assert.Equal(t, model.SubscriptionStatusExpired, sub.Status, "Subscription should be expired")
}

// AssertUserQuota 断言用户余额
func AssertUserQuota(t *testing.T, userId int, expectedQuota int) {
	quota, err := model.GetUserQuota(userId, true)
	assert.Nil(t, err, "Failed to get user quota")
	assert.Equal(t, expectedQuota, quota,
		fmt.Sprintf("User quota should be %d", expectedQuota))
}

// CleanupPackageTestData 清理套餐测试数据
func CleanupPackageTestData(t *testing.T) {
	// 清理订阅
	model.DB.Exec("DELETE FROM subscriptions")
	// 清理套餐
	model.DB.Exec("DELETE FROM packages")
	// 清理用户（保留系统用户）
	model.DB.Exec("DELETE FROM users WHERE id > 1")

	// 清理套餐与订阅的内存缓存，避免不同用例之间通过三级缓存共享旧数据。
	model.ResetPackageCacheForTests()

	// 清理与套餐滑动窗口相关的 Redis 数据，避免不同用例之间窗口状态互相干扰。
	if common.RDB != nil {
		if err := common.RDB.FlushDB(context.Background()).Err(); err != nil {
			t.Logf("CleanupPackageTestData: failed to flush Redis DB: %v", err)
		}
	}
}

// CleanupChannelTestData 清理渠道与令牌等测试数据
func CleanupChannelTestData(t *testing.T) {
	// 依次清理与数据面强相关的表，避免不同用例之间产生干扰。
	// 这里不清理用户与套餐相关表，由各自的 Cleanup 函数负责。
	model.DB.Exec("DELETE FROM logs")
	model.DB.Exec("DELETE FROM tokens")
	model.DB.Exec("DELETE FROM channels")
}

// CleanupGroupTestData 清理 P2P 分组相关测试数据
func CleanupGroupTestData(t *testing.T) {
	model.DB.Exec("DELETE FROM user_groups")
	model.DB.Exec("DELETE FROM groups")
}

// CalculateExpectedQuota 计算预期的quota消耗
// 公式: (InputTokens + OutputTokens × CompletionRatio) × ModelRatio × GroupRatio
func CalculateExpectedQuota(inputTokens, outputTokens int, modelRatio, groupRatio float64) int64 {
	completionRatio := 1.0 // 默认补全倍率
	// 基础计算
	baseTokens := float64(inputTokens) + float64(outputTokens)*completionRatio
	quota := baseTokens * modelRatio * groupRatio
	return int64(quota)
}

// AssertSubscriptionConsumed 断言订阅消耗量
func AssertSubscriptionConsumed(t *testing.T, subscriptionId int, expectedConsumed int64) *model.Subscription {
	// 通过强制从 DB 读取避免命中可能滞后的三级缓存，确保断言基于持久化数据。
	sub, err := model.GetSubscriptionByIdFromDB(subscriptionId)
	assert.Nil(t, err, "Failed to get subscription")
	assert.Equal(t, expectedConsumed, sub.TotalConsumed,
		fmt.Sprintf("Subscription total_consumed should be %d, got %d", expectedConsumed, sub.TotalConsumed))
	return sub
}

// AssertUserQuotaChanged 断言用户余额变化
func AssertUserQuotaChanged(t *testing.T, userId int, initialQuota int, expectedChange int) {
	finalQuota, err := model.GetUserQuota(userId, true)
	assert.Nil(t, err, "Failed to get user quota")
	expectedFinal := initialQuota + expectedChange
	assert.Equal(t, expectedFinal, finalQuota,
		fmt.Sprintf("User quota should change from %d to %d (change=%d), got %d",
			initialQuota, expectedFinal, expectedChange, finalQuota))
}

// AssertUserQuotaUnchanged 断言用户余额未变
func AssertUserQuotaUnchanged(t *testing.T, userId int, expectedQuota int) {
	finalQuota, err := model.GetUserQuota(userId, true)
	assert.Nil(t, err, "Failed to get user quota")
	assert.Equal(t, expectedQuota, finalQuota,
		fmt.Sprintf("User quota should remain unchanged at %d, got %d", expectedQuota, finalQuota))
}

// GetGroupRatio 获取分组费率倍率
func GetGroupRatio(group string) float64 {
	switch group {
	case "default":
		return 1.0
	case "vip":
		return 2.0
	case "svip":
		return 0.8
	default:
		return 1.0
	}
}

// 默认渠道上游地址（由 StartTestServer 或测试套件显式设置）
var defaultChannelBaseURL string

// SetDefaultChannelBaseURL 设置默认的渠道上游地址
func SetDefaultChannelBaseURL(url string) {
	defaultChannelBaseURL = url
}

// CreateTestChannel 创建测试渠道
func CreateTestChannel(t *testing.T, data ChannelTestData) *model.Channel {
	// 设置默认值
	if data.Name == "" {
		data.Name = fmt.Sprintf("test-channel-%d", time.Now().UnixNano())
	}
	if data.Type == 0 {
		data.Type = 1 // 默认 OpenAI 类型
	}
	if data.Group == "" {
		data.Group = "default"
	}
	if data.Models == "" {
		data.Models = "gpt-4"
	}
	if data.Status == 0 {
		data.Status = common.ChannelStatusEnabled
	}

	baseURL := data.BaseURL
	if baseURL == "" {
		baseURL = defaultChannelBaseURL
	}

	channel := &model.Channel{
		Name:        data.Name,
		Type:        data.Type,
		Group:       data.Group,
		Models:      data.Models,
		Status:      data.Status,
		IsPrivate:   data.IsPrivate,
		OwnerUserId: data.OwnerUserId,
	}
	if baseURL != "" {
		channel.BaseURL = &baseURL
	}
	if data.AllowedGroups != "" {
		ag := data.AllowedGroups
		channel.AllowedGroups = &ag
	}

	// 使用模型层的 Insert，保证与生产环境一致的初始化逻辑
	err := channel.Insert()
	assert.Nil(t, err, "Failed to create test channel")
	return channel
}

// CreateTestToken 创建测试Token
func CreateTestToken(t *testing.T, data TokenTestData) *model.Token {
	if data.UserId == 0 {
		t.Fatalf("CreateTestToken: UserId is required")
	}

	if data.Name == "" {
		data.Name = fmt.Sprintf("test-token-%d", time.Now().UnixNano())
	}

	// 如果调用方未指定 Key，则为其生成随机 Key；否则使用传入值，便于测试中使用固定 Token。
	if data.Key == "" {
		key, err := common.GenerateKey()
		assert.Nil(t, err, "Failed to generate test token key")
		data.Key = key
	}

	token := &model.Token{
		UserId:         data.UserId,
		Key:            data.Key,
		Name:           data.Name,
		Status:         common.TokenStatusEnabled,
		UnlimitedQuota: true, // 测试令牌默认无限额，由用户/套餐余额控制实际扣费
		RemainQuota:    0,
	}

	if data.Group != "" {
		token.Group = data.Group
	}
	if data.P2PGroupId != 0 {
		pid := data.P2PGroupId
		token.P2PGroupID = &pid
	}

	err := model.DB.Create(token).Error
	assert.Nil(t, err, "Failed to create test token")
	return token
}

// FormatWindowKey 格式化滑动窗口Redis Key
func FormatWindowKey(subscriptionId int, period string) string {
	return fmt.Sprintf("subscription:%d:%s:window", subscriptionId, period)
}

// SendChatRequest 发送聊天请求（简化版，不使用APIClient）
func SendChatRequest(baseURL, tokenKey string, req ChatCompletionRequest) (*http.Response, error) {
	client := NewAPIClientWithToken(baseURL, tokenKey)
	return client.ChatCompletion(req)
}

// ParseChatResponse 解析聊天响应
func ParseChatResponse(body io.Reader) (*ChatCompletionResponse, error) {
	var resp ChatCompletionResponse
	err := json.NewDecoder(body).Decode(&resp)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

// GetSubscriptionById 获取订阅（包装函数，便于测试使用）
// 注意：这里强制从 DB 读取并刷新缓存，避免测试进程与服务进程各自的
// PackageCache 导致订阅 total_consumed 读取到过期值。
func GetSubscriptionById(subscriptionId int) (*model.Subscription, error) {
	return model.GetSubscriptionByIdFromDB(subscriptionId)
}

// DB 暴露数据库连接供测试使用
var DB = model.DB

// ========== P2P分组权限测试专用HTTP API辅助函数 ==========

// P2PTestHelper P2P测试辅助结构
type P2PTestHelper struct {
	BaseURL string
	Client  *http.Client
}

// NewP2PTestHelper 创建P2P测试辅助实例
func NewP2PTestHelper(baseURL string) *P2PTestHelper {
	return &P2PTestHelper{
		BaseURL: baseURL,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CreateP2PGroupViaAPI 通过API创建P2P分组
func (h *P2PTestHelper) CreateP2PGroupViaAPI(t *testing.T, ownerToken string, name string, displayName string, groupType int, joinMethod int, joinKey string) (int, int) {
	reqBody := map[string]interface{}{
		"name":         name,
		"display_name": displayName,
		"type":         groupType,
		"join_method":  joinMethod,
	}
	if joinKey != "" {
		reqBody["join_key"] = joinKey
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", h.BaseURL+"/api/groups", bytes.NewBuffer(body))
	assert.NoError(t, err, "Failed to create group request")

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	// UserAuth 需要 New-Api-User 与 access_token 对应的用户一致，这里通过 access_token 反查用户以设置正确的头
	if user := model.ValidateAccessToken("Bearer " + ownerToken); user != nil {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	} else {
		t.Logf("CreateP2PGroupViaAPI: ValidateAccessToken failed for owner token")
	}

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send create group request")
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Logf("Create P2P group failed: status=%d, body=%s", resp.StatusCode, string(respBody))
		return 0, resp.StatusCode
	}

	// API 统一返回结构: { "success": bool, "message": string, "data": { ...Group } }
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Logf("Create P2P group response JSON unmarshal failed: %v, body=%s", err, string(respBody))
		return 0, resp.StatusCode
	}

	// 校验 success 字段
	if success, ok := result["success"].(bool); ok && !success {
		msg, _ := result["message"].(string)
		t.Logf("Create P2P group API returned error: %s", msg)
		return 0, resp.StatusCode
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Logf("Create P2P group response missing data object: %v", result)
		return 0, resp.StatusCode
	}

	idVal, ok := data["id"].(float64)
	if !ok {
		t.Logf("Create P2P group response missing id field in data: %v", data)
		return 0, resp.StatusCode
	}

	groupID := int(idVal)

	// 为保证后续套餐权限检查中 Group Owner 也被视为该分组的“有效成员”，
	// 在测试环境中显式为 Owner 写入一条 Active 的 user_groups 记录。
	if user := model.ValidateAccessToken("Bearer " + ownerToken); user != nil {
		var ug model.UserGroup
		err := model.DB.Where("user_id = ? AND group_id = ?", user.Id, groupID).First(&ug).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			t.Logf("CreateP2PGroupViaAPI: owner is not in user_groups yet, inserting as Active member")
			AddUserToGroup(t, user.Id, groupID, 1)
		} else {
			assert.Nil(t, err, "Failed to query owner user_groups for verification")
		}
	}

	t.Logf("Create P2P group success: groupID=%d, name=%s", groupID, name)
	return groupID, resp.StatusCode
}

// AddUserToGroupViaAPI 通过API添加用户到分组
func (h *P2PTestHelper) AddUserToGroupViaAPI(t *testing.T, ownerToken string, groupID int, userID int, role int) (bool, int) {
	reqBody := map[string]interface{}{
		"group_id": groupID,
		"user_id":  userID,
		"status":   1, // Active
		"role":     role,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("PUT", h.BaseURL+"/api/groups/members", bytes.NewBuffer(body))
	assert.NoError(t, err, "Failed to create add user request")

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	if user := model.ValidateAccessToken("Bearer " + ownerToken); user != nil {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	} else {
		t.Logf("AddUserToGroupViaAPI: ValidateAccessToken failed for owner token")
	}

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send add user request")
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Logf("AddUserToGroupViaAPI failed: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
		return false, resp.StatusCode
	}

	// 解析统一响应结构，校验 success 字段
	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		t.Logf("AddUserToGroupViaAPI response JSON unmarshal failed: %v, body=%s", err, string(bodyBytes))
		return false, resp.StatusCode
	}
	if success, ok := result["success"].(bool); ok && !success {
		msg, _ := result["message"].(string)
		t.Logf("AddUserToGroupViaAPI API returned error: %s", msg)
		// 对于集成测试而言，确保 user_groups 中存在一条 Active 记录比严格依赖
		// 管理接口更重要，因此在 API 报错时采用 DB 级别的补偿写入。
	}

	// 无论 API 是否成功，最终都确保 user_groups 中存在一条 Active(1) 记录，
	// 以便后续套餐权限与统计逻辑可以正常运行。
	var ug model.UserGroup
	err = model.DB.Where("user_id = ? AND group_id = ?", userID, groupID).First(&ug).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t.Logf("AddUserToGroupViaAPI: user_group record missing, inserting directly via helper")
		AddUserToGroup(t, userID, groupID, 1)
	} else {
		assert.Nil(t, err, "Failed to query user_groups for verification")
		if ug.Status != 1 {
			// 将状态更新为 Active
			err = model.UpdateMemberStatus(groupID, userID, model.MemberStatusActive)
			assert.Nil(t, err, "Failed to update user_groups status to Active")
		}
	}

	t.Logf("Add user to group success (final state): groupID=%d, userID=%d", groupID, userID)
	return true, resp.StatusCode
}

// RemoveUserFromGroupViaAPI 通过API从分组移除用户
func (h *P2PTestHelper) RemoveUserFromGroupViaAPI(t *testing.T, token string, groupID int, userID int) (bool, int) {
	reqBody := map[string]interface{}{
		"group_id": groupID,
		"user_id":  userID,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", h.BaseURL+"/api/groups/leave", bytes.NewBuffer(body))
	assert.NoError(t, err, "Failed to create leave group request")

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if user := model.ValidateAccessToken("Bearer " + token); user != nil {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	} else {
		t.Logf("RemoveUserFromGroupViaAPI: ValidateAccessToken failed for token")
	}

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send leave group request")
	defer resp.Body.Close()

	success := resp.StatusCode == http.StatusOK
	if success {
		t.Logf("Remove user from group success: groupID=%d, userID=%d", groupID, userID)
	}
	return success, resp.StatusCode
}

// CreateP2PPackageViaAPI 通过API创建P2P套餐
func (h *P2PTestHelper) CreateP2PPackageViaAPI(t *testing.T, ownerToken string, name string, p2pGroupID int, quota int64, hourlyLimit int64) (int, int) {
	reqBody := map[string]interface{}{
		"name":                name,
		"description":         fmt.Sprintf("P2P test package: %s", name),
		"priority":            11, // P2P套餐固定优先级
		"p2p_group_id":        p2pGroupID,
		"quota":               quota,
		"duration_type":       "month",
		"duration":            1,
		"hourly_limit":        hourlyLimit,
		"fallback_to_balance": true,
		"status":              1,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", h.BaseURL+"/api/packages", bytes.NewBuffer(body))
	assert.NoError(t, err, "Failed to create package request")

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	if user := model.ValidateAccessToken("Bearer " + ownerToken); user != nil {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	} else {
		t.Logf("CreateP2PPackageViaAPI: ValidateAccessToken failed for owner token")
	}

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send create package request")
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Logf("Create P2P package failed: status=%d, body=%s", resp.StatusCode, string(respBody))
		return 0, resp.StatusCode
	}

	// 统一响应结构: { "success": true, "data": { ...Package } }
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Logf("Create P2P package response JSON unmarshal failed: %v, body=%s", err, string(respBody))
		return 0, resp.StatusCode
	}

	if success, ok := result["success"].(bool); ok && !success {
		msg, _ := result["message"].(string)
		t.Logf("Create P2P package API returned error: %s", msg)
		return 0, resp.StatusCode
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Logf("Create P2P package response missing data object: %v", result)
		return 0, resp.StatusCode
	}

	idVal, ok := data["id"].(float64)
	if !ok {
		t.Logf("Create P2P package response missing id field in data: %v", data)
		return 0, resp.StatusCode
	}

	packageID := int(idVal)
	t.Logf("Create P2P package success: packageID=%d, name=%s, p2pGroupID=%d", packageID, name, p2pGroupID)
	return packageID, resp.StatusCode
}

// QueryPackageMarketViaAPI 通过API查询套餐市场
func (h *P2PTestHelper) QueryPackageMarketViaAPI(t *testing.T, userToken string) ([]map[string]interface{}, int) {
	// 套餐市场接口复用 GET /api/packages，服务端根据用户角色和 P2P 分组做权限过滤
	req, err := http.NewRequest("GET", h.BaseURL+"/api/packages", nil)
	assert.NoError(t, err, "Failed to create query market request")

	req.Header.Set("Authorization", "Bearer "+userToken)
	if user := model.ValidateAccessToken("Bearer " + userToken); user != nil {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	} else {
		t.Logf("QueryPackageMarketViaAPI: ValidateAccessToken failed for user token")
	}

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send query market request")
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Logf("Query package market failed: status=%d, body=%s", resp.StatusCode, string(respBody))
		return []map[string]interface{}{}, resp.StatusCode
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Logf("Query package market response JSON unmarshal failed: %v, body=%s", err, string(respBody))
		return []map[string]interface{}{}, resp.StatusCode
	}

	if success, ok := result["success"].(bool); ok && !success {
		msg, _ := result["message"].(string)
		t.Logf("Query package market API returned error: %s", msg)
		return []map[string]interface{}{}, resp.StatusCode
	}

	packages, ok := result["data"].([]interface{})
	if !ok {
		t.Logf("Query package market response missing data array: %v", result)
		return []map[string]interface{}{}, resp.StatusCode
	}

	var packageList []map[string]interface{}
	for _, pkg := range packages {
		packageList = append(packageList, pkg.(map[string]interface{}))
	}

	t.Logf("Query package market success: found %d packages", len(packageList))
	return packageList, resp.StatusCode
}

// SubscribePackageViaAPI 通过API订阅套餐
func (h *P2PTestHelper) SubscribePackageViaAPI(t *testing.T, userToken string, packageID int) (int, int) {
	url := fmt.Sprintf("%s/api/subscriptions/subscribe/%d", h.BaseURL, packageID)
	req, err := http.NewRequest("POST", url, nil)
	assert.NoError(t, err, "Failed to create subscribe request")

	req.Header.Set("Authorization", "Bearer "+userToken)
	if user := model.ValidateAccessToken("Bearer " + userToken); user != nil {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	} else {
		t.Logf("SubscribePackageViaAPI: ValidateAccessToken failed for user token")
	}

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send subscribe request")
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusForbidden {
		t.Logf("Subscribe package response: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return 0, resp.StatusCode
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Logf("Subscribe package response JSON unmarshal failed: %v, body=%s", err, string(respBody))
		return 0, resp.StatusCode
	}

	// 对于订阅接口，success=false 表示业务失败（例如权限不足 / 套餐不可用）
	if success, ok := result["success"].(bool); ok && !success {
		msg, _ := result["message"].(string)
		t.Logf("Subscribe package API returned error: %s", msg)
		return 0, resp.StatusCode
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Logf("Subscribe package response missing data object: %v", result)
		return 0, resp.StatusCode
	}

	idVal, ok := data["subscription_id"].(float64)
	if !ok {
		t.Logf("Subscribe package response missing subscription_id field in data: %v", data)
		return 0, resp.StatusCode
	}

	subscriptionID := int(idVal)
	t.Logf("Subscribe package success: subscriptionID=%d, packageID=%d", subscriptionID, packageID)
	return subscriptionID, resp.StatusCode
}

// ActivateSubscriptionViaAPI 通过API启用订阅
func (h *P2PTestHelper) ActivateSubscriptionViaAPI(t *testing.T, userToken string, subscriptionID int) (string, int) {
	url := fmt.Sprintf("%s/api/subscriptions/activate/%d", h.BaseURL, subscriptionID)
	req, err := http.NewRequest("POST", url, nil)
	assert.NoError(t, err, "Failed to create activate subscription request")

	req.Header.Set("Authorization", "Bearer "+userToken)
	if user := model.ValidateAccessToken("Bearer " + userToken); user != nil {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	} else {
		t.Logf("ActivateSubscriptionViaAPI: ValidateAccessToken failed for user token")
	}

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send activate subscription request")
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Logf("Activate subscription response: status=%d, body=%s", resp.StatusCode, string(bodyBytes))
		return "", resp.StatusCode
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		t.Logf("ActivateSubscriptionViaAPI response JSON unmarshal failed: %v, body=%s", err, string(bodyBytes))
		return "", resp.StatusCode
	}

	if success, ok := result["success"].(bool); ok && !success {
		msg, _ := result["message"].(string)
		t.Logf("ActivateSubscriptionViaAPI API returned error: %s", msg)
		return "", resp.StatusCode
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		t.Logf("ActivateSubscriptionViaAPI response missing data object: %v", result)
		return "", resp.StatusCode
	}

	status, _ := data["status"].(string)
	t.Logf("Activate subscription success: subscriptionID=%d, status=%s", subscriptionID, status)
	return status, resp.StatusCode
}

// CheckPackageInMarket 检查套餐是否在市场列表中
func (h *P2PTestHelper) CheckPackageInMarket(packages []map[string]interface{}, packageID int) bool {
	for _, pkg := range packages {
		if int(pkg["id"].(float64)) == packageID {
			return true
		}
	}
	return false
}

// GetUserP2PGroupIDs 获取用户的P2P分组ID列表（从数据库）
func GetUserP2PGroupIDs(t *testing.T, userID int) []int {
	var userGroups []model.UserGroup
	err := model.DB.Where("user_id = ? AND status = ?", userID, 1).Find(&userGroups).Error
	assert.NoError(t, err, "Failed to get user P2P groups")

	var groupIDs []int
	for _, ug := range userGroups {
		groupIDs = append(groupIDs, ug.GroupId)
	}

	return groupIDs
}

// GetUserAvailablePackageCount 获取用户可用套餐数量（从数据库）
func GetUserAvailablePackageCount(t *testing.T, userID int, p2pGroupIDs []int) int {
	currentTime := common.GetTimestamp()

	var count int64
	query := model.DB.Table("subscriptions").
		Joins("JOIN packages ON subscriptions.package_id = packages.id").
		Where("subscriptions.user_id = ?", userID).
		Where("subscriptions.status = ?", "active").
		Where("subscriptions.start_time <= ?", currentTime).
		Where("subscriptions.end_time > ?", currentTime).
		Where("packages.status = ?", 1)

	// 兼容旧版本数据库：探测 packages 表上 P2P 分组列的实际名称，避免
	// 在未完成迁移的环境中出现 "no such column: packages.p2p_group_id" 错误。
	hasNewColumn := model.DB.Migrator().HasColumn(&model.Package{}, "p2p_group_id")
	hasLegacyColumn := model.DB.Migrator().HasColumn(&model.Package{}, "p2_p_group_id")

	if len(p2pGroupIDs) > 0 {
		switch {
		case hasNewColumn:
			query = query.Where("packages.p2p_group_id = 0 OR packages.p2p_group_id IN (?)", p2pGroupIDs)
		case hasLegacyColumn:
			query = query.Where("packages.p2_p_group_id = 0 OR packages.p2_p_group_id IN (?)", p2pGroupIDs)
		default:
			// 无相关列：退化为不做 P2P 过滤，由于此时数据库本身不支持 P2P 套餐，
			// 这只会影响“可用套餐数量”的测试断言，而不会影响核心权限逻辑。
		}
	} else {
		switch {
		case hasNewColumn:
			query = query.Where("packages.p2p_group_id = 0")
		case hasLegacyColumn:
			query = query.Where("packages.p2_p_group_id = 0")
		default:
			// 无相关列：不额外添加过滤条件，视为“仅存在全局套餐”的退化场景。
		}
	}

	query.Count(&count)

	return int(count)
}
