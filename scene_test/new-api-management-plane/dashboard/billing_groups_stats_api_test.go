// Package dashboard contains integration tests for the management-plane
// dashboard / statistics APIs. This file focuses on the personal billing
// group statistics endpoints (/api/billing_groups/self/*), in particular:
//   - UBG-04: 权限与隔离性 - 确认每个用户只能看到自己的计费分组聚合结果。
package dashboard

import (
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// userBillingGroupStats models a single item from /api/billing_groups/self/stats.
type userBillingGroupStats struct {
	BillingGroup string `json:"billing_group"`
	TotalTokens  int64  `json:"total_tokens"`
	TotalQuota   int64  `json:"total_quota"`
	RequestCount int    `json:"request_count"`
	TPM          int    `json:"tpm"`
	RPM          int    `json:"rpm"`
}

// billingGroupStatsResponse models the JSON response for /api/billing_groups/self/stats.
type billingGroupStatsResponse struct {
	Success bool                    `json:"success"`
	Message string                  `json:"message"`
	Data    []userBillingGroupStats `json:"data"`
}

// TestBillingGroups_UBG04_UserIsolation implements UBG-04:
//   - Create two normal users U_A and U_B (both in default billing group).
//   - Each user uses自己的 token 调用 /v1/chat/completions 不同次数 (U_A:1次, U_B:3次)。
//   - 调用各自的 /api/billing_groups/self/stats?period=7d。
//   - 断言：
//   - 两个调用均成功且返回至少一条 "default" 计费分组记录。
//   - U_A 的 default.RequestCount=1, U_B 的 default.RequestCount=3。
//     若后端错误地跨用户聚合或忽略 user_id 过滤，则两个用户看到的 request_count 将相同，测试会失败。
func TestBillingGroups_UBG04_UserIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping billing group isolation integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Create two default-group users.
	userA, err := fixtures.CreateTestUser("ubg04_userA", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user A: %v", err)
	}
	userB, err := fixtures.CreateTestUser("ubg04_userB", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user B: %v", err)
	}

	// Login each user with its own session client.
	userAClient := suite.Client.Clone()
	if _, err := userAClient.Login(userA.Username, "password123"); err != nil {
		t.Fatalf("failed to login user A: %v", err)
	}
	userBClient := suite.Client.Clone()
	if _, err := userBClient.Login(userB.Username, "password123"); err != nil {
		t.Fatalf("failed to login user B: %v", err)
	}

	// Create a simple default-group channel bound to the mock upstream.
	baseURL := fixtures.GetUpstreamURL()
	channel, err := fixtures.CreateTestChannel(
		"ubg04-default-channel",
		"gpt-4",
		"default",
		baseURL,
		false, // not private
		0,     // no explicit owner
		"",    // no P2P restriction
	)
	if err != nil {
		t.Fatalf("failed to create test channel: %v", err)
	}
	t.Logf("UBG-04: created shared default channel id=%d", channel.ID)

	// Create unlimited tokens for each user.
	tokenA, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "ubg04-token-A",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token for user A: %v", err)
	}

	tokenB, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "ubg04-token-B",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token for user B: %v", err)
	}

	// Use token-based clients for data-plane requests.
	apiClientA := suite.Client.WithToken(tokenA)
	apiClientB := suite.Client.WithToken(tokenB)

	// U_A: send 1 request.
	if ok, status, msg := apiClientA.TryChatCompletion("gpt-4", "UBG-04 user A request"); !ok {
		t.Fatalf("user A chat completion failed: status=%d msg=%s", status, msg)
	}

	// U_B: send 3 requests.
	for i := 0; i < 3; i++ {
		if ok, status, msg := apiClientB.TryChatCompletion("gpt-4", "UBG-04 user B request"); !ok {
			t.Fatalf("user B chat completion %d failed: status=%d msg=%s", i, status, msg)
		}
	}

	// Call /api/billing_groups/self/stats for each user (must use session clients).
	var respA billingGroupStatsResponse
	if err := userAClient.GetJSON("/api/billing_groups/self/stats?period=7d", &respA); err != nil {
		t.Fatalf("failed to call /api/billing_groups/self/stats for user A: %v", err)
	}
	if !respA.Success {
		t.Fatalf("user A billing_groups/self/stats returned success=false: %s", respA.Message)
	}

	var respB billingGroupStatsResponse
	if err := userBClient.GetJSON("/api/billing_groups/self/stats?period=7d", &respB); err != nil {
		t.Fatalf("failed to call /api/billing_groups/self/stats for user B: %v", err)
	}
	if !respB.Success {
		t.Fatalf("user B billing_groups/self/stats returned success=false: %s", respB.Message)
	}

	// Helper to find the "default" billing group entry.
	findDefault := func(stats []userBillingGroupStats) *userBillingGroupStats {
		for i := range stats {
			if stats[i].BillingGroup == "default" {
				return &stats[i]
			}
		}
		return nil
	}

	aDefault := findDefault(respA.Data)
	if aDefault == nil {
		t.Fatalf("user A stats missing default billing_group entry: %+v", respA.Data)
	}
	bDefault := findDefault(respB.Data)
	if bDefault == nil {
		t.Fatalf("user B stats missing default billing_group entry: %+v", respB.Data)
	}

	// Isolation check:
	//   - User A made exactly 1 request, so RequestCount should be 1.
	//   - User B made exactly 3 requests, so RequestCount should be 3.
	if aDefault.RequestCount != 1 {
		t.Fatalf("user A default.RequestCount mismatch: got %d, want 1", aDefault.RequestCount)
	}
	if bDefault.RequestCount != 3 {
		t.Fatalf("user B default.RequestCount mismatch: got %d, want 3", bDefault.RequestCount)
	}

	// Sanity check: both users should have positive quota/tokens.
	if aDefault.TotalQuota <= 0 || aDefault.TotalTokens <= 0 {
		t.Fatalf("user A default stats should have positive tokens/quota, got tokens=%d quota=%d",
			aDefault.TotalTokens, aDefault.TotalQuota)
	}
	if bDefault.TotalQuota <= 0 || bDefault.TotalTokens <= 0 {
		t.Fatalf("user B default stats should have positive tokens/quota, got tokens=%d quota=%d",
			bDefault.TotalTokens, bDefault.TotalQuota)
	}

	t.Logf("UBG-04: user A and user B billing group stats are isolated: A.RequestCount=%d, B.RequestCount=%d",
		aDefault.RequestCount, bDefault.RequestCount)
}

