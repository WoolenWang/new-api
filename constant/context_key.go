package constant

type ContextKey string

const (
	ContextKeyTokenCountMeta ContextKey = "token_count_meta"
	ContextKeyPromptTokens   ContextKey = "prompt_tokens"

	ContextKeyOriginalModel    ContextKey = "original_model"
	ContextKeyRequestStartTime ContextKey = "request_start_time"

	/* token related keys */
	ContextKeyTokenUnlimited         ContextKey = "token_unlimited_quota"
	ContextKeyTokenKey               ContextKey = "token_key"
	ContextKeyTokenId                ContextKey = "token_id"
	ContextKeyTokenGroup             ContextKey = "token_group"
	ContextKeyTokenSpecificChannelId ContextKey = "specific_channel_id"
	ContextKeyTokenModelLimitEnabled ContextKey = "token_model_limit_enabled"
	ContextKeyTokenModelLimit        ContextKey = "token_model_limit"
	ContextKeyTokenAllowedP2PGroups  ContextKey = "token_allowed_p2p_groups" // P2P 分组限制 ([]int)

	/* channel related keys */
	ContextKeyChannelId                ContextKey = "channel_id"
	ContextKeyChannelName              ContextKey = "channel_name"
	ContextKeyChannelCreateTime        ContextKey = "channel_create_time"
	ContextKeyChannelBaseUrl           ContextKey = "base_url"
	ContextKeyChannelType              ContextKey = "channel_type"
	ContextKeyChannelSetting           ContextKey = "channel_setting"
	ContextKeyChannelOtherSetting      ContextKey = "channel_other_setting"
	ContextKeyChannelParamOverride     ContextKey = "param_override"
	ContextKeyChannelHeaderOverride    ContextKey = "header_override"
	ContextKeyChannelOrganization      ContextKey = "channel_organization"
	ContextKeyChannelAutoBan           ContextKey = "auto_ban"
	ContextKeyChannelModelMapping      ContextKey = "model_mapping"
	ContextKeyChannelStatusCodeMapping ContextKey = "status_code_mapping"
	ContextKeyChannelIsMultiKey        ContextKey = "channel_is_multi_key"
	ContextKeyChannelMultiKeyIndex     ContextKey = "channel_multi_key_index"
	ContextKeyChannelKey               ContextKey = "channel_key"
	ContextKeyChannelAccountHint       ContextKey = "account_hint"
	ContextKeyChannelForcedKey         ContextKey = "channel_forced_key"
	ContextKeyChannelForcedKeyIndex    ContextKey = "channel_forced_key_index"
	ContextKeyStickyChannelId          ContextKey = "sticky_channel_id"

	/* user related keys */
	ContextKeyUserId                    ContextKey = "id"
	ContextKeyUserSetting               ContextKey = "user_setting"
	ContextKeyUserQuota                 ContextKey = "user_quota"
	ContextKeyUserStatus                ContextKey = "user_status"
	ContextKeyUserEmail                 ContextKey = "user_email"
	ContextKeyUserGroup                 ContextKey = "user_group"
	ContextKeyUsingGroup                ContextKey = "group"
	ContextKeyUserName                  ContextKey = "username"
	ContextKeyUserMaxConcurrentSessions ContextKey = "user_max_concurrent_sessions"

	/* session related keys */
	ContextKeySessionID            ContextKey = "session_id"
	ContextKeySessionBindingKey    ContextKey = "session_binding_key"
	ContextKeySessionBindingHit    ContextKey = "session_binding_hit"
	ContextKeySessionIsNew         ContextKey = "session_is_new"
	ContextKeySessionSelectedGroup ContextKey = "session_selected_group"

	ContextKeyLocalCountTokens ContextKey = "local_count_tokens"

	ContextKeySystemPromptOverride ContextKey = "system_prompt_override"
)
