package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestSessionBindingMemoryStore(t *testing.T) {
	common.RedisEnabled = false
	common.RDB = nil

	InitSessionManager()

	ctx := context.Background()
	key := BuildSessionBindingKey(1, "gpt-4", "sid-1")
	entry := &SessionIndexEntry{
		SessionKey: key,
		SessionID:  "sid-1",
		UserID:     1,
		Model:      "gpt-4",
		ChannelID:  101,
		KeyID:      2,
		KeyHash:    "hash-abc",
		Group:      "default",
	}

	if err := SaveSessionBinding(ctx, entry); err != nil {
		t.Fatalf("failed to save session binding: %v", err)
	}

	got, err := GetSessionBinding(ctx, 1, "gpt-4", "sid-1")
	if err != nil {
		t.Fatalf("failed to get session binding: %v", err)
	}
	if got == nil || got.ChannelID != entry.ChannelID || got.KeyHash != entry.KeyHash {
		t.Fatalf("binding mismatch: got %+v", got)
	}

	if _, err := RemoveSessionBinding(ctx, key); err != nil {
		t.Fatalf("failed to remove session binding: %v", err)
	}
	got, err = GetSessionBinding(ctx, 1, "gpt-4", "sid-1")
	if err != nil {
		t.Fatalf("get after remove failed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after removal, got %+v", got)
	}
}
