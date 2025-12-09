package routing_authorization

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// TestToken_TKN01_ModelWhitelist verifies that token-level model whitelist
// blocks access to models not in the allowed list.
// Design ref: 2.8 TKN-01 令牌模型白名单.
func TestToken_TKN01_ModelWhitelist(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// User in default group.
	_, err := fixtures.CreateTestUser("tkn01_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	userClient := suite.Client.Clone()
	if _, err := userClient.Login("tkn01_user", "password123"); err != nil {
		t.Fatalf("failed to login user: %v", err)
	}

	// Channel that serves both allowed and forbidden models.
	_, err = fixtures.CreateTestChannel(
		"tkn01-multi-model-channel",
		"gpt-4o,claude-3-opus",
		"default",
		suite.Upstream.BaseURL,
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create multi-model channel: %v", err)
	}

	// Token that only allows gpt-4o.
	tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "tkn01-token-gpt4o-only",
		Status:             1,
		UnlimitedQuota:     true,
		ModelLimitsJson:    "gpt-4o",
		ModelLimitsEnabled: true,
	})
	if err != nil {
		t.Fatalf("failed to create limited token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)

	// Request allowed model -> should succeed.
	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4o", "TKN-01 allowed model request")
	if !success {
		t.Fatalf("expected allowed model request to succeed, got status=%d err=%s", statusCode, errMsg)
	}

	// Request forbidden model -> should be rejected before hitting upstream.
	success, statusCode, errMsg = apiClient.TryChatCompletion("claude-3-opus", "TKN-01 forbidden model request")
	if success {
		t.Fatalf("expected forbidden model request to fail due to model whitelist")
	}
	if statusCode != 403 {
		t.Fatalf("expected status 403 for forbidden model, got %d (err=%s)", statusCode, errMsg)
	}
	if !strings.Contains(errMsg, "该令牌无权访问模型") {
		t.Fatalf("unexpected error message for forbidden model: %s", errMsg)
	}

	// Upstream should only see the allowed model request.
	if suite.Upstream.GetRequestCount() != 1 {
		t.Fatalf("expected 1 upstream request (allowed model only), got %d", suite.Upstream.GetRequestCount())
	}
}

// TestToken_TKN02_IPWhitelist verifies that token-level IP whitelist blocks
// requests coming from non-whitelisted IPs.
// Design ref: 2.8 TKN-02 令牌IP白名单.
func TestToken_TKN02_IPWhitelist(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures

	// User in default group.
	_, err := fixtures.CreateTestUser("tkn02_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	userClient := suite.Client.Clone()
	if _, err := userClient.Login("tkn02_user", "password123"); err != nil {
		t.Fatalf("failed to login user: %v", err)
	}

	// Simple channel for gpt-4.
	_, err = fixtures.CreateTestChannel(
		"tkn02-channel",
		"gpt-4",
		"default",
		suite.Upstream.BaseURL,
		false,
		0,
		"",
	)
	if err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	// Token with IP whitelist that does NOT include 127.0.0.1.
	// Our test client will originate from 127.0.0.1, so the request should be rejected.
	allowIps := "192.168.1.100"
	tokenKey, err := userClient.CreateTokenFull(&testutil.TokenModel{
		Name:               "tkn02-token-ip-whitelist",
		Status:             1,
		UnlimitedQuota:     true,
		AllowIps:           &allowIps,
		ModelLimitsJson:    "",
		ModelLimitsEnabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create IP-limited token: %v", err)
	}

	apiClient := suite.Client.WithToken(tokenKey)

	success, statusCode, errMsg := apiClient.TryChatCompletion("gpt-4", "TKN-02 IP whitelist request")
	if success {
		t.Fatalf("expected request to be rejected by IP whitelist, but it succeeded")
	}
	if statusCode != 403 {
		t.Fatalf("expected status 403 for IP whitelist failure, got %d (err=%s)", statusCode, errMsg)
	}
	if !strings.Contains(errMsg, "IP 不在令牌允许访问的列表") {
		t.Fatalf("unexpected error message for IP whitelist failure: %s", errMsg)
	}

	if suite.Upstream.GetRequestCount() != 0 {
		t.Fatalf("expected 0 upstream requests for IP-rejected token, got %d", suite.Upstream.GetRequestCount())
	}
}
