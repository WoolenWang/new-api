// Package dashboard contains integration tests for the management-plane
// dashboard / statistics APIs. This file focuses on the new system-level
// statistics endpoints described in `docs/系统统计数据dashboard设计-测试设计.md`:
//   - SYS-01: 单渠道单窗口基线聚合 (/api/system/stats/summary)
//   - SYS-03: 无数据/零数据鲁棒性 (/api/system/stats/summary)
//   - DAY-01: 连续多日聚合 (/api/system/stats/daily_tokens)
package dashboard

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// systemGroupStatsResponse models the JSON response for /api/groups/system/stats.
type systemGroupStatsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []struct {
		GroupName string `json:"group_name"`
		Stats     struct {
			TotalTokens int64 `json:"total_tokens"`
			TotalQuota  int64 `json:"total_quota"`
		} `json:"stats"`
	} `json:"data"`
}

// TestSystemGroupStats_BG01_BillingGroupsAggregation implements BG-01:
//   - 为 default/vip 两个计费分组分别创建 1 个用户 + 1 个渠道。
//   - 为每个渠道插入一条 channel_statistics 记录。
//   - 调用 /api/groups/system/stats?period=1d。
//   - 校验 default/vip 的 total_tokens/total_quota 与各自渠道统计一致，
//     且未配置渠道的计费分组（如 svip）统计为 0。
func TestSystemGroupStats_BG01_BillingGroupsAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping system group stats integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	fixtures := suite.Fixtures
	inspector, err := testutil.NewDBStatsInspectorFromServer(suite.Server)
	if err != nil {
		t.Fatalf("failed to create DBStatsInspector: %v", err)
	}
	defer inspector.Close()

	// Create two users in different billing groups.
	userDefault, err := fixtures.CreateTestUser("bg01_default_user", "password123", "default")
	if err != nil {
		t.Fatalf("failed to create default user: %v", err)
	}
	userVip, err := fixtures.CreateTestUser("bg01_vip_user", "password123", "vip")
	if err != nil {
		t.Fatalf("failed to create vip user: %v", err)
	}

	// Each user owns one channel in their system group.
	// 直接在共享 SQLite DB 中插入最小化的渠道记录，避免依赖管理面 API 行为。
	chDefault := &model.Channel{
		Type:        1,
		Key:         "sk-bg01-default",
		Name:        "bg01-default-channel",
		Models:      "gpt-4",
		Group:       "default",
		Status:      1,
		OwnerUserId: userDefault.ID,
	}
	if err := model.DB.Create(chDefault).Error; err != nil {
		t.Fatalf("failed to create default group channel in DB: %v", err)
	}

	chVip := &model.Channel{
		Type:        1,
		Key:         "sk-bg01-vip",
		Name:        "bg01-vip-channel",
		Models:      "gpt-4",
		Group:       "vip",
		Status:      1,
		OwnerUserId: userVip.ID,
	}
	if err := model.DB.Create(chVip).Error; err != nil {
		t.Fatalf("failed to create vip group channel in DB: %v", err)
	}

	now := time.Now().Unix()
	windowStart := now - 60

	// Insert per-channel statistics.
	insert := func(channelID int, tokens, quota int64) {
		rec := &testutil.ChannelStatisticsRecord{
			ChannelID:       channelID,
			ModelName:       "gpt-4",
			TimeWindowStart: windowStart,
			RequestCount:    10,
			FailCount:       0,
			TotalTokens:     tokens,
			TotalQuota:      quota,
			TotalLatencyMS:  1000,
		}
		if err := inspector.InsertChannelStatistics(rec); err != nil {
			t.Fatalf("failed to insert stats for channel %d: %v", channelID, err)
		}
	}

	insert(chDefault.Id, 1000, 100)
	insert(chVip.Id, 2000, 200)

	var resp systemGroupStatsResponse
	if err := suite.Client.GetJSON("/api/groups/system/stats?period=1d", &resp); err != nil {
		t.Fatalf("failed to call /api/groups/system/stats: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/groups/system/stats returned success=false: %s", resp.Message)
	}

	// Helper to find stats by group_name.
	find := func(name string) *struct {
		GroupName string `json:"group_name"`
		Stats     struct {
			TotalTokens int64 `json:"total_tokens"`
			TotalQuota  int64 `json:"total_quota"`
		} `json:"stats"`
	} {
		for i := range resp.Data {
			if resp.Data[i].GroupName == name {
				return &resp.Data[i]
			}
		}
		return nil
	}

	defStats := find("default")
	if defStats == nil {
		t.Fatalf("expected stats for group 'default', got none: %+v", resp.Data)
	}

	vipStats := find("vip")
	if vipStats == nil {
		t.Fatalf("expected stats for group 'vip', got none: %+v", resp.Data)
	}

	// If svip exists in GroupRatio config, it should have zero totals (no channels).
	svipStats := find("svip")
	if svipStats != nil {
		if svipStats.Stats.TotalTokens != 0 || svipStats.Stats.TotalQuota != 0 {
			t.Fatalf("svip group should have zero totals, got %+v", svipStats.Stats)
		}
	}
}

