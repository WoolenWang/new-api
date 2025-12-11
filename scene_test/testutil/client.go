// Package testutil provides utilities for integration testing of the NewAPI service.
package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// APIClient provides HTTP client utilities for testing NewAPI endpoints.
type APIClient struct {
	BaseURL    string
	Token      string
	UserID     int // For New-Api-User header
	HTTPClient *http.Client
	// Server is the underlying test server instance when the client is
	// created via NewAPIClient. It is nil for ad-hoc clients created
	// with NewAPIClientWithToken and is used by some tests to locate
	// the SQLite database on disk.
	Server *TestServer
}

// NewAPIClient creates a new API client for the given test server.
func NewAPIClient(server *TestServer) *APIClient {
	// Create a cookie jar for session-based auth
	jar, _ := cookiejar.New(nil)
	return &APIClient{
		BaseURL: server.BaseURL,
		Token:   server.AdminToken,
		Server:  server,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}
}

// NewAPIClientWithToken creates a new API client with a specific token.
func NewAPIClientWithToken(baseURL, token string) *APIClient {
	jar, _ := cookiejar.New(nil)
	return &APIClient{
		BaseURL: baseURL,
		Token:   token,
		Server:  nil,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
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
	// Add New-Api-User header for session-based auth
	if c.UserID > 0 {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", c.UserID))
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

// WithToken returns a new client with the specified token (for API authentication).
func (c *APIClient) WithToken(token string) *APIClient {
	jar, _ := cookiejar.New(nil) // New jar for new session
	return &APIClient{
		BaseURL: c.BaseURL,
		Token:   token,
		Server:  c.Server,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar, // Fresh cookie jar
		},
	}
}

// Clone returns a new client with a fresh session (new cookie jar).
func (c *APIClient) Clone() *APIClient {
	jar, _ := cookiejar.New(nil)
	return &APIClient{
		BaseURL: c.BaseURL,
		Token:   c.Token,
		Server:  c.Server,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
	}
}

// --- Helper Methods for Common Test Operations ---

// LoginRequest represents the login request body.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents the login response (returns user data, not token).
type LoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		ID          int    `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        int    `json:"role"`
		Status      int    `json:"status"`
		Group       string `json:"group"`
	} `json:"data"`
}

// Login performs a login and stores session cookies. Returns the user ID.
// It also sets the UserID on the client for subsequent API calls.
func (c *APIClient) Login(username, password string) (int, error) {
	var resp LoginResponse
	err := c.PostJSON("/api/user/login", LoginRequest{
		Username: username,
		Password: password,
	}, &resp)
	if err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("login failed: %s", resp.Message)
	}
	// Store the user ID for New-Api-User header
	c.UserID = resp.Data.ID
	return resp.Data.ID, nil
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

// --- System Setup Methods ---

// SetupRequest represents the system setup request.
type SetupRequest struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	ConfirmPassword    string `json:"confirmPassword"`
	SelfUseModeEnabled bool   `json:"SelfUseModeEnabled"`
	DemoSiteEnabled    bool   `json:"DemoSiteEnabled"`
}

// SetupResponse represents the setup API response.
type SetupResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Status       bool   `json:"status"`
		RootInit     bool   `json:"root_init"`
		DatabaseType string `json:"database_type"`
	} `json:"data"`
}

// GetSetup retrieves the current setup status.
func (c *APIClient) GetSetup() (*SetupResponse, error) {
	var resp SetupResponse
	err := c.GetJSON("/api/setup", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// PostSetup initializes the system with root user.
func (c *APIClient) PostSetup(username, password string) error {
	var resp APIResponse
	err := c.PostJSON("/api/setup", SetupRequest{
		Username:           username,
		Password:           password,
		ConfirmPassword:    password,
		SelfUseModeEnabled: false,
		DemoSiteEnabled:    false,
	}, &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("setup failed: %s", resp.Message)
	}
	return nil
}

// InitializeSystem checks if system needs setup and initializes it.
// Returns the root user credentials used.
func (c *APIClient) InitializeSystem() (username, password string, err error) {
	// Check current setup status
	setup, err := c.GetSetup()
	if err != nil {
		return "", "", fmt.Errorf("failed to get setup status: %w", err)
	}

	// If already set up, just return default credentials
	if setup.Data.Status {
		return "root", "rootpass123", nil
	}

	// Need to initialize
	username = "root"
	password = "rootpass123"
	if err := c.PostSetup(username, password); err != nil {
		return "", "", fmt.Errorf("failed to setup system: %w", err)
	}

	return username, password, nil
}

// --- Extended API Methods for Testing ---

// ChannelModel wraps channel data for API requests
type ChannelModel struct {
	ID                int     `json:"id,omitempty"`
	Type              int     `json:"type"`
	Key               string  `json:"key"`
	Name              string  `json:"name"`
	BaseURL           *string `json:"base_url,omitempty"`
	Models            string  `json:"models"`
	Group             string  `json:"group,omitempty"`
	Priority          *int64  `json:"priority,omitempty"`
	Weight            *uint   `json:"weight,omitempty"`
	Status            int     `json:"status,omitempty"`
	OwnerUserId       int     `json:"owner_user_id,omitempty"`
	IsPrivate         bool    `json:"is_private,omitempty"`
	AllowedUsers      *string `json:"allowed_users,omitempty"`
	AllowedGroups     *string `json:"allowed_groups,omitempty"`
	HourlyQuotaLimit  int64   `json:"hourly_quota_limit,omitempty"`
	DailyQuotaLimit   int64   `json:"daily_quota_limit,omitempty"`
	WeeklyQuotaLimit  int64   `json:"weekly_quota_limit,omitempty"`
	MonthlyQuotaLimit int64   `json:"monthly_quota_limit,omitempty"`
}

// AddChannelRequest matches the backend's AddChannelRequest structure
type AddChannelRequest struct {
	Mode    string        `json:"mode,omitempty"`
	Channel *ChannelModel `json:"channel"`
}

// AddChannel creates a new channel using the admin API.
func (c *APIClient) AddChannel(channel *ChannelModel) (int, error) {
	req := AddChannelRequest{
		Mode:    "single", // Use single mode for one channel with one key
		Channel: channel,
	}

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	err := c.PostJSON("/api/channel/", req, &resp)
	if err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("add channel failed: %s", resp.Message)
	}

	// The API doesn't return the ID directly, we need to query it
	// For now, return 0 and let caller handle it
	return 0, nil
}

// AddChannelWithResponse creates a channel and returns full response for inspection
func (c *APIClient) AddChannelWithResponse(channel *ChannelModel) (*http.Response, error) {
	req := AddChannelRequest{
		Channel: channel,
	}
	return c.Post("/api/channel/", req)
}

// GetAllChannels retrieves all channels.
func (c *APIClient) GetAllChannels() ([]ChannelModel, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Items []ChannelModel `json:"items"`
			Total int            `json:"total"`
		} `json:"data"`
	}
	err := c.GetJSON("/api/channel/?p=0&page_size=100", &resp)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get channels failed: %s", resp.Message)
	}
	return resp.Data.Items, nil
}

// DeleteChannel deletes a channel by ID.
func (c *APIClient) DeleteChannel(id int) error {
	var resp APIResponse
	err := c.DeleteJSON(fmt.Sprintf("/api/channel/%d", id), &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("delete channel failed: %s", resp.Message)
	}
	return nil
}

// UserModel represents user data
type UserModel struct {
	ID                    int    `json:"id"`
	Username              string `json:"username"`
	Password              string `json:"password,omitempty"`
	DisplayName           string `json:"display_name,omitempty"`
	Email                 string `json:"email,omitempty"`
	Group                 string `json:"group,omitempty"`
	Quota                 int64  `json:"quota,omitempty"`
	Role                  int    `json:"role,omitempty"`
	Status                int    `json:"status,omitempty"`
	ExternalId            string `json:"external_id,omitempty"`
	ShareQuota            int64  `json:"share_quota,omitempty"`
	HistoryShareQuota     int64  `json:"history_share_quota,omitempty"`
	MaxConcurrentSessions int    `json:"max_concurrent_sessions,omitempty"`
}

// CreateUserFull creates a user with full control over fields
func (c *APIClient) CreateUserFull(user *UserModel) (int, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	err := c.PostJSON("/api/user/", user, &resp)
	if err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("create user failed: %s", resp.Message)
	}
	return resp.Data.ID, nil
}

// GetUser retrieves a user by ID.
func (c *APIClient) GetUser(id int) (*UserModel, error) {
	var resp struct {
		Success bool       `json:"success"`
		Message string     `json:"message"`
		Data    *UserModel `json:"data"`
	}
	err := c.GetJSON(fmt.Sprintf("/api/user/%d", id), &resp)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get user failed: %s", resp.Message)
	}
	return resp.Data, nil
}

// UpdateUser updates a user (admin only).
func (c *APIClient) UpdateUser(user *UserModel) error {
	var resp APIResponse
	if err := c.PutJSON("/api/user/", user, &resp); err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("update user failed: %s", resp.Message)
	}
	return nil
}

// DeleteUser deletes a user by ID.
func (c *APIClient) DeleteUser(id int) error {
	var resp APIResponse
	err := c.DeleteJSON(fmt.Sprintf("/api/user/%d", id), &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("delete user failed: %s", resp.Message)
	}
	return nil
}

// AdjustUserQuota adjusts a user's quota by delta amount (requires admin).
// Delta can be positive (add quota) or negative (subtract quota).
func (c *APIClient) AdjustUserQuota(userID int, delta int) error {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			UserId   int `json:"user_id"`
			Delta    int `json:"delta"`
			NewQuota int `json:"new_quota"`
		} `json:"data"`
	}
	err := c.PostJSON("/api/user/quota/adjust", map[string]interface{}{
		"user_id": userID,
		"delta":   delta,
	}, &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("adjust quota failed: %s", resp.Message)
	}
	return nil
}

// TokenModel represents API token data
type TokenModel struct {
	ID                 int     `json:"id"`
	UserId             int     `json:"user_id"`
	Key                string  `json:"key,omitempty"`
	Name               string  `json:"name"`
	Status             int     `json:"status"`
	RemainQuota        int64   `json:"remain_quota,omitempty"`
	UnlimitedQuota     bool    `json:"unlimited_quota,omitempty"`
	ExpiredTime        int64   `json:"expired_time,omitempty"`
	Group              string  `json:"group,omitempty"`
	P2PGroupID         *int    `json:"p2p_group_id,omitempty"`
	ModelLimitsJson    string  `json:"model_limits,omitempty"`
	ModelLimitsEnabled bool    `json:"model_limits_enabled,omitempty"`
	AllowIps           *string `json:"allow_ips,omitempty"`
}

// CreateTokenFull creates a token with full control over fields.
// Returns the token key (sk-*) which can be used for API authentication.
func (c *APIClient) CreateTokenFull(token *TokenModel) (string, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	err := c.PostJSON("/api/token/", token, &resp)
	if err != nil {
		return "", err
	}
	if !resp.Success {
		return "", fmt.Errorf("create token failed: %s", resp.Message)
	}

	// The API doesn't return the key, we need to fetch the token list
	// and find the one we just created by name
	tokens, err := c.GetAllTokens()
	if err != nil {
		return "", fmt.Errorf("failed to fetch tokens after creation: %w", err)
	}

	// Find the token by name
	for _, t := range tokens {
		if t.Name == token.Name {
			return t.Key, nil
		}
	}

	return "", fmt.Errorf("token created but not found in list")
}

// GetAllTokens retrieves all tokens for the current user.
func (c *APIClient) GetAllTokens() ([]TokenModel, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Items []TokenModel `json:"items"`
			Total int          `json:"total"`
		} `json:"data"`
	}
	err := c.GetJSON("/api/token/?p=0&page_size=100", &resp)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get tokens failed: %s", resp.Message)
	}
	return resp.Data.Items, nil
}

// DeleteToken deletes a token by ID.
func (c *APIClient) DeleteToken(id int) error {
	var resp APIResponse
	err := c.DeleteJSON(fmt.Sprintf("/api/token/%d", id), &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("delete token failed: %s", resp.Message)
	}
	return nil
}

// P2PGroupModel represents a P2P group
type P2PGroupModel struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	OwnerId     int    `json:"owner_id"`
	Type        int    `json:"type"`        // 1=Private, 2=Shared
	JoinMethod  int    `json:"join_method"` // 0=Invite, 1=Review, 2=Password
	JoinKey     string `json:"join_key,omitempty"`
	Description string `json:"description,omitempty"`
}

// CreateP2PGroup creates a P2P group.
func (c *APIClient) CreateP2PGroup(group *P2PGroupModel) (int, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	err := c.PostJSON("/api/groups", group, &resp)
	if err != nil {
		return 0, err
	}
	if !resp.Success {
		return 0, fmt.Errorf("create P2P group failed: %s", resp.Message)
	}
	return resp.Data.ID, nil
}

// ApplyToP2PGroup applies to join a P2P group.
func (c *APIClient) ApplyToP2PGroup(groupID int, password string) error {
	var resp APIResponse
	err := c.PostJSON("/api/groups/apply", map[string]interface{}{
		"group_id": groupID,
		"password": password,
	}, &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("apply to P2P group failed: %s", resp.Message)
	}
	return nil
}

// UpdateMemberStatus updates a member's status in a P2P group.
// Status: 0=Pending, 1=Active, 2=Rejected, 3=Banned, 4=Left
func (c *APIClient) UpdateMemberStatus(groupID, userID, status int) error {
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
		return fmt.Errorf("update member status failed: %s", resp.Message)
	}
	return nil
}

// p2pGroupPage models the paginated response for group list APIs.
type p2pGroupPage struct {
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	Total    int             `json:"total"`
	Items    []P2PGroupModel `json:"items"`
}

// groupMemberPage models the paginated response for group member list APIs.
type groupMemberPage struct {
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int              `json:"total"`
	Items    []P2PGroupMember `json:"items"`
}

// P2PGroupMember represents a P2P group member record returned by the API.
type P2PGroupMember struct {
	ID        int   `json:"id"`
	UserID    int   `json:"user_id"`
	GroupID   int   `json:"group_id"`
	Role      int   `json:"role"`
	Status    int   `json:"status"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
	// Optional user fields (may be empty depending on query)
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// GetSelfJoinedGroups gets the current user's joined P2P groups (Active status only).
// It calls GET /api/groups/self/joined and flattens the paginated result.
func (c *APIClient) GetSelfJoinedGroups() ([]P2PGroupModel, error) {
	var resp struct {
		Success bool         `json:"success"`
		Message string       `json:"message"`
		Data    p2pGroupPage `json:"data"`
	}
	err := c.GetJSON("/api/groups/self/joined?p=1&page_size=100", &resp)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get joined groups failed: %s", resp.Message)
	}
	return resp.Data.Items, nil
}

// GetSelfOwnedGroups returns groups owned by the current user.
// It calls GET /api/groups/self/owned and returns the items list.
func (c *APIClient) GetSelfOwnedGroups() ([]P2PGroupModel, error) {
	var resp struct {
		Success bool         `json:"success"`
		Message string       `json:"message"`
		Data    p2pGroupPage `json:"data"`
	}
	err := c.GetJSON("/api/groups/self/owned?p=1&page_size=100", &resp)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get owned groups failed: %s", resp.Message)
	}
	return resp.Data.Items, nil
}

