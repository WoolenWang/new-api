// Package checkin contains end-to-end tests for the user daily check-in feature.
//
// Covered scenarios (aligned with design & test docs):
// - C-CHK-01: Basic daily checkin flow (status, reward, streak=1, logs)
// - C-CHK-02: Daily once only (second checkin on same day should fail, no extra reward/log)
// - C-CHK-03: Streak bonus when reaching streak_days (using DB to simulate previous days)
// - C-CHK-04: Checkin disabled via admin option (no reward, no logs)
package checkin

import (
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// CheckinSuite holds shared resources for checkin tests.
type CheckinSuite struct {
	Server      *testutil.TestServer
	AdminClient *testutil.APIClient
}

// setupCheckinSuite starts a fresh server and logs in as root (admin).
func setupCheckinSuite(t *testing.T) (*CheckinSuite, func()) {
	t.Helper()

	projectRoot, err := testutil.FindProjectRoot()
	require.NoError(t, err, "failed to find project root")

	cfg := testutil.DefaultConfig()
	cfg.ProjectRoot = projectRoot
	cfg.Verbose = testing.Verbose()

	server, err := testutil.StartServer(cfg)
	require.NoError(t, err, "failed to start test server")

	adminClient := testutil.NewAPIClient(server)

	// Initialize system and login as root.
	rootUser, rootPass, err := adminClient.InitializeSystem()
	require.NoError(t, err, "failed to initialize system")

	_, err = adminClient.Login(rootUser, rootPass)
	require.NoError(t, err, "failed to login as root")

	suite := &CheckinSuite{
		Server:      server,
		AdminClient: adminClient,
	}

	cleanup := func() {
		if suite.Server != nil {
			_ = suite.Server.Stop()
		}
	}

	return suite, cleanup
}

// updateOption is a small helper to call /api/option.
func updateOption(client *testutil.APIClient, key, value string) error {
	var resp testutil.APIResponse
	body := map[string]any{
		"key":   key,
		"value": value,
	}
	if err := client.PutJSON("/api/option", body, &resp); err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("update option %s failed: %s", key, resp.Message)
	}
	return nil
}

// configureCheckinSettings configures the checkin feature via admin options.
func configureCheckinSettings(t *testing.T, client *testutil.APIClient, enabled bool, dailyQuota, bonusQuota, streakDays int) {
	t.Helper()

	err := updateOption(client, "checkin_setting.enabled", strconv.FormatBool(enabled))
	require.NoError(t, err, "failed to update checkin_setting.enabled")

	err = updateOption(client, "checkin_setting.daily_quota", strconv.Itoa(dailyQuota))
	require.NoError(t, err, "failed to update checkin_setting.daily_quota")

	err = updateOption(client, "checkin_setting.streak_bonus_quota", strconv.Itoa(bonusQuota))
	require.NoError(t, err, "failed to update checkin_setting.streak_bonus_quota")

	err = updateOption(client, "checkin_setting.streak_days", strconv.Itoa(streakDays))
	require.NoError(t, err, "failed to update checkin_setting.streak_days")
}

// TestUser wraps a created user and its client.
type TestUser struct {
	ID       int
	Username string
	Client   *testutil.APIClient
}

// createCheckinUser creates a normal user and returns its TestUser wrapper.
func createCheckinUser(t *testing.T, suite *CheckinSuite, username string) *TestUser {
	t.Helper()

	externalID := fmt.Sprintf("checkin_%s_%d", username, time.Now().UnixNano())

	req := &testutil.UserModel{
		Username:   username,
		Password:   "testpass123",
		Group:      "default",
		Role:       1, // common user
		Status:     1, // enabled
		ExternalId: externalID,
	}

	id, err := suite.AdminClient.CreateUserFull(req)
	require.NoError(t, err, "failed to create user %s", username)

	// New client for the user session.
	userClient := testutil.NewAPIClient(suite.Server)
	_, err = userClient.Login(username, "testpass123")
	require.NoError(t, err, "failed to login user %s", username)

	return &TestUser{
		ID:       id,
		Username: username,
		Client:   userClient,
	}
}

// getUserQuota retrieves the current quota for a given user ID.
func getUserQuota(t *testing.T, client *testutil.APIClient, userID int) int {
	t.Helper()
	user, err := client.GetUser(userID)
	require.NoError(t, err, "failed to get user %d", userID)
	return int(user.Quota)
}

