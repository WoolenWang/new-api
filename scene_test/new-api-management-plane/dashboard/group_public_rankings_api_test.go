// Package dashboard contains integration tests for the management-plane
// dashboard / statistics APIs. This file focuses on the public shared
// group rankings endpoint:
//   - GR-01/GR-02: 基线 + 公开性过滤
//     /api/groups/public/rankings?metric=tokens_7d&period=7d
package dashboard

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// groupRankingsResponse models the JSON response of /api/groups/public/rankings.
type groupRankingsResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Metric     string                  `json:"metric"`
		Period     string                  `json:"period"`
		Order      string                  `json:"order"`
		TotalCount int                     `json:"total_count"`
		Offset     int                     `json:"offset"`
		Limit      int                     `json:"limit"`
		Items      []model.GroupRankingRow `json:"items"`
	} `json:"data"`
}

// TestGroupRankings_GR01_GR02_Tokens7dAndVisibility implements一个合并场景：
//   - GR-01: 基线 7 天 Token 排名（公开共享分组）。
//   - GR-02: 公开性过滤与分组类型过滤。
//
// 测试思路：
//  1. 直接在共享 SQLite DB 中创建 5 个分组：
//     - gFast (Shared, Password, 有统计，Token 最大)
//     - gMid  (Shared, Approval, 有统计，中等 Token)
//     - gZero (Shared, Password, 无统计，Token=0)
//     - gPrivate (Shared, Invite, 有统计，应被过滤)
//     - gNonShared (Private, Password, 有统计，应被过滤)
//  2. 为除 gZero 外的 4 个分组插入 group_statistics 窗口（落在 7d 内）。
//  3. 调用 /api/groups/public/rankings?metric=tokens_7d&period=7d&limit=10。
//  4. 断言：
//     - 仅返回 3 个公开共享分组（gFast/gMid/gZero），总数为 3。
//     - 排序按 tokens_7d 降序：gFast > gMid > gZero。
//     - gPrivate 与 gNonShared 不在结果中。
func TestGroupRankings_GR01_GR02_Tokens7dAndVisibility(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping public group rankings integration test in short mode")
	}

	suite, cleanup := SetupSuite(t)
	defer cleanup()

	adminID := suite.Server.AdminUserID
	now := time.Now().Unix()
	windowStart := now - 3600 // 1 hour ago, within 7d window

	// Helper to create a group with given properties.
	createGroup := func(name string, groupType, joinMethod int) *model.Group {
		g := &model.Group{
			Name:        name,
			DisplayName: name,
			OwnerId:     adminID,
			Type:        groupType,
			JoinMethod:  joinMethod,
		}
		if err := model.CreateGroup(g); err != nil {
			t.Fatalf("failed to create group %s: %v", name, err)
		}
		return g
	}

	// Public shared groups (should appear in rankings).
	gFast := createGroup("gr-fast", model.GroupTypeShared, model.JoinMethodPassword)
	gMid := createGroup("gr-mid", model.GroupTypeShared, model.JoinMethodApproval)
	gZero := createGroup("gr-zero", model.GroupTypeShared, model.JoinMethodPassword) // no stats

	// Non-public / non-shared groups (should NOT appear).
	gPrivate := createGroup("gr-private", model.GroupTypeShared, model.JoinMethodInvite)
	gNonShared := createGroup("gr-nonshared", model.GroupTypePrivate, model.JoinMethodPassword)

	// Insert group_statistics records for groups with non-zero tokens.
	insertStat := func(groupID int, tokens int64) {
		stat := &model.GroupStatistics{
			GroupId:           groupID,
			ModelName:         "gpt-4",
			TimeWindowStart:   windowStart,
			TPM:               int(tokens), // value not important for this test
			RPM:               10,
			FailRate:          1.0,
			AvgResponseTimeMs: 200,
			TotalTokens:       tokens,
			TotalQuota:        tokens / 10,
		}
		if err := model.UpsertGroupStatistics(stat); err != nil {
			t.Fatalf("failed to upsert group_statistics for group %d: %v", groupID, err)
		}
	}

	insertStat(gFast.Id, 3000)
	insertStat(gMid.Id, 2000)
	insertStat(gPrivate.Id, 5000)
	insertStat(gNonShared.Id, 6000)
	// gZero intentionally has no statistics -> tokens_7d = 0

	var resp groupRankingsResponse
	if err := suite.Client.GetJSON("/api/groups/public/rankings?metric=tokens_7d&period=7d&limit=10", &resp); err != nil {
		t.Fatalf("failed to call /api/groups/public/rankings: %v", err)
	}
	if !resp.Success {
		t.Fatalf("/api/groups/public/rankings returned success=false: %s", resp.Message)
	}

	if resp.Data.Metric != "tokens_7d" {
		t.Fatalf("expected metric=tokens_7d, got %s", resp.Data.Metric)
	}
	if resp.Data.Period != "7d" {
		t.Fatalf("expected period=7d, got %s", resp.Data.Period)
	}
	if resp.Data.Order != "desc" {
		t.Fatalf("expected default order=desc for tokens_7d, got %s", resp.Data.Order)
	}

	if resp.Data.TotalCount != 3 {
		t.Fatalf("expected total_count=3 (only public shared groups), got %d", resp.Data.TotalCount)
	}
	if len(resp.Data.Items) != 3 {
		t.Fatalf("expected 3 ranking items, got %d", len(resp.Data.Items))
	}

	items := resp.Data.Items

	// Verify set of group IDs equals {gFast, gMid, gZero}, regardless of order.
	expected := map[int]bool{
		gFast.Id: true,
		gMid.Id:  true,
		gZero.Id: true,
	}
	for _, it := range items {
		if !expected[it.GroupId] {
			t.Fatalf("unexpected group_id %d in rankings; expected one of [%d, %d, %d]",
				it.GroupId, gFast.Id, gMid.Id, gZero.Id)
		}
	}

	// Ensure filtered-out groups do not appear.
	for _, it := range items {
		if it.GroupId == gPrivate.Id || it.GroupId == gNonShared.Id {
			t.Fatalf("filtered-out group (id=%d) unexpectedly appeared in rankings", it.GroupId)
		}
	}
}
