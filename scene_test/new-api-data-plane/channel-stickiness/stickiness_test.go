// Package channel_stickiness contains end-to-end tests for session stickiness
// and lifecycle behaviour of the data plane.
//
// It implements the scenarios defined in
// docs/01-NewAPI数据面转发渠道粘性和限量问题-测试设计.md §2.1:
// - S-01: 首次请求成功绑定
// - S-02: 后续请求命中粘性
// - S-03: 渠道失败自动解绑与重路由
// - S-03-A: 粘性渠道超额后自动解绑
// - S-03-B: 粘性渠道被禁用后自动解绑
// - S-05: 会话ID提取优先级
// - S-06: 会话超时自动失效
//
// The tests start a dedicated NewAPI server binary and a dedicated Redis
// instance (miniredis) so that session bindings can be inspected directly.
package channel_stickiness

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

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// StickinessSuite holds shared resources for the stickiness tests.
type StickinessSuite struct {
	Server      *testutil.TestServer
	AdminClient *testutil.APIClient
	Fixtures    *testutil.TestFixtures

	// Upstreams for different channels so we can observe routing decisions.
	Upstream1 *testutil.MockUpstreamServer
	Upstream2 *testutil.MockUpstreamServer

	// Redis backing store for session bindings.
	RedisServer *miniredis.Miniredis
	RedisClient *redis.Client
}

