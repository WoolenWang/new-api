package setting

import (
	"encoding/json"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var SystemMaxConcurrentSessions = 5

var (
	groupMaxConcurrentSessions      = map[string]int{}
	groupMaxConcurrentSessionsMutex sync.RWMutex
)

// GroupMaxConcurrentSessions2JSONString exports the per-group concurrent session limits.
func GroupMaxConcurrentSessions2JSONString() string {
	groupMaxConcurrentSessionsMutex.RLock()
	defer groupMaxConcurrentSessionsMutex.RUnlock()

	data, err := json.Marshal(groupMaxConcurrentSessions)
	if err != nil {
		common.SysLog("failed to marshal group concurrent session limits: " + err.Error())
		return "{}"
	}
	return string(data)
}

// UpdateGroupMaxConcurrentSessionsByJSONString imports the per-group concurrent session limits.
func UpdateGroupMaxConcurrentSessionsByJSONString(jsonStr string) error {
	groupMaxConcurrentSessionsMutex.Lock()
	defer groupMaxConcurrentSessionsMutex.Unlock()

	limits := map[string]int{}
	if err := json.Unmarshal([]byte(jsonStr), &limits); err != nil {
		return err
	}
	groupMaxConcurrentSessions = limits
	return nil
}

// GetGroupMaxConcurrentSessions returns the configured limit for a group, 0 if unset.
func GetGroupMaxConcurrentSessions(group string) int {
	groupMaxConcurrentSessionsMutex.RLock()
	defer groupMaxConcurrentSessionsMutex.RUnlock()

	if limit, ok := groupMaxConcurrentSessions[group]; ok {
		return limit
	}
	return 0
}
