package cliproxy

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// DeleteCredential deletes a credential from CLIProxyAPI service
// baseUrl: CLIProxyAPI service base URL (e.g., "http://localhost:8081")
// apiKey: CLIProxyAPI service API key
// accountHint: The credential identifier to delete
//
// API Endpoint: DELETE /api/credentials/{account_hint}
// Note: This endpoint uses RESTful path parameters instead of the originally
// proposed /v0/management/auth-files?name= query parameter approach.
// This is the agreed-upon interface between NewAPI and CLIProxyAPI.
func DeleteCredential(baseUrl, apiKey, accountHint string) error {
	if accountHint == "" {
		return fmt.Errorf("account_hint is empty, cannot delete credential")
	}

	// Ensure baseUrl doesn't end with slash
	baseUrl = strings.TrimSuffix(baseUrl, "/")

	// Build DELETE request URL
	url := fmt.Sprintf("%s/api/credentials/%s", baseUrl, accountHint)

	// Create HTTP request
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete credential request: %w", err)
	}

	// Set authorization header
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send delete credential request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode == http.StatusNotFound {
		// Credential not found - this is acceptable (might have been deleted already)
		common.SysLog(fmt.Sprintf("CLIProxyAPI credential %s not found, may have been deleted already", accountHint))
		return nil
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete credential from CLIProxyAPI, status code: %d", resp.StatusCode)
	}

	common.SysLog(fmt.Sprintf("Successfully deleted CLIProxyAPI credential: %s", accountHint))
	return nil
}