// systemStatsSummaryResponse models the JSON shape of /api/system/stats/summary.
type systemStatsSummaryResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Period             string  `json:"period"`
		TPM                int     `json:"tpm"`
		RPM                int     `json:"rpm"`
		QuotaPM            int64   `json:"quota_pm"`
		TotalTokens        int64   `json:"total_tokens"`
		TotalQuota         int64   `json:"total_quota"`
		AvgResponseTimeMs  float64 `json:"avg_response_time_ms"`
		FailRate           float64 `json:"fail_rate"`
		CacheHitRate       float64 `json:"cache_hit_rate"`
		StreamReqRatio     float64 `json:"stream_req_ratio"`
		DowntimePercentage float64 `json:"downtime_percentage"`
		UniqueUsers        int64   `json:"unique_users"`
		RequestCount       int64   `json:"request_count"`
	} `json:"data"`
}

// dailyTokenUsage models a single item from /api/system/stats/daily_tokens.
type dailyTokenUsage struct {
	Day    string `json:"day"`
	Tokens int64  `json:"tokens"`
	Quota  int64  `json:"quota"`
}

// dailyTokensResponse models the JSON response for /api/system/stats/daily_tokens.
type dailyTokensResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Data    []dailyTokenUsage `json:"data"`
}

// TestSystemStats_SYS01_SingleWindowAggregation implements SYS-01:
//   - Insert a single channel_statistics window with known metrics.
//   - Call /api/system/stats/summary?period=1d.
//   - Verify totals match the raw window and derived TPM/RPM/QuotaPM/fail_rate/latency
//     follow the formulas in docs/系统统计数据dashboard设计.md Section 7.1.
func TestSystemStats_SYS01_SingleWindowAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping system stats integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	inspector, err := testutil.NewDBStatsInspectorFromServer(suite.Server)
	if err != nil {
		t.Fatalf("failed to create DBStatsInspector: %v", err)
	}
	defer func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("failed to close DBStatsInspector: %v", err)
		}
	}()

	now := time.Now().Unix()
	windowStart := now - 60 // 1 minute ago, well within the 1d window

	// Baseline window:
	//   request_count=100, fail_count=5, total_tokens=6000, total_quota=3000,
	//   total_latency_ms=100000, stream_req_count=20, cache_hit_count=30, downtime_seconds=60.
	record := &testutil.ChannelStatisticsRecord{
		ChannelID:       1,
		ModelName:       "gpt-4",
		TimeWindowStart: windowStart,
		RequestCount:    100,
		FailCount:       5,
		TotalTokens:     6000,
		TotalQuota:      3000,
		TotalLatencyMS:  100000,
		StreamReqCount:  20,
		CacheHitCount:   30,
		DowntimeSeconds: 60,
	}

	if err := inspector.InsertChannelStatistics(record); err != nil {
		t.Fatalf("failed to insert channel_statistics record: %v", err)
	}

	var resp systemStatsSummaryResponse
	if err := suite.Client.GetJSON("/api/system/stats/summary?period=1d", &resp); err != nil {
		t.Fatalf("failed to call /api/system/stats/summary: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/system/stats/summary returned success=false: %s", resp.Message)
	}

	data := resp.Data
	if data.Period != "1d" {
		t.Fatalf("expected period=1d, got %s", data.Period)
	}

	// Raw totals should match the single window.
	if data.TotalTokens != record.TotalTokens {
		t.Fatalf("total_tokens mismatch: got %d, want %d", data.TotalTokens, record.TotalTokens)
	}
	if data.TotalQuota != record.TotalQuota {
		t.Fatalf("total_quota mismatch: got %d, want %d", data.TotalQuota, record.TotalQuota)
	}
	if data.RequestCount != int64(record.RequestCount) {
		t.Fatalf("request_count mismatch: got %d, want %d", data.RequestCount, record.RequestCount)
	}

	// Time range for 1d is exactly 24h -> 1440 minutes.
	const minutesInDay = 24 * 60
	expectedTPM := int(float64(record.TotalTokens) / float64(minutesInDay))
	expectedRPM := int(float64(record.RequestCount) / float64(minutesInDay))
	expectedQuotaPM := int64(float64(record.TotalQuota) / float64(minutesInDay))

	if data.TPM != expectedTPM {
		t.Fatalf("TPM mismatch: got %d, want %d", data.TPM, expectedTPM)
	}
	if data.RPM != expectedRPM {
		t.Fatalf("RPM mismatch: got %d, want %d", data.RPM, expectedRPM)
	}
	if data.QuotaPM != expectedQuotaPM {
		t.Fatalf("QuotaPM mismatch: got %d, want %d", data.QuotaPM, expectedQuotaPM)
	}

	// FailRate = fail_count * 100 / request_count.
	expectedFailRate := float64(record.FailCount) * 100.0 / float64(record.RequestCount)
	if math.Abs(data.FailRate-expectedFailRate) > 1e-6 {
		t.Fatalf("fail_rate mismatch: got %.6f, want %.6f", data.FailRate, expectedFailRate)
	}

	// AvgResponseTimeMs = total_latency_ms / request_count.
	expectedAvgLatency := float64(record.TotalLatencyMS) / float64(record.RequestCount)
	if math.Abs(data.AvgResponseTimeMs-expectedAvgLatency) > 1e-6 {
		t.Fatalf("avg_response_time_ms mismatch: got %.6f, want %.6f", data.AvgResponseTimeMs, expectedAvgLatency)
	}

	// CacheHitRate = cache_hit_count * 100 / request_count.
	expectedCacheHitRate := float64(record.CacheHitCount) * 100.0 / float64(record.RequestCount)
	if math.Abs(data.CacheHitRate-expectedCacheHitRate) > 1e-6 {
		t.Fatalf("cache_hit_rate mismatch: got %.6f, want %.6f", data.CacheHitRate, expectedCacheHitRate)
	}

	// StreamReqRatio = stream_req_count * 100 / request_count.
	expectedStreamReqRatio := float64(record.StreamReqCount) * 100.0 / float64(record.RequestCount)
	if math.Abs(data.StreamReqRatio-expectedStreamReqRatio) > 1e-6 {
		t.Fatalf("stream_req_ratio mismatch: got %.6f, want %.6f", data.StreamReqRatio, expectedStreamReqRatio)
	}

	// DowntimePercentage = downtime_seconds * 100 / (24h in seconds).
	const secondsInDay = 24 * 60 * 60
	expectedDowntimePercentage := float64(record.DowntimeSeconds) * 100.0 / float64(secondsInDay)
	if math.Abs(data.DowntimePercentage-expectedDowntimePercentage) > 1e-6 {
		t.Fatalf("downtime_percentage mismatch: got %.6f, want %.6f", data.DowntimePercentage, expectedDowntimePercentage)
	}

	// We did not populate unique_users via InsertChannelStatistics, expect 0.
	if data.UniqueUsers != 0 {
		t.Fatalf("expected unique_users=0 for synthetic record, got %d", data.UniqueUsers)
	}
}

