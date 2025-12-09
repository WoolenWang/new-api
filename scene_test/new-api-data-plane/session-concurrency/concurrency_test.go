// Package session_concurrency contains end-to-end tests for user session
// concurrency limits and session monitoring.
//
// It implements the scenarios defined in
// docs/01-NewAPI数据面转发渠道粘性和限量问题-测试设计.md §2.2:
// - C-01: 超出并发会话数限制
// - C-01-Boundary: 并发上限为 0 或 1 的边界行为
// - C-02: 复用已有会话不计入并发
// - C-03: 会话结束/移除后并发额度恢复
// - C-04: 监控 API 数据准确性
package session_concurrency

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// ConcurrencySuite holds shared resources for the concurrency tests.
type ConcurrencySuite struct {
	Server      *testutil.TestServer
	AdminClient *testutil.APIClient
	Fixtures    *testutil.TestFixtures

	Upstream *testutil.MockUpstreamServer

	RedisServer *miniredis.Miniredis
	RedisClient *redis.Client
}

// setupConcurrencySuite starts Redis, the test server and basic fixtures.
func setupConcurrencySuite(t *testing.T) (*ConcurrencySuite, func()) {
	t.Helper()

	// Dedicated in-memory Redis for this suite.
	mr, err := miniredis.Run()
	require.NoError(t, err, "failed to start miniredis")

	redisURL := fmt.Sprintf("redis://%s/0", mr.Addr())

	// Single mock upstream for all concurrency tests.
	up := testutil.NewMockUpstreamServer()

	projectRoot, err := testutil.FindProjectRoot()
	require.NoError(t, err, "failed to find project root")

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()
	if cfg.CustomEnv == nil {
		cfg.CustomEnv = make(map[string]string)
	}
	cfg.CustomEnv["REDIS_CONN_STRING"] = redisURL

	server, err := testutil.StartServer(cfg)
	if err != nil {
		up.Close()
		mr.Close()
		require.NoError(t, err, "failed to start test server")
	}

	adminClient := testutil.NewAPIClient(server)

	// Initialize system and login as root (admin).
	rootUser, rootPass, err := adminClient.InitializeSystem()
	require.NoError(t, err, "failed to initialize system")

	_, err = adminClient.Login(rootUser, rootPass)
	require.NoError(t, err, "failed to login as root")

	fixtures := testutil.NewTestFixtures(t, adminClient)
	fixtures.SetUpstream(up)

	// Redis client for inspecting concurrency keys.
	opt, err := redis.ParseURL(redisURL)
	require.NoError(t, err, "failed to parse redis URL")
	rdb := redis.NewClient(opt)

	suite := &ConcurrencySuite{
		Server:      server,
		AdminClient: adminClient,
		Fixtures:    fixtures,
		Upstream:    up,
		RedisServer: mr,
		RedisClient: rdb,
	}

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if suite.RedisClient != nil {
			_ = suite.RedisClient.FlushDB(ctx).Err()
			_ = suite.RedisClient.Close()
		}
		if suite.Fixtures != nil {
			suite.Fixtures.Cleanup()
		}
		if suite.Server != nil {
			_ = suite.Server.Stop()
		}
		if suite.Upstream != nil {
			suite.Upstream.Close()
		}
		if suite.RedisServer != nil {
			suite.RedisServer.Close()
		}
	}

	return suite, cleanup
}

// buildChatRequest builds an HTTP request for /v1/chat/completions using a raw
// token string and optional extra headers (e.g. session id).
func buildChatRequest(t *testing.T, baseURL, token, model string, extraHeaders map[string]string) *http.Request {
	t.Helper()

	body := testutil.ChatCompletionRequest{
		Model: model,
		Messages: []testutil.ChatMessage{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err, "failed to marshal chat body")

	req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(raw))
	require.NoError(t, err, "failed to create request")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	return req
}

