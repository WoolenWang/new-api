package cliproxy

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// Adaptor implements the channel.Adaptor interface for CLIProxyAPI channels
type Adaptor struct {
}

// Init initializes the adaptor
func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	// No special initialization needed for CLIProxyAPI adaptor
}

// GetRequestURL returns the request URL for the channel
func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	// For CLIProxyAPI, use the base URL from channel config
	return info.BaseUrl, nil
}

// SetupRequestHeader sets up the request headers for CLIProxyAPI
// The key implementation: adds X-CLIProxy-Account-Hint header from channel's AccountHint field
func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	// Call common header setup
	channel.SetupApiRequestHeader(info, c, header)

	// Set standard authorization header
	header.Set("Authorization", "Bearer "+info.ApiKey)

	// Set CLIProxyAPI specific header: X-CLIProxy-Account-Hint
	// This tells CLIProxyAPI which OAuth credential to use
	if info.AccountHint != "" {
		header.Set("X-CLIProxy-Account-Hint", info.AccountHint)
	}

	return nil
}

// ConvertRequest converts the request to CLIProxyAPI format
// For CLIProxyAPI, we don't need special request conversion as it supports standard OpenAI/Claude/Gemini formats
func (a *Adaptor) ConvertRequest(c *gin.Context, info *relaycommon.RelayInfo, request *relaycommon.GeneralOpenAIRequest) (any, error) {
	// CLIProxyAPI supports standard OpenAI/Claude/Gemini formats directly
	// No conversion needed
	return request, nil
}

// DoRequest performs the actual HTTP request to CLIProxyAPI
func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

// DoResponse handles the response from CLIProxyAPI
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage *relaycommon.Usage, err *relaycommon.ErrorWithStatusCode) {
	// CLIProxyAPI returns standard OpenAI/Claude/Gemini format responses
	// Use common response handler
	return channel.DoCommonResponse(c, resp, info)
}

// GetModelList returns the list of models supported by this channel
func (a *Adaptor) GetModelList() []string {
	// CLIProxyAPI supports all models configured in its backend
	// Model list is managed at channel configuration level
	return nil
}

// GetChannelName returns the channel name
func (a *Adaptor) GetChannelName() string {
	return "CLIProxyAPI"
}
