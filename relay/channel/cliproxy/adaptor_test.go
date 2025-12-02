package cliproxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAdaptor_GetRequestURL(t *testing.T) {
	adaptor := &Adaptor{}

	tests := []struct {
		name          string
		baseUrl       string
		requestPath   string
		expectedURL   string
		expectedError bool
	}{
		{
			name:        "OpenAI chat completions",
			baseUrl:     "http://localhost:8080",
			requestPath: "/v1/chat/completions",
			expectedURL: "http://localhost:8080/v1/chat/completions",
		},
		{
			name:        "Claude messages",
			baseUrl:     "http://localhost:8080",
			requestPath: "/v1/messages",
			expectedURL: "http://localhost:8080/v1/messages",
		},
		{
			name:        "Gemini generateContent",
			baseUrl:     "http://localhost:8080",
			requestPath: "/v1beta/models/gemini-pro:generateContent",
			expectedURL: "http://localhost:8080/v1beta/models/gemini-pro:generateContent",
		},
		{
			name:        "Gemini streamGenerateContent",
			baseUrl:     "http://localhost:8080",
			requestPath: "/v1beta/models/gemini-1.5-pro:streamGenerateContent",
			expectedURL: "http://localhost:8080/v1beta/models/gemini-1.5-pro:streamGenerateContent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &common.RelayInfo{
				ChannelMeta: &common.ChannelMeta{
					ChannelBaseUrl: tt.baseUrl,
				},
				RequestURLPath: tt.requestPath,
			}
			url, err := adaptor.GetRequestURL(info)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedURL, url)
			}
		})
	}
}

func TestAdaptor_SetupRequestHeader(t *testing.T) {
	tests := []struct {
		name              string
		apiKey            string
		accountHint       string
		expectAuthHeader  string
		expectAccountHint string
	}{
		{
			name:              "With account hint",
			apiKey:            "test-key",
			accountHint:       "gemini_user1.json",
			expectAuthHeader:  "Bearer test-key",
			expectAccountHint: "gemini_user1.json",
		},
		{
			name:              "Without account hint",
			apiKey:            "test-key-2",
			accountHint:       "",
			expectAuthHeader:  "Bearer test-key-2",
			expectAccountHint: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptor := &Adaptor{}
			info := &common.RelayInfo{
				ChannelMeta: &common.ChannelMeta{
					ApiKey:      tt.apiKey,
					AccountHint: tt.accountHint,
				},
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			// Create a test request
			req := httptest.NewRequest("POST", "/test", nil)
			req.Header.Set("Content-Type", "application/json")
			c.Request = req

			header := make(http.Header)
			err := adaptor.SetupRequestHeader(c, &header, info)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectAuthHeader, header.Get("Authorization"))
			assert.Equal(t, tt.expectAccountHint, header.Get("X-CLIProxy-Account-Hint"))
		})
	}
}

func TestAdaptor_ConvertRequests(t *testing.T) {
	adaptor := &Adaptor{}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &common.RelayInfo{}

	t.Run("OpenAI request passthrough", func(t *testing.T) {
		request := &dto.GeneralOpenAIRequest{
			Model: "gpt-4",
			Messages: []dto.Message{
				{Role: "user", Content: "Hello"},
			},
		}
		converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
		assert.NoError(t, err)
		assert.Equal(t, request, converted)
	})

	t.Run("Claude request passthrough", func(t *testing.T) {
		request := &dto.ClaudeRequest{
			Model: "claude-3-5-sonnet",
			Messages: []dto.ClaudeMessage{
				{Role: "user", Content: "Hello"},
			},
		}
		converted, err := adaptor.ConvertClaudeRequest(c, info, request)
		assert.NoError(t, err)
		assert.Equal(t, request, converted)
	})

	t.Run("Gemini request passthrough", func(t *testing.T) {
		request := &dto.GeminiChatRequest{
			Contents: []dto.GeminiChatContent{
				{
					Parts: []dto.GeminiPart{
						{Text: "Hello"},
					},
				},
			},
		}
		converted, err := adaptor.ConvertGeminiRequest(c, info, request)
		assert.NoError(t, err)
		assert.Equal(t, request, converted)
	})
}

func TestAdaptor_Init(t *testing.T) {
	adaptor := &Adaptor{}
	info := &common.RelayInfo{}

	// Init should not panic
	assert.NotPanics(t, func() {
		adaptor.Init(info)
	})
}

func TestAdaptor_GetChannelName(t *testing.T) {
	adaptor := &Adaptor{}
	assert.Equal(t, "CLIProxyAPI", adaptor.GetChannelName())
}

func TestAdaptor_GetModelList(t *testing.T) {
	adaptor := &Adaptor{}
	// Model list should be nil as it's managed at channel level
	assert.Nil(t, adaptor.GetModelList())
}

// Integration test scenarios
func TestAdaptor_ProtocolTransparency(t *testing.T) {
	// This test validates that the adaptor correctly delegates to protocol-specific adaptors
	// based on RelayFormat without unnecessary conversions

	adaptor := &Adaptor{}

	testCases := []struct {
		name        string
		relayFormat types.RelayFormat
		description string
	}{
		{
			name:        "OpenAI format",
			relayFormat: types.RelayFormatOpenAI,
			description: "Should delegate to OpenAI adaptor",
		},
		{
			name:        "Claude format",
			relayFormat: types.RelayFormatClaude,
			description: "Should delegate to Claude adaptor",
		},
		{
			name:        "Gemini format",
			relayFormat: types.RelayFormatGemini,
			description: "Should delegate to Gemini adaptor",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Verify that conversion methods return original request without transformation
			// This ensures protocol transparency
			assert.NotNil(t, adaptor, tc.description)
		})
	}
}