// createUserTokenAndChannel creates a user in the given group, an API token,
// and a default gpt-4 channel for that group.
func createUserTokenAndChannel(t *testing.T, suite *ConcurrencySuite, username, group string) (userID int, tokenKey string, channelID int) {
	t.Helper()

	user, err := suite.Fixtures.CreateTestUser(username, "testpass123", group)
	require.NoError(t, err, "failed to create test user")

	userClient := suite.AdminClient.Clone()
	_, err = userClient.Login(username, "testpass123")
	require.NoError(t, err, "failed to login test user")

	tokenKey, err = suite.Fixtures.CreateTestAPIToken(username+"-token", userClient, nil)
	require.NoError(t, err, "failed to create API token")

	ch, err := suite.Fixtures.CreateTestChannel(
		fmt.Sprintf("%s-channel", username),
		"gpt-4",
		group,
		suite.Fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	require.NoError(t, err, "failed to create test channel")

	return user.ID, tokenKey, ch.ID
}

// setSystemMaxConcurrentSessions updates the SystemMaxConcurrentSessions option.
func setSystemMaxConcurrentSessions(t *testing.T, suite *ConcurrencySuite, limit int) {
	t.Helper()
	var resp testutil.APIResponse
	err := suite.AdminClient.PutJSON("/api/option", map[string]interface{}{
		"key":   "SystemMaxConcurrentSessions",
		"value": strconv.Itoa(limit),
	}, &resp)
	require.NoError(t, err, "failed to update SystemMaxConcurrentSessions")
	require.True(t, resp.Success, "SystemMaxConcurrentSessions option update should succeed")
}

// setGroupMaxConcurrentSessions updates per-group max_concurrent_sessions limits.
func setGroupMaxConcurrentSessions(t *testing.T, suite *ConcurrencySuite, limits map[string]int) {
	t.Helper()
	raw, err := json.Marshal(limits)
	require.NoError(t, err, "failed to marshal group limits")

	var resp testutil.APIResponse
	err = suite.AdminClient.PutJSON("/api/option", map[string]interface{}{
		"key":   "GroupMaxConcurrentSessions",
		"value": string(raw),
	}, &resp)
	require.NoError(t, err, "failed to update GroupMaxConcurrentSessions")
	require.True(t, resp.Success, "GroupMaxConcurrentSessions option update should succeed")
}

// getUserSessionSetSize returns SCARD session:user:{id}.
func getUserSessionSetSize(t *testing.T, suite *ConcurrencySuite, userID int) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := fmt.Sprintf("session:user:%d", userID)
	count, err := suite.RedisClient.SCard(ctx, key).Result()
	require.NoError(t, err, "failed to SCARD user session set")
	return count
}

// setUserMaxConcurrentSessions updates a specific user's max_concurrent_sessions via admin API.
func setUserMaxConcurrentSessions(t *testing.T, suite *ConcurrencySuite, userID int, username, group, password string, limit int) {
	t.Helper()

	// 先读取当前用户信息，保留现有配额等字段，避免被意外重置。
	existing, err := suite.AdminClient.GetUser(userID)
	require.NoError(t, err, "failed to load user before updating max_concurrent_sessions")

	update := &testutil.UserModel{
		ID:                    userID,
		Username:              existing.Username,
		Password:              password,
		Group:                 existing.Group,
		DisplayName:           existing.DisplayName,
		Quota:                 existing.Quota,
		MaxConcurrentSessions: limit,
	}
	err = suite.AdminClient.UpdateUser(update)
	require.NoError(t, err, "failed to update user max_concurrent_sessions")
}

// --- Test Cases ---

// TestConcurrency_C01_ExceedLimit
// C-01: 超出并发会话数限制
func TestConcurrency_C01_ExceedLimit(t *testing.T) {
	suite, cleanup := setupConcurrencySuite(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	// Ensure we rely on group-level limit only.
	setSystemMaxConcurrentSessions(t, suite, 0)
	setGroupMaxConcurrentSessions(t, suite, map[string]int{
		"default": 2,
	})

	userID, token, _ := createUserTokenAndChannel(t, suite, "c01-user", "default")
	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)

	sessionIDs := []string{"c01-sid-1", "c01-sid-2", "c01-sid-3"}
	statusCodes := make([]int, 0, len(sessionIDs))

	for _, sid := range sessionIDs {
		req := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
			"X-NewAPI-Session-ID": sid,
		})
		resp, err := client.HTTPClient.Do(req)
		require.NoError(t, err)
		statusCodes = append(statusCodes, resp.StatusCode)
		resp.Body.Close()
	}

	require.Equal(t, http.StatusOK, statusCodes[0], "first session should be accepted")
	require.Equal(t, http.StatusOK, statusCodes[1], "second session should be accepted")
	require.Equal(t, http.StatusTooManyRequests, statusCodes[2], "third session should be rejected due to concurrency limit")

	count := getUserSessionSetSize(t, suite, userID)
	require.Equal(t, int64(2), count, "user session set size should be 2")
}

