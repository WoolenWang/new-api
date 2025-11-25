package cliproxy

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
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
	// For CLIProxyAPI, delegate to OpenAI adaptor since it uses OpenAI-compatible format
	openaiAdaptor := &openai.Adaptor{}
	return openaiAdaptor.GetRequestURL(info)
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

// ConvertOpenAIRequest converts the request to CLIProxyAPI format
func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	// CLIProxyAPI supports standard OpenAI format directly
	// No conversion needed
	return request, nil
}

// ConvertRerankRequest converts rerank requests
func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("rerank not implemented for CLIProxyAPI")
}

// ConvertEmbeddingRequest converts embedding requests
func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return request, nil
}

// ConvertAudioRequest converts audio requests
func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("audio not implemented for CLIProxyAPI")
}

// ConvertImageRequest converts image requests
func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return request, nil
}

// ConvertOpenAIResponsesRequest converts OpenAI responses requests
func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("OpenAI responses not implemented for CLIProxyAPI")
}

// ConvertClaudeRequest converts Claude requests
func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return request, nil
}

// ConvertGeminiRequest converts Gemini requests
func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return request, nil
}

// DoRequest performs the actual HTTP request to CLIProxyAPI
func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

// DoResponse handles the response from CLIProxyAPI
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	// CLIProxyAPI returns standard OpenAI/Claude/Gemini format responses
	// Delegate to OpenAI adaptor for response handling
	openaiAdaptor := &openai.Adaptor{}
	return openaiAdaptor.DoResponse(c, resp, info)
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