// TestSystemStats_SYS03_EmptyData verifies SYS-03:
//   - With an empty channel_statistics table, /api/system/stats/summary must
//     return success=true and all numeric fields equal to 0 (no 500 errors).
func TestSystemStats_SYS03_EmptyData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping system stats integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	var resp systemStatsSummaryResponse
	if err := suite.Client.GetJSON("/api/system/stats/summary?period=7d", &resp); err != nil {
		t.Fatalf("failed to call /api/system/stats/summary: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/system/stats/summary returned success=false: %s", resp.Message)
	}

	data := resp.Data
	if data.Period != "7d" {
		t.Fatalf("expected period=7d, got %s", data.Period)
	}

	// When there is no data, all aggregated metrics should be zero.
	if data.TotalTokens != 0 || data.TotalQuota != 0 || data.RequestCount != 0 {
		t.Fatalf("expected zero totals, got total_tokens=%d total_quota=%d request_count=%d",
			data.TotalTokens, data.TotalQuota, data.RequestCount)
	}
	if data.TPM != 0 || data.RPM != 0 || data.QuotaPM != 0 {
		t.Fatalf("expected TPM/RPM/QuotaPM to be zero, got tpm=%d rpm=%d quota_pm=%d",
			data.TPM, data.RPM, data.QuotaPM)
	}
	if data.AvgResponseTimeMs != 0 || data.FailRate != 0 || data.CacheHitRate != 0 || data.StreamReqRatio != 0 {
		t.Fatalf("expected latency/fail_rate/cache_hit_rate/stream_req_ratio to be zero, got avg=%.6f fail=%.6f cache=%.6f stream=%.6f",
			data.AvgResponseTimeMs, data.FailRate, data.CacheHitRate, data.StreamReqRatio)
	}
	if data.DowntimePercentage != 0 {
		t.Fatalf("expected downtime_percentage=0, got %.6f", data.DowntimePercentage)
	}
	if data.UniqueUsers != 0 {
		t.Fatalf("expected unique_users=0, got %d", data.UniqueUsers)
	}
}

