// Package testutil - Mock LLM Server for Testing
//
// This file provides a configurable Mock LLM server that simulates upstream
// LLM providers (OpenAI, Anthropic, etc.) for testing purposes.
//
// Features:
// - Configurable response delay (for latency testing)
// - Error injection (5xx, 4xx errors)
// - Streaming response support
// - Per-channel and per-model response configuration
// - Token usage simulation
package testutil

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// MockLLMServer simulates an LLM upstream provider.
type MockLLMServer struct {
	server    *httptest.Server
	responses map[string]*MockLLMResponse
	mu        sync.RWMutex
	baseURL   string
}

// MockLLMResponse defines a configurable mock response.
type MockLLMResponse struct {
	StatusCode       int           // HTTP status code (200, 500, etc.)
	Delay            time.Duration // Response delay (first-byte latency)
	IsStream         bool          // Whether to send streaming response
	Content          string        // Response content
	PromptTokens     int           // Simulated prompt tokens
	CompletionTokens int           // Simulated completion tokens
	ErrorMessage     string        // Error message for non-200 responses
	FailureRate      float64       // Probability of returning error (0.0-1.0)
}

// NewMockLLMServer creates a new Mock LLM server.
func NewMockLLMServer() *MockLLMServer {
	mock := &MockLLMServer{
		responses: make(map[string]*MockLLMResponse),
	}

	// Create HTTP server.
	mock.server = httptest.NewServer(http.HandlerFunc(mock.handleRequest))
	mock.baseURL = mock.server.URL

	return mock
}

// URL returns the base URL of the mock server.
func (m *MockLLMServer) URL() string {
	return m.baseURL
}

// Close shuts down the mock server.
func (m *MockLLMServer) Close() {
	m.server.Close()
}

// SetResponse configures a response for a specific channel and model.
func (m *MockLLMServer) SetResponse(channelID int, model string, response *MockLLMResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.makeKey(channelID, model)
	m.responses[key] = response
}

// SetDefaultResponse sets a default response for all unmatched requests.
func (m *MockLLMServer) SetDefaultResponse(response *MockLLMResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.responses["default"] = response
}

// makeKey creates a lookup key for channel and model.
func (m *MockLLMServer) makeKey(channelID int, model string) string {
	return fmt.Sprintf("ch%d:model:%s", channelID, model)
}

// handleRequest is the main HTTP handler for the mock server.
func (m *MockLLMServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Parse request body to extract model.
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	model, _ := reqBody["model"].(string)
	if model == "" {
		model = "gpt-4" // Default model
	}

	// Try to get channel ID from header (if provided).
	channelIDStr := r.Header.Get("X-Channel-ID")
	var channelID int
	if channelIDStr != "" {
		fmt.Sscanf(channelIDStr, "%d", &channelID)
	}

	// Lookup response configuration.
	m.mu.RLock()
	key := m.makeKey(channelID, model)
	response, ok := m.responses[key]
	if !ok {
		// Try default response.
		response, ok = m.responses["default"]
		if !ok {
			// No configuration found, return a basic success response.
			m.mu.RUnlock()
			m.sendDefaultSuccessResponse(w, model)
			return
		}
	}
	m.mu.RUnlock()

	// Simulate delay (first-byte latency).
	if response.Delay > 0 {
		time.Sleep(response.Delay)
	}

	// Check for failure injection.
	if response.FailureRate > 0 && shouldFail(response.FailureRate) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": response.ErrorMessage,
				"type":    "server_error",
				"code":    "internal_error",
			},
		})
		return
	}

	// Return configured status code.
	if response.StatusCode >= 400 {
		w.WriteHeader(response.StatusCode)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": response.ErrorMessage,
				"type":    getErrorType(response.StatusCode),
				"code":    getErrorCode(response.StatusCode),
			},
		})
		return
	}

	// Send successful response.
	if response.IsStream {
		m.sendStreamingResponse(w, model, response)
	} else {
		m.sendNormalResponse(w, model, response)
	}
}

