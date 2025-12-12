package dto

// PackageCreateRequest 创建套餐请求
type PackageCreateRequest struct {
	Name              string `json:"name" binding:"required"`
	Description       string `json:"description"`
	Priority          int    `json:"priority" binding:"min=1,max=21"`
	P2PGroupId        int    `json:"p2p_group_id"`
	Quota             int64  `json:"quota" binding:"required,min=1"`
	DurationType      string `json:"duration_type" binding:"required,oneof=week month quarter year"`
	Duration          int    `json:"duration" binding:"required,min=1"`
	RpmLimit          int    `json:"rpm_limit"`
	HourlyLimit       int64  `json:"hourly_limit"`
	FourHourlyLimit   int64  `json:"four_hourly_limit"`
	DailyLimit        int64  `json:"daily_limit"`
	WeeklyLimit       int64  `json:"weekly_limit"`
	FallbackToBalance bool   `json:"fallback_to_balance"`
}

// PackageUpdateRequest 更新套餐请求（部分字段可选）
type PackageUpdateRequest struct {
	Id                *int    `json:"id" binding:"required"`
	Name              *string `json:"name"`
	Description       *string `json:"description"`
	Status            *int    `json:"status"`
	RpmLimit          *int    `json:"rpm_limit"`
	HourlyLimit       *int64  `json:"hourly_limit"`
	FourHourlyLimit   *int64  `json:"four_hourly_limit"`
	DailyLimit        *int64  `json:"daily_limit"`
	WeeklyLimit       *int64  `json:"weekly_limit"`
	FallbackToBalance *bool   `json:"fallback_to_balance"`
}

// SubscriptionResponse 订阅响应（扩展信息）
type SubscriptionResponse struct {
	SubscriptionId int    `json:"subscription_id"`
	UserId         int    `json:"user_id"`
	PackageId      int    `json:"package_id"`
	PackageName    string `json:"package_name"`
	Priority       int    `json:"priority"`
	Status         string `json:"status"`
	TotalConsumed  int64  `json:"total_consumed"`
	TotalQuota     int64  `json:"total_quota"`
	RemainingQuota int64  `json:"remaining_quota"`
	StartTime      *int64 `json:"start_time,omitempty"`
	EndTime        *int64 `json:"end_time,omitempty"`
	SubscribedAt   int64  `json:"subscribed_at"`
}

// SlidingWindowStatusDTO 滑动窗口状态 DTO
type SlidingWindowStatusDTO struct {
	Period       string `json:"period"`                   // rpm, hourly, 4hourly, daily, weekly
	IsActive     bool   `json:"is_active"`                // 窗口是否活跃
	Consumed     int64  `json:"consumed"`                 // 已消耗
	Limit        int64  `json:"limit"`                    // 限额
	Remaining    int64  `json:"remaining"`                // 剩余额度
	StartTime    int64  `json:"start_time"`               // 窗口开始时间（Unix时间戳）
	EndTime      int64  `json:"end_time"`                 // 窗口结束时间（Unix时间戳）
	TimeLeft     int64  `json:"time_left"`                // 剩余时间（秒）
	StartTimeStr string `json:"start_time_str,omitempty"` // 格式化后的开始时间
	EndTimeStr   string `json:"end_time_str,omitempty"`   // 格式化后的结束时间
}

// SubscriptionStatusResponse 订阅详细状态响应（含滑动窗口）
type SubscriptionStatusResponse struct {
	SubscriptionId int                                `json:"subscription_id"`
	PackageName    string                             `json:"package_name"`
	PackageId      int                                `json:"package_id"`
	Status         string                             `json:"status"`
	Priority       int                                `json:"priority"`
	TotalQuota     int64                              `json:"total_quota"`
	TotalConsumed  int64                              `json:"total_consumed"`
	RemainingQuota int64                              `json:"remaining_quota"`
	StartTime      *int64                             `json:"start_time,omitempty"`
	EndTime        *int64                             `json:"end_time,omitempty"`
	DaysRemaining  *int64                             `json:"days_remaining,omitempty"`
	SlidingWindows map[string]*SlidingWindowStatusDTO `json:"sliding_windows"`
}
