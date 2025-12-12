package p2p_permission_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scene_test/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// P2PPermissionTestSuite P2P分组与套餐权限组合测试套件
// 测试目标: 验证P2P套餐的权限隔离、订阅限制、动态权限变更等关键安全特性
type P2PPermissionTestSuite struct {
	suite.Suite
	server    *testutil.TestServer
	p2pHelper *testutil.P2PTestHelper
	baseURL   string

	// 测试数据
	ownerUser    *model.User // 分组Owner
	memberUserA  *model.User // 普通成员用户A
	memberUserB  *model.User // 普通成员用户B
	outsiderUser *model.User // 未加入分组的外部用户

	ownerToken    *model.Token
	memberAToken  *model.Token
	memberBToken  *model.Token
	outsiderToken *model.Token
}

// SetupSuite 测试套件初始化
func (s *P2PPermissionTestSuite) SetupSuite() {
	// 启动测试服务器
	cfg := testutil.DefaultConfig()
	cfg.UseInMemoryDB = true
	cfg.Verbose = false

	var err error
	s.server, err = testutil.StartServer(cfg)
	assert.NoError(s.T(), err, "Failed to start test server")

	s.baseURL = s.server.BaseURL
	s.p2pHelper = testutil.NewP2PTestHelper(s.baseURL)

	s.T().Log("P2P Permission Test Suite initialized")
}

// TearDownSuite 测试套件清理
func (s *P2PPermissionTestSuite) TearDownSuite() {
	if s.server != nil {
		s.server.Stop()
	}
	s.T().Log("P2P Permission Test Suite cleaned up")
}

// SetupTest 每个测试用例前的准备
func (s *P2PPermissionTestSuite) SetupTest() {
	// 清理旧数据
	testutil.CleanupPackageTestData(s.T())

	// 创建测试用户
	s.ownerUser = testutil.CreateTestUser(s.T(), testutil.UserTestData{
		Username: fmt.Sprintf("owner-%d", time.Now().UnixNano()),
		Group:    "vip",
		Quota:    100000000, // 100M
		Role:     common.RoleCommonUser,
	})

	s.memberUserA = testutil.CreateTestUser(s.T(), testutil.UserTestData{
		Username: fmt.Sprintf("memberA-%d", time.Now().UnixNano()),
		Group:    "default",
		Quota:    50000000, // 50M
	})

	s.memberUserB = testutil.CreateTestUser(s.T(), testutil.UserTestData{
		Username: fmt.Sprintf("memberB-%d", time.Now().UnixNano()),
		Group:    "default",
		Quota:    50000000,
	})

	s.outsiderUser = testutil.CreateTestUser(s.T(), testutil.UserTestData{
		Username: fmt.Sprintf("outsider-%d", time.Now().UnixNano()),
		Group:    "default",
		Quota:    50000000,
	})

	// 创建Token
	s.ownerToken = testutil.CreateTestToken(s.T(), s.ownerUser.Id, "owner-token")
	s.memberAToken = testutil.CreateTestToken(s.T(), s.memberUserA.Id, "memberA-token")
	s.memberBToken = testutil.CreateTestToken(s.T(), s.memberUserB.Id, "memberB-token")
	s.outsiderToken = testutil.CreateTestToken(s.T(), s.outsiderUser.Id, "outsider-token")

	s.T().Logf("Test users created: owner=%d, memberA=%d, memberB=%d, outsider=%d",
		s.ownerUser.Id, s.memberUserA.Id, s.memberUserB.Id, s.outsiderUser.Id)
}

// TearDownTest 每个测试用例后的清理
func (s *P2PPermissionTestSuite) TearDownTest() {
	// 由SetupTest中的Cleanup处理
}