// openUserDB opens a gorm DB connection to the server's SQLite file.
func openUserDB(t *testing.T, server *testutil.TestServer) *gorm.DB {
	t.Helper()
	dbFile := filepath.Join(server.DataDir, "one-api.db")
	db, err := gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	require.NoError(t, err, "failed to open sqlite db at %s", dbFile)
	return db
}

// setUserCheckinState updates last_checkin_time and checkin_streak for a user.
func setUserCheckinState(t *testing.T, db *gorm.DB, userID int, lastCheckinTime int64, streak int) {
	t.Helper()
	err := db.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"last_checkin_time": lastCheckinTime,
		"checkin_streak":    streak,
	}).Error
	require.NoError(t, err, "failed to update checkin state for user %d", userID)
}

// CheckinStatusResponse mirrors GET /api/user/checkin/status.
type CheckinStatusResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Enabled           bool  `json:"enabled"`
		HasCheckedInToday bool  `json:"has_checked_in_today"`
		CurrentStreak     int   `json:"current_streak"`
		LastCheckinTime   int64 `json:"last_checkin_time"`
		StreakDays        int   `json:"streak_days"`
		DaysUntilBonus    int   `json:"days_until_bonus"`
		DailyQuota        int   `json:"daily_quota"`
		StreakBonusQuota  int   `json:"streak_bonus_quota"`
	} `json:"data"`
}

// CheckinResponse mirrors POST /api/user/checkin.
type CheckinResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Quota         int  `json:"quota"`
		BonusQuota    int  `json:"bonus_quota"`
		TotalQuota    int  `json:"total_quota"`
		CurrentStreak int  `json:"current_streak"`
		IsStreakBonus bool `json:"is_streak_bonus"`
	} `json:"data"`
}

// UserLog is a minimal view of a log entry for /api/log/self.
type UserLog struct {
	Type    int    `json:"type"`
	Quota   int    `json:"quota"`
	Content string `json:"content"`
}

// getUserCheckinLogs returns recent checkin logs for the current user.
func getUserCheckinLogs(t *testing.T, userClient *testutil.APIClient) []UserLog {
	t.Helper()

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Page     int       `json:"page"`
			PageSize int       `json:"page_size"`
			Total    int       `json:"total"`
			Items    []UserLog `json:"items"`
		} `json:"data"`
	}

	path := "/api/log/self?type=9&p=1&page_size=20"
	err := userClient.GetJSON(path, &resp)
	require.NoError(t, err, "failed to call /api/log/self")
	require.True(t, resp.Success, "/api/log/self should succeed: %s", resp.Message)

	return resp.Data.Items
}

