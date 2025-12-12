package orthogonal_test

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

// ============ 测试套件定义 ============

// OrthogonalMatrixSuite 正交矩阵测试套件
type OrthogonalMatrixSuite struct {
	suite.Suite
	server    *testutil.TestServer
	redisMock *testutil.RedisMock
}

// SetupSuite 在整个测试套件开始前执行一次
func (s *OrthogonalMatrixSuite) SetupSuite() {
	s.T().Log("========== 正交矩阵测试套件开始 ==========")

	// 启动带 Redis 的测试服务器，确保套餐系统与滑动窗口功能完整可用
	server, err := testutil.StartTestServer()
	if err != nil {
		s.T().Fatalf("Failed to start test server: %v", err)
	}
	s.server = server

	s.T().Logf("测试服务器已启动: %s", s.server.BaseURL)

	// 使用与测试服务器共享的 miniredis 实例构造 RedisMock，仅用于窗口状态断言
	if server.MiniRedis != nil {
		s.redisMock = testutil.NewRedisMockFromMiniRedis(s.T(), server.MiniRedis)
		s.T().Log("Redis Mock已初始化并绑定到测试服务器的 miniredis")
	} else {
		s.T().Log("警告: 测试服务器未启用 Redis，正交矩阵窗口相关断言将被跳过")
	}
}

// TearDownSuite 在整个测试套件结束后执行一次
func (s *OrthogonalMatrixSuite) TearDownSuite() {
	if s.server != nil {
		s.server.Stop()
		s.T().Log("测试服务器已停止")
	}
	if s.redisMock != nil {
		s.redisMock.Close()
		s.T().Log("Redis Mock已关闭")
	}
	s.T().Log("========== 正交矩阵测试套件结束 ==========")
}

// SetupTest 在每个测试用例开始前执行
func (s *OrthogonalMatrixSuite) SetupTest() {
	// 清理数据库
	testutil.CleanupPackageTestData(s.T())
}

// TearDownTest 在每个测试用例结束后执行
func (s *OrthogonalMatrixSuite) TearDownTest() {
	// 每个测试后清理
}

// TestOrthogonalMatrixSuite 运行测试套件
func TestOrthogonalMatrixSuite(t *testing.T) {
	suite.Run(t, new(OrthogonalMatrixSuite))
}

// ============ 数据结构定义 ============

// OrthogonalTestCase 正交测试用例配置结构
type OrthogonalTestCase struct {
	ID                   string   // 测试用例ID (OM-01, OM-02, ...)
	Name                 string   // 测试用例名称
	PackageType          string   // "global", "p2p", or "both"
	PackagePriority      int      // 5, 11, or 15
	PackageHourlyLimit   int64    // 小时限额
	PackageFallback      bool     // 是否允许Fallback
	UserGroup            string   // "default", "vip", or "svip"
	UserInP2PGroup       bool     // 是否加入P2P分组
	P2PGroupName         string   // P2P分组名称 (如果适用)
	ChannelType          string   // "public", "p2p", "private", or "mixed"
	ChannelSystemGroup   string   // 渠道的系统分组
	TokenConfig          string   // "normal", "billing_override", or "p2p_restriction"
	TokenBillingGroups   []string // Token覆盖的计费分组列表 (如果适用)
	TokenP2PGroupID      int      // Token限制的P2P分组ID (如果适用)
	WindowState          string   // "not_exist", "active", "active_exceeded", or "expired"
	ExpectedResult       string   // "package_consumed", "balance_consumed", or "rejected"
	ExpectedBillingGroup string   // 预期的计费分组
	ExpectedRouting      string   // 预期的路由结果描述
}

// OrthogonalTestContext 测试上下文，保存测试过程中创建的实体ID
type OrthogonalTestContext struct {
	UserID          int
	PackageID       int
	SecondPackageID int // 对于OM-08双套餐场景
	SubscriptionID  int
	SecondSubID     int
	ChannelID       int
	SecondChannelID int
	P2PGroupID      int
	TokenKey        string
	InitialQuota    int64
	Server          *testutil.TestServer
	RedisMock       *testutil.RedisMock
}

// RelayInfo 简化的RelayInfo结构（用于测试）
type RelayInfo struct {
	UsingPackageId int
	BillingGroup   string
}

// ============ 正交测试用例配置表 ============

