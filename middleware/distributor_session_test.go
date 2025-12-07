package middleware

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func newJSONContext(method, path, body string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c
}

func TestExtractSessionIDPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Header should override query/body/metadata
	c := newJSONContext("POST", "/v1/chat/completions?session_id=query-id", `{"session_id":"body-id","metadata":{"session_id":"meta-id"}}`)
	c.Request.Header.Set("Session-ID", "header-id")
	if got := extractSessionID(c); got != "header-id" {
		t.Fatalf("expected header session id, got %s", got)
	}

	// Query should be used when no header
	c2 := newJSONContext("POST", "/v1/chat/completions?session_id=query-id", `{"session_id":"body-id"}`)
	if got := extractSessionID(c2); got != "query-id" {
		t.Fatalf("expected query session id, got %s", got)
	}

	// Body session_id when no header/query
	c3 := newJSONContext("POST", "/v1/chat/completions", `{"session_id":"body-id"}`)
	if got := extractSessionID(c3); got != "body-id" {
		t.Fatalf("expected body session id, got %s", got)
	}

	// Metadata fallback
	c4 := newJSONContext("POST", "/v1/chat/completions", `{"metadata":{"conversation_id":"meta-id"}}`)
	if got := extractSessionID(c4); got != "meta-id" {
		t.Fatalf("expected metadata session id, got %s", got)
	}
}

func TestChannelSupportsModel(t *testing.T) {
	ch := &model.Channel{
		Models: "gpt-4,claude-3-opus,gpt-4o-mini",
	}
	if !channelSupportsModel(ch, "gpt-4o-mini") {
		t.Fatalf("expected channel to support gpt-4o-mini")
	}
	if channelSupportsModel(ch, "unknown-model") {
		t.Fatalf("expected channel not to support unknown-model")
	}
}
