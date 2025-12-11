// Package testutil provides utilities for integration testing of the NewAPI service.
package testutil

import (
	"fmt"
)

// --- Model Baseline Models ---

// ModelBaselineModel represents a model baseline entity.
type ModelBaselineModel struct {
	ID                 int    `json:"id,omitempty"`
	ModelName          string `json:"model_name"`
	TestType           string `json:"test_type"`
	EvaluationStandard string `json:"evaluation_standard"`
	BaselineChannelID  int    `json:"baseline_channel_id"`
	Prompt             string `json:"prompt"`
	BaselineOutput     string `json:"baseline_output"`
	CreatedAt          int64  `json:"created_at,omitempty"`
}

// MonitorPolicyModel represents a monitoring policy entity.
type MonitorPolicyModel struct {
	ID                 int      `json:"id,omitempty"`
	Name               string   `json:"name"`
	TargetModels       []string `json:"target_models"`
	TestTypes          []string `json:"test_types"`
	EvaluationStandard string   `json:"evaluation_standard"`
	TargetChannels     []int    `json:"target_channels,omitempty"`
	ScheduleCron       string   `json:"schedule_cron"`
	IsEnabled          bool     `json:"is_enabled"`
	CreatedAt          int64    `json:"created_at,omitempty"`
	UpdatedAt          int64    `json:"updated_at,omitempty"`
}

// ModelMonitoringResultModel represents a monitoring result entity.
type ModelMonitoringResultModel struct {
	ID            int64   `json:"id,omitempty"`
	ChannelID     int     `json:"channel_id"`
	ModelName     string  `json:"model_name"`
	BaselineID    int     `json:"baseline_id"`
	TestTimestamp int64   `json:"test_timestamp"`
	Status        string  `json:"status"`
	DiffScore     float64 `json:"diff_score"`
	Reason        string  `json:"reason"`
	RawOutput     string  `json:"raw_output,omitempty"`
}

// --- Baseline Management API Methods ---