// TestConcurrency_C01_BoundaryLimitOne
// C-01-Boundary (part): 并发上限为 1 的行为
func TestConcurrency_C01_BoundaryLimitOne(t *testing.T) {
	suite, cleanup := setupConcurrencySuite(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	setSystemMaxConcurrentSessions(t, suite, 0)
	setGroupMaxConcurrentSessions(t, suite, map[string]int{
		"default": 1,
	})

	userID, token, _ := createUserTokenAndChannel(t, suite, "c01b-user", "default")
	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)

	// First new session should succeed.
	req1 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": "c01b-sid-1",
	})
	resp1, err := client.HTTPClient.Do(req1)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp1.StatusCode)
	resp1.Body.Close()

	// Second new session with different session_id should be rejected.
	req2 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": "c01b-sid-2",
	})
	resp2, err := client.HTTPClient.Do(req2)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, resp2.StatusCode)
	resp2.Body.Close()

	count := getUserSessionSetSize(t, suite, userID)
	require.Equal(t, int64(1), count, "user session set size should be 1 when limit is 1")
}

// TestConcurrency_C02_ReuseExistingSessionDoesNotCount
// C-02: 复用已有会话不计入并发
func TestConcurrency_C02_ReuseExistingSessionDoesNotCount(t *testing.T) {
	suite, cleanup := setupConcurrencySuite(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	setSystemMaxConcurrentSessions(t, suite, 0)
	setGroupMaxConcurrentSessions(t, suite, map[string]int{
		"default": 1,
	})

	userID, token, _ := createUserTokenAndChannel(t, suite, "c02-user", "default")
	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)

	sessionID := "c02-sid-1"

	// First request creates the session.
	req1 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp1, err := client.HTTPClient.Do(req1)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp1.StatusCode)
	resp1.Body.Close()

	// Second request with the same session_id should also succeed and should
	// not be counted as a new concurrent session.
	req2 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp2, err := client.HTTPClient.Do(req2)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	resp2.Body.Close()

	count := getUserSessionSetSize(t, suite, userID)
	require.Equal(t, int64(1), count, "reusing existing session should not increase concurrent session count")
}

// TestConcurrency_C03_SessionsRemovalRestoresCapacity
// C-03: 会话结束后并发数恢复
//
// 说明：原设计依赖 Redis key 过期事件和定时 reaper 来在 TTL 到期后回收计数。
// 在测试环境中我们通过直接从 session:user:{id} 集合中移除一个 session_id
// 来模拟会话结束，从而验证并发额度恢复的行为。
func TestConcurrency_C03_SessionsRemovalRestoresCapacity(t *testing.T) {
	suite, cleanup := setupConcurrencySuite(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	setSystemMaxConcurrentSessions(t, suite, 0)
	setGroupMaxConcurrentSessions(t, suite, map[string]int{
		"default": 2,
	})

	userID, token, _ := createUserTokenAndChannel(t, suite, "c03-user", "default")
	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)

	// Create two concurrent sessions up to the limit.
	for _, sid := range []string{"c03-sid-1", "c03-sid-2"} {
		req := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
			"X-NewAPI-Session-ID": sid,
		})
		resp, err := client.HTTPClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()
	}

	require.Equal(t, int64(2), getUserSessionSetSize(t, suite, userID))

	// Simulate one session ending by removing it from the session:user set.
	userKey := fmt.Sprintf("session:user:%d", userID)
	err := suite.RedisClient.SRem(ctx, userKey, "c03-sid-1").Err()
	require.NoError(t, err, "failed to SREM session from user set")

	require.Equal(t, int64(1), getUserSessionSetSize(t, suite, userID))

	// Now a new session should be accepted again.
	req3 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": "c03-sid-3",
	})
	resp3, err := client.HTTPClient.Do(req3)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp3.StatusCode)
	resp3.Body.Close()

	require.Equal(t, int64(2), getUserSessionSetSize(t, suite, userID))
}

