package channel_statistics

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"

	"github.com/QuantumNous/new-api/scene_test/testutil"
	
)

// TestCL06_L2L3SchedulingUsingRedisInspector verifies that the Redis-level
// metadata used for L2/L3 调度（Hash + dirty_channels + next_db_sync_time）
// 可以通过 SimulateL1Flush / SetNextDBSyncTime 快速验证，而无需真实长时间窗口。
//
// 这对应设计文档中的 CL-06「L2到L3错峰同步」场景，但通过预埋 Redis 数据
// 的方式做灰盒测试：验证
//   - channel_stats Hash 已写入且带 TTL
//   - dirty_channels ZSet 中存在对应 member
//   - 不同渠道的 next_db_sync_time 可以被设置为错峰时间
func TestCL06_L2L3SchedulingUsingRedisInspector(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Redis inspector test in short mode")
	}

	// Start an isolated in-memory Redis for this test.
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	inspector, err := testutil.NewRedisStatsInspector(mr.Addr())
	if err != nil {
		t.Fatalf("failed to create RedisStatsInspector: %v", err)
	}
	defer inspector.Close()

	const (
		modelName    = "gpt-4"
		channelID1   = 6001
		channelID2   = 6002
		requestCount = 10
	)

	stats := map[string]int64{
		"req_count":    requestCount,
		"total_tokens": 1000,
	}
	userIDs := []int{1, 2, 3}

	// Simulate L1 -> L2 flush for two channels.
	if err := inspector.SimulateL1Flush(channelID1, modelName, stats, userIDs); err != nil {
		t.Fatalf("SimulateL1Flush for channel %d failed: %v", channelID1, err)
	}
	if err := inspector.SimulateL1Flush(channelID2, modelName, stats, userIDs); err != nil {
		t.Fatalf("SimulateL1Flush for channel %d failed: %v", channelID2, err)
	}

	// Verify basic Redis data flow for one channel（Hash + TTL + dirty_channels）。
	if err := inspector.VerifyRedisDataFlow(channelID1, modelName); err != nil {
		t.Fatalf("VerifyRedisDataFlow failed for channel %d: %v", channelID1, err)
	}

	// Now mock L2->L3 scheduling metadata via next_db_sync_time 字段。
	now := time.Now().Unix()
	base := now + 900 // 15 分钟后的基准时间

	if err := inspector.SetNextDBSyncTime(channelID1, modelName, base); err != nil {
		t.Fatalf("SetNextDBSyncTime for channel %d failed: %v", channelID1, err)
	}
	if err := inspector.SetNextDBSyncTime(channelID2, modelName, base+30); err != nil { // +30s 抖动
		t.Fatalf("SetNextDBSyncTime for channel %d failed: %v", channelID2, err)
	}

	next1, err := inspector.GetNextDBSyncTime(channelID1, modelName)
	if err != nil {
		t.Fatalf("GetNextDBSyncTime for channel %d failed: %v", channelID1, err)
	}
	next2, err := inspector.GetNextDBSyncTime(channelID2, modelName)
	if err != nil {
		t.Fatalf("GetNextDBSyncTime for channel %d failed: %v", channelID2, err)
	}

	if next1 == 0 || next2 == 0 {
		t.Fatalf("next_db_sync_time should not be zero (ch1=%d, ch2=%d)", next1, next2)
	}
	if next1 == next2 {
		t.Errorf("expected staggered next_db_sync_time, got identical values: %d == %d", next1, next2)
	}
}

