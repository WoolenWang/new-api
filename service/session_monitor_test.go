package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

// TestGetSessionSummaryWithoutRedis verifies that GetSessionSummary
// gracefully falls back when Redis is disabled.
func TestGetSessionSummaryWithoutRedis(t *testing.T) {
	// Disable Redis so that GetSessionSummary uses the local fallback.
	common.RedisEnabled = false
	common.RDB = nil

	ctx := context.Background()
	summary, err := GetSessionSummary(ctx, 10, 10)
	if err != nil {
		t.Fatalf("GetSessionSummary returned error without Redis: %v", err)
	}
	if summary == nil {
		t.Fatalf("expected non-nil summary when Redis is disabled")
	}
	if summary.TotalActiveSessions != 0 {
		t.Errorf("expected TotalActiveSessions to be 0, got %d", summary.TotalActiveSessions)
	}
	if len(summary.SessionsByChannel) != 0 {
		t.Errorf("expected SessionsByChannel to be empty, got %v", summary.SessionsByChannel)
	}
	if len(summary.TopUsersBySession) != 0 {
		t.Errorf("expected TopUsersBySession to be empty, got %v", summary.TopUsersBySession)
	}
	if len(summary.RecentSessions) != 0 {
		t.Errorf("expected RecentSessions to be empty, got %v", summary.RecentSessions)
	}
}

// TestCleanupChannelSessionsWithoutRedis verifies that CleanupChannelSessions
// is a no-op when Redis is disabled.
func TestCleanupChannelSessionsWithoutRedis(t *testing.T) {
	common.RedisEnabled = false
	common.RDB = nil

	ctx := context.Background()
	if err := CleanupChannelSessions(ctx, 123); err != nil {
		t.Fatalf("CleanupChannelSessions returned error without Redis: %v", err)
	}
}