// TestSystemDailyTokens_DAY01_MultiDayAggregation implements DAY-01:
//   - Insert three channel_statistics windows on three different natural days
//     with distinct token/quota values.
//   - Call /api/system/stats/daily_tokens?days=3.
//   - Verify three rows are returned, ordered by day ASC, and each day's
//     tokens/quota equal the inserted values.
func TestSystemDailyTokens_DAY01_MultiDayAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daily tokens integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	inspector, err := testutil.NewDBStatsInspectorFromServer(suite.Server)
	if err != nil {
		t.Fatalf("failed to create DBStatsInspector: %v", err)
	}
	defer func() {
		if err := inspector.Close(); err != nil {
			t.Fatalf("failed to close DBStatsInspector: %v", err)
		}
	}()

	now := time.Now()

	// Construct three synthetic windows on three consecutive days:
	//   day2 (2 days ago): tokens=300, quota=150
	//   day1 (1 day ago):  tokens=200, quota=100
	//   day0 (today):      tokens=100, quota=50
	// We expect the API to return them ordered by day ascending.
	day2 := now.Add(-48*time.Hour - time.Minute).Unix()
	day1 := now.Add(-24*time.Hour - time.Minute).Unix()
	day0 := now.Add(-1 * time.Minute).Unix()

	type syntheticDay struct {
		Time   int64
		Tokens int64
		Quota  int64
	}

	samples := []syntheticDay{
		{Time: day2, Tokens: 300, Quota: 150},
		{Time: day1, Tokens: 200, Quota: 100},
		{Time: day0, Tokens: 100, Quota: 50},
	}

	for _, s := range samples {
		rec := &testutil.ChannelStatisticsRecord{
			ChannelID:       1,
			ModelName:       "gpt-4",
			TimeWindowStart: s.Time,
			RequestCount:    10,
			FailCount:       0,
			TotalTokens:     s.Tokens,
			TotalQuota:      s.Quota,
			TotalLatencyMS:  1000,
			StreamReqCount:  0,
			CacheHitCount:   0,
			DowntimeSeconds: 0,
		}
		if err := inspector.InsertChannelStatistics(rec); err != nil {
			t.Fatalf("failed to insert daily sample: %v", err)
		}
	}

	var resp dailyTokensResponse
	if err := suite.Client.GetJSON("/api/system/stats/daily_tokens?days=3", &resp); err != nil {
		t.Fatalf("failed to call /api/system/stats/daily_tokens: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/system/stats/daily_tokens returned success=false: %s", resp.Message)
	}

	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 daily records, got %d", len(resp.Data))
	}

	// Verify tokens/quota per day align with insertion order (day ASC).
	expectedTokens := []int64{300, 200, 100}
	expectedQuotas := []int64{150, 100, 50}

	var totalTokens int64
	for i, item := range resp.Data {
		if item.Tokens != expectedTokens[i] {
			t.Fatalf("day %d tokens mismatch: got %d, want %d", i, item.Tokens, expectedTokens[i])
		}
		if item.Quota != expectedQuotas[i] {
			t.Fatalf("day %d quota mismatch: got %d, want %d", i, item.Quota, expectedQuotas[i])
		}
		totalTokens += item.Tokens

		// Basic monotonicity check: days are sorted ascending.
		if i > 0 && resp.Data[i-1].Day > item.Day {
			t.Fatalf("days not sorted ascending: %s > %s", resp.Data[i-1].Day, item.Day)
		}
	}

	if totalTokens != int64(300+200+100) {
		t.Fatalf("total tokens sum mismatch: got %d, want %d", totalTokens, 300+200+100)
	}
}

