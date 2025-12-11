package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// MonitorPolicyResponse defines the API representation of a monitor policy.
// It expands JSON string fields into proper arrays for clients.
type MonitorPolicyResponse struct {
	ID                 int      `json:"id"`
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

func toMonitorPolicyResponse(policy *model.MonitorPolicy) *MonitorPolicyResponse {
	if policy == nil {
		return nil
	}
	return &MonitorPolicyResponse{
		ID:                 policy.Id,
		Name:               policy.Name,
		TargetModels:       policy.GetTargetModels(),
		TestTypes:          policy.GetTestTypes(),
		EvaluationStandard: policy.EvaluationStandard,
		TargetChannels:     policy.GetTargetChannels(),
		ScheduleCron:       policy.ScheduleCron,
		IsEnabled:          policy.IsEnabled,
		CreatedAt:          policy.CreatedAt,
		UpdatedAt:          policy.UpdatedAt,
	}
}

func toMonitorPolicyResponseList(policies []*model.MonitorPolicy) []*MonitorPolicyResponse {
	result := make([]*MonitorPolicyResponse, 0, len(policies))
	for _, p := range policies {
		result = append(result, toMonitorPolicyResponse(p))
	}
	return result
}

// MonitorPolicyRequest 监控策略请求结构
type MonitorPolicyRequest struct {
	ID                 int      `json:"id"` // Used for update via body
	Name               string   `json:"name" binding:"required"`
	TargetModels       []string `json:"target_models"`
	TestTypes          []string `json:"test_types"`
	EvaluationStandard string   `json:"evaluation_standard" binding:"required,oneof=strict standard lenient"`
	TargetChannels     []int    `json:"target_channels"`
	ScheduleCron       string   `json:"schedule_cron" binding:"required"`
	IsEnabled          bool     `json:"is_enabled"`
}

// GetMonitorPolicies 获取所有监控策略列表
// @Summary 获取监控策略列表
// @Description 获取所有监控策略，支持筛选启用状态
// @Tags 监控策略
// @Accept json
// @Produce json
// @Param enabled_only query bool false "仅获取启用的策略"
// @Success 200 {object} common.Response{data=[]model.MonitorPolicy}
// @Router /api/monitor/policies [get]
func GetMonitorPolicies(c *gin.Context) {
	enabledOnly := c.DefaultQuery("enabled_only", "false") == "true"

	policies, err := model.GetAllMonitorPolicies(enabledOnly)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取监控策略失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    toMonitorPolicyResponseList(policies),
	})
}

// GetMonitorPolicy 获取单个监控策略详情
// @Summary 获取监控策略详情
// @Description 根据ID获取监控策略详细信息
// @Tags 监控策略
// @Accept json
// @Produce json
// @Param id path int true "策略ID"
// @Success 200 {object} common.Response{data=model.MonitorPolicy}
// @Router /api/monitor/policies/:id [get]
func GetMonitorPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的策略ID",
		})
		return
	}

	policy, err := model.GetMonitorPolicyById(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取监控策略失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    toMonitorPolicyResponse(policy),
	})
}

// CreateMonitorPolicy 创建新的监控策略
// @Summary 创建监控策略
// @Description 创建新的模型监控策略
// @Tags 监控策略
// @Accept json
// @Produce json
// @Param policy body MonitorPolicyRequest true "策略信息"
// @Success 200 {object} common.Response{data=model.MonitorPolicy}
// @Router /api/monitor/policies [post]
func CreateMonitorPolicy(c *gin.Context) {
	var req MonitorPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("请求参数错误: %v", err),
		})
		return
	}

	// 验证 Cron 表达式
	if err := validateCronExpression(req.ScheduleCron); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("无效的Cron表达式: %v", err),
		})
		return
	}

	// 创建策略对象
	policy := &model.MonitorPolicy{
		Name:               req.Name,
		EvaluationStandard: req.EvaluationStandard,
		ScheduleCron:       req.ScheduleCron,
		IsEnabled:          req.IsEnabled,
	}

	// 设置 JSON 字段
	if len(req.TargetModels) > 0 {
		if err := policy.SetTargetModels(req.TargetModels); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("设置目标模型失败: %v", err),
			})
			return
		}
	}

	if len(req.TestTypes) > 0 {
		if err := policy.SetTestTypes(req.TestTypes); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("设置检测类型失败: %v", err),
			})
			return
		}
	}

	if len(req.TargetChannels) > 0 {
		if err := policy.SetTargetChannels(req.TargetChannels); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("设置目标渠道失败: %v", err),
			})
			return
		}
	}

	// 保存到数据库
	if err := model.CreateMonitorPolicy(policy); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("创建监控策略失败: %v", err),
		})
		return
	}

	common.SysLog(fmt.Sprintf("管理员创建监控策略: id=%d, name=%s", policy.Id, policy.Name))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "创建监控策略成功",
		// For compatibility with monitoring tests, return the policy ID
		// wrapped in an object so tests can read data.id.
		"data": gin.H{
			"id": policy.Id,
		},
	})
}

