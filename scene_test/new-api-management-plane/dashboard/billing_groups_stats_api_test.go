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
