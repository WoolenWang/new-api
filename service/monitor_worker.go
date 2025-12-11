package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// MonitorWorker 监控执行 Worker
// 负责实际执行监控任务：探测渠道、评估结果、存储数据
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: MS3-2 探测 Worker (渠道选择+请求发起)
type MonitorWorker struct {
	httpClient *http.Client
	evaluator  *MonitorEvaluator
}

// NewMonitorWorker 创建监控 Worker 实例
func NewMonitorWorker() *MonitorWorker {
	return &MonitorWorker{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		evaluator: NewMonitorEvaluator(),
	}
}

// ExecuteMonitoring 执行单次监控任务
// 包含完整的监控流程：加载基准 -> 探测渠道 -> 评估 -> 存储结果
func (w *MonitorWorker) ExecuteMonitoring(ctx context.Context, channelId int, modelName, testType, evaluationStandard string, policyId int) error {
	common.SysLog(fmt.Sprintf("Starting monitoring: channel=%d, model=%s, test=%s, standard=%s, policy=%d",
		channelId, modelName, testType, evaluationStandard, policyId))

	// 1. 加载模型基准
	baseline, err := model.GetModelBaseline(modelName, testType, evaluationStandard)
	if err != nil {
		return fmt.Errorf("failed to get baseline for model %s, test %s: %w", modelName, testType, err)
	}

	// 2. 获取渠道信息
	channel, err := model.GetChannelById(channelId, true)
	if err != nil {
		return fmt.Errorf("failed to get channel %d: %w", channelId, err)
	}

	// 检查渠道状态
	if channel.Status != common.ChannelStatusEnabled {
		common.SysLog(fmt.Sprintf("Channel %d is not enabled, skipping monitoring", channelId))
		return nil
	}

	// 2.5 获取策略的阈值配置（如果有策略ID）
	var customThreshold *float64
	if policyId > 0 {
		policy, err := model.GetMonitorPolicyById(policyId)
		if err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to get policy %d, using default threshold: %v", policyId, err))
		} else {
			customThreshold = policy.GetThresholdForStandard(evaluationStandard)
			if customThreshold != nil {
				common.SysLog(fmt.Sprintf("Using policy %d custom threshold for %s: %.2f", policyId, evaluationStandard, *customThreshold))
			}
		}
	}

	// 3. 发起探测请求（带重试）
	rawOutput, probeErr := w.probeChannelWithRetry(ctx, channel, modelName, baseline.Prompt, 3)

	// 4. 评估结果
	var result *model.ModelMonitoringResult
	if probeErr != nil {
		// 探测失败，记录 monitor_failed 状态
		reason := fmt.Sprintf("Probe failed: %v", probeErr)
		result = &model.ModelMonitoringResult{
			ChannelId:          channelId,
			ModelName:          modelName,
			BaselineId:         baseline.Id,
			TestType:           testType,
			TestTimestamp:      time.Now().Unix(),
			Status:             "monitor_failed",
			Reason:             &reason,
			EvaluationStandard: evaluationStandard,
			PolicyId:           policyId,
		}
	} else {
		// 探测成功，进行评估（传递自定义阈值）
		evalResult, err := w.evaluator.Evaluate(ctx, testType, evaluationStandard, baseline.BaselineOutput, rawOutput, customThreshold)
		if err != nil {
			// 评估失败，也记录为 monitor_failed
			reason := fmt.Sprintf("Evaluation failed: %v", err)
			result = &model.ModelMonitoringResult{
				ChannelId:          channelId,
				ModelName:          modelName,
				BaselineId:         baseline.Id,
				TestType:           testType,
				TestTimestamp:      time.Now().Unix(),
				Status:             "monitor_failed",
				Reason:             &reason,
				RawOutput:          &rawOutput,
				EvaluationStandard: evaluationStandard,
				PolicyId:           policyId,
			}
		} else {
			// 评估成功，根据评估结果设置状态
			result = &model.ModelMonitoringResult{
				ChannelId:          channelId,
				ModelName:          modelName,
				BaselineId:         baseline.Id,
				TestType:           testType,
				TestTimestamp:      time.Now().Unix(),
				Status:             evalResult.Status,
				DiffScore:          evalResult.DiffScore,
				SimilarityScore:    evalResult.SimilarityScore,
				Reason:             &evalResult.Reason,
				RawOutput:          &rawOutput,
				EvaluationStandard: evaluationStandard,
				PolicyId:           policyId,
			}
		}
	}

	// 5. 异步存储结果
	if err := w.saveResultAsync(result); err != nil {
		common.SysLog(fmt.Sprintf("Failed to save monitoring result: %v", err))
		return err
	}

	common.SysLog(fmt.Sprintf("Monitoring completed: channel=%d, model=%s, status=%s",
		channelId, modelName, result.Status))

	return nil
}

