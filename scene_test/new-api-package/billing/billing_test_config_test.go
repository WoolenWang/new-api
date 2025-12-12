package billing_test

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/scene_test/testutil"
)

// updateBillingOption is a small helper to call /api/option for billing-related settings.
// It mirrors the helper used in data-plane billing tests so that package billing
// scenarios share the same ratio configuration contract.
func updateBillingOption(t *testing.T, client *testutil.APIClient, key, value string) {
	t.Helper()

	var resp testutil.APIResponse
	body := map[string]any{
		"key":   key,
		"value": value,
	}
	if err := client.PutJSON("/api/option", body, &resp); err != nil {
		t.Fatalf("failed to update option %s via /api/option: %v", key, err)
	}
	if !resp.Success {
		t.Fatalf("update option %s failed: %s", key, resp.Message)
	}
}

// configurePackageBillingEnvironment configures ModelRatio / GroupRatio / CompletionRatio
// for package billing tests so that the runtime billing formula matches the test
// expectations documented in:
//   - scene_test/new-api-package/billing/README.md
//   - scene_test/new-api-package/billing/BILLING_TEST_IMPLEMENTATION_SUMMARY.md
//
// Key expectations for these tests:
//   - CompletionRatio (gpt-4, gpt-3.5) = 1.0
//   - ModelRatio: gpt-4 = 2.0, gpt-3.5 = 1.0
//   - GroupRatio: default = 1.0, vip = 2.0, svip = 0.8
func configurePackageBillingEnvironment(t *testing.T, client *testutil.APIClient) {
	t.Helper()

	// 1. GroupRatio: default=1.0, vip=2.0, svip=0.8
	groupRatio := map[string]float64{
		"default": 1.0,
		"vip":     2.0,
		"svip":    0.8,
	}
	grBytes, err := json.Marshal(groupRatio)
	if err != nil {
		t.Fatalf("failed to marshal GroupRatio: %v", err)
	}
	updateBillingOption(t, client, "GroupRatio", string(grBytes))

	// 2. Optional GroupGroupRatio: configure basic override used by other billing tests.
	// Package billing tests do not rely on it directly, but keeping it consistent
	// avoids surprises when combining with routing / anti-downgrade scenarios.
	groupGroupRatio := map[string]map[string]float64{
		"svip": {
			"default": 0.5,
		},
	}
	ggrBytes, err := json.Marshal(groupGroupRatio)
	if err != nil {
		t.Fatalf("failed to marshal GroupGroupRatio: %v", err)
	}
	updateBillingOption(t, client, "GroupGroupRatio", string(ggrBytes))

	// 3. ModelRatio: align gpt-4 / gpt-3.5 with the simplified ratios used in tests.
	modelRatio := map[string]float64{
		"gpt-4":   2.0,
		"gpt-3.5": 1.0,
	}
	mrBytes, err := json.Marshal(modelRatio)
	if err != nil {
		t.Fatalf("failed to marshal ModelRatio: %v", err)
	}
	updateBillingOption(t, client, "ModelRatio", string(mrBytes))

	// 4. CompletionRatio: tests assume completion multiplier = 1.0 for both models.
	completionRatio := map[string]float64{
		"gpt-4":   1.0,
		"gpt-3.5": 1.0,
	}
	crBytes, err := json.Marshal(completionRatio)
	if err != nil {
		t.Fatalf("failed to marshal CompletionRatio: %v", err)
	}
	updateBillingOption(t, client, "CompletionRatio", string(crBytes))

	t.Logf("Package billing environment configured: GroupRatio=%v, ModelRatio=%v, CompletionRatio=%v",
		groupRatio, modelRatio, completionRatio)
}