// userBillingGroupDailyUsage models a single item from /api/billing_groups/self/daily_tokens.
type userBillingGroupDailyUsage struct {
	Day          string `json:"day"`
	BillingGroup string `json:"billing_group"`
	Tokens       int64  `json:"tokens"`
	Quota        int64  `json:"quota"`
}

// billingGroupDailyTokensResponse models the JSON response for /api/billing_groups/self/daily_tokens.
type billingGroupDailyTokensResponse struct {
	Success bool                         `json:"success"`
	Message string                       `json:"message"`
	Data    []userBillingGroupDailyUsage `json:"data"`
}

// TestBillingGroups_UBG03_DailyTokensBasic implements UBG-03 的核心接口覆盖：
//   - 单用户在 default 计费分组下产生若干消费日志。
//   - 调用 /api/billing_groups/self/daily_tokens?days=30。
//   - 校验返回至少一条记录，billing_group=default 且 tokens/quota 为正数。
func TestBillingGroups_UBG03_DailyTokensBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping billing group daily tokens integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Create a single default-group user.
	user, err := fixtures.CreateTestUser("ubg03_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create ubg03_user: %v", err)
	}

	// Login session client.
	userClient := suite.Client.Clone()
	if _, err := userClient.Login(user.Username, "password123"); err != nil {
		t.Fatalf("failed to login ubg03_user: %v", err)
	}

	// Create a simple default-group channel.
	baseURL := fixtures.GetUpstreamURL()
	channel, err := fixtures.CreateTestChannel(
		"ubg03-default-channel",
		"gpt-4",
		"default",
		baseURL,
		false,
		user.ID,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create ubg03 channel: %v", err)
	}
	t.Logf("UBG-03: created channel id=%d for user %s", channel.ID, user.Username)

	// Create an unlimited token and send a couple of requests.
	tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "ubg03-token",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create ubg03 token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)
	for i := 0; i < 2; i++ {
		if ok, status, msg := apiClient.TryChatCompletion("gpt-4", "UBG-03 daily tokens request"); !ok {
			t.Fatalf("ubg03 chat completion %d failed: status=%d msg=%s", i, status, msg)
		}
	}

	// Query daily tokens for the current user; must use session client.
	var resp billingGroupDailyTokensResponse
	if err := userClient.GetJSON("/api/billing_groups/self/daily_tokens?days=30", &resp); err != nil {
		t.Fatalf("failed to call /api/billing_groups/self/daily_tokens: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/billing_groups/self/daily_tokens returned success=false: %s", resp.Message)
	}

	if len(resp.Data) == 0 {
		t.Fatalf("expected at least one daily tokens record, got 0")
	}

	for _, item := range resp.Data {
		if item.BillingGroup != "default" {
			t.Fatalf("expected billing_group=default, got %s", item.BillingGroup)
		}
		if item.Tokens <= 0 || item.Quota <= 0 {
			t.Fatalf("expected positive tokens/quota, got tokens=%d quota=%d", item.Tokens, item.Quota)
		}
	}
}

