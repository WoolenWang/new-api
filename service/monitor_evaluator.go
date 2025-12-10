package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

// EvaluationResult 评估结果
type EvaluationResult struct {
	Status          string  `json:"status"`           // "pass" 或 "fail"
	DiffScore       float64 `json:"diff_score"`       // 差异得分 (0-100, 0表示完全一致)
	SimilarityScore float64 `json:"similarity_score"` // 相似度得分 (0-100, 100表示完全一致)
	Reason          string  `json:"reason"`           // 评估原因说明
}

// MonitorEvaluator 监控评估器
// 负责对比基准输出和实际输出，判断渠道模型质量
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: MS4 评估引擎 (规则+LLM Judge)
type MonitorEvaluator struct {
	httpClient *http.Client
	judgeModel string // LLM Judge 使用的模型
	judgeURL   string // LLM Judge API URL
	judgeKey   string // LLM Judge API Key
}

// NewMonitorEvaluator 创建评估器实例
func NewMonitorEvaluator() *MonitorEvaluator {
	// 默认使用本地部署的 OpenAI 兼容服务作为 Judge
	// 可通过环境变量配置
	judgeURL := common.GetEnvOrDefaultString("MONITOR_JUDGE_URL", "http://localhost:3000/v1/chat/completions")
	judgeKey := common.GetEnvOrDefaultString("MONITOR_JUDGE_KEY", "")
	judgeModel := common.GetEnvOrDefaultString("MONITOR_JUDGE_MODEL", "gpt-4o-mini")

	return &MonitorEvaluator{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		judgeModel: judgeModel,
		judgeURL:   judgeURL,
		judgeKey:   judgeKey,
	}
}

// Evaluate 执行评估
// testType: 检测类型 (encoding, structure_consistency, style, reasoning, instruction_following)
// evaluationStandard: 评估标准 (strict, standard, lenient)
// baselineOutput: 基准输出（可信渠道的输出）
// rawOutput: 待评估输出（被测渠道的输出）
// customThreshold: 可选的自定义阈值（nil则使用环境变量或硬编码默认值）
func (e *MonitorEvaluator) Evaluate(ctx context.Context, testType, evaluationStandard, baselineOutput, rawOutput string, customThreshold *float64) (*EvaluationResult, error) {
	common.SysLog(fmt.Sprintf("Starting evaluation: testType=%s, standard=%s", testType, evaluationStandard))

	// 根据检测类型选择评估方法
	var result *EvaluationResult
	var err error

	switch testType {
	case "encoding":
		result, err = e.evaluateEncoding(baselineOutput, rawOutput)
	case "structure_consistency":
		result, err = e.evaluateStructure(baselineOutput, rawOutput)
	case "style", "reasoning", "instruction_following":
		result, err = e.evaluateWithLLMJudge(ctx, testType, baselineOutput, rawOutput)
	default:
		return nil, fmt.Errorf("unsupported test type: %s", testType)
	}

	if err != nil {
		return nil, fmt.Errorf("evaluation failed: %w", err)
	}

	// 应用评估标准阈值
	result.Status = e.applyEvaluationStandard(evaluationStandard, result.SimilarityScore, result.DiffScore, customThreshold)

	common.SysLog(fmt.Sprintf("Evaluation completed: status=%s, similarity=%.2f, diff=%.2f",
		result.Status, result.SimilarityScore, result.DiffScore))

	return result, nil
}

// evaluateEncoding 评估编码一致性（规则评估）
// 检查字符编码、乱码等问题
func (e *MonitorEvaluator) evaluateEncoding(baselineOutput, rawOutput string) (*EvaluationResult, error) {
	// 检查是否为有效的 UTF-8 编码
	baselineValid := utf8.ValidString(baselineOutput)
	rawValid := utf8.ValidString(rawOutput)

	// 计算差异得分
	diffScore := 0.0
	reason := "Encoding is valid"

	if !rawValid {
		diffScore = 100.0
		reason = "Output contains invalid UTF-8 encoding (乱码)"
	} else if !baselineValid {
		// 基准本身有问题，不应该发生
		return nil, fmt.Errorf("baseline output has invalid encoding")
	}

	// 检查特殊字符异常（如大量不可打印字符）
	unprintableCount := 0
	for _, r := range rawOutput {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			unprintableCount++
		}
	}

	if unprintableCount > len(rawOutput)/20 { // 超过5%
		diffScore = 80.0
		reason = fmt.Sprintf("Output contains too many unprintable characters (%d)", unprintableCount)
	}

	// 相似度得分 = 100 - 差异得分
	similarityScore := 100.0 - diffScore

	return &EvaluationResult{
		Status:          "", // Will be set by applyEvaluationStandard
		DiffScore:       diffScore,
		SimilarityScore: similarityScore,
		Reason:          reason,
	}, nil
}