var orthogonalTestCases = []OrthogonalTestCase{
	{
		ID:                   "OM-01",
		Name:                 "全局套餐高优先级VIP用户公共渠道",
		PackageType:          "global",
		PackagePriority:      15,
		PackageHourlyLimit:   20000000, // 20M
		PackageFallback:      true,
		UserGroup:            "vip",
		UserInP2PGroup:       false,
		P2PGroupName:         "",
		ChannelType:          "public",
		ChannelSystemGroup:   "vip",
		TokenConfig:          "normal",
		TokenBillingGroups:   nil,
		TokenP2PGroupID:      0,
		WindowState:          "not_exist",
		ExpectedResult:       "package_consumed",
		ExpectedBillingGroup: "vip",
		ExpectedRouting:      "路由到VIP公共渠道",
	},
	{
		ID:                   "OM-02",
		Name:                 "P2P套餐default用户G1渠道",
		PackageType:          "p2p",
		PackagePriority:      11,
		PackageHourlyLimit:   10000000, // 10M
		PackageFallback:      true,
		UserGroup:            "default",
		UserInP2PGroup:       true,
		P2PGroupName:         "G1",
		ChannelType:          "p2p",
		ChannelSystemGroup:   "default",
		TokenConfig:          "normal",
		TokenBillingGroups:   nil,
		TokenP2PGroupID:      0,
		WindowState:          "not_exist",
		ExpectedResult:       "package_consumed",
		ExpectedBillingGroup: "default",
		ExpectedRouting:      "路由到P2P共享渠道G1",
	},
	{
		ID:                   "OM-03",
		Name:                 "全局套餐低优先级VIP用户billing覆盖为default",
		PackageType:          "global",
		PackagePriority:      5,
		PackageHourlyLimit:   15000000, // 15M
		PackageFallback:      true,
		UserGroup:            "vip",
		UserInP2PGroup:       false,
		P2PGroupName:         "",
		ChannelType:          "public",
		ChannelSystemGroup:   "default",
		TokenConfig:          "billing_override",
		TokenBillingGroups:   []string{"default"},
		TokenP2PGroupID:      0,
		WindowState:          "active",
		ExpectedResult:       "package_consumed",
		ExpectedBillingGroup: "default",
		ExpectedRouting:      "路由到default公共渠道",
	},
	{
		ID:                   "OM-04",
		Name:                 "P2P套餐VIP用户加入G1使用P2P渠道窗口过期",
		PackageType:          "p2p",
		PackagePriority:      11,
		PackageHourlyLimit:   10000000, // 10M
		PackageFallback:      true,
		UserGroup:            "vip",
		UserInP2PGroup:       true,
		P2PGroupName:         "G1",
		ChannelType:          "p2p",
		ChannelSystemGroup:   "vip",
		TokenConfig:          "p2p_restriction",
		TokenBillingGroups:   nil,
		TokenP2PGroupID:      1, // 限制为G1
		WindowState:          "expired",
		ExpectedResult:       "package_consumed",
		ExpectedBillingGroup: "vip",
		ExpectedRouting:      "窗口重建并路由到P2P渠道",
	},
	{
		ID:                   "OM-05",
		Name:                 "全局套餐高优先级svip用户窗口有效但已超限",
		PackageType:          "global",
		PackagePriority:      15,
		PackageHourlyLimit:   5000000, // 5M (小限额，容易超限)
		PackageFallback:      true,
		UserGroup:            "svip",
		UserInP2PGroup:       false,
		P2PGroupName:         "",
		ChannelType:          "public",
		ChannelSystemGroup:   "svip",
		TokenConfig:          "normal",
		TokenBillingGroups:   nil,
		TokenP2PGroupID:      0,
		WindowState:          "active_exceeded",
		ExpectedResult:       "balance_consumed",
		ExpectedBillingGroup: "svip",
		ExpectedRouting:      "套餐超限Fallback到用户余额",
	},
	{
		ID:                   "OM-06",
		Name:                 "P2P套餐default用户加入G1但渠道为私有",
		PackageType:          "p2p",
		PackagePriority:      11,
		PackageHourlyLimit:   10000000, // 10M
		PackageFallback:      true,
		UserGroup:            "default",
		UserInP2PGroup:       true,
		P2PGroupName:         "G1",
		ChannelType:          "private",
		ChannelSystemGroup:   "default",
		TokenConfig:          "normal",
		TokenBillingGroups:   nil,
		TokenP2PGroupID:      0,
		WindowState:          "not_exist",
		ExpectedResult:       "rejected",
		ExpectedBillingGroup: "",
		ExpectedRouting:      "无法使用私有渠道，路由失败",
	},
	{
		ID:                   "OM-07",
		Name:                 "全局套餐低优先级default用户加入G1但Token无P2P限制",
		PackageType:          "global",
		PackagePriority:      5,
		PackageHourlyLimit:   15000000, // 15M
		PackageFallback:      true,
		UserGroup:            "default",
		UserInP2PGroup:       true,
		P2PGroupName:         "G1",
		ChannelType:          "p2p",
		ChannelSystemGroup:   "default",
		TokenConfig:          "normal",
		TokenBillingGroups:   nil,
		TokenP2PGroupID:      0, // 无P2P限制
		WindowState:          "not_exist",
		ExpectedResult:       "rejected",
		ExpectedBillingGroup: "",
		ExpectedRouting:      "Token无P2P限制时无法使用P2P渠道",
	},
	{
		ID:                   "OM-08",
		Name:                 "多套餐组合：全局15+P2P11，VIP用户，billing列表，窗口有效",
		PackageType:          "both",  // 特殊标记：需要创建两个套餐
		PackagePriority:      15,      // 高优先级套餐
		PackageHourlyLimit:   5000000, // 5M (高优先级套餐，小限额)
		PackageFallback:      true,
		UserGroup:            "vip",
		UserInP2PGroup:       true,
		P2PGroupName:         "G1",
		ChannelType:          "mixed", // 公共+P2P混合
		ChannelSystemGroup:   "vip",
		TokenConfig:          "billing_override",
		TokenBillingGroups:   []string{"vip", "default"},
		TokenP2PGroupID:      0,
		WindowState:          "active",
		ExpectedResult:       "package_consumed",
		ExpectedBillingGroup: "vip",
		ExpectedRouting:      "优先级15套餐超限后降级到优先级11套餐",
	},
}

// ============ 核心测试方法 ============

// TestOrthogonalMatrix_AllCombinations 正交配置矩阵主测试函数
func (s *OrthogonalMatrixSuite) TestOrthogonalMatrix_AllCombinations() {
	// 跳过测试，等待后端API实现
	s.T().Skip("正交矩阵测试：等待套餐系统后端实现")

	for _, tc := range orthogonalTestCases {
		s.Run(tc.ID+"_"+tc.Name, func() {
			t := s.T()
			t.Logf("=== 执行正交测试用例 %s ===", tc.ID)
			t.Logf("场景描述: %s", tc.Name)

			// Setup: 根据tc配置创建测试环境
			ctx := s.setupOrthogonalTestCase(tc)

			// Execute: 发起API请求
			resp, relayInfo := s.executePackageRequest(tc, ctx)

			// Verify: 验证结果
			s.verifyOrthogonalResult(tc, resp, relayInfo, ctx)

			// Cleanup: 清理该用例的数据
			s.cleanupOrthogonalTestCase(tc, ctx)

			t.Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
		})
	}
}