// TestPP01_P2PPackageOnlyVisibleToGroupMembers 测试PP-01: P2P套餐仅组内可见
//
// Test ID: PP-01
// Priority: P0
// Test Scenario: P2P套餐仅组内可见
//
//	用户A未加入G1分组，查询套餐市场时，绑定到G1的P2P套餐不应该出现在列表中。
//
// Expected Result:
//  1. 外部用户查询套餐市场，不显示P2P套餐
//  2. 分组成员查询套餐市场，显示P2P套餐
//  3. 分组Owner查询套餐市场，显示P2P套餐
func (s *P2PPermissionTestSuite) TestPP01_P2PPackageOnlyVisibleToGroupMembers() {
	t := s.T()
	t.Log("PP-01: Testing P2P package visibility - only visible to group members")

	// Arrange: 创建P2P分组G1
	groupID, statusCode := s.p2pHelper.CreateP2PGroupViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("test-group-%d", time.Now().UnixNano()),
		"Test Group G1",
		2, // Shared
		1, // 审核制
		"",
	)
	assert.Equal(t, http.StatusOK, statusCode, "创建分组应该成功")
	assert.Greater(t, groupID, 0, "分组ID应该大于0")

	// Arrange: 添加memberUserA到G1
	success, _ := s.p2pHelper.AddUserToGroupViaAPI(
		t, s.ownerToken.Key, groupID, s.memberUserA.Id, 0,
	)
	assert.True(t, success, "添加成员应该成功")

	// Arrange: 创建P2P套餐（绑定到G1）
	packageID, statusCode := s.p2pHelper.CreateP2PPackageViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("p2p-package-%d", time.Now().UnixNano()),
		groupID,
		200000000, // 200M quota
		20000000,  // 20M hourly_limit
	)
	assert.Equal(t, http.StatusOK, statusCode, "创建P2P套餐应该成功")
	assert.Greater(t, packageID, 0, "套餐ID应该大于0")

	// 验证套餐确实绑定到P2P分组
	pkg := testutil.AssertPackageExists(t, packageID)
	assert.Equal(t, groupID, pkg.P2PGroupId, "套餐应该绑定到P2P分组G1")
	assert.Equal(t, 11, pkg.Priority, "P2P套餐优先级应该固定为11")

	// Act & Assert 1: 外部用户查询套餐市场，不应该看到P2P套餐
	t.Log("验证外部用户不可见P2P套餐")
	outsiderPackages, statusCode := s.p2pHelper.QueryPackageMarketViaAPI(t, s.outsiderToken.Key)
	assert.Equal(t, http.StatusOK, statusCode, "查询套餐市场应该成功")
	assert.False(t, s.p2pHelper.CheckPackageInMarket(outsiderPackages, packageID),
		"外部用户不应该在套餐市场看到P2P套餐")

	// Act & Assert 2: 分组成员查询套餐市场，应该看到P2P套餐
	t.Log("验证分组成员可见P2P套餐")
	memberPackages, statusCode := s.p2pHelper.QueryPackageMarketViaAPI(t, s.memberAToken.Key)
	assert.Equal(t, http.StatusOK, statusCode, "查询套餐市场应该成功")
	assert.True(t, s.p2pHelper.CheckPackageInMarket(memberPackages, packageID),
		"分组成员应该在套餐市场看到P2P套餐")

	// Act & Assert 3: 分组Owner查询套餐市场，应该看到P2P套餐
	t.Log("验证分组Owner可见P2P套餐")
	ownerPackages, statusCode := s.p2pHelper.QueryPackageMarketViaAPI(t, s.ownerToken.Key)
	assert.Equal(t, http.StatusOK, statusCode, "查询套餐市场应该成功")
	assert.True(t, s.p2pHelper.CheckPackageInMarket(ownerPackages, packageID),
		"分组Owner应该在套餐市场看到P2P套餐")

	t.Log("PP-01: Test completed - P2P package visibility verified")
}

// TestPP02_P2PPackageOnlySubscribableByGroupMembers 测试PP-02: P2P套餐仅组内可订阅
//
// Test ID: PP-02
// Priority: P0
// Test Scenario: P2P套餐仅组内可订阅
//
//	用户A未加入G1分组，尝试订阅绑定到G1的P2P套餐，应该返回403 Forbidden。
//
// Expected Result:
//
//	外部用户订阅P2P套餐返回403错误
func (s *P2PPermissionTestSuite) TestPP02_P2PPackageOnlySubscribableByGroupMembers() {
	t := s.T()
	t.Log("PP-02: Testing P2P package subscription - only subscribable by group members")

	// Arrange: 创建P2P分组G1
	groupID, _ := s.p2pHelper.CreateP2PGroupViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("test-group-%d", time.Now().UnixNano()),
		"Test Group G1",
		2, 1, "",
	)

	// Arrange: 创建P2P套餐（绑定到G1）
	packageID, _ := s.p2pHelper.CreateP2PPackageViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("p2p-package-%d", time.Now().UnixNano()),
		groupID, 200000000, 20000000,
	)

	// Act: 外部用户尝试订阅P2P套餐
	t.Log("验证外部用户订阅P2P套餐被拒绝")
	subscriptionID, statusCode := s.p2pHelper.SubscribePackageViaAPI(
		t, s.outsiderToken.Key, packageID,
	)

	// Assert: 应该返回403 Forbidden
	assert.Equal(t, http.StatusForbidden, statusCode,
		"外部用户订阅P2P套餐应该返回403 Forbidden")
	assert.Equal(t, 0, subscriptionID, "订阅ID应该为0（订阅失败）")

	// 验证数据库中没有创建订阅记录
	var count int64
	model.DB.Model(&model.Subscription{}).
		Where("user_id = ? AND package_id = ?", s.outsiderUser.Id, packageID).
		Count(&count)
	assert.Equal(t, int64(0), count, "数据库中不应该存在订阅记录")

	t.Log("PP-02: Test completed - P2P package subscription permission verified")
}