// evaluateStructure 评估结构一致性（规则评估）
// 对于 JSON 输出，检查结构是否一致
func (e *MonitorEvaluator) evaluateStructure(baselineOutput, rawOutput string) (*EvaluationResult, error) {
	// 尝试解析为 JSON
	var baselineJSON, rawJSON interface{}

	errBaseline := json.Unmarshal([]byte(baselineOutput), &baselineJSON)
	errRaw := json.Unmarshal([]byte(rawOutput), &rawJSON)

	// 如果都不是 JSON，则按文本结构比较
	if errBaseline != nil && errRaw != nil {
		return e.evaluateTextStructure(baselineOutput, rawOutput)
	}

	// 如果一个是 JSON 另一个不是，则结构不一致
	if (errBaseline == nil && errRaw != nil) || (errBaseline != nil && errRaw == nil) {
		return &EvaluationResult{
			Status:          "",
			DiffScore:       100.0,
			SimilarityScore: 0.0,
			Reason:          "One output is JSON while the other is not",
		}, nil
	}

	// 都是 JSON，比较结构
	structureSimilarity := e.compareJSONStructure(baselineJSON, rawJSON)
	diffScore := 100.0 - structureSimilarity

	reason := "JSON structure is consistent"
	if structureSimilarity < 90 {
		reason = fmt.Sprintf("JSON structure differs (similarity: %.1f%%)", structureSimilarity)
	}

	return &EvaluationResult{
		Status:          "",
		DiffScore:       diffScore,
		SimilarityScore: structureSimilarity,
		Reason:          reason,
	}, nil
}

// evaluateTextStructure 评估文本结构
func (e *MonitorEvaluator) evaluateTextStructure(baselineOutput, rawOutput string) (*EvaluationResult, error) {
	// 简单的文本结构比较：行数、段落数、长度比例
	baselineLines := strings.Split(baselineOutput, "\n")
	rawLines := strings.Split(rawOutput, "\n")

	lineDiff := float64(abs(len(baselineLines) - len(rawLines)))
	lengthDiff := float64(abs(len(baselineOutput) - len(rawOutput)))

	maxLines := float64(max(len(baselineLines), len(rawLines)))
	maxLength := float64(max(len(baselineOutput), len(rawOutput)))

	// 计算结构相似度（行数相似度 50% + 长度相似度 50%）
	lineSimilarity := 100.0
	if maxLines > 0 {
		lineSimilarity = 100.0 * (1.0 - lineDiff/maxLines)
	}

	lengthSimilarity := 100.0
	if maxLength > 0 {
		lengthSimilarity = 100.0 * (1.0 - lengthDiff/maxLength)
	}

	structureSimilarity := (lineSimilarity + lengthSimilarity) / 2.0
	diffScore := 100.0 - structureSimilarity

	return &EvaluationResult{
		Status:          "",
		DiffScore:       diffScore,
		SimilarityScore: structureSimilarity,
		Reason:          fmt.Sprintf("Text structure similarity: %.1f%% (lines: %d vs %d)", structureSimilarity, len(baselineLines), len(rawLines)),
	}, nil
}

// compareJSONStructure 比较 JSON 结构相似度
func (e *MonitorEvaluator) compareJSONStructure(baseline, raw interface{}) float64 {
	// 递归比较 JSON 结构
	switch baselineVal := baseline.(type) {
	case map[string]interface{}:
		rawMap, ok := raw.(map[string]interface{})
		if !ok {
			return 0.0
		}

		// 比较键集合
		baselineKeys := make(map[string]bool)
		for k := range baselineVal {
			baselineKeys[k] = true
		}

		rawKeys := make(map[string]bool)
		for k := range rawMap {
			rawKeys[k] = true
		}

		// 计算键的交集和并集
		intersection := 0
		union := len(baselineKeys)
		for k := range rawKeys {
			if baselineKeys[k] {
				intersection++
			} else {
				union++
			}
		}

		if union == 0 {
			return 100.0
		}

		keySimilarity := 100.0 * float64(intersection) / float64(union)

		// 递归比较值结构
		var valueSimilarities []float64
		for k := range baselineKeys {
			if rawKeys[k] {
				valueSim := e.compareJSONStructure(baselineVal[k], rawMap[k])
				valueSimilarities = append(valueSimilarities, valueSim)
			}
		}

		avgValueSimilarity := 0.0
		if len(valueSimilarities) > 0 {
			for _, sim := range valueSimilarities {
				avgValueSimilarity += sim
			}
			avgValueSimilarity /= float64(len(valueSimilarities))
		}

		// 键相似度 60% + 值相似度 40%
		return keySimilarity*0.6 + avgValueSimilarity*0.4

	case []interface{}:
		rawArray, ok := raw.([]interface{})
		if !ok {
			return 0.0
		}

		// 数组长度相似度
		lenDiff := float64(abs(len(baselineVal) - len(rawArray)))
		maxLen := float64(max(len(baselineVal), len(rawArray)))
		if maxLen == 0 {
			return 100.0
		}

		lenSimilarity := 100.0 * (1.0 - lenDiff/maxLen)

		// 元素类型相似度（简单比较第一个元素）
		typeSimilarity := 100.0
		if len(baselineVal) > 0 && len(rawArray) > 0 {
			typeSimilarity = e.compareJSONStructure(baselineVal[0], rawArray[0])
		}

		return (lenSimilarity + typeSimilarity) / 2.0

	default:
		// 基本类型，检查类型是否一致
		if fmt.Sprintf("%T", baseline) == fmt.Sprintf("%T", raw) {
			return 100.0
		}
		return 0.0
	}
}

