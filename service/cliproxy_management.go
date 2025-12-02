package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// CLIProxyManagementClient CLIProxyAPI 管理接口客户端
// 用于调用 CLIProxyAPI 的 /v0/management/* 端点
type CLIProxyManagementClient struct {
	BaseURL string
	APIKey  string
	client  *http.Client
}

// NewCLIProxyManagementClient 创建管理客户端
func NewCLIProxyManagementClient(baseURL, apiKey string) *CLIProxyManagementClient {
	return &CLIProxyManagementClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// OAuthURLRequest 获取 OAuth URL 的请求参数
type OAuthURLRequest struct {
	Provider    string `json:"provider"`
	State       string `json:"state"`
	RedirectURI string `json:"redirect_uri"`
}

// OAuthURLResponse OAuth URL 响应
type OAuthURLResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
}

// OAuthCompleteRequest 完成 OAuth 的请求参数
type OAuthCompleteRequest struct {
	Provider       string `json:"provider"`
	Code           string `json:"code"`
	State          string `json:"state"`
	UserIdentifier string `json:"user_identifier"`
}

// AuthStatusResponse 授权状态响应
type AuthStatusResponse struct {
	Status  string `json:"status"` // "success", "pending", "failed"
	Message string `json:"message,omitempty"`
}

// GetOAuthAuthURL 获取 OAuth 授权 URL
// 对应 CLIProxyAPI 的 POST /v0/management/oauth/generate-url
func (c *CLIProxyManagementClient) GetOAuthAuthURL(provider, state, redirectURI string) (*OAuthURLResponse, error) {
	url := fmt.Sprintf("%s/v0/management/oauth/generate-url", c.BaseURL)

	reqBody := OAuthURLRequest{
		Provider:    provider,
		State:       state,
		RedirectURI: redirectURI,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CLIProxy returned status %d: %s", resp.StatusCode, string(body))
	}

	var result OAuthURLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetAuthStatus 查询 OAuth 授权状态
// 对应 CLIProxyAPI 的 GET /v0/management/get-auth-status
func (c *CLIProxyManagementClient) GetAuthStatus(state string) (*AuthStatusResponse, error) {
	url := fmt.Sprintf("%s/v0/management/get-auth-status?state=%s", c.BaseURL, state)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CLIProxy returned status %d: %s", resp.StatusCode, string(body))
	}

	var result AuthStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// CompleteOAuth 完成 OAuth 令牌交换
// 对应 CLIProxyAPI 的 POST /v0/management/oauth/complete
func (c *CLIProxyManagementClient) CompleteOAuth(provider, code, state, userIdentifier string) error {
	url := fmt.Sprintf("%s/v0/management/oauth/complete", c.BaseURL)

	reqBody := OAuthCompleteRequest{
		Provider:       provider,
		Code:           code,
		State:          state,
		UserIdentifier: userIdentifier,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CLIProxy returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteAuthFile 删除凭证文件
// 对应 CLIProxyAPI 的 DELETE /v0/management/auth-files
func (c *CLIProxyManagementClient) DeleteAuthFile(filename string) error {
	url := fmt.Sprintf("%s/v0/management/auth-files?name=%s", c.BaseURL, filename)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CLIProxy returned status %d: %s", resp.StatusCode, string(body))
	}

	// 404 Not Found is acceptable (file already deleted)
	return nil
}

// InitiateDeviceFlow 启动设备授权流程（用于 Qwen 等）
// 对应 CLIProxyAPI 的 POST /v0/management/oauth/device-flow/initiate
func (c *CLIProxyManagementClient) InitiateDeviceFlow(provider string) (*DeviceFlowResponse, error) {
	url := fmt.Sprintf("%s/v0/management/oauth/device-flow/initiate", c.BaseURL)

	reqBody := map[string]string{
		"provider": provider,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CLIProxy returned status %d: %s", resp.StatusCode, string(body))
	}

	var result DeviceFlowResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeviceFlowResponse 设备授权流程响应
type DeviceFlowResponse struct {
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	UserCode                string `json:"user_code"`
	DeviceCode              string `json:"device_code"`
	State                   string `json:"state"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// ImportCredential 导入凭证（用于 Cookie、Service Account 等）
// 对应 CLIProxyAPI 的 POST /v0/management/import-credential
func (c *CLIProxyManagementClient) ImportCredential(provider, credType, credential, userIdentifier string) error {
	url := fmt.Sprintf("%s/v0/management/import-credential", c.BaseURL)

	reqBody := map[string]string{
		"provider":        provider,
		"type":            credType, // "cookie", "service_account", etc.
		"credential":      credential,
		"user_identifier": userIdentifier,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CLIProxy returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteCLIProxyCredential 删除 CLIProxyAPI 凭证的辅助函数
// 用于在删除 NewAPI 渠道时同步删除 CLIProxyAPI 中的凭证文件
func DeleteCLIProxyCredential(baseURL, apiKey, accountHint string) error {
	if baseURL == "" || apiKey == "" || accountHint == "" {
		return fmt.Errorf("baseURL, apiKey and accountHint are required")
	}

	client := NewCLIProxyManagementClient(baseURL, apiKey)
	err := client.DeleteAuthFile(accountHint)
	if err != nil {
		common.SysLog(fmt.Sprintf("CLIProxy credential deletion failed but will continue: baseURL=%s, accountHint=%s, error=%v",
			baseURL, accountHint, err))
		// 删除失败不阻塞 NewAPI 渠道删除，仅记录日志
		return err
	}

	common.SysLog(fmt.Sprintf("Successfully deleted CLIProxy credential: baseURL=%s, accountHint=%s", baseURL, accountHint))
	return nil
}

// TestCLIProxyConnection 测试 CLIProxyAPI 连接
// 对应 CLIProxyAPI 的 GET /v0/management/health 或类似端点
func (c *CLIProxyManagementClient) TestConnection() error {
	url := fmt.Sprintf("%s/v0/management/health", c.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("CLIProxy health check failed, status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ListAuthFiles 列出所有凭证文件
// 对应 CLIProxyAPI 的 GET /v0/management/auth-files
func (c *CLIProxyManagementClient) ListAuthFiles() ([]string, error) {
	url := fmt.Sprintf("%s/v0/management/auth-files", c.BaseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("CLIProxy returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Files []string `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Files, nil
}