// sessionSummary mirrors the JSON structure returned by
// GET /api/admin/sessions/summary for the parts we care about in tests.
type sessionSummary struct {
	TotalActiveSessions int64             `json:"total_active_sessions"`
	SessionsByChannel   map[string]int64  `json:"sessions_by_channel"`
	TopUsersBySession   []userSessionInfo `json:"top_users_by_session"`
}

type userSessionInfo struct {
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	SessionCount int64  `json:"session_count"`
}

// TestConcurrency_C04_MonitoringAPIAccuracy
// C-04: 监控API数据准确性
func TestConcurrency_C04_MonitoringAPIAccuracy(t *testing.T) {
	suite, cleanup := setupConcurrencySuite(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	setSystemMaxConcurrentSessions(t, suite, 0)
	setGroupMaxConcurrentSessions(t, suite, map[string]int{
		"default": 5,
	})

	// Create a shared channel first so both users use the same channel.
	sharedChannel, err := suite.Fixtures.CreateTestChannel(
		"c04-shared-channel",
		"gpt-4",
		"default",
		suite.Fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	require.NoError(t, err, "failed to create shared channel")

	// Helper to create user + token but reuse shared channel.
	createUserAndToken := func(username string) (int, string) {
		user, err := suite.Fixtures.CreateTestUser(username, "testpass123", "default")
		require.NoError(t, err, "failed to create user %s", username)

		userClient := suite.AdminClient.Clone()
		_, err = userClient.Login(username, "testpass123")
		require.NoError(t, err, "failed to login user %s", username)

		tokenKey, err := suite.Fixtures.CreateTestAPIToken(username+"-token", userClient, nil)
		require.NoError(t, err, "failed to create token for %s", username)

		return user.ID, tokenKey
	}

	user1ID, token1 := createUserAndToken("c04-user1")
	user2ID, token2 := createUserAndToken("c04-user2")

	_ = sharedChannel // channel is referenced via group + model in routing

	client1 := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token1)
	client2 := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token2)

	// User1: create two sessions.
	for _, sid := range []string{"c04-u1-sid-1", "c04-u1-sid-2"} {
		req := buildChatRequest(t, client1.BaseURL, token1, "gpt-4", map[string]string{
			"X-NewAPI-Session-ID": sid,
		})
		resp, err := client1.HTTPClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()
	}

	// User2: create one session.
	req := buildChatRequest(t, client2.BaseURL, token2, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": "c04-u2-sid-1",
	})
	resp, err := client2.HTTPClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Call monitoring API.
	var monitorResp struct {
		Success bool           `json:"success"`
		Message string         `json:"message"`
		Data    sessionSummary `json:"data"`
	}
	err = suite.AdminClient.GetJSON("/api/admin/sessions/summary?top_users_limit=10&recent_sessions_limit=10", &monitorResp)
	require.NoError(t, err, "failed to call sessions summary API")
	require.True(t, monitorResp.Success, "sessions summary API should succeed")

	summary := monitorResp.Data

	require.Equal(t, int64(3), summary.TotalActiveSessions, "total_active_sessions should equal sum of all active sessions")

	// SessionsByChannel: there should be exactly one entry for the shared channel with count 3.
	require.Len(t, summary.SessionsByChannel, 1, "should have sessions aggregated under one shared channel")
	var channelSessionCount int64
	for _, c := range summary.SessionsByChannel {
		channelSessionCount = c
	}
	require.Equal(t, int64(3), channelSessionCount, "shared channel should report 3 active sessions")

	// TopUsersBySession: user1 (2 sessions) should rank before user2 (1 session).
	require.GreaterOrEqual(t, len(summary.TopUsersBySession), 2, "should list at least two users in top_users_by_session")
	user1Info := summary.TopUsersBySession[0]
	user2Info := summary.TopUsersBySession[1]

	// Map user IDs to their reported positions.
	if user1Info.UserId == user2ID {
		// swap if ordering is reversed; we only require counts to match.
		user1Info, user2Info = user2Info, user1Info
	}

	require.Equal(t, user1ID, user1Info.UserId, "user1 should appear in top users")
	require.Equal(t, int64(2), user1Info.SessionCount, "user1 should have 2 sessions")
	require.Equal(t, user2ID, user2Info.UserId, "user2 should appear in top users")
	require.Equal(t, int64(1), user2Info.SessionCount, "user2 should have 1 session")
}

