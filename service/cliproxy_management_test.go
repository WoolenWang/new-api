package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCLIProxyManagementClient(t *testing.T) {
	client := NewCLIProxyManagementClient("http://localhost:8080", "test-key")
	assert.NotNil(t, client)
	assert.Equal(t, "http://localhost:8080", client.BaseURL)
	assert.Equal(t, "test-key", client.APIKey)
	assert.NotNil(t, client.client)
}

func TestGetOAuthAuthURL(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		assert.Equal(t, "/v0/management/oauth/generate-url", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// 解析请求体
		var reqBody OAuthURLRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		assert.NoError(t, err)
		assert.Equal(t, "gemini", reqBody.Provider)
		assert.Equal(t, "test-state", reqBody.State)

		// 返回模拟响应
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(OAuthURLResponse{
			AuthURL: "https://accounts.google.com/oauth?state=test-state",
			State:   "test-state",
		})
	}))
	defer server.Close()

	client := NewCLIProxyManagementClient(server.URL, "test-key")
	resp, err := client.GetOAuthAuthURL("gemini", "test-state", "http://callback.com")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "https://accounts.google.com/oauth?state=test-state", resp.AuthURL)
	assert.Equal(t, "test-state", resp.State)
}

func TestGetAuthStatus(t *testing.T) {
	tests := []struct {
		name           string
		state          string
		mockStatus     string
		mockStatusCode int
		expectError    bool
	}{
		{
			name:           "Success status",
			state:          "test-state",
			mockStatus:     "success",
			mockStatusCode: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Pending status",
			state:          "test-state",
			mockStatus:     "pending",
			mockStatusCode: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Server error",
			state:          "test-state",
			mockStatus:     "",
			mockStatusCode: http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v0/management/get-auth-status", r.URL.Path)
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, tt.state, r.URL.Query().Get("state"))

				w.WriteHeader(tt.mockStatusCode)
				if tt.mockStatusCode == http.StatusOK {
					json.NewEncoder(w).Encode(AuthStatusResponse{
						Status:  tt.mockStatus,
						Message: "",
					})
				}
			}))
			defer server.Close()

			client := NewCLIProxyManagementClient(server.URL, "test-key")
			resp, err := client.GetAuthStatus(tt.state)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				assert.Equal(t, tt.mockStatus, resp.Status)
			}
		})
	}
}

func TestCompleteOAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v0/management/oauth/complete", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var reqBody OAuthCompleteRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		assert.NoError(t, err)
		assert.Equal(t, "gemini", reqBody.Provider)
		assert.Equal(t, "test-code", reqBody.Code)
		assert.Equal(t, "user123", reqBody.UserIdentifier)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewCLIProxyManagementClient(server.URL, "test-key")
	err := client.CompleteOAuth("gemini", "test-code", "test-state", "user123")

	assert.NoError(t, err)
}

func TestDeleteAuthFile(t *testing.T) {
	tests := []struct {
		name           string
		filename       string
		mockStatusCode int
		expectError    bool
	}{
		{
			name:           "Successful deletion",
			filename:       "gemini_user1.json",
			mockStatusCode: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "File not found (acceptable)",
			filename:       "nonexistent.json",
			mockStatusCode: http.StatusNotFound,
			expectError:    false,
		},
		{
			name:           "No content",
			filename:       "gemini_user2.json",
			mockStatusCode: http.StatusNoContent,
			expectError:    false,
		},
		{
			name:           "Server error",
			filename:       "bad.json",
			mockStatusCode: http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v0/management/auth-files", r.URL.Path)
				assert.Equal(t, "DELETE", r.Method)
				assert.Equal(t, tt.filename, r.URL.Query().Get("name"))
				assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

				w.WriteHeader(tt.mockStatusCode)
			}))
			defer server.Close()

			client := NewCLIProxyManagementClient(server.URL, "test-key")
			err := client.DeleteAuthFile(tt.filename)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeleteCLIProxyCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v0/management/auth-files", r.URL.Path)
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "gemini_user1.json", r.URL.Query().Get("name"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := DeleteCLIProxyCredential(server.URL, "test-key", "gemini_user1.json")
	assert.NoError(t, err)
}

func TestDeleteCLIProxyCredential_EmptyParams(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		apiKey      string
		accountHint string
	}{
		{"Empty baseURL", "", "key", "hint"},
		{"Empty apiKey", "url", "", "hint"},
		{"Empty accountHint", "url", "key", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DeleteCLIProxyCredential(tt.baseURL, tt.apiKey, tt.accountHint)
			assert.Error(t, err)
		})
	}
}

func TestInitiateDeviceFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v0/management/oauth/device-flow/initiate", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(DeviceFlowResponse{
			VerificationURI:         "https://qwen.ai/activate",
			VerificationURIComplete: "https://qwen.ai/activate?user_code=ABCD-EFGH",
			UserCode:                "ABCD-EFGH",
			DeviceCode:              "device123",
			State:                   "state456",
			ExpiresIn:               300,
			Interval:                5,
		})
	}))
	defer server.Close()

	client := NewCLIProxyManagementClient(server.URL, "test-key")
	resp, err := client.InitiateDeviceFlow("qwen")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "https://qwen.ai/activate", resp.VerificationURI)
	assert.Equal(t, "ABCD-EFGH", resp.UserCode)
	assert.Equal(t, "state456", resp.State)
}

func TestTestConnection(t *testing.T) {
	tests := []struct {
		name           string
		mockStatusCode int
		expectError    bool
	}{
		{
			name:           "Healthy connection",
			mockStatusCode: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Unhealthy connection",
			mockStatusCode: http.StatusServiceUnavailable,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v0/management/health", r.URL.Path)
				assert.Equal(t, "GET", r.Method)
				w.WriteHeader(tt.mockStatusCode)
			}))
			defer server.Close()

			client := NewCLIProxyManagementClient(server.URL, "test-key")
			err := client.TestConnection()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
