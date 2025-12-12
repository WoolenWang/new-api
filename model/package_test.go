package model

import (
	"testing"
	"time"
)

// TestGetDurationSeconds tests the duration calculation helper function
func TestGetDurationSeconds(t *testing.T) {
	tests := []struct {
		name         string
		durationType string
		duration     int
		wantErr      bool
	}{
		{"Week", "week", 1, false},
		{"Month", "month", 1, false},
		{"Quarter", "quarter", 1, false},
		{"Year", "year", 1, false},
		{"Invalid Type", "invalid", 1, true},
		{"Zero Duration", "month", 0, true},
		{"Negative Duration", "month", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seconds, err := GetDurationSeconds(tt.durationType, tt.duration)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetDurationSeconds() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && seconds <= 0 {
				t.Errorf("GetDurationSeconds() returned invalid seconds = %v", seconds)
			}
		})
	}
}

// TestCalculateEndTime tests end time calculation with different duration types
func TestCalculateEndTime(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name    string
		pkg     *Package
		wantErr bool
	}{
		{
			name: "Week Package",
			pkg: &Package{
				DurationType: "week",
				Duration:     2,
			},
			wantErr: false,
		},
		{
			name: "Month Package",
			pkg: &Package{
				DurationType: "month",
				Duration:     3,
			},
			wantErr: false,
		},
		{
			name: "Quarter Package",
			pkg: &Package{
				DurationType: "quarter",
				Duration:     1,
			},
			wantErr: false,
		},
		{
			name: "Year Package",
			pkg: &Package{
				DurationType: "year",
				Duration:     1,
			},
			wantErr: false,
		},
		{
			name: "Invalid Type",
			pkg: &Package{
				DurationType: "invalid",
				Duration:     1,
			},
			wantErr: true,
		},
		{
			name:    "Nil Package",
			pkg:     nil,
			wantErr: true,
		},
		{
			name: "Zero Duration",
			pkg: &Package{
				DurationType: "month",
				Duration:     0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endTime, err := CalculateEndTime(now, tt.pkg)
			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateEndTime() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if endTime <= now {
					t.Errorf("CalculateEndTime() endTime = %v should be greater than startTime = %v", endTime, now)
				}

				// Verify the duration type calculation is reasonable
				expectedMinDuration := int64(0)
				switch tt.pkg.DurationType {
				case "week":
					expectedMinDuration = int64(7 * 24 * 3600 * tt.pkg.Duration)
				case "month":
					expectedMinDuration = int64(28 * 24 * 3600 * tt.pkg.Duration) // Min days in a month
				case "quarter":
					expectedMinDuration = int64(90 * 24 * 3600 * tt.pkg.Duration)
				case "year":
					expectedMinDuration = int64(365 * 24 * 3600 * tt.pkg.Duration)
				}

				actualDuration := endTime - now
				if actualDuration < expectedMinDuration {
					t.Errorf("CalculateEndTime() duration too short: got %v seconds, expected at least %v seconds",
						actualDuration, expectedMinDuration)
				}
			}
		})
	}
}

// TestPackageTableName tests the TableName method
func TestPackageTableName(t *testing.T) {
	pkg := Package{}
	if pkg.TableName() != "packages" {
		t.Errorf("Package.TableName() = %v, want %v", pkg.TableName(), "packages")
	}
}

// TestSubscriptionTableName tests the TableName method
func TestSubscriptionTableName(t *testing.T) {
	sub := Subscription{}
	if sub.TableName() != "subscriptions" {
		t.Errorf("Subscription.TableName() = %v, want %v", sub.TableName(), "subscriptions")
	}
}

// TestSubscriptionHistoryTableName tests the TableName method
func TestSubscriptionHistoryTableName(t *testing.T) {
	history := SubscriptionHistory{}
	if history.TableName() != "subscription_history" {
		t.Errorf("SubscriptionHistory.TableName() = %v, want %v", history.TableName(), "subscription_history")
	}
}

// TestSubscriptionStatusConstants tests that status constants are defined correctly
func TestSubscriptionStatusConstants(t *testing.T) {
	expectedStatuses := map[string]string{
		"inventory": SubscriptionStatusInventory,
		"active":    SubscriptionStatusActive,
		"expired":   SubscriptionStatusExpired,
		"cancelled": SubscriptionStatusCancelled,
	}

	for expected, actual := range expectedStatuses {
		if actual != expected {
			t.Errorf("Status constant mismatch: expected %v, got %v", expected, actual)
		}
	}
}

// TestGetPackagesByIdsEmptyInput tests GetPackagesByIds with empty input
func TestGetPackagesByIdsEmptyInput(t *testing.T) {
	packages, err := GetPackagesByIds([]int{})
	if err != nil {
		t.Errorf("GetPackagesByIds() with empty input should not error, got: %v", err)
	}
	if len(packages) != 0 {
		t.Errorf("GetPackagesByIds() with empty input should return empty slice, got length: %v", len(packages))
	}
}

// BenchmarkCalculateEndTime benchmarks the end time calculation
func BenchmarkCalculateEndTime(b *testing.B) {
	now := time.Now().Unix()
	pkg := &Package{
		DurationType: "month",
		Duration:     1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CalculateEndTime(now, pkg)
	}
}

// BenchmarkGetDurationSeconds benchmarks the duration calculation
func BenchmarkGetDurationSeconds(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GetDurationSeconds("month", 1)
	}
}
