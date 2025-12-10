package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/robfig/cron/v3"
)

// MonitorScheduler 监控调度器
// 负责根据策略的 Cron 表达式定时触发监控任务
// 设计文档: docs/01-P2P共享分组与用户创建渠道的状态信息监控统计与展示.md
// Section: MS3-1 Cron 调度器注册与任务触发
type MonitorScheduler struct {
	cron     *cron.Cron
	resolver *MonitorConfigResolver
	worker   *MonitorWorker
	mu       sync.RWMutex
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc
}

// globalScheduler 全局调度器实例
var (
	globalScheduler *MonitorScheduler
	schedulerOnce   sync.Once
)

// GetMonitorScheduler 获取全局监控调度器实例（单例模式）
func GetMonitorScheduler() *MonitorScheduler {
	schedulerOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		globalScheduler = &MonitorScheduler{
			cron:     cron.New(cron.WithSeconds()), // 支持秒级 Cron 表达式
			resolver: NewMonitorConfigResolver(),
			worker:   NewMonitorWorker(),
			running:  false,
			ctx:      ctx,
			cancel:   cancel,
		}
	})
	return globalScheduler
}

// Start 启动监控调度器
func (s *MonitorScheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("monitor scheduler already running")
	}

	common.SysLog("Starting model monitoring scheduler...")

	// 加载并注册所有启用的监控策略
	if err := s.reloadPolicies(); err != nil {
		return fmt.Errorf("failed to load monitor policies: %w", err)
	}

	// 启动 Cron 调度器
	s.cron.Start()
	s.running = true

	// 启动后台任务：定期重新加载策略（每10分钟）
	go s.periodicReload()

	common.SysLog("Model monitoring scheduler started successfully")
	return nil
}

// Stop 停止监控调度器
func (s *MonitorScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	common.SysLog("Stopping model monitoring scheduler...")

	// 停止 Cron 调度器
	ctx := s.cron.Stop()
	<-ctx.Done() // 等待所有运行中的任务完成

	// 取消后台任务
	s.cancel()

	s.running = false
	common.SysLog("Model monitoring scheduler stopped")
}

// reloadPolicies 重新加载所有启用的监控策略
func (s *MonitorScheduler) reloadPolicies() error {
	// 清除现有的 Cron 任务
	entries := s.cron.Entries()
	for _, entry := range entries {
		s.cron.Remove(entry.ID)
	}

	// 获取所有启用的监控策略
	policies, err := model.GetEnabledMonitorPolicies()
	if err != nil {
		return fmt.Errorf("failed to get enabled policies: %w", err)
	}

	if len(policies) == 0 {
		common.SysLog("No enabled monitor policies to schedule")
		return nil
	}

	// 为每个策略注册 Cron 任务
	registeredCount := 0
	for _, policy := range policies {
		if err := s.registerPolicy(policy); err != nil {
			common.SysLog(fmt.Sprintf("Failed to register policy %d (%s): %v", policy.Id, policy.Name, err))
			continue
		}
		registeredCount++
	}

	common.SysLog(fmt.Sprintf("Registered %d/%d monitor policies", registeredCount, len(policies)))
	return nil
}

// registerPolicy 注册单个监控策略的 Cron 任务
func (s *MonitorScheduler) registerPolicy(policy *model.MonitorPolicy) error {
	// 验证 Cron 表达式
	if policy.ScheduleCron == "" {
		return fmt.Errorf("policy %d has empty cron expression", policy.Id)
	}

	// 创建任务执行函数
	taskFunc := func() {
		s.executePolicy(policy.Id)
	}

	// 注册到 Cron 调度器
	entryId, err := s.cron.AddFunc(policy.ScheduleCron, taskFunc)
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	// 计算下次执行时间
	entry := s.cron.Entry(entryId)
	nextRun := entry.Next

	// 更新策略的下次执行时间
	if err := policy.UpdateLastExecutedTime(0, nextRun.Unix()); err != nil {
		common.SysLog(fmt.Sprintf("Failed to update next execution time for policy %d: %v", policy.Id, err))
	}

	common.SysLog(fmt.Sprintf("Registered policy %d (%s) with cron '%s', next run: %s",
		policy.Id, policy.Name, policy.ScheduleCron, nextRun.Format(time.RFC3339)))

	return nil
}

