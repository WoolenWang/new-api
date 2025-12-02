package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// InitiateCLIProxyOAuthRequest 启动 OAuth 流程的请求参数
type InitiateCLIProxyOAuthRequest struct {
	ChannelID   int    `json:"channel_id" binding:"required"`   // 渠道ID（必须是CLIProxy类型）
	Provider    string `json:"provider" binding:"required"`     // OAuth 提供商 (gemini, claude, codex, qwen)
	RedirectURI string `json:"redirect_uri" binding:"required"` // WQuant 后端的回调地址
}

// InitiateCLIProxyOAuthResponse 启动 OAuth 流程的响应
type InitiateCLIProxyOAuthResponse struct {
	AuthURL string `json:"auth_url"` // 用户应该访问的授权 URL
	State   string `json:"state"`    // 状态参数，用于后续验证
}

// GetCLIProxyOAuthStatusRequest 查询 OAuth 状态的请求参数
type GetCLIProxyOAuthStatusRequest struct {
	State string `form:"state" binding:"required"` // OAuth 状态参数
}

// GetCLIProxyOAuthStatusResponse OAuth 状态响应
type GetCLIProxyOAuthStatusResponse struct {
	Status  string `json:"status"`            // "pending", "success", "failed"
	Message string `json:"message,omitempty"` // 错误信息（如果失败）
}

// CompleteCLIProxyOAuthRequest 完成 OAuth 的请求参数
type CompleteCLIProxyOAuthRequest struct {
	ChannelID      int    `json:"channel_id" binding:"required"`      // 渠道ID
	Provider       string `json:"provider" binding:"required"`        // OAuth 提供商
	Code           string `json:"code" binding:"required"`            // 授权码
	State          string `json:"state" binding:"required"`           // 状态参数
	UserIdentifier string `json:"user_identifier" binding:"required"` // 用户标识符（用于生成凭证文件名）
	AccountHint    string `json:"account_hint"`                       // 可选：指定凭证文件名，不指定则自动生成
}

// InitiateCLIProxyOAuth 启动 CLIProxy OAuth 流程
// POST /api/channel/oauth/initiate
// 用于获取 OAuth 授权 URL，返回给前端让用户在浏览器中打开
func InitiateCLIProxyOAuth(c *gin.Context) {
	var req InitiateCLIProxyOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		common.ApiError(c, fmt.Errorf("channel not found: %w", err))
		return
	}

	// 验证是 CLIProxy 类型
	if channel.Type != constant.ChannelTypeCliProxy {
		common.ApiError(c, errors.New("channel is not CLIProxy type"))
		return
	}

	// 验证权限：管理员或渠道所有者
	userId := c.GetInt("id")
	userRole := c.GetInt("role")
	if userRole != common.RoleRootUser && channel.OwnerUserId != userId {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权操作此渠道",
		})
		return
	}

	// 检查渠道配置
	if channel.BaseURL == nil || *channel.BaseURL == "" {
		common.ApiError(c, errors.New("channel BaseURL is not configured"))
		return
	}

	// 创建 CLIProxy 管理客户端
	cliproxyClient := service.NewCLIProxyManagementClient(*channel.BaseURL, channel.Key)

	// 生成 state（由 WQuant 后端生成并管理）
	state := common.GetRandomString(32)

	// 调用 CLIProxyAPI 获取授权 URL
	oauthResp, err := cliproxyClient.GetOAuthAuthURL(req.Provider, state, req.RedirectURI)
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to get OAuth URL from CLIProxy: %w", err))
		return
	}

	// 返回授权 URL 和 state
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": InitiateCLIProxyOAuthResponse{
			AuthURL: oauthResp.AuthURL,
			State:   oauthResp.State,
		},
	})
}

// GetCLIProxyOAuthStatus 查询 OAuth 授权状态
// GET /api/channel/oauth/status?state=xxx
// WQuant 前端轮询此接口，检查用户是否已完成授权
func GetCLIProxyOAuthStatus(c *gin.Context) {
	var req GetCLIProxyOAuthStatusRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	// 从 query 参数获取 channel_id
	channelID := c.GetInt("channel_id")
	if channelID == 0 {
		common.ApiError(c, errors.New("channel_id is required"))
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		common.ApiError(c, fmt.Errorf("channel not found: %w", err))
		return
	}

	// 验证是 CLIProxy 类型
	if channel.Type != constant.ChannelTypeCliProxy {
		common.ApiError(c, errors.New("channel is not CLIProxy type"))
		return
	}

	// 验证权限
	userId := c.GetInt("id")
	userRole := c.GetInt("role")
	if userRole != common.RoleRootUser && channel.OwnerUserId != userId {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权操作此渠道",
		})
		return
	}

	if channel.BaseURL == nil || *channel.BaseURL == "" {
		common.ApiError(c, errors.New("channel BaseURL is not configured"))
		return
	}

	// 创建 CLIProxy 管理客户端
	cliproxyClient := service.NewCLIProxyManagementClient(*channel.BaseURL, channel.Key)

	// 查询授权状态
	statusResp, err := cliproxyClient.GetAuthStatus(req.State)
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to get auth status from CLIProxy: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": GetCLIProxyOAuthStatusResponse{
			Status:  statusResp.Status,
			Message: statusResp.Message,
		},
	})
}

