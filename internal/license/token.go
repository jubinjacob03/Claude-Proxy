package license

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Token is what the client keeps after activation so the user never types the
// licence key again. It is an HMAC over the licence id and the machine it was
// bound to, which means a stolen token is useless on another machine: the relay
// recomputes the HWID from the caller and the signature stops matching.
//
// Tokens carry no expiry because a licence ends when its quota runs out, not
// when a clock says so.
type Token struct {
	LicenseID string
	HWID      string
}

var ErrBadToken = errors.New("invalid activation token")

// Sign produces the opaque token handed to the client.
func Sign(secret []byte, licenseID, hwid string) string {
	payload := licenseID + ":" + hwid
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + mac(secret, payload)
}

// Verify checks a token and returns the licence it refers to. The caller must
// still confirm the licence is active and funded.
func Verify(secret []byte, token string) (*Token, error) {
	encoded, sig, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok {
		return nil, ErrBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrBadToken
	}
	payload := string(raw)
	if !hmac.Equal([]byte(sig), []byte(mac(secret, payload))) {
		return nil, ErrBadToken
	}
	licenseID, hwid, ok := strings.Cut(payload, ":")
	if !ok || licenseID == "" || hwid == "" {
		return nil, ErrBadToken
	}
	return &Token{LicenseID: licenseID, HWID: hwid}, nil
}

func mac(secret []byte, payload string) string {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
