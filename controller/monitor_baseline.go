package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// ModelBaselineRequest 模型基准请求结构
type ModelBaselineRequest struct {
	ModelName          string `json:"model_name" binding:"required"`
	TestType           string `json:"test_type" binding:"required,oneof=encoding reasoning style instruction_following structure_consistency"`
	EvaluationStandard string `json:"evaluation_standard" binding:"required,oneof=strict standard lenient"`
	BaselineChannelId  int    `json:"baseline_channel_id" binding:"required"`
	Prompt             string `json:"prompt" binding:"required"`
	BaselineOutput     string `json:"baseline_output" binding:"required"`
}

// GetModelBaselines 获取所有模型基准列表
// @Summary 获取模型基准列表
// @Description 获取所有模型基准
// @Tags 模型基准
// @Accept json
// @Produce json
// @Success 200 {object} common.Response{data=[]model.ModelBaseline}
// @Router /api/monitor/baselines [get]
func GetModelBaselines(c *gin.Context) {
	// Optional filter: when model_name, test_type and evaluation_standard
	// are all provided, return a single baseline that matches this
	// unique key. This aligns with the monitoring tests which call
	// /api/monitor/baselines?model_name=...&test_type=...&evaluation_standard=...
	modelName := c.Query("model_name")
	testType := c.Query("test_type")
	evaluationStandard := c.Query("evaluation_standard")

	if modelName != "" || testType != "" || evaluationStandard != "" {
		// All three parameters must be present to form a valid key.
		if modelName == "" || testType == "" || evaluationStandard == "" {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "缺少必要参数: model_name, test_type, evaluation_standard 需要同时提供",
			})
			return
		}

		baseline, err := model.GetModelBaseline(modelName, testType, evaluationStandard)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": fmt.Sprintf("获取模型基准失败: %v", err),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data":    baseline,
		})
		return
	}

	// No filter: return full baseline list.
	baselines, err := model.GetAllModelBaselines()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取模型基准失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    baselines,
	})
}

// GetModelBaseline 获取单个模型基准详情
// @Summary 获取模型基准详情
// @Description 根据ID获取模型基准详细信息
// @Tags 模型基准
// @Accept json
// @Produce json
// @Param id path int true "基准ID"
// @Success 200 {object} common.Response{data=model.ModelBaseline}
// @Router /api/monitor/baselines/:id [get]
func GetModelBaseline(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的基准ID",
		})
		return
	}

	baseline, err := model.GetModelBaselineById(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取模型基准失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    baseline,
	})
}

// CreateOrUpdateModelBaseline 创建或更新模型基准
// @Summary 创建或更新模型基准
// @Description 创建新的模型基准，或更新已存在的基准（基于 model_name + test_type + evaluation_standard 唯一性）
// @Tags 模型基准
// @Accept json
// @Produce json
// @Param baseline body ModelBaselineRequest true "基准信息"
// @Success 200 {object} common.Response{data=model.ModelBaseline}
// @Router /api/monitor/baselines [post]
func CreateOrUpdateModelBaseline(c *gin.Context) {
	var req ModelBaselineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("请求参数错误: %v", err),
		})
		return
	}

	// 验证渠道是否存在
	channel, err := model.GetChannelById(req.BaselineChannelId, false)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("基准渠道不存在: %v", err),
		})
		return
	}

	// 创建基准对象
	baseline := &model.ModelBaseline{
		ModelName:          req.ModelName,
		TestType:           req.TestType,
		EvaluationStandard: req.EvaluationStandard,
		BaselineChannelId:  req.BaselineChannelId,
		Prompt:             req.Prompt,
		BaselineOutput:     req.BaselineOutput,
	}

	// 使用 Upsert 逻辑（插入或更新）
	if err := model.UpsertModelBaseline(baseline); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("保存模型基准失败: %v", err),
		})
		return
	}

	common.SysLog(fmt.Sprintf("管理员设置模型基准: model=%s, test_type=%s, standard=%s, channel_id=%d",
		baseline.ModelName, baseline.TestType, baseline.EvaluationStandard, channel.Id))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "保存模型基准成功",
		// For API consistency with tests, return the baseline ID as data.
		"data": baseline.Id,
	})
}

// DeleteModelBaseline 删除模型基准
// @Summary 删除模型基准
// @Description 删除指定的模型基准
// @Tags 模型基准
// @Accept json
// @Produce json
// @Param id path int true "基准ID"
// @Success 200 {object} common.Response
// @Router /api/monitor/baselines/:id [delete]
func DeleteModelBaseline(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无效的基准ID",
		})
		return
	}

	// 删除基准
	if err := model.DeleteModelBaseline(id); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("删除模型基准失败: %v", err),
		})
		return
	}

	common.SysLog(fmt.Sprintf("管理员删除模型基准: id=%d", id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "删除模型基准成功",
	})
}

// GetModelBaselinesByModel 获取指定模型的所有基准
// @Summary 获取模型的基准列表
// @Description 根据模型名称获取所有相关基准
// @Tags 模型基准
// @Accept json
// @Produce json
// @Param model_name query string true "模型名称"
// @Success 200 {object} common.Response{data=[]model.ModelBaseline}
// @Router /api/monitor/baselines/by-model [get]
func GetModelBaselinesByModel(c *gin.Context) {
	modelName := c.Query("model_name")
	if modelName == "" {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "缺少模型名称参数",
		})
		return
	}

	baselines, err := model.GetModelBaselinesByModel(modelName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取模型基准失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    baselines,
	})
}

// SearchModelBaselines 搜索模型基准
// @Summary 搜索模型基准
// @Description 根据关键词搜索模型基准
// @Tags 模型基准
// @Accept json
// @Produce json
// @Param keyword query string false "搜索关键词"
// @Success 200 {object} common.Response{data=[]model.ModelBaseline}
// @Router /api/monitor/baselines/search [get]
func SearchModelBaselines(c *gin.Context) {
	keyword := c.Query("keyword")

	baselines, err := model.SearchModelBaselines(keyword)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("搜索模型基准失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    baselines,
	})
}

// GetDistinctModelNames 获取所有已设定基准的模型名称列表
// @Summary 获取模型名称列表
// @Description 获取所有已设定基准的模型名称（去重）
// @Tags 模型基准
// @Accept json
// @Produce json
// @Success 200 {object} common.Response{data=[]string}
// @Router /api/monitor/baselines/models [get]
func GetDistinctModelNames(c *gin.Context) {
	modelNames, err := model.GetDistinctModelNamesFromBaselines()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取模型名称列表失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    modelNames,
	})
}

// GetDistinctTestTypes 获取所有已使用的检测类型列表
// @Summary 获取检测类型列表
// @Description 获取所有已使用的检测类型（去重）
// @Tags 模型基准
// @Accept json
// @Produce json
// @Success 200 {object} common.Response{data=[]string}
// @Router /api/monitor/baselines/test-types [get]
func GetDistinctTestTypes(c *gin.Context) {
	testTypes, err := model.GetDistinctTestTypesFromBaselines()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取检测类型列表失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    testTypes,
	})
}