// CompleteCLIProxyOAuth 完成 OAuth 流程并更新渠道
// POST /api/channel/oauth/complete
// WQuant 后端在收到 OAuth 回调后，调用此接口完成令牌交换和凭证保存
func CompleteCLIProxyOAuth(c *gin.Context) {
	var req CompleteCLIProxyOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		common.ApiError(c, fmt.Errorf("channel not found: %w", err))
		return
	}

	// 验证是 CLIProxy 类型
	if channel.Type != constant.ChannelTypeCliProxy {
		common.ApiError(c, errors.New("channel is not CLIProxy type"))
		return
	}

	// 验证权限
	userId := c.GetInt("id")
	userRole := c.GetInt("role")
	if userRole != common.RoleRootUser && channel.OwnerUserId != userId {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权操作此渠道",
		})
		return
	}

	if channel.BaseURL == nil || *channel.BaseURL == "" {
		common.ApiError(c, errors.New("channel BaseURL is not configured"))
		return
	}

	// 创建 CLIProxy 管理客户端
	cliproxyClient := service.NewCLIProxyManagementClient(*channel.BaseURL, channel.Key)

	// 调用 CLIProxyAPI 完成 OAuth
	err = cliproxyClient.CompleteOAuth(req.Provider, req.Code, req.State, req.UserIdentifier)
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to complete OAuth with CLIProxy: %w", err))
		return
	}

	// 更新渠道的 AccountHint
	// 如果请求中指定了 AccountHint，使用指定的；否则根据 provider 和 userIdentifier 生成
	accountHint := req.AccountHint
	if accountHint == "" {
		accountHint = fmt.Sprintf("%s_%s.json", req.Provider, req.UserIdentifier)
	}

	channel.AccountHint = &accountHint
	err = channel.Update()
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to update channel AccountHint: %w", err))
		return
	}

	common.SysLog(fmt.Sprintf("CLIProxy OAuth completed: channel_id=%d, provider=%s, account_hint=%s",
		req.ChannelID, req.Provider, accountHint))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "OAuth 认证完成",
		"data": gin.H{
			"account_hint": accountHint,
		},
	})
}

// InitiateDeviceFlowRequest 启动设备授权流程的请求参数
type InitiateDeviceFlowRequest struct {
	ChannelID int    `json:"channel_id" binding:"required"` // 渠道ID
	Provider  string `json:"provider" binding:"required"`   // OAuth 提供商 (qwen)
}