// TestPP03_CanSubscribeAfterJoiningGroup 测试PP-03: 加入分组后可订阅
//
// Test ID: PP-03
// Priority: P0
// Test Scenario: 加入分组后可订阅
//
//	用户A加入G1分组（status=1 Active），然后订阅绑定到G1的P2P套餐，应该成功。
//
// Expected Result:
//
//	用户加入分组后，订阅P2P套餐成功
func (s *P2PPermissionTestSuite) TestPP03_CanSubscribeAfterJoiningGroup() {
	t := s.T()
	t.Log("PP-03: Testing subscription after joining group")

	// Arrange: 创建P2P分组G1
	groupID, _ := s.p2pHelper.CreateP2PGroupViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("test-group-%d", time.Now().UnixNano()),
		"Test Group G1",
		2, 1, "",
	)

	// Arrange: 创建P2P套餐（绑定到G1）
	packageID, _ := s.p2pHelper.CreateP2PPackageViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("p2p-package-%d", time.Now().UnixNano()),
		groupID, 200000000, 20000000,
	)

	// Act 1: 用户A加入分组G1（status=1 Active）
	t.Log("步骤1: 用户A加入分组G1")
	success, statusCode := s.p2pHelper.AddUserToGroupViaAPI(
		t, s.ownerToken.Key, groupID, s.memberUserA.Id, 0,
	)
	assert.Equal(t, http.StatusOK, statusCode, "添加用户到分组应该成功")
	assert.True(t, success, "添加用户到分组应该返回true")

	// 验证用户确实加入了分组（数据库验证）
	var userGroup model.UserGroup
	err := model.DB.Where("user_id = ? AND group_id = ? AND status = ?",
		s.memberUserA.Id, groupID, 1).First(&userGroup).Error
	assert.NoError(t, err, "应该能在数据库中找到用户分组关系记录")
	assert.Equal(t, 1, userGroup.Status, "用户分组状态应该为Active(1)")

	// Act 2: 用户A订阅P2P套餐
	t.Log("步骤2: 用户A订阅P2P套餐")
	subscriptionID, statusCode := s.p2pHelper.SubscribePackageViaAPI(
		t, s.memberAToken.Key, packageID,
	)

	// Assert: 订阅应该成功
	assert.Equal(t, http.StatusOK, statusCode, "订阅P2P套餐应该成功")
	assert.Greater(t, subscriptionID, 0, "订阅ID应该大于0")

	// 验证订阅记录存在且状态正确
	sub, err := model.GetSubscriptionById(subscriptionID)
	assert.NoError(t, err, "应该能获取订阅记录")
	assert.NotNil(t, sub, "订阅记录不应该为nil")
	assert.Equal(t, s.memberUserA.Id, sub.UserId, "订阅用户ID应该匹配")
	assert.Equal(t, packageID, sub.PackageId, "订阅套餐ID应该匹配")
	assert.Equal(t, model.SubscriptionStatusInventory, sub.Status,
		"订阅初始状态应该为inventory")

	t.Log("PP-03: Test completed - Subscription after joining group verified")
}

