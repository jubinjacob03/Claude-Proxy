package licensing

import "fmt"

// EnsureActivated returns a token usable against the relay, activating this
// machine first if no cached token exists yet. dir is where the token cache
// lives (the executable's directory in production).
//
// This is the client's entire trust boundary: once it holds a token it never
// needs the licence key again, and every subsequent proxy request carries only
// this token, never an upstream API key.
func EnsureActivated(client *Client, dir, licenseKey string) (string, error) {
	hwid, err := HWID()
	if err != nil {
		return "", fmt.Errorf("licensing: could not determine this machine's hardware id: %w", err)
	}

	if cached, err := loadCache(dir); err == nil {
		if cached.RelayBaseURL == client.BaseURL && cached.HWID == hwid && cached.Token != "" {
			return cached.Token, nil
		}
	}

	if licenseKey == "" {
		return "", ErrNotLicensed
	}

	token, err := client.Activate(licenseKey, hwid)
	if err != nil {
		return "", err
	}
	// A cache write failure must not block startup: the token is still valid
	// for this run, it will just re-activate (idempotently) next launch.
	_ = saveCache(dir, &cachedActivation{RelayBaseURL: client.BaseURL, Token: token, HWID: hwid})
	return token, nil
}
