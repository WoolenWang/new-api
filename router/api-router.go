package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func SetApiRouter(router *gin.Engine) {
	apiRouter := router.Group("/api")
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(middleware.GlobalAPIRateLimit())
	{
		apiRouter.GET("/setup", controller.GetSetup)
		apiRouter.POST("/setup", controller.PostSetup)
		apiRouter.GET("/status", controller.GetStatus)
		apiRouter.GET("/uptime/status", controller.GetUptimeKumaStatus)
		apiRouter.GET("/models", middleware.UserAuth(), controller.DashboardListModels)
		apiRouter.GET("/status/test", middleware.AdminAuth(), controller.TestStatus)
		apiRouter.GET("/notice", controller.GetNotice)
		apiRouter.GET("/user-agreement", controller.GetUserAgreement)
		apiRouter.GET("/privacy-policy", controller.GetPrivacyPolicy)
		apiRouter.GET("/about", controller.GetAbout)
		//apiRouter.GET("/midjourney", controller.GetMidjourney)
		apiRouter.GET("/home_page_content", controller.GetHomePageContent)
		apiRouter.GET("/pricing", middleware.TryUserAuth(), controller.GetPricing)
		apiRouter.GET("/verification", middleware.EmailVerificationRateLimit(), middleware.TurnstileCheck(), controller.SendEmailVerification)
		apiRouter.GET("/reset_password", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.SendPasswordResetEmail)
		apiRouter.POST("/user/reset", middleware.CriticalRateLimit(), controller.ResetPassword)
		apiRouter.GET("/oauth/github", middleware.CriticalRateLimit(), controller.GitHubOAuth)
		apiRouter.GET("/oauth/discord", middleware.CriticalRateLimit(), controller.DiscordOAuth)
		apiRouter.GET("/oauth/oidc", middleware.CriticalRateLimit(), controller.OidcAuth)
		apiRouter.GET("/oauth/linuxdo", middleware.CriticalRateLimit(), controller.LinuxdoOAuth)
		apiRouter.GET("/oauth/state", middleware.CriticalRateLimit(), controller.GenerateOAuthCode)
		apiRouter.GET("/oauth/wechat", middleware.CriticalRateLimit(), controller.WeChatAuth)
		apiRouter.GET("/oauth/wechat/bind", middleware.CriticalRateLimit(), controller.WeChatBind)
		apiRouter.GET("/oauth/email/bind", middleware.CriticalRateLimit(), controller.EmailBind)
		apiRouter.GET("/oauth/telegram/login", middleware.CriticalRateLimit(), controller.TelegramLogin)
		apiRouter.GET("/oauth/telegram/bind", middleware.CriticalRateLimit(), controller.TelegramBind)
		apiRouter.GET("/ratio_config", middleware.CriticalRateLimit(), controller.GetRatioConfig)

		apiRouter.POST("/stripe/webhook", controller.StripeWebhook)
		apiRouter.POST("/creem/webhook", controller.CreemWebhook)

		// Universal secure verification routes
		apiRouter.POST("/verify", middleware.UserAuth(), middleware.CriticalRateLimit(), controller.UniversalVerify)
		apiRouter.GET("/verify/status", middleware.UserAuth(), controller.GetVerificationStatus)

		userRoute := apiRouter.Group("/user")
		{
			userRoute.POST("/register", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.Register)
			userRoute.POST("/login", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.Login)
			userRoute.POST("/login/2fa", middleware.CriticalRateLimit(), controller.Verify2FALogin)
			userRoute.POST("/passkey/login/begin", middleware.CriticalRateLimit(), controller.PasskeyLoginBegin)
			userRoute.POST("/passkey/login/finish", middleware.CriticalRateLimit(), controller.PasskeyLoginFinish)
			//userRoute.POST("/tokenlog", middleware.CriticalRateLimit(), controller.TokenLog)
			userRoute.GET("/logout", controller.Logout)
			userRoute.GET("/epay/notify", controller.EpayNotify)
			userRoute.GET("/groups", controller.GetUserGroups)

			selfRoute := userRoute.Group("/")
			selfRoute.Use(middleware.UserAuth())
			{
				selfRoute.GET("/self/groups", controller.GetUserGroups)
				selfRoute.GET("/self", controller.GetSelf)
				selfRoute.GET("/models", controller.GetUserModels)
				selfRoute.PUT("/self", controller.UpdateSelf)
				selfRoute.DELETE("/self", controller.DeleteSelf)
				selfRoute.GET("/token", controller.GenerateAccessToken)
				selfRoute.GET("/passkey", controller.PasskeyStatus)
				selfRoute.POST("/passkey/register/begin", controller.PasskeyRegisterBegin)
				selfRoute.POST("/passkey/register/finish", controller.PasskeyRegisterFinish)
				selfRoute.POST("/passkey/verify/begin", controller.PasskeyVerifyBegin)
				selfRoute.POST("/passkey/verify/finish", controller.PasskeyVerifyFinish)
				selfRoute.DELETE("/passkey", controller.PasskeyDelete)
				selfRoute.GET("/aff", controller.GetAffCode)
				selfRoute.GET("/topup/info", controller.GetTopUpInfo)
				selfRoute.GET("/topup/self", controller.GetUserTopUps)
				selfRoute.POST("/topup", middleware.CriticalRateLimit(), controller.TopUp)
				selfRoute.POST("/pay", middleware.CriticalRateLimit(), controller.RequestEpay)
				selfRoute.POST("/amount", controller.RequestAmount)
				selfRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), controller.RequestStripePay)
				selfRoute.POST("/stripe/amount", controller.RequestStripeAmount)
				selfRoute.POST("/creem/pay", middleware.CriticalRateLimit(), controller.RequestCreemPay)
				selfRoute.POST("/aff_transfer", controller.TransferAffQuota)
				selfRoute.POST("/quota/:id", controller.ExchangeShareQuota) // Phase 1: Exchange share_quota to quota
				selfRoute.PUT("/setting", controller.UpdateUserSetting)

				// 2FA routes
				selfRoute.GET("/2fa/status", controller.Get2FAStatus)
				selfRoute.POST("/2fa/setup", controller.Setup2FA)
				selfRoute.POST("/2fa/enable", controller.Enable2FA)
				selfRoute.POST("/2fa/disable", controller.Disable2FA)
				selfRoute.POST("/2fa/backup_codes", controller.RegenerateBackupCodes)

				// Checkin routes
				selfRoute.POST("/checkin", controller.Checkin)
				selfRoute.GET("/checkin/status", controller.GetCheckinStatus)
			}

			adminRoute := userRoute.Group("/")
			adminRoute.Use(middleware.AdminAuth())
			{
				adminRoute.GET("/", controller.GetAllUsers)
				adminRoute.GET("/topup", controller.GetAllTopUps)
				adminRoute.POST("/topup/complete", controller.AdminCompleteTopUp)
				adminRoute.GET("/search", controller.SearchUsers)
				adminRoute.GET("/query", controller.QueryUser)
				adminRoute.GET("/:id", controller.GetUser)
				adminRoute.POST("/", controller.CreateUser)
				adminRoute.POST("/manage", controller.ManageUser)
				adminRoute.PUT("/", controller.UpdateUser)
				adminRoute.DELETE("/:id", controller.DeleteUser)
				adminRoute.DELETE("/:id/reset_passkey", controller.AdminResetPasskey)
				adminRoute.POST("/quota/adjust", controller.AdminAdjustUserQuota) // Phase 1: Admin quota adjustment

				// Admin 2FA routes
				adminRoute.GET("/2fa/stats", controller.Admin2FAStats)
				adminRoute.DELETE("/:id/2fa", controller.AdminDisable2FA)
			}
		}
		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(middleware.RootAuth())
		{
			optionRoute.GET("/", controller.GetOptions)
			optionRoute.PUT("/", controller.UpdateOption)
			optionRoute.POST("/rest_model_ratio", controller.ResetModelRatio)
			optionRoute.POST("/migrate_console_setting", controller.MigrateConsoleSetting) // 用于迁移检测的旧键，下个版本会删除
		}
		ratioSyncRoute := apiRouter.Group("/ratio_sync")
		ratioSyncRoute.Use(middleware.RootAuth())
		{
			ratioSyncRoute.GET("/channels", controller.GetSyncableChannels)
			ratioSyncRoute.POST("/fetch", controller.FetchUpstreamRatios)
		}
		channelRoute := apiRouter.Group("/channel")
		channelRoute.Use(middleware.AdminAuth())
		{
			channelRoute.GET("/", controller.GetAllChannels)
			channelRoute.GET("/search", controller.SearchChannels)
			channelRoute.GET("/models", controller.ChannelListModels)
			channelRoute.GET("/models_enabled", controller.EnabledListModels)
			channelRoute.GET("/:id", controller.GetChannel)
			channelRoute.POST("/:id/key", middleware.RootAuth(), middleware.CriticalRateLimit(), middleware.DisableCache(), middleware.SecureVerificationRequired(), controller.GetChannelKey)
			channelRoute.GET("/test", controller.TestAllChannels)
			channelRoute.GET("/test/:id", controller.TestChannel)
			channelRoute.GET("/update_balance", controller.UpdateAllChannelsBalance)
			channelRoute.GET("/update_balance/:id", controller.UpdateChannelBalance)
			channelRoute.POST("/", controller.AddChannel)
			channelRoute.PUT("/", controller.UpdateChannel)
			channelRoute.DELETE("/disabled", controller.DeleteDisabledChannel)
			channelRoute.POST("/tag/disabled", controller.DisableTagChannels)
			channelRoute.POST("/tag/enabled", controller.EnableTagChannels)
			channelRoute.PUT("/tag", controller.EditTagChannels)
			channelRoute.DELETE("/:id", controller.DeleteChannel)
			channelRoute.POST("/batch", controller.DeleteChannelBatch)
			channelRoute.POST("/fix", controller.FixChannelsAbilities)
			channelRoute.GET("/fetch_models/:id", controller.FetchUpstreamModels)
			channelRoute.POST("/fetch_models", controller.FetchModels)
			channelRoute.POST("/batch/tag", controller.BatchSetChannelTag)
			channelRoute.GET("/tag/models", controller.GetTagModels)
			channelRoute.POST("/copy/:id", controller.CopyChannel)
			channelRoute.POST("/multi_key/manage", controller.ManageMultiKeys)
			// P2P Channel Admin Routes
			channelRoute.GET("/p2p", controller.GetP2PChannels)                        // List all P2P channels with usage
			channelRoute.GET("/:id/usage", controller.GetChannelUsage)                 // Get detailed usage for specific channel
			channelRoute.GET("/concurrency", controller.GetChannelConcurrencySnapshot) // Get lightweight concurrency snapshot for all channels

			// Phase 8.5: Channel Statistics Query API
			channelRoute.GET("/:id/stats", controller.GetChannelStats)                // Get aggregated stats with time range
			channelRoute.GET("/:id/current_stats", controller.GetChannelCurrentStats) // Get latest stats from channels table
			channelRoute.POST("/:id/reset_stats", controller.ResetChannelStats)       // Reset channel statistics
		}
		// P2P Channel Self-Service Routes (Phase 1)
		channelSelfRoute := apiRouter.Group("/channel/self")
		channelSelfRoute.Use(middleware.UserAuth())
		{
			channelSelfRoute.GET("/models", controller.ChannelListModels)
			channelSelfRoute.GET("/models_enabled", controller.EnabledListModels)
			channelSelfRoute.GET("/", controller.GetUserChannels)
			channelSelfRoute.GET("/:id", controller.GetUserChannel)
			channelSelfRoute.POST("/", controller.CreateUserChannel)
			channelSelfRoute.PUT("/:id", controller.UpdateUserChannel)
			channelSelfRoute.DELETE("/:id", controller.DeleteUserChannel)
		}
		tokenRoute := apiRouter.Group("/token")
		tokenRoute.Use(middleware.UserAuth())
		{
			tokenRoute.GET("/", controller.GetAllTokens)
			tokenRoute.GET("/search", controller.SearchTokens)
			tokenRoute.GET("/:id", controller.GetToken)
			tokenRoute.POST("/", controller.AddToken)
			tokenRoute.PUT("/", controller.UpdateToken)
			tokenRoute.DELETE("/:id", controller.DeleteToken)
			tokenRoute.POST("/batch", controller.DeleteTokenBatch)
		}

		usageRoute := apiRouter.Group("/usage")
		usageRoute.Use(middleware.CriticalRateLimit())
		{
			tokenUsageRoute := usageRoute.Group("/token")
			tokenUsageRoute.Use(middleware.TokenAuth())
			{
				tokenUsageRoute.GET("/", controller.GetTokenUsage)
			}
		}

		redemptionRoute := apiRouter.Group("/redemption")
		redemptionRoute.Use(middleware.AdminAuth())
		{
			redemptionRoute.GET("/", controller.GetAllRedemptions)
			redemptionRoute.GET("/search", controller.SearchRedemptions)
			redemptionRoute.GET("/:id", controller.GetRedemption)
			redemptionRoute.POST("/", controller.AddRedemption)
			redemptionRoute.PUT("/", controller.UpdateRedemption)
			redemptionRoute.DELETE("/invalid", controller.DeleteInvalidRedemption)
			redemptionRoute.DELETE("/:id", controller.DeleteRedemption)
		}
		logRoute := apiRouter.Group("/log")
		logRoute.GET("/", middleware.AdminAuth(), controller.GetAllLogs)
		logRoute.DELETE("/", middleware.AdminAuth(), controller.DeleteHistoryLogs)
		logRoute.GET("/stat", middleware.AdminAuth(), controller.GetLogsStat)
		logRoute.GET("/self/stat", middleware.UserAuth(), controller.GetLogsSelfStat)
		logRoute.GET("/search", middleware.AdminAuth(), controller.SearchAllLogs)
		logRoute.GET("/self", middleware.UserAuth(), controller.GetUserLogs)
		logRoute.GET("/self/search", middleware.UserAuth(), controller.SearchUserLogs)

		dataRoute := apiRouter.Group("/data")
		dataRoute.GET("/", middleware.AdminAuth(), controller.GetAllQuotaDates)
		dataRoute.GET("/self", middleware.UserAuth(), controller.GetUserQuotaDates)

		logRoute.Use(middleware.CORS())
		{
			logRoute.GET("/token", controller.GetLogByKey)
		}
		groupRoute := apiRouter.Group("/group")
		groupRoute.Use(middleware.AdminAuth())
		{
			groupRoute.GET("/", controller.GetGroups)
		}

		prefillGroupRoute := apiRouter.Group("/prefill_group")
		prefillGroupRoute.Use(middleware.AdminAuth())
		{
			prefillGroupRoute.GET("/", controller.GetPrefillGroups)
			prefillGroupRoute.POST("/", controller.CreatePrefillGroup)
			prefillGroupRoute.PUT("/", controller.UpdatePrefillGroup)
			prefillGroupRoute.DELETE("/:id", controller.DeletePrefillGroup)
		}

		mjRoute := apiRouter.Group("/mj")
		mjRoute.GET("/self", middleware.UserAuth(), controller.GetUserMidjourney)
		mjRoute.GET("/", middleware.AdminAuth(), controller.GetAllMidjourney)

		taskRoute := apiRouter.Group("/task")
		{
			taskRoute.GET("/self", middleware.UserAuth(), controller.GetUserTask)
			taskRoute.GET("/", middleware.AdminAuth(), controller.GetAllTask)
		}

		vendorRoute := apiRouter.Group("/vendors")
		vendorRoute.Use(middleware.AdminAuth())
		{
			vendorRoute.GET("/", controller.GetAllVendors)
			vendorRoute.GET("/search", controller.SearchVendors)
			vendorRoute.GET("/:id", controller.GetVendorMeta)
			vendorRoute.POST("/", controller.CreateVendorMeta)
			vendorRoute.PUT("/", controller.UpdateVendorMeta)
			vendorRoute.DELETE("/:id", controller.DeleteVendorMeta)
		}

		modelsRoute := apiRouter.Group("/models")
		modelsRoute.Use(middleware.AdminAuth())
		{
			modelsRoute.GET("/sync_upstream/preview", controller.SyncUpstreamPreview)
			modelsRoute.POST("/sync_upstream", controller.SyncUpstreamModels)
			modelsRoute.GET("/missing", controller.GetMissingModels)
			modelsRoute.GET("/", controller.GetAllModelsMeta)
			modelsRoute.GET("/search", controller.SearchModelsMeta)
			modelsRoute.GET("/:id", controller.GetModelMeta)
			modelsRoute.GET("/:id/monitoring_report", controller.GetModelMonitoringReport)
			modelsRoute.POST("/", controller.CreateModelMeta)
			modelsRoute.PUT("/", controller.UpdateModelMeta)
			modelsRoute.DELETE("/:id", controller.DeleteModelMeta)
		}

		// P2P Group Management Routes
		// Note: This is for P2P groups, NOT system groups (system groups are under /api/group)
		p2pGroupsRoute := apiRouter.Group("/groups")
		p2pGroupsRoute.Use(middleware.UserAuth())
		{
			// P2P Group CRUD
			p2pGroupsRoute.POST("", controller.CreateP2PGroup)        // Create group
			p2pGroupsRoute.GET("/public", controller.GetPublicGroups) // Get public shared groups
			p2pGroupsRoute.PUT("", controller.UpdateP2PGroup)         // Update group
			p2pGroupsRoute.DELETE("", controller.DeleteP2PGroup)      // Delete group

			// User Self-Service Routes (automatically use authenticated user ID)
			p2pGroupsRoute.GET("/self/owned", controller.GetSelfOwnedGroups)   // Get current user's owned groups
			p2pGroupsRoute.GET("/self/joined", controller.GetSelfJoinedGroups) // Get current user's joined groups

			// Member Management
			p2pGroupsRoute.POST("/apply", controller.ApplyToJoinGroup)    // Apply to join group
			p2pGroupsRoute.GET("/members", controller.GetGroupMembers)    // Get group members
			p2pGroupsRoute.GET("/member", controller.GetMemberInfo)       // Get specific member info
			p2pGroupsRoute.PUT("/members", controller.UpdateMemberStatus) // Approve/reject/ban member
			p2pGroupsRoute.POST("/leave", controller.LeaveGroup)          // Leave group

			// Group Statistics Routes (Phase 10: P2P Group Statistics)
			p2pGroupsRoute.GET("/:id/stats", controller.GetP2PGroupStats)              // Get aggregated stats with time range
			p2pGroupsRoute.GET("/:id/stats/latest", controller.GetP2PGroupStatsLatest) // Get latest stats snapshot
		}

		// P2P Group Admin Routes (for querying any user's groups)
		p2pGroupsAdminRoute := apiRouter.Group("/groups/admin")
		p2pGroupsAdminRoute.Use(middleware.AdminAuth())
		{
			p2pGroupsAdminRoute.POST("", controller.CreateP2PGroup)            // Create group
			p2pGroupsAdminRoute.GET("/owned", controller.GetUserOwnedGroups)   // Get specific user's owned groups (requires user_id param)
			p2pGroupsAdminRoute.GET("/joined", controller.GetUserJoinedGroups) // Get specific user's joined groups (requires user_id param)
		}

		// Model Monitoring Routes (Admin only) - Phase 9: Model Intelligence & Drift Monitoring
		monitorRoute := apiRouter.Group("/monitor")
		monitorRoute.Use(middleware.AdminAuth())
		{
			// Monitor Policy Management
			monitorRoute.GET("/policies", controller.GetMonitorPolicies)
			monitorRoute.GET("/policies/:id", controller.GetMonitorPolicy)
			monitorRoute.POST("/policies", controller.CreateMonitorPolicy)
			monitorRoute.PUT("/policies/:id", controller.UpdateMonitorPolicy)
			monitorRoute.DELETE("/policies/:id", controller.DeleteMonitorPolicy)
			monitorRoute.POST("/policies/:id/toggle", controller.ToggleMonitorPolicyStatus)
			monitorRoute.GET("/policies/search", controller.SearchMonitorPolicies)
			monitorRoute.POST("/policies/:id/trigger", controller.TriggerPolicyNow)

			// Scheduler Management
			monitorRoute.GET("/scheduler/status", controller.GetSchedulerStatus)

			// Model Baseline Management
			monitorRoute.GET("/baselines", controller.GetModelBaselines)
			monitorRoute.GET("/baselines/:id", controller.GetModelBaseline)
			monitorRoute.POST("/baselines", controller.CreateOrUpdateModelBaseline)
			monitorRoute.DELETE("/baselines/:id", controller.DeleteModelBaseline)
			monitorRoute.GET("/baselines/by-model", controller.GetModelBaselinesByModel)
			monitorRoute.GET("/baselines/search", controller.SearchModelBaselines)
			monitorRoute.GET("/baselines/models", controller.GetDistinctModelNames)
			monitorRoute.GET("/baselines/test-types", controller.GetDistinctTestTypes)

			// Monitoring Results Query
			monitorRoute.GET("/results", controller.GetMonitoringResults)
			monitorRoute.GET("/results/latest", controller.GetLatestMonitoringResult)
			monitorRoute.GET("/results/:id", controller.DeleteMonitoringResult) // Note: Should be DELETE but using GET for compatibility
			monitorRoute.DELETE("/results/:id", controller.DeleteMonitoringResult)
			monitorRoute.DELETE("/results/cleanup", controller.CleanupOldMonitoringResults)
			monitorRoute.GET("/statistics", controller.GetMonitoringStatistics)
			monitorRoute.GET("/failed_channels", controller.GetFailedChannels)
		}

		// Channel Monitoring Results (can be accessed by channel routes for convenience)
		channelRoute.GET("/:id/monitoring_results", controller.GetChannelMonitoringResults)

		// Session Monitoring Routes (Admin only)
		sessionsRoute := apiRouter.Group("/admin/sessions")
		sessionsRoute.Use(middleware.AdminAuth())
		{
			sessionsRoute.GET("/summary", controller.GetSessionsSummary)                  // Get session monitoring summary
			sessionsRoute.GET("/user/:id", controller.GetUserSessionCount)                // Get specific user's session count
			sessionsRoute.POST("/cleanup/:channel_id", controller.CleanupChannelSessions) // Clean up sessions for a channel
		}
	}
}
