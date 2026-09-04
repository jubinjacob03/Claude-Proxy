//go:build !windows

package licensing

import (
	"fmt"
	"os"
)

// machineIdentifier falls back to /etc/machine-id on Linux (stable per OS
// install) or the hostname as a last resort. The proxy ships for Windows, so
// this path exists for local development and tests only.
func machineIdentifier() (string, error) {
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		return string(data), nil
	}
	host, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("licensing: no machine identifier available: %w", err)
	}
	return host, nil
}

// HWID returns this machine's hashed hardware id.
func HWID() (string, error) {
	raw, err := machineIdentifier()
	if err != nil {
		return "", err
	}
	return hashHWID(raw), nil
}
