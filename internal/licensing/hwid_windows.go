//go:build windows

package licensing

import "golang.org/x/sys/windows/registry"

// machineIdentifier reads the Windows-assigned MachineGuid, which is created
// once at OS install time and stable across reboots, user accounts, and most
// hardware changes short of a reinstall.
func machineIdentifier() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return "", err
	}
	defer k.Close()

	guid, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return "", err
	}
	return guid, nil
}

// HWID returns this machine's hashed hardware id.
func HWID() (string, error) {
	raw, err := machineIdentifier()
	if err != nil {
		return "", err
	}
	return hashHWID(raw), nil
}