// TestPP04_SubscriptionInvalidAfterLeavingGroup 测试PP-04: 退出分组后订阅失效
//
// Test ID: PP-04
// Priority: P0 (关键安全测试)
// Test Scenario: 退出分组后订阅失效
//  1. 用户A订阅G1套餐并启用
//  2. 用户A退出G1分组
//  3. 再次发起请求，查询用户可用套餐时不应包含G1套餐
//
// Expected Result:
//
//	退出分组后，用户无法使用该分组的P2P套餐
func (s *P2PPermissionTestSuite) TestPP04_SubscriptionInvalidAfterLeavingGroup() {
	t := s.T()
	t.Log("PP-04: Testing subscription invalidation after leaving group (CRITICAL SECURITY TEST)")

	// Arrange: 创建P2P分组G1
	groupID, _ := s.p2pHelper.CreateP2PGroupViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("test-group-%d", time.Now().UnixNano()),
		"Test Group G1",
		2, 1, "",
	)

	// Arrange: 创建P2P套餐（绑定到G1）
	packageID, _ := s.p2pHelper.CreateP2PPackageViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("p2p-package-%d", time.Now().UnixNano()),
		groupID, 200000000, 20000000,
	)

	// Arrange: 用户A加入分组G1
	t.Log("步骤1: 用户A加入分组G1")
	s.p2pHelper.AddUserToGroupViaAPI(t, s.ownerToken.Key, groupID, s.memberUserA.Id, 0)

	// Arrange: 用户A订阅P2P套餐
	t.Log("步骤2: 用户A订阅P2P套餐")
	subscriptionID, _ := s.p2pHelper.SubscribePackageViaAPI(t, s.memberAToken.Key, packageID)

	// Arrange: 启用订阅
	t.Log("步骤3: 启用订阅")
	subscription := testutil.CreateAndActivateSubscription(t, s.memberUserA.Id, packageID)
	assert.NotNil(t, subscription, "订阅启用应该成功")
	assert.Equal(t, model.SubscriptionStatusActive, subscription.Status,
		"订阅状态应该为Active")

	// Arrange: 验证用户在退出分组前有P2P分组权限
	t.Log("步骤4: 验证用户退出前的P2P分组权限")
	p2pGroupIDsBefore := testutil.GetUserP2PGroupIDs(t, s.memberUserA.Id)
	assert.Contains(t, p2pGroupIDsBefore, groupID, "用户应该拥有G1分组权限")

	// Arrange: 验证用户在退出分组前可以使用该套餐
	availableCountBefore := testutil.GetUserAvailablePackageCount(t, s.memberUserA.Id, p2pGroupIDsBefore)
	assert.Greater(t, availableCountBefore, 0, "用户在退出前应该有可用套餐")

	// Act: 用户A退出分组G1
	t.Log("步骤5: 用户A退出分组G1 (关键操作)")
	success, statusCode := s.p2pHelper.RemoveUserFromGroupViaAPI(
		t, s.memberAToken.Key, groupID, s.memberUserA.Id,
	)
	assert.Equal(t, http.StatusOK, statusCode, "退出分组应该成功")
	assert.True(t, success, "退出分组应该返回true")

	// Assert 1: 验证用户已从数据库中移除分组关系
	t.Log("验证1: 检查数据库中的分组关系")
	var userGroupCount int64
	model.DB.Model(&model.UserGroup{}).
		Where("user_id = ? AND group_id = ? AND status = ?",
			s.memberUserA.Id, groupID, 1).
		Count(&userGroupCount)
	assert.Equal(t, int64(0), userGroupCount,
		"用户应该已从分组中移除（数据库中不应有Active状态的记录）")

	// Assert 2: 验证用户退出后的P2P分组列表不包含G1
	t.Log("验证2: 检查用户的P2P分组列表")
	p2pGroupIDsAfter := testutil.GetUserP2PGroupIDs(t, s.memberUserA.Id)
	assert.NotContains(t, p2pGroupIDsAfter, groupID,
		"用户退出后不应该再拥有G1分组权限")

	// Assert 3: 验证用户退出后无法使用该P2P套餐（关键断言）
	t.Log("验证3: 检查用户的可用套餐数量（关键安全验证）")
	availableCountAfter := testutil.GetUserAvailablePackageCount(t, s.memberUserA.Id, p2pGroupIDsAfter)
	assert.Less(t, availableCountAfter, availableCountBefore,
		"用户退出分组后，可用套餐数量应该减少")
	assert.Equal(t, 0, availableCountAfter,
		"用户退出分组后，不应该有任何可用的P2P套餐（关键安全验证）")

	// Assert 4: 验证订阅记录仍然存在，但因为用户失去分组权限而无法使用
	t.Log("验证4: 检查订阅记录状态")
	sub, err := model.GetSubscriptionById(subscriptionID)
	assert.NoError(t, err, "订阅记录应该仍然存在")
	assert.Equal(t, model.SubscriptionStatusActive, sub.Status,
		"订阅状态仍为Active，但因用户失去分组权限而无法使用")

	t.Log("PP-04: Test completed - CRITICAL SECURITY VERIFIED: Subscription invalid after leaving group")
}

