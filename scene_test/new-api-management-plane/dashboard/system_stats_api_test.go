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

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

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