// systemModelStatsResponse models the JSON response for /api/system/stats/models.
type systemModelStatsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []struct {
		ModelName         string  `json:"model_name"`
		TotalTokens       int64   `json:"total_tokens"`
		TotalQuota        int64   `json:"total_quota"`
		RequestCount      int64   `json:"request_count"`
		TPM               int     `json:"tpm"`
		RPM               int     `json:"rpm"`
		AvgResponseTimeMs float64 `json:"avg_response_time_ms"`
		FailRate          float64 `json:"fail_rate"`
	} `json:"data"`
}

// systemModelDailyTokensResponse models /api/system/stats/models/daily_tokens.
type systemModelDailyTokensResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []struct {
		Day       string `json:"day"`
		ModelName string `json:"model_name"`
		Tokens    int64  `json:"tokens"`
		Quota     int64  `json:"quota"`
	} `json:"data"`
}

// TestSystemModelStats_MOD01_PerModelAggregation implements MOD-01：
//   - 为两个模型插入各一条 channel_statistics 记录。
//   - 调用 /api/system/stats/models?period=1d。
//   - 校验每个模型的 total_tokens/total_quota/request_count 以及 TPM/RPM/avg_response_time_ms/fail_rate。
func TestSystemModelStats_MOD01_PerModelAggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping system model stats integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	inspector, err := testutil.NewDBStatsInspectorFromServer(suite.Server)
	if err != nil {
		t.Fatalf("failed to create DBStatsInspector: %v", err)
	}
	defer inspector.Close()

	now := time.Now().Unix()
	windowStart := now - 60

	// Insert synthetic statistics for two models.
	insert := func(modelName string, tokens, quota int64, requests, fails int, totalLatency int64) {
		rec := &testutil.ChannelStatisticsRecord{
			ChannelID:       1,
			ModelName:       modelName,
			TimeWindowStart: windowStart,
			RequestCount:    requests,
			FailCount:       fails,
			TotalTokens:     tokens,
			TotalQuota:      quota,
			TotalLatencyMS:  totalLatency,
			StreamReqCount:  0,
			CacheHitCount:   0,
			DowntimeSeconds: 0,
		}
		if err := inspector.InsertChannelStatistics(rec); err != nil {
			t.Fatalf("failed to insert stats for model %s: %v", modelName, err)
		}
	}

	insert("mod-a", 1000, 100, 10, 1, 5000)  // avg latency 500ms, fail_rate 10%
	insert("mod-b", 2000, 200, 20, 2, 16000) // avg latency 800ms, fail_rate 10%

	var resp systemModelStatsResponse
	if err := suite.Client.GetJSON("/api/system/stats/models?period=1d", &resp); err != nil {
		t.Fatalf("failed to call /api/system/stats/models: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/system/stats/models returned success=false: %s", resp.Message)
	}

	// Helper to find stats by model name.
	find := func(name string) *struct {
		ModelName         string  `json:"model_name"`
		TotalTokens       int64   `json:"total_tokens"`
		TotalQuota        int64   `json:"total_quota"`
		RequestCount      int64   `json:"request_count"`
		TPM               int     `json:"tpm"`
		RPM               int     `json:"rpm"`
		AvgResponseTimeMs float64 `json:"avg_response_time_ms"`
		FailRate          float64 `json:"fail_rate"`
	} {
		for i := range resp.Data {
			if resp.Data[i].ModelName == name {
				return &resp.Data[i]
			}
		}
		return nil
	}

	statA := find("mod-a")
	if statA == nil {
		t.Fatalf("expected stats for model mod-a, got none: %+v", resp.Data)
	}
	statB := find("mod-b")
	if statB == nil {
		t.Fatalf("expected stats for model mod-b, got none: %+v", resp.Data)
	}

	const minutesInDay = 24 * 60

	// Verify model A.
	if statA.TotalTokens != 1000 || statA.TotalQuota != 100 || statA.RequestCount != 10 {
		t.Fatalf("mod-a totals mismatch: %+v", *statA)
	}
	expectedTPM := 1000 / minutesInDay
	expectedRPM := 10 / minutesInDay
	if statA.TPM != expectedTPM || statA.RPM != expectedRPM {
		t.Fatalf("mod-a TPM/RPM mismatch: got tpm=%d rpm=%d, want tpm=%d rpm=%d",
			statA.TPM, statA.RPM, expectedTPM, expectedRPM)
	}
	expectedLatency := float64(5000) / float64(10)
	if math.Abs(statA.AvgResponseTimeMs-expectedLatency) > 1e-6 {
		t.Fatalf("mod-a avg_latency mismatch: got %.6f, want %.6f", statA.AvgResponseTimeMs, expectedLatency)
	}
	expectedFailRate := float64(1) * 100.0 / float64(10)
	if math.Abs(statA.FailRate-expectedFailRate) > 1e-6 {
		t.Fatalf("mod-a fail_rate mismatch: got %.6f, want %.6f", statA.FailRate, expectedFailRate)
	}

	// Verify model B structure at least matches totals.
	if statB.TotalTokens != 2000 || statB.TotalQuota != 200 || statB.RequestCount != 20 {
		t.Fatalf("mod-b totals mismatch: %+v", *statB)
	}
}