// TestPP05_OwnerCanSubscribeOwnPackage 测试PP-05: P2P Owner自己订阅
//
// Test ID: PP-05
// Priority: P1
// Test Scenario: P2P Owner自己订阅
//
//	分组G1的Owner创建套餐并订阅自己创建的套餐，应该成功。
//
// Expected Result:
//
//	分组Owner可以订阅自己创建的P2P套餐
func (s *P2PPermissionTestSuite) TestPP05_OwnerCanSubscribeOwnPackage() {
	t := s.T()
	t.Log("PP-05: Testing P2P owner can subscribe to own package")

	// Arrange: 创建P2P分组G1
	groupID, _ := s.p2pHelper.CreateP2PGroupViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("test-group-%d", time.Now().UnixNano()),
		"Test Group G1",
		2, 1, "",
	)

	// Arrange: Owner创建P2P套餐（绑定到G1）
	t.Log("步骤1: Owner创建P2P套餐")
	packageID, statusCode := s.p2pHelper.CreateP2PPackageViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("p2p-package-%d", time.Now().UnixNano()),
		groupID, 200000000, 20000000,
	)
	assert.Equal(t, http.StatusOK, statusCode, "Owner创建P2P套餐应该成功")

	// 验证套餐的创建者是Owner
	pkg := testutil.AssertPackageExists(t, packageID)
	assert.Equal(t, s.ownerUser.Id, pkg.CreatorId, "套餐创建者应该是Owner")

	// Act: Owner订阅自己创建的P2P套餐
	t.Log("步骤2: Owner订阅自己创建的套餐")
	subscriptionID, statusCode := s.p2pHelper.SubscribePackageViaAPI(
		t, s.ownerToken.Key, packageID,
	)

	// Assert: 订阅应该成功
	assert.Equal(t, http.StatusOK, statusCode, "Owner订阅自己的P2P套餐应该成功")
	assert.Greater(t, subscriptionID, 0, "订阅ID应该大于0")

	// 验证订阅记录
	sub, err := model.GetSubscriptionById(subscriptionID)
	assert.NoError(t, err, "应该能获取订阅记录")
	assert.Equal(t, s.ownerUser.Id, sub.UserId, "订阅用户应该是Owner")
	assert.Equal(t, packageID, sub.PackageId, "订阅套餐ID应该匹配")

	t.Log("PP-05: Test completed - P2P owner can subscribe to own package verified")
}

