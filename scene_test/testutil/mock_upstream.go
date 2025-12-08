// Package testutil provides utilities for integration testing of the NewAPI service.
package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// MockUpstreamServer simulates an upstream LLM provider (like OpenAI).
// It records all requests and returns configurable responses.
type MockUpstreamServer struct {
	Server       *httptest.Server
	BaseURL      string
	RequestCount int
	Requests     []MockRequest
	mu           sync.Mutex

	// Response configuration
	ResponseDelay  time.Duration
	ResponseStatus int
	ResponseBody   interface{}
	ErrorResponse  *MockErrorResponse
}

// MockRequest represents a recorded request to the mock server.
type MockRequest struct {
	Method    string
	Path      string
	Headers   http.Header
	Body      []byte
	Timestamp time.Time
}

// MockErrorResponse configures an error response.
type MockErrorResponse struct {
	StatusCode int
	ErrorType  string
	Message    string
}

// MockChatResponse represents a standard chat completion response.
type MockChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// NewMockUpstreamServer creates a new mock upstream server.
func NewMockUpstreamServer() *MockUpstreamServer {
	mock := &MockUpstreamServer{
		Requests:       make([]MockRequest, 0),
		ResponseStatus: http.StatusOK,
	}

	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.handleRequest(w, r)
	}))
	mock.BaseURL = mock.Server.URL

	return mock
}

// handleRequest processes incoming requests.
func (m *MockUpstreamServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Record the request
	m.mu.Lock()
	m.RequestCount++
	m.Requests = append(m.Requests, MockRequest{
		Method:    r.Method,
		Path:      r.URL.Path,
		Headers:   r.Header.Clone(),
		Timestamp: time.Now(),
	})
	m.mu.Unlock()

	// Apply delay if configured
	if m.ResponseDelay > 0 {
		time.Sleep(m.ResponseDelay)
	}

	// Return error response if configured
	if m.ErrorResponse != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.ErrorResponse.StatusCode)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    m.ErrorResponse.ErrorType,
				"message": m.ErrorResponse.Message,
			},
		})
		return
	}

	// Return custom response if configured
	if m.ResponseBody != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.ResponseStatus)
		json.NewEncoder(w).Encode(m.ResponseBody)
		return
	}

	// Default: return a standard chat completion response
	m.sendDefaultChatResponse(w, r)
}

// sendDefaultChatResponse sends a standard successful chat completion response.
func (m *MockUpstreamServer) sendDefaultChatResponse(w http.ResponseWriter, r *http.Request) {
	response := MockChatResponse{
		ID:      fmt.Sprintf("chatcmpl-test-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "gpt-4",
		Choices: []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{
				Index: 0,
				Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{
					Role:    "assistant",
					Content: "This is a test response from the mock upstream server.",
				},
				FinishReason: "stop",
			},
		},
		Usage: struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		}{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// SetResponse configures a custom response.
func (m *MockUpstreamServer) SetResponse(status int, body interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResponseStatus = status
	m.ResponseBody = body
	m.ErrorResponse = nil
}

// SetError configures an error response.
func (m *MockUpstreamServer) SetError(statusCode int, errorType, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ErrorResponse = &MockErrorResponse{
		StatusCode: statusCode,
		ErrorType:  errorType,
		Message:    message,
	}
}

// SetDelay configures response delay.
func (m *MockUpstreamServer) SetDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResponseDelay = delay
}

// Reset clears all recorded requests and resets configuration.
func (m *MockUpstreamServer) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RequestCount = 0
	m.Requests = make([]MockRequest, 0)
	m.ResponseDelay = 0
	m.ResponseStatus = http.StatusOK
	m.ResponseBody = nil
	m.ErrorResponse = nil
}

// GetRequestCount returns the number of requests received.
func (m *MockUpstreamServer) GetRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RequestCount
}

// GetLastRequest returns the last request received.
func (m *MockUpstreamServer) GetLastRequest() *MockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Requests) == 0 {
		return nil
	}
	return &m.Requests[len(m.Requests)-1]
}

// Close shuts down the mock server.
func (m *MockUpstreamServer) Close() {
	if m.Server != nil {
		m.Server.Close()
	}
}
