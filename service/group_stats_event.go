package service

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// ChannelStatsUpdatedEvent 渠道统计更新事件
// 当渠道统计数据成功写入数据库后发出此事件
type ChannelStatsUpdatedEvent struct {
	ChannelId       int    `json:"channel_id"`
	ModelName       string `json:"model_name"`
	TimeWindowStart int64  `json:"time_window_start"`
}

// GroupStatsEventBus 分组统计事件总线
// 负责管理事件的发布和订阅
type GroupStatsEventBus struct {
	subscribers []chan ChannelStatsUpdatedEvent
	mu          sync.RWMutex
	enabled     bool
}

var (
	globalEventBus     *GroupStatsEventBus
	eventBusOnce       sync.Once
	eventBusBufferSize = 1000 // 事件缓冲区大小
)

// GetGlobalEventBus 获取全局事件总线实例（单例模式）
func GetGlobalEventBus() *GroupStatsEventBus {
	eventBusOnce.Do(func() {
		globalEventBus = &GroupStatsEventBus{
			subscribers: make([]chan ChannelStatsUpdatedEvent, 0),
			enabled:     true,
		}
		common.SysLog("GroupStatsEventBus initialized")
	})
	return globalEventBus
}

// Subscribe 订阅渠道统计更新事件
// 返回一个只读channel，订阅者可以从中接收事件
func (bus *GroupStatsEventBus) Subscribe() <-chan ChannelStatsUpdatedEvent {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	ch := make(chan ChannelStatsUpdatedEvent, eventBusBufferSize)
	bus.subscribers = append(bus.subscribers, ch)

	common.SysLog("New subscriber added to GroupStatsEventBus, total subscribers: %d", len(bus.subscribers))
	return ch
}

// Unsubscribe 取消订阅（移除channel）
func (bus *GroupStatsEventBus) Unsubscribe(ch <-chan ChannelStatsUpdatedEvent) {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	for i, subscriber := range bus.subscribers {
		if subscriber == ch {
			// 关闭channel
			close(bus.subscribers[i])
			// 从列表中移除
			bus.subscribers = append(bus.subscribers[:i], bus.subscribers[i+1:]...)
			common.SysLog("Subscriber removed from GroupStatsEventBus, remaining: %d", len(bus.subscribers))
			return
		}
	}
}

// Publish 发布渠道统计更新事件
// 非阻塞式发布，如果某个订阅者的缓冲区满了，会跳过该订阅者并记录警告
func (bus *GroupStatsEventBus) Publish(event ChannelStatsUpdatedEvent) {
	if !bus.enabled {
		return
	}

	bus.mu.RLock()
	defer bus.mu.RUnlock()

	if len(bus.subscribers) == 0 {
		// 没有订阅者，直接返回
		return
	}

	// 非阻塞式发送
	for i, subscriber := range bus.subscribers {
		select {
		case subscriber <- event:
			// 成功发送
		default:
			// 缓冲区满，记录警告但不阻塞
			common.SysLog("Warning: subscriber %d buffer is full, skipping event for channel %d, model %s",
				i, event.ChannelId, event.ModelName)
		}
	}
}

// Enable 启用事件总线
func (bus *GroupStatsEventBus) Enable() {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.enabled = true
	common.SysLog("GroupStatsEventBus enabled")
}

// Disable 禁用事件总线（用于测试或临时关闭功能）
func (bus *GroupStatsEventBus) Disable() {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.enabled = false
	common.SysLog("GroupStatsEventBus disabled")
}

// IsEnabled 检查事件总线是否启用
func (bus *GroupStatsEventBus) IsEnabled() bool {
	bus.mu.RLock()
	defer bus.mu.RUnlock()
	return bus.enabled
}

// GetSubscriberCount 获取当前订阅者数量（用于调试和监控）
func (bus *GroupStatsEventBus) GetSubscriberCount() int {
	bus.mu.RLock()
	defer bus.mu.RUnlock()
	return len(bus.subscribers)
}

// ========== 便捷函数 ==========

// PublishChannelStatsUpdatedEvent 便捷函数：发布渠道统计更新事件
func PublishChannelStatsUpdatedEvent(channelId int, modelName string, timeWindowStart int64) {
	event := ChannelStatsUpdatedEvent{
		ChannelId:       channelId,
		ModelName:       modelName,
		TimeWindowStart: timeWindowStart,
	}
	GetGlobalEventBus().Publish(event)
}

// SubscribeGroupStatsEvents 便捷函数：订阅分组统计事件
// 返回一个只读channel用于接收事件
func SubscribeGroupStatsEvents() <-chan ChannelStatsUpdatedEvent {
	return GetGlobalEventBus().Subscribe()
}