// sendDefaultSuccessResponse sends a basic success response.
func (m *MockLLMServer) sendDefaultSuccessResponse(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      "chatcmpl-mock-" + time.Now().Format("20060102150405"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": "This is a mock response from the test LLM server.",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     10,
			"completion_tokens": 20,
			"total_tokens":      30,
		},
	})
}

// sendNormalResponse sends a normal (non-streaming) response.
func (m *MockLLMServer) sendNormalResponse(w http.ResponseWriter, model string, response *MockLLMResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      "chatcmpl-mock-" + time.Now().Format("20060102150405"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": response.Content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     response.PromptTokens,
			"completion_tokens": response.CompletionTokens,
			"total_tokens":      response.PromptTokens + response.CompletionTokens,
		},
	})
}

// sendStreamingResponse sends a streaming response.
func (m *MockLLMServer) sendStreamingResponse(w http.ResponseWriter, model string, response *MockLLMResponse) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Split content into chunks.
	chunks := m.splitIntoChunks(response.Content, 10)

	for i, chunk := range chunks {
		// Send chunk as SSE event.
		data := map[string]interface{}{
			"id":      fmt.Sprintf("chatcmpl-mock-%d", i),
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"delta": map[string]string{
						"content": chunk,
					},
					"finish_reason": nil,
				},
			},
		}

		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", jsonData)
		flusher.Flush()

		// Small delay between chunks.
		time.Sleep(10 * time.Millisecond)
	}

	// Send final chunk with finish_reason.
	finalChunk := map[string]interface{}{
		"id":      "chatcmpl-mock-final",
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]string{},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     response.PromptTokens,
			"completion_tokens": response.CompletionTokens,
			"total_tokens":      response.PromptTokens + response.CompletionTokens,
		},
	}

	jsonData, _ := json.Marshal(finalChunk)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// splitIntoChunks splits content into smaller chunks for streaming.
func (m *MockLLMServer) splitIntoChunks(content string, chunkSize int) []string {
	var chunks []string
	runes := []rune(content)

	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}

	return chunks
}

// shouldFail determines if a request should fail based on failure rate.
func shouldFail(rate float64) bool {
	// Simple random failure based on nanosecond time.
	return float64(time.Now().Nanosecond()%100)/100.0 < rate
}

// getErrorType returns error type based on status code.
func getErrorType(statusCode int) string {
	switch {
	case statusCode >= 500:
		return "server_error"
	case statusCode == 429:
		return "rate_limit_exceeded"
	case statusCode >= 400:
		return "invalid_request_error"
	default:
		return "unknown_error"
	}
}

// getErrorCode returns error code based on status code.
func getErrorCode(statusCode int) string {
	switch statusCode {
	case 400:
		return "invalid_request"
	case 401:
		return "invalid_api_key"
	case 403:
		return "forbidden"
	case 404:
		return "not_found"
	case 429:
		return "rate_limit_exceeded"
	case 500:
		return "internal_error"
	case 503:
		return "service_unavailable"
	default:
		return fmt.Sprintf("error_%d", statusCode)
	}
}

// NewDefaultSuccessResponse creates a standard success response.
func NewDefaultSuccessResponse(content string, promptTokens, completionTokens int) *MockLLMResponse {
	return &MockLLMResponse{
		StatusCode:       http.StatusOK,
		Delay:            100 * time.Millisecond, // Default delay
		IsStream:         false,
		Content:          content,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}
}

// NewStreamingResponse creates a streaming response.
func NewStreamingResponse(content string, promptTokens, completionTokens int) *MockLLMResponse {
	return &MockLLMResponse{
		StatusCode:       http.StatusOK,
		Delay:            50 * time.Millisecond,
		IsStream:         true,
		Content:          content,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}
}

// NewErrorResponse creates an error response.
func NewErrorResponse(statusCode int, errorMessage string) *MockLLMResponse {
	return &MockLLMResponse{
		StatusCode:   statusCode,
		Delay:        10 * time.Millisecond,
		ErrorMessage: errorMessage,
	}
}

// NewDelayedResponse creates a response with custom delay.
func NewDelayedResponse(content string, delay time.Duration, promptTokens, completionTokens int) *MockLLMResponse {
	return &MockLLMResponse{
		StatusCode:       http.StatusOK,
		Delay:            delay,
		IsStream:         false,
		Content:          content,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}
}