// setupStickinessSuite starts Redis, the test server and basic fixtures.
// ttlSeconds controls SESSION_BINDING_TTL_SECONDS for this server instance.
func setupStickinessSuite(t *testing.T, ttlSeconds int) (*StickinessSuite, func()) {
	t.Helper()

	// Start dedicated in‑memory Redis for this suite.
	mr, err := miniredis.Run()
	require.NoError(t, err, "failed to start miniredis")

	redisURL := fmt.Sprintf("redis://%s/0", mr.Addr())

	// Prepare two mock upstream servers so we can tell which channel was used.
	up1 := testutil.NewMockUpstreamServer()
	up2 := testutil.NewMockUpstreamServer()

	projectRoot, err := testutil.FindProjectRoot()
	require.NoError(t, err, "failed to find project root")

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()
	if cfg.CustomEnv == nil {
		cfg.CustomEnv = make(map[string]string)
	}
	cfg.CustomEnv["REDIS_CONN_STRING"] = redisURL
	cfg.CustomEnv["SESSION_BINDING_TTL_SECONDS"] = fmt.Sprintf("%d", ttlSeconds)

	server, err := testutil.StartServer(cfg)
	if err != nil {
		up1.Close()
		up2.Close()
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

	// Redis client for inspecting bindings.
	opt, err := redis.ParseURL(redisURL)
	require.NoError(t, err, "failed to parse redis URL")
	rdb := redis.NewClient(opt)

	// Ensure we have at least one retry so that channel failures can trigger
	// re-routing within a single HTTP request (used by S-03 scenarios).
	var optResp testutil.APIResponse
	err = adminClient.PutJSON("/api/option", map[string]interface{}{
		"key":   "RetryTimes",
		"value": "1",
	}, &optResp)
	require.NoError(t, err, "failed to configure RetryTimes option")
	require.True(t, optResp.Success, "RetryTimes option update should succeed")

	suite := &StickinessSuite{
		Server:      server,
		AdminClient: adminClient,
		Fixtures:    fixtures,
		Upstream1:   up1,
		Upstream2:   up2,
		RedisServer: mr,
		RedisClient: rdb,
	}

	cleanup := func() {
		// Best‑effort cleanup; tests should not fail in cleanup.
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
		if suite.Upstream1 != nil {
			suite.Upstream1.Close()
		}
		if suite.Upstream2 != nil {
			suite.Upstream2.Close()
		}
		if suite.RedisServer != nil {
			suite.RedisServer.Close()
		}
	}

	return suite, cleanup
}

// buildChatRequest builds an HTTP request for /v1/chat/completions using a raw
// token string and optional extra headers (e.g. session id).
func buildChatRequest(t *testing.T, baseURL, token, model string, sessionIDHeader map[string]string) *http.Request {
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
	for k, v := range sessionIDHeader {
		req.Header.Set(k, v)
	}
	return req
}

// createUserAndToken creates a simple user in the given billing group and
// returns its API token key (sk-*) and user id.
func createUserAndToken(t *testing.T, suite *StickinessSuite, username, group string) (userID int, tokenKey string) {
	t.Helper()

	user, err := suite.Fixtures.CreateTestUser(username, "testpass123", group)
	require.NoError(t, err, "failed to create test user")

	// Login with this user to create its own token.
	userClient := suite.AdminClient.Clone()
	_, err = userClient.Login(username, "testpass123")
	require.NoError(t, err, "failed to login test user")

	tokenKey, err = suite.Fixtures.CreateTestAPIToken(username+"-token", userClient, nil)
	require.NoError(t, err, "failed to create API token")

	return user.ID, tokenKey
}

// createChannelsForStickiness creates two default-group channels pointing to
// different upstream servers.
func createChannelsForStickiness(t *testing.T, suite *StickinessSuite) (ch1ID, ch2ID int) {
	t.Helper()

	ch1, err := suite.Fixtures.CreateTestChannel(
		"stickiness-channel-1",
		"gpt-4",
		"default",
		suite.Upstream1.BaseURL,
		false,
		0,
		"",
	)
	require.NoError(t, err, "failed to create channel 1")

	ch2, err := suite.Fixtures.CreateTestChannel(
		"stickiness-channel-2",
		"gpt-4",
		"default",
		suite.Upstream2.BaseURL,
		false,
		0,
		"",
	)
	require.NoError(t, err, "failed to create channel 2")

	return ch1.ID, ch2.ID
}

// getSessionBindingChannelID reads the bound channel_id from Redis.
func getSessionBindingChannelID(t *testing.T, suite *StickinessSuite, userID int, model, sessionID string) (int, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("session:%d:%s:%s", userID, model, sessionID)
	val, err := suite.RedisClient.HGet(ctx, key, "channel_id").Result()
	if err == redis.Nil {
		return 0, false
	}
	require.NoError(t, err, "failed to read session binding from redis")
	id, err := strconv.Atoi(val)
	require.NoError(t, err, "invalid channel_id in session binding")
	return id, true
}

// getSessionTTLSeconds returns TTL in seconds for a given binding key.
func getSessionTTLSeconds(t *testing.T, suite *StickinessSuite, userID int, model, sessionID string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := fmt.Sprintf("session:%d:%s:%s", userID, model, sessionID)
	ttl, err := suite.RedisClient.TTL(ctx, key).Result()
	require.NoError(t, err, "failed to read session TTL from redis")
	if ttl < 0 {
		return int64(ttl)
	}
	return int64(ttl.Seconds())
}

// --- Test Cases ---

// TestStickiness_S01_FirstRequestShouldCreateBinding
// S-01: 首次请求成功绑定
func TestStickiness_S01_FirstRequestShouldCreateBinding(t *testing.T) {
	suite, cleanup := setupStickinessSuite(t, 60)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	userID, token := createUserAndToken(t, suite, "s01-user", "default")
	ch1ID, ch2ID := createChannelsForStickiness(t, suite)

	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)
	sessionID := "s01-session-123"

	req := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp, err := client.HTTPClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	channelID, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist in Redis")
	if channelID != ch1ID && channelID != ch2ID {
		t.Fatalf("session bound to unexpected channel: got %d, want %d or %d", channelID, ch1ID, ch2ID)
	}

	ttl := getSessionTTLSeconds(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ttl > 0, "session binding TTL should be positive")
}