// TestPP06_MultipleP2PPackagePriority 测试PP-06: 多P2P分组套餐优先级
//
// Test ID: PP-06
// Priority: P1
// Test Scenario: 多P2P分组套餐优先级
//
//	用户加入G1和G2两个分组，两个分组都有套餐（优先级都是11），
//	套餐ID分别为1和2，应该优先使用ID=1的G1套餐（同优先级按ID排序）。
//
// Expected Result:
//
//	同优先级的P2P套餐按ID升序排序，ID小的优先使用
func (s *P2PPermissionTestSuite) TestPP06_MultipleP2PPackagePriority() {
	t := s.T()
	t.Log("PP-06: Testing multiple P2P package priority (same priority, order by ID)")

	// Arrange: 创建两个P2P分组G1和G2
	t.Log("步骤1: 创建两个P2P分组")
	groupID1, _ := s.p2pHelper.CreateP2PGroupViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("test-group-1-%d", time.Now().UnixNano()),
		"Test Group G1",
		2, 1, "",
	)

	groupID2, _ := s.p2pHelper.CreateP2PGroupViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("test-group-2-%d", time.Now().UnixNano()),
		"Test Group G2",
		2, 1, "",
	)

	// Arrange: 为两个分组分别创建套餐
	t.Log("步骤2: 为两个分组创建套餐")
	packageID1, _ := s.p2pHelper.CreateP2PPackageViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("p2p-package-g1-%d", time.Now().UnixNano()),
		groupID1, 200000000, 20000000,
	)

	packageID2, _ := s.p2pHelper.CreateP2PPackageViaAPI(
		t, s.ownerToken.Key,
		fmt.Sprintf("p2p-package-g2-%d", time.Now().UnixNano()),
		groupID2, 150000000, 15000000,
	)

	// 验证两个套餐的优先级都是11
	pkg1 := testutil.AssertPackageExists(t, packageID1)
	pkg2 := testutil.AssertPackageExists(t, packageID2)
	assert.Equal(t, 11, pkg1.Priority, "G1套餐优先级应该是11")
	assert.Equal(t, 11, pkg2.Priority, "G2套餐优先级应该是11")

	// Arrange: 用户A加入两个分组
	t.Log("步骤3: 用户A加入两个分组")
	s.p2pHelper.AddUserToGroupViaAPI(t, s.ownerToken.Key, groupID1, s.memberUserA.Id, 0)
	s.p2pHelper.AddUserToGroupViaAPI(t, s.ownerToken.Key, groupID2, s.memberUserA.Id, 0)

	// 验证用户同时拥有两个分组的权限
	p2pGroupIDs := testutil.GetUserP2PGroupIDs(t, s.memberUserA.Id)
	assert.Contains(t, p2pGroupIDs, groupID1, "用户应该拥有G1分组权限")
	assert.Contains(t, p2pGroupIDs, groupID2, "用户应该拥有G2分组权限")
	assert.Len(t, p2pGroupIDs, 2, "用户应该拥有2个P2P分组权限")

	// Arrange: 用户A订阅两个套餐并启用
	t.Log("步骤4: 用户A订阅并启用两个套餐")
	sub1 := testutil.CreateAndActivateSubscription(t, s.memberUserA.Id, packageID1)
	sub2 := testutil.CreateAndActivateSubscription(t, s.memberUserA.Id, packageID2)

	assert.NotNil(t, sub1, "订阅1应该成功")
	assert.NotNil(t, sub2, "订阅2应该成功")

	// Act: 查询用户可用套餐（从数据库验证优先级排序）
	t.Log("步骤5: 查询用户可用套餐，验证优先级排序")
	currentTime := common.GetTimestamp()

	var subscriptions []*model.Subscription
	query := model.DB.Table("subscriptions").
		Select("subscriptions.*").
		Joins("JOIN packages ON subscriptions.package_id = packages.id").
		Where("subscriptions.user_id = ?", s.memberUserA.Id).
		Where("subscriptions.status = ?", "active").
		Where("subscriptions.start_time <= ?", currentTime).
		Where("subscriptions.end_time > ?", currentTime).
		Where("packages.status = ?", 1).
		Where("packages.p2p_group_id IN (?)", p2pGroupIDs).
		Order("packages.priority DESC, subscriptions.id ASC"). // 关键排序逻辑
		Find(&subscriptions)

	// Assert: 验证优先级排序
	assert.Len(t, subscriptions, 2, "应该查询到2个可用套餐")

	// 关键断言：同优先级按ID升序，ID小的在前
	firstSubscription := subscriptions[0]
	secondSubscription := subscriptions[1]

	t.Logf("第一个套餐ID: %d, 第二个套餐ID: %d", firstSubscription.Id, secondSubscription.Id)

	// 验证第一个订阅的ID应该小于第二个（同优先级按ID排序）
	assert.Less(t, firstSubscription.Id, secondSubscription.Id,
		"同优先级的P2P套餐应该按订阅ID升序排序，ID小的优先")

	// 验证第一个订阅对应的是ID较小的套餐
	if packageID1 < packageID2 {
		assert.Equal(t, packageID1, firstSubscription.PackageId,
			"第一个应该是packageID较小的G1套餐")
		assert.Equal(t, packageID2, secondSubscription.PackageId,
			"第二个应该是packageID较大的G2套餐")
	} else {
		assert.Equal(t, packageID2, firstSubscription.PackageId,
			"第一个应该是packageID较小的G2套餐")
		assert.Equal(t, packageID1, secondSubscription.PackageId,
			"第二个应该是packageID较大的G1套餐")
	}

	t.Log("PP-06: Test completed - Multiple P2P package priority verified (order by ID)")
}

// TestInP2PPermissionTestSuite 运行P2P权限测试套件
func TestInP2PPermissionTestSuite(t *testing.T) {
	suite.Run(t, new(P2PPermissionTestSuite))
}