// TestConcurrency_DC03_ConcurrentNewSessionsRace
// DC-03: 并发创建会话
//
// 场景：
//  1. 将用户所在分组的并发会话上限设置为 1。
//  2. 几乎同时发送两个不同 session_id 的新会话请求（sid=1, sid=2）。
//  3. 期望只有一个请求成功创建会话并返回 200，另一个因并发竞争失败返回 429；
//     最终 Redis 中 session:user:{id} 的基数为 1。
func TestConcurrency_DC03_ConcurrentNewSessionsRace(t *testing.T) {
	suite, cleanup := setupConcurrencySuite(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	setSystemMaxConcurrentSessions(t, suite, 0)
	setGroupMaxConcurrentSessions(t, suite, map[string]int{
		"default": 1,
	})

	userID, token, _ := createUserTokenAndChannel(t, suite, "dc03-user", "default")
	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)

	sessionIDs := []string{"dc03-sid-1", "dc03-sid-2"}

	type result struct {
		status int
		err    error
	}

	resultsCh := make(chan result, len(sessionIDs))
	startCh := make(chan struct{})

	for _, sid := range sessionIDs {
		sidCopy := sid
		go func() {
			<-startCh
			req := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
				"X-NewAPI-Session-ID": sidCopy,
			})
			resp, err := client.HTTPClient.Do(req)
			if err != nil {
				resultsCh <- result{status: 0, err: err}
				return
			}
			status := resp.StatusCode
			resp.Body.Close()
			resultsCh <- result{status: status, err: nil}
		}()
	}

	// Release both goroutines at (almost) the same time.
	close(startCh)

	statuses := make([]int, 0, len(sessionIDs))
	for range sessionIDs {
		r := <-resultsCh
		require.NoError(t, r.err)
		statuses = append(statuses, r.status)
	}

	var okCount, rejectedCount int
	for _, s := range statuses {
		if s == http.StatusOK {
			okCount++
		}
		if s == http.StatusTooManyRequests {
			rejectedCount++
		}
	}

	require.Equal(t, 1, okCount, "exactly one concurrent session creation should succeed when limit is 1")
	require.Equal(t, 1, rejectedCount, "exactly one concurrent session creation should be rejected when limit is 1")

	count := getUserSessionSetSize(t, suite, userID)
	require.Equal(t, int64(1), count, "user session set size should remain 1 after concurrent creation attempts")
}