// executePolicy 执行监控策略
func (s *MonitorScheduler) executePolicy(policyId int) {
	startTime := time.Now()
	common.SysLog(fmt.Sprintf("Executing monitor policy %d...", policyId))

	// 获取策略
	policy, err := model.GetMonitorPolicyById(policyId)
	if err != nil {
		common.SysLog(fmt.Sprintf("Failed to get policy %d: %v", policyId, err))
		return
	}

	// 检查策略是否仍然启用
	if !policy.IsEnabled {
		common.SysLog(fmt.Sprintf("Policy %d is no longer enabled, skipping", policyId))
		return
	}

	// 获取该策略的所有监控计划
	plans, err := s.resolver.GetMonitoringPlansForPolicy(policyId)
	if err != nil {
		common.SysLog(fmt.Sprintf("Failed to get monitoring plans for policy %d: %v", policyId, err))
		return
	}

	if len(plans) == 0 {
		common.SysLog(fmt.Sprintf("No monitoring plans generated for policy %d", policyId))
		return
	}

	common.SysLog(fmt.Sprintf("Policy %d generated %d monitoring plans", policyId, len(plans)))

	// 执行所有监控计划
	successCount := 0
	failCount := 0
	for _, plan := range plans {
		// 为每个计划执行监控
		for _, testType := range plan.TestTypes {
			err := s.worker.ExecuteMonitoring(s.ctx, plan.ChannelId, plan.ModelName, testType, plan.EvaluationStandard, policyId)
			if err != nil {
				common.SysLog(fmt.Sprintf("Failed to execute monitoring for channel %d, model %s, test %s: %v",
					plan.ChannelId, plan.ModelName, testType, err))
				failCount++
			} else {
				successCount++
			}
		}
	}

	// 更新策略的执行时间
	now := time.Now().Unix()
	// 计算下次执行时间
	nextRun := time.Now().Add(time.Hour) // 默认1小时后，实际由 Cron 调度器控制
	if err := policy.UpdateLastExecutedTime(now, nextRun.Unix()); err != nil {
		common.SysLog(fmt.Sprintf("Failed to update execution time for policy %d: %v", policyId, err))
	}

	elapsed := time.Since(startTime)
	common.SysLog(fmt.Sprintf("Policy %d execution completed: success=%d, fail=%d, elapsed=%v",
		policyId, successCount, failCount, elapsed))
}

// periodicReload 定期重新加载监控策略
// 每10分钟检查一次策略变更
func (s *MonitorScheduler) periodicReload() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.running {
				common.SysLog("Reloading monitor policies...")
				if err := s.reloadPolicies(); err != nil {
					common.SysLog(fmt.Sprintf("Failed to reload monitor policies: %v", err))
				}
			}
			s.mu.Unlock()
		}
	}
}

// TriggerPolicyNow 立即触发指定策略的执行（用于手动测试）
func (s *MonitorScheduler) TriggerPolicyNow(policyId int) {
	go s.executePolicy(policyId)
}

// GetSchedulerStatus 获取调度器状态
func (s *MonitorScheduler) GetSchedulerStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := s.cron.Entries()
	scheduledTasks := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		scheduledTasks = append(scheduledTasks, map[string]interface{}{
			"entry_id": entry.ID,
			"next_run": entry.Next.Format(time.RFC3339),
			"prev_run": entry.Prev.Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"running":         s.running,
		"scheduled_tasks": len(entries),
		"tasks":           scheduledTasks,
	}
}

// ReloadPoliciesManually 手动重新加载策略（用于管理员操作）
func (s *MonitorScheduler) ReloadPoliciesManually() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return fmt.Errorf("scheduler is not running")
	}

	return s.reloadPolicies()
}