// GetGroupMembers lists members of a group, optionally filtered by status.
// Status: 0=Pending, 1=Active, 2=Rejected, 3=Banned, 4=Left. Use -1 for all.
func (c *APIClient) GetGroupMembers(groupID int, status int) ([]P2PGroupMember, error) {
	query := fmt.Sprintf("/api/groups/members?group_id=%d&p=1&page_size=100", groupID)
	if status >= 0 {
		query = fmt.Sprintf("%s&status=%d", query, status)
	}

	var resp struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    groupMemberPage `json:"data"`
	}
	if err := c.GetJSON(query, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get group members failed: %s", resp.Message)
	}
	return resp.Data.Items, nil
}

// GetGroupMemberInfo retrieves a single member relation by group and user.
func (c *APIClient) GetGroupMemberInfo(groupID, userID int) (*P2PGroupMember, error) {
	var resp struct {
		Success bool           `json:"success"`
		Message string         `json:"message"`
		Data    P2PGroupMember `json:"data"`
	}
	path := fmt.Sprintf("/api/groups/member?group_id=%d&user_id=%d", groupID, userID)
	if err := c.GetJSON(path, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("get member info failed: %s", resp.Message)
	}
	return &resp.Data, nil
}

// LeaveGroup makes the current user leave the specified group.
func (c *APIClient) LeaveGroup(groupID int) error {
	var resp APIResponse
	if err := c.PostJSON("/api/groups/leave", map[string]interface{}{
		"group_id": groupID,
	}, &resp); err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("leave group failed: %s", resp.Message)
	}
	return nil
}