// TestCON03_NextSyncEligibilityUsingRedisInspector 模拟 CON-03「DB Sync并发控制」
// 中对 next_db_sync_time 的调度判定：我们通过 Redis 预埋两个渠道的 next_db_sync_time，
// 一个在过去，一个在未来，然后根据当前时间判断「应当被同步」的渠道集合。
//
// 虽然这里没有直接调用 L3 Worker 的 shouldSync 方法，但测试了相同的
// 决策逻辑：now >= next_db_sync_time 的渠道才有资格参与本轮同步。
func TestCON03_NextSyncEligibilityUsingRedisInspector(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Redis inspector test in short mode")
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	inspector, err := testutil.NewRedisStatsInspector(mr.Addr())
	if err != nil {
		t.Fatalf("failed to create RedisStatsInspector: %v", err)
	}
	defer inspector.Close()

	const (
		modelName  = "gpt-4"
		channelOld = 7001
		channelNew = 7002
	)

	now := time.Now().Unix()
	past := now - 10    // 已到期，应当立即同步
	future := now + 600 // 未来10分钟，不应在当前轮被同步

	if err := inspector.SetNextDBSyncTime(channelOld, modelName, past); err != nil {
		t.Fatalf("SetNextDBSyncTime (old) failed: %v", err)
	}
	if err := inspector.SetNextDBSyncTime(channelNew, modelName, future); err != nil {
		t.Fatalf("SetNextDBSyncTime (new) failed: %v", err)
	}

	nextOld, err := inspector.GetNextDBSyncTime(channelOld, modelName)
	if err != nil {
		t.Fatalf("GetNextDBSyncTime (old) failed: %v", err)
	}
	nextNew, err := inspector.GetNextDBSyncTime(channelNew, modelName)
	if err != nil {
		t.Fatalf("GetNextDBSyncTime (new) failed: %v", err)
	}

	shouldSyncOld := now >= nextOld
	shouldSyncNew := now >= nextNew

	if !shouldSyncOld {
		t.Errorf("expected old channel (next_db_sync_time=%d, now=%d) to be eligible for sync", nextOld, now)
	}
	if shouldSyncNew {
		t.Errorf("expected new channel (next_db_sync_time=%d, now=%d) NOT to be eligible for sync", nextNew, now)
	}
}

// TestCL08_WindowedRedisKeysUsingRedisInspector 验证通过 SimulateL1Flush 预埋的
// Redis 键名已经引入「窗口起点」维度，并且与 ChannelStatsL2Service.AlignToWindow
// / CHANNEL_STATS_WINDOW_SECONDS 一致。这是 CL-08「三级缓存读路径」的辅助灰盒
// 校验：确保 L2 侧的键空间按窗口切分，为后续 /api/channels/:id/stats 的窗口聚合、
// 冷热分层提供基础。
func TestCL08_WindowedRedisKeysUsingRedisInspector(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Redis inspector test in short mode")
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	inspector, err := testutil.NewRedisStatsInspector(mr.Addr())
	if err != nil {
		t.Fatalf("failed to create RedisStatsInspector: %v", err)
	}
	defer inspector.Close()

	const (
		channelID = 8001
		modelName = "gpt-4"
	)

	stats := map[string]int64{
		"request_count": 5,
		"total_tokens":  500,
	}
	userIDs := []int{101, 102}

	if err := inspector.SimulateL1Flush(channelID, modelName, stats, userIDs); err != nil {
		t.Fatalf("SimulateL1Flush failed: %v", err)
	}

	// 检查 channel_stats:* 键名是否带窗口起点，并且与 AlignToWindow 对齐。
	keys, err := inspector.GetAllKeys("channel_stats:*")
	if err != nil {
		t.Fatalf("failed to list channel_stats keys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatalf("expected at least one channel_stats key after SimulateL1Flush")
	}

	var matchedKey string
	for _, k := range keys {
		if strings.HasPrefix(k, "channel_stats:") {
			matchedKey = k
			break
		}
	}
	if matchedKey == "" {
		t.Fatalf("no channel_stats key with expected prefix found")
	}

	parts := strings.Split(matchedKey, ":")
	if len(parts) != 4 {
		t.Fatalf("expected channel_stats key format channel_stats:{id}:{model}:{window}, got %q", matchedKey)
	}

	if parts[0] != "channel_stats" {
		t.Errorf("expected prefix channel_stats, got %q", parts[0])
	}
	id, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("failed to parse channel id from key %q: %v", matchedKey, err)
	}
	if id != channelID {
		t.Errorf("expected channel id %d in key, got %d", channelID, id)
	}
	if parts[2] != modelName {
		t.Errorf("expected model %q in key, got %q", modelName, parts[2])
	}

	windowFromKey, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		t.Fatalf("failed to parse window_start from key %q: %v", matchedKey, err)
	}

	alignedNow := service.AlignToWindow(time.Now().Unix())
	if windowFromKey != alignedNow {
		t.Errorf("expected window_start aligned to %d, got %d (key=%q)", alignedNow, windowFromKey, matchedKey)
	}

	// 验证 dirty_channels 成员同样带窗口信息，并与键名中的 window_start 一致。
	dirty, err := inspector.GetDirtyChannels()
	if err != nil {
		t.Fatalf("failed to get dirty_channels: %v", err)
	}
	expectedMember := fmt.Sprintf("%d:%s:%d", channelID, modelName, windowFromKey)
	if _, ok := dirty[expectedMember]; !ok {
		t.Errorf("expected dirty_channels to contain member %q, got %#v", expectedMember, dirty)
	}
}
