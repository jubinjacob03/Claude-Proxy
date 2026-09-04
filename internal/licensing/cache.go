package licensing

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// cachedActivation is what survives on disk after a successful activation, so
// the licence key never has to be entered again. It is not a security boundary
// by itself — the token is the credential — it only saves the user a step.
type cachedActivation struct {
	RelayBaseURL string `json:"relay_base_url"`
	Token        string `json:"token"`
	HWID         string `json:"hwid"`
}

func cachePath(dir string) string {
	return filepath.Join(dir, "license.json")
}

func loadCache(dir string) (*cachedActivation, error) {
	data, err := os.ReadFile(cachePath(dir))
	if err != nil {
		return nil, err
	}
	var c cachedActivation
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveCache(dir string, c *cachedActivation) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath(dir), data, 0o600)
}

func deleteCache(dir string) error {
	if err := os.Remove(cachePath(dir)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type DebugActivationStatus struct {
	RelayBaseURL string
	HWID         string
}

func DebugStatus(dir string) (*DebugActivationStatus, error) {
	cached, err := loadCache(dir)
	if err != nil {
		return nil, err
	}
	return &DebugActivationStatus{
		RelayBaseURL: cached.RelayBaseURL,
		HWID:         cached.HWID,
	}, nil
}