// evaluateWithLLMJudge 使用 LLM Judge 评估
// 用于 style, reasoning, instruction_following 等主观质量检测
func (e *MonitorEvaluator) evaluateWithLLMJudge(ctx context.Context, testType, baselineOutput, rawOutput string) (*EvaluationResult, error) {
	// 构造评估 Prompt
	judgePrompt := e.buildJudgePrompt(testType, baselineOutput, rawOutput)

	// 调用 LLM Judge（带重试）
	judgeResponse, err := e.callLLMJudgeWithRetry(ctx, judgePrompt, 3)
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM judge after retries: %w", err)
	}

	// 解析 Judge 响应
	result, err := e.parseJudgeResponse(judgeResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to parse judge response: %w", err)
	}

	return result, nil
}

// callLLMJudgeWithRetry 带重试的 LLM Judge 调用
func (e *MonitorEvaluator) callLLMJudgeWithRetry(ctx context.Context, prompt string, maxRetries int) (string, error) {
	var lastErr error
	var errorsCollected []string

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避: 1s, 2s, 4s...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			common.SysLog(fmt.Sprintf("Retrying LLM judge call (attempt %d/%d) after %v", attempt+1, maxRetries, backoff))

			// Context-aware sleep
			select {
			case <-time.After(backoff):
				// 继续
			case <-ctx.Done():
				return "", fmt.Errorf("LLM judge cancelled: %w", ctx.Err())
			}
		}

		// 为每次调用创建带超时的子 context（90秒超时，Judge 可能需要更长时间）
		judgeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		response, err := e.callLLMJudge(judgeCtx, prompt)
		cancel()

		if err == nil {
			if attempt > 0 {
				common.SysLog(fmt.Sprintf("LLM judge call succeeded after %d attempts", attempt+1))
			}
			return response, nil
		}

		lastErr = err
		errorsCollected = append(errorsCollected, fmt.Sprintf("Attempt %d: %v", attempt+1, err))
		common.SysLog(fmt.Sprintf("LLM judge call failed (attempt %d): %v", attempt+1, err))
	}

	return "", fmt.Errorf("LLM judge failed after %d attempts. Errors: %v", maxRetries, errorsCollected)
}

// buildJudgePrompt 构造 Judge 评估 Prompt
func (e *MonitorEvaluator) buildJudgePrompt(testType, baselineOutput, rawOutput string) string {
	var criteria string

	switch testType {
	case "style":
		criteria = "评估两个输出的**风格一致性**，包括：语气、用词习惯、格式风格、表达方式等。"
	case "reasoning":
		criteria = "评估两个输出的**推理质量**，包括：逻辑严谨性、推理步骤完整性、结论合理性等。"
	case "instruction_following":
		criteria = "评估两个输出的**指令遵循程度**，包括：是否完整响应要求、是否遵守格式约束、是否包含必要信息等。"
	default:
		criteria = "评估两个输出的质量差异。"
	}

	prompt := fmt.Sprintf(`你是一个专业的 AI 模型输出质量评估专家。你需要比较两个 AI 模型的输出，判断它们的质量差异。

**评估标准：**
%s

**基准输出（Baseline，可信渠道）：**
%s

**待评估输出（Raw，被测渠道）：**
%s

**任务：**
1. 根据上述评估标准，对比两个输出的质量差异
2. 给出一个 0-100 的相似度得分（100表示完全一致，0表示完全不同）
3. 给出一个 0-100 的差异得分（0表示完全一致，100表示完全不同）
4. 说明判断理由

**输出格式（必须严格按照以下 JSON 格式）：**
{
  "similarity_score": <0-100的数字>,
  "diff_score": <0-100的数字>,
  "reason": "<判断理由>"
}

注意：similarity_score + diff_score 应该等于 100。`, criteria, baselineOutput, rawOutput)

	return prompt
}