// TestCheckin_CCHK01_BasicFlow
// C-CHK-01: Basic daily checkin flow.
func TestCheckin_CCHK01_BasicFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration checkin test in short mode")
	}

	suite, cleanup := setupCheckinSuite(t)
	defer cleanup()

	const (
		dailyQuota = 1000
		bonusQuota = 5000
		streakDays = 7
	)

	configureCheckinSettings(t, suite.AdminClient, true, dailyQuota, bonusQuota, streakDays)

	user := createCheckinUser(t, suite, "chk01-user")

	// Initial status: not checked in today, streak=0, config reflected.
	var status CheckinStatusResponse
	err := user.Client.GetJSON("/api/user/checkin/status", &status)
	require.NoError(t, err, "failed to get checkin status")
	require.True(t, status.Success)
	require.True(t, status.Data.Enabled)
	require.False(t, status.Data.HasCheckedInToday)
	require.Equal(t, 0, status.Data.CurrentStreak)
	require.Equal(t, dailyQuota, status.Data.DailyQuota)
	require.Equal(t, bonusQuota, status.Data.StreakBonusQuota)
	require.Equal(t, streakDays, status.Data.StreakDays)
	require.Equal(t, streakDays, status.Data.DaysUntilBonus)

	initialQuota := getUserQuota(t, suite.AdminClient, user.ID)

	// First checkin of the day should succeed and grant dailyQuota.
	var chkResp CheckinResponse
	err = user.Client.PostJSON("/api/user/checkin", map[string]any{}, &chkResp)
	require.NoError(t, err, "failed to call /api/user/checkin")
	require.True(t, chkResp.Success, "first checkin should succeed: %s", chkResp.Message)
	require.Equal(t, dailyQuota, chkResp.Data.Quota)
	require.Equal(t, 0, chkResp.Data.BonusQuota)
	require.Equal(t, dailyQuota, chkResp.Data.TotalQuota)
	require.Equal(t, 1, chkResp.Data.CurrentStreak)
	require.False(t, chkResp.Data.IsStreakBonus)

	newQuota := getUserQuota(t, suite.AdminClient, user.ID)
	require.Equal(t, initialQuota+dailyQuota, newQuota, "user quota should increase by dailyQuota")

	// Check that a checkin log was recorded.
	logs := getUserCheckinLogs(t, user.Client)
	require.Len(t, logs, 1, "expected exactly one checkin log")
	require.Equal(t, 9, logs[0].Type)
	require.Equal(t, dailyQuota, logs[0].Quota)

	// Status after checkin: already checked in today, streak=1, days_until_bonus=streakDays-1.
	var status2 CheckinStatusResponse
	err = user.Client.GetJSON("/api/user/checkin/status", &status2)
	require.NoError(t, err, "failed to get checkin status after checkin")
	require.True(t, status2.Success)
	require.True(t, status2.Data.Enabled)
	require.True(t, status2.Data.HasCheckedInToday)
	require.Equal(t, 1, status2.Data.CurrentStreak)
	require.Equal(t, streakDays-1, status2.Data.DaysUntilBonus)
}

// TestCheckin_CCHK02_DailyOnceOnly
// C-CHK-02: Daily once only (second checkin same day should fail, quota/log unchanged).
func TestCheckin_CCHK02_DailyOnceOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration checkin test in short mode")
	}

	suite, cleanup := setupCheckinSuite(t)
	defer cleanup()

	const (
		dailyQuota = 800
		bonusQuota = 0
		streakDays = 7
	)

	configureCheckinSettings(t, suite.AdminClient, true, dailyQuota, bonusQuota, streakDays)

	user := createCheckinUser(t, suite, "chk02-user")

	initialQuota := getUserQuota(t, suite.AdminClient, user.ID)

	// First checkin: should succeed.
	var firstResp CheckinResponse
	err := user.Client.PostJSON("/api/user/checkin", map[string]any{}, &firstResp)
	require.NoError(t, err)
	require.True(t, firstResp.Success)

	quotaAfterFirst := getUserQuota(t, suite.AdminClient, user.ID)
	require.Equal(t, initialQuota+dailyQuota, quotaAfterFirst)

	logsAfterFirst := getUserCheckinLogs(t, user.Client)
	require.Len(t, logsAfterFirst, 1)

	// Second checkin on the same day: should fail with "今日已签到" and not change quota/logs.
	var secondResp CheckinResponse
	err = user.Client.PostJSON("/api/user/checkin", map[string]any{}, &secondResp)
	require.NoError(t, err, "second checkin request should not error at transport level")
	require.False(t, secondResp.Success, "second checkin should fail")
	require.Contains(t, secondResp.Message, "已签到", "second checkin message should indicate already checked in")

	quotaAfterSecond := getUserQuota(t, suite.AdminClient, user.ID)
	require.Equal(t, quotaAfterFirst, quotaAfterSecond, "quota should not change on second checkin")

	logsAfterSecond := getUserCheckinLogs(t, user.Client)
	require.Len(t, logsAfterSecond, 1, "no additional checkin log should be created on second checkin")
}