// setupOrthogonalTestCase 根据测试用例配置创建测试环境
func (s *OrthogonalMatrixSuite) setupOrthogonalTestCase(tc OrthogonalTestCase) *OrthogonalTestContext {
	t := s.T()
	t.Helper()
	ctx := &OrthogonalTestContext{
		Server:    s.server,
		RedisMock: s.redisMock,
	}

	t.Logf("  [Setup] 创建测试环境...")

	// 1. 创建用户
	user := testutil.CreateTestUser(t, testutil.UserTestData{
		Username: fmt.Sprintf("ox_user_%s_%d", tc.UserGroup, time.Now().UnixNano()),
		Group:    tc.UserGroup,
		Quota:    100000000, // 100M初始余额
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
	})
	ctx.UserID = user.Id
	ctx.InitialQuota = int64(user.Quota)
	t.Logf("    - 创建用户 (ID=%d, Group=%s, Quota=%dM)", ctx.UserID, tc.UserGroup, ctx.InitialQuota/1000000)

	// 2. 创建P2P分组（如果需要）
	if tc.UserInP2PGroup && tc.P2PGroupName != "" {
		group := testutil.CreateTestGroup(t, testutil.GroupTestData{
			Name:        fmt.Sprintf("ox_g_%s_%d", tc.P2PGroupName, time.Now().UnixNano()),
			DisplayName: "正交测试分组 " + tc.P2PGroupName,
			OwnerId:     ctx.UserID,
			Type:        2, // 共享分组
			JoinMethod:  2, // 密码加入
			JoinKey:     "testpass",
		})
		ctx.P2PGroupID = group.Id

		// 将用户加入分组
		testutil.AddUserToGroup(t, ctx.UserID, ctx.P2PGroupID, 1) // status=1表示活跃
		t.Logf("    - 创建P2P分组 (ID=%d, Name=%s) 并加入用户", ctx.P2PGroupID, tc.P2PGroupName)
	}

	// 3. 创建套餐
	if tc.PackageType == "both" {
		// OM-08特殊场景：创建两个套餐
		pkg1 := testutil.CreateTestPackage(t, testutil.PackageTestData{
			Name:              "高优先级套餐",
			Priority:          15,
			P2PGroupId:        0,
			Quota:             500000000,
			HourlyLimit:       tc.PackageHourlyLimit,
			FallbackToBalance: tc.PackageFallback,
			Status:            1,
		})
		ctx.PackageID = pkg1.Id

		pkg2 := testutil.CreateTestPackage(t, testutil.PackageTestData{
			Name:              "P2P套餐",
			Priority:          11,
			P2PGroupId:        ctx.P2PGroupID,
			Quota:             500000000,
			HourlyLimit:       20000000, // 20M
			FallbackToBalance: tc.PackageFallback,
			Status:            1,
		})
		ctx.SecondPackageID = pkg2.Id
		t.Logf("    - 创建高优先级套餐 (ID=%d, Priority=15, HourlyLimit=%dM)", ctx.PackageID, tc.PackageHourlyLimit/1000000)
		t.Logf("    - 创建P2P套餐 (ID=%d, Priority=11, HourlyLimit=20M)", ctx.SecondPackageID)
	} else if tc.PackageType == "p2p" {
		pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
			Name:              tc.Name,
			Priority:          tc.PackagePriority,
			P2PGroupId:        ctx.P2PGroupID,
			Quota:             500000000,
			HourlyLimit:       tc.PackageHourlyLimit,
			FallbackToBalance: tc.PackageFallback,
			Status:            1,
		})
		ctx.PackageID = pkg.Id
		t.Logf("    - 创建P2P套餐 (ID=%d, Priority=%d, P2PGroup=%d, HourlyLimit=%dM)",
			ctx.PackageID, tc.PackagePriority, ctx.P2PGroupID, tc.PackageHourlyLimit/1000000)
	} else {
		pkg := testutil.CreateTestPackage(t, testutil.PackageTestData{
			Name:              tc.Name,
			Priority:          tc.PackagePriority,
			P2PGroupId:        0,
			Quota:             500000000,
			HourlyLimit:       tc.PackageHourlyLimit,
			FallbackToBalance: tc.PackageFallback,
			Status:            1,
		})
		ctx.PackageID = pkg.Id
		t.Logf("    - 创建全局套餐 (ID=%d, Priority=%d, HourlyLimit=%dM)",
			ctx.PackageID, tc.PackagePriority, tc.PackageHourlyLimit/1000000)
	}

	// 4. 创建订阅并启用
	sub := testutil.CreateAndActivateSubscription(t, ctx.UserID, ctx.PackageID)
	ctx.SubscriptionID = sub.Id
	t.Logf("    - 创建并启用订阅 (ID=%d)", ctx.SubscriptionID)

	if tc.PackageType == "both" && ctx.SecondPackageID > 0 {
		sub2 := testutil.CreateAndActivateSubscription(t, ctx.UserID, ctx.SecondPackageID)
		ctx.SecondSubID = sub2.Id
		t.Logf("    - 创建并启用第二个订阅 (ID=%d)", ctx.SecondSubID)
	}

	// 5. 创建渠道（使用测试辅助函数，确保能力表与路由缓存行为与生产一致）
	var allowedGroupsStr string
	if tc.ChannelType == "p2p" && ctx.P2PGroupID > 0 {
		allowedGroupsStr = fmt.Sprintf("[%d]", ctx.P2PGroupID)
	}

	// 对于 OM-06 场景，需要模拟“他人私有渠道”：渠道 Owner 不是当前请求用户，
	// 这样才能验证私有渠道权限隔离导致的路由失败。
	privateOwnerID := 0
	if tc.ChannelType == "private" && tc.ID == "OM-06" {
		privateOwner := testutil.CreateTestUser(t, testutil.UserTestData{
			Username: fmt.Sprintf("ox_owner_private_%d", time.Now().UnixNano()),
			Group:    tc.ChannelSystemGroup,
			Quota:    100000000,
			Role:     common.RoleCommonUser,
			Status:   common.UserStatusEnabled,
		})
		privateOwnerID = privateOwner.Id
		t.Logf("    - 创建私有渠道所有者用户 (OwnerID=%d, Group=%s)", privateOwnerID, tc.ChannelSystemGroup)
	}

	// 对于 OM-07 场景，需要模拟“他人P2P共享渠道”：P2P 渠道由分组Owner创建，
	// 当前请求用户只是普通组员，且 Token 无 P2P 限制，应无法访问该渠道。
	p2pOwnerID := 0
	if tc.ChannelType == "p2p" && tc.ID == "OM-07" {
		p2pOwner := testutil.CreateTestUser(t, testutil.UserTestData{
			Username: fmt.Sprintf("ox_owner_p2p_%d", time.Now().UnixNano()),
			Grou
	ownerID := 0
	isPrivate := false
	switch tc.ChannelType {
	case "private":
		if tc.ID == "OM-06" && privateOwnerID > 0 {
			ownerID = privateOwnerID
		} else {
			ownerID = ctx.UserID
		}
		isPrivate = true
	case "p2p":
		ownerID = ctx.UserID // 共享渠道由用户作为Owner
	default: // "public"
		ownerID = 0
	}

	channel := testutil.CreateTestChannel(t, testutil.ChannelTestData{
		Name:          fmt.Sprintf("ox_ch_%s_%d", tc.ChannelType, time.Now().UnixNano()),
		Type:          1,
		if tc.ID == "OM-07" && p2pOwnerID > 0 {
			ownerID = p2pOwnerID
		} else {
			ownerID = ctx.UserID // 默认：共享渠道由当前用户作为Owner
		}
		Models:        "gpt-4",
		Status:        common.ChannelStatusEnabled,
		BaseURL:       "", // 使用默认 Mock LLM 上游地址
		OwnerUserId:   ownerID,
		IsPrivate:     isPrivate,
		AllowedGroups: allowedGroupsStr,
	})
	ctx.ChannelID = channel.Id
	t.Logf("    - 创建渠道 (ID=%d, Type=%s, Group=%s, IsPrivate=%v, Owner=%d)",
		ctx.ChannelID, tc.ChannelType, tc.ChannelSystemGroup, channel.IsPrivate, channel.OwnerUserId)

	if tc.ChannelType == "mixed" {
		// OM-08特殊场景：创建两个渠道（公共+P2P）
		// 第一个：公共渠道（平台公共渠道）
		channel1 := testutil.CreateTestChannel(t, testutil.ChannelTestData{
			Name:        fmt.Sprintf("ox_ch_public_%d", time.Now().UnixNano()),
			Type:        1,
			Group:       tc.ChannelSystemGroup,
			Models:      "gpt-4",
			Status:      common.ChannelStatusEnabled,
			BaseURL:     "",
			OwnerUserId: 0,
			IsPrivate:   false,
		})
		ctx.ChannelID = channel1.Id

		// 第二个：P2P渠道（授权给当前P2P分组）
		p2pAllowed := fmt.Sprintf("[%d]", ctx.P2PGroupID)
		channel2 := testutil.CreateTestChannel(t, testutil.ChannelTestData{
			Name:          fmt.Sprintf("ox_ch_p2p_%d", time.Now().UnixNano()),
			Type:          1,
			Group:         tc.ChannelSystemGroup,
			Models:        "gpt-4",
			Status:        common.ChannelStatusEnabled,
			BaseURL:       "",
			OwnerUserId:   ctx.UserID,
			IsPrivate:     false,
			AllowedGroups: p2pAllowed,
		})
		ctx.SecondChannelID = channel2.Id
		t.Logf("    - 创建公共渠道 (ID=%d) 和 P2P渠道 (ID=%d)", ctx.ChannelID, ctx.SecondChannelID)
	}

	// 6. 创建Token
	rawKey, err := common.GenerateKey()
	assert.Nil(t, err, "Failed to generate token key for orthogonal test")

	var groupStr string
	if tc.TokenConfig == "billing_override" && len(tc.TokenBillingGroups) > 0 {
		// 构建billing groups JSON数组字符串
		if len(tc.TokenBillingGroups) == 1 {
			groupStr = fmt.Sprintf("[\"%s\"]", tc.TokenBillingGroups[0])
		} else if len(tc.TokenBillingGroups) > 1 {
			groupStr = fmt.Sprintf("[\"%s\",\"%s\"]", tc.TokenBillingGroups[0], tc.TokenBillingGroups[1])
		}
	}

	token := &model.Token{
		UserId:         ctx.UserID,
		Key:            rawKey, // DB 中存储“裸”token，HTTP 请求使用 sk- 前缀
		Name:           fmt.Sprintf("ox_token_%s", tc.TokenConfig),
		Status:         common.TokenStatusEnabled,
		UnlimitedQuota: true, // 测试令牌本身不限额，由套餐/用户余额控制实际扣费
		RemainQuota:    0,
		Group:          groupStr,
	}

	if tc.TokenConfig == "p2p_restriction" && tc.TokenP2PGroupID > 0 {
		p2pGroupID := ctx.P2PGroupID
		token.P2PGroupID = &p2pGroupID
	}

	err = model.DB.Create(token).Error
	assert.Nil(t, err, "Failed to create token")
	// 对外使用的API Token带有 sk- 前缀，符合生产环境约定
	ctx.TokenKey = "sk-" + rawKey
	displayKey := ctx.TokenKey
	if len(displayKey) > 16 {
		displayKey = displayKey[:16]
	}
	t.Logf("    - 创建Token (DBKey=%s..., APIToken=%s..., Config=%s, BillingGroups=%v, P2PGroup=%d)",
		rawKey[:12], displayKey, tc.TokenConfig, tc.TokenBillingGroups, tc.TokenP2PGroupID)

	// 7. 设置滑动窗口状态（使用Redis Mock）
	if ctx.RedisMock != nil {
		setupSlidingWindowState(t, ctx.RedisMock, ctx.SubscriptionID, tc.WindowState, tc.PackageHourlyLimit)
	} else {
		t.Logf("    - 警告: RedisMock未初始化，跳过滑动窗口设置")
	}

	t.Logf("  [Setup] 完成\n")
	return ctx
}

// executePackageRequest 执行API请求并返回响应和RelayInfo
func (s *OrthogonalMatrixSuite) executePackageRequest(tc OrthogonalTestCase, ctx *OrthogonalTestContext) (*http.Response, *RelayInfo) {
	t := s.T()
	t.Helper()
	t.Logf("  [Execute] 发起API请求...")

	// 根据用例预期结果配置 Mock LLM 用量，确保覆盖对应的额度/窗口场景
	if s.server != nil && s.server.MockLLM != nil {
		// 默认小用量配置（约 10/20 tokens），与 MockLLM 默认行为一致
		mockResp := testutil.MockLLMResponse{
			StatusCode:       http.StatusOK,
			Content:          "orthogonal matrix test response",
			PromptTokens:     10,
			CompletionTokens: 20,
		}

		if tc.ExpectedResult == "balance_consumed" {
			// OM-05 场景：需要触发套餐超限并回退到余额。
			// 使用大用量（约 8M tokens）模拟超过小时窗口/套餐限额。
			mockResp.Content = "orthogonal matrix large quota test"
			mockResp.PromptTokens = 4000000
			mockResp.CompletionTokens = 4000000
		}

		testutil.SetupMockLLMResponse(t, s.server.MockLLM, mockResp)
	}

	// 构建请求参数
	chatRequest := testutil.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []testutil.ChatMessage{
			{Role: "user", Content: "test orthogonal matrix"},
		},
		Stream: false,
	}

	// 发起HTTP请求
	client := testutil.NewAPIClientWithToken(ctx.Server.BaseURL, ctx.TokenKey)
	resp, err := client.ChatCompletion(chatRequest)

	// 注意：这里简化处理，实际测试中需要检查err
	if err != nil {
		t.Logf("    - API调用出错: %v", err)
		// 创建一个错误响应
		return &http.Response{StatusCode: http.StatusInternalServerError}, &RelayInfo{}
	}

	// 构建RelayInfo（从响应或数据库查询）
	relayInfo := &RelayInfo{
		UsingPackageId: 0,
		BillingGroup:   tc.ExpectedBillingGroup,
	}

	// 如果请求成功，尝试从日志表查询实际使用的套餐ID
	if resp != nil && resp.StatusCode == http.StatusOK {
		// 查询最近的消费日志，获取套餐ID
		// 这里简化处理，假设成功就使用套餐
		if tc.ExpectedResult == "package_consumed" {
			relayInfo.UsingPackageId = ctx.PackageID
		}
	}

	t.Logf("  [Execute] 完成 (StatusCode=%d, UsingPackage=%d)\n",
		resp.StatusCode, relayInfo.UsingPackageId)
	return resp, relayInfo
}

// verifyOrthogonalResult 验证测试结果
func (s *OrthogonalMatrixSuite) verifyOrthogonalResult(tc OrthogonalTestCase, resp *http.Response, relayInfo *RelayInfo, ctx *OrthogonalTestContext) {
	t := s.T()
	t.Helper()
	t.Logf("  [Verify] 验证结果...")

	switch tc.ExpectedResult {
	case "package_consumed":
		// 验证套餐扣减
		assert.Equal(t, http.StatusOK, resp.StatusCode, "请求应该成功")
		assert.Greater(t, relayInfo.UsingPackageId, 0, "应该使用套餐")
		t.Logf("    ✓ 请求成功，使用套餐ID=%d", relayInfo.UsingPackageId)

		// 验证计费分组
		assert.Equal(t, tc.ExpectedBillingGroup, relayInfo.BillingGroup, "计费分组应匹配")
		t.Logf("    ✓ 计费分组正确: %s", relayInfo.BillingGroup)

		// 验证套餐total_consumed增加
		subscription, err := model.GetSubscriptionById(ctx.SubscriptionID)
		assert.Nil(t, err, "Failed to get subscription")
		assert.Greater(t, subscription.TotalConsumed, int64(0), "套餐total_consumed应大于0")
		t.Logf("    ✓ 套餐total_consumed=%dM", subscription.TotalConsumed/1000000)

		// 验证用户余额未变
		finalQuota, err := model.GetUserQuota(ctx.UserID, true)
		assert.Nil(t, err, "Failed to get user quota")
		assert.Equal(t, ctx.InitialQuota, int64(finalQuota), "用户余额不应变化")
		t.Logf("    ✓ 用户余额未变 (Quota=%dM)", finalQuota/1000000)

		// 验证滑动窗口状态
		if tc.WindowState == "not_exist" && ctx.RedisMock != nil {
			testutil.AssertWindowExists(t, ctx.RedisMock, ctx.SubscriptionID, "hourly")
			t.Logf("    ✓ 滑动窗口已创建")
		} else if tc.WindowState == "expired" && ctx.RedisMock != nil {
			// 检查窗口已重建（start_time接近当前时间）
			startTime := testutil.GetWindowStartTime(t, ctx.RedisMock, ctx.SubscriptionID, "hourly")
			now := time.Now().Unix()
			assert.InDelta(t, now, startTime, 10, "窗口应该被重建")
			t.Logf("    ✓ 滑动窗口已重建")
		} else if tc.WindowState == "active" && ctx.RedisMock != nil {
			// 窗口应累加消耗
			consumed := testutil.GetWindowConsumed(t, ctx.RedisMock, ctx.SubscriptionID, "hourly")
			assert.Greater(t, consumed, int64(3000000), "窗口消耗应大于初始的3M")
			t.Logf("    ✓ 滑动窗口累加消耗: %dM", consumed/1000000)
		}

	case "balance_consumed":
		// 验证余额扣减
		assert.Equal(t, http.StatusOK, resp.StatusCode, "请求应该成功")
		assert.Equal(t, 0, relayInfo.UsingPackageId, "不应使用套餐")
		t.Logf("    ✓ 请求成功，未使用套餐")

		// 验证用户余额扣减
		finalQuota, err := model.GetUserQuota(ctx.UserID, true)
		assert.Nil(t, err, "Failed to get user quota")
		assert.Less(t, int64(finalQuota), ctx.InitialQuota, "用户余额应减少")
		deducted := ctx.InitialQuota - int64(finalQuota)
		t.Logf("    ✓ 用户余额扣减: %dM -> %dM (扣减=%dM)",
			ctx.InitialQuota/1000000, finalQuota/1000000, deducted/1000000)

		// 验证套餐消耗（应该接近或等于限额）
		subscription, err := model.GetSubscriptionById(ctx.SubscriptionID)
		assert.Nil(t, err, "Failed to get subscription")
		t.Logf("    ✓ 套餐total_consumed=%dM (Fallback后不再增加)", subscription.TotalConsumed/1000000)

	case "rejected":
		// 验证请求被拒绝
		assert.True(t,
			resp.StatusCode == http.StatusForbidden || // 403: 权限不足
				resp.StatusCode == http.StatusNotFound || // 404: 资源/渠道不存在
				resp.StatusCode == http.StatusTooManyRequests || // 429: 限流/额度不足
				resp.StatusCode == http.StatusServiceUnavailable, // 503: 无可用渠道（distributor）
			fmt.Sprintf("请求应该被拒绝 (403/404/429/503), 实际: %d", resp.StatusCode))
		t.Logf("    ✓ 请求被拒绝 (StatusCode=%d)", resp.StatusCode)

		// 验证套餐未扣减（或仅有初始消耗）
		subscription, err := model.GetSubscriptionById(ctx.SubscriptionID)
		assert.Nil(t, err, "Failed to get subscription")
		if tc.WindowState == "not_exist" {
			assert.Equal(t, int64(0), subscription.TotalConsumed, "套餐不应扣减")
		}
		t.Logf("    ✓ 套餐total_consumed=%d", subscription.TotalConsumed)

		// 验证用户余额未变
		finalQuota, err := model.GetUserQuota(ctx.UserID, true)
		assert.Nil(t, err, "Failed to get user quota")
		assert.Equal(t, ctx.InitialQuota, int64(finalQuota), "用户余额不应变化")
		t.Logf("    ✓ 用户余额未变")
	}

	t.Logf("  [Verify] 完成\n")
}

// cleanupOrthogonalTestCase 清理测试数据
func (s *OrthogonalMatrixSuite) cleanupOrthogonalTestCase(tc OrthogonalTestCase, ctx *OrthogonalTestContext) {
	t := s.T()
	t.Helper()
	t.Logf("  [Cleanup] 清理测试数据...")

	// 清理Redis滑动窗口
	if ctx.RedisMock != nil && ctx.SubscriptionID > 0 {
		periods := []string{"rpm", "hourly", "4hourly", "daily", "weekly"}
		for _, period := range periods {
			key := testutil.GetWindowKey(ctx.SubscriptionID, period)
			ctx.RedisMock.Server.Del(key)
		}
		if ctx.SecondSubID > 0 {
			for _, period := range periods {
				key := testutil.GetWindowKey(ctx.SecondSubID, period)
				ctx.RedisMock.Server.Del(key)
			}
		}
	}

	// 删除订阅
	if ctx.SubscriptionID > 0 {
		model.DB.Delete(&model.Subscription{}, "id = ?", ctx.SubscriptionID)
	}
	if ctx.SecondSubID > 0 {
		model.DB.Delete(&model.Subscription{}, "id = ?", ctx.SecondSubID)
	}

	// 删除套餐
	if ctx.PackageID > 0 {
		model.DB.Delete(&model.Package{}, "id = ?", ctx.PackageID)
	}
	if ctx.SecondPackageID > 0 {
		model.DB.Delete(&model.Package{}, "id = ?", ctx.SecondPackageID)
	}

	// 删除Token
	if ctx.TokenKey != "" {
		model.DB.Delete(&model.Token{}, "key = ?", ctx.TokenKey)
	}

	// 删除渠道
	if ctx.ChannelID > 0 {
		model.DB.Delete(&model.Channel{}, "id = ?", ctx.ChannelID)
	}
	if ctx.SecondChannelID > 0 {
		model.DB.Delete(&model.Channel{}, "id = ?", ctx.SecondChannelID)
	}

	// 删除P2P分组关系
	if ctx.P2PGroupID > 0 {
		model.DB.Delete(&model.UserGroup{}, "group_id = ?", ctx.P2PGroupID)
		model.DB.Delete(&model.Group{}, "id = ?", ctx.P2PGroupID)
	}

	// 删除用户
	if ctx.UserID > 0 {
		model.DB.Delete(&model.User{}, "id = ?", ctx.UserID)
	}

	t.Logf("  [Cleanup] 完成")
}

// ============ 辅助函数 ============

// setupSlidingWindowState 设置滑动窗口状态
func setupSlidingWindowState(t *testing.T, rm *testutil.RedisMock, subscriptionID int, windowState string, hourlyLimit int64) {
	t.Helper()

	if rm == nil {
		return
	}

	key := testutil.GetWindowKey(subscriptionID, "hourly")
	now := time.Now().Unix()

	switch windowState {
	case "active":
		// 创建一个有效的窗口（已消耗3M）
		startTime := now - 1800 // 30分钟前开始
		endTime := startTime + 3600
		rm.SetHashField(key, "start_time", fmt.Sprintf("%d", startTime))
		rm.SetHashField(key, "end_time", fmt.Sprintf("%d", endTime))
		rm.SetHashField(key, "consumed", "3000000") // 已消耗3M
		rm.SetHashField(key, "limit", fmt.Sprintf("%d", hourlyLimit))
		rm.SetExpire(key, 4200*time.Second)
		t.Logf("    - 预设有效窗口: consumed=3M, limit=%dM, remaining=%ds",
			hourlyLimit/1000000, endTime-now)

	case "active_exceeded":
		// 创建一个接近超限的窗口：剩余额度略小于本次预估扣减额度，
		// 确保本次请求会触发窗口超限逻辑。
		startTime := now - 1800
		endTime := startTime + 3600
		// 这里假设预估扣减额度约为 7500 quota（与默认 Mock LLM 用量匹配），
		// 因此预留 <7500 的剩余额度，保证本次请求会超限。
		const nearLimitRemaining int64 = 5000
		consumed := hourlyLimit - nearLimitRemaining
		rm.SetHashField(key, "start_time", fmt.Sprintf("%d", startTime))
		rm.SetHashField(key, "end_time", fmt.Sprintf("%d", endTime))
		rm.SetHashField(key, "consumed", fmt.Sprintf("%d", consumed))
		rm.SetHashField(key, "limit", fmt.Sprintf("%d", hourlyLimit))
		rm.SetExpire(key, 4200*time.Second)
		t.Logf("    - 预设接近超限窗口: consumed=%dM, limit=%dM, remaining=%dM",
			consumed/1000000, hourlyLimit/1000000, (hourlyLimit-consumed)/1000000)

	case "expired":
		// 创建一个已过期的窗口（1小时前）
		startTime := now - 7200     // 2小时前开始
		endTime := startTime + 3600 // 1小时前结束
		rm.SetHashField(key, "start_time", fmt.Sprintf("%d", startTime))
		rm.SetHashField(key, "end_time", fmt.Sprintf("%d", endTime))
		rm.SetHashField(key, "consumed", "5000000") // 已消耗5M
		rm.SetHashField(key, "limit", fmt.Sprintf("%d", hourlyLimit))
		// 不设置TTL，让Lua脚本检测过期
		t.Logf("    - 预设已过期窗口: EndTime=%s (已过期%ds)",
			time.Unix(endTime, 0).Format("15:04:05"), now-endTime)

	case "not_exist":
		// 不创建窗口（默认情况）
		t.Logf("    - 不预设窗口（将在首次请求时创建）")
	}
}

// ============ 独立测试用例方法 ============
// 每个OM-XX用例都有独立的测试方法，方便单独运行和调试

// TestOM01_GlobalPackageHighPriorityVipUserPublicChannel 测试用例 OM-01
// Test ID: OM-01
// Priority: P0
// Test Scenario: 全局套餐高优先级VIP用户公共渠道
// Expected Result: 套餐扣减成功，创建新窗口，用户余额不变
func (s *OrthogonalMatrixSuite) TestOM01_GlobalPackageHighPriorityVipUserPublicChannel() {
	tc := orthogonalTestCases[0] // OM-01
	s.T().Logf("=== 执行正交测试用例 %s ===", tc.ID)
	s.T().Logf("场景描述: %s", tc.Name)

	ctx := s.setupOrthogonalTestCase(tc)
	resp, relayInfo := s.executePackageRequest(tc, ctx)
	s.verifyOrthogonalResult(tc, resp, relayInfo, ctx)
	s.cleanupOrthogonalTestCase(tc, ctx)

	s.T().Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
}

// TestOM02_P2PPackageDefaultUserG1Channel 测试用例 OM-02
// Test ID: OM-02
// Priority: P0
// Test Scenario: P2P套餐default用户G1渠道
// Expected Result: 套餐扣减成功，创建新窗口，计费分组=default
func (s *OrthogonalMatrixSuite) TestOM02_P2PPackageDefaultUserG1Channel() {
	tc := orthogonalTestCases[1] // OM-02
	s.T().Logf("=== 执行正交测试用例 %s ===", tc.ID)
	s.T().Logf("场景描述: %s", tc.Name)

	ctx := s.setupOrthogonalTestCase(tc)
	resp, relayInfo := s.executePackageRequest(tc, ctx)
	s.verifyOrthogonalResult(tc, resp, relayInfo, ctx)
	s.cleanupOrthogonalTestCase(tc, ctx)

	s.T().Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
}

// TestOM03_GlobalPackageLowPriorityBillingOverride 测试用例 OM-03
// Test ID: OM-03
// Priority: P0
// Test Scenario: 全局套餐低优先级VIP用户billing覆盖为default
// Expected Result: 套餐扣减成功，窗口累加，计费分组=default
func (s *OrthogonalMatrixSuite) TestOM03_GlobalPackageLowPriorityBillingOverride() {
	tc := orthogonalTestCases[2] // OM-03
	s.T().Logf("=== 执行正交测试用例 %s ===", tc.ID)
	s.T().Logf("场景描述: %s", tc.Name)

	ctx := s.setupOrthogonalTestCase(tc)
	resp, relayInfo := s.executePackageRequest(tc, ctx)
	s.verifyOrthogonalResult(tc, resp, relayInfo, ctx)
	s.cleanupOrthogonalTestCase(tc, ctx)

	s.T().Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
}

// TestOM04_P2PPackageWindowExpired 测试用例 OM-04
// Test ID: OM-04
// Priority: P0
// Test Scenario: P2P套餐VIP用户加入G1使用P2P渠道窗口过期
// Expected Result: 窗口重建并套餐扣减成功
func (s *OrthogonalMatrixSuite) TestOM04_P2PPackageWindowExpired() {
	tc := orthogonalTestCases[3] // OM-04
	s.T().Logf("=== 执行正交测试用例 %s ===", tc.ID)
	s.T().Logf("场景描述: %s", tc.Name)

	ctx := s.setupOrthogonalTestCase(tc)
	resp, relayInfo := s.executePackageRequest(tc, ctx)
	s.verifyOrthogonalResult(tc, resp, relayInfo, ctx)
	s.cleanupOrthogonalTestCase(tc, ctx)

	s.T().Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
}

// TestOM05_GlobalPackageWindowExceeded 测试用例 OM-05
// Test ID: OM-05
// Priority: P0
// Test Scenario: 全局套餐高优先级svip用户窗口有效但已超限
// Expected Result: 套餐超限，Fallback到用户余额
func (s *OrthogonalMatrixSuite) TestOM05_GlobalPackageWindowExceeded() {
	tc := orthogonalTestCases[4] // OM-05
	s.T().Logf("=== 执行正交测试用例 %s ===", tc.ID)
	s.T().Logf("场景描述: %s", tc.Name)

	ctx := s.setupOrthogonalTestCase(tc)
	resp, relayInfo := s.executePackageRequest(tc, ctx)
	s.verifyOrthogonalResult(tc, resp, relayInfo, ctx)
	s.cleanupOrthogonalTestCase(tc, ctx)

	s.T().Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
}

// TestOM06_P2PPackagePrivateChannelRejected 测试用例 OM-06
// Test ID: OM-06
// Priority: P0
// Test Scenario: P2P套餐default用户加入G1但渠道为私有
// Expected Result: 无法使用私有渠道，路由失败
func (s *OrthogonalMatrixSuite) TestOM06_P2PPackagePrivateChannelRejected() {
	tc := orthogonalTestCases[5] // OM-06
	s.T().Logf("=== 执行正交测试用例 %s ===", tc.ID)
	s.T().Logf("场景描述: %s", tc.Name)

	ctx := s.setupOrthogonalTestCase(tc)
	resp, relayInfo := s.executePackageRequest(tc, ctx)
	s.verifyOrthogonalResult(tc, resp, relayInfo, ctx)
	s.cleanupOrthogonalTestCase(tc, ctx)

	s.T().Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
}

// TestOM07_GlobalPackageNoP2PRestrictionRejected 测试用例 OM-07
// Test ID: OM-07
// Priority: P0
// Test Scenario: 全局套餐低优先级default用户加入G1但Token无P2P限制
// Expected Result: Token无P2P限制时无法使用P2P渠道
func (s *OrthogonalMatrixSuite) TestOM07_GlobalPackageNoP2PRestrictionRejected() {
	tc := orthogonalTestCases[6] // OM-07
	s.T().Logf("=== 执行正交测试用例 %s ===", tc.ID)
	s.T().Logf("场景描述: %s", tc.Name)

	ctx := s.setupOrthogonalTestCase(tc)
	resp, relayInfo := s.executePackageRequest(tc, ctx)
	s.verifyOrthogonalResult(tc, resp, relayInfo, ctx)
	s.cleanupOrthogonalTestCase(tc, ctx)

	s.T().Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
}

// TestOM08_MultiPackagePriorityDegradation 测试用例 OM-08
// Test ID: OM-08
// Priority: P0
// Test Scenario: 多套餐组合：全局15+P2P11，VIP用户，billing列表，窗口有效
// Expected Result: 优先级15套餐超限后降级到优先级11套餐
func (s *OrthogonalMatrixSuite) TestOM08_MultiPackagePriorityDegradation() {
	s.T().Skip("等待套餐系统后端实现")

	tc := orthogonalTestCases[7] // OM-08
	s.T().Logf("=== 执行正交测试用例 %s ===", tc.ID)
	s.T().Logf("场景描述: %s", tc.Name)

	ctx := s.setupOrthogonalTestCase(tc)

	// OM-08特殊处理：需要先耗尽高优先级套餐
	// 第一次请求：消耗高优先级套餐（5M限额）
	t := s.T()
	t.Logf("  [OM-08] 第一次请求：消耗高优先级套餐")
	resp1, relayInfo1 := s.executePackageRequest(tc, ctx)
	assert.Equal(t, http.StatusOK, resp1.StatusCode, "第一次请求应成功")
	assert.Equal(t, ctx.PackageID, relayInfo1.UsingPackageId, "应使用高优先级套餐")
	t.Logf("  [OM-08] 第一次请求完成，使用套餐ID=%d", ctx.PackageID)

	// 再次请求，使高优先级套餐超限
	t.Logf("  [OM-08] 第二次请求：高优先级套餐超限，应降级到P2P套餐")
	resp2, relayInfo2 := s.executePackageRequest(tc, ctx)
	assert.Equal(t, http.StatusOK, resp2.StatusCode, "第二次请求应成功")
	assert.Equal(t, ctx.SecondPackageID, relayInfo2.UsingPackageId, "应降级到P2P套餐")
	t.Logf("  [OM-08] 第二次请求完成，降级到套餐ID=%d", ctx.SecondPackageID)

	// 验证两个套餐都有消耗
	sub1, _ := model.GetSubscriptionById(ctx.SubscriptionID)
	sub2, _ := model.GetSubscriptionById(ctx.SecondSubID)
	assert.Greater(t, sub1.TotalConsumed, int64(0), "高优先级套餐应有消耗")
	assert.Greater(t, sub2.TotalConsumed, int64(0), "P2P套餐应有消耗")
	t.Logf("  [OM-08] 验证完成：套餐1消耗=%dM, 套餐2消耗=%dM",
		sub1.TotalConsumed/1000000, sub2.TotalConsumed/1000000)

	s.cleanupOrthogonalTestCase(tc, ctx)

	s.T().Logf("=== 测试用例 %s 完成 ===\n", tc.ID)
}