// TestStickiness_S02_SubsequentRequestsHitSameChannel
// S-02: 后续请求命中粘性
func TestStickiness_S02_SubsequentRequestsHitSameChannel(t *testing.T) {
	suite, cleanup := setupStickinessSuite(t, 60)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	userID, token := createUserAndToken(t, suite, "s02-user", "default")
	ch1ID, ch2ID := createChannelsForStickiness(t, suite)

	// Reset upstream stats before the test.
	suite.Upstream1.Reset()
	suite.Upstream2.Reset()

	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)
	sessionID := "s02-session-abc"

	// First request: should create binding.
	req1 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp1, err := client.HTTPClient.Do(req1)
	require.NoError(t, err)
	defer resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	channelID1, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after first request")
	require.True(t, channelID1 == ch1ID || channelID1 == ch2ID, "binding channel must be one of the test channels")

	ttl1 := getSessionTTLSeconds(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ttl1 > 0, "TTL after first request should be positive")

	reqCount1Ch1 := suite.Upstream1.GetRequestCount()
	reqCount1Ch2 := suite.Upstream2.GetRequestCount()
	require.Equal(t, 1, reqCount1Ch1+reqCount1Ch2, "exactly one upstream should receive the first request")

	// Second request: should hit same channel via sticky binding.
	req2 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp2, err := client.HTTPClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	channelID2, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should still exist after second request")
	require.Equal(t, channelID1, channelID2, "second request should stay on the same channel")

	ttl2 := getSessionTTLSeconds(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ttl2 > 0, "TTL after second request should be positive")
	// TTL should be refreshed (or at least not decrease significantly).
	require.GreaterOrEqual(t, ttl2, ttl1-1, "TTL should be refreshed on subsequent access")

	reqCount2Ch1 := suite.Upstream1.GetRequestCount()
	reqCount2Ch2 := suite.Upstream2.GetRequestCount()
	if channelID1 == ch1ID {
		require.Equal(t, 2, reqCount2Ch1, "all requests for this session should go to upstream1")
		require.Equal(t, 0, reqCount2Ch2, "upstream2 should not receive sticky session traffic")
	} else {
		require.Equal(t, 0, reqCount2Ch1, "upstream1 should not receive sticky session traffic")
		require.Equal(t, 2, reqCount2Ch2, "all requests for this session should go to upstream2")
	}
}