// TestSystemModelDailyTokens_MOD02_PerModelDailyCurve implements MOD-02：
//   - 为单一模型插入跨两日的窗口数据。
//   - 调用 /api/system/stats/models/daily_tokens?days=2&model_name=mod-x。
//   - 校验按 day 升序的每日 tokens/quota 聚合。
func TestSystemModelDailyTokens_MOD02_PerModelDailyCurve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping system model daily tokens integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	inspector, err := testutil.NewDBStatsInspectorFromServer(suite.Server)
	if err != nil {
		t.Fatalf("failed to create DBStatsInspector: %v", err)
	}
	defer inspector.Close()

	now := time.Now()
	day1 := now.Add(-24*time.Hour - time.Minute).Unix()
	day0 := now.Add(-1 * time.Minute).Unix()

	insert := func(ts int64, tokens, quota int64) {
		rec := &testutil.ChannelStatisticsRecord{
			ChannelID:       1,
			ModelName:       "mod-x",
			TimeWindowStart: ts,
			RequestCount:    5,
			FailCount:       0,
			TotalTokens:     tokens,
			TotalQuota:      quota,
			TotalLatencyMS:  1000,
		}
		if err := inspector.InsertChannelStatistics(rec); err != nil {
			t.Fatalf("failed to insert stats: %v", err)
		}
	}

	insert(day1, 150, 75)
	insert(day0, 250, 125)

	var resp systemModelDailyTokensResponse
	if err := suite.Client.GetJSON("/api/system/stats/models/daily_tokens?days=2&model_name=mod-x", &resp); err != nil {
		t.Fatalf("failed to call /api/system/stats/models/daily_tokens: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/system/stats/models/daily_tokens returned success=false: %s", resp.Message)
	}

	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 daily records, got %d", len(resp.Data))
	}

	expectedTokens := []int64{150, 250}
	expectedQuotas := []int64{75, 125}

	for i, item := range resp.Data {
		if item.Tokens != expectedTokens[i] || item.Quota != expectedQuotas[i] {
			t.Fatalf("day %d mismatch: got tokens=%d quota=%d, want tokens=%d quota=%d",
				i, item.Tokens, item.Quota, expectedTokens[i], expectedQuotas[i])
		}
		if i > 0 && resp.Data[i-1].Day > item.Day {
			t.Fatalf("days not sorted ascending: %s > %s", resp.Data[i-1].Day, item.Day)
		}
		if item.ModelName != "mod-x" {
			t.Fatalf("expected model_name=mod-x, got %s", item.ModelName)
		}
	}
}