// InitiateDeviceFlowResponse 设备授权流程响应
type InitiateDeviceFlowResponse struct {
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	UserCode                string `json:"user_code"`
	State                   string `json:"state"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// InitiateCLIProxyDeviceFlow 启动设备授权流程（用于 Qwen 等）
// POST /api/channel/oauth/device-flow/initiate
func InitiateCLIProxyDeviceFlow(c *gin.Context) {
	var req InitiateDeviceFlowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		common.ApiError(c, fmt.Errorf("channel not found: %w", err))
		return
	}

	// 验证是 CLIProxy 类型
	if channel.Type != constant.ChannelTypeCliProxy {
		common.ApiError(c, errors.New("channel is not CLIProxy type"))
		return
	}

	// 验证权限
	userId := c.GetInt("id")
	userRole := c.GetInt("role")
	if userRole != common.RoleRootUser && channel.OwnerUserId != userId {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权操作此渠道",
		})
		return
	}

	if channel.BaseURL == nil || *channel.BaseURL == "" {
		common.ApiError(c, errors.New("channel BaseURL is not configured"))
		return
	}

	// 创建 CLIProxy 管理客户端
	cliproxyClient := service.NewCLIProxyManagementClient(*channel.BaseURL, channel.Key)

	// 启动设备授权流程
	deviceResp, err := cliproxyClient.InitiateDeviceFlow(req.Provider)
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to initiate device flow with CLIProxy: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": InitiateDeviceFlowResponse{
			VerificationURI:         deviceResp.VerificationURI,
			VerificationURIComplete: deviceResp.VerificationURIComplete,
			UserCode:                deviceResp.UserCode,
			State:                   deviceResp.State,
			ExpiresIn:               deviceResp.ExpiresIn,
			Interval:                deviceResp.Interval,
		},
	})
}

// ImportCLIProxyCredentialRequest 导入凭证的请求参数（用于 Cookie、Service Account 等）
type ImportCLIProxyCredentialRequest struct {
	ChannelID      int    `json:"channel_id" binding:"required"`      // 渠道ID
	Provider       string `json:"provider" binding:"required"`        // 提供商
	CredType       string `json:"cred_type" binding:"required"`       // 凭证类型: "cookie", "service_account"
	Credential     string `json:"credential" binding:"required"`      // 凭证内容
	UserIdentifier string `json:"user_identifier" binding:"required"` // 用户标识符
	AccountHint    string `json:"account_hint"`                       // 可选：指定凭证文件名
}

// ImportCLIProxyCredential 导入凭证（用于 Cookie、Service Account JSON Key 等）
// POST /api/channel/oauth/import-credential
func ImportCLIProxyCredential(c *gin.Context) {
	var req ImportCLIProxyCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		common.ApiError(c, fmt.Errorf("channel not found: %w", err))
		return
	}

	// 验证是 CLIProxy 类型
	if channel.Type != constant.ChannelTypeCliProxy {
		common.ApiError(c, errors.New("channel is not CLIProxy type"))
		return
	}

	// 验证权限
	userId := c.GetInt("id")
	userRole := c.GetInt("role")
	if userRole != common.RoleRootUser && channel.OwnerUserId != userId {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权操作此渠道",
		})
		return
	}

	if channel.BaseURL == nil || *channel.BaseURL == "" {
		common.ApiError(c, errors.New("channel BaseURL is not configured"))
		return
	}

	// 创建 CLIProxy 管理客户端
	cliproxyClient := service.NewCLIProxyManagementClient(*channel.BaseURL, channel.Key)

	// 导入凭证
	err = cliproxyClient.ImportCredential(req.Provider, req.CredType, req.Credential, req.UserIdentifier)
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to import credential to CLIProxy: %w", err))
		return
	}

	// 更新渠道的 AccountHint
	accountHint := req.AccountHint
	if accountHint == "" {
		accountHint = fmt.Sprintf("%s_%s.json", req.Provider, req.UserIdentifier)
	}

	channel.AccountHint = &accountHint
	err = channel.Update()
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to update channel AccountHint: %w", err))
		return
	}

	common.SysLog(fmt.Sprintf("CLIProxy credential imported: channel_id=%d, provider=%s, type=%s, account_hint=%s",
		req.ChannelID, req.Provider, req.CredType, accountHint))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "凭证导入成功",
		"data": gin.H{
			"account_hint": accountHint,
		},
	})
}

// TestCLIProxyChannelRequest 测试 CLIProxy 渠道连接的请求参数
type TestCLIProxyChannelRequest struct {
	ChannelID int `json:"channel_id" binding:"required"` // 渠道ID
}

// TestCLIProxyChannel 测试 CLIProxy 渠道连接
// POST /api/channel/oauth/test
func TestCLIProxyChannel(c *gin.Context) {
	var req TestCLIProxyChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	// 获取渠道信息
	channel, err := model.GetChannelById(req.ChannelID, true)
	if err != nil {
		common.ApiError(c, fmt.Errorf("channel not found: %w", err))
		return
	}

	// 验证是 CLIProxy 类型
	if channel.Type != constant.ChannelTypeCliProxy {
		common.ApiError(c, errors.New("channel is not CLIProxy type"))
		return
	}

	// 验证权限
	userId := c.GetInt("id")
	userRole := c.GetInt("role")
	if userRole != common.RoleRootUser && channel.OwnerUserId != userId {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权操作此渠道",
		})
		return
	}

	if channel.BaseURL == nil || *channel.BaseURL == "" {
		common.ApiError(c, errors.New("channel BaseURL is not configured"))
		return
	}

	// 创建 CLIProxy 管理客户端
	cliproxyClient := service.NewCLIProxyManagementClient(*channel.BaseURL, channel.Key)

	// 测试连接
	err = cliproxyClient.TestConnection()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("CLIProxy 连接测试失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "CLIProxy 连接正常",
	})
}