// TestStickiness_S03_ChannelFailureUnbindsAndReroutes
// S-03: 渠道失败自动解绑与重路由
//
// This test verifies the end-to-end behaviour when the bound channel starts
// returning upstream errors:
//  1. First request creates a sticky binding to channel C1.
//  2. We configure C1's upstream to return 503 on every call.
//  3. Second request with the same session id:
//     - First attempt hits C1 and fails.
//     - Relay removes the existing binding and retries.
//     - Retry uses the remaining healthy channel C2 (thanks to the exclusion
//     set built from use_channel) and succeeds.
//     - A new sticky binding is created for C2 in the same HTTP request.
//  4. Subsequent requests continue to use the new healthy channel.
func TestStickiness_S03_ChannelFailureUnbindsAndReroutes(t *testing.T) {
	suite, cleanup := setupStickinessSuite(t, 60)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	userID, token := createUserAndToken(t, suite, "s03-user", "default")
	ch1ID, ch2ID := createChannelsForStickiness(t, suite)

	suite.Upstream1.Reset()
	suite.Upstream2.Reset()

	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)
	sessionID := "s03-session-fail"

	// First request binds the session to one of the channels.
	req1 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp1, err := client.HTTPClient.Do(req1)
	require.NoError(t, err)
	defer resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	channelID1, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after first request")

	// Determine which upstream the session is currently bound to.
	reqCount1Ch1 := suite.Upstream1.GetRequestCount()
	reqCount1Ch2 := suite.Upstream2.GetRequestCount()
	require.Equal(t, 1, reqCount1Ch1+reqCount1Ch2, "exactly one upstream should receive the first request")

	var failingUpstream, healthyUpstream *testutil.MockUpstreamServer
	var healthyChannelID int
	var failCountBefore, healthyCountBefore int
	if channelID1 == ch1ID {
		failingUpstream, healthyUpstream = suite.Upstream1, suite.Upstream2
		healthyChannelID = ch2ID
		failCountBefore = reqCount1Ch1
		healthyCountBefore = reqCount1Ch2
	} else {
		failingUpstream, healthyUpstream = suite.Upstream2, suite.Upstream1
		healthyChannelID = ch1ID
		failCountBefore = reqCount1Ch2
		healthyCountBefore = reqCount1Ch1
	}

	// Configure the currently bound upstream to return 503 errors.
	failingUpstream.SetError(http.StatusServiceUnavailable, "upstream_error", "simulated failure for S-03")

	// Second request with the same session should trigger:
	//   - A first attempt to the failing channel.
	//   - Removal of the old binding.
	//   - Retry on the healthy channel (skipping the failing one).
	//   - Creation of a new binding for the healthy channel if the retry
	//     succeeds.
	req2 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp2, err := client.HTTPClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode, "second request should succeed after retry on healthy channel")

	// Verify that both upstreams have been hit: the failing one for the first
	// attempt, and the healthy one for the retry.
	failCountAfter := failingUpstream.GetRequestCount()
	healthyCountAfter := healthyUpstream.GetRequestCount()
	require.GreaterOrEqual(t, failCountAfter, failCountBefore+1, "failing upstream should receive at least one request on second call")
	require.GreaterOrEqual(t, healthyCountAfter, healthyCountBefore+1, "healthy upstream should receive at least one retry request")

	// After the retry succeeds, a new binding should exist and point to the
	// healthy channel instead of the failing one.
	channelID2, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after successful retry")
	require.Equal(t, healthyChannelID, channelID2, "session should be rebound to the healthy channel after failure")

	// Reset upstream errors so that subsequent requests can proceed normally
	// using the new binding.
	failingUpstream.Reset()
	healthyUpstream.Reset()

	req3 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp3, err := client.HTTPClient.Do(req3)
	require.NoError(t, err)
	defer resp3.Body.Close()
	require.Equal(t, http.StatusOK, resp3.StatusCode)

	channelID3, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should still exist after subsequent request")
	require.Equal(t, healthyChannelID, channelID3, "session should remain bound to the healthy channel")
}

// TestStickiness_S03A_QuotaExhaustionUnbinds
// S-03-A: 粘性渠道超额后自动解绑
func TestStickiness_S03A_QuotaExhaustionUnbinds(t *testing.T) {
	suite, cleanup := setupStickinessSuite(t, 60)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	userID, token := createUserAndToken(t, suite, "s03a-user", "default")
	ch1ID, ch2ID := createChannelsForStickiness(t, suite)

	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)
	sessionID := "s03a-session-quota"

	// First request to create sticky binding.
	req1 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp1, err := client.HTTPClient.Do(req1)
	require.NoError(t, err)
	defer resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	channelID1, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after first request")
	require.True(t, channelID1 == ch1ID || channelID1 == ch2ID, "binding channel must be one of the test channels")

	// Configure the bound channel to have a small hourly quota limit,
	// then directly mark its Redis hourly quota counter as exhausted.
	channels, err := suite.AdminClient.GetAllChannels()
	require.NoError(t, err, "failed to list channels")

	var target testutil.ChannelModel
	found := false
	for _, ch := range channels {
		if ch.ID == channelID1 {
			target = ch
			found = true
			break
		}
	}
	require.True(t, found, "bound channel should exist in admin list")

	const hourlyLimit int64 = 1000
	target.HourlyQuotaLimit = hourlyLimit

	var resp testutil.APIResponse
	err = suite.AdminClient.PutJSON("/api/channel/", &target, &resp)
	require.NoError(t, err, "failed to update channel hourly_quota_limit via admin API")
	require.True(t, resp.Success, "update channel API should succeed")

	// Simulate that other requests have already consumed the full hourly quota
	// by directly setting the Redis channel_quota counter for the current hour.
	bucket := time.Now().Format("2006010215")
	quotaKey := fmt.Sprintf("channel_quota:%d:hourly:%s", channelID1, bucket)
	err = suite.RedisClient.Set(ctx, quotaKey, strconv.FormatInt(hourlyLimit, 10), 0).Err()
	require.NoError(t, err, "failed to set simulated hourly quota in Redis")

	// Second request with the same session should observe the quota exhaustion
	// during validateSessionBinding (via CheckChannelRiskControl) and therefore:
	//   - drop the old binding to the exhausted channel,
	//   - reselect a healthy channel (the other one),
	//   - create a new binding pointing to the healthy channel.
	req2 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp2, err := client.HTTPClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	channelID2, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after rebinding due to quota exhaustion")
	require.NotEqual(t, channelID1, channelID2, "session should be rebound to a different channel after quota exhaustion")
	require.True(t, channelID2 == ch1ID || channelID2 == ch2ID, "rebinding should select one of the existing channels")
}

