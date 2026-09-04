package licensing

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashHWID turns a raw machine identifier into a fixed-length opaque id. The
// raw value never leaves this function: only the hash is sent to the relay,
// so the machine's real identifier is not exposed over the network.
func hashHWID(raw string) string {
	sum := sha256.Sum256([]byte("claude-proxy-hwid:" + raw))
	return hex.EncodeToString(sum[:])
}