// TestConcurrency_DC04_MultiTokenSharedLimit
// DC-04: 多 Token 叠加并发限制
//
// 场景：
//  1. 同一用户下创建两个不同的 API Token（可以绑定到相同分组/渠道）。
//  2. 将该用户所在分组的并发会话上限设为 1。
//  3. 使用两个 Token 几乎同时创建两个不同 session_id 的新会话。
//  4. 预期只有一个请求返回 200，另一个返回 429，且最终 user 会话集合基数为 1，
//     验证并发限制按“用户维度”生效而非“Token 维度”。
func TestConcurrency_DC04_MultiTokenSharedLimit(t *testing.T) {
	suite, cleanup := setupConcurrencySuite(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	setSystemMaxConcurrentSessions(t, suite, 0)
	setGroupMaxConcurrentSessions(t, suite, map[string]int{
		"default": 1,
	})

	// 创建用户及其登录客户端
	user, err := suite.Fixtures.CreateTestUser("dc04-user", "testpass123", "default")
	require.NoError(t, err, "failed to create dc04 user")

	userClient := suite.AdminClient.Clone()
	_, err = userClient.Login("dc04-user", "testpass123")
	require.NoError(t, err, "failed to login dc04 user")

	// 为同一用户创建两个不同的 API Token
	token1, err := suite.Fixtures.CreateTestAPIToken("dc04-token-1", userClient, nil)
	require.NoError(t, err, "failed to create dc04 token1")

	token2, err := suite.Fixtures.CreateTestAPIToken("dc04-token-2", userClient, nil)
	require.NoError(t, err, "failed to create dc04 token2")

	// 创建一个共享渠道，所有请求都会通过该渠道路由
	_, err = suite.Fixtures.CreateTestChannel(
		"dc04-shared-channel",
		"gpt-4",
		"default",
		suite.Fixtures.GetUpstreamURL(),
		false,
		0,
		"",
	)
	require.NoError(t, err, "failed to create dc04 shared channel")

	client1 := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token1)
	client2 := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token2)

	sessionIDs := []struct {
		client *testutil.APIClient
		token  string
		sid    string
	}{
		{client: client1, token: token1, sid: "dc04-sid-1"},
		{client: client2, token: token2, sid: "dc04-sid-2"},
	}

	type result struct {
		status int
		err    error
	}

	resultsCh := make(chan result, len(sessionIDs))
	startCh := make(chan struct{})

	for _, item := range sessionIDs {
		it := item
		go func() {
			<-startCh
			req := buildChatRequest(t, it.client.BaseURL, it.token, "gpt-4", map[string]string{
				"X-NewAPI-Session-ID": it.sid,
			})
			resp, err := it.client.HTTPClient.Do(req)
			if err != nil {
				resultsCh <- result{status: 0, err: err}
				return
			}
			status := resp.StatusCode
			resp.Body.Close()
			resultsCh <- result{status: status, err: nil}
		}()
	}

	// 几乎同时释放两个请求
	close(startCh)

	statuses := make([]int, 0, len(sessionIDs))
	for range sessionIDs {
		r := <-resultsCh
		require.NoError(t, r.err)
		statuses = append(statuses, r.status)
	}

	var okCount, rejectedCount int
	for _, s := range statuses {
		if s == http.StatusOK {
			okCount++
		}
		if s == http.StatusTooManyRequests {
			rejectedCount++
		}
	}

	require.Equal(t, 1, okCount, "exactly one session across two tokens should be accepted when limit is 1")
	require.Equal(t, 1, rejectedCount, "exactly one session across two tokens should be rejected when limit is 1")

	count := getUserSessionSetSize(t, suite, user.ID)
	require.Equal(t, int64(1), count, "user session set size should remain 1 after multi-token concurrent creation attempts")
}

// TestConcurrency_Priority_UserLimitOverridesGroup
// 用户级 max_concurrent_sessions 与组级/系统级同时配置时的优先级：
//   - SystemMaxConcurrentSessions = 5
//   - GroupMaxConcurrentSessions["default"] = 2
//   - 用户自身 MaxConcurrentSessions = 1
//
// 预期：有效并发上限应为 1，第二个新会话被拒绝。
func TestConcurrency_Priority_UserLimitOverridesGroup(t *testing.T) {
	suite, cleanup := setupConcurrencySuite(t)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	// 系统级限制为 5，组级限制为 2。
	setSystemMaxConcurrentSessions(t, suite, 5)
	setGroupMaxConcurrentSessions(t, suite, map[string]int{
		"default": 2,
	})

	// 创建用户 + token + 渠道（group=default），随后将其用户级并发上限设为 1。
	userID, token, _ := createUserTokenAndChannel(t, suite, "priority-user", "default")
	setUserMaxConcurrentSessions(t, suite, userID, "priority-user", "default", "testpass123", 1)

	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)

	// 第一个新会话应成功。
	req1 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": "priority-sid-1",
	})
	resp1, err := client.HTTPClient.Do(req1)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp1.StatusCode)
	resp1.Body.Close()

	// 第二个新会话应因用户级 limit=1 而被拒绝（尽管组级=2、系统级=5）。
	req2 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": "priority-sid-2",
	})
	resp2, err := client.HTTPClient.Do(req2)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, resp2.StatusCode)
	resp2.Body.Close()

	// Redis 中仍应只保留 1 个活跃会话。
	count := getUserSessionSetSize(t, suite, userID)
	require.Equal(t, int64(1), count, "effective concurrent session limit should be 1 when user-level limit overrides group/system")
}