// TestStickiness_S03B_DisabledChannelUnbinds
// S-03-B: 粘性渠道被禁用后自动解绑
func TestStickiness_S03B_DisabledChannelUnbinds(t *testing.T) {
	suite, cleanup := setupStickinessSuite(t, 60)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	userID, token := createUserAndToken(t, suite, "s03b-user", "default")
	ch1ID, ch2ID := createChannelsForStickiness(t, suite)

	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)
	sessionID := "s03b-session-disable"

	// First request to create binding.
	req1 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp1, err := client.HTTPClient.Do(req1)
	require.NoError(t, err)
	defer resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	channelID1, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after first request")

	// Disable the currently bound channel via admin API.
	channels, err := suite.AdminClient.GetAllChannels()
	require.NoError(t, err, "failed to list channels")

	var target testutil.ChannelModel
	found := false
	for _, ch := range channels {
		if ch.ID == channelID1 {
			target = ch
			found = true
			break
		}
	}
	require.True(t, found, "bound channel should exist in admin list")

	target.Status = common.ChannelStatusManuallyDisabled
	var resp testutil.APIResponse
	err = suite.AdminClient.PutJSON("/api/channel/", &target, &resp)
	require.NoError(t, err, "failed to disable channel via admin API")
	require.True(t, resp.Success, "disable channel API should succeed")

	// Give session cleanup hook a moment to run.
	time.Sleep(100 * time.Millisecond)

	// Second request with the same session should rebind to another channel.
	req2 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp2, err := client.HTTPClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	channelID2, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after rebinding")
	require.NotEqual(t, channelID1, channelID2, "session should be rebound to a different channel after disable")
	require.True(t, channelID2 == ch1ID || channelID2 == ch2ID, "rebinding should select one of the existing channels")
}

// TestStickiness_S04_StaleBindingIsCleanedUp
// S-04: 粘性渠道失效后恢复（Redis 绑定被手工篡改为失效 channel_id）
func TestStickiness_S04_StaleBindingIsCleanedUp(t *testing.T) {
	suite, cleanup := setupStickinessSuite(t, 60)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	userID, token := createUserAndToken(t, suite, "s04-user", "default")
	ch1ID, ch2ID := createChannelsForStickiness(t, suite)

	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)
	sessionID := "s04-session-stale"

	// First request to create a valid binding.
	req1 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp1, err := client.HTTPClient.Do(req1)
	require.NoError(t, err)
	defer resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	channelID1, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after first request")
	require.True(t, channelID1 == ch1ID || channelID1 == ch2ID, "binding channel must be one of the test channels")

	// Manually corrupt the binding to point to a non-existent channel.
	key := fmt.Sprintf("session:%d:%s:%s", userID, "gpt-4", sessionID)
	err = suite.RedisClient.HSet(ctx, key, "channel_id", "999999").Err()
	require.NoError(t, err, "failed to corrupt session binding")

	// Second request should detect the invalid binding, remove it, and create
	// a new binding to a healthy channel.
	req2 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp2, err := client.HTTPClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	channelID2, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after recovery request")
	require.NotEqual(t, 999999, channelID2, "binding should no longer point to invalid channel")
	require.True(t, channelID2 == ch1ID || channelID2 == ch2ID, "binding should point to a real channel")
}