// probeChannelWithRetry 带重试的渠道探测
// 使用指数退避策略重试失败的探测请求
func (w *MonitorWorker) probeChannelWithRetry(ctx context.Context, channel *model.Channel, modelName, prompt string, maxRetries int) (string, error) {
	var lastErr error
	var errorsCollected []string

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避: 1s, 2s, 4s, 8s...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			common.SysLog(fmt.Sprintf("Retrying probe for channel %d (attempt %d/%d) after %v (last error: %v)",
				channel.Id, attempt+1, maxRetries, backoff, lastErr))

			// 使用 context-aware sleep，支持提前取消
			select {
			case <-time.After(backoff):
				// 继续
			case <-ctx.Done():
				return "", fmt.Errorf("probe cancelled: %w", ctx.Err())
			}
		}

		// 为每次探测创建带超时的子 context（60秒超时）
		probeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		output, err := w.probeChannel(probeCtx, channel, modelName, prompt)
		cancel() // 及时释放资源

		if err == nil {
			if attempt > 0 {
				common.SysLog(fmt.Sprintf("Probe succeeded for channel %d after %d attempts", channel.Id, attempt+1))
			}
			return output, nil
		}

		lastErr = err
		errorsCollected = append(errorsCollected, fmt.Sprintf("Attempt %d: %v", attempt+1, err))
	}

	// 所有重试都失败，返回详细错误信息
	return "", fmt.Errorf("probe failed after %d attempts. Errors: %v", maxRetries, errorsCollected)
}

// probeChannel 探测渠道
// 发送测试 Prompt 并获取模型响应
func (w *MonitorWorker) probeChannel(ctx context.Context, channel *model.Channel, modelName, prompt string) (string, error) {
	// 构造 OpenAI 格式的请求
	requestBody := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"max_tokens":  500,
		"temperature": 0.7,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// 获取渠道 base_url
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		return "", fmt.Errorf("channel %d has no base_url", channel.Id)
	}

	// 构造完整 URL
	url := baseURL
	if url[len(url)-1] != '/' {
		url += "/"
	}
	url += "v1/chat/completions"

	// 创建 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 获取渠道 API Key
	key, _, err := channel.GetNextEnabledKey()
	if err != nil {
		// For monitoring tasks we prefer to be resilient to
		// multi-key/ChannelInfo misconfiguration. If a key is
		// present on the channel record, fall back to it so that
		// scheduled probes and tests can still run.
		if channel.Key != "" {
			common.SysLog(fmt.Sprintf("MonitorWorker: GetNextEnabledKey failed for channel %d, falling back to raw key: %v", channel.Id, err))
			key = channel.Key
		} else {
			return "", fmt.Errorf("failed to get channel key: %w", err)
		}
	}

	// 根据渠道类型设置鉴权头
	switch channel.Type {
	case 1: // OpenAI
		req.Header.Set("Authorization", "Bearer "+key)
	case 11: // Anthropic Claude
		req.Header.Set("x-api-key", key)
	case 16: // Google Gemini
		req.Header.Set("x-goog-api-key", key)
	default:
		req.Header.Set("Authorization", "Bearer "+key)
	}

	// 发送请求
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// 检查是否有错误
	if response.Error != nil {
		return "", fmt.Errorf("API error: %s", response.Error.Message)
	}

	// 提取内容
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	content := response.Choices[0].Message.Content
	if content == "" {
		return "", fmt.Errorf("empty content in response")
	}

	return content, nil
}

// saveResultAsync 异步保存监控结果
// 设计文档: Section MS3-3 探测结果异步存储服务
func (w *MonitorWorker) saveResultAsync(result *model.ModelMonitoringResult) error {
	// 使用 Goroutine 异步保存，避免阻塞主流程
	go func() {
		if err := model.CreateMonitoringResult(result); err != nil {
			common.SysLog(fmt.Sprintf("Failed to save monitoring result to database: %v", err))
		}
	}()

	return nil
}

// TriggerManualMonitoring 手动触发监控（用于测试和调试）
func (w *MonitorWorker) TriggerManualMonitoring(channelId int, modelName, testType, evaluationStandard string) error {
	ctx := context.Background()
	return w.ExecuteMonitoring(ctx, channelId, modelName, testType, evaluationStandard, 0)
}