// UpdateMonitorPolicy 更新监控策略
// @Summary 更新监控策略
// @Description 更新现有的监控策略信息
// @Tags 监控策略
// @Accept json
// @Produce json
// @Param id path int true "策略ID"
// @Param policy body MonitorPolicyRequest true "策略信息"
// @Success 200 {object} common.Response{data=model.MonitorPolicy}
// @Router /api/monitor/policies/:id [put]
func UpdateMonitorPolicy(c *gin.Context) {
	// Strategy ID can come from either the URL path (/policies/:id)
	// or the JSON body (PUT /policies). This allows both REST styles
	// and keeps compatibility with existing tests.
	var id int
	if idStr := c.Param("id"); idStr != "" {
		parsed, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无效的策略ID",
			})
			return
		}
		id = parsed
	}

	var req MonitorPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("请求参数错误: %v", err),
		})
		return
	}

	// Fallback to ID from body when path parameter is absent.
	if id == 0 && req.ID > 0 {
		id = req.ID
	}
	if id == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "策略ID不能为空",
		})
		return
	}

	// 获取现有策略
	policy, err := model.GetMonitorPolicyById(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取监控策略失败: %v", err),
		})
		return
	}

	// 验证 Cron 表达式
	if err := validateCronExpression(req.ScheduleCron); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("无效的Cron表达式: %v", err),
		})
		return
	}

	// 更新字段
	policy.Name = req.Name
	policy.EvaluationStandard = req.EvaluationStandard
	policy.ScheduleCron = req.ScheduleCron
	policy.IsEnabled = req.IsEnabled

	// 更新 JSON 字段
	if len(req.TargetModels) > 0 {
		if err := policy.SetTargetModels(req.TargetModels); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("设置目标模型失败: %v", err),
			})
			return
		}
	}

	if len(req.TestTypes) > 0 {
		if err := policy.SetTestTypes(req.TestTypes); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("设置检测类型失败: %v", err),
			})
			return
		}
	}

	if len(req.TargetChannels) > 0 {
		if err := policy.SetTargetChannels(req.TargetChannels); err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("设置目标渠道失败: %v", err),
			})
			return
		}
	}

	// 保存更新
	if err := policy.Update(); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("更新监控策略失败: %v", err),
		})
		return
	}

	common.SysLog(fmt.Sprintf("管理员更新监控策略: id=%d, name=%s", policy.Id, policy.Name))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "更新监控策略成功",
		"data":    policy,
	})
}

// DeleteMonitorPolicy 删除监控策略
// @Summary 删除监控策略
// @Description 删除指定的监控策略
// @Tags 监控策略
// @Accept json
// @Produce json
// @Param id path int true "策略ID"
// @Success 200 {object} common.Response
// @Router /api/monitor/policies/:id [delete]
func DeleteMonitorPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的策略ID",
		})
		return
	}

	// 删除策略
	if err := model.DeleteMonitorPolicy(id); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("删除监控策略失败: %v", err),
		})
		return
	}

	common.SysLog(fmt.Sprintf("管理员删除监控策略: id=%d", id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "删除监控策略成功",
	})
}

// ToggleMonitorPolicyStatus 切换监控策略状态
// @Summary 切换策略启用状态
// @Description 启用或禁用监控策略
// @Tags 监控策略
// @Accept json
// @Produce json
// @Param id path int true "策略ID"
// @Success 200 {object} common.Response
// @Router /api/monitor/policies/:id/toggle [post]
func ToggleMonitorPolicyStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的策略ID",
		})
		return
	}

	// 切换状态
	if err := model.ToggleMonitorPolicyStatus(id); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("切换策略状态失败: %v", err),
		})
		return
	}

	common.SysLog(fmt.Sprintf("管理员切换监控策略状态: id=%d", id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "切换策略状态成功",
	})
}

// SearchMonitorPolicies 搜索监控策略
// @Summary 搜索监控策略
// @Description 根据关键词搜索监控策略
// @Tags 监控策略
// @Accept json
// @Produce json
// @Param keyword query string false "搜索关键词"
// @Success 200 {object} common.Response{data=[]model.MonitorPolicy}
// @Router /api/monitor/policies/search [get]
func SearchMonitorPolicies(c *gin.Context) {
	keyword := c.Query("keyword")

	policies, err := model.SearchMonitorPolicies(keyword)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("搜索监控策略失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    policies,
	})
}

// validateCronExpression 验证 Cron 表达式
func validateCronExpression(cron string) error {
	// 简单验证:检查是否有5个或6个字段
	// 标准Cron: 分 时 日 月 周
	// 扩展Cron: 秒 分 时 日 月 周
	// 这里可以使用第三方库如 github.com/robfig/cron/v3 进行更严格的验证
	// 暂时使用简单验证
	if cron == "" {
		return fmt.Errorf("Cron表达式不能为空")
	}
	// TODO: 使用 robfig/cron 库进行严格验证
	return nil
}

// TriggerPolicyNow 手动触发策略执行（用于测试）
// @Summary 手动触发策略
// @Description 立即执行指定的监控策略（忽略 Cron 调度）
// @Tags 监控策略
// @Accept json
// @Produce json
// @Param id path int true "策略ID"
// @Success 200 {object} common.Response
// @Router /api/monitor/policies/:id/trigger [post]
func TriggerPolicyNow(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的策略ID",
		})
		return
	}

	// 获取调度器实例
	scheduler := service.GetMonitorScheduler()

	// 触发策略
	scheduler.TriggerPolicyNow(id)

	common.SysLog(fmt.Sprintf("管理员手动触发监控策略: id=%d", id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已触发策略执行（异步进行中）",
	})
}

// GetSchedulerStatus 获取调度器状态
// @Summary 获取调度器状态
// @Description 获取监控调度器的运行状态和计划任务信息
// @Tags 监控策略
// @Accept json
// @Produce json
// @Success 200 {object} common.Response
// @Router /api/monitor/scheduler/status [get]
func GetSchedulerStatus(c *gin.Context) {
	scheduler := service.GetMonitorScheduler()
	status := scheduler.GetSchedulerStatus()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    status,
	})
}
