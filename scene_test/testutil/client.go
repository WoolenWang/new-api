// Package testutil provides utilities for integration testing of the NewAPI service.
package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// APIClient provides HTTP client utilities for testing NewAPI endpoints.
type APIClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewAPIClient creates a new API client for the given test server.
func NewAPIClient(server *TestServer) *APIClient {
	return &APIClient{
		BaseURL: server.BaseURL,
		Token:   server.AdminToken,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewAPIClientWithToken creates a new API client with a specific token.
func NewAPIClientWithToken(baseURL, token string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// APIResponse represents a generic API response.
type APIResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Request performs an HTTP request and returns the response.
func (c *APIClient) Request(method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	return c.HTTPClient.Do(req)
}

// Get performs a GET request.
func (c *APIClient) Get(path string) (*http.Response, error) {
	return c.Request(http.MethodGet, path, nil)
}

// Post performs a POST request.
func (c *APIClient) Post(path string, body interface{}) (*http.Response, error) {
	return c.Request(http.MethodPost, path, body)
}

// Put performs a PUT request.
func (c *APIClient) Put(path string, body interface{}) (*http.Response, error) {
	return c.Request(http.MethodPut, path, body)
}

// Delete performs a DELETE request.
func (c *APIClient) Delete(path string) (*http.Response, error) {
	return c.Request(http.MethodDelete, path, nil)
}

// GetJSON performs a GET request and decodes the JSON response.
func (c *APIClient) GetJSON(path string, result interface{}) error {
	resp, err := c.Get(path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

// PostJSON performs a POST request and decodes the JSON response.
func (c *APIClient) PostJSON(path string, body, result interface{}) error {
	resp, err := c.Post(path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// PutJSON performs a PUT request and decodes the JSON response.
func (c *APIClient) PutJSON(path string, body, result interface{}) error {
	resp, err := c.Put(path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// DeleteJSON performs a DELETE request and decodes the JSON response.
func (c *APIClient) DeleteJSON(path string, result interface{}) error {
	resp, err := c.Delete(path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// WithToken returns a new client with the specified token.
func (c *APIClient) WithToken(token string) *APIClient {
	return &APIClient{
		BaseURL:    c.BaseURL,
		Token:      token,
		HTTPClient: c.HTTPClient,
	}
}

// --- Helper Methods for Common Test Operations ---

// LoginRequest represents the login request body.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents the login response.
type LoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    string `json:"data"` // token
}

// Login performs a login and returns the access token.
func (c *APIClient) Login(username, password string) (string, error) {
	var resp LoginResponse
	err := c.PostJSON("/api/user/login", LoginRequest{
		Username: username,
		Password: password,
	}, &resp)
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("login failed: %s", resp.Message)
	}
	return resp.Data, nil
}

// CreateUserRequest represents the user creation request.
type CreateUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Group       string `json:"group,omitempty"`
	Quota       int64  `json:"quota,omitempty"`
}

// CreateUser creates a new user (requires admin privileges).
func (c *APIClient) CreateUser(req CreateUserRequest) (int, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	err := c.PostJSON("/api/user", req, &resp)
	if err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("create user failed: %s", resp.Message)
	}
	return resp.Data.ID, nil
}

// CreateTokenRequest represents the token creation request.
type CreateTokenRequest struct {
	Name        string   `json:"name"`
	RemainQuota int64    `json:"remain_quota,omitempty"`
	ExpiredTime int64    `json:"expired_time,omitempty"`
	Models      []string `json:"models,omitempty"`
	Group       string   `json:"group,omitempty"`
	P2PGroupID  int      `json:"p2p_group_id,omitempty"`
	ModelLimits string   `json:"model_limits,omitempty"`
}

// CreateToken creates a new API token.
func (c *APIClient) CreateToken(req CreateTokenRequest) (string, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    string `json:"data"` // the token key
	}
	err := c.PostJSON("/api/token", req, &resp)
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("create token failed: %s", resp.Message)
	}
	return resp.Data, nil
}

// CreateChannelRequest represents the channel creation request.
type CreateChannelRequest struct {
	Name          string `json:"name"`
	Type          int    `json:"type"`
	Key           string `json:"key"`
	BaseURL       string `json:"base_url,omitempty"`
	Models        string `json:"models"`
	Group         string `json:"group,omitempty"`
	Priority      int    `json:"priority,omitempty"`
	Weight        int    `json:"weight,omitempty"`
	Status        int    `json:"status,omitempty"`
	AllowedGroups []int  `json:"allowed_groups,omitempty"` // P2P group IDs
}

// CreateChannel creates a new channel (requires admin privileges).
func (c *APIClient) CreateChannel(req CreateChannelRequest) (int, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	err := c.PostJSON("/api/channel", req, &resp)
	if err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("create channel failed: %s", resp.Message)
	}
	return resp.Data.ID, nil
}

// CreateGroupRequest represents the P2P group creation request.
type CreateGroupRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	OwnerID     int    `json:"owner_id"`
	Type        int    `json:"type"`        // 1=Private, 2=Shared
	JoinMethod  int    `json:"join_method"` // 0=Invite, 1=Review, 2=Password
	JoinKey     string `json:"join_key,omitempty"`
	Description string `json:"description,omitempty"`
}

// CreateGroup creates a new P2P group.
func (c *APIClient) CreateGroup(req CreateGroupRequest) (int, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	err := c.PostJSON("/api/groups", req, &resp)
	if err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("create group failed: %s", resp.Message)
	}
	return resp.Data.ID, nil
}

// ApplyToGroup applies to join a P2P group.
func (c *APIClient) ApplyToGroup(groupID, userID int, password string) error {
	var resp APIResponse
	err := c.PostJSON("/api/groups/apply", map[string]interface{}{
		"group_id": groupID,
		"user_id":  userID,
		"password": password,
	}, &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("apply to group failed: %s", resp.Message)
	}
	return nil
}

// ApproveGroupMember approves or rejects a group membership.
func (c *APIClient) ApproveGroupMember(groupID, userID, status int) error {
	var resp APIResponse
	err := c.PutJSON("/api/groups/members", map[string]interface{}{
		"group_id": groupID,
		"user_id":  userID,
		"status":   status,
	}, &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("approve member failed: %s", resp.Message)
	}
	return nil
}

// ChatCompletionRequest represents an OpenAI-compatible chat request.
type ChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

// ChatMessage represents a message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletion sends a chat completion request.
func (c *APIClient) ChatCompletion(req ChatCompletionRequest) (*http.Response, error) {
	return c.Post("/v1/chat/completions", req)
}

// GetStatus retrieves the server status.
func (c *APIClient) GetStatus() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := c.GetJSON("/api/status", &result)
	return result, err
}
