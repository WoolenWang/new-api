package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
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
		CreatedAt:         common.GetTimestamp(),
		UpdatedAt:         common.GetTimestamp(),
	}

	err := model.DB.Create(pkg).Error
	assert.Nil(t, err, "Failed to create test package")
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

	user := &model.User{
		Username: data.Username,
		Group:    data.Group,
		Quota:    data.Quota,
		Role:     data.Role,
		Status:   data.Status,
	}

	err := model.DB.Create(user).Error
	assert.Nil(t, err, "Failed to create test user")
	return user
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
	sub, err := model.GetSubscriptionById(subscriptionId)
	assert.Nil(t, err, "Subscription should exist")
	assert.Equal(t, expectedStatus, sub.Status,
		fmt.Sprintf("Subscription status should be %s", expectedStatus))
}

// AssertSubscriptionActive 断言订阅已激活
func AssertSubscriptionActive(t *testing.T, subscriptionId int) *model.Subscription {
	sub, err := model.GetSubscriptionById(subscriptionId)
	assert.Nil(t, err, "Subscription should exist")
	assert.Equal(t, model.SubscriptionStatusActive, sub.Status, "Subscription should be active")
	assert.NotNil(t, sub.StartTime, "Start time should be set")
	assert.NotNil(t, sub.EndTime, "End time should be set")
	assert.Greater(t, *sub.EndTime, *sub.StartTime, "End time should be after start time")
	return sub
}

// AssertSubscriptionExpired 断言订阅已过期
func AssertSubscriptionExpired(t *testing.T, subscriptionId int) {
	sub, err := model.GetSubscriptionById(subscriptionId)
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
	// 清理用户分组关系
	model.DB.Exec("DELETE FROM user_groups")
	// 清理分组
	model.DB.Exec("DELETE FROM groups")
	// 清理用户（保留系统用户）
	model.DB.Exec("DELETE FROM users WHERE id > 1")
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
	sub, err := model.GetSubscriptionById(subscriptionId)
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

// CreateTestChannel 创建测试渠道
func CreateTestChannel(t *testing.T, name string, group string, models string, baseURL string) *model.Channel {
	channel := &model.Channel{
		Name:    name,
		Type:    1, // OpenAI type
		Group:   group,
		Models:  models,
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}
	err := model.DB.Create(channel).Error
	assert.Nil(t, err, "Failed to create test channel")
	return channel
}

// CreateTestToken 创建测试Token
func CreateTestToken(t *testing.T, userId int, name string) *model.Token {
	tokenKey := fmt.Sprintf("sk-test-%d", time.Now().UnixNano())
	token := &model.Token{
		UserId:         userId,
		Key:            tokenKey,
		Name:           name,
		Status:         common.TokenStatusEnabled,
		UnlimitedQuota: false,
		RemainQuota:    0, // 使用用户余额
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
func GetSubscriptionById(subscriptionId int) (*model.Subscription, error) {
	return model.GetSubscriptionById(subscriptionId)
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

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send create group request")
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Logf("Create P2P group failed: status=%d, body=%s", resp.StatusCode, string(respBody))
		return 0, resp.StatusCode
	}

	var result map[string]interface{}
	json.Unmarshal(respBody, &result)

	groupID := int(result["id"].(float64))
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

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send add user request")
	defer resp.Body.Close()

	success := resp.StatusCode == http.StatusOK
	if success {
		t.Logf("Add user to group success: groupID=%d, userID=%d", groupID, userID)
	}
	return success, resp.StatusCode
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

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send create package request")
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Logf("Create P2P package failed: status=%d, body=%s", resp.StatusCode, string(respBody))
		return 0, resp.StatusCode
	}

	var result map[string]interface{}
	json.Unmarshal(respBody, &result)

	packageID := int(result["id"].(float64))
	t.Logf("Create P2P package success: packageID=%d, name=%s, p2pGroupID=%d", packageID, name, p2pGroupID)
	return packageID, resp.StatusCode
}

// QueryPackageMarketViaAPI 通过API查询套餐市场
func (h *P2PTestHelper) QueryPackageMarketViaAPI(t *testing.T, userToken string) ([]map[string]interface{}, int) {
	req, err := http.NewRequest("GET", h.BaseURL+"/api/packages/market", nil)
	assert.NoError(t, err, "Failed to create query market request")

	req.Header.Set("Authorization", "Bearer "+userToken)

	resp, err := h.Client.Do(req)
	assert.NoError(t, err, "Failed to send query market request")
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Logf("Query package market failed: status=%d, body=%s", resp.StatusCode, string(respBody))
		return []map[string]interface{}{}, resp.StatusCode
	}

	var result map[string]interface{}
	json.Unmarshal(respBody, &result)

	packages, ok := result["data"].([]interface{})
	if !ok {
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
	json.Unmarshal(respBody, &result)

	subscriptionID := int(result["subscription_id"].(float64))
	t.Logf("Subscribe package success: subscriptionID=%d, packageID=%d", subscriptionID, packageID)
	return subscriptionID, resp.StatusCode
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

	if len(p2pGroupIDs) > 0 {
		query = query.Where("packages.p2p_group_id = 0 OR packages.p2p_group_id IN (?)", p2pGroupIDs)
	} else {
		query = query.Where("packages.p2p_group_id = 0")
	}

	query.Count(&count)

	return int(count)
}