// DeleteGroup deletes a P2P group by ID.
func (c *APIClient) DeleteGroup(groupID int) error {
	var resp APIResponse
	path := fmt.Sprintf("/api/groups?id=%d", groupID)
	if err := c.DeleteJSON(path, &resp); err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("delete group failed: %s", resp.Message)
	}
	return nil
}

// ChatCompletionResponse represents the response from chat completion
type ChatCompletionResponse struct {
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

// ChatCompletionError represents an error response from chat completion
type ChatCompletionError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// DoChatCompletion sends a chat completion request and returns the parsed response.
func (c *APIClient) DoChatCompletion(model, message string) (*ChatCompletionResponse, error) {
	req := ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "user", Content: message},
		},
	}

	resp, err := c.ChatCompletion(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		var errResp ChatCompletionError
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("chat completion failed: %s (type: %s, code: %s)",
				errResp.Error.Message, errResp.Error.Type, errResp.Error.Code)
		}
		return nil, fmt.Errorf("chat completion failed with status %d: %s", resp.StatusCode, string(body))
	}

	var result ChatCompletionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// TryChatCompletion attempts a chat completion and returns success status and error message.
// This is useful for testing routing - we want to know if routing succeeded, not if upstream succeeded.
func (c *APIClient) TryChatCompletion(model, message string) (success bool, statusCode int, errMsg string) {
	req := ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "user", Content: message},
		},
	}

	resp, err := c.ChatCompletion(req)
	if err != nil {
		return false, 0, err.Error()
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	statusCode = resp.StatusCode

	if resp.StatusCode >= 400 {
		var errResp ChatCompletionError
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
			return false, statusCode, errResp.Error.Message
		}
		return false, statusCode, string(body)
	}

	return true, statusCode, ""
}