// TestCheckin_CCHK03_StreakBonus
// C-CHK-03: Streak bonus when reaching configured streak_days.
func TestCheckin_CCHK03_StreakBonus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration checkin test in short mode")
	}

	suite, cleanup := setupCheckinSuite(t)
	defer cleanup()

	// Use small streak window to simplify the test.
	const (
		dailyQuota = 100
		bonusQuota = 500
		streakDays = 3
	)

	configureCheckinSettings(t, suite.AdminClient, true, dailyQuota, bonusQuota, streakDays)

	user := createCheckinUser(t, suite, "chk03-user")

	initialQuota := getUserQuota(t, suite.AdminClient, user.ID)

	// Simulate that the user has already checked in streakDays-1 days and last checked in "yesterday".
	db := openUserDB(t, suite.Server)
	now := time.Now().Unix()
	yesterday := now - 24*60*60
	setUserCheckinState(t, db, user.ID, yesterday, streakDays-1)

	// Status before today's checkin: current_streak = streakDays-1, days_until_bonus = 1.
	var status CheckinStatusResponse
	err := user.Client.GetJSON("/api/user/checkin/status", &status)
	require.NoError(t, err, "failed to get checkin status before streak day")
	require.True(t, status.Success)
	require.True(t, status.Data.Enabled)
	require.False(t, status.Data.HasCheckedInToday)
	require.Equal(t, streakDays-1, status.Data.CurrentStreak)
	require.Equal(t, 1, status.Data.DaysUntilBonus)

	// Today's checkin should grant daily + bonus quota and set IsStreakBonus=true.
	var chkResp CheckinResponse
	err = user.Client.PostJSON("/api/user/checkin", map[string]any{}, &chkResp)
	require.NoError(t, err, "failed to call /api/user/checkin on streak day")
	require.True(t, chkResp.Success)
	require.Equal(t, dailyQuota, chkResp.Data.Quota)
	require.Equal(t, bonusQuota, chkResp.Data.BonusQuota)
	require.Equal(t, dailyQuota+bonusQuota, chkResp.Data.TotalQuota)
	require.Equal(t, streakDays, chkResp.Data.CurrentStreak)
	require.True(t, chkResp.Data.IsStreakBonus)

	newQuota := getUserQuota(t, suite.AdminClient, user.ID)
	require.Equal(t, initialQuota+dailyQuota+bonusQuota, newQuota, "quota should increase by daily+bonus on streak day")

	// Verify log records total reward for this streak day.
	logs := getUserCheckinLogs(t, user.Client)
	require.Len(t, logs, 1, "expected exactly one checkin log")
	require.Equal(t, 9, logs[0].Type)
	require.Equal(t, dailyQuota+bonusQuota, logs[0].Quota)

	// Status after streak bonus: current_streak=streakDays, days_until_bonus=streakDays.
	var status2 CheckinStatusResponse
	err = user.Client.GetJSON("/api/user/checkin/status", &status2)
	require.NoError(t, err, "failed to get checkin status after streak bonus")
	require.True(t, status2.Success)
	require.True(t, status2.Data.HasCheckedInToday)
	require.Equal(t, streakDays, status2.Data.CurrentStreak)
	require.Equal(t, streakDays, status2.Data.DaysUntilBonus)
}

// TestCheckin_CCHK04_Disabled
// C-CHK-04: Checkin disabled via options (no reward, no logs).
func TestCheckin_CCHK04_Disabled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration checkin test in short mode")
	}

	suite, cleanup := setupCheckinSuite(t)
	defer cleanup()

	const (
		dailyQuota = 1000
		bonusQuota = 5000
		streakDays = 7
	)

	// Disable checkin.
	configureCheckinSettings(t, suite.AdminClient, false, dailyQuota, bonusQuota, streakDays)

	user := createCheckinUser(t, suite, "chk04-user")

	initialQuota := getUserQuota(t, suite.AdminClient, user.ID)

	// Status should report enabled=false.
	var status CheckinStatusResponse
	err := user.Client.GetJSON("/api/user/checkin/status", &status)
	require.NoError(t, err, "failed to get checkin status when disabled")
	require.True(t, status.Success)
	require.False(t, status.Data.Enabled)

	// Checkin call should fail with "签到功能已关闭" and not change quota or logs.
	var chkResp CheckinResponse
	err = user.Client.PostJSON("/api/user/checkin", map[string]any{}, &chkResp)
	require.NoError(t, err, "disabled checkin request should not error at transport level")
	require.False(t, chkResp.Success, "checkin should fail when disabled")
	require.Contains(t, chkResp.Message, "签到功能已关闭")

	newQuota := getUserQuota(t, suite.AdminClient, user.ID)
	require.Equal(t, initialQuota, newQuota, "quota should not change when checkin is disabled")

	logs := getUserCheckinLogs(t, user.Client)
	require.Len(t, logs, 0, "no checkin logs should be created when disabled")
}