// NewFlakeyResponse creates a response with configurable failure rate.
func NewFlakeyResponse(failureRate float64, errorMessage string) *MockLLMResponse {
	return &MockLLMResponse{
		StatusCode:       http.StatusOK,
		Delay:            100 * time.Millisecond,
		Content:          "Success response",
		PromptTokens:     10,
		CompletionTokens: 20,
		FailureRate:      failureRate,
		ErrorMessage:     errorMessage,
	}
}

// SetupMockLLMResponse 为指定的 MockLLMServer 配置默认响应。
// 场景测试通常只关心“本次请求的模型与用量”，因此这里简单地设置全局默认响应即可。
func SetupMockLLMResponse(t *testing.T, mock *MockLLMServer, resp MockLLMResponse) {
	t.Helper()
	if mock == nil {
		t.Fatalf("SetupMockLLMResponse: mock LLM server is nil")
	}

	// 默认使用 200 OK
	if resp.StatusCode == 0 {
		resp.StatusCode = http.StatusOK
	}
	// 提供一个默认内容，避免返回空字符串导致上游解析异常
	if resp.Content == "" {
		resp.Content = "mock response"
	}

	mock.SetDefaultResponse(&resp)
}

// ConfigureChannelResponse is a helper to configure responses for a channel.
func (m *MockLLMServer) ConfigureChannelResponse(channelID int, model string, config ResponseConfig) {
	response := &MockLLMResponse{
		StatusCode:       config.StatusCode,
		Delay:            config.Delay,
		IsStream:         config.IsStream,
		Content:          config.Content,
		PromptTokens:     config.PromptTokens,
		CompletionTokens: config.CompletionTokens,
		ErrorMessage:     config.ErrorMessage,
		FailureRate:      config.FailureRate,
	}

	m.SetResponse(channelID, model, response)
}

// ResponseConfig is a simplified configuration structure.
type ResponseConfig struct {
	StatusCode       int
	Delay            time.Duration
	IsStream         bool
	Content          string
	PromptTokens     int
	CompletionTokens int
	ErrorMessage     string
	FailureRate      float64
}

// DefaultResponseConfig returns a standard success configuration.
func DefaultResponseConfig() ResponseConfig {
	return ResponseConfig{
		StatusCode:       http.StatusOK,
		Delay:            100 * time.Millisecond,
		IsStream:         false,
		Content:          "This is a mock LLM response for testing.",
		PromptTokens:     10,
		CompletionTokens: 20,
	}
}

// MockRequestCounter tracks the number of requests to the mock server.
type MockRequestCounter struct {
	mu           sync.Mutex
	requestCount map[string]int
}

// NewMockRequestCounter creates a new request counter.
func NewMockRequestCounter() *MockRequestCounter {
	return &MockRequestCounter{
		requestCount: make(map[string]int),
	}
}

// Increment increments the counter for a key.
func (c *MockRequestCounter) Increment(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestCount[key]++
}

// Get returns the count for a key.
func (c *MockRequestCounter) Get(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requestCount[key]
}

// Reset resets all counters.
func (c *MockRequestCounter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestCount = make(map[string]int)
}

// ParseStreamingResponse parses a streaming SSE response.
func ParseStreamingResponse(reader io.Reader) ([]string, error) {
	var chunks []string
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: {json}"
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			if data == "[DONE]" {
				break
			}

			// Parse JSON chunk.
			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			// Extract content from delta.
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if content, ok := delta["content"].(string); ok {
							chunks = append(chunks, content)
						}
					}
				}
			}
		}
	}

	return chunks, scanner.Err()
}

// VerifyStreamingResponse verifies a streaming response.
func VerifyStreamingResponse(reader io.Reader, expectedContent string) error {
	chunks, err := ParseStreamingResponse(reader)
	if err != nil {
		return fmt.Errorf("failed to parse streaming response: %w", err)
	}

	actualContent := strings.Join(chunks, "")
	if actualContent != expectedContent {
		return fmt.Errorf("content mismatch: expected %q, got %q", expectedContent, actualContent)
	}

	return nil
}