// TestStickiness_S05_SessionIDExtractionPriority
// S-05: 会话ID提取优先级 Header > Query > Body
func TestStickiness_S05_SessionIDExtractionPriority(t *testing.T) {
	suite, cleanup := setupStickinessSuite(t, 60)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	userID, token := createUserAndToken(t, suite, "s05-user", "default")
	_, _ = createChannelsForStickiness(t, suite)

	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)

	// Case 1: Query vs Body (no header) -> should use Query session_id.
	{
		sessionQuery := "s05-query"
		sessionBody := "s05-body"

		body := map[string]interface{}{
			"model":      "gpt-4",
			"session_id": sessionBody,
			"messages": []testutil.ChatMessage{
				{Role: "user", Content: "hello"},
			},
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)

		url := fmt.Sprintf("%s/v1/chat/completions?session_id=%s", client.BaseURL, sessionQuery)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.HTTPClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		// Binding should exist for query session id.
		_, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionQuery)
		require.True(t, ok, "binding should use query session_id")

		// No binding for body session id.
		_, ok = getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionBody)
		require.False(t, ok, "binding should not be created for body session_id when query is present")

		require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())
	}

	// Case 2: Header vs Query vs Body -> should use Header.
	{
		sessionHeader := "s05-header"
		sessionQuery := "s05-query2"
		sessionBody := "s05-body2"

		body := map[string]interface{}{
			"model":      "gpt-4",
			"session_id": sessionBody,
			"messages": []testutil.ChatMessage{
				{Role: "user", Content: "hello again"},
			},
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)

		url := fmt.Sprintf("%s/v1/chat/completions?session_id=%s", client.BaseURL, sessionQuery)
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-NewAPI-Session-ID", sessionHeader)

		resp, err := client.HTTPClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		// Binding should exist for header session id.
		_, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionHeader)
		require.True(t, ok, "binding should use header session_id")

		// No binding for query or body ids.
		_, ok = getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionQuery)
		require.False(t, ok, "binding should not be created for query session_id when header is present")
		_, ok = getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionBody)
		require.False(t, ok, "binding should not be created for body session_id when header is present")
	}
}

// TestStickiness_S06_SessionTimeoutExpiresBinding
// S-06: 会话超时自动失效
func TestStickiness_S06_SessionTimeoutExpiresBinding(t *testing.T) {
	// Use a short TTL so the test can observe expiration.
	const ttlSeconds = 2
	suite, cleanup := setupStickinessSuite(t, ttlSeconds)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, suite.RedisClient.FlushDB(ctx).Err())

	userID, token := createUserAndToken(t, suite, "s06-user", "default")
	_, _ = createChannelsForStickiness(t, suite)

	client := testutil.NewAPIClientWithToken(suite.Server.BaseURL, token)
	sessionID := "s06-session-expire"

	// First request creates the binding.
	req1 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp1, err := client.HTTPClient.Do(req1)
	require.NoError(t, err)
	defer resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	_, ok := getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "session binding should exist after first request")

	// Advance the miniredis clock so that the key expires.
	suite.RedisServer.FastForward(time.Duration(ttlSeconds+1) * time.Second)

	// Verify the binding key has expired.
	key := fmt.Sprintf("session:%d:%s:%s", userID, "gpt-4", sessionID)
	exists, err := suite.RedisClient.Exists(ctx, key).Result()
	require.NoError(t, err)
	require.Equal(t, int64(0), exists, "session binding should expire after TTL")

	// Second request with the same session id should behave like a new session:
	// a new binding should be created.
	req2 := buildChatRequest(t, client.BaseURL, token, "gpt-4", map[string]string{
		"X-NewAPI-Session-ID": sessionID,
	})
	resp2, err := client.HTTPClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	_, ok = getSessionBindingChannelID(t, suite, userID, "gpt-4", sessionID)
	require.True(t, ok, "a new session binding should be created after expiration")
}
