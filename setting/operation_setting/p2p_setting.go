package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// P2PSetting manages P2P channel sharing configuration
type P2PSetting struct {
	// ShareRatio is the revenue sharing ratio (0-1, e.g., 0.1 = 10%)
	// Channel owners receive ShareRatio * consumed_quota as revenue
	ShareRatio float64 `json:"share_ratio"`
}

// Default P2P configuration
var p2pSetting = P2PSetting{
	ShareRatio: 0.1, // Default 10% sharing ratio
}

func init() {
	// Register with global config manager
	config.GlobalConfig.Register("p2p_setting", &p2pSetting)
}

// GetP2PSetting returns current P2P configuration
func GetP2PSetting() *P2PSetting {
	return &p2pSetting
}

// GetShareRatio returns the current share ratio
func GetShareRatio() float64 {
	if p2pSetting.ShareRatio < 0 {
		return 0
	}
	if p2pSetting.ShareRatio > 1 {
		return 1
	}
	return p2pSetting.ShareRatio
}