// TestBillingGroups_SystemStats_GlobalAggregation verifies that the new
// system-level billing group endpoint (/api/billing_groups/system/stats)
// aggregates consumption across all users while preserving the same
// response shape as /api/billing_groups/self/stats.
//
// Scenario:
//   - Create two default-group users U_A and U_B.
//   - Each user uses自己的 token 调用 /v1/chat/completions 不同次数 (U_A:1次, U_B:3次)。
//   - 调用各自的 /api/billing_groups/self/stats?period=7d，得到 default 分组的
//     total_tokens/total_quota/request_count。
//   - 使用任一登录用户调用 /api/billing_groups/system/stats?period=7d。
//   - 断言系统级 default 分组的 total_tokens/total_quota/request_count
//     等于两位用户对应字段之和。
func TestBillingGroups_SystemStats_GlobalAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping system billing group stats integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// Create two default-group users.
	userA, err := fixtures.CreateTestUser("sysbg_userA", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create sysbg_userA: %v", err)
	}
	userB, err := fixtures.CreateTestUser("sysbg_userB", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create sysbg_userB: %v", err)
	}

	// Login each user with its own session client.
	userAClient := suite.Client.Clone()
	if _, err := userAClient.Login(userA.Username, "password123"); err != nil {
		t.Fatalf("failed to login sysbg_userA: %v", err)
	}
	userBClient := suite.Client.Clone()
	if _, err := userBClient.Login(userB.Username, "password123"); err != nil {
		t.Fatalf("failed to login sysbg_userB: %v", err)
	}

	// Create a shared default-group channel backed by the mock upstream.
	baseURL := fixtures.GetUpstreamURL()
	channel, err := fixtures.CreateTestChannel(
		"sysbg-default-channel",
		"gpt-4",
		"default",
		baseURL,
		false, // not private
		0,     // no explicit owner
		"",    // no P2P restriction
	)
	if err != nil {
		t.Fatalf("failed to create sysbg-default-channel: %v", err)
	}
	t.Logf("SYSBG: created shared default channel id=%d", channel.ID)

	// Create unlimited tokens for each user.
	tokenA, err := userAClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "sysbg-token-A",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token for sysbg_userA: %v", err)
	}

	tokenB, err := userBClient.CreateTokenFull(&testutil.TokenModel{
		Name:           "sysbg-token-B",
		Status:         1,
		UnlimitedQuota: true,
	})
	if err != nil {
		t.Fatalf("failed to create token for sysbg_userB: %v", err)
	}

	// Use token-based clients for data-plane requests.
	apiClientA := suite.Client.WithToken(tokenA)
	apiClientB := suite.Client.WithToken(tokenB)

	// U_A: send 1 request.
	if ok, status, msg := apiClientA.TryChatCompletion("gpt-4", "SYSBG user A request"); !ok {
		t.Fatalf("sysbg_userA chat completion failed: status=%d msg=%s", status, msg)
	}

	// U_B: send 3 requests.
	for i := 0; i < 3; i++ {
		if ok, status, msg := apiClientB.TryChatCompletion("gpt-4", "SYSBG user B request"); !ok {
			t.Fatalf("sysbg_userB chat completion %d failed: status=%d msg=%s", i, status, msg)
		}
	}

	// Helper to find a billing group entry by name.
	findGroup := func(stats []userBillingGroupStats, name string) *userBillingGroupStats {
		for i := range stats {
			if stats[i].BillingGroup == name {
				return &stats[i]
			}
		}
		return nil
	}

	// Query personal stats for each user via /api/billing_groups/self/stats.
	var respA billingGroupStatsResponse
	if err := userAClient.GetJSON("/api/billing_groups/self/stats?period=7d", &respA); err != nil {
		t.Fatalf("failed to call /api/billing_groups/self/stats for sysbg_userA: %v", err)
	}
	if !respA.Success {
		t.Fatalf("sysbg_userA billing_groups/self/stats returned success=false: %s", respA.Message)
	}
	aDefault := findGroup(respA.Data, "default")
	if aDefault == nil {
		t.Fatalf("sysbg_userA stats missing default billing_group entry: %+v", respA.Data)
	}

	var respB billingGroupStatsResponse
	if err := userBClient.GetJSON("/api/billing_groups/self/stats?period=7d", &respB); err != nil {
		t.Fatalf("failed to call /api/billing_groups/self/stats for sysbg_userB: %v", err)
	}
	if !respB.Success {
		t.Fatalf("sysbg_userB billing_groups/self/stats returned success=false: %s", respB.Message)
	}
	bDefault := findGroup(respB.Data, "default")
	if bDefault == nil {
		t.Fatalf("sysbg_userB stats missing default billing_group entry: %+v", respB.Data)
	}

	// Query system-level billing group stats using a logged-in user (any user is fine).
	var sysResp billingGroupStatsResponse
	if err := userAClient.GetJSON("/api/billing_groups/system/stats?period=7d", &sysResp); err != nil {
		t.Fatalf("failed to call /api/billing_groups/system/stats: %v", err)
	}
	if !sysResp.Success {
		t.Fatalf("/api/billing_groups/system/stats returned success=false: %s", sysResp.Message)
	}

	sysDefault := findGroup(sysResp.Data, "default")
	if sysDefault == nil {
		t.Fatalf("system stats missing default billing_group entry: %+v", sysResp.Data)
	}

	// System-level default stats should equal the sum of the two users' default stats
	// for request_count/total_tokens/total_quota. TPM/RPM are derived from the same
	// underlying logs but use slightly different endTime snapshots,所以这里只比较总量字段。
	wantRequests := aDefault.RequestCount + bDefault.RequestCount
	if sysDefault.RequestCount != wantRequests {
		t.Fatalf("system default.RequestCount mismatch: got %d, want %d (a=%d,b=%d)",
			sysDefault.RequestCount, wantRequests, aDefault.RequestCount, bDefault.RequestCount)
	}

	wantTokens := aDefault.TotalTokens + bDefault.TotalTokens
	if sysDefault.TotalTokens != wantTokens {
		t.Fatalf("system default.TotalTokens mismatch: got %d, want %d (a=%d,b=%d)",
			sysDefault.TotalTokens, wantTokens, aDefault.TotalTokens, bDefault.TotalTokens)
	}

	wantQuota := aDefault.TotalQuota + bDefault.TotalQuota
	if sysDefault.TotalQuota != wantQuota {
		t.Fatalf("system default.TotalQuota mismatch: got %d, want %d (a=%d,b=%d)",
			sysDefault.TotalQuota, wantQuota, aDefault.TotalQuota, bDefault.TotalQuota)
	}
}
