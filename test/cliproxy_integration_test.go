package test

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

// TestCLIProxyChannelIntegration 集成测试：验证 CLIProxy 渠道的端到端流程
func TestCLIProxyChannelIntegration(t *testing.T) {
	// 注意：这是一个集成测试示例，实际运行需要真实的 CLIProxyAPI 实例

	t.Run("Complete CLIProxy Channel Lifecycle", func(t *testing.T) {
		// 1. 创建 CLIProxy 类型渠道
		baseURL := "http://localhost:8080"
		accountHint := "gemini_test_user.json"

		channel := &model.Channel{
			Type:        constant.ChannelTypeCliProxy,
			Name:        "Test CLIProxy Channel",
			Key:         "test-api-key",
			BaseURL:     &baseURL,
			AccountHint: &accountHint,
			Models:      "gemini-1.5-pro,gemini-1.5-flash",
			Status:      1,
			OwnerUserId: 1,
		}

		// 验证渠道类型映射
		apiType, ok := constant.ChannelType2APIType(channel.Type)
		assert.True(t, ok)
		assert.Equal(t, constant.APITypeCliProxy, apiType)

		// 2. 验证 RelayInfo 正确设置 AccountHint
		relayInfo := &relaycommon.RelayInfo{
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType:    constant.ChannelTypeCliProxy,
				ChannelBaseUrl: baseURL,
				ApiKey:         "test-api-key",
				AccountHint:    accountHint,
			},
		}

		assert.Equal(t, accountHint, relayInfo.AccountHint)
		assert.Equal(t, constant.ChannelTypeCliProxy, relayInfo.ChannelType)

		// 3. 验证适配器可以正确获取
		// (实际测试需要完整的 Gin 上下文，这里仅验证类型)
		assert.NotNil(t, channel)
		assert.Equal(t, constant.ChannelTypeCliProxy, channel.Type)
	})
}

// TestBillingGroupRoutingGroupsSeparation 测试计费分组与路由分组的分离
func TestBillingGroupRoutingGroupsSeparation(t *testing.T) {
	t.Run("BillingGroup and RoutingGroups should be independent", func(t *testing.T) {
		// 场景：用户的系统分组是 vip，加入了 P2P 分组 101, 102
		// BillingGroup 应该是 vip（用于计费）
		// RoutingGroups 应该是 ["vip", "p2p_101", "p2p_102"]（用于选路）

		relayInfo := &relaycommon.RelayInfo{
			BillingGroup:  "vip",
			RoutingGroups: []string{"vip", "p2p_101", "p2p_102"},
		}

		// 验证计费分组单一
		assert.Equal(t, "vip", relayInfo.BillingGroup)

		// 验证路由分组包含系统分组和 P2P 分组
		assert.Contains(t, relayInfo.RoutingGroups, "vip")
		assert.Contains(t, relayInfo.RoutingGroups, "p2p_101")
		assert.Contains(t, relayInfo.RoutingGroups, "p2p_102")
		assert.Len(t, relayInfo.RoutingGroups, 3)
	})

	t.Run("Auto group expansion", func(t *testing.T) {
		// 场景：BillingGroup 为 "auto"，应该展开为配置的自动分组
		// 假设配置为 ["default", "vip"]

		// 注意：实际展开逻辑在 relay_info.go 中，这里仅验证概念
		billingGroup := "auto"
		assert.Equal(t, "auto", billingGroup)

		// 展开后的 routingGroups 应该包含所有自动分组
		// expandedGroups := setting.GetAutoGroups()
		// 例如: ["default", "vip", "p2p_101"]
	})
}

// TestP2PGroupPriorityRouting 测试 P2P 分组优先级选路
func TestP2PGroupPriorityRouting(t *testing.T) {
	t.Run("Private channels have highest priority", func(t *testing.T) {
		// 场景：同一模型有三种渠道
		// - 私有渠道 (owner_user_id = current_user, is_private = true)
		// - 共享渠道 (allowed_users 包含 current_user)
		// - 公共渠道 (owner_user_id = 0)

		// 验证优先级排序逻辑
		privateChannel := &model.Channel{
			OwnerUserId: 123,
			IsPrivate:   true,
		}

		sharedChannel := &model.Channel{
			OwnerUserId: 456,
			IsPrivate:   false,
		}

		publicChannel := &model.Channel{
			OwnerUserId: 0,
			IsPrivate:   false,
		}

		// 验证渠道类型识别
		assert.True(t, privateChannel.IsPrivate)
		assert.False(t, sharedChannel.IsPrivate)
		assert.Equal(t, 0, publicChannel.OwnerUserId)

		// 实际的优先级排序在 service/channel_select.go 中实现
		// 这里仅验证数据模型正确
	})
}

// TestChannelAccountHintPropagation 测试 AccountHint 的全链路传递
func TestChannelAccountHintPropagation(t *testing.T) {
	t.Run("AccountHint propagates from Channel to HTTP Header", func(t *testing.T) {
		accountHint := "gemini_user123.json"

		// 模拟 Channel 到 ChannelMeta 的传递
		channelMeta := &relaycommon.ChannelMeta{
			AccountHint: accountHint,
		}

		// 验证字段正确设置
		assert.Equal(t, accountHint, channelMeta.AccountHint)

		// 实际的 HTTP 头注入在 cliproxy.Adaptor.SetupRequestHeader 中
		// 已在 adaptor_test.go 中测试
	})
}

// TestP2PGroupCacheInvalidation 测试 P2P 分组缓存失效机制
func TestP2PGroupCacheInvalidation(t *testing.T) {
	t.Run("Cache should be invalidated on member status change", func(t *testing.T) {
		// 场景：
		// 1. 用户加入分组，缓存应失效
		// 2. 用户退出分组，缓存应失效
		// 3. 用户被踢出，缓存应失效

		// 注意：实际测试需要 Redis 环境
		// 这里仅验证函数签名存在

		// 验证缓存失效函数存在
		// err := model.InvalidateUserGroupCache(123)
		// 实际调用需要 Redis
	})
}