// callLLMJudge 调用 LLM Judge API
func (e *MonitorEvaluator) callLLMJudge(ctx context.Context, prompt string) (string, error) {
	if e.judgeURL == "" {
		return "", fmt.Errorf("MONITOR_JUDGE_URL not configured")
	}

	// 构造 OpenAI 格式请求
	requestBody := map[string]interface{}{
		"model": e.judgeModel,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"max_tokens":  1000,
		"temperature": 0.3, // 低温度保证评估稳定性
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.judgeURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if e.judgeKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.judgeKey)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

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

	if response.Error != nil {
		return "", fmt.Errorf("API error: %s", response.Error.Message)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	content := response.Choices[0].Message.Content
	if content == "" {
		return "", fmt.Errorf("empty content in response")
	}

	return content, nil
}

// parseJudgeResponse 解析 Judge 响应
func (e *MonitorEvaluator) parseJudgeResponse(response string) (*EvaluationResult, error) {
	// 尝试提取 JSON 部分（Judge 可能会输出额外文本）
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")

	if jsonStart == -1 || jsonEnd == -1 || jsonEnd < jsonStart {
		return nil, fmt.Errorf("no valid JSON found in judge response")
	}

	jsonStr := response[jsonStart : jsonEnd+1]

	var result struct {
		SimilarityScore float64 `json:"similarity_score"`
		DiffScore       float64 `json:"diff_score"`
		Reason          string  `json:"reason"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse judge JSON: %w", err)
	}

	// 验证得分范围
	if result.SimilarityScore < 0 || result.SimilarityScore > 100 {
		return nil, fmt.Errorf("invalid similarity_score: %.2f", result.SimilarityScore)
	}
	if result.DiffScore < 0 || result.DiffScore > 100 {
		return nil, fmt.Errorf("invalid diff_score: %.2f", result.DiffScore)
	}

	return &EvaluationResult{
		Status:          "", // Will be set by applyEvaluationStandard
		DiffScore:       result.DiffScore,
		SimilarityScore: result.SimilarityScore,
		Reason:          result.Reason,
	}, nil
}

// applyEvaluationStandard 应用评估标准阈值
// 根据相似度得分和差异得分，结合评估标准，判断 pass/fail
// 阈值优先级：customThreshold（策略自定义）> 环境变量 > 硬编码默认值
func (e *MonitorEvaluator) applyEvaluationStandard(standard string, similarityScore, diffScore float64, customThreshold *float64) string {
	var passThreshold float64

	// 优先使用策略自定义阈值
	if customThreshold != nil {
		passThreshold = *customThreshold
		common.SysLog(fmt.Sprintf("Using custom threshold for %s: %.2f", standard, passThreshold))
	} else {
		// 使用环境变量或硬编码默认值
		passThreshold = e.getDefaultThreshold(standard)
		common.SysLog(fmt.Sprintf("Using default threshold for %s: %.2f", standard, passThreshold))
	}

	if similarityScore >= passThreshold {
		return "pass"
	}
	return "fail"
}

// getDefaultThreshold 获取默认阈值
// 优先级：环境变量 > 硬编码默认值
func (e *MonitorEvaluator) getDefaultThreshold(standard string) float64 {
	var envKey string
	var hardcodedDefault float64

	switch standard {
	case "strict":
		envKey = "MONITOR_THRESHOLD_STRICT"
		hardcodedDefault = 95.0
	case "standard":
		envKey = "MONITOR_THRESHOLD_STANDARD"
		hardcodedDefault = 85.0
	case "lenient":
		envKey = "MONITOR_THRESHOLD_LENIENT"
		hardcodedDefault = 70.0
	default:
		// 未知标准，使用标准阈值
		envKey = "MONITOR_THRESHOLD_STANDARD"
		hardcodedDefault = 85.0
	}

	// 尝试从环境变量读取
	envValue := common.GetEnvOrDefaultString(envKey, "")
	if envValue != "" {
		// 解析环境变量
		var threshold float64
		if _, err := fmt.Sscanf(envValue, "%f", &threshold); err == nil && threshold > 0 && threshold <= 100 {
			return threshold
		}
		// 解析失败，记录警告并使用硬编码默认值
		common.SysLog(fmt.Sprintf("Warning: invalid %s value '%s', using default %.2f", envKey, envValue, hardcodedDefault))
	}

	return hardcodedDefault
}

// 辅助函数
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