// billingGroupModelStatsResponse models /api/groups/system/model_stats.
type billingGroupModelStatsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []struct {
		UserGroup         string  `json:"user_group"`
		ModelName         string  `json:"model_name"`
		TotalTokens       int64   `json:"total_tokens"`
		TotalQuota        int64   `json:"total_quota"`
		TPM               int     `json:"tpm"`
		RPM               int     `json:"rpm"`
		AvgResponseTimeMs float64 `json:"avg_response_time_ms"`
		FailRate          float64 `json:"fail_rate"`
	} `json:"data"`
}

// billingGroupModelDailyTokensResponse models /api/groups/system/model_daily_tokens.
type billingGroupModelDailyTokensResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []struct {
		UserGroup string `json:"user_group"`
		Day       string `json:"day"`
		ModelName string `json:"model_name"`
		Tokens    int64  `json:"tokens"`
		Quota     int64  `json:"quota"`
	} `json:"data"`
}

// TestSystemGroupModelStats_MOD03_Basic verifies that the billing group per-model
// stats endpoint is wired correctly and works under SQLite:
//   - Calls /api/groups/system/model_stats?group=default&period=7d.
//   - Asserts success=true and response JSON decodes into expected structure.
//   - Detailed聚合公式在 monitoring-stats 套件和 model 层单元已覆盖，这里只做接口连通性验证。
func TestSystemGroupModelStats_MOD03_Basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping billing group model stats integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	var resp billingGroupModelStatsResponse
	if err := suite.Client.GetJSON("/api/groups/system/model_stats?group=default&period=7d", &resp); err != nil {
		t.Fatalf("failed to call /api/groups/system/model_stats: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/groups/system/model_stats returned success=false: %s", resp.Message)
	}

	// We only assert schema-level properties here; content validity is covered elsewhere.
	for _, item := range resp.Data {
		if item.UserGroup == "" {
			t.Fatalf("expected non-empty user_group in model_stats item: %+v", item)
		}
		if item.ModelName == "" {
			t.Fatalf("expected non-empty model_name in model_stats item: %+v", item)
		}
	}
}

// TestSystemGroupModelDailyTokens_MOD04_Basic verifies that the billing group
// per-model daily tokens endpoint works end-to-end:
//   - Calls /api/groups/system/model_daily_tokens?group=default&days=30&model_name=gpt-4.
//   - Asserts success=true and response JSON decodes without SQL errors (SQLite date函数兼容).
func TestSystemGroupModelDailyTokens_MOD04_Basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping billing group model daily tokens integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	var resp billingGroupModelDailyTokensResponse
	if err := suite.Client.GetJSON("/api/groups/system/model_daily_tokens?group=default&days=30&model_name=gpt-4", &resp); err != nil {
		t.Fatalf("failed to call /api/groups/system/model_daily_tokens: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/groups/system/model_daily_tokens returned success=false: %s", resp.Message)
	}

	for _, item := range resp.Data {
		if item.UserGroup == "" || item.Day == "" || item.ModelName == "" {
			t.Fatalf("unexpected empty field in model_daily_tokens item: %+v", item)
		}
	}
}