// CreateBaseline creates a new model baseline.
func (c *APIClient) CreateBaseline(baseline *ModelBaselineModel) (int, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    int    `json:"data"`
	}

	err := c.PostJSON("/api/monitor/baselines", baseline, &resp)
	if err != nil {
		return 0, err
	}

	if !resp.Success {
		return 0, fmt.Errorf("create baseline failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// GetBaseline retrieves a specific baseline by model, test type, and evaluation standard.
func (c *APIClient) GetBaseline(modelName, testType, evaluationStandard string) (*ModelBaselineModel, error) {
	var resp struct {
		Success bool                `json:"success"`
		Message string              `json:"message"`
		Data    *ModelBaselineModel `json:"data"`
	}

	path := fmt.Sprintf("/api/monitor/baselines?model_name=%s&test_type=%s&evaluation_standard=%s",
		modelName, testType, evaluationStandard)

	err := c.GetJSON(path, &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get baseline failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// GetAllBaselines retrieves all model baselines.
func (c *APIClient) GetAllBaselines() ([]*ModelBaselineModel, error) {
	var resp struct {
		Success bool                  `json:"success"`
		Message string                `json:"message"`
		Data    []*ModelBaselineModel `json:"data"`
	}

	err := c.GetJSON("/api/monitor/baselines", &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get all baselines failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// UpdateBaseline updates an existing baseline.
func (c *APIClient) UpdateBaseline(baseline *ModelBaselineModel) error {
	var resp APIResponse

	err := c.PutJSON("/api/monitor/baselines", baseline, &resp)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("update baseline failed: %s", resp.Message)
	}

	return nil
}

// DeleteBaseline deletes a baseline by ID.
func (c *APIClient) DeleteBaseline(id int) error {
	var resp APIResponse

	path := fmt.Sprintf("/api/monitor/baselines/%d", id)
	err := c.DeleteJSON(path, &resp)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("delete baseline failed: %s", resp.Message)
	}

	return nil
}

// --- Monitor Policy API Methods ---

// CreateMonitorPolicy creates a new monitoring policy.
func (c *APIClient) CreateMonitorPolicy(policy *MonitorPolicyModel) (int, error) {
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    int    `json:"data"`
	}

	err := c.PostJSON("/api/monitor/policies", policy, &resp)
	if err != nil {
		return 0, err
	}

	if !resp.Success {
		return 0, fmt.Errorf("create monitor policy failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// GetMonitorPolicy retrieves a specific policy by ID.
func (c *APIClient) GetMonitorPolicy(id int) (*MonitorPolicyModel, error) {
	var resp struct {
		Success bool                `json:"success"`
		Message string              `json:"message"`
		Data    *MonitorPolicyModel `json:"data"`
	}

	path := fmt.Sprintf("/api/monitor/policies/%d", id)
	err := c.GetJSON(path, &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get monitor policy failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// GetAllMonitorPolicies retrieves all monitoring policies.
func (c *APIClient) GetAllMonitorPolicies() ([]*MonitorPolicyModel, error) {
	var resp struct {
		Success bool                  `json:"success"`
		Message string                `json:"message"`
		Data    []*MonitorPolicyModel `json:"data"`
	}

	err := c.GetJSON("/api/monitor/policies", &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get all monitor policies failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// UpdateMonitorPolicy updates an existing monitoring policy.
func (c *APIClient) UpdateMonitorPolicy(policy *MonitorPolicyModel) error {
	var resp APIResponse

	err := c.PutJSON("/api/monitor/policies", policy, &resp)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("update monitor policy failed: %s", resp.Message)
	}

	return nil
}

// DeleteMonitorPolicy deletes a policy by ID.
func (c *APIClient) DeleteMonitorPolicy(id int) error {
	var resp APIResponse

	path := fmt.Sprintf("/api/monitor/policies/%d", id)
	err := c.DeleteJSON(path, &resp)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("delete monitor policy failed: %s", resp.Message)
	}

	return nil
}

// TriggerMonitorWorker manually triggers a monitoring task for a specific policy.
func (c *APIClient) TriggerMonitorWorker(policyID int) error {
	var resp APIResponse

	path := fmt.Sprintf("/api/monitor/policies/%d/trigger", policyID)
	err := c.PostJSON(path, nil, &resp)
	if err != nil {
		return err
	}

	if !resp.Success {
		return fmt.Errorf("trigger monitor worker failed: %s", resp.Message)
	}

	return nil
}

// --- Monitoring Results API Methods ---

// GetChannelMonitoringResults retrieves monitoring results for a specific channel.
func (c *APIClient) GetChannelMonitoringResults(channelID int, modelName, testType string, startTime, endTime int64) ([]*ModelMonitoringResultModel, error) {
	var resp struct {
		Success bool                          `json:"success"`
		Message string                        `json:"message"`
		Data    []*ModelMonitoringResultModel `json:"data"`
	}

	path := fmt.Sprintf("/api/channels/%d/monitoring_results?model_name=%s&test_type=%s&start_time=%d&end_time=%d",
		channelID, modelName, testType, startTime, endTime)

	err := c.GetJSON(path, &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get channel monitoring results failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// GetModelMonitoringReport retrieves a monitoring report for a specific model across all channels.
func (c *APIClient) GetModelMonitoringReport(modelName string) ([]*ModelMonitoringResultModel, error) {
	var resp struct {
		Success bool                          `json:"success"`
		Message string                        `json:"message"`
		Data    []*ModelMonitoringResultModel `json:"data"`
	}

	path := fmt.Sprintf("/api/models/%s/monitoring_report", modelName)
	err := c.GetJSON(path, &resp)
	if err != nil {
		return nil, err
	}

	if !resp.Success {
		return nil, fmt.Errorf("get model monitoring report failed: %s", resp.Message)
	}

	return resp.Data, nil
}

// GetLatestMonitoringResult retrieves the most recent monitoring result for a channel and model.
func (c *APIClient) GetLatestMonitoringResult(channelID int, modelName string) (*ModelMonitoringResultModel, error) {
	results, err := c.GetChannelMonitoringResults(channelID, modelName, "", 0, 9999999999)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no monitoring results found for channel %d and model %s", channelID, modelName)
	}

	// Return the most recent result (assuming results are sorted by timestamp descending)
	return results[0], nil
}
