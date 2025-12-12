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

// MockJudgeLLM simulates a judge LLM service for model monitoring evaluation.
// It returns configurable similarity scores and evaluation results.
type MockJudgeLLM struct {
	Server       *httptest.Server
	BaseURL      string
	RequestCount int
	Requests     []MockJudgeRequest
	mu           sync.Mutex

	// Response configuration
	ResponseDelay   time.Duration
	SimilarityScore float64
	IsPass          bool
	Reason          string
	// RawContent, when set, is returned directly as the judge message content.
	// This enables boundary tests for invalid/non-JSON judge outputs.
	RawContent       *string
	ShouldFail       bool
	FailureErrorType string
	FailureMessage   string
}

// MockJudgeRequest represents a recorded request to the mock judge LLM.
type MockJudgeRequest struct {
	Method    string
	Path      string
	Headers   http.Header
	Body      []byte
	Timestamp time.Time
}

// JudgeEvaluationResponse represents the JSON response from judge LLM.
type JudgeEvaluationResponse struct {
	SimilarityScore float64 `json:"similarity_score"`
	DiffScore       float64 `json:"diff_score"`
	IsPass          bool    `json:"is_pass"`
	Reason          string  `json:"reason"`
}

// NewMockJudgeLLM creates a new mock judge LLM server.
func NewMockJudgeLLM() *MockJudgeLLM {
	mock := &MockJudgeLLM{
		Requests:        make([]MockJudgeRequest, 0),
		SimilarityScore: 95.0, // Default high similarity
		IsPass:          true,
		Reason:          "输出风格与基准高度一致",
		ShouldFail:      false,
	}

	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.handleRequest(w, r)
	}))
	mock.BaseURL = mock.Server.URL

	return mock
}

// handleRequest processes incoming requests to the judge LLM.
func (m *MockJudgeLLM) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Record the request
	m.mu.Lock()
	m.RequestCount++
	m.Requests = append(m.Requests, MockJudgeRequest{
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

	// Return failure response if configured
	if m.ShouldFail {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    m.FailureErrorType,
				"message": m.FailureMessage,
			},
		})
		return
	}

	// Return evaluation response
	response := JudgeEvaluationResponse{
		SimilarityScore: m.SimilarityScore,
		DiffScore:       100.0 - m.SimilarityScore,
		IsPass:          m.IsPass,
		Reason:          m.Reason,
	}

	content := toJSONString(response)
	if m.RawContent != nil {
		content = *m.RawContent
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      fmt.Sprintf("judge-eval-%d", time.Now().UnixNano()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "gpt-4-judge",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	})
}

// toJSONString converts the evaluation response to a JSON string.
func toJSONString(response JudgeEvaluationResponse) string {
	bytes, _ := json.Marshal(response)
	return string(bytes)
}

// SetEvaluationResult configures the evaluation result.
func (m *MockJudgeLLM) SetEvaluationResult(similarityScore float64, isPass bool, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SimilarityScore = similarityScore
	m.IsPass = isPass
	m.Reason = reason
	m.RawContent = nil
	m.ShouldFail = false
}

// SetResponse configures the judge LLM to return a raw content string.
// modelName and testType are accepted for compatibility with older tests.
func (m *MockJudgeLLM) SetResponse(modelName, testType, rawContent string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RawContent = &rawContent
	m.ShouldFail = false
}

// SetHighSimilarity sets a high similarity score (>= 90).
func (m *MockJudgeLLM) SetHighSimilarity() {
	m.SetEvaluationResult(95.0, true, "输出风格与基准高度一致，语气、逻辑和内容质量相似")
}

// SetLowSimilarity sets a low similarity score (< 60).
func (m *MockJudgeLLM) SetLowSimilarity() {
	m.SetEvaluationResult(30.0, false, "输出风格与基准差异显著，内容质量和逻辑结构明显不同")
}

// SetMediumSimilarity sets a medium similarity score (60-89).
func (m *MockJudgeLLM) SetMediumSimilarity() {
	m.SetEvaluationResult(75.0, true, "输出风格与基准基本一致，有少量差异但可接受")
}

// SetFailure configures the judge LLM to return an error.
func (m *MockJudgeLLM) SetFailure(errorType, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ShouldFail = true
	m.FailureErrorType = errorType
	m.FailureMessage = message
}

// SetDelay configures response delay.
func (m *MockJudgeLLM) SetDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResponseDelay = delay
}

// Reset clears all recorded requests and resets configuration.
func (m *MockJudgeLLM) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RequestCount = 0
	m.Requests = make([]MockJudgeRequest, 0)
	m.ResponseDelay = 0
	m.SimilarityScore = 95.0
	m.IsPass = true
	m.Reason = "输出风格与基准高度一致"
	m.RawContent = nil
	m.ShouldFail = false
	m.FailureErrorType = ""
	m.FailureMessage = ""
}

// GetRequestCount returns the number of requests received.
func (m *MockJudgeLLM) GetRequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RequestCount
}

// GetLastRequest returns the last request received.
func (m *MockJudgeLLM) GetLastRequest() *MockJudgeRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Requests) == 0 {
		return nil
	}
	return &m.Requests[len(m.Requests)-1]
}

// Close shuts down the mock judge LLM server.
func (m *MockJudgeLLM) Close() {
	if m.Server != nil {
		m.Server.Close()
	}
}
